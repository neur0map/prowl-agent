# Task B0.4 - Provider-neutral loop and read-only context tools - report

## Summary
Implemented the smallest provider-neutral agent loop plus an explicit read-only
tool registry for the tracer: a deterministic scripted fake exercised through
the real loop, a fixture-tested chat-completion-compatible wire contract, and exactly the
three read-only Prowl tools (`search_context`, `get_context`, `read_source`) over
the canonical `internal/context` service and the rooted `boundedio` reader. No
production provider configuration, provider breadth, side-effect tools,
approvals, background processes, delegation, or UI were added.

## Changed paths (created - 8 files, nothing else modified)
- `internal/agent/provider.go`
- `internal/agent/provider_fake_test.go`
- `internal/agent/loop.go`
- `internal/agent/loop_test.go`
- `internal/toolruntime/registry.go`
- `internal/toolruntime/registry_test.go`
- `internal/toolruntime/readonly_context.go`
- `internal/toolruntime/readonly_context_test.go`

`internal/agent/prompt.go`, `internal/agent/exposure.go`, and every
session/operations/profile file are untouched. The only tracked working-tree
change, `internal/profile/model.go`, predates this task (present in
`git status` before any B0.4 edit) and was not authored here.

## Architecture and reuse
- **No import cycle.** `internal/session` imports `internal/agent`, so the loop
  never imports session. It records through an agent-local `TurnRecorder` port;
  the test's `sessionRecorder` adapter calls the real `session.Service.AppendTurn`.
- **Loop is registry-agnostic.** `loop.go` depends only on the `ToolExecutor`
  port. The test's `registryExecutor` binds the real `toolruntime.Registry` with
  a `Pinned` toolset and `ReadOnlyGrant`. `agent` never imports `toolruntime`.
- **Canonical services reused, not duplicated.** `search_context`/`get_context`
  call `context.Service.Search`/`Get` and return the same `context.Packet`
  (citations, freshness, `omitted`, and budget/bounds preserved verbatim).
  `read_source` uses `os.Root` + `boundedio.OpenRegular`/`ReadAllContext`, the
  established rooted, symlink-safe, regular-file-only, byte-bounded reader.
- **Pinning.** A session pins its toolset via `Registry.Pin`/`PinAll`. `Execute`
  refuses any name not in the pin and any registration whose canonical schema
  hash drifted from the pinned hash, so a tool added mid-session is never
  exposed.
- **Deny-by-default permissions.** `Evaluate(grant, required)` denies an empty
  required set and any class outside the grant; evaluation runs before the
  handler, proven by a side-effect probe.

## TDD RED → GREEN evidence

### toolruntime
RED (tests written, no implementation):
```
$ go test -tags sqlite_fts5 ./internal/toolruntime
github.com/prowl-agent/prowl-agent/internal/toolruntime: no non-test Go files ...
FAIL	github.com/prowl-agent/prowl-agent/internal/toolruntime [build failed]
```
GREEN (after `registry.go` + `readonly_context.go`):
```
$ go test -race -tags sqlite_fts5 ./internal/toolruntime -count=1
ok  	github.com/prowl-agent/prowl-agent/internal/toolruntime	1.025s
```

### agent
RED (tests written, no implementation):
```
$ go test -tags sqlite_fts5 ./internal/agent
internal/agent/loop_test.go:27: undefined: agent.ToolInvocation
internal/agent/provider_fake_test.go:18: undefined: agent.Completion
... (undefined: CompletionRequest, Delta, RecordedTurn, TurnOutcome, ToolDefinition, ...)
FAIL	github.com/prowl-agent/prowl-agent/internal/agent [build failed]
```
GREEN (after `provider.go` + `loop.go`):
```
$ go test -race -tags sqlite_fts5 ./internal/agent -count=1
ok  	github.com/prowl-agent/prowl-agent/internal/agent	1.050s
```

### Required gate - PASS
```
$ go test -race -tags sqlite_fts5 ./internal/agent ./internal/toolruntime \
    -run 'Test(FakeProvider|ChatWireFixture|ReadOnlyContext|PermissionDenied)' -count=1
ok  	github.com/prowl-agent/prowl-agent/internal/agent	1.042s
ok  	github.com/prowl-agent/prowl-agent/internal/toolruntime	1.026s
```

### Full two-package race suite - PASS (27 tests, incl. pre-existing agent tests)
```
$ go test -race -tags sqlite_fts5 ./internal/agent ./internal/toolruntime -count=1
ok  	github.com/prowl-agent/prowl-agent/internal/agent	1.049s
ok  	github.com/prowl-agent/prowl-agent/internal/toolruntime	1.027s
```
Selector-matched tests observed passing: `TestFakeProviderTextTurn`,
`TestFakeProviderToolCallContinuation`, `TestFakeProviderCancellation`,
`TestFakeProviderMalformedResponse`, `TestFakeProviderBoundedSteps`,
`TestFakeProviderBoundedToolCalls`, `TestFakeProviderInvalidConfig`,
`TestFakeProviderDeterministicScript`, `TestChatWireFixtureRequest`,
`TestChatWireFixtureResponse`, `TestReadOnlyContextToolset`,
`TestReadOnlyContextSearchAndGet`, `TestReadOnlyContextSearchRejectsBlankQuestion`,
`TestReadOnlyContextReadSource`, `TestReadOnlyContextReadSourceDenied`,
`TestPermissionDenied`, `TestPermissionDeniedBeforeHandlerSideEffect`.

