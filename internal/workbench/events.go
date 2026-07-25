package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/prowl-agent/prowl-agent/internal/events"
)

const (
	maxSSEEventBytes = 4096
	keepaliveEvery   = 15 * time.Second
)

func serveEvents(response http.ResponseWriter, request *http.Request, service *Service) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeError(response, request, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed", unavailableVersion)
		return
	}
	cursor, err := parseEventCursor(request.URL.Query())
	if err != nil {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "request is invalid", unavailableVersion)
		return
	}
	live, err := service.liveOperations()
	if err != nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "service is unavailable", unavailableVersion)
		return
	}
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeError(response, request, http.StatusInternalServerError, "stream_unavailable", "stream is unavailable", unavailableVersion)
		return
	}
	subscription, err := live.broker.Subscribe(request.Context(), cursor)
	if err != nil {
		writeError(response, request, http.StatusServiceUnavailable, "stream_unavailable", "stream is unavailable", unavailableVersion)
		return
	}
	defer subscription.Close()
	setSecurityHeaders(response)
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("X-Accel-Buffering", "no")
	response.WriteHeader(http.StatusOK)
	flusher.Flush()
	for {
		nextCtx, cancel := context.WithTimeout(request.Context(), keepaliveEvery)
		delivery, err := subscription.Next(nextCtx)
		cancel()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				if _, err := response.Write([]byte(": keepalive\n\n")); err != nil {
					return
				}
				flusher.Flush()
				continue
			}
			return
		}
		if err := writeSSEDelivery(response, delivery); err != nil {
			return
		}
		flusher.Flush()
	}
}

func parseEventCursor(values url.Values) (events.Cursor, error) {
	if len(values) != 4 {
		return events.Cursor{}, errors.New("invalid cursor")
	}
	keys := []string{"stream_scope", "scope_id", "epoch", "sequence"}
	for _, key := range keys {
		if len(values[key]) != 1 {
			return events.Cursor{}, errors.New("invalid cursor")
		}
	}
	for key := range values {
		if key != "stream_scope" && key != "scope_id" && key != "epoch" && key != "sequence" {
			return events.Cursor{}, errors.New("invalid cursor")
		}
	}
	if values.Get("stream_scope") != string(events.ProjectJob) || len(values.Get("scope_id")) == 0 || len(values.Get("scope_id")) > 128 {
		return events.Cursor{}, errors.New("invalid cursor")
	}
	epoch, err := parseUint(values.Get("epoch"))
	if err != nil || epoch == 0 {
		return events.Cursor{}, errors.New("invalid cursor")
	}
	sequence, err := parseUint(values.Get("sequence"))
	if err != nil {
		return events.Cursor{}, errors.New("invalid cursor")
	}
	cursor := events.Cursor{StreamScope: events.ProjectJob, ScopeID: values.Get("scope_id"), Epoch: epoch, Sequence: sequence}
	return cursor, cursor.Validate()
}

func parseUint(value string) (uint64, error) {
	if value == "" || len(value) > 20 || (len(value) > 1 && value[0] == '0') {
		return 0, errors.New("invalid number")
	}
	return strconv.ParseUint(value, 10, 64)
}

func writeSSEDelivery(response http.ResponseWriter, delivery events.Delivery) error {
	var name string
	var data any
	switch {
	case delivery.Event != nil && delivery.Reset == nil:
		name = "project-job.changed"
		data = struct {
			Cursor events.Cursor `json:"cursor"`
			Kind   string        `json:"kind"`
		}{Cursor: delivery.Event.Cursor, Kind: "project-job.changed"}
	case delivery.Reset != nil && delivery.Event == nil:
		snapshotURI, err := safeSnapshotURI(delivery.Reset.SnapshotURI)
		if err != nil {
			return err
		}
		name = "reset"
		data = struct {
			Cursor      events.Cursor `json:"cursor"`
			SnapshotURI string        `json:"snapshot_uri"`
		}{Cursor: delivery.Reset.Cursor, SnapshotURI: snapshotURI}
	default:
		return errors.New("invalid stream delivery")
	}
	encoded, err := json.Marshal(data)
	if err != nil || len(encoded) > maxSSEEventBytes {
		return errors.New("stream event exceeds bounds")
	}
	if _, err = response.Write([]byte("event: " + name + "\ndata: " + string(encoded) + "\n\n")); err != nil {
		return err
	}
	return nil
}

func safeSnapshotURI(value string) (string, error) {
	if len(value) == 0 || len(value) > 256 {
		return "", errors.New("invalid snapshot URI")
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Scheme != "snapshot" || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("invalid snapshot URI")
	}
	for _, character := range parsed.Host {
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-') {
			return "", errors.New("invalid snapshot URI")
		}
	}
	return value, nil
}
