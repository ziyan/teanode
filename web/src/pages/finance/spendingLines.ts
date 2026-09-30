import { amountOf } from './financeApi'

// What a spending summary's groups spent, counted the way the month chart,
// the day by day chart and the budgets count it, so the table's total is
// the month's headline: refunds come off what a category spent, money that
// came in under an income category is not spending at all, and money in
// with no spending category is not taken off anything.

export type SpendingGroupBy = 'spendingCategory' | 'merchant' | 'financeAccount'

// A group's money as the server summarizes it, both figures positive.
export type SummaryAmounts = {
  groupKey: string
  groupLabel: string
  currencyCode: string
  moneyOut: string
  moneyIn: string
  financeTransactionCount: number
}

export type SpendingLine = {
  key: string
  groupKey: string
  label: string
  currencyCode: string
  spendingAmount: number
  financeTransactionCount: number
}

// spendingLines turns the summary's groups into what each spent, largest
// first. By spending category: money out less money in for a category
// that is not income, money out alone for no spending category, and income
// categories left out. By anything else, money out less money in, keeping
// only the groups that spent something: an employer paying a salary, or a
// fund paying a dividend, is not a place money went.
export function spendingLines(
  rows: SummaryAmounts[],
  groupBy: SpendingGroupBy,
  incomeSpendingCategoryIds: Set<string>,
): SpendingLine[] {
  const lines: SpendingLine[] = []
  for (const row of rows) {
    const moneyOut = amountOf(row.moneyOut)
    const moneyIn = amountOf(row.moneyIn)
    if (groupBy === 'spendingCategory' && row.groupKey && incomeSpendingCategoryIds.has(row.groupKey)) continue
    const isNet = groupBy !== 'spendingCategory' || row.groupKey !== ''
    // Rounded to the four places amounts are kept in, so a refund that
    // cancels a purchase is nothing rather than a float's leftover.
    const spendingAmount = Math.round((isNet ? moneyOut - moneyIn : moneyOut) * 10000) / 10000
    if (groupBy === 'spendingCategory' ? spendingAmount === 0 : spendingAmount <= 0) continue
    lines.push({
      key: `${row.groupKey}:${row.currencyCode}`,
      groupKey: row.groupKey,
      label: row.groupLabel || row.groupKey,
      currencyCode: row.currencyCode,
      spendingAmount,
      financeTransactionCount: row.financeTransactionCount,
    })
  }
  return lines.sort((left, right) => right.spendingAmount - left.spendingAmount)
}

// spendingTotals adds the lines up a currency at a time, never across.
export function spendingTotals(
  lines: SpendingLine[],
): { currencyCode: string; spendingAmount: number; financeTransactionCount: number }[] {
  const totals = new Map<string, { currencyCode: string; spendingAmount: number; financeTransactionCount: number }>()
  for (const line of lines) {
    const total = totals.get(line.currencyCode) ?? {
      currencyCode: line.currencyCode,
      spendingAmount: 0,
      financeTransactionCount: 0,
    }
    total.spendingAmount += line.spendingAmount
    total.financeTransactionCount += line.financeTransactionCount
    totals.set(line.currencyCode, total)
  }
  return [...totals.values()].sort((left, right) => left.currencyCode.localeCompare(right.currencyCode))
}
