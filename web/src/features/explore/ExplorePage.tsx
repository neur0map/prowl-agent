import { useEffect, useState } from 'preact/hooks'

import { apiFetch } from '../../transport/api'
import { sourceLink, type APIEnvelope, type Explore } from '../../transport/contracts'
import { useI18n } from '../../i18n'

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
  const { t, formatNumber } = useI18n()
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
    return <section aria-label={t('explore.aria')} aria-busy="true"><span class="eyebrow">{t('explore.eyebrow')}</span><h1>{t('explore.heading')}</h1><p role="status">{t('explore.loading')}</p></section>
  }

  if (state.kind === 'error') {
    return <section aria-label={t('explore.aria')}><span class="eyebrow">{t('explore.eyebrow')}</span><h1>{t('explore.heading')}</h1><p role="alert">{t('explore.unavailable')}</p></section>
  }

  if (state.explore.sections.length === 0) {
    return <section aria-label={t('explore.aria')}><span class="eyebrow">{t('explore.eyebrow')}</span><h1>{state.explore.workspace.name}</h1><article><h2>{t('explore.noFacts')}</h2><p>{t('explore.noFactsDetail')}</p></article></section>
  }

  return (
    <section aria-label={t('explore.aria')}>
      <header class="page-header"><div><span class="eyebrow">{t('explore.eyebrow')}</span><h1>{state.explore.workspace.name}</h1><p>{t('explore.projectMap')}</p></div></header>
      <section aria-label={t('explore.projectMapAria')}>
        {state.explore.sections.map((section) => (
          <article key={section.id}>
            <h2>{section.title}</h2>
            <p>{section.description}</p>
            {section.facts.length === 0 ? <p>{t('explore.emptySection')}</p> : <ul>
              {section.facts.map((fact) => {
                const link = fact.anchor ? sourceLink(fact.anchor) : null
                return (
                  <li key={fact.id}>
                    <strong>{fact.label}</strong><span>{fact.detail}</span>
                    {link ? <a href={link.href}>{t('app.sourceLink', { path: link.target.path, start: link.target.line_start, end: link.target.line_end })}</a> : null}
                  </li>
                )
              })}
            </ul>}
          </article>
        ))}
      </section>
      {state.explore.tours.length > 0 ? <section aria-label={t('explore.guidedToursAria')}><h2>{t('explore.guidedTours')}</h2><ul>{state.explore.tours.map((tour) => <li key={tour.id}><a href={`#/explore?tour=${encodeURIComponent(tour.id)}`}>{tour.title}</a><span>{t('explore.steps', { count: formatNumber(tour.steps) })}</span></li>)}</ul></section> : null}
    </section>
  )
}

function isExploreEnvelope(value: unknown): value is APIEnvelope<Explore> {
  if (!isRecord(value) || !isRecord(value.data) || !isRecord(value.meta)) return false
  const { data } = value
  return isRecord(data.workspace)
    && typeof data.workspace.name === 'string'
    && Array.isArray(data.sections)
    && data.sections.every(isExploreSection)
    && Array.isArray(data.tours)
    && data.tours.every((tour) => isRecord(tour)
      && typeof tour.id === 'string'
      && typeof tour.title === 'string'
      && Number.isInteger(tour.steps))
}

function isExploreSection(value: unknown): boolean {
  return isRecord(value)
    && typeof value.id === 'string'
    && typeof value.title === 'string'
    && typeof value.description === 'string'
    && Array.isArray(value.facts)
    && value.facts.every((fact) => isRecord(fact)
      && typeof fact.id === 'string'
      && typeof fact.label === 'string'
      && typeof fact.detail === 'string'
      && (fact.anchor === undefined || isSourceTarget(fact.anchor)))
}

function isSourceTarget(value: unknown): boolean {
  return isRecord(value)
    && typeof value.path === 'string'
    && typeof value.line_start === 'number'
    && Number.isInteger(value.line_start)
    && value.line_start > 0
    && typeof value.line_end === 'number'
    && Number.isInteger(value.line_end)
    && value.line_end >= value.line_start
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}
