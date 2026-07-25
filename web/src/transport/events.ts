import { apiFetch } from './api'

const decoder = new TextDecoder()
const scopeIDPattern = /^[a-f0-9]{64}$/
const snapshotURIPattern = /^snapshot:\/\/[a-z0-9-]+$/

type PromiseResolvers<T> = {
  promise: Promise<T>
  resolve: (value: T | PromiseLike<T>) => void
  reject: (reason?: unknown) => void
}

export type EventCursor = {
  stream_scope: 'project-job'
  scope_id: string
  epoch: number
  sequence: number
}

export type StreamNotification =
  | { type: 'invalidate'; cursor: EventCursor }
  | { type: 'reset'; cursor: EventCursor; snapshotURI: string }

export type ConnectionState = 'connecting' | 'live' | 'retrying' | 'offline'

export type WatchOptions = {
  signal: AbortSignal
  onEvent: (notification: StreamNotification) => void
  onState: (state: ConnectionState) => void
}

export function isEventCursor(value: unknown): value is EventCursor {
  return isExactRecord(value, ['stream_scope', 'scope_id', 'epoch', 'sequence'])
    && value.stream_scope === 'project-job'
    && typeof value.scope_id === 'string'
    && scopeIDPattern.test(value.scope_id)
    && isPositiveSafeInteger(value.epoch)
    && isNonNegativeSafeInteger(value.sequence)
}

export async function streamProjectJobEvents(cursor: EventCursor, options: WatchOptions): Promise<void> {
  if (!isEventCursor(cursor)) throw new Error('event cursor was invalid')
  if (options.signal.aborted) throw abortError()

  const query = new URLSearchParams({
    stream_scope: cursor.stream_scope,
    scope_id: cursor.scope_id,
    epoch: String(cursor.epoch),
    sequence: String(cursor.sequence),
  })
  const response = await apiFetch(`/api/v1/events?${query}`, {
    headers: { Accept: 'text/event-stream' },
    signal: options.signal,
    cache: 'no-store',
  })
  const contentType = response.headers.get('content-type')?.split(';', 1)[0]?.trim().toLowerCase()
  if (!response.ok || contentType !== 'text/event-stream' || response.body === null) {
    throw new Error('event stream response was invalid')
  }

  options.onState('live')
  await parseEventStream(response.body, options)
}

export async function watchProjectJobEvents(initialCursor: EventCursor, options: WatchOptions): Promise<void> {
  if (!isEventCursor(initialCursor)) throw new Error('event cursor was invalid')
  let cursor = initialCursor
  let attempts = 0

  while (!options.signal.aborted) {
    options.onState(attempts === 0 ? 'connecting' : 'retrying')
    try {
      await streamProjectJobEvents(cursor, {
        ...options,
        onEvent: (notification) => {
          cursor = notification.cursor
          options.onEvent(notification)
        },
      })
      if (options.signal.aborted) break
      options.onState(isOffline() ? 'offline' : 'retrying')
    } catch (error) {
      if (options.signal.aborted || isAbortError(error)) break
      options.onState(isOffline() ? 'offline' : 'retrying')
    }

    await abortableDelay(reconnectDelayMilliseconds(attempts), options.signal)
    attempts += 1
  }
}

export function reconnectDelayMilliseconds(attempt: number): number {
  return Math.min(4_000, 250 * 2 ** Math.max(0, Math.min(attempt, 4)))
}

export function abortableDelay(milliseconds: number, signal: AbortSignal): Promise<void> {
  if (signal.aborted) return Promise.resolve()
  const { promise, resolve } = (Promise as PromiseConstructor & { withResolvers<T>(): PromiseResolvers<T> }).withResolvers<void>()
  const timer = window.setTimeout(done, milliseconds)
  function done() {
    window.clearTimeout(timer)
    signal.removeEventListener('abort', done)
    resolve()
  }
  signal.addEventListener('abort', done, { once: true })
  return promise
}

