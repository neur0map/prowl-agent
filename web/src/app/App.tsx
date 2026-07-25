import { useEffect, useRef, useState } from 'preact/hooks'

import { BriefPage } from '../features/brief/BriefPage'
import { ContextLensPage } from '../features/context/ContextLensPage'
import { ExplorePage } from '../features/explore/ExplorePage'
import { ImpactPage } from '../features/impact/ImpactPage'
import { KnowledgePage } from '../features/knowledge/KnowledgePage'
import { SetupPage } from '../features/setup/SetupPage'
import { TimelinePage } from '../features/timeline/TimelinePage'
import { apiJSON } from '../transport/api'
import { sourceLink, type ContextLens, type GuidedTour } from '../transport/contracts'

const views = [
  ['Home', '#/'],
  ['Explore', '#/explore'],
  ['Context Lens', '#/context'],
  ['Impact', '#/impact'],
  ['Knowledge', '#/knowledge'],
  ['Timeline', '#/timeline'],
  ['Setup', '#/setup'],
] as const

type AppProps = {
  sessionError?: boolean
}

type Route = {
  path: string
  params: URLSearchParams
  key: string
}

type SourceRequest = {
  path: string
  lineStart: number
  lineEnd: number
  previewEnd: number
}

type SourcePreview = {
  path: string
  line_start: number
  line_end: number
  lines: Array<{ number: number; text: string }>
}

type LocalState<T> =
  | { kind: 'loading' }
  | { kind: 'ready'; value: T }
  | { kind: 'error' }

function readRoute(): Route {
  const raw = window.location.hash.startsWith('#') ? window.location.hash.slice(1) : '/'
  const route = new URL(raw.startsWith('/') ? raw : '/', window.location.origin)
  const isPrimaryRoute = views.some(([, href]) => href.slice(1) === route.pathname)
  return {
    path: isPrimaryRoute || route.pathname === '/source' ? route.pathname : '/',
    params: route.searchParams,
    key: `${route.pathname}${route.search}`,
  }
}

function validIdentifier(value: string | null): string | null {
  return value !== null && /^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$/.test(value) ? value : null
}

function parsePositiveInteger(value: string | null): number | null {
  return value !== null && /^[1-9]\d*$/.test(value) && Number.isSafeInteger(Number(value)) ? Number(value) : null
}

function parseSourceRequest(params: URLSearchParams): SourceRequest | null {
  const path = params.get('path')
  const lineStart = parsePositiveInteger(params.get('line_start'))
  const lineEnd = parsePositiveInteger(params.get('line_end'))
  const previewEnd = parsePositiveInteger(params.get('preview_end'))
  if (
    path === null
    || path.length === 0
    || path.length > 4096
    || path.startsWith('/')
    || path.includes('\\')
    || /[\u0000-\u001F\u007F-\u009F]/u.test(path)
    || path.split('/').some((part) => part === '' || part === '.' || part === '..')
    || lineStart === null
    || lineEnd === null
    || previewEnd === null
    || lineEnd < lineStart
    || previewEnd < lineStart
    || previewEnd > lineEnd
    || previewEnd - lineStart >= 400
  ) return null
  return { path, lineStart, lineEnd, previewEnd }
}

