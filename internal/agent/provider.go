package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Role is a provider-neutral conversation role.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// StopReason is the neutral reason a provider stopped a step.
type StopReason string

const (
	StopEndTurn   StopReason = "end_turn"
	StopToolCalls StopReason = "tool_calls"
	StopMaxTokens StopReason = "max_tokens"
)

// ToolCall is a provider-neutral request to invoke a tool. Arguments is the raw
// JSON argument payload as produced by the provider.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// Message is one neutral conversation message. ToolCalls is set on assistant
// messages that invoke tools; ToolCallID and Name identify a tool result.
type Message struct {
	Role       Role
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string
	Name       string
}

// ToolDefinition offers a tool's frozen schema to the provider.
type ToolDefinition struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

// Usage is neutral token accounting.
type Usage struct {
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
}

// CompletionRequest is the neutral request for one provider step.
type CompletionRequest struct {
	Model     string
	Messages  []Message
	Tools     []ToolDefinition
	MaxTokens int
	Stream    bool
}

// Delta is one streaming-ready increment of a provider step.
type Delta struct {
	Text     string
	ToolCall *ToolCallDelta
}

// ToolCallDelta is a streaming-ready fragment of an in-progress tool call.
type ToolCallDelta struct {
	Index         int
	ID            string
	Name          string
	ArgumentsFrag string
}

// Completion is the neutral result of one provider step.
type Completion struct {
	Text       string
	ToolCalls  []ToolCall
	Usage      Usage
	StopReason StopReason
}

// DeltaSink consumes streaming-ready deltas. It may be nil.
type DeltaSink func(Delta) error

// Provider is neutral over completion, streaming-ready deltas, tool calls,
// usage, cancellation, and typed failures. Implementations MUST honor ctx
// cancellation and MUST keep provider-specific wire conversion out of semantic
// prompt construction.
type Provider interface {
	Complete(ctx context.Context, req CompletionRequest, sink DeltaSink) (Completion, error)
}

// Typed provider failures.
var (
	// ErrProviderMalformedResponse reports an unparseable or structurally
	// invalid provider response.
	ErrProviderMalformedResponse = errors.New("agent: provider returned a malformed response")
	// ErrProviderUnavailable reports a transport or availability failure.
	ErrProviderUnavailable = errors.New("agent: provider unavailable")
	// ErrProviderExhausted reports a scripted provider with no remaining steps.
	ErrProviderExhausted = errors.New("agent: provider script exhausted")
)

// --- chat-completion-compatible wire contract -------------------------------------
//
// The wire types stay unexported: the rest of the system speaks only the
// neutral request/response types above. EncodeChatRequest and
// DecodeChatResponse are the sole boundary, so session and domain services
// never depend on provider-specific shapes.

type openAIRequest struct {
	Model     string          `json:"model"`
	Messages  []openAIMessage `json:"messages"`
	Tools     []openAITool    `json:"tools,omitempty"`
	MaxTokens int             `json:"max_tokens,omitempty"`
	Stream    bool            `json:"stream"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    *string          `json:"content"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAITool struct {
	Type     string             `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type openAIResponse struct {
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
}

type openAIChoice struct {
	FinishReason string            `json:"finish_reason"`
	Message      openAIRespMessage `json:"message"`
}

type openAIRespMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []openAIToolCall `json:"tool_calls"`
}

type openAIUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

// EncodeChatRequest converts a neutral request to canonical chat-completion-compatible
// chat-completion wire bytes.
func EncodeChatRequest(req CompletionRequest) ([]byte, error) {
	wire := openAIRequest{Model: req.Model, MaxTokens: req.MaxTokens, Stream: req.Stream}
	wire.Messages = make([]openAIMessage, 0, len(req.Messages))
	for _, message := range req.Messages {
		converted := openAIMessage{Role: string(message.Role), ToolCallID: message.ToolCallID, Name: message.Name}
		if message.Role == RoleAssistant && message.Content == "" && len(message.ToolCalls) > 0 {
			converted.Content = nil
		} else {
			content := message.Content
			converted.Content = &content
		}
		for _, call := range message.ToolCalls {
			converted.ToolCalls = append(converted.ToolCalls, openAIToolCall{
				ID:       call.ID,
				Type:     "function",
				Function: openAIFunctionCall{Name: call.Name, Arguments: call.Arguments},
			})
		}
		wire.Messages = append(wire.Messages, converted)
	}
	for _, tool := range req.Tools {
		wire.Tools = append(wire.Tools, openAITool{
			Type:     "function",
			Function: openAIToolFunction{Name: tool.Name, Description: tool.Description, Parameters: tool.Schema},
		})
	}
	return json.Marshal(wire)
}

// DecodeChatResponse parses chat-completion-compatible chat-completion response bytes
// into a neutral completion. It is lenient about unknown fields but fails
// closed on structurally invalid responses.
func DecodeChatResponse(data []byte) (Completion, error) {
	var wire openAIResponse
	if err := json.Unmarshal(data, &wire); err != nil {
		return Completion{}, fmt.Errorf("%w: %v", ErrProviderMalformedResponse, err)
	}
	if len(wire.Choices) != 1 {
		return Completion{}, fmt.Errorf("%w: expected exactly one choice, got %d", ErrProviderMalformedResponse, len(wire.Choices))
	}
	choice := wire.Choices[0]
	completion := Completion{
		Text:       choice.Message.Content,
		Usage:      Usage{InputTokens: wire.Usage.PromptTokens, OutputTokens: wire.Usage.CompletionTokens, TotalTokens: wire.Usage.TotalTokens},
		StopReason: mapFinishReason(choice.FinishReason),
	}
	for _, call := range choice.Message.ToolCalls {
		if call.Type != "function" || call.Function.Name == "" {
			return Completion{}, fmt.Errorf("%w: invalid tool call", ErrProviderMalformedResponse)
		}
		if !json.Valid([]byte(call.Function.Arguments)) {
			return Completion{}, fmt.Errorf("%w: tool arguments are not valid JSON", ErrProviderMalformedResponse)
		}
		completion.ToolCalls = append(completion.ToolCalls, ToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
	}
	return completion, nil
}

func mapFinishReason(reason string) StopReason {
	switch reason {
	case "tool_calls":
		return StopToolCalls
	case "length":
		return StopMaxTokens
	default:
		return StopEndTurn
	}
}
