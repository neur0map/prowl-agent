package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/prowl-agent/prowl-agent/internal/knowledge"
	"github.com/prowl-agent/prowl-agent/internal/query"
)

const (
	MaxImpactRequestBytes  = 8 << 10
	MaxImpactResponseBytes = 128 << 10
	MaxImpactDocuments     = 1000
	MaxImpactEntries       = 16_000
)

var (
	ErrInvalidImpactRequest = errors.New("invalid impact request")
	ErrImpactNotFound       = errors.New("impact source is unavailable")
)

// Impact combines deterministic graph, test, entrypoint, and knowledge-anchor
// evidence for one indexed, project-relative source file.
type Impact struct {
	Path            string                    `json:"path"`
	Blast           query.BlastSummary        `json:"blast"`
	Relations       query.Relations           `json:"relations"`
	Tests           query.TestsResult         `json:"tests"`
	Entrypoints     query.EntrypointSet       `json:"entrypoints"`
	Knowledge       []ImpactKnowledgeEvidence `json:"knowledge"`
	resourceVersion string
}

// ImpactKnowledgeEvidence identifies one durable claim that anchors to the
// requested source region without expanding its Markdown body.
type ImpactKnowledgeEvidence struct {
	ID     string       `json:"id"`
	Title  string       `json:"title"`
	Type   string       `json:"type"`
	Status string       `json:"status"`
	Anchor ImpactAnchor `json:"anchor"`
}

// ImpactAnchor is a project-relative, line-addressable durable evidence link.
type ImpactAnchor struct {
	Path        string `json:"path"`
	LineStart   int    `json:"line_start"`
	LineEnd     int    `json:"line_end"`
	ContentHash string `json:"content_hash,omitempty"`
}

type impactInput struct {
	Path string `json:"path"`
}

func (input impactInput) request() (string, error) {
	if err := validateImpactPath(input.Path); err != nil {
		return "", ErrInvalidImpactRequest
	}
	return input.Path, nil
}

