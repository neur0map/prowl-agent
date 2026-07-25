import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/preact'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { KnowledgePage, type KnowledgeClient } from './KnowledgePage'

const page = {
  items: [{
    id: 'doc-1', path: 'docs/guide.md', type: 'guide', title: 'Guide', description: 'Canonical guide', tags: ['docs'], related: [], anchors: [{ path: 'docs/guide.md', line_start: 2, line_end: 4 }],
  }],
  next: 'next-page',
}
const detail = { ...page.items[0], body: '# Guide\nContent', backlinks: [] }
const proposal = { proposal: { id: 'proposal-1', title: 'Update guide' }, diff: '@@ -1 +1 @@\n-old\n+new', version: 'proposal-version' }

function client(overrides: Partial<KnowledgeClient> = {}): KnowledgeClient {
  return {
    loadPage: vi.fn().mockResolvedValue(page),
    loadDetail: vi.fn().mockResolvedValue(detail),
    loadProposal: vi.fn().mockResolvedValue(proposal),
    decide: vi.fn().mockResolvedValue({ version: 'decided-version', idempotent: false }),
    ...overrides,
  }
}

afterEach(cleanup)

describe('KnowledgePage', () => {
  it('announces loading before rendering the canonical empty list', async () => {
    let resolve!: (value: typeof page) => void
    const loadPage = vi.fn(() => new Promise<typeof page>((done) => { resolve = done }))
    render(<KnowledgePage client={client({ loadPage })} />)
    expect(screen.getByRole('status').textContent).toContain('Loading knowledge')
    resolve({ items: [], next: '' })
    expect(await screen.findByText('No knowledge documents are available.')).toBeTruthy()
  })

  it('reports a privacy-safe error when knowledge loading fails', async () => {
    render(<KnowledgePage client={client({ loadPage: vi.fn().mockRejectedValue(new Error('private detail')) })} />)
    expect((await screen.findByRole('alert')).textContent).toBe('Knowledge is unavailable. Try again.')
  })

  it('renders canonical list data and selected detail without rendering body HTML', async () => {
    render(<KnowledgePage client={client()} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Guide' }))
    expect(await screen.findByRole('heading', { name: 'Guide' })).toBeTruthy()
    expect(screen.getByText((_, element) => element?.tagName === 'PRE' && element.textContent === '# Guide\nContent')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'docs/guide.md lines 2–4' }).getAttribute('href')).toContain('#/source?path=docs%2Fguide.md&line_start=2&line_end=4&preview_end=4')
  })

  it('requests the next canonical knowledge page through its injected client', async () => {
    const api = client({ loadPage: vi.fn()
      .mockResolvedValueOnce({ items: [page.items[0]], next: 'next-page' })
      .mockResolvedValueOnce({ items: [{ ...page.items[0], id: 'doc-2', title: 'Second guide' }], next: '' }) })
    render(<KnowledgePage client={api} />)
    fireEvent.click(await screen.findByRole('button', { name: 'Load more knowledge' }))
    await waitFor(() => expect(api.loadPage).toHaveBeenLastCalledWith('next-page'))
    expect(await screen.findByRole('button', { name: 'Second guide' })).toBeTruthy()
  })

  it('requires confirmation before deciding a server-provided proposal and sends exact preconditions', async () => {
    const api = client()
    render(<KnowledgePage client={api} proposalID="proposal-1" createIdempotencyKey={() => 'fresh-key'} />)
    expect(await screen.findByText((_, element) => element?.tagName === 'PRE' && element.textContent === '@@ -1 +1 @@\n-old\n+new')).toBeTruthy()
    const accept = screen.getByRole('button', { name: 'Accept proposal' }) as HTMLButtonElement
    expect(accept.disabled).toBe(true)
    fireEvent.click(screen.getByRole('checkbox', { name: 'I have reviewed this proposal diff' }))
    fireEvent.click(accept)
    await waitFor(() => expect(api.decide).toHaveBeenCalledWith('proposal-1', 'accept', {
      expected_version: 'proposal-version', idempotency_key: 'fresh-key', confirm: true,
    }))
    expect((await screen.findByRole('status')).textContent).toBe('Proposal accepted.')
  })

  it('focuses a conflict alert while keeping the reviewed proposal visible', async () => {
    const api = client({ decide: vi.fn().mockRejectedValue({ code: 'conflict' }) })
    render(<KnowledgePage client={api} proposalID="proposal-1" createIdempotencyKey={() => 'fresh-key'} />)
    await screen.findByText((_, element) => element?.tagName === 'PRE' && element.textContent === '@@ -1 +1 @@\n-old\n+new')
    fireEvent.click(screen.getByRole('checkbox', { name: 'I have reviewed this proposal diff' }))
    fireEvent.click(screen.getByRole('button', { name: 'Reject proposal' }))
    const alert = await screen.findByRole('alert')
    await waitFor(() => expect(document.activeElement).toBe(alert))
    expect(screen.getByText((_, element) => element?.tagName === 'PRE' && element.textContent === '@@ -1 +1 @@\n-old\n+new')).toBeTruthy()
  })
})
