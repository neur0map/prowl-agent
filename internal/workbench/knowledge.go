package workbench

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/prowl-agent/prowl-agent/internal/knowledge"
)

const (
	MaxKnowledgePageLimit           = 100
	defaultKnowledgePageLimit       = 50
	MaxKnowledgeCursorBytes         = 4096
	MaxKnowledgeIDBytes             = 4096
	MaxKnowledgeDetailBodyBytes     = 128 << 10
	MaxKnowledgeListResponseBytes   = 256 << 10
	MaxKnowledgeDetailResponseBytes = 256 << 10
	MaxKnowledgeProposalDiffBytes   = 128 << 10
	MaxKnowledgeDocuments           = 1000
	MaxKnowledgeEntries             = 16_000
)

var (
	ErrInvalidKnowledgeRequest = errors.New("invalid knowledge request")
	ErrKnowledgeNotFound       = errors.New("knowledge is unavailable")
	ErrKnowledgeTooLarge       = errors.New("knowledge response exceeds bounds")
)

// KnowledgePageRequest carries a bounded, opaque continuation request.
type KnowledgePageRequest struct {
	Limit  int
	Cursor string
}

// KnowledgePage is a stable page of canonical knowledge summaries.
type KnowledgePage struct {
	Items []KnowledgeSummary `json:"items"`
	Next  string             `json:"next,omitempty"`

	resourceVersion string
}

// KnowledgeSummary exposes canonical metadata and exact source anchors without
// loading a document's potentially large Markdown body.
type KnowledgeSummary struct {
	ID          string            `json:"id"`
	Path        string            `json:"path"`
	Type        string            `json:"type"`
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Resource    string            `json:"resource,omitempty"`
	Tags        []string          `json:"tags"`
	Timestamp   string            `json:"timestamp,omitempty"`
	Status      string            `json:"status,omitempty"`
	Confidence  string            `json:"confidence,omitempty"`
	Related     []string          `json:"related"`
	Anchors     []KnowledgeAnchor `json:"anchors"`
}

// KnowledgeAnchor is a project-relative durable evidence reference.
type KnowledgeAnchor struct {
	Path        string `json:"path"`
	LineStart   int    `json:"line_start"`
	LineEnd     int    `json:"line_end"`
	ContentHash string `json:"content_hash,omitempty"`
	Symbol      string `json:"symbol,omitempty"`
}

// KnowledgeDetail includes one bounded canonical Markdown body and backlinks.
type KnowledgeDetail struct {
	KnowledgeSummary
	Body      string              `json:"body"`
	Backlinks []KnowledgeBacklink `json:"backlinks"`

	resourceVersion string
}

// KnowledgeBacklink is a canonical document that explicitly relates to the
// selected document ID.
type KnowledgeBacklink struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

// KnowledgeProposalDetail exposes immutable review metadata plus the existing
// deterministic candidate diff. It performs no mutation.
type KnowledgeProposalDetail struct {
	Proposal knowledge.Proposal `json:"proposal"`
	Diff     string             `json:"diff"`

	resourceVersion string
}

