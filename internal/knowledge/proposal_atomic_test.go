package knowledge

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type atomicTestCodec struct{}

func (atomicTestCodec) Parse(path string, data []byte) (*Document, error) {
	return &Document{Path: path, Type: "Note", Title: filepath.Base(path), Body: append([]byte(nil), data...)}, nil
}

func (atomicTestCodec) Marshal(document *Document) ([]byte, error) {
	return append([]byte(nil), document.Body...), nil
}

func decideAtomicProposal(t *testing.T, inbox *ReviewInbox, id string, now time.Time) (*Proposal, error) {
	t.Helper()
	state, err := inbox.Describe(id)
	if err != nil {
		return nil, err
	}
	result, err := inbox.Decide(context.Background(), DecisionRequest{
		ProposalID: id, Action: DecisionAccept, ExpectedVersion: state.Version,
		IdempotencyKey: "atomic-" + state.Version[:16], PrincipalID: "test",
	}, now)
	if err != nil {
		return nil, err
	}
	return &result.Proposal, nil
}

func TestAcceptRestoresAllCanonicalFilesAfterPostWriteFailure(t *testing.T) {
	root := t.TempDir()
	repository := NewRepository(filepath.Join(root, "knowledge"), atomicTestCodec{})
	if err := repository.Init(); err != nil {
		t.Fatal(err)
	}
	original, _ := atomicTestCodec{}.Parse("existing.md", []byte("---\ntype: Note\ntitle: Original\n---\nOriginal body.\n"))
	if err := repository.Write(original); err != nil {
		t.Fatal(err)
	}
	if err := repository.GenerateIndex(); err != nil {
		t.Fatal(err)
	}
	if err := repository.AppendLog("seeded", "existing.md", time.Now()); err != nil {
		t.Fatal(err)
	}
	inbox := NewReviewInbox(filepath.Join(root, "proposals"), repository)
	candidate := filepath.Join(root, "candidate.md")
	if err := os.WriteFile(candidate, []byte("---\ntype: Note\ntitle: Proposed\n---\nProposed body.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	proposal, _, err := inbox.Propose(candidate, "existing.md", "test", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{"existing.md", "index.md", "log.md"}
	before := map[string][]byte{}
	for _, path := range paths {
		before[path], err = repository.ReadBundleFile(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	injected := errors.New("injected post-write failure")
	inbox.afterCanonicalWrite = func() error {
		if err := repository.writeBundleFile("index.md", []byte("corrupt index\n"), 0o644); err != nil {
			return err
		}
		if err := repository.writeBundleFile("log.md", []byte("corrupt log\n"), 0o644); err != nil {
			return err
		}
		return injected
	}
	if _, err := decideAtomicProposal(t, inbox, proposal.ID, time.Now()); !errors.Is(err, injected) {
		t.Fatalf("accept error = %v", err)
	}
	after, readErr := repository.ReadBundleFile("existing.md")
	if readErr != nil || !bytes.Equal(after, before["existing.md"]) {
		t.Fatalf("transaction-owned document not restored: err=%v\nbefore=%q\nafter=%q", readErr, before["existing.md"], after)
	}
	for path, want := range map[string]string{"index.md": "corrupt index\n", "log.md": "corrupt log\n"} {
		after, readErr = repository.ReadBundleFile(path)
		if readErr != nil || string(after) != want {
			t.Fatalf("external %s overwritten: err=%v\nafter=%q", path, readErr, after)
		}
	}
}

func TestAcceptReportsRollbackFailure(t *testing.T) {
	root := t.TempDir()
	repository := NewRepository(filepath.Join(root, "knowledge"), atomicTestCodec{})
	if err := repository.Init(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repository.Root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	original, _ := atomicTestCodec{}.Parse("nested/existing.md", []byte("---\ntype: Note\ntitle: Original\n---\nOriginal.\n"))
	if err := repository.Write(original); err != nil {
		t.Fatal(err)
	}
	inbox := NewReviewInbox(filepath.Join(root, "proposals"), repository)
	candidate := filepath.Join(root, "candidate.md")
	if err := os.WriteFile(candidate, []byte("---\ntype: Note\ntitle: Proposed\n---\nProposed.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	proposal, _, err := inbox.Propose(candidate, "nested/existing.md", "test", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	inbox.afterCanonicalWrite = func() error {
		if err := os.Remove(filepath.Join(repository.Root, "nested", "existing.md")); err != nil {
			return err
		}
		if err := os.Remove(filepath.Join(repository.Root, "nested")); err != nil {
			return err
		}
		if err := os.Symlink(outside, filepath.Join(repository.Root, "nested")); err != nil {
			return err
		}
		return errors.New("injected failure")
	}
	_, err = decideAtomicProposal(t, inbox, proposal.ID, time.Now())
	if err == nil || !strings.Contains(err.Error(), "proposal rollback failed") {
		t.Fatalf("rollback failure not reported: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(outside, "existing.md")); !os.IsNotExist(statErr) {
		t.Fatal("rollback escaped through replacement symlink")
	}
}
