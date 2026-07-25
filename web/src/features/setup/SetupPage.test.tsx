import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/preact'
import { afterEach, describe, expect, it, vi } from 'vitest'

const { apiFetch } = vi.hoisted(() => ({ apiFetch: vi.fn() }))
vi.mock('../../transport/api', () => ({ apiFetch }))

import { SetupPage, type SetupClient } from './SetupPage'

const detected = { integrations: ['vscode', 'neovim'], project_config_version: 'config-v1' }
const plan = {
  integrations: ['vscode'], project_config_version: 'config-v1', hash: 'plan-hash',
  actions: [{ integration: 'vscode', path: '.vscode/mcp.json', description: 'Install Prowl server' }],
}

function client(overrides: Partial<SetupClient> = {}): SetupClient {
  return {
    detect: vi.fn().mockResolvedValue(detected),
    plan: vi.fn().mockResolvedValue(plan),
    apply: vi.fn().mockResolvedValue({ plan_hash: 'plan-hash', project_config_version: 'config-v2', idempotency_key: 'fresh-key', rollback_manifest: [], verified: true }),
    verify: vi.fn().mockResolvedValue({ verified: true }),
    ...overrides,
  }
}

afterEach(() => {
  cleanup()
  apiFetch.mockReset()
})

describe('SetupPage', () => {
  it('announces detection loading and renders a useful empty state', async () => {
    let resolve!: (value: typeof detected) => void
    const detect = vi.fn(() => new Promise<typeof detected>((done) => { resolve = done }))
    render(<SetupPage client={client({ detect })} />)
    expect(screen.getByRole('status').textContent).toContain('Detecting setup')
    resolve({ integrations: [], project_config_version: 'config-v1' })
    expect(await screen.findByText('No supported integrations were detected.')).toBeTruthy()
  })

  it('reports a privacy-safe error when detection fails', async () => {
    render(<SetupPage client={client({ detect: vi.fn().mockRejectedValue(new Error('private config')) })} />)
    expect((await screen.findByRole('alert')).textContent).toBe('Setup is unavailable. Try again.')
  })

  it('renders detected integrations and the exact server plan preview', async () => {
    const api = client()
    render(<SetupPage client={api} />)
    fireEvent.click(await screen.findByRole('checkbox', { name: 'vscode' }))
    fireEvent.click(screen.getByRole('button', { name: 'Review setup plan' }))
    expect(await screen.findByText('Install Prowl server')).toBeTruthy()
    expect(screen.getByText('.vscode/mcp.json')).toBeTruthy()
    expect(api.plan).toHaveBeenCalledWith(['vscode'])
  })

  it('requires approval before applying and sends the reviewed preconditions', async () => {
    const api = client()
    render(<SetupPage client={api} createIdempotencyKey={() => 'fresh-key'} />)
    fireEvent.click(await screen.findByRole('checkbox', { name: 'vscode' }))
    fireEvent.click(screen.getByRole('button', { name: 'Review setup plan' }))
    const apply = await screen.findByRole('button', { name: 'Apply reviewed setup plan' }) as HTMLButtonElement
    expect(apply.disabled).toBe(true)
    fireEvent.click(screen.getByRole('checkbox', { name: 'I approve this setup plan' }))
    fireEvent.click(apply)
    await waitFor(() => expect(api.apply).toHaveBeenCalledWith({
      integrations: ['vscode'], plan_hash: 'plan-hash', expected_project_config_version: 'config-v1', approved: true, idempotency_key: 'fresh-key',
    }))
    expect((await screen.findByRole('status')).textContent).toBe('Setup applied and verified.')
    expect(screen.getByText('config-v2')).toBeTruthy()
  })

  it('focuses a conflict alert while retaining the reviewed plan', async () => {
    const api = client({ apply: vi.fn().mockRejectedValue({ code: 'conflict' }) })
    render(<SetupPage client={api} createIdempotencyKey={() => 'fresh-key'} />)
    fireEvent.click(await screen.findByRole('checkbox', { name: 'vscode' }))
    fireEvent.click(screen.getByRole('button', { name: 'Review setup plan' }))
    await screen.findByText('Install Prowl server')
    fireEvent.click(screen.getByRole('checkbox', { name: 'I approve this setup plan' }))
    fireEvent.click(screen.getByRole('button', { name: 'Apply reviewed setup plan' }))
    const alert = await screen.findByRole('alert')
    await waitFor(() => expect(document.activeElement).toBe(alert))
    expect(screen.getByText('Install Prowl server')).toBeTruthy()
  })

  it('verifies only the selected reviewed integrations through its injected client', async () => {
    const api = client()
    render(<SetupPage client={api} />)
    fireEvent.click(await screen.findByRole('checkbox', { name: 'vscode' }))
    fireEvent.click(screen.getByRole('button', { name: 'Review setup plan' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Verify reviewed setup' }))
    await waitFor(() => expect(api.verify).toHaveBeenCalledWith(['vscode']))
    expect((await screen.findByRole('status')).textContent).toBe('Selected integrations verified.')
  })

  it('maps a production setup conflict to the focused alert', async () => {
    apiFetch.mockImplementation((path: string) => Promise.resolve(new Response(JSON.stringify(path.endsWith('/detect') ? { data: detected, meta: {} } : path.endsWith('/plan') ? { data: plan, meta: {} } : { error: { code: 'setup_conflict' }, meta: {} }), { status: path.endsWith('/apply') ? 409 : 200 })))
    render(<SetupPage createIdempotencyKey={() => 'fresh-key'} />)
    fireEvent.click(await screen.findByRole('checkbox', { name: 'vscode' }))
    fireEvent.click(screen.getByRole('button', { name: 'Review setup plan' }))
    fireEvent.click(await screen.findByRole('checkbox', { name: 'I approve this setup plan' }))
    fireEvent.click(screen.getByRole('button', { name: 'Apply reviewed setup plan' }))
    const alert = await screen.findByRole('alert')
    await waitFor(() => expect(document.activeElement).toBe(alert))
  })

  it('requires fresh approval after a newly reviewed plan arrives', async () => {
    const api = client({ plan: vi.fn().mockResolvedValueOnce(plan).mockResolvedValueOnce({ ...plan, hash: 'new-hash' }) })
    render(<SetupPage client={api} />)
    fireEvent.click(await screen.findByRole('checkbox', { name: 'vscode' }))
    fireEvent.click(screen.getByRole('button', { name: 'Review setup plan' }))
    fireEvent.click(await screen.findByRole('checkbox', { name: 'I approve this setup plan' }))
    fireEvent.click(screen.getByRole('button', { name: 'Review setup plan' }))
    await waitFor(() => expect(api.plan).toHaveBeenCalledTimes(2))
    expect((screen.getByRole('checkbox', { name: 'I approve this setup plan' }) as HTMLInputElement).checked).toBe(false)
  })

  it('ignores a plan response superseded by a selection change', async () => {
    const pending = deferred<typeof plan>()
    const api = client({ plan: vi.fn(() => pending.promise) })
    render(<SetupPage client={api} />)
    fireEvent.click(await screen.findByRole('checkbox', { name: 'vscode' }))
    fireEvent.click(screen.getByRole('button', { name: 'Review setup plan' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'vscode' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'neovim' }))
    pending.resolve(plan)
    await Promise.resolve()
    await Promise.resolve()
    expect(screen.queryByText('Install Prowl server')).toBeNull()
  })

  it('shows a safe error for malformed default detection data', async () => {
    apiFetch.mockResolvedValue(new Response(JSON.stringify({ data: {}, meta: {} }), { status: 200 }))
    render(<SetupPage />)
    expect((await screen.findByRole('alert')).textContent).toBe('Setup is unavailable. Try again.')
  })

  it('disables Apply while its mutation is pending', async () => {
    const pending = deferred<{ plan_hash: string; project_config_version: string; idempotency_key: string; rollback_manifest: []; verified: boolean }>()
    const api = client({ apply: vi.fn(() => pending.promise) })
    render(<SetupPage client={api} />)
    fireEvent.click(await screen.findByRole('checkbox', { name: 'vscode' }))
    fireEvent.click(screen.getByRole('button', { name: 'Review setup plan' }))
    fireEvent.click(await screen.findByRole('checkbox', { name: 'I approve this setup plan' }))
    fireEvent.click(screen.getByRole('button', { name: 'Apply reviewed setup plan' }))
    expect((screen.getByRole('button', { name: 'Apply reviewed setup plan' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('ignores an Apply completion after its reviewed plan is superseded', async () => {
    const pending = deferred<{ plan_hash: string; project_config_version: string; idempotency_key: string; rollback_manifest: []; verified: boolean }>()
    const api = client({ apply: vi.fn(() => pending.promise) })
    render(<SetupPage client={api} />)
    fireEvent.click(await screen.findByRole('checkbox', { name: 'vscode' }))
    fireEvent.click(screen.getByRole('button', { name: 'Review setup plan' }))
    fireEvent.click(await screen.findByRole('checkbox', { name: 'I approve this setup plan' }))
    fireEvent.click(screen.getByRole('button', { name: 'Apply reviewed setup plan' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'vscode' }))
    pending.resolve({ plan_hash: 'plan-hash', project_config_version: 'config-v2', idempotency_key: 'fresh-key', rollback_manifest: [], verified: true })
    await Promise.resolve()
    await Promise.resolve()
    expect(screen.queryByText('Setup applied and verified.')).toBeNull()
  })

  it('ignores a Verify completion after its reviewed plan is superseded', async () => {
    const pending = deferred<{ verified: boolean }>()
    const api = client({ verify: vi.fn(() => pending.promise) })
    render(<SetupPage client={api} />)
    fireEvent.click(await screen.findByRole('checkbox', { name: 'vscode' }))
    fireEvent.click(screen.getByRole('button', { name: 'Review setup plan' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Verify reviewed setup' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'vscode' }))
    pending.resolve({ verified: true })
    await Promise.resolve()
    await Promise.resolve()
    expect(screen.queryByText('Selected integrations verified.')).toBeNull()
  })
})
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}
