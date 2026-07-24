package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"time"

	"github.com/prowl-agent/prowl-agent/internal/knowledge"
)

const (
	MaxKnowledgeProposalDecisionRequestBytes = 2 << 10
	maxKnowledgeProposalDecisionFieldBytes   = 128
)

var (
	ErrKnowledgeRequestTooLarge      = errors.New("knowledge request exceeds bounds")
	ErrKnowledgeConfirmationRequired = errors.New("knowledge proposal confirmation is required")
)

// KnowledgeProposalDecisionInput contains explicit, browser-supplied
// preconditions. The server chooses the action and authenticated principal.
type KnowledgeProposalDecisionInput struct {
	ExpectedVersion string `json:"expected_version"`
	IdempotencyKey  string `json:"idempotency_key"`
	Confirm         bool   `json:"confirm"`
}

// KnowledgeProposalDecision is the durable, replay-safe review outcome.
type KnowledgeProposalDecision struct {
	Proposal   knowledge.Proposal      `json:"proposal"`
	Version    string                  `json:"version"`
	Audit      knowledge.DecisionAudit `json:"audit"`
	Idempotent bool                    `json:"idempotent"`

	resourceVersion string
}

// DecideKnowledgeProposal performs a confirmed, versioned proposal decision.
// The local principal is derived at the server boundary, not supplied by a
// browser request.
func (service *Service) DecideKnowledgeProposal(ctx context.Context, id string, action knowledge.DecisionAction, input KnowledgeProposalDecisionInput) (KnowledgeProposalDecision, error) {
	if err := validateKnowledgeProposalID(id); err != nil {
		return KnowledgeProposalDecision{}, ErrInvalidKnowledgeRequest
	}
	if err := input.validate(); err != nil {
		return KnowledgeProposalDecision{}, err
	}
	release, version, err := service.beginKnowledgeRead(ctx)
	if err != nil {
		return KnowledgeProposalDecision{}, err
	}
	defer release()
	fail := func(err error) (KnowledgeProposalDecision, error) {
		return KnowledgeProposalDecision{}, versionedProjectionError(version, err)
	}
	inbox := knowledge.NewReviewInbox(service.project.Workspace.Proposals, service.project.Knowledge)
	result, err := inbox.Decide(ctx, knowledge.DecisionRequest{
		ProposalID:      id,
		Action:          action,
		ExpectedVersion: input.ExpectedVersion,
		IdempotencyKey:  input.IdempotencyKey,
		PrincipalID:     knowledge.LocalPrincipalID,
	}, time.Now().UTC())
	if errors.Is(err, fs.ErrNotExist) {
		return KnowledgeProposalDecision{}, ErrKnowledgeNotFound
	}
	if err != nil {
		return fail(err)
	}
	proposal, err := safeKnowledgeProposal(result.Proposal, service.workspaceRoots())
	if err != nil {
		return fail(err)
	}
	audit, err := safeKnowledgeDecisionAudit(result.Audit, proposal, service.workspaceRoots())
	if err != nil {
		return fail(err)
	}
	if audit.VersionAfter != result.Version {
		return fail(ErrInvalidDerivedData)
	}
	decision := KnowledgeProposalDecision{Proposal: proposal, Version: result.Version, Audit: audit, Idempotent: result.Idempotent, resourceVersion: version}
	encoded, err := json.Marshal(decision)
	if err != nil || len(encoded) > MaxKnowledgeDetailResponseBytes {
		return fail(ErrKnowledgeTooLarge)
	}
	return decision, nil
}

func (input KnowledgeProposalDecisionInput) validate() error {
	if !input.Confirm {
		return ErrKnowledgeConfirmationRequired
	}
	if !validKnowledgeText(input.ExpectedVersion, maxKnowledgeProposalDecisionFieldBytes) || !validKnowledgeText(input.IdempotencyKey, maxKnowledgeProposalDecisionFieldBytes) || input.ExpectedVersion == "" || input.IdempotencyKey == "" {
		return ErrInvalidKnowledgeRequest
	}
	return nil
}

