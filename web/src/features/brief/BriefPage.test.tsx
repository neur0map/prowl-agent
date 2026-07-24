import { cleanup, render, screen } from '@testing-library/preact'
import { afterEach, describe, expect, it } from 'vitest'

import { BriefPage, type Brief } from './BriefPage'

afterEach(cleanup)

const populatedBrief: Brief = {
  workspace: { name: 'go-auth-service' },
  overview: {
    counts: { files: 4, symbols: 12, edges: 6, resources: 2, langs: { go: 4 } },
    docs: ['README.md'],
    entrypoints: ['cmd/server/main.go'],
    clusters: [{ label: 'internal/auth', lang: 'go', files: 2 }],
    hotspots: [{ file: 'internal/auth/service.go', in: 3 }],
  },
  knowledge: { status: 'healthy', documents: 1 },
  freshness: { status: 'current', last_indexed: '2026-07-24T18:00:00Z' },
  capabilities: [{ name: 'go-service', title: 'Go service', description: 'Investigate a Go service.', privacy: 'local', version: '1.0.0' }],
}

describe('BriefPage', () => {
  it('renders source-backed project facts after loading', async () => {
    render(<BriefPage load={() => Promise.resolve(populatedBrief)} />)

    expect(await screen.findByRole('heading', { name: 'go-auth-service' })).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Start with evidence' })).toBeTruthy()
    expect(screen.getByText('README.md')).toBeTruthy()
    expect(screen.getByText('cmd/server/main.go')).toBeTruthy()
    expect(screen.getByText('internal/auth')).toBeTruthy()
    expect(screen.getByText('Go service')).toBeTruthy()
    expect(screen.getByText('Accepted knowledge')).toBeTruthy()
  })

  it('explains when no source facts are indexed', async () => {
    const empty = { ...populatedBrief, overview: { ...populatedBrief.overview, counts: { ...populatedBrief.overview.counts, files: 0 } } }
    render(<BriefPage load={() => Promise.resolve(empty)} />)

    expect(await screen.findByText('No indexed source facts yet.')).toBeTruthy()
  })

  it('reports an unavailable Brief without exposing transport details', async () => {
    render(<BriefPage load={() => Promise.reject(new Error('sensitive implementation detail'))} />)

    expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'Project brief unavailable. Refresh to retry.')
  })
})
