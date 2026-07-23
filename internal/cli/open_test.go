package cli

import (
	"bytes"
	"context"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/prowl-agent/prowl-agent/internal/workbench"
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
