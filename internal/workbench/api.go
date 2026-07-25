// Package workbench serves Prowl's authenticated, loopback-only human interface.
package workbench

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"

	contextpacket "github.com/prowl-agent/prowl-agent/internal/context"
	"github.com/prowl-agent/prowl-agent/internal/store"
)

const (
	APIVersion            = "v1"
	MaxRequestIDBytes     = 128
	MaxErrorResponseBytes = 4096
	unavailableVersion    = "unavailable"
	bootstrapRoute        = "/api/v1/auth/bootstrap"
)

// APIOptions defines the browser security boundary for the local API.
type APIOptions struct {
	Bootstrap          *BootstrapAuthority
	AllowedOrigin      string
	Service            *Service
	RequestIDGenerator func([]byte) (int, error)
}

var fallbackRequestID atomic.Uint64

type apiRoute struct {
	Method string
	Path   string
}

var declaredAPIRoutes = []apiRoute{
	{Method: http.MethodPost, Path: bootstrapRoute},
	{Method: http.MethodGet, Path: "/api/v1/health"},
	{Method: http.MethodGet, Path: "/api/v1/brief"},
	{Method: http.MethodGet, Path: "/api/v1/explore"},
	{Method: http.MethodGet, Path: "/api/v1/tours/{tour_id}"},
	{Method: http.MethodGet, Path: "/api/v1/source"},
	{Method: http.MethodPost, Path: "/api/v1/context/search"},
	{Method: http.MethodPost, Path: "/api/v1/context/get"},
	{Method: http.MethodPost, Path: "/api/v1/impact"},
	{Method: http.MethodGet, Path: "/api/v1/knowledge"},
	{Method: http.MethodGet, Path: "/api/v1/knowledge/{id}"},
	{Method: http.MethodGet, Path: "/api/v1/knowledge/proposals/{id}"},
	{Method: http.MethodPost, Path: "/api/v1/knowledge/proposals/{id}/accept"},
	{Method: http.MethodPost, Path: "/api/v1/knowledge/proposals/{id}/reject"},
	{Method: http.MethodGet, Path: "/api/v1/timeline"},
	{Method: http.MethodGet, Path: "/api/v1/setup/detect"},
	{Method: http.MethodPost, Path: "/api/v1/setup/plan"},
	{Method: http.MethodPost, Path: "/api/v1/setup/apply"},
	{Method: http.MethodPost, Path: "/api/v1/setup/verify"},
	{Method: http.MethodGet, Path: "/api/v1/events"},
	{Method: http.MethodGet, Path: "/api/v1/jobs/{id}"},
	{Method: http.MethodPost, Path: "/api/v1/jobs/{id}/cancel"},
	{Method: http.MethodPost, Path: "/api/v1/index/refresh"},
}

func routeInventory() []apiRoute {
	return append([]apiRoute(nil), declaredAPIRoutes...)
}

func validateRouteInventory(routes []apiRoute) error {
	seen := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		if route.Method != http.MethodGet && route.Method != http.MethodPost || !strings.HasPrefix(route.Path, "/api/v1/") {
			return errors.New("invalid workbench route declaration")
		}
		key := route.Method + "\x00" + route.Path
		if _, exists := seen[key]; exists {
			return errors.New("duplicate workbench route declaration")
		}
		seen[key] = struct{}{}
	}
	return nil
}

