import { expect, it } from 'vitest'

import {
  NO_TRANSACTION_FILTERS,
  isYear,
  lastDayOfMonth,
  latestMonthOfYear,
  monthRange,
  searchFromSpendingPeriod,
  searchFromTransactionFilters,
  spendingMonthFromSearch,
  spendingPeriodFromSearch,
  transactionFiltersFromSearch,
  transactionsPath,
  yearRange,
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
      'from=2031-03-01&to=2031-03-31&spendingCategoryId=category-a&financeAccountId=account-b&text=+corner+shop+&isUncategorized=true&isDuplicateIncluded=true',
    ),
  )
  expect(filters).toEqual({
    from: '2031-03-01',
    to: '2031-03-31',
    spendingCategoryId: 'category-a',
    financeAccountId: 'account-b',
    text: 'corner shop',
    isUncategorized: true,
    isDuplicateIncluded: true,
  })
})

// A day that is not a day would only be refused by the server; it is
// dropped, and an address with nothing in it is no filter at all.
it('drops days that are not days and reads an empty address as no filters', () => {
  const filters = transactionFiltersFromSearch(
    new URLSearchParams('from=2031-02-30&to=yesterday&isUncategorized=1&isDuplicateIncluded=yes'),
  )
  expect(filters).toEqual(NO_TRANSACTION_FILTERS)
  expect(transactionFiltersFromSearch(new URLSearchParams(''))).toEqual(NO_TRANSACTION_FILTERS)
})

it('writes only the filters that are set, and reads back what it wrote', () => {
  const filters = { ...NO_TRANSACTION_FILTERS, from: '2031-03-01', spendingCategoryId: 'category-a' }
  const search = searchFromTransactionFilters(filters)
  expect(search.toString()).toBe('from=2031-03-01&spendingCategoryId=category-a')
  expect(transactionFiltersFromSearch(search)).toEqual(filters)
  expect(searchFromTransactionFilters(NO_TRANSACTION_FILTERS).toString()).toBe('')
  const withDuplicates = { ...NO_TRANSACTION_FILTERS, isDuplicateIncluded: true }
  expect(searchFromTransactionFilters(withDuplicates).toString()).toBe('isDuplicateIncluded=true')
  expect(transactionFiltersFromSearch(searchFromTransactionFilters(withDuplicates))).toEqual(withDuplicates)
})

it('links to the transactions section with its filters', () => {
  expect(transactionsPath({})).toBe('/finance/transactions')
  expect(transactionsPath({ from: '2031-03-01', to: '2031-03-31', isUncategorized: true })).toBe(
    '/finance/transactions?from=2031-03-01&to=2031-03-31&isUncategorized=true',
  )
  expect(transactionsPath({ text: 'Tea & Cake' })).toBe('/finance/transactions?text=Tea+%26+Cake')
})

// A year in the address is Year mode, but only a year that has begun; a
// year that is not one falls back to the month, as a bad month does.
it('reads Month or Year mode out of the address', () => {
  const read = (search: string) => spendingPeriodFromSearch(new URLSearchParams(search), '2031-05')
  expect(read('')).toEqual({ spendingPeriodKind: 'month', month: '2031-05', year: '2031' })
  expect(read('month=2030-08')).toEqual({ spendingPeriodKind: 'month', month: '2030-08', year: '2030' })
  expect(read('year=2031')).toEqual({ spendingPeriodKind: 'year', month: '2031-05', year: '2031' })
  expect(read('year=2029')).toEqual({ spendingPeriodKind: 'year', month: '2029-12', year: '2029' })
  expect(read('year=2032')).toEqual({ spendingPeriodKind: 'month', month: '2031-05', year: '2031' })
  expect(read('year=31')).toEqual({ spendingPeriodKind: 'month', month: '2031-05', year: '2031' })
  expect(read('year=2029&month=2030-02').spendingPeriodKind).toBe('year')
})

// Year zero matches four digits and the server refuses it; a year before
// the history's twenty would show only zeros. Both fall back to this
// month, as a year still to come does.
it('keeps years the server refuses and years before the history out of Year mode', () => {
  const read = (search: string) => spendingPeriodFromSearch(new URLSearchParams(search), '2031-05')
  const thisMonth = { spendingPeriodKind: 'month', month: '2031-05', year: '2031' }
  expect(read('year=0000')).toEqual(thisMonth)
  expect(read('year=1899')).toEqual(thisMonth)
  expect(read('year=2011')).toEqual(thisMonth)
  expect(read('year=2012')).toEqual({ spendingPeriodKind: 'year', month: '2012-12', year: '2012' })
  expect(read('month=0000-01')).toEqual(thisMonth)
  expect(isYear('0000', '2031-05')).toBe(false)
  expect(isYear('1900', '2031-05')).toBe(true)
  expect(isYear('2041', '2031-05')).toBe(true)
  expect(isYear('2042', '2031-05')).toBe(false)
})

it('writes a year always and a month only when it is not this one, and reads back what it wrote', () => {
  const write = (period: Parameters<typeof searchFromSpendingPeriod>[0]) =>
    new URLSearchParams(searchFromSpendingPeriod(period, '2031-05'))
  expect(write({ spendingPeriodKind: 'year', month: '2031-05', year: '2031' }).toString()).toBe('year=2031')
  expect(write({ spendingPeriodKind: 'month', month: '2031-05', year: '2031' }).toString()).toBe('')
  expect(write({ spendingPeriodKind: 'month', month: '2030-11', year: '2030' }).toString()).toBe('month=2030-11')
  for (const period of [
    { spendingPeriodKind: 'year' as const, month: '2029-12', year: '2029' },
    { spendingPeriodKind: 'month' as const, month: '2030-11', year: '2030' },
  ]) {
    expect(spendingPeriodFromSearch(write(period), '2031-05')).toEqual(period)
  }
})

it('lands a year on its latest month begun, and reads the year in progress to today', () => {
  expect(latestMonthOfYear('2031', '2031-05')).toBe('2031-05')
  expect(latestMonthOfYear('2030', '2031-05')).toBe('2030-12')
  expect(yearRange('2030', '2031-05-14')).toEqual({ from: '2030-01-01', to: '2030-12-31' })
  expect(yearRange('2031', '2031-05-14')).toEqual({ from: '2031-01-01', to: '2031-05-14' })
})
