import { useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { graphql } from '../../api'
import { ErrorMessage, Loading, Tag, formatMoney } from '../../components/common'
import { MeterBar } from '../../components/budgetBar'
import { SeriesChart } from '../../components/seriesChart'
import { Select } from '../../components/select'
import { SettingsEmpty, SettingsSection } from '../../components/settingsList'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import {
  BUDGET_STATUS,
  BudgetPace,
  BudgetStatus,
  CASH_FLOW,
  CashFlow,
  SPENDING_BY_DAY,
  SPENDING_SUMMARY,
  SpendingByDay,
  SpendingCategoryBudgetStatus,
  SpendingDay,
  SpendingSummary,
  amountOf,
  monthBefore,
  monthLabel,
  personMonth,
  personToday,
} from './financeApi'
import { Money, UnconvertedNote, compactMoney, useFinanceWords } from './financeCommon'
import { TransactionFilters, monthRange, spendingMonthFromSearch, transactionsPath } from './financeFilters'
import { RING_SLICE_COUNT, SpendingRing, foldIntoOther } from './spendingRing'

// The Spending section: a year of spending a bar a month, and under it the
// month chosen there: its spending day by day against the month before,
// each spending category against its budget, and what went where, each
// group opening its transactions. All of it converted into the reporting
// currency where there is an exchange rate, with what could not be
// converted named rather than quietly left out. The month is in the
// address, so coming Back from a category's transactions lands on it.
export function FinanceSpendingSection() {
  const [search, setSearch] = useSearchParams()
  const currentMonth = personMonth()
  const month = spendingMonthFromSearch(search, currentMonth)
  const selectMonth = (chosen: string) =>
    setSearch(chosen === currentMonth ? {} : { month: chosen }, { replace: true })
  return (
    <>
      <SpendingByMonthPanel month={month} currentMonth={currentMonth} onSelectMonth={selectMonth} />
      <SpendingByDayPanel month={month} />
      <BudgetStatusPanel month={month} />
      <SpendingSummaryPanel month={month} />
    </>
  )
}

// A bar a month for the twelve months to this one, the chosen month
// standing out, and under it that month's income, spending and what was
// left. A month further back than the chart reaches, chosen in the month
// field, moves the chart to the twelve months ending there.
function SpendingByMonthPanel({
  month,
  currentMonth,
  onSelectMonth,
}: {
  month: string
  currentMonth: string
  onSelectMonth: (month: string) => void
}) {
  const { t } = useTranslation()
  const toMonth = month >= monthBefore(currentMonth, 11) ? currentMonth : month
  const fromMonth = monthBefore(toMonth, 11)
  const { data, error, loading } = useQuery(
    () => graphql<{ CashFlow: CashFlow }>(CASH_FLOW, { fromMonth, toMonth }),
    [fromMonth, toMonth],
    { refresh: false },
  )
  const flow = data?.CashFlow
  const months = flow?.cashFlowMonths ?? []
  const currency = flow?.reportingCurrencyCode || 'USD'
  const chosen = months.find((candidate) => candidate.cashFlowMonth === month)
  const spentOf = (amount?: string) => Math.abs(amountOf(amount))
  const hasSpending = months.some((candidate) => amountOf(candidate.spendingAmount) !== 0)
  return (
    <SettingsSection
      card
      title={t('finance.spendingByMonthTitle')}
      description={t('finance.spendingByMonthHint')}
      action={
        <label className="shrink finance-month">
          <span>{t('finance.month')}</span>
          <input
            type="month"
            value={month}
            max={currentMonth}
            onChange={(event) => event.target.value && onSelectMonth(event.target.value)}
          />
        </label>
      }
    >
      <ErrorMessage error={error} />
      {loading && !data ? <Loading /> : null}
      {flow && !hasSpending ? <SettingsEmpty>{t('finance.noCashFlow')}</SettingsEmpty> : null}
      {hasSpending ? (
        <SeriesChart
          label={t('finance.spendingByMonthTitle')}
          keys={months.map((candidate) => candidate.cashFlowMonth)}
          keyLabel={(key) => monthLabel(key)}
          format={(value) => formatMoney(value, currency)}
          axisFormat={(value) => compactMoney(value, currency)}
          headline={formatMoney(spentOf(chosen?.spendingAmount), currency)}
          caption={
            month === currentMonth
              ? t('finance.spentSoFar')
              : t('finance.spentInMonth', { month: monthLabel(month, 'long') })
          }
          series={[
            {
              id: 'spending',
              label: t('finance.spending'),
              tone: 'output',
              shape: 'column',
              values: months.map((candidate) => spentOf(candidate.spendingAmount)),
            },
          ]}
          selectedKey={month}
          onSelectKey={onSelectMonth}
        />
      ) : null}
      {chosen ? (
        <p className="muted finance-month-flow">
          <span>
            {t('finance.income')} <strong>{formatMoney(amountOf(chosen.incomeAmount), currency)}</strong>
          </span>
          <span>
            {t('finance.spending')} <strong>{formatMoney(spentOf(chosen.spendingAmount), currency)}</strong>
          </span>
          <span>
            {t('finance.leftOver')} <strong>{formatMoney(amountOf(chosen.netAmount), currency)}</strong>
          </span>
        </p>
      ) : null}
      <UnconvertedNote currencyCodes={flow?.unconvertedCurrencyCodes} />
    </SettingsSection>
  )
}

function SpendingByDayPanel({ month }: { month: string }) {
  const { t } = useTranslation()
  return (
    <SettingsSection card title={t('finance.spendingTitle')} description={t('finance.spendingHint')}>
      <SpendingByDayChart month={month} />
    </SettingsSection>
  )
}

function previousMonth(month: string): string {
  return monthBefore(month, 1)
}

// This month against last, cumulative: the columns are this month so far,
// the line is where last month stood on the same day.
function SpendingByDayChart({ month }: { month: string }) {
  const { t } = useTranslation()
  const compareMonth = previousMonth(month)
  const { data, error, loading } = useQuery(
    () => graphql<{ SpendingByDay: SpendingByDay }>(SPENDING_BY_DAY, { month, compareMonth }),
    [month],
    { refresh: false },
  )
  const answer = data?.SpendingByDay
  const monthDays = answer?.monthDays ?? []
  const compareDays = answer?.compareMonthDays ?? []
  const currency = answer?.reportingCurrencyCode || 'USD'
  const isCurrent = month === personMonth()
  // A slot for every day either month has: the month so far (this month
  // stops at today) and the whole of the one it is compared with, lined up
  // by the day of the month.
  const dayCount = Math.max(monthDays.length, compareDays.length)
  const keys = Array.from({ length: dayCount }, (_, index) => String(index + 1))
  const cumulative = (days: SpendingDay[], index: number): number | null =>
    days[index] ? amountOf(days[index].cumulativeSpendingAmount) : null
  const spent = amountOf(monthDays[monthDays.length - 1]?.cumulativeSpendingAmount)
  const hasSpending = [...monthDays, ...compareDays].some((day) => amountOf(day.spendingAmount) !== 0)

  if (loading && !data) return <Loading />
  return (
    <>
      <ErrorMessage error={error} />
      {answer && !hasSpending ? <SettingsEmpty>{t('finance.noSpending')}</SettingsEmpty> : null}
      {hasSpending ? (
        <SeriesChart
          label={t('finance.spendingByDay')}
          keys={keys}
          keyLabel={(key) => key}
          format={(value) => formatMoney(value, currency)}
          axisFormat={(value) => compactMoney(value, currency)}
          headline={formatMoney(spent, currency)}
          caption={
            isCurrent ? t('finance.spentSoFar') : t('finance.spentInMonth', { month: monthLabel(month, 'long') })
          }
          series={[
            {
              id: 'month',
              label: monthLabel(month, 'long'),
              tone: 'output',
              shape: 'column',
              values: keys.map((_, index) => cumulative(monthDays, index)),
            },
            {
              id: 'compare',
              label: monthLabel(compareMonth, 'long'),
              tone: 'cached',
              shape: 'line',
              values: keys.map((_, index) => cumulative(compareDays, index)),
            },
          ]}
        />
      ) : null}
      <UnconvertedNote currencyCodes={answer?.unconvertedCurrencyCodes} />
    </>
  )
}

// paceTone is how a budget pace is coloured: room and on track are good,
// heading over is worth a look, over is over.
function paceTone(pace: BudgetPace): 'good' | 'warn' | 'bad' {
  if (pace === 'over') return 'bad'
  if (pace === 'at_risk') return 'warn'
  return 'good'
}

function BudgetStatusPanel({ month }: { month: string }) {
  const { t } = useTranslation()
  const { data, error, loading } = useQuery(
    () => graphql<{ BudgetStatus: BudgetStatus }>(BUDGET_STATUS, { month }),
    [month],
    { refresh: false },
  )
  const status = data?.BudgetStatus
  const rows = status?.spendingCategories ?? []
  return (
    <SettingsSection
      card
      title={t('finance.budgetStatusTitle')}
      description={
        status ? t('finance.budgetStatusHint', { day: status.dayOfMonth, days: status.daysInMonth }) : undefined
      }
    >
      <ErrorMessage error={error} />
      {loading && !data ? <Loading /> : null}
      {status && rows.length === 0 ? <SettingsEmpty>{t('finance.noBudgetStatus')}</SettingsEmpty> : null}
      {rows.length > 0 ? (
        <div className="finance-budget-bars">
          {rows.map((row) => (
            <BudgetStatusRow key={row.spendingCategoryId} row={row} />
          ))}
        </div>
      ) : null}
    </SettingsSection>
  )
}

function BudgetStatusRow({ row }: { row: SpendingCategoryBudgetStatus }) {
  const { t } = useTranslation()
  const words = useFinanceWords()
  const budget = amountOf(row.budgetAmount)
  const spent = amountOf(row.spendingAmount)
  const projected = amountOf(row.projectedAmount)
  const fixedDue = amountOf(row.fixedChargesDueAmount)
  const tone = paceTone(row.budgetPace)
  const said = t('finance.spentOfBudget', {
    spent: formatMoney(spent, row.currencyCode),
    budget: formatMoney(budget, row.currencyCode),
  })
  return (
    <div className="finance-budget-row">
      <div className="finance-budget-row-head">
        <strong>{row.spendingCategoryName}</strong>
        <Tag value={words.budgetPace(row.budgetPace)} tone={tone} />
        <span className="finance-budget-row-said">{said}</span>
      </div>
      <MeterBar
        fraction={budget > 0 ? spent / budget : 0}
        tone={tone}
        label={said}
        marker={budget > 0 ? projected / budget : null}
      />
      <div className="muted finance-budget-row-detail">
        {t('finance.projected', { amount: formatMoney(projected, row.currencyCode) })}
        {' · '}
        {t('finance.sameDayLastMonth', {
          amount: formatMoney(amountOf(row.spendingBySameDayLastMonthAmount), row.currencyCode),
        })}
        {fixedDue > 0 ? ` · ${t('finance.fixedChargesDue', { amount: formatMoney(fixedDue, row.currencyCode) })}` : ''}
      </div>
      {row.unconvertedSpending.length > 0 ? (
        <p className="muted field-hint">
          {t('finance.unconvertedSpending', {
            amounts: row.unconvertedSpending
              .map((unconverted) => formatMoney(amountOf(unconverted.amount), unconverted.currencyCode))
              .join(', '),
          })}
        </p>
      ) : null}
    </div>
  )
}

// The ways a month's spending is grouped. By month is not one of them: the
// section shows one month at a time, and the chart at its top is by month.
type GroupBy = 'spendingCategory' | 'merchant' | 'financeAccount' | 'providerCategory'
const GROUP_BY: GroupBy[] = ['spendingCategory', 'merchant', 'financeAccount', 'providerCategory']

// groupTransactionFilters is how a group's transactions are found on the
// Transactions section, over the month's days: a spending category (or
// none), a finance account, or a merchant's words. The provider category
// is not a filter that section offers, so its groups link nowhere.
function groupTransactionFilters(
  groupBy: GroupBy,
  groupKey: string,
  range: { from: string; to: string },
): Partial<TransactionFilters> | null {
  if (groupBy === 'spendingCategory') {
    return groupKey ? { ...range, spendingCategoryId: groupKey } : { ...range, isUncategorized: true }
  }
  if (groupBy === 'financeAccount') return groupKey ? { ...range, financeAccountId: groupKey } : null
  if (groupBy === 'merchant') return groupKey ? { ...range, text: groupKey } : null
  return null
}

function SpendingSummaryPanel({ month }: { month: string }) {
  const { t } = useTranslation()
  const [groupBy, setGroupBy] = useState<GroupBy>('spendingCategory')
  const range = monthRange(month, personToday())
  const { data, error, loading } = useQuery(
    () =>
      graphql<{ FinanceSpendingSummary: SpendingSummary }>(SPENDING_SUMMARY, {
        from: range.from,
        to: range.to,
        groupBy,
      }),
    [range.from, range.to, groupBy],
    { refresh: false },
  )
  const summary = data?.FinanceSpendingSummary
  // One line a group. In the reporting currency where there is one, with
  // how many transactions each group counted across its currencies; where
  // there is none, a line a group and currency, never added together.
  const lines = useMemo(() => {
    if (!summary) return []
    const counts = new Map<string, number>()
    for (const row of summary.spendingSummaryRows) {
      counts.set(row.groupKey, (counts.get(row.groupKey) ?? 0) + row.financeTransactionCount)
    }
    const converted = summary.reportingCurrencyCode
      ? summary.convertedSpendingSummaryRows.map((row) => ({
          key: row.groupKey,
          groupKey: row.groupKey,
          label: row.groupLabel || row.groupKey,
          currencyCode: summary.reportingCurrencyCode ?? '',
          moneyOut: row.moneyOut,
          moneyIn: row.moneyIn,
          count: counts.get(row.groupKey) ?? 0,
        }))
      : summary.spendingSummaryRows.map((row) => ({
          key: `${row.groupKey}:${row.currencyCode}`,
          groupKey: row.groupKey,
          label: row.groupLabel || row.groupKey,
          currencyCode: row.currencyCode,
          moneyOut: row.moneyOut,
          moneyIn: row.moneyIn,
          count: row.financeTransactionCount,
        }))
    return converted.sort((left, right) => amountOf(right.moneyOut) - amountOf(left.moneyOut))
  }, [summary])
  const groupLabel = (value: GroupBy) => t(`finance.groupBy.${value}` as 'finance.groupBy.merchant')
  const totals = summary?.currencyTotals ?? []
  const lineName = (label: string) => label || t('finance.uncategorized')
  // The ring is money out by spending category, so only where it can be
  // added up: grouped by category, in the reporting currency.
  const slices =
    groupBy === 'spendingCategory' && summary?.reportingCurrencyCode
      ? foldIntoOther(
          lines.map((line) => ({ key: line.key, label: lineName(line.label), amount: amountOf(line.moneyOut) })),
          RING_SLICE_COUNT,
        ).map((slice) =>
          slice.isOther ? { ...slice, label: t('finance.otherCategories', { count: slice.foldedCount }) } : slice,
        )
      : []

  return (
    <SettingsSection
      card
      title={t('finance.summaryTitle')}
      description={t('finance.summaryHint', { month: monthLabel(month, 'long') })}
      action={
        <label className="shrink finance-group-by">
          <span>{t('finance.groupByLabel')}</span>
          <Select
            block
            value={groupBy}
            label={t('finance.groupByLabel')}
            options={GROUP_BY.map((value) => ({ value, label: groupLabel(value) }))}
            onChange={(value) => setGroupBy(value as GroupBy)}
          />
        </label>
      }
    >
      <ErrorMessage error={error} />
      {loading && !data ? <Loading /> : null}
      {summary && lines.length === 0 ? <SettingsEmpty>{t('finance.noSpending')}</SettingsEmpty> : null}
      {slices.length > 0 && summary?.reportingCurrencyCode ? (
        <SpendingRing
          slices={slices}
          currency={summary.reportingCurrencyCode}
          label={t('finance.ringLabel', { month: monthLabel(month, 'long') })}
          totalLabel={t('finance.moneyOut')}
        />
      ) : null}
      {lines.length > 0 ? (
        <div className="table-wrap">
          <table className="numbers-table finance-table">
            <thead>
              <tr>
                <th>{groupLabel(groupBy)}</th>
                <th className="numeric">{t('finance.moneyOut')}</th>
                <th className="numeric optional">{t('finance.moneyIn')}</th>
                <th className="numeric optional">{t('finance.transactionCount')}</th>
              </tr>
            </thead>
            <tbody>
              {lines.map((line) => {
                const filters = groupTransactionFilters(groupBy, line.groupKey, range)
                const name = lineName(line.label)
                return (
                  <tr key={line.key}>
                    <td>
                      {filters ? (
                        <Link
                          className="finance-group-link"
                          to={transactionsPath(filters)}
                          title={t('finance.openTransactions', { name })}
                        >
                          {name}
                        </Link>
                      ) : (
                        name
                      )}
                    </td>
                    <td className="numeric">
                      <Money amount={line.moneyOut} currency={line.currencyCode} />
                    </td>
                    <td className="numeric optional">
                      <Money amount={line.moneyIn} currency={line.currencyCode} />
                    </td>
                    <td className="numeric optional">{line.count}</td>
                  </tr>
                )
              })}
            </tbody>
            <tfoot>
              {summary?.reportingCurrencyCode ? (
                <tr>
                  <th>{t('finance.totalIn', { currency: summary.reportingCurrencyCode })}</th>
                  <th className="numeric">
                    <Money amount={summary.convertedMoneyOut} currency={summary.reportingCurrencyCode} />
                  </th>
                  <th className="numeric optional">
                    <Money amount={summary.convertedMoneyIn} currency={summary.reportingCurrencyCode} />
                  </th>
                  <th className="optional" />
                </tr>
              ) : null}
              {/* Each currency as it was spent, where there was more than
                  one or no reporting currency to add them up in. */}
              {totals.length > 1 || !summary?.reportingCurrencyCode
                ? totals.map((total) => (
                    <tr key={total.currencyCode}>
                      <th>{t('finance.totalIn', { currency: total.currencyCode })}</th>
                      <th className="numeric">
                        <Money amount={total.moneyOut} currency={total.currencyCode} />
                      </th>
                      <th className="numeric optional">
                        <Money amount={total.moneyIn} currency={total.currencyCode} />
                      </th>
                      <th className="numeric optional">{total.financeTransactionCount}</th>
                    </tr>
                  ))
                : null}
            </tfoot>
          </table>
        </div>
      ) : null}
      <UnconvertedNote currencyCodes={summary?.unconvertedCurrencyCodes} />
    </SettingsSection>
  )
}
