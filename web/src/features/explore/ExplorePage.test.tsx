import { cleanup, render, screen, waitFor } from '@testing-library/preact'
import { afterEach, describe, expect, it } from 'vitest'

import { ExplorePage } from './ExplorePage'
import type { Explore } from '../../transport/contracts'

afterEach(() => {
  cleanup()
  window.history.replaceState(null, '', '/')
})

const populatedExplore: Explore = {
  workspace: { name: 'go-auth-service' },
  sections: [{
    id: 'entrypoints',
    title: 'Entrypoints',
    description: 'Start where the service begins.',
    facts: [{
      id: 'main',
      label: 'Main service',
      detail: 'HTTP server',
      anchor: { path: 'cmd/server/main.go', line_start: 8, line_end: 20 },
    }],
  }],
  tours: [{ id: 'onboarding', title: 'Onboarding tour', steps: 5 }],
}

describe('ExplorePage', () => {
  it('announces loading while source-backed exploration is requested', () => {
    render(<ExplorePage load={() => Promise.race<Explore>([])} />)

    expect(screen.getByRole('status').textContent).toBe('Loading project map…')
  })

  it('reports a privacy-safe error when exploration is unavailable', async () => {
    render(<ExplorePage load={() => Promise.reject(new Error('sensitive transport detail'))} />)

    expect((await screen.findByRole('alert')).textContent).toBe('Project exploration unavailable. Refresh to retry.')
  })

  it('explains when the project map has no source-backed sections', async () => {
    render(<ExplorePage load={() => Promise.resolve({ ...populatedExplore, sections: [] })} />)

    expect((await screen.findByRole('heading', { name: 'No source-backed project facts yet.' })).textContent).toBeTruthy()
  })

  it('renders source facts and a keyboard-complete deterministic source link', async () => {
    render(<ExplorePage load={() => Promise.resolve(populatedExplore)} />)

    expect(await screen.findByRole('heading', { name: 'go-auth-service' })).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Entrypoints' })).toBeTruthy()
    expect(screen.getByText('Main service')).toBeTruthy()
    expect(screen.getByText('HTTP server')).toBeTruthy()
    const link = screen.getByRole('link', { name: 'cmd/server/main.go lines 8–20' })
    expect(link.getAttribute('href')).toBe('#source?path=cmd%2Fserver%2Fmain.go&line_start=8&line_end=20')

    expect(link.tagName).toBe('A')
    link.focus()
    expect(document.activeElement).toBe(link)
    link.click()
    await waitFor(() => expect(window.location.hash).toBe('#source?path=cmd%2Fserver%2Fmain.go&line_start=8&line_end=20'))
  })
})
