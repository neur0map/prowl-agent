# Prowl Agent: Hermes-Inspired Agent Operations Port

**Date:** 2026-07-23
**Status:** Active companion implementation contract adopted by the canonical product-evolution plan
**Prowl baseline:** `product-evolution` at `95ba94fd865b911c4b1a9b592f0b880b7ef2167d`
**Hermes evidence revision:** NousResearch/hermes-agent `c4f5a45d5d9903998fb318ac6f3c5e6623e60445` (inspected 2026-07-23; author `2026-07-23T09:52:16-07:00`; committer `2026-07-23T12:01:24-07:00`; subject `fix(gateway): tolerate bare GatewayRunner instances in the ignored-channel guard`)
**Implementation rule:** clean-room Go implementation of behavior and contracts unless an adapted file is explicitly attributed under the upstream MIT license

## 1. Decision

Prowl should evolve from a knowledge compiler with agent-facing surfaces into a **knowledge-native local agent operating system**.

The knowledge compiler remains the differentiator. The Hermes-inspired expansion adds the missing operating layers above it:

```text
projects + sources + Git + portable knowledge
                    │
                    ▼
 deterministic index + evidence + context compiler
                    │
      ┌─────────────┴─────────────┐
      ▼                           ▼
 human workbench          provider-neutral agent kernel
      │                           │
      └─────────────┬─────────────┘
                    ▼
 profiles + sessions + tools + skills + approvals
                    │
                    ▼
 durable task boards + workers + automations + events
                    │
                    ▼
 live operations dashboard + CLI + MCP + plugins
```

“Almost full port” means parity for the reusable **behavioral contracts** that make Hermes coherent—not a mechanical Python-to-Go rewrite, pixel copy, or immediate duplication of every provider and messaging adapter.

The implementation must preserve Prowl’s existing strengths:

- deterministic evidence before synthesis;
- portable, Git-reviewable Markdown knowledge;
- SQLite indexes that are rebuildable;
- provider-neutral CLI/MCP contracts;
- loopback-only secure defaults;
- compact model-facing surfaces;
- no mandatory cloud control plane.

## 2. Evidence and provenance boundary

Primary evidence inspected at the pinned Hermes revision:

- `AGENTS.md` — narrow-waist, prompt-cache, extension, testing, and contribution invariants.
- `agent/system_prompt.py`, `agent/prompt_builder.py`, `agent/agent_init.py` — prompt assembly and worker guidance.
- `tools/registry.py`, `toolsets.py`, `model_tools.py` — self-registering tools, toolsets, availability gates, and runtime filtering.
- `tools/kanban_tools.py` — model-facing task coordination surface.
- `hermes_cli/kanban_db.py`, `hermes_cli/kanban.py`, `hermes_cli/kanban_diagnostics.py` — task kernel, dispatch, state transitions, diagnostics, recovery, and storage.
- `website/docs/user-guide/features/kanban.md` and `kanban-worker-lanes.md` — documented human/worker contracts.
- `plugins/kanban/dashboard/plugin_api.py` and `dist/index.js` — dashboard API, event tailing, cursor/reload behavior, and board UI.
- `hermes_cli/web_server.py`, `web/src/lib/api.ts`, `web/src/App.tsx`, and `web/src/pages/*` — management dashboard, auth, profiles, sessions, system actions, and live data patterns.
- `tools/delegate_tool.py`, `tools/session_search_tool.py`, `tools/memory_tool.py`, `tools/skill_manager_tool.py`, and `agent/context_engine.py` — delegation, recall, durable learning, and compaction boundaries.
- Upstream tests under `tests/hermes_cli/`, `tests/plugins/`, `tests/tools/`, `tests/gateway/`, and `tests/dashboard/` — executable behavior contracts.

Immutable source anchors for the material contracts:

