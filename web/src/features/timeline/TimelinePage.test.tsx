import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/preact'
import { afterEach, describe, expect, it, vi } from 'vitest'

const { apiFetch } = vi.hoisted(() => ({ apiFetch: vi.fn() }))
vi.mock('../../transport/api', () => ({ apiFetch }))

import { TimelinePage, type TimelineClient } from './TimelinePage'

const events = [
  { id: 'git-1', occurred_at: '2026-07-24T12:00:00Z', kind: 'commit', provenance: 'git', git: { commit: 'abc123', subject: 'Add timeline' } },
  { id: 'knowledge-1', occurred_at: '2026-07-24T11:00:00Z', kind: 'decision', provenance: 'knowledge-log', knowledge: { action: 'accepted', path: 'docs/guide.md' } },
  { id: 'context-1', occurred_at: '2026-07-24T10:00:00Z', kind: 'retrieval', provenance: 'context', context: { run_id: 'run-1', query_hash: 'hash', hash_version: 'v1', mode: 'compact', budget_tokens: 10, budget_bytes: 20, estimated_tokens: 5, estimated_bytes: 10, strategy_version: 'v1', status: 'complete' } },
]

function client(overrides: Partial<TimelineClient> = {}): TimelineClient {
  return { loadPage: vi.fn().mockResolvedValue({ events, next: 'next-cursor' }), ...overrides }
}

afterEach(() => {
  cleanup()
  apiFetch.mockReset()
})

describe('TimelinePage', () => {
  it('announces loading and renders a useful empty state', async () => {
    let resolve!: (value: { events: typeof events; next: string }) => void
    const loadPage = vi.fn(() => new Promise<{ events: typeof events; next: string }>((done) => { resolve = done }))
    render(<TimelinePage client={client({ loadPage })} />)
    expect(screen.getByRole('status').textContent).toContain('Loading timeline')
    resolve({ events: [], next: '' })
    expect(await screen.findByText('No timeline events are available.')).toBeTruthy()
  })

  it('reports a privacy-safe error when the timeline is unavailable', async () => {
    render(<TimelinePage client={client({ loadPage: vi.fn().mockRejectedValue(new Error('private event')) })} />)
    expect((await screen.findByRole('alert')).textContent).toBe('Timeline is unavailable. Try again.')
  })

  it('renders server-provided provenance data in received newest-first order', async () => {
    render(<TimelinePage client={client()} />)
    const items = await screen.findAllByRole('listitem')
    expect(items.map((item) => item.textContent)).toEqual([
      'git2026-07-24T12:00:00ZAdd timelineabc123',
      'knowledge-log2026-07-24T11:00:00Zaccepteddocs/guide.md',
      'context2026-07-24T10:00:00ZRun IDrun-1Query hashhashHash versionv1ModecompactToken budget10Byte budget20Estimated tokens5Estimated bytes10Strategy versionv1Statuscomplete',
    ])
    expect(screen.getByText('hash')).toBeTruthy()
    expect(screen.getAllByText('10')).toHaveLength(2)
  })

  it('requests the next server page through its injected client', async () => {
    const api = client({ loadPage: vi.fn()
      .mockResolvedValueOnce({ events: [events[0]], next: 'next-cursor' })
      .mockResolvedValueOnce({ events: [events[1]], next: '' }) })
    render(<TimelinePage client={api} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Load more timeline events' }))
    await waitFor(() => expect(api.loadPage).toHaveBeenLastCalledWith('next-cursor'))
    expect(await screen.findByText('accepted')).toBeTruthy()
  })

  it('shows a safe error for malformed default timeline data', async () => {
    apiFetch.mockResolvedValue(new Response(JSON.stringify({ data: {}, meta: {} }), { status: 200 }))
    render(<TimelinePage />)
    expect((await screen.findByRole('alert')).textContent).toBe('Timeline is unavailable. Try again.')
  })

  it('does not offer pagination when a final server page omits next', async () => {
    render(<TimelinePage client={client({ loadPage: vi.fn().mockResolvedValue({ events }) })} />)
    await screen.findAllByRole('listitem')
    expect(screen.queryByRole('button', { name: 'Load more timeline events' })).toBeNull()
  })
})
