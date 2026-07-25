package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"github.com/prowl-agent/prowl-agent/internal/jobs"
	"github.com/prowl-agent/prowl-agent/internal/workbench"
	"github.com/prowl-agent/prowl-agent/internal/workspace"
	workbenchweb "github.com/prowl-agent/prowl-agent/web"
)

func TestOpenCommandServesEmbeddedShellWithoutBrowser(t *testing.T) {
	var launched []string
	var served bool
	var handoffURL string
	dependencies := openDependencies{
		listen: workbench.ListenLoopback,
		assets: workbenchweb.Assets,
		openURL: func(target string) error {
			launched = append(launched, target)
			return nil
		},
		writeHandoff: func(target string) (string, error) {
			handoffURL = target
			return filepath.Join(t.TempDir(), "bootstrap-url"), nil
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
	if parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Query().Encode() != "" || parsed.Fragment != "" {
		t.Fatalf("redacted launch URL %q", address)
	}
	if strings.Contains(output.String(), "nonce=") || handoffURL == "" {
		t.Fatalf("stdout=%q handoff=%q", output.String(), handoffURL)
	}
	handoff, err := url.Parse(handoffURL)
	if err != nil {
		t.Fatal(err)
	}
	params, err := url.ParseQuery(handoff.Fragment)
	if err != nil {
		t.Fatal(err)
	}
	if nonce := params.Get("nonce"); len(nonce) != 43 {
		t.Fatalf("handoff URL=%q does not contain a 256-bit nonce", handoffURL)
	}
}

func TestOpenCommandLaunchesBrowserByDefault(t *testing.T) {
	var launched string
	command := newOpenCmd(openDependencies{
		listen: workbench.ListenLoopback,
		assets: workbenchweb.Assets,
		openURL: func(target string) error {
			launched = target
			return nil
		},
		serve: func(context.Context, net.Listener, http.Handler) error { return nil },
	})
	var output bytes.Buffer
	command.SetOut(&output)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(launched)
	if err != nil {
		t.Fatal(err)
	}
	fragment, err := url.ParseQuery(parsed.Fragment)
	if err != nil {
		t.Fatal(err)
	}
	if nonce := fragment.Get("nonce"); len(nonce) != 43 {
		t.Fatalf("browser URL=%q does not contain a 256-bit nonce", launched)
	}
	if strings.Contains(output.String(), "nonce=") {
		t.Fatalf("automatic launch leaked bootstrap nonce to stdout: %q", output.String())
	}
}

func TestOpenCommandRevealURLRequiresInteractiveTerminal(t *testing.T) {
	command := newOpenCmd(openDependencies{
		interactive: func() bool { return false },
		listen: func(int) (net.Listener, error) {
			t.Fatal("listener opened for a non-interactive reveal")
			return nil, nil
		},
	})
	var output, errors bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&errors)
	command.SetArgs([]string{"--no-browser", "--reveal-url"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("error=%v want interactive terminal requirement", err)
	}
	if strings.Contains(output.String(), "nonce=") || strings.Contains(errors.String(), "nonce=") {
		t.Fatalf("non-interactive reveal leaked a nonce: stdout=%q stderr=%q", output.String(), errors.String())
	}
}

func TestOpenCommandRevealsURLOnlyToInteractiveTerminal(t *testing.T) {
	command := newOpenCmd(openDependencies{
		interactive: func() bool { return true },
		listen:      workbench.ListenLoopback,
		assets:      workbenchweb.Assets,
		openURL:     func(string) error { return nil },
		writeHandoff: func(string) (string, error) {
			t.Fatal("interactive reveal wrote a handoff file")
			return "", nil
		},
		serve: func(context.Context, net.Listener, http.Handler) error { return nil },
	})
	var output, errors bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&errors)
	command.SetArgs([]string{"--no-browser", "--reveal-url"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	revealed := strings.TrimSpace(strings.TrimPrefix(errors.String(), "Prowl Workbench bootstrap URL: "))
	parsed, err := url.Parse(revealed)
	if err != nil {
		t.Fatal(err)
	}
	fragment, err := url.ParseQuery(parsed.Fragment)
	if err != nil {
		t.Fatal(err)
	}
	if nonce := fragment.Get("nonce"); len(nonce) != 43 {
		t.Fatalf("revealed URL=%q does not contain a 256-bit nonce", revealed)
	}
	if strings.Contains(output.String(), "nonce=") {
		t.Fatalf("interactive reveal leaked a nonce to stdout: %q", output.String())
	}
}

func TestWriteBootstrapHandoffUsesPrivatePermissions(t *testing.T) {
	target := "http://127.0.0.1:43117/#nonce=" + strings.Repeat("n", 43)
	path, err := writeBootstrapHandoff(target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("handoff mode=%#o want 0600", info.Mode().Perm())
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != target+"\n" {
		t.Fatalf("handoff body=%q want %q", body, target+"\\n")
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
	var nonce string
	dependencies := openDependencies{
		openProject: func(context.Context) (*application.Project, error) { return project, nil },
		closeProject: func(project *application.Project) error {
			projectCloses.Add(1)
			return project.Close()
		},
		listen: func(int) (net.Listener, error) { return listener, nil },
		bootstrap: func() (*workbench.BootstrapAuthority, string, error) {
			authority, issued, err := workbench.NewBootstrapAuthority()
			nonce = issued
			return authority, issued, err
		},
		assets:  workbenchweb.Assets,
		openURL: func(string) error { return nil },
		serve: func(_ context.Context, owned net.Listener, handler http.Handler) error {
			bearer := bootstrapBearer(t, handler, owned.Addr().String(), nonce)
			request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
			request.Host = owned.Addr().String()
			request.Header.Set("Authorization", "Bearer "+bearer)
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
			return nil, context.DeadlineExceeded
		},
		listen: func(int) (net.Listener, error) {
			listens.Add(1)
			return nil, errors.New("unexpected listen")
		},
		assets:  workbenchweb.Assets,
		openURL: func(string) error { return nil },
		serve:   func(context.Context, net.Listener, http.Handler) error { return nil },
	})
	if err := command.Execute(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v want startup deadline", err)
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
		bootstrap: func() (*workbench.BootstrapAuthority, string, error) {
			return nil, "", errors.New("bootstrap failed")
		},
		listen: func(int) (net.Listener, error) {
			t.Fatal("listen called after bootstrap failure")
			return nil, nil
		},
		assets: workbenchweb.Assets,
	})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "bootstrap failed") {
		t.Fatalf("error=%v want bootstrap failure", err)
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

func bootstrapBearer(t *testing.T, handler http.Handler, address, nonce string) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/bootstrap", bytes.NewBufferString(`{"nonce":"`+nonce+`"}`))
	request.Host = address
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://"+address)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("bootstrap status=%d body=%q", response.Code, response.Body.String())
	}
	var payload struct {
		Bearer string `json:"bearer"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Bearer) != 43 {
		t.Fatalf("bootstrap bearer=%q", payload.Bearer)
	}
	return payload.Bearer
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

func TestProjectRefreshRunnerForwardsNondecreasingIndexProgress(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	project := openReadyWorkbenchProject(t)
	defer project.Close()

	last := -1
	err := projectRefreshRunner(project)(context.Background(), jobs.Job{}, func(_ string, progress int) error {
		if progress < last {
			return errors.New("decreasing durable progress")
		}
		last = progress
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if last != 100 {
		t.Fatalf("final progress=%d, want 100", last)
	}
}

func TestOpenStartupJobWiringOnlyEnqueuesPendingProject(t *testing.T) {
	runOpen := func(project *application.Project) uint64 {
		t.Helper()
		root := project.Workspace.Root
		command := newOpenCmd(openDependencies{
			openProject: func(context.Context) (*application.Project, error) { return project, nil },
			listen:      workbench.ListenLoopback,
			assets:      workbenchweb.Assets,
			openURL:     func(string) error { return nil },
			serve:       func(context.Context, net.Listener, http.Handler) error { return nil },
		})
		command.SetOut(&bytes.Buffer{})
		command.SetArgs([]string{"--no-browser"})
		if err := command.Execute(); err != nil {
			t.Fatal(err)
		}
		store, err := jobs.Open(context.Background(), root)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		state, err := store.State(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return state.Head
	}

	current := openReadyWorkbenchProject(t)
	if head := runOpen(current); head != 0 {
		t.Fatalf("current project job head=%d, want 0", head)
	}

	pendingSeed := openReadyWorkbenchProject(t)
	root := pendingSeed.Workspace.Root
	if err := pendingSeed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "startup-pending.go"), []byte("package main\nfunc StartupPending() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pending, err := application.OpenWorkbenchProject(context.Background(), root, application.Options{}, application.StartupLimits{Timeout: time.Second, CandidatePaths: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if !pending.StartupRefreshPending() {
		t.Fatal("stale project is not pending")
	}
	if head := runOpen(pending); head == 0 {
		t.Fatal("pending project did not enqueue a startup job")
	}
}
