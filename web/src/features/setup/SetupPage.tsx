import { useEffect, useRef, useState } from 'preact/hooks'

import { apiFetch } from '../../transport/api'
import { useI18n } from '../../i18n'

type SetupDetection = { integrations: string[]; project_config_version: string }
type SetupPlan = { integrations: string[]; actions: Array<{ integration: string; path: string; description: string }>; project_config_version: string; hash: string }
type SetupApplyRequest = { integrations: string[]; plan_hash: string; expected_project_config_version: string; approved: true; idempotency_key: string }
type SetupApplyOutcome = { plan_hash: string; project_config_version: string; idempotency_key: string; rollback_manifest: Array<{ path: string; existed: boolean }>; verified: boolean }
type SetupVerifyOutcome = { verified: boolean }

export type SetupClient = { detect: () => Promise<SetupDetection>; plan: (integrations: string[]) => Promise<SetupPlan>; apply: (request: SetupApplyRequest) => Promise<SetupApplyOutcome>; verify: (integrations: string[]) => Promise<SetupVerifyOutcome> }
type DetectState = { kind: 'loading' } | { kind: 'ready'; value: SetupDetection } | { kind: 'error' }

class ClientError extends Error {
  constructor(readonly code?: string) { super('request failed') }
}

async function request(path: string, init?: RequestInit): Promise<unknown> {
  const response = await apiFetch(path, init)
  const payload: unknown = await response.json().catch(() => null)
  if (!response.ok) throw new ClientError(errorCode(payload))
  if (!isEnvelope(payload)) throw new ClientError()
  return payload.data
}

const defaultClient: SetupClient = {
  async detect() { const value = await request('/api/v1/setup/detect'); if (!isDetection(value)) throw new ClientError(); return value },
  async plan(integrations) { const value = await request('/api/v1/setup/plan', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ integrations }) }); if (!isPlan(value)) throw new ClientError(); return value },
  async apply(input) { const value = await request('/api/v1/setup/apply', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(input) }); if (!isApplyOutcome(value)) throw new ClientError(); return value },
  async verify(integrations) { const value = await request('/api/v1/setup/verify', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ integrations }) }); if (!isVerifyOutcome(value)) throw new ClientError(); return value },
}

export function createIdempotencyKey(): string { return crypto.randomUUID() }

