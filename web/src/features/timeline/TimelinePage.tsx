import { useEffect, useState } from 'preact/hooks'

import { apiFetch } from '../../transport/api'
import type { APIEnvelope } from '../../transport/contracts'

type TimelineGitCommit = { commit: string; subject: string }
type TimelineKnowledgeLog = { action: string; path: string }
type TimelineContextTrace = { run_id: string; query_hash: string; hash_version: string; mode: string; budget_tokens: number; budget_bytes: number; estimated_tokens: number; estimated_bytes: number; strategy_version: string; status: string; error_code?: string }
type TimelineEvent = { id: string; occurred_at: string; kind: string; provenance: string; git?: TimelineGitCommit; knowledge?: TimelineKnowledgeLog; context?: TimelineContextTrace }
type TimelineData = { events: TimelineEvent[]; next: string }

export type TimelineClient = { loadPage: (cursor?: string) => Promise<TimelineData> }

type TimelineState = { kind: 'loading'; events: TimelineEvent[] } | { kind: 'ready'; events: TimelineEvent[]; next: string } | { kind: 'error'; events: TimelineEvent[] }

const defaultClient: TimelineClient = {
  async loadPage(cursor = '') {
    const response = await apiFetch(`/api/v1/timeline${cursor === '' ? '' : `?cursor=${encodeURIComponent(cursor)}`}`)
    if (!response.ok) throw new Error('request failed')
    const payload: unknown = await response.json()
    if (typeof payload !== 'object' || payload === null || !('data' in payload) || !('meta' in payload)) throw new Error('invalid response')
    return (payload as APIEnvelope<TimelineData>).data
  },
}

export function TimelinePage({ client = defaultClient }: { client?: TimelineClient }) {
  const [state, setState] = useState<TimelineState>({ kind: 'loading', events: [] })

  useEffect(() => {
    let active = true
    void client.loadPage().then(
      (value) => { if (active) setState({ kind: 'ready', events: value.events, next: value.next }) },
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
      (value) => setState({ kind: 'ready', events: [...prior, ...value.events], next: value.next }),
      () => setState({ kind: 'error', events: prior }),
    )
  }

  return <section aria-label="Timeline">
    <header><h1>Timeline</h1><p>Server-supplied provenance events, newest first.</p></header>
    {state.kind === 'loading' ? <p role="status">Loading timeline…</p> : null}
    {state.kind === 'error' ? <p role="alert">Timeline is unavailable. Try again.</p> : null}
    {state.kind === 'ready' && state.events.length === 0 ? <p>No timeline events are available.</p> : null}
    {state.events.length > 0 ? <ol>{state.events.map((event) => <li key={event.id}><strong>{event.provenance}</strong><time dateTime={event.occurred_at}>{event.occurred_at}</time>{event.git ? <><span>{event.git.subject}</span><code>{event.git.commit}</code></> : null}{event.knowledge ? <><span>{event.knowledge.action}</span><code>{event.knowledge.path}</code></> : null}{event.context ? <dl><dt>Run ID</dt><dd>{event.context.run_id}</dd><dt>Query hash</dt><dd>{event.context.query_hash}</dd><dt>Hash version</dt><dd>{event.context.hash_version}</dd><dt>Mode</dt><dd>{event.context.mode}</dd><dt>Token budget</dt><dd>{event.context.budget_tokens}</dd><dt>Byte budget</dt><dd>{event.context.budget_bytes}</dd><dt>Estimated tokens</dt><dd>{event.context.estimated_tokens}</dd><dt>Estimated bytes</dt><dd>{event.context.estimated_bytes}</dd><dt>Strategy version</dt><dd>{event.context.strategy_version}</dd><dt>Status</dt><dd>{event.context.status}</dd>{event.context.error_code ? <><dt>Error code</dt><dd>{event.context.error_code}</dd></> : null}</dl> : null}</li>)}</ol> : null}
    {state.kind === 'ready' && state.next !== '' ? <button type="button" onClick={loadNext}>Load more timeline events</button> : null}
  </section>
}
