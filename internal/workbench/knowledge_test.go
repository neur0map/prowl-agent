package workbench

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prowl-agent/prowl-agent/internal/knowledge"
)

func TestKnowledgeReadReturnsStablePagesDetailsAndProposalDiff(t *testing.T) {
	project := openWorkbenchProject(t, nil)
	if err := project.Knowledge.Init(); err != nil {
		t.Fatal(err)
	}
	for _, document := range []*knowledge.Document{
		{Path: "architecture/alpha.md", Type: "Concept", Title: "Alpha architecture", Description: "The first durable concept.", Body: []byte("Alpha detail.\n"), Prowl: knowledge.Metadata{ID: "architecture-alpha", Status: "accepted", Confidence: "high"}},
		{Path: "decisions/bravo.md", Type: "Decision", Title: "Bravo decision", Description: "A decision linked to Alpha.", Body: []byte("Bravo detail.\n"), Prowl: knowledge.Metadata{ID: "decision-bravo", Status: "accepted", Related: []string{"architecture-alpha"}}},
		{Path: "decisions/charlie.md", Type: "Decision", Title: "Charlie decision", Description: "A later stable decision.", Body: []byte("Charlie detail.\n"), Prowl: knowledge.Metadata{ID: "decision-charlie", Status: "accepted"}},
	} {
		if err := project.Knowledge.Write(document); err != nil {
			t.Fatal(err)
		}
	}
	candidatePath := filepath.Join(t.TempDir(), "candidate.md")
	if err := os.WriteFile(candidatePath, []byte("---\ntype: Decision\ntitle: Reviewable candidate\nprowl:\n  id: reviewable-candidate\n---\nCandidate detail.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inbox := knowledge.NewReviewInbox(project.Workspace.Proposals, project.Knowledge)
	proposal, _, err := inbox.Propose(candidatePath, "decisions/candidate.md", "tester", time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}
	page, err := service.Knowledge(context.Background(), KnowledgePageRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Path != "architecture/alpha.md" || page.Next == "" || page.Next == page.Items[0].Path {
		t.Fatalf("first page=%+v", page)
	}
	second, err := service.Knowledge(context.Background(), KnowledgePageRequest{Limit: 1, Cursor: page.Next})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].Path != "decisions/bravo.md" || second.Next == "" {
		t.Fatalf("second page=%+v", second)
	}
	third, err := service.Knowledge(context.Background(), KnowledgePageRequest{Limit: 1, Cursor: second.Next})
	if err != nil {
		t.Fatal(err)
	}
	if len(third.Items) != 1 || third.Items[0].Path != "decisions/charlie.md" || third.Next != "" {
		t.Fatalf("third page=%+v", third)
	}
	detail, err := service.KnowledgeDetail(context.Background(), "architecture-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Body != "Alpha detail.\n" || len(detail.Backlinks) != 1 || detail.Backlinks[0].ID != "decision-bravo" {
		t.Fatalf("detail=%+v", detail)
	}
	proposalDetail, err := service.KnowledgeProposal(context.Background(), proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if proposalDetail.Proposal.ID != proposal.ID || !strings.Contains(proposalDetail.Diff, "+Candidate detail.") {
		t.Fatalf("proposal detail=%+v", proposalDetail)
	}

	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: service})
	if err != nil {
		t.Fatal(err)
	}
	first := knowledgePageFromResponse(t, handler, "/api/v1/knowledge?limit=1")
	if len(first.Items) != 1 || first.Next == "" {
		t.Fatalf("HTTP first page=%+v", first)
	}
	second = knowledgePageFromResponse(t, handler, "/api/v1/knowledge?limit=1&cursor="+first.Next)
	if len(second.Items) != 1 || second.Items[0].ID != "decision-bravo" || second.Next == "" {
		t.Fatalf("HTTP second page=%+v", second)
	}
	third = knowledgePageFromResponse(t, handler, "/api/v1/knowledge?limit=1&cursor="+second.Next)
	if len(third.Items) != 1 || third.Items[0].ID != "decision-charlie" || third.Next != "" {
		t.Fatalf("HTTP third page=%+v", third)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authorizedKnowledgeRequest("/api/v1/knowledge/architecture-alpha"))
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), project.Workspace.Root) {
		t.Fatalf("detail status=%d body=%q", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, authorizedKnowledgeRequest("/api/v1/knowledge/proposals/"+proposal.ID))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "+Candidate detail.") {
		t.Fatalf("proposal status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestKnowledgeReadRejectsInvalidPaginationAndMissingDetail(t *testing.T) {
	project := openWorkbenchProject(t, nil)
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: service})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path string
		want int
	}{
		{path: "/api/v1/knowledge?limit=101", want: http.StatusBadRequest},
		{path: "/api/v1/knowledge?cursor=not-a-valid-cursor", want: http.StatusBadRequest},
		{path: "/api/v1/knowledge/missing", want: http.StatusNotFound},
		{path: "/api/v1/knowledge/proposals/missing", want: http.StatusNotFound},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, authorizedKnowledgeRequest(test.path))
		if response.Code != test.want || strings.Contains(response.Body.String(), project.Workspace.Root) {
			t.Fatalf("path=%s status=%d want=%d body=%q", test.path, response.Code, test.want, response.Body.String())
		}
	}
}

func knowledgePageFromResponse(t *testing.T, handler http.Handler, path string) KnowledgePage {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authorizedKnowledgeRequest(path))
	if response.Code != http.StatusOK {
		t.Fatalf("path=%s status=%d body=%q", path, response.Code, response.Body.String())
	}
	var envelope struct {
		Data KnowledgePage `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

func authorizedKnowledgeRequest(path string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Host = "127.0.0.1:43117"
	request.Header.Set("Authorization", "Bearer test-secret")
	return request
}
