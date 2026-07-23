// Package workbench serves Prowl's authenticated, loopback-only human interface.
package workbench

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
)

const APIVersion = "v1"

// APIOptions defines the browser security boundary for the local API.
type APIOptions struct {
	Token         string
	AllowedOrigin string
}

// NewAPI constructs the versioned workbench API. Network binding is handled by
// the launcher; this handler independently enforces bearer and origin checks.
func NewAPI(options APIOptions) (http.Handler, error) {
	if options.Token == "" {
		return nil, errors.New("workbench bearer token is required")
	}
	origin, err := url.Parse(options.AllowedOrigin)
	if err != nil || origin.Scheme != "http" || origin.Hostname() != "127.0.0.1" || origin.Port() == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return nil, errors.New("workbench origin must be a bare http://127.0.0.1:<port> origin")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]string{"api_version": APIVersion, "status": "ok"})
	})
	return securityBoundary(mux, options), nil
}

func securityBoundary(next http.Handler, options APIOptions) http.Handler {
	origin, _ := url.Parse(options.AllowedOrigin)
	expectedHost := origin.Host
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		setSecurityHeaders(response)
		response.Header().Set("Cache-Control", "no-store")
		if request.Host != expectedHost {
			http.Error(response, "forbidden host", http.StatusForbidden)
			return
		}
		if request.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(response, "forbidden fetch site", http.StatusForbidden)
			return
		}
		if origin := request.Header.Get("Origin"); origin != "" && origin != options.AllowedOrigin {
			http.Error(response, "forbidden origin", http.StatusForbidden)
			return
		}
		provided := request.Header.Get("Authorization")
		expected := "Bearer " + options.Token
		if len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			response.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(response, "authentication required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(response, request)
	})
}