// Knowledge returns canonical summaries in stable path order.
func (service *Service) Knowledge(ctx context.Context, request KnowledgePageRequest) (KnowledgePage, error) {
	limit, after, err := request.pagination("knowledge")
	if err != nil {
		return KnowledgePage{}, err
	}
	release, version, err := service.beginKnowledgeRead(ctx)
	if err != nil {
		return KnowledgePage{}, err
	}
	defer release()
	fail := func(err error) (KnowledgePage, error) { return KnowledgePage{}, versionedProjectionError(version, err) }
	documents, err := service.project.Knowledge.ListContextBounded(ctx, knowledge.ListLimits{Documents: MaxKnowledgeDocuments, Entries: MaxKnowledgeEntries})
	if err != nil {
		return fail(err)
	}
	items := make([]KnowledgeSummary, 0, min(limit, len(documents)))
	hasMore := false
	for _, document := range documents {
		if document == nil {
			return fail(ErrInvalidDerivedData)
		}
		summary, err := knowledgeSummary(document, service.workspaceRoots())
		if err != nil {
			return fail(err)
		}
		if summary.Path <= after {
			continue
		}
		if len(items) == limit {
			hasMore = true
			break
		}
		items = append(items, summary)
	}
	if items == nil {
		items = []KnowledgeSummary{}
	}
	page := KnowledgePage{Items: items, resourceVersion: version}
	if hasMore {
		page.Next = encodePageCursor("knowledge", items[len(items)-1].Path)
	}
	encoded, err := json.Marshal(page)
	if err != nil || len(encoded) > MaxKnowledgeListResponseBytes {
		return fail(ErrKnowledgeTooLarge)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return page, nil
}

// KnowledgeDetail returns one document selected by its durable ID.
func (service *Service) KnowledgeDetail(ctx context.Context, id string) (KnowledgeDetail, error) {
	if err := validateKnowledgeID(id); err != nil {
		return KnowledgeDetail{}, ErrInvalidKnowledgeRequest
	}
	release, version, err := service.beginKnowledgeRead(ctx)
	if err != nil {
		return KnowledgeDetail{}, err
	}
	defer release()
	fail := func(err error) (KnowledgeDetail, error) {
		return KnowledgeDetail{}, versionedProjectionError(version, err)
	}
	documents, err := service.project.Knowledge.ListContextBounded(ctx, knowledge.ListLimits{Documents: MaxKnowledgeDocuments, Entries: MaxKnowledgeEntries})
	if err != nil {
		return fail(err)
	}
	document, err := selectedKnowledgeDocument(documents, id)
	if err != nil {
		return KnowledgeDetail{}, err
	}
	summary, err := knowledgeSummary(document, service.workspaceRoots())
	if err != nil {
		return fail(err)
	}
	if len(document.Body) > MaxKnowledgeDetailBodyBytes || !utf8.Valid(document.Body) {
		return KnowledgeDetail{}, ErrKnowledgeTooLarge
	}
	backlinks, err := knowledgeBacklinks(documents, id, service.workspaceRoots())
	if err != nil {
		return fail(err)
	}
	detail := KnowledgeDetail{KnowledgeSummary: summary, Body: string(document.Body), Backlinks: backlinks, resourceVersion: version}
	encoded, err := json.Marshal(detail)
	if err != nil || len(encoded) > MaxKnowledgeDetailResponseBytes {
		return fail(ErrKnowledgeTooLarge)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return detail, nil
}

// KnowledgeProposal returns one read-only review candidate and its existing
// deterministic diff.
func (service *Service) KnowledgeProposal(ctx context.Context, id string) (KnowledgeProposalDetail, error) {
	if err := validateKnowledgeProposalID(id); err != nil {
		return KnowledgeProposalDetail{}, ErrInvalidKnowledgeRequest
	}
	release, version, err := service.beginKnowledgeRead(ctx)
	if err != nil {
		return KnowledgeProposalDetail{}, err
	}
	defer release()
	fail := func(err error) (KnowledgeProposalDetail, error) {
		return KnowledgeProposalDetail{}, versionedProjectionError(version, err)
	}
	inbox := knowledge.NewReviewInbox(service.project.Workspace.Proposals, service.project.Knowledge)
	proposals, err := inbox.List()
	if err != nil {
		return fail(err)
	}
	var proposal *knowledge.Proposal
	for index := range proposals {
		if proposals[index].ID == id {
			proposal = &proposals[index]
			break
		}
	}
	if proposal == nil {
		return KnowledgeProposalDetail{}, ErrKnowledgeNotFound
	}
	diff, err := inbox.Diff(id)
	if err != nil {
		return fail(err)
	}
	if len(diff) > MaxKnowledgeProposalDiffBytes || !utf8.ValidString(diff) {
		return KnowledgeProposalDetail{}, ErrKnowledgeTooLarge
	}
	detail := KnowledgeProposalDetail{Proposal: *proposal, Diff: diff, resourceVersion: version}
	encoded, err := json.Marshal(detail)
	if err != nil || len(encoded) > MaxKnowledgeDetailResponseBytes {
		return fail(ErrKnowledgeTooLarge)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return detail, nil
}

func (service *Service) beginKnowledgeRead(ctx context.Context) (func(), string, error) {
	release, err := service.project.ReadGuard(ctx)
	if err != nil {
		return nil, "", err
	}
	if err := service.project.Store.RequirePublishedGenerationContext(ctx); err != nil {
		release()
		return nil, "", err
	}
	version, err := service.resourceVersion(ctx)
	if err != nil {
		release()
		return nil, "", err
	}
	return release, version, nil
}

func knowledgeSummary(document *knowledge.Document, roots []string) (KnowledgeSummary, error) {
	if document == nil || validateKnowledgePath(document.Path, roots...) != nil {
		return KnowledgeSummary{}, ErrInvalidDerivedData
	}
	id := knowledgeDocumentID(document)
	if err := validateKnowledgeID(id); err != nil {
		return KnowledgeSummary{}, ErrInvalidDerivedData
	}
	if !validKnowledgeText(document.Type, 256, roots...) || !validKnowledgeText(document.Title, 1024, roots...) || !validKnowledgeText(document.Description, 4096, roots...) || !validKnowledgeText(document.Resource, 4096, roots...) || !validKnowledgeText(document.Timestamp, 64, roots...) || !validKnowledgeText(document.Prowl.Status, 128, roots...) || !validKnowledgeText(document.Prowl.Confidence, 128, roots...) {
		return KnowledgeSummary{}, ErrInvalidDerivedData
	}
	tags := append([]string(nil), document.Tags...)
	for _, tag := range tags {
		if !validKnowledgeText(tag, 256, roots...) {
			return KnowledgeSummary{}, ErrInvalidDerivedData
		}
	}
	related := append([]string(nil), document.Prowl.Related...)
	for _, target := range related {
		if err := validateKnowledgeID(target); err != nil {
			return KnowledgeSummary{}, ErrInvalidDerivedData
		}
	}
	anchors := make([]KnowledgeAnchor, 0, len(document.Prowl.Anchors))
	for _, anchor := range document.Prowl.Anchors {
		if err := validateKnowledgeAnchor(anchor, roots...); err != nil {
			return KnowledgeSummary{}, err
		}
		anchors = append(anchors, KnowledgeAnchor{Path: anchor.Path, LineStart: anchor.LineStart, LineEnd: anchor.LineEnd, ContentHash: anchor.ContentHash, Symbol: anchor.Symbol})
	}
	if tags == nil {
		tags = []string{}
	}
	if related == nil {
		related = []string{}
	}
	if anchors == nil {
		anchors = []KnowledgeAnchor{}
	}
	return KnowledgeSummary{ID: id, Path: document.Path, Type: document.Type, Title: document.Title, Description: document.Description, Resource: document.Resource, Tags: tags, Timestamp: document.Timestamp, Status: document.Prowl.Status, Confidence: document.Prowl.Confidence, Related: related, Anchors: anchors}, nil
}

func knowledgeDocumentID(document *knowledge.Document) string {
	if document == nil {
		return ""
	}
	if document.Prowl.ID != "" {
		return document.Prowl.ID
	}
	return strings.TrimSuffix(filepath.ToSlash(document.Path), filepath.Ext(document.Path))
}

func selectedKnowledgeDocument(documents []*knowledge.Document, id string) (*knowledge.Document, error) {
	var selected *knowledge.Document
	for _, document := range documents {
		if knowledgeDocumentID(document) != id {
			continue
		}
		if selected != nil {
			return nil, ErrKnowledgeNotFound
		}
		selected = document
	}
	if selected == nil {
		return nil, ErrKnowledgeNotFound
	}
	return selected, nil
}

func knowledgeBacklinks(documents []*knowledge.Document, id string, roots []string) ([]KnowledgeBacklink, error) {
	backlinks := make([]KnowledgeBacklink, 0)
	for _, document := range documents {
		summary, err := knowledgeSummary(document, roots)
		if err != nil {
			return nil, err
		}
		if !containsString(summary.Related, id) {
			continue
		}
		backlinks = append(backlinks, KnowledgeBacklink{ID: summary.ID, Path: summary.Path, Type: summary.Type, Title: summary.Title})
	}
	sort.Slice(backlinks, func(left, right int) bool {
		if backlinks[left].ID != backlinks[right].ID {
			return backlinks[left].ID < backlinks[right].ID
		}
		return backlinks[left].Path < backlinks[right].Path
	})
	if backlinks == nil {
		return []KnowledgeBacklink{}, nil
	}
	return backlinks, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func validateKnowledgeAnchor(anchor knowledge.Anchor, roots ...string) error {
	if validateKnowledgePath(anchor.Path, roots...) != nil || anchor.LineStart < 1 || anchor.LineEnd < anchor.LineStart || !validKnowledgeText(anchor.ContentHash, 256, roots...) || !validKnowledgeText(anchor.Symbol, 1024, roots...) {
		return ErrInvalidDerivedData
	}
	return nil
}

func validateKnowledgePath(value string, roots ...string) error {
	if err := validateImpactPath(value); err != nil {
		return err
	}
	for _, root := range roots {
		if root != "" && strings.Contains(value, root) {
			return ErrInvalidDerivedData
		}
	}
	return nil
}

func validateKnowledgeID(value string) error {
	if value == "" || len(value) > MaxKnowledgeIDBytes || !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 || strings.Contains(value, "\\") || pathpkg.IsAbs(value) || strings.HasPrefix(value, "/") {
		return ErrInvalidDerivedData
	}
	clean := pathpkg.Clean(value)
	if clean != value || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return ErrInvalidDerivedData
	}
	return nil
}

func validateKnowledgeProposalID(value string) error {
	if err := validateKnowledgeID(value); err != nil || strings.Contains(value, "/") {
		return ErrInvalidDerivedData
	}
	return nil
}

func validKnowledgeText(value string, maxBytes int, roots ...string) bool {
	if value == "" {
		return true
	}
	if len(value) > maxBytes || !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return false
	}
	for _, root := range roots {
		if root != "" && strings.Contains(value, root) {
			return false
		}
	}
	return true
}

func (request KnowledgePageRequest) pagination(scope string) (int, string, error) {
	return paginate(request.Limit, request.Cursor, scope, func(value string) error {
		return validateKnowledgePath(value)
	})
}

func paginate(limit int, cursor, scope string, validate func(string) error) (int, string, error) {
	if limit == 0 {
		limit = defaultKnowledgePageLimit
	}
	if limit < 1 || limit > MaxKnowledgePageLimit {
		return 0, "", ErrInvalidKnowledgeRequest
	}
	if cursor == "" {
		return limit, "", nil
	}
	after, err := decodePageCursor(scope, cursor, validate)
	if err != nil {
		return 0, "", err
	}
	return limit, after, nil
}

func parseKnowledgePageRequest(values url.Values) (KnowledgePageRequest, error) {
	for key, entries := range values {
		if key != "limit" && key != "cursor" || len(entries) != 1 {
			return KnowledgePageRequest{}, ErrInvalidKnowledgeRequest
		}
	}
	request := KnowledgePageRequest{Cursor: values.Get("cursor")}
	if raw := values.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return KnowledgePageRequest{}, ErrInvalidKnowledgeRequest
		}
		request.Limit = limit
	}
	if _, _, err := request.pagination("knowledge"); err != nil {
		return KnowledgePageRequest{}, err
	}
	return request, nil
}

