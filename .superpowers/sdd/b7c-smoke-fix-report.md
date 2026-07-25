# B7c Brief smoke fix

## Scope
- Normalized successful query `Overview` collection slices to JSON arrays.
- Revalidated loader results at the Brief state boundary so malformed runtime values use the existing unavailable state.

## RED evidence
- `CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/query ./internal/workbench -run 'Test(Overview|Brief)' -count=1` failed because empty `entrypoints`, `palette`, and `hotspots` marshaled as `null`.
- `cd web && npm test -- --run src/features/brief/BriefPage.test.tsx` failed with `TypeError: Cannot read properties of null (reading 'length')` for a malformed loader result.

## GREEN verification
- `CGO_ENABLED=1 go test -race -tags sqlite_fts5 ./internal/query ./internal/workbench -run 'Test(Overview|Brief)' -count=1` passed.
- `cd web && npm test -- --run src/features/brief/BriefPage.test.tsx` passed: 4 tests.
- `cd web && npm run typecheck` passed.

## Changed files
- `internal/query/overview.go`
- `internal/query/query_test.go`
- `web/src/features/brief/BriefPage.tsx`
- `web/src/features/brief/BriefPage.test.tsx`

## Concerns
None.
