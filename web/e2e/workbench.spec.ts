import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { spawn, spawnSync, type ChildProcessWithoutNullStreams } from 'node:child_process'
import { cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from 'node:fs'
import { once } from 'node:events'
import { tmpdir } from 'node:os'
import { setTimeout as delay } from 'node:timers/promises'
import { join, resolve } from 'node:path'

const repository = resolve(import.meta.dirname, '..', '..')
const sourceAnchor = 'cmd/server/main.go'
const fixtureNames = ['go-auth-service', 'ts-checkout', 'rice-config'] as const
const contextQuestionByFixture: Record<(typeof fixtureNames)[number], string> = {
  'go-auth-service': 'access tokens',
  'ts-checkout': 'checkout calculation',
  'rice-config': 'Hyprland',
}
const fixtureProjects = new Map<(typeof fixtureNames)[number], string>()
const requiredScreenRoutes = [
  'GET /api/v1/brief',
  'GET /api/v1/explore',
  'GET /api/v1/tours/{tour_id}',
  'GET /api/v1/source',
  'POST /api/v1/context/search',
  'POST /api/v1/context/get',
  'POST /api/v1/impact',
  'GET /api/v1/knowledge',
  'GET /api/v1/timeline',
  'GET /api/v1/setup/detect',
] as const

function inventoryRoute(method: string, url: string): string | null {
  const path = new URL(url).pathname
  if (/^\/api\/v1\/tours\/[A-Za-z0-9._-]+$/.test(path)) return `${method} /api/v1/tours/{tour_id}`
  if (path === '/api/v1/source') return `${method} /api/v1/source`
  const route = `${method} ${path}`
  return requiredScreenRoutes.includes(route as (typeof requiredScreenRoutes)[number]) ? route : null
}
type EventCursor = {
  stream_scope: 'project-job'
  scope_id: string
  epoch: number
  sequence: number
}

type SSEMessage = {
  event: string
  data: Record<string, unknown>
}

async function readSSEMessage(origin: string, bearer: string, cursor: EventCursor): Promise<SSEMessage> {
  const query = new URLSearchParams({
    stream_scope: cursor.stream_scope,
    scope_id: cursor.scope_id,
    epoch: String(cursor.epoch),
    sequence: String(cursor.sequence),
  })
  const controller = new AbortController()
  const response = await fetch(`${origin}/api/v1/events?${query}`, {
    headers: { Authorization: `Bearer ${bearer}` },
    signal: AbortSignal.any([controller.signal, AbortSignal.timeout(5_000)]),
  })
  if (!response.ok || response.body === null) throw new Error(`SSE request failed with status ${response.status}`)
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffered = ''
  try {
    while (buffered.length <= 64 * 1024) {
      const chunk = await reader.read()
      if (chunk.done) break
      buffered += decoder.decode(chunk.value, { stream: true })
      for (const block of buffered.split('\n\n')) {
        const event = block.match(/^event: (.+)$/m)?.[1]
        const data = block.match(/^data: (.+)$/m)?.[1]
        if (event && data) return { event, data: JSON.parse(data) as Record<string, unknown> }
      }
    }
    throw new Error('SSE stream did not emit one bounded event')
  } finally {
    controller.abort()
    await reader.cancel().catch(() => undefined)
  }
}

function productionFrontendFiles(directory: string): string[] {
  const files: string[] = []
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) files.push(...productionFrontendFiles(path))
    else if (/\.(?:css|ts|tsx)$/.test(entry.name) && !/\.test\.[^.]+$/.test(entry.name)) files.push(path)
  }
  return files
}

let temporaryRoot = ''
let project = ''
let binary = ''
let childEnvironment: NodeJS.ProcessEnv
let cliOverview: { entrypoints: string[] }

type Startup = {
  handoff: string
  origin: string
  stdout: string
  stderr: string
}

