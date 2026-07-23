# Prowl Agent: Product Evolution Research and Implementation Plan

**Date:** 2026-07-23
**Repository:** `https://github.com/neur0map/prowl-agent`
**Inspected revision:** `5e103fc` (`main`)
**Status:** Research and planning; no product code changed

## Executive decision

Prowl should not become another coding agent, another chat wrapper, or a decorative graph viewer.

It should become **Prowl — a local knowledge compiler and context workbench for humans and AI agents**.

Prowl's durable product promise should be:

> Point Prowl at a project or knowledge workspace. It deterministically maps what exists, helps an agent compile sources into reviewable knowledge, and gives both humans and agents the right level of context—with provenance, freshness, and explicit control.

The differentiator is not “we have a code graph.” Many products now do. The defensible combination is:

1. **Deterministic truth:** code structure, files, links, Git changes, specs, and source anchors.
2. **Curated meaning:** portable OKF-compatible Markdown concepts, decisions, claims, playbooks, and research.
3. **Progressive context:** overview → neighborhood/timeline → cited detail, with an explicit token/attention budget.
4. **Two first-class views:** a visual, explanatory human workbench and a compact, structured agent interface over the same substrate.
5. **Local-first interoperability:** Obsidian, MCP, CLI, LSP, Git, and agent hooks without binding users to a model vendor.

A useful internal metaphor is **compiler, not database**:

```text
raw sources + code + specs + session evidence
                    │
                    ▼
       deterministic extraction and linking
                    │
                    ▼
       curated, reviewable knowledge layer
                    │
          ┌─────────┴─────────┐
          ▼                   ▼
 human workbench       agent context packets
 Obsidian / Web UI      MCP / CLI / skills
```

SQLite remains the acceleration and derived-index layer. It must not be the only durable memory.

---

## 1. What exists today

### Genuine strengths worth preserving

- A compact local Go core with SQLite/FTS5 and Tree-sitter.
- Incremental index refresh before queries; no mandatory watcher or cloud service.
- Structural graph operations: symbol lookup, relations, callers/callees, blast radius, entrypoints, clusters, hotspots, tests, changes, and health checks.
- Broad parser support already advertised for Go, Rust, Java, Kotlin, Ruby, C#, PHP, Dart, Elixir, TypeScript/TSX, JavaScript, Python, Lua, Bash/Fish, C/C++, QML, CSS/SCSS, Markdown, TOML, YAML, JSON/JSONC, INI, and Hyprland.
- Three delivery surfaces over one index: shell, MCP, and LSP.
- Cited file/line results and intentionally compact model-facing output.
- Local semantic search through optional Ollama rather than a mandatory hosted provider.
- A reasonably substantial Go test estate: source inspection found 179 `Test*` functions.
- The released binary successfully completed isolated `init`, `status`, and `overview` tests.

These are not rice-only capabilities. The implementation has already outgrown its original dotfiles identity; the UX and product model have not caught up.

### Current product/UX gaps found in the repository

#### Onboarding and distribution

- The README supports only Linux x86_64. That excludes a large share of Obsidian, macOS, Windows, and Apple Silicon users.
- `init` performs many unrelated writes in one operation: index, `AGENTS.md`, generic MCP files, Cursor, VS Code, OpenCode, OMP, Factory, editor snippets, and Helix config.
- Setup does not first show a change preview or let users select only the clients they use.
- The final message claims “MCP and editor LSP also configured” without a clear per-integration success/failure report.
- Optional AI setup can install software and pull models; this needs an explicit, visible consent boundary and a dry run.
- “Safe to rerun after a reboot” is confusing because the architecture claims no daemon and setup should not be a reboot task.

#### Human experience

- There is no GUI or native visual exploration surface.
- The status card is visually polished, but it is operational telemetry rather than understanding.
- The default query output is TOON, including for humans. In the isolated first run, `overview` emitted fields such as `clusters[0]:` and `null`, not a useful explanation of the tiny project.
- There is no human-friendly `--format human`/rich output contract separate from agent serialization.
- There are no guided tours, conceptual summaries, timeline views, source previews, confidence/freshness cues, or “why this is relevant” explanations.
- A raw force graph would not fix this; graph hairballs increase cognitive load without guided hierarchy.

#### Agent experience

- The MCP server exposes many individual tools but no first-class MCP Resources or Prompts.
- The 17-tool surface increases schema/discovery overhead and leaves workflow discipline to the model.
- There is no universal context-packet API that explains selection, token/byte cost, provenance, freshness, omissions, and next drill-down options.
- `AGENTS.md` is treated as an instruction payload instead of a small table of contents into progressively disclosed knowledge.
- Generated `AGENTS.md` became part of the first query's index in the isolated setup test. Generated integration files should be excluded or indexed only after explicit opt-in.
- Tool results are machine-efficient but not layered: an agent gets a complete result shape instead of index → context → detail.

#### Data and memory model

- Current storage models files, symbols, edges, resources, chunks, embeddings, and savings. It is excellent as a derived source index but insufficient as durable project memory.
- There is no first-class model for concepts, claims, decisions, rationales, evidence, temporal validity, review state, or agent observations.
- There is no canonical, Git-reviewable knowledge format. Important meaning would have to remain in a rebuildable SQLite database.
- There is no separation between deterministic facts and AI-authored interpretations.
- There is no memory inbox/review workflow or source-anchor staleness mechanism.

#### General-purpose and multilingual behavior

- `config.languages` is persisted and tested but not consumed by indexing; source search found no indexing path that reads it. This is a configuration contract bug.
- The default language list still emphasizes the project's rice/config origins while the parser supports far more mainstream languages.
- “Supported languages” currently means programming/config formats, not localized UI or multilingual natural-language knowledge.
- FTS/search behavior needs explicit Unicode, CJK, stemming, diacritic, and mixed-language test coverage.
- “violations,” `doctor`, keybind, command, and hardcoded-color checks mix universal repository health with rice-specific domain checks.

#### Product positioning

- The README leads with token savings and one-command code lookup. Those are useful proof points, not an emotionally compelling product.
- Savings estimates compare answer bytes with full cited file sizes. The repository documents this honestly, but this should not remain the primary success story.
- The current product has no memorable workflow that a user can show another person beyond terminal output.

