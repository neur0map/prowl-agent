import { useEffect, useRef, useState } from 'preact/hooks'

import { apiFetch } from '../../transport/api'
import { sourceLink, type APIEnvelope } from '../../transport/contracts'

type KnowledgeAnchor = { path: string; line_start: number; line_end: number; content_hash?: string; symbol?: string }
type KnowledgeSummary = { id: string; path: string; type: string; title: string; description?: string; resource?: string; tags: string[]; timestamp?: string; status?: string; confidence?: string; related: string[]; anchors: KnowledgeAnchor[] }
type KnowledgePageData = { items: KnowledgeSummary[]; next: string }
type KnowledgeDetail = KnowledgeSummary & { body: string; backlinks: Array<{ id: string; path: string; type: string; title: string }> }
type KnowledgeProposal = { proposal: { id: string; title?: string }; diff: string; version: string }
type KnowledgeDecision = { version: string; idempotent: boolean }
type KnowledgeDecisionRequest = { expected_version: string; idempotency_key: string; confirm: true }

export type KnowledgeClient = {
  loadPage: (cursor?: string) => Promise<KnowledgePageData>
  loadDetail: (id: string) => Promise<KnowledgeDetail>
  loadProposal: (id: string) => Promise<KnowledgeProposal>
  decide: (id: string, action: 'accept' | 'reject', request: KnowledgeDecisionRequest) => Promise<KnowledgeDecision>
}

type LoadState<T> = { kind: 'loading' } | { kind: 'ready'; value: T } | { kind: 'error' }
type ProposalState = { kind: 'idle' } | { kind: 'loading' } | { kind: 'ready'; value: KnowledgeProposal } | { kind: 'error' }

async function envelope<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await apiFetch(path, init)
  if (!response.ok) throw new Error('request failed')
  const payload: unknown = await response.json()
  if (!isEnvelope<T>(payload)) throw new Error('invalid response')
  return payload.data
}

function isEnvelope<T>(value: unknown): value is APIEnvelope<T> {
  return typeof value === 'object' && value !== null && 'data' in value && 'meta' in value
}

const defaultClient: KnowledgeClient = {
  loadPage: (cursor = '') => envelope<KnowledgePageData>(`/api/v1/knowledge${cursor === '' ? '' : `?cursor=${encodeURIComponent(cursor)}`}`),
  loadDetail: (id) => envelope<KnowledgeDetail>(`/api/v1/knowledge/${encodeURIComponent(id)}`),
  loadProposal: (id) => envelope<KnowledgeProposal>(`/api/v1/knowledge/proposals/${encodeURIComponent(id)}`),
  decide: (id, action, request) => envelope<KnowledgeDecision>(`/api/v1/knowledge/proposals/${encodeURIComponent(id)}/${action}`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(request),
  }),
}

export function createIdempotencyKey(): string {
  return crypto.randomUUID()
}

