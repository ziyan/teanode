import { formatMoney } from '../../components/common'
import type { SelectOption } from '../../components/select'
import { IncomePace, SavingPace, SavingSummary, SpendingCategory, amountOf } from './financeApi'

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
// is heading as the mark, which a month that is over does not have. None
// when the budgets expect nothing saved, since a share of nothing is not
// a bar anybody can read.
export function savingMeter(
  summary: SavingSummary,
  isPast: boolean,
): { fraction: number; marker: number | null } | null {
  const expected = amountOf(summary.expectedSavingAmount)
  if (!(expected > 0)) return null
  return {
    fraction: Math.max(0, amountOf(summary.savingAmount) / expected),
    marker: isPast ? null : Math.max(0, amountOf(summary.projectedSavingAmount) / expected),
  }
}
