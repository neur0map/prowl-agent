import { authenticatedBearer } from './auth'

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
