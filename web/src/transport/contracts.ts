export type APIEnvelope<T> = { data: T; meta: Record<string, unknown> }

export type SourceTarget = {
  path: string
  line_start: number
  line_end: number
}

export type SourceLink = {
  href: string
  label: string
  target: SourceTarget
}

// Must match internal/workbench.MaxSourcePreviewLines so hash navigation always reaches /api/v1/source.
const maxSourcePreviewLines = 400

export function sourceLink(target: SourceTarget): SourceLink {
  const previewEnd = Math.min(target.line_end, target.line_start + maxSourcePreviewLines - 1)
  return {
    href: `#/source?path=${encodeURIComponent(target.path)}&line_start=${target.line_start}&line_end=${previewEnd}`,
    label: `${target.path} lines ${target.line_start}–${target.line_end}`,
    target,
  }
}

export type WorkspaceIdentity = { name: string }

export type ExploreFact = {
  id: string
  label: string
  detail: string
  anchor?: SourceTarget
}

export type ExploreSection = {
  id: string
  title: string
  description: string
  facts: ExploreFact[]
}

export type TourSummary = {
  id: string
  title: string
  steps: number
}

export type Explore = {
  workspace: WorkspaceIdentity
  sections: ExploreSection[]
  tours: TourSummary[]
}

export type GuidedTour = {
  id: string
  title: string
  description: string
  steps: Array<{
    number: number
    section_id: string
    title: string
    description: string
    facts: ExploreFact[]
  }>
}

export type ContextMode = 'compact' | 'standard' | 'full'

export type ContextSearchRequest = {
  question: string
  mode?: ContextMode
  budget_tokens?: number
  budget_bytes?: number
}

export type ContextGetRequest = {
  ids: string[]
  mode?: ContextMode
  budget_tokens?: number
  budget_bytes?: number
}

export type ContextCitation = {
  uri: string
  path?: string
  line_start?: number
  line_end?: number
  content_hash?: string
}

export type ContextItem = {
  id: string
  kind: string
  title: string
  summary?: string
  content?: string
  why_selected: string[]
  freshness: string
  confidence: number
  audience: string[]
  citations: ContextCitation[]
  detail_resource: string
  estimated_tokens: number
}

export type ContextBudget = {
  requested_tokens?: number
  requested_bytes?: number
  estimated_tokens: number
  estimated_bytes: number
  exact_bytes: number
}

export type ContextLens = {
  schema_version: string
  question?: string
  summary: string
  items: ContextItem[]
  budget: ContextBudget
  omitted: Record<string, number>
  next: string[]
  trace_id?: string
}

export type RelationEdge = {
  file: string
  kind: string
  line: number
  raw: string
  resolved: boolean
}

export type SymbolHit = {
  id: number
  name: string
  kind: string
  signature: string
  file: string
  line: number
  end_line: number
}

export type BlastSummary = {
  file: string
  total: number
  direct: number
  by_subsystem: Array<{ subsystem: string; count: number }>
  direct_files: string[]
}

export type Relations = {
  file: string
  exists: boolean
  symbols: SymbolHit[]
  includes: RelationEdge[]
  included_by: RelationEdge[]
}

export type TestsResult = {
  file: string
  tests?: string[]
  runners?: Array<{
    src_type: string
    src_id: number
    dst_type?: string
    dst_id?: number
    kind: string
    file: string
    line: number
    resolved: boolean
    raw?: string
  }>
  limited?: boolean
  note: string
}

export type EntrypointSet = {
  file: string
  count: number
  entrypoints: string[]
}

export type ImpactKnowledgeEvidence = {
  id: string
  title: string
  type: string
  status: string
  anchor: SourceTarget & { content_hash?: string }
}

export type Impact = {
  path: string
  blast: BlastSummary
  relations: Relations
  tests: TestsResult
  entrypoints: EntrypointSet
  knowledge: ImpactKnowledgeEvidence[]
}
