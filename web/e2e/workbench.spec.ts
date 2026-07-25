import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { spawn, spawnSync, type ChildProcessWithoutNullStreams } from 'node:child_process'
import { cpSync, existsSync, mkdtempSync, readFileSync, rmSync, statSync } from 'node:fs'
import { once } from 'node:events'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

const repository = resolve(import.meta.dirname, '..', '..')
const fixture = join(repository, 'testdata', 'workbench', 'go-auth-service')
const sourceAnchor = 'cmd/server/main.go'
let temporaryRoot = ''
let project = ''
let binary = ''
let childEnvironment: NodeJS.ProcessEnv
let cliOverview: { entrypoints: string[] }

type Startup = {
  handoff: string
  origin: string
  stdout: string
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
      if (origin && handoff) finish(undefined, { origin, handoff, stdout })
    }

    process.stdout.on('data', onStdout)
    process.stderr.on('data', onStderr)
    process.once('exit', onExit)
  })
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
  cpSync(fixture, project, { recursive: true })

  const build = spawnSync('go', ['build', '-tags', 'sqlite_fts5', '-o', binary, './cmd/prowl-agent'], {
    cwd: repository,
    encoding: 'utf8',
  })
  if (build.status !== 0) throw new Error(`Go build failed:\n${build.stdout}\n${build.stderr}`)

  // The first initialization writes Prowl's ignored metadata rule after the
  // index transaction; repeat it so the fixture starts from a current snapshot.
  for (let pass = 1; pass <= 2; pass += 1) {
    const init = spawnSync(binary, ['init', '--no-ai', '--no-input'], {
      cwd: project,
      encoding: 'utf8',
      env: childEnvironment,
    })
    if (init.status !== 0) throw new Error(`Prowl initialization pass ${pass} failed:\n${init.stdout}\n${init.stderr}`)
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
    expect(nonce).toMatch(/^[A-Za-z0-9_-]{43}$/)
    const bootstrap = await request.post(`${startup.origin}/api/v1/auth/bootstrap`, {
      data: { nonce },
      headers: { Origin: startup.origin },
    })
    expect(bootstrap.status()).toBe(200)
    const payload = await bootstrap.json() as { bearer: string }
    expect(payload.bearer).toMatch(/^[A-Za-z0-9_-]{43}$/)

    const exported = await request.post(`${startup.origin}/api/v1/export`, {
      headers: { Authorization: `Bearer ${payload.bearer}` },
    })
    expect(exported.status()).toBe(200)
    expect(exported.headers()['content-type']).toBe('text/html; charset=utf-8')
    const html = await exported.text()
    for (const forbidden of [payload.bearer, nonce!, '/api/v1/', 'Bearer ', temporaryRoot, project]) {
      expect(html).not.toContain(forbidden)
    }
    expect(html).toContain("default-src 'none'")
    expect(html).toContain("connect-src 'none'")
    expect(html).toContain("script-src 'none'")

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