- [three-tier, session-stable prompt construction](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/agent/system_prompt.py#L3-L19) and [cached assembly invariant](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/agent/system_prompt.py#L147-L163);
- [lazy nested project-rule discovery without system-prompt mutation](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/agent/subdirectory_hints.py#L1-L11);
- [tool registration metadata and result/schema controls](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/tools/registry.py#L88-L116) and [context-gated Kanban/webhook-safe toolsets](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/toolsets.py#L68-L96);
- [isolated, capability-reduced delegation](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/tools/delegate_tool.py#L3-L16) and [safe default child approvals](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/tools/delegate_tool.py#L45-L73);
- [zero-schema normal sessions and task-scoped Kanban tools](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/tools/kanban_tools.py#L1-L27) plus [worker/orchestrator capability separation](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/tools/kanban_tools.py#L75-L124);
- [per-board WAL/CAS coordination](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/hermes_cli/kanban_db.py#L49-L67) and [claim/heartbeat/reclaim guardrails](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/hermes_cli/kanban_db.py#L189-L215);
- [snapshot cursor](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/plugins/kanban/dashboard/plugin_api.py#L444-L508), [board-pinned polling](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/plugins/kanban/dashboard/plugin_api.py#L2232-L2236), and [stream cursor/replay](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/plugins/kanban/dashboard/plugin_api.py#L2515-L2573);
- [deterministic FTS session discovery/scroll/browse](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/tools/session_search_tool.py#L5-L23);
- [authenticated, filtered, bounded, rate-limited, replay-aware webhook ingress](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/gateway/platforms/webhook.py#L1-L30).

Authoritative documentation:

- <https://hermes-agent.nousresearch.com/docs/user-guide/features/kanban>
- <https://hermes-agent.nousresearch.com/docs/user-guide/features/kanban-worker-lanes>
- <https://github.com/NousResearch/hermes-agent/tree/c4f5a45d5d9903998fb318ac6f3c5e6623e60445>

### Licensing and clean-room rule

Hermes Agent is MIT licensed, copyright Nous Research. Prowl currently has no repository license file.

Until Prowl’s licensing is decided:

- do not copy upstream source or generated bundles;
- implement from behavior contracts and independent tests;
- preserve this revision/path provenance ledger;
- use Prowl names, schemas, UI composition, and Go idioms;
- if any substantial upstream code is later adapted, add the upstream MIT notice and enumerate adapted paths in `THIRD_PARTY_NOTICES.md` before merging.

## 3. Source-verified Hermes behavior

### 3.0 Five exposure channels

Hermes behavior is not one undifferentiated feature surface. Every capability must be classified as one or more of:

1. cached system-prompt content;
2. model-visible tool schema;
3. progressively discovered skill/context/tool metadata;
4. ephemeral turn/tool-result injection;
5. human-only CLI/dashboard/API control-plane behavior.

Prowl must preserve these boundaries. A dashboard action does not imply a model tool; a registered tool does not imply a granted session schema; a procedural skill does not belong in every cached prompt; and a worker-only lifecycle tool must not leak into normal sessions.

### 3.1 Narrow core and discoverable capability edges

Hermes keeps the core agent waist narrow because every model-visible tool has recurring schema cost. Tools self-register with schema, handler, toolset, availability checks, result limits, and optional dynamic schema overrides. Toolsets compose tools and can be gated by runtime context. Normal sessions do not receive Kanban schemas unless configured; dispatched workers receive task-scoped Kanban tools.

The model is taught procedures through compact system-prompt guidance and progressively loaded skills rather than by keeping every workflow in the core prompt. Prompt prefix stability is treated as an architectural invariant.

### 3.2 Profiles and sessions

Profiles are named agent identities with model, toolset, skill, memory, and channel configuration. They are state namespaces, not security sandboxes; OS/process/container boundaries are required when hostile isolation matters. Sessions persist conversation and tool trajectories, support search and export, and survive surface changes. Session state, durable memory, skills, and process-local working context are separate lifecycles.

Root project context participates in the cached session prefix. Nested `AGENTS.md`, `CLAUDE.md`, and `.cursorrules`-style hints are discovered only when a tool enters that rooted subtree and are appended ephemerally, preserving prompt-cache stability. Prowl should support the behavior while treating every repository rule file as untrusted project content below system/profile/user policy.

### 3.3 Kanban as durable coordination, not delegation RPC

Hermes distinguishes:

- delegation: short-lived fork/join reasoning returned to the caller;
- Kanban: durable fire-and-forget work with named identities, retries, human comments, audit history, and restart survival.

The task kernel provides:

- isolated boards;
- tasks, parent-child links, comments, append-only events, run attempts, attachments, and notification subscriptions;
- statuses including triage, dependency waiting, scheduling, ready, running, blocking, review, completion, and archival;
- typed blockers (`dependency`, `needs_input`, `capability`, `transient`);
- idempotency keys;
- WAL mode and compare-and-swap claims;
- claim locks, leases, heartbeats, stale/crash reclaim, maximum runtime, retry ceilings, and circuit breakers;
- scratch, trusted-directory, and Git-worktree workspaces;
- per-task profiles, skills, model overrides, artifacts, and structured handoffs;
- diagnostics for stranded tasks and unhealthy workers.

Human CLI/dashboard actions and model tools call the same kernel. A worker or lane never owns lifecycle truth.

### 3.4 What a task worker knows

A dispatched worker receives a concise task-only protocol:

1. inspect the current task and parent handoffs first;
2. work only in the assigned workspace;
3. heartbeat only for long operations;
4. block rather than guess when genuinely unable to proceed;
5. terminate with one structured outcome;
6. create follow-up cards instead of expanding scope;
7. never use interactive clarification in a headless run;
8. never fabricate task IDs, artifacts, results, or verification.

The worker receives only task-scoped lifecycle tools. Orchestrator profiles receive broader create/link/list/routing tools but should not execute implementation work.

### 3.5 Live dashboard data

The Kanban dashboard:

1. loads a canonical board snapshot with `latest_event_id`;
2. connects to a board-pinned event stream from that cursor;
3. receives ordered append-only event batches;
4. treats events as invalidation signals;
5. reloads canonical board/task data rather than trusting client-side event reduction as durable truth;
6. reconnects with backoff and a cursor;
7. opens a new stream when the board changes.

At the inspected revision, the server-side WebSocket tails SQLite every 300 ms and emits up to 200 new task events per query. Other dashboard data uses endpoint-specific polling, including slower shell status/session/log refresh. WebSocket authorization delegates to a shared dashboard auth gate, with short-lived single-use tickets in gated deployments.

### 3.6 Management dashboard breadth

Hermes’s dashboard exposes chat, profiles, sessions/search, files, skills, plugins, MCP, models/providers, config, credentials, channels/pairing, cron, webhooks, logs, analytics, system actions, documentation, and plugin-contributed pages. A shared authenticated API client handles profile scope and auth. Plugins receive a constrained dashboard SDK rather than reaching internal state directly.

### 3.7 Source/documentation drift and implementation cautions

The inspected revision contains behavior/documentation mismatches that must not become Prowl requirements:

- delegation documentation disagreed with the source blocklist around `execute_code`;
- webhook documentation implied CLI-equivalent breadth while source defaults to a constrained webhook-safe toolset;
- registry prose implied later registration could win collisions while source rejects collisions except authorized plugin overrides and MCP refresh cases;
- dashboard documentation mixed current remote-auth guidance with stale no-auth/localhost-trust language;
- Kanban plugin test import paths include a fail-open authorization fallback that is not a valid production pattern.

Prowl enforcement derives from versioned service/policy tests, not copied prose or framework accidents. Future upstream refreshes must record changed behavior instead of preserving these cautions as eternal claims.

## 4. Prowl determinations: adopt, adapt, improve, defer

| Hermes pattern | Prowl determination |
|---|---|
| Narrow core and toolsets | Adopt. Extend the existing capability catalog into executable, permissioned runtime toolsets; do not create a second unrelated discovery system. |
| Byte-stable prompt prefix | Adopt. Pin prompt/toolset/profile snapshots for a session; changes apply on a new session or explicit restart boundary. |
| Profiles | Adopt with XDG-backed global profiles and optional project overlays. No secrets in profile files. |
| Session ledger/search | Adopt. Store append-only messages, tool calls, usage, compaction checkpoints, and source/surface metadata in a durable operational DB. |
| Skills and memory | Adopt, integrating Prowl knowledge/context rather than duplicating it. Skills are procedural; portable project knowledge remains Markdown. |
| Delegation | Adopt as bounded fork/join work with strict depth/concurrency and no durable-board mutation from child contexts. |
| Kanban kernel | Adopt behavior, reimplement in Go, add first-class review gates and explicit policy fields. |
| Multi-board isolation | Adopt. Each board has its own durable DB, artifacts, logs, and workspace root. |
| Worker lanes | Adopt behind a typed runner interface. Ship native Prowl first, then opt-in Codex/Claude Code/OpenCode/Hermes adapters. |
| Dashboard events | Adapt to authenticated fetch-streamed SSE for one-way operational events; reserve WebSockets for future bidirectional terminal/media use. |
| Polling SQLite every 300 ms | Improve. Commit event rows transactionally and publish after commit through an in-process broker; use DB cursor replay on reconnect/restart. |
| Event-driven client patches | Adapt as snapshot invalidation. Server snapshots remain canonical; events include affected resource/version and cursor only. |
| Query-token WebSocket auth | Do not copy. Bearer-authenticated fetch streaming avoids credentials in URLs. Future WebSockets require single-use, short-TTL tickets. |
| Test-only fail-open plugin auth fallback | Do not copy. Auth dependencies are injected; production and tests fail closed. |
| Review-required block prefix | Improve. Review is a first-class status/gate with explicit approver, evidence, and decision events. Importers may understand the Hermes prefix. |
| Scratch/dir/worktree workspaces | Adopt with rooted path policy, symlink checks, deterministic cleanup, artifact preservation, and explicit operator trust. |
| Embedded PTY chat | Defer. First build semantic run/session streaming; terminal embedding is optional later. |
| Every provider/channel integration | Defer breadth. Build stable provider/channel/plugin interfaces plus representative adapters; do not burden the core with dozens of integrations. |
| Desktop/mobile shells | Defer packaging until the loopback API, bridge, and workbench contracts are stable. |

### 4.1 Hermes parity ledger

This ledger is normative. New upstream discoveries receive a new ID; IDs are never renumbered or silently removed.

| ID | Behavior contract | Disposition / target |
|---|---|---|
| HP-001 | Provider-neutral completion, streaming, tool calls, usage, retries | Implement Phase 3B |
| HP-002 | Named state-namespace profiles with model/toolset/skill/memory policy; no false sandbox claim | Implement Phase 3B |
| HP-003 | Durable sessions/messages/tool trajectories and source metadata | Implement Phase 3B |
| HP-004 | Search, resume, branch/checkpoint, export, repair, retention | Implement Phase 3B/5 |
| HP-005 | Stable semantic prompt prefix, session-pinned configuration, and ephemeral rooted nested-rule discovery | Implement Phase 3B |
| HP-006 | Executable tool registry with schema, handler, availability, permission, and result bound | Implement Phase 3B by extending Prowl capability manifests |
| HP-007 | Context-gated composable toolsets with zero unused schema footprint | Implement Phase 3B |
| HP-008 | Progressive skill metadata discovery and explicit full-body load | Implement Phase 3B |
| HP-009 | Scoped user memory separated from skills, sessions, tasks, and project knowledge | Implement Phase 3B/5 |
| HP-010 | Session search before asking users to repeat retrievable context | Implement Phase 3B/5 |
| HP-011 | Context budgeting/compaction with original durable history preserved | Implement Phase 3B |
| HP-012 | Mid-turn steering, queued follow-up, cancellation, and status events | Implement Phase 3B |
| HP-013 | Tracked background processes with bounded logs and completion notification | Implement Phase 3B |
| HP-014 | Bounded fork/join subagents with depth/toolset/context isolation | Implement Phase 3B |
| HP-015 | Explicit risky-action approvals and writable/network/process scope | Improve and implement Phase 3B |
| HP-016 | Verification evidence ledger and finish-with-real-output enforcement | Implement Phase 3B/5 |
| HP-017 | Multiple isolated task boards with validated slugs and metadata | Implement Phase 3C |
| HP-018 | Durable task state machine and optimistic versioning | Implement Phase 3C |
| HP-019 | Acyclic dependencies, fan-out/fan-in, and automatic readiness | Implement Phase 3C |
| HP-020 | Scheduled tasks and automation-safe idempotent creation | Implement Phase 3C/5 |
| HP-021 | Durable human/agent comments and structured handoffs | Implement Phase 3C |
| HP-022 | Transactional append-only events, ordered cursor replay, retention | Improve and implement Phase 3C |
| HP-023 | Per-attempt runs, logs, summaries, evidence, outcomes | Implement Phase 3C |
| HP-024 | Bounded attachments and declared artifact preservation | Implement Phase 3C |
| HP-025 | First-class review/approval state and immutable decisions | Improve and implement Phase 3C |
| HP-026 | Typed dependency/input/capability/transient blockers and loop breaker | Implement Phase 3C |
| HP-027 | CAS claims, unique leases, heartbeats, runtime caps | Implement Phase 3C |
| HP-028 | Spawn/reap, explicit unknown-after-crash state, stale reclaim, retries, circuit breakers, stranded diagnostics | Implement Phase 3C |
| HP-029 | Rooted scratch/trusted-directory/Git-worktree workspaces | Implement Phase 3C |
| HP-030 | Native and external typed worker lanes | Implement native in Phase 3C; external adapters Phase 3C/6 |
| HP-031 | Task-scoped worker tools and orchestrator-only routing tools | Implement Phase 3C |
| HP-032 | Goal/judge multi-turn task mode | Defer until single-run evidence and review gates are stable; reassess Phase 7 |
| HP-033 | Task notifications/subscriptions and reply-to-resume context | Implement Phase 5/7 |
| HP-034 | Authoritative snapshot plus live invalidation/refetch | Implement Phase 3A/3C |
| HP-035 | Cursor reconnect, backoff, gap/reset, scope-switch isolation | Improve and implement Phase 3A/3C |
| HP-036 | Loopback bearer auth and secret-free browser bootstrap | Existing tracer bullet; harden continuously |
| HP-037 | Remote multi-user dashboard auth, CSRF, tickets, proxy/TLS trust | Defer to Phase 7; disabled by default |
| HP-038 | Kanban board/task/run/review/worker/diagnostic UI | Implement Phase 3C |
| HP-039 | Session/chat/live-run UI | Implement semantic events Phase 3B; PTY optional Phase 7 |
| HP-040 | Profiles/models/toolsets/skills/plugins/config management UI | Implement Phase 7 |
| HP-041 | Files/logs/health/repair/update/usage management UI | Implement selected safe operations Phase 7 |
| HP-042 | Bounded/idempotent cron, webhook, script-only, direct-delivery, chaining, and automation UI | Implement Phase 5/7 |
| HP-043 | Versioned backend plugin API and constrained frontend plugin SDK | Implement Phase 6/7 |
| HP-044 | Profile-scoped dashboard queries without cross-profile bleed | Implement Phase 3B/7 |
| HP-045 | Localization, themes, responsive UI, keyboard, screen-reader, reduced motion | Implement Phase 3A/6/7 |
| HP-046 | CLI over the same application services | Implement continuously |
| HP-047 | MCP client/server and agent-tool parity over shared services | Extend Phase 3B/6 |
| HP-048 | Versioned stdio/ACP bridge | Implement Phase 4/7 |
| HP-049 | TUI and desktop packaging | Defer until Phase 7 contract review |
| HP-050 | Messaging gateway, pairing, and home-channel delivery | Implement interface plus one reference adapter Phase 7; broad channels optional |
| HP-051 | Voice, browser/computer use, smart-home, and media breadth | Capability/plugin ecosystem, not core parity gate |
| HP-052 | Local-only usage/cost analytics and no unconsented telemetry | Implement Phase 7 |
| HP-053 | Import/export/migration across profiles, sessions, skills, tasks, and knowledge | Implement Phase 5–7 |
| HP-054 | Setup/doctor/docs that expose capability, security, and lifecycle truth | Implement continuously and audit finally |

## 5. Target architecture

### 5.1 State classification

| State | Location/model | Durability |
|---|---|---|
| Project knowledge | `.prowl/knowledge/**/*.md` | Canonical, portable, Git-reviewable |
| Project configuration/rules | `.prowl/config.toml`, `.prowl/rules.toml` | Canonical project configuration |
| Source index/embeddings | `.prowl/index.db` | Derived and rebuildable |
| Global configuration/profiles | XDG config home | Durable configuration, atomic writes, no secret values |
| Credentials | OS credential provider or permission-restricted secret references | Durable secret material, never returned by normal APIs |
| Sessions, tool calls, approvals, usage | XDG data home operational DB | Durable user state, exportable and repairable |
| Boards/tasks/runs/events | Per-board DB under XDG data home | Durable operational truth; never mixed with `index.db` |
| Attachments/artifacts/logs | Per-board data/state roots | Durable by declared retention policy |
| Active leases/process handles/event broker | Process memory plus lease rows | Ephemeral runtime backed by durable recovery state |
| Launch bearer and stream subscribers | Process memory plus one-shot browser fragment handoff | Ephemeral; automatic launch prints only a redacted origin; manual/failure handoff may print the fragment once with a sensitive-value warning; never sent to the server before bootstrap or persisted by Prowl |

### 5.2 Shared service graph

```text
CLI ─────────────┐
MCP ─────────────┤
Workbench API ───┤
Plugin bridge ───┤──► application services ─► repositories/state machines
Agent tools ─────┤
Automations ─────┤
Worker runners ──┘
```

No CLI command, model tool, frontend handler, plugin, or automation may implement lifecycle SQL directly. HTTP and model-tool layers remain thin adapters.

### 5.3 Proposed Go packages

Exact paths may be refined by the implementation-plan review, but responsibilities are fixed:

- `internal/operations` — durable global operational store, migrations, backups, repair, retention.
- `internal/events` — typed event envelopes, cursors, replay, broker, backpressure, gap/reset semantics.
- `internal/profile` — profile resolution, inheritance, validation, snapshots.
- `internal/session` — sessions, messages, tool calls, steering queue, usage, checkpoints, export/search.
- `internal/toolruntime` — executable registry layered on `internal/capability`, toolsets, permission checks, result bounds.
- `internal/skill` — metadata discovery, full-load execution, provenance, validation.
- `internal/agent` — provider-neutral loop, stable prompt assembly, context budgeting, tool dispatch, compaction, cancellation.
- `internal/approval` — explicit policy and human approval records for risky actions.
- `internal/taskboard` — board/task/run state machines and transactional event append.
- `internal/runner` — native and external worker-lane interfaces.
- `internal/dispatcher` — dependency promotion, CAS claims, leases, spawn/reap, reclaim, circuit breakers.
- `internal/automation` — cron, webhook, idempotency, task/session launch policies.
- `internal/workbench` — authenticated transport adapters over shared services only.

### 5.4 Event contract

Every event has:

- schema version;
- monotonic board/global cursor;
- event ID and timestamp;
- resource kind and ID;
- event kind;
- actor and run/session correlation IDs;
- current resource version;
- redacted, size-bounded metadata.

Writes append state and event in one transaction. Publishing happens only after commit. If an in-memory publish is missed, clients recover from the durable cursor. Slow clients receive a `reset`/gap marker and reload a snapshot rather than exhausting memory.

SSE clients use `fetch` with the bearer header and an explicit cursor. Keepalive comments contain no data. Disconnect cancels the subscriber context. Event payloads never contain credentials, full prompts, raw private file content, or unbounded logs.

### 5.5 Agent prompt and capability contract

A session pins:

- profile snapshot;
- provider/model and context limits;
- core prompt version;
- active toolset/schema generation;
- project context manifest and hashes;
- explicit skills loaded for the session/task;
- permission/approval policy.

The prompt layers are:

1. compact invariant core;
2. active profile and surface constraints;
3. project index/table-of-contents context;
4. task/worker protocol only when applicable;
5. loaded skills and selected context packet;
6. current conversation window/compaction handoff.

Normal sessions do not receive Kanban tools. Task workers receive task-scoped show/comment/heartbeat/block/complete tools. Orchestrators receive list/create/link/reassign/unblock tools but implementation capabilities are excluded by default.

### 5.6 Agent operating protocol

Prowl's compact invariant guidance teaches every agent to:

1. discover and load relevant capabilities/skills before improvising a workflow;
2. retrieve a budgeted Prowl context packet before broad project search;
3. distinguish instructions from untrusted source, web, attachment, task-body, and tool-output content;
4. resolve prerequisites before side effects and request approval only when policy requires it;
5. execute and verify real work rather than narrating a plan or fabricating blocked results;
6. issue independent read-only calls concurrently while preserving true dependencies;
7. treat mid-turn human steering and cancellation as authoritative session events;
8. use fork/join delegation only for bounded subtasks whose results return to the active run;
9. use durable tasks for work that must survive the run, cross profiles, await humans, or remain auditable;
10. save procedural learning as a skill, stable user facts in scoped memory, portable project facts through the knowledge proposal flow, and temporary progress only in the session/task ledger;
11. search prior sessions or task history before asking the user to repeat retrievable context;
12. expose uncertainty, omissions, verification evidence, changed artifacts, and exact blockers in every handoff.

Task-worker guidance additionally requires `show` first, workspace confinement, lease-aware lifecycle calls, sparse heartbeats, explicit block kinds, structured evidence, and exactly one terminal outcome. Headless workers never invoke interactive clarification; they comment and block for input.

The prompt must stay compact. Detailed workflows live in versioned skills and task context, while provider-specific message conversion stays outside the semantic prompt builder.

### 5.7 Live-data ownership

The dashboard does not infer truth from DOM state, process output, or optimistic client patches. For every live domain:

- snapshot endpoints return resource versions and the latest durable cursor;
- event streams carry ordered invalidations and lifecycle summaries;
- clients debounce and refetch affected canonical resources;
- optimistic actions include expected versions and roll back on conflict;
- reconnect resumes from the cursor or receives an explicit reset/gap event;
- long jobs and model runs expose phase, progress, cancellation state, latest evidence, and bounded log tails;
- server restart, profile/board switch, and session rotation clear stale subscriptions before opening new scoped streams.

This contract applies to indexing, research, agent turns, tool calls, tasks, runs, approvals, diagnostics, automations, logs, and setup operations.

### 5.8 Task lifecycle contract

Recommended statuses:

```text
triage → todo/scheduled → ready → running → review → done → archived
                                  └────────► blocked ──────┘
```

Invariants:

- dependencies form an acyclic graph;
- a task is claimable only from `ready` and only when dependencies and schedule allow;
- claim is compare-and-swap with a unique lease token;
- every run has exactly one terminal outcome;
- task-scoped workers cannot mutate unrelated tasks;
- delegated children cannot mutate durable boards;
- completion requires declared verification/evidence policy;
- review-gated tasks cannot enter `done` without an approval event;
- heartbeats cannot revive a superseded lease;
- stale workers cannot terminate a newer run;
- idempotency keys are unique within board/tenant scope;
- all destructive operations are rooted, reversible where possible, and audited.

Task records carry explicit objective, acceptance evidence, priority/ordinal, parent/dependencies, required capabilities/skills, preferred runner, profile/model override, schedule, attempt/backoff/runtime policy, issue/external references, workspace policy, artifact declarations, optimistic version, and idempotency key. Do not encode lifecycle policy in free-form descriptions.

### 5.9 Automation ingress and execution

Scheduled and event-driven work uses the same run/task/session services but a stricter default capability envelope:

1. authenticate the trigger before reading an unbounded body;
2. enforce body, rate, concurrency, runtime, cost, and delivery bounds;
3. filter accepted event types and source scopes;
4. transform untrusted payloads into a typed event plus clearly delimited content;
5. deduplicate with a scoped idempotency key before launching work;
6. select a pinned profile/model/toolset/skill/workdir policy;
7. run either a restricted agent, a declared script-only job, or direct delivery;
8. persist claim, execution, output/evidence, terminal status, and delivery result;
9. deliver a bounded summary while keeping detailed local audit data under retention policy.

Cron supports one-shot and recurring schedules, missed-run policy, pause/resume, finite repeats, model/toolset/skill/workdir overrides, script-only execution, output chaining from named upstream jobs, and explicit local/origin/fan-out delivery. Each tick starts a fresh run; cron state is profile-scoped. Webhooks require HMAC/timestamp replay protection or an equivalent authenticated transport and fail closed outside explicit test fixtures. Automation-created tasks and sessions use the same idempotency and approval contracts as interactive requests.

## 6. Phased implementation

### Phase 3A — Finish the knowledge workbench

Complete the original Phase 3 services and views before adding operational breadth:

- reusable project/service assembly;
- real Brief, Explore, Context Lens, Impact, Knowledge, Timeline, and Setup data;
- safe source preview;
- cancellable indexing/research jobs and the initial SSE substrate;
- knowledge review flows;
- localization/pseudolocale, accessibility, visual regression, export, and guided fixtures;
- complete API schema and independent acceptance review.

**Exit:** every original Phase 3 journey works against real project data, browser tests cover disconnect/cancel/security, and no view owns domain logic.

### Phase 3B — Provider-neutral agent kernel

1. Define global/profile/project configuration resolution and atomic persistence.
2. Add durable session/message/tool-call/usage/checkpoint storage with migration, backup, repair, export, and FTS search.
3. Extend capability manifests into executable tool registrations and composable toolsets.
4. Implement provider-neutral chat-completion and tool-call interfaces; ship Ollama/OpenAI-compatible adapters first.
5. Build byte-stable prompt assembly and project-context hashing.
6. Add skill discovery/full-load, procedural-memory separation, and provenance.
7. Add cancellation, mid-turn steering queue, background process handles, bounded delegation, and compaction checkpoints.
8. Add explicit approvals and writable-scope policies.
9. Expose the same session/run services through CLI, MCP, and authenticated workbench endpoints.

**Exit:** one session can be started, steered, cancelled, resumed, searched, exported, and observed live; tools are permissioned and stable for the session; no provider secret enters project state or event payloads.

### Phase 3C — Durable Kanban and live operations

1. Implement board path resolution, slug validation, per-board durable DBs, migrations, backup, integrity check, and repair.
2. Implement task, dependency, comment, event, run, approval, attachment, artifact, and subscription repositories.
3. Implement the tested lifecycle state machine and typed blockers.
4. Add transactional event append and replayable SSE.
5. Implement native Prowl worker tools and task-scoped prompt guidance.
6. Implement CAS dispatcher claims, dependency promotion, scheduling, leases, heartbeats, PID/process observation, runtime caps, reclaim, retries, and circuit breakers.
7. Implement rooted scratch/dir/worktree workspace policy and cleanup.
8. Add native Prowl worker lane and opt-in external runner adapters.
9. Add CLI (`board`, `task`, `dispatch`, `watch`, `diagnostics`, `runs`) over the shared kernel.
10. Add workbench board, task drawer, dependencies, comments, review, runs/logs, workers, diagnostics, and live invalidation.
11. Add idempotent automation creation and completion/block notifications.
12. Add concurrency, crash, restart, migration, redaction, and browser reconnect tests.

**Exit:** a user can create a review-gated task, observe a worker claim/run/heartbeat live, steer or block it, approve evidence, survive a server restart, and audit every transition without direct DB access.

### Phase 4 — Obsidian on shared contracts

Proceed with the original Phase 4, adding optional session/task context panes through the versioned bridge. Obsidian must not open operational SQLite files directly.

### Phase 5 — Memory, research, and automation

Proceed with original memory/research work and add:

- session-to-memory proposals with explicit review;
- retention and privacy policy by event/session/artifact class;
- cron/webhook automation launching sessions or idempotent tasks;
- notification subscriptions;
- verification ledgers and experiment checkpoints;
- research jobs as resumable task workflows.

### Phase 6 — Ecosystem and plugins

Proceed with original adapters/localization/catalog work and add:

- backend plugin lifecycle and capability registration;
- frontend plugin manifest, constrained SDK, page/slot registration, and authenticated fetch/stream helpers;
- typed runner/channel/provider interfaces;
- compatibility fixtures and version negotiation;
- one representative third-party-style plugin proving no core-file edits are required.

### Phase 7 — Broader Hermes-parity surfaces

After the shared contracts are stable, implement or explicitly waive:

- profile/session/model/tool/skill/plugin/config management pages;
- embedded semantic chat/run view;
- system logs, health, update, and repair actions;
- remote authenticated dashboard mode with TLS/proxy trust and short-lived stream tickets;
- TUI/ACP/stdio bridge boundaries;
- desktop packaging and mobile-safe read-only behavior;
- representative messaging/gateway adapter and pairing contract;
- usage/cost analytics with local-only defaults and no outbound telemetry;
- import/export/migration for sessions, profiles, skills, tasks, and portable knowledge.

Do not claim “full parity” from route counts. Each selected capability needs a cross-surface behavior contract, real-path test, security test, migration story, and documentation.

## 7. Verification strategy

### Unit/property tests

- lifecycle transition table and forbidden transitions;
- dependency-cycle rejection;
- CAS claim race with exactly one winner;
- stale lease/run termination rejection;
- idempotency under retries;
- event ordering, replay, gap/reset, and redaction;
- prompt/toolset snapshot stability;
- rooted workspace and symlink escape rejection;
- approval/review policy invariants.

### Integration tests

- real SQLite WAL contention with concurrent claimers/readers;
- process spawn, exit, crash, timeout, heartbeat, and reclaim;
- restart between commit and publish with cursor recovery;
- session resume/compaction without message/tool-call corruption;
- provider adapter against deterministic fake OpenAI-compatible/Ollama servers;
- task worker using actual model-tool schemas against the task kernel;
- CLI/MCP/API parity over one fixture.

### Browser tests

- real compiled Go binary and committed frontend bundle;
- snapshot → event → refetch reconciliation;
- disconnect/reconnect from cursor and server restart;
- board/profile switching without cross-scope data bleed;
- keyboard/touch task movement with transition validation;
- review/approval, comments, attachments, diagnostics, cancellation;
- storage/token leak checks, CSP, hostile Host/Origin/fetch-site tests;
- axe, contrast, reduced motion, pseudolocale, viewport, and visual snapshots.

### Performance targets

Targets remain provisional until measured:

- event commit-to-visible p95 under 500 ms on loopback;
- board snapshot p95 under 200 ms at 10,000 tasks with pagination/virtualization;
- no subscriber may allocate unbounded memory;
- idle dispatcher/dashboard CPU near zero rather than fixed 300 ms DB polling;
- tool schema and prompt prefix unchanged across ordinary turns;
- crash/restart loses no committed state or terminal run outcome.

## 8. First shippable connected slice

After Phase 3A acceptance, the first operations slice is:

1. operational DB and typed event ledger;
2. one profile and one resumable session;
3. executable `read-only-context` toolset;
4. one board with task/comment/dependency/run tables;
5. native Prowl worker lane;
6. authenticated SSE replay;
7. workbench board plus live run detail;
8. explicit complete/block/review outcomes;
9. crash/restart and cursor-recovery tests.

This slice proves the full thesis: deterministic project context enters a named agent session; a durable task coordinates the work; a human sees real progress; the worker produces structured, cited evidence; and the result can become reviewed portable knowledge.

## 9. Explicit non-goals and deferred breadth

- No literal copy of Hermes branding, layout, generated JavaScript, or Python modules.
- No attempt to ship every Hermes model provider or messaging platform before interfaces stabilize.
- No direct frontend/Obsidian access to SQLite.
- No task status inferred from prose or process exit alone.
- No credentials in network request URLs/query strings, events, server logs, task metadata, profiles, or project files. The local launch fragment is the sole delivery exception and follows the redacted/manual-output rules above.
- No global autonomous mode without scope, budget, verifier, approval, and rollback policy.
- No plugin that bypasses the application services or authentication boundary.
- No full-parity claim until the final plan audit records implemented, adapted, intentionally deferred, and rejected items.

## 10. Final audit evidence

The final requirement audit must include, for every item in the canonical plan and this expansion:

- requirement ID;
- implementation path;
- unit/integration/browser/security test path;
- executed command and result;
- migration/rollback evidence;
- documentation path;
- independent review disposition;
- commit containing the verified behavior;
- explicit blocker or waiver when incomplete.