function parseContextIDs(params: URLSearchParams): string[] | null {
  const raw = params.get('ids')
  if (raw === null) return null
  const ids = raw.split(',')
  if (ids.length === 0 || ids.length > 32 || ids.some((id) => id.trim().length === 0 || /[\u0000-\u001F\u007F-\u009F]/u.test(id) || new TextEncoder().encode(id).length > 8 * 1024)) return null
  return ids
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

function isSourcePreview(value: unknown): value is SourcePreview {
  return isRecord(value)
    && typeof value.path === 'string'
    && Number.isInteger(value.line_start)
    && Number.isInteger(value.line_end)
    && Array.isArray(value.lines)
    && value.lines.every((line) => isRecord(line) && Number.isInteger(line.number) && typeof line.text === 'string')
}

function isGuidedTour(value: unknown): value is GuidedTour {
  return isRecord(value)
    && typeof value.id === 'string'
    && typeof value.title === 'string'
    && typeof value.description === 'string'
    && Array.isArray(value.steps)
    && value.steps.every((step) => isRecord(step)
      && typeof step.number === 'number'
      && Number.isInteger(step.number)
      && step.number > 0
      && typeof step.section_id === 'string'
      && typeof step.title === 'string'
      && typeof step.description === 'string'
      && Array.isArray(step.facts)
      && step.facts.every((fact) => isRecord(fact)
        && typeof fact.id === 'string'
        && typeof fact.label === 'string'
        && typeof fact.detail === 'string'
        && (fact.anchor === undefined || (isRecord(fact.anchor)
          && typeof fact.anchor.path === 'string'
          && !fact.anchor.path.startsWith('/')
          && !fact.anchor.path.includes('\\')
          && !fact.anchor.path.split('/').some((part) => part === '' || part === '.' || part === '..')
          && typeof fact.anchor.line_start === 'number'
          && Number.isInteger(fact.anchor.line_start)
          && fact.anchor.line_start > 0
          && typeof fact.anchor.line_end === 'number'
          && Number.isInteger(fact.anchor.line_end)
          && fact.anchor.line_end >= fact.anchor.line_start))))
}

function isContextLens(value: unknown): value is ContextLens {
  return isRecord(value)
    && typeof value.summary === 'string'
    && Array.isArray(value.items)
    && isRecord(value.budget)
    && Number.isFinite(value.budget.estimated_tokens)
    && Number.isFinite(value.budget.estimated_bytes)
    && Number.isFinite(value.budget.exact_bytes)
    && value.items.every((item) => isRecord(item)
      && typeof item.id === 'string'
      && typeof item.title === 'string'
      && typeof item.kind === 'string'
      && Array.isArray(item.why_selected)
      && item.why_selected.every((reason) => typeof reason === 'string')
      && Number.isFinite(item.estimated_tokens))
}

function SourcePreviewPage({ request }: { request: SourceRequest | null }) {
  const [state, setState] = useState<LocalState<SourcePreview>>(request === null ? { kind: 'error' } : { kind: 'loading' })

  useEffect(() => {
    let active = true
    if (request === null) {
      setState({ kind: 'error' })
      return () => { active = false }
    }
    setState({ kind: 'loading' })
    const query = new URLSearchParams({ path: request.path, line_start: String(request.lineStart), line_end: String(request.previewEnd) })
    void apiJSON<unknown>(`/api/v1/source?${query}`).then(
      (value) => { if (active) setState(isSourcePreview(value) ? { kind: 'ready', value } : { kind: 'error' }) },
      () => { if (active) setState({ kind: 'error' }) },
    )
    return () => { active = false }
  }, [request?.path, request?.lineStart, request?.previewEnd])

  return <section class="local-route" aria-label="Source preview">
    <span class="eyebrow">Source evidence</span>
    <h1>Source preview</h1>
    {request !== null ? <p class="route-anchor">Full source anchor: lines {request.lineStart}–{request.lineEnd}</p> : null}
    {state.kind === 'loading' ? <p role="status">Loading bounded source preview…</p> : null}
    {state.kind === 'error' ? <p role="alert">Source preview unavailable. Check the selected source link and try again.</p> : null}
    {state.kind === 'ready' && state.value.lines.length === 0 ? <p>No source lines were returned for this bounded preview.</p> : null}
    {state.kind === 'ready' && state.value.lines.length > 0 ? <pre aria-label={`Source preview for ${state.value.path}`}>{state.value.lines.map((line) => `${line.number}  ${line.text}`).join('\n')}</pre> : null}
  </section>
}

function GuidedTourPage({ tourID }: { tourID: string | null }) {
  const [state, setState] = useState<LocalState<GuidedTour>>(tourID === null ? { kind: 'error' } : { kind: 'loading' })

  useEffect(() => {
    let active = true
    if (tourID === null) {
      setState({ kind: 'error' })
      return () => { active = false }
    }
    setState({ kind: 'loading' })
    void apiJSON<unknown>(`/api/v1/tours/${encodeURIComponent(tourID)}`).then(
      (value) => { if (active) setState(isGuidedTour(value) ? { kind: 'ready', value } : { kind: 'error' }) },
      () => { if (active) setState({ kind: 'error' }) },
    )
    return () => { active = false }
  }, [tourID])

  if (state.kind === 'loading') return <section class="local-route" aria-label="Guided tour" aria-busy="true"><span class="eyebrow">Guided tour</span><h1>Guided tour</h1><p role="status">Loading source-backed guided tour…</p></section>
  if (state.kind === 'error') return <section class="local-route" aria-label="Guided tour"><span class="eyebrow">Guided tour</span><h1>Guided tour</h1><p role="alert">Guided tour unavailable. Check the selected tour and try again.</p></section>
  if (state.value.steps.length === 0) return <section class="local-route" aria-label="Guided tour"><span class="eyebrow">Guided tour</span><h1>{state.value.title}</h1><p>No source-backed steps are available for this tour.</p></section>
  return <section class="local-route" aria-label="Guided tour">
    <span class="eyebrow">Guided tour</span>
    <h1>{state.value.title}</h1>
    <p>{state.value.description}</p>
    <ol class="tour-steps">
      {state.value.steps.map((step) => <li key={`${step.number}-${step.section_id}`}>
        <h2>{step.number}. {step.title}</h2>
        <p>{step.description}</p>
        <ul>{step.facts.map((fact) => { const link = fact.anchor ? sourceLink(fact.anchor) : null; return <li key={fact.id}><strong>{fact.label}</strong><span>{fact.detail}</span>{link ? <a href={link.href}>{link.label}</a> : null}</li> })}</ul>
      </li>)}
    </ol>
  </section>
}

function ContextSelectionPage({ ids }: { ids: string[] | null }) {
  const [state, setState] = useState<LocalState<ContextLens>>(ids === null ? { kind: 'error' } : { kind: 'loading' })

  useEffect(() => {
    let active = true
    if (ids === null) {
      setState({ kind: 'error' })
      return () => { active = false }
    }
    setState({ kind: 'loading' })
    void apiJSON<unknown>('/api/v1/context/get', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ids }),
    }).then(
      (value) => { if (active) setState(isContextLens(value) ? { kind: 'ready', value } : { kind: 'error' }) },
      () => { if (active) setState({ kind: 'error' }) },
    )
    return () => { active = false }
  }, [ids?.join(',')])

  if (state.kind === 'loading') return <section class="local-route" aria-label="Context selection" aria-busy="true"><span class="eyebrow">Context selection</span><h1>Selected context</h1><p role="status">Loading bounded context selection…</p></section>
  if (state.kind === 'error') return <section class="local-route" aria-label="Context selection"><span class="eyebrow">Context selection</span><h1>Selected context</h1><p role="alert">Selected context unavailable. Check the selected items and try again.</p></section>
  if (state.value.items.length === 0) return <section class="local-route" aria-label="Context selection"><span class="eyebrow">Context selection</span><h1>Selected context</h1><p>{state.value.summary}</p><p>No context items were returned for this selection.</p></section>
  return <section class="local-route" aria-label="Context selection">
    <span class="eyebrow">Context selection</span>
    <h1>Selected context</h1>
    <p>{state.value.summary}</p>
    <ul class="context-packet">{state.value.items.map((item) => <li key={item.id}><h2>{item.title}</h2><p>{item.kind} · {item.estimated_tokens} estimated tokens</p><ul>{item.why_selected.map((reason) => <li key={reason}>{reason}</li>)}</ul></li>)}</ul>
  </section>
}

