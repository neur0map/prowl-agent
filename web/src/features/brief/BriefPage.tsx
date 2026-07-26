import { useEffect, useState } from 'preact/hooks'

import { apiFetch } from '../../transport/api'
import { useI18n } from '../../i18n'


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
  const { t } = useI18n()
  const [state, setState] = useState<BriefState>({ kind: 'loading' })

  useEffect(() => {
    let current = true
    setState({ kind: 'loading' })
    void load().then(
      (brief) => {
        if (current) setState(isBrief(brief) ? { kind: 'ready', brief } : { kind: 'error' })
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
      <section class="brief-state" aria-label={t('brief.aria')} aria-busy="true">
        <span class="eyebrow">{t('brief.eyebrow')}</span>
        <h1>{t('brief.heading')}</h1>
        <p role="status">{t('brief.loading')}</p>
      </section>
    )
  }

  if (state.kind === 'error') {
    return (
      <section class="brief-state" aria-label={t('brief.aria')}>
        <span class="eyebrow">{t('brief.eyebrow')}</span>
        <h1>{t('brief.heading')}</h1>
        <p role="alert">{t('brief.unavailable')}</p>
      </section>
    )
  }

  return <BriefReady brief={state.brief} />
}

function BriefReady({ brief }: { brief: Brief }) {
  const { t, formatNumber } = useI18n()
  const { counts, docs, entrypoints, clusters, hotspots } = brief.overview
  const indexed = brief.freshness.last_indexed
  const sourceIsEmpty = counts.files === 0

  if (sourceIsEmpty) {
    return (
      <section class="brief-page" aria-label={t('brief.aria')}>
        <header class="page-header">
          <div>
            <span class="eyebrow">{t('brief.eyebrow')}</span>
            <h1>{brief.workspace.name}</h1>
          </div>
          <span class="status-pill">{t('brief.indexStatus', { status: brief.freshness.status })}</span>
        </header>
        <article class="empty-brief">
          <h2>{t('brief.noFacts')}</h2>
          <p>{t('brief.emptyDetail')}</p>
        </article>
      </section>
    )
  }

  return (
    <section class="brief-page" aria-label={t('brief.aria')}>
      <header class="page-header">
        <div>
          <span class="eyebrow">{t('brief.eyebrow')}</span>
          <h1>{brief.workspace.name}</h1>
          <p>{t('brief.summary', { files: formatNumber(counts.files), symbols: formatNumber(counts.symbols), edges: formatNumber(counts.edges) })}</p>
        </div>
        <div class="brief-statuses">
          <span class="status-pill">{t('brief.indexStatus', { status: brief.freshness.status })}</span>
          <span class="brief-indexed">{indexed ? t('brief.indexed', { date: indexed }) : t('brief.indexTimeUnavailable')}</span>
        </div>
      </header>

      <section class="brief-metrics" aria-label={t('brief.metricsAria')}>
        <article class="metric-card"><span class="eyebrow">{t('brief.sourceFiles')}</span><strong>{formatNumber(counts.files)}</strong></article>
        <article class="metric-card"><span class="eyebrow">{t('brief.symbols')}</span><strong>{formatNumber(counts.symbols)}</strong></article>
        <article class="metric-card"><span class="eyebrow">{t('brief.knowledgeAccepted')}</span><strong>{formatNumber(brief.knowledge.documents)}</strong></article>
        <article class="metric-card"><span class="eyebrow">{t('brief.capabilities')}</span><strong>{formatNumber(brief.capabilities.length)}</strong></article>
      </section>

      <section class="brief-columns" aria-label={t('brief.projectMapAria')}>
        <article class="brief-card evidence-card">
          <span class="eyebrow">{t('brief.onboarding')}</span>
          <h2>{t('brief.startEvidence')}</h2>
          <FactList title={t('brief.guides')} items={docs} empty={t('brief.noGuides')} />
          <FactList title={t('brief.entrypoints')} items={entrypoints} empty={t('brief.noEntrypoints')} />
        </article>

        <article class="brief-card">
          <span class="eyebrow">{t('brief.architecture')}</span>
          <h2>{t('brief.subsystemMap')}</h2>
          {clusters.length === 0 ? <p class="empty-list">{t('brief.noSubsystems')}</p> : (
            <ul class="cluster-list">
              {clusters.map((cluster) => (
                <li key={`${cluster.label}:${cluster.lang}`}>
                  <code>{cluster.label}</code>
                  <span>{t('brief.clusterMeta', { language: cluster.lang, files: formatNumber(cluster.files) })}</span>
                </li>
              ))}
            </ul>
          )}
        </article>
      </section>

      <section class="brief-columns brief-secondary" aria-label={t('brief.reviewContextAria')}>
        <article class="brief-card">
          <span class="eyebrow">{t('brief.reviewFocus')}</span>
          <h2>{t('brief.dependencyHotspots')}</h2>
          {hotspots.length === 0 ? <p class="empty-list">{t('brief.noHotspots')}</p> : (
            <ul class="fact-list">
              {hotspots.map((hotspot) => <li key={hotspot.file}><code>{hotspot.file}</code><span>{t('brief.incomingLinks', { count: formatNumber(hotspot.in) })}</span></li>)}
            </ul>
          )}
        </article>

        <article class="brief-card">
          <span class="eyebrow">{t('brief.availableWorkflows')}</span>
          <h2>{t('brief.capabilities')}</h2>
          {brief.capabilities.length === 0 ? <p class="empty-list">{t('brief.noCapabilitiesDiscovered')}</p> : (
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


function isBriefEnvelope(value: unknown): value is { data: Brief } {
  return isRecord(value) && isBrief(value.data)
}

function isBrief(value: unknown): value is Brief {
  if (!isRecord(value)) return false
  return (
    isRecord(value.workspace) &&
    typeof value.workspace.name === 'string' &&
    isOverview(value.overview) &&
    isRecord(value.knowledge) &&
    typeof value.knowledge.status === 'string' &&
    typeof value.knowledge.documents === 'number' &&
    isRecord(value.freshness) &&
    typeof value.freshness.status === 'string' &&
    (value.freshness.last_indexed === undefined || typeof value.freshness.last_indexed === 'string') &&
    isArrayOf(value.capabilities, isCapability)
  )
}

function isOverview(value: unknown): value is Brief['overview'] {
  if (!isRecord(value) || !isRecord(value.counts)) return false
  const { counts } = value
  return (
    typeof counts.files === 'number' &&
    typeof counts.symbols === 'number' &&
    typeof counts.edges === 'number' &&
    typeof counts.resources === 'number' &&
    isNumberRecord(counts.langs) &&
    isStringArray(value.docs) &&
    isStringArray(value.entrypoints) &&
    isArrayOf(value.clusters, isCluster) &&
    isArrayOf(value.hotspots, isHotspot)
  )
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isNumberRecord(value: unknown): value is Record<string, number> {
  return isRecord(value) && Object.values(value).every((item) => typeof item === 'number')
}

function isStringArray(value: unknown): value is string[] {
  return isArrayOf(value, (item): item is string => typeof item === 'string')
}

function isArrayOf<T>(value: unknown, isItem: (item: unknown) => item is T): value is T[] {
  return Array.isArray(value) && value.every(isItem)
}

function isCluster(value: unknown): value is Brief['overview']['clusters'][number] {
  return isRecord(value) && typeof value.label === 'string' && typeof value.lang === 'string' && typeof value.files === 'number'
}

function isHotspot(value: unknown): value is Brief['overview']['hotspots'][number] {
  return isRecord(value) && typeof value.file === 'string' && typeof value.in === 'number'
}

function isCapability(value: unknown): value is Brief['capabilities'][number] {
  return (
    isRecord(value) &&
    typeof value.name === 'string' &&
    typeof value.title === 'string' &&
    typeof value.description === 'string' &&
    typeof value.privacy === 'string' &&
    typeof value.version === 'string'
  )
}
