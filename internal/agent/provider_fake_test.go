package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/agent"
)

// scriptStep is one scripted provider turn. Exactly one of completion/err is
// returned after emitting any deltas. cancel (if set) is invoked before
// returning; block waits for context cancellation to exercise propagation.
type scriptStep struct {
	completion agent.Completion
	err        error
	deltas     []agent.Delta
	cancel     func()
	block      bool
}

// scriptedProvider is a deterministic fake Provider. It returns the next
// scripted step per Complete call, honors context cancellation, records every
// request it receives, and fails closed once the script is exhausted.
type scriptedProvider struct {
	steps    []scriptStep
	calls    int
	requests []agent.CompletionRequest
}

func (p *scriptedProvider) Complete(ctx context.Context, req agent.CompletionRequest, sink agent.DeltaSink) (agent.Completion, error) {
	p.requests = append(p.requests, req)
	if p.calls >= len(p.steps) {
		return agent.Completion{}, agent.ErrProviderExhausted
	}
	step := p.steps[p.calls]
	p.calls++
	if step.cancel != nil {
		step.cancel()
	}
	if step.block {
		<-ctx.Done()
		return agent.Completion{}, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return agent.Completion{}, err
	}
	for _, delta := range step.deltas {
		if sink != nil {
			if err := sink(delta); err != nil {
				return agent.Completion{}, err
			}
		}
	}
	return step.completion, step.err
}

func TestFakeProviderDeterministicScript(t *testing.T) {
	build := func() *scriptedProvider {
		return &scriptedProvider{steps: []scriptStep{
			{completion: agent.Completion{Text: "hello", StopReason: agent.StopEndTurn, Usage: agent.Usage{InputTokens: 3, OutputTokens: 1, TotalTokens: 4}}},
		}}
	}
	ctx := context.Background()
	req := agent.CompletionRequest{Model: "model-a"}
	first, err := build().Complete(ctx, req, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := build().Complete(ctx, req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("nondeterministic: %+v vs %+v", first, second)
	}
	provider := build()
	if _, err := provider.Complete(ctx, req, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Complete(ctx, req, nil); !errors.Is(err, agent.ErrProviderExhausted) {
		t.Fatalf("exhaustion = %v", err)
	}
}

func TestChatWireFixtureRequest(t *testing.T) {
	req := agent.CompletionRequest{
		Model: "model-a",
		Messages: []agent.Message{
			{Role: agent.RoleSystem, Content: "You are Prowl."},
			{Role: agent.RoleUser, Content: "find auth"},
			{Role: agent.RoleAssistant, ToolCalls: []agent.ToolCall{{ID: "call_1", Name: "search_context", Arguments: `{"question":"auth"}`}}},
			{Role: agent.RoleTool, ToolCallID: "call_1", Name: "search_context", Content: "1 result"},
		},
		Tools: []agent.ToolDefinition{
			{Name: "search_context", Description: "Search project context.", Schema: json.RawMessage(`{"type":"object","properties":{"question":{"type":"string"}},"required":["question"]}`)},
		},
		MaxTokens: 1024,
	}
	// Hand-authored expected wire bytes: an independent fixture, not computed
	// through the production encoder.
	want := []byte(`{"model":"model-a","messages":[{"role":"system","content":"You are Prowl."},{"role":"user","content":"find auth"},{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"search_context","arguments":"{\"question\":\"auth\"}"}}]},{"role":"tool","content":"1 result","tool_call_id":"call_1","name":"search_context"}],"tools":[{"type":"function","function":{"name":"search_context","description":"Search project context.","parameters":{"type":"object","properties":{"question":{"type":"string"}},"required":["question"]}}}],"max_tokens":1024,"stream":false}`)
	got, err := agent.EncodeChatRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("wire request mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestChatWireFixtureResponse(t *testing.T) {
	toolResponse := []byte(`{"id":"chatcmpl-1","object":"chat.completion","model":"model-a","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"search_context","arguments":"{\"question\":\"auth\"}"}}]}}],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}`)
	comp, err := agent.DecodeChatResponse(toolResponse)
	if err != nil {
		t.Fatal(err)
	}
	if comp.StopReason != agent.StopToolCalls {
		t.Fatalf("stop reason = %v", comp.StopReason)
	}
	if len(comp.ToolCalls) != 1 || comp.ToolCalls[0].ID != "call_1" || comp.ToolCalls[0].Name != "search_context" || comp.ToolCalls[0].Arguments != `{"question":"auth"}` {
		t.Fatalf("tool calls = %+v", comp.ToolCalls)
	}
	if comp.Usage != (agent.Usage{InputTokens: 11, OutputTokens: 7, TotalTokens: 18}) {
		t.Fatalf("usage = %+v", comp.Usage)
	}

	textResponse := []byte(`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"hello there"}}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)
	comp2, err := agent.DecodeChatResponse(textResponse)
	if err != nil {
		t.Fatal(err)
	}
	if comp2.Text != "hello there" || comp2.StopReason != agent.StopEndTurn || len(comp2.ToolCalls) != 0 {
		t.Fatalf("text completion = %+v", comp2)
	}

	malformed := map[string]string{
		"invalid json":  `{not json`,
		"no choices":    `{"choices":[]}`,
		"bad tool args": `{"choices":[{"finish_reason":"tool_calls","message":{"tool_calls":[{"id":"c","type":"function","function":{"name":"x","arguments":"{bad"}}]}}]}`,
	}
	for name, body := range malformed {
		if _, err := agent.DecodeChatResponse([]byte(body)); !errors.Is(err, agent.ErrProviderMalformedResponse) {
			t.Fatalf("%s: expected malformed, got %v", name, err)
		}
	}
}