### Execution limitation during this audit

The host did not have Go installed, so source-level Go tests could not be executed from the clone. I exercised the shipped nightly binary in an isolated temporary workspace instead. Before implementation begins, CI and a local Go toolchain should run `go test ./...`, race tests where practical, and release-target smoke tests.

---

## 2. Research conclusions

### 2.1 Google Open Knowledge Format (OKF)

Google Cloud introduced **Open Knowledge Format v0.1** on 2026-06-12. It formalizes the LLM-wiki pattern as a vendor-neutral directory of Markdown files with YAML frontmatter.

Core facts from the official spec:

- Each non-reserved Markdown concept has one required field: `type`.
- Recommended fields include `title`, `description`, `resource`, `tags`, and `timestamp`.
- Markdown links create a navigable knowledge graph.
- `index.md` provides progressive disclosure at any directory level.
- `log.md` records update history.
- Consumers must tolerate unknown types/fields and should not reject bundles merely for broken links or missing indexes.
- OKF defines representation and interoperability, not storage engines, agent runtimes, governance, or retrieval algorithms.

**Determination for Prowl:** adopt OKF as the durable interchange and authoring layer, not as a replacement for Prowl's graph/index.

Prowl can add namespaced metadata while preserving compatibility:

```yaml
---
type: Decision
title: Keep SQLite as derived storage
description: Durable knowledge remains Markdown; SQLite is rebuilt.
resource: file://docs/architecture/storage.md
tags: [architecture, storage]
timestamp: 2026-07-23T00:00:00Z
prowl:
  id: decision-storage-001
  status: accepted
  confidence: verified
  valid_from: 2026-07-23
  related: [architecture/context-layer]
  anchors:
    - path: internal/store/store.go
      line_start: 1
      content_hash: sha256:...
---
```

Do not fork OKF. Keep Prowl extensions under `prowl:` and maintain import/export/lint tests against OKF v0.1 fixtures. Because v0.1 is explicitly a draft, isolate it behind a versioned codec and domain-neutral internal model; permissive import and lossless unknown-field round-tripping are release requirements.

Google's wider agent architecture reinforces an important separation. ADK distinguishes ephemeral per-call **working context**, durable **session** events, scoped **state**, searchable cross-session **memory**, and named/versioned **artifacts**. Prowl should preserve those lifecycles rather than putting all of them in one generic memory table. In particular, large artifacts should be referenced from context, not repeatedly copied into prompts.

### 2.2 Karpathy's LLM-wiki strategy

The useful pattern is a three-layer system:

1. Raw sources remain available and attributable.
2. An agent incrementally compiles them into an interlinked Markdown wiki.
3. A small schema/instruction layer defines ingestion, query, and lint behavior.

The important distinction from ordinary RAG is that synthesis compounds. The agent does not repeatedly rediscover the same facts from raw chunks; it maintains concept pages, indexes, links, and contradictions.

**Determination for Prowl:** support both paths.

- Deterministic graph/search finds evidence and source structure.
- Curated wiki stores synthesized understanding.
- Retrieval chooses curated concepts first when healthy, then drills into deterministic evidence.
- Raw-source fallback remains mandatory because a wiki can be incomplete or wrong.

Karpathy's broader agent-engineering work adds three constraints:

1. **Context is part of the program.** Prompts, tools, skills, memory-selection rules, and repository instructions must be versioned and evaluated as product artifacts.
2. **Autonomy follows verifiability.** `autoresearch` succeeds by constraining writable scope, fixing the evaluator and time budget, checkpointing every experiment, and reverting regressions. Prowl should expose inspect → propose → scoped execute modes rather than one global autonomous switch.
3. **Human verification is the bottleneck.** Small diffs, visual evidence, explicit acceptance criteria, and rollback points matter more than maximizing generated work. A huge plausible change the human cannot audit is a product failure.

For autonomous Prowl workflows, every lane must declare its writable scope, verifier, budget, retry ceiling, rollback behavior, and risks the verifier cannot measure. Keep failed hypotheses in an experiment ledger so agents do not repeatedly revisit equivalent dead ends.

### 2.3 Claude-mem

Claude-mem's strongest lessons are architectural, not product-specific:

- Lifecycle hooks capture observations asynchronously.
- A worker/service stores structured sessions, observations, and summaries.
- A live web viewer makes hidden memory inspectable.
- Search follows progressive disclosure: compact search index → timeline/context → selected full observations.
- Progressive disclosure is built into tool shape rather than left as a prompt suggestion.
- Memory is project-scoped and temporally navigable.

Risks Prowl should avoid:

- Capturing every tool event by default creates privacy, noise, and storage problems.
- AI-compressed observations can become ungrounded if source evidence is not preserved.
- Auto-injected memory can crowd out current task context.

**Determination for Prowl:** introduce optional agent-session adapters and a reviewable memory inbox. Store evidence pointers and hashes. Promote durable concepts only through explicit agent proposal or human approval.

### 2.4 OMP / Oh My Pi

“OMP” resolves to **Oh My Pi**, a terminal coding agent and Pi fork. Relevant design choices include:

- A complete but extensible harness with TUI, prompt mode, SDK, RPC, and ACP surfaces.
- Built-in tools, but the active tool set can be pinned while hidden tools remain searchable through a BM25 discovery tool.
- Packaged web search and source-aware extraction.
- Hooks/rules that activate only when relevant rather than consuming context continuously.
- Multiple context compaction strategies, including deterministic pruning/elision.
- First-run discovery of existing rules, skills, and MCP configurations instead of forcing migration.

OMP exposes the same engine through an interactive TUI, one-shot/JSONL CLI, TypeScript SDK, NDJSON RPC over stdio, and standardized ACP JSON-RPC for editors. Its sessions are append-only trees; branches preserve prior timelines, skills expose metadata before loading their full bodies, and compaction preserves original history on disk. Prowl should copy this boundary discipline: use a public versioned bridge contract for integrations, stream progress/cancellation as events, and never couple Obsidian or a GUI directly to SQLite.

