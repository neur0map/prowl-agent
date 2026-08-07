package workbench

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contextpacket "github.com/prowl-agent/prowl-agent/internal/context"
)

func TestTimelineMergesProvenanceWithoutLeakingContextQuestion(t *testing.T) {
	project := openWorkbenchProject(t, map[string]string{"main.go": "package main\nfunc main() {}\n"})
	if err := project.Knowledge.Init(); err != nil {
		t.Fatal(err)
	}
	if err := project.Knowledge.AppendLog("accepted", "decisions/timeline.md", time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := project.Context.Search(contextpacket.Request{Question: "timeline-secret-do-not-expose", Mode: contextpacket.ModeCompact, BudgetTokens: 100}); err != nil {
		t.Fatal(err)
	}
	commitTimelineFixture(t, project.Workspace.Root)
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}

	page, err := service.Timeline(context.Background(), TimelinePageRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Next == "" {
		t.Fatalf("first timeline page=%+v", page)
	}
	second, err := service.Timeline(context.Background(), TimelinePageRequest{Limit: 1, Cursor: page.Next})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Events) != 1 || second.Events[0].ID == page.Events[0].ID {
		t.Fatalf("second timeline page=%+v", second)
	}
	all, err := service.Timeline(context.Background(), TimelinePageRequest{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, event := range all.Events {
		seen[event.Provenance] = true
	}
	if !seen["git"] || !seen["knowledge_log"] || !seen["context_trace"] {
		t.Fatalf("missing timeline provenance: %+v", all.Events)
	}
	encoded, err := json.Marshal(all)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("timeline-secret-do-not-expose")) || bytes.Contains(encoded, []byte(project.Workspace.Root)) {
		t.Fatalf("timeline leaked private data: %s", encoded)
	}

	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: service})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authorizedTimelineRequest("/api/v1/timeline?limit=100"))
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "timeline-secret-do-not-expose") || strings.Contains(response.Body.String(), project.Workspace.Root) {
		t.Fatalf("timeline status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestTimelinePaginationRetainsEqualTimestampEvents(t *testing.T) {
	project := openWorkbenchProject(t, nil)
	if err := project.Knowledge.Init(); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	for _, target := range []string{"decisions/alpha.md", "decisions/bravo.md"} {
		if err := project.Knowledge.AppendLog("accepted", target, at); err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.Timeline(context.Background(), TimelinePageRequest{Limit: 1})
	if err != nil || len(first.Events) != 1 || first.Next == "" {
		t.Fatalf("first equal-timestamp page=%+v err=%v", first, err)
	}
	second, err := service.Timeline(context.Background(), TimelinePageRequest{Limit: 1, Cursor: first.Next})
	if err != nil || len(second.Events) != 1 || second.Events[0].ID == first.Events[0].ID {
		t.Fatalf("second equal-timestamp page=%+v err=%v", second, err)
	}
}

func TestTimelineReturnsBoundedEmptyStateAndRejectsInvalidPage(t *testing.T) {
	project := openWorkbenchProject(t, nil)
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}
	page, err := service.Timeline(context.Background(), TimelinePageRequest{Limit: 100})
	if err != nil || len(page.Events) != 0 || page.Next != "" {
		t.Fatalf("empty timeline=%+v err=%v", page, err)
	}
	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: service})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path string
		want int
	}{
		{path: "/api/v1/timeline?limit=101", want: http.StatusBadRequest},
		{path: "/api/v1/timeline?cursor=not-a-valid-cursor", want: http.StatusBadRequest},
		{path: "/api/v1/timeline", want: http.StatusOK},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, authorizedTimelineRequest(test.path))
		if response.Code != test.want || strings.Contains(response.Body.String(), project.Workspace.Root) {
			t.Fatalf("path=%s status=%d want=%d body=%q", test.path, response.Code, test.want, response.Body.String())
		}
	}
}

func commitTimelineFixture(t *testing.T, root string) {
	t.Helper()
	// A second commit is essential: git's --format joins records with a newline
	// that the parser must strip, a defect invisible with a single commit.
	if err := os.WriteFile(filepath.Join(root, "second.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, arguments := range [][]string{
		{"init"},
		{"config", "user.email", "timeline@example.test"},
		{"config", "user.name", "Timeline Test"},
		{"add", "main.go"},
		{"commit", "-m", "Add timeline fixture"},
		{"add", "second.go"},
		{"commit", "-m", "Add second timeline commit"},
	} {
		command := exec.Command("git", arguments...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", arguments, err, output)
		}
	}
}

func authorizedTimelineRequest(path string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Host = "127.0.0.1:43117"
	request.Header.Set("Authorization", "Bearer test-secret")
	return request
}
