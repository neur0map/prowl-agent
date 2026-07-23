package workbench

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestHandlerServesShellButKeepsAPIBearerProtected(t *testing.T) {
	assets := fstest.MapFS{
		"dist/index.html":           {Data: []byte("<!doctype html><title>Prowl Workbench</title>")},
		"dist/assets/app-abc123.js": {Data: []byte("console.log('prowl')")},
	}
	handler, err := NewHandler(HandlerOptions{
		API:    APIOptions{Token: "test-secret", AllowedOrigin: "http://127.0.0.1:43117"},
		Assets: assets,
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		path      string
		bearer    bool
		want      int
		cache     string
		mediaType string
	}{
		{name: "shell", path: "/", want: http.StatusOK, cache: "no-store", mediaType: "text/html; charset=utf-8"},
		{name: "hashed asset", path: "/assets/app-abc123.js", want: http.StatusOK, cache: "public, max-age=31536000, immutable", mediaType: "text/javascript; charset=utf-8"},
		{name: "protected api", path: "/api/v1/health", want: http.StatusUnauthorized},
		{name: "authorized api", path: "/api/v1/health", bearer: true, want: http.StatusOK, cache: "no-store", mediaType: "application/json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			request.Host = "127.0.0.1:43117"
			if test.bearer {
				request.Header.Set("Authorization", "Bearer test-secret")
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d want=%d body=%q", response.Code, test.want, response.Body.String())
			}
			if test.cache != "" && response.Header().Get("Cache-Control") != test.cache {
				t.Errorf("cache=%q want=%q", response.Header().Get("Cache-Control"), test.cache)
			}
			if test.mediaType != "" && response.Header().Get("Content-Type") != test.mediaType {
				t.Errorf("content-type=%q want=%q", response.Header().Get("Content-Type"), test.mediaType)
			}
		})
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Host = "attacker.example"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("foreign host status=%d", response.Code)
	}
}

func TestHandlerRejectsMissingAssetTree(t *testing.T) {
	_, err := NewHandler(HandlerOptions{API: APIOptions{
		Token: "test-secret", AllowedOrigin: "http://127.0.0.1:43117",
	}, Assets: fs.FS(fstest.MapFS{})})
	if err == nil {
		t.Fatal("expected missing dist tree to fail")
	}
}
