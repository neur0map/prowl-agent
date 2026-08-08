package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prowl-agent/prowl-agent/internal/capability"
	contextpacket "github.com/prowl-agent/prowl-agent/internal/context"
	"github.com/prowl-agent/prowl-agent/internal/knowledge"
	"github.com/prowl-agent/prowl-agent/internal/query"
)

var falseHint = false

func readOnlyAnnotations(title string) *sdk.ToolAnnotations {
	return &sdk.ToolAnnotations{Title: title, ReadOnlyHint: true, OpenWorldHint: &falseHint}
}

func registerCoreTools(server *sdk.Server, h *handlers) {
	sdk.AddTool(server, &sdk.Tool{Name: "search_context", Description: "Build a bounded, cited context packet for a project question. Ranks by full text with no model. For a relevance-sensitive question, set rerank=true to have your own model reorder the candidates (one extra model call, no local model needed).", Annotations: readOnlyAnnotations("Search context")}, tracked(h, h.searchContext))
	sdk.AddTool(server, &sdk.Tool{Name: "get_context", Description: "Fetch selected context IDs with progressive detail and an explicit budget.", Annotations: readOnlyAnnotations("Get context")}, tracked(h, h.getContext))
	sdk.AddTool(server, &sdk.Tool{Name: "analyze_change", Description: "Analyze the structural blast radius of a project-relative file.", Annotations: readOnlyAnnotations("Analyze change")}, tracked(h, h.analyzeChange))
	sdk.AddTool(server, &sdk.Tool{Name: "propose_knowledge_change", Description: "Create a reviewable OKF proposal. This never accepts durable knowledge.", Annotations: &sdk.ToolAnnotations{Title: "Propose knowledge change", ReadOnlyHint: false, DestructiveHint: &falseHint, OpenWorldHint: &falseHint}}, tracked(h, h.proposeKnowledge))
	sdk.AddTool(server, &sdk.Tool{Name: "validate_knowledge", Description: "Validate durable project knowledge, evidence anchors, and links.", Annotations: readOnlyAnnotations("Validate knowledge")}, tracked(h, h.validateKnowledge))
	sdk.AddTool(server, &sdk.Tool{Name: "search_capabilities", Description: "Discover token-lean Prowl workflow metadata before fetching details.", Annotations: readOnlyAnnotations("Search capabilities")}, tracked(h, h.searchCapabilities))
}

type contextSearchIn struct {
	Query        string `json:"query" jsonschema:"project question"`
	Mode         string `json:"mode,omitempty" jsonschema:"compact, standard, or full"`
	BudgetTokens int    `json:"budget_tokens,omitempty" jsonschema:"estimated token budget"`
	BudgetBytes  int    `json:"budget_bytes,omitempty" jsonschema:"optional byte budget"`
	Synthesize   bool   `json:"synthesize,omitempty" jsonschema:"request optional client-side semantic synthesis"`
	Rerank       bool   `json:"rerank,omitempty" jsonschema:"reorder results with your own model for better relevance; needs no local model and is ignored when the client does not support sampling"`
}

type contextGetIn struct {
	IDs          []string `json:"ids" jsonschema:"context item IDs"`
	Mode         string   `json:"mode,omitempty" jsonschema:"compact, standard, or full"`
	BudgetTokens int      `json:"budget_tokens,omitempty"`
	BudgetBytes  int      `json:"budget_bytes,omitempty"`
}

type proposalIn struct {
	Target    string `json:"target" jsonschema:"bundle-relative destination path"`
	Candidate string `json:"candidate" jsonschema:"complete OKF Markdown candidate"`
	Author    string `json:"author,omitempty"`
}

type capabilitySearchIn struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

type proposalOut struct {
	Proposal *knowledge.Proposal `json:"proposal"`
	Diff     string              `json:"diff"`
}

type findingsOut struct {
	Findings []knowledge.Finding `json:"findings"`
}

type capabilitiesOut struct {
	Capabilities []capability.Summary `json:"capabilities"`
}

