// Package doctor computes deterministic health diagnostics for a rice from the
// indexed graph: cyclic includes, fan-in/out risk, oversized configs, duplicate
// keybinds, broken commands, orphan scripts, dangling references, hardcoded
// colors, forbidden layer crossings, and git-churn hotspots.
package doctor

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/prowl-agent/prowl-agent/internal/config"
	"github.com/prowl-agent/prowl-agent/internal/store"
)

// Severity grades a finding.
type Severity string

const (
	SevError Severity = "error"
	SevWarn  Severity = "warn"
	SevInfo  Severity = "info"
)

// Finding is one diagnostic result.
type Finding struct {
	Check    string   `json:"check"`
	Severity Severity `json:"severity"`
	File     string   `json:"file,omitempty"`
	Line     int      `json:"line,omitempty"`
	Detail   string   `json:"detail"`
}

// Report is the full diagnostic output with a 0-100 health score.
type Report struct {
	Findings []Finding      `json:"findings"`
	Summary  map[string]int `json:"summary"`
	Score    int            `json:"score"`
}

// Options tunes thresholds.
type Options struct {
	Root            string
	OversizedLines  int
	FanInThreshold  int
	FanOutThreshold int
	ChurnCommits    int
}

func (o Options) withDefaults() Options {
	if o.OversizedLines == 0 {
		o.OversizedLines = 400
	}
	if o.FanInThreshold == 0 {
		o.FanInThreshold = 8
	}
	if o.FanOutThreshold == 0 {
		o.FanOutThreshold = 15
	}
	if o.ChurnCommits == 0 {
		o.ChurnCommits = 200
	}
	return o
}

// Run executes every check and returns a deterministic, sorted report.
func Run(s *store.Store, rules config.Rules, opt Options) (Report, error) {
	opt = opt.withDefaults()
	var f []Finding
	for _, fn := range []func(*store.Store, Options) ([]Finding, error){
		checkCycles, checkFan, checkOversized, checkDuplicateKeybinds,
		checkBrokenCommands, checkOrphansAndDangling, checkHardcodedColors,
	} {
		got, err := fn(s, opt)
		if err != nil {
			return Report{}, err
		}
		f = append(f, got...)
	}
	fb, err := checkForbidden(s, rules)
	if err != nil {
		return Report{}, err
	}
	f = append(f, fb...)
	f = append(f, checkChurn(s, opt)...) // best-effort; needs git

	sort.SliceStable(f, func(i, j int) bool {
		if f[i].Check != f[j].Check {
			return f[i].Check < f[j].Check
		}
		if f[i].File != f[j].File {
			return f[i].File < f[j].File
		}
		return f[i].Line < f[j].Line
	})

	summary := map[string]int{}
	weight := 0
	for _, x := range f {
		summary[x.Check]++
		switch x.Severity {
		case SevError:
			weight += 5
		case SevWarn:
			weight += 2
		}
	}
	score := 100 - weight
	if score < 0 {
		score = 0
	}
	return Report{Findings: f, Summary: summary, Score: score}, nil
}

