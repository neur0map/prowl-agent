package workbench

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testBootstrap(t *testing.T) *BootstrapAuthority {
	t.Helper()
	values := []string{"test-bootstrap-nonce", "test-secret"}
	authority, nonce, err := newBootstrapAuthority(time.Now, time.Minute, func() (string, error) {
		value := values[0]
		values = values[1:]
		return value, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authority.consume(nonce); err != nil {
		t.Fatal(err)
	}
	return authority
}

func TestAPISecurityRequiresBearerAndRejectsForeignOrigin(t *testing.T) {
	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117"})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		token  string
		origin string
		want   int
	}{
		{name: "missing bearer", origin: "http://127.0.0.1:43117", want: http.StatusUnauthorized},
		{name: "wrong bearer", token: "wrong", origin: "http://127.0.0.1:43117", want: http.StatusUnauthorized},
		{name: "foreign origin", token: "test-secret", origin: "https://attacker.example", want: http.StatusForbidden},
		{name: "foreign host", token: "test-secret", origin: "http://127.0.0.1:43117", want: http.StatusForbidden},
		{name: "same origin", token: "test-secret", origin: "http://127.0.0.1:43117", want: http.StatusServiceUnavailable},
		{name: "non-browser client", token: "test-secret", want: http.StatusServiceUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
			request.Host = "127.0.0.1:43117"
			if test.name == "foreign host" {
				request.Host = "attacker.example"
			}
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d body=%q", response.Code, test.want, response.Body.String())
			}
			if response.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatalf("API unexpectedly enabled cross-origin access: %q", response.Header().Get("Access-Control-Allow-Origin"))
			}
		})
	}
}

func TestAPISecurityRejectsCrossSiteFetchAndSetsSecurityHeaders(t *testing.T) {
	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	request.Host = "127.0.0.1:43117"
	request.Header.Set("Authorization", "Bearer test-secret")
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-site status=%d body=%q", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	request.Host = "127.0.0.1:43117"
	request.Header.Set("Authorization", "Bearer test-secret")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("same-site status=%d body=%q", response.Code, response.Body.String())
	}
	want := map[string]string{
		"Content-Security-Policy": "default-src 'self'; connect-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'",
		"Referrer-Policy":         "no-referrer",
		"X-Content-Type-Options":  "nosniff",
		"Cache-Control":           "no-store",
	}
	for name, expected := range want {
		if actual := response.Header().Get(name); actual != expected {
			t.Errorf("%s=%q want=%q", name, actual, expected)
		}
	}
}

func TestAPISecurityRejectsUnsafeConfiguration(t *testing.T) {
	bootstrap := testBootstrap(t)
	tests := []APIOptions{
		{AllowedOrigin: "http://127.0.0.1:43117"},
		{Bootstrap: bootstrap, AllowedOrigin: "http://localhost:43117"},
		{Bootstrap: bootstrap, AllowedOrigin: "http://0.0.0.0:43117"},
		{Bootstrap: bootstrap, AllowedOrigin: "https://127.0.0.1:43117"},
		{Bootstrap: bootstrap, AllowedOrigin: "http://127.0.0.1:43117/path"},
		{Bootstrap: bootstrap, AllowedOrigin: "http://127.0.0.1:43117?query=yes"},
		{Bootstrap: bootstrap, AllowedOrigin: "http://user@127.0.0.1:43117"},
	}
	for _, options := range tests {
		if _, err := NewAPI(options); err == nil {
			t.Fatalf("unsafe API configuration accepted: %+v", options)
		}
	}
}