There is also a secondary project named `bparlan/omp-agent`: a spec-driven framework built on top of OMP. It is not the core Oh My Pi runtime and should be evaluated as one workflow pack, not treated as OMP itself.

**Determination for Prowl:** borrow discovery and context-economy patterns, not OMP's agent runtime.

Prowl should expose a small stable core and a discoverable capability catalog:

- `workspace_overview`
- `search_context`
- `get_context`
- `analyze_change`
- `propose_memory`
- `search_capabilities`

Specialized operations remain available behind `search_capabilities` or generated task skills without putting dozens of schemas in every session.

### 2.5 MCP design

MCP distinguishes:

- **Resources:** application-controlled context, searchable/browsable and optionally subscribable.
- **Prompts:** user-controlled workflows.
- **Tools:** model-controlled actions.
- **Sampling/elicitation:** server requests for model or human input while the host retains control.

Prowl currently concentrates on tools. It should become a balanced MCP server:

```text
Resources
  prowl://workspaces
  prowl://workspace/{id}/overview
  prowl://workspace/{id}/knowledge/index
  prowl://workspace/{id}/concept/{id}
  prowl://workspace/{id}/source/{path}
  prowl://workspace/{id}/changes

Prompts
  /understand-project
  /research-topic
  /review-change
  /update-knowledge
  /prepare-implementation

Tools
  search_context
  get_context
  analyze_change
  propose_knowledge_change
  validate_knowledge
```

Every result should use MCP audience/priority/last-modified annotations where supported and return structured output plus resource links.

Use client sampling for optional semantic synthesis when available, so Prowl can leverage the user's existing model without owning provider credentials. Keep local Ollama as an optional standalone path.

### 2.6 Spec-driven toolkits

GitHub Spec Kit's useful pattern is artifact continuity:

```text
constitution → specification → clarification → plan → tasks → consistency analysis → implementation → convergence
```

Prowl should not reimplement Spec Kit. It should ingest and connect spec artifacts to code and knowledge:

- requirements → files/symbols/tests
- decisions → implementation anchors
- tasks → Git changes
- acceptance criteria → validation evidence
- changed code → potentially stale specs/knowledge

This produces a differentiated **intent-to-implementation trace** instead of another slash-command framework.

Do not hard-code Spec Kit as Prowl's workflow model. The useful cross-tool synthesis is:

| Toolkit | Pattern to adopt |
|---|---|
| GitHub Spec Kit | Packaged, agent-neutral process bundles and artifact continuity. |
| OpenSpec | Canonical current specifications plus per-change delta specifications and archival/sync. |
| Kiro Specs | Dependency-aware task DAGs and conditional steering/hooks. |
| Agent OS | Selective injection of only the standards relevant to the current task. |
| BMAD | Optional domain personas and full-lifecycle packs, without making its ceremony global. |
| `omp-agent` | Strict checkpoints, verification before implementation, and read-before-write discipline. |

Prowl's workflow definition should therefore be declarative: artifact types, dependency edges, validation rules, approval policy, permissions, and lifecycle transitions. Engineering is one work pack; research briefs, comparisons, travel planning, purchasing decisions, localization, and content projects can use the same substrate later.

### 2.7 Obsidian

Obsidian is the strongest human authoring partner because its native model already matches OKF:

- Markdown and YAML properties.
- Wikilinks, resolved/unresolved links, tags, headings, blocks, and backlinks through `MetadataCache`.
- Vault create/read/process/rename events through the official API.
- Custom `ItemView` panes and React-based views.
- Commands, ribbon actions, editor suggestions, context menus, and protocol handlers.
- Desktop and mobile, with important runtime limitations on mobile.

**Determination:** build an official desktop-first Prowl plugin with a graceful mobile/read-only mode. Do not require users to move an entire code repository into a vault.

Support three workspace modes:

1. **Vault workspace:** index the current vault.
2. **Linked project:** vault contains curated knowledge while Prowl links to an external repo.
3. **Embedded knowledge folder:** a repo's OKF bundle is opened as or mounted into an Obsidian vault.

Plugin features should include:

- Prowl sidebar: project health, current context, stale concepts, memory inbox.
- Context pane linked to the active note/file.
- Search modal with human explanations and citations.
- Concept editor backed by OKF frontmatter.
- “Explain this neighborhood,” “show impact,” “cite source,” and “propose update” commands.
- Visual map with semantic zoom and filters, not a raw all-node graph.
- Timeline/diff view for concept and source evolution.
- Open source at exact file/line through editor URI or configured IDE.
- Mobile: render/search synced OKF knowledge; disable local binary-dependent code indexing with a clear status.

### 2.8 Competitive landscape and defensible wedge

The market already contains increasingly similar combinations of Tree-sitter, a local graph, MCP, semantic search, and a browser visualization:

- **GitNexus** offers local code graphs, MCP Resources/Prompts, process traces, impact analysis, a bridge-backed web UI, and a global repository registry.
- **CodeNexus/CodeCortex-style tools** emphasize interactive graph visualization, history/impact overlays, and local code retrieval.
- **Understand Anything** combines deterministic extraction, LLM summaries, guided tours, persona-adaptive views, and multilingual documentation.
- **Codemap** separates rebuildable source index data from curated, source-anchored project memory and exposes freshness/trust signals.
- **Graphify** expands graph generation to documents, PDFs, images, and transcripts and can emit an agent-crawlable wiki.
- **Claude-mem and OMP/mnemopi** address cross-session memory and temporal recall inside agent harnesses.

Therefore Prowl cannot differentiate by saying “knowledge graph for AI,” “MCP for your codebase,” “local-first,” or “interactive visualization.” Those are category requirements.

Prowl's wedge should be the complete **evidence-to-knowledge loop**:

```text
deterministic evidence
  → agent-proposed synthesis
  → human review in Obsidian/Prowl
  → portable OKF knowledge
  → budgeted agent context
  → changed-source freshness checks
  → visible refresh proposal
```

The distinctive product demonstration should show the same fact moving through both worlds:

