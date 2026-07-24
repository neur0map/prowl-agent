import { cleanup, fireEvent, render, screen } from '@testing-library/preact'
import { afterEach, describe, expect, it } from 'vitest'

import { ImpactPage } from './ImpactPage'
import type { Impact } from '../../transport/contracts'

afterEach(cleanup)

const populatedImpact: Impact = {
  path: 'internal/auth/service.go',
  blast: {
    file: 'internal/auth/service.go',
    total: 3,
    direct: 1,
    by_subsystem: [{ subsystem: 'cmd/server', count: 1 }],
    direct_files: ['cmd/server/main.go'],
  },
  relations: {
    file: 'internal/auth/service.go',
    exists: true,
    symbols: [],
    includes: [],
    included_by: [],
  },
  tests: { file: 'internal/auth/service.go', tests: ['internal/auth/service_test.go'], note: '' },
  entrypoints: { file: 'internal/auth/service.go', count: 1, entrypoints: ['cmd/server/main.go'] },
  knowledge: [{
    id: 'auth-design',
    title: 'Authentication design',
    type: 'decision',
    status: 'accepted',
    anchor: { path: 'docs/auth.md', line_start: 4, line_end: 9, content_hash: 'abc123' },
  }],
}

describe('ImpactPage', () => {
  it('explains how to request impact evidence before a path is submitted', () => {
    render(<ImpactPage load={() => Promise.resolve(populatedImpact)} />)

    expect(screen.getByRole('heading', { name: 'Enter a project-relative source path' })).toBeTruthy()
  })

  it('announces loading while the injected impact client runs', () => {
    const pending = Promise.race<Impact>([])
    render(<ImpactPage load={() => pending} />)

    submitPath('internal/auth/service.go')

    expect(screen.getByRole('status').textContent).toBe('Loading source-backed impact…')
  })

  it('reports a privacy-safe error when impact evidence is unavailable', async () => {
    render(<ImpactPage load={() => Promise.reject(new Error('sensitive graph detail'))} />)

    submitPath('internal/auth/service.go')

    expect((await screen.findByRole('alert')).textContent).toBe('Impact evidence unavailable. Try another source path.')
  })

  it('submits a project-relative path to the injected client and renders server-owned evidence', async () => {
    const paths: string[] = []
    render(<ImpactPage load={(path) => {
      paths.push(path)
      return Promise.resolve(populatedImpact)
    }} />)

    submitPath('  internal/auth/service.go  ')

    expect(await screen.findByRole('heading', { name: 'Impact: internal/auth/service.go' })).toBeTruthy()
    expect(paths).toEqual(['internal/auth/service.go'])
    expect(screen.getByText('3 transitive dependents')).toBeTruthy()
    expect(screen.getByText('internal/auth/service_test.go')).toBeTruthy()
    const link = screen.getByRole('link', { name: 'docs/auth.md lines 4–9' })
    expect(link.getAttribute('href')).toBe('#source?path=docs%2Fauth.md&line_start=4&line_end=9')
  })
})

function submitPath(path: string) {
  fireEvent.input(screen.getByLabelText('Source path'), { target: { value: path } })
  fireEvent.submit(screen.getByRole('form', { name: 'Inspect source impact' }))
}
