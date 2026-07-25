import { authenticatedBearer } from './auth'

class APIResponseError extends Error {
  constructor() {
    super('workbench API response was unavailable')
  }
}

function isEnvelope(value: unknown): value is { data: unknown; meta: Record<string, unknown> } {
  return typeof value === 'object' && value !== null
    && 'data' in value
    && 'meta' in value
    && typeof value.meta === 'object'
    && value.meta !== null
}

export async function apiJSON<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await apiFetch(path, init)
  const payload: unknown = await response.json().catch(() => null)
  if (!response.ok || !isEnvelope(payload)) throw new APIResponseError()
  return payload.data as T
}

export async function apiFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const target = new URL(path, window.location.origin)
  if (
    !path.startsWith('/api/v1/')
    || path.startsWith('//')
    || target.origin !== window.location.origin
    || !target.pathname.startsWith('/api/v1/')
    || target.hash !== ''
  ) {
    throw new Error('workbench bearer is restricted to the versioned same-origin API')
  }
  const headers = new Headers(init.headers)
  headers.set('Authorization', `Bearer ${authenticatedBearer()}`)
  return fetch(`${target.pathname}${target.search}`, { ...init, headers, redirect: 'error' })
}
