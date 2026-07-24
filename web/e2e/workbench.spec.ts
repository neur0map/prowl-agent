import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { spawn, spawnSync, type ChildProcessWithoutNullStreams } from 'node:child_process'
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { once } from 'node:events'
import { tmpdir } from 'node:os'
import { createInterface } from 'node:readline'
import { join, resolve } from 'node:path'

const repository = resolve(import.meta.dirname, '..', '..')
let temporaryRoot = ''
let project = ''
let binary = ''
let childEnvironment: NodeJS.ProcessEnv

function readLine(process: ChildProcessWithoutNullStreams): Promise<string> {
  const output = createInterface({ input: process.stdout, crlfDelay: Infinity })
  const controller = new AbortController()
  let stderr = ''
  const onStderr = (chunk: Buffer) => {
    stderr += chunk.toString('utf8')
  }
  process.stderr.on('data', onStderr)

  const firstLine = once(output, 'line', { signal: controller.signal }).then(([line]) => {
    if (typeof line !== 'string') throw new Error('workbench emitted a non-text startup line')
    return line
  })
  const exit = once(process, 'exit', { signal: controller.signal }).then(([code]) => {
    throw new Error(`workbench exited before startup (code ${code}): ${stderr.trim()}`)
  })

  return Promise.race([firstLine, exit]).finally(() => {
    controller.abort()
    output.close()
    process.stderr.off('data', onStderr)
  })
}

test.beforeAll(() => {
  temporaryRoot = mkdtempSync(join(tmpdir(), 'prowl-workbench-e2e-'))
  project = join(temporaryRoot, 'project')
  binary = join(temporaryRoot, 'prowl-agent-workbench-e2e')
  childEnvironment = {
    ...process.env,
    XDG_CACHE_HOME: join(temporaryRoot, 'cache'),
    XDG_CONFIG_HOME: join(temporaryRoot, 'config'),
    XDG_STATE_HOME: join(temporaryRoot, 'state'),
  }
  mkdirSync(project)
  writeFileSync(join(project, 'README.md'), '# Workbench test project\n')

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
})

test.afterAll(() => {
  if (temporaryRoot) rmSync(temporaryRoot, { force: true, recursive: true })
})

test('compiled workbench is secure, accessible, and usable without network services', async ({ page, request }) => {
  const process = spawn(binary, ['open', '--no-browser', '--port', '0'], {
    cwd: project,
    stdio: ['pipe', 'pipe', 'pipe'],
    env: childEnvironment,
  })
  try {
    const startup = await readLine(process)
    const launchURL = startup.replace(/^Prowl Workbench: /, '')
    const parsed = new URL(launchURL)
    const token = new URLSearchParams(parsed.hash.slice(1)).get('token')
    expect(token).toMatch(/^[A-Za-z0-9_-]{43}$/)

    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto(launchURL)
    await expect(page.getByRole('heading', { name: 'Prowl Workbench' })).toBeVisible()
    await expect(page.getByRole('navigation', { name: 'Primary' })).toBeVisible()
    await expect(page).toHaveURL(`${parsed.origin}/`)

    expect(await page.evaluate(() => ({
      cookie: document.cookie,
      local: localStorage.length,
      query: location.search,
      session: sessionStorage.length,
    }))).toEqual({ cookie: '', local: 0, query: '', session: 0 })
    expect(await page.context().cookies()).toEqual([])

    await page.keyboard.press('Tab')
    await expect(page.getByRole('link', { name: 'Skip to content' })).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(page.locator('#main-content')).toBeFocused()

    const health = await request.get(`${parsed.origin}/api/v1/health`, {
      headers: { Authorization: `Bearer ${token}` },
    })
    expect(health.status()).toBe(200)
    const healthBody = await health.json()
    expect(healthBody.data).toEqual({ api_version: 'v1', status: 'ok' })
    expect(healthBody.meta).toMatchObject({
      request_id: expect.stringMatching(/\S+/),
      resource_version: expect.stringMatching(/\S+/),
    })

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
  } finally {
    process.kill('SIGTERM')
    if (process.exitCode === null) await once(process, 'exit')
  }
})
