# Full Roadmap Verification Ledger

This ledger records only commands that were actually observed. An omitted phase or
criterion has not yet been verified.

| Phase / task | Timestamp | Command | Fixture / environment | Observed result | Changed paths at observation | Artifacts and volatile exclusions | Review disposition |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Task 1 - A2/A3 workbench baseline | 2026-07-24T14:11:07-04:00 | `CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/workbench ./internal/query ./internal/store ./internal/knowledge -run 'Test(Brief\|API\|Projection\|MalformedResourceVersion\|IdentifierValidation\|SuccessWriter\|ErrorWriter\|Overview\|RepositoryList\|RepositoryRootSwap)' -count=1` | Local Linux host; Go 1.26; SQLite FTS5 enabled | Passed: `4 packages ok` | In-progress release and validation-harness changes; exact branch state captured below | No persistent artifact. Go test cache and temporary test databases excluded. | Focused A2/A3 gate accepted; broader Phase 3A work remains unverified. |
| Task 1 - startup and bounded-input baseline | 2026-07-24T14:11:07-04:00 | `CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/cli ./internal/application ./internal/config ./internal/index ./internal/store ./internal/workspace ./internal/knowledge -run 'Test(OpenCommand\|StartupFreshness\|OpenProjectClose\|LoadContext\|OpenContext\|ResolveContext\|RepositoryListContextBoundedRejectsFIFO\|.*Candidate)' -count=1` | Local Linux host; Go 1.26; SQLite FTS5 enabled | Passed: `7 packages ok` | In-progress release and validation-harness changes; exact branch state captured below | No persistent artifact. Go test cache and temporary test databases excluded. | Focused bounded startup gate accepted; A4 must retain this contract. |

## Source-state checkpoint

Observed immediately after the baseline gates on branch
`prowl-full-evolution` at `cb8fad8`:

```text
M .github/workflows/release.yml
M internal/config/config_test.go
M scripts/release-smoke.ps1
MM scripts/release-smoke.sh
M web/e2e/workbench.spec.ts
M web/vite.config.ts
?? web/src/test/
```

The staged executable-bit change for `scripts/release-smoke.sh` is intentional.
The remaining changes repair the release matrix and validation harness; their
own commands are recorded only after they pass.

## Observed validation gates

| Phase / task | Timestamp | Command | Fixture / environment | Observed result | Changed paths at observation | Artifacts and volatile exclusions | Review disposition |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Task 4 - frontend validation harness | 2026-07-24T14:26:27-04:00 | `cd web && npm run check` | Local Linux host; Node 26; Vitest JSDOM and Chromium | Passed: 4 unit tests, production bundle, and 1 compiled-binary Playwright test | `web/vite.config.ts`, `web/src/test/jsdom-storage.ts`, `web/e2e/workbench.spec.ts` | Playwright trace/results are ignored volatile output; committed bundle was unchanged. | Accepted for A4; browser storage now resolves to JSDOM rather than Node process storage. |
| Task 4 - source candidate classifier | 2026-07-24T14:26:27-04:00 | `CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/index ./internal/cli ./internal/application -run 'Test(.*Symlink\|StartupFreshness\|OpenCommand)' -count=1` | Local Linux host; SQLite FTS5 enabled | Passed: `3 packages ok` | `internal/index/walk.go`, `internal/index/index_test.go`, `internal/index/walk_test.go` | Go test cache and temporary test databases excluded. | Accepted; external, missing, directory, and special symlink targets do not consume candidate capacity. |
| Task 4 - read-only corpus overlay | 2026-07-24T14:26:27-04:00 | `scripts/read-only-corpus-smoke.sh /home/nero/Work/livewall-poc` | Bubblewrap network-isolated overlay with source lower layer mounted read-only | Passed; `/home/nero/Work/livewall-poc/.prowl` remained absent after the run | `scripts/read-only-corpus-smoke.sh` | Temporary overlay, state, cache, config, and generated binary were removed. | Accepted for single-project corpus smoke; whole-corpus acceptance remains pending. |
| Task 3 - durable knowledge foundation | 2026-07-24T14:29:41-04:00 | `CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/knowledge ./internal/store -count=1` | Local Linux host; SQLite FTS5 enabled | Passed: `2 packages ok` | No source changes | Go test cache and temporary test databases excluded. | Phase 1 exit criteria have current unit coverage; independent requirements review remains pending final acceptance. |
| Task 3 - context and MCP foundation | 2026-07-24T14:29:41-04:00 | `CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/context ./internal/query ./internal/mcp ./internal/capability -count=1` | Local Linux host; SQLite FTS5 enabled | Passed: `4 packages ok` | No source changes | Go test cache and temporary test databases excluded. | Phase 2 deterministic, citation, freshness, and MCP parity coverage is current. |
