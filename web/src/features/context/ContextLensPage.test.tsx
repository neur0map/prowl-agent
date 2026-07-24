import { cleanup, fireEvent, render, screen } from '@testing-library/preact'
import { afterEach, describe, expect, it } from 'vitest'

import { ContextLensPage } from './ContextLensPage'
import type { ContextLens, ContextSearchRequest } from '../../transport/contracts'

afterEach(cleanup)

const populatedContext: ContextLens = {
  schema_version: 'prowl.context/v1',
  question: 'Where does authentication start?',
  summary: 'Authentication begins in the HTTP entrypoint.',
  items: [{
    id: 'entrypoint',
    kind: 'source',
    title: 'HTTP server',
    summary: 'Registers authentication middleware.',
    why_selected: ['Matches the requested entrypoint.'],
    freshness: 'current',
    confidence: 0.98,
    audience: ['developer'],
    citations: [{ uri: 'source://cmd/server/main.go', path: 'cmd/server/main.go', line_start: 8, line_end: 20 }],
    detail_resource: 'context://entrypoint',
    estimated_tokens: 120,
  }],
  budget: { estimated_tokens: 120, estimated_bytes: 600, exact_bytes: 560 },
  omitted: {},
  next: [],
}

describe('ContextLensPage', () => {
  it('explains how to request bounded project context before a search', () => {
    render(<ContextLensPage search={() => Promise.resolve(populatedContext)} />)

    expect(screen.getByRole('heading', { name: 'Ask a project question' })).toBeTruthy()
  })

  it('announces loading while the injected context client searches', () => {
    const pending = Promise.race<ContextLens>([])
    render(<ContextLensPage search={() => pending} />)

    submitQuestion('Where does authentication start?')

    expect(screen.getByRole('status').textContent).toBe('Searching source-backed context…')
  })

  it('reports a privacy-safe error when context is unavailable', async () => {
    render(<ContextLensPage search={() => Promise.reject(new Error('sensitive transport detail'))} />)

    submitQuestion('Where does authentication start?')

    expect((await screen.findByRole('alert')).textContent).toBe('Project context unavailable. Try another question.')
  })

  it('constructs a typed search request and renders returned citations', async () => {
    const requests: ContextSearchRequest[] = []
    render(<ContextLensPage search={(request) => {
      requests.push(request)
      return Promise.resolve(populatedContext)
    }} />)

    submitQuestion('  Where does authentication start?  ')

    expect(await screen.findByText('Authentication begins in the HTTP entrypoint.')).toBeTruthy()
    expect(requests).toEqual([{ question: 'Where does authentication start?' }])
    expect(screen.getByRole('heading', { name: 'HTTP server' })).toBeTruthy()
    const citation = screen.getByRole('link', { name: 'cmd/server/main.go lines 8–20' })
    expect(citation.getAttribute('href')).toBe('#source?path=cmd%2Fserver%2Fmain.go&line_start=8&line_end=20')
  })
})

function submitQuestion(question: string) {
  fireEvent.input(screen.getByLabelText('Question'), { target: { value: question } })
  fireEvent.submit(screen.getByRole('form', { name: 'Search project context' }))
}
