import { useState } from 'preact/hooks'
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

  function submit(event: JSX.TargetedEvent<HTMLFormElement, SubmitEvent>) {
    event.preventDefault()
    const trimmed = question.trim()
    if (trimmed === '') return

    setState({ kind: 'loading' })
    void search({ question: trimmed }).then(
      (context) => setState({ kind: 'ready', context }),
      () => setState({ kind: 'error' }),
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
  if (!isRecord(value) || !isRecord(value.data)) return false
  return typeof value.data.summary === 'string'
    && Array.isArray(value.data.items)
    && isRecord(value.data.budget)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}