func TestAPIEnvelopeHealthUsesStableRequestID(t *testing.T) {
	project := openWorkbenchProject(t, nil)
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: service})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	request.Host = "127.0.0.1:43117"
	request.Header.Set("Authorization", "Bearer test-secret")
	request.Header.Set("X-Request-ID", "caller-request-7")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("content type=%q", contentType)
	}
	var envelope struct {
		Data struct {
			APIVersion string `json:"api_version"`
			Status     string `json:"status"`
		} `json:"data"`
		Meta struct {
			RequestID       string `json:"request_id"`
			ResourceVersion string `json:"resource_version"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.APIVersion != APIVersion || envelope.Data.Status != "ok" {
		t.Fatalf("health=%+v", envelope.Data)
	}
	if envelope.Meta.RequestID != "caller-request-7" || envelope.Meta.ResourceVersion == APIVersion || envelope.Meta.ResourceVersion == "unavailable" {
		t.Fatalf("meta=%+v", envelope.Meta)
	}
	if got := response.Header().Get("X-Request-ID"); got != envelope.Meta.RequestID {
		t.Fatalf("response request ID=%q meta=%q", got, envelope.Meta.RequestID)
	}
}

func TestAPIEnvelopeHealthReportsUnavailableProject(t *testing.T) {
	project := openWorkbenchProject(t, nil)
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: service})
	if err != nil {
		t.Fatal(err)
	}
	if err := project.Store.SetMeta("index_state", "incomplete"); err != nil {
		t.Fatal(err)
	}
	request := authorizedAPIRequest("/api/v1/health", "health-error")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"code":"project_unavailable"`) {
		t.Fatalf("body=%q", response.Body.String())
	}
}

