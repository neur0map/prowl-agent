import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/preact'
import { afterEach, describe, expect, it, vi } from 'vitest'

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

afterEach(cleanup)

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
})