func safeKnowledgeProposal(proposal knowledge.Proposal, roots []string) (knowledge.Proposal, error) {
	if validateKnowledgeProposalID(proposal.ID) != nil || validateKnowledgePath(proposal.TargetPath, roots...) != nil || validateKnowledgePath(proposal.CandidatePath, roots...) != nil || (proposal.Operation != "create" && proposal.Operation != "update") || (proposal.Status != "proposed" && proposal.Status != "accepted" && proposal.Status != "rejected") || !validKnowledgeText(proposal.BaseHash, maxKnowledgeProposalDecisionFieldBytes, roots...) || !validKnowledgeText(proposal.Author, maxKnowledgeProposalDecisionFieldBytes, roots...) || !validKnowledgeText(proposal.CreatedAt, 64, roots...) || !validKnowledgeText(proposal.ReviewedAt, 64, roots...) {
		return knowledge.Proposal{}, ErrInvalidDerivedData
	}
	clean := proposal
	if proposal.Decision != nil {
		audit, err := safeKnowledgeDecisionAudit(*proposal.Decision, clean, roots)
		if err != nil {
			return knowledge.Proposal{}, err
		}
		clean.Decision = &audit
	}
	return clean, nil
}

func safeKnowledgeDecisionAudit(audit knowledge.DecisionAudit, proposal knowledge.Proposal, roots []string) (knowledge.DecisionAudit, error) {
	if audit.ProposalID != proposal.ID || audit.PrincipalID != knowledge.LocalPrincipalID || audit.Action != knowledge.DecisionAccept && audit.Action != knowledge.DecisionReject || audit.ExpectedVersion == "" || audit.VersionBefore == "" || audit.VersionAfter == "" || !validKnowledgeText(audit.SchemaVersion, 128, roots...) || !validKnowledgeText(audit.IdempotencyKey, maxKnowledgeProposalDecisionFieldBytes, roots...) || !validKnowledgeText(audit.ExpectedVersion, maxKnowledgeProposalDecisionFieldBytes, roots...) || !validKnowledgeText(audit.VersionBefore, maxKnowledgeProposalDecisionFieldBytes, roots...) || !validKnowledgeText(audit.VersionAfter, maxKnowledgeProposalDecisionFieldBytes, roots...) || !validKnowledgeText(audit.PrincipalID, maxKnowledgeProposalDecisionFieldBytes, roots...) || !validKnowledgeText(audit.DecidedAt, 64, roots...) {
		return knowledge.DecisionAudit{}, ErrInvalidDerivedData
	}
	if audit.Action == knowledge.DecisionAccept && proposal.Status != "accepted" || audit.Action == knowledge.DecisionReject && proposal.Status != "rejected" {
		return knowledge.DecisionAudit{}, ErrInvalidDerivedData
	}
	if len(audit.Rollback.Paths) != 3 || audit.Rollback.Paths[0] != proposal.TargetPath || audit.Rollback.Paths[1] != "index.md" || audit.Rollback.Paths[2] != "log.md" {
		return knowledge.DecisionAudit{}, ErrInvalidDerivedData
	}
	if _, err := time.Parse(time.RFC3339Nano, audit.DecidedAt); err != nil {
		return knowledge.DecisionAudit{}, ErrInvalidDerivedData
	}
	clean := audit
	clean.Rollback.Paths = append([]string(nil), audit.Rollback.Paths...)
	for _, path := range clean.Rollback.Paths {
		if validateKnowledgePath(path, roots...) != nil {
			return knowledge.DecisionAudit{}, ErrInvalidDerivedData
		}
	}
	return clean, nil
}

func serveKnowledgeProposalDecision(response http.ResponseWriter, request *http.Request, service *Service, id string, action knowledge.DecisionAction) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		writeError(response, request, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed", unavailableVersion)
		return
	}
	if service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "workbench service is unavailable", unavailableVersion)
		return
	}
	if len(request.URL.Query()) != 0 {
		writeKnowledgeError(response, request, ErrInvalidKnowledgeRequest)
		return
	}
	input, err := decodeKnowledgeProposalDecisionInput(response, request)
	if err != nil {
		writeKnowledgeError(response, request, err)
		return
	}
	decision, err := service.DecideKnowledgeProposal(request.Context(), id, action, input)
	if err != nil {
		writeKnowledgeError(response, request, err)
		return
	}
	writeSuccess(response, request, decision.resourceVersion, decision, MaxKnowledgeDetailResponseBytes)
}

func decodeKnowledgeProposalDecisionInput(response http.ResponseWriter, request *http.Request) (KnowledgeProposalDecisionInput, error) {
	var input KnowledgeProposalDecisionInput
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, MaxKnowledgeProposalDecisionRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return KnowledgeProposalDecisionInput{}, ErrKnowledgeRequestTooLarge
		}
		return KnowledgeProposalDecisionInput{}, ErrInvalidKnowledgeRequest
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return KnowledgeProposalDecisionInput{}, ErrInvalidKnowledgeRequest
	}
	if err := input.validate(); err != nil {
		return KnowledgeProposalDecisionInput{}, err
	}
	return input, nil
}
