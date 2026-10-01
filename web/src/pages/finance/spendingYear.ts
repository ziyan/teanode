import { CashFlowMonth, amountOf, monthBefore } from './financeApi'

// The Spending section's years: what each adds up to, as the chart of
// months counts it, and the years and months there are to choose from.

// HISTORY_YEAR_COUNT is how many years, this one included, the Spending
// section reads cash flow over to find the years and months that have any.
// One answer a month for twenty years is a few hundred rows; a history
// longer than that starts at its twentieth year.
export const HISTORY_YEAR_COUNT = 20

// historyRange is the months the Spending section reads its history over:
// January twenty years back, this one included, to this month.
export function historyRange(currentMonth: string): { fromMonth: string; toMonth: string } {
  return { fromMonth: `${Number(currentMonth.slice(0, 4)) - HISTORY_YEAR_COUNT + 1}-01`, toMonth: currentMonth }
}

export type YearCashFlowTotals = { incomeAmount: number; spendingAmount: number; netAmount: number }

// yearCashFlowTotals is a year's income, spending and what was left, added
// up from its months as the server counted each: spending net of refunds,
// transfers and mirrored copies left out, in the reporting currency. Only
// the year's own months count, whatever else the answer holds.
export function yearCashFlowTotals(months: CashFlowMonth[], year: string): YearCashFlowTotals {
  const totals: YearCashFlowTotals = { incomeAmount: 0, spendingAmount: 0, netAmount: 0 }
  for (const month of months) {
    if (month.cashFlowMonth.slice(0, 4) !== year) continue
    totals.incomeAmount += amountOf(month.incomeAmount)
    totals.spendingAmount += amountOf(month.spendingAmount)
    totals.netAmount += amountOf(month.netAmount)
  }
  return totals
}

const hasCashFlow = (month: CashFlowMonth) => amountOf(month.incomeAmount) !== 0 || amountOf(month.spendingAmount) !== 0

// firstCashFlowMonth is the earliest month with any income or spending,
// or none when there is none.
export function firstCashFlowMonth(months: CashFlowMonth[]): string | null {
  let first: string | null = null
  for (const month of months) {
    if (hasCashFlow(month) && (first === null || month.cashFlowMonth < first)) first = month.cashFlowMonth
  }
  return first
}

export type YearCashFlow = { year: string } & YearCashFlowTotals

// cashFlowYears is every year from the first with any income or spending
// to this one, oldest first, each added up from its months: the years the
// chart of years draws. Just this year when nothing has come in or gone
// out yet.
export function cashFlowYears(months: CashFlowMonth[], currentYear: string): YearCashFlow[] {
  const first = firstCashFlowMonth(months)
  const firstYear = first && first.slice(0, 4) < currentYear ? Number(first.slice(0, 4)) : Number(currentYear)
  const years: YearCashFlow[] = []
  for (let year = firstYear; year <= Number(currentYear); year++) {
    years.push({ year: String(year), ...yearCashFlowTotals(months, String(year)) })
  }
  return years
}

// yearOptions is the years the year menu offers, newest first: the years
// with cash flow (cashFlowYears), and the one chosen when it is further
// back than those.
export function yearOptions(years: string[], chosenYear: string): string[] {
  const options = [...years].sort().reverse()
  if (!options.includes(chosenYear)) options.push(chosenYear)
  return options
}

// monthOptions is the months the month menu offers, newest first: this one
// back to the first with cash flow (a year of them when there is none
// yet), and the one chosen when it is further back than those.
export function monthOptions(firstMonth: string | null, currentMonth: string, chosenMonth: string): string[] {
  const oldest = firstMonth && firstMonth < currentMonth ? firstMonth : monthBefore(currentMonth, 11)
  const options: string[] = []
  for (let month = currentMonth; month >= oldest; month = monthBefore(month, 1)) options.push(month)
  if (!options.includes(chosenMonth) && chosenMonth < currentMonth) options.push(chosenMonth)
  return options
}

// yearStartLabel is the first of January the way the reader writes a day
// without its year, for "Jan 1 to today".
export function yearStartLabel(year: string): string {
  const day = new Date(`${year}-01-01T00:00:00`)
  if (Number.isNaN(day.getTime())) return `${year}-01-01`
  return day.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}
