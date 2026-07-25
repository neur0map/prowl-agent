package workbench

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/prowl-agent/prowl-agent/internal/events"
	"github.com/prowl-agent/prowl-agent/internal/jobs"
)

const (
	maxJobResponseBytes = 4096
	maxCancelBodyBytes  = 512
	maxReplayEntries    = 256
)

type jobResponse struct {
	ID        string        `json:"id"`
	Kind      string        `json:"kind"`
	Status    string        `json:"status"`
	Version   uint64        `json:"version"`
	Phase     string        `json:"phase"`
	Progress  int           `json:"progress"`
	Outcome   string        `json:"outcome,omitempty"`
	ErrorCode string        `json:"error_code,omitempty"`
	CreatedAt string        `json:"created_at"`
	UpdatedAt string        `json:"updated_at"`
	Stream    events.Cursor `json:"stream"`
}

type cancelRequest struct {
	ExpectedVersion uint64 `json:"expected_version"`
	IdempotencyKey  string `json:"idempotency_key"`
}

type jobReplayCache struct {
	mu      sync.Mutex
	entries map[string]jobReplayEntry
	order   []string
}

type jobReplayEntry struct {
	jobID   string
	version uint64
	outcome jobReplayOutcome
}

type jobReplayOutcome struct {
	status  int
	code    string
	message string
	job     *jobResponse
}

func serveJobRoute(response http.ResponseWriter, request *http.Request, service *Service) {
	path := strings.TrimPrefix(request.URL.Path, "/api/v1/jobs/")
	if strings.HasSuffix(path, "/cancel") {
		serveJobCancel(response, request, service, strings.TrimSuffix(path, "/cancel"))
		return
	}
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeError(response, request, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed", unavailableVersion)
		return
	}
	if !validJobID(path) {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "request is invalid", unavailableVersion)
		return
	}
	live, err := service.liveOperations()
	if err != nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "service is unavailable", unavailableVersion)
		return
	}
	job, state, err := live.jobs.Snapshot(request.Context(), path)
	if err != nil {
		writeJobError(response, request, err)
		return
	}
	result, err := newJobResponse(job, state)
	if err != nil {
		writeError(response, request, http.StatusServiceUnavailable, "job_unavailable", "job is unavailable", unavailableVersion)
		return
	}
	writeJobSuccess(response, request, http.StatusOK, result)
}

func serveRefresh(response http.ResponseWriter, request *http.Request, service *Service) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		writeError(response, request, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed", unavailableVersion)
		return
	}
	if request.Body != nil && request.Body != http.NoBody {
		reader := http.MaxBytesReader(response, request.Body, 1)
		var byteRead [1]byte
		if count, err := reader.Read(byteRead[:]); count != 0 || err != io.EOF {
			writeError(response, request, http.StatusBadRequest, "invalid_request", "request is invalid", unavailableVersion)
			return
		}
	}
	live, err := service.liveOperations()
	if err != nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "service is unavailable", unavailableVersion)
		return
	}
	job, _, err := live.jobs.EnqueueOrResumeIndex(request.Context())
	if err != nil {
		writeJobError(response, request, err)
		return
	}
	job, state, err := live.jobs.Snapshot(request.Context(), job.ID)
	if err != nil {
		writeJobError(response, request, err)
		return
	}
	result, err := newJobResponse(job, state)
	if err != nil {
		writeError(response, request, http.StatusServiceUnavailable, "job_unavailable", "job is unavailable", unavailableVersion)
		return
	}
	writeJobSuccess(response, request, http.StatusAccepted, result)
}

