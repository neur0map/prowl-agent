package workbench

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIRequiresBearerAndRejectsForeignOrigin(t *testing.T) {
	handler, err := NewAPI(APIOptions{
		Token:         "test-secret",
		AllowedOrigin: "http://127.0.0.1:43117",
	})
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
		{name: "same origin", token: "test-secret", origin: "http://127.0.0.1:43117", want: http.StatusOK},
		{name: "non-browser client", token: "test-secret", want: http.StatusOK},
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

func TestAPIRejectsCrossSiteFetchAndSetsSecurityHeaders(t *testing.T) {
	handler, err := NewAPI(APIOptions{Token: "test-secret", AllowedOrigin: "http://127.0.0.1:43117"})
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
	if response.Code != http.StatusOK {
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

func TestAPIRejectsUnsafeConfiguration(t *testing.T) {
	tests := []APIOptions{
		{AllowedOrigin: "http://127.0.0.1:43117"},
		{Token: "secret", AllowedOrigin: "http://localhost:43117"},
		{Token: "secret", AllowedOrigin: "http://0.0.0.0:43117"},
		{Token: "secret", AllowedOrigin: "https://127.0.0.1:43117"},
		{Token: "secret", AllowedOrigin: "http://127.0.0.1:43117/path"},
		{Token: "secret", AllowedOrigin: "http://127.0.0.1:43117?query=yes"},
		{Token: "secret", AllowedOrigin: "http://user@127.0.0.1:43117"},
	}
	for _, options := range tests {
		if _, err := NewAPI(options); err == nil {
			t.Fatalf("unsafe API configuration accepted: %+v", options)
		}
	}
}

func TestAPIHealthIsVersionedJSON(t *testing.T) {
	handler, err := NewAPI(APIOptions{Token: "test-secret", AllowedOrigin: "http://127.0.0.1:43117"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	request.Host = "127.0.0.1:43117"
	request.Header.Set("Authorization", "Bearer test-secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("content type=%q", contentType)
	}
	var health struct {
		APIVersion string `json:"api_version"`
		Status     string `json:"status"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health.APIVersion != "v1" || health.Status != "ok" {
		t.Fatalf("health=%+v", health)
	}
}
