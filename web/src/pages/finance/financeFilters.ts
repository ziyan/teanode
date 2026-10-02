// The Finance page's filters as they are written in the address, so a
// month on the Spending section and a narrowed list of transactions can be
// linked to, shared, and gone back to.

import { historyRange } from './spendingYear'

// TransactionFilters narrow the Transactions section, one field for each
// of the finance transactions query's arguments it offers. Empty is no
// filter. The mirrored copies are left out unless isDuplicateIncluded,
// as every total leaves them out.
export type TransactionFilters = {
  from: string
  to: string
  financeAccountId: string
  spendingCategoryId: string
  text: string
  isUncategorized: boolean
  isDuplicateIncluded: boolean
}

export const NO_TRANSACTION_FILTERS: TransactionFilters = {
  from: '',
  to: '',
  financeAccountId: '',
  spendingCategoryId: '',
  text: '',
  isUncategorized: false,
  isDuplicateIncluded: false,
}

const DAY_PATTERN = /^\d{4}-\d{2}-\d{2}$/
const MONTH_PATTERN = /^\d{4}-(0[1-9]|1[0-2])$/
const YEAR_PATTERN = /^\d{4}$/

// FIRST_YEAR and YEARS_AHEAD bound the years the server answers for: 1900
// through ten years past this one. Year zero matches the pattern, and the
// server refuses it.
const FIRST_YEAR = 1900
const YEARS_AHEAD = 10

// isYear says a value is a year written 2006 that the server answers for,
// from FIRST_YEAR to YEARS_AHEAD past this one.
export function isYear(value: string, currentMonth: string): boolean {
  if (!YEAR_PATTERN.test(value)) return false
  const year = Number(value)
  return year >= FIRST_YEAR && year <= Number(currentMonth.slice(0, 4)) + YEARS_AHEAD
}

function isDay(value: string): boolean {
  if (!DAY_PATTERN.test(value)) return false
  const parsed = new Date(`${value}T00:00:00`)
  return !Number.isNaN(parsed.getTime()) && parsed.getDate() === Number(value.slice(8, 10))
}

// transactionFiltersFromSearch reads the filters out of the address. A day
// that is not a day is dropped rather than sent to the server to refuse.
export function transactionFiltersFromSearch(search: URLSearchParams): TransactionFilters {
  const day = (name: string) => {
    const value = (search.get(name) ?? '').trim()
    return isDay(value) ? value : ''
  }
  return {
    from: day('from'),
    to: day('to'),
    financeAccountId: (search.get('financeAccountId') ?? '').trim(),
    spendingCategoryId: (search.get('spendingCategoryId') ?? '').trim(),
    text: (search.get('text') ?? '').trim(),
    isUncategorized: search.get('isUncategorized') === 'true',
    isDuplicateIncluded: search.get('isDuplicateIncluded') === 'true',
  }
}

// searchFromTransactionFilters writes the filters into an address, leaving
// out the ones that are empty, so an unfiltered list has a bare address.
export function searchFromTransactionFilters(filters: Partial<TransactionFilters>): URLSearchParams {
  const search = new URLSearchParams()
  const names: (keyof Omit<TransactionFilters, 'isUncategorized' | 'isDuplicateIncluded'>)[] = [
    'from',
    'to',
    'financeAccountId',
    'spendingCategoryId',
    'text',
  ]
  for (const name of names) {
    const value = (filters[name] ?? '').trim()
    if (value) search.set(name, value)
  }
  if (filters.isUncategorized) search.set('isUncategorized', 'true')
  if (filters.isDuplicateIncluded) search.set('isDuplicateIncluded', 'true')
  return search
}

// transactionsPath is the Transactions section narrowed by some filters.
export function transactionsPath(filters: Partial<TransactionFilters>): string {
  const search = searchFromTransactionFilters(filters).toString()
  return search ? `/finance/transactions?${search}` : '/finance/transactions'
}

// lastDayOfMonth is the month's last day, 2006-01-31, from 2006-01.
export function lastDayOfMonth(month: string): string {
  const [year, number] = month.split('-').map(Number)
  const last = new Date(year, number, 0).getDate()
  return `${month}-${String(last).padStart(2, '0')}`
}

// monthRange is the days a month's spending is read over: its first day to
// its last, or to today while it is the month in progress.
export function monthRange(month: string, today: string): { from: string; to: string } {
  const to = today.slice(0, 7) === month ? today : lastDayOfMonth(month)
  return { from: `${month}-01`, to }
}

// spendingMonthFromSearch is the month the Spending section shows: the one
// in the address when it is a month that has begun, of a year the server
// answers for, otherwise this one.
export function spendingMonthFromSearch(search: URLSearchParams, currentMonth: string): string {
  const month = (search.get('month') ?? '').trim()
  return MONTH_PATTERN.test(month) && isYear(month.slice(0, 4), currentMonth) && month <= currentMonth
    ? month
    : currentMonth
}

// SpendingPeriodKind is whether the Spending section shows one month or one
// calendar year.
export type SpendingPeriodKind = 'month' | 'year'

// SpendingPeriod is what the Spending section shows: a month, or a year.
// Both are always set, so switching between them has somewhere to land:
// in a year, month is its latest month that has begun, and in a month,
// year is the month's.
export type SpendingPeriod = { spendingPeriodKind: SpendingPeriodKind; month: string; year: string }

// latestMonthOfYear is the last month of a year that has begun: this month
// in the year in progress, December in a year that is over.
export function latestMonthOfYear(year: string, currentMonth: string): string {
  return year === currentMonth.slice(0, 4) ? currentMonth : `${year}-12`
}

// spendingPeriodFromSearch is the period the Spending section shows: a
// year when the address names one that has begun and that the history
// reaches (historyRange: a year further back would show nothing, read as
// a year of no money), otherwise the month spendingMonthFromSearch reads.
export function spendingPeriodFromSearch(search: URLSearchParams, currentMonth: string): SpendingPeriod {
  const year = (search.get('year') ?? '').trim()
  const firstHistoryYear = historyRange(currentMonth).fromMonth.slice(0, 4)
  if (isYear(year, currentMonth) && year <= currentMonth.slice(0, 4) && year >= firstHistoryYear) {
    return { spendingPeriodKind: 'year', year, month: latestMonthOfYear(year, currentMonth) }
  }
  const month = spendingMonthFromSearch(search, currentMonth)
  return { spendingPeriodKind: 'month', month, year: month.slice(0, 4) }
}

// searchFromSpendingPeriod writes a period into the address: a year always
// (a bare address is this month), a month only when it is not this one.
export function searchFromSpendingPeriod(period: SpendingPeriod, currentMonth: string): Record<string, string> {
  if (period.spendingPeriodKind === 'year') return { year: period.year }
  return period.month === currentMonth ? {} : { month: period.month }
}

// yearRange is the days a year's spending is read over: its first day to
// its last, or to today while it is the year in progress.
export function yearRange(year: string, today: string): { from: string; to: string } {
  return { from: `${year}-01-01`, to: today.slice(0, 4) === year ? today : `${year}-12-31` }
}
