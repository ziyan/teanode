import { expect, it } from 'vitest'

import { en } from '../../i18n/en'
import { ja } from '../../i18n/ja'
import { Key } from '../../i18n/i18n'
import { zh } from '../../i18n/zh'
import { spendingCategoryDisplayName } from './spendingCategoryName'

const english = (key: Key) => en[key]

it('shows the built-in names in the reader\'s words', () => {
  expect(spendingCategoryDisplayName('groceries', english)).toBe('Groceries')
  expect(spendingCategoryDisplayName('dining', english)).toBe('Dining out')
  expect(spendingCategoryDisplayName('gifts and donations', english)).toBe('Gifts and donations')
  expect(spendingCategoryDisplayName('dining', (key) => zh[key])).not.toBe('Dining out')
  expect(spendingCategoryDisplayName('dining', (key) => ja[key])).not.toBe('Dining out')
})

// Only the stored built-in name, exactly: a name somebody typed is theirs.
it('leaves a name the person chose as they wrote it', () => {
  expect(spendingCategoryDisplayName('Groceries', english)).toBe('Groceries')
  expect(spendingCategoryDisplayName('weekend groceries', english)).toBe('weekend groceries')
  expect(spendingCategoryDisplayName(' dining', english)).toBe(' dining')
  expect(spendingCategoryDisplayName('toString', english)).toBe('toString')
  expect(spendingCategoryDisplayName('', english)).toBe('')
})

it('names the later built-in categories too', () => {
  expect(spendingCategoryDisplayName('children', english)).toBe('Kids')
  expect(spendingCategoryDisplayName('taxes', english)).toBe('Taxes')
  expect(spendingCategoryDisplayName('transfer', english)).toBe('Transfer')
  expect(spendingCategoryDisplayName('transfer', (key) => ja[key])).toBe('振替')
})
