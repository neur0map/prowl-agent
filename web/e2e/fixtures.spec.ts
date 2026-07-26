import { expect, test } from '@playwright/test'

import { openAuthenticatedWorkbench, prepareFixture, removeFixture, startWorkbench, stopWorkbench } from './support'

const fixtures = [
  { name: 'go-auth-service', entrypoint: 'cmd/server/main.go' },
  { name: 'ts-checkout', entrypoint: 'src/checkout.test.ts' },
  { name: 'rice-config', entrypoint: null },
] as const

test.describe('real source fixture visual baselines', () => {
  for (const fixture of fixtures) {
    test(`${fixture.name} renders source-backed desktop brief`, async ({ page }) => {
      const workspace = prepareFixture(fixture.name)
      const { process, startup } = startWorkbench(workspace)
      try {
        await page.setViewportSize({ width: 1280, height: 800 })
        await page.emulateMedia({ reducedMotion: 'reduce' })
        const authentication = await openAuthenticatedWorkbench(page, await startup)

        expect(page.viewportSize()).toEqual({ width: 1280, height: 800 })
        expect(authentication).toEqual({
          bearerHeader: true,
          bearerAbsentFromURL: true,
          cleanBrowserLocation: true,
        })
        await expect(page.getByRole('heading', { name: fixture.name })).toBeVisible()
        await expect(page.getByRole('heading', { name: 'Start with evidence' })).toBeVisible()
        await expect(page.getByText('README.md', { exact: true })).toBeVisible()
        if (fixture.entrypoint === null) {
          await expect(page.getByText('No entrypoints were identified.', { exact: true })).toBeVisible()
        } else {
          await expect(page.getByText(fixture.entrypoint, { exact: true })).toBeVisible()
        }
        await expect(page.getByRole('button', { name: 'Reload current view' })).toBeVisible()
        await expect(page).toHaveScreenshot(`${fixture.name}-brief-1280x800.png`, {
          animations: 'disabled',
          mask: [page.locator('.brief-indexed')],
        })
      } finally {
        await stopWorkbench(process)
        removeFixture(workspace)
      }
    })
  }
})
