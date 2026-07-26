import { useEffect, useRef, useState } from 'preact/hooks'

import { apiFetch } from '../../transport/api'
import { sourceLink } from '../../transport/contracts'
import { useI18n } from '../../i18n'

type KnowledgeAnchor = { path: string; line_start: number; line_end: number; content_hash?: string; symbol?: string }
type KnowledgeSummary = { id: string; path: string; type: string; title: string; description?: string; resource?: string; tags: string[]; timestamp?: string; status?: string; confidence?: string; related: string[]; anchors: KnowledgeAnchor[] }
type KnowledgePageData = { items: KnowledgeSummary[]; next: string }
type KnowledgeDetail = KnowledgeSummary & { body: string; backlinks: Array<{ id: string; path: string; type: string; title: string }> }
type KnowledgeProposal = { proposal: { id: string; operation: string; target_path: string; candidate_path: string; status: string; created_at: string }; diff: string; version: string }
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

class ClientError extends Error {
  constructor(readonly code?: string) { super('request failed') }
}

async function request(path: string, init?: RequestInit): Promise<unknown> {
  const response = await apiFetch(path, init)
  const payload: unknown = await response.json().catch(() => null)
  if (!response.ok) throw new ClientError(errorCode(payload))
  if (!isEnvelope(payload)) throw new ClientError()
  return payload.data
}

export async function loadKnowledgePage(cursor = ''): Promise<KnowledgePageData> {
  const value = await request(`/api/v1/knowledge${cursor === '' ? '' : `?cursor=${encodeURIComponent(cursor)}`}`)
  if (!isKnowledgePage(value)) throw new ClientError()
  return { items: value.items.map(normalizeSummary), next: value.next ?? '' }
}

export async function loadKnowledgeDetail(id: string): Promise<KnowledgeDetail> {
  const value = await request(`/api/v1/knowledge/${encodeURIComponent(id)}`)
  if (!isKnowledgeDetail(value)) throw new ClientError()
  return { ...normalizeSummary(value), body: value.body, backlinks: value.backlinks }
}

