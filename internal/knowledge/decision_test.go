package knowledge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
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
	proposal, _, err := inbox.Propose(candidate, "decisions/candidate.md", "tester", "", nil, time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC))
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

func TestDecisionRecoversCanonicalAcceptBeforeProposalCommit(t *testing.T) {
	root := t.TempDir()
	repository := NewRepository(filepath.Join(root, "knowledge"), atomicTestCodec{})
	if err := repository.Init(); err != nil {
		t.Fatal(err)
	}
	candidatePath := filepath.Join(root, "candidate.md")
	if err := os.WriteFile(candidatePath, []byte("---\ntype: Note\ntitle: Recovery\n---\nRecovery body.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inbox := NewReviewInbox(filepath.Join(root, "proposals"), repository)
	createdAt := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	proposal, _, err := inbox.Propose(candidatePath, "decisions/recovery.md", "tester", "", nil, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	before, err := inbox.Describe(proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	decidedAt := createdAt.Add(time.Minute)
	request := DecisionRequest{
		ProposalID: proposal.ID, Action: DecisionAccept, ExpectedVersion: before.Version,
		IdempotencyKey: "recovery-decision", PrincipalID: "local-operator",
	}
	interrupted := errors.New("simulated process interruption")
	inbox.afterCanonicalWrites = func() { panic(interrupted) }
	func() {
		defer func() {
			if recovered := recover(); recovered != interrupted {
				t.Fatalf("interruption=%v", recovered)
			}
		}()
		_, _ = inbox.Decide(context.Background(), request, decidedAt)
	}()

	restarted := NewReviewInbox(inbox.Root, repository)
	result, err := restarted.Decide(context.Background(), request, decidedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Idempotent || result.Proposal.Status != "accepted" {
		t.Fatalf("recovered result=%+v", result)
	}
	if _, err := repository.ReadBundleFile(proposal.TargetPath); err != nil {
		t.Fatalf("accepted canonical document missing after recovery: %v", err)
	}
	if _, err := os.Stat(filepath.Join(inbox.Root, proposal.ID, decisionTransactionFile)); !os.IsNotExist(err) {
		t.Fatalf("recovery transaction remains: %v", err)
	}
}

func TestDecisionPreservesExternallyChangedDerivedFile(t *testing.T) {
	root := t.TempDir()
	repository := NewRepository(filepath.Join(root, "knowledge"), atomicTestCodec{})
	if err := repository.Init(); err != nil {
		t.Fatal(err)
	}
	inbox := NewReviewInbox(filepath.Join(root, "proposals"), repository)
	candidatePath := filepath.Join(root, "candidate.md")
	if err := os.WriteFile(candidatePath, []byte("---\ntype: Note\ntitle: Conflict\n---\nCandidate body.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	proposal, _, err := inbox.Propose(candidatePath, "decisions/conflict.md", "tester", "", nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	state, err := inbox.Describe(proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	const externalIndex = "# Knowledge\n\nmanual index change\n"
	inbox.afterCanonicalWrite = func() error {
		return repository.writeBundleFile("index.md", []byte(externalIndex), 0o644)
	}
	_, err = inbox.Decide(context.Background(), DecisionRequest{
		ProposalID: proposal.ID, Action: DecisionAccept, ExpectedVersion: state.Version,
		IdempotencyKey: "derived-file-conflict", PrincipalID: "local-operator",
	}, time.Now())
	if !errors.Is(err, ErrProposalVersionConflict) {
		t.Fatalf("decision error=%v", err)
	}
	index, err := repository.ReadBundleFile("index.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(index) != externalIndex {
		t.Fatalf("external index overwritten: %q", index)
	}
	if _, err := repository.ReadBundleFile(proposal.TargetPath); !os.IsNotExist(err) {
		t.Fatalf("canonical target remains after conflict: %v", err)
	}
	stored, err := inbox.load(proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "proposed" || stored.Decision != nil {
		t.Fatalf("proposal committed after conflict: %+v", stored)
	}
}

func TestDecisionRecoveryPreservesChangedCandidateAndRollsBack(t *testing.T) {
	root := t.TempDir()
	repository := NewRepository(filepath.Join(root, "knowledge"), atomicTestCodec{})
	if err := repository.Init(); err != nil {
		t.Fatal(err)
	}
	inbox := NewReviewInbox(filepath.Join(root, "proposals"), repository)
	candidatePath := filepath.Join(root, "candidate.md")
	if err := os.WriteFile(candidatePath, []byte("---\ntype: Note\ntitle: Recovery\n---\nOriginal candidate.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	proposal, _, err := inbox.Propose(candidatePath, "decisions/recovery-conflict.md", "tester", "", nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	state, err := inbox.Describe(proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := DecisionRequest{
		ProposalID: proposal.ID, Action: DecisionAccept, ExpectedVersion: state.Version,
		IdempotencyKey: "candidate-conflict", PrincipalID: "local-operator",
	}
	interrupted := errors.New("simulated process interruption")
	inbox.afterCanonicalWrites = func() { panic(interrupted) }
	func() {
		defer func() {
			if recovered := recover(); recovered != interrupted {
				t.Fatalf("interruption=%v", recovered)
			}
		}()
		_, _ = inbox.Decide(context.Background(), request, time.Now())
	}()
	const changedCandidate = "---\ntype: Note\ntitle: Recovery\n---\nExternally changed candidate.\n"
	if err := os.WriteFile(filepath.Join(inbox.Root, filepath.FromSlash(proposal.CandidatePath)), []byte(changedCandidate), 0o644); err != nil {
		t.Fatal(err)
	}
	restarted := NewReviewInbox(inbox.Root, repository)
	_, err = restarted.Decide(context.Background(), request, time.Now())
	if !errors.Is(err, ErrProposalVersionConflict) {
		t.Fatalf("recovery error=%v", err)
	}
	candidate, err := os.ReadFile(filepath.Join(inbox.Root, filepath.FromSlash(proposal.CandidatePath)))
	if err != nil {
		t.Fatal(err)
	}
	if string(candidate) != changedCandidate {
		t.Fatalf("candidate overwritten during recovery: %q", candidate)
	}
	if _, err := repository.ReadBundleFile(proposal.TargetPath); !os.IsNotExist(err) {
		t.Fatalf("canonical target remains after candidate conflict: %v", err)
	}
	stored, err := restarted.load(proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "proposed" || stored.Decision != nil {
		t.Fatalf("proposal committed after candidate conflict: %+v", stored)
	}
	if _, err := os.Stat(filepath.Join(inbox.Root, proposal.ID, decisionTransactionFile)); !os.IsNotExist(err) {
		t.Fatalf("recovery transaction remains: %v", err)
	}
}

func TestDecideSerializesConflictingAcceptAndReject(t *testing.T) {
	root := t.TempDir()
	repository := NewRepository(filepath.Join(root, "knowledge"), atomicTestCodec{})
	if err := repository.Init(); err != nil {
		t.Fatal(err)
	}
	candidatePath := filepath.Join(root, "candidate.md")
	if err := os.WriteFile(candidatePath, []byte("---\ntype: Note\ntitle: Serialized\n---\nSerialized body.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inbox := NewReviewInbox(filepath.Join(root, "proposals"), repository)
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	proposal, _, err := inbox.Propose(candidatePath, "decisions/serialized.md", "tester", "", nil, now)
	if err != nil {
		t.Fatal(err)
	}
	state, err := inbox.Describe(proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan DecisionResult, 2)
	decisionErrors := make(chan error, 2)
	var wait sync.WaitGroup
	for _, action := range []DecisionAction{DecisionAccept, DecisionReject} {
		wait.Add(1)
		go func(action DecisionAction) {
			defer wait.Done()
			<-start
			result, err := inbox.Decide(context.Background(), DecisionRequest{
				ProposalID: proposal.ID, Action: action, ExpectedVersion: state.Version,
				IdempotencyKey: "concurrent-" + string(action), PrincipalID: "local-operator",
			}, now.Add(time.Minute))
			results <- result
			decisionErrors <- err
		}(action)
	}
	close(start)
	wait.Wait()
	close(results)
	close(decisionErrors)
	var accepted, rejected, conflicts int
	for err := range decisionErrors {
		if err == nil {
			continue
		}
		if errors.Is(err, ErrProposalVersionConflict) {
			conflicts++
			continue
		}
		t.Fatalf("concurrent decision error=%v", err)
	}
	for result := range results {
		switch result.Proposal.Status {
		case "accepted":
			accepted++
		case "rejected":
			rejected++
		}
	}
	if conflicts != 1 || accepted+rejected != 1 {
		t.Fatalf("conflicts=%d accepted=%d rejected=%d", conflicts, accepted, rejected)
	}
	final, err := inbox.Describe(proposal.ID)
	if err != nil || final.Proposal.Decision == nil {
		t.Fatalf("final proposal=%+v err=%v", final, err)
	}
	if final.Proposal.Status == "accepted" {
		if _, err := repository.ReadBundleFile(proposal.TargetPath); err != nil {
			t.Fatalf("accepted canonical document missing: %v", err)
		}
	} else if _, err := repository.ReadBundleFile(proposal.TargetPath); !os.IsNotExist(err) {
		t.Fatalf("rejected proposal changed canonical knowledge: %v", err)
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
	proposal, _, err := inbox.Propose(candidate, "decisions/rollback.md", "tester", "", nil, time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC))
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
