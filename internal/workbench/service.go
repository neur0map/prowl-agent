package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/prowl-agent/prowl-agent/internal/application"
	"github.com/prowl-agent/prowl-agent/internal/capability"
	"github.com/prowl-agent/prowl-agent/internal/knowledge"
	"github.com/prowl-agent/prowl-agent/internal/query"
	"github.com/prowl-agent/prowl-agent/internal/setup"
	"github.com/prowl-agent/prowl-agent/internal/store"
)

const (
	MaxBriefCapabilities     = 16
	MaxBriefDocs             = 8
	MaxBriefEntrypoints      = 20
	MaxBriefClusters         = 8
	MaxBriefPalette          = 16
	MaxBriefHotspots         = 5
	MaxBriefDocuments        = 1000
	MaxBriefKnowledgeEntries = 16000
	MaxBriefStringBytes      = 128
	MaxBriefResponseBytes    = 64 << 10
)

var ErrInvalidDerivedData = errors.New("invalid derived project data")

// ProjectionError identifies a projection failure that happened after the
// immutable resource version was established.
type ProjectionError struct {
	ResourceVersion string
	Err             error
}

func (err *ProjectionError) Error() string { return err.Err.Error() }
func (err *ProjectionError) Unwrap() error { return err.Err }

// Brief is the bounded, path-safe initial project projection for the workbench.
type Brief struct {
	Workspace       WorkspaceIdentity    `json:"workspace"`
	Overview        query.Overview       `json:"overview"`
	Knowledge       KnowledgeHealth      `json:"knowledge"`
	Freshness       Freshness            `json:"freshness"`
	Capabilities    []capability.Summary `json:"capabilities"`
	resourceVersion string
}

type WorkspaceIdentity struct {
	Name string `json:"name"`
}

type KnowledgeHealth struct {
	Status    string `json:"status"`
	Documents int    `json:"documents"`
}

type Freshness struct {
	Status      string `json:"status"`
	LastIndexed string `json:"last_indexed,omitempty"`
}

type Health struct {
	APIVersion      string `json:"api_version"`
	Status          string `json:"status"`
	resourceVersion string
}

// Service projects the application-owned graph without constructing alternate stores.
type Service struct {
	project       *application.Project
	setup         *setup.Service
	afterOverview func() // deterministic package test seam; invoked under ReadGuard
}

func NewService(project *application.Project) (*Service, error) {
	if project == nil || project.Workspace == nil || project.Store == nil || project.Query == nil || project.Context == nil || project.Knowledge == nil || project.Capabilities == nil || project.ReadGuard == nil {
		return nil, errors.New("complete application project is required")
	}
	setupService, err := setup.NewService(project.Workspace.Root)
	if err != nil {
		return nil, err
	}
	return &Service{project: project, setup: setupService}, nil
}

// localPrincipalID is the only workbench actor identity. Browser payloads
// never supply an authoritative principal.
func (service *Service) localPrincipalID() string {
	return knowledge.LocalPrincipalID
}

func (service *Service) Health(ctx context.Context) (Health, error) {
	release, err := service.project.ReadGuard(ctx)
	if err != nil {
		return Health{}, err
	}
	defer release()
	if err := service.project.Store.RequirePublishedGenerationContext(ctx); err != nil {
		return Health{}, err
	}
	version, err := service.resourceVersion(ctx)
	if err != nil {
		return Health{}, err
	}
	fail := func(err error) (Health, error) { return Health{}, versionedProjectionError(version, err) }
	if _, err := service.project.Knowledge.ListContextBounded(ctx, knowledge.ListLimits{Documents: MaxBriefDocuments, Entries: MaxBriefKnowledgeEntries}); err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return Health{APIVersion: APIVersion, Status: "ok", resourceVersion: version}, nil
}

