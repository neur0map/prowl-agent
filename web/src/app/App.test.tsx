import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/preact'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { bootstrapWorkbenchSession, resetBearerForTests } from '../transport/auth'
import { App } from './App'

const nonce = 'n'.repeat(43)
const bearer = 'b'.repeat(43)

async function authenticatedFetch(...responses: Response[]) {
  const fetchMock = vi.fn()
    .mockResolvedValueOnce(new Response(JSON.stringify({ bearer }), { status: 200 }))
  for (const response of responses) fetchMock.mockResolvedValueOnce(response)
  window.history.replaceState({}, '', `/#nonce=${nonce}`)
  vi.stubGlobal('fetch', fetchMock)
  await bootstrapWorkbenchSession()
  return fetchMock
}

afterEach(() => {
  cleanup()
  resetBearerForTests()
  vi.restoreAllMocks()
  window.history.replaceState({}, '', '/')
})

describe('workbench shell', () => {
  it('starts with an accessible project-brief loading state', () => {
    render(<App />)

    expect(screen.getByRole('heading', { name: 'Project brief' })).toBeTruthy()
    expect(screen.getByRole('status')).toHaveProperty('textContent', 'Loading project brief…')
    expect(screen.getByLabelText('Project brief').getAttribute('aria-busy')).toBe('true')
  })

  it('gives an accessible recovery path when the one-time session is unavailable', () => {
    render(<App sessionError />)

    expect(screen.getByRole('heading', { name: 'Secure workbench session unavailable' })).toBeTruthy()
    expect(screen.getByRole('alert')).toHaveProperty('textContent', 'Secure workbench session unavailable. Reopen Prowl from your terminal.')
  })
  it('tracks hash navigation, marks the active primary route, and moves focus into main', async () => {
    window.history.replaceState({}, '', '/#/explore')
    render(<App />)

    expect(screen.getByRole('link', { name: 'Explore' }).getAttribute('aria-current')).toBe('page')
    expect(screen.getByRole('link', { name: 'Home' }).getAttribute('aria-current')).toBeNull()
    window.location.hash = '#/timeline'
    window.dispatchEvent(new HashChangeEvent('hashchange'))

    expect(await screen.findByRole('heading', { name: 'Timeline' })).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Timeline' }).getAttribute('aria-current')).toBe('page')
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('main')))
  })

  it('keeps the active route when the skip link moves focus to main', async () => {
    window.history.replaceState({}, '', '/#/timeline')
    render(<App />)
    await screen.findByRole('heading', { name: 'Timeline' })

    const skipLink = screen.getByRole('link', { name: 'Skip to content' })
    skipLink.focus()
    fireEvent.click(skipLink)

    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('main')))
    expect(window.location.hash).toBe('#/timeline')
    expect(screen.getByRole('heading', { name: 'Timeline' })).toBeTruthy()
  })

  it('loads a bounded source preview while retaining the full source anchor and retries safely', async () => {
    const fetchMock = await authenticatedFetch(
      new Response(JSON.stringify({ code: 'source_unavailable', message: 'private detail' }), { status: 500 }),
      new Response(JSON.stringify({ data: { path: 'src/app.ts', line_start: 10, line_end: 12, lines: [{ number: 10, text: 'const app = 1' }, { number: 11, text: '' }, { number: 12, text: '' }] }, meta: {} }), { status: 200 }),
    )
    window.history.replaceState({}, '', '/#/source?path=src%2Fapp.ts&line_start=10&line_end=900&preview_end=12')
    render(<App />)

    expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'Source preview unavailable. Check the selected source link and try again.')
    expect(fetchMock.mock.calls[1]?.[0]).toBe('/api/v1/source?path=src%2Fapp.ts&line_start=10&line_end=12')
    expect(screen.getByText('Full source anchor: lines 10–900')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Retry source preview' }))

    await waitFor(() => expect(screen.getByLabelText('Source preview for src/app.ts').textContent).toContain('const app = 1'))
    expect(fetchMock).toHaveBeenCalledTimes(3)
  })

  it('rejects source previews that do not match the requested bounded range', async () => {
    await authenticatedFetch(new Response(JSON.stringify({
      data: { path: 'src/other.ts', line_start: 10, line_end: 12, lines: [{ number: 10, text: 'private source' }, { number: 11, text: 'private source' }, { number: 12, text: 'private source' }] },
      meta: {},
    }), { status: 200 }))
    window.history.replaceState({}, '', '/#/source?path=src%2Fapp.ts&line_start=10&line_end=900&preview_end=12')
    render(<App />)

    expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'Source preview unavailable. Check the selected source link and try again.')
    expect(screen.queryByText('private source')).toBeNull()
  })

  it('rejects malformed source hashes without sending a source request', async () => {
    const fetchMock = await authenticatedFetch()
    window.history.replaceState({}, '', '/#/source?path=..%2Fsecrets.txt&line_start=1&line_end=2&preview_end=2')
    render(<App />)

    expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'Source preview unavailable. Check the selected source link and try again.')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('rejects drive-qualified source paths without sending a source request', async () => {
    const fetchMock = await authenticatedFetch()
    window.history.replaceState({}, '', '/#/source?path=C%3A%2Fworkspace%2Fsecret.ts&line_start=1&line_end=2&preview_end=2')
    render(<App />)

    expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'Source preview unavailable. Check the selected source link and try again.')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('rejects control characters in source paths without sending a source request', async () => {
    const fetchMock = await authenticatedFetch()
    window.history.replaceState({}, '', '/#/source?path=src%2Fapi%0Aprivate%09.go&line_start=1&line_end=2&preview_end=2')
    render(<App />)

    expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'Source preview unavailable. Check the selected source link and try again.')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('loads the selected guided tour through the authenticated API and renders its source-backed steps', async () => {
    const fetchMock = await authenticatedFetch(new Response(JSON.stringify({
      data: {
        id: 'architecture',
        title: 'Architecture tour',
        description: 'A guided path.',
        steps: [{ number: 1, section_id: 'core', title: 'Core', description: 'Start here.', facts: [{ id: 'fact-1', label: 'Entrypoint', detail: 'src/main.ts', anchor: { path: 'src/main.ts', line_start: 4, line_end: 8 } }] }],
      },
      meta: {},
    }), { status: 200 }))
    window.history.replaceState({}, '', '/#/explore?tour=architecture')
    render(<App />)

    expect(await screen.findByRole('heading', { name: 'Architecture tour' })).toBeTruthy()
    expect(screen.getByRole('link', { name: 'src/main.ts lines 4–8' })).toBeTruthy()
    expect(fetchMock.mock.calls[1]?.[0]).toBe('/api/v1/tours/architecture')
  })

  it('does not render malformed guided tour facts', async () => {
    await authenticatedFetch(new Response(JSON.stringify({
      data: { id: 'architecture', title: 'private title', description: 'private detail', steps: [{ number: 1, section_id: 'core', title: 'Core', description: 'Start here.', facts: [{ id: 'fact-1', label: 7, detail: 'private fact' }] }] },
      meta: {},
    }), { status: 200 }))
    window.history.replaceState({}, '', '/#/explore?tour=architecture')
    render(<App />)

    expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'Guided tour unavailable. Check the selected tour and try again.')
    expect(screen.queryByText('private title')).toBeNull()
  })

  it('posts only selected context IDs and renders the server-provided packet', async () => {
    const fetchMock = await authenticatedFetch(new Response(JSON.stringify({
      data: {
        summary: 'One bounded context packet.',
        items: [{ id: 'ctx-1', title: 'Server-selected fact', kind: 'fact', why_selected: ['direct match'], freshness: 'current', confidence: 1, audience: ['developer'], citations: [], detail_resource: 'source', estimated_tokens: 12 }],
        budget: { estimated_tokens: 12, estimated_bytes: 34, exact_bytes: 34 },
      },
      meta: {},
    }), { status: 200 }))
    window.history.replaceState({}, '', '/#/context?ids=ctx-1,ctx-2')
    render(<App />)

    expect(await screen.findByText('One bounded context packet.')).toBeTruthy()
    expect(screen.getByText('Server-selected fact')).toBeTruthy()
    const [input, init] = fetchMock.mock.calls[1] as [RequestInfo | URL, RequestInit]
    expect(input).toBe('/api/v1/context/get')
    expect(init.body).toBe('{"ids":["ctx-1","ctx-2"]}')
  })

  it('posts canonical colon-bearing context IDs without treating them as proposal identifiers', async () => {
    const fetchMock = await authenticatedFetch(new Response(JSON.stringify({
      data: {
        summary: 'Canonical source selection.',
        items: [{ id: 'source:api', title: 'Authenticated API', kind: 'source', why_selected: ['selected source'], estimated_tokens: 12 }],
        budget: { estimated_tokens: 12, estimated_bytes: 34, exact_bytes: 34 },
      },
      meta: {},
    }), { status: 200 }))
    window.history.replaceState({}, '', '/#/context?ids=source%3Aapi')
    render(<App />)

    expect(await screen.findByText('Canonical source selection.')).toBeTruthy()
    expect(fetchMock.mock.calls[1]?.[0]).toBe('/api/v1/context/get')
    expect((fetchMock.mock.calls[1]?.[1] as RequestInit).body).toBe('{"ids":["source:api"]}')
  })

  it('does not render malformed context packets', async () => {
    await authenticatedFetch(new Response(JSON.stringify({
      data: {
        summary: 'private packet detail',
        items: [{ id: 'ctx-1', title: 'invalid', kind: 'fact', why_selected: [42], estimated_tokens: 12 }],
        budget: { estimated_tokens: 12, estimated_bytes: 34, exact_bytes: 34 },
      },
      meta: {},
    }), { status: 200 }))
    window.history.replaceState({}, '', '/#/context?ids=ctx-1')
    render(<App />)

    expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'Selected context unavailable. Check the selected items and try again.')
    expect(screen.queryByText('private packet detail')).toBeNull()
  })

  it('passes knowledge proposal hashes to the production knowledge view', async () => {
    const fetchMock = await authenticatedFetch(
      new Response(JSON.stringify({ data: { items: [], next: '' }, meta: {} }), { status: 200 }),
      new Response(JSON.stringify({ data: { proposal: { id: 'proposal-7', operation: 'create', target_path: 'docs/guide.md', candidate_path: 'docs/guide.md', status: 'pending', created_at: '2026-01-01T00:00:00Z' }, diff: '@@ proposal', version: 'version-1' }, meta: {} }), { status: 200 }),
    )
    window.history.replaceState({}, '', '/#/knowledge?proposal=proposal-7')
    render(<App />)

    expect(await screen.findByRole('heading', { name: 'Knowledge proposal' })).toBeTruthy()
    expect(fetchMock.mock.calls.slice(1).map(([input]) => input)).toEqual([
      '/api/v1/knowledge',
      '/api/v1/knowledge/proposals/proposal-7',
    ])
  })
})
