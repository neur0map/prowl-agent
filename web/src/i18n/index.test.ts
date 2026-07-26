import { h } from 'preact'
import { cleanup, render, screen } from '@testing-library/preact'
import { afterEach, describe, expect, it } from 'vitest'

import enXA from './en-XA'
import en from './en'
import { createI18n, I18nProvider, resolveLocale, useI18n } from './index'

afterEach(() => {
  cleanup()
  document.documentElement.lang = 'en'
})

function LocalizedButton() {
  const { t } = useI18n()
  return h('button', null, t('app.reloadCurrentView'))
}

describe('workbench localization', () => {
  it('uses English when the document does not request a supported locale', () => {
    expect(resolveLocale('')).toBe('en')
    expect(resolveLocale('fr-CA')).toBe('en')
    expect(createI18n('en').t('app.skipToContent')).toBe('Skip to content')
  })

  it('renders expanded en-XA messages while preserving interpolation values', () => {
    const english = createI18n('en').t('app.fullSourceAnchor', { start: 10, end: 24 })
    const pseudo = createI18n('en-XA').t('app.fullSourceAnchor', { start: 10, end: 24 })

    expect(pseudo).toContain('10')
    expect(pseudo).toContain('24')
    expect(pseudo).not.toBe(english)
    expect(pseudo.length).toBeGreaterThan(english.length)
    expect(createI18n('en-XA').formatNumber(12_345)).toBe('12,345')
  })

  it('keeps every pseudo-locale message expanded with the same interpolation tokens', () => {
    expect(Object.keys(enXA)).toEqual(Object.keys(en))
    for (const key of Object.keys(en) as Array<keyof typeof en>) {
      const english = en[key]
      const pseudo = enXA[key]
      expect(pseudo.length, key).toBeGreaterThan(english.length)
      expect(pseudo.match(/\{\{[A-Za-z0-9_]+\}\}/g), key).toEqual(english.match(/\{\{[A-Za-z0-9_]+\}\}/g))
    }
  })

  it('renders the document-selected locale case insensitively', () => {
    document.documentElement.lang = 'EN-xA'
    render(h(I18nProvider, null, h(LocalizedButton, null)))

    const label = screen.getByRole('button').textContent ?? ''
    expect(label).not.toBe('Reload current view')
    expect(label).toContain('［')
  })
})
