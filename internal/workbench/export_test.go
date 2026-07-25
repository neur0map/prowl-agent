package workbench

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prowl-agent/prowl-agent/internal/events"
	"github.com/prowl-agent/prowl-agent/internal/jobs"
)

func TestOfflineExportRendersSelfContainedBoundedSnapshot(t *testing.T) {
	project := openWorkbenchProject(t, map[string]string{
		"cmd/server/main.go": "package main\n\nfunc main() {}\n",
		"README.md":          "# Prowl export fixture\n",
	})
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}
	service.exportNow = func() time.Time { return time.Date(2026, 7, 25, 6, 0, 0, 0, time.UTC) }

	export, err := service.OfflineExport(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	html := string(export.HTML)
	for _, want := range []string{
		"<!doctype html>",
		"Prowl offline project snapshot",
		"README.md",
		"generated-at\" content=\"2026-07-25T06:00:00Z",
		"resource-version",
		"default-src 'none'",
		"connect-src 'none'",
		"script-src 'none'",
		"form-action 'none'",
		"</html>",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("export is missing %q:\n%s", want, html)
		}
	}
	for _, forbidden := range []string{
		project.Workspace.Root,
		"Bearer ",
		"nonce",
		"/api/v1/",
		"fetch(",
		"EventSource",
		"WebSocket",
	} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("export leaked or referenced %q:\n%s", forbidden, html)
		}
	}
	if strings.Contains(html, "img-src") {
		t.Fatalf("export permits an unused image source: %s", html)
	}
	if len(export.HTML) == 0 || len(export.HTML) > MaxOfflineExportBytes {
		t.Fatalf("export bytes=%d want 1..%d", len(export.HTML), MaxOfflineExportBytes)
	}
}

func TestOfflineExportIsDeterministicExceptGeneratedAt(t *testing.T) {
	service, err := NewService(openWorkbenchProject(t, map[string]string{"main.go": "package main\n"}))
	if err != nil {
		t.Fatal(err)
	}
	firstTime := time.Date(2026, 7, 25, 6, 0, 0, 0, time.UTC)
	service.exportNow = func() time.Time { return firstTime }
	first, err := service.OfflineExport(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	service.exportNow = func() time.Time { return firstTime.Add(time.Second) }
	second, err := service.OfflineExport(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	firstHTML := strings.ReplaceAll(string(first.HTML), firstTime.Format(time.RFC3339), "<generated-at>")
	secondHTML := strings.ReplaceAll(string(second.HTML), firstTime.Add(time.Second).Format(time.RFC3339), "<generated-at>")
	if firstHTML != secondHTML {
		t.Fatalf("offline export varied beyond generated-at:\nfirst=%s\nsecond=%s", firstHTML, secondHTML)
	}
}

func TestOfflineExportRouteReturnsAuthenticatedStaticHTML(t *testing.T) {
	service, err := NewService(openWorkbenchProject(t, map[string]string{"README.md": "# Export fixture\n"}))
	if err != nil {
		t.Fatal(err)
	}
	service.exportNow = func() time.Time { return time.Date(2026, 7, 25, 6, 0, 0, 0, time.UTC) }
	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: service})
	if err != nil {
		t.Fatal(err)
	}

	request := authorizedAPIRequest("/api/v1/export", "offline-export")
	request.Method = http.MethodPost
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("content type=%q", got)
	}
	if got := response.Header().Get("Content-Disposition"); got != `attachment; filename="prowl-workbench.html"` {
		t.Fatalf("content disposition=%q", got)
	}
	if got := response.Header().Get("Content-Security-Policy"); got != offlineExportCSP {
		t.Fatalf("content security policy=%q", got)
	}
	if got := response.Header().Get("X-Request-ID"); got != "offline-export" {
		t.Fatalf("request ID=%q", got)
	}
	if body := response.Body.String(); !strings.Contains(body, "Prowl offline project snapshot") || strings.Contains(body, "test-secret") {
		t.Fatalf("unsafe export body=%q", body)
	}

	request = authorizedAPIRequest("/api/v1/export", "offline-export-method")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("method status=%d allow=%q", response.Code, response.Header().Get("Allow"))
	}
}

func TestOfflineExportRouteQueuesAndServesDurableArtifact(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	project := openWorkbenchProject(t, map[string]string{"README.md": "# Export fixture\n"})
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}
	service.exportNow = func() time.Time { return time.Date(2026, 7, 25, 6, 0, 0, 0, time.UTC) }
	service.maxSynchronousExportBytes = 1
	store, err := jobs.Open(context.Background(), project.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	broker, err := events.NewBroker(events.NewProjectJobsOutbox(store), events.BrokerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	jobService := jobs.NewService(store, broker, func(context.Context, jobs.Job, func(string, int) error) error { return nil })
	if err := project.AttachJobsService(jobService); err != nil {
		t.Fatal(err)
	}
	if err := service.AttachLiveOperations(jobService, broker); err != nil {
		t.Fatal(err)
	}
	if err := jobService.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: service})
	if err != nil {
		t.Fatal(err)
	}

	request := authorizedAPIRequest("/api/v1/export", "queued-offline-export")
	request.Method = http.MethodPost
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("queue status=%d body=%q", response.Code, response.Body.String())
	}
	var queued struct {
		Data jobResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &queued); err != nil {
		t.Fatal(err)
	}
	if queued.Data.Kind != string(jobs.KindExport) || queued.Data.ID == "" {
		t.Fatalf("queued job=%+v", queued.Data)
	}

	deadline := time.After(2 * time.Second)
	for {
		job, err := jobService.Get(context.Background(), queued.Data.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Terminal() {
			if job.Status != jobs.StatusSucceeded {
				t.Fatalf("export job=%+v", job)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for export artifact")
		case <-time.After(10 * time.Millisecond):
		}
	}

	request = authorizedAPIRequest("/api/v1/export/"+queued.Data.ID, "download-offline-export")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Prowl offline project snapshot") {
		t.Fatalf("download status=%d body=%q", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), project.Workspace.Root) || strings.Contains(response.Body.String(), "test-secret") {
		t.Fatalf("unsafe artifact=%q", response.Body.String())
	}
}

func TestOfflineExportArtifactRouteDoesNotExposeOtherOrQueuedJobs(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
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
	jobService := jobs.NewService(store, broker, func(context.Context, jobs.Job, func(string, int) error) error { return nil })
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

	index, _, err := jobService.EnqueueOrResumeIndex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	request := authorizedAPIRequest("/api/v1/export/"+index.ID, "non-export")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "Prowl offline project snapshot") || strings.Contains(response.Body.String(), project.Workspace.Root) {
		t.Fatalf("non-export status=%d body=%q", response.Code, response.Body.String())
	}

	export, _, err := jobService.EnqueueOrResumeExport(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	request = authorizedAPIRequest("/api/v1/export/"+export.ID, "queued-export")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || strings.Contains(response.Body.String(), "Prowl offline project snapshot") || !strings.Contains(response.Body.String(), `"kind":"export"`) {
		t.Fatalf("queued status=%d body=%q", response.Code, response.Body.String())
	}
}
