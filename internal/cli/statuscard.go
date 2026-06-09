package cli

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/prowl-agent/prowl-agent/internal/query"
	"github.com/prowl-agent/prowl-agent/internal/selfupdate"
)

var (
	cAccent = lipgloss.Color("#89b4fa")
	cGood   = lipgloss.Color("#a6e3a1")
	cWarn   = lipgloss.Color("#f9e2af")
	cMuted  = lipgloss.Color("#9399b2")
	cFaint  = lipgloss.Color("#585b70")

	stTitle = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	stLabel = lipgloss.NewStyle().Foreground(cMuted)
	stNum   = lipgloss.NewStyle().Bold(true).Foreground(cAccent)
	stBig   = lipgloss.NewStyle().Bold(true).Foreground(cGood)
	stWarn  = lipgloss.NewStyle().Foreground(cWarn)
	stFaint = lipgloss.NewStyle().Foreground(cFaint)
)

// isTTY reports whether f is an interactive terminal (so we render the card; a
// pipe or file gets plain text instead).
func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func bar(frac float64, width int, fill string) string {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	n := int(frac*float64(width) + 0.5)
	if n > width {
		n = width
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(fill)).Render(strings.Repeat("█", n)) +
		stFaint.Render(strings.Repeat("░", width-n))
}

// humanTokens renders a token count compactly (1.2M, 9.4k, 530).
func humanTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 10_000:
		return fmt.Sprintf("%.0fk", float64(n)/1e3)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	default:
		return strconv.FormatInt(n, 10)
	}
}

// comma inserts thousands separators into a non-negative count.
func comma(n int) string {
	s := strconv.Itoa(n)
	if n < 1000 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func relTime(meta string) string {
	n, err := strconv.ParseInt(meta, 10, 64)
	if err != nil || n == 0 {
		return "never"
	}
	d := time.Since(time.Unix(n, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func langBars(langs map[string]int) []string {
	type lc struct {
		l string
		n int
	}
	arr := make([]lc, 0, len(langs))
	maxN := 1
	for l, n := range langs {
		arr = append(arr, lc{l, n})
		if n > maxN {
			maxN = n
		}
	}
	sort.Slice(arr, func(i, j int) bool {
		if arr[i].n != arr[j].n {
			return arr[i].n > arr[j].n
		}
		return arr[i].l < arr[j].l
	})
	if len(arr) > 6 {
		arr = arr[:6]
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		out = append(out, "  "+stLabel.Width(7).Render(e.l)+
			bar(float64(e.n)/float64(maxN), 18, "#89b4fa")+"  "+stFaint.Render(strconv.Itoa(e.n)))
	}
	return out
}

// renderStatusCard builds the bordered, colored status card for a terminal.
func renderStatusCard(version, root, name string, st query.Status, upd selfupdate.Result, perProject []projSaving, combined query.Savings) string {
	c := st.Counts
	var L []string
	L = append(L, stTitle.Render("prowl-agent")+"  "+stFaint.Render(version))
	L = append(L, stNum.Render(name)+"  "+stFaint.Render(root))
	L = append(L, "")

	resolvedFrac := 0.0
	if c.Edges > 0 {
		resolvedFrac = float64(c.Resolved) / float64(c.Edges)
	}
	ai := "off"
	if st.AIEnabled {
		ai = "on"
	}
	L = append(L, stLabel.Render("INDEX"))
	L = append(L, "  "+stLabel.Width(9).Render("files")+stNum.Width(12).Render(comma(c.Files))+
		stLabel.Width(9).Render("symbols")+stNum.Render(comma(c.Symbols)))
	L = append(L, "  "+stLabel.Width(9).Render("edges")+stNum.Width(12).Render(comma(c.Edges))+
		stLabel.Width(9).Render("resolved")+bar(resolvedFrac, 12, "#89b4fa")+fmt.Sprintf(" %3.0f%%", resolvedFrac*100))
	L = append(L, "  "+stLabel.Width(9).Render("updated")+stFaint.Width(12).Render(relTime(st.LastIndex))+
		stLabel.Width(9).Render("ai")+stFaint.Render(ai))
	L = append(L, "")

	L = append(L, stLabel.Render("LANGUAGES"))
	L = append(L, langBars(c.Langs)...)
	L = append(L, "")

	sv := st.Savings
	L = append(L, stLabel.Render("TOKENS SAVED ")+stFaint.Render("(estimated)"))
	L = append(L, "  "+stBig.Render("~"+humanTokens(sv.SavedTokens)))
	denom := sv.SavedTokens + sv.AnswerTokens
	frac := 0.0
	if denom > 0 {
		frac = float64(sv.SavedTokens) / float64(denom)
	}
	L = append(L, "  "+bar(frac, 28, "#a6e3a1"))
	if sv.Queries > 0 {
		avg := sv.AnswerTokens / sv.Queries
		L = append(L, "  "+stFaint.Render(fmt.Sprintf("across %s answers · ~%s tokens each vs reading the files",
			comma(int(sv.Queries)), humanTokens(avg))))
	} else {
		L = append(L, "  "+stFaint.Render("no queries yet; savings grow as your agent uses prowl"))
	}

	if len(perProject) >= 2 {
		L = append(L, "")
		L = append(L, stLabel.Render("ACROSS YOUR PROJECTS"))
		shown := perProject
		if len(shown) > 4 {
			shown = shown[:4]
		}
		for _, p := range shown {
			L = append(L, "  "+stLabel.Width(18).Render(p.Name)+stNum.Render("~"+humanTokens(p.Saved)))
		}
		L = append(L, "  "+stLabel.Width(18).Render("combined")+stBig.Render("~"+humanTokens(combined.SavedTokens)))
	}

	if upd.Available {
		L = append(L, "")
		L = append(L, stWarn.Render("update available")+stFaint.Render("  ·  run ")+stNum.Render("prowl-agent update"))
	}

	L = append(L, "")
	L = append(L, stFaint.Render("measure it yourself: "+tokensDocURL))

	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cFaint).Padding(0, 2)
	return box.Render(lipgloss.JoinVertical(lipgloss.Left, L...))
}