func (service *Service) Brief(ctx context.Context) (Brief, error) {
	release, err := service.project.ReadGuard(ctx)
	if err != nil {
		return Brief{}, err
	}
	defer release()
	if err := service.project.Store.RequirePublishedGenerationContext(ctx); err != nil {
		return Brief{}, err
	}
	version, err := service.resourceVersion(ctx)
	if err != nil {
		return Brief{}, err
	}
	fail := func(err error) (Brief, error) { return Brief{}, versionedProjectionError(version, err) }
	limits := query.DefaultOverviewLimits()
	limits.Docs = MaxBriefDocs
	limits.Entrypoints = MaxBriefEntrypoints
	limits.Clusters = MaxBriefClusters
	limits.Palette = MaxBriefPalette
	limits.Hotspots = MaxBriefHotspots
	limits.StringBytes = MaxBriefStringBytes
	overview, err := service.project.Query.OverviewContext(ctx, limits)
	if err != nil {
		return fail(err)
	}
	if service.afterOverview != nil {
		service.afterOverview()
	}
	documents, err := service.project.Knowledge.ListContextBounded(ctx, knowledge.ListLimits{Documents: MaxBriefDocuments, Entries: MaxBriefKnowledgeEntries})
	if err != nil {
		return fail(err)
	}
	lastIndex, err := service.project.Store.GetMetaContext(ctx, "last_index")
	if err != nil {
		return fail(err)
	}
	lastIndexed, err := parseLastIndex(lastIndex)
	if err != nil {
		return fail(err)
	}
	workspaceName := filepath.Base(service.project.Workspace.Root)
	if err := validateSegment(workspaceName, service.workspaceRoots()...); err != nil {
		return fail(err)
	}
	if err := service.validateOverview(&overview); err != nil {
		return fail(err)
	}
	for _, document := range documents {
		if document == nil || validateRelativePath(document.Path, service.workspaceRoots()...) != nil {
			return fail(ErrInvalidDerivedData)
		}
	}
	capabilities, err := validatedCapabilities(service.project.Capabilities.Search("", MaxBriefCapabilities))
	if err != nil {
		return fail(err)
	}
	brief := Brief{
		Workspace:       WorkspaceIdentity{Name: workspaceName},
		Overview:        overview,
		Knowledge:       KnowledgeHealth{Status: "healthy", Documents: len(documents)},
		Freshness:       Freshness{Status: "current", LastIndexed: lastIndexed},
		Capabilities:    capabilities,
		resourceVersion: version,
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	encoded, err := json.Marshal(brief)
	if err != nil || len(encoded) > MaxBriefResponseBytes {
		return fail(errors.New("project brief exceeds response bound"))
	}
	return brief, nil
}

func (service *Service) resourceVersion(ctx context.Context) (string, error) {
	value, err := service.project.Store.GetMetaContext(ctx, "cli_sig")
	if err != nil {
		return "", sanitizeProjectionError(err)
	}
	if value == "" || len(value) > 16 {
		return "", ErrInvalidDerivedData
	}
	parsed, err := strconv.ParseUint(value, 16, 64)
	if err != nil || strconv.FormatUint(parsed, 16) != value {
		return "", ErrInvalidDerivedData
	}
	return value, nil
}

func parseLastIndex(value string) (string, error) {
	if value == "" || len(value) > 20 {
		return "", ErrInvalidDerivedData
	}
	unix, err := strconv.ParseInt(value, 10, 64)
	if err != nil || unix <= 0 {
		return "", ErrInvalidDerivedData
	}
	indexed := time.Unix(unix, 0).UTC()
	if indexed.Year() < 1 || indexed.Year() > 9999 {
		return "", ErrInvalidDerivedData
	}
	formatted := indexed.Format(time.RFC3339)
	if _, err := time.Parse(time.RFC3339, formatted); err != nil {
		return "", ErrInvalidDerivedData
	}
	return formatted, nil
}

func (service *Service) validateOverview(overview *query.Overview) error {
	roots := service.workspaceRoots()
	for key := range overview.Roles {
		if validateIdentifier(key, roots...) != nil {
			return ErrInvalidDerivedData
		}
	}
	for key := range overview.Counts.Langs {
		if validateIdentifier(key, roots...) != nil {
			return ErrInvalidDerivedData
		}
	}
	for _, value := range overview.Docs {
		if validateRelativePath(value, roots...) != nil {
			return ErrInvalidDerivedData
		}
	}
	for _, value := range overview.Entrypoints {
		if validateRelativePath(value, roots...) != nil {
			return ErrInvalidDerivedData
		}
	}
	for _, cluster := range overview.Clusters {
		if validatePlain(cluster.Label, roots...) != nil || validateIdentifier(cluster.Lang, roots...) != nil {
			return ErrInvalidDerivedData
		}
	}
	for _, resource := range overview.Palette {
		if validateIdentifier(resource.Kind, roots...) != nil || validatePlain(resource.Name, roots...) != nil || validatePlain(resource.Value, roots...) != nil || validateRelativePath(resource.File, roots...) != nil {
			return ErrInvalidDerivedData
		}
	}
	for _, hotspot := range overview.Hotspots {
		if validateRelativePath(hotspot.File, roots...) != nil {
			return ErrInvalidDerivedData
		}
	}
	return nil
}

func (service *Service) workspaceRoots() []string {
	return []string{service.project.Workspace.Root, service.project.Workspace.Path}
}

func validatePlain(value string, roots ...string) error {
	if value == "" || !utf8.ValidString(value) || len(value) > MaxBriefStringBytes || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return ErrInvalidDerivedData
	}
	for _, root := range roots {
		if root != "" && strings.Contains(value, root) {
			return ErrInvalidDerivedData
		}
	}
	return nil
}

