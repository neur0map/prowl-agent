package workbench

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	contextpacket "github.com/prowl-agent/prowl-agent/internal/context"
)

func TestContextLensParity(t *testing.T) {
	project := openWorkbenchProject(t, map[string]string{
		"internal/auth/token.go": "package auth\n\n// Validate verifies the local bearer token.\nfunc Validate(token string) bool { return token != \"\" }\n",
	})
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}
	request := contextpacket.Request{Question: "Where is the local bearer token validated?", Mode: contextpacket.ModeCompact, BudgetTokens: 180}

	rawSearch, err := project.Context.Search(request)
	if err != nil {
		t.Fatal(err)
	}
	wantSearch, err := contextpacket.MarshalCanonicalProjection(rawSearch)
	if err != nil {
		t.Fatal(err)
	}
	lensSearch, err := service.ContextSearch(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	gotSearch, err := contextpacket.MarshalCanonicalProjection(lensSearch.Packet)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotSearch, wantSearch) {
		t.Fatalf("service projection differs from canonical context packet:\n got: %s\nwant: %s", gotSearch, wantSearch)
	}

	if len(rawSearch.Items) == 0 {
		t.Fatal("search fixture did not produce a source item")
	}
	getRequest := contextpacket.Request{IDs: []string{rawSearch.Items[0].ID}, Mode: contextpacket.ModeCompact, BudgetTokens: 180}
	rawGet, err := project.Context.Get(getRequest)
	if err != nil {
		t.Fatal(err)
	}
	wantGet, err := contextpacket.MarshalCanonicalProjection(rawGet)
	if err != nil {
		t.Fatal(err)
	}
	lensGet, err := service.ContextGet(context.Background(), getRequest)
	if err != nil {
		t.Fatal(err)
	}
	gotGet, err := contextpacket.MarshalCanonicalProjection(lensGet.Packet)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotGet, wantGet) {
		t.Fatalf("get projection differs from canonical context packet:\n got: %s\nwant: %s", gotGet, wantGet)
	}

	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: service})
	if err != nil {
		t.Fatal(err)
	}
	assertContextLensHTTPParity(t, handler, "/api/v1/context/search", contextLensBody(t, contextSearchInput{Question: request.Question, Mode: request.Mode, BudgetTokens: request.BudgetTokens}), wantSearch)
	assertContextLensHTTPParity(t, handler, "/api/v1/context/get", contextLensBody(t, contextGetInput{IDs: getRequest.IDs, Mode: getRequest.Mode, BudgetTokens: getRequest.BudgetTokens}), wantGet)
}

func TestContextLensRejectsUnboundedRequests(t *testing.T) {
	project := openWorkbenchProject(t, nil)
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: service})
	if err != nil {
		t.Fatal(err)
	}

	overflowIDs := make([]string, MaxContextLensIDs+1)
	for index := range overflowIDs {
		overflowIDs[index] = "source:fixture"
	}
	tests := []struct {
		name, path string
		body       []byte
		status     int
	}{
		{name: "missing question", path: "/api/v1/context/search", body: []byte(`{"question":"   "}`), status: http.StatusBadRequest},
		{name: "oversized body", path: "/api/v1/context/search", body: []byte(`{"question":"` + strings.Repeat("x", MaxContextRequestBytes) + `"}`), status: http.StatusRequestEntityTooLarge},
		{name: "unknown field", path: "/api/v1/context/search", body: []byte(`{"question":"valid","unbounded":true}`), status: http.StatusBadRequest},
		{name: "trailing JSON", path: "/api/v1/context/search", body: []byte(`{"question":"valid"} {}`), status: http.StatusBadRequest},
		{name: "excess budget", path: "/api/v1/context/search", body: contextLensBody(t, contextSearchInput{Question: "valid", BudgetTokens: MaxContextLensBudgetTokens + 1}), status: http.StatusBadRequest},
		{name: "too many IDs", path: "/api/v1/context/get", body: contextLensBody(t, contextGetInput{IDs: overflowIDs}), status: http.StatusBadRequest},
		{name: "oversized ID", path: "/api/v1/context/get", body: contextLensBody(t, contextGetInput{IDs: []string{strings.Repeat("x", MaxContextLensIDBytes+1)}}), status: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, authorizedContextRequest(test.path, test.body))
			if response.Code != test.status {
				t.Fatalf("status=%d want=%d body=%q", response.Code, test.status, response.Body.String())
			}
			if strings.Contains(response.Body.String(), project.Workspace.Root) {
				t.Fatalf("error leaked workspace root: %q", response.Body.String())
			}
		})
	}

	request := authorizedContextRequest("/api/v1/context/search", []byte(`{"question":"valid"}`))
	request.Method = http.MethodGet
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("method status=%d allow=%q body=%q", response.Code, response.Header().Get("Allow"), response.Body.String())
	}
}

func assertContextLensHTTPParity(t *testing.T, handler http.Handler, path string, body, want []byte) {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authorizedContextRequest(path, body))
	if response.Code != http.StatusOK {
		t.Fatalf("%s status=%d body=%q", path, response.Code, response.Body.String())
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
		Meta responseMeta    `json:"meta"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(envelope.Data, want) {
		t.Fatalf("%s projection differs from canonical packet:\n got: %s\nwant: %s", path, envelope.Data, want)
	}
	if bytes.Contains(envelope.Data, []byte(`"trace_id"`)) || envelope.Meta.ResourceVersion == "" || envelope.Meta.ResourceVersion == unavailableVersion {
		t.Fatalf("%s returned volatile or unavailable data: %s", path, response.Body.Bytes())
	}
}

func contextLensBody(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func authorizedContextRequest(path string, body []byte) *http.Request {
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	request.Host = "127.0.0.1:43117"
	request.Header.Set("Authorization", "Bearer test-secret")
	request.Header.Set("Content-Type", "application/json")
	return request
}
