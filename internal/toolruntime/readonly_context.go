package toolruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/prowl-agent/prowl-agent/internal/boundedio"
	contextpkg "github.com/prowl-agent/prowl-agent/internal/context"
)

// Registered read-only tool names.
const (
	ToolSearchContext = "search_context"
	ToolGetContext    = "get_context"
	ToolReadSource    = "read_source"
)

// ErrInvalidSourcePath reports a rejected read_source path before any open.
var ErrInvalidSourcePath = errors.New("toolruntime: invalid source path")

// ContextRetriever is the canonical bounded context service (internal/context)
// shared by CLI, MCP, and the workbench. Only its read methods are needed here;
// citations, freshness, omissions, and budget bounds flow through unchanged.
type ContextRetriever interface {
	Search(contextpkg.Request) (contextpkg.Packet, error)
	Get(contextpkg.Request) (contextpkg.Packet, error)
}

// ReadOnlyConfig wires the three read-only tools to the canonical context
// service and a rooted, symlink-safe source reader.
type ReadOnlyConfig struct {
	Context             ContextRetriever
	SourceRoot          *os.Root
	MaxSourceBytes      int64
	MaxSourceLines      int
	DefaultBudgetTokens int
	ToolBounds          Bounds
}

func (cfg ReadOnlyConfig) valid() error {
	if cfg.Context == nil {
		return errors.New("toolruntime: ReadOnlyConfig requires a context retriever")
	}
	if cfg.SourceRoot == nil {
		return errors.New("toolruntime: ReadOnlyConfig requires a source root")
	}
	if cfg.MaxSourceBytes <= 0 || cfg.MaxSourceLines <= 0 {
		return errors.New("toolruntime: ReadOnlyConfig source bounds must be positive")
	}
	if !cfg.ToolBounds.valid() {
		return errors.New("toolruntime: ReadOnlyConfig tool bounds must be positive")
	}
	return nil
}

// RegisterReadOnlyContext registers exactly search_context, get_context, and
// read_source. All three are read-only and have no side effects.
func RegisterReadOnlyContext(reg *Registry, cfg ReadOnlyConfig) error {
	if err := cfg.valid(); err != nil {
		return err
	}
	tools := []Tool{
		{
			Name:        ToolSearchContext,
			Description: "Search curated knowledge and indexed source for bounded, cited project context.",
			Schema:      json.RawMessage(searchContextSchema),
			Permissions: []PermissionClass{PermissionReadOnly},
			Bounds:      cfg.ToolBounds,
			Handler:     cfg.handleSearchContext,
		},
		{
			Name:        ToolGetContext,
			Description: "Fetch specific context items by id with the same bounded, cited packet contract.",
			Schema:      json.RawMessage(getContextSchema),
			Permissions: []PermissionClass{PermissionReadOnly},
			Bounds:      cfg.ToolBounds,
			Handler:     cfg.handleGetContext,
		},
		{
			Name:        ToolReadSource,
			Description: "Read a rooted, regular-file-only source path within bounded line and byte limits.",
			Schema:      json.RawMessage(readSourceSchema),
			Permissions: []PermissionClass{PermissionReadOnly},
			Bounds:      cfg.ToolBounds,
			Handler:     cfg.handleReadSource,
		},
	}
	for _, tool := range tools {
		if err := reg.Register(tool); err != nil {
			return err
		}
	}
	return nil
}

const searchContextSchema = `{"type":"object","additionalProperties":false,"required":["question"],"properties":{"question":{"type":"string","minLength":1},"mode":{"type":"string","enum":["compact","standard","full"]},"budget_tokens":{"type":"integer","minimum":0}}}`

const getContextSchema = `{"type":"object","additionalProperties":false,"required":["ids"],"properties":{"ids":{"type":"array","items":{"type":"string"},"minItems":1},"mode":{"type":"string","enum":["compact","standard","full"]},"budget_tokens":{"type":"integer","minimum":0}}}`

const readSourceSchema = `{"type":"object","additionalProperties":false,"required":["path"],"properties":{"path":{"type":"string","minLength":1},"line_start":{"type":"integer","minimum":1},"line_end":{"type":"integer","minimum":1}}}`