// NewAPI constructs the versioned workbench API. Network binding is handled by
// the launcher; this handler independently enforces bearer and origin checks.
func NewAPI(options APIOptions) (http.Handler, error) {
	if options.Bootstrap == nil {
		return nil, errors.New("workbench bootstrap authority is required")
	}
	origin, err := url.Parse(options.AllowedOrigin)
	if err != nil || origin.Scheme != "http" || origin.Hostname() != "127.0.0.1" || origin.Port() == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return nil, errors.New("workbench origin must be a bare http://127.0.0.1:<port> origin")
	}
	if options.RequestIDGenerator == nil {
		options.RequestIDGenerator = rand.Read
	}
	if err := validateRouteInventory(routeInventory()); err != nil {
		return nil, err
	}

	setupRoutes := serveSetupRoute(nil)
	if options.Service != nil {
		setupRoutes = serveSetupRoute(options.Service.setup)
	}

	routes := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case bootstrapRoute:
			if request.Method != http.MethodPost {
				response.Header().Set("Allow", http.MethodPost)
				writeError(response, request, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed", unavailableVersion)
				return
			}
			var payload struct {
				Nonce string `json:"nonce"`
			}
			decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 512))
			if err := decoder.Decode(&payload); err != nil {
				writeError(response, request, http.StatusUnauthorized, "bootstrap_denied", "bootstrap was denied", unavailableVersion)
				return
			}
			bearer, err := options.Bootstrap.consume(payload.Nonce)
			if err != nil {
				writeError(response, request, http.StatusUnauthorized, "bootstrap_denied", "bootstrap was denied", unavailableVersion)
				return
			}
			writeBootstrapSuccess(response, request, bearer)
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
		case "/api/v1/explore":
			if request.Method != http.MethodGet {
				response.Header().Set("Allow", http.MethodGet)
				writeError(response, request, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed", unavailableVersion)
				return
			}
			if options.Service == nil {
				writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "workbench service is unavailable", unavailableVersion)
				return
			}
			explore, err := options.Service.Explore(request.Context())
			if err != nil {
				status, code, message := projectionError(err, "explore")
				writeError(response, request, status, code, message, errorResourceVersion(err))
				return
			}
			writeSuccess(response, request, explore.resourceVersion, explore, MaxExploreResponseBytes)
		case "/api/v1/source":
			if request.Method != http.MethodGet {
				response.Header().Set("Allow", http.MethodGet)
				writeError(response, request, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed", unavailableVersion)
				return
			}
			if options.Service == nil {
				writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "workbench service is unavailable", unavailableVersion)
				return
			}
			sourceRequest, err := parseSourcePreviewRequest(request.URL.Query())
			if err != nil {
				writeError(response, request, http.StatusBadRequest, "invalid_request", "request is invalid", unavailableVersion)
				return
			}
			preview, err := options.Service.SourcePreview(request.Context(), sourceRequest)
			if err != nil {
				writeSourcePreviewError(response, request, err)
				return
			}
			writeSuccess(response, request, preview.resourceVersion, preview, MaxSourcePreviewResponseBytes)
		case "/api/v1/context/search":
			serveContextLens(response, request, options.Service, decodeContextSearchRequest, (*Service).ContextSearch)
		case "/api/v1/context/get":
			serveContextLens(response, request, options.Service, decodeContextGetRequest, (*Service).ContextGet)
		case "/api/v1/impact":
			serveImpact(response, request, options.Service)
		case "/api/v1/knowledge":
			serveKnowledgeList(response, request, options.Service)
		case "/api/v1/timeline":
			serveTimeline(response, request, options.Service)
		case "/api/v1/events":
			serveEvents(response, request, options.Service)
		case "/api/v1/index/refresh":
			serveRefresh(response, request, options.Service)
		case "/api/v1/setup/detect", "/api/v1/setup/plan", "/api/v1/setup/apply", "/api/v1/setup/verify":
			setupRoutes.ServeHTTP(response, request)
			return
		default:
			if strings.HasPrefix(request.URL.Path, "/api/v1/knowledge/") {
				serveKnowledgeRoute(response, request, options.Service)
				return
			}
			if strings.HasPrefix(request.URL.Path, "/api/v1/jobs/") {
				serveJobRoute(response, request, options.Service)
				return
			}
			if strings.HasPrefix(request.URL.Path, "/api/v1/tours/") {
				serveGuidedTour(response, request, options.Service)
				return
			}
			writeError(response, request, http.StatusNotFound, "not_found", "API route was not found", unavailableVersion)
		}
	})
	return securityBoundary(routes, options), nil
}

func serveGuidedTour(response http.ResponseWriter, request *http.Request, service *Service) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeError(response, request, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed", unavailableVersion)
		return
	}
	if service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "workbench service is unavailable", unavailableVersion)
		return
	}
	id, err := url.PathUnescape(strings.TrimPrefix(request.URL.EscapedPath(), "/api/v1/tours/"))
	if err != nil || id == "" {
		writeError(response, request, http.StatusNotFound, "not_found", "API route was not found", unavailableVersion)
		return
	}
	tour, err := service.GuidedTour(request.Context(), id)
	if errors.Is(err, ErrTourNotFound) {
		writeError(response, request, http.StatusNotFound, "not_found", "API route was not found", unavailableVersion)
		return
	}
	if err != nil {
		status, code, message := projectionError(err, "tour")
		writeError(response, request, status, code, message, errorResourceVersion(err))
		return
	}
	writeSuccess(response, request, tour.resourceVersion, tour, MaxExploreResponseBytes)
}

