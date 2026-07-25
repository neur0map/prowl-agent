import { useEffect, useRef, useState } from 'preact/hooks'

import { APIResponseError, apiJSON } from '../../transport/api'
import { isEventCursor, watchProjectJobEvents, type EventCursor, type WatchOptions } from '../../transport/events'

const refetchDebounceMilliseconds = 200
const jobIDPattern = /^[a-f0-9]{32}$/
const jobTextPattern = /^[a-z0-9_-]{0,128}$/
const timestampPattern = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/
const requiredJobFields = ['id', 'kind', 'status', 'version', 'phase', 'progress', 'created_at', 'updated_at', 'stream'] as const
const jobFields: Record<string, true> = {
  id: true,
  kind: true,
  status: true,
  version: true,
  phase: true,
  progress: true,
  outcome: true,
  error_code: true,
  created_at: true,
  updated_at: true,
  stream: true,
}

export type ProjectJob = {
  id: string
  kind: 'index'
  status: 'queued' | 'running' | 'cancelling' | 'succeeded' | 'failed' | 'cancelled'
  version: number
  phase: string
  progress: number
  outcome?: string
  error_code?: string
  created_at: string
  updated_at: string
  stream: EventCursor
}

export type JobClient = {
  refresh: (signal: AbortSignal) => Promise<ProjectJob>
  get: (id: string, signal: AbortSignal) => Promise<ProjectJob>
  cancel: (id: string, expectedVersion: number, idempotencyKey: string, signal: AbortSignal) => Promise<ProjectJob>
}

export type JobWatcher = (cursor: EventCursor, options: WatchOptions) => Promise<void>
export type { WatchOptions } from '../../transport/events'

type JobStatusProps = {
  onInvalidate?: () => void
  client?: JobClient
  watch?: JobWatcher
  createIdempotencyKey?: () => string
}

type Action = 'refreshing' | 'cancelling' | null

type RefetchState = {
  inFlight: boolean
  trailing: boolean
  timer: number | undefined
  controller: AbortController | null
}

const defaultClient: JobClient = {
  refresh: (signal) => loadJob('/api/v1/index/refresh', { method: 'POST', signal }),
  get: (id, signal) => loadJob(`/api/v1/jobs/${encodeURIComponent(id)}`, { signal }),
  cancel: (id, expectedVersion, idempotencyKey, signal) => loadJob(`/api/v1/jobs/${encodeURIComponent(id)}/cancel`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ expected_version: expectedVersion, idempotency_key: idempotencyKey }),
    signal,
  }),
}

export function createIdempotencyKey(): string {
  return crypto.randomUUID()
}