const defaultClient: KnowledgeClient = {
  loadPage: loadKnowledgePage,
  loadDetail: loadKnowledgeDetail,
  async loadProposal(id) {
    const value = await request(`/api/v1/knowledge/proposals/${encodeURIComponent(id)}`)
    if (!isKnowledgeProposal(value)) throw new ClientError()
    return value
  },
  async decide(id, action, input) {
    const value = await request(`/api/v1/knowledge/proposals/${encodeURIComponent(id)}/${action}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(input) })
    if (!isKnowledgeDecision(value)) throw new ClientError()
    return value
  },
}

export function createIdempotencyKey(): string {
  return crypto.randomUUID()
}

export function KnowledgePage({ client = defaultClient, proposalID, createIdempotencyKey: newKey = createIdempotencyKey }: { client?: KnowledgeClient; proposalID?: string; createIdempotencyKey?: () => string }) {
  const { t } = useI18n()
  const [page, setPage] = useState<LoadState<KnowledgePageData>>({ kind: 'loading' })
  const [detail, setDetail] = useState<LoadState<KnowledgeDetail> | null>(null)
  const [loadingMore, setLoadingMore] = useState(false)
  const [proposal, setProposal] = useState<ProposalState>({ kind: 'idle' })
  const [reviewed, setReviewed] = useState(false)
  const [outcome, setOutcome] = useState('')
  const [decisionError, setDecisionError] = useState(false)
  const [decisionPending, setDecisionPending] = useState(false)
  const [conflict, setConflict] = useState(false)
  const detailRequest = useRef(0)
  const proposalRequest = useRef(0)
  const conflictAlert = useRef<HTMLParagraphElement>(null)

  useEffect(() => {
    let active = true
    void client.loadPage().then((value) => { if (active) setPage({ kind: 'ready', value: { items: value.items, next: value.next ?? '' } }) }, () => { if (active) setPage({ kind: 'error' }) })
    return () => { active = false }
  }, [client])

  useEffect(() => {
    const requestID = ++proposalRequest.current
    setReviewed(false)
    setOutcome('')
    setDecisionError(false)
    setConflict(false)
    setDecisionPending(false)
    if (!proposalID) { setProposal({ kind: 'idle' }); return }
    setProposal({ kind: 'loading' })
    void client.loadProposal(proposalID).then(
      (value) => { if (proposalRequest.current === requestID) setProposal({ kind: 'ready', value }) },
      () => { if (proposalRequest.current === requestID) setProposal({ kind: 'error' }) },
    )
  }, [client, proposalID])

  useEffect(() => { if (conflict) conflictAlert.current?.focus() }, [conflict])

  function selectDetail(id: string) {
    const requestID = ++detailRequest.current
    setDetail({ kind: 'loading' })
    void client.loadDetail(id).then(
      (value) => { if (detailRequest.current === requestID) setDetail({ kind: 'ready', value }) },
      () => { if (detailRequest.current === requestID) setDetail({ kind: 'error' }) },
    )
  }

  function loadMore() {
    if (page.kind !== 'ready' || page.value.next === '' || loadingMore) return
    const current = page.value
    setLoadingMore(true)
    void client.loadPage(current.next).then(
      (next) => setPage({ kind: 'ready', value: { items: [...current.items, ...next.items], next: next.next ?? '' } }),
      () => setPage({ kind: 'error' }),
    ).finally(() => setLoadingMore(false))
  }

  function decide(action: 'accept' | 'reject') {
    if (proposal.kind !== 'ready' || !reviewed || decisionPending) return
    const generation = proposalRequest.current
    setDecisionPending(true)
    setConflict(false)
    setDecisionError(false)
    setOutcome('')
    void client.decide(proposalID ?? proposal.value.proposal.id, action, { expected_version: proposal.value.version, idempotency_key: newKey(), confirm: true }).then(
      () => { if (proposalRequest.current === generation) { setDecisionPending(false); setOutcome(action === 'accept' ? t('knowledge.decisionAccepted') : t('knowledge.decisionRejected')) } },
      (error: unknown) => { if (proposalRequest.current === generation) { setDecisionPending(false); if (isKnowledgeConflict(errorCode(error))) setConflict(true); else setDecisionError(true) } },
    )
  }

  return <section aria-label={t('knowledge.aria')}>
    <header><h1>{t('knowledge.heading')}</h1><p>{t('knowledge.description')}</p></header>
    {page.kind === 'loading' ? <p role="status">{t('knowledge.loading')}</p> : null}
    {page.kind === 'error' ? <p role="alert">{t('knowledge.unavailable')}</p> : null}
    {page.kind === 'ready' && page.value.items.length === 0 ? <p>{t('knowledge.empty')}</p> : null}
    {page.kind === 'ready' && page.value.items.length > 0 ? <><ul aria-label={t('knowledge.documentsAria')}>{page.value.items.map((item) => <li key={item.id}><button type="button" onClick={() => selectDetail(item.id)}>{item.title}</button><p>{item.description}</p></li>)}</ul>{page.value.next !== '' ? <button type="button" disabled={loadingMore} onClick={loadMore}>{t('knowledge.loadMore')}</button> : null}</> : null}
    {detail?.kind === 'loading' ? <p role="status">{t('knowledge.detailLoading')}</p> : null}
    {detail?.kind === 'error' ? <p role="alert">{t('knowledge.detailUnavailable')}</p> : null}
    {detail?.kind === 'ready' ? <article aria-label={t('knowledge.detailAria')}><h2>{detail.value.title}</h2><pre>{detail.value.body}</pre>{detail.value.anchors.map((anchor) => { const link = sourceLink(anchor); return <a key={`${anchor.path}:${anchor.line_start}`} href={link.href}>{t('app.sourceLink', { path: link.target.path, start: link.target.line_start, end: link.target.line_end })}</a> })}</article> : null}
    {proposal.kind === 'loading' ? <p role="status">{t('knowledge.proposalLoading')}</p> : null}
    {proposal.kind === 'error' ? <p role="alert">{t('knowledge.proposalUnavailable')}</p> : null}
    {proposal.kind === 'ready' ? <section aria-label={t('knowledge.proposalReviewAria')}><h2>{t('knowledge.proposal')}</h2><pre>{proposal.value.diff}</pre><label><input type="checkbox" checked={reviewed} onInput={(event) => setReviewed(event.currentTarget.checked)} />{t('knowledge.reviewConfirmation')}</label><div><button type="button" disabled={!reviewed || decisionPending} onClick={() => decide('accept')}>{t('knowledge.accept')}</button><button type="button" disabled={!reviewed || decisionPending} onClick={() => decide('reject')}>{t('knowledge.reject')}</button></div></section> : null}
    {outcome ? <p role="status">{outcome}</p> : null}
    {decisionError ? <p role="alert">{t('knowledge.decisionUnavailable')}</p> : null}
    {conflict ? <p ref={conflictAlert} role="alert" tabIndex={-1}>{t('knowledge.conflict')}</p> : null}
  </section>
}

function isEnvelope(value: unknown): value is { data: unknown; meta: Record<string, unknown> } {
  return isRecord(value) && 'data' in value && isRecord(value.meta)
}

function isKnowledgePage(value: unknown): value is { items: KnowledgeSummary[]; next?: string } {
  return isRecord(value) && Array.isArray(value.items) && value.items.every(isKnowledgeSummary) && (value.next === undefined || typeof value.next === 'string')
}

function isKnowledgeDetail(value: unknown): value is KnowledgeDetail {
  if (!isRecord(value)) return false
  const record = value
  if (!isKnowledgeSummary(value)) return false
  const body = record.body
  const backlinks = record.backlinks
  return typeof body === 'string' && Array.isArray(backlinks) && backlinks.every((item) => isRecord(item) && isString(item.id) && isString(item.path) && isString(item.type) && isString(item.title))
}

function isKnowledgeSummary(value: unknown): value is KnowledgeSummary {
  return isRecord(value) && isString(value.id) && isString(value.path) && isString(value.type) && isString(value.title) && isOptionalString(value.description) && isOptionalString(value.resource) && isOptionalString(value.timestamp) && isOptionalString(value.status) && isOptionalString(value.confidence) && isNullableStringArray(value.tags) && isNullableStringArray(value.related) && Array.isArray(value.anchors) && value.anchors.every((anchor) => isRecord(anchor) && isString(anchor.path) && isFiniteNumber(anchor.line_start) && isFiniteNumber(anchor.line_end) && isOptionalString(anchor.content_hash) && isOptionalString(anchor.symbol))
}

function isKnowledgeProposal(value: unknown): value is KnowledgeProposal {
  return isRecord(value) && isRecord(value.proposal) && isString(value.proposal.id) && isString(value.proposal.operation) && isString(value.proposal.target_path) && isString(value.proposal.candidate_path) && isString(value.proposal.status) && isString(value.proposal.created_at) && typeof value.diff === 'string' && isString(value.version)
}

function isKnowledgeDecision(value: unknown): value is KnowledgeDecision {
  return isRecord(value) && isString(value.version) && typeof value.idempotent === 'boolean'
}

function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null }
function normalizeSummary(summary: KnowledgeSummary): KnowledgeSummary { return { ...summary, tags: summary.tags ?? [], related: summary.related ?? [] } }
function isNullableStringArray(value: unknown): boolean { return value === null || isStringArray(value) }
function isString(value: unknown): value is string { return typeof value === 'string' }
function isOptionalString(value: unknown): boolean { return value === undefined || typeof value === 'string' }
function isStringArray(value: unknown): value is string[] { return Array.isArray(value) && value.every(isString) }
function isFiniteNumber(value: unknown): value is number { return typeof value === 'number' && Number.isFinite(value) }
function errorCode(value: unknown): string | undefined {
  if (!isRecord(value)) return undefined
  if (isString(value.code)) return value.code
  if (isRecord(value.error) && isString(value.error.code)) return value.error.code
  return undefined
}
function isKnowledgeConflict(code: string | undefined): boolean { return code === 'proposal_version_conflict' || code === 'decision_in_progress' || code === 'conflict' }
