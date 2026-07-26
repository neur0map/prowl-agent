import { createContext, h, type ComponentChildren } from 'preact'
import { useContext, useMemo } from 'preact/hooks'

import en from './en'
import enXA from './en-XA'

export type SupportedLocale = 'en' | 'en-XA'
export type MessageKey = keyof typeof en
type Interpolation = Record<string, string | number>
type Catalog = typeof en

export type I18n = {
  locale: SupportedLocale
  t: (key: MessageKey, values?: Interpolation) => string
  formatNumber: (value: number) => string
}

function interpolate(message: string, values: Interpolation | undefined): string {
  if (!values) return message
  return message.replace(/\{\{([A-Za-z0-9_]+)\}\}/g, (match, key: string) => key in values ? String(values[key]) : match)
}

function intlLocale(locale: SupportedLocale): string {
  return locale === 'en-XA' ? 'en-US' : locale
}

export function resolveLocale(language: string | null | undefined): SupportedLocale {
  return language?.toLowerCase() === 'en-xa' ? 'en-XA' : 'en'
}

export function createI18n(locale: SupportedLocale = 'en'): I18n {
  const catalog: Catalog = locale === 'en-XA' ? enXA : en
  const formatter = new Intl.NumberFormat(intlLocale(locale))
  return {
    locale,
    t: (key, values) => interpolate(catalog[key], values),
    formatNumber: (value) => formatter.format(value),
  }
}
const I18nContext = createContext<I18n>(createI18n())

export function I18nProvider({ locale, children }: { locale?: SupportedLocale; children: ComponentChildren }) {
  const resolved = locale ?? resolveLocale(typeof document === 'undefined' ? undefined : document.documentElement.lang)
  const value = useMemo(() => createI18n(resolved), [resolved])
  return h(I18nContext.Provider, { value }, children)
}

export function useI18n(): I18n {
  return useContext(I18nContext)
}