export function JobStatus({ onInvalidate, client = defaultClient, watch = watchProjectJobEvents, createIdempotencyKey: newKey = createIdempotencyKey }: JobStatusProps) {
  const [job, setJob] = useState<ProjectJob | null>(null)
  const [action, setAction] = useState<Action>(null)
  const [connection, setConnection] = useState<'connecting' | 'live' | 'retrying' | 'offline' | null>(null)
  const [message, setMessage] = useState('Index is ready to refresh.')
  const mounted = useRef(true)
  const jobRef = useRef<ProjectJob | null>(null)
  const clientRef = useRef(client)
  const invalidateRef = useRef(onInvalidate)
  const actionController = useRef<AbortController | null>(null)
  const refetch = useRef<RefetchState>({ inFlight: false, trailing: false, timer: undefined, controller: null })
  const scheduleRefetch = useRef<(delay: number) => void>(() => undefined)

  clientRef.current = client
  invalidateRef.current = onInvalidate

  function applyCanonicalJob(next: ProjectJob, notifyTerminal = false) {
    jobRef.current = next
    if (mounted.current) setJob(next)
    if (notifyTerminal && isTerminal(next)) invalidateRef.current?.()
  }

  function refetchCanonical() {
    const current = jobRef.current
    const state = refetch.current
    if (current === null || !isActive(current)) return
    if (state.inFlight) {
      state.trailing = true
      return
    }
    state.inFlight = true
    const controller = new AbortController()
    state.controller = controller
    void clientRef.current.get(current.id, controller.signal).then(
      (next) => {
        if (!isProjectJob(next) || jobRef.current?.id !== current.id || controller.signal.aborted) {
          if (!controller.signal.aborted && mounted.current) setMessage('Index status could not be verified.')
          return
        }
        applyCanonicalJob(next, true)
      },
      () => {
        if (!controller.signal.aborted && mounted.current) {
          setConnection('offline')
          setMessage('Connection unavailable. Retrying when available.')
        }
      },
    ).finally(() => {
      if (state.controller === controller) state.controller = null
      state.inFlight = false
      if (state.trailing && !controller.signal.aborted) {
        state.trailing = false
        scheduleRefetch.current(0)
      }
    })
  }

  scheduleRefetch.current = (delay) => {
    const current = jobRef.current
    const state = refetch.current
    if (current === null || !isActive(current) || state.timer !== undefined) return
    state.timer = window.setTimeout(() => {
      state.timer = undefined
      refetchCanonical()
    }, delay)
  }

  useEffect(() => () => {
    mounted.current = false
    actionController.current?.abort()
    const state = refetch.current
    if (state.timer !== undefined) window.clearTimeout(state.timer)
    state.controller?.abort()
  }, [])

  const streamKey = job !== null && isActive(job) ? `${job.id}:${job.stream.scope_id}:${job.stream.epoch}:${job.stream.sequence}` : ''
  useEffect(() => {
    const current = jobRef.current
    if (streamKey === '' || current === null) return
    const controller = new AbortController()
    void watch(current.stream, {
      signal: controller.signal,
      onEvent: () => scheduleRefetch.current(refetchDebounceMilliseconds),
      onState: (state) => {
        if (!mounted.current || controller.signal.aborted) return
        setConnection(state)
        if (state === 'retrying' || state === 'offline') scheduleRefetch.current(refetchDebounceMilliseconds)
      },
    })
    return () => controller.abort()
  }, [streamKey, watch])

  function refreshIndex() {
    const controller = new AbortController()
    actionController.current?.abort()
    actionController.current = controller
    setAction('refreshing')
    setMessage('Starting index refresh…')
    void clientRef.current.refresh(controller.signal).then(
      (next) => {
        if (!isProjectJob(next)) {
          if (!controller.signal.aborted) setMessage('Index response could not be verified.')
          return
        }
        if (!controller.signal.aborted) {
          applyCanonicalJob(next)
          setMessage('Index refresh started.')
        }
      },
      () => {
        if (!controller.signal.aborted) {
          setConnection('offline')
          setMessage('Index refresh is unavailable. Retry when the connection is available.')
        }
      },
    ).finally(() => {
      if (!controller.signal.aborted && mounted.current) setAction(null)
    })
  }

  function cancelIndex() {
    const current = jobRef.current
    if (current === null || (current.status !== 'queued' && current.status !== 'running')) return
    const idempotencyKey = newKey()
    if (!isPrintableKey(idempotencyKey)) {
      setMessage('Cancel request could not be prepared. Retry the action.')
      return
    }
    const controller = new AbortController()
    actionController.current?.abort()
    actionController.current = controller
    setAction('cancelling')
    setMessage('Requesting index cancellation…')
    void clientRef.current.cancel(current.id, current.version, idempotencyKey, controller.signal).then(
      (next) => {
        if (!isProjectJob(next)) {
          if (!controller.signal.aborted) setMessage('Index response could not be verified.')
          return
        }
        if (!controller.signal.aborted) {
          applyCanonicalJob(next)
          setMessage('Index cancellation requested.')
        }
      },
      (error: unknown) => {
        if (controller.signal.aborted) return
        if (error instanceof APIResponseError && error.status === 409 && error.code === 'job_version_conflict') {
          setMessage('Index state changed; reloaded current status.')
          scheduleRefetch.current(0)
          return
        }
        setConnection('offline')
        setMessage('Connection unavailable. Retrying when available.')
      },
    ).finally(() => {
      if (!controller.signal.aborted && mounted.current) setAction(null)
    })
  }

  const terminal = job !== null && isTerminal(job)
  const actionDisabled = action !== null
  const liveJob = job !== null && isActive(job)
  const connectionText = connection === 'connecting' ? 'Connecting to live updates.'
    : connection === 'live' ? 'Live updates connected.'
      : connection === 'retrying' ? 'Reconnecting to live updates.'
        : connection === 'offline' ? 'Live updates unavailable; retrying when available.'
          : ''
  const announce = job !== null || action !== null || connection !== null || message !== 'Index is ready to refresh.'


  return (
    <section class="job-status" aria-label="Index status" data-state={job?.status ?? 'idle'} aria-busy={action !== null ? 'true' : undefined}>
      <div class="job-status-heading">
        <span class="eyebrow">Workspace index</span>
        <strong>{job === null ? 'Index ready' : `Index ${job.status}`}</strong>
      </div>
      {liveJob ? <div class="job-status-detail">
        <span>{job.phase}</span>
        <label>
          <span>Index progress: {job.progress}%</span>
          <progress aria-label={`Index progress: ${job.progress}%`} value={job.progress} max="100">{job.progress}%</progress>
        </label>
      </div> : null}
      {terminal && job !== null ? <p class="job-outcome">{terminalOutcome(job)}</p> : null}
      <div class="job-status-actions">
        {job === null || terminal ? <button type="button" onClick={refreshIndex} disabled={actionDisabled}>{action === 'refreshing' ? 'Refreshing index…' : 'Refresh index'}</button> : null}
        {job !== null && (job.status === 'queued' || job.status === 'running' || job.status === 'cancelling') ? <button type="button" onClick={cancelIndex} disabled={actionDisabled || job.status === 'cancelling'}>{action === 'cancelling' || job.status === 'cancelling' ? 'Cancelling index…' : 'Cancel index'}</button> : null}
      </div>
      <p class="job-status-message" role={announce ? 'status' : undefined} aria-live={announce ? 'polite' : undefined}>{message}{connectionText ? ` ${connectionText}` : ''}</p>
    </section>
  )
}

