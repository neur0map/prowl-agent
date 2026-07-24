package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gofrs/flock"
)

const (
	decisionAuditSchemaVersion = "prowl.knowledge-decision/v1"
	maxDecisionFieldBytes      = 128
	decisionLockTimeout        = 5 * time.Second
)

var (
	ErrInvalidDecisionRequest  = errors.New("invalid proposal decision request")
	ErrProposalVersionConflict = errors.New("proposal version conflict")
	ErrIdempotencyConflict     = errors.New("idempotency key conflict")
	ErrDecisionInProgress      = errors.New("proposal decision is in progress")
)

// DecisionAction is the only durable review decision a workbench client may
// request. The caller cannot provide a proposal status directly.
type DecisionAction string

const (
	DecisionAccept DecisionAction = "accept"
	DecisionReject DecisionAction = "reject"
)

// DecisionRequest contains only server-validated mutation preconditions. The
// workbench derives PrincipalID from its authenticated local session instead of
// accepting it from a browser request.
type DecisionRequest struct {
	ProposalID      string
	Action          DecisionAction
	ExpectedVersion string
	IdempotencyKey  string
	PrincipalID     string
}

// ProposalState is the immutable review material required before a decision.
type ProposalState struct {
	Proposal Proposal
	Version  string
	Diff     string
}

// RollbackPlan names only repository-rooted canonical paths that Accept
// snapshots before changing durable knowledge.
type RollbackPlan struct {
	Paths []string `json:"paths"`
}

// DecisionAudit is the durable, immutable record of one successful review
// decision. It is committed inside the same atomic proposal-record write as the
// final proposal status, and contains no absolute paths or client actor ID.
type DecisionAudit struct {
	SchemaVersion   string         `json:"schema_version"`
	ProposalID      string         `json:"proposal_id"`
	Action          DecisionAction `json:"action"`
	IdempotencyKey  string         `json:"idempotency_key"`
	PrincipalID     string         `json:"principal_id"`
	ExpectedVersion string         `json:"expected_version"`
	VersionBefore   string         `json:"version_before"`
	VersionAfter    string         `json:"version_after"`
	DecidedAt       string         `json:"decided_at"`
	Rollback        RollbackPlan   `json:"rollback"`
}

// DecisionResult returns the durable result of a proposal decision. Idempotent
// replays return the immutable original audit without attempting a second write.
type DecisionResult struct {
	Proposal   Proposal      `json:"proposal"`
	Version    string        `json:"version"`
	Audit      DecisionAudit `json:"audit"`
	Idempotent bool          `json:"idempotent"`
}

// Describe returns the current review material and its optimistic-concurrency
// version without mutating proposal or canonical knowledge state.
func (inbox *ReviewInbox) Describe(id string) (ProposalState, error) {
	proposal, err := inbox.load(id)
	if err != nil {
		return ProposalState{}, err
	}
	version, err := inbox.proposalVersion(proposal)
	if err != nil {
		return ProposalState{}, err
	}
	diff, err := inbox.Diff(id)
	if err != nil {
		return ProposalState{}, err
	}
	return ProposalState{Proposal: *proposal, Version: version, Diff: diff}, nil
}

// Decide applies or rejects a proposal under an exclusive proposal-inbox lock.
// It checks the optimistic version before mutation, records a rooted rollback
// plan, and writes exactly one immutable audit record per idempotency key.
func (inbox *ReviewInbox) Decide(ctx context.Context, request DecisionRequest, now time.Time) (DecisionResult, error) {
	if err := request.validate(); err != nil {
		return DecisionResult{}, err
	}
	if inbox == nil || inbox.Repository == nil || inbox.Repository.Codec == nil {
		return DecisionResult{}, errors.New("knowledge repository is required")
	}
	if err := os.MkdirAll(inbox.Root, 0o755); err != nil {
		return DecisionResult{}, err
	}
	lock := flock.New(filepath.Join(inbox.Root, ".decision.lock"))
	locked, err := lock.TryLockContext(ctx, decisionLockTimeout)
	if err != nil {
		return DecisionResult{}, err
	}
	if !locked {
		return DecisionResult{}, ErrDecisionInProgress
	}
	defer func() { _ = lock.Unlock() }()

	state, err := inbox.Describe(request.ProposalID)
	if err != nil {
		return DecisionResult{}, err
	}
	if audit := state.Proposal.Decision; audit != nil {
		if err := validStoredDecisionAudit(*audit, state.Proposal, state.Version); err != nil {
			return DecisionResult{}, err
		}
		if audit.IdempotencyKey != request.IdempotencyKey {
			return DecisionResult{}, ErrProposalVersionConflict
		}
		if audit.Action != request.Action || audit.ExpectedVersion != request.ExpectedVersion || audit.PrincipalID != request.PrincipalID {
			return DecisionResult{}, ErrIdempotencyConflict
		}
		return DecisionResult{Proposal: state.Proposal, Version: state.Version, Audit: *audit, Idempotent: true}, nil
	}
	if state.Version != request.ExpectedVersion || state.Proposal.Status != "proposed" {
		return DecisionResult{}, ErrProposalVersionConflict
	}
	rollback, err := inbox.decisionRollbackPlan(state.Proposal)
	if err != nil {
		return DecisionResult{}, err
	}
	final := state.Proposal
	switch request.Action {
	case DecisionAccept:
		final.Status = "accepted"
	case DecisionReject:
		final.Status = "rejected"
	default:
		return DecisionResult{}, ErrInvalidDecisionRequest
	}
	final.ReviewedAt = now.UTC().Format(time.RFC3339)
	final.Decision = nil
	version, err := inbox.proposalVersion(&final)
	if err != nil {
		return DecisionResult{}, err
	}
	audit := DecisionAudit{
		SchemaVersion: decisionAuditSchemaVersion,
		ProposalID:    final.ID, Action: request.Action, IdempotencyKey: request.IdempotencyKey, PrincipalID: request.PrincipalID,
		ExpectedVersion: request.ExpectedVersion, VersionBefore: state.Version, VersionAfter: version, DecidedAt: now.UTC().Format(time.RFC3339Nano),
		Rollback: rollback,
	}
	var proposal *Proposal
	switch request.Action {
	case DecisionAccept:
		proposal, err = inbox.accept(request.ProposalID, now, &audit)
	case DecisionReject:
		proposal, err = inbox.reject(request.ProposalID, now, &audit)
	}
	if err != nil {
		return DecisionResult{}, err
	}
	actualVersion, err := inbox.proposalVersion(proposal)
	if err != nil {
		return DecisionResult{}, err
	}
	if actualVersion != version || proposal.Decision == nil {
		return DecisionResult{}, errors.New("proposal decision transaction did not persist its audit")
	}
	return DecisionResult{Proposal: *proposal, Version: actualVersion, Audit: *proposal.Decision}, nil
}

