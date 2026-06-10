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

// matchProwlServer reports whether a process (args from /proc cmdline, cwd) is a
// prowl-agent serve/lsp worth stopping. scope=="" matches regardless of cwd;
// otherwise only processes whose cwd is at or under scope match.
func matchProwlServer(args []string, cwd, scope string) bool {
	if len(args) < 2 || !strings.HasSuffix(args[0], "prowl-agent") {
		return false
	}
	if args[1] != "serve" && args[1] != "lsp" {
		return false
	}
	if scope == "" {
		return true
	}
	return cwd == scope || strings.HasPrefix(cwd, scope+"/")
}

// findProwlServers returns the PIDs of prowl-agent serve/lsp processes matching
// scope (see matchProwlServer), skipping this process. Linux-only (reads /proc).
func findProwlServers(scope string) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	self := os.Getpid()
	var pids []int
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
		cwd, _ := os.Readlink("/proc/" + e.Name() + "/cwd")
		if matchProwlServer(args, cwd, scope) {
			pids = append(pids, pid)
		}
	}
	return pids
}

// stopServers SIGTERMs the given PIDs, returning how many were signaled, so the
// launching agent or editor respawns the current binary on next use.
func stopServers(pids []int) int {
	n := 0
	for _, pid := range pids {
		if syscall.Kill(pid, syscall.SIGTERM) == nil {
			n++
		}
	}
	return n
}

// stopWorkspaceServers stops the serve/lsp processes rooted at root.
func stopWorkspaceServers(root string) int {
	return stopServers(findProwlServers(root))
}
