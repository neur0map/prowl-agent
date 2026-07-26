import { useEffect, useRef, useState } from 'preact/hooks'

import { BriefPage } from '../features/brief/BriefPage'
import { ContextLensPage } from '../features/context/ContextLensPage'
import { ExplorePage } from '../features/explore/ExplorePage'
import { ImpactPage } from '../features/impact/ImpactPage'
import { KnowledgePage } from '../features/knowledge/KnowledgePage'
import { SetupPage } from '../features/setup/SetupPage'
import { JobStatus } from '../features/jobs/JobStatus'
import { TimelinePage } from '../features/timeline/TimelinePage'
import { apiJSON } from '../transport/api'
import { sourceLink, type ContextLens, type GuidedTour } from '../transport/contracts'
import { useI18n, type MessageKey } from '../i18n'

const views = [
  ['nav.home', '#/'],
  ['nav.explore', '#/explore'],
  ['nav.context', '#/context'],
  ['nav.impact', '#/impact'],
  ['nav.knowledge', '#/knowledge'],
  ['nav.timeline', '#/timeline'],
  ['nav.setup', '#/setup'],
] as const satisfies ReadonlyArray<readonly [MessageKey, string]>

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
    || new TextEncoder().encode(path).length > 4096
    || path.startsWith('/')
    || /^[A-Za-z]:/.test(path)
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

function isSourcePreview(value: unknown, request: SourceRequest): value is SourcePreview {
  return isRecord(value)
    && value.path === request.path
    && typeof value.line_start === 'number'
    && value.line_start === request.lineStart
    && typeof value.line_end === 'number'
    && value.line_end === request.previewEnd
    && Array.isArray(value.lines)
    && value.lines.length === request.previewEnd - request.lineStart + 1
    && value.lines.every((line, index) => isRecord(line)
      && typeof line.number === 'number'
      && line.number === request.lineStart + index
      && typeof line.text === 'string')
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
  const { t } = useI18n()
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
      (value) => { if (active) setState(isSourcePreview(value, request) ? { kind: 'ready', value } : { kind: 'error' }) },
      () => { if (active) setState({ kind: 'error' }) },
    )
    return () => { active = false }
  }, [request?.path, request?.lineStart, request?.previewEnd])

  return <section class="local-route" aria-label={t('app.sourcePreviewAria')}>
    <span class="eyebrow">{t('app.sourceEvidence')}</span>
    <h1>{t('app.sourcePreview')}</h1>
    {request !== null ? <p class="route-anchor">{t('app.sourceAnchor', { start: request.lineStart, end: request.lineEnd })}</p> : null}
    {state.kind === 'loading' ? <p role="status">{t('app.sourceLoading')}</p> : null}
    {state.kind === 'error' ? <p role="alert">{t('app.sourceUnavailable')}</p> : null}
    {state.kind === 'ready' && state.value.lines.length === 0 ? <p>{t('app.sourceEmpty')}</p> : null}
    {state.kind === 'ready' && state.value.lines.length > 0 ? <pre aria-label={t('app.sourcePreviewFor', { path: state.value.path })}>{state.value.lines.map((line) => `${line.number}  ${line.text}`).join('\n')}</pre> : null}
  </section>
}

function GuidedTourPage({ tourID }: { tourID: string | null }) {
  const { t } = useI18n()
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

  if (state.kind === 'loading') return <section class="local-route" aria-label={t('app.guidedTour')} aria-busy="true"><span class="eyebrow">{t('app.guidedTour')}</span><h1>{t('app.guidedTour')}</h1><p role="status">{t('app.guidedTourLoading')}</p></section>
  if (state.kind === 'error') return <section class="local-route" aria-label={t('app.guidedTour')}><span class="eyebrow">{t('app.guidedTour')}</span><h1>{t('app.guidedTour')}</h1><p role="alert">{t('app.guidedTourUnavailable')}</p></section>
  if (state.value.steps.length === 0) return <section class="local-route" aria-label={t('app.guidedTour')}><span class="eyebrow">{t('app.guidedTour')}</span><h1>{state.value.title}</h1><p>{t('app.guidedTourEmpty')}</p></section>
  return <section class="local-route" aria-label={t('app.guidedTour')}>
    <span class="eyebrow">{t('app.guidedTour')}</span>
    <h1>{state.value.title}</h1>
    <p>{state.value.description}</p>
    <ol class="tour-steps">
      {state.value.steps.map((step) => <li key={`${step.number}-${step.section_id}`}>
        <h2>{step.number}. {step.title}</h2>
        <p>{step.description}</p>
        <ul>{step.facts.map((fact) => { const link = fact.anchor ? sourceLink(fact.anchor) : null; return <li key={fact.id}><strong>{fact.label}</strong><span>{fact.detail}</span>{link ? <a href={link.href}>{t('app.sourceLink', { path: link.target.path, start: link.target.line_start, end: link.target.line_end })}</a> : null}</li> })}</ul>
      </li>)}
    </ol>
  </section>
}