func (h *handlers) searchContext(ctx context.Context, request *sdk.CallToolRequest, in contextSearchIn) (*sdk.CallToolResult, contextpacket.Packet, error) {
	if h.context == nil {
		return nil, contextpacket.Packet{}, fmt.Errorf("context service unavailable")
	}
	mode := contextpacket.Mode(in.Mode)
	if mode == "" {
		mode = contextpacket.ModeCompact
	}
	if in.BudgetTokens == 0 && in.BudgetBytes == 0 && mode != contextpacket.ModeFull {
		in.BudgetTokens = 1800
	}
	searchRequest := contextpacket.Request{Question: in.Query, Mode: mode, BudgetTokens: in.BudgetTokens, BudgetBytes: in.BudgetBytes}
	if in.Rerank && request != nil && sessionSupportsSampling(request.Session) {
		searchRequest.Reranker = samplingReranker{ctx: ctx, session: request.Session}
	}
	packet, err := h.context.Search(searchRequest)
	if err == nil && in.Synthesize {
		packet = synthesizePacket(ctx, request, packet)
	}
	packet = contextpacket.CanonicalProjection(packet)
	return packetResourceLinks(packet), packet, err
}

func synthesizePacket(ctx context.Context, request *sdk.CallToolRequest, packet contextpacket.Packet) contextpacket.Packet {
	if request == nil || request.Session == nil {
		return packet
	}
	initialized := request.Session.InitializeParams()
	if initialized == nil || initialized.Capabilities == nil || initialized.Capabilities.Sampling == nil {
		return packet
	}
	type compactItem struct {
		Title     string                   `json:"title"`
		Summary   string                   `json:"summary"`
		Freshness string                   `json:"freshness"`
		Citations []contextpacket.Citation `json:"citations"`
	}
	items := make([]compactItem, 0, min(len(packet.Items), 12))
	for _, item := range packet.Items {
		if len(items) == 12 {
			break
		}
		items = append(items, compactItem{Title: boundedText(item.Title, 160), Summary: boundedText(item.Summary, 320), Freshness: item.Freshness, Citations: item.Citations})
	}
	payload, _ := json.Marshal(map[string]any{"deterministic_summary": boundedText(packet.Summary, 500), "items": items})
	result, err := request.Session.CreateMessage(ctx, &sdk.CreateMessageParams{
		MaxTokens:    160,
		SystemPrompt: "Summarize only the supplied local evidence in at most three sentences. Preserve uncertainty and freshness warnings. Do not add facts.",
		Messages:     []*sdk.SamplingMessage{{Role: sdk.Role("user"), Content: &sdk.TextContent{Text: string(payload)}}},
		Temperature:  0.1,
		ModelPreferences: &sdk.ModelPreferences{
			CostPriority: 0.7, SpeedPriority: 0.7, IntelligencePriority: 0.4,
		},
	})
	if err != nil {
		return packet
	}
	text, ok := result.Content.(*sdk.TextContent)
	if !ok || strings.TrimSpace(text.Text) == "" {
		return packet
	}
	packet.Summary = boundedText(strings.TrimSpace(text.Text), 1200)
	return packet
}

func boundedText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

func (h *handlers) getContext(_ context.Context, _ *sdk.CallToolRequest, in contextGetIn) (*sdk.CallToolResult, contextpacket.Packet, error) {
	if h.context == nil {
		return nil, contextpacket.Packet{}, fmt.Errorf("context service unavailable")
	}
	mode := contextpacket.Mode(in.Mode)
	if mode == "" {
		mode = contextpacket.ModeCompact
	}
	if in.BudgetTokens == 0 && in.BudgetBytes == 0 && mode != contextpacket.ModeFull {
		in.BudgetTokens = 1800
	}
	packet, err := h.context.Get(contextpacket.Request{IDs: in.IDs, Mode: mode, BudgetTokens: in.BudgetTokens, BudgetBytes: in.BudgetBytes})
	packet = contextpacket.CanonicalProjection(packet)
	return packetResourceLinks(packet), packet, err
}

func (h *handlers) analyzeChange(_ context.Context, _ *sdk.CallToolRequest, in pathIn) (*sdk.CallToolResult, query.BlastSummary, error) {
	if h.q == nil {
		return nil, query.BlastSummary{}, fmt.Errorf("query service unavailable")
	}
	result, err := h.q.BlastSummarize(in.Path)
	return resourceLinkResult(sourceLink(in.Path)), result, err
}

