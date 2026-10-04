import { expect, it } from 'vitest'

import { SummaryAmounts, spendingLines, spendingTotals } from './spendingLines'

const row = (groupKey: string, moneyOut: string, moneyIn: string, currencyCode = 'USD'): SummaryAmounts => ({
  groupKey,
  groupLabel: groupKey ? `Label ${groupKey}` : '',
  currencyCode,
  moneyOut,
  moneyIn,
  financeTransactionCount: 2,
})

// Refunds come off a category, income categories are not spending, and
// money in with no category is not taken off what it spent.
it('counts categories net of refunds and leaves income out', () => {
  const lines = spendingLines(
    [
      row('category-food', '300.00', '20.00'),
      row('category-salary', '0', '5200.00'),
      row('', '80.00', '1000.00'),
      row('category-returns', '0', '45.00'),
      row('category-even', '30.00', '30.00'),
    ],
    'spendingCategory',
    new Set(['category-salary']),
  )
  expect(lines.map((line) => [line.groupKey, line.spendingAmount])).toEqual([
    ['category-food', 280],
    ['', 80],
    ['category-returns', -45],
  ])
})

// The other category is counted as no category was: what came in under
// it is income, so it does not come off what went out under it.
it('counts the other category by its money out alone', () => {
  const lines = spendingLines(
    [
      row('category-other', '50.00', '20.00'),
      row('category-food', '100.00', '10.00'),
      row('category-refunded', '0', '15.00'),
    ],
    'spendingCategory',
    new Set(),
    'category-other',
  )
  expect(lines.map((line) => [line.groupKey, line.spendingAmount])).toEqual([
    ['category-food', 90],
    ['category-other', 50],
    ['category-refunded', -15],
  ])
})

// By merchant or account a group that only paid in is not a place money
// went, and one that spent is counted net.
it('keeps only the merchants and accounts that spent something', () => {
  const lines = spendingLines(
    [row('Corner Shop', '120.00', '10.00'), row('Employer', '0', '5200.00'), row('Fund', '5.00', '40.00')],
    'merchant',
    new Set(),
  )
  expect(lines.map((line) => [line.groupKey, line.spendingAmount])).toEqual([['Corner Shop', 110]])
})

it('adds lines up a currency at a time', () => {
  const lines = spendingLines(
    [row('category-food', '100', '0'), row('category-rent', '50', '0'), row('category-food', '30', '0', 'EUR')],
    'spendingCategory',
    new Set(),
  )
  expect(spendingTotals(lines)).toEqual([
    { currencyCode: 'EUR', spendingAmount: 30, financeTransactionCount: 2 },
    { currencyCode: 'USD', spendingAmount: 150, financeTransactionCount: 4 },
  ])
})
