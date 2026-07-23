import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'
import { spawn, spawnSync, type ChildProcessWithoutNullStreams } from 'node:child_process'
import { mkdirSync } from 'node:fs'
import { resolve } from 'node:path'
import { once } from 'node:events'

const repository = resolve(import.meta.dirname, '..', '..')
const binaryDir = resolve(repository, '.tmp')
const binary = resolve(binaryDir, 'prowl-agent-workbench-e2e')

function readLine(process: ChildProcessWithoutNullStreams): Promise<string> {
  return new Promise((resolveLine, reject) => {
    let buffer = ''
    const onData = (chunk: Buffer) => {
      buffer += chunk.toString('utf8')
      const newline = buffer.indexOf('\n')
      if (newline < 0) return
      process.stdout.off('data', onData)
      resolveLine(buffer.slice(0, newline))
    }
    process.stdout.on('data', onData)
    process.once('error', reject)
    process.once('exit', (code) => reject(new Error(`workbench exited before startup (code ${code})`)))
  })
}

test.beforeAll(() => {
  mkdirSync(binaryDir, { recursive: true })
  const result = spawnSync('go', ['build', '-tags', 'sqlite_fts5', '-o', binary, './cmd/prowl-agent'], {
    cwd: repository,
    encoding: 'utf8',
  })
  if (result.status !== 0) throw new Error(`Go build failed:\n${result.stdout}\n${result.stderr}`)
})

test('compiled workbench is secure, accessible, and usable without network services', async ({ page, request }) => {
  const process = spawn(binary, ['open', '--no-browser', '--port', '0'], {
    cwd: repository,
    stdio: ['pipe', 'pipe', 'pipe'],
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
    expect(await health.json()).toEqual({ api_version: 'v1', status: 'ok' })

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
