package workbench

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/knowledge"
)

func TestImpactReturnsCitedProjectEvidence(t *testing.T) {
	project := openWorkbenchProject(t, map[string]string{
		"internal/auth/token.go": "package auth\n\n// Validate verifies a local bearer token.\nfunc Validate(token string) bool { return token != \"\" }\n",
	})
	if err := project.Knowledge.Init(); err != nil {
		t.Fatal(err)
	}
	if err := project.Knowledge.Write(&knowledge.Document{
		Path: "decisions/token-boundary.md", Type: "Decision", Title: "Keep token validation local", Description: "The local workbench validates bearer tokens at the API boundary.",
		Body:  []byte("Validation remains in the loopback API.\n"),
		Prowl: knowledge.Metadata{ID: "token-boundary", Status: "accepted", Anchors: []knowledge.Anchor{{Path: "internal/auth/token.go", LineStart: 3, LineEnd: 4}}},
	}); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}

	impact, err := service.Impact(context.Background(), "internal/auth/token.go")
	if err != nil {
		t.Fatal(err)
	}
	if impact.Path != "internal/auth/token.go" || !impact.Relations.Exists || len(impact.Knowledge) != 1 {
		t.Fatalf("impact=%+v", impact)
	}
	if got := impact.Knowledge[0]; got.ID != "token-boundary" || got.Anchor.Path != "internal/auth/token.go" || got.Anchor.LineStart != 3 || got.Anchor.LineEnd != 4 {
		t.Fatalf("knowledge evidence=%+v", got)
	}

	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: service})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(impactInput{Path: "internal/auth/token.go"})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authorizedImpactRequest(body))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	var envelope struct {
		Data Impact       `json:"data"`
		Meta responseMeta `json:"meta"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Path != impact.Path || len(envelope.Data.Knowledge) != 1 || envelope.Meta.ResourceVersion == "" || strings.Contains(response.Body.String(), project.Workspace.Root) {
		t.Fatalf("unsafe impact response=%q", response.Body.String())
	}
}

func TestImpactRejectsUnsafeOrUnknownPath(t *testing.T) {
	project := openWorkbenchProject(t, map[string]string{"safe.go": "package safe\n"})
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
		{path: "../.env", want: http.StatusBadRequest},
		{path: "missing.go", want: http.StatusNotFound},
	} {
		body, err := json.Marshal(impactInput{Path: test.path})
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, authorizedImpactRequest(body))
		if response.Code != test.want || strings.Contains(response.Body.String(), project.Workspace.Root) {
			t.Fatalf("path=%q status=%d want=%d body=%q", test.path, response.Code, test.want, response.Body.String())
		}
	}
}

func authorizedImpactRequest(body []byte) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/impact", bytes.NewReader(body))
	request.Host = "127.0.0.1:43117"
	request.Header.Set("Authorization", "Bearer test-secret")
	request.Header.Set("Content-Type", "application/json")
	return request
}