export function SetupPage({ client = defaultClient, createIdempotencyKey: newKey = createIdempotencyKey }: { client?: SetupClient; createIdempotencyKey?: () => string }) {
  const { t, formatNumber } = useI18n()
  const [detection, setDetection] = useState<DetectState>({ kind: 'loading' })
  const [selected, setSelected] = useState<string[]>([])
  const [plan, setPlan] = useState<SetupPlan | null>(null)
  const [approved, setApproved] = useState(false)
  const [status, setStatus] = useState('')
  const [outcome, setOutcome] = useState<SetupApplyOutcome | null>(null)
  const [applying, setApplying] = useState(false)
  const [verifying, setVerifying] = useState(false)
  const [operationError, setOperationError] = useState(false)
  const [conflict, setConflict] = useState(false)
  const planRequest = useRef(0)
  const conflictAlert = useRef<HTMLParagraphElement>(null)

  useEffect(() => {
    let active = true
    void client.detect().then((value) => { if (active) setDetection({ kind: 'ready', value }) }, () => { if (active) setDetection({ kind: 'error' }) })
    return () => { active = false }
  }, [client])

  useEffect(() => { if (conflict) conflictAlert.current?.focus() }, [conflict])

  function toggle(integration: string, checked: boolean) {
    ++planRequest.current
    setSelected((current) => checked ? [...current, integration] : current.filter((value) => value !== integration))
    setPlan(null)
    setApproved(false)
    setStatus('')
    setOutcome(null)
    setOperationError(false)
    setConflict(false)
    setApplying(false)
    setVerifying(false)
  }

  function reviewPlan() {
    if (selected.length === 0) return
    const requestID = ++planRequest.current
    setPlan(null)
    setApproved(false)
    setStatus('')
    setOutcome(null)
    setOperationError(false)
    setConflict(false)
    setApplying(false)
    setVerifying(false)
    void client.plan(selected).then(
      (value) => { if (planRequest.current === requestID) setPlan(value) },
      () => { if (planRequest.current === requestID) setOperationError(true) },
    )
  }

  function apply() {
    if (!plan || !approved || applying) return
    const generation = planRequest.current
    setApplying(true)
    setConflict(false)
    setOperationError(false)
    setStatus('')
    setOutcome(null)
    void client.apply({ integrations: plan.integrations, plan_hash: plan.hash, expected_project_config_version: plan.project_config_version, approved: true, idempotency_key: newKey() }).then(
      (value) => { if (planRequest.current === generation) { setApplying(false); setOutcome(value); setStatus(value.verified ? t('setup.appliedVerified') : t('setup.applied')) } },
      (error: unknown) => { if (planRequest.current === generation) { setApplying(false); if (isSetupConflict(errorCode(error))) setConflict(true); else setOperationError(true) } },
    )
  }

  function verify() {
    if (!plan || verifying) return
    const generation = planRequest.current
    setVerifying(true)
    setOperationError(false)
    setStatus('')
    void client.verify(plan.integrations).then(
      (value) => { if (planRequest.current === generation) { setVerifying(false); setStatus(value.verified ? t('setup.selectedVerified') : t('setup.selectedUnverified')) } },
      () => { if (planRequest.current === generation) { setVerifying(false); setOperationError(true) } },
    )
  }

  return <section aria-label={t('setup.aria')}>
    <header><h1>{t('setup.heading')}</h1><p>{t('setup.description')}</p></header>
    {detection.kind === 'loading' ? <p role="status">{t('setup.detecting')}</p> : null}
    {detection.kind === 'error' ? <p role="alert">{t('setup.unavailable')}</p> : null}
    {detection.kind === 'ready' && detection.value.integrations.length === 0 ? <p>{t('setup.empty')}</p> : null}
    {detection.kind === 'ready' && detection.value.integrations.length > 0 ? <fieldset><legend>{t('setup.select')}</legend>{detection.value.integrations.map((integration) => <label key={integration}><input type="checkbox" checked={selected.includes(integration)} onInput={(event) => toggle(integration, event.currentTarget.checked)} />{integration}</label>)}</fieldset> : null}
    {detection.kind === 'ready' && detection.value.integrations.length > 0 ? <button type="button" disabled={selected.length === 0} onClick={reviewPlan}>{t('setup.review')}</button> : null}
    {plan ? <section aria-label={t('setup.reviewedAria')}><h2>{t('setup.reviewedHeading')}</h2><ul>{plan.actions.map((action) => <li key={`${action.integration}:${action.path}`}><strong>{action.integration}</strong><code>{action.path}</code><span>{action.description}</span></li>)}</ul><label><input type="checkbox" checked={approved} onInput={(event) => setApproved(event.currentTarget.checked)} />{t('setup.approve')}</label><div><button type="button" disabled={!approved || applying} onClick={apply}>{t('setup.apply')}</button><button type="button" disabled={verifying} onClick={verify}>{t('setup.verifyReviewed')}</button></div></section> : null}
    {status ? <p role="status">{status}</p> : null}
    {outcome ? <dl aria-label={t('setup.outcomeAria')}><dt>{t('setup.planHash')}</dt><dd>{outcome.plan_hash}</dd><dt>{t('setup.configVersion')}</dt><dd>{outcome.project_config_version}</dd><dt>{t('setup.idempotencyKey')}</dt><dd>{outcome.idempotency_key}</dd><dt>{t('setup.verified')}</dt><dd>{String(outcome.verified)}</dd><dt>{t('setup.rollbackEntries')}</dt><dd>{formatNumber(outcome.rollback_manifest.length)}</dd></dl> : null}
    {operationError ? <p role="alert">{t('setup.operationUnavailable')}</p> : null}
    {conflict ? <p ref={conflictAlert} role="alert" tabIndex={-1}>{t('setup.conflictDetail')}</p> : null}
  </section>
}

function isEnvelope(value: unknown): value is { data: unknown; meta: Record<string, unknown> } { return isRecord(value) && 'data' in value && isRecord(value.meta) }
function isDetection(value: unknown): value is SetupDetection { return isRecord(value) && isStringArray(value.integrations) && isString(value.project_config_version) }
function isPlan(value: unknown): value is SetupPlan { return isRecord(value) && isStringArray(value.integrations) && isString(value.project_config_version) && isString(value.hash) && Array.isArray(value.actions) && value.actions.every((action) => isRecord(action) && isString(action.integration) && isString(action.path) && isString(action.description)) }
function isApplyOutcome(value: unknown): value is SetupApplyOutcome { return isRecord(value) && isString(value.plan_hash) && isString(value.project_config_version) && isString(value.idempotency_key) && typeof value.verified === 'boolean' && Array.isArray(value.rollback_manifest) && value.rollback_manifest.every((item) => isRecord(item) && isString(item.path) && typeof item.existed === 'boolean') }
function isVerifyOutcome(value: unknown): value is SetupVerifyOutcome { return isRecord(value) && typeof value.verified === 'boolean' }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null }
function isString(value: unknown): value is string { return typeof value === 'string' }
function isStringArray(value: unknown): value is string[] { return Array.isArray(value) && value.every(isString) }
function errorCode(value: unknown): string | undefined {
  if (!isRecord(value)) return undefined
  if (isString(value.code)) return value.code
  if (isRecord(value.error) && isString(value.error.code)) return value.error.code
  return undefined
}
function isSetupConflict(code: string | undefined): boolean { return code === 'setup_conflict' || code === 'conflict' }
