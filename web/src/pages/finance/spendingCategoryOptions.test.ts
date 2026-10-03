import { expect, it } from 'vitest'

import { en } from '../../i18n/en'
import type { SpendingCategory } from './financeApi'
import { spendingCategoryOptions } from './financeCommon'
import { BuiltInFlags, spendingCategoryDisplayName } from './spendingCategoryName'

function category(id: string, name: string, flags: Partial<SpendingCategory> = {}): SpendingCategory {
  return { id, spendingCategoryName: name, isIncome: false, isHidden: false, isTransfer: false, isOther: false, ...flags }
}

const displayName = (name: string, flags?: BuiltInFlags) => spendingCategoryDisplayName(name, (key) => en[key], flags)

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

// A person's own category called transfer stays theirs, shown as they
// wrote it, beside the built-in one that was named around it.
it('shows a person\'s own transfer as they named it', () => {
  const categories = [
    category('own-transfer-id', 'transfer'),
    category('transfer-id', 'transfer between own accounts', { isTransfer: true }),
  ]
  expect(spendingCategoryOptions(categories, displayName, null, 'heading')).toEqual([
    { value: 'own-transfer-id', label: 'transfer' },
    { value: 'transfer-id', label: 'transfer between own accounts', group: 'heading' },
  ])
})

// Other is the choice for what fits nothing: after the rest of the
// spending, before the transfer category, and there is no option for no
// spending category at all.
it('offers other after the rest of the spending and no choice of none', () => {
  const categories = [
    category('transfer-id', 'transfer', { isTransfer: true }),
    category('other-id', 'other', { isOther: true }),
    category('travel-id', 'travel'),
    category('dining-id', 'dining'),
  ]
  const options = spendingCategoryOptions(categories, displayName, null, 'Neither spending nor income')
  expect(options).toEqual([
    { value: 'dining-id', label: 'Dining out' },
    { value: 'travel-id', label: 'Travel' },
    { value: 'other-id', label: 'Other' },
    { value: 'transfer-id', label: 'Transfer', group: 'Neither spending nor income' },
  ])
  expect(options.some((option) => option.value === '')).toBe(false)
})

// A person's own "other" beside the built-in one is theirs, sorted with
// the rest and shown as they wrote it; the built-in one, renamed around
// it, still comes after the spending.
it('keeps a person\'s own other apart from the built-in one', () => {
  const categories = [
    category('own-other-id', 'other'),
    category('other-id', 'anything else', { isOther: true }),
    category('zoo-id', 'zoo trips'),
  ]
  expect(spendingCategoryOptions(categories, displayName)).toEqual([
    { value: 'own-other-id', label: 'other' },
    { value: 'zoo-id', label: 'zoo trips' },
    { value: 'other-id', label: 'anything else' },
  ])
})