// Impact returns only source-backed evidence from the application-owned
// services; it never reconstructs graph or knowledge state in the workbench.
func (service *Service) Impact(ctx context.Context, filePath string) (Impact, error) {
	if err := validateImpactPath(filePath); err != nil {
		return Impact{}, ErrInvalidImpactRequest
	}
	release, err := service.project.ReadGuard(ctx)
	if err != nil {
		return Impact{}, err
	}
	defer release()
	if err := service.project.Store.RequirePublishedGenerationContext(ctx); err != nil {
		return Impact{}, err
	}
	version, err := service.resourceVersion(ctx)
	if err != nil {
		return Impact{}, err
	}
	fail := func(err error) (Impact, error) { return Impact{}, versionedProjectionError(version, err) }
	if _, found, err := service.project.Store.GetFileByPath(filePath); err != nil {
		return fail(err)
	} else if !found {
		return Impact{}, ErrImpactNotFound
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	blast, err := service.project.Query.BlastSummarize(filePath)
	if err != nil {
		return fail(err)
	}
	if blast.BySubsystem == nil {
		blast.BySubsystem = []query.SubsystemCount{}
	}
	if blast.DirectFiles == nil {
		blast.DirectFiles = []string{}
	}
	relations, err := service.project.Query.FileRelations(filePath)
	if err != nil {
		return fail(err)
	}
	if !relations.Exists {
		return Impact{}, ErrImpactNotFound
	}
	tests, err := service.project.Query.TestsFor(filePath)
	if err != nil {
		return fail(err)
	}
	entrypoints, err := service.project.Query.EntrypointsFor(filePath)
	if err != nil {
		return fail(err)
	}
	items, err := impactKnowledgeEvidence(ctx, service.project.Knowledge, filePath, service.workspaceRoots())
	if err != nil {
		return fail(err)
	}
	impact := Impact{Path: filePath, Blast: blast, Relations: relations, Tests: tests, Entrypoints: entrypoints, Knowledge: items, resourceVersion: version}
	if err := validateImpact(impact, service.workspaceRoots()); err != nil {
		return fail(err)
	}
	encoded, err := json.Marshal(impact)
	if err != nil || len(encoded) > MaxImpactResponseBytes {
		return fail(errors.New("impact response exceeds bounds"))
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return impact, nil
}

func impactKnowledgeEvidence(ctx context.Context, repository *knowledge.Repository, filePath string, roots []string) ([]ImpactKnowledgeEvidence, error) {
	documents, err := repository.ListContextBounded(ctx, knowledge.ListLimits{Documents: MaxImpactDocuments, Entries: MaxImpactEntries})
	if err != nil {
		return nil, err
	}
	items := make([]ImpactKnowledgeEvidence, 0)
	for _, document := range documents {
		summary, err := knowledgeSummary(document, roots)
		if err != nil {
			return nil, err
		}
		for _, anchor := range summary.Anchors {
			if anchor.Path != filePath {
				continue
			}
			items = append(items, ImpactKnowledgeEvidence{
				ID: summary.ID, Title: summary.Title, Type: summary.Type, Status: summary.Status,
				Anchor: ImpactAnchor{Path: anchor.Path, LineStart: anchor.LineStart, LineEnd: anchor.LineEnd, ContentHash: anchor.ContentHash},
			})
		}
	}
	if items == nil {
		return []ImpactKnowledgeEvidence{}, nil
	}
	return items, nil
}

func validateImpact(impact Impact, roots []string) error {
	if validateImpactPath(impact.Path) != nil || impact.Blast.File != impact.Path || impact.Relations.File != impact.Path || impact.Tests.File != impact.Path || impact.Entrypoints.File != impact.Path {
		return ErrInvalidDerivedData
	}
	for _, item := range impact.Knowledge {
		if item.ID == "" || validateImpactAnchor(knowledge.Anchor{Path: item.Anchor.Path, LineStart: item.Anchor.LineStart, LineEnd: item.Anchor.LineEnd, ContentHash: item.Anchor.ContentHash}, roots...) != nil {
			return ErrInvalidDerivedData
		}
	}
	return nil
}

func validateImpactAnchor(anchor knowledge.Anchor, roots ...string) error {
	return validateKnowledgeAnchor(anchor, roots...)
}

func validateImpactPath(value string) error {
	return (SourcePreviewRequest{Path: value, LineStart: 1, LineEnd: 1}).validate()
}

func decodeImpactRequest(response http.ResponseWriter, request *http.Request) (string, error) {
	var input impactInput
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, MaxImpactRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return "", ErrContextRequestTooLarge
		}
		return "", ErrInvalidImpactRequest
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return "", ErrInvalidImpactRequest
	}
	return input.request()
}

func serveImpact(response http.ResponseWriter, request *http.Request, service *Service) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		writeError(response, request, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed", unavailableVersion)
		return
	}
	if service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "workbench service is unavailable", unavailableVersion)
		return
	}
	filePath, err := decodeImpactRequest(response, request)
	if err != nil {
		writeImpactError(response, request, err)
		return
	}
	impact, err := service.Impact(request.Context(), filePath)
	if err != nil {
		writeImpactError(response, request, err)
		return
	}
	writeSuccess(response, request, impact.resourceVersion, impact, MaxImpactResponseBytes)
}

func writeImpactError(response http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, ErrContextRequestTooLarge):
		writeError(response, request, http.StatusRequestEntityTooLarge, "request_too_large", "request exceeds bounds", unavailableVersion)
	case errors.Is(err, ErrInvalidImpactRequest):
		writeError(response, request, http.StatusBadRequest, "invalid_request", "request is invalid", errorResourceVersion(err))
	case errors.Is(err, ErrImpactNotFound):
		writeError(response, request, http.StatusNotFound, "source_not_found", "source is unavailable", unavailableVersion)
	default:
		status, code, message := projectionError(err, "impact")
		writeError(response, request, status, code, message, errorResourceVersion(err))
	}
}