async function parseEventStream(body: ReadableStream<Uint8Array>, options: WatchOptions): Promise<void> {
  const reader = body.getReader()
  let pending = ''
  let eventName: string | undefined
  let data: string | undefined
  const onAbort = () => { void reader.cancel() }
  options.signal.addEventListener('abort', onAbort, { once: true })

  const dispatch = () => {
    if (eventName === undefined && data === undefined) return
    if (eventName === undefined || data === undefined) throw new Error('event payload was invalid')
    const payload: unknown = JSON.parse(data)
    if (eventName === 'project-job.changed') {
      if (!isChangedEvent(payload)) throw new Error('event payload was invalid')
      options.onEvent({ type: 'invalidate', cursor: payload.cursor })
    } else if (eventName === 'reset') {
      if (!isResetEvent(payload)) throw new Error('event payload was invalid')
      options.onEvent({ type: 'reset', cursor: payload.cursor, snapshotURI: payload.snapshot_uri })
    } else {
      throw new Error('event payload was invalid')
    }
    eventName = undefined
    data = undefined
  }

  const line = (raw: string) => {
    if (raw === '') {
      dispatch()
      return
    }
    if (raw.startsWith(':')) return
    const delimiter = raw.indexOf(':')
    if (delimiter <= 0) throw new Error('event payload was invalid')
    const field = raw.slice(0, delimiter)
    const value = raw.slice(delimiter + 1).startsWith(' ') ? raw.slice(delimiter + 2) : raw.slice(delimiter + 1)
    if (field === 'event' && eventName === undefined) {
      eventName = value
      return
    }
    if (field === 'data' && data === undefined) {
      data = value
      return
    }
    throw new Error('event payload was invalid')
  }

  try {
    while (true) {
      if (options.signal.aborted) throw abortError()
      const result = await reader.read()
      if (result.done) break
      pending += decoder.decode(result.value, { stream: true })
      let newline = pending.indexOf('\n')
      while (newline !== -1) {
        const raw = pending.slice(0, newline)
        line(raw.endsWith('\r') ? raw.slice(0, -1) : raw)
        pending = pending.slice(newline + 1)
        newline = pending.indexOf('\n')
      }
    }
    pending += decoder.decode()
    if (pending !== '') line(pending.endsWith('\r') ? pending.slice(0, -1) : pending)
    dispatch()
    if (options.signal.aborted) throw abortError()
  } catch (error) {
    if (options.signal.aborted) throw abortError()
    if (error instanceof SyntaxError) throw new Error('event payload was invalid')
    throw error
  } finally {
    options.signal.removeEventListener('abort', onAbort)
    await reader.cancel().catch(() => undefined)
    reader.releaseLock()
  }
}

function isChangedEvent(value: unknown): value is { cursor: EventCursor; kind: 'project-job.changed' } {
  return isExactRecord(value, ['cursor', 'kind']) && isEventCursor(value.cursor) && value.kind === 'project-job.changed'
}

function isResetEvent(value: unknown): value is { cursor: EventCursor; snapshot_uri: string } {
  return isExactRecord(value, ['cursor', 'snapshot_uri'])
    && isEventCursor(value.cursor)
    && typeof value.snapshot_uri === 'string'
    && value.snapshot_uri.length > 0
    && value.snapshot_uri.length <= 256
    && snapshotURIPattern.test(value.snapshot_uri)
}

function isExactRecord(value: unknown, keys: string[]): value is Record<string, unknown> {
  return typeof value === 'object'
    && value !== null
    && !Array.isArray(value)
    && Object.keys(value).length === keys.length
    && keys.every((key) => key in value)
}

function isPositiveSafeInteger(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value > 0
}

function isNonNegativeSafeInteger(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0
}

function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === 'AbortError'
}

function abortError(): DOMException {
  return new DOMException('event stream was aborted', 'AbortError')
}

function isOffline(): boolean {
  return typeof navigator !== 'undefined' && navigator.onLine === false
}
