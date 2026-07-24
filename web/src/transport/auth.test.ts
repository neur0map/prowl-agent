import { afterEach, describe, expect, it, vi } from 'vitest'

import { bootstrapWorkbenchSession, resetBearerForTests } from './auth'

const nonce = 'n'.repeat(43)
const bearer = 'b'.repeat(43)

afterEach(() => {
  resetBearerForTests()
  vi.restoreAllMocks()
  window.history.replaceState({}, '', '/')
})

describe('workbench bootstrap handoff', () => {
  it('strips a 256-bit nonce before exchanging it for a memory-only bearer', async () => {
    window.history.replaceState({}, '', `/#nonce=${nonce}`)
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => new Response(JSON.stringify({ bearer }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    const exchange = bootstrapWorkbenchSession()
    expect(window.location.hash).toBe('')
    await exchange

    expect(window.localStorage.length).toBe(0)
    expect(window.sessionStorage.length).toBe(0)
    const [input, init] = fetchMock.mock.calls[0] as [RequestInfo | URL, RequestInit]
    expect(input).toBe('/api/v1/auth/bootstrap')
    expect(init).toMatchObject({ method: 'POST', redirect: 'error' })
    expect(new Headers(init.headers).get('Content-Type')).toBe('application/json')
    expect(JSON.parse(String(init.body))).toEqual({ nonce })
  })

  it('rejects malformed or legacy fragments after removing them from history', async () => {
    for (const fragment of ['#nonce=short&extra=leak', `#token=${bearer}`]) {
      window.history.replaceState({}, '', `/${fragment}`)
      await expect(bootstrapWorkbenchSession()).rejects.toThrow(/invalid workbench bootstrap nonce/i)
      expect(window.location.hash).toBe('')
    }
  })
})