export function KnowledgePage({ client = defaultClient, proposalID, createIdempotencyKey: newKey = createIdempotencyKey }: {
  client?: KnowledgeClient
  proposalID?: string
  createIdempotencyKey?: () => string
}) {
  const [page, setPage] = useState<LoadState<KnowledgePageData>>({ kind: 'loading' })
  const [detail, setDetail] = useState<LoadState<KnowledgeDetail> | null>(null)
  const [loadingMore, setLoadingMore] = useState(false)
  const [proposal, setProposal] = useState<ProposalState>({ kind: 'idle' })
  const [reviewed, setReviewed] = useState(false)
  const [outcome, setOutcome] = useState('')
  const [conflict, setConflict] = useState(false)
  const conflictAlert = useRef<HTMLParagraphElement>(null)

  useEffect(() => {
    let active = true
    void client.loadPage().then(
      (value) => { if (active) setPage({ kind: 'ready', value }) },
      () => { if (active) setPage({ kind: 'error' }) },
    )
    return () => { active = false }
  }, [client])

  useEffect(() => {
    if (!proposalID) return
    let active = true
    setProposal({ kind: 'loading' })
    void client.loadProposal(proposalID).then(
      (value) => { if (active) setProposal({ kind: 'ready', value }) },
      () => { if (active) setProposal({ kind: 'error' }) },
    )
    return () => { active = false }
  }, [client, proposalID])

  useEffect(() => { if (conflict) conflictAlert.current?.focus() }, [conflict])

  function selectDetail(id: string) {
    setDetail({ kind: 'loading' })
    void client.loadDetail(id).then(
      (value) => setDetail({ kind: 'ready', value }),
      () => setDetail({ kind: 'error' }),
    )
  }

  function loadMore() {
    if (page.kind !== 'ready' || page.value.next === '' || loadingMore) return
    const current = page.value
    setLoadingMore(true)
    void client.loadPage(current.next).then(
      (next) => setPage({ kind: 'ready', value: { items: [...current.items, ...next.items], next: next.next } }),
      () => setPage({ kind: 'error' }),
    ).finally(() => setLoadingMore(false))
  }

  function decide(action: 'accept' | 'reject') {
    if (proposal.kind !== 'ready' || !reviewed) return
    setConflict(false)
    setOutcome('')
    void client.decide(proposalID ?? proposal.value.proposal.id, action, {
      expected_version: proposal.value.version, idempotency_key: newKey(), confirm: true,
    }).then(
      () => setOutcome(`Proposal ${action === 'accept' ? 'accepted' : 'rejected'}.`),
      (error: unknown) => { if (errorCode(error) === 'conflict') setConflict(true) },
    )
  }

  return <section aria-label="Knowledge">
    <header><h1>Knowledge</h1><p>Browse canonical knowledge and review server-provided proposals.</p></header>
    {page.kind === 'loading' ? <p role="status">Loading knowledge…</p> : null}
    {page.kind === 'error' ? <p role="alert">Knowledge is unavailable. Try again.</p> : null}
    {page.kind === 'ready' && page.value.items.length === 0 ? <p>No knowledge documents are available.</p> : null}
    {page.kind === 'ready' && page.value.items.length > 0 ? <><ul aria-label="Knowledge documents">{page.value.items.map((item) => <li key={item.id}><button type="button" onClick={() => selectDetail(item.id)}>{item.title}</button><p>{item.description}</p></li>)}</ul>{page.value.next !== '' ? <button type="button" disabled={loadingMore} onClick={loadMore}>Load more knowledge</button> : null}</> : null}
    {detail?.kind === 'loading' ? <p role="status">Loading document…</p> : null}
    {detail?.kind === 'error' ? <p role="alert">Document details are unavailable. Try again.</p> : null}
    {detail?.kind === 'ready' ? <article aria-label="Knowledge detail"><h2>{detail.value.title}</h2><pre>{detail.value.body}</pre>{detail.value.anchors.map((anchor) => { const link = sourceLink(anchor); return <a key={`${anchor.path}:${anchor.line_start}`} href={link.href}>{link.label}</a> })}</article> : null}
    {proposal.kind === 'loading' ? <p role="status">Loading proposal…</p> : null}
    {proposal.kind === 'error' ? <p role="alert">Proposal is unavailable. Try again.</p> : null}
    {proposal.kind === 'ready' ? <section aria-label="Proposal review"><h2>{proposal.value.proposal.title ?? 'Knowledge proposal'}</h2><pre>{proposal.value.diff}</pre><label><input type="checkbox" checked={reviewed} onInput={(event) => setReviewed(event.currentTarget.checked)} />I have reviewed this proposal diff</label><div><button type="button" disabled={!reviewed} onClick={() => decide('accept')}>Accept proposal</button><button type="button" disabled={!reviewed} onClick={() => decide('reject')}>Reject proposal</button></div></section> : null}
    {outcome ? <p role="status">{outcome}</p> : null}
    {conflict ? <p ref={conflictAlert} role="alert" tabIndex={-1}>The proposal changed before your decision. Review the displayed diff and try again.</p> : null}
  </section>
}

function errorCode(error: unknown): string | undefined {
  return typeof error === 'object' && error !== null && 'code' in error && typeof error.code === 'string' ? error.code : undefined
}
