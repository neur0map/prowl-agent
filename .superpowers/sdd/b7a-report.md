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
#/source?path=${encodeURIComponent(projectRelativePath)}&line_start=<start>&line_end=<full-end>&preview_end=<bounded-preview-end>
```

The hash retains the complete project-relative source anchor and carries a bounded preview end for source requests. The Explore test proves the anchor exposes that hash, receives keyboard focus, and activates as a native link.

## Self-review and concerns

Reviewed all seven scoped implementation/test files and the shared DTO contract. B7a keeps API clients injectable, exposes privacy-safe errors only, and renders server-computed facts without browser graph/context/knowledge computation.

Concerns: none.

## Remediation

### RED evidence

- `cd web && npm test -- --run src/features/explore/ExplorePage.test.tsx` failed after the shell-route assertion changed: the link was `#source?...`, not `#/source?...`.
- `cd web && npm test -- --run src/features/context/ContextLensPage.test.tsx` failed its two-search race: the late first response replaced the authoritative second result.
- `cd web && npm test -- --run src/features/impact/ImpactPage.test.tsx` failed its two-submission race: the late first response replaced the authoritative second result.
- Each feature-specific command then failed its malformed response and missing `meta` cases because the initial loaders accepted incomplete envelopes; Explore and Context also exposed unsafe nested render paths before validation.

### GREEN evidence

- Source links now use the established `#/source?path=<encoded-project-relative-path>&line_start=<start>&line_end=<full-end>&preview_end=<bounded-preview-end>` grammar.
- Context Lens and Impact retain a request identity and accept only the latest completion.
- Explore validates envelope metadata plus every rendered workspace, section, fact, anchor, and tour field.
- Context Lens validates envelope metadata, packet fields, all items, and all citations.
- Impact validates envelope metadata and the complete nested Impact DTO before rendering.
- `cd web && npm test -- --run src/features/explore src/features/context src/features/impact` passed: 3 files, 20 tests.
- `cd web && npm run typecheck` passed.

Remediation concerns: none.

### Source preview bound

- RED: `cd web && npm test -- --run src/features/explore/ExplorePage.test.tsx` failed because a source anchor from line 8 through 1000 generated an unbounded `line_end=1000` hash.
- GREEN: `sourceLink` keeps the original `SourceTarget` reference and clamps only the preview hash end to `line_start + 399`, matching the local `maxSourcePreviewLines = 400` compatibility constant for `internal/workbench.MaxSourcePreviewLines`.

### Full source range route state

- RED: `cd web && npm test -- --run src/features/explore/ExplorePage.test.tsx` failed because the previous bounded hash rewrote `line_end=1000` to `line_end=407`, losing the exact citation range.
- GREEN: the route now preserves the original `line_start` and `line_end`, while `preview_end` alone is bounded to `line_start + 399`. `SourceLink.previewEnd` makes the transport compatibility value explicit without mutating the typed anchor.
- `cd web && npm test -- --run src/features/explore src/features/context src/features/impact` passed: 3 files, 21 tests.
- `cd web && npm run typecheck` passed.
