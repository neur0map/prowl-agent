package query

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// SymbolCommit is one commit that touched a symbol's current line range: a short
// SHA, author, relative date, and subject, plus the file whose range it touched.
// The file is carried so that when a name matches several symbols, each commit
// stays attributable to the symbol it came from.
type SymbolCommit struct {
	Commit  string `json:"commit"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Subject string `json:"subject"`
	File    string `json:"file"`
}

// DefaultHistoryLimit bounds how many commits History returns per matching
// symbol. A long-lived symbol can be touched by hundreds of commits; returning
// them all would flood an agent's context, so the recent ones are the default
// and the caller can widen it.
const DefaultHistoryLimit = 10

// historyGitTimeout is a defensive ceiling on the `git log -L` line-history walk
// symbolCommits runs. `-n limit` bounds the common case to the most recent few
// commits regardless of repository size, but a symbol whose current lines were
// last modified in ancient history forces git to diff every commit on that path
// back to those few -- cost that scales with history depth, not with the limit,
// and runs into seconds-to-minutes on a large repository. A full walk of this
// repo's most-churned source file (touched by ~40 commits) measures ~10ms, i.e.
// ~0.26ms per commit that touches the path; padded to ~0.5ms for larger files
// and cold caches, 15s caps the walk at ~30k such commits -- far deeper than the
// current-lines history of any real symbol, so a legitimate query never trips
// it, while a runaway is bounded rather than left to hang. It is 3x the repo's
// 5s timeout for the far cheaper `git diff`/`ls-files` (mcp.gitChangedFiles),
// reflecting that a line-history walk is a materially heavier operation. The
// deadline is derived from the caller's context, so an earlier caller deadline
// or cancellation still wins.
const historyGitTimeout = 15 * time.Second

// History returns the commits that touched a symbol's CURRENT line range, newest
// first -- the "why is this code the way it is" lookup. It resolves the symbol's
// present span from the index and asks git for the commits over that exact range
// with `git log -L`, which is precise where a whole-file log is not.
//
// Two real caveats, stated so a caller is not misled:
//   - The range is where the symbol lives NOW, not where it lived then. History
//     is computed over the current lines, so a commit that reworked those lines
//     before this symbol occupied them can appear.
//   - `git log -L` cannot combine with `--follow`, so a rename of the file
//     truncates the history at the rename: commits from before the file's
//     current path are not reached.
//
// Ambiguity follows `def`: when the name resolves to several symbols with that
// exact name (the same method in two files, say), every match is traced and each
// commit carries its File so the choice is visible. A name that only matches via
// full-text/substring/fuzzy resolution falls back to the single best match, as
// `def` does.
//
// ctx bounds and cancels the underlying git walk: the caller's cancellation (an
// MCP client dropping the call, say) reaches the subprocess, and a defensive
// historyGitTimeout caps a runaway walk even when the caller has no deadline.
//
// It degrades to an empty result -- never an error -- when the workspace is not a
// git repository, git is missing, the file is untracked or in a shallow clone
// that lacks the history, or ctx is cancelled or the defensive timeout trips:
// the metadata simply is not there to return, so a trip reads as "no history"
// exactly like the other failure modes rather than surfacing a raw git error.
func (q *Querier) History(ctx context.Context, root, name string, limit int) ([]SymbolCommit, error) {
	if limit <= 0 {
		limit = DefaultHistoryLimit
	}
	spans, err := q.Span(root, name)
	if err != nil {
		return nil, err
	}
	if len(spans) == 0 {
		return nil, nil
	}

	// Trace every symbol that carries the exact requested name; if none does, the
	// name resolved by search rather than by identity, so take the best match
	// alone -- the same disambiguation `def` uses.
	targets := spans[:0:0]
	for _, s := range spans {
		if s.Name == name {
			targets = append(targets, s)
		}
	}
	if len(targets) == 0 {
		targets = spans[:1]
	}

	// One deadline for the whole git walk across every traced symbol; honours an
	// earlier caller deadline/cancellation and otherwise caps a runaway at
	// historyGitTimeout. A trip surfaces as an empty result, not an error.
	ctx, cancel := context.WithTimeout(ctx, historyGitTimeout)
	defer cancel()

	out := make([]SymbolCommit, 0, limit)
	seen := make(map[string]bool, len(targets))
	for _, s := range targets {
		if s.File == "" || s.LineStart <= 0 || s.LineEnd < s.LineStart {
			continue
		}
		key := fmt.Sprintf("%s:%d:%d", s.File, s.LineStart, s.LineEnd)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, symbolCommits(ctx, root, s.File, s.LineStart, s.LineEnd, limit)...)
	}
	return out, nil
}

// symbolCommits runs the verified `git log -L` invocation for one line range and
// parses its NUL-separated rows. Any git failure -- not a repository, missing
// binary, untracked path, shallow clone, or a cancelled/timed-out ctx (which
// kills the subprocess) -- yields no commits rather than an error, so the caller
// degrades cleanly.
//
// Verified invocation (metadata only; -s suppresses the diff):
//
//	git -C <root> log --no-merges -n <limit> --format=%h%x00%an%x00%ar%x00%s -L<start>,<end>:<file> -s
func symbolCommits(ctx context.Context, root, file string, start, end, limit int) []SymbolCommit {
	args := []string{
		"-C", root,
		"log", "--no-merges",
		"-n", strconv.Itoa(limit),
		"--format=%h%x00%an%x00%ar%x00%s",
		fmt.Sprintf("-L%d,%d:%s", start, end, file),
		"-s",
	}
	body, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return nil
	}
	var commits []SymbolCommit
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, "\x00", 4)
		if len(f) != 4 {
			continue
		}
		commits = append(commits, SymbolCommit{
			Commit:  f[0],
			Author:  f[1],
			Date:    f[2],
			Subject: f[3],
			File:    file,
		})
	}
	return commits
}
