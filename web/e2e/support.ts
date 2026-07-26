import { spawn, spawnSync, type ChildProcessWithoutNullStreams } from 'node:child_process'
import { cpSync, mkdtempSync, readFileSync, rmSync } from 'node:fs'
import { once } from 'node:events'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import type { Page } from '@playwright/test'

export const repository = resolve(import.meta.dirname, '..', '..')

export type Startup = {
  handoff: string
  origin: string
}

export type FixtureWorkspace = {
  root: string
  project: string
  binary: string
  environment: NodeJS.ProcessEnv
}

export function prepareFixture(name: string): FixtureWorkspace {
  const root = mkdtempSync(join(tmpdir(), `prowl-workbench-${name}-`))
  const project = join(root, name)
  const binary = join(root, 'prowl-agent-workbench-e2e')
  const environment = {
    ...process.env,
    XDG_CACHE_HOME: join(root, 'cache'),
    XDG_CONFIG_HOME: join(root, 'config'),
    XDG_STATE_HOME: join(root, 'state'),
  }
  cpSync(join(repository, 'testdata', 'workbench', name), project, { recursive: true })

  const build = spawnSync('go', ['build', '-tags', 'sqlite_fts5', '-o', binary, './cmd/prowl-agent'], {
    cwd: repository,
    encoding: 'utf8',
  })
  if (build.status !== 0) throw new Error(`Go build failed:\n${build.stdout}\n${build.stderr}`)

  // The metadata rule is committed after the first transaction. A second pass
  // gives every real fixture a current source-backed projection.
  for (let pass = 1; pass <= 2; pass += 1) {
    const init = spawnSync(binary, ['init', '--no-ai', '--no-input'], {
      cwd: project,
      encoding: 'utf8',
      env: environment,
    })
    if (init.status !== 0) throw new Error(`Prowl initialization pass ${pass} failed:\n${init.stdout}\n${init.stderr}`)
  }

  return { root, project, binary, environment }
}

export function removeFixture(workspace: FixtureWorkspace) {
  rmSync(workspace.root, { force: true, recursive: true })
}

export function startWorkbench(workspace: FixtureWorkspace): { process: ChildProcessWithoutNullStreams; startup: Promise<Startup> } {
  const process = spawn(workspace.binary, ['open', '--no-browser', '--port', '0'], {
    cwd: workspace.project,
    stdio: ['pipe', 'pipe', 'pipe'],
    env: workspace.environment,
  })
  return { process, startup: waitForStartup(process) }
}

export async function stopWorkbench(process: ChildProcessWithoutNullStreams) {
  if (process.exitCode !== null || process.signalCode !== null) return
  process.kill('SIGTERM')
  await once(process, 'exit')
}

export function readBrowserLaunchURL(startup: Startup): string {
  const launchURL = readFileSync(startup.handoff, 'utf8').trim()
  const parsed = new URL(launchURL)
  const nonce = new URLSearchParams(parsed.hash.slice(1)).get('nonce')
  if (!nonce) throw new Error('workbench launch handoff omitted bootstrap nonce')
  return launchURL
}

export async function openAuthenticatedWorkbench(page: Page, startup: Startup): Promise<{
  bearerHeader: boolean
  bearerAbsentFromURL: boolean
  cleanBrowserLocation: boolean
}> {
  const briefRequest = page.waitForRequest((request) => new URL(request.url()).pathname === '/api/v1/brief')
  await page.goto(readBrowserLaunchURL(startup))
  const request = await briefRequest
  const authorization = request.headers().authorization ?? ''
  const bearer = authorization.startsWith('Bearer ') ? authorization.slice('Bearer '.length) : ''
  const browserLocation = new URL(page.url())
  return {
    bearerHeader: /^[A-Za-z0-9_-]{43}$/.test(bearer),
    bearerAbsentFromURL: bearer !== '' && !request.url().includes(bearer) && !page.url().includes(bearer),
    cleanBrowserLocation: browserLocation.origin === startup.origin
      && browserLocation.pathname === '/'
      && browserLocation.search === ''
      && browserLocation.hash === '',
  }
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
      if (origin && handoff) finish(undefined, { origin, handoff })
    }

    process.stdout.on('data', onStdout)
    process.stderr.on('data', onStderr)
    process.once('exit', onExit)
  })
}
