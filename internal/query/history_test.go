package query

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// gitRepo builds a throwaway git repository at root and returns a helper that
// runs git commands against it with a deterministic identity, so a commit's
// author and date do not depend on the machine running the test.
func gitRepo(t *testing.T, root string) func(args ...string) {
	t.Helper()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "--quiet")
	return git
}

// "Why does this exist" was answered with raw git during a real session, and it
// was decisive. This is that lookup, keyed to a symbol's line range.
func TestHistoryReturnsCommitsTouchingTheSymbol(t *testing.T) {
	root := t.TempDir()
	git := gitRepo(t, root)
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(root, "a.go"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package p\n\nfunc Target() int {\n\treturn 1\n}\n")
	git("add", "-A")
	git("commit", "--quiet", "-m", "feat: add Target")
	write("package p\n\nfunc Target() int {\n\treturn 2\n}\n")
	git("add", "-A")
	git("commit", "--quiet", "-m", "fix: correct Target")

	q := indexedAt(t, root) // index the temp repo
	commits, err := q.History(context.Background(), root, "Target", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) < 2 {
		t.Fatalf("got %d commits, want at least 2", len(commits))
	}
	if commits[0].Subject != "fix: correct Target" {
		t.Fatalf("newest = %q, want the fix commit first", commits[0].Subject)
	}
	if commits[0].Author == "" || commits[0].Date == "" || commits[0].Commit == "" {
		t.Fatalf("incomplete commit metadata: %+v", commits[0])
	}
	if commits[0].File != "a.go" {
		t.Fatalf("commit file = %q, want a.go", commits[0].File)
	}
}

// A missing symbol resolves to no span, so History is empty rather than an error:
// there is nothing to trace, which is a result, not a failure.
func TestHistoryWithoutGitReturnsEmpty(t *testing.T) {
	q := indexed(t)
	root := filepath.Join("..", "..", "testdata", "sample-config")
	if _, err := q.History(context.Background(), root, "no-such-symbol-anywhere", 10); err != nil {
		t.Fatalf("History on a missing symbol should be empty, not an error: %v", err)
	}
}

// A real symbol whose workspace is not a git repository must degrade to an empty
// result, never surface a raw "fatal: not a git repository" to the agent.
func TestHistoryOutsideGitRepoDegradesToEmpty(t *testing.T) {
	root := t.TempDir() // a plain directory, no git
	if err := os.WriteFile(filepath.Join(root, "a.go"),
		[]byte("package p\n\nfunc Target() int {\n\treturn 1\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	q := indexedAt(t, root)
	commits, err := q.History(context.Background(), root, "Target", 10)
	if err != nil {
		t.Fatalf("History outside a git repo must not error: %v", err)
	}
	if len(commits) != 0 {
		t.Fatalf("history outside a git repo = %d commits, want 0", len(commits))
	}
}

// A committed repository where the symbol's file was never added still degrades:
// git has no line log for an untracked path, and the agent must see empty, not a
// fatal.
func TestHistoryUntrackedFileDegradesToEmpty(t *testing.T) {
	root := t.TempDir()
	git := gitRepo(t, root)
	if err := os.WriteFile(filepath.Join(root, "tracked.go"),
		[]byte("package p\n\nfunc Anchor() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "--quiet", "-m", "init")
	// Write, but never `git add`, the file that defines the queried symbol.
	if err := os.WriteFile(filepath.Join(root, "loose.go"),
		[]byte("package p\n\nfunc Target() int {\n\treturn 1\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	q := indexedAt(t, root)
	commits, err := q.History(context.Background(), root, "Target", 10)
	if err != nil {
		t.Fatalf("History on an untracked file must not error: %v", err)
	}
	if len(commits) != 0 {
		t.Fatalf("history of an untracked file = %d commits, want 0", len(commits))
	}
}

// The limit bounds how many commits a single symbol returns, so a long-lived
// symbol cannot flood an agent's context. It must be honored and overridable.
func TestHistoryHonorsLimit(t *testing.T) {
	root := t.TempDir()
	git := gitRepo(t, root)
	for i := 1; i <= 5; i++ {
		body := "package p\n\nfunc Target() int {\n\treturn " + itoa(i) + "\n}\n"
		if err := os.WriteFile(filepath.Join(root, "a.go"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", "-A")
		git("commit", "--quiet", "-m", "change "+itoa(i))
	}
	q := indexedAt(t, root)
	commits, err := q.History(context.Background(), root, "Target", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("limit 2 returned %d commits, want exactly 2", len(commits))
	}
}

// A cancelled context must interrupt the git line-history walk promptly rather
// than let it run to completion: an MCP client that drops the call, or a killed
// CLI, must not leave git walking a large repository's history. git is shadowed
// with a shim that blocks for ~10s, so a run that ignored ctx would take that
// long; the test cancels shortly after the walk starts and requires History to
// return well inside that window, empty and without error -- the same clean
// degradation the other failure modes use.
func TestHistoryHonorsContextCancellation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"),
		[]byte("package p\n\nfunc Target() int {\n\treturn 1\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	q := indexedAt(t, root) // Span resolves "Target" from the index; no git needed.
	shadowGitWithBlockingShim(t, 10*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	type result struct {
		commits []SymbolCommit
		err     error
	}
	done := make(chan result, 1)
	go func() {
		commits, err := q.History(ctx, root, "Target", 10)
		done <- result{commits, err}
	}()
	// Let the subprocess actually start before cancelling, so the test exercises
	// killing a RUNNING walk, not merely a pre-cancelled fast path.
	time.Sleep(200 * time.Millisecond)
	start := time.Now()
	cancel()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("cancelled History must degrade to empty, not error: %v", r.err)
		}
		if len(r.commits) != 0 {
			t.Fatalf("cancelled History = %d commits, want 0", len(r.commits))
		}
		if elapsed := time.Since(start); elapsed > 3*time.Second {
			t.Fatalf("History took %v to return after cancellation; the context is not reaching the git subprocess", elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("History did not return within 3s of cancellation: the context is not reaching the git subprocess")
	}
}

// shadowGitWithBlockingShim prepends a directory to PATH holding a `git` that
// blocks for d, so a symbolCommits run that does NOT honour its context would
// take that long. `exec sleep` makes the shim a single process the context's
// SIGKILL reaps cleanly, so a cancelled run returns at once rather than waiting
// on an orphaned child holding the output pipe open.
func shadowGitWithBlockingShim(t *testing.T, d time.Duration) {
	t.Helper()
	dir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nexec sleep %d\n", int(d.Seconds()))
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func itoa(i int) string {
	return string(rune('0' + i))
}