function waitForStartup(process: ChildProcessWithoutNullStreams): Promise<Startup> {
  return new Promise((resolveStartup, rejectStartup) => {
    let stdout = ''
    let stderr = ''
    const timeout = setTimeout(() => finish(new Error(`workbench did not publish startup details:\n${stdout}\n${stderr}`)), 10_000)

    const onStdout = (chunk: Buffer) => {
      stdout += chunk.toString('utf8')
      check()
    }
    const onStderr = (chunk: Buffer) => {
      stderr += chunk.toString('utf8')
      check()
    }
    const onExit = (code: number | null) => finish(new Error(`workbench exited before startup (code ${code}): ${stderr.trim()}`))
    const finish = (error?: Error, startup?: Startup) => {
      clearTimeout(timeout)
      process.stdout.off('data', onStdout)
      process.stderr.off('data', onStderr)
      process.off('exit', onExit)
      if (error) rejectStartup(error)
      else if (startup) resolveStartup(startup)
    }
    const check = () => {
      const origin = stdout.match(/^Prowl Workbench: (http:\/\/127\.0\.0\.1:\d+)\/$/m)?.[1]
      const handoff = stderr.match(/^Prowl Workbench bootstrap handoff: (.+)$/m)?.[1]
      if (origin && handoff) finish(undefined, { origin, handoff, stdout, stderr })
    }

    process.stdout.on('data', onStdout)
    process.stderr.on('data', onStderr)
    process.once('exit', onExit)
  })
}

type BoundedProcessOutput = {
  snapshot: () => { stdout: string; stderr: string; truncated: boolean }
  stop: () => void
}

function collectBoundedProcessOutput(process: ChildProcessWithoutNullStreams, limit = 64 * 1024): BoundedProcessOutput {
  let stdout = ''
  let stderr = ''
  let truncated = false
  const append = (current: string, chunk: Buffer) => {
    const next = current + chunk.toString('utf8')
    if (next.length <= limit) return next
    truncated = true
    return next.slice(0, limit)
  }
  const onStdout = (chunk: Buffer) => {
    stdout = append(stdout, chunk)
  }
  const onStderr = (chunk: Buffer) => {
    stderr = append(stderr, chunk)
  }
  process.stdout.on('data', onStdout)
  process.stderr.on('data', onStderr)
  return {
    snapshot: () => ({ stdout, stderr, truncated }),
    stop: () => {
      process.stdout.off('data', onStdout)
      process.stderr.off('data', onStderr)
    },
  }
}

test.beforeAll(() => {
  temporaryRoot = mkdtempSync(join(tmpdir(), 'prowl-workbench-e2e-'))
  project = join(temporaryRoot, 'go-auth-service')
  binary = join(temporaryRoot, 'prowl-agent-workbench-e2e')
  childEnvironment = {
    ...process.env,
    XDG_CACHE_HOME: join(temporaryRoot, 'cache'),
    XDG_CONFIG_HOME: join(temporaryRoot, 'config'),
    XDG_STATE_HOME: join(temporaryRoot, 'state'),
  }
  for (const fixtureName of fixtureNames) {
    const fixtureProject = join(temporaryRoot, fixtureName)
    cpSync(join(repository, 'testdata', 'workbench', fixtureName), fixtureProject, { recursive: true })
    fixtureProjects.set(fixtureName, fixtureProject)
  }
  project = fixtureProjects.get('go-auth-service')!

  const build = spawnSync('go', ['build', '-tags', 'sqlite_fts5', '-o', binary, './cmd/prowl-agent'], {
    cwd: repository,
    encoding: 'utf8',
  })
  if (build.status !== 0) throw new Error(`Go build failed:\n${build.stdout}\n${build.stderr}`)

  // The first initialization writes Prowl's ignored metadata rule after the
  // index transaction; repeat it so every fixture starts from a current snapshot.
  for (const fixtureName of fixtureNames) {
    const fixtureProject = fixtureProjects.get(fixtureName)!
    for (let pass = 1; pass <= 2; pass += 1) {
      const init = spawnSync(binary, ['init', '--no-ai', '--no-input'], {
        cwd: fixtureProject,
        encoding: 'utf8',
        env: childEnvironment,
      })
      if (init.status !== 0) throw new Error(`Prowl ${fixtureName} initialization pass ${pass} failed:\n${init.stdout}\n${init.stderr}`)
    }
  }

  const overview = spawnSync(binary, ['overview', '--json'], {
    cwd: project,
    encoding: 'utf8',
    env: childEnvironment,
  })
  if (overview.status !== 0) throw new Error(`Prowl overview failed:\n${overview.stdout}\n${overview.stderr}`)
  cliOverview = JSON.parse(overview.stdout) as { entrypoints: string[] }
})

test.afterAll(() => {
  if (temporaryRoot) rmSync(temporaryRoot, { force: true, recursive: true })
})