func serveJobCancel(response http.ResponseWriter, request *http.Request, service *Service, id string) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		writeError(response, request, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed", unavailableVersion)
		return
	}
	if !validJobID(id) {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "request is invalid", unavailableVersion)
		return
	}
	input, err := decodeCancelRequest(response, request)
	if err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "request is invalid", unavailableVersion)
		return
	}
	live, err := service.liveOperations()
	if err != nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "service is unavailable", unavailableVersion)
		return
	}
	service.replay.mu.Lock()
	defer service.replay.mu.Unlock()
	if prior, conflict := service.replay.lookupLocked(input.IdempotencyKey, id, input.ExpectedVersion); conflict {
		writeError(response, request, http.StatusConflict, "idempotency_conflict", "request conflicts with prior request", unavailableVersion)
		return
	} else if prior != nil {
		writeReplayOutcome(response, request, *prior)
		return
	}
	job, err := live.jobs.Cancel(request.Context(), id, input.ExpectedVersion)
	if err != nil {
		outcome := jobErrorOutcome(err)
		service.replay.recordLocked(input.IdempotencyKey, id, input.ExpectedVersion, outcome)
		writeReplayOutcome(response, request, outcome)
		return
	}
	job, state, err := live.jobs.Snapshot(request.Context(), job.ID)
	if err != nil {
		outcome := jobErrorOutcome(err)
		service.replay.recordLocked(input.IdempotencyKey, id, input.ExpectedVersion, outcome)
		writeReplayOutcome(response, request, outcome)
		return
	}
	result, err := newJobResponse(job, state)
	if err != nil {
		outcome := jobUnavailableOutcome()
		service.replay.recordLocked(input.IdempotencyKey, id, input.ExpectedVersion, outcome)
		writeReplayOutcome(response, request, outcome)
		return
	}
	outcome := jobReplayOutcome{status: http.StatusOK, job: &result}
	service.replay.recordLocked(input.IdempotencyKey, id, input.ExpectedVersion, outcome)
	writeReplayOutcome(response, request, outcome)
}

func decodeCancelRequest(response http.ResponseWriter, request *http.Request) (cancelRequest, error) {
	var input cancelRequest
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, maxCancelBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return cancelRequest{}, err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return cancelRequest{}, errors.New("trailing JSON")
	}
	if input.ExpectedVersion == 0 || !safeKey(input.IdempotencyKey) {
		return cancelRequest{}, errors.New("invalid cancel request")
	}
	return input, nil
}

func (cache *jobReplayCache) lookupLocked(key, id string, version uint64) (*jobReplayOutcome, bool) {
	entry, found := cache.entries[key]
	if !found {
		return nil, false
	}
	if entry.jobID != id || entry.version != version {
		return nil, true
	}
	copy := entry.outcome
	if copy.job != nil {
		job := *copy.job
		copy.job = &job
	}
	return &copy, false
}

func (cache *jobReplayCache) recordLocked(key, id string, version uint64, outcome jobReplayOutcome) {
	if cache.entries == nil {
		cache.entries = make(map[string]jobReplayEntry, maxReplayEntries)
	}
	if _, exists := cache.entries[key]; exists {
		return
	}
	if len(cache.order) == maxReplayEntries {
		delete(cache.entries, cache.order[0])
		cache.order = cache.order[1:]
	}
	cache.entries[key] = jobReplayEntry{jobID: id, version: version, outcome: outcome}
	cache.order = append(cache.order, key)
}

func newJobResponse(job jobs.Job, state jobs.StreamState) (jobResponse, error) {
	if !validJobID(job.ID) || !validJobKind(job.Kind) || !validJobStatus(job.Status) || job.Version == 0 || job.Progress < 0 || job.Progress > 100 || !safeJobText(job.Phase) || !safeJobText(job.Outcome) || !safeJobText(job.ErrorCode) {
		return jobResponse{}, errors.New("invalid job")
	}
	created, updated, err := boundedTimestamp(job.CreatedAt, job.UpdatedAt)
	if err != nil || state.ScopeID == "" || state.Epoch == 0 {
		return jobResponse{}, errors.New("invalid job state")
	}
	result := jobResponse{ID: job.ID, Kind: string(job.Kind), Status: string(job.Status), Version: job.Version, Phase: job.Phase, Progress: job.Progress, Outcome: job.Outcome, ErrorCode: job.ErrorCode, CreatedAt: created, UpdatedAt: updated, Stream: events.Cursor{StreamScope: events.ProjectJob, ScopeID: state.ScopeID, Epoch: state.Epoch, Sequence: state.Head}}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > maxJobResponseBytes {
		return jobResponse{}, errors.New("job response exceeds bounds")
	}
	return result, nil
}

