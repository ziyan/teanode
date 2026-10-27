import { expect, it } from 'vitest'
import { surfacesByCategory } from '../pages/settings/nav'
import type { ServiceCapabilities } from '../api'
import { ACCOUNT_ICONS, navigablePages } from './sidebar'

it('gives every page of the account settings an icon', () => {
  expect(surfacesByCategory('account').filter((surface) => !ACCOUNT_ICONS[surface.segment])).toEqual([])
})

const SERVICES = { isCalendarEnabled: true, isContactsEnabled: true, isAssistantEnabled: true } as ServiceCapabilities

function pagePaths(permissions: string[], overrides: Partial<Parameters<typeof navigablePages>[0]> = {}): string[] {
  return navigablePages({
    permissions: { everywhere: permissions, byDomain: [] },
    services: SERVICES,
    hasMailbox: true,
    hasMailboxRail: true,
    isAgentAvailable: true,
    isFinanceShown: false,
    hasCoding: false,
    ...overrides,
  }).map((page) => page.to)
}

it('offers the management pages only to somebody who may open them', () => {
  expect(pagePaths(['journal:use'])).not.toContain('/manage/queue')
  const paths = pagePaths(['queue:manage'])
  expect(paths).toContain('/manage/queue')
  expect(paths).not.toContain('/manage/domains')
})

it('offers Finance and Coding only where the rail would show them', () => {
  expect(pagePaths(['agent:use'])).not.toContain('/finance')
  expect(pagePaths(['agent:use'])).not.toContain('/coding')
  const paths = pagePaths(['agent:use'], { isFinanceShown: true, hasCoding: true })
  expect(paths).toContain('/finance')
  expect(paths).toContain('/coding')
  expect(pagePaths([], { isFinanceShown: true, hasCoding: true })).not.toContain('/coding')
})

it('follows the rail of somebody with no mailbox', () => {
  const withoutMailbox = { hasMailbox: false, hasMailboxRail: false }
  expect(pagePaths(['agent:use'], withoutMailbox)).toContain('/knowledge')
  expect(pagePaths(['agent:use'], withoutMailbox)).not.toContain('/mailbox')
  expect(pagePaths(['agent:use'])).toContain('/mailbox')
  expect(pagePaths([], withoutMailbox)).not.toContain('/settings/mailbox')
})

it('offers each page once', () => {
  const pages = navigablePages({
    permissions: { everywhere: ['agent:use', 'journal:use'], byDomain: [] },
    services: SERVICES,
    hasMailbox: true,
    hasMailboxRail: true,
    isAgentAvailable: true,
    isFinanceShown: true,
    hasCoding: true,
  })
  const identities = pages.map((page) => `${page.to} ${page.label}`)
  expect(new Set(identities).size).toBe(identities.length)
})
