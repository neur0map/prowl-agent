import { cleanup, fireEvent, render, screen } from '@testing-library/preact'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ContextLensPage } from './ContextLensPage'
import type { ContextLens, ContextSearchRequest } from '../../transport/contracts'

const { apiFetch } = vi.hoisted(() => ({ apiFetch: vi.fn() }))
vi.mock('../../transport/api', () => ({ apiFetch }))

afterEach(() => {
  cleanup()
  apiFetch.mockReset()
})

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
    expect(citation.getAttribute('href')).toBe('#/source?path=cmd%2Fserver%2Fmain.go&line_start=8&line_end=20&preview_end=20')
  })

  it('ignores a context result superseded by a later search', async () => {
    const first = deferredPromise<ContextLens>()
    const newerContext = { ...populatedContext, summary: 'The newer result is authoritative.' }
    let calls = 0
    render(<ContextLensPage search={() => ++calls === 1 ? first.promise : Promise.resolve(newerContext)} />)

    submitQuestion('First question')
    await Promise.resolve()
    submitQuestion('Second question')
    expect(await screen.findByText('The newer result is authoritative.')).toBeTruthy()

    first.resolve(populatedContext)
    await screen.findByText('The newer result is authoritative.')
    expect(screen.queryByText('Authentication begins in the HTTP entrypoint.')).toBeNull()
  })

  it('treats malformed rendered context data as unavailable', async () => {
    apiFetch.mockResolvedValue(new Response(JSON.stringify({
      data: { ...populatedContext, items: [{ ...populatedContext.items[0], citations: 'not-an-array' }] },
      meta: {},
    })))

    render(<ContextLensPage />)
    submitQuestion('Where does authentication start?')

    expect((await screen.findByRole('alert')).textContent).toBe('Project context unavailable. Try another question.')
  })

  it('requires context envelope metadata', async () => {
    apiFetch.mockResolvedValue(new Response(JSON.stringify({ data: populatedContext, meta: null })))

    render(<ContextLensPage />)
    submitQuestion('Where does authentication start?')

    expect((await screen.findByRole('alert')).textContent).toBe('Project context unavailable. Try another question.')
  })
})

function submitQuestion(question: string) {
  fireEvent.input(screen.getByLabelText('Question'), { target: { value: question } })
  fireEvent.submit(screen.getByRole('form', { name: 'Search project context' }))
}

function deferredPromise<T>() {
  let resolve: (value: T) => void = () => {}
  const thenable: PromiseLike<T> = {
    then(onfulfilled) {
      resolve = (value) => { void onfulfilled?.(value) }
      return Promise.race<never>([])
    },
  }
  return { promise: Promise.resolve(thenable), resolve: (value: T) => resolve(value) }
}
