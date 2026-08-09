# External source evaluation (2026-08-08)

Persistent ledger for evaluating user-provided sources against prowl's mission:
a single-binary Go code-intelligence CLI + MCP with token-efficient retrieval, a
deterministic no-model core, durable reviewable knowledge, and a dogfood +
release cadence. Each source is judged for genuine, portable value. "Port" means
reimplement the idea natively in Go (licenses/languages differ); prowl never
takes on external services, daemons, or non-Go runtimes.

## Verdicts

| Source | Lang / License | Verdict | One-line rationale |
|---|---|---|---|
| neur0map/docrawl | Rust / MIT | **PORT (approach)** | Doc-focused crawl to clean Markdown; the reference design for prowl's crawler. |
| volcengine/OpenViking | Python / AGPLv3 | **PORT (design ideas)** | Validates docs/repo ingestion + token-efficient tiered retrieval (benchmarked 34-91% token cut). Ideas only; AGPL, no code reuse. |
| bosun-ai/swiftide | Rust / MIT | **REFERENCE** | Streaming indexing pipeline shape (load -> transform -> store). Confirms architecture; prowl has its own indexer. |
| Nayshins/ummon | Rust / Apache-2.0 | **REJECT (overlap)** | Code knowledge graph + NL/structured query; prowl already has find/callers/graph/impact/context. NL needs a model (against deterministic core). |
| D4Vinci/Scrapling | Python / BSD-3 | **REJECT (overkill)** | Adaptive anti-bot scraper; docs crawling needs politeness, not stealth. Python, not portable. docrawl's doc focus is the right fit. |
| danielmiessler/Fabric | Go / MIT | **REJECT (different domain)** | Curated LLM prompt-pattern library; prowl is deterministic code intelligence, not a prompt library. |
| ruvnet/ruflo | TS / MIT | **REJECT (category mismatch)** | Agent meta-harness (swarms/memory/RAG). prowl is a tool harnesses call, not a harness. Complementary. |
| supermemoryai/supermemory | TS / MIT | **REJECT (memory category)** | Autonomous fact extraction + profiles + connectors; same fork as claude-mem, opposite of prowl's reviewed knowledge. |
| mordang7/ContextKeep | Python / MIT | **REJECT (memory category)** | MCP long-term memory server; overlaps prowl knowledge, autonomous not reviewed. |
| Maciek-roboblog/Claude-Code-Usage-Monitor | Python / MIT | **REJECT (out of scope)** | Monitors Claude account usage/limits; prowl is code intelligence and already exposes retrieval token cost. |

## Decision

The convergent, explicitly-requested, mission-aligned build is **external
documentation ingestion + token-efficient retrieval**: crawl a documentation
site (or ingest a local docs tree) into clean Markdown, index it as a separate
corpus, and let agents query it with the same budget-bounded, cited retrieval
prowl already uses for code. This is the one thing worth building from this
batch; everything else is either a framework/harness prowl is not, a memory
system prowl deliberately does not do, or already covered by prowl's index.

Security is first-class: crawled content is untrusted and flows into an agent's
context, so prompt-injection quarantine (from docrawl) is part of the core, not
an add-on.

## Port sources in detail

### neur0map/docrawl (reference implementation of the crawler)
- Doc-framework-aware content extraction (Docusaurus, MkDocs, Sphinx, Next.js docs).
- HTML -> clean Markdown, preserving code blocks/tables, YAML frontmatter (title, source_url, fetched_at).
- Polite: robots.txt, rate limiting, sitemap hints, concurrency pool.
- Security: content sanitization, prompt-injection detection + quarantine, output sandboxing.
- Path-mirroring output (URL hierarchy -> folders + index.md), persistent cache, resume.

### volcengine/OpenViking (design ideas; AGPL, ideas only)
- Resources = ingested docs/repos/web pages, browsable + searchable (`add-resource`, `ls/tree/find/grep`).
- Tiered loading L0 (abstract) / L1 (overview) / L2 (details) to cut token spend; each directory carries its own summary.
- Directory-recursive retrieval: find the best directory, then drill down with surrounding context intact.
- Observable retrieval trajectory. Benchmarks: 34.3-91.0% input-token reduction, 58-66% latency cut.
- prowl already has budget-bounded compact/cited retrieval; per-directory tiered summaries are a future refinement.

## Status log
- 2026-08-08: All 10 sources read and triaged. Build target chosen: docs ingestion + retrieval. Next: map prowl index/store/context seam, then implement increment 1.
- 2026-08-08: Shipped increment 1 (`internal/docs`): `docs add` crawls a docs
  site (bounded/polite/robots, HTML to Markdown, injection quarantine) or ingests
  a local Markdown tree into a shared per-machine corpus, indexed via the existing
  `index.Index`; `docs search` and the `search_docs` MCP tool retrieve with the
  existing `context.Service` (no model). Dogfooded on prowl's own docs (local) and
  mkdocs.org (remote). All PORT value from docrawl + OpenViking is captured for
  increment 1. REFERENCE/REJECT sources yielded nothing further to port. Possible
  later increments (not required): tiered L0/L1/L2 per-directory summaries
  (OpenViking), remote-repo doc ingestion, concurrent fetch pool.
- 2026-08-08: Final web search validated the docs-ingestion approach (crawl to
  Markdown + budget-bounded retrieval is 2026 best practice; ~2,500-token context
  cliff makes precise retrieval essential) and surfaced one concrete enhancement:
  the `llms.txt` / `llms-full.txt` convention. Built it: `docs add` now prefers a
  site's `llms-full.txt` (whole docs in one fetch) or `llms.txt` (curated page
  list), falling back to the crawl; `--no-llms` opts out. Verified on hono.dev
  (1 fetch, 0.5s) with correct text/html rejection (docs.cursor.com). The search
  also confirmed the scored resolution-accuracy gap (Python/TS dangling edges) is
  real and addressable (import-chain symbol resolution, import maps, confidence
  gating) -- logged as the top future improvement, not built this loop.
