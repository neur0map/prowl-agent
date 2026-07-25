package workbench

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/events"
	"github.com/prowl-agent/prowl-agent/internal/jobs"
)

func TestJobRoutesRefreshGetCancelAndIdempotency(t *testing.T) {
	project := openWorkbenchProject(t, nil)
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}
	store, err := jobs.Open(context.Background(), project.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	broker, err := events.NewBroker(events.NewProjectJobsOutbox(store), events.BrokerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	jobService := jobs.NewService(store, broker, nil)
	if err := project.AttachJobsService(jobService); err != nil {
		t.Fatal(err)
	}
	if err := service.AttachLiveOperations(jobService, broker); err != nil {
		t.Fatal(err)
	}
	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: service})
	if err != nil {
		t.Fatal(err)
	}

	refresh := authorizedAPIRequest("/api/v1/index/refresh", "refresh")
	refresh.Method = http.MethodPost
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, refresh)
	if response.Code != http.StatusAccepted {
		t.Fatalf("refresh status=%d body=%q", response.Code, response.Body.String())
	}
	bodyRefresh := authorizedAPIRequest("/api/v1/index/refresh", "refresh-body")
	bodyRefresh.Method = http.MethodPost
	bodyRefresh.Body = io.NopCloser(strings.NewReader(`{}`))
	bodyRefresh.ContentLength = -1
	bodyResponse := httptest.NewRecorder()
	handler.ServeHTTP(bodyResponse, bodyRefresh)
	if bodyResponse.Code != http.StatusBadRequest {
		t.Fatalf("refresh body status=%d body=%q", bodyResponse.Code, bodyResponse.Body.String())
	}
	var refreshed struct {
		Data jobResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &refreshed); err != nil {
		t.Fatal(err)
	}
	if refreshed.Data.ID == "" || refreshed.Data.Kind != string(jobs.KindIndex) || refreshed.Data.Status != string(jobs.StatusQueued) || refreshed.Data.Version != 1 {
		t.Fatalf("refresh=%+v", refreshed.Data)
	}
	if strings.Contains(response.Body.String(), project.Workspace.Root) {
		t.Fatalf("response leaked workspace path: %q", response.Body.String())
	}

	get := authorizedAPIRequest("/api/v1/jobs/"+refreshed.Data.ID, "get")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, get)
	if response.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%q", response.Code, response.Body.String())
	}

	missingGet := authorizedAPIRequest("/api/v1/jobs/"+strings.Repeat("e", 32), "missing-get")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, missingGet)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"job_not_found"`) {
		t.Fatalf("missing get status=%d body=%q", response.Code, response.Body.String())
	}

	cancelBody := []byte(`{"expected_version":1,"idempotency_key":"request-1"}`)
	cancel := authorizedAPIRequest("/api/v1/jobs/"+refreshed.Data.ID+"/cancel", "cancel")
	cancel.Method, cancel.Body = http.MethodPost, io.NopCloser(bytes.NewReader(cancelBody))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, cancel)
	if response.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%q", response.Code, response.Body.String())
	}
	first := response.Body.String()
	cancel = authorizedAPIRequest("/api/v1/jobs/"+refreshed.Data.ID+"/cancel", "cancel-replay")
	cancel.Method, cancel.Body = http.MethodPost, io.NopCloser(bytes.NewReader(cancelBody))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, cancel)
	if response.Code != http.StatusOK || response.Body.String() == "" {
		t.Fatalf("replay status=%d body=%q", response.Code, response.Body.String())
	}
	if first == "" {
		t.Fatal("empty first response")
	}
	conflict := authorizedAPIRequest("/api/v1/jobs/"+refreshed.Data.ID+"/cancel", "cancel-conflict")
	conflict.Method, conflict.Body = http.MethodPost, io.NopCloser(strings.NewReader(`{"expected_version":2,"idempotency_key":"request-1"}`))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, conflict)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"idempotency_conflict"`) {
		t.Fatalf("conflict status=%d body=%q", response.Code, response.Body.String())
	}

	missingID := strings.Repeat("f", 32)
	missingBody := []byte(`{"expected_version":1,"idempotency_key":"missing-key"}`)
	missing := authorizedAPIRequest("/api/v1/jobs/"+missingID+"/cancel", "missing")
	missing.Method, missing.Body = http.MethodPost, io.NopCloser(bytes.NewReader(missingBody))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, missing)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"job_not_found"`) {
		t.Fatalf("missing status=%d body=%q", response.Code, response.Body.String())
	}
	missing = authorizedAPIRequest("/api/v1/jobs/"+missingID+"/cancel", "missing-replay")
	missing.Method, missing.Body = http.MethodPost, io.NopCloser(bytes.NewReader(missingBody))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, missing)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"job_not_found"`) {
		t.Fatalf("missing replay status=%d body=%q", response.Code, response.Body.String())
	}
	missing = authorizedAPIRequest("/api/v1/jobs/"+refreshed.Data.ID+"/cancel", "missing-conflict")
	missing.Method, missing.Body = http.MethodPost, io.NopCloser(strings.NewReader(`{"expected_version":1,"idempotency_key":"missing-key"}`))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, missing)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"idempotency_conflict"`) {
		t.Fatalf("missing conflict status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestJobRoutesRejectMalformedRequestsAndCredentialQueries(t *testing.T) {
	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117"})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/api/v1/jobs/not-an-id",
		"/api/v1/jobs/0123456789abcdef0123456789abcdef/cancel?access_TOKEN=secret",
	} {
		request := authorizedAPIRequest(path, "bad")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("path=%q status=%d body=%q", path, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "secret") {
			t.Fatalf("credential leaked: %q", response.Body.String())
		}
	}
}

func TestJobCancelUnavailableIsPrivacySafe(t *testing.T) {
	tests := []struct {
		name    string
		service *Service
	}{
		{name: "nil service"},
		{name: "unattached service", service: func() *Service {
			service, err := NewService(openWorkbenchProject(t, nil))
			if err != nil {
				t.Fatal(err)
			}
			return service
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: test.service})
			if err != nil {
				t.Fatal(err)
			}
			request := authorizedAPIRequest("/api/v1/jobs/"+strings.Repeat("a", 32)+"/cancel", "cancel-unavailable")
			request.Method = http.MethodPost
			request.Body = io.NopCloser(strings.NewReader(`{"expected_version":1,"idempotency_key":"unavailable-key"}`))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"service_unavailable"`) {
				t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
			}
		})
	}
}
