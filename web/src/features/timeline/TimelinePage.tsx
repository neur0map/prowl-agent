import { useEffect, useState } from 'preact/hooks'

import { apiFetch } from '../../transport/api'
import { useI18n } from '../../i18n'

type TimelineGitCommit = { commit: string; subject: string }
type TimelineKnowledgeLog = { action: string; path: string }
type TimelineContextTrace = { run_id: string; query_hash: string; hash_version: string; mode: string; budget_tokens: number; budget_bytes: number; estimated_tokens: number; estimated_bytes: number; strategy_version: string; status: string; error_code?: string }
type TimelineEvent = { id: string; occurred_at: string; kind: string; provenance: string; git?: TimelineGitCommit; knowledge?: TimelineKnowledgeLog; context?: TimelineContextTrace }
type TimelineData = { events: TimelineEvent[]; next: string }

export type TimelineClient = { loadPage: (cursor?: string) => Promise<TimelineData> }
type TimelineState = { kind: 'loading'; events: TimelineEvent[] } | { kind: 'ready'; events: TimelineEvent[]; next: string } | { kind: 'error'; events: TimelineEvent[] }

async function loadTimeline(cursor = ''): Promise<TimelineData> {
  const response = await apiFetch(`/api/v1/timeline${cursor === '' ? '' : `?cursor=${encodeURIComponent(cursor)}`}`)
  const payload: unknown = await response.json().catch(() => null)
  if (!response.ok || !isEnvelope(payload) || !isTimelineData(payload.data)) throw new Error('timeline unavailable')
  return { events: payload.data.events, next: payload.data.next ?? '' }
}

const defaultClient: TimelineClient = { loadPage: loadTimeline }

export function TimelinePage({ client = defaultClient }: { client?: TimelineClient }) {
  const { t, formatNumber } = useI18n()
  const [state, setState] = useState<TimelineState>({ kind: 'loading', events: [] })

  useEffect(() => {
    let active = true
    void client.loadPage().then(
      (value) => { if (active) setState({ kind: 'ready', events: value.events, next: value.next ?? '' }) },
      () => { if (active) setState({ kind: 'error', events: [] }) },
    )
    return () => { active = false }
  }, [client])

  function loadNext() {
    if (state.kind !== 'ready' || state.next === '') return
    const prior = state.events
    const cursor = state.next
    setState({ kind: 'loading', events: prior })
    void client.loadPage(cursor).then(
      (value) => setState({ kind: 'ready', events: [...prior, ...value.events], next: value.next ?? '' }),
      () => setState({ kind: 'error', events: prior }),
    )
  }

  return <section aria-label={t('timeline.aria')}>
    <header><h1>{t('timeline.heading')}</h1><p>{t('timeline.description')}</p></header>
    {state.kind === 'loading' ? <p role="status">{t('timeline.loading')}</p> : null}
    {state.kind === 'error' ? <p role="alert">{t('timeline.unavailable')}</p> : null}
    {state.kind === 'ready' && state.events.length === 0 ? <p>{t('timeline.empty')}</p> : null}
    {state.events.length > 0 ? <ol>{state.events.map((event) => <li key={event.id}><strong>{event.provenance}</strong><time dateTime={event.occurred_at}>{event.occurred_at}</time>{event.git ? <><span>{event.git.subject}</span><code>{event.git.commit}</code></> : null}{event.knowledge ? <><span>{event.knowledge.action}</span><code>{event.knowledge.path}</code></> : null}{event.context ? <dl><dt>{t('timeline.runID')}</dt><dd>{event.context.run_id}</dd><dt>{t('timeline.queryHash')}</dt><dd>{event.context.query_hash}</dd><dt>{t('timeline.hashVersion')}</dt><dd>{event.context.hash_version}</dd><dt>{t('timeline.mode')}</dt><dd>{event.context.mode}</dd><dt>{t('timeline.tokenBudget')}</dt><dd>{formatNumber(event.context.budget_tokens)}</dd><dt>{t('timeline.byteBudget')}</dt><dd>{formatNumber(event.context.budget_bytes)}</dd><dt>{t('timeline.estimatedTokens')}</dt><dd>{formatNumber(event.context.estimated_tokens)}</dd><dt>{t('timeline.estimatedBytes')}</dt><dd>{formatNumber(event.context.estimated_bytes)}</dd><dt>{t('timeline.strategyVersion')}</dt><dd>{event.context.strategy_version}</dd><dt>{t('timeline.status')}</dt><dd>{event.context.status}</dd>{event.context.error_code ? <><dt>{t('timeline.errorCode')}</dt><dd>{event.context.error_code}</dd></> : null}</dl> : null}</li>)}</ol> : null}
    {state.kind === 'ready' && state.next !== '' ? <button type="button" onClick={loadNext}>{t('timeline.loadMore')}</button> : null}
  </section>
}

function isEnvelope(value: unknown): value is { data: unknown; meta: Record<string, unknown> } { return isRecord(value) && 'data' in value && isRecord(value.meta) }
function isTimelineData(value: unknown): value is { events: TimelineEvent[]; next?: string } { return isRecord(value) && Array.isArray(value.events) && value.events.every(isTimelineEvent) && (value.next === undefined || typeof value.next === 'string') }
function isTimelineEvent(value: unknown): value is TimelineEvent { return isRecord(value) && isString(value.id) && isString(value.occurred_at) && isString(value.kind) && isString(value.provenance) && (value.git === undefined || isGit(value.git)) && (value.knowledge === undefined || isKnowledge(value.knowledge)) && (value.context === undefined || isContext(value.context)) }
function isGit(value: unknown): value is TimelineGitCommit { return isRecord(value) && isString(value.commit) && isString(value.subject) }
function isKnowledge(value: unknown): value is TimelineKnowledgeLog { return isRecord(value) && isString(value.action) && isString(value.path) }
function isContext(value: unknown): value is TimelineContextTrace { return isRecord(value) && isString(value.run_id) && isString(value.query_hash) && isString(value.hash_version) && isString(value.mode) && isFiniteNumber(value.budget_tokens) && isFiniteNumber(value.budget_bytes) && isFiniteNumber(value.estimated_tokens) && isFiniteNumber(value.estimated_bytes) && isString(value.strategy_version) && isString(value.status) && (value.error_code === undefined || isString(value.error_code)) }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null }
function isString(value: unknown): value is string { return typeof value === 'string' }
function isFiniteNumber(value: unknown): value is number { return typeof value === 'number' && Number.isFinite(value) }
