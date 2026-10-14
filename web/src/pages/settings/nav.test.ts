import { expect, it } from 'vitest'
import { matchSettingsSurface, surfacesByCategory } from './nav'

it('keeps Preferences first among the account surfaces', () => {
  expect(surfacesByCategory('account')[0].segment).toBe('preference')
})

it('has one Devices entry after API tokens and before Apps, and no Device setup or Phones entry', () => {
  const segments = surfacesByCategory('account').map((surface) => surface.segment)
  expect(segments.filter((segment) => segment === 'devices')).toHaveLength(1)
  expect(segments.indexOf('devices')).toBe(segments.indexOf('tokens') + 1)
  expect(segments.indexOf('apps')).toBe(segments.indexOf('devices') + 1)
  expect(segments).not.toContain('sync')
  expect(segments).not.toContain('phones')
})

it('names the Devices surface for the page and its sub paths', () => {
  expect(matchSettingsSurface('/settings/devices')?.label).toBe('devices.title')
  expect(matchSettingsSurface('/settings/devices/anything')?.segment).toBe('devices')
})
