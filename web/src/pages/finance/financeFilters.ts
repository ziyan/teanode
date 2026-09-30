// The Finance page's filters as they are written in the address, so a
// month on the Spending section and a narrowed list of transactions can be
// linked to, shared, and gone back to.

// TransactionFilters narrow the Transactions section, one field for each
// of the finance transactions query's arguments it offers. Empty is no
// filter.
export type TransactionFilters = {
  from: string
  to: string
  financeAccountId: string
  spendingCategoryId: string
  text: string
  isUncategorized: boolean
}

export const NO_TRANSACTION_FILTERS: TransactionFilters = {
  from: '',
  to: '',
  financeAccountId: '',
  spendingCategoryId: '',
  text: '',
  isUncategorized: false,
}

const DAY_PATTERN = /^\d{4}-\d{2}-\d{2}$/
const MONTH_PATTERN = /^\d{4}-(0[1-9]|1[0-2])$/

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
  }
}

// searchFromTransactionFilters writes the filters into an address, leaving
// out the ones that are empty, so an unfiltered list has a bare address.
export function searchFromTransactionFilters(filters: Partial<TransactionFilters>): URLSearchParams {
  const search = new URLSearchParams()
  const names: (keyof Omit<TransactionFilters, 'isUncategorized'>)[] = [
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
// in the address when it is a month that has begun, otherwise this one.
export function spendingMonthFromSearch(search: URLSearchParams, currentMonth: string): string {
  const month = (search.get('month') ?? '').trim()
  return MONTH_PATTERN.test(month) && month <= currentMonth ? month : currentMonth
}
