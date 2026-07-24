package workbench

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/prowl-agent/prowl-agent/internal/knowledge"
)

func TestProposalAPIRequiresConfirmationAndIsIdempotent(t *testing.T) {
	project := openWorkbenchProject(t, nil)
	if err := project.Knowledge.Init(); err != nil {
		t.Fatal(err)
	}
	inbox := knowledge.NewReviewInbox(project.Workspace.Proposals, project.Knowledge)
	accepted := newKnowledgeProposal(t, inbox, "accepted.md", "decisions/accepted.md")
	rejected := newKnowledgeProposal(t, inbox, "rejected.md", "decisions/rejected.md")
	stale := newKnowledgeProposal(t, inbox, "stale.md", "decisions/stale.md")
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: service})
	if err != nil {
		t.Fatal(err)
	}

	acceptedDetail, err := service.KnowledgeProposal(context.Background(), accepted.ID)
	if err != nil || acceptedDetail.Version == "" {
		t.Fatalf("accepted detail=%+v err=%v", acceptedDetail, err)
	}
	missingConfirmation := proposalDecisionResponse(t, handler, accepted.ID, "accept", map[string]any{"expected_version": acceptedDetail.Version, "idempotency_key": "accepted-confirmation", "confirm": false})
	if missingConfirmation.Code != http.StatusBadRequest || !strings.Contains(missingConfirmation.Body.String(), "confirmation_required") {
		t.Fatalf("confirmation response=%d %q", missingConfirmation.Code, missingConfirmation.Body.String())
	}
	if _, err := project.Knowledge.ReadBundleFile("decisions/accepted.md"); !os.IsNotExist(err) {
		t.Fatalf("unconfirmed decision wrote canonical knowledge: %v", err)
	}

	response := proposalDecisionResponse(t, handler, accepted.ID, "accept", map[string]any{"expected_version": acceptedDetail.Version, "idempotency_key": "accepted-decision", "confirm": true})
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), project.Workspace.Root) {
		t.Fatalf("accept response=%d %q", response.Code, response.Body.String())
	}
	var acceptedEnvelope struct {
		Data KnowledgeProposalDecision `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &acceptedEnvelope); err != nil {
		t.Fatal(err)
	}
	if acceptedEnvelope.Data.Idempotent || acceptedEnvelope.Data.Proposal.Status != "accepted" || acceptedEnvelope.Data.Audit.PrincipalID != knowledge.LocalPrincipalID || acceptedEnvelope.Data.Audit.VersionBefore != acceptedDetail.Version || acceptedEnvelope.Data.Version == "" {
		t.Fatalf("accept result=%+v", acceptedEnvelope.Data)
	}
	if _, err := project.Knowledge.ReadBundleFile("decisions/accepted.md"); err != nil {
		t.Fatalf("accepted canonical knowledge missing: %v", err)
	}
	replay := proposalDecisionResponse(t, handler, accepted.ID, "accept", map[string]any{"expected_version": acceptedDetail.Version, "idempotency_key": "accepted-decision", "confirm": true})
	var replayEnvelope struct {
		Data KnowledgeProposalDecision `json:"data"`
	}
	if replay.Code != http.StatusOK || json.Unmarshal(replay.Body.Bytes(), &replayEnvelope) != nil || !replayEnvelope.Data.Idempotent || !reflect.DeepEqual(replayEnvelope.Data.Audit, acceptedEnvelope.Data.Audit) {
		t.Fatalf("replay response=%d body=%q result=%+v", replay.Code, replay.Body.String(), replayEnvelope.Data)
	}
	response = proposalDecisionResponse(t, handler, accepted.ID, "reject", map[string]any{"expected_version": acceptedDetail.Version, "idempotency_key": "accepted-decision", "confirm": true})
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "proposal_version_conflict") {
		t.Fatalf("mismatched replay response=%d %q", response.Code, response.Body.String())
	}

	rejectedDetail, err := service.KnowledgeProposal(context.Background(), rejected.ID)
	if err != nil {
		t.Fatal(err)
	}
	response = proposalDecisionResponse(t, handler, rejected.ID, "reject", map[string]any{"expected_version": rejectedDetail.Version, "idempotency_key": "rejected-decision", "confirm": true})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"rejected"`) {
		t.Fatalf("reject response=%d %q", response.Code, response.Body.String())
	}
	var rejectedEnvelope struct {
		Data KnowledgeProposalDecision `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &rejectedEnvelope); err != nil || rejectedEnvelope.Data.Audit.Action != knowledge.DecisionReject || rejectedEnvelope.Data.Audit.PrincipalID != knowledge.LocalPrincipalID || rejectedEnvelope.Data.Idempotent {
		t.Fatalf("reject result=%+v err=%v", rejectedEnvelope.Data, err)
	}
	if _, err := project.Knowledge.ReadBundleFile("decisions/rejected.md"); !os.IsNotExist(err) {
		t.Fatalf("rejected decision wrote canonical knowledge: %v", err)
	}

	response = proposalDecisionResponse(t, handler, stale.ID, "accept", map[string]any{"expected_version": strings.Repeat("0", 64), "idempotency_key": "stale-decision", "confirm": true})
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "proposal_version_conflict") {
		t.Fatalf("stale response=%d %q", response.Code, response.Body.String())
	}
	if _, err := project.Knowledge.ReadBundleFile("decisions/stale.md"); !os.IsNotExist(err) {
		t.Fatalf("stale decision wrote canonical knowledge: %v", err)
	}

	response = proposalDecisionResponse(t, handler, stale.ID, "accept", map[string]any{"expected_version": strings.Repeat("0", 64), "idempotency_key": "unknown-actor", "confirm": true, "actor_id": "browser"})
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid_request") {
		t.Fatalf("client actor response=%d %q", response.Code, response.Body.String())
	}

	escapeBody, err := json.Marshal(map[string]any{"expected_version": strings.Repeat("0", 64), "idempotency_key": "path-escape", "confirm": true})
	if err != nil {
		t.Fatal(err)
	}
	escapeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge/proposals/placeholder/accept", bytes.NewReader(escapeBody))
	escapeRequest.URL.Path = "/api/v1/knowledge/proposals/../outside/accept"
	escapeRequest.URL.RawPath = "/api/v1/knowledge/proposals/%2e%2e%2foutside/accept"
	escapeRequest.Host = "127.0.0.1:43117"
	escapeRequest.Header.Set("Authorization", "Bearer test-secret")
	escapeResponse := httptest.NewRecorder()
	handler.ServeHTTP(escapeResponse, escapeRequest)
	if escapeResponse.Code != http.StatusBadRequest || strings.Contains(escapeResponse.Body.String(), project.Workspace.Root) {
		t.Fatalf("path escape response=%d %q", escapeResponse.Code, escapeResponse.Body.String())
	}
}

func newKnowledgeProposal(t *testing.T, inbox *knowledge.ReviewInbox, filename, target string) *knowledge.Proposal {
	t.Helper()
	candidate := filepath.Join(t.TempDir(), filename)
	content := "---\ntype: Decision\ntitle: " + filename + "\nprowl:\n  id: " + strings.TrimSuffix(filename, ".md") + "\n---\nCandidate " + filename + ".\n"
	if err := os.WriteFile(candidate, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	proposal, _, err := inbox.Propose(candidate, target, "tester", time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return proposal
}

func proposalDecisionResponse(t *testing.T, handler http.Handler, id, action string, body any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/knowledge/proposals/"+id+"/"+action, bytes.NewReader(encoded))
	request.Host = "127.0.0.1:43117"
	request.Header.Set("Authorization", "Bearer test-secret")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
