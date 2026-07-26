import AxeBuilder from '@axe-core/playwright'
import { expect, test } from '@playwright/test'

import { openAuthenticatedWorkbench, prepareFixture, removeFixture, startWorkbench, stopWorkbench } from './support'

const viewports = [
  { name: '1280x800', width: 1280, height: 800 },
  { name: '768x1024', width: 768, height: 1024 },
  { name: '390x844', width: 390, height: 844 },
] as const

test.describe('accessible responsive workbench', () => {
  for (const viewport of viewports) {
    test(`ts-checkout supports keyboard and accessibility at ${viewport.name}`, async ({ page }) => {
      const workspace = prepareFixture('ts-checkout')
      const { process, startup } = startWorkbench(workspace)
      try {
        await page.setViewportSize(viewport)
        await page.emulateMedia({ reducedMotion: 'reduce' })
        const authentication = await openAuthenticatedWorkbench(page, await startup)

        expect(page.viewportSize()).toEqual({ width: viewport.width, height: viewport.height })
        expect(authentication).toEqual({
          bearerHeader: true,
          bearerAbsentFromURL: true,
          cleanBrowserLocation: true,
        })
        await expect(page.getByRole('heading', { name: 'ts-checkout' })).toBeVisible()
        const reducedMotion = await page.evaluate(() => {
          const duration = getComputedStyle(document.body).animationDuration
          return {
            requested: matchMedia('(prefers-reduced-motion: reduce)').matches,
            durationMilliseconds: Number.parseFloat(duration) * (duration.endsWith('ms') ? 1 : 1_000),
          }
        })
        expect(reducedMotion.requested).toBe(true)
        expect(reducedMotion.durationMilliseconds).toBeLessThanOrEqual(0.01)

        await page.keyboard.press('Tab')
        await expect(page.getByRole('link', { name: 'Skip to content' })).toBeFocused()
        await page.keyboard.press('Enter')
        await expect(page.locator('#main-content')).toBeFocused()

        const reload = page.getByRole('button', { name: 'Reload current view' })
        const accessibility = await new AxeBuilder({ page }).analyze()
        expect(accessibility.violations).toEqual([])
        expect(accessibility.incomplete.filter((result) => result.impact === 'serious')).toEqual([])
        await expect(page).toHaveScreenshot(`ts-checkout-brief-${viewport.name}.png`, {
          animations: 'disabled',
          mask: [page.locator('.brief-indexed')],
        })

        await page.evaluate(() => { document.body.style.zoom = '200%' })
        expect(await page.evaluate(() => Number.parseFloat(getComputedStyle(document.body).zoom))).toBe(2)

        const refresh = page.getByRole('button', { name: 'Refresh index' })
        await page.keyboard.press('Tab')
        await expect(refresh).toBeFocused()
        await expect(refresh).toBeVisible()
        await expect(refresh).toBeInViewport()

        await page.keyboard.press('Tab')
        await expect(reload).toBeFocused()
        await expect(reload).toBeVisible()
        await expect(reload).toBeInViewport()
        await page.keyboard.press('Enter')
        await expect(page.locator('#main-content')).toBeFocused()
        await expect(page.getByRole('heading', { name: 'ts-checkout' })).toBeVisible()
      } finally {
        await stopWorkbench(process)
        removeFixture(workspace)
      }
    })
  }
})