func (h *handlers) proposeKnowledge(ctx context.Context, request *sdk.CallToolRequest, in proposalIn) (*sdk.CallToolResult, proposalOut, error) {
	if h.knowledge == nil {
		return nil, proposalOut{}, fmt.Errorf("knowledge repository unavailable")
	}
	if int64(len([]byte(in.Candidate))) > knowledge.MaxDocumentBytes {
		return nil, proposalOut{}, fmt.Errorf("knowledge candidate exceeds %d bytes", knowledge.MaxDocumentBytes)
	}
	if err := approveProposal(ctx, request, in.Target); err != nil {
		return nil, proposalOut{}, err
	}
	staged, err := os.CreateTemp(filepath.Dir(h.knowledge.Root), ".prowl-proposal-*.md")
	if err != nil {
		return nil, proposalOut{}, err
	}
	stagedPath := staged.Name()
	defer os.Remove(stagedPath)
	if _, err := staged.WriteString(in.Candidate); err != nil {
		staged.Close()
		return nil, proposalOut{}, err
	}
	if err := staged.Close(); err != nil {
		return nil, proposalOut{}, err
	}
	inbox := knowledge.NewReviewInbox(filepath.Join(filepath.Dir(h.knowledge.Root), "proposals"), h.knowledge)
	proposal, diff, err := inbox.Propose(stagedPath, in.Target, in.Author, time.Now())
	return resourceLinkResult(knowledgeIndexLink()), proposalOut{Proposal: proposal, Diff: diff}, err
}

func approveProposal(ctx context.Context, request *sdk.CallToolRequest, target string) error {
	if request != nil && request.Session != nil {
		initialized := request.Session.InitializeParams()
		if initialized != nil && initialized.Capabilities != nil && initialized.Capabilities.Elicitation != nil {
			result, err := request.Session.Elicit(ctx, &sdk.ElicitParams{
				Mode:    "form",
				Message: fmt.Sprintf("Create a review-only knowledge proposal for %q? This will not accept it.", target),
				RequestedSchema: map[string]any{
					"type":       "object",
					"properties": map[string]any{"confirm": map[string]any{"type": "boolean", "description": "Confirm proposal creation"}},
					"required":   []string{"confirm"},
				},
			})
			if err != nil {
				return fmt.Errorf("proposal elicitation failed: %w", err)
			}
			confirmed, _ := result.Content["confirm"].(bool)
			if result.Action != "accept" || !confirmed {
				return fmt.Errorf("proposal creation was not approved")
			}
			return nil
		}
	}
	return fmt.Errorf("client does not support host-approved elicitation; use `prowl-agent knowledge propose` locally")
}

func (h *handlers) validateKnowledge(_ context.Context, _ *sdk.CallToolRequest, _ Empty) (*sdk.CallToolResult, findingsOut, error) {
	if h.knowledge == nil {
		return nil, findingsOut{Findings: []knowledge.Finding{}}, fmt.Errorf("knowledge repository unavailable")
	}
	findings, err := h.knowledge.Lint(h.root)
	return resourceLinkResult(knowledgeIndexLink()), findingsOut{Findings: findings}, err
}

func (h *handlers) searchCapabilities(_ context.Context, _ *sdk.CallToolRequest, in capabilitySearchIn) (*sdk.CallToolResult, capabilitiesOut, error) {
	if h.capabilities == nil {
		return nil, capabilitiesOut{Capabilities: []capability.Summary{}}, fmt.Errorf("capability catalog unavailable")
	}
	return resourceLinkResult(overviewLink()), capabilitiesOut{Capabilities: h.capabilities.Search(in.Query, in.Limit)}, nil
}

func packetResourceLinks(packet contextpacket.Packet) *sdk.CallToolResult {
	links := make([]*sdk.ResourceLink, 0, len(packet.Items))
	seen := map[string]bool{}
	for _, item := range packet.Items {
		if item.DetailResource == "" || seen[item.DetailResource] {
			continue
		}
		seen[item.DetailResource] = true
		links = append(links, &sdk.ResourceLink{URI: item.DetailResource, Name: item.ID, Title: item.Title, Description: item.Summary})
	}
	if len(links) == 0 {
		links = append(links, overviewLink())
	}
	return resourceLinkResult(links...)
}

func resourceLinkResult(links ...*sdk.ResourceLink) *sdk.CallToolResult {
	content := make([]sdk.Content, 0, len(links))
	for _, link := range links {
		if link != nil {
			content = append(content, link)
		}
	}
	return &sdk.CallToolResult{Content: content}
}

func sourceLink(path string) *sdk.ResourceLink {
	clean := filepath.ToSlash(path)
	return &sdk.ResourceLink{URI: "prowl://workspace/current/source/" + url.PathEscape(clean), Name: clean, Title: "Changed source"}
}

func knowledgeIndexLink() *sdk.ResourceLink {
	return &sdk.ResourceLink{URI: "prowl://workspace/current/knowledge/index", Name: "knowledge-index", Title: "Knowledge index", MIMEType: "text/markdown"}
}

func overviewLink() *sdk.ResourceLink {
	return &sdk.ResourceLink{URI: "prowl://workspace/current/overview", Name: "workspace-overview", Title: "Workspace overview", MIMEType: "application/json"}
}
