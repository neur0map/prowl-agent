let bearerToken: string | null = null

const tokenPattern = /^[A-Za-z0-9_-]{43}$/

function stripFragment(location: Location, history: History): void {
  history.replaceState(history.state, '', `${location.pathname}${location.search}`)
}

function consumeBootstrapNonce(
  location: Location = window.location,
  history: History = window.history,
): string {
  const fragment = location.hash
  stripFragment(location, history)
  if (!fragment.startsWith('#nonce=')) {
    throw new Error('invalid workbench bootstrap nonce')
  }
  const params = new URLSearchParams(fragment.slice(1))
  const nonce = params.get('nonce')
  if (params.size !== 1 || nonce === null || !tokenPattern.test(nonce)) {
    throw new Error('invalid workbench bootstrap nonce')
  }
  return nonce
}

type BootstrapResponse = {
  bearer: string
}

function isBootstrapResponse(payload: unknown, nonce: string): payload is BootstrapResponse {
  return typeof payload === 'object'
    && payload !== null
    && 'bearer' in payload
    && typeof payload.bearer === 'string'
    && tokenPattern.test(payload.bearer)
    && payload.bearer !== nonce
}

export async function bootstrapWorkbenchSession(
  location: Location = window.location,
  history: History = window.history,
): Promise<void> {
  if (bearerToken !== null) return
  const nonce = consumeBootstrapNonce(location, history)
  const response = await fetch('/api/v1/auth/bootstrap', {
    body: JSON.stringify({ nonce }),
    headers: { 'Content-Type': 'application/json' },
    method: 'POST',
    redirect: 'error',
  })
  if (!response.ok) {
    throw new Error('workbench bootstrap was denied')
  }
  const payload: unknown = await response.json()
  if (!isBootstrapResponse(payload, nonce)) {
    throw new Error('invalid workbench bootstrap response')
  }
  bearerToken = payload.bearer
}

export function authenticatedBearer(): string {
  if (bearerToken === null) throw new Error('workbench session is not authenticated')
  return bearerToken
}

export function resetBearerForTests(): void {
  bearerToken = null
}
