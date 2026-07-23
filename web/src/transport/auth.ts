let bearerToken: string | null = null

const tokenPattern = /^[A-Za-z0-9_-]{43}$/

export function consumeLaunchToken(
  location: Location = window.location,
  history: History = window.history,
): string | null {
  if (!location.hash.startsWith('#token=')) return bearerToken

  const params = new URLSearchParams(location.hash.slice(1))
  const token = params.get('token')
  history.replaceState(history.state, '', `${location.pathname}${location.search}`)
  if (params.size !== 1 || token === null || !tokenPattern.test(token)) {
    bearerToken = null
    throw new Error('invalid workbench token')
  }
  bearerToken = token
  return token
}

export async function apiFetch(path: string, init: RequestInit = {}): Promise<Response> {
  if (!bearerToken) throw new Error('workbench session is not authenticated')
  const target = new URL(path, window.location.origin)
  if (!path.startsWith('/api/v1/') || path.startsWith('//') || target.origin !== window.location.origin || target.hash !== '') {
    throw new Error('workbench bearer is restricted to the versioned same-origin API')
  }
  const headers = new Headers(init.headers)
  headers.set('Authorization', `Bearer ${bearerToken}`)
  return fetch(`${target.pathname}${target.search}`, { ...init, headers })
}

export function resetBearerForTests(): void {
  bearerToken = null
}