func TestAPIProjectionErrorsPreserveKnownVersionAndRouteSemantics(t *testing.T) {
	project := openWorkbenchProject(t, nil)
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}
	version, err := project.Store.GetMeta("cli_sig")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(project.Workspace.Knowledge, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project.Workspace.Knowledge, "broken.md"), []byte("not valid OKF"), 0o644); err != nil {
		t.Fatal(err)
	}
	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: service})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct {
		path, code string
	}{
		{path: "/api/v1/brief", code: "brief_unavailable"},
		{path: "/api/v1/health", code: "health_unavailable"},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, authorizedAPIRequest(route.path, "versioned-error"))
		var envelope struct {
			Error struct{ Code string } `json:"error"`
			Meta  responseMeta          `json:"meta"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusInternalServerError || envelope.Error.Code != route.code || envelope.Meta.ResourceVersion != version {
			t.Fatalf("route=%s status=%d envelope=%+v", route.path, response.Code, envelope)
		}
	}
}

func TestAPIEnvelopeBriefSuccessAndStableErrors(t *testing.T) {
	project := openWorkbenchProject(t, map[string]string{"main.go": "package main\nfunc main() {}\n"})
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: service})
	if err != nil {
		t.Fatal(err)
	}

	request := authorizedAPIRequest("/api/v1/brief", "brief-success")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	var success struct {
		Data Brief `json:"data"`
		Meta struct {
			RequestID       string `json:"request_id"`
			ResourceVersion string `json:"resource_version"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &success); err != nil {
		t.Fatal(err)
	}
	if success.Data.Overview.Counts.Files != 1 || success.Meta.RequestID != "brief-success" || success.Meta.ResourceVersion == APIVersion || success.Meta.ResourceVersion == "unavailable" {
		t.Fatalf("success=%+v", success)
	}
	if strings.Contains(response.Body.String(), project.Workspace.Root) {
		t.Fatalf("response leaked workspace root: %s", response.Body.String())
	}

	if err := project.Store.SetMeta("index_state", "incomplete"); err != nil {
		t.Fatal(err)
	}
	request = authorizedAPIRequest("/api/v1/brief", "brief-error")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	var failure struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Meta struct {
			RequestID string `json:"request_id"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil {
		t.Fatal(err)
	}
	if failure.Error.Code != "project_unavailable" || failure.Error.Message != "project data is unavailable" {
		t.Fatalf("error=%+v", failure.Error)
	}
	if failure.Meta.RequestID != "brief-error" || response.Header().Get("X-Request-ID") != "brief-error" {
		t.Fatalf("error meta=%+v header=%q", failure.Meta, response.Header().Get("X-Request-ID"))
	}
}

func TestAPIEnvelopeGeneratesDistinctRequestIDs(t *testing.T) {
	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117"})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for range 2 {
		request := authorizedAPIRequest("/api/v1/health", "")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		id := response.Header().Get("X-Request-ID")
		if id == "" || len(id) > MaxRequestIDBytes {
			t.Fatalf("generated request ID=%q", id)
		}
		ids[id] = true
	}
	if len(ids) != 2 {
		t.Fatalf("request IDs were reused: %v", ids)
	}
}

func TestAPIAllFailuresUseStableBoundedEnvelope(t *testing.T) {
	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, method, path string
		mutate             func(*http.Request)
		status             int
		allow, challenge   string
	}{
		{name: "host", method: http.MethodGet, path: "/api/v1/health", status: 403, mutate: func(r *http.Request) { r.Host = "evil.example" }},
		{name: "origin", method: http.MethodGet, path: "/api/v1/health", status: 403, mutate: func(r *http.Request) { r.Header.Set("Origin", "https://evil.example/secret") }},
		{name: "fetch site", method: http.MethodGet, path: "/api/v1/health", status: 403, mutate: func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }},
		{name: "missing bearer", method: http.MethodGet, path: "/api/v1/health", status: 401, challenge: "Bearer", mutate: func(r *http.Request) { r.Header.Del("Authorization") }},
		{name: "unknown", method: http.MethodGet, path: "/api/v1/hostile-token", status: 404},
		{name: "method", method: http.MethodPost, path: "/api/v1/brief", status: 405, allow: http.MethodGet},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := authorizedAPIRequest(test.path, "safe-id")
			request.Method = test.method
			if test.mutate != nil {
				test.mutate(request)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
			}
			var envelope struct {
				Error struct{ Code, Message string } `json:"error"`
				Meta  responseMeta                   `json:"meta"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Error.Code == "" || envelope.Error.Message == "" {
				t.Fatalf("invalid envelope: %v %q", err, response.Body.String())
			}
			if envelope.Meta.RequestID != "safe-id" || envelope.Meta.ResourceVersion != "unavailable" || response.Header().Get("X-Request-ID") != "safe-id" {
				t.Fatalf("meta=%+v headers=%v", envelope.Meta, response.Header())
			}
			if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "evil.example") || len(response.Body.Bytes()) > MaxErrorResponseBytes {
				t.Fatalf("unsafe response: %q", response.Body.String())
			}
			if response.Header().Get("Allow") != test.allow || response.Header().Get("WWW-Authenticate") != test.challenge {
				t.Fatalf("allow=%q auth=%q", response.Header().Get("Allow"), response.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

func TestRouteInventoryRegistersSetupSubtree(t *testing.T) {
	project := openWorkbenchProject(t, nil)
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: service})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		method, path, body string
		want               int
	}{
		{method: http.MethodGet, path: "/api/v1/setup/detect", want: http.StatusOK},
		{method: http.MethodPost, path: "/api/v1/setup/plan", body: `{"integrations":["agents"]}`, want: http.StatusOK},
		{method: http.MethodPost, path: "/api/v1/setup/apply", body: `{}`, want: http.StatusBadRequest},
		{method: http.MethodPost, path: "/api/v1/setup/verify", body: `{"integrations":[]}`, want: http.StatusOK},
	}
	for _, test := range tests {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		request.Host = "127.0.0.1:43117"
		request.Header.Set("Authorization", "Bearer test-secret")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.want {
			t.Fatalf("%s %s status=%d body=%q want=%d", test.method, test.path, response.Code, response.Body.String(), test.want)
		}
	}
}

