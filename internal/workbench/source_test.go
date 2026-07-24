package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/store"
)

func TestSourcePreviewReturnsExactRelativeAnchors(t *testing.T) {
	service, err := NewService(openWorkbenchProject(t, map[string]string{
		"src/main.go": "package main\n\nfunc main() {\n\tserve()\n}\n",
	}))
	if err != nil {
		t.Fatal(err)
	}

	preview, err := service.SourcePreview(context.Background(), SourcePreviewRequest{Path: "src/main.go", LineStart: 3, LineEnd: 4})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Path != "src/main.go" || preview.LineStart != 3 || preview.LineEnd != 4 {
		t.Fatalf("preview=%+v", preview)
	}
	if len(preview.Lines) != 2 || preview.Lines[0].Number != 3 || preview.Lines[0].Text != "func main() {" || preview.Lines[1].Number != 4 || preview.Lines[1].Text != "\tserve()" {
		t.Fatalf("lines=%+v", preview.Lines)
	}
}

func TestSourcePreviewRejectsUnsafeOrUnboundedRequests(t *testing.T) {
	project := openWorkbenchProject(t, map[string]string{
		"safe.go": "package safe\n",
		".env":    "PROWL_SECRET=do-not-expose\n",
	})
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}
	large := []byte("//" + strings.Repeat("x", MaxSourcePreviewBytes+1))
	if err := os.WriteFile(filepath.Join(project.Workspace.Root, "large.go"), large, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := project.Store.UpsertFile(store.File{RelPath: "large.go", Lang: "go", Hash: "large", Size: int64(len(large)), MTime: 1}); err != nil {
		t.Fatal(err)
	}

	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("package outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(project.Workspace.Root, "outside-link.go")
	if err := os.Symlink(outside, linkPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := project.Store.UpsertFile(store.File{RelPath: "outside-link.go", Lang: "go", Hash: "test", Size: 16, MTime: 1}); err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		name string
		in   SourcePreviewRequest
		want error
	}{
		{name: "traversal", in: SourcePreviewRequest{Path: "../outside.go", LineStart: 1, LineEnd: 1}, want: ErrInvalidSourcePreview},
		{name: "absolute", in: SourcePreviewRequest{Path: outside, LineStart: 1, LineEnd: 1}, want: ErrInvalidSourcePreview},
		{name: "unindexed secret", in: SourcePreviewRequest{Path: ".env", LineStart: 1, LineEnd: 1}, want: ErrSourceNotFound},
		{name: "external symlink", in: SourcePreviewRequest{Path: "outside-link.go", LineStart: 1, LineEnd: 1}, want: ErrSourceNotFound},
		{name: "line range", in: SourcePreviewRequest{Path: "safe.go", LineStart: 1, LineEnd: MaxSourcePreviewLines + 1}, want: ErrInvalidSourcePreview},
		{name: "oversized", in: SourcePreviewRequest{Path: "large.go", LineStart: 1, LineEnd: 1}, want: ErrSourceTooLarge},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			_, err := service.SourcePreview(context.Background(), check.in)
			if !errors.Is(err, check.want) {
				t.Fatalf("error=%v want %v", err, check.want)
			}
		})
	}
}

func TestSourcePreviewAPIRouteReturnsBoundedLines(t *testing.T) {
	service, err := NewService(openWorkbenchProject(t, map[string]string{
		"src/main.go": "package main\n\nfunc main() {}\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: service})
	if err != nil {
		t.Fatal(err)
	}

	request := authorizedAPIRequest("/api/v1/source?path=src%2Fmain.go&line_start=3&line_end=3", "source-preview")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	var envelope struct {
		Data SourcePreview `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Path != "src/main.go" || len(envelope.Data.Lines) != 1 || envelope.Data.Lines[0].Text != "func main() {}" {
		t.Fatalf("preview=%+v", envelope.Data)
	}

	request = authorizedAPIRequest("/api/v1/source?path=..%2F.env&line_start=1&line_end=1", "source-invalid")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), ".env") {
		t.Fatalf("unsafe source response: status=%d body=%q", response.Code, response.Body.String())
	}
}
