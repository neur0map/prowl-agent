# B7a report - Read-only analysis views

## Commit

- `12b7a7d` - `feat(workbench): add source-backed analysis views`

## Files changed

- `web/src/transport/contracts.ts`
- `web/src/features/explore/ExplorePage.tsx`
- `web/src/features/explore/ExplorePage.test.tsx`
- `web/src/features/context/ContextLensPage.tsx`
- `web/src/features/context/ContextLensPage.test.tsx`
- `web/src/features/impact/ImpactPage.tsx`
- `web/src/features/impact/ImpactPage.test.tsx`

## TDD evidence

Each feature test file was created before its component implementation and was run RED first:

| Feature | RED command | Observed result |
| --- | --- | --- |
| Explore | `cd web && npm test -- --run src/features/explore/ExplorePage.test.tsx` | Failed because `./ExplorePage` did not exist. |
| Context Lens | `cd web && npm test -- --run src/features/context/ContextLensPage.test.tsx` | Failed because `./ContextLensPage` did not exist. |
| Impact | `cd web && npm test -- --run src/features/impact/ImpactPage.test.tsx` | Failed because `./ImpactPage` did not exist. |

Each corresponding focused test then passed after its minimal component implementation. The initial typecheck caught use of `Promise.withResolvers`, which is unavailable under the project's ES2022 lib target; loading tests now use the standard permanently-pending `Promise.race<T>([])` instead.

## Final verification

- `cd web && npm test -- --run src/features/explore src/features/context src/features/impact` - PASS, 3 files / 12 tests.
- `cd web && npm run typecheck` - PASS.

## Source navigation

Source evidence uses a native `<a>` with this deterministic, same-origin hash:

```text
#source?path=${encodeURIComponent(projectRelativePath)}&line_start=<start>&line_end=<end>
```

The hash carries only the project-relative path and server-provided line range. The Explore test proves the anchor exposes that hash, receives keyboard focus, and activates as a native link.

## Self-review and concerns

Reviewed all seven scoped implementation/test files and the shared DTO contract. B7a keeps API clients injectable, exposes privacy-safe errors only, and renders server-computed facts without browser graph/context/knowledge computation.

Concerns: none.