export function App({ sessionError = false }: AppProps) {
  const [route, setRoute] = useState(readRoute)
  const [reloadKey, setReloadKey] = useState(0)
  const main = useRef<HTMLElement>(null)

  useEffect(() => {
    const updateRoute = () => setRoute(readRoute())
    window.addEventListener('hashchange', updateRoute)
    return () => window.removeEventListener('hashchange', updateRoute)
  }, [])

  useEffect(() => {
    main.current?.focus()
  }, [route.key, reloadKey])

  const activeHref = views.find(([, href]) => href.slice(1) === route.path)?.[1]
  const reloadLabel = route.path === '/source' ? 'Retry source preview' : route.path === '/explore' && route.params.has('tour') ? 'Retry guided tour' : route.path === '/context' && route.params.has('ids') ? 'Retry context selection' : 'Reload current view'
  let view = <BriefPage />
  if (route.path === '/explore') view = route.params.has('tour') ? <GuidedTourPage tourID={validIdentifier(route.params.get('tour'))} /> : <ExplorePage />
  else if (route.path === '/context') view = route.params.has('ids') ? <ContextSelectionPage ids={parseContextIDs(route.params)} /> : <ContextLensPage />
  else if (route.path === '/impact') view = <ImpactPage />
  else if (route.path === '/knowledge') view = <KnowledgePage proposalID={validIdentifier(route.params.get('proposal')) ?? undefined} />
  else if (route.path === '/timeline') view = <TimelinePage />
  else if (route.path === '/setup') view = <SetupPage />
  else if (route.path === '/source') view = <SourcePreviewPage request={parseSourceRequest(route.params)} />

  return (
    <div class="workbench-shell">
      <a class="skip-link" href="#main-content">Skip to content</a>
      <aside class="sidebar" aria-label="Workbench navigation">
        <div class="brand">
          <span class="brand-mark" aria-hidden="true">P</span>
          <div>
            <span class="eyebrow">Local workspace</span>
            <strong>Prowl Workbench</strong>
          </div>
        </div>
        <nav aria-label="Primary">
          <ul>
            {views.map(([label, href]) => <li key={href}><a href={href} aria-current={href === activeHref ? 'page' : undefined}>{label}</a></li>)}
          </ul>
        </nav>
        <p class="privacy-note">Loopback only</p>
      </aside>

      <main id="main-content" tabIndex={-1} ref={main}>
        {sessionError ? (
          <section class="brief-state" aria-label="Secure workbench session">
            <span class="eyebrow">Session security</span>
            <h1>Secure workbench session unavailable</h1>
            <p role="alert">Secure workbench session unavailable. Reopen Prowl from your terminal.</p>
          </section>
        ) : <div key={reloadKey}>{view}<p class="route-reload"><button type="button" onClick={() => setReloadKey((key) => key + 1)}>{reloadLabel}</button></p></div>}
      </main>
    </div>
  )
}