func TestRouteInventory(t *testing.T) {
	want := []string{
		"POST /api/v1/auth/bootstrap",
		"GET /api/v1/health",
		"GET /api/v1/brief",
		"GET /api/v1/explore",
		"GET /api/v1/tours/{tour_id}",
		"GET /api/v1/source",
		"POST /api/v1/context/search",
		"POST /api/v1/context/get",
		"POST /api/v1/impact",
		"GET /api/v1/knowledge",
		"GET /api/v1/knowledge/{id}",
		"GET /api/v1/knowledge/proposals/{id}",
		"POST /api/v1/knowledge/proposals/{id}/accept",
		"POST /api/v1/knowledge/proposals/{id}/reject",
		"GET /api/v1/timeline",
		"GET /api/v1/setup/detect",
		"POST /api/v1/setup/plan",
		"POST /api/v1/setup/apply",
		"POST /api/v1/setup/verify",
	}
	got := make([]string, 0, len(routeInventory()))
	for _, route := range routeInventory() {
		got = append(got, route.Method+" "+route.Path)
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("route inventory:\n got %s\nwant %s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestRequestBoundsRejectSetupBodies(t *testing.T) {
	project := openWorkbenchProject(t, nil)
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: service})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"integrations":["` + strings.Repeat("x", MaxSetupRequestBytes) + `"]}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/setup/plan", strings.NewReader(body))
	request.Host = "127.0.0.1:43117"
	request.Header.Set("Authorization", "Bearer test-secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("oversized setup status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestMutationAuthRequiresConfirmedSetupRequest(t *testing.T) {
	project := openWorkbenchProject(t, nil)
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: service})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"integrations":[],"plan_hash":"plan","expected_project_config_version":"version","approved":false,"idempotency_key":"key"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/setup/apply", strings.NewReader(body))
	request.Host = "127.0.0.1:43117"
	request.Header.Set("Authorization", "Bearer test-secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"approval_required"`) {
		t.Fatalf("unconfirmed setup mutation status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestAPIRequestIDRejectsMalformedAndEntropyFailureStaysUnique(t *testing.T) {
	generator := func([]byte) (int, error) { return 0, errors.New("entropy unavailable") }
	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", RequestIDGenerator: generator})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, hostile := range []string{" bad-id ", "line\nbreak"} {
		request := authorizedAPIRequest("/api/v1/health", "")
		request.Header.Set("X-Request-ID", hostile)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		id := response.Header().Get("X-Request-ID")
		if id == "" || id == hostile || len(id) > MaxRequestIDBytes {
			t.Fatalf("generated id=%q", id)
		}
		ids[id] = true
	}
	if len(ids) != 2 {
		t.Fatalf("fallback IDs reused: %v", ids)
	}
}

func TestSuccessWriterEnforcesCompleteEnvelopeBoundBeforeStatus(t *testing.T) {
	request := authorizedAPIRequest("/api/v1/brief", "near-bound")
	response := httptest.NewRecorder()
	writeSuccess(response, request, "abc123", strings.Repeat("x", MaxBriefResponseBytes), MaxBriefResponseBytes)
	if response.Code != http.StatusInternalServerError || len(response.Body.Bytes()) > MaxBriefResponseBytes {
		t.Fatalf("status=%d bytes=%d body=%q", response.Code, len(response.Body.Bytes()), response.Body.String())
	}
	if strings.Contains(response.Body.String(), strings.Repeat("x", 64)) {
		t.Fatal("oversized data was reflected")
	}
}

func TestErrorWriterFallbackIsValidJSONWithMatchingRequestID(t *testing.T) {
	response := httptest.NewRecorder()
	writeErrorWithID(response, "fallback-request", http.StatusInternalServerError, strings.Repeat("x", MaxErrorResponseBytes), "ignored", "abc123")
	var envelope struct {
		Error struct{ Code, Message string } `json:"error"`
		Meta  responseMeta                   `json:"meta"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("fallback is not valid JSON: %v body=%q", err, response.Body.String())
	}
	if envelope.Error.Code != "response_unavailable" || envelope.Meta.RequestID != "fallback-request" || response.Header().Get("X-Request-ID") != envelope.Meta.RequestID {
		t.Fatalf("fallback envelope=%+v header=%q", envelope, response.Header().Get("X-Request-ID"))
	}
}

func authorizedAPIRequest(path, requestID string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Host = "127.0.0.1:43117"
	request.Header.Set("Authorization", "Bearer test-secret")
	if requestID != "" {
		request.Header.Set("X-Request-ID", requestID)
	}
	return request
}
