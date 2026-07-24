package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gofrs/flock"
	"github.com/prowl-agent/prowl-agent/internal/application"
	"github.com/prowl-agent/prowl-agent/internal/config"
	"github.com/prowl-agent/prowl-agent/internal/store"
	"github.com/prowl-agent/prowl-agent/internal/workspace"
)

func TestBriefBuildsBoundedProjectSummary(t *testing.T) {
	project := openWorkbenchProject(t, map[string]string{
		"main.go":   "package main\n\nfunc main() { run() }\nfunc run() {}\n",
		"README.md": "# Example project\n",
	})
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}

	brief, err := service.Brief(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if brief.Workspace.Name != filepath.Base(project.Workspace.Root) {
		t.Fatalf("workspace=%+v", brief.Workspace)
	}
	if brief.Overview.Counts.Files != 2 || brief.Overview.Counts.Symbols == 0 {
		t.Fatalf("overview=%+v", brief.Overview)
	}
	if brief.Knowledge.Status != "healthy" || brief.Knowledge.Documents != 0 {
		t.Fatalf("knowledge=%+v", brief.Knowledge)
	}
	if brief.Freshness.Status != "current" || brief.Freshness.LastIndexed == "" {
		t.Fatalf("freshness=%+v", brief.Freshness)
	}
	if len(brief.Capabilities) == 0 || len(brief.Capabilities) > MaxBriefCapabilities {
		t.Fatalf("capabilities=%d", len(brief.Capabilities))
	}
}

