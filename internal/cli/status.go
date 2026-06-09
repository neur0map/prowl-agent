package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/query"
	"github.com/prowl-agent/prowl-agent/internal/selfupdate"
	"github.com/prowl-agent/prowl-agent/internal/store"
	"github.com/prowl-agent/prowl-agent/internal/workspace"
)

func newStatusCmd(version string) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "status",
		Short: "Show index status, token savings, and update availability",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			ws, err := workspace.Resolve(".")
			if err == workspace.ErrNotFound {
				entries, _ := workspace.List()
				if asJSON {
					return json.NewEncoder(out).Encode(entries)
				}
				if len(entries) == 0 {
					fmt.Fprintln(out, "No prowl-agent workspaces yet. Run 'prowl-agent init'.")
					return nil
				}
				fmt.Fprintln(out, "Prowl Agent workspaces:")
				for _, e := range entries {
					fmt.Fprintf(out, "  %s  (ai=%v)\n", e.Root, e.AI)
				}
				return nil
			}
			if err != nil {
				return err
			}
			s, err := store.Open(ws.DB)
			if err != nil {
				return err
			}
			defer s.Close()
			st, err := query.New(s).Status()
			if err != nil {
				return err
			}
			if asJSON {
				return json.NewEncoder(out).Encode(st)
			}
			upd := selfupdate.Check(version)
			if f, ok := out.(*os.File); ok && isTTY(f) {
				fmt.Fprintln(out, renderStatusCard(version, ws.Root, filepath.Base(ws.Root), st, upd))
				return nil
			}
			printPlainStatus(out, ws.Root, st, upd)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return c
}

func printPlainStatus(out io.Writer, root string, st query.Status, upd selfupdate.Result) {
	c := st.Counts
	fmt.Fprintf(out, "Project:   %s\n", root)
	fmt.Fprintf(out, "Files:     %d\n", c.Files)
	fmt.Fprintf(out, "Symbols:   %d\n", c.Symbols)
	fmt.Fprintf(out, "Edges:     %d (resolved %d, dangling %d)\n", c.Edges, c.Resolved, c.Dangling)
	fmt.Fprintf(out, "Resources: %d\n", c.Resources)
	langs := make([]string, 0, len(c.Langs))
	for l := range c.Langs {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	fmt.Fprint(out, "Languages: ")
	for _, l := range langs {
		fmt.Fprintf(out, "%s=%d ", l, c.Langs[l])
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "AI-assist: %v\n", st.AIEnabled)
	if st.Savings.Queries > 0 {
		fmt.Fprintf(out, "Saved:     ~%s tokens across %d answers (estimated)\n",
			humanTokens(st.Savings.SavedTokens), st.Savings.Queries)
	}
	if upd.Available {
		fmt.Fprintln(out, "Update:    available. Run 'prowl-agent update'.")
	}
}
