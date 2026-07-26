import { useEffect, useRef, useState } from 'preact/hooks'

import { APIResponseError, apiJSON } from '../../transport/api'
import { isEventCursor, watchProjectJobEvents, type EventCursor, type WatchOptions } from '../../transport/events'
import { useI18n, type I18n } from '../../i18n'

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
  const { t, formatNumber } = useI18n()
  const [job, setJob] = useState<ProjectJob | null>(null)
  const [action, setAction] = useState<Action>(null)
  const [connection, setConnection] = useState<'connecting' | 'live' | 'retrying' | 'offline' | null>(null)
  const [message, setMessage] = useState(() => t('jobs.ready'))
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
          if (!controller.signal.aborted && mounted.current) setMessage(t('jobs.verifyFailed'))
          return
        }
        applyCanonicalJob(next, true)
      },
      () => {
        if (!controller.signal.aborted && mounted.current) {
          setConnection('offline')
          setMessage(t('jobs.connectionUnavailable'))
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
    setMessage(t('jobs.starting'))
    void clientRef.current.refresh(controller.signal).then(
      (next) => {
        if (!isProjectJob(next)) {
          if (!controller.signal.aborted) setMessage(t('jobs.responseVerifyFailed'))
          return
        }
        if (!controller.signal.aborted) {
          applyCanonicalJob(next)
          setMessage(t('jobs.started'))
        }
      },
      () => {
        if (!controller.signal.aborted) {
          setConnection('offline')
          setMessage(t('jobs.refreshUnavailable'))
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
      setMessage(t('jobs.cancelInvalid'))
      return
    }
    const controller = new AbortController()
    actionController.current?.abort()
    actionController.current = controller
    setAction('cancelling')
    setMessage(t('jobs.cancellationRequesting'))
    void clientRef.current.cancel(current.id, current.version, idempotencyKey, controller.signal).then(
      (next) => {
        if (!isProjectJob(next)) {
          if (!controller.signal.aborted) setMessage(t('jobs.responseVerifyFailed'))
          return
        }
        if (!controller.signal.aborted) {
          applyCanonicalJob(next)
          setMessage(t('jobs.cancellationRequested'))
        }
      },
      (error: unknown) => {
        if (controller.signal.aborted) return
        if (error instanceof APIResponseError && error.status === 409 && error.code === 'job_version_conflict') {
          setMessage(t('jobs.stateChanged'))
          scheduleRefetch.current(0)
          return
        }
        setConnection('offline')
        setMessage(t('jobs.connectionUnavailable'))
      },
    ).finally(() => {
      if (!controller.signal.aborted && mounted.current) setAction(null)
    })
  }

  const terminal = job !== null && isTerminal(job)
  const actionDisabled = action !== null
  const liveJob = job !== null && isActive(job)
  const connectionText = connection === 'connecting' ? t('jobs.connecting')
    : connection === 'live' ? t('jobs.live')
      : connection === 'retrying' ? t('jobs.retrying')
        : connection === 'offline' ? t('jobs.offline')
          : ''
  const announce = job !== null || action !== null || connection !== null || message !== t('jobs.ready')

  const progressText = liveJob && job !== null ? t('jobs.indexProgress', { progress: formatNumber(job.progress) }) : ''


  return (
    <section class="job-status" aria-label={t('jobs.statusAria')} data-state={job?.status ?? 'idle'} aria-busy={action !== null ? 'true' : undefined}>
      <div class="job-status-heading">
        <span class="eyebrow">{t('jobs.workspaceIndex')}</span>
        <strong>{job === null ? t('jobs.indexReady') : t('jobs.indexWithStatus', { status: job.status })}</strong>
      </div>
      {liveJob ? <div class="job-status-detail">
        <span>{job.phase}</span>
        <label>
          <span>{progressText}</span>
          <progress aria-label={progressText} value={job.progress} max="100">{progressText}</progress>
        </label>
      </div> : null}
      {terminal && job !== null ? <p class="job-outcome">{terminalOutcome(job, t)}</p> : null}
      <div class="job-status-actions">
        {job === null || terminal ? <button type="button" onClick={refreshIndex} disabled={actionDisabled}>{action === 'refreshing' ? t('jobs.refreshing') : t('jobs.refresh')}</button> : null}
        {job !== null && (job.status === 'queued' || job.status === 'running' || job.status === 'cancelling') ? <button type="button" onClick={cancelIndex} disabled={actionDisabled || job.status === 'cancelling'}>{action === 'cancelling' || job.status === 'cancelling' ? t('jobs.cancellingAction') : t('jobs.cancel')}</button> : null}
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

function terminalOutcome(job: ProjectJob, t: I18n['t']): string {
  const detail = job.status === 'failed' ? job.error_code : job.outcome
  const suffix = detail ? t('jobs.outcomeDetail', { detail }) : t('jobs.outcomePeriod')
  if (job.status === 'succeeded') return t('jobs.completedOutcome', { detail: suffix })
  if (job.status === 'cancelled') return t('jobs.cancelledOutcome', { detail: suffix })
  return t('jobs.failedOutcome', { detail: suffix })
}

function isPrintableKey(value: string): boolean {
  return value.length > 0 && value.length <= 128 && /^[\x21-\x7e]+$/.test(value)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