1. Prowl finds the deterministic source and dependency path.
2. The agent proposes a decision/concept with citations.
3. The human reviews it in Obsidian or the workbench.
4. Another agent receives it later as a compact context item.
5. A code change invalidates the anchor, and both human and agent see that the knowledge is stale.

That closed, inspectable loop is more defensible than a larger parser count or a prettier force graph.

### 2.9 Packaged web/research architecture

Research providers solve different parts of the pipeline:

- **Tavily:** strongest packaged search/extract/crawl/research workflow with explicit budgets and citations.
- **Exa:** semantic discovery, highlights, structured enrichment, and language-aware search.
- **Firecrawl:** robust rendered-site acquisition, crawl/map/extract, and browser-like fallback.
- **Jina Reader/Search:** low-friction multilingual URL/PDF-to-Markdown extraction and reranking.
- **Perplexity:** cited synthesis and deep-research verification.
- **Browser Use:** stateful interaction for forms, authenticated pages, and UI workflows—not normal retrieval.
- **SearXNG:** self-hosted metasearch for privacy/provider fallback, but not an extraction or citation solution by itself.

Prowl should present a small provider-neutral facade:

```text
search · read_url · map_site · crawl_site · research · browser_task
```

Discovery, extraction, and synthesis must remain separate stages. Preserve raw URLs/snapshots, retrieval time, publication time, source language, extraction method, quotation language, translation status, and source quality. Primitive search/read should be the default; escalate to an agentic deep-research provider only for genuinely multi-step work.

Long research must be resumable and asynchronous: job ID, progress events, cancellation, explicit budgets, partial evidence, and retry ceilings. Provider-native adapters belong behind the facade; MCP remains the user-extensible integration boundary.

---

## 3. Product architecture

### 3.1 Four-layer model

```text
┌───────────────────────────────────────────────────────────────┐
│ Surfaces                                                      │
│ Web workbench · Obsidian · CLI · MCP · LSP · exported HTML    │
├───────────────────────────────────────────────────────────────┤
│ Context service                                               │
│ budget packing · ranking · provenance · freshness · summaries │
├───────────────────────────────────────────────────────────────┤
│ Knowledge layer                                               │
│ OKF concepts · claims · decisions · observations · timelines  │
├───────────────────────────────────────────────────────────────┤
│ Evidence layer                                                │
│ files · symbols · links · imports · Git · specs · source data  │
├───────────────────────────────────────────────────────────────┤
│ Storage                                                       │
│ Markdown/Git (canonical) · SQLite/FTS/vector (derived cache)   │
└───────────────────────────────────────────────────────────────┘
```

### 3.2 Canonical versus derived data

**Canonical/reviewable:**

- OKF concept Markdown.
- Human-authored settings.
- Accepted decisions and rationale.
- Source references and stable IDs.
- Optional tracked workspace manifest.

**Derived/rebuildable:**

- File/symbol/edge/chunk tables.
- FTS/vector indexes.
- Reverse links, centrality, clusters, inferred code paths.
- Render/layout cache.
- Query analytics.

Never hide accepted durable memory exclusively in `.prowl/index.db`.

### 3.2.1 Separate knowledge and memory planes

Do not flatten “memory” into one vector collection. Prowl needs distinct authority, scope, retention, and retrieval policy for:

1. **Live source truth:** checkout, Git revision/diff, tests, build output, symbols, links, and indexes; rebuildable and authoritative for current implementation facts.
2. **Policy memory:** versioned instructions, conventions, ownership, security limits, and test requirements; human-reviewed with explicit scope/precedence.
3. **Episodic evidence:** append-only task/session/worktree events, decisions, failed attempts, verifier output, tool/model versions, timestamps, and costs; summaries are disposable views.
4. **Curated semantic knowledge:** promoted decisions, invariants, gotchas, rejected approaches, and playbooks with source anchors, author/model, confidence, validity, and proposed/confirmed/superseded/contradicted/deprecated state.
5. **User/application state:** scoped preferences and current variables, never silently mixed into a project's shared facts.
6. **Artifacts:** large/versioned files referenced by handle rather than pasted repeatedly.
7. **Working context:** ephemeral output compiled under the current task's budget and permissions.

Parallel branches, worktrees, tasks, and subagents must remain isolated until evidence or knowledge is deliberately promoted. Main-agent memory must not automatically flood every worker.

### 3.3 Unified entities without discarding the code engine

Introduce generic records alongside existing tables first; do not perform a big-bang rewrite.

```text
artifacts
  id, uri, kind, title, mime, language, content_hash, modified_at,
  source_adapter, canonical_path, metadata_json

nodes
  id, artifact_id, stable_key, kind, name, summary, start/end anchor,
  deterministic, confidence, valid_from, valid_to, metadata_json

relations
  id, from_node, to_node, kind, deterministic, confidence,
  evidence_anchor, valid_from, valid_to, metadata_json

knowledge_documents
  id, concept_id, path, okf_type, title, description, resource,
  tags, timestamp, review_state, content_hash

observations
  id, workspace, session, type, title, summary, occurred_at,
  source_adapter, privacy_class, promoted_concept_id

context_runs
  id, query_hash, budget, selected_ids, omitted_counts, latency,
  retrieval_strategy, created_at
```

Map existing files/symbols/edges through compatibility views or adapter methods. Add migrations with rollback/backup and schema-version fixtures.

### 3.4 Context packet contract

All surfaces should consume one service contract:

```json
{
  "question": "Where is authentication enforced?",
  "summary": "Authentication is enforced at the HTTP middleware boundary...",
  "items": [
    {
      "id": "concept:auth-boundary",
      "kind": "decision",
      "title": "Authentication boundary",
      "why_selected": ["semantic match", "directly linked to changed file"],
      "freshness": "current",
      "confidence": "verified",
      "audience": ["user", "assistant"],
      "citations": [
        {"uri": "file://internal/http/auth.go", "line_start": 40, "line_end": 92}
      ],
      "detail_resource": "prowl://workspace/x/concept/auth-boundary"
    }
  ],
  "budget": {"requested_tokens": 1800, "estimated_tokens": 1460},
  "omitted": {"low_rank": 18, "stale": 2},
  "next": ["get timeline", "inspect middleware neighborhood"]
}
```

