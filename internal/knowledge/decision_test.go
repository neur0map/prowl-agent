package knowledge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestExpectedVersionRequiresCurrentProposalAndPersistsImmutableAudit(t *testing.T) {
	root := t.TempDir()
	repository := NewRepository(filepath.Join(root, "knowledge"), atomicTestCodec{})
	if err := repository.Init(); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(root, "candidate.md")
	if err := os.WriteFile(candidate, []byte("---\ntype: Note\ntitle: Candidate\n---\nCandidate body.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inbox := NewReviewInbox(filepath.Join(root, "proposals"), repository)
	proposal, _, err := inbox.Propose(candidate, "decisions/candidate.md", "tester", time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	state, err := inbox.Describe(proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := DecisionRequest{
		ProposalID:      proposal.ID,
		Action:          DecisionAccept,
		ExpectedVersion: state.Version,
		IdempotencyKey:  "decision-key-1",
		PrincipalID:     "local-operator",
	}
	result, err := inbox.Decide(context.Background(), request, time.Date(2026, 7, 24, 12, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if result.Idempotent || result.Proposal.Status != "accepted" || result.Audit.PrincipalID != "local-operator" || result.Audit.VersionBefore != state.Version || result.Audit.VersionAfter != result.Version {
		t.Fatalf("decision result=%+v", result)
	}
	if len(result.Audit.Rollback.Paths) != 3 || strings.Contains(strings.Join(result.Audit.Rollback.Paths, ","), root) {
		t.Fatalf("unsafe rollback plan=%+v", result.Audit.Rollback)
	}
	if _, err := repository.ReadBundleFile("decisions/candidate.md"); err != nil {
		t.Fatalf("accepted canonical document missing: %v", err)
	}

	replay, err := inbox.Decide(context.Background(), request, time.Date(2026, 7, 24, 12, 2, 0, 0, time.UTC))
	if err != nil || !replay.Idempotent || !reflect.DeepEqual(replay.Audit, result.Audit) {
		t.Fatalf("idempotent replay=%+v err=%v", replay, err)
	}
	if _, err := inbox.Decide(context.Background(), DecisionRequest{ProposalID: proposal.ID, Action: DecisionReject, ExpectedVersion: state.Version, IdempotencyKey: request.IdempotencyKey, PrincipalID: request.PrincipalID}, time.Now()); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("mismatched idempotency request error=%v", err)
	}
	if _, err := inbox.Decide(context.Background(), DecisionRequest{ProposalID: proposal.ID, Action: DecisionAccept, ExpectedVersion: state.Version, IdempotencyKey: "decision-key-2", PrincipalID: request.PrincipalID}, time.Now()); !errors.Is(err, ErrProposalVersionConflict) {
		t.Fatalf("stale version error=%v", err)
	}
}

func TestRollbackDecisionLeavesProposalProposedWithoutAudit(t *testing.T) {
	root := t.TempDir()
	repository := NewRepository(filepath.Join(root, "knowledge"), atomicTestCodec{})
	if err := repository.Init(); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(root, "candidate.md")
	if err := os.WriteFile(candidate, []byte("---\ntype: Note\ntitle: Rollback\n---\nRollback body.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inbox := NewReviewInbox(filepath.Join(root, "proposals"), repository)
	proposal, _, err := inbox.Propose(candidate, "decisions/rollback.md", "tester", time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	before, err := inbox.Describe(proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("after canonical write")
	inbox.afterCanonicalWrite = func() error { return injected }
	_, err = inbox.Decide(context.Background(), DecisionRequest{
		ProposalID:      proposal.ID,
		Action:          DecisionAccept,
		ExpectedVersion: before.Version,
		IdempotencyKey:  "rollback-decision",
		PrincipalID:     "local-operator",
	}, time.Date(2026, 7, 24, 12, 1, 0, 0, time.UTC))
	if !errors.Is(err, injected) {
		t.Fatalf("decision error=%v", err)
	}
	after, err := inbox.Describe(proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != before.Version || after.Proposal.Status != "proposed" || after.Proposal.Decision != nil {
		t.Fatalf("rolled-back proposal=%+v", after)
	}
	if _, err := repository.ReadBundleFile("decisions/rollback.md"); !os.IsNotExist(err) {
		t.Fatalf("rollback left canonical knowledge: %v", err)
	}
}
