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
- [per-board WAL/CAS coordination](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/hermes_cli/kanban_db.py#L49-L67), [CAS claim creation](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/hermes_cli/kanban_db.py#L3989-L4067), and [heartbeat/stale-reclaim guardrails](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/hermes_cli/kanban_db.py#L4186-L4275);
- [snapshot cursor](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/plugins/kanban/dashboard/plugin_api.py#L444-L508), [board-pinned polling](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/plugins/kanban/dashboard/plugin_api.py#L2232-L2236), and [stream cursor/replay](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/plugins/kanban/dashboard/plugin_api.py#L2515-L2573);
- [deterministic FTS session discovery/scroll/browse](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/tools/session_search_tool.py#L5-L23);
- [authenticated, filtered, bounded, rate-limited, replay-aware webhook ingress](https://github.com/NousResearch/hermes-agent/blob/c4f5a45d5d9903998fb318ac6f3c5e6623e60445/gateway/platforms/webhook.py#L1-L30).

Authoritative documentation:

- <https://hermes-agent.nousresearch.com/docs/user-guide/features/kanban>
- <https://hermes-agent.nousresearch.com/docs/user-guide/features/kanban-worker-lanes>
- <https://github.com/NousResearch/hermes-agent/tree/c4f5a45d5d9903998fb318ac6f3c5e6623e60445>

After inspection, upstream `main` advanced to `53bdcacf119f1172c77f91d223933754ed0c2c76`. The intervening diff changed only `hermes-desktop/src/renderer/hooks/useMessageStream.ts` and `tests/gateway/test_session_mirror.py`; none of the cited agent, tool, Kanban, or dashboard sources changed. The evidence contract therefore remains pinned to the exact inspected revision.

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

This ledger is normative for every behavior found in the pinned-source audit. New upstream discoveries receive a new ID; IDs are never renumbered or silently removed. “Comprehensive” means all IDs through the latest recorded audit have a disposition and executable acceptance test; it never means uninspected upstream behavior is implicitly covered.

| ID | Behavior contract | Disposition / target |
|---|---|---|
| HP-001 | Provider-neutral completion, streaming, tool calls, usage, retries | Implement Phase 3B |
| HP-002 | Named state-namespace profiles with model/toolset/skill/memory policy; no false sandbox claim | Implement Phase 3B |
| HP-003 | Durable sessions/messages/tool trajectories and source metadata | Implement Phase 3B |
| HP-004 | Search, resume, branch/checkpoint, export, repair, retention | Implement Phase 3B/5 |
| HP-005 | Stable semantic prompt prefix and session-pinned configuration | Implement Phase 3B0/3B |
| HP-006 | Executable tool registry with schema, handler, availability, permission, and result bound | Implement Phase 3B by extending Prowl capability manifests |
| HP-007 | Context-gated composable toolsets with zero unused schema footprint | Implement Phase 3B |
| HP-008 | Progressive skill metadata discovery and explicit full-body load as an ordinary tool result | Implement Phase 3B0/3B |
| HP-009 | Scoped user memory separated from skills, sessions, tasks, and project knowledge | Implement Phase 3B/5 |
| HP-010 | Session search before asking users to repeat retrievable context | Implement Phase 3B/5 |
| HP-011 | Context budgeting/compaction with original durable history preserved | Implement Phase 3B |
| HP-012 | Mid-turn steering, queued follow-up, cancellation, and status events | Implement Phase 3B |
| HP-013 | Tracked background processes with bounded logs and completion notification | Implement Phase 3B |
| HP-014 | Bounded fork/join subagents with depth/context isolation and capability/permission subset enforcement | Implement Phase 3B |
| HP-015 | Explicit risky-action approvals and writable/network/process scope | Improve and implement Phase 3B |
| HP-016 | Verification evidence ledger and finish-with-real-output enforcement | Implement Phase 3B/5 |
| HP-017 | Multiple isolated task boards with validated slugs and metadata | Implement Phase 3C |
| HP-018 | Durable task state machine and optimistic versioning | Implement Phase 3C |
| HP-019 | Acyclic dependencies, fan-out/fan-in, and automatic readiness | Implement Phase 3C |
| HP-020 | Scheduled tasks and automation-safe idempotent creation | Implement Phase 3C/5 |
| HP-021 | Durable human/agent comments and structured handoffs | Implement Phase 3C |
| HP-022 | Transactional per-store outboxes, scoped cursor replay, publisher watermark/sweep, retention reset | Improve and implement Phase 3C0/3C |
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
| HP-048 | Minimal versioned stdio bridge (framing, cancellation, capability negotiation, compatibility fixtures) | Required Phase 4 prerequisite; broader ACP Phase 7 |
| HP-049 | TUI and desktop packaging | Defer until Phase 7 contract review |
| HP-050 | Messaging gateway, pairing, and home-channel delivery | Implement interface plus one reference adapter Phase 7; broad channels optional |
| HP-051 | Voice, browser/computer use, smart-home, and media breadth | Capability/plugin ecosystem, not core parity gate |
| HP-052 | Local-only usage/cost analytics and no unconsented telemetry | Implement Phase 7 |
| HP-053 | Import/export/migration across profiles, sessions, skills, tasks, and knowledge | Implement Phase 5–7 |
| HP-054 | Setup/doctor/docs that expose capability, security, and lifecycle truth | Implement continuously and audit finally |
| HP-055 | Context-source inventory and precedence across system policy, profile identity/SOUL, user profile, memory, project instructions, and task context | Implement Phase 3B0/3B |
| HP-056 | Root and nested project-context discovery, rooted scope, and local precedence | Implement Phase 3B |
| HP-057 | Immutable session-start prompt/tool/profile/context snapshot with hashes and provenance | Implement Phase 3B0 |
| HP-058 | Role-safe per-turn overlays that never mutate the cached system prefix | Implement Phase 3B0/3B |
| HP-059 | Trusted-instruction versus untrusted source/task/attachment/web/tool-output classification and prompt-injection boundary | Implement Phase 3B0/3B |
| HP-060 | Inspectable model-exposure manifest: exposed/omitted context, tools, skills, policies, hashes, provenance, and policy reasons without secret values | Implement API/CLI in Phase 3B0; UI Phase 3B |
| HP-061 | Skill resolution, categorized/qualified names, local-over-external precedence, and provenance | Implement Phase 3B |
| HP-062 | Skill readiness from required environment, commands, platform, disabled state, and availability reasons | Implement Phase 3B |
| HP-063 | Path-safe create/patch/edit/delete and supporting-file mutation with profile authorization | Implement Phase 6; management UI Phase 7 |
| HP-064 | Plugin-qualified skill discovery and collision-safe resolution | Implement Phase 6 |
| HP-065 | Profile-scoped skill roots and cross-profile isolation | Implement Phase 3B; mutation controls Phase 6 |
| HP-066 | Skill cache/reload semantics: edits affect new sessions unless explicitly restarted | Implement Phase 3B |
| HP-067 | Skill import/export with provenance and conflict handling | Implement Phase 6/7 |
| HP-068 | Triage specification that converts an underspecified card into reviewable objective/acceptance/capability fields | Implement post-kernel Phase 3C1 |
| HP-069 | Automatic decomposition with transactional parent/child/link creation and cycle/idempotency checks | Implement post-kernel Phase 3C1 |
| HP-070 | Explicit swarm fan-out, verifier, and synthesizer workflow | Defer until HP-069 and single-worker evidence gates pass; reassess Phase 7 |
| HP-071 | Project-linked task workspaces/worktrees and source revision identity | Implement Phase 3C1 over HP-029 |
| HP-072 | Policy-controlled auto-promotion of eligible child tasks | Implement Phase 3C1; disabled by default |
| HP-073 | Auto-decompose/orchestrator configuration with bounded fan-out/depth/budget | Implement Phase 3C1; disabled by default |
| HP-074 | Versioned workflow-template fields and validation | Implement Phase 3C1/6 |
| HP-075 | Durable principal/profile/surface/delegated-run identity and server-derived actor attribution | Schema prerequisite before Phase 3B0 |

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
| Launch bootstrap and stream subscribers | Single-use nonce registry plus bearer/subscribers in process memory | The fragment contains only a 60-second single-use nonce. `POST /api/v1/auth/bootstrap` atomically consumes it and mints a distinct bearer. Automatic launch prints only a redacted origin; non-browser handoff uses interactive reveal or a mode-`0600` one-time file/FD, never ordinary stdout. Neither value is persisted by Prowl. |

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
- `internal/events` — typed scoped cursor/envelope interfaces, per-authority outbox adapters, replay, publisher watermarks/sweeps, broker, backpressure, gap/reset semantics. This is the sole event package from Phase 3A onward.
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

### 5.4 Scoped outbox and event contract

There is no scalar global cursor and no claimed total order across SQLite files. Every cursor is:

```text
{stream_scope, scope_id, epoch, sequence}
```

- `stream_scope` identifies one authority (`project-job`, `operations`, or `board` initially);
- `scope_id` is the rooted workspace ID, local-operator operational namespace, or board ID;
- `epoch` changes when that authority is restored/recreated so old cursors cannot alias new rows;
- `sequence` is monotonic only inside that authority's outbox transaction boundary.

Each event also has schema version, immutable event ID/timestamp, resource kind/ID/version, event kind, server-derived principal/profile/surface/delegated-run attribution, correlation IDs, and redacted size-bounded metadata. Board state and board outbox commit in the same board DB transaction. Session/approval state and operational outbox commit in the global operational DB transaction. Project jobs and project outbox commit in their project job authority. No transaction spans those stores. Cross-domain APIs return independently cursor-tagged streams and merge by timestamp/event ID only as a presentation order, never as a concurrency or causality guarantee.

Publishing occurs after commit. Every authority records a durable publisher watermark. A post-commit signal and a bounded sweep while subscribers exist publish rows above that watermark, so a commit-before-publish failure reaches an already-connected client without requiring reconnect. There is no fixed sub-second idle poll. Slow clients receive `reset`/gap with snapshot URI/current epoch instead of unbounded buffering. Retention records the oldest retained sequence; an older/mismatched cursor gets a per-stream reset and canonical snapshot refetch.

SSE clients use authenticated `fetch` with the bearer header and explicit cursor tuple. Keepalive comments contain no data. Disconnect cancels subscriber context. Event payloads never contain credentials, full prompts, raw private file content, or unbounded logs. Required tests force commit success followed by publication failure, keep the subscriber connected, and assert bounded sweep delivery; they also cover rollback silence, scope mismatch, epoch reset, retention expiry, queue overflow, and restart replay.

### 5.5 Agent prompt, AI-context, skill, and exposure contract

Before the first provider call, a session pins an immutable snapshot of provider/model limits, core prompt version, authenticated principal ID, active profile identity/SOUL, user-profile and memory selections, root project-instruction manifest, active tool schemas/generation, metadata index for discoverable skills, force-preloaded task-skill bodies, permission/approval policy, and hashes/provenance for every included source. Profile identity is not the authenticated principal.

Normative precedence from strongest to weakest is: executable system/security policy → active profile identity/SOUL and surface policy → explicit user-profile policy → scoped durable memory → rooted project instructions → task/worker instructions → untrusted source/web/attachment/tool-output content. A lower tier cannot redefine the authority of a higher tier. Root project files enter the frozen session snapshot; nested instructions are discovered only on rooted subtree entry and arrive as role-safe per-turn overlays marked untrusted project instructions. Other volatile context packets and steering are per-turn messages/events, never system-prompt mutation.

Stable prompt content consists of the invariant core, frozen identity/policy/context snapshot, active tool schemas, skill metadata index, and explicitly force-preloaded task skills. Ordinary `skill_view` loads return full skill bodies as normal tool results in durable conversation history. They do not change the system prompt, tool schemas, or snapshot. Skill edits/re-resolution affect new sessions only unless the operator explicitly restarts the session. Compaction may preserve a loaded skill result's semantic effect in a checkpoint but never rewrites original durable history or retroactively mutates the frozen prefix.

Every provider request produces an inspectable exposure manifest containing snapshot ID; hashes/provenance/scope/precedence for context and policies; exact tool schemas and skill metadata/body IDs exposed; per-turn overlays; omitted/disabled sources; and policy/readiness/permission reasons. APIs and UI redact secret values while showing that a secret reference was used. Byte-level tests compare the system prompt and tool-schema JSON before and after mid-session `skill_view`, nested-context discovery, steering, and compaction; only role-safe conversation/overlay messages may differ.

Normal sessions receive no Kanban schema. Task workers receive task-scoped show/comment/heartbeat/block/complete tools. Orchestrators receive list/create/link/reassign/unblock tools but implementation capabilities are excluded by default.

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

Delegation authorization is intersection-only after expanding aliases/composite toolsets:

```text
child_capabilities ⊆ parent_effective_capabilities
child_permissions  ⊆ parent_effective_permissions
child_approval_policy may only become stricter
```

The repository layer denies durable board mutation from delegated fork/join children even if a handler is reached indirectly. Children default to deny interactive and risky approvals and lose delegation/Kanban-sensitive capabilities unless the parent policy explicitly retains a safe subset. Tests expand every alias before intersection and prove a requested composite cannot widen access.

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

### 5.8.1 Principal and actor model (schema prerequisite)

Persist actor attribution correctly from the first Phase 3B0 migration:

- `principal_id`: authenticated security principal; local mode maps to one generated durable local-operator principal;
- `profile_id`: active agent/profile configuration snapshot, never an authentication principal;
- `surface_id`: CLI, MCP client, workbench browser session, automation, plugin, or bridge identity;
- `delegated_identity`: optional worker/run ID and parent run/principal chain;
- `owner_principal_id` and authorization scope on every project/board/session authority.

HTTP/MCP/CLI adapters derive principal and surface server-side. Clients may request a profile but cannot submit authoritative `actor`, `principal_id`, owner, or delegated chain fields. Events, approvals, sessions, idempotency scope, tasks, and run rows store these IDs from day one. Local mode grants the local operator only inside owned scopes. Remote mode remains disabled until Phase 7 policy enforcement, but no retrofit may be required to distinguish actor, profile, surface, worker, project, or board ownership.

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

**Exit oracle:** execute Phase 3A plan Task D2. Its tagged Go/race/vet, frontend, compiled-binary Chromium, route-inventory, canonical Context Lens projection, scoped reconnect/cancel/security, three-fixture tour, and measured newcomer rubric all pass with the exact volatile-field exclusions and thresholds stated there. No view owns domain logic and no route returns placeholder data.

### Phase 3B0 — Bounded agent-kernel tracer

This slice is a hard gate before Phase 3B breadth. It implements only one immutable local profile snapshot, one deterministic fake/OpenAI-compatible provider, one resumable session, one read-only context toolset, frozen prompt/exposure semantics, and one operational scoped event stream. No FTS, broad repair UI, background processes, delegation, provider breadth, general skill mutation, or management UI belongs here.

| Task | Exact implementation and test ownership | Route/CLI/schema ownership | Gate |
|---|---|---|---|
| B0.1 principal + operations v1 | Create `internal/operations/{store.go,store_test.go,principal.go,principal_test.go}` and `internal/operations/migrations/001_principal_sessions_outbox.sql`; create `internal/operations/testdata/{v0.sql,v1.sql}` | Schema `prowl.operations/v1`; principals, sessions, operations outbox, authority epoch, retention floor, and publisher watermark share one operations DB; local principal and owner/surface columns; no client actor fields | `go test -race -tags sqlite_fts5 ./internal/operations -run 'Test(MigrationV1|Principal|ClientActorRejected)' -count=1` → PASS |
| B0.2 immutable profile/prompt snapshot | Create `internal/profile/{model.go,snapshot.go,snapshot_test.go}` and `internal/agent/{prompt.go,prompt_test.go,exposure.go,exposure_test.go}` | One built-in `local` profile; HP-055–060 precedence/snapshot/exposure; no mutable management route | `go test ./internal/profile ./internal/agent -run 'Test(ProfileSnapshot|PromptBytes|ExposureManifest|TrustPrecedence)' -count=1` → byte fixtures PASS |
| B0.3 resumable session ledger | Create `internal/session/{model.go,repository.go,repository_test.go,service.go,service_test.go}` and `internal/session/testdata/session_v1.json`; create `internal/cli/{session.go,session_test.go}` | `POST /api/v1/sessions`, `POST /api/v1/sessions/{id}/turns`, `GET /api/v1/sessions/{id}`, `GET /api/v1/sessions/{id}/exposure`; CLI `session start|turn|show|resume`; state/event writes share operations DB transaction | `go test -race -tags sqlite_fts5 ./internal/session -run 'Test(CreateTurnResume|Restart|Exposure|Actor)' -count=1` → PASS |
| B0.4 provider + read-only tools | Create `internal/agent/{provider.go,provider_fake_test.go,loop.go,loop_test.go}` and `internal/toolruntime/{registry.go,registry_test.go,readonly_context.go,readonly_context_test.go}` | Deterministic scripted fake plus OpenAI-compatible wire interface; only `search_context`, `get_context`, and rooted bounded `read_source`; no side effects | `go test -race -tags sqlite_fts5 ./internal/agent ./internal/toolruntime -run 'Test(FakeProvider|OpenAIWireFixture|ReadOnlyContext|PermissionDenied)' -count=1` → PASS |
| B0.5 skill prompt stability | Create `internal/skill/{discover.go,discover_test.go,view.go,view_test.go}` and fixtures `internal/skill/testdata/{profile,external}/**`; modify only B0-owned prompt tests | Metadata index and force-preloaded bodies freeze at start; normal `skill_view` body is a tool result; no mutation | `go test ./internal/agent ./internal/skill -run 'Test(SkillViewDoesNotMutatePrompt|SkillIndexFrozen|SkillProvenance)' -count=1` → prompt/tool-schema bytes unchanged |
| B0.6a operations API + stream | Create `internal/events/{operations.go,operations_test.go}`, `internal/workbench/{session_api.go,session_api_test.go,operations_events.go,operations_events_test.go}`; modify `internal/workbench/api.go` | Register only B0.3 routes plus authenticated `GET /api/v1/events?stream_scope=operations&scope_id=&epoch=&sequence=`; cursor is `{operations,<local-principal>,epoch,sequence}` | `go test -race -tags sqlite_fts5 ./internal/operations ./internal/events ./internal/session ./internal/workbench -run 'Test(SessionRoutes|OperationsSSE|OutboxTransaction|CommitBeforePublish|ConnectedSubscriberSweep|RestartReplay)' -count=1` → PASS, including forced broker failure while a browser subscriber remains connected |
| B0.6b session tracer UI | Create `web/src/features/session/{SessionTracer.tsx,SessionTracer.test.tsx}`; modify `web/src/transport/{api.ts,events.ts}`, `web/src/app/App.tsx` and tests | Snapshot + scoped invalidation/refetch; no browser-owned session truth | `cd web && npm test -- --run src/features/session src/transport/events.test.ts src/app/App.test.tsx && npm run build` → PASS |
| B0.6c operations runtime composition | Create `internal/application/{operations_runtime.go,operations_runtime_test.go}`; modify `internal/cli/{open.go,open_test.go}` and `internal/workbench/{handler.go,handler_test.go}` through the serialized post-Phase-3A owner handoff | The sole operations DB is `$XDG_DATA_HOME/prowl-agent/operations.db`. Production startup opens it, resolves the local principal, constructs session/profile/agent services, starts the operations outbox publisher and connected-subscriber sweeper, and injects the same instances into the workbench handler. Shutdown stops HTTP mutation intake, cancels and joins sweeper/publisher goroutines, closes session services and the operations store, then closes the project; restart reopens the same DB and resumes watermark/outbox/session state without rebuilding actor truth from browser input. | `go test -race -tags sqlite_fts5 ./internal/application ./internal/cli ./internal/workbench ./internal/operations ./internal/session -run 'Test(OperationsRuntimeComposition|OperationsDBPath|OperationsRuntimeShutdownJoin|OperationsRuntimeKillRestart)' -count=1` → PASS with goroutine leak checks and committed-state recovery |
| B0.6d compiled crash acceptance | Create `web/e2e/session-tracer.spec.ts`; regenerate `web/dist/**` as sole serialized bundle owner after B0.6c | Compiled Go binary uses the production operations runtime; process kill/restart, cursor replay/reset, frozen exposure resume | `go test -tags sqlite_fts5 ./internal/operations ./internal/profile ./internal/session ./internal/agent ./internal/toolruntime ./internal/skill ./internal/application ./internal/workbench ./internal/cli -count=1 && cd web && npm run check && npx playwright test e2e/session-tracer.spec.ts` → PASS |

**B0 executable exit oracle:** run the B0.6a–B0.6d gates against `testdata/workbench/go-auth-service`. The browser starts a session, fake provider invokes `search_context`, UI shows a cited source, the Go server is killed after the committed turn, and a new process resumes the same session and exposure manifest from `$XDG_DATA_HOME/prowl-agent/operations.db`. Assertions require identical frozen prompt/tool-schema hashes, server-derived principal/profile/surface fields, joined runtime goroutines on graceful shutdown, no write-capable tool, no secret value in DB/events/browser storage, and operation cursor replay/reset correctness.

### Phase 3B — Provider-neutral agent-kernel breadth

Only after B0 passes:

1. Add atomic global/profile/project configuration overlays and secret references.
2. Extend sessions with usage, steering, compaction, branch/checkpoint, backup, repair, export, retention/deletion, then FTS search.
3. Extend executable toolsets with availability, result bounds, explicit approvals, and writable/network/process scopes.
4. Add Ollama and production OpenAI-compatible adapters without changing session services.
5. Complete HP-055–060 context/exposure UI and byte-level fixtures.
6. Complete skill HP-061, HP-062, HP-065, and HP-066. HP-063/064/067 stay in Phases 6–7 as ledgered.
7. Add cancellation, steering queue, bounded background processes, and compaction checkpoints.
8. Add bounded delegation with expanded-alias subset enforcement, deny-by-default child approvals, and repository-level board mutation denial.
9. Expose the same services through CLI, MCP, and authenticated workbench adapters.

**Exact AI-context and skill breadth ownership:**

- **B-CTX1 (HP-056/058/059/060):** create `internal/agent/{context_sources.go,context_sources_test.go,turn_overlay.go,turn_overlay_test.go}`; extend `internal/agent/{exposure.go,exposure_test.go}`; create `internal/workbench/exposure_api.go`, `internal/workbench/exposure_api_test.go`, and `web/src/features/session/ExposureManifest.tsx` with its test. Own `GET /api/v1/sessions/{id}/exposure`. Gate: `go test ./internal/agent ./internal/workbench -run 'Test(ContextPrecedence|NestedRootScope|RoleSafeOverlay|TrustBoundary|ExposureManifest)' -count=1` → PASS with byte fixtures and secret-value redaction.
- **B-SK1 (HP-061/062/065/066):** create `internal/skill/{resolve.go,resolve_test.go,readiness.go,readiness_test.go,roots.go,roots_test.go,cache.go,cache_test.go}`. Gate: `go test ./internal/skill -run 'Test(QualifiedResolution|LocalPrecedence|Readiness|PlatformAndDisabled|ProfileIsolation|NewSessionReload)' -count=1` → PASS; cross-profile paths and traversal are denied.
- **S6-SK1 (HP-063):** create `internal/skill/{mutate.go,mutate_test.go,supporting_files.go,supporting_files_test.go}` and CLI adapter `internal/cli/{skill.go,skill_test.go}`. Gate: `go test ./internal/skill ./internal/cli -run 'Test(SkillCreatePatchEditDelete|SupportingFilePathSafety|ProfileAuthorization|AtomicRollback)' -count=1` → PASS.
- **S6-SK2 (HP-064):** create `internal/skill/{plugin.go,plugin_test.go}` with fixtures `internal/skill/testdata/plugins/**`. Gate: `go test ./internal/skill -run 'Test(PluginQualifiedSkill|Collision|PluginProvenance)' -count=1` → PASS.
- **S6-SK3 (HP-067):** create `internal/skill/{archive.go,archive_test.go}`. Gate: `go test ./internal/skill -run 'Test(SkillImportExport|Conflict|ProvenanceRoundTrip|ArchivePathSafety)' -count=1` → PASS.
- **S7-SKUI (HP-040 only, no new semantics):** create `web/src/features/skills/{SkillsPage.tsx,SkillsPage.test.tsx}` and `internal/workbench/{skills_api.go,skills_api_test.go}` over S6 services. Compiled-binary `web/e2e/skills-management.spec.ts` proves API/CLI/UI parity, profile isolation, readiness reasons, conflict handling, and no secret-value exposure.

**3B executable exit oracle:** `go test -race -tags sqlite_fts5 ./internal/operations ./internal/profile ./internal/session ./internal/toolruntime ./internal/skill ./internal/agent ./internal/approval ./internal/workbench ./internal/mcp ./internal/cli -run 'Test(SessionLifecycle|PromptAndSchemaStable|SkillViewDoesNotMutatePrompt|ChildCapabilitySubset|Approval|SecretRedaction)' -count=1` exits 0, then compiled-binary `web/e2e/session-lifecycle.spec.ts` starts, steers, cancels, resumes, searches, exports, and observes one session across restart.

### Phase 3C0 — Bounded Kanban/live-operations tracer

This slice adds only one board, minimal task/run/review state, native Prowl worker, one board-scoped stream, and one live board/run view. It defers schedules, general repair, external lanes, broad attachments/subscriptions, automation, triage/decomposition, project worktrees, and management breadth.

| Task | Exact implementation and test ownership | Route/CLI/schema ownership | Gate |
|---|---|---|---|
| C0.1 board v1 + outbox | Create `internal/taskboard/{store.go,store_test.go,model.go,repository.go,repository_test.go}`; `internal/taskboard/migrations/001_board_tracer.sql`; `internal/taskboard/testdata/{v0.sql,v1.sql}`; extend only `internal/events/{cursor.go,outbox.go}` and tests | Schema `prowl.board/v1`: boards, tasks, dependencies, comments, runs, review decisions, outbox, publisher watermark; cursor `{board,board-id,epoch,sequence}` | `go test -race -tags sqlite_fts5 ./internal/events ./internal/taskboard -run 'Test(BoardMigrationV1|TaskOutboxTransaction|CommitBeforePublish|CursorReset)' -count=1` → PASS |
| C0.2 minimal state machine | Create `internal/taskboard/{lifecycle.go,lifecycle_test.go,service.go,service_test.go}` | `triage→ready→running→review→done` plus `blocked`; expected version, typed blocker, one terminal run outcome, immutable review decision | `go test -race -tags sqlite_fts5 ./internal/taskboard -run 'Test(Lifecycle|ReviewGate|ExpectedVersion|TerminalOutcome)' -count=1` → PASS |
| C0.3 native worker + task-scoped agent | Create `internal/runner/{runner.go,native.go,native_test.go}`, `internal/dispatcher/{claim.go,claim_test.go,worker.go,worker_test.go}`, `internal/agent/{task_worker.go,task_worker_test.go}`, `internal/toolruntime/{task_tools.go,task_tools_test.go}`, and `internal/session/{run_evidence.go,run_evidence_test.go}` | Dispatcher assembles the B0 provider/session loop with read-only context plus only `show/comment/heartbeat/block/complete`; alias-expanded toolset is an intersection, never a union; cited evidence and terminal outcome append transactionally to the run | `go test -race -tags sqlite_fts5 ./internal/runner ./internal/dispatcher ./internal/agent ./internal/toolruntime ./internal/session -run 'Test(NativeWorker|TaskToolIntersection|TaskScopeDenied|CitedRunEvidence|ExactlyOneClaim|Heartbeat|CrashReclaim|StaleRunRejected)' -count=1` → PASS |
| C0.4 board API/CLI/SSE | Create `internal/workbench/{board_api.go,board_api_test.go}`; modify `internal/workbench/api.go`; create `internal/cli/{board.go,board_test.go}` | `POST /api/v1/boards`, `GET /api/v1/boards/{id}`, `POST /api/v1/boards/{id}/tasks`, `GET /api/v1/boards/{id}/tasks/{task}`, `POST /api/v1/boards/{id}/tasks/{task}/comments`, `POST /api/v1/boards/{id}/tasks/{task}/claim|heartbeat|block|complete|review`, `GET /api/v1/boards/{id}/runs/{run}`, `GET /api/v1/events?stream_scope=board...`; CLI equivalents include task show/comment | `go test -race -tags sqlite_fts5 ./internal/workbench ./internal/cli -run 'Test(BoardTracerRoutes|BoardCLIParity|BoardSSE)' -count=1` → PASS |
| C0.5a live board UI | Create `web/src/features/board/{BoardTracer.tsx,BoardTracer.test.tsx,RunDetail.tsx,RunDetail.test.tsx}`; modify `web/src/transport/{api.ts,events.ts}`, `web/src/app/App.tsx` and tests | Authoritative snapshot + invalidation/refetch only | `cd web && npm test -- --run src/features/board src/transport/events.test.ts src/app/App.test.tsx && npm run build` → PASS |
| C0.5b board/worker runtime composition | Create `internal/application/{board_runtime.go,board_runtime_test.go}`; modify `internal/cli/{open.go,open_test.go}` and `internal/workbench/{handler.go,handler_test.go}` through the serialized handoff after B0.6c | The tracer's sole board DB is `$XDG_DATA_HOME/prowl-agent/boards.db`. Production startup opens operations then board stores, reconciles nonterminal leases, constructs the board service and native task-scoped worker, injects them into the handler, binds the listener, then starts the board publisher/sweeper and dispatcher. Graceful shutdown enters draining mode, stops new claims/mutations, cancels and joins dispatcher/native workers, cancels and joins publisher/sweeper goroutines, closes subscribers/listener, then closes board, operations, and project stores in reverse dependency order. Kill/restart reopens both exact DBs, resumes committed outboxes/watermarks, reconciles leases, and rejects stale worker tokens. | `go test -race -tags sqlite_fts5 ./internal/application ./internal/cli ./internal/workbench ./internal/taskboard ./internal/dispatcher ./internal/runner -run 'Test(BoardRuntimeComposition|BoardDBPath|RuntimeStartupOrder|RuntimeShutdownJoin|RuntimeKillRestart|StaleWorkerAfterRestart)' -count=1` → PASS with leak, order, and restart fixtures |
| C0.5c compiled worker/restart acceptance | Create `web/e2e/operations-tracer.spec.ts`; regenerate `web/dist/**` as sole serialized bundle owner after C0.5b | Compiled server uses production board/worker runtime; native worker kill/restart, stale worker denial, cited evidence, review completion | `go test -tags sqlite_fts5 ./internal/events ./internal/taskboard ./internal/runner ./internal/dispatcher ./internal/agent ./internal/toolruntime ./internal/session ./internal/application ./internal/workbench ./internal/cli -count=1 && cd web && npm run check && npx playwright test e2e/operations-tracer.spec.ts` → PASS |

**C0 executable exit oracle:** run C0.1–C0.5c. Compiled-binary Playwright creates one review-gated task, native worker claims and heartbeats, cited fake-provider evidence appears live, server/worker are killed at fixed checkpoints, and restart reopens `$XDG_DATA_HOME/prowl-agent/operations.db` plus `$XDG_DATA_HOME/prowl-agent/boards.db`, replays committed scoped events without reconnect-only assumptions, reconciles leases, and rejects stale worker completion. Local operator approval reaches `done`; exactly one claim wins; graceful shutdown joins all publishers/dispatchers/workers; no committed event/outcome is lost; all cursors remain board-scoped.

### Phase 3C — Durable Kanban breadth

After C0 passes, add schedules, complete repositories, attachments/artifacts/subscriptions, dispatcher policy, rooted scratch/trusted-directory/worktree modes, external runners, diagnostics, automation, and full CLI/MCP/UI. Extend the C0 schema with ordered migrations; do not replace the kernel or event package.

**3C executable exit oracle:** `go test -race -tags sqlite_fts5 ./internal/events ./internal/taskboard ./internal/runner ./internal/dispatcher ./internal/automation ./internal/workbench ./internal/mcp ./internal/cli -run 'Test(OperationsLifecycle|AdapterParity|ExactlyOneClaim|StaleRunRejected|CommitBeforePublish|RestartReplay|ScopedCursor|ReviewGate)' -count=1` exits 0 against `internal/taskboard/testdata/lifecycle_v1.json`, then compiled-binary `web/e2e/operations-lifecycle.spec.ts` executes create/claim/heartbeat/steer/block/resume/review/complete/restart. Canonical transition projections are equal across adapters after excluding only transport request IDs.

### Phase 3C1 — Pinned Kanban behavior after the kernel

| Task | Exact ownership | Executable gate |
|---|---|---|
| C1.1 specification (HP-068) | Create `internal/taskboard/{specification.go,specification_test.go}`; add `POST /api/v1/boards/{id}/tasks/{task}/specify` in `internal/workbench/{board_spec_api.go,board_spec_api_test.go}` | `go test -race -tags sqlite_fts5 ./internal/taskboard ./internal/workbench -run 'Test(Specification|SpecifyExpectedVersion|SpecifyAudit)' -count=1` → PASS |
| C1.2 decomposition (HP-069) | Create `internal/taskboard/{decompose.go,decompose_test.go}`; add `POST /api/v1/boards/{id}/tasks/{task}/decompose` in the C1.1 adapter | `go test -race -tags sqlite_fts5 ./internal/taskboard ./internal/workbench -run 'Test(DecomposeAtomic|DecomposeIdempotent|DecomposeCycle|DecomposeRollback)' -count=1` → PASS |
| C1.3 project-linked workspaces (HP-071) | Create `internal/runner/{project_workspace.go,project_workspace_test.go}` and extend `internal/taskboard/model.go` through serialized schema migration `internal/taskboard/migrations/004_project_links.sql` | `go test -race -tags sqlite_fts5 ./internal/runner ./internal/taskboard -run 'Test(ProjectRevisionIdentity|LinkedWorktreeRoot|WorktreeCleanup|ProjectScope)' -count=1` → PASS |
| C1.4 promotion/orchestrator policy (HP-072/073) | Create `internal/dispatcher/{promotion.go,promotion_test.go,orchestrator.go,orchestrator_test.go}` | `go test -race -tags sqlite_fts5 ./internal/dispatcher -run 'Test(AutoPromotionDisabled|PromotionEligibility|FanoutDepthBudget|OrchestratorIdempotency)' -count=1` → PASS |
| C1.5 workflow templates (HP-074) | Create `internal/taskboard/{template.go,template_test.go}` and fixtures `internal/taskboard/testdata/templates/v1/**` | `go test -tags sqlite_fts5 ./internal/taskboard -run 'Test(TemplateV1RoundTrip|TemplateUnknownFields|TemplateValidation)' -count=1` → PASS |

HP-070 swarm fan-out/verifier/synthesizer remains deferred until C1.1–C1.5 and independent single-worker evidence review pass because it multiplies claims, capability envelopes, and synthesis authority. Reassessment in Phase 7 must add a new exact task/gate before implementation; omission is not parity.

### Phase 4 — Minimal bridge prerequisite, then Obsidian

Before any Obsidian feature, create `internal/bridge/{protocol.go,server.go,server_test.go,compat_test.go}`, `internal/cli/bridge.go`, and `internal/cli/bridge_test.go` for `prowl-agent bridge --stdio`. The v1 protocol must define NDJSON framing, request/response/event IDs, cancellation, capability negotiation, maximum frame size, redaction, shutdown, and compatibility fixtures under `internal/bridge/testdata/v1/**`. Gate: `go test -race -tags sqlite_fts5 ./internal/bridge ./internal/cli -run 'Test(Framing|Cancellation|Capabilities|CompatibilityV1|Oversize|Shutdown)' -count=1` exits 0.

Only then proceed with the Obsidian plugin and optional session/task context panes over that bridge. Obsidian never opens SQLite directly. TUI, broad ACP, desktop packaging, and management UI remain Phase 7.

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
- broader ACP and TUI surfaces over the Phase 4 stdio v1 bridge;
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
- scoped cursor isolation, epoch/retention reset, and commit-before-publish delivery to an already-connected subscriber;
- prompt/toolset snapshot stability;
- context precedence/exposure manifests and mid-session `skill_view` prompt-byte stability;
- delegated child capability/permission subset and stricter-approval enforcement after alias expansion;
- server-derived principal/profile/surface/delegated-run attribution;
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

After Phase 3A Task D2 acceptance, execute **B0.1–B0.6d and C0.1–C0.5c only**, in that order. Those tables define exact files, migrations, routes, schemas, test commands, and crash checkpoints. Phase 3B/3C breadth is prohibited until both tracer exit oracles pass.

The connected acceptance composes the B0 and C0 fixtures: deterministic project context enters one immutable-profile session through the read-only toolset; one board task invokes the native worker; scoped operational and board outboxes drive the live UI; the worker produces cited evidence; local review completes the task; and kill/restart checkpoints preserve session, run, outcome, and cursor recovery. FTS, general repair, external lanes, broad attachments/subscriptions, automation breadth, provider breadth, and management UI are explicitly outside this slice.

## 9. Explicit non-goals and deferred breadth

- No literal copy of Hermes branding, layout, generated JavaScript, or Python modules.
- No attempt to ship every Hermes model provider or messaging platform before interfaces stabilize.
- No direct frontend/Obsidian access to SQLite.
- No task status inferred from prose or process exit alone.
- No credentials in network request URLs/query strings, events, server logs, task metadata, profiles, or project files. The local launch fragment carries only a short-lived single-use bootstrap nonce, not the API bearer, and follows the redacted/manual-handoff rules above.
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