test('brief vertical slice: compiled workbench is secure, accessible, and source-backed', async ({ page, request }) => {
  const process = spawn(binary, ['open', '--no-browser', '--port', '0'], {
    cwd: project,
    stdio: ['pipe', 'pipe', 'pipe'],
    env: childEnvironment,
  })
  let stopped = false
  let handoff = ''

  try {
    const startup = await waitForStartup(process)
    handoff = startup.handoff
    expect(startup.stdout).not.toContain('nonce=')
    expect(startup.stdout).not.toContain('#')
    expect(statSync(handoff).mode & 0o777).toBe(0o600)

    const launchURL = readFileSync(handoff, 'utf8').trim()
    const parsed = new URL(launchURL)
    const nonce = new URLSearchParams(parsed.hash.slice(1)).get('nonce')
    expect(parsed.origin).toBe(startup.origin)
    expect(nonce).toMatch(/^[A-Za-z0-9_-]{43}$/)

    const unauthenticated = await request.get(`${parsed.origin}/api/v1/brief`)
    expect(unauthenticated.status()).toBe(401)

    const foreignHost = await request.get(`${parsed.origin}/api/v1/brief`, {
      headers: { Host: '127.0.0.1:1' },
    })
    expect(foreignHost.status()).toBe(403)

    const foreignFetchSite = await request.get(`${parsed.origin}/api/v1/brief`, {
      headers: { 'Sec-Fetch-Site': 'cross-site' },
    })
    expect(foreignFetchSite.status()).toBe(403)

    const foreignOrigin = await request.post(`${parsed.origin}/api/v1/auth/bootstrap`, {
      data: { nonce },
      headers: { Origin: 'https://attacker.example' },
    })
    expect(foreignOrigin.status()).toBe(403)

    await page.emulateMedia({ reducedMotion: 'reduce' })
    const briefResponse = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/v1/brief' && response.status() === 200)
    await page.goto(launchURL)
    const apiBrief = await (await briefResponse).json() as { data: { overview: { entrypoints: string[] } } }

    await expect(page.getByRole('heading', { name: 'go-auth-service' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Start with evidence' })).toBeVisible()
    await expect(page.getByText(sourceAnchor)).toBeVisible()
    expect(cliOverview.entrypoints).toContain(sourceAnchor)
    expect(apiBrief.data.overview.entrypoints).toContain(sourceAnchor)
    await expect(page).toHaveURL(`${parsed.origin}/`)

    expect(await page.evaluate(() => ({
      cookie: document.cookie,
      hash: location.hash,
      history: history.state,
      local: localStorage.length,
      query: location.search,
      session: sessionStorage.length,
    }))).toEqual({ cookie: '', hash: '', history: null, local: 0, query: '', session: 0 })
    expect(await page.context().cookies()).toEqual([])

    const replay = await request.post(`${parsed.origin}/api/v1/auth/bootstrap`, {
      data: { nonce },
      headers: { Origin: parsed.origin },
    })
    expect(replay.status()).toBe(401)

    await page.keyboard.press('Tab')
    await expect(page.getByRole('link', { name: 'Skip to content' })).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(page.locator('#main-content')).toBeFocused()

    const accessibility = await new AxeBuilder({ page }).analyze()
    expect(accessibility.violations).toEqual([])
    expect(accessibility.incomplete.filter((result) => result.impact === 'serious')).toEqual([])

    const privacyContrast = await page.locator('.privacy-note').evaluate((element) => {
      const channel = (value: number) => {
        const normalized = value / 255
        return normalized <= 0.04045 ? normalized / 12.92 : ((normalized + 0.055) / 1.055) ** 2.4
      }
      const luminance = (value: string) => {
        const channels = value.match(/[\d.]+/g)?.slice(0, 3).map(Number)
        if (!channels || channels.length !== 3) throw new Error(`cannot parse color ${value}`)
        return 0.2126 * channel(channels[0]) + 0.7152 * channel(channels[1]) + 0.0722 * channel(channels[2])
      }
      const foreground = luminance(getComputedStyle(element).color)
      const background = luminance(getComputedStyle(element.parentElement!).backgroundColor)
      return (Math.max(foreground, background) + 0.05) / (Math.min(foreground, background) + 0.05)
    })
    expect(privacyContrast).toBeGreaterThanOrEqual(4.5)

    await page.reload()
    await expect(page.getByRole('alert')).toHaveText('Secure workbench session unavailable. Reopen Prowl from your terminal.')

    process.kill('SIGTERM')
    const [exitCode] = await once(process, 'exit')
    expect(exitCode).toBe(0)
    expect(existsSync(handoff)).toBe(false)
    stopped = true
  } finally {
    if (!stopped && process.exitCode === null && process.signalCode === null) {
      process.kill('SIGTERM')
      await once(process, 'exit')
    }
  }
})

test('expired nonce: compiled workbench rejects expiry without process disclosure', async ({ request }) => {
  test.setTimeout(75_000)
  const process = spawn(binary, ['open', '--no-browser', '--port', '0'], {
    cwd: project,
    stdio: ['pipe', 'pipe', 'pipe'],
    env: childEnvironment,
  })
  const output = collectBoundedProcessOutput(process)
  let stopped = false
  try {
    const startup = await waitForStartup(process)
    const launchURL = readFileSync(startup.handoff, 'utf8').trim()
    const nonce = new URLSearchParams(new URL(launchURL).hash.slice(1)).get('nonce')
    expect(nonce !== null && /^[A-Za-z0-9_-]{43}$/.test(nonce)).toBe(true)
    if (nonce === null) throw new Error('bootstrap handoff omitted nonce')
    const processMetadata = Buffer.concat([
      readFileSync(`/proc/${process.pid}/cmdline`),
      readFileSync(`/proc/${process.pid}/environ`),
    ]).toString('utf8')
    expect(processMetadata.includes(nonce)).toBe(false)

    await delay(61_000)
    const expired = await request.post(`${startup.origin}/api/v1/auth/bootstrap`, {
      data: { nonce },
      headers: { Origin: startup.origin },
    })
    expect(expired.status()).toBe(401)

    process.kill('SIGTERM')
    await once(process, 'close')
    stopped = true
    const captured = output.snapshot()
    expect(captured.truncated).toBe(false)
    expect(captured.stdout.includes(nonce)).toBe(false)
    expect(captured.stderr.includes(nonce)).toBe(false)
  } finally {
    if (!stopped && process.exitCode === null && process.signalCode === null) {
      process.kill('SIGTERM')
      await once(process, 'close')
    }
    output.stop()
  }
})

for (const fixtureName of fixtureNames) {
  test(`fixture journey: ${fixtureName} maps every real screen and rooted guided step`, async ({ page }) => {
    const fixtureProject = fixtureProjects.get(fixtureName)!
    test.setTimeout(90_000)
    const process = spawn(binary, ['open', '--no-browser', '--port', '0'], {
      cwd: fixtureProject,
      stdio: ['pipe', 'pipe', 'pipe'],
      env: childEnvironment,
    })
    const observedRoutes = new Set<string>()
    page.on('request', (request) => {
      const route = inventoryRoute(request.method(), request.url())
      if (route !== null) observedRoutes.add(route)
    })
    try {
      const startup = await waitForStartup(process)
      const briefResponse = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/v1/brief' && response.status() === 200)
      await page.goto(readFileSync(startup.handoff, 'utf8').trim())
      const brief = await (await briefResponse).json() as { data: { workspace: { name: string } }; meta: { resource_version: string } }
      expect(brief.data.workspace.name).toBe(fixtureName)
      expect(brief.meta.resource_version).toMatch(/^[a-f0-9]{1,16}$/)
      await expect(page.getByRole('heading', { name: fixtureName })).toBeVisible()

      const exploreResponse = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/v1/explore' && response.status() === 200)
      await page.getByRole('link', { name: 'Explore' }).click()
      const explore = await (await exploreResponse).json() as { data: { workspace: { name: string } }; meta: { resource_version: string } }
      expect(explore.data.workspace.name).toBe(fixtureName)
      expect(explore.meta.resource_version).toBe(brief.meta.resource_version)

      const tourResponse = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/v1/tours/onboarding' && response.status() === 200)
      await page.getByRole('link', { name: /Start with the project/ }).click()
      const tour = await (await tourResponse).json() as { data: { steps: unknown[] }; meta: { resource_version: string } }
      expect(tour.meta.resource_version).toBe(brief.meta.resource_version)

      const steps = page.locator('.tour-steps > li')
      await expect(steps.first()).toBeVisible()
      const stepCount = await steps.count()
      expect(stepCount).toBeGreaterThanOrEqual(5)
      expect(stepCount).toBeLessThanOrEqual(12)
      expect(tour.data.steps).toHaveLength(stepCount)

      const anchorHrefs: string[] = []
      for (let index = 0; index < stepCount; index += 1) {
        const anchors = steps.nth(index).getByRole('link')
        expect(await anchors.count(), `guided step ${index + 1} must be rooted in source evidence`).toBeGreaterThan(0)
        for (let anchor = 0; anchor < await anchors.count(); anchor += 1) {
          await expect(anchors.nth(anchor)).toBeVisible()
          const href = await anchors.nth(anchor).getAttribute('href')
          expect(href).toMatch(/^#\/source\?/)
          anchorHrefs.push(href!)
        }
      }

      for (const href of anchorHrefs) {
        const sourceResponse = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/v1/source' && response.status() === 200)
        await page.evaluate((target) => { window.location.hash = target.slice(1) }, href)
        const source = await (await sourceResponse).json() as { data: { path: string; lines: unknown[] }; meta: { resource_version: string } }
        const expectedPath = new URLSearchParams(href.slice(href.indexOf('?') + 1)).get('path')
        expect(source.data.path).toBe(expectedPath)
        expect(source.data.lines.length).toBeGreaterThan(0)
        expect(source.meta.resource_version).toBe(brief.meta.resource_version)
        await expect(page.getByRole('heading', { name: 'Source preview' })).toBeVisible()
        await expect(page.getByRole('alert')).toHaveCount(0)
        await expect(page.getByLabel(/^Source preview for /)).toBeVisible()
      }

      const primary = page.getByRole('navigation', { name: 'Primary' })
      const contextResponse = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/v1/context/search' && response.request().method() === 'POST' && response.status() === 200)
      await primary.getByRole('link', { name: 'Context Lens' }).click()
      const question = contextQuestionByFixture[fixtureName]
      await page.getByLabel('Question').fill(question)
      await page.getByRole('button', { name: 'Search context' }).click()
      const context = await (await contextResponse).json() as { data: { question: string; items: Array<{ id: string }> }; meta: { resource_version: string } }
      expect(context.data.question).toBe(question)
      expect(context.data.items.length).toBeGreaterThan(0)
      expect(context.meta.resource_version).toBe(brief.meta.resource_version)
      await expect(page.getByRole('heading', { name: 'Source-backed context' })).toBeVisible()

      const selectedContextResponse = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/v1/context/get' && response.request().method() === 'POST' && response.status() === 200)
      const selected = new URLSearchParams({ ids: context.data.items[0].id })
      await page.evaluate((target) => { window.location.hash = target }, `#/context?${selected}`)
      const selectedContext = await (await selectedContextResponse).json() as { data: { items: unknown[] }; meta: { resource_version: string } }
      expect(selectedContext.data.items.length).toBeGreaterThan(0)
      expect(selectedContext.meta.resource_version).toBe(brief.meta.resource_version)
      await expect(page.getByRole('heading', { name: 'Selected context' })).toBeVisible()

      const sourcePath = new URLSearchParams(anchorHrefs[0].slice(anchorHrefs[0].indexOf('?') + 1)).get('path')!
      const impactResponse = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/v1/impact' && response.request().method() === 'POST' && response.status() === 200)
      await primary.getByRole('link', { name: 'Impact' }).click()
      await page.getByLabel('Source path').fill(sourcePath)
      await page.getByRole('button', { name: 'Inspect impact' }).click()
      const impact = await (await impactResponse).json() as { data: { path: string }; meta: { resource_version: string } }
      expect(impact.data.path).toBe(sourcePath)
      expect(impact.meta.resource_version).toBe(brief.meta.resource_version)
      await expect(page.getByRole('heading', { name: `Impact: ${sourcePath}` })).toBeVisible()

      const knowledgeResponse = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/v1/knowledge' && response.status() === 200)
      await primary.getByRole('link', { name: 'Knowledge' }).click()
      const knowledge = await (await knowledgeResponse).json() as { data: { items: unknown[] }; meta: { resource_version: string } }
      expect(Array.isArray(knowledge.data.items)).toBe(true)
      expect(knowledge.meta.resource_version).toBe(brief.meta.resource_version)
      await expect(page.getByRole('heading', { name: 'Knowledge' })).toBeVisible()

      const timelineResponse = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/v1/timeline' && response.status() === 200)
      await primary.getByRole('link', { name: 'Timeline' }).click()
      const timeline = await (await timelineResponse).json() as { data: { events: unknown[] }; meta: { resource_version: string } }
      expect(Array.isArray(timeline.data.events)).toBe(true)
      expect(timeline.meta.resource_version).toBe(brief.meta.resource_version)
      await expect(page.getByRole('heading', { name: 'Timeline' })).toBeVisible()

      const setupResponse = page.waitForResponse((response) => new URL(response.url()).pathname === '/api/v1/setup/detect' && response.status() === 200)
      await primary.getByRole('link', { name: 'Setup' }).click()
      const setup = await (await setupResponse).json() as { data: { integrations: unknown[] }; meta: { resource_version: string } }
      expect(Array.isArray(setup.data.integrations)).toBe(true)
      expect(setup.meta.resource_version).toMatch(/^[a-f0-9]{64}$/)
      await expect(page.getByRole('heading', { name: 'Setup' })).toBeVisible()

      for (const required of requiredScreenRoutes) {
        expect(observedRoutes.has(required), `${fixtureName} did not exercise inventory route ${required}`).toBe(true)
      }
    } finally {
      if (process.exitCode === null && process.signalCode === null) {
        process.kill('SIGTERM')
        await once(process, 'exit')
      }
    }
  })
}

test('production frontend quality: executable sources contain no placeholder selectors', () => {
  const findings = productionFrontendFiles(join(repository, 'web', 'src')).flatMap((file) => {
    const source = readFileSync(file, 'utf8')
    return /placeholder|TODO metric|Math\.random/i.test(source) ? [file.slice(repository.length + 1)] : []
  })
  expect(findings).toEqual([])
})

test('events jobs and offline export: compiled workbench snapshot renders without networking', async ({ page, request }) => {
  const process = spawn(binary, ['open', '--no-browser', '--port', '0'], {
    cwd: project,
    stdio: ['pipe', 'pipe', 'pipe'],
    env: childEnvironment,
  })
  let stopped = false
  try {
    const startup = await waitForStartup(process)
    const launchURL = readFileSync(startup.handoff, 'utf8').trim()
    const nonce = new URLSearchParams(new URL(launchURL).hash.slice(1)).get('nonce')
    expect(nonce !== null && /^[A-Za-z0-9_-]{43}$/.test(nonce)).toBe(true)
    if (nonce === null) throw new Error('bootstrap handoff omitted nonce')
    const bootstrap = await request.post(`${startup.origin}/api/v1/auth/bootstrap`, {
      data: { nonce },
      headers: { Origin: startup.origin },
    })
    expect(bootstrap.status()).toBe(200)
    const payload = await bootstrap.json() as { bearer: string }
    expect(/^[A-Za-z0-9_-]{43}$/.test(payload.bearer)).toBe(true)

    const exported = await request.post(`${startup.origin}/api/v1/export`, {
      headers: { Authorization: `Bearer ${payload.bearer}` },
    })
    expect(exported.status()).toBe(200)
    expect(exported.headers()['content-type']).toBe('text/html; charset=utf-8')
    const html = await exported.text()
    for (const forbidden of [payload.bearer, nonce, '/api/v1/', 'Bearer ', temporaryRoot, project]) {
      expect(html.includes(forbidden)).toBe(false)
    }
    expect(html).toContain("default-src 'none'")
    expect(html).toContain("connect-src 'none'")
    expect(html).toContain("script-src 'none'")

    const cancellationLoad = join(project, 'internal', 'd2-cancellation-load')
    mkdirSync(cancellationLoad, { recursive: true })
    for (let index = 0; index < 500; index += 1) {
      writeFileSync(join(cancellationLoad, `source-${String(index).padStart(3, '0')}.go`), `package cancellation\n\nfunc Work${index}() int { return ${index} }\n`)
    }
    const refreshed = await request.post(`${startup.origin}/api/v1/index/refresh`, {
      headers: { Authorization: `Bearer ${payload.bearer}` },
    })
    expect(refreshed.status()).toBe(202)
    const refreshEnvelope = await refreshed.json() as {
      data: { id: string; status: string; version: number; stream: EventCursor }
    }
    expect(refreshEnvelope.data.status).toBe('queued')

    let cancellationRequested = false
    for (let attempt = 0; attempt < 8; attempt += 1) {
      const currentJob = await request.get(`${startup.origin}/api/v1/jobs/${refreshEnvelope.data.id}`, {
        headers: { Authorization: `Bearer ${payload.bearer}` },
      })
      expect(currentJob.status()).toBe(200)
      const currentEnvelope = await currentJob.json() as { data: { status: string; version: number } }
      if (currentEnvelope.data.status === 'cancelled') {
        cancellationRequested = true
        break
      }
      expect(['queued', 'running', 'cancelling']).toContain(currentEnvelope.data.status)
      if (currentEnvelope.data.status === 'cancelling') {
        cancellationRequested = true
        break
      }
      const cancelled = await request.post(`${startup.origin}/api/v1/jobs/${refreshEnvelope.data.id}/cancel`, {
        data: { expected_version: currentEnvelope.data.version, idempotency_key: `d2-compiled-cancellation-${attempt}` },
        headers: { Authorization: `Bearer ${payload.bearer}` },
      })
      if (cancelled.status() === 200) {
        const cancelEnvelope = await cancelled.json() as { data: { status: string } }
        expect(['cancelling', 'cancelled']).toContain(cancelEnvelope.data.status)
        cancellationRequested = true
        break
      }
      expect(cancelled.status()).toBe(409)
    }
    expect(cancellationRequested).toBe(true)
    await expect.poll(async () => {
      const currentJob = await request.get(`${startup.origin}/api/v1/jobs/${refreshEnvelope.data.id}`, {
        headers: { Authorization: `Bearer ${payload.bearer}` },
      })
      if (currentJob.status() !== 200) return `http-${currentJob.status()}`
      const currentEnvelope = await currentJob.json() as { data: { status: string } }
      return currentEnvelope.data.status
    }, {
      message: 'index refresh job reaches terminal cancelled state',
      timeout: 15_000,
      intervals: [50, 100, 250, 500],
    }).toBe('cancelled')

    const replay = await readSSEMessage(startup.origin, payload.bearer, {
      ...refreshEnvelope.data.stream,
      sequence: 0,
    })
    expect(replay.event).toBe('project-job.changed')
    const replayCursor = replay.data.cursor as EventCursor
    expect(replayCursor.stream_scope).toBe('project-job')
    expect(replayCursor.scope_id).toBe(refreshEnvelope.data.stream.scope_id)
    expect(replayCursor.epoch).toBe(refreshEnvelope.data.stream.epoch)
    expect(replayCursor.sequence).toBeGreaterThan(0)

    const resumed = await readSSEMessage(startup.origin, payload.bearer, replayCursor)
    expect(resumed.event).toBe('project-job.changed')
    const resumedCursor = resumed.data.cursor as EventCursor
    expect(resumedCursor.scope_id).toBe(replayCursor.scope_id)
    expect(resumedCursor.epoch).toBe(replayCursor.epoch)
    expect(resumedCursor.sequence).toBeGreaterThan(replayCursor.sequence)

    const reset = await readSSEMessage(startup.origin, payload.bearer, {
      ...replayCursor,
      scope_id: '0'.repeat(64),
      sequence: 0,
    })
    expect(reset.event).toBe('reset')
    expect((reset.data.cursor as EventCursor).scope_id).toBe(refreshEnvelope.data.stream.scope_id)
    expect(reset.data.snapshot_uri).toBe(`snapshot://${refreshEnvelope.data.stream.scope_id}`)


    await page.route('**/*', (route) => route.abort())
    await page.setContent(html)
    await expect(page.getByRole('heading', { name: 'go-auth-service' })).toBeVisible()
    await expect(page.getByText(sourceAnchor)).toBeVisible()
    const csp = await page.locator('meta[http-equiv="Content-Security-Policy"]').getAttribute('content')
    expect(csp).toContain("connect-src 'none'")
    expect(await page.evaluate(() => performance.getEntriesByType('resource').length)).toBe(0)

    process.kill('SIGTERM')
    const [exitCode] = await once(process, 'exit')
    expect(exitCode).toBe(0)
    stopped = true
  } finally {
    if (!stopped && process.exitCode === null && process.signalCode === null) {
      process.kill('SIGTERM')
      await once(process, 'exit')
    }
  }
})
