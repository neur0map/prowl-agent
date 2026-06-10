package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/config"
	"github.com/prowl-agent/prowl-agent/internal/index"
	"github.com/prowl-agent/prowl-agent/internal/store"
	"github.com/prowl-agent/prowl-agent/internal/workspace"
)

// newWatchCmd runs a long-lived watcher that keeps the index fresh on every file
// change, independent of an agent or editor being connected. Run it in the
// background or as a user service; serve and lsp also watch while they run.
func newWatchCmd(string) *cobra.Command {
	return &cobra.Command{
		Use:   "watch",
		Short: "Keep the index fresh in the background (re-indexes on file changes)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ws, err := workspace.Resolve(".")
			if err != nil {
				return err
			}
			s, err := store.Open(ws.DB)
			if err != nil {
				return err
			}
			defer s.Close()
			cfg, _ := config.Load(ws.Path)
			out := cmd.OutOrStdout()

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			inf := maybeInferencer(ctx, cfg)
			reindex := reindexer(s, ws.Root, cfg.Ignore, cfg.AI.EmbedModel, inf)
			run := func() {
				msg, err := reindex(ctx)
				if err != nil {
					fmt.Fprintf(out, "[%s] reindex error: %v\n", time.Now().Format("15:04:05"), err)
					return
				}
				fmt.Fprintf(out, "[%s] %s\n", time.Now().Format("15:04:05"), msg)
			}

			ai := "off"
			if inf != nil {
				ai = "on"
			}
			fmt.Fprintf(out, "Watching %s (ai=%s, Ctrl-C to stop)\n", ws.Root, ai)
			run() // initial pass
			if err := index.Watch(ctx, ws.Root, 750*time.Millisecond, run); err != nil && ctx.Err() == nil {
				return err
			}
			fmt.Fprintln(out, "stopped.")
			return nil
		},
	}
}
