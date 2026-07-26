# Task B0.2 report

## Status

PASS. The immutable local profile, canonical profile snapshot, prompt serialization, and exposure manifest are implemented and covered by focused package gates.

The checkpoint intentionally does not persist snapshots, bind them to operations, or resume them. Task B0.3 owns that integration.

## RED evidence

### Missing snapshot and exposure implementation

Command:

```text
go test ./internal/profile ./internal/agent -run 'Test(ProfileSnapshot|PromptBytes|ExposureManifest|TrustPrecedence)' -count=1
```

Observed result: FAIL. The tests did not compile because the profile snapshot, prompt serialization, and exposure manifest APIs did not exist.

### Closed provenance and scope regression

Command:

```text
go test ./internal/profile -run 'TestTrustPrecedenceRejectsMislabeledProvenance' -count=1
```

Observed result:

```text
--- FAIL: TestTrustPrecedenceRejectsMislabeledProvenance (0.00s)
    snapshot_test.go:105: rooted project authority accepted web provenance
FAIL
FAIL github.com/prowl-agent/prowl-agent/internal/profile 0.002s
```

Root cause: source kinds selected precedence and trust but did not yet constrain their provenance and scope pair. The repair added one closed classification function shared by snapshot validation and exposure reopening.

## GREEN evidence

Required focused gate:

```text
go test ./internal/profile ./internal/agent -run 'Test(ProfileSnapshot|PromptBytes|ExposureManifest|TrustPrecedence)' -count=1
```

Observed result: PASS, 2 packages.

Full focused package race gate:

```text
go test -race ./internal/profile ./internal/agent -count=1
```

Observed result: PASS, 2 packages.

Focused static analysis:

```text
go vet ./internal/profile ./internal/agent
```

Observed result: PASS, no output.

No project-wide suite was run. No formatter was run, per controller instruction.

## Canonical fixtures

| Fixture | SHA-256 |
|---|---|
| `internal/profile/testdata/profile-snapshot.json` | `060c0387d37e8bbad6004c6274d3dc083cd787aeb3cad036a9e684a0203aacde` |
| `internal/agent/testdata/prompt.txt` | `20886b109677c758632d6b13ed13cdba8e60244b835a8527625e320c5538fd32` |
| `internal/agent/testdata/exposure.json` | `face06b0ca9fb57a07358280b8779d3f4450d1a00895b297de9deda43ad4cc56` |

Tests compare produced bytes to these checked-in fixtures and also pin each exact hash.

## Changed paths

- `internal/profile/model.go`
- `internal/profile/snapshot.go`
- `internal/profile/snapshot_test.go`
- `internal/profile/testdata/profile-snapshot.json`
- `internal/agent/prompt.go`
- `internal/agent/prompt_test.go`
- `internal/agent/exposure.go`
- `internal/agent/exposure_test.go`
- `internal/agent/testdata/prompt.txt`
- `internal/agent/testdata/exposure.json`
- `.superpowers/sdd/2026-07-23-hermes-inspired-agent-operations-port/task-B0.2-report.md`

The pre-existing change to `.superpowers/sdd/2026-07-23-hermes-inspired-agent-operations-port/progress.md` was not modified or staged by this task.

## Contract coverage

- Exactly one closed built-in profile identity, `local`; profile identity is tested as distinct from authenticated principal ID.
- Provider/model limits, core prompt version, authenticated principal ID, active profile fields, permission/approval/readiness policy, tool schemas, skill metadata, preloaded skill bodies, and source inventory are frozen in canonical JSON.
- Normative precedence is fixed from executable system/security through profile, user, durable memory, rooted project instructions, task instructions, and untrusted content. Secret references are classified separately and never serialize values.
- Provenance and scope are closed per source kind, including web, generic source, attachment, and tool-output origins for untrusted turn content.
- Prompt bytes use one versioned JSON schema and carry the snapshot ID plus the exact frozen context; prompt and exposure construction consume only a frozen snapshot.
- Exposure records included and omitted sources with reason, precedence, trust, provenance, scope, content hash, tool schema IDs, skill metadata IDs, preloaded body IDs, and secret reference names.
- Snapshot and exposure reopening reject malformed, non-canonical, or internally inconsistent bytes.
- Construction and every byte/slice accessor clone mutable state; tests mutate inputs and returned values and confirm canonical bytes remain unchanged.

## Self-review

- No operation persistence, resume binding, provider API, or workbench integration was added.
- No provider-specific behavior or provider credential value is present.
- Canonical ordering uses semantic precedence followed by stable IDs; input ordering does not affect bytes or IDs.
- JSON schema bodies are canonicalized and validated on construction and reopen.
- Profile and policy records are immutable value copies. Source/tool/skill collections and canonical byte buffers are copied at every public boundary.

## Concerns and handoff

No B0.2 blocker remains. Task B0.3 must store and restore these exact canonical bytes and IDs at operation boundaries and must reject resume when the pinned snapshot cannot be validated. The new packages are intentionally not wired into production call paths before that task.
