import { useEffect, useMemo, useState } from 'react'
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
  SPENDING_CATEGORIES,
  SPENDING_SUMMARY,
  SpendingByDay,
  SpendingCategory,
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
import {
  TransactionFilters,
  lastDayOfMonth,
  monthRange,
  spendingMonthFromSearch,
  transactionsPath,
} from './financeFilters'
import { SpendingGroupBy, SummaryAmounts, spendingLines, spendingTotals } from './spendingLines'
import { useSpendingCategoryDisplayName } from './spendingCategoryName'
import { RING_SLICE_COUNT, SpendingRing, foldIntoOther, ringSliceClass } from './spendingRing'

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
  // On the headline's line rather than in the panel's heading: there it
  // sat alone at the right of an empty band above the chart on a phone.
  const monthPicker = (
    <label className="finance-month">
      <span>{t('finance.month')}</span>
      <input
        type="month"
        value={month}
        max={currentMonth}
        onChange={(event) => event.target.value && onSelectMonth(event.target.value)}
      />
    </label>
  )
  return (
    <SettingsSection card title={t('finance.spendingByMonthTitle')} description={t('finance.spendingByMonthHint')}>
      <ErrorMessage error={error} />
      {loading && !data ? <Loading /> : null}
      {flow && !hasSpending ? (
        <>
          <div className="finance-month-alone">{monthPicker}</div>
          <SettingsEmpty>{t('finance.noCashFlow')}</SettingsEmpty>
        </>
      ) : null}
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
          headAction={monthPicker}
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
  // A slot for every day of the chosen month, whether or not it has come
  // yet, lined up by the day of the month with the month before: the
  // columns stop at today in the month in progress, and the line stops at
  // the end of a shorter month before, or is cut at this month's last day.
  const dayCount = Number(lastDayOfMonth(month).slice(8, 10))
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
  // A month that is over has nowhere left to head: it is the whole month,
  // with no projection and no fixed charges still to come.
  const isPast = month < personMonth()
  return (
    <SettingsSection
      card
      title={t('finance.budgetStatusTitle')}
      description={
        status
          ? isPast
            ? t('finance.budgetStatusHintPast', { month: monthLabel(month, 'long') })
            : t('finance.budgetStatusHint', { day: status.dayOfMonth, days: status.daysInMonth })
          : undefined
      }
    >
      <ErrorMessage error={error} />
      {loading && !data ? <Loading /> : null}
      {status && rows.length === 0 ? <SettingsEmpty>{t('finance.noBudgetStatus')}</SettingsEmpty> : null}
      {rows.length > 0 ? (
        <div className="finance-budget-bars">
          {rows.map((row) => (
            <BudgetStatusRow key={row.spendingCategoryId} row={row} isPast={isPast} />
          ))}
        </div>
      ) : null}
    </SettingsSection>
  )
}

