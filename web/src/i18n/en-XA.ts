import en from './en'

const vowelMap: Record<string, string> = {
  a: 'à', A: 'À', e: 'ë', E: 'Ë', i: 'ï', I: 'Ï', o: 'ô', O: 'Ô', u: 'û', U: 'Û', y: 'ÿ', Y: 'Ÿ',
}

/**
 * Pseudo-localization deliberately expands static English without touching
 * interpolation markers. It makes clipped controls and untranslated UI visible
 * during deterministic browser fixtures without claiming to be a real locale.
 */
export function pseudoLocalize(message: string): string {
  const fragments = message.split(/(\{\{[A-Za-z0-9_]+\}\})/g)
  const transformed = fragments.map((fragment) => {
    if (/^\{\{[A-Za-z0-9_]+\}\}$/.test(fragment)) return fragment
    return fragment.replace(/[A-Za-z]/g, (character) => vowelMap[character] ?? character)
  }).join('')
  const padding = ' ~'.repeat(Math.max(1, Math.ceil(message.replace(/\{\{[A-Za-z0-9_]+\}\}/g, '').length / 10)))
  return `［${transformed}${padding}］`
}

const enXA = Object.fromEntries(Object.entries(en).map(([key, value]) => [key, pseudoLocalize(value)])) as typeof en

export default enXA
