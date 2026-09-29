import { describe, expect, it } from 'vitest'

import { RUN_KINDS } from '../agentRuns'
import { en } from './en'
import type { Catalog, Key } from './i18n'
import { ja } from './ja'
import { SAME_IN_EVERY_LANGUAGE } from './sameInEveryLanguage'
import { zh } from './zh'

// The types already make every catalogue carry exactly the English keys. What
// they cannot see is a value copied from English and never translated, or a
// translation that dropped or invented a {placeholder}, which renders as a
// sentence with a hole in it or a literal brace rather than an error.

const TRANSLATIONS: [string, Catalog][] = [
  ['ja', ja],
  ['zh', zh],
]
const KEYS = Object.keys(en) as Key[]

function placeholdersOf(text: string): string[] {
  return [...new Set(text.match(/\{\w+\}/g) ?? [])].sort()
}

describe('translation catalogues', () => {
  it.each(TRANSLATIONS)('%s has exactly the English keys', (_, catalog) => {
    expect(Object.keys(catalog).sort()).toEqual([...KEYS].sort())
  })

  it.each(TRANSLATIONS)('%s translates every value not meant to read as English', (_, catalog) => {
    const copiedKeys = KEYS.filter((key) => catalog[key] === en[key] && !SAME_IN_EVERY_LANGUAGE.has(key))
    expect(copiedKeys).toEqual([])
  })

  it.each(TRANSLATIONS)('%s uses the same placeholders as English', (_, catalog) => {
    const mismatchedKeys = KEYS.filter(
      (key) => placeholdersOf(catalog[key]).join() !== placeholdersOf(en[key]).join(),
    ).map(
      (key) => `${key}: ${placeholdersOf(en[key]).join() || 'none'} / ${placeholdersOf(catalog[key]).join() || 'none'}`,
    )
    expect(mismatchedKeys).toEqual([])
  })

  // A key left on the list after every language translated it would let the
  // next copied value through unnoticed. One language keeping the English is
  // enough: Japanese writes Cc where Chinese has a word of its own.
  it('lists only keys some language does write as English', () => {
    const staleKeys = [...SAME_IN_EVERY_LANGUAGE].filter((key) =>
      TRANSLATIONS.every(([, catalog]) => catalog[key] !== en[key]),
    )
    expect(staleKeys).toEqual([])
  })

  // Run kinds are looked up by a key built from the server's word, which the
  // types cannot check.
  it('names every run kind the filter offers', () => {
    const missingKeys = RUN_KINDS.map((kind) => `agent.runKinds.${kind}`).filter((key) => !(key in en))
    expect(missingKeys).toEqual([])
  })
})
