// Package workbench serves Prowl's authenticated, loopback-only human interface.
package workbench

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"

	"github.com/prowl-agent/prowl-agent/internal/store"
)

const (
	APIVersion            = "v1"
	MaxRequestIDBytes     = 128
	MaxErrorResponseBytes = 4096
	unavailableVersion    = "unavailable"
)

// APIOptions defines the browser security boundary for the local API.
type APIOptions struct {
	Token              string
	AllowedOrigin      string
	Service            *Service
	RequestIDGenerator func([]byte) (int, error)
}

var fallbackRequestID atomic.Uint64

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
	if options.RequestIDGenerator == nil {
		options.RequestIDGenerator = rand.Read
	}

	routes := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/health":
			if request.Method != http.MethodGet {
				response.Header().Set("Allow", http.MethodGet)
				writeError(response, request, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed", unavailableVersion)
				return
			}
			if options.Service == nil {
				writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "workbench service is unavailable", unavailableVersion)
				return
			}
			health, err := options.Service.Health(request.Context())
			if err != nil {
				status, code, message := projectionError(err, "health")
				writeError(response, request, status, code, message, errorResourceVersion(err))
				return
			}
			writeSuccess(response, request, health.resourceVersion, health, MaxBriefResponseBytes)
		case "/api/v1/brief":
			if request.Method != http.MethodGet {
				response.Header().Set("Allow", http.MethodGet)
				writeError(response, request, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed", unavailableVersion)
				return
			}
			if options.Service == nil {
				writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "workbench service is unavailable", unavailableVersion)
				return
			}
			brief, err := options.Service.Brief(request.Context())
			if err != nil {
				status, code, message := projectionError(err, "brief")
				writeError(response, request, status, code, message, errorResourceVersion(err))
				return
			}
			writeSuccess(response, request, brief.resourceVersion, brief, MaxBriefResponseBytes)
		default:
			writeError(response, request, http.StatusNotFound, "not_found", "API route was not found", unavailableVersion)
		}
	})
	return securityBoundary(routes, options), nil
}

type responseMeta struct {
	RequestID       string `json:"request_id"`
	ResourceVersion string `json:"resource_version"`
}

func writeSuccess(response http.ResponseWriter, request *http.Request, resourceVersion string, data any, maxBytes int) {
	requestID := responseRequestID(request)
	payload := struct {
		Data any          `json:"data"`
		Meta responseMeta `json:"meta"`
	}{Data: data, Meta: responseMeta{RequestID: requestID, ResourceVersion: resourceVersion}}
	encoded, err := json.Marshal(payload)
	if err != nil || len(encoded)+1 > maxBytes {
		writeErrorWithID(response, requestID, http.StatusInternalServerError, "response_unavailable", "response is unavailable", resourceVersion)
		return
	}
	writeJSONBytes(response, requestID, http.StatusOK, append(encoded, '\n'))
}

func writeError(response http.ResponseWriter, request *http.Request, status int, code, message, resourceVersion string) {
	writeErrorWithID(response, responseRequestID(request), status, code, message, resourceVersion)
}

func writeErrorWithID(response http.ResponseWriter, requestID string, status int, code, message, resourceVersion string) {
	payload := struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Meta responseMeta `json:"meta"`
	}{Meta: responseMeta{RequestID: requestID, ResourceVersion: resourceVersion}}
	payload.Error.Code = code
	payload.Error.Message = message
	encoded, err := json.Marshal(payload)
	if err != nil || len(encoded)+1 > MaxErrorResponseBytes {
		// Re-marshal a bounded, non-reflecting payload so the fallback remains
		// valid JSON and uses the exact request ID written to the header.
		payload.Error.Code = "response_unavailable"
		payload.Error.Message = "response is unavailable"
		payload.Meta = responseMeta{RequestID: requestID, ResourceVersion: resourceVersion}
		encoded, _ = json.Marshal(payload)
	}
	writeJSONBytes(response, requestID, status, append(encoded, '\n'))
}

func writeJSONBytes(response http.ResponseWriter, requestID string, status int, body []byte) {
	setSecurityHeaders(response)
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("X-Request-ID", requestID)
	response.WriteHeader(status)
	_, _ = response.Write(body)
}

func responseRequestID(request *http.Request) string {
	requestID := request.Header.Get("X-Request-ID")
	if requestID != "" && len(requestID) <= MaxRequestIDBytes && strings.IndexFunc(requestID, invalidRequestIDRune) < 0 {
		return requestID
	}
	var id [16]byte
	generator, _ := request.Context().Value(requestIDGeneratorKey{}).(func([]byte) (int, error))
	if generator == nil {
		generator = rand.Read
	}
	if n, err := generator(id[:]); err == nil && n == len(id) {
		return hex.EncodeToString(id[:])
	}
	return fmt.Sprintf("fallback-%016x", fallbackRequestID.Add(1))
}

type requestIDGeneratorKey struct{}

func invalidRequestIDRune(r rune) bool {
	return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:-", r))
}

func projectionError(err error, route string) (int, string, string) {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return http.StatusRequestTimeout, "request_canceled", "request was canceled"
	case errors.Is(err, store.ErrGenerationIncomplete):
		return http.StatusServiceUnavailable, "project_unavailable", "project data is unavailable"
	default:
		return http.StatusInternalServerError, route + "_unavailable", "project " + route + " is unavailable"
	}
}

func errorResourceVersion(err error) string {
	var projection *ProjectionError
	if errors.As(err, &projection) {
		return projection.ResourceVersion
	}
	return unavailableVersion
}

func securityBoundary(next http.Handler, options APIOptions) http.Handler {
	origin, _ := url.Parse(options.AllowedOrigin)
	expectedHost := origin.Host
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		setSecurityHeaders(response)
		response.Header().Set("Cache-Control", "no-store")
		if request.Host != expectedHost {
			writeError(response, request, http.StatusForbidden, "forbidden", "request is forbidden", unavailableVersion)
			return
		}
		if request.Header.Get("Sec-Fetch-Site") == "cross-site" {
			writeError(response, request, http.StatusForbidden, "forbidden", "request is forbidden", unavailableVersion)
			return
		}
		if requestOrigin := request.Header.Get("Origin"); requestOrigin != "" && requestOrigin != options.AllowedOrigin {
			writeError(response, request, http.StatusForbidden, "forbidden", "request is forbidden", unavailableVersion)
			return
		}
		provided := request.Header.Get("Authorization")
		expected := "Bearer " + options.Token
		if len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			response.Header().Set("WWW-Authenticate", "Bearer")
			writeError(response, request, http.StatusUnauthorized, "authentication_required", "authentication is required", unavailableVersion)
			return
		}
		ctx := context.WithValue(request.Context(), requestIDGeneratorKey{}, options.RequestIDGenerator)
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}
