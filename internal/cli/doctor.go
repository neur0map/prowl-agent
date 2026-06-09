package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/config"
	"github.com/prowl-agent/prowl-agent/internal/doctor"
	"github.com/prowl-agent/prowl-agent/internal/store"
	"github.com/prowl-agent/prowl-agent/internal/workspace"
)

func newDoctorCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose rice health: cycles, keybind conflicts, dead scripts, broken commands",
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
			rules, _ := config.LoadRules(ws.Path)
			rep, err := doctor.Run(s, rules, doctor.Options{Root: ws.Root})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				return json.NewEncoder(out).Encode(rep)
			}
			fmt.Fprintf(out, "Health score: %d/100\n", rep.Score)
			if len(rep.Findings) == 0 {
				fmt.Fprintln(out, "No issues found.")
				return nil
			}
			fmt.Fprintln(out)
			for _, f := range rep.Findings {
				loc := f.File
				if f.Line > 0 {
					loc = fmt.Sprintf("%s:%d", f.File, f.Line)
				}
				fmt.Fprintf(out, "[%-5s] %-20s %s\n          %s\n", strings.ToUpper(string(f.Severity)), f.Check, loc, f.Detail)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return c
}