func encodePageCursor(scope, after string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(scope + "\x00" + after))
}

func decodePageCursor(scope, cursor string, validate func(string) error) (string, error) {
	if len(cursor) > MaxKnowledgeCursorBytes {
		return "", ErrInvalidKnowledgeRequest
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", ErrInvalidKnowledgeRequest
	}
	actualScope, after, found := strings.Cut(string(decoded), "\x00")
	if !found || actualScope != scope || after == "" || strings.Contains(after, "\x00") || validate(after) != nil {
		return "", ErrInvalidKnowledgeRequest
	}
	return after, nil
}

func serveKnowledgeList(response http.ResponseWriter, request *http.Request, service *Service) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeError(response, request, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed", unavailableVersion)
		return
	}
	if service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "workbench service is unavailable", unavailableVersion)
		return
	}
	pageRequest, err := parseKnowledgePageRequest(request.URL.Query())
	if err != nil {
		writeKnowledgeError(response, request, err)
		return
	}
	page, err := service.Knowledge(request.Context(), pageRequest)
	if err != nil {
		writeKnowledgeError(response, request, err)
		return
	}
	writeSuccess(response, request, page.resourceVersion, page, MaxKnowledgeListResponseBytes)
}

func serveKnowledgeDetail(response http.ResponseWriter, request *http.Request, service *Service, id string) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeError(response, request, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed", unavailableVersion)
		return
	}
	if service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "workbench service is unavailable", unavailableVersion)
		return
	}
	if len(request.URL.Query()) != 0 {
		writeKnowledgeError(response, request, ErrInvalidKnowledgeRequest)
		return
	}
	detail, err := service.KnowledgeDetail(request.Context(), id)
	if err != nil {
		writeKnowledgeError(response, request, err)
		return
	}
	writeSuccess(response, request, detail.resourceVersion, detail, MaxKnowledgeDetailResponseBytes)
}

