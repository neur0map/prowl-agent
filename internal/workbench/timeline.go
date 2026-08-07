package workbench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/prowl-agent/prowl-agent/internal/knowledge"
	"github.com/prowl-agent/prowl-agent/internal/store"
)

const (
	MaxTimelineSourceEvents  = 1000
	MaxTimelineResponseBytes = 256 << 10
	maxTimelineGitOutput     = 512 << 10
	maxTimelineGitSubject    = 512
)

var ErrInvalidTimelineRequest = errors.New("invalid timeline request")

// TimelinePageRequest carries a bounded opaque continuation request.
type TimelinePageRequest struct {
	Limit  int
	Cursor string
}

// TimelinePage merges evidence-bearing events from local Git, the canonical
// knowledge log, and privacy-safe context trace metadata.
type TimelinePage struct {
	Events []TimelineEvent `json:"events"`
	Next   string          `json:"next,omitempty"`

	resourceVersion string
}

// TimelineEvent reports source provenance directly rather than synthesizing a
// narrative from heterogeneous event streams.
type TimelineEvent struct {
	ID         string                `json:"id"`
	OccurredAt string                `json:"occurred_at"`
	Kind       string                `json:"kind"`
	Provenance string                `json:"provenance"`
	Git        *TimelineGitCommit    `json:"git,omitempty"`
	Knowledge  *TimelineKnowledgeLog `json:"knowledge,omitempty"`
	Context    *TimelineContextTrace `json:"context,omitempty"`

	at     time.Time
	cursor string
	order  string
}

// TimelineGitCommit is a bounded, raw Git commit record.
type TimelineGitCommit struct {
	Commit  string `json:"commit"`
	Subject string `json:"subject"`
}

// TimelineKnowledgeLog is an exact canonical knowledge-log entry.
type TimelineKnowledgeLog struct {
	Action string `json:"action"`
	Path   string `json:"path"`
}

// TimelineContextTrace deliberately omits questions, snippets, source bodies,
// selected IDs, generated text, and timing details. It retains only the
// privacy-safe retrieval metadata useful for provenance and cost inspection.
type TimelineContextTrace struct {
	RunID           string `json:"run_id"`
	QueryHash       string `json:"query_hash"`
	HashVersion     string `json:"hash_version"`
	Mode            string `json:"mode"`
	BudgetTokens    int    `json:"budget_tokens"`
	BudgetBytes     int    `json:"budget_bytes"`
	EstimatedTokens int    `json:"estimated_tokens"`
	EstimatedBytes  int    `json:"estimated_bytes"`
	StrategyVersion string `json:"strategy_version"`
	Status          string `json:"status"`
	ErrorCode       string `json:"error_code,omitempty"`
}

// Timeline returns stable, provenance-labeled events ordered newest-first.
func (service *Service) Timeline(ctx context.Context, request TimelinePageRequest) (TimelinePage, error) {
	limit, after, err := request.pagination()
	if err != nil {
		return TimelinePage{}, err
	}
	release, version, err := service.beginKnowledgeRead(ctx)
	if err != nil {
		return TimelinePage{}, err
	}
	defer release()
	fail := func(err error) (TimelinePage, error) { return TimelinePage{}, versionedProjectionError(version, err) }
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	gitEvents, err := timelineGitEvents(ctx, service.project.Workspace.Root, service.workspaceRoots())
	if err != nil {
		return fail(err)
	}
	knowledgeEvents, err := timelineKnowledgeEvents(service.project.Knowledge, service.workspaceRoots())
	if err != nil {
		return fail(err)
	}
	contextEvents, err := timelineContextEvents(service.project.Store)
	if err != nil {
		return fail(err)
	}
	events := append(append(gitEvents, knowledgeEvents...), contextEvents...)
	sort.Slice(events, func(left, right int) bool {
		if !events[left].at.Equal(events[right].at) {
			return events[left].at.After(events[right].at)
		}
		if events[left].Provenance != events[right].Provenance {
			return events[left].Provenance < events[right].Provenance
		}
		return events[left].ID < events[right].ID
	})
	pageEvents := make([]TimelineEvent, 0, limit)
	hasMore := false
	for _, event := range events {
		if after != "" && !timelineEventFollows(event, after) {
			continue
		}
		if len(pageEvents) == limit {
			hasMore = true
			break
		}
		pageEvents = append(pageEvents, event)
	}
	if pageEvents == nil {
		pageEvents = []TimelineEvent{}
	}
	page := TimelinePage{Events: pageEvents, resourceVersion: version}
	if hasMore {
		page.Next = encodePageCursor("timeline", pageEvents[len(pageEvents)-1].cursor)
	}
	encoded, err := json.Marshal(page)
	if err != nil || len(encoded) > MaxTimelineResponseBytes {
		return fail(errors.New("timeline response exceeds bounds"))
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return page, nil
}

func (request TimelinePageRequest) pagination() (int, string, error) {
	limit, after, err := paginate(request.Limit, request.Cursor, "timeline", validateTimelineCursor)
	if err != nil {
		return 0, "", ErrInvalidTimelineRequest
	}
	return limit, after, nil
}

func parseTimelinePageRequest(values url.Values) (TimelinePageRequest, error) {
	for key, entries := range values {
		if (key != "limit" && key != "cursor") || len(entries) != 1 {
			return TimelinePageRequest{}, ErrInvalidTimelineRequest
		}
	}
	request := TimelinePageRequest{Cursor: values.Get("cursor")}
	if raw := values.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return TimelinePageRequest{}, ErrInvalidTimelineRequest
		}
		request.Limit = limit
	}
	if _, _, err := request.pagination(); err != nil {
		return TimelinePageRequest{}, err
	}
	return request, nil
}

