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
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/workbench"
	workbenchweb "github.com/prowl-agent/prowl-agent/web"
)

type openDependencies struct {
	listen  func(int) (net.Listener, error)
	token   func() (string, error)
	assets  fs.FS
	openURL func(string) error
	serve   func(context.Context, net.Listener, http.Handler) error
}

func defaultOpenDependencies() openDependencies {
	return openDependencies{
		listen:  workbench.ListenLoopback,
		token:   workbench.NewBearerToken,
		assets:  workbenchweb.Assets,
		openURL: openBrowserURL,
		serve:   serveWorkbenchHTTP,
	}
}

func newOpenCmd(dependencies openDependencies) *cobra.Command {
	var port int
	var noBrowser bool
	command := &cobra.Command{
		Use:   "open",
		Short: "Open the local Prowl Workbench",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			listener, err := dependencies.listen(port)
			if err != nil {
				return err
			}
			defer listener.Close()
			token, err := dependencies.token()
			if err != nil {
				return err
			}
			origin := "http://" + listener.Addr().String()
			handler, err := workbench.NewHandler(workbench.HandlerOptions{
				API:    workbench.APIOptions{Token: token, AllowedOrigin: origin},
				Assets: dependencies.assets,
			})
			if err != nil {
				return err
			}
			launchURL := origin + "/#token=" + token
			fmt.Fprintf(cmd.OutOrStdout(), "Prowl Workbench: %s\n", launchURL)
			if !noBrowser {
				if err := dependencies.openURL(launchURL); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not open browser: %v\n", err)
				}
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return dependencies.serve(ctx, listener, handler)
		},
	}
	command.Flags().IntVar(&port, "port", 0, "loopback port (0 chooses an available port)")
	command.Flags().BoolVar(&noBrowser, "no-browser", false, "print the URL without opening a browser")
	return command
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
