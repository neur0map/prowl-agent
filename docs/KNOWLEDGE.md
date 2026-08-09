# Durable knowledge and OKF

Prowl stores accepted project knowledge as portable Markdown under `.prowl/knowledge/`. SQLite is a derived search/index cache: deleting `.prowl/index.db` never deletes accepted concepts, decisions, claims, or playbooks.

## Layout

```text
.prowl/
  config.toml       # trackable settings
  rules.toml        # trackable policy
  knowledge/        # canonical OKF bundle; track this in Git
    index.md
    log.md
  proposals/        # review inbox; optionally track this in Git
  index.db*         # derived and ignored
  cache/            # derived and ignored
  logs/             # derived and ignored
```

Older repositories may contain a broad `.prowl/` ignore. Prowl preserves that user-visible line and appends marker-owned negations so settings, knowledge, and proposals remain trackable. It never silently removes an unowned ignore rule.

## Start a bundle

```bash
prowl-agent knowledge init
prowl-agent knowledge list
```

A concept is an OKF v0.1 Markdown file:

```markdown
---
type: Decision
title: Keep accepted knowledge in Markdown
resource: file://docs/architecture/storage.md
tags: [architecture, storage]
timestamp: 2026-07-23T00:00:00Z
prowl:
  id: decision-storage
  status: accepted
  confidence: verified
  anchors:
    - path: internal/store/store.go
      line_start: 28
      line_end: 72
      content_hash: sha256:...
---

SQLite is rebuildable. Accepted reasoning is not.
```

Unknown OKF types, top-level fields, nested `prowl:` fields, and YAML scalar types are retained. `index.md` and `log.md` may omit `type`. Missing indexes and broken links are lint findings rather than import failures.

## Review proposals

Proposal ingestion is deterministic and requires no model:

```bash
prowl-agent knowledge propose \
  --file /tmp/candidate.md \
  --target decisions/storage.md \
  --author codex

prowl-agent knowledge accept <proposal-id>
# or
prowl-agent knowledge reject <proposal-id>
```

`propose` validates the candidate and prints a deterministic diff without modifying accepted knowledge. `accept` prints the same diff, writes atomically, appends `log.md`, refreshes the marker-owned index, and only then marks the proposal accepted. Destination collisions are refused. A failed later write restores the prior accepted document, index, and log.

## Inspect and validate

```bash
prowl-agent knowledge show decision-storage
prowl-agent knowledge show decisions/storage.md
prowl-agent knowledge lint
prowl-agent knowledge export ./knowledge-export
```

Every command that returns structured records supports `--json`.

Lint findings use stable namespaced codes and include:

- `knowledge.invalid_document`
- `knowledge.duplicate_id`
- `knowledge.contradiction`
- `knowledge.orphan`
- `knowledge.broken_link`
- `knowledge.invalid_resource`
- `knowledge.invalid_temporal_range`
- `knowledge.stale_anchor`
- `knowledge.missing_anchor`
- `knowledge.invalid_anchor`
- `knowledge.missing_evidence`

A source anchor pins a claim to code in one of two ways:

- **Line range** (`line_start`/`line_end`, 1-based inclusive). A line inserted
  above the region shifts it and reports `stale_anchor`.
- **Symbol** (`symbol: <name>`). The symbol's range is re-resolved from the index
  on each `propose` and `lint`. The anchor follows the symbol when lines move
  above it and goes stale only when the symbol's body changes.

Anchor hashes are SHA-256 over the region; CRLF and LF normalize to LF, and the
final newline does not affect the digest. Omit `content_hash` in a candidate and
`propose` computes it from the current source. An unreadable path or an
unresolved symbol is reported by lint.

## Migration safety

Opening an older on-disk index creates a self-contained SQLite backup using `VACUUM INTO` before applying the transactional migration. Future schema versions are refused without modification. `store.RestoreBackup` replaces a closed database and retains the previous database as `index.db.restore-previous` for reversal.

These safeguards protect derived state. Canonical knowledge remains independently recoverable from `.prowl/knowledge/` and Git.
