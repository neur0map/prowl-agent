package agent

import (
	"context"
	"errors"
	"fmt"
)

// LoopBounds caps a single agent-loop run so it cannot exhaust resources or the
// durable ledger. Every field must be positive.
type LoopBounds struct {
	MaxSteps       int // maximum provider completion steps
	MaxToolCalls   int // maximum tool invocations across the whole run
	MaxBodyBytes   int // maximum recorded bytes per message/tool-call body
	MaxResultBytes int // maximum recorded bytes per tool result
}

func (b LoopBounds) valid() bool {
	return b.MaxSteps > 0 && b.MaxToolCalls > 0 && b.MaxBodyBytes > 0 && b.MaxResultBytes > 0
}

// ToolInvocation is a resolved tool call the loop asks the executor to run.
type ToolInvocation struct {
	CallID    string
	Name      string
	Arguments string
}

// ToolOutcome is the bounded result of an executed tool.
type ToolOutcome struct {
	Content string
	IsError bool
}

// ToolExecutor runs a permitted, pinned tool call. Implementations MUST enforce
// deny-by-default permission and pinned-schema resolution before any handler
// side effect. The loop never imports the tool registry directly; it depends
// only on this port.
type ToolExecutor interface {
	ExecuteTool(ctx context.Context, call ToolInvocation) (ToolOutcome, error)
}

// RecordedEntryKind classifies one ordered trajectory entry.
type RecordedEntryKind string

const (
	RecordedMessage    RecordedEntryKind = "message"
	RecordedToolCall   RecordedEntryKind = "tool_call"
	RecordedToolResult RecordedEntryKind = "tool_result"
)

// RecordedEntry is one ordered item of a turn trajectory.
type RecordedEntry struct {
	Kind       RecordedEntryKind
	Body       string
	Role       string
	ToolName   string
	ToolCallID string
}

// TurnOutcome is the terminal disposition of a loop run.
type TurnOutcome string

const (
	TurnSucceeded TurnOutcome = "succeeded"
	TurnFailed    TurnOutcome = "failed"
	TurnCancelled TurnOutcome = "cancelled"
)

// RecordedTurn is the whole trajectory of one loop run, recorded atomically.
type RecordedTurn struct {
	IdempotencyKey  string
	ExpectedVersion int64
	RunID           string
	Outcome         TurnOutcome
	Entries         []RecordedEntry
	Usage           Usage
}

// RecordedTurnResult is the durable identity of a recorded turn.
type RecordedTurnResult struct {
	TurnID           string
	ResultingVersion int64
}

// TurnRecorder persists one completed turn trajectory transactionally. It is
// the B0.3 session boundary; the loop never imports the session package, which
// avoids the session -> agent import cycle.
type TurnRecorder interface {
	RecordTurn(ctx context.Context, turn RecordedTurn) (RecordedTurnResult, error)
}

// LoopConfig configures one provider-neutral loop run (a single session turn).
type LoopConfig struct {
	Provider        Provider
	Executor        ToolExecutor
	Recorder        TurnRecorder
	DeltaSink       DeltaSink
	Model           string
	MaxTokens       int
	Tools           []ToolDefinition
	Bounds          LoopBounds
	IdempotencyKey  string
	ExpectedVersion int64
	RunID           string
}

func (c LoopConfig) valid() bool {
	return c.Provider != nil && c.Executor != nil && c.Recorder != nil &&
		c.Model != "" && c.IdempotencyKey != "" && c.RunID != "" &&
		c.ExpectedVersion > 0 && c.Bounds.valid()
}

// RunResult summarizes a completed loop run.
type RunResult struct {
	Text             string
	Steps            int
	ToolCalls        int
	Usage            Usage
	Outcome          TurnOutcome
	TurnID           string
	ResultingVersion int64
}

// Loop control errors.
var (
	ErrLoopConfig     = errors.New("agent: invalid loop configuration")
	ErrLoopStepBudget = errors.New("agent: loop exceeded step budget")
	ErrLoopToolBudget = errors.New("agent: loop exceeded tool-call budget")
)

