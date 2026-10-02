import { formatMoney } from '../../components/common'
import type { SelectOption } from '../../components/select'
import {
  Budget,
  IncomePace,
  SavingPace,
  SavingSummary,
  SpendingCategory,
  SpendingCategoryBudgetStatus,
  amountOf,
} from './financeApi'

// A budget on an income spending category is the income expected each
// month rather than a limit, so the Budgets list, the budget dialog and
// the month's budgets show income in a group of its own.

// splitByIncome is rows on spending categories apart from rows on income
// ones, each in the order given. A row whose spending category is gone
// stays with the spending, where it was before income budgets existed.
export function splitByIncome<Row extends { spendingCategoryId: string }>(
  rows: Row[],
  categories: SpendingCategory[],
): { spending: Row[]; income: Row[] } {
  const incomeIds = new Set(categories.filter((category) => category.isIncome).map((category) => category.id))
  return {
    spending: rows.filter((row) => !incomeIds.has(row.spendingCategoryId)),
    income: rows.filter((row) => incomeIds.has(row.spendingCategoryId)),
  }
}

// groupedCategoryOptions is the dialog's options in two groups, spending
// first and then income, each already sorted, with the group named on
// every option so the list can head each group.
export function groupedCategoryOptions(
  options: SelectOption[],
  categories: SpendingCategory[],
  groupLabels: { spending: string; income: string },
): SelectOption[] {
  const { spending, income } = splitByIncome(
    options.map((option) => ({ ...option, spendingCategoryId: option.value })),
    categories,
  )
  const strip = ({ value, label }: SelectOption) => ({ value, label })
  return [
    ...spending.map((option) => ({ ...strip(option), group: groupLabels.spending })),
    ...income.map((option) => ({ ...strip(option), group: groupLabels.income })),
  ]
}

// reachTone is how an income or saving pace is colored: falling short is
// worth a look, on track and ahead are good.
export function reachTone(pace: IncomePace | SavingPace): 'good' | 'warn' {
  return pace === 'behind' ? 'warn' : 'good'
}

// formatSigned is an amount with a plus in front when it is above zero, so
// a difference reads as more or less rather than as an amount.
export function formatSigned(amount: number, currency: string): string {
  const formatted = formatMoney(amount, currency)
  return amount > 0 ? `+${formatted}` : formatted
}

// savingMeter is the month's saving against the saving its budgets
// expect: how much of it is saved so far as the bar, and where the month
// is heading as the forecast band, which a month that is over does not
// have. None when the budgets expect nothing saved, since a share of
// nothing is not a bar anybody can read.
export function savingMeter(
  summary: SavingSummary,
  isPast: boolean,
): { fraction: number; forecast: number | null } | null {
  const expected = amountOf(summary.expectedSavingAmount)
  if (!(expected > 0)) return null
  return {
    fraction: Math.max(0, amountOf(summary.savingAmount) / expected),
    forecast: isPast ? null : Math.max(0, amountOf(summary.projectedSavingAmount) / expected),
  }
}

// spendingForecastParts is a spending budget's projection as the sum it
// is: what was spent, the repeat charges still to come, and the rest of
// the month at this month's pace, which is whatever the projection holds
// beyond the first two. Never below zero: a refund can leave the spending
// so far under what its repeat charges took.
export function spendingForecastParts(
  row: Pick<SpendingCategoryBudgetStatus, 'spendingAmount' | 'fixedChargesDueAmount' | 'projectedAmount'>,
): { spentAmount: number; repeatChargesAmount: number; atPaceAmount: number } {
  const spentAmount = amountOf(row.spendingAmount)
  const repeatChargesAmount = amountOf(row.fixedChargesDueAmount)
  const restAmount = amountOf(row.projectedAmount) - spentAmount - repeatChargesAmount
  // Amounts come as four-place decimals; what is left of a subtraction
  // under a hundredth of a cent is rounding, not spending.
  return { spentAmount, repeatChargesAmount, atPaceAmount: restAmount > 0.00005 ? restAmount : 0 }
}

// budgetAmountSince is the month a budget's current amount began: the
// effective month of the given row, or of the earliest row before it in an
// unbroken run with the same amount and currency. Setting a budget again
// from an earlier month with the same amount adds a row before the one in
// force, and the list would otherwise name the later, redundant row's month
// as the start.
export function budgetAmountSince(budget: Budget, budgets: Budget[]): string {
  const ownRows = budgets
    .filter((candidate) => candidate.spendingCategoryId === budget.spendingCategoryId)
    .filter((candidate) => candidate.effectiveFrom <= budget.effectiveFrom)
    .sort((left, right) => right.effectiveFrom.localeCompare(left.effectiveFrom))
  let since = budget.effectiveFrom
  for (const row of ownRows) {
    if (row.currencyCode !== budget.currencyCode || amountOf(row.monthlyAmount) !== amountOf(budget.monthlyAmount)) break
    since = row.effectiveFrom
  }
  return since
}