func checkCycles(s *store.Store, _ Options) ([]Finding, error) {
	edges, err := s.FileDepEdges("includes")
	if err != nil {
		return nil, err
	}
	adj := map[string][]string{}
	for _, e := range edges {
		adj[e.SrcFile] = append(adj[e.SrcFile], e.DstFile)
	}
	const white, gray, black = 0, 1, 2
	color := map[string]int{}
	var stack []string
	var cycles [][]string
	var dfs func(string)
	dfs = func(n string) {
		color[n] = gray
		stack = append(stack, n)
		for _, m := range adj[n] {
			switch color[m] {
			case gray:
				for i, x := range stack {
					if x == m {
						cycles = append(cycles, append([]string{}, stack[i:]...))
						break
					}
				}
			case white:
				dfs(m)
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
	}
	nodes := make([]string, 0, len(adj))
	for n := range adj {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	for _, n := range nodes {
		if color[n] == white {
			dfs(n)
		}
	}
	var out []Finding
	seen := map[string]bool{}
	for _, c := range cycles {
		key := strings.Join(c, ">")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Finding{Check: "cyclic_include", Severity: SevError, File: c[0],
			Detail: "include cycle: " + strings.Join(append(c, c[0]), " -> ")})
	}
	return out, nil
}

func checkFan(s *store.Store, opt Options) ([]Finding, error) {
	var out []Finding
	in, err := s.FanIn(100)
	if err != nil {
		return nil, err
	}
	for _, r := range in {
		if r.In >= opt.FanInThreshold {
			out = append(out, Finding{Check: "fan_in_risk", Severity: SevWarn, File: r.File,
				Detail: fmt.Sprintf("%d files depend on this; edits ripple widely", r.In)})
		}
	}
	o, err := s.FanOut(100)
	if err != nil {
		return nil, err
	}
	for _, r := range o {
		if r.In >= opt.FanOutThreshold {
			out = append(out, Finding{Check: "fan_out_risk", Severity: SevWarn, File: r.File,
				Detail: fmt.Sprintf("references %d other files; a monolithic config", r.In)})
		}
	}
	return out, nil
}

func checkOversized(s *store.Store, opt Options) ([]Finding, error) {
	m, err := s.FileMetrics()
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, x := range m {
		if x.Lines >= opt.OversizedLines {
			out = append(out, Finding{Check: "oversized_file", Severity: SevWarn, File: x.File,
				Detail: fmt.Sprintf("%d lines; consider splitting", x.Lines)})
		}
	}
	return out, nil
}

func checkDuplicateKeybinds(s *store.Store, _ Options) ([]Finding, error) {
	kb, err := s.SymbolsByKind("keybind")
	if err != nil {
		return nil, err
	}
	groups := map[string][]store.SymbolHit{}
	for _, k := range kb {
		groups[canonKey(k.Name)] = append(groups[canonKey(k.Name)], k)
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []Finding
	for _, key := range keys {
		ks := groups[key]
		if len(ks) < 2 {
			continue
		}
		var locs []string
		for _, k := range ks {
			locs = append(locs, fmt.Sprintf("%s:%d", k.File, k.Line))
		}
		out = append(out, Finding{Check: "duplicate_keybind", Severity: SevWarn, File: ks[0].File, Line: ks[0].Line,
			Detail: fmt.Sprintf("key %q bound %d times: %s", key, len(ks), strings.Join(locs, ", "))})
	}
	return out, nil
}

func canonKey(name string) string {
	toks := strings.Fields(strings.NewReplacer(",", " ", "+", " ").Replace(strings.ToLower(name)))
	sort.Strings(toks)
	return strings.Join(toks, "+")
}

func checkBrokenCommands(s *store.Store, _ Options) ([]Finding, error) {
	ex, err := s.UnresolvedEdges("execs", "binds", "autostarts")
	if err != nil {
		return nil, err
	}
	var out []Finding
	seen := map[string]bool{}
	for _, e := range ex {
		cmd := firstToken(e.Raw)
		if cmd == "" || strings.ContainsAny(cmd, "/$") {
			continue // path or variable, not a bare binary
		}
		if seen[cmd] {
			continue
		}
		seen[cmd] = true
		if _, err := exec.LookPath(cmd); err != nil {
			out = append(out, Finding{Check: "broken_command", Severity: SevWarn, File: e.File, Line: e.Line,
				Detail: "command not on PATH: " + cmd})
		}
	}
	return out, nil
}

func firstToken(s string) string {
	f := strings.Fields(strings.TrimSpace(s))
	if len(f) == 0 {
		return ""
	}
	return strings.Trim(f[0], `"'`)
}

func checkOrphansAndDangling(s *store.Store, _ Options) ([]Finding, error) {
	var out []Finding
	orphans, err := s.OrphanFiles("script")
	if err != nil {
		return nil, err
	}
	for _, o := range orphans {
		out = append(out, Finding{Check: "orphan_script", Severity: SevWarn, File: o.RelPath,
			Detail: "script not referenced by any config or keybind"})
	}
	dang, err := s.UnresolvedEdges("includes", "references", "uses_resource")
	if err != nil {
		return nil, err
	}
	for _, e := range dang {
		if e.Kind != "uses_resource" && !pathy(e.Raw) {
			continue
		}
		sev := SevWarn
		if e.Kind == "includes" {
			sev = SevError
		}
		out = append(out, Finding{Check: "dangling_reference", Severity: sev, File: e.File, Line: e.Line,
			Detail: e.Kind + ": " + e.Raw})
	}
	return out, nil
}

func pathy(s string) bool {
	return strings.ContainsAny(s, "/") || strings.HasPrefix(s, "$") || strings.HasPrefix(s, "@")
}

func checkHardcodedColors(s *store.Store, _ Options) ([]Finding, error) {
	res, err := s.AllResources()
	if err != nil {
		return nil, err
	}
	declByValue := map[string]string{}
	for _, r := range res {
		if r.Name != "" && r.Value != "" {
			declByValue[r.Value] = r.Name
		}
	}
	var out []Finding
	for _, r := range res {
		if r.Name == "" && r.Value != "" {
			if name, ok := declByValue[r.Value]; ok {
				out = append(out, Finding{Check: "hardcoded_color", Severity: SevInfo, File: r.File, Line: r.Line,
					Detail: r.Value + " duplicates variable " + name})
			}
		}
	}
	return out, nil
}

func checkForbidden(s *store.Store, rules config.Rules) ([]Finding, error) {
	if len(rules.Forbid) == 0 {
		return nil, nil
	}
	edges, err := s.FileDepEdges()
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, fb := range rules.Forbid {
		for _, e := range edges {
			if globMatch(fb.From, e.SrcFile) && globMatch(fb.To, e.DstFile) {
				name := fb.Name
				if name == "" {
					name = "forbidden crossing"
				}
				out = append(out, Finding{Check: "forbidden_crossing", Severity: SevError, File: e.SrcFile, Line: e.Line,
					Detail: name + ": " + e.SrcFile + " -> " + e.DstFile})
			}
		}
	}
	return out, nil
}

func globMatch(pattern, path string) bool {
	if !strings.ContainsAny(pattern, "*?[") {
		return strings.Contains(path, pattern)
	}
	if ok, _ := filepath.Match(pattern, path); ok {
		return true
	}
	if ok, _ := filepath.Match(pattern, filepath.Base(path)); ok {
		return true
	}
	return false
}

func checkChurn(s *store.Store, opt Options) []Finding {
	if opt.Root == "" {
		return nil
	}
	out, err := exec.Command("git", "-C", opt.Root, "log", "--no-merges",
		"--pretty=format:", "--name-only", "-n", strconv.Itoa(opt.ChurnCommits)).Output()
	if err != nil {
		return nil // not a git repo
	}
	counts := map[string]int{}
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			counts[line]++
		}
	}
	files, err := s.AllFiles()
	if err != nil {
		return nil
	}
	indexed := map[string]bool{}
	for _, f := range files {
		indexed[f.RelPath] = true
	}
	type cf struct {
		file string
		n    int
	}
	var list []cf
	for f, n := range counts {
		if indexed[f] && n >= 5 {
			list = append(list, cf{f, n})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].n != list[j].n {
			return list[i].n > list[j].n
		}
		return list[i].file < list[j].file
	})
	var findings []Finding
	for i, c := range list {
		if i >= 10 {
			break
		}
		findings = append(findings, Finding{Check: "churn_hotspot", Severity: SevInfo, File: c.file,
			Detail: fmt.Sprintf("changed %d times recently; review for stability", c.n)})
	}
	return findings
}
