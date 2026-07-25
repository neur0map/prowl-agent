import { afterEach, describe, expect, it, vi } from 'vitest'

import { bootstrapWorkbenchSession, resetBearerForTests } from './auth'
import { abortableDelay, reconnectDelayMilliseconds, streamProjectJobEvents, watchProjectJobEvents, type EventCursor, type StreamNotification } from './events'

const nonce = 'n'.repeat(43)
const bearer = 'b'.repeat(43)
const scopeID = 'a'.repeat(64)

function cursor(sequence = 1): EventCursor {
  return { stream_scope: 'project-job', scope_id: scopeID, epoch: 3, sequence }
}

function encodedStream(...parts: string[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder()
  return new ReadableStream({
    start(controller) {
      for (const part of parts) controller.enqueue(encoder.encode(part))
      controller.close()
    },
  })
}

function controlledStream() {
  let controller: ReadableStreamDefaultController<Uint8Array> | undefined
  const body = new ReadableStream<Uint8Array>({ start: (value) => { controller = value } })
  return {
    body,
    enqueue: (value: Uint8Array) => controller?.enqueue(value),
    close: () => controller?.close(),
  }
}

async function authenticatedFetch(...responses: Response[]) {
  const fetchMock = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify({ bearer }), { status: 200 }))
  for (const response of responses) fetchMock.mockResolvedValueOnce(response)
  window.history.replaceState({}, '', `/#nonce=${nonce}`)
  vi.stubGlobal('fetch', fetchMock)
  await bootstrapWorkbenchSession()
  return fetchMock
}

async function flush() {
  await Promise.resolve()
  await Promise.resolve()
}

afterEach(() => {
  resetBearerForTests()
  vi.useRealTimers()
  vi.restoreAllMocks()
  window.history.replaceState({}, '', '/')
})

