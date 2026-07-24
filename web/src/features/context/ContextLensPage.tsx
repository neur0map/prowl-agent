import { useRef, useState } from 'preact/hooks'
import type { JSX } from 'preact'

import { apiFetch } from '../../transport/api'
import { sourceLink, type APIEnvelope, type ContextLens, type ContextSearchRequest } from '../../transport/contracts'

export type ContextSearchClient = (request: ContextSearchRequest) => Promise<ContextLens>

type ContextState =
  | { kind: 'empty' }
  | { kind: 'loading' }
  | { kind: 'ready'; context: ContextLens }
  | { kind: 'error' }

export async function searchContext(request: ContextSearchRequest): Promise<ContextLens> {
  const response = await apiFetch('/api/v1/context/search', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  })
  if (!response.ok) throw new Error(`context search failed with ${response.status}`)

  const payload: unknown = await response.json()
  if (!isContextEnvelope(payload)) throw new Error('context search response was invalid')
  return payload.data
}

export function ContextLensPage({ search = searchContext }: { search?: ContextSearchClient }) {
  const [question, setQuestion] = useState('')
  const [state, setState] = useState<ContextState>({ kind: 'empty' })
  const requestID = useRef(0)

  function submit(event: JSX.TargetedEvent<HTMLFormElement, SubmitEvent>) {
    event.preventDefault()
    const trimmed = question.trim()
    if (trimmed === '') return

    const currentRequestID = ++requestID.current
    setState({ kind: 'loading' })
    void search({ question: trimmed }).then(
      (context) => { if (requestID.current === currentRequestID) setState({ kind: 'ready', context }) },
      () => { if (requestID.current === currentRequestID) setState({ kind: 'error' }) },
    )
  }

  return (
    <section aria-label="Context lens">
      <header class="page-header"><div><span class="eyebrow">Context</span><h1>Context lens</h1><p>Request bounded, source-backed project context.</p></div></header>
      <form aria-label="Search project context" onSubmit={submit}>
        <label>
          Question
          <input value={question} onInput={(event) => setQuestion(event.currentTarget.value)} required />
        </label>
        <button type="submit">Search context</button>
      </form>
      {state.kind === 'empty' ? <article><h2>Ask a project question</h2><p>Search for a source-backed answer without reconstructing project context in the browser.</p></article> : null}
      {state.kind === 'loading' ? <p role="status">Searching source-backed context…</p> : null}
      {state.kind === 'error' ? <p role="alert">Project context unavailable. Try another question.</p> : null}
      {state.kind === 'ready' ? <ContextResult context={state.context} /> : null}
    </section>
  )
}

function ContextResult({ context }: { context: ContextLens }) {
  return (
    <section aria-label="Context results">
      <h2>Source-backed context</h2>
      <p>{context.summary}</p>
      {context.items.length === 0 ? <p>No source-backed context matched this question.</p> : <ul>
        {context.items.map((item) => (
          <li key={item.id}>
            <article>
              <h3>{item.title}</h3>
              {item.summary ? <p>{item.summary}</p> : null}
              {item.citations.length > 0 ? <ul aria-label={`${item.title} citations`}>
                {item.citations.map((citation) => {
                  if (!citation.path || citation.line_start === undefined || citation.line_end === undefined || citation.line_start < 1 || citation.line_end < citation.line_start) return <li key={citation.uri}><code>{citation.uri}</code></li>
                  const link = sourceLink({ path: citation.path, line_start: citation.line_start, line_end: citation.line_end })
                  return <li key={citation.uri}><a href={link.href}>{link.label}</a></li>
                })}
              </ul> : null}
            </article>
          </li>
        ))}
      </ul>}
    </section>
  )
}

function isContextEnvelope(value: unknown): value is APIEnvelope<ContextLens> {
  if (!isRecord(value) || !isRecord(value.data) || !isRecord(value.meta)) return false
  const { data } = value
  return typeof data.schema_version === 'string'
    && (data.question === undefined || typeof data.question === 'string')
    && typeof data.summary === 'string'
    && Array.isArray(data.items)
    && data.items.every(isContextItem)
    && isContextBudget(data.budget)
    && isNumberRecord(data.omitted)
    && Array.isArray(data.next)
    && data.next.every((item) => typeof item === 'string')
    && (data.trace_id === undefined || typeof data.trace_id === 'string')
}

function isContextItem(value: unknown): boolean {
  return isRecord(value)
    && typeof value.id === 'string'
    && typeof value.kind === 'string'
    && typeof value.title === 'string'
    && (value.summary === undefined || typeof value.summary === 'string')
    && (value.content === undefined || typeof value.content === 'string')
    && isStringArray(value.why_selected)
    && typeof value.freshness === 'string'
    && isFiniteNumber(value.confidence)
    && isStringArray(value.audience)
    && Array.isArray(value.citations)
    && value.citations.every(isCitation)
    && typeof value.detail_resource === 'string'
    && isFiniteNumber(value.estimated_tokens)
}

function isCitation(value: unknown): boolean {
  return isRecord(value)
    && typeof value.uri === 'string'
    && (value.path === undefined || typeof value.path === 'string')
    && (value.line_start === undefined || isFiniteNumber(value.line_start))
    && (value.line_end === undefined || isFiniteNumber(value.line_end))
    && (value.content_hash === undefined || typeof value.content_hash === 'string')
}

function isContextBudget(value: unknown): boolean {
  return isRecord(value)
    && (value.requested_tokens === undefined || isFiniteNumber(value.requested_tokens))
    && (value.requested_bytes === undefined || isFiniteNumber(value.requested_bytes))
    && isFiniteNumber(value.estimated_tokens)
    && isFiniteNumber(value.estimated_bytes)
    && isFiniteNumber(value.exact_bytes)
}

function isStringArray(value: unknown): boolean {
  return Array.isArray(value) && value.every((item) => typeof item === 'string')
}

function isNumberRecord(value: unknown): boolean {
  return isRecord(value) && Object.values(value).every(isFiniteNumber)
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}
