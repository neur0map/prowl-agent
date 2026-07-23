package workbench

import (
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
)

// HandlerOptions configures the complete local workbench HTTP surface.
type HandlerOptions struct {
	API    APIOptions
	Assets fs.FS
}

// NewHandler combines public immutable application assets with the protected
// versioned API. Static files contain neither project data nor bearer tokens.
func NewHandler(options HandlerOptions) (http.Handler, error) {
	if options.Assets == nil {
		return nil, errors.New("workbench assets are required")
	}
	assets, err := fs.Sub(options.Assets, "dist")
	if err != nil {
		return nil, err
	}
	if _, err := fs.Stat(assets, "index.html"); err != nil {
		return nil, errors.New("workbench index asset is required")
	}
	api, err := NewAPI(options.API)
	if err != nil {
		return nil, err
	}
	parsedOrigin, _ := url.Parse(options.API.AllowedOrigin)
	expectedHost := parsedOrigin.Host
	static := http.FileServer(http.FS(assets))

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/api/") {
			api.ServeHTTP(response, request)
			return
		}
		setSecurityHeaders(response)
		if request.Host != expectedHost {
			http.Error(response, "forbidden host", http.StatusForbidden)
			return
		}
		if request.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(response, "forbidden fetch site", http.StatusForbidden)
			return
		}
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			response.Header().Set("Allow", "GET, HEAD")
			http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.HasPrefix(request.URL.Path, "/assets/") {
			response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			response.Header().Set("Cache-Control", "no-store")
		}
		static.ServeHTTP(response, request)
	}), nil
}

func setSecurityHeaders(response http.ResponseWriter) {
	response.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	response.Header().Set("Referrer-Policy", "no-referrer")
	response.Header().Set("X-Content-Type-Options", "nosniff")
}