func validateSegment(value string, roots ...string) error {
	if validatePlain(value, roots...) != nil || value == "." || value == ".." || strings.ContainsAny(value, `/\`) {
		return ErrInvalidDerivedData
	}
	return nil
}

func validateIdentifier(value string, roots ...string) error {
	if validatePlain(value, roots...) != nil || value == "." || value == ".." || strings.ContainsAny(value, `/\`) || filepath.IsAbs(value) || path.IsAbs(value) {
		return ErrInvalidDerivedData
	}
	if len(value) >= 2 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' {
		return ErrInvalidDerivedData
	}
	return nil
}

func validateRelativePath(value string, roots ...string) error {
	if validatePlain(value, roots...) != nil || strings.Contains(value, "\\") || filepath.IsAbs(value) || path.IsAbs(value) {
		return ErrInvalidDerivedData
	}
	if len(value) >= 2 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' {
		return ErrInvalidDerivedData
	}
	clean := path.Clean(value)
	if clean != value || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return ErrInvalidDerivedData
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return ErrInvalidDerivedData
		}
	}
	return nil
}

func validatedCapabilities(values []capability.Summary) ([]capability.Summary, error) {
	out := append([]capability.Summary(nil), values...)
	for index := range out {
		item := &out[index]
		for _, value := range []string{item.Name, item.Title, item.Description, item.Privacy, item.Version} {
			if validatePlain(value) != nil {
				return nil, ErrInvalidDerivedData
			}
		}
		triggers := append([]string(nil), item.Triggers...)
		for _, trigger := range triggers {
			if validatePlain(trigger) != nil {
				return nil, ErrInvalidDerivedData
			}
		}
		sort.Strings(triggers)
		if len(triggers) > MaxBriefDocs {
			triggers = triggers[:MaxBriefDocs]
		}
		item.Triggers = triggers
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func sanitizeProjectionError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, store.ErrGenerationIncomplete) || errors.Is(err, knowledge.ErrInvalidDecisionRequest) || errors.Is(err, knowledge.ErrProposalVersionConflict) || errors.Is(err, knowledge.ErrIdempotencyConflict) || errors.Is(err, knowledge.ErrDecisionInProgress) {
		return err
	}
	return ErrInvalidDerivedData
}

func versionedProjectionError(version string, err error) error {
	return &ProjectionError{ResourceVersion: version, Err: sanitizeProjectionError(err)}
}
