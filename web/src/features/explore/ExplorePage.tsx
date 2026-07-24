import { useEffect, useState } from 'preact/hooks'

import { apiFetch } from '../../transport/api'
import { sourceLink, type APIEnvelope, type Explore } from '../../transport/contracts'

export type ExploreLoader = () => Promise<Explore>

type ExploreState =
  | { kind: 'loading' }
  | { kind: 'ready'; explore: Explore }
  | { kind: 'error' }

export async function loadExplore(): Promise<Explore> {
  const response = await apiFetch('/api/v1/explore')
  if (!response.ok) throw new Error(`explore request failed with ${response.status}`)

  const payload: unknown = await response.json()
  if (!isExploreEnvelope(payload)) throw new Error('explore response was invalid')
  return payload.data
}

export function ExplorePage({ load = loadExplore }: { load?: ExploreLoader }) {
  const [state, setState] = useState<ExploreState>({ kind: 'loading' })

  useEffect(() => {
    let current = true
    setState({ kind: 'loading' })
    void load().then(
      (explore) => { if (current) setState({ kind: 'ready', explore }) },
      () => { if (current) setState({ kind: 'error' }) },
    )
    return () => { current = false }
  }, [load])

  if (state.kind === 'loading') {
    return <section aria-label="Project exploration" aria-busy="true"><span class="eyebrow">Explore</span><h1>Explore project</h1><p role="status">Loading project map…</p></section>
  }

  if (state.kind === 'error') {
    return <section aria-label="Project exploration"><span class="eyebrow">Explore</span><h1>Explore project</h1><p role="alert">Project exploration unavailable. Refresh to retry.</p></section>
  }

  if (state.explore.sections.length === 0) {
    return <section aria-label="Project exploration"><span class="eyebrow">Explore</span><h1>{state.explore.workspace.name}</h1><article><h2>No source-backed project facts yet.</h2><p>Index this workspace, then refresh to explore its project map.</p></article></section>
  }

  return (
    <section aria-label="Project exploration">
      <header class="page-header"><div><span class="eyebrow">Explore</span><h1>{state.explore.workspace.name}</h1><p>Source-backed project map</p></div></header>
      <section aria-label="Project map">
        {state.explore.sections.map((section) => (
          <article key={section.id}>
            <h2>{section.title}</h2>
            <p>{section.description}</p>
            {section.facts.length === 0 ? <p>No source-backed facts in this section.</p> : <ul>
              {section.facts.map((fact) => (
                <li key={fact.id}>
                  <strong>{fact.label}</strong><span>{fact.detail}</span>
                  {fact.anchor ? (() => {
                    const link = sourceLink(fact.anchor)
                    return <a href={link.href}>{link.label}</a>
                  })() : null}
                </li>
              ))}
            </ul>}
          </article>
        ))}
      </section>
      {state.explore.tours.length > 0 ? <section aria-label="Guided tours"><h2>Guided tours</h2><ul>{state.explore.tours.map((tour) => <li key={tour.id}><strong>{tour.title}</strong><span>{tour.steps} steps</span></li>)}</ul></section> : null}
    </section>
  )
}

function isExploreEnvelope(value: unknown): value is APIEnvelope<Explore> {
  if (!isRecord(value) || !isRecord(value.data)) return false
  const { data } = value
  return isRecord(data.workspace)
    && typeof data.workspace.name === 'string'
    && Array.isArray(data.sections)
    && Array.isArray(data.tours)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}