func TestBriefBoundsAndDeterministicallyOrdersPaths(t *testing.T) {
	files := map[string]string{}
	for i := 11; i >= 0; i-- {
		name := "docs/architecture-" + string(rune('a'+i)) + ".md"
		files[name] = "# Guide architecture\n"
	}
	project := openWorkbenchProject(t, files)
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.Brief(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Brief(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("brief output changed between identical calls")
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > MaxBriefResponseBytes {
		t.Fatalf("encoded brief=%d bytes want <= %d", len(encoded), MaxBriefResponseBytes)
	}
	if len(first.Overview.Docs) > MaxBriefDocs {
		t.Fatalf("docs=%d want <= %d", len(first.Overview.Docs), MaxBriefDocs)
	}
	for index, path := range first.Overview.Docs {
		if len(path) > MaxBriefStringBytes {
			t.Fatalf("doc path length=%d want <= %d", len(path), MaxBriefStringBytes)
		}
		if index > 0 && first.Overview.Docs[index-1] > path {
			t.Fatalf("docs are not sorted: %v", first.Overview.Docs)
		}
	}
}

func TestBriefRejectsCancellationAndUnavailableData(t *testing.T) {
	t.Run("cancellation", func(t *testing.T) {
		service, err := NewService(openWorkbenchProject(t, nil))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := service.Brief(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("error=%v want context canceled", err)
		}
	})

	t.Run("unpublished store", func(t *testing.T) {
		project := openWorkbenchProject(t, nil)
		service, err := NewService(project)
		if err != nil {
			t.Fatal(err)
		}
		if err := project.Store.SetMeta("index_state", "incomplete"); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Brief(context.Background()); !errors.Is(err, store.ErrGenerationIncomplete) {
			t.Fatalf("error=%v want incomplete generation", err)
		}
	})

	t.Run("malformed knowledge", func(t *testing.T) {
		project := openWorkbenchProject(t, nil)
		service, err := NewService(project)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(project.Workspace.Knowledge, 0o755); err != nil {
			t.Fatal(err)
		}
		malformed := filepath.Join(project.Workspace.Knowledge, "broken.md")
		if err := os.WriteFile(malformed, []byte("not valid OKF"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Brief(context.Background()); err == nil {
			t.Fatal("malformed knowledge was accepted")
		} else if strings.Contains(err.Error(), project.Workspace.Root) {
			t.Fatalf("error leaked workspace root: %v", err)
		}
	})
}

func TestProjectionErrorsCarryKnownResourceVersion(t *testing.T) {
	project := openWorkbenchProject(t, nil)
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}
	version, err := project.Store.GetMeta("cli_sig")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(project.Workspace.Knowledge, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project.Workspace.Knowledge, "broken.md"), []byte("not valid OKF"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, call := range []struct {
		name string
		run  func() error
	}{
		{name: "brief", run: func() error { _, err := service.Brief(context.Background()); return err }},
		{name: "health", run: func() error { _, err := service.Health(context.Background()); return err }},
	} {
		t.Run(call.name, func(t *testing.T) {
			err := call.run()
			var projection *ProjectionError
			if !errors.As(err, &projection) || projection.ResourceVersion != version || !errors.Is(err, ErrInvalidDerivedData) {
				t.Fatalf("error=%v projection=%+v want version %q", err, projection, version)
			}
		})
	}
}

func TestMalformedResourceVersionRemainsUnavailable(t *testing.T) {
	project := openWorkbenchProject(t, nil)
	if err := project.Store.SetMeta("cli_sig", "not-hex"); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Brief(context.Background())
	var projection *ProjectionError
	if !errors.Is(err, ErrInvalidDerivedData) || errors.As(err, &projection) {
		t.Fatalf("pre-resource error=%v projection=%+v", err, projection)
	}
}

func TestIdentifierValidationRejectsControlsInvalidUTF8AndOversize(t *testing.T) {
	for _, value := range []string{"line\nbreak", string([]byte{0xff}), strings.Repeat("x", MaxBriefStringBytes+1)} {
		if err := validateIdentifier(value); !errors.Is(err, ErrInvalidDerivedData) {
			t.Fatalf("identifier %q error=%v", value, err)
		}
	}
	for _, value := range []string{"C++", "test fixture", "objective-c"} {
		if err := validateIdentifier(value); err != nil {
			t.Fatalf("valid identifier %q rejected: %v", value, err)
		}
	}
}

func TestBriefRedactsAbsoluteWorkspacePathsAndHandlesEmptyData(t *testing.T) {
	project := openWorkbenchProject(t, nil)
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}
	brief, err := service.Brief(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if brief.Overview.Counts.Files != 0 || brief.Knowledge.Documents != 0 {
		t.Fatalf("empty brief=%+v", brief)
	}
	encoded := fmt.Sprintf("%+v", brief)
	if strings.Contains(encoded, project.Workspace.Root) || strings.Contains(encoded, project.Workspace.Path) {
		t.Fatalf("brief leaked absolute workspace path: %s", encoded)
	}
}

func TestBriefRejectsMalformedDerivedStoreDataWithoutReflection(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *application.Project, string)
	}{
		{name: "absolute document path", mutate: func(t *testing.T, project *application.Project, hostile string) {
			_, err := project.Store.UpsertFile(store.File{RelPath: hostile + "/guide.md", Lang: "markdown", Role: "guide", Hash: "bad"})
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "traversing document path", mutate: func(t *testing.T, project *application.Project, _ string) {
			_, err := project.Store.UpsertFile(store.File{RelPath: "../private/guide.md", Lang: "markdown", Role: "guide", Hash: "bad"})
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "path-bearing role", mutate: func(t *testing.T, project *application.Project, hostile string) {
			_, err := project.Store.UpsertFile(store.File{RelPath: "safe.go", Lang: "go", Role: hostile, Hash: "bad"})
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "path-bearing language", mutate: func(t *testing.T, project *application.Project, hostile string) {
			_, err := project.Store.UpsertFile(store.File{RelPath: "safe.go", Lang: hostile, Role: "source", Hash: "bad"})
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "path-bearing resource", mutate: func(t *testing.T, project *application.Project, hostile string) {
			fileID, err := project.Store.UpsertFile(store.File{RelPath: "safe.css", Lang: "css", Role: "source", Hash: "bad"})
			if err != nil {
				t.Fatal(err)
			}
			if err := project.Store.ReplaceFileGraph(fileID, nil, []store.Resource{{Kind: "color", Name: "--secret", Value: hostile, Line: 1}}, nil, nil); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "malformed timestamp", mutate: func(t *testing.T, project *application.Project, hostile string) {
			if err := project.Store.SetMeta("last_index", hostile); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			project := openWorkbenchProject(t, nil)
			hostile := project.Workspace.Root + "/customer-secret"
			test.mutate(t, project, hostile)
			service, err := NewService(project)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.Brief(context.Background()); !errors.Is(err, ErrInvalidDerivedData) {
				t.Fatalf("error=%v want invalid derived data", err)
			}

			handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: service})
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, authorizedAPIRequest("/api/v1/brief", "malformed-store"))
			if response.Code != 500 || !strings.Contains(response.Body.String(), `"code":"brief_unavailable"`) {
				t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), hostile) || strings.Contains(response.Body.String(), project.Workspace.Root) {
				t.Fatalf("response reflected malformed data: %q", response.Body.String())
			}
		})
	}
}

func TestBriefIdentifierValidationRejectsPathAliasesWithoutAPIReflection(t *testing.T) {
	for _, field := range []string{"role", "language"} {
		for _, hostile := range []string{"/etc/passwd", "../secret", `C:\secret`, "."} {
			t.Run(field+"/"+strings.ReplaceAll(hostile, "/", "_"), func(t *testing.T) {
				project := openWorkbenchProject(t, nil)
				workspaceAlias := filepath.Dir(project.Workspace.Root) + "/./" + filepath.Base(project.Workspace.Root)
				value := hostile
				if hostile == "." {
					value = workspaceAlias
				}
				file := store.File{RelPath: "safe.go", Lang: "go", Role: "source", Hash: "bad"}
				if field == "role" {
					file.Role = value
				} else {
					file.Lang = value
				}
				if _, err := project.Store.UpsertFile(file); err != nil {
					t.Fatal(err)
				}
				service, err := NewService(project)
				if err != nil {
					t.Fatal(err)
				}
				handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: service})
				if err != nil {
					t.Fatal(err)
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, authorizedAPIRequest("/api/v1/brief", "identifier-error"))
				if response.Code != 500 || strings.Contains(response.Body.String(), value) || strings.Contains(response.Body.String(), project.Workspace.Root) {
					t.Fatalf("status=%d reflected malformed identifier in %q", response.Code, response.Body.String())
				}
			})
		}
	}
}

func TestBriefPinsGenerationAndHonorsInFlightCancellation(t *testing.T) {
	t.Run("generation guard remains held after overview", func(t *testing.T) {
		project := openWorkbenchProject(t, map[string]string{"main.go": "package main\n"})
		service, err := NewService(project)
		if err != nil {
			t.Fatal(err)
		}
		checked := make(chan bool, 1)
		release := make(chan struct{})
		service.afterOverview = func() {
			exclusive := flock.New(filepath.Join(project.Workspace.Path, "index-refresh.lock"))
			locked, lockErr := exclusive.TryLock()
			if locked {
				_ = exclusive.Unlock()
			}
			checked <- lockErr == nil && !locked
			<-release
		}
		done := make(chan error, 1)
		go func() {
			_, err := service.Brief(context.Background())
			done <- err
		}()
		if held := <-checked; !held {
			t.Fatal("exclusive generation lock was acquired during Brief projection")
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})

	t.Run("cancel after overview", func(t *testing.T) {
		service, err := NewService(openWorkbenchProject(t, map[string]string{"main.go": "package main\n"}))
		if err != nil {
			t.Fatal(err)
		}
		started := make(chan struct{})
		release := make(chan struct{})
		service.afterOverview = func() {
			close(started)
			<-release
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := service.Brief(ctx)
			done <- err
		}()
		<-started
		cancel()
		close(release)
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("error=%v want canceled", err)
		}
	})
}

func openWorkbenchProject(t *testing.T, files map[string]string) *application.Project {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	state, err := workspace.Create(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Save(state.Path, config.Default()); err != nil {
		t.Fatal(err)
	}
	project, err := application.OpenProject(context.Background(), root, application.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = project.Close() })
	return project
}
