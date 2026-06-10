package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/config"
	"github.com/prowl-agent/prowl-agent/internal/store"
	"github.com/prowl-agent/prowl-agent/internal/workspace"
)

// newRestartCmd rebuilds the index from scratch and stops any running servers so
// the agent/editor relaunches the current binary. Use it after upgrading or if a
// project's data looks stale.
func newRestartCmd(string) *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Rebuild the index from scratch and restart running MCP/LSP servers",
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

			// Rebuild first: restart is then the sole writer while any live server
			// just reads via WAL. Stopping servers first would make the agent respawn
			// one that re-indexes concurrently and contends on the database.
			fmt.Fprintf(out, "Rebuilding index for %s ...\n", ws.Root)
			if err := s.SetMeta("index_version", ""); err != nil { // force a full reparse
				return err
			}
			// Rebuild structural data only; the relaunched serve re-embeds lazily.
			// This keeps restart fast and immune to an Ollama or model issue
			// blocking the server stop below.
			msg, err := reindexer(s, ws.Root, cfg.Ignore, "", nil)(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintln(out, "Rebuilt:", msg)

			if n := stopWorkspaceServers(ws.Root); n > 0 {
				fmt.Fprintf(out, "Stopped %d running server(s); your agent/editor relaunches them on next use.\n", n)
			}
			return nil
		},
	}
}

// stopWorkspaceServers SIGTERMs any prowl-agent serve/lsp processes whose working
// directory is inside root, so the launching agent or editor respawns the current
// binary. Linux-only (reads /proc) and best-effort.
func stopWorkspaceServers(root string) int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	self := os.Getpid()
	n := 0
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		raw, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err != nil {
			continue
		}
		args := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
		if len(args) < 2 || !strings.HasSuffix(args[0], "prowl-agent") {
			continue
		}
		if args[1] != "serve" && args[1] != "lsp" {
			continue
		}
		cwd, err := os.Readlink("/proc/" + e.Name() + "/cwd")
		if err != nil || (cwd != root && !strings.HasPrefix(cwd, root+"/")) {
			continue
		}
		if syscall.Kill(pid, syscall.SIGTERM) == nil {
			n++
		}
	}
	return n
}
