package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/config"
	"github.com/prowl-agent/prowl-agent/internal/doctor"
	"github.com/prowl-agent/prowl-agent/internal/store"
	"github.com/prowl-agent/prowl-agent/internal/workspace"
)

// cRedHex is the error accent (Catppuccin red); the shared palette in
// statuscard.go covers the rest.
const cRedHex = "#f38ba8"

func newDoctorCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose project health: cycles, keybind conflicts, dead scripts, broken commands",
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
			if f, ok := out.(*os.File); ok && isTTY(f) {
				fmt.Fprintln(out, renderDoctorCard(rep))
				return nil
			}
			fmt.Fprint(out, renderDoctorPlain(rep))
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return c
}

// severityCounts tallies findings by severity.
func severityCounts(fs []doctor.Finding) (errs, warns, infos int) {
	for _, f := range fs {
		switch f.Severity {
		case doctor.SevError:
			errs++
		case doctor.SevWarn:
			warns++
		case doctor.SevInfo:
			infos++
		}
	}
	return
}

func findingLoc(f doctor.Finding) string {
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d", f.File, f.Line)
	}
	return f.File
}

// renderDoctorPlain is the un-styled report for pipes and non-terminals. It
// leads with the score and a severity breakdown, then one block per finding.
func renderDoctorPlain(rep doctor.Report) string {
	var b strings.Builder
	errs, warns, infos := severityCounts(rep.Findings)
	fmt.Fprintf(&b, "Health score: %d/100\n", rep.Score)
	fmt.Fprintf(&b, "Findings: %d (errors %d, warns %d, info %d)\n", len(rep.Findings), errs, warns, infos)
	if len(rep.Findings) == 0 {
		b.WriteString("No issues found.\n")
		return b.String()
	}
	b.WriteString("\n")
	for _, f := range rep.Findings {
		fmt.Fprintf(&b, "[%-5s] %-20s %s\n          %s\n",
			strings.ToUpper(string(f.Severity)), f.Check, findingLoc(f), f.Detail)
	}
	return b.String()
}

// scoreColor bands the health score: green (good), yellow (mid), red (low).
func scoreColor(score int) string {
	switch {
	case score >= 80:
		return "#a6e3a1"
	case score >= 50:
		return "#f9e2af"
	default:
		return cRedHex
	}
}

func sevTag(s doctor.Severity) string {
	switch s {
	case doctor.SevError:
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(cRedHex)).Render("error")
	case doctor.SevWarn:
		return stWarn.Render("warn ")
	default:
		return stFaint.Render("info ")
	}
}

// renderDoctorCard is the bordered, colored report for a terminal: a score bar,
// a severity breakdown, per-check counts, and grouped findings.
func renderDoctorCard(rep doctor.Report) string {
	errs, warns, infos := severityCounts(rep.Findings)
	var L []string
	L = append(L, stTitle.Render("doctor")+"  "+stFaint.Render(fmt.Sprintf("%d findings", len(rep.Findings))))
	L = append(L, "")
	L = append(L, stLabel.Render("HEALTH"))
	L = append(L, "  "+bar(float64(rep.Score)/100, 24, scoreColor(rep.Score))+fmt.Sprintf("  %d/100", rep.Score))
	L = append(L, "  "+lipgloss.NewStyle().Foreground(lipgloss.Color(cRedHex)).Render(fmt.Sprintf("errors %d", errs))+
		"   "+stWarn.Render(fmt.Sprintf("warns %d", warns))+
		"   "+stFaint.Render(fmt.Sprintf("info %d", infos)))

	if len(rep.Summary) > 0 {
		L = append(L, "")
		L = append(L, stLabel.Render("BY CHECK"))
		keys := make([]string, 0, len(rep.Summary))
		for k := range rep.Summary {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			L = append(L, "  "+stLabel.Width(24).Render(k)+stNum.Render(strconv.Itoa(rep.Summary[k])))
		}
	}

	L = append(L, "")
	if len(rep.Findings) == 0 {
		L = append(L, stBig.Render("No issues found."))
	} else {
		L = append(L, stLabel.Render("FINDINGS"))
		for _, f := range rep.Findings {
			L = append(L, "  "+sevTag(f.Severity)+"  "+stNum.Render(f.Check)+"  "+stFaint.Render(findingLoc(f)))
			L = append(L, "      "+f.Detail)
		}
	}

	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cFaint).Padding(0, 2)
	return box.Render(lipgloss.JoinVertical(lipgloss.Left, L...))
}
