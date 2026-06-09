package cli

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/query"
	"github.com/prowl-agent/prowl-agent/internal/store"
	"github.com/prowl-agent/prowl-agent/internal/workspace"
)

func newStatusCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "status",
		Short: "Show index status for this project, or list all initialized projects",
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
			fmt.Fprintf(out, "Project:   %s\n", ws.Root)
			fmt.Fprintf(out, "Files:     %d\n", st.Counts.Files)
			fmt.Fprintf(out, "Symbols:   %d\n", st.Counts.Symbols)
			fmt.Fprintf(out, "Edges:     %d (resolved %d, dangling %d)\n", st.Counts.Edges, st.Counts.Resolved, st.Counts.Dangling)
			fmt.Fprintf(out, "Resources: %d\n", st.Counts.Resources)
			langs := make([]string, 0, len(st.Counts.Langs))
			for l := range st.Counts.Langs {
				langs = append(langs, l)
			}
			sort.Strings(langs)
			fmt.Fprint(out, "Languages: ")
			for _, l := range langs {
				fmt.Fprintf(out, "%s=%d ", l, st.Counts.Langs[l])
			}
			fmt.Fprintln(out)
			fmt.Fprintf(out, "AI-assist: %v\n", st.AIEnabled)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return c
}