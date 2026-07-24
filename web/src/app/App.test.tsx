import { cleanup, render, screen } from '@testing-library/preact'
import { afterEach, describe, expect, it } from 'vitest'

import { App } from './App'

afterEach(cleanup)

describe('workbench shell', () => {
  it('starts with an accessible project-brief loading state', () => {
    render(<App />)

    expect(screen.getByRole('heading', { name: 'Project brief' })).toBeTruthy()
    expect(screen.getByRole('status')).toHaveProperty('textContent', 'Loading project brief…')
    expect(screen.getByLabelText('Project brief').getAttribute('aria-busy')).toBe('true')
  })

  it('gives an accessible recovery path when the one-time session is unavailable', () => {
    render(<App sessionError />)

    expect(screen.getByRole('heading', { name: 'Secure workbench session unavailable' })).toBeTruthy()
    expect(screen.getByRole('alert')).toHaveProperty('textContent', 'Secure workbench session unavailable. Reopen Prowl from your terminal.')
  })
})