describe('project-job event transport', () => {
  it('uses authenticated same-origin fetch and parses chunked comments, changes, and resets', async () => {
    const changed = { cursor: cursor(2), kind: 'project-job.changed' }
    const reset = { cursor: cursor(9), snapshot_uri: 'snapshot://project-job' }
    const fetchMock = await authenticatedFetch(new Response(encodedStream(
      ': keepalive\r\n\r\nevent: project-job.changed\r\ndata: {"cursor":{"stream_scope":"project-job","scope_id":"' + scopeID + '","epoch":3,',
      '"sequence":2},"kind":"project-job.changed"}\r\n\r\nevent: reset\n',
      'data: {"cursor":{"stream_scope":"project-job","scope_id":"' + scopeID + '","epoch":3,"sequence":9},"snapshot_uri":"snapshot://project-job"}\n\n',
    ), { headers: { 'Content-Type': 'text/event-stream; charset=utf-8' } }))
    const received: StreamNotification[] = []
    const states: string[] = []

    await streamProjectJobEvents(cursor(), {
      signal: new AbortController().signal,
      onEvent: (notification) => received.push(notification),
      onState: (state) => states.push(state),
    })

    const [input, init] = fetchMock.mock.calls[1] as [RequestInfo | URL, RequestInit]
    expect(input).toBe(`/api/v1/events?stream_scope=project-job&scope_id=${scopeID}&epoch=3&sequence=1`)
    expect(new Headers(init.headers).get('Authorization')).toBe(`Bearer ${bearer}`)
    expect(new Headers(init.headers).get('Accept')).toBe('text/event-stream')
    expect(received).toEqual([
      { type: 'invalidate', cursor: changed.cursor },
      { type: 'reset', cursor: reset.cursor, snapshotURI: reset.snapshot_uri },
    ])
    expect(states).toEqual(['live'])
  })

  it('keeps TextDecoder state isolated for concurrent chunked streams', async () => {
    const first = controlledStream()
    const second = controlledStream()
    await authenticatedFetch(
      new Response(first.body, { headers: { 'Content-Type': 'text/event-stream' } }),
      new Response(second.body, { headers: { 'Content-Type': 'text/event-stream' } }),
    )
    const firstPending = streamProjectJobEvents(cursor(), { signal: new AbortController().signal, onEvent: vi.fn(), onState: vi.fn() })
    await flush()
    first.enqueue(new Uint8Array([58, 32, 0xe2]))
    await flush()
    const received: StreamNotification[] = []
    const secondPending = streamProjectJobEvents(cursor(2), { signal: new AbortController().signal, onEvent: (notification) => received.push(notification), onState: vi.fn() })
    await flush()
    second.enqueue(new TextEncoder().encode(`: keepalive\n\nevent: project-job.changed\ndata: {"cursor":{"stream_scope":"project-job","scope_id":"${scopeID}","epoch":3,"sequence":3},"kind":"project-job.changed"}\n\n`))
    second.close()
    first.enqueue(new Uint8Array([0x9c, 0x93, 10, 10]))
    first.close()

    await expect(secondPending).resolves.toBeUndefined()
    await expect(firstPending).resolves.toBeUndefined()
    expect(received).toEqual([{ type: 'invalidate', cursor: cursor(3) }])
  })

  it('rejects malformed events and unusable stream responses before they reach UI state', async () => {
    const malformed = new Response(encodedStream(
      `event: project-job.changed\ndata: {"cursor":{"stream_scope":"operations","scope_id":"${scopeID}","epoch":3,"sequence":2},"kind":"project-job.changed"}\n\n`,
    ), { headers: { 'Content-Type': 'text/event-stream' } })
    const missingType = new Response(encodedStream(''), { headers: { 'Content-Type': 'text/plain' } })
    const missingBody = new Response(null, { headers: { 'Content-Type': 'text/event-stream' } })
    const fetchMock = await authenticatedFetch(malformed, missingType, missingBody)
    const options = { signal: new AbortController().signal, onEvent: vi.fn(), onState: vi.fn() }

    await expect(streamProjectJobEvents(cursor(), options)).rejects.toThrow('event payload was invalid')
    await expect(streamProjectJobEvents(cursor(), options)).rejects.toThrow('event stream response was invalid')
    await expect(streamProjectJobEvents(cursor(), options)).rejects.toThrow('event stream response was invalid')
    expect(options.onEvent).not.toHaveBeenCalled()
    expect(fetchMock).toHaveBeenCalledTimes(4)
  })

  it('cancels the fetch reader when its request is aborted', async () => {
    let cancelled = false
    const fetchMock = await authenticatedFetch(new Response(new ReadableStream({
      cancel() { cancelled = true },
    }), { headers: { 'Content-Type': 'text/event-stream' } }))
    const controller = new AbortController()
    const pending = streamProjectJobEvents(cursor(), { signal: controller.signal, onEvent: vi.fn(), onState: vi.fn() })

    await flush()
    controller.abort()

    await expect(pending).rejects.toHaveProperty('name', 'AbortError')
    expect(cancelled).toBe(true)
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('delays and aborts safely when native promise resolvers are unavailable', async () => {
    vi.useFakeTimers()
    const descriptor = Object.getOwnPropertyDescriptor(Promise, 'withResolvers')
    Object.defineProperty(Promise, 'withResolvers', { configurable: true, value: undefined })
    try {
      const timeoutController = new AbortController()
      let settled = 0
      const timeout = abortableDelay(250, timeoutController.signal).then(() => { settled += 1 })
      await vi.advanceTimersByTimeAsync(249)
      expect(settled).toBe(0)
      await vi.advanceTimersByTimeAsync(1)
      await timeout
      timeoutController.abort()
      expect(settled).toBe(1)

      const abortController = new AbortController()
      const aborted = abortableDelay(4_000, abortController.signal)
      abortController.abort()
      await expect(aborted).resolves.toBeUndefined()
    } finally {
      if (descriptor === undefined) delete (Promise as PromiseConstructor & { withResolvers?: unknown }).withResolvers
      else Object.defineProperty(Promise, 'withResolvers', descriptor)
    }
  })

  it('reconnects from the latest reset cursor with bounded, abortable delay', async () => {
    vi.useFakeTimers()
    const controller = new AbortController()
    const fetchMock = await authenticatedFetch(
      new Response(encodedStream(`event: reset\ndata: {"cursor":{"stream_scope":"project-job","scope_id":"${scopeID}","epoch":3,"sequence":12},"snapshot_uri":"snapshot://project-job"}\n\n`), { headers: { 'Content-Type': 'text/event-stream' } }),
      new Response(new ReadableStream({}), { headers: { 'Content-Type': 'text/event-stream' } }),
    )
    const received: StreamNotification[] = []
    const states: string[] = []
    const watching = watchProjectJobEvents(cursor(), {
      signal: controller.signal,
      onEvent: (notification) => received.push(notification),
      onState: (state) => states.push(state),
    })

    await flush()
    await vi.advanceTimersByTimeAsync(250)
    await flush()

    expect(received).toEqual([{ type: 'reset', cursor: cursor(12), snapshotURI: 'snapshot://project-job' }])
    expect(fetchMock.mock.calls[2]?.[0]).toBe(`/api/v1/events?stream_scope=project-job&scope_id=${scopeID}&epoch=3&sequence=12`)
    expect(states).toContain('retrying')
    expect(reconnectDelayMilliseconds(0)).toBe(250)
    expect(reconnectDelayMilliseconds(8)).toBe(4_000)

    const waiting = abortableDelay(4_000, controller.signal)
    controller.abort()
    await expect(waiting).resolves.toBeUndefined()
    await expect(watching).resolves.toBeUndefined()
  })
})