func timelineGitEvents(ctx context.Context, root string, roots []string) ([]TimelineEvent, error) {
	if root == "" {
		return []TimelineEvent{}, nil
	}
	output, err := timelineGitOutput(ctx, root, "log", "--max-count="+strconv.Itoa(MaxTimelineSourceEvents), "--format=%H%x00%cI%x00%s%x00")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return []TimelineEvent{}, nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return []TimelineEvent{}, nil
		}
		return nil, err
	}
	fields := strings.Split(string(output), "\x00")
	if len(fields) > 0 && strings.TrimSpace(fields[len(fields)-1]) == "" {
		fields = fields[:len(fields)-1]
	}
	if len(fields)%3 != 0 {
		return nil, ErrInvalidDerivedData
	}
	events := make([]TimelineEvent, 0, len(fields)/3)
	for index := 0; index < len(fields); index += 3 {
		commit, occurred, subject := strings.TrimLeft(fields[index], "\r\n"), fields[index+1], fields[index+2]
		at, err := time.Parse(time.RFC3339, occurred)
		if err != nil || !validTimelineID(commit) || !validTimelineText(subject, maxTimelineGitSubject, roots...) {
			return nil, ErrInvalidDerivedData
		}
		event := TimelineEvent{ID: "git:" + commit, OccurredAt: at.UTC().Format(time.RFC3339Nano), Kind: "commit", Provenance: "git", Git: &TimelineGitCommit{Commit: commit, Subject: subject}, at: at.UTC()}
		event.order = timelineOrder(event)
		event.cursor = timelineCursor(event.at, event.order)
		events = append(events, event)
	}
	return events, nil
}

func timelineKnowledgeEvents(repository *knowledge.Repository, roots []string) ([]TimelineEvent, error) {
	if repository == nil {
		return []TimelineEvent{}, nil
	}
	data, err := repository.ReadBundleFile("log.md")
	if os.IsNotExist(err) {
		return []TimelineEvent{}, nil
	}
	if err != nil || !utf8.Valid(data) {
		return nil, ErrInvalidDerivedData
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) > MaxTimelineSourceEvents+1 {
		lines = lines[len(lines)-MaxTimelineSourceEvents:]
	}
	events := make([]TimelineEvent, 0)
	for index, line := range lines {
		at, action, target, ok := parseTimelineKnowledgeLine(line)
		if !ok {
			continue
		}
		if !validTimelineAction(action) || validateKnowledgePath(target, roots...) != nil {
			return nil, ErrInvalidDerivedData
		}
		event := TimelineEvent{ID: fmt.Sprintf("knowledge:%06d", index), OccurredAt: at.UTC().Format(time.RFC3339Nano), Kind: "knowledge_log", Provenance: "knowledge_log", Knowledge: &TimelineKnowledgeLog{Action: action, Path: target}, at: at.UTC()}
		event.order = timelineOrder(event)
		event.cursor = timelineCursor(event.at, event.order)
		events = append(events, event)
	}
	return events, nil
}

func parseTimelineKnowledgeLine(line string) (time.Time, string, string, bool) {
	if !strings.HasPrefix(line, "- ") {
		return time.Time{}, "", "", false
	}
	occurred, details, found := strings.Cut(strings.TrimPrefix(line, "- "), " \u2014 ")
	if !found {
		return time.Time{}, "", "", false
	}
	action, quotedPath, found := strings.Cut(details, " `")
	if !found || !strings.HasSuffix(quotedPath, "`") {
		return time.Time{}, "", "", false
	}
	at, err := time.Parse(time.RFC3339, occurred)
	if err != nil {
		return time.Time{}, "", "", false
	}
	return at, action, strings.TrimSuffix(quotedPath, "`"), true
}