Support `compact`, `standard`, and `full` modes plus byte/token budget. Results must state what was omitted.

### 3.5 Retrieval strategy

Use staged retrieval, evaluated independently:

1. Workspace/domain filter.
2. Cheap lexical and metadata search.
3. Deterministic graph expansion with bounded depth.
4. Optional semantic ranking.
5. Freshness/confidence/provenance scoring.
6. Diversity-aware packet packing under budget.
7. Raw-source fallback when curated knowledge is absent or stale.

Do not assume vector search is always best. Build a retrieval benchmark with lexical, graph, hybrid, and optional vector variants.

### 3.6 Provider and tool neutrality

- Deterministic indexing must require no model.
- Local Ollama remains optional.
- MCP sampling should be the preferred host-provided synthesis route when available.
- A provider adapter may support OpenAI-compatible, Anthropic, Gemini, or local endpoints later, but credentials stay outside indexed state.
- Web research should be a packaged workflow/tool adapter with source capture, not a hardcoded search vendor inside the graph core.
- Every AI-authored knowledge change enters a proposal/review flow and preserves citations.

---

## 4. Human UX design

### 4.1 First-run journey

Replace “one command silently configures everything” with a guided but scriptable flow:

```text
$ prowl init

What are you working with?
  ● Code repository
  ○ Notes / knowledge folder
  ○ Mixed project

What should Prowl enable?
  ☑ Local structural index
  ☑ Human workbench
  ☐ Local semantic search
  ☐ Obsidian connection

Detected integrations
  Claude Code   detected   [enable]
  VS Code       detected   [enable]
  OMP           not found  [skip]

Planned changes
  + .prowl/config.toml
  + .gitignore entries: .prowl/index.db, .prowl/cache/
  ~ AGENTS.md: 8-line Prowl index block

[Apply] [Show full diff] [Save plan only]
```

Requirements:

- `--yes`, `--no-input`, `--preset`, and `--json` for automation.
- `--dry-run` with exact writes and commands.
- Detect existing integrations; never write all vendor configs by default.
- Per-step success, warning, rollback command, and docs link.
- `prowl doctor setup` validates PATH, DB, client configs, and source visibility.
- `prowl uninstall --integration <name>` removes only marker-owned changes.

### 4.2 Human-friendly CLI

Every query should support:

- `--format human` (default on a TTY).
- `--format toon` (agent shell use).
- `--format json` (automation).
- `--format markdown` (reports/Obsidian).

Human output should suppress null/empty sections, explain why results matter, use tables/tree diagrams where appropriate, and provide drill-down commands.

Example:

```text
Prowl found 3 authentication boundaries

1. HTTP middleware                        verified
   internal/http/auth.go:40-92
   Guards 14 routes and calls session validation.

2. WebSocket handshake                    likely
   internal/ws/serve.go:71-104
   Separate path; does not reuse HTTP middleware.

Next: prowl inspect 1 · prowl impact internal/http/auth.go
```

### 4.3 Local web workbench

Command:

```text
prowl open
```

Architecture:

- Go serves embedded static assets and a versioned loopback API.
- Bind `127.0.0.1` only by default.
- Generate an ephemeral bearer token; strict origin/CORS policy.
- Optional idle shutdown; no always-on daemon required.
- REST for snapshots and SSE/WebSocket for progress/index updates.
- `--no-browser`, `--port`, and `--export-html` modes.

Core screens:

1. **Home / Brief:** what the project is, primary domains, entrypoints, recent changes, unresolved risks, knowledge health.
2. **Explore:** hierarchical project → domain → flow → file/symbol/concept navigation.
3. **Context Lens:** show the exact packet an AI would receive, selection reasons, cost, omitted items, and citations.
4. **Impact:** before/after change map with tests/specs/docs likely affected.
5. **Knowledge:** OKF concepts, backlinks, sources, confidence, freshness, and review inbox.
6. **Timeline:** Git changes, decisions, agent observations, and concept revisions.
7. **Setup:** integrations and health with explicit edit previews.

Visual principles:

- Use progressive zoom and stable hierarchy; never open on the full force graph.
- Default to a narrative “guided tour” of 5–12 high-value nodes.
- Maintain a synchronized split pane: map, explanation, and evidence.
- Distinguish deterministic facts, human statements, and AI inferences visually.
- Color cannot be the only state signal; support keyboard navigation and screen readers.
- Save layout separately from semantic knowledge.

### 4.4 Obsidian plugin

Suggested package layout:

```text
web/                         shared workbench components
integrations/obsidian/
  manifest.json
  src/main.ts
  src/bridge.ts
  src/views/
  src/commands/
  src/settings.ts
```

Desktop bridge options, in order:

1. Connect to `prowl bridge --stdio` for a long-lived local session.
2. Fall back to short `prowl ... --json` commands.
3. Allow an explicitly configured loopback API from `prowl open --headless`.

Do not open SQLite directly from the plugin; keep one API/contract and avoid schema coupling.

Plugin MVP:

- Detect/install/check Prowl binary with consent.
- Link current vault to a Prowl workspace.
- Sidebar brief and health status.
- Search/context modal.
- Active-note backlinks plus code/source relations.
- Create/edit/validate OKF concept.
- Review proposed memory changes with diff and citations.
- Open linked source and exact line.

Later:

- Guided graph/map view.
- Spec coverage and change impact.
- Timeline.
- Bases-compatible views for concepts/decisions/research.
- Canvas export for hand-curated maps.
- Mobile read/search of synced OKF; no false promise of local code indexing where process execution is unavailable.

---

## 5. Agent UX design

### 5.1 Small, structured MCP surface

Consolidate overlapping read tools around user intent, preserve old tools behind a compatibility capability for at least one deprecation cycle, and add Resources/Prompts.

An agent should begin with a cheap workspace resource or `search_context`, not receive every specialist schema up front.

### 5.2 Progressive disclosure enforced by contract

```text
search_context(query, mode=compact)
  → IDs, titles, types, freshness, why-selected, estimated cost

get_context(ids[], mode=standard, budget=...)
  → summaries, neighborhoods/timeline, citations, next links

read resource URI
  → exact full concept/source only after selection
```

