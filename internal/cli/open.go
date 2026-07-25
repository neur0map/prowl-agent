package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/term"

	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/application"
	"github.com/prowl-agent/prowl-agent/internal/events"
	"github.com/prowl-agent/prowl-agent/internal/jobs"
	"github.com/prowl-agent/prowl-agent/internal/workbench"
	workbenchweb "github.com/prowl-agent/prowl-agent/web"
)

type openDependencies struct {
	listen       func(int) (net.Listener, error)
	bootstrap    func() (*workbench.BootstrapAuthority, string, error)
	writeHandoff func(string) (string, error)
	interactive  func() bool
	assets       fs.FS
	openURL      func(string) error
	serve        func(context.Context, net.Listener, http.Handler) error
	openProject  func(context.Context) (*application.Project, error)
	closeProject func(*application.Project) error
}

func defaultOpenDependencies() openDependencies {
	return openDependencies{
		listen:       workbench.ListenLoopback,
		bootstrap:    workbench.NewBootstrapAuthority,
		writeHandoff: writeBootstrapHandoff,
		interactive:  standardStreamsAreTerminals,
		assets:       workbenchweb.Assets,
		openURL:      openBrowserURL,
		serve:        serveWorkbenchHTTP,
		openProject: func(ctx context.Context) (*application.Project, error) {
			return application.OpenWorkbenchProject(ctx, ".", application.Options{}, application.DefaultStartupLimits())
		},
		closeProject: func(project *application.Project) error { return project.Close() },
	}
}

func newOpenCmd(dependencies openDependencies) *cobra.Command {
	if dependencies.bootstrap == nil {
		dependencies.bootstrap = workbench.NewBootstrapAuthority
	}
	if dependencies.writeHandoff == nil {
		dependencies.writeHandoff = writeBootstrapHandoff
	}
	if dependencies.interactive == nil {
		dependencies.interactive = standardStreamsAreTerminals
	}
	var port int
	var noBrowser bool
	var revealURL bool
	command := &cobra.Command{
		Use:   "open",
		Short: "Open the local Prowl Workbench",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) (runErr error) {
			if noBrowser && revealURL && !dependencies.interactive() {
				return errors.New("--reveal-url requires an interactive terminal")
			}
			var (
				service    *workbench.Service
				jobService *jobs.Service
			)
			if dependencies.openProject != nil {
				project, err := dependencies.openProject(cmd.Context())
				if err != nil {
					return err
				}
				closeProject := dependencies.closeProject
				if closeProject == nil {
					closeProject = func(project *application.Project) error { return project.Close() }
				}
				defer func() { runErr = errors.Join(runErr, closeProject(project)) }()
				service, err = func() (*workbench.Service, error) {
					service, err := workbench.NewService(project)
					if err != nil {
						return nil, err
					}
					jobService, err = newProjectJobsService(project)
					if err != nil {
						return nil, err
					}
					if project.StartupRefreshPending() {
						if _, _, err := jobService.EnqueueOrResumeIndex(cmd.Context()); err != nil {
							return nil, err
						}
					}
					return service, nil
				}()
				if err != nil {
					return err
				}
			}
			authority, nonce, err := dependencies.bootstrap()
			if err != nil {
				return err
			}
			listener, err := dependencies.listen(port)
			if err != nil {
				return err
			}
			listener = &closeOnceListener{Listener: listener}
			defer func() { runErr = errors.Join(runErr, listener.Close()) }()
			origin := "http://" + listener.Addr().String()
			handler, err := func() (http.Handler, error) {
				handler, err := workbench.NewHandler(workbench.HandlerOptions{API: workbench.APIOptions{Bootstrap: authority, AllowedOrigin: origin, Service: service},
					Assets: dependencies.assets})
				if err != nil {
					return nil, err
				}
				if jobService != nil {
					if err := jobService.Start(cmd.Context()); err != nil {
						return nil, err
					}
				}
				return handler, nil
			}()
			if err != nil {
				return err
			}
			launchURL := origin + "/#nonce=" + nonce
			handoff := ""
			if noBrowser && !revealURL {
				handoff, err = dependencies.writeHandoff(launchURL)
				if err != nil {
					return err
				}
				defer os.Remove(handoff)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Prowl Workbench: %s/\n", origin)
			if noBrowser {
				if revealURL {
					fmt.Fprintf(cmd.ErrOrStderr(), "Prowl Workbench bootstrap URL: %s\n", launchURL)
				} else {
					fmt.Fprintf(cmd.ErrOrStderr(), "Prowl Workbench bootstrap handoff: %s\n", handoff)
				}
			} else if err := dependencies.openURL(launchURL); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not open browser: %v\n", err)
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return dependencies.serve(ctx, listener, handler)
		},
	}
	command.Flags().IntVar(&port, "port", 0, "loopback port (0 chooses an available port)")
	command.Flags().BoolVar(&noBrowser, "no-browser", false, "serve without launching a browser")
	command.Flags().BoolVar(&revealURL, "reveal-url", false, "reveal the bootstrap URL on an interactive terminal")
	return command
}

func standardStreamsAreTerminals() bool {
	return term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stderr.Fd())
}

func newProjectJobsService(project *application.Project) (*jobs.Service, error) {
	store, err := jobs.Open(context.Background(), project.Workspace.Root)
	if err != nil {
		return nil, err
	}
	broker, err := events.NewBroker(events.NewProjectJobsOutbox(store), events.BrokerOptions{})
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	service := jobs.NewService(store, broker, func(ctx context.Context, _ jobs.Job, progress func(string, int) error) error {
		if err := progress("refreshing", 1); err != nil {
			return err
		}
		if _, err := project.Refresh(ctx); err != nil {
			return err
		}
		return progress("complete", 100)
	})
	if err := project.AttachJobsService(service); err != nil {
		_ = service.Close()
		return nil, err
	}
	return service, nil
}

func writeBootstrapHandoff(target string) (string, error) {
	file, err := os.CreateTemp("", "prowl-workbench-*.url")
	if err != nil {
		return "", fmt.Errorf("create workbench bootstrap handoff: %w", err)
	}
	path := file.Name()
	cleanup := func(err error) (string, error) {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Chmod(0o600); err != nil {
		return cleanup(fmt.Errorf("secure workbench bootstrap handoff: %w", err))
	}
	if _, err := fmt.Fprintln(file, target); err != nil {
		return cleanup(fmt.Errorf("write workbench bootstrap handoff: %w", err))
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close workbench bootstrap handoff: %w", err)
	}
	return path, nil
}

type closeOnceListener struct {
	net.Listener
	once sync.Once
	err  error
}

func (listener *closeOnceListener) Close() error {
	listener.once.Do(func() { listener.err = listener.Listener.Close() })
	return listener.err
}

func openBrowserURL(target string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", target)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		command = exec.Command("xdg-open", target)
	}
	_, err := startAndReap(command)
	return err
}

func startAndReap(command *exec.Cmd) (<-chan error, error) {
	if err := command.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() {
		done <- command.Wait()
		close(done)
	}()
	return done, nil
}

func serveWorkbenchHTTP(ctx context.Context, listener net.Listener, handler http.Handler) error {
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	serveDone := make(chan struct{})
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdownCtx)
		case <-serveDone:
		}
	}()
	err := server.Serve(listener)
	close(serveDone)
	<-shutdownDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
