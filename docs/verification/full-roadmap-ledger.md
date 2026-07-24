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