func timelineContextEvents(database *store.Store) ([]TimelineEvent, error) {
	if database == nil {
		return []TimelineEvent{}, nil
	}
	runs, err := database.ListContextRuns(MaxTimelineSourceEvents)
	if err != nil {
		return nil, err
	}
	events := make([]TimelineEvent, 0, len(runs))
	for _, run := range runs {
		at, err := time.Parse(time.RFC3339Nano, run.CreatedAt)
		if err != nil || !validTimelineID(run.ID) || !validTimelineText(run.QueryHash, 256) || !validTimelineText(run.HashVersion, 64) || !validTimelineText(run.Mode, 64) || !validTimelineText(run.StrategyVersion, 128) || !validTimelineText(run.Status, 64) || !validTimelineText(run.ErrorCode, 128) {
			return nil, ErrInvalidDerivedData
		}
		trace := TimelineContextTrace{RunID: run.ID, QueryHash: run.QueryHash, HashVersion: run.HashVersion, Mode: run.Mode, BudgetTokens: run.BudgetTokens, BudgetBytes: run.BudgetBytes, EstimatedTokens: run.EstimatedTokens, EstimatedBytes: run.EstimatedBytes, StrategyVersion: run.StrategyVersion, Status: run.Status, ErrorCode: run.ErrorCode}
		event := TimelineEvent{ID: "context:" + run.ID, OccurredAt: at.UTC().Format(time.RFC3339Nano), Kind: "context_trace", Provenance: "context_trace", Context: &trace, at: at.UTC()}
		event.order = timelineOrder(event)
		event.cursor = timelineCursor(event.at, event.order)
		events = append(events, event)
	}
	return events, nil
}

func timelineGitOutput(ctx context.Context, root string, arguments ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", arguments...)
	command.Dir = root
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return nil, err
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, maxTimelineGitOutput+1))
	if len(output) > maxTimelineGitOutput {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, errors.New("Git timeline output exceeds bounds")
	}
	waitErr := command.Wait()
	if readErr != nil {
		return nil, readErr
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if waitErr != nil {
		return nil, fmt.Errorf("git timeline failed: %w", waitErr)
	}
	return output, nil
}

const timelineCursorTimeLayout = "2006-01-02T15:04:05.000000000Z"

func timelineOrder(event TimelineEvent) string {
	return event.Provenance + ":" + event.ID
}

func timelineCursor(at time.Time, order string) string {
	return at.UTC().Format(timelineCursorTimeLayout) + "|" + order
}

func timelineEventFollows(event TimelineEvent, cursor string) bool {
	at, order, err := parseTimelineCursor(cursor)
	if err != nil {
		return false
	}
	if event.at.Before(at) {
		return true
	}
	return event.at.Equal(at) && event.order > order
}

func validateTimelineCursor(value string) error {
	_, _, err := parseTimelineCursor(value)
	return err
}

func parseTimelineCursor(value string) (time.Time, string, error) {
	occurred, order, found := strings.Cut(value, "|")
	if !found || !validTimelineID(order) {
		return time.Time{}, "", ErrInvalidTimelineRequest
	}
	at, err := time.Parse(timelineCursorTimeLayout, occurred)
	if err != nil {
		return time.Time{}, "", ErrInvalidTimelineRequest
	}
	return at.UTC(), order, nil
}

func validTimelineID(value string) bool {
	return value != "" && len(value) <= 512 && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}

func validTimelineAction(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, rune := range value {
		if !(rune >= 'a' && rune <= 'z' || rune == '-' || rune == '_') {
			return false
		}
	}
	return true
}

func validTimelineText(value string, maxBytes int, roots ...string) bool {
	if len(value) > maxBytes || !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return false
	}
	for _, root := range roots {
		if root != "" && strings.Contains(value, root) {
			return false
		}
	}
	return true
}

func serveTimeline(response http.ResponseWriter, request *http.Request, service *Service) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeError(response, request, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed", unavailableVersion)
		return
	}
	if service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "workbench service is unavailable", unavailableVersion)
		return
	}
	pageRequest, err := parseTimelinePageRequest(request.URL.Query())
	if err != nil {
		writeTimelineError(response, request, err)
		return
	}
	page, err := service.Timeline(request.Context(), pageRequest)
	if err != nil {
		writeTimelineError(response, request, err)
		return
	}
	writeSuccess(response, request, page.resourceVersion, page, MaxTimelineResponseBytes)
}

func writeTimelineError(response http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, ErrInvalidTimelineRequest), errors.Is(err, ErrInvalidKnowledgeRequest):
		writeError(response, request, http.StatusBadRequest, "invalid_request", "request is invalid", errorResourceVersion(err))
	default:
		status, code, message := projectionError(err, "timeline")
		writeError(response, request, status, code, message, errorResourceVersion(err))
	}
}
