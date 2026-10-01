import { expect, it } from 'vitest'

import { en } from '../../i18n/en'
import type { SpendingCategory } from './financeApi'
import { spendingCategoryOptions } from './financeCommon'
import { spendingCategoryDisplayName } from './spendingCategoryName'

function category(id: string, name: string, flags: Partial<SpendingCategory> = {}): SpendingCategory {
  return { id, spendingCategoryName: name, isIncome: false, isHidden: false, isTransfer: false, ...flags }
}

const displayName = (name: string) => spendingCategoryDisplayName(name, (key) => en[key])

// Transfer is offered like any spending category, but last and under a
// heading of its own, so it does not read as one more kind of spending.
it('offers the transfer category last, under its own heading', () => {
  const categories = [
    category('transfer-id', 'transfer', { isTransfer: true }),
    category('travel-id', 'travel'),
    category('dining-id', 'dining'),
  ]
  expect(spendingCategoryOptions(categories, displayName, null, 'Neither spending nor income')).toEqual([
    { value: 'dining-id', label: 'Dining out' },
    { value: 'travel-id', label: 'Travel' },
    { value: 'transfer-id', label: 'Transfer', group: 'Neither spending nor income' },
  ])
  const withoutHeading = spendingCategoryOptions(categories, displayName)
  expect(withoutHeading[withoutHeading.length - 1]).toEqual({ value: 'transfer-id', label: 'Transfer' })
})

// Renamed, it is still the transfer category: it is known by its flag.
it('keeps a renamed transfer category last', () => {
  const categories = [
    category('transfer-id', 'between my accounts', { isTransfer: true }),
    category('zoo-id', 'zoo trips'),
  ]
  expect(spendingCategoryOptions(categories, displayName, null, 'heading').map((option) => option.value)).toEqual([
    'zoo-id',
    'transfer-id',
  ])
})
