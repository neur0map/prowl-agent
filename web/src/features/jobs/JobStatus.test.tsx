import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/preact'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { APIResponseError } from '../../transport/api'
import { abortableDelay, type StreamNotification } from '../../transport/events'
import { JobStatus, type JobClient, type JobWatcher, type ProjectJob, type WatchOptions } from './JobStatus'

const jobID = 'a'.repeat(32)
const scopeID = 'b'.repeat(64)

function job(overrides: Partial<ProjectJob> = {}): ProjectJob {
  return {
    id: jobID,
    kind: 'index',
    status: 'running',
    version: 4,
    phase: 'indexing',
    progress: 42,
    outcome: '',
    error_code: '',
    created_at: '2026-07-25T12:00:00Z',
    updated_at: '2026-07-25T12:00:01Z',
    stream: { stream_scope: 'project-job', scope_id: scopeID, epoch: 2, sequence: 5 },
    ...overrides,
  }
}

function client(overrides: Partial<JobClient> = {}): JobClient {
  return {
    refresh: vi.fn(async () => job()),
    get: vi.fn(async () => job()),
    cancel: vi.fn(async () => job({ status: 'cancelling', version: 5 })),
    ...overrides,
  }
}

function watcher(onOptions?: (options: WatchOptions) => void): JobWatcher {
  return vi.fn(async (_cursor, options) => {
    onOptions?.(options)
    await abortableDelay(60_000, options.signal)
  })
}

function change(sequence: number): StreamNotification {
  return { type: 'invalidate', cursor: { stream_scope: 'project-job', scope_id: scopeID, epoch: 2, sequence } }
}

function reset(sequence: number): StreamNotification {
  return { type: 'reset', cursor: { stream_scope: 'project-job', scope_id: scopeID, epoch: 2, sequence }, snapshotURI: 'snapshot://project-job' }
}

async function startIndex() {
  fireEvent.click(screen.getByRole('button', { name: 'Refresh index' }))
  await screen.findByRole('progressbar', { name: 'Index progress: 42%' })
}

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('JobStatus', () => {
  it('does not create an index job until the operator clicks Refresh index', async () => {
    const jobs = client()
    render(<JobStatus client={jobs} watch={watcher()} />)

    expect(screen.getByRole('button', { name: 'Refresh index' })).toBeTruthy()
    expect(jobs.refresh).not.toHaveBeenCalled()

    await startIndex()

    expect(jobs.refresh).toHaveBeenCalledTimes(1)
    expect(screen.getByText('Index running')).toBeTruthy()
    expect(screen.getByText('indexing')).toBeTruthy()
  })

  it('does not render an invalid refresh DTO and reports the failed canonical verification', async () => {
    const jobs = client({ refresh: vi.fn(async () => job({ id: 'not-a-canonical-job' })) })
    render(<JobStatus client={jobs} watch={watcher()} />)

    fireEvent.click(screen.getByRole('button', { name: 'Refresh index' }))

    await waitFor(() => expect(within(screen.getByLabelText('Index status')).getByRole('status').textContent).toContain('Index response could not be verified.'))
    expect(screen.queryByRole('progressbar')).toBeNull()
  })

  it('renders canonical queued/running progress and sends the current version with a fresh idempotency key', async () => {
    const jobs = client()
    const newKey = vi.fn(() => 'fresh-cancel-key')
    render(<JobStatus client={jobs} watch={watcher()} createIdempotencyKey={newKey} />)

    await startIndex()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel index' }))

    await waitFor(() => expect(jobs.cancel).toHaveBeenCalledWith(jobID, 4, 'fresh-cancel-key', expect.any(AbortSignal)))
    expect(newKey).toHaveBeenCalledTimes(1)
    expect(screen.getByRole('button', { name: 'Cancelling index…' }).hasAttribute('disabled')).toBe(true)
  })

  it('uses stream notifications only to debounce canonical refetches and invalidates the app after a terminal snapshot', async () => {
    vi.useFakeTimers()
    let options: WatchOptions | undefined
    const jobs = client({ get: vi.fn(async () => job({ status: 'succeeded', progress: 100, phase: 'complete', outcome: 'completed', stream: { stream_scope: 'project-job', scope_id: scopeID, epoch: 2, sequence: 8 } })) })
    const invalidate = vi.fn()
    render(<JobStatus client={jobs} watch={watcher((value) => { options = value })} onInvalidate={invalidate} />)

    await startIndex()
    await waitFor(() => expect(options).toBeDefined())
    options?.onEvent(change(6))
    options?.onEvent(reset(7))
    expect(screen.getByRole('progressbar', { name: 'Index progress: 42%' })).toBeTruthy()

    await vi.advanceTimersByTimeAsync(200)
    await waitFor(() => expect(jobs.get).toHaveBeenCalledTimes(1))

    expect(screen.queryByRole('progressbar')).toBeNull()
    expect(screen.getByText('Index completed: completed.')).toBeTruthy()
    expect(invalidate).toHaveBeenCalledTimes(1)
  })

  it('refetches canonical state after a stale cancellation conflict instead of guessing a version locally', async () => {
    vi.useFakeTimers()
    const jobs = client({
      cancel: vi.fn(async () => { throw new APIResponseError(409, 'job_version_conflict') }),
      get: vi.fn(async () => job({ version: 5, progress: 57, stream: { stream_scope: 'project-job', scope_id: scopeID, epoch: 2, sequence: 9 } })),
    })
    render(<JobStatus client={jobs} watch={watcher()} createIdempotencyKey={() => 'conflict-key'} />)

    await startIndex()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel index' }))

    await vi.advanceTimersByTimeAsync(0)
    await waitFor(() => expect(jobs.get).toHaveBeenCalledTimes(1))

    expect(jobs.cancel).toHaveBeenCalledWith(jobID, 4, 'conflict-key', expect.any(AbortSignal))
    expect(screen.getByRole('progressbar', { name: 'Index progress: 57%' })).toBeTruthy()
    expect(within(screen.getByLabelText('Index status')).getByRole('status').textContent).toContain('Index state changed; reloaded current status.')
  })

  it('keeps the last canonical job and exposes offline retry status after a network cancellation failure', async () => {
    const jobs = client({ cancel: vi.fn(async () => { throw new TypeError('network unavailable') }) })
    render(<JobStatus client={jobs} watch={watcher()} createIdempotencyKey={() => 'offline-key'} />)

    await startIndex()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel index' }))

    await waitFor(() => expect(within(screen.getByLabelText('Index status')).getByRole('status').textContent).toContain('Connection unavailable. Retrying when available.'))
    expect(screen.getByRole('progressbar', { name: 'Index progress: 42%' })).toBeTruthy()
  })
})