func boundedTimestamp(created, updated time.Time) (string, string, error) {
	if created.IsZero() || updated.IsZero() || updated.Before(created) {
		return "", "", errors.New("invalid timestamp")
	}
	first, second := created.UTC().Format(time.RFC3339), updated.UTC().Format(time.RFC3339)
	if _, err := time.Parse(time.RFC3339, first); err != nil {
		return "", "", err
	}
	if _, err := time.Parse(time.RFC3339, second); err != nil {
		return "", "", err
	}
	return first, second, nil
}

func validJobID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func validJobKind(kind jobs.Kind) bool {
	return kind == jobs.KindIndex || kind == jobs.KindExport || kind == jobs.KindResearch || kind == jobs.KindSetup
}
func validJobStatus(status jobs.Status) bool {
	return status == jobs.StatusQueued || status == jobs.StatusRunning || status == jobs.StatusCancelling || status == jobs.StatusSucceeded || status == jobs.StatusFailed || status == jobs.StatusCancelled
}
func safeKey(value string) bool {
	return len(value) > 0 && len(value) <= 128 && utf8.ValidString(value) && strings.IndexFunc(value, func(r rune) bool { return r < 0x21 || r > 0x7e }) < 0
}
func safeJobText(value string) bool {
	return len(value) <= 128 && utf8.ValidString(value) && strings.IndexFunc(value, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') }) < 0
}

func jobErrorOutcome(err error) jobReplayOutcome {
	switch {
	case errors.Is(err, jobs.ErrUnknownJob):
		return jobReplayOutcome{status: http.StatusNotFound, code: "job_not_found", message: "job was not found"}
	case errors.Is(err, jobs.ErrStaleVersion):
		return jobReplayOutcome{status: http.StatusConflict, code: "job_version_conflict", message: "job version conflicts"}
	case errors.Is(err, jobs.ErrInvalidTransition):
		return jobReplayOutcome{status: http.StatusConflict, code: "job_not_cancellable", message: "job cannot be cancelled"}
	case errors.Is(err, jobs.ErrInvalidJob):
		return jobReplayOutcome{status: http.StatusBadRequest, code: "invalid_request", message: "request is invalid"}
	default:
		return jobUnavailableOutcome()
	}
}

func jobUnavailableOutcome() jobReplayOutcome {
	return jobReplayOutcome{status: http.StatusServiceUnavailable, code: "job_unavailable", message: "job is unavailable"}
}

func writeJobError(response http.ResponseWriter, request *http.Request, err error) {
	writeReplayOutcome(response, request, jobErrorOutcome(err))
}

func writeReplayOutcome(response http.ResponseWriter, request *http.Request, outcome jobReplayOutcome) {
	if outcome.job != nil {
		writeJobSuccess(response, request, outcome.status, *outcome.job)
		return
	}
	writeError(response, request, outcome.status, outcome.code, outcome.message, unavailableVersion)
}

func writeJobSuccess(response http.ResponseWriter, request *http.Request, status int, job jobResponse) {
	requestID := responseRequestID(request)
	payload := struct {
		Data jobResponse  `json:"data"`
		Meta responseMeta `json:"meta"`
	}{Data: job, Meta: responseMeta{RequestID: requestID, ResourceVersion: unavailableVersion}}
	encoded, err := json.Marshal(payload)
	if err != nil || len(encoded)+1 > maxJobResponseBytes {
		writeErrorWithID(response, requestID, http.StatusServiceUnavailable, "job_unavailable", "job is unavailable", unavailableVersion)
		return
	}
	writeJSONBytes(response, requestID, status, append(encoded, '\n'))
}
