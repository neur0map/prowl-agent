import { useEffect, useRef, useState } from 'preact/hooks'

import { apiFetch } from '../../transport/api'
import type { APIEnvelope } from '../../transport/contracts'

type SetupDetection = { integrations: string[]; project_config_version: string }
type SetupPlan = { integrations: string[]; actions: Array<{ integration: string; path: string; description: string }>; project_config_version: string; hash: string }
type SetupApplyRequest = { integrations: string[]; plan_hash: string; expected_project_config_version: string; approved: true; idempotency_key: string }
type SetupApplyOutcome = { plan_hash: string; project_config_version: string; idempotency_key: string; rollback_manifest: Array<{ path: string; existed: boolean }>; verified: boolean }
type SetupVerifyOutcome = { verified: boolean }

export type SetupClient = {
  detect: () => Promise<SetupDetection>
  plan: (integrations: string[]) => Promise<SetupPlan>
  apply: (request: SetupApplyRequest) => Promise<SetupApplyOutcome>
  verify: (integrations: string[]) => Promise<SetupVerifyOutcome>
}

type DetectState = { kind: 'loading' } | { kind: 'ready'; value: SetupDetection } | { kind: 'error' }

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await apiFetch(path, init)
  if (!response.ok) throw new Error('request failed')
  const payload: unknown = await response.json()
  if (typeof payload !== 'object' || payload === null || !('data' in payload) || !('meta' in payload)) throw new Error('invalid response')
  return (payload as APIEnvelope<T>).data
}

const defaultClient: SetupClient = {
  detect: () => request<SetupDetection>('/api/v1/setup/detect'),
  plan: (integrations) => request<SetupPlan>('/api/v1/setup/plan', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ integrations }) }),
  apply: (input) => request<SetupApplyOutcome>('/api/v1/setup/apply', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(input) }),
  verify: (integrations) => request<SetupVerifyOutcome>('/api/v1/setup/verify', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ integrations }) }),
}

export function createIdempotencyKey(): string {
  return crypto.randomUUID()
}

export function SetupPage({ client = defaultClient, createIdempotencyKey: newKey = createIdempotencyKey }: {
  client?: SetupClient
  createIdempotencyKey?: () => string
}) {
  const [detection, setDetection] = useState<DetectState>({ kind: 'loading' })
  const [selected, setSelected] = useState<string[]>([])
  const [plan, setPlan] = useState<SetupPlan | null>(null)
  const [approved, setApproved] = useState(false)
  const [status, setStatus] = useState('')
  const [outcome, setOutcome] = useState<SetupApplyOutcome | null>(null)
  const [conflict, setConflict] = useState(false)
  const conflictAlert = useRef<HTMLParagraphElement>(null)

  useEffect(() => {
    let active = true
    void client.detect().then(
      (value) => { if (active) setDetection({ kind: 'ready', value }) },
      () => { if (active) setDetection({ kind: 'error' }) },
    )
    return () => { active = false }
  }, [client])

  useEffect(() => { if (conflict) conflictAlert.current?.focus() }, [conflict])

  function toggle(integration: string, checked: boolean) {
    setSelected((current) => checked ? [...current, integration] : current.filter((value) => value !== integration))
    setPlan(null)
    setApproved(false)
    setStatus('')
    setOutcome(null)
    setConflict(false)
  }

  function reviewPlan() {
    if (selected.length === 0) return
    setStatus('')
    setOutcome(null)
    setConflict(false)
    void client.plan(selected).then(setPlan, () => setStatus('Setup plan is unavailable. Try again.'))
  }

  function apply() {
    if (!plan || !approved) return
    setConflict(false)
    setStatus('')
    setOutcome(null)
    void client.apply({ integrations: plan.integrations, plan_hash: plan.hash, expected_project_config_version: plan.project_config_version, approved: true, idempotency_key: newKey() }).then(
      (value) => {
        setOutcome(value)
        setStatus(value.verified ? 'Setup applied and verified.' : 'Setup applied.')
      },
      (error: unknown) => { if (errorCode(error) === 'conflict') setConflict(true); else setStatus('Setup could not be applied. Try again.') },
    )
  }

  function verify() {
    if (!plan) return
    setStatus('')
    void client.verify(plan.integrations).then(
      (outcome) => setStatus(outcome.verified ? 'Selected integrations verified.' : 'Selected integrations could not be verified.'),
      () => setStatus('Selected integrations could not be verified.'),
    )
  }

  return <section aria-label="Setup">
    <header><h1>Setup</h1><p>Review server-provided integration actions before applying them.</p></header>
    {detection.kind === 'loading' ? <p role="status">Detecting setup…</p> : null}
    {detection.kind === 'error' ? <p role="alert">Setup is unavailable. Try again.</p> : null}
    {detection.kind === 'ready' && detection.value.integrations.length === 0 ? <p>No supported integrations were detected.</p> : null}
    {detection.kind === 'ready' && detection.value.integrations.length > 0 ? <fieldset><legend>Select integrations</legend>{detection.value.integrations.map((integration) => <label key={integration}><input type="checkbox" checked={selected.includes(integration)} onInput={(event) => toggle(integration, event.currentTarget.checked)} />{integration}</label>)}</fieldset> : null}
    {detection.kind === 'ready' && detection.value.integrations.length > 0 ? <button type="button" disabled={selected.length === 0} onClick={reviewPlan}>Review setup plan</button> : null}
    {plan ? <section aria-label="Reviewed setup plan"><h2>Reviewed setup plan</h2><ul>{plan.actions.map((action) => <li key={`${action.integration}:${action.path}`}><strong>{action.integration}</strong><code>{action.path}</code><span>{action.description}</span></li>)}</ul><label><input type="checkbox" checked={approved} onInput={(event) => setApproved(event.currentTarget.checked)} />I approve this setup plan</label><div><button type="button" disabled={!approved} onClick={apply}>Apply reviewed setup plan</button><button type="button" onClick={verify}>Verify reviewed setup</button></div></section> : null}
    {status ? <p role="status">{status}</p> : null}
    {outcome ? <dl aria-label="Setup outcome"><dt>Plan hash</dt><dd>{outcome.plan_hash}</dd><dt>Project configuration version</dt><dd>{outcome.project_config_version}</dd><dt>Idempotency key</dt><dd>{outcome.idempotency_key}</dd><dt>Verified</dt><dd>{String(outcome.verified)}</dd><dt>Rollback entries</dt><dd>{outcome.rollback_manifest.length}</dd></dl> : null}
    {conflict ? <p ref={conflictAlert} role="alert" tabIndex={-1}>The setup plan changed before it could be applied. Review the displayed plan and try again.</p> : null}
  </section>
}

function errorCode(error: unknown): string | undefined {
  return typeof error === 'object' && error !== null && 'code' in error && typeof error.code === 'string' ? error.code : undefined
}
