import { useState } from 'preact/hooks'
import type { JSX } from 'preact'

import { apiFetch } from '../../transport/api'
import { sourceLink, type APIEnvelope, type Impact } from '../../transport/contracts'

export type ImpactLoader = (path: string) => Promise<Impact>

type ImpactState =
  | { kind: 'empty' }
  | { kind: 'loading' }
  | { kind: 'ready'; impact: Impact }
  | { kind: 'error' }

export async function loadImpact(path: string): Promise<Impact> {
  const response = await apiFetch('/api/v1/impact', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path }),
  })
  if (!response.ok) throw new Error(`impact request failed with ${response.status}`)

  const payload: unknown = await response.json()
  if (!isImpactEnvelope(payload)) throw new Error('impact response was invalid')
  return payload.data
}

export function ImpactPage({ load = loadImpact }: { load?: ImpactLoader }) {
  const [path, setPath] = useState('')
  const [state, setState] = useState<ImpactState>({ kind: 'empty' })

  function submit(event: JSX.TargetedEvent<HTMLFormElement, SubmitEvent>) {
    event.preventDefault()
    const trimmed = path.trim()
    if (trimmed === '') return

    setState({ kind: 'loading' })
    void load(trimmed).then(
      (impact) => setState({ kind: 'ready', impact }),
      () => setState({ kind: 'error' }),
    )
  }

  return (
    <section aria-label="Impact analysis">
      <header class="page-header"><div><span class="eyebrow">Impact</span><h1>Source impact</h1><p>Inspect server-computed evidence for one project source file.</p></div></header>
      <form aria-label="Inspect source impact" onSubmit={submit}>
        <label>
          Source path
          <input value={path} onInput={(event) => setPath(event.currentTarget.value)} required />
        </label>
        <button type="submit">Inspect impact</button>
      </form>
      {state.kind === 'empty' ? <article><h2>Enter a project-relative source path</h2><p>Inspect graph, test, entrypoint, and knowledge evidence without recomputing it in the browser.</p></article> : null}
      {state.kind === 'loading' ? <p role="status">Loading source-backed impact…</p> : null}
      {state.kind === 'error' ? <p role="alert">Impact evidence unavailable. Try another source path.</p> : null}
      {state.kind === 'ready' ? <ImpactEvidence impact={state.impact} /> : null}
    </section>
  )
}

function ImpactEvidence({ impact }: { impact: Impact }) {
  return (
    <section aria-label="Impact evidence">
      <h2>Impact: {impact.path}</h2>
      <section><h3>Dependency radius</h3><p>{impact.blast.total} transitive dependents</p><p>{impact.blast.direct} direct dependents</p></section>
      <section><h3>Tests</h3>{impact.tests.tests && impact.tests.tests.length > 0 ? <ul>{impact.tests.tests.map((test) => <li key={test}><code>{test}</code></li>)}</ul> : <p>No test evidence was identified.</p>}</section>
      <section><h3>Entrypoints</h3>{impact.entrypoints.entrypoints.length > 0 ? <ul>{impact.entrypoints.entrypoints.map((entrypoint) => <li key={entrypoint}><code>{entrypoint}</code></li>)}</ul> : <p>No entrypoint evidence was identified.</p>}</section>
      <section><h3>Knowledge evidence</h3>{impact.knowledge.length > 0 ? <ul>{impact.knowledge.map((evidence) => {
        const link = sourceLink(evidence.anchor)
        return <li key={evidence.id}><strong>{evidence.title}</strong><a href={link.href}>{link.label}</a></li>
      })}</ul> : <p>No knowledge evidence was identified.</p>}</section>
    </section>
  )
}

function isImpactEnvelope(value: unknown): value is APIEnvelope<Impact> {
  if (!isRecord(value) || !isRecord(value.data)) return false
  return typeof value.data.path === 'string'
    && isRecord(value.data.blast)
    && isRecord(value.data.relations)
    && isRecord(value.data.tests)
    && isRecord(value.data.entrypoints)
    && Array.isArray(value.data.knowledge)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}