The agent should be unable to request unbounded details accidentally without an explicit budget or `full` mode.

### 5.3 Prowl-generated agent guidance

Keep root guidance under roughly 10–15 lines:

```markdown
## Prowl context
Use `prowl overview --format toon` or the Prowl MCP workspace resource before broad repository search.
For task context, use `search_context`; fetch full details only for selected IDs.
Knowledge index: `prowl://workspace/current/knowledge/index`.
Validate change impact with `analyze_change` before finishing.
```

Put full workflows in discoverable skills/prompts, not always-loaded `AGENTS.md`.

### 5.4 Capability packs

Represent optional workflows as compact manifests:

```yaml
name: web-research
triggers: [research, current information, compare sources]
requires: [web_search, web_extract]
outputs: [OKF Reference, OKF Claim, citation]
privacy: external-network
```

`search_capabilities` returns metadata; the agent or host loads details only when needed. Packs may cover:

- Web research.
- PDF/document ingestion.
- GitHub issues/PRs.
- Spec Kit.
- Obsidian.
- Agent-session capture.
- Domain-specific rice checks as an optional pack, no longer global behavior.

### 5.5 Memory lifecycle

```text
observe → summarize → propose → review → accept → verify → age → refresh/deprecate
```

Rules:

- Deterministic evidence can update automatically.
- AI interpretation is a proposal.
- Accepted knowledge records author/source class and timestamp.
- Anchored code claims become stale when content hashes or line regions change.
- Contradictions are surfaced, not silently overwritten.
- Secrets and explicitly private blocks are excluded before persistence.
- Users can inspect, export, edit, and delete all memory.

---

## 6. Multilingual and general-use expansion

Treat three dimensions separately:

### Programming-language support

- Fix `config.languages` so it actually controls parsing/indexing, or remove it in favor of explicit include/exclude rules.
- Auto-detect parsers from content/extensions; do not default to a rice-oriented allowlist.
- Publish a parser capability matrix: symbols, imports, references, calls, complexity, tests.
- Add adapter interfaces and fixture contracts so grammar support can expand without core branching.

### Natural-language content

- Store document language and optional per-section language.
- Use Unicode normalization and test diacritics, CJK, RTL, and mixed code/prose.
- Make semantic models capability-declared; warn when an embedding model is weak for detected languages.
- Do not translate canonical content silently. Store translations as linked concepts with provenance.
- Keep concept paths/IDs language-neutral where possible. Translating filenames would create new OKF identities; localize titles/bodies and link locale variants explicitly.
- Search in the user's language first, optionally fan out into English and relevant regional variants, semantically deduplicate results, preserve source-language quotations, and label every translation.

### UI localization

- Introduce message IDs from the first GUI/Obsidian release; do not hardcode copy across components.
- Ship English first with pseudolocale tests, then add Spanish and Portuguese as initial real locales, followed by community translations.
- Localize help, errors, dates/numbers, setup, and accessibility labels—not command names or machine schemas.
- Add `--locale` and respect `LANG`, with stable English error codes for automation.

### General-use source adapters

Prioritize structured text before multimodal breadth:

1. Code, Markdown/OKF, plain text, Git, specs.
2. HTML/web snapshots and PDFs with citations.
3. GitHub issues/PRs and selected work trackers.
4. Office documents and transcripts only after provenance/redaction architecture is proven.

Avoid “index everything on the computer.” Workspaces and source permissions must be explicit.

---

## 7. Phased implementation roadmap

### Phase 0 — Product correction and UX foundations

**Goal:** make the existing product trustworthy and understandable before adding architecture.

Tasks:

- Add cross-platform release jobs: Linux amd64/arm64, macOS amd64/arm64, Windows amd64.
- Add release smoke tests for install, init, query, status, update, and uninstall paths.
- Add `human|toon|json|markdown` output abstraction; TTY defaults to human.
- Rewrite human `overview`, empty states, and errors; never render `null` or empty collection syntax.
- Split setup into detect → plan → preview → apply → verify.
- Add `init --dry-run`, selectable integrations, and machine-readable setup report.
- Stop indexing generated agent/client files by default.
- Fix or remove the unused `config.languages` contract.
- Move rice-specific doctor checks into a `rice` domain pack/profile.
- Shorten generated root agent guidance and link to discoverable workflows.
- Replace token-savings-first landing copy with the knowledge/context promise; retain measured efficiency evidence deeper in docs.
- Build an end-to-end onboarding fixture test.

**Exit criteria:**

- New user reaches a useful human overview in under two minutes.
- Setup modifies only selected integrations and shows every planned write.
- Empty/small repositories produce an explanatory overview.
- Release binaries work on the target matrix.

### Phase 1 — Durable knowledge and OKF

**Goal:** add reviewable, portable meaning without destabilizing code intelligence.

Tasks:

- Add `artifact`, generic `node/relation`, and knowledge-document migrations alongside existing schema.
- Implement OKF parser, validator, index generator, log writer, import, and export.
- Add Prowl namespaced metadata for stable ID, status, confidence, temporal validity, and source anchors.
- Define canonical/cache locations and Git-ignore migration.
- Add source-anchor hashing and stale detection.
- Implement `prowl knowledge init|list|show|lint|propose|accept|reject|export`.
- Add contradiction/orphan/broken-link/stale-anchor health checks.
- Add fixtures from Google's OKF samples plus unknown-type/unknown-field compatibility tests.
- Add backup and rollback around schema/config migration.

**Exit criteria:**

- Deleting the SQLite index never deletes accepted knowledge.
- OKF round-trips without losing unknown fields.
- Changed source anchors mark affected knowledge stale.
- A human can review every AI-proposed durable change as a diff.

### Phase 2 — Unified context service and MCP v2

**Goal:** make Prowl materially improve agent behavior, not just expose a graph.

Tasks:

- Implement the context-packet contract and budget-aware packing.
- Add deterministic ranking features and optional semantic reranking.
- Add compact/standard/full modes and explicit omission metadata.
- Add MCP Resources, resource templates, Prompts, annotations, and structured tool outputs.
- Consolidate common MCP intents; keep legacy tool aliases behind compatibility mode.
- Add `search_capabilities` and capability manifests.
- Add optional MCP sampling and elicitation paths with host approval.
- Add query tracing that stores metadata, never source/reply text by default.
- Build a benchmark suite with question, expected sources/concepts, distractors, and budget.
- Compare grep/file exploration, existing Prowl, lexical context, graph context, and hybrid context.

**Exit criteria:**

- Agent benchmark improves source hit rate/tool-call count under a fixed context budget.
- Every context result explains selection, freshness, citations, and omissions.
- Core MCP schema overhead is lower than the current all-tools surface.
- No model/provider is required for deterministic operation.

### Phase 3 — Local web workbench

**Goal:** provide the showable, memorable human product.

Tasks:

- Add versioned loopback API and embed frontend assets in the binary.
- Implement Home/Brief, Explore, Context Lens, Impact, Knowledge, Timeline, and Setup views incrementally.
- Use progressive hierarchy and guided tours before force-graph mode.
- Add source preview, exact-line navigation, backlinks, and deterministic/AI/human badges.
- Add memory review inbox and diff acceptance UI.
- Add index/research progress events and graceful cancellation.
- Add keyboard, screen-reader, contrast, reduced-motion, and pseudolocale tests.
- Add read-only single-file HTML export for sharing snapshots.
- Add visual regression and interaction tests.

**Exit criteria:**

- A newcomer can identify purpose, architecture, and a risky change area without using the CLI.
- The Context Lens exactly matches MCP/CLI packet content.
- UI binds loopback only and passes local API security tests.
- A guided tour is useful on at least three very different fixture projects.

### Phase 4 — Official Obsidian integration

**Goal:** make Obsidian the best human authoring environment for Prowl knowledge.

Tasks:

- Build desktop bridge and capability negotiation.
- Add current-vault and linked-project setup.
- Add sidebar brief, context/search modal, source relation pane, and concept editor.
- Integrate MetadataCache links/frontmatter/headings/backlinks and Vault change events.
- Add proposed-memory review with atomic `Vault.process()` writes and conflict detection.
- Add code/source URI handlers and configured external-editor links.
- Reuse workbench components for visual exploration.
- Add mobile capability detection and read-only/synced-knowledge UX.
- Prepare Obsidian community-plugin review, privacy policy, and reproducible build.

**Exit criteria:**

- User can move from active note → related code/spec/decision → exact evidence without leaving context.
- External file changes and Obsidian edits converge without data loss.
- Mobile never presents unavailable local-index actions as working.

### Phase 5 — Memory, research, and packaged tools

**Goal:** let knowledge compound across work while remaining inspectable.

Tasks:

- Add observation/session model and adapters for Claude Code, Codex, OMP, and generic MCP clients.
- Make capture opt-in by source and event type.
- Add asynchronous summarization through host sampling/local provider.
- Implement observation search → timeline → detail flow.
- Add privacy classes, redaction hooks, retention, export, and deletion.
- Build web-research capability pack: search, extract, snapshot, cite, propose OKF concepts/claims.
- Preserve query/source provenance and distinguish quote, claim, synthesis, and decision.
- Add scheduled or user-triggered knowledge lint/gardening with proposals only.
- Add capability-pack signing/version metadata and permission declarations.

**Exit criteria:**

- Past context can be recovered without bulk session injection.
- Every promoted memory has inspectable provenance.
- Research claims link to captured sources and expose stale/broken citations.
- Users can disable and erase capture without corrupting deterministic indexes.

### Phase 6 — Spec traceability, multilingual maturity, and ecosystem

**Goal:** turn Prowl into an extensible general knowledge layer.

Tasks:

- Add Spec Kit adapter first; later adapters for OpenSpec/BMAD/other artifact layouts.
- Implement requirement → decision → code → test → change trace views.
- Add spec drift and acceptance-evidence checks.
- Publish source-adapter and capability-pack SDK/contracts.
- Add HTML/PDF/GitHub source adapters after privacy/provenance gates.
- Add locale infrastructure, Spanish/Portuguese translations, community translation workflow, and multilingual retrieval benchmark.
- Add global workspace registry and cross-workspace search with explicit access scope.
- Add plugin marketplace/catalog with permissions and compatibility metadata.

**Exit criteria:**

- Prowl can answer “which requirement justified this code, and what verifies it?” with citations.
- Multilingual benchmark covers search, UI, and generated knowledge behavior.
- Third-party adapters pass compatibility, permission, and provenance tests.

---

## 8. Evaluation plan

### UX metrics

- Median install-to-first-insight time.
- Setup completion and rollback rate.
- Percentage of users who can answer five comprehension tasks without documentation.
- Time to locate source evidence from a concept.
- Memory proposal acceptance/rejection/edit rate.
- Search reformulation rate and zero-result recovery.

### Agent metrics

- Expected-source hit rate, precision/recall@K, MRR.
- Answer citation correctness.
- Tool calls and bytes/tokens consumed.
- Latency by retrieval stage.
- Stale-memory warning coverage.
- False-confidence rate when knowledge conflicts with current source.
- Impact-analysis coverage for changed files.
- Retrieved context actually cited or used by the final plan/patch, not merely returned.
- Cross-session, branch, worktree, and subagent leakage.
- Hallucinated or unsupported durable-memory proposal rate.
- Duplicate experiment/rejected-hypothesis recurrence.
- Secret-retention and pre-external-call redaction failures.
- Human review time and correction rate.

ContextBench's warning should shape evaluation: sophisticated scaffolding may yield only marginal retrieval gains; high recall can introduce harmful noise; agents may inspect relevant context but fail to use it. Measure precision, recall, utilization, task success, latency, and token cost together. “Search returned something” is not success.

### Performance targets to validate, not market before measurement

- Structural query p95 under 100 ms on a warm medium repository.
- Single-file incremental refresh p95 under 300 ms.
- Human workbench first meaningful paint under 1.5 s after API readiness.
- Context packet respects requested budget within ±10%.
- No unbounded graph traversal or UI rendering path.

### Fixture portfolio

- Tiny empty/single-file project.
- Prowl itself (Go + configs + docs).
- TypeScript monorepo with aliases.
- Python service with tests and docs.
- Mixed Java/Kotlin repository.
- Config/rice repository to preserve original strength.
- Pure Obsidian vault with backlinks and multilingual notes.
- Mixed repo + external knowledge vault.
- Spec Kit project with requirements and implementation drift.

---

## 9. What not to build

- **Not another coding agent.** Integrate with OMP, Claude, Codex, Cursor, and others.
- **Not a chat box as the product.** Chat may consume Prowl, but the asset is structured, inspectable knowledge.
- **Not a graph hairball.** Use guided hierarchy, stories, filters, and evidence.
- **Not mandatory cloud RAG.** Deterministic local operation remains the floor.
- **Not a mandatory daemon.** Long-lived bridge/UI modes are opt-in and lifecycle-managed.
- **Not hidden auto-memory.** Durable interpretations are proposed, reviewable, exportable, and deletable.
- **Not an Obsidian clone.** Obsidian is the authoring environment; Prowl supplies intelligence and interoperability.
- **Not a proprietary OKF dialect.** Extend under a namespace and preserve round-trip compatibility.
- **Not every source type at once.** Earn trust on code, Markdown, specs, web, and PDFs before multimodal sprawl.
- **Not rice deletion.** Preserve config/rice intelligence as a high-quality optional domain pack instead of letting it define the whole product.

---

## 10. First implementation slice

The first shippable slice should be narrow enough to validate the direction:

1. Human output mode and useful `overview`.
2. Selective/dry-run setup with generated-file exclusion.
3. Cross-platform releases.
4. Minimal OKF import/lint/list/show with a canonical Markdown folder.
5. One context-packet endpoint used by CLI and MCP.
6. MCP workspace/knowledge Resources plus `search_context` and `get_context`.
7. Read-only local workbench showing Brief, Context Lens, and Knowledge.

This slice proves the core promise—same evidence, better human view, better agent view—before investing in full memory capture or a large Obsidian plugin.

Suggested implementation order inside the slice:

```text
output contract
  → setup transaction model
  → OKF domain model
  → context packet service
  → MCP resources/tools
  → loopback API
  → read-only workbench
  → end-to-end benchmark and UX test