function ContextSelectionPage({ ids }: { ids: string[] | null }) {
  const { t, formatNumber } = useI18n()
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

  if (state.kind === 'loading') return <section class="local-route" aria-label={t('app.contextSelection')} aria-busy="true"><span class="eyebrow">{t('app.contextSelection')}</span><h1>{t('app.selectedContext')}</h1><p role="status">{t('app.contextSelectionLoading')}</p></section>
  if (state.kind === 'error') return <section class="local-route" aria-label={t('app.contextSelection')}><span class="eyebrow">{t('app.contextSelection')}</span><h1>{t('app.selectedContext')}</h1><p role="alert">{t('app.contextSelectionUnavailable')}</p></section>
  if (state.value.items.length === 0) return <section class="local-route" aria-label={t('app.contextSelection')}><span class="eyebrow">{t('app.contextSelection')}</span><h1>{t('app.selectedContext')}</h1><p>{state.value.summary}</p><p>{t('app.contextSelectionEmpty')}</p></section>
  return <section class="local-route" aria-label={t('app.contextSelection')}>
    <span class="eyebrow">{t('app.contextSelection')}</span>
    <h1>{t('app.selectedContext')}</h1>
    <p>{state.value.summary}</p>
    <ul class="context-packet">{state.value.items.map((item) => <li key={item.id}><h2>{item.title}</h2><p>{t('app.contextItemMeta', { kind: item.kind, tokens: formatNumber(item.estimated_tokens) })}</p><ul>{item.why_selected.map((reason) => <li key={reason}>{reason}</li>)}</ul></li>)}</ul>
  </section>
}

export function App({ sessionError = false }: AppProps) {
  const { t } = useI18n()
  const [route, setRoute] = useState(readRoute)
  const [reloadKey, setReloadKey] = useState(0)
  const main = useRef<HTMLElement>(null)
  const initialFocus = useRef(true)

  useEffect(() => {
    const updateRoute = () => setRoute(readRoute())
    window.addEventListener('hashchange', updateRoute)
    return () => window.removeEventListener('hashchange', updateRoute)
  }, [])

  useEffect(() => {
    if (initialFocus.current) {
      initialFocus.current = false
      return
    }
    main.current?.focus()
  }, [route.key, reloadKey])

  const activeHref = views.find(([, href]) => href.slice(1) === route.path)?.[1]
  const reloadLabel = route.path === '/source' ? t('app.retrySourcePreview') : route.path === '/explore' && route.params.has('tour') ? t('app.retryGuidedTour') : route.path === '/context' && route.params.has('ids') ? t('app.retryContextSelection') : t('app.reloadCurrentView')
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
      <a class="skip-link" href="#main-content" onClick={(event) => { event.preventDefault(); main.current?.focus() }}>{t('app.skipToContent')}</a>
      <aside class="sidebar" aria-label={t('app.navigation')}>
        <div class="brand">
          <span class="brand-mark" aria-hidden="true">P</span>
          <div>
            <span class="eyebrow">{t('app.localWorkspace')}</span>
            <strong>{t('app.workbench')}</strong>
          </div>
        </div>
        <nav aria-label={t('app.primaryNavigation')}>
          <ul>
            {views.map(([label, href]) => <li key={href}><a href={href} aria-current={href === activeHref ? 'page' : undefined}>{t(label)}</a></li>)}
          </ul>
        </nav>
        <p class="privacy-note">{t('app.loopbackOnly')}</p>
      </aside>

      <main id="main-content" tabIndex={-1} ref={main}>
        {sessionError ? (
          <section class="brief-state" aria-label={t('app.secureSessionUnavailable')}>
            <span class="eyebrow">{t('app.sessionSecurity')}</span>
            <h1>{t('app.secureSessionUnavailable')}</h1>
            <p role="alert">{t('app.secureSessionUnavailableDetail')}</p>
          </section>
        ) : <><JobStatus onInvalidate={() => setReloadKey((key) => key + 1)} /><div key={reloadKey}>{view}<p class="route-reload"><button type="button" onClick={() => setReloadKey((key) => key + 1)}>{reloadLabel}</button></p></div></>}
      </main>
    </div>
  )
}
