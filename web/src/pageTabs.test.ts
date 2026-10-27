import { expect, it } from 'vitest'

import type { ServiceCapabilities } from './api'
import { ACCESS_TABS, CALENDAR_TABS, PAGE_TABS, shownTabs, tabAddress } from './pageTabs'

function addressOf(path: string, tabId: string): string {
  const page = PAGE_TABS.find((candidate) => candidate.path === path)
  if (!page) throw new Error(`no tabs for ${path}`)
  return tabAddress(page, tabId)
}

it('puts a tab in the path, in a query parameter, or leaves the first at the page', () => {
  expect(addressOf('/manage/access', 'roles')).toBe('/manage/access/roles')
  expect(addressOf('/location', 'travel')).toBe('/location?tab=travel')
  expect(addressOf('/location', 'visits')).toBe('/location')
  expect(addressOf('/sharing', 'by-you')).toBe('/sharing')
  expect(addressOf('/sharing', 'with-you')).toBe('/sharing/with-you')
  expect(addressOf('/calendar', 'week')).toBe('/calendar?view=week')
})

it('has no tab twice in a page, and no page twice', () => {
  expect(new Set(PAGE_TABS.map((page) => page.path)).size).toBe(PAGE_TABS.length)
  for (const page of PAGE_TABS) {
    expect(new Set(page.tabs.map((tab) => tab.id)).size).toBe(page.tabs.length)
  }
})

it('leaves out the tabs the permissions and the services do not give', () => {
  const holds = (...keys: string[]) => ({ hasPermission: (key: string) => keys.includes(key) })
  expect(shownTabs(ACCESS_TABS, holds('audit:read')).map((tab) => tab.id)).toEqual(['audit'])
  expect(shownTabs(ACCESS_TABS, holds()).map((tab) => tab.id)).toEqual([])
  const remindersOnly = { isCalendarEnabled: false, isRemindersEnabled: true } as ServiceCapabilities
  expect(shownTabs(CALENDAR_TABS, { services: remindersOnly }).map((tab) => tab.id)).toEqual(['reminders'])
})
