package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	contextpacket "github.com/prowl-agent/prowl-agent/internal/context"
)

const (
	MaxContextRequestBytes      = 64 << 10
	MaxContextLensResponseBytes = 256 << 10
	MaxContextLensQuestionBytes = 8 << 10
	MaxContextLensIDs           = 32
	MaxContextLensIDBytes       = 8 << 10
	MaxContextLensBudgetTokens  = 16_000
	MaxContextLensBudgetBytes   = 256 << 10
)

var (
	ErrInvalidContextLensRequest = errors.New("invalid Context Lens request")
	ErrContextRequestTooLarge    = errors.New("Context Lens request exceeds bounds")
)

// ContextLens is the transport-safe, canonical context packet. Packet fields
// remain flat in JSON so the browser sees the exact deterministic packet an
// agent receives; the resource version is carried only by the API envelope.
type ContextLens struct {
	contextpacket.Packet
	resourceVersion string
}

type contextSearchInput struct {
	Question     string             `json:"question"`
	Mode         contextpacket.Mode `json:"mode,omitempty"`
	BudgetTokens int                `json:"budget_tokens,omitempty"`
	BudgetBytes  int                `json:"budget_bytes,omitempty"`
}

type contextGetInput struct {
	IDs          []string           `json:"ids"`
	Mode         contextpacket.Mode `json:"mode,omitempty"`
	BudgetTokens int                `json:"budget_tokens,omitempty"`
	BudgetBytes  int                `json:"budget_bytes,omitempty"`
}

func (input contextSearchInput) request() (contextpacket.Request, error) {
	return normalizeContextRequest(contextpacket.Request{
		Question:     input.Question,
		Mode:         input.Mode,
		BudgetTokens: input.BudgetTokens,
		BudgetBytes:  input.BudgetBytes,
	}, true)
}

func (input contextGetInput) request() (contextpacket.Request, error) {
	return normalizeContextRequest(contextpacket.Request{
		IDs:          input.IDs,
		Mode:         input.Mode,
		BudgetTokens: input.BudgetTokens,
		BudgetBytes:  input.BudgetBytes,
	}, false)
}

// ContextSearch returns the same packet as the context service, excluding only
// execution-specific trace data so it is stable across local transports.
func (service *Service) ContextSearch(ctx context.Context, request contextpacket.Request) (ContextLens, error) {
	request, err := normalizeContextRequest(request, true)
	if err != nil {
		return ContextLens{}, err
	}
	return service.contextLens(ctx, request, service.project.Context.Search)
}

// ContextGet returns selected context IDs under the same canonical contract as
// ContextSearch.
func (service *Service) ContextGet(ctx context.Context, request contextpacket.Request) (ContextLens, error) {
	request, err := normalizeContextRequest(request, false)
	if err != nil {
		return ContextLens{}, err
	}
	return service.contextLens(ctx, request, service.project.Context.Get)
}

func (service *Service) contextLens(ctx context.Context, request contextpacket.Request, projectContext func(contextpacket.Request) (contextpacket.Packet, error)) (ContextLens, error) {
	release, err := service.project.ReadGuard(ctx)
	if err != nil {
		return ContextLens{}, err
	}
	defer release()
	if err := service.project.Store.RequirePublishedGenerationContext(ctx); err != nil {
		return ContextLens{}, err
	}
	version, err := service.resourceVersion(ctx)
	if err != nil {
		return ContextLens{}, err
	}
	fail := func(err error) (ContextLens, error) {
		return ContextLens{}, versionedProjectionError(version, err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	packet, err := projectContext(request)
	if err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	packet = contextpacket.CanonicalProjection(packet)
	encoded, err := contextpacket.MarshalCanonicalProjection(packet)
	if err != nil || len(encoded) > MaxContextLensResponseBytes {
		return fail(errors.New("Context Lens response exceeds bounds"))
	}
	return ContextLens{Packet: packet, resourceVersion: version}, nil
}

func normalizeContextRequest(request contextpacket.Request, search bool) (contextpacket.Request, error) {
	if search {
		if strings.TrimSpace(request.Question) == "" || !validContextLensText(request.Question, MaxContextLensQuestionBytes) || len(request.IDs) != 0 {
			return contextpacket.Request{}, ErrInvalidContextLensRequest
		}
	} else if len(request.IDs) == 0 || request.Question != "" {
		return contextpacket.Request{}, ErrInvalidContextLensRequest
	}
	if err := validateContextLensIDs(request.IDs); err != nil {
		return contextpacket.Request{}, err
	}
	if len(request.Filters) != 0 {
		return contextpacket.Request{}, ErrInvalidContextLensRequest
	}
	if err := request.Validate(); err != nil {
		return contextpacket.Request{}, fmt.Errorf("%w: %v", ErrInvalidContextLensRequest, err)
	}
	if request.BudgetTokens == 0 && request.BudgetBytes == 0 && request.Mode != contextpacket.ModeFull {
		request.BudgetTokens = 1800
	}
	if request.BudgetTokens > MaxContextLensBudgetTokens || request.BudgetBytes > MaxContextLensBudgetBytes {
		return contextpacket.Request{}, ErrInvalidContextLensRequest
	}
	return request, nil
}

func validateContextLensIDs(ids []string) error {
	if len(ids) > MaxContextLensIDs {
		return ErrInvalidContextLensRequest
	}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || !validContextLensText(id, MaxContextLensIDBytes) {
			return ErrInvalidContextLensRequest
		}
	}
	return nil
}

func validContextLensText(value string, maxBytes int) bool {
	return utf8.ValidString(value) && len(value) <= maxBytes
}

func decodeContextLensBody(response http.ResponseWriter, request *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, MaxContextRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return ErrContextRequestTooLarge
		}
		return ErrInvalidContextLensRequest
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ErrInvalidContextLensRequest
	}
	return nil
}

func decodeContextSearchRequest(response http.ResponseWriter, request *http.Request) (contextpacket.Request, error) {
	var input contextSearchInput
	if err := decodeContextLensBody(response, request, &input); err != nil {
		return contextpacket.Request{}, err
	}
	return input.request()
}

func decodeContextGetRequest(response http.ResponseWriter, request *http.Request) (contextpacket.Request, error) {
	var input contextGetInput
	if err := decodeContextLensBody(response, request, &input); err != nil {
		return contextpacket.Request{}, err
	}
	return input.request()
}
