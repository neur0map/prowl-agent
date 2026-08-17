package query

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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
	commits, err := q.History(root, "Target", 10)
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
	if _, err := q.History(root, "no-such-symbol-anywhere", 10); err != nil {
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
	commits, err := q.History(root, "Target", 10)
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
	commits, err := q.History(root, "Target", 10)
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
	commits, err := q.History(root, "Target", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("limit 2 returned %d commits, want exactly 2", len(commits))
	}
}

func itoa(i int) string {
	return string(rune('0' + i))
}
