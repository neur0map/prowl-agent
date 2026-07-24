import { useEffect, useState } from 'preact/hooks'

import { apiFetch } from '../../transport/api'

const countFormatter = new Intl.NumberFormat('en-US')

export type Brief = {
  workspace: { name: string }
  overview: {
    counts: { files: number; symbols: number; edges: number; resources: number; langs: Record<string, number> }
    docs: string[]
    entrypoints: string[]
    clusters: Array<{ label: string; lang: string; files: number }>
    hotspots: Array<{ file: string; in: number }>
  }
  knowledge: { status: string; documents: number }
  freshness: { status: string; last_indexed?: string }
  capabilities: Array<{ name: string; title: string; description: string; privacy: string; version: string }>
}

export type BriefLoader = () => Promise<Brief>

type BriefState =
  | { kind: 'loading' }
  | { kind: 'ready'; brief: Brief }
  | { kind: 'error' }

export async function loadBrief(): Promise<Brief> {
  const response = await apiFetch('/api/v1/brief')
  if (!response.ok) {
    throw new Error(`brief request failed with ${response.status}`)
  }
  const payload: unknown = await response.json()
  if (!isBriefEnvelope(payload)) {
    throw new Error('brief response was invalid')
  }
  return payload.data
}

export function BriefPage({ load = loadBrief }: { load?: BriefLoader }) {
  const [state, setState] = useState<BriefState>({ kind: 'loading' })

  useEffect(() => {
    let current = true
    setState({ kind: 'loading' })
    void load().then(
      (brief) => {
        if (current) setState({ kind: 'ready', brief })
      },
      () => {
        if (current) setState({ kind: 'error' })
      },
    )
    return () => {
      current = false
    }
  }, [load])

  if (state.kind === 'loading') {
    return (
      <section class="brief-state" aria-label="Project brief" aria-busy="true">
        <span class="eyebrow">Home / Brief</span>
        <h1>Project brief</h1>
        <p role="status">Loading project brief…</p>
      </section>
    )
  }

  if (state.kind === 'error') {
    return (
      <section class="brief-state" aria-label="Project brief">
        <span class="eyebrow">Home / Brief</span>
        <h1>Project brief</h1>
        <p role="alert">Project brief unavailable. Refresh to retry.</p>
      </section>
    )
  }

  return <BriefReady brief={state.brief} />
}

function BriefReady({ brief }: { brief: Brief }) {
  const { counts, docs, entrypoints, clusters, hotspots } = brief.overview
  const indexed = brief.freshness.last_indexed
  const sourceIsEmpty = counts.files === 0

  if (sourceIsEmpty) {
    return (
      <section class="brief-page" aria-label="Project brief">
        <header class="page-header">
          <div>
            <span class="eyebrow">Home / Brief</span>
            <h1>{brief.workspace.name}</h1>
          </div>
          <span class="status-pill">Index {brief.freshness.status}</span>
        </header>
        <article class="empty-brief">
          <h2>No indexed source facts yet.</h2>
          <p>Index this workspace, then refresh this page to build its source-backed brief.</p>
        </article>
      </section>
    )
  }

  return (
    <section class="brief-page" aria-label="Project brief">
      <header class="page-header">
        <div>
          <span class="eyebrow">Home / Brief</span>
          <h1>{brief.workspace.name}</h1>
          <p>{formatCount(counts.files)} indexed source files · {formatCount(counts.symbols)} symbols · {formatCount(counts.edges)} relationships</p>
        </div>
        <div class="brief-statuses">
          <span class="status-pill">Index {brief.freshness.status}</span>
          <span class="brief-indexed">{indexed ? `Indexed ${indexed}` : 'Index time unavailable'}</span>
        </div>
      </header>

      <section class="brief-metrics" aria-label="Project metrics">
        <article class="metric-card"><span class="eyebrow">Source files</span><strong>{formatCount(counts.files)}</strong></article>
        <article class="metric-card"><span class="eyebrow">Symbols</span><strong>{formatCount(counts.symbols)}</strong></article>
        <article class="metric-card"><span class="eyebrow">Accepted knowledge</span><strong>{formatCount(brief.knowledge.documents)}</strong></article>
        <article class="metric-card"><span class="eyebrow">Capabilities</span><strong>{formatCount(brief.capabilities.length)}</strong></article>
      </section>

      <section class="brief-columns" aria-label="Project map">
        <article class="brief-card evidence-card">
          <span class="eyebrow">Onboarding</span>
          <h2>Start with evidence</h2>
          <FactList title="Guides" items={docs} empty="No guide documents were identified." />
          <FactList title="Entrypoints" items={entrypoints} empty="No entrypoints were identified." />
        </article>

        <article class="brief-card">
          <span class="eyebrow">Architecture</span>
          <h2>Subsystem map</h2>
          {clusters.length === 0 ? <p class="empty-list">No connected subsystems were identified.</p> : (
            <ul class="cluster-list">
              {clusters.map((cluster) => (
                <li key={`${cluster.label}:${cluster.lang}`}>
                  <code>{cluster.label}</code>
                  <span>{cluster.lang} · {formatCount(cluster.files)} files</span>
                </li>
              ))}
            </ul>
          )}
        </article>
      </section>

      <section class="brief-columns brief-secondary" aria-label="Review context">
        <article class="brief-card">
          <span class="eyebrow">Review focus</span>
          <h2>Dependency hotspots</h2>
          {hotspots.length === 0 ? <p class="empty-list">No dependency hotspots were identified.</p> : (
            <ul class="fact-list">
              {hotspots.map((hotspot) => <li key={hotspot.file}><code>{hotspot.file}</code><span>{formatCount(hotspot.in)} incoming links</span></li>)}
            </ul>
          )}
        </article>

        <article class="brief-card">
          <span class="eyebrow">Available workflows</span>
          <h2>Capabilities</h2>
          {brief.capabilities.length === 0 ? <p class="empty-list">No local capabilities were discovered.</p> : (
            <ul class="capability-list">
              {brief.capabilities.map((capability) => (
                <li key={capability.name}>
                  <strong>{capability.title}</strong>
                  <span>{capability.description}</span>
                </li>
              ))}
            </ul>
          )}
        </article>
      </section>
    </section>
  )
}


function FactList({ title, items, empty }: { title: string; items: string[]; empty: string }) {
  return (
    <section class="fact-group">
      <h3>{title}</h3>
      {items.length === 0 ? <p class="empty-list">{empty}</p> : <ul class="fact-list">{items.map((item) => <li key={item}><code>{item}</code></li>)}</ul>}
    </section>
  )
}

function formatCount(value: number) {
  return countFormatter.format(value)
}

function isBriefEnvelope(value: unknown): value is { data: Brief } {
  if (typeof value !== 'object' || value === null || !('data' in value)) return false
  const data = value.data
  return typeof data === 'object' && data !== null && 'workspace' in data && 'overview' in data
}
