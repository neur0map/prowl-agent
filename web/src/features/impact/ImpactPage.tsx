import { useRef, useState } from 'preact/hooks'
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
  const requestID = useRef(0)

  function submit(event: JSX.TargetedEvent<HTMLFormElement, SubmitEvent>) {
    event.preventDefault()
    const trimmed = path.trim()
    if (trimmed === '') return

    const currentRequestID = ++requestID.current
    setState({ kind: 'loading' })
    void load(trimmed).then(
      (impact) => { if (requestID.current === currentRequestID) setState({ kind: 'ready', impact }) },
      () => { if (requestID.current === currentRequestID) setState({ kind: 'error' }) },
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
  if (!isRecord(value) || !isRecord(value.data) || !isRecord(value.meta)) return false
  const { data } = value
  return typeof data.path === 'string'
    && isBlastSummary(data.blast)
    && isRelations(data.relations)
    && isTestsResult(data.tests)
    && isEntrypointSet(data.entrypoints)
    && Array.isArray(data.knowledge)
    && data.knowledge.every(isKnowledgeEvidence)
}

function isBlastSummary(value: unknown): boolean {
  return isRecord(value)
    && typeof value.file === 'string'
    && isFiniteNumber(value.total)
    && isFiniteNumber(value.direct)
    && Array.isArray(value.by_subsystem)
    && value.by_subsystem.every((entry) => isRecord(entry) && typeof entry.subsystem === 'string' && isFiniteNumber(entry.count))
    && isStringArray(value.direct_files)
}

function isRelations(value: unknown): boolean {
  return isRecord(value)
    && typeof value.file === 'string'
    && typeof value.exists === 'boolean'
    && Array.isArray(value.symbols)
    && value.symbols.every(isSymbol)
    && Array.isArray(value.includes)
    && value.includes.every(isRelationEdge)
    && Array.isArray(value.included_by)
    && value.included_by.every(isRelationEdge)
}

function isSymbol(value: unknown): boolean {
  return isRecord(value)
    && isFiniteNumber(value.id)
    && typeof value.name === 'string'
    && typeof value.kind === 'string'
    && typeof value.signature === 'string'
    && typeof value.file === 'string'
    && isFiniteNumber(value.line)
    && isFiniteNumber(value.end_line)
}

function isRelationEdge(value: unknown): boolean {
  return isRecord(value)
    && typeof value.file === 'string'
    && typeof value.kind === 'string'
    && isFiniteNumber(value.line)
    && typeof value.raw === 'string'
    && typeof value.resolved === 'boolean'
}

function isTestsResult(value: unknown): boolean {
  return isRecord(value)
    && typeof value.file === 'string'
    && (value.tests === undefined || isStringArray(value.tests))
    && (value.runners === undefined || (Array.isArray(value.runners) && value.runners.every(isRunner)))
    && (value.limited === undefined || typeof value.limited === 'boolean')
    && typeof value.note === 'string'
}

function isRunner(value: unknown): boolean {
  return isRecord(value)
    && typeof value.src_type === 'string'
    && isFiniteNumber(value.src_id)
    && (value.dst_type === undefined || typeof value.dst_type === 'string')
    && (value.dst_id === undefined || isFiniteNumber(value.dst_id))
    && typeof value.kind === 'string'
    && typeof value.file === 'string'
    && isFiniteNumber(value.line)
    && typeof value.resolved === 'boolean'
    && (value.raw === undefined || typeof value.raw === 'string')
}

function isEntrypointSet(value: unknown): boolean {
  return isRecord(value)
    && typeof value.file === 'string'
    && isFiniteNumber(value.count)
    && isStringArray(value.entrypoints)
}

function isKnowledgeEvidence(value: unknown): boolean {
  return isRecord(value)
    && typeof value.id === 'string'
    && typeof value.title === 'string'
    && typeof value.type === 'string'
    && typeof value.status === 'string'
    && isSourceAnchor(value.anchor)
}

function isSourceAnchor(value: unknown): boolean {
  return isRecord(value)
    && typeof value.path === 'string'
    && typeof value.line_start === 'number'
    && Number.isInteger(value.line_start)
    && value.line_start > 0
    && typeof value.line_end === 'number'
    && Number.isInteger(value.line_end)
    && value.line_end >= value.line_start
    && (value.content_hash === undefined || typeof value.content_hash === 'string')
}

function isStringArray(value: unknown): boolean {
  return Array.isArray(value) && value.every((item) => typeof item === 'string')
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}