function BudgetStatusRow({ row, isPast }: { row: SpendingCategoryBudgetStatus; isPast: boolean }) {
  const { t } = useTranslation()
  const words = useFinanceWords()
  const categoryName = useSpendingCategoryDisplayName()
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
        <strong>{categoryName(row.spendingCategoryName)}</strong>
        <Tag value={words.budgetPace(row.budgetPace)} tone={tone} />
        <span className="finance-budget-row-said">{said}</span>
      </div>
      <MeterBar
        fraction={budget > 0 ? spent / budget : 0}
        tone={tone}
        label={said}
        marker={budget > 0 && !isPast ? projected / budget : null}
      />
      {isPast ? null : (
        <div className="muted finance-budget-row-detail">
          {t('finance.projected', { amount: formatMoney(projected, row.currencyCode) })}
          {' · '}
          {t('finance.sameDayLastMonth', {
            amount: formatMoney(amountOf(row.spendingBySameDayLastMonthAmount), row.currencyCode),
          })}
          {fixedDue > 0
            ? ` · ${t('finance.fixedChargesDue', { amount: formatMoney(fixedDue, row.currencyCode) })}`
            : ''}
        </div>
      )}
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
const GROUP_BY: SpendingGroupBy[] = ['spendingCategory', 'merchant', 'financeAccount', 'providerCategory']

// groupTransactionFilters is how a group's transactions are found on the
// Transactions section, over the month's days: a spending category (or
// none), a finance account, or a merchant's words. The provider category
// is not a filter that section offers, so its groups link nowhere.
function groupTransactionFilters(
  groupBy: SpendingGroupBy,
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

// What went where in the month, counted as the chart above counts it (see
// spendingLines), so the table's total is the month's headline: one amount
// a group, what it spent, and how many transactions it holds.
// How many groups a long grouping shows before "Show all": a month has
// dozens of merchants, and the few at the top are what the table is read
// for.
const SHORT_GROUP_COUNT = 20

function SpendingSummaryPanel({ month }: { month: string }) {
  const { t } = useTranslation()
  const categoryName = useSpendingCategoryDisplayName()
  const [groupBy, setGroupBy] = useState<SpendingGroupBy>('spendingCategory')
  const [isShowingAll, setIsShowingAll] = useState(false)
  const [highlightedKey, setHighlightedKey] = useState<string | null>(null)
  useEffect(() => setIsShowingAll(false), [groupBy, month])
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
  // Which spending categories are income, whose money in is not a refund.
  const categories = useQuery(() => graphql<{ SpendingCategories: SpendingCategory[] }>(SPENDING_CATEGORIES), [], {
    refresh: false,
  })
  const summary = categories.data ? data?.FinanceSpendingSummary : undefined
  const incomeSpendingCategoryIds = useMemo(
    () =>
      new Set(
        (categories.data?.SpendingCategories ?? [])
          .filter((category) => category.isIncome)
          .map((category) => category.id),
      ),
    [categories.data],
  )
  // Each currency as it was spent, and, where there is a reporting
  // currency, the same groups converted into it with the transactions each
  // counted across its currencies.
  const currencyLines = useMemo(
    () => (summary ? spendingLines(summary.spendingSummaryRows, groupBy, incomeSpendingCategoryIds) : []),
    [summary, groupBy, incomeSpendingCategoryIds],
  )
  const lines = useMemo(() => {
    if (!summary) return []
    if (!summary.reportingCurrencyCode) return currencyLines
    const counts = new Map<string, number>()
    for (const row of summary.spendingSummaryRows) {
      counts.set(row.groupKey, (counts.get(row.groupKey) ?? 0) + row.financeTransactionCount)
    }
    const converted: SummaryAmounts[] = summary.convertedSpendingSummaryRows.map((row) => ({
      ...row,
      currencyCode: summary.reportingCurrencyCode ?? '',
      financeTransactionCount: counts.get(row.groupKey) ?? 0,
    }))
    return spendingLines(converted, groupBy, incomeSpendingCategoryIds)
  }, [summary, currencyLines, groupBy, incomeSpendingCategoryIds])
  const reportingTotal = summary?.reportingCurrencyCode ? spendingTotals(lines)[0] : undefined
  const currencyTotals = spendingTotals(currencyLines)
  const groupLabel = (value: SpendingGroupBy) => t(`finance.groupBy.${value}` as 'finance.groupBy.merchant')
  // A spending category's name as the reader reads it; merchants, accounts
  // and provider categories as they came.
  const lineName = (label: string) =>
    !label ? t('finance.uncategorized') : groupBy === 'spendingCategory' ? categoryName(label) : label
  // The ring is the table's own numbers, where they can be added up:
  // grouped by category, in the reporting currency.
  const slices =
    groupBy === 'spendingCategory' && summary?.reportingCurrencyCode
      ? foldIntoOther(
          lines.map((line) => ({ key: line.key, label: lineName(line.label), amount: line.spendingAmount })),
          RING_SLICE_COUNT,
        ).map((slice) =>
          slice.isOther ? { ...slice, label: t('finance.otherCategories', { count: slice.foldedCount }) } : slice,
        )
      : []
  // The table is the ring's legend: each row carries its slice's swatch,
  // the rows folded into "Other" that slice's, and its share of the month.
  const sliceIndexes = new Map(slices.map((slice, index) => [slice.key, index]))
  const otherIndex = slices.findIndex((slice) => slice.isOther)
  const sliceOf = (key: string): { className: string; sliceKey: string } | null => {
    const index = sliceIndexes.get(key)
    if (index !== undefined) return { className: ringSliceClass(slices[index], index), sliceKey: key }
    return otherIndex >= 0 ? { className: 'other', sliceKey: slices[otherIndex].key } : null
  }
  const percent = new Intl.NumberFormat(undefined, { style: 'percent', maximumFractionDigits: 0 })
  const isLongGrouping = groupBy === 'merchant' || groupBy === 'providerCategory'
  const shownLines = isLongGrouping && !isShowingAll ? lines.slice(0, SHORT_GROUP_COUNT) : lines

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
            onChange={(value) => setGroupBy(value as SpendingGroupBy)}
          />
        </label>
      }
    >
      <ErrorMessage error={error || categories.error} />
      {(loading && !data) || (categories.loading && !categories.data) ? <Loading /> : null}
      {summary && lines.length === 0 ? <SettingsEmpty>{t('finance.noSpending')}</SettingsEmpty> : null}
      {slices.length > 0 && summary?.reportingCurrencyCode ? (
        <SpendingRing
          slices={slices}
          currency={summary.reportingCurrencyCode}
          label={t('finance.ringLabel', { month: monthLabel(month, 'long') })}
          totalLabel={t('finance.spending')}
          totalAmount={reportingTotal?.spendingAmount}
          highlightedKey={highlightedKey}
        />
      ) : null}
      {lines.length > 0 ? (
        <div className="table-wrap">
          <table className="numbers-table finance-table finance-summary-table">
            <thead>
              <tr>
                <th>{groupLabel(groupBy)}</th>
                <th className="numeric">{t('finance.spending')}</th>
                <th className="numeric optional">{t('finance.transactionCount')}</th>
              </tr>
            </thead>
            <tbody>
              {shownLines.map((line) => {
                const filters = groupTransactionFilters(groupBy, line.groupKey, range)
                const name = lineName(line.label)
                const slice = slices.length > 0 ? sliceOf(line.key) : null
                const fraction =
                  slice && reportingTotal && reportingTotal.spendingAmount > 0
                    ? line.spendingAmount / reportingTotal.spendingAmount
                    : null
                // A sliver is "<1%" rather than a "0%" that reads as nothing.
                const share =
                  fraction === null
                    ? null
                    : fraction > 0 && fraction < 0.005
                      ? `<${percent.format(0.01)}`
                      : percent.format(fraction)
                const highlight = slice
                  ? { onPointerEnter: () => setHighlightedKey(slice.sliceKey), onPointerLeave: () => setHighlightedKey(null) }
                  : {}
                return (
                  <tr key={line.key} {...highlight}>
                    {/* The name gives way, with an ellipsis and the whole
                        of it on hover, so the amount stays in sight on a
                        phone however long a merchant's name is. */}
                    <td className="finance-group-cell">
                      {slice ? <i className={`spending-ring-swatch ${slice.className}`} aria-hidden="true" /> : null}
                      {filters ? (
                        <Link
                          className="finance-group-name finance-group-link"
                          to={transactionsPath(filters)}
                          title={t('finance.openTransactions', { name })}
                          onFocus={slice ? () => setHighlightedKey(slice.sliceKey) : undefined}
                          onBlur={slice ? () => setHighlightedKey(null) : undefined}
                        >
                          {name}
                        </Link>
                      ) : (
                        <span className="finance-group-name" title={name}>
                          {name}
                        </span>
                      )}
                    </td>
                    <td className="numeric">
                      <Money amount={line.spendingAmount} currency={line.currencyCode} />
                      {share ? <span className="muted finance-share">{share}</span> : null}
                    </td>
                    <td className="numeric optional">{line.financeTransactionCount}</td>
                  </tr>
                )
              })}
            </tbody>
            <tfoot>
              {reportingTotal ? (
                <tr>
                  <th>{t('finance.totalIn', { currency: reportingTotal.currencyCode })}</th>
                  <th className="numeric">
                    <Money amount={reportingTotal.spendingAmount} currency={reportingTotal.currencyCode} />
                  </th>
                  <th className="numeric optional">{reportingTotal.financeTransactionCount}</th>
                </tr>
              ) : null}
              {/* Each currency as it was spent, where there was more than
                  one or no reporting currency to add them up in. */}
              {currencyTotals.length > 1 || !reportingTotal
                ? currencyTotals.map((total) => (
                    <tr key={total.currencyCode}>
                      <th>{t('finance.totalIn', { currency: total.currencyCode })}</th>
                      <th className="numeric">
                        <Money amount={total.spendingAmount} currency={total.currencyCode} />
                      </th>
                      <th className="numeric optional">{total.financeTransactionCount}</th>
                    </tr>
                  ))
                : null}
            </tfoot>
          </table>
        </div>
      ) : null}
      {/* The total above is the whole month's either way. */}
      {isLongGrouping && lines.length > SHORT_GROUP_COUNT ? (
        <div className="page-actions page-actions-end">
          <button type="button" className="link" onClick={() => setIsShowingAll((previous) => !previous)}>
            {isShowingAll
              ? t('finance.showTopGroups', { count: SHORT_GROUP_COUNT })
              : t('finance.showAllGroups', { count: lines.length })}
          </button>
        </div>
      ) : null}
      <UnconvertedNote currencyCodes={summary?.unconvertedCurrencyCodes} />
    </SettingsSection>
  )
}
