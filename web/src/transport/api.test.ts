import { afterEach, describe, expect, it, vi } from 'vitest'

import { apiJSON } from './api'
import { apiFetch } from './api'
import { bootstrapWorkbenchSession, resetBearerForTests } from './auth'

const nonce = 'n'.repeat(43)
const bearer = 'b'.repeat(43)


afterEach(() => {
  resetBearerForTests()
  vi.restoreAllMocks()
  window.history.replaceState({}, '', '/')
})

describe('authenticated workbench API transport', () => {
  it('adds the memory bearer only to normalized same-origin API requests and rejects redirects', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ bearer }), { status: 200 }))
      .mockResolvedValueOnce(new Response('{}', { status: 200 }))
    window.history.replaceState({}, '', `/#nonce=${nonce}`)
    vi.stubGlobal('fetch', fetchMock)
    await bootstrapWorkbenchSession()

    await apiFetch('/api/v1/health?fresh=1', { headers: { 'X-Request-ID': 'request-7' }, redirect: 'manual' })

    expect(fetchMock).toHaveBeenCalledTimes(2)
    const [input, init] = fetchMock.mock.calls[1] as [RequestInfo | URL, RequestInit]
    expect(input).toBe('/api/v1/health?fresh=1')
    expect(init.redirect).toBe('error')
    const headers = new Headers(init.headers)
    expect(headers.get('Authorization')).toBe(`Bearer ${bearer}`)
    expect(headers.get('X-Request-ID')).toBe('request-7')
  })

  it('refuses to send the bearer outside the versioned same-origin API', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify({ bearer }), { status: 200 }))
    window.history.replaceState({}, '', `/#nonce=${nonce}`)
    vi.stubGlobal('fetch', fetchMock)
    await bootstrapWorkbenchSession()

    for (const target of ['https://attacker.example/api/v1/health', '//attacker.example/api/v1/health', '/assets/app.js', '/api/v2/health', '/api/v1/health#fragment']) {
      await expect(apiFetch(target)).rejects.toThrow(/versioned same-origin API/i)
    }
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('returns envelope data and hides malformed or failed response details', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ bearer }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ code: 'private_failure', message: 'private server detail' }), { status: 500 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ data: { path: 'src/app.ts' }, meta: {} }), { status: 200 }))
    window.history.replaceState({}, '', `/#nonce=${nonce}`)
    vi.stubGlobal('fetch', fetchMock)
    await bootstrapWorkbenchSession()

    await expect(apiJSON<{ path: string }>('/api/v1/source')).rejects.toThrow('workbench API response was unavailable')
    await expect(apiJSON<{ path: string }>('/api/v1/source')).resolves.toEqual({ path: 'src/app.ts' })
  })
})