func (request DecisionRequest) validate() error {
	if request.Action != DecisionAccept && request.Action != DecisionReject || !validDecisionField(request.ProposalID) || !validDecisionField(request.ExpectedVersion) || !validDecisionField(request.IdempotencyKey) || !validDecisionField(request.PrincipalID) {
		return ErrInvalidDecisionRequest
	}
	if _, err := hex.DecodeString(request.ExpectedVersion); err != nil || len(request.ExpectedVersion) != sha256.Size*2 {
		return ErrInvalidDecisionRequest
	}
	if filepath.Base(request.ProposalID) != request.ProposalID || strings.ContainsAny(request.ProposalID, `/\`) {
		return ErrInvalidDecisionRequest
	}
	return nil
}

func validDecisionField(value string) bool {
	return value != "" && len(value) <= maxDecisionFieldBytes && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0 && !strings.ContainsAny(value, `/\`)
}
func validStoredDecisionAudit(audit DecisionAudit, proposal Proposal, version string) error {
	request := DecisionRequest{
		ProposalID:      audit.ProposalID,
		Action:          audit.Action,
		ExpectedVersion: audit.ExpectedVersion,
		IdempotencyKey:  audit.IdempotencyKey,
		PrincipalID:     audit.PrincipalID,
	}
	if audit.SchemaVersion != decisionAuditSchemaVersion || request.validate() != nil || audit.ProposalID != proposal.ID || audit.VersionBefore == "" || audit.VersionAfter != version || len(audit.Rollback.Paths) != 3 || audit.Rollback.Paths[0] != proposal.TargetPath || audit.Rollback.Paths[1] != "index.md" || audit.Rollback.Paths[2] != "log.md" {
		return errors.New("invalid proposal decision audit")
	}
	if audit.Action == DecisionAccept && proposal.Status != "accepted" || audit.Action == DecisionReject && proposal.Status != "rejected" {
		return errors.New("proposal decision audit does not match proposal status")
	}
	if _, err := time.Parse(time.RFC3339Nano, audit.DecidedAt); err != nil {
		return errors.New("invalid proposal decision timestamp")
	}
	return nil
}

func (inbox *ReviewInbox) proposalVersion(proposal *Proposal) (string, error) {
	if proposal == nil {
		return "", errors.New("proposal is required")
	}
	candidate, err := readRootFile(inbox.Root, proposal.CandidatePath, MaxDocumentBytes)
	if err != nil {
		return "", err
	}
	candidateHash := sha256.Sum256(candidate)
	payload := struct {
		ID, Operation, TargetPath, CandidatePath, BaseHash, Status, Author, CreatedAt, ReviewedAt, CandidateHash string
	}{
		ID: proposal.ID, Operation: proposal.Operation, TargetPath: proposal.TargetPath, CandidatePath: proposal.CandidatePath,
		BaseHash: proposal.BaseHash, Status: proposal.Status, Author: proposal.Author, CreatedAt: proposal.CreatedAt, ReviewedAt: proposal.ReviewedAt,
		CandidateHash: hex.EncodeToString(candidateHash[:]),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

func (inbox *ReviewInbox) decisionRollbackPlan(proposal Proposal) (RollbackPlan, error) {
	if inbox.Repository == nil {
		return RollbackPlan{}, errors.New("knowledge repository is required")
	}
	paths := []string{proposal.TargetPath, "index.md", "log.md"}
	for index, path := range paths {
		_, clean, err := inbox.Repository.resolve(path)
		if err != nil {
			return RollbackPlan{}, err
		}
		paths[index] = clean
	}
	return RollbackPlan{Paths: paths}, nil
}