func serveKnowledgeProposal(response http.ResponseWriter, request *http.Request, service *Service, id string) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeError(response, request, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed", unavailableVersion)
		return
	}
	if service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "workbench service is unavailable", unavailableVersion)
		return
	}
	if len(request.URL.Query()) != 0 {
		writeKnowledgeError(response, request, ErrInvalidKnowledgeRequest)
		return
	}
	detail, err := service.KnowledgeProposal(request.Context(), id)
	if err != nil {
		writeKnowledgeError(response, request, err)
		return
	}
	writeSuccess(response, request, detail.resourceVersion, detail, MaxKnowledgeDetailResponseBytes)
}

func serveKnowledgeRoute(response http.ResponseWriter, request *http.Request, service *Service) {
	escaped := strings.TrimPrefix(request.URL.EscapedPath(), "/api/v1/knowledge/")
	if escaped == "" {
		writeError(response, request, http.StatusNotFound, "not_found", "API route was not found", unavailableVersion)
		return
	}
	proposal := false
	if strings.HasPrefix(escaped, "proposals/") {
		proposal = true
		escaped = strings.TrimPrefix(escaped, "proposals/")
	}
	id, err := url.PathUnescape(escaped)
	if err != nil || id == "" {
		writeError(response, request, http.StatusNotFound, "not_found", "API route was not found", unavailableVersion)
		return
	}
	if proposal {
		serveKnowledgeProposal(response, request, service, id)
		return
	}
	serveKnowledgeDetail(response, request, service, id)
}

func writeKnowledgeError(response http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, ErrInvalidKnowledgeRequest):
		writeError(response, request, http.StatusBadRequest, "invalid_request", "request is invalid", errorResourceVersion(err))
	case errors.Is(err, ErrKnowledgeNotFound):
		writeError(response, request, http.StatusNotFound, "knowledge_not_found", "knowledge is unavailable", unavailableVersion)
	case errors.Is(err, ErrKnowledgeTooLarge):
		writeError(response, request, http.StatusRequestEntityTooLarge, "knowledge_too_large", "knowledge response exceeds bounds", errorResourceVersion(err))
	default:
		status, code, message := projectionError(err, "knowledge")
		writeError(response, request, status, code, message, errorResourceVersion(err))
	}
}