type searchContextInput struct {
	Question     string `json:"question"`
	Mode         string `json:"mode,omitempty"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type getContextInput struct {
	IDs          []string `json:"ids"`
	Mode         string   `json:"mode,omitempty"`
	BudgetTokens int      `json:"budget_tokens,omitempty"`
}

type readSourceInput struct {
	Path      string `json:"path"`
	LineStart int    `json:"line_start,omitempty"`
	LineEnd   int    `json:"line_end,omitempty"`
}

func (cfg ReadOnlyConfig) handleSearchContext(_ context.Context, input json.RawMessage) (Result, error) {
	var in searchContextInput
	if err := strictDecode(input, &in); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(in.Question) == "" {
		return Result{}, fmt.Errorf("%w: question is required", ErrInvalidSourcePath)
	}
	packet, err := cfg.Context.Search(contextpkg.Request{
		Question:     in.Question,
		Mode:         cfg.mode(in.Mode),
		BudgetTokens: cfg.budget(in.BudgetTokens),
	})
	if err != nil {
		return Result{}, err
	}
	return marshalPacket(packet)
}

func (cfg ReadOnlyConfig) handleGetContext(_ context.Context, input json.RawMessage) (Result, error) {
	var in getContextInput
	if err := strictDecode(input, &in); err != nil {
		return Result{}, err
	}
	if len(in.IDs) == 0 {
		return Result{}, fmt.Errorf("%w: at least one id is required", ErrInvalidSourcePath)
	}
	packet, err := cfg.Context.Get(contextpkg.Request{
		IDs:          in.IDs,
		Mode:         cfg.mode(in.Mode),
		BudgetTokens: cfg.budget(in.BudgetTokens),
	})
	if err != nil {
		return Result{}, err
	}
	return marshalPacket(packet)
}

func (cfg ReadOnlyConfig) handleReadSource(ctx context.Context, input json.RawMessage) (Result, error) {
	var in readSourceInput
	if err := strictDecode(input, &in); err != nil {
		return Result{}, err
	}
	if err := validSourcePath(in.Path); err != nil {
		return Result{}, err
	}
	if in.LineStart < 0 || in.LineEnd < 0 || (in.LineEnd > 0 && in.LineStart > 0 && in.LineEnd < in.LineStart) {
		return Result{}, fmt.Errorf("%w: invalid line range", ErrInvalidSourcePath)
	}
	file, err := boundedio.OpenRegular(cfg.SourceRoot, filepath.ToSlash(in.Path))
	if err != nil {
		return Result{}, err
	}
	defer file.Close()
	data, err := boundedio.ReadAllContext(ctx, file, cfg.MaxSourceBytes)
	if err != nil {
		return Result{}, err
	}
	return Result{Content: sliceLines(string(data), in.LineStart, in.LineEnd, cfg.MaxSourceLines)}, nil
}

func (cfg ReadOnlyConfig) mode(value string) contextpkg.Mode {
	if value == "" {
		return contextpkg.ModeCompact
	}
	return contextpkg.Mode(value)
}

func (cfg ReadOnlyConfig) budget(value int) int {
	if value > 0 {
		return value
	}
	return cfg.DefaultBudgetTokens
}

func marshalPacket(packet contextpkg.Packet) (Result, error) {
	data, err := json.Marshal(packet)
	if err != nil {
		return Result{}, err
	}
	return Result{Content: string(data)}, nil
}

func strictDecode(input json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSourcePath, err)
	}
	if decoder.More() {
		return fmt.Errorf("%w: trailing data", ErrInvalidSourcePath)
	}
	return nil
}

// validSourcePath rejects empty, absolute, and rooted paths before any open;
// the rooted reader additionally confines traversal and symlinks to the root.
func validSourcePath(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("%w: empty path", ErrInvalidSourcePath)
	}
	if filepath.IsAbs(path) || strings.HasPrefix(path, "/") {
		return fmt.Errorf("%w: absolute path %q", ErrInvalidSourcePath, path)
	}
	return nil
}

// sliceLines returns the requested 1-based inclusive line range (or the whole
// content when no range is given), bounded to at most maxLines lines.
func sliceLines(content string, start, end, maxLines int) string {
	lines := strings.SplitAfter(content, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	total := len(lines)
	if total == 0 {
		return ""
	}
	if start <= 0 {
		start = 1
	}
	if start > total {
		return ""
	}
	if end <= 0 || end > total {
		end = total
	}
	if end-start+1 > maxLines {
		end = start + maxLines - 1
	}
	return strings.Join(lines[start-1:end], "")
}
