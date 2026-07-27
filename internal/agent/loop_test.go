package agent_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/prowl-agent/prowl-agent/internal/agent"
	"github.com/prowl-agent/prowl-agent/internal/operations"
	"github.com/prowl-agent/prowl-agent/internal/profile"
	"github.com/prowl-agent/prowl-agent/internal/session"
	"github.com/prowl-agent/prowl-agent/internal/toolruntime"
)

// --- adapters bridging the loop ports to the real B0.3/B0.4 services ---

type registryExecutor struct {
	reg    *toolruntime.Registry
	pinned toolruntime.Pinned
	grant  toolruntime.Grant
}

func (e *registryExecutor) ExecuteTool(ctx context.Context, call agent.ToolInvocation) (agent.ToolOutcome, error) {
	res, err := e.reg.Execute(ctx, e.pinned, e.grant, toolruntime.Call{Name: call.Name, Input: json.RawMessage(call.Arguments)})
	if err != nil {
		return agent.ToolOutcome{}, err
	}
	return agent.ToolOutcome{Content: res.Content, IsError: res.IsError}, nil
}

type sessionRecorder struct {
	svc       session.Service
	sessionID string
}

func (r *sessionRecorder) RecordTurn(ctx context.Context, turn agent.RecordedTurn) (agent.RecordedTurnResult, error) {
	entries := make([]session.TurnEntryInput, 0, len(turn.Entries))
	for _, entry := range turn.Entries {
		entries = append(entries, session.TurnEntryInput{
			Kind: mapKind(entry.Kind),
			Body: entry.Body,
			Metadata: session.EntryMetadata{
				Role:       entry.Role,
				ToolName:   entry.ToolName,
				ToolCallID: entry.ToolCallID,
			},
		})
	}
	view, err := r.svc.AppendTurn(ctx, session.AppendTurnRequest{
		SessionID:       r.sessionID,
		IdempotencyKey:  turn.IdempotencyKey,
		ExpectedVersion: turn.ExpectedVersion,
		RunID:           turn.RunID,
		Status:          mapStatus(turn.Outcome),
		Entries:         entries,
		Usage:           session.Usage{InputTokens: turn.Usage.InputTokens, OutputTokens: turn.Usage.OutputTokens, TotalTokens: turn.Usage.TotalTokens},
	})
	if err != nil {
		return agent.RecordedTurnResult{}, err
	}
	return agent.RecordedTurnResult{TurnID: view.ID, ResultingVersion: view.ResultingVersion}, nil
}

func mapKind(kind agent.RecordedEntryKind) session.EntryKind {
	switch kind {
	case agent.RecordedToolCall:
		return session.EntryToolCall
	case agent.RecordedToolResult:
		return session.EntryToolResult
	default:
		return session.EntryMessage
	}
}

func mapStatus(outcome agent.TurnOutcome) session.TurnStatus {
	switch outcome {
	case agent.TurnFailed:
		return session.TurnFailed
	case agent.TurnCancelled:
		return session.TurnCancelled
	default:
		return session.TurnSucceeded
	}
}

// --- harness: real operations DB + session service + registry executor ---

type harness struct {
	svc       session.Service
	sessionID string
	version   int64
	executor  *registryExecutor
	recorder  *sessionRecorder
	tools     []agent.ToolDefinition
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	store, err := operations.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := session.NewService(store, operations.SurfaceCLI)

	snapshot := buildSnapshot(t)
	manifest, err := agent.NewExposureManifest(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreateSession(context.Background(), session.CreateSessionRequest{
		SnapshotBytes: snapshot.CanonicalBytes(),
		ExposureBytes: manifest.CanonicalBytes(),
	})
	if err != nil {
		t.Fatal(err)
	}

	reg := toolruntime.NewRegistry()
	tool := toolruntime.Tool{
		Name:        "read_source",
		Description: "deterministic read-only tool",
		Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		Permissions: []toolruntime.PermissionClass{toolruntime.PermissionReadOnly},
		Bounds:      toolruntime.Bounds{MaxInputBytes: 4096, MaxOutputBytes: 4096},
		Handler: func(_ context.Context, input json.RawMessage) (toolruntime.Result, error) {
			return toolruntime.Result{Content: "read:" + string(input)}, nil
		},
	}
	if err := reg.Register(tool); err != nil {
		t.Fatal(err)
	}
	tools := make([]agent.ToolDefinition, 0)
	for _, def := range reg.Definitions() {
		tools = append(tools, agent.ToolDefinition{Name: def.Name, Description: def.Description, Schema: def.Schema})
	}
	return &harness{
		svc:       svc,
		sessionID: created.ID,
		version:   created.Version,
		executor:  &registryExecutor{reg: reg, pinned: reg.PinAll(), grant: toolruntime.ReadOnlyGrant()},
		recorder:  &sessionRecorder{svc: svc, sessionID: created.ID},
		tools:     tools,
	}
}

func (h *harness) config(provider agent.Provider) agent.LoopConfig {
	return agent.LoopConfig{
		Provider:        provider,
		Executor:        h.executor,
		Recorder:        h.recorder,
		Model:           "model-a",
		MaxTokens:       1024,
		Tools:           h.tools,
		Bounds:          agent.LoopBounds{MaxSteps: 8, MaxToolCalls: 8, MaxBodyBytes: 65536, MaxResultBytes: 65536},
		IdempotencyKey:  "turn-1",
		ExpectedVersion: h.version,
		RunID:           "run-1",
	}
}

func (h *harness) turn(t *testing.T) session.TurnView {
	t.Helper()
	view, err := h.svc.GetSession(context.Background(), session.GetSessionRequest{SessionID: h.sessionID})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Turns) != 1 {
		t.Fatalf("want exactly one recorded turn, got %d", len(view.Turns))
	}
	return view.Turns[0]
}

