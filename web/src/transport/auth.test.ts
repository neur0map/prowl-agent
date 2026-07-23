import { afterEach, describe, expect, it, vi } from 'vitest'

import { apiFetch, consumeLaunchToken, resetBearerForTests } from './auth'

afterEach(() => {
  resetBearerForTests()
  vi.restoreAllMocks()
})

describe('workbench bearer handoff', () => {
  it('consumes a 256-bit URL-safe token and immediately removes the fragment', async () => {
    const token = 'a'.repeat(43)
    window.history.replaceState({}, '', `/#token=${token}`)
    expect(consumeLaunchToken()).toBe(token)
    expect(window.location.hash).toBe('')
    expect(window.localStorage.length).toBe(0)

    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => new Response('{}', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    await apiFetch('/api/v1/health')
    const init = fetchMock.mock.calls[0]?.[1] as RequestInit
    expect(new Headers(init.headers).get('Authorization')).toBe(`Bearer ${token}`)
  })

  it('refuses to send the bearer outside the versioned same-origin API', async () => {
    const token = 'b'.repeat(43)
    window.history.replaceState({}, '', `/#token=${token}`)
    consumeLaunchToken()
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    for (const target of ['https://attacker.example/api/v1/health', '//attacker.example/api/v1/health', '/assets/app.js', '/api/v2/health']) {
      await expect(apiFetch(target)).rejects.toThrow(/versioned same-origin API/i)
    }
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('rejects malformed fragments without retaining them', () => {
    window.history.replaceState({}, '', '/#token=short&extra=leak')
    expect(() => consumeLaunchToken()).toThrow(/invalid workbench token/i)
    expect(window.location.hash).toBe('')
  })
})