async function loadJob(path: string, init: RequestInit): Promise<ProjectJob> {
  const value = await apiJSON<unknown>(path, init)
  if (!isProjectJob(value)) throw new Error('job response was invalid')
  return value
}

export function isProjectJob(value: unknown): value is ProjectJob {
  return isRecord(value)
    && Object.keys(value).every((key) => jobFields[key] === true)
    && requiredJobFields.every((key) => key in value)
    && typeof value.id === 'string'
    && jobIDPattern.test(value.id)
    && value.kind === 'index'
    && (value.status === 'queued' || value.status === 'running' || value.status === 'cancelling' || value.status === 'succeeded' || value.status === 'failed' || value.status === 'cancelled')
    && typeof value.version === 'number'
    && Number.isSafeInteger(value.version)
    && value.version > 0
    && typeof value.phase === 'string'
    && jobTextPattern.test(value.phase)
    && typeof value.progress === 'number'
    && Number.isInteger(value.progress)
    && value.progress >= 0
    && value.progress <= 100
    && (value.outcome === undefined || (typeof value.outcome === 'string' && jobTextPattern.test(value.outcome)))
    && (value.error_code === undefined || (typeof value.error_code === 'string' && jobTextPattern.test(value.error_code)))
    && typeof value.created_at === 'string'
    && timestampPattern.test(value.created_at)
    && typeof value.updated_at === 'string'
    && timestampPattern.test(value.updated_at)
    && isEventCursor(value.stream)
}

function isActive(job: ProjectJob): boolean {
  return job.status === 'queued' || job.status === 'running' || job.status === 'cancelling'
}

function isTerminal(job: ProjectJob): boolean {
  return job.status === 'succeeded' || job.status === 'failed' || job.status === 'cancelled'
}

function terminalOutcome(job: ProjectJob): string {
  if (job.status === 'succeeded') return `Index completed${job.outcome ? `: ${job.outcome}.` : '.'}`
  if (job.status === 'cancelled') return `Index cancelled${job.outcome ? `: ${job.outcome}.` : '.'}`
  return `Index failed${job.error_code ? `: ${job.error_code}.` : '.'}`
}

function isPrintableKey(value: string): boolean {
  return value.length > 0 && value.length <= 128 && /^[\x21-\x7e]+$/.test(value)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
