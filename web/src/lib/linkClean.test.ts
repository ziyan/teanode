import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

import { cleanLink, linkDisplayText } from './linkClean'

// The same cases are read by the server's and the iPhone app's tests, so the
// three implementations answer alike.
const cases: { input: string; cleaned: string; display: string }[] = JSON.parse(
  readFileSync(
    resolve(dirname(fileURLToPath(import.meta.url)), '../../../internal/util/linkclean/testdata/links.json'),
    'utf8',
  ),
)

describe('the shared link cases', () => {
  it('has cases to run', () => {
    expect(cases.length).toBeGreaterThan(10)
  })
  for (const found of cases) {
    it(`cleans ${found.input}`, () => {
      expect(cleanLink(found.input)).toBe(found.cleaned)
    })
    it(`shows ${found.input} as ${found.display}`, () => {
      expect(linkDisplayText(found.input)).toBe(found.display)
    })
  }
})

it('never shows more than the limit of characters', () => {
  const display = linkDisplayText(
    'https://example.com/aaaaaaaaaabbbbbbbbbbccccccccccddddddddddeeeeeeeeeeffffffffffgggggggggg',
  )
  expect(Array.from(display)).toHaveLength(48)
})
