import { expect, it } from 'vitest'
import { surfacesByCategory } from '../pages/settings/nav'
import { ACCOUNT_ICONS } from './sidebar'

it('gives every page of the account settings an icon', () => {
  expect(surfacesByCategory('account').filter((surface) => !ACCOUNT_ICONS[surface.segment])).toEqual([])
})