```

---

## 11. Primary sources

- Google Cloud, “Introducing the Open Knowledge Format”:
  https://cloud.google.com/blog/products/data-analytics/how-the-open-knowledge-format-can-improve-data-sharing
- GoogleCloudPlatform Knowledge Catalog, OKF v0.1 and specification:
  https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf
  https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md
- Andrej Karpathy, LLM Wiki idea file:
  https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f
- Andrej Karpathy, constrained autonomous research harness:
  https://github.com/karpathy/autoresearch
  https://raw.githubusercontent.com/karpathy/autoresearch/master/program.md
- Google ADK context, sessions, memory, and artifacts:
  https://google.github.io/adk-docs/context/
  https://google.github.io/adk-docs/sessions/
  https://google.github.io/adk-docs/sessions/memory/
  https://google.github.io/adk-docs/artifacts/
- Claude-mem architecture and progressive search:
  https://github.com/thedotmack/claude-mem
  https://github.com/thedotmack/claude-mem/blob/v13.2.0/docs/public/architecture/overview.mdx
  https://github.com/thedotmack/claude-mem/blob/v13.2.0/docs/public/architecture/search-architecture.mdx
- Oh My Pi / OMP:
  https://omp.sh/
  https://github.com/can1357/oh-my-pi
  https://omp.sh/docs/rpc
  https://omp.sh/docs/acp
  https://github.com/bparlan/omp-agent
- GitHub Spec Kit:
  https://github.com/github/spec-kit
  https://github.com/github/spec-kit/blob/main/docs/reference/agentic-sdd.md
- Additional spec/workflow references:
  https://github.com/Fission-AI/OpenSpec
  https://kiro.dev/docs/specs/
  https://github.com/buildermethods/agent-os
  https://github.com/bmad-code-org/BMAD-METHOD
- MCP server concepts and specification:
  https://modelcontextprotocol.io/docs/learn/server-concepts
  https://modelcontextprotocol.io/specification/2025-06-18/server/resources
  https://modelcontextprotocol.io/specification/2025-06-18/server/tools
- Obsidian plugin APIs:
  https://docs.obsidian.md/Plugins/Vault
  https://docs.obsidian.md/Reference/TypeScript+API/MetadataCache
  https://docs.obsidian.md/Plugins/User+interface/Views
  https://obsidian.md/help/uri
- OpenAI, Harness Engineering:
  https://openai.com/index/harness-engineering/
- ContextBench retrieval evaluation:
  https://arxiv.org/html/2602.05892v1
- Packaged research provider references:
  https://docs.tavily.com/agents
  https://exa.ai/docs/reference/search-api-guide
  https://docs.firecrawl.dev/features/search
  https://jina.ai/reader/
  https://docs.perplexity.ai/docs/search/quickstart
  https://docs.searxng.org/dev/search_api.html
- Competitive references used to test differentiation:
  https://github.com/J0o1ey/GitNexus
  https://github.com/AyoubAchour/codemap
  https://github.com/Egonex-AI/Understand-Anything
  https://github.com/selika/graphify

## 12. Repository evidence inspected

- `README.md`
- `docs/ARCHITECTURE.md`
- `docs/TOKENS.md`
- `install.sh`
- `go.mod`
- `cmd/prowl-agent/main.go`
- `internal/cli/init.go`
- `internal/cli/inject.go`
- `internal/cli/inject_editor.go`
- `internal/cli/query.go`
- `internal/cli/format.go`
- `internal/cli/status.go`
- `internal/cli/statuscard.go`
- `internal/config/config.go`
- `internal/index/index.go`
- `internal/parse/detect.go`
- `internal/query/overview.go`
- `internal/mcp/server.go`
- `internal/store/schema.sql`

The isolated release smoke workspace was created under `/tmp/prowl-ux-fixture`; it did not modify the repository.