func buildSnapshot(t *testing.T) *profile.Snapshot {
	t.Helper()
	hash := func(body string) string {
		digest := sha256.Sum256([]byte(body))
		return hex.EncodeToString(digest[:])
	}
	snapshot, err := profile.NewSnapshot(profile.SnapshotInput{
		Provider:             profile.ProviderModel{ProviderID: "fake", ModelID: "model-a", MaxInputTokens: 8192, MaxOutputTokens: 1024},
		CoreVersion:          profile.CoreProwlV1,
		PrincipalID:          "principal-local",
		Profile:              profile.Local(),
		Policy:               profile.PolicyInput{Permission: "read-only", Approval: "explicit", Readiness: "ready"},
		ToolSchemaGeneration: "tools-1",
		Sources: []profile.SourceInput{
			{ID: "task:brief", Kind: profile.TaskInstructionSource, Body: "Trace one turn.", Provenance: profile.TaskProvenance, Scope: profile.TaskScope, Included: true, Reason: "assigned"},
			{ID: "system-security", Kind: profile.SystemPolicySource, Body: "Never reveal secrets.", Provenance: profile.BuiltinProvenance, Scope: profile.GlobalScope, Included: true, Reason: "required"},
			{ID: "user-profile:concise", Kind: profile.UserProfileSource, Body: "Prefer concise answers.", Provenance: profile.UserSelectedProvenance, Scope: profile.UserScope, Included: true, Reason: "selected"},
		},
		Tools: []profile.ToolSchemaInput{
			{ID: "read_source", Schema: []byte(`{"type":"object","properties":{"path":{"type":"string"}}}`)},
		},
		Skills: []profile.SkillMetadataInput{
			{ID: "skill:test", Name: "test-driven-development", Description: "Write a failing test first.", ContentHash: hash("RED then GREEN."), Root: "profile/skills", ForcePreload: true},
		},
		PreloadedSkills: []profile.SkillBodyInput{{ID: "skill:test", Body: "RED then GREEN."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func countKind(turn session.TurnView, kind session.EntryKind) int {
	n := 0
	for _, entry := range turn.Entries {
		if entry.Kind == kind {
			n++
		}
	}
	return n
}

// --- loop scenarios exercised through the scripted fake and real services ---

func TestFakeProviderTextTurn(t *testing.T) {
	h := newHarness(t)
	provider := &scriptedProvider{steps: []scriptStep{
		{
			deltas:     []agent.Delta{{Text: "final "}, {Text: "answer"}},
			completion: agent.Completion{Text: "final answer", StopReason: agent.StopEndTurn, Usage: agent.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}},
		},
	}}
	var streamed strings.Builder
	cfg := h.config(provider)
	cfg.DeltaSink = func(delta agent.Delta) error { streamed.WriteString(delta.Text); return nil }

	result, err := agent.Run(context.Background(), cfg, "trace this")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Text != "final answer" || result.Outcome != agent.TurnSucceeded || result.Steps != 1 || result.ToolCalls != 0 {
		t.Fatalf("result = %+v", result)
	}
	if streamed.String() != "final answer" {
		t.Fatalf("streamed deltas did not flow through the loop: %q", streamed.String())
	}
	turn := h.turn(t)
	if turn.Status != session.TurnSucceeded || turn.ResultingVersion != 2 {
		t.Fatalf("turn = status %q version %d", turn.Status, turn.ResultingVersion)
	}
	if turn.Usage.TotalTokens != 15 {
		t.Fatalf("usage not recorded: %+v", turn.Usage)
	}
	if countKind(turn, session.EntryMessage) != 2 {
		t.Fatalf("want user+assistant messages, entries = %+v", turn.Entries)
	}
}

func TestFakeProviderToolCallContinuation(t *testing.T) {
	h := newHarness(t)
	provider := &scriptedProvider{steps: []scriptStep{
		{completion: agent.Completion{ToolCalls: []agent.ToolCall{{ID: "c1", Name: "read_source", Arguments: `{"path":"a"}`}}, StopReason: agent.StopToolCalls, Usage: agent.Usage{InputTokens: 5, TotalTokens: 5}}},
		{completion: agent.Completion{Text: "done", StopReason: agent.StopEndTurn, Usage: agent.Usage{InputTokens: 6, OutputTokens: 3, TotalTokens: 9}}},
	}}
	result, err := agent.Run(context.Background(), h.config(provider), "trace this")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Steps != 2 || result.ToolCalls != 1 || result.Text != "done" {
		t.Fatalf("result = %+v", result)
	}
	if result.Usage.TotalTokens != 14 {
		t.Fatalf("usage aggregate = %+v", result.Usage)
	}
	// Continuation: the second provider request must carry the tool result.
	if len(provider.requests) != 2 {
		t.Fatalf("provider calls = %d", len(provider.requests))
	}
	sawToolResult := false
	for _, msg := range provider.requests[1].Messages {
		if msg.Role == agent.RoleTool && strings.Contains(msg.Content, "read:") {
			sawToolResult = true
		}
	}
	if !sawToolResult {
		t.Fatalf("continuation did not feed the tool result back: %+v", provider.requests[1].Messages)
	}
	turn := h.turn(t)
	if countKind(turn, session.EntryToolCall) != 1 || countKind(turn, session.EntryToolResult) != 1 {
		t.Fatalf("trajectory not recorded: %+v", turn.Entries)
	}
}

func TestFakeProviderCancellation(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	provider := &scriptedProvider{steps: []scriptStep{{cancel: cancel, block: true}}}

	result, err := agent.Run(ctx, h.config(provider), "trace this")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if result.Outcome != agent.TurnCancelled {
		t.Fatalf("outcome = %v", result.Outcome)
	}
	// The partial trajectory is still recorded transactionally as cancelled.
	turn := h.turn(t)
	if turn.Status != session.TurnCancelled {
		t.Fatalf("cancelled turn not recorded: %q", turn.Status)
	}
}

func TestFakeProviderMalformedResponse(t *testing.T) {
	h := newHarness(t)
	provider := &scriptedProvider{steps: []scriptStep{{err: agent.ErrProviderMalformedResponse}}}
	result, err := agent.Run(context.Background(), h.config(provider), "trace this")
	if !errors.Is(err, agent.ErrProviderMalformedResponse) {
		t.Fatalf("expected malformed error, got %v", err)
	}
	if result.Outcome != agent.TurnFailed {
		t.Fatalf("outcome = %v", result.Outcome)
	}
	if h.turn(t).Status != session.TurnFailed {
		t.Fatalf("failed turn not recorded")
	}
}

func TestFakeProviderBoundedSteps(t *testing.T) {
	h := newHarness(t)
	loop := scriptStep{completion: agent.Completion{ToolCalls: []agent.ToolCall{{ID: "c", Name: "read_source", Arguments: `{"path":"a"}`}}, StopReason: agent.StopToolCalls}}
	provider := &scriptedProvider{steps: []scriptStep{loop, loop, loop, loop, loop}}
	cfg := h.config(provider)
	cfg.Bounds.MaxSteps = 3
	result, err := agent.Run(context.Background(), cfg, "trace this")
	if !errors.Is(err, agent.ErrLoopStepBudget) {
		t.Fatalf("expected step budget, got %v", err)
	}
	if result.Steps != 3 {
		t.Fatalf("steps = %d", result.Steps)
	}
	if h.turn(t).Status != session.TurnFailed {
		t.Fatalf("bounded turn not recorded as failed")
	}
}

func TestFakeProviderBoundedToolCalls(t *testing.T) {
	h := newHarness(t)
	step := scriptStep{completion: agent.Completion{ToolCalls: []agent.ToolCall{
		{ID: "c1", Name: "read_source", Arguments: `{"path":"a"}`},
		{ID: "c2", Name: "read_source", Arguments: `{"path":"b"}`},
		{ID: "c3", Name: "read_source", Arguments: `{"path":"c"}`},
	}, StopReason: agent.StopToolCalls}}
	provider := &scriptedProvider{steps: []scriptStep{step}}
	cfg := h.config(provider)
	cfg.Bounds.MaxToolCalls = 2
	_, err := agent.Run(context.Background(), cfg, "trace this")
	if !errors.Is(err, agent.ErrLoopToolBudget) {
		t.Fatalf("expected tool budget, got %v", err)
	}
}

func TestFakeProviderInvalidConfig(t *testing.T) {
	_, err := agent.Run(context.Background(), agent.LoopConfig{}, "x")
	if !errors.Is(err, agent.ErrLoopConfig) {
		t.Fatalf("expected config error, got %v", err)
	}
}