// Run drives a provider-neutral agent loop for one turn. It offers only the
// pinned tools, bounds provider steps, tool calls, and recorded body/result
// bytes, propagates context cancellation, and records the whole trajectory and
// usage into the recorder in a single transactional call regardless of outcome.
// It never logs or emits message or tool payloads.
func Run(ctx context.Context, cfg LoopConfig, input string) (RunResult, error) {
	if !cfg.valid() {
		return RunResult{}, ErrLoopConfig
	}

	messages := []Message{{Role: RoleUser, Content: input}}
	entries := []RecordedEntry{{Kind: RecordedMessage, Role: string(RoleUser), Body: boundBytes(input, cfg.Bounds.MaxBodyBytes)}}

	var usage Usage
	var finalText string
	steps := 0
	toolCalls := 0
	outcome := TurnSucceeded
	var runErr error

	var sink DeltaSink
	if cfg.DeltaSink != nil {
		sink = cfg.DeltaSink
	}

loop:
	for {
		if err := ctx.Err(); err != nil {
			outcome, runErr = TurnCancelled, err
			break
		}
		if steps >= cfg.Bounds.MaxSteps {
			outcome, runErr = TurnFailed, ErrLoopStepBudget
			break
		}
		steps++

		completion, err := cfg.Provider.Complete(ctx, CompletionRequest{
			Model:     cfg.Model,
			Messages:  cloneMessages(messages),
			Tools:     cfg.Tools,
			MaxTokens: cfg.MaxTokens,
			Stream:    sink != nil,
		}, sink)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				outcome = TurnCancelled
			} else {
				outcome = TurnFailed
			}
			runErr = err
			break
		}
		usage = addUsage(usage, completion.Usage)

		if completion.Text != "" {
			messages = append(messages, Message{Role: RoleAssistant, Content: completion.Text, ToolCalls: completion.ToolCalls})
			entries = append(entries, RecordedEntry{Kind: RecordedMessage, Role: string(RoleAssistant), Body: boundBytes(completion.Text, cfg.Bounds.MaxBodyBytes)})
		} else if len(completion.ToolCalls) > 0 {
			messages = append(messages, Message{Role: RoleAssistant, ToolCalls: completion.ToolCalls})
		}

		if len(completion.ToolCalls) == 0 {
			finalText = completion.Text
			outcome = TurnSucceeded
			break
		}

		for _, call := range completion.ToolCalls {
			if toolCalls >= cfg.Bounds.MaxToolCalls {
				outcome, runErr = TurnFailed, ErrLoopToolBudget
				break loop
			}
			toolCalls++
			entries = append(entries, RecordedEntry{
				Kind:       RecordedToolCall,
				Body:       boundBytes(call.Arguments, cfg.Bounds.MaxBodyBytes),
				ToolName:   call.Name,
				ToolCallID: call.ID,
			})
			resultBody := executeTool(ctx, cfg, call)
			resultBody = boundBytes(resultBody, cfg.Bounds.MaxResultBytes)
			entries = append(entries, RecordedEntry{
				Kind:       RecordedToolResult,
				Body:       resultBody,
				ToolName:   call.Name,
				ToolCallID: call.ID,
			})
			messages = append(messages, Message{Role: RoleTool, Name: call.Name, ToolCallID: call.ID, Content: resultBody})
		}
	}

	// Record the trajectory transactionally. A detached context ensures a
	// cancelled run still persists its cancelled turn.
	recorded, recErr := cfg.Recorder.RecordTurn(context.WithoutCancel(ctx), RecordedTurn{
		IdempotencyKey:  cfg.IdempotencyKey,
		ExpectedVersion: cfg.ExpectedVersion,
		RunID:           cfg.RunID,
		Outcome:         outcome,
		Entries:         entries,
		Usage:           usage,
	})
	if recErr != nil {
		return RunResult{}, fmt.Errorf("agent: record turn: %w", recErr)
	}

	return RunResult{
		Text:             finalText,
		Steps:            steps,
		ToolCalls:        toolCalls,
		Usage:            usage,
		Outcome:          outcome,
		TurnID:           recorded.TurnID,
		ResultingVersion: recorded.ResultingVersion,
	}, runErr
}

// executeTool runs one tool call, converting an executor error into a bounded
// error tool result so a single tool failure feeds back to the model instead of
// aborting the whole loop.
func executeTool(ctx context.Context, cfg LoopConfig, call ToolCall) string {
	outcome, err := cfg.Executor.ExecuteTool(ctx, ToolInvocation{CallID: call.ID, Name: call.Name, Arguments: call.Arguments})
	if err != nil {
		return "tool error: " + err.Error()
	}
	if outcome.IsError {
		return "tool error: " + outcome.Content
	}
	return outcome.Content
}

func cloneMessages(messages []Message) []Message {
	out := make([]Message, len(messages))
	copy(out, messages)
	return out
}

func addUsage(a, b Usage) Usage {
	return Usage{
		InputTokens:  a.InputTokens + b.InputTokens,
		OutputTokens: a.OutputTokens + b.OutputTokens,
		TotalTokens:  a.TotalTokens + b.TotalTokens,
	}
}

// boundBytes truncates value to at most max bytes at a valid UTF-8 boundary.
func boundBytes(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	cut := max
	for cut > 0 && value[cut]&0xC0 == 0x80 {
		cut--
	}
	return value[:cut]
}
