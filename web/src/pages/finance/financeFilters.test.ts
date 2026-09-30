import { expect, it } from 'vitest'

import {
  NO_TRANSACTION_FILTERS,
  lastDayOfMonth,
  monthRange,
  searchFromTransactionFilters,
  spendingMonthFromSearch,
  transactionFiltersFromSearch,
  transactionsPath,
} from './financeFilters'

it('finds the last day of a month, leap years included', () => {
  expect(lastDayOfMonth('2031-01')).toBe('2031-01-31')
  expect(lastDayOfMonth('2031-04')).toBe('2031-04-30')
  expect(lastDayOfMonth('2032-02')).toBe('2032-02-29')
  expect(lastDayOfMonth('2031-02')).toBe('2031-02-28')
  expect(lastDayOfMonth('2031-12')).toBe('2031-12-31')
})

// The month in progress is read to today, not to its last day, so the
// range matches what the day chart and the budgets count.
it('reads a past month whole and the month in progress to today', () => {
  expect(monthRange('2031-03', '2031-05-14')).toEqual({ from: '2031-03-01', to: '2031-03-31' })
  expect(monthRange('2031-05', '2031-05-14')).toEqual({ from: '2031-05-01', to: '2031-05-14' })
})

it('takes the month from the address only when it is a month that has begun', () => {
  const read = (search: string) => spendingMonthFromSearch(new URLSearchParams(search), '2031-05')
  expect(read('month=2031-02')).toBe('2031-02')
  expect(read('month=2031-05')).toBe('2031-05')
  expect(read('')).toBe('2031-05')
  expect(read('month=2031-06')).toBe('2031-05')
  expect(read('month=2031-13')).toBe('2031-05')
  expect(read('month=soon')).toBe('2031-05')
})

it('reads the transaction filters out of the address', () => {
  const filters = transactionFiltersFromSearch(
    new URLSearchParams(
      'from=2031-03-01&to=2031-03-31&spendingCategoryId=category-a&financeAccountId=account-b&text=+corner+shop+&isUncategorized=true',
    ),
  )
  expect(filters).toEqual({
    from: '2031-03-01',
    to: '2031-03-31',
    spendingCategoryId: 'category-a',
    financeAccountId: 'account-b',
    text: 'corner shop',
    isUncategorized: true,
  })
})

// A day that is not a day would only be refused by the server; it is
// dropped, and an address with nothing in it is no filter at all.
it('drops days that are not days and reads an empty address as no filters', () => {
  const filters = transactionFiltersFromSearch(new URLSearchParams('from=2031-02-30&to=yesterday&isUncategorized=1'))
  expect(filters).toEqual(NO_TRANSACTION_FILTERS)
  expect(transactionFiltersFromSearch(new URLSearchParams(''))).toEqual(NO_TRANSACTION_FILTERS)
})

it('writes only the filters that are set, and reads back what it wrote', () => {
  const filters = { ...NO_TRANSACTION_FILTERS, from: '2031-03-01', spendingCategoryId: 'category-a' }
  const search = searchFromTransactionFilters(filters)
  expect(search.toString()).toBe('from=2031-03-01&spendingCategoryId=category-a')
  expect(transactionFiltersFromSearch(search)).toEqual(filters)
  expect(searchFromTransactionFilters(NO_TRANSACTION_FILTERS).toString()).toBe('')
})

it('links to the transactions section with its filters', () => {
  expect(transactionsPath({})).toBe('/finance/transactions')
  expect(transactionsPath({ from: '2031-03-01', to: '2031-03-31', isUncategorized: true })).toBe(
    '/finance/transactions?from=2031-03-01&to=2031-03-31&isUncategorized=true',
  )
  expect(transactionsPath({ text: 'Tea & Cake' })).toBe('/finance/transactions?text=Tea+%26+Cake')
})