### go vet - exit 0
```
$ go vet -tags sqlite_fts5 ./internal/agent ./internal/toolruntime
(exit 0)
```
`gofmt -l` over all eight files reports nothing.

## Fake and wire fixtures (hand-authored)

### Scripted fake (`provider_fake_test.go`)
`scriptedProvider` returns the next `scriptStep` per `Complete` call, records
every `CompletionRequest`, honors `ctx` (a step may `block` on `ctx.Done()` and
another may invoke a stored `cancel`), and returns `ErrProviderExhausted` past
the script. It drives the real `agent.Run` for text, tool-call/continuation,
cancellation, malformed response, and step/tool budgets.

### chat completion request fixture (byte-exact, `TestChatWireFixtureRequest`)
Expected bytes are authored independently and compared with `bytes.Equal` to
`EncodeChatRequest` output (never computed via the encoder):
```json
{"model":"model-a","messages":[{"role":"system","content":"You are Prowl."},{"role":"user","content":"find auth"},{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"search_context","arguments":"{\"question\":\"auth\"}"}}]},{"role":"tool","content":"1 result","tool_call_id":"call_1","name":"search_context"}],"tools":[{"type":"function","function":{"name":"search_context","description":"Search project context.","parameters":{"type":"object","properties":{"question":{"type":"string"}},"required":["question"]}}}],"max_tokens":1024,"stream":false}
```

### chat completion response fixture (`TestChatWireFixtureResponse`)
Tool-call response (with extra `id`/`object`/`index` fields to prove leniency)
decodes to one `ToolCall` (`call_1`/`search_context`/`{"question":"auth"}`),
`StopToolCalls`, usage `{11,7,18}`. A text response decodes to `"hello there"`
with `StopEndTurn`. `{not json`, `{"choices":[]}`, and a tool call with invalid
`arguments` each return `ErrProviderMalformedResponse`.

## Self-review against the contract
- Provider interface is neutral (completion, streaming-ready `Delta`/`DeltaSink`,
  tool calls, `Usage`, cancellation, typed `ErrProvider*`). Wire conversion is
  isolated in unexported `openAI*` types behind `Encode/Decode` only. ✔
- Fake exercises text, tool call, tool result, continuation, cancellation,
  malformed, and bounded-loop through the real loop. ✔
- chat-completion wire is fixtured with hand-authored bytes; no session/domain coupling to
  external provider types; no network. ✔
- Registry has explicit name, canonical stable schema + hash, handler,
  availability, permission set, and I/O bounds; duplicate names and
  invalid/typeless/non-object schemas fail closed. ✔
- Only `search_context`, `get_context`, `read_source` registered; all read-only. ✔
- Context tools call the canonical `context.Service`; citations/freshness/
  omissions/budget retained in the returned packet. ✔
- `read_source` is rooted, symlink-safe, regular-file-only, and byte/line
  bounded; absolute, traversal, symlink-escape, directory, oversize, and empty
  paths are denied with no out-of-root leak. ✔
- Permission evaluation is deny-by-default and before side effects; unknown,
  write, network, process, widened-composite, and widened-request calls denied. ✔
- Loop bounds steps, tool calls, and body/result bytes; propagates cancellation;
  records the whole trajectory + usage into B0.3 in one transactional
  `AppendTurn`; does not log or emit payloads. ✔
- Running sessions offer/execute only the pinned toolset; a tool added after the
  pin is unresolvable (`TestRegistryPinnedToolsetIsolation`). ✔

## Concerns / notes
- Cancellation records a cancelled turn using `context.WithoutCancel` so the
  partial trajectory persists transactionally even though the run context is
  done. This is a deliberate choice (durable evidence over silent drop); revisit
  if B0.6 prefers not to persist cancelled turns.
- `boundBytes` (loop) and `boundString` (registry) duplicate trivial UTF-8-safe
  truncation. They live in different packages and the 8-file ownership rule
  forbids a shared helper here; fold into a shared util when a natural home
  exists in a later slice.
- Loop-test recording uses the real `session.Service`; only the provider is
  faked, matching "mock only the external provider boundary." The read-only
  context tools are tested directly over a real indexed `store.Store`
  (`UpsertFile` + `ReplaceFileGraph` + FTS `SearchChunkText`), so the gate
  requires the `sqlite_fts5` build tag (as specified).
- Not committed, per task instruction.
