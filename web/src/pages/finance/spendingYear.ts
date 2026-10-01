import { CashFlowMonth, amountOf } from './financeApi'

// The Spending section's year: what its twelve months add up to, as the
// chart of months counts them, and the years there are to choose from.

// yearMonths is the twelve months of a year, 2006-01 to 2006-12.
export function yearMonths(year: string): string[] {
  return Array.from({ length: 12 }, (_, index) => `${year}-${String(index + 1).padStart(2, '0')}`)
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

// yearOptions is the years the year menu offers, newest first: this one
// and the count before it, and the one chosen when it is further back.
export function yearOptions(currentYear: string, chosenYear: string, count = 10): string[] {
  const newest = Number(currentYear)
  const years = Array.from({ length: count }, (_, index) => String(newest - index))
  if (!years.includes(chosenYear) && chosenYear < currentYear) years.push(chosenYear)
  return years
}

// yearBefore and yearAfter are the years either side of one.
export function yearBefore(year: string): string {
  return String(Number(year) - 1)
}

export function yearAfter(year: string): string {
  return String(Number(year) + 1)
}

// yearStartLabel is the first of January the way the reader writes a day
// without its year, for "Jan 1 to today".
export function yearStartLabel(year: string): string {
  const day = new Date(`${year}-01-01T00:00:00`)
  if (Number.isNaN(day.getTime())) return `${year}-01-01`
  return day.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}
