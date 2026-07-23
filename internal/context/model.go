// Package context compiles bounded, cited context packets shared by CLI, MCP,
// the web workbench, and editor integrations.
package context

import "fmt"

// Mode controls progressive disclosure.
type Mode string

const (
	ModeCompact  Mode = "compact"
	ModeStandard Mode = "standard"
	ModeFull     Mode = "full"
)

// Request asks the compiler for bounded project context.
type Request struct {
	Question     string            `json:"question,omitempty"`
	IDs          []string          `json:"ids"`
	Mode         Mode              `json:"mode"`
	BudgetTokens int               `json:"budget_tokens,omitempty"`
	BudgetBytes  int               `json:"budget_bytes,omitempty"`
	Filters      map[string]string `json:"filters"`
}

// Citation points to deterministic evidence.
type Citation struct {
	URI         string `json:"uri"`
	Path        string `json:"path,omitempty"`
	LineStart   int    `json:"line_start,omitempty"`
	LineEnd     int    `json:"line_end,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
}

// Item is one selected context unit.
type Item struct {
	ID              string     `json:"id"`
	Kind            string     `json:"kind"`
	Title           string     `json:"title"`
	Summary         string     `json:"summary,omitempty"`
	Content         string     `json:"content,omitempty"`
	WhySelected     []string   `json:"why_selected"`
	Freshness       string     `json:"freshness"`
	Confidence      float64    `json:"confidence"`
	Audience        []string   `json:"audience"`
	Citations       []Citation `json:"citations"`
	DetailResource  string     `json:"detail_resource"`
	EstimatedTokens int        `json:"estimated_tokens"`
}

// Budget reports requested and consumed estimated context cost.
type Budget struct {
	RequestedTokens int `json:"requested_tokens,omitempty"`
	RequestedBytes  int `json:"requested_bytes,omitempty"`
	EstimatedTokens int `json:"estimated_tokens"`
	EstimatedBytes  int `json:"estimated_bytes"`
}

// Packet is the stable context result shared by every transport.
type Packet struct {
	Question string         `json:"question,omitempty"`
	Summary  string         `json:"summary"`
	Items    []Item         `json:"items"`
	Budget   Budget         `json:"budget"`
	Omitted  map[string]int `json:"omitted"`
	Next     []string       `json:"next"`
	TraceID  string         `json:"trace_id"`
}

// Validate rejects ambiguous or unbounded requests.
func (request *Request) Validate() error {
	if request.Mode == "" {
		request.Mode = ModeCompact
	}
	switch request.Mode {
	case ModeCompact, ModeStandard, ModeFull:
	default:
		return fmt.Errorf("unknown context mode %q", request.Mode)
	}
	if request.BudgetTokens < 0 || request.BudgetBytes < 0 {
		return fmt.Errorf("context budgets cannot be negative")
	}
	if request.Mode == ModeFull && request.BudgetTokens == 0 && request.BudgetBytes == 0 {
		return fmt.Errorf("full context requires --budget-tokens or --budget-bytes")
	}
	if request.IDs == nil {
		request.IDs = []string{}
	}
	if request.Filters == nil {
		request.Filters = map[string]string{}
	}
	return nil
}

func emptyPacket(request Request) Packet {
	return Packet{
		Question: request.Question, Items: []Item{}, Omitted: map[string]int{}, Next: []string{},
		Budget: Budget{RequestedTokens: request.BudgetTokens, RequestedBytes: request.BudgetBytes},
	}
}
