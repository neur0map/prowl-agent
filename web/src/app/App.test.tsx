import { cleanup, render, screen } from '@testing-library/preact'
import { afterEach, describe, expect, it } from 'vitest'

import { App } from './App'

afterEach(cleanup)

describe('workbench shell', () => {
  it('explains the product and exposes every primary view', () => {
    render(<App />)

    expect(screen.getByRole('heading', { name: 'Prowl Workbench' })).toBeTruthy()
    expect(screen.getByText(/local knowledge compiler/i)).toBeTruthy()

    for (const view of ['Home', 'Explore', 'Context Lens', 'Impact', 'Knowledge', 'Timeline', 'Setup']) {
      expect(screen.getByRole('link', { name: view })).toBeTruthy()
    }
  })
})
