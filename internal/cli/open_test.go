package cli

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prowl-agent/prowl-agent/internal/application"
	"github.com/prowl-agent/prowl-agent/internal/config"
	"github.com/prowl-agent/prowl-agent/internal/workbench"
	"github.com/prowl-agent/prowl-agent/internal/workspace"
	workbenchweb "github.com/prowl-agent/prowl-agent/web"
)

func TestOpenCommandServesEmbeddedShellWithoutBrowser(t *testing.T) {
	var launched []string
	var served bool
	dependencies := openDependencies{
		listen: workbench.ListenLoopback,
		token:  func() (string, error) { return "ephemeral-test-token", nil },
		assets: fs.FS(workbenchweb.Assets),
		openURL: func(target string) error {
			launched = append(launched, target)
			return nil
		},
		serve: func(_ context.Context, listener net.Listener, handler http.Handler) error {
			served = true
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Host = listener.Addr().String()
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Prowl Workbench") {
				t.Fatalf("embedded shell status=%d body=%q", response.Code, response.Body.String())
			}
			return nil
		},
	}
	command := newOpenCmd(dependencies)
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--no-browser", "--port", "0"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !served {
		t.Fatal("workbench server was not invoked")
	}
	if len(launched) != 0 {
		t.Fatalf("browser launched with --no-browser: %v", launched)
	}
	address := strings.TrimSpace(strings.TrimPrefix(output.String(), "Prowl Workbench: "))
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Query().Encode() != "" || parsed.Fragment != "token=ephemeral-test-token" {
		t.Fatalf("unsafe launch URL %q", address)
	}
}

func TestOpenCommandLaunchesBrowserByDefault(t *testing.T) {
	var launched string
	command := newOpenCmd(openDependencies{
		listen: workbench.ListenLoopback,
		token:  func() (string, error) { return "browser-token", nil },
		assets: workbenchweb.Assets,
		openURL: func(target string) error {
			launched = target
			return nil
		},
		serve: func(context.Context, net.Listener, http.Handler) error { return nil },
	})
	command.SetOut(&bytes.Buffer{})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(launched, "/#token=browser-token") {
		t.Fatalf("browser URL=%q", launched)
	}
}

func TestOpenCommandInjectsProjectServiceAndClosesOwnedResourcesOnce(t *testing.T) {
	project := openReadyWorkbenchProject(t)
	defer project.Close()
	base, err := workbench.ListenLoopback(0)
	if err != nil {
		t.Fatal(err)
	}
	listener := &countingListener{Listener: base}
	var projectCloses atomic.Int32
	dependencies := openDependencies{
		openProject: func(context.Context) (*application.Project, error) { return project, nil },
		closeProject: func(project *application.Project) error {
			projectCloses.Add(1)
			return project.Close()
		},
		listen:  func(int) (net.Listener, error) { return listener, nil },
		token:   func() (string, error) { return "real-service-token", nil },
		assets:  workbenchweb.Assets,
		openURL: func(string) error { return nil },
		serve: func(_ context.Context, owned net.Listener, handler http.Handler) error {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
			request.Host = owned.Addr().String()
			request.Header.Set("Authorization", "Bearer real-service-token")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"ok"`) {
				t.Fatalf("health status=%d body=%q", response.Code, response.Body.String())
			}
			if err := owned.Close(); err != nil {
				t.Fatal(err)
			}
			return owned.Close()
		},
	}
	command := newOpenCmd(dependencies)
	command.SetOut(&bytes.Buffer{})
	command.SetArgs([]string{"--no-browser"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if projectCloses.Load() != 1 || listener.closes.Load() != 1 {
		t.Fatalf("project closes=%d listener closes=%d", projectCloses.Load(), listener.closes.Load())
	}
}

func TestOpenCommandStartupFailureDoesNotListen(t *testing.T) {
	var listens atomic.Int32
	command := newOpenCmd(openDependencies{
		openProject: func(context.Context) (*application.Project, error) {
			return nil, &application.StartupRefreshRequiredError{Cause: context.DeadlineExceeded}
		},
		listen: func(int) (net.Listener, error) {
			listens.Add(1)
			return nil, errors.New("unexpected listen")
		},
		token:   func() (string, error) { return "unused", nil },
		assets:  workbenchweb.Assets,
		openURL: func(string) error { return nil },
		serve:   func(context.Context, net.Listener, http.Handler) error { return nil },
	})
	if err := command.Execute(); !errors.Is(err, application.ErrStartupRefreshRequired) {
		t.Fatalf("error=%v want startup refresh required", err)
	}
	if listens.Load() != 0 {
		t.Fatalf("listen called %d times after startup refusal", listens.Load())
	}
}

func TestOpenCommandClosesProjectWhenLaterSetupFails(t *testing.T) {
	project := openReadyWorkbenchProject(t)
	defer project.Close()
	var closes atomic.Int32
	command := newOpenCmd(openDependencies{
		openProject: func(context.Context) (*application.Project, error) { return project, nil },
		closeProject: func(project *application.Project) error {
			closes.Add(1)
			return project.Close()
		},
		token: func() (string, error) { return "", errors.New("token failed") },
		listen: func(int) (net.Listener, error) {
			t.Fatal("listen called after token failure")
			return nil, nil
		},
		assets: workbenchweb.Assets,
	})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "token failed") {
		t.Fatalf("error=%v want token failure", err)
	}
	if closes.Load() != 1 {
		t.Fatalf("project closes=%d want 1", closes.Load())
	}
}

func TestServeWorkbenchHTTPStopsOnCancellation(t *testing.T) {
	listener, err := workbench.ListenLoopback(0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- serveWorkbenchHTTP(ctx, listener, http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusNoContent)
		}))
	}()
	response, err := http.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status=%d", response.StatusCode)
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("workbench server did not stop after cancellation")
	}
}

func TestStartAndReapWaitsForBrowserLauncher(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestBrowserLauncherHelper$")
	command.Env = append(os.Environ(), "PROWL_BROWSER_HELPER=1")
	done, err := startAndReap(command)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("browser launcher was not reaped")
	}
	if command.ProcessState == nil || !command.ProcessState.Exited() {
		t.Fatalf("browser process state=%v", command.ProcessState)
	}
}

func TestBrowserLauncherHelper(t *testing.T) {
	if os.Getenv("PROWL_BROWSER_HELPER") != "1" {
		return
	}
	os.Exit(0)
}

type countingListener struct {
	net.Listener
	closes atomic.Int32
}

func (listener *countingListener) Close() error {
	listener.closes.Add(1)
	return listener.Listener.Close()
}

func openReadyWorkbenchProject(t *testing.T) *application.Project {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := workspace.Create(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Save(state.Path, config.Default()); err != nil {
		t.Fatal(err)
	}
	seed, err := application.OpenProject(context.Background(), root, application.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	project, err := application.OpenWorkbenchProject(context.Background(), root, application.Options{}, application.StartupLimits{Timeout: time.Second, CandidatePaths: 2000})
	if err != nil {
		t.Fatal(err)
	}
	return project
}
