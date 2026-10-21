// The cases in internal/passwords/testdata/sitematch.json are read by the Go
// tests of the same rule (internal/passwords/sitematch_test.go) and here. Both
// must give the same answers, so a site that matches in the server's check
// matches in the extension's, and one that does not, does not.

import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { doSitesMatchPage, normalizeHost } from './hosts'

interface SharedCases {
  normalizedHostCases: { site: string; expectedHost: string }[]
  originCases: { name: string; sites: string[]; page: string; isMatch: boolean }[]
}

const casesPath = resolve(import.meta.dirname, '../../../../internal/passwords/testdata/sitematch.json')

describe('the cases shared with the Go site rule', () => {
  it('has the file', () => {
    expect(existsSync(casesPath)).toBe(true)
  })

  const cases = existsSync(casesPath) ? (JSON.parse(readFileSync(casesPath, 'utf8')) as SharedCases) : null

  it('reads every site to the same host', () => {
    expect(cases?.normalizedHostCases.length).toBeGreaterThan(20)
    for (const { site: savedSite, expectedHost } of cases?.normalizedHostCases ?? []) {
      expect(normalizeHost(savedSite), JSON.stringify(savedSite)).toBe(expectedHost)
    }
  })

  it('matches every page to the same sites', () => {
    expect(cases?.originCases.length).toBeGreaterThan(20)
    for (const each of cases?.originCases ?? []) {
      expect(doSitesMatchPage(each.sites, each.page), each.name).toBe(each.isMatch)
    }
  })
})