type contextRequestDecoder func(http.ResponseWriter, *http.Request) (contextpacket.Request, error)
type contextLensOperation func(*Service, context.Context, contextpacket.Request) (ContextLens, error)

func serveContextLens(response http.ResponseWriter, request *http.Request, service *Service, decode contextRequestDecoder, operation contextLensOperation) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		writeError(response, request, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed", unavailableVersion)
		return
	}
	if service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "workbench service is unavailable", unavailableVersion)
		return
	}
	contextRequest, err := decode(response, request)
	if err != nil {
		writeContextLensError(response, request, err)
		return
	}
	lens, err := operation(service, request.Context(), contextRequest)
	if err != nil {
		writeContextLensError(response, request, err)
		return
	}
	writeSuccess(response, request, lens.resourceVersion, lens, MaxContextLensResponseBytes)
}

func writeContextLensError(response http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, ErrContextRequestTooLarge):
		writeError(response, request, http.StatusRequestEntityTooLarge, "request_too_large", "request exceeds bounds", unavailableVersion)
	case errors.Is(err, ErrInvalidContextLensRequest):
		writeError(response, request, http.StatusBadRequest, "invalid_request", "request is invalid", errorResourceVersion(err))
	default:
		status, code, message := projectionError(err, "context")
		writeError(response, request, status, code, message, errorResourceVersion(err))
	}
}

func parseSourcePreviewRequest(values url.Values) (SourcePreviewRequest, error) {
	pathValues, starts, ends := values["path"], values["line_start"], values["line_end"]
	if len(pathValues) != 1 || len(starts) != 1 || len(ends) != 1 {
		return SourcePreviewRequest{}, ErrInvalidSourcePreview
	}
	lineStart, err := strconv.Atoi(starts[0])
	if err != nil {
		return SourcePreviewRequest{}, ErrInvalidSourcePreview
	}
	lineEnd, err := strconv.Atoi(ends[0])
	if err != nil {
		return SourcePreviewRequest{}, ErrInvalidSourcePreview
	}
	request := SourcePreviewRequest{Path: pathValues[0], LineStart: lineStart, LineEnd: lineEnd}
	return request, request.validate()
}

func writeSourcePreviewError(response http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, ErrInvalidSourcePreview):
		writeError(response, request, http.StatusBadRequest, "invalid_request", "request is invalid", unavailableVersion)
	case errors.Is(err, ErrSourceNotFound):
		writeError(response, request, http.StatusNotFound, "source_not_found", "source is unavailable", unavailableVersion)
	case errors.Is(err, ErrSourceTooLarge):
		writeError(response, request, http.StatusRequestEntityTooLarge, "source_too_large", "source preview exceeds bounds", unavailableVersion)
	default:
		status, code, message := projectionError(err, "source")
		writeError(response, request, status, code, message, errorResourceVersion(err))
	}
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

func writeBootstrapSuccess(response http.ResponseWriter, request *http.Request, bearer string) {
	requestID := responseRequestID(request)
	payload := struct {
		Bearer string `json:"bearer"`
	}{Bearer: bearer}
	encoded, _ := json.Marshal(payload)
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
		query, err := url.ParseQuery(request.URL.RawQuery)
		if err != nil || hasCredentialQuery(query) {
			writeError(response, request, http.StatusBadRequest, "invalid_request", "request is invalid", unavailableVersion)
			return
		}
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
		if request.URL.Path == bootstrapRoute {
			if request.Header.Get("Origin") != options.AllowedOrigin {
				writeError(response, request, http.StatusForbidden, "forbidden", "request is forbidden", unavailableVersion)
				return
			}
			ctx := context.WithValue(request.Context(), requestIDGeneratorKey{}, options.RequestIDGenerator)
			next.ServeHTTP(response, request.WithContext(ctx))
			return
		}
		if !options.Bootstrap.authorizes(request.Header.Get("Authorization")) {
			response.Header().Set("WWW-Authenticate", "Bearer")
			writeError(response, request, http.StatusUnauthorized, "authentication_required", "authentication is required", unavailableVersion)
			return
		}
		ctx := context.WithValue(request.Context(), requestIDGeneratorKey{}, options.RequestIDGenerator)
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

func hasCredentialQuery(values url.Values) bool {
	for key := range values {
		switch strings.ToLower(key) {
		case "access_token", "token", "bearer", "authorization", "api_key":
			return true
		}
	}
	return false
}
