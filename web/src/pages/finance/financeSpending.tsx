import { useMemo, useState } from 'react'

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
  isoDay,
  isoMonth,
  monthLabel,
  monthsBefore,
} from './financeApi'
import { Money, UnconvertedNote, useFinanceWords } from './financeCommon'

// The Spending section: what went where over a range, this month's spending
// day by day against last month's, each spending category against its
// budget, and twelve months of income against spending. All of it converted
// into the reporting currency where there is an exchange rate, with what
// could not be converted named rather than quietly left out.
export function FinanceSpendingSection() {
  const { t } = useTranslation()
  const [month, setMonth] = useState(() => isoMonth(new Date()))
  return (
    <>
      <SettingsSection
        card
        title={t('finance.spendingTitle')}
        description={t('finance.spendingHint')}
        action={
          <label className="shrink finance-month">
            <span>{t('finance.month')}</span>
            <input
              type="month"
              value={month}
              max={isoMonth(new Date())}
              onChange={(event) => event.target.value && setMonth(event.target.value)}
            />
          </label>
        }
      >
        <SpendingByDayChart month={month} />
      </SettingsSection>
      <BudgetStatusPanel month={month} />
      <SpendingSummaryPanel />
      <CashFlowPanel />
    </>
  )
}

function previousMonth(month: string): string {
  const [year, number] = month.split('-').map(Number)
  return isoMonth(new Date(year, number - 2, 1))
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
  const isCurrent = month === isoMonth(new Date())
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

type GroupBy = 'spendingCategory' | 'merchant' | 'month' | 'financeAccount' | 'providerCategory'
const GROUP_BY: GroupBy[] = ['spendingCategory', 'merchant', 'month', 'financeAccount', 'providerCategory']

function SpendingSummaryPanel() {
  const { t } = useTranslation()
  const [groupBy, setGroupBy] = useState<GroupBy>('spendingCategory')
  const [from, setFrom] = useState(() => isoDay(monthsBefore(new Date(), 0)))
  const [to, setTo] = useState(() => isoDay(new Date()))
  const { data, error, loading } = useQuery(
    () =>
      graphql<{ FinanceSpendingSummary: SpendingSummary }>(SPENDING_SUMMARY, {
        from: from || undefined,
        to: to || undefined,
        groupBy,
      }),
    [from, to, groupBy],
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
          label: row.groupLabel || row.groupKey,
          currencyCode: summary.reportingCurrencyCode ?? '',
          moneyOut: row.moneyOut,
          moneyIn: row.moneyIn,
          count: counts.get(row.groupKey) ?? 0,
        }))
      : summary.spendingSummaryRows.map((row) => ({
          key: `${row.groupKey}:${row.currencyCode}`,
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

  return (
    <SettingsSection card title={t('finance.summaryTitle')} description={t('finance.summaryHint')}>
      <div className="row finance-filters">
        <label>
          <span>{t('finance.from')}</span>
          <input type="date" value={from} max={to || undefined} onChange={(event) => setFrom(event.target.value)} />
        </label>
        <label>
          <span>{t('finance.to')}</span>
          <input type="date" value={to} min={from || undefined} onChange={(event) => setTo(event.target.value)} />
        </label>
        <label>
          <span>{t('finance.groupByLabel')}</span>
          <Select
            block
            value={groupBy}
            label={t('finance.groupByLabel')}
            options={GROUP_BY.map((value) => ({ value, label: groupLabel(value) }))}
            onChange={(value) => setGroupBy(value as GroupBy)}
          />
        </label>
      </div>
      <ErrorMessage error={error} />
      {loading && !data ? <Loading /> : null}
      {summary && lines.length === 0 ? <SettingsEmpty>{t('finance.noSpending')}</SettingsEmpty> : null}
      {lines.length > 0 ? (
        <div className="table-wrap">
          <table className="numbers-table finance-table">
            <thead>
              <tr>
                <th>{groupLabel(groupBy)}</th>
                <th className="numeric">{t('finance.moneyOut')}</th>
                <th className="numeric">{t('finance.moneyIn')}</th>
                <th className="numeric">{t('finance.transactionCount')}</th>
              </tr>
            </thead>
            <tbody>
              {lines.map((line) => (
                <tr key={line.key}>
                  <td>{line.label || t('finance.uncategorized')}</td>
                  <td className="numeric">
                    <Money amount={line.moneyOut} currency={line.currencyCode} />
                  </td>
                  <td className="numeric">
                    <Money amount={line.moneyIn} currency={line.currencyCode} />
                  </td>
                  <td className="numeric">{line.count}</td>
                </tr>
              ))}
            </tbody>
            <tfoot>
              {summary?.reportingCurrencyCode ? (
                <tr>
                  <th>{t('finance.totalIn', { currency: summary.reportingCurrencyCode })}</th>
                  <th className="numeric">
                    <Money amount={summary.convertedMoneyOut} currency={summary.reportingCurrencyCode} />
                  </th>
                  <th className="numeric">
                    <Money amount={summary.convertedMoneyIn} currency={summary.reportingCurrencyCode} />
                  </th>
                  <th />
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
                      <th className="numeric">
                        <Money amount={total.moneyIn} currency={total.currencyCode} />
                      </th>
                      <th className="numeric">{total.financeTransactionCount}</th>
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

// Twelve months of income against spending, the months as columns side by
// side and what was left over as a line through them.
function CashFlowPanel() {
  const { t } = useTranslation()
  const [range] = useState(() => {
    const now = new Date()
    return { fromMonth: isoMonth(monthsBefore(now, 11)), toMonth: isoMonth(now) }
  })
  const { data, error, loading } = useQuery(() => graphql<{ CashFlow: CashFlow }>(CASH_FLOW, range), [], {
    refresh: false,
  })
  const flow = data?.CashFlow
  const months = flow?.cashFlowMonths ?? []
  const currency = flow?.reportingCurrencyCode || 'USD'
  const net = months.reduce((total, month) => total + amountOf(month.netAmount), 0)
  return (
    <SettingsSection card title={t('finance.cashFlowTitle')} description={t('finance.cashFlowHint')}>
      <ErrorMessage error={error} />
      {loading && !data ? <Loading /> : null}
      {flow && months.length === 0 ? <SettingsEmpty>{t('finance.noCashFlow')}</SettingsEmpty> : null}
      {months.length > 0 ? (
        <SeriesChart
          label={t('finance.cashFlowTitle')}
          keys={months.map((month) => month.cashFlowMonth)}
          keyLabel={(key) => monthLabel(key)}
          format={(value) => formatMoney(value, currency)}
          headline={formatMoney(net, currency)}
          caption={t('finance.cashFlowNet')}
          series={[
            {
              id: 'income',
              label: t('finance.income'),
              tone: 'output',
              shape: 'column',
              values: months.map((month) => amountOf(month.incomeAmount)),
            },
            {
              id: 'spending',
              label: t('finance.spending'),
              tone: 'cached',
              shape: 'column',
              values: months.map((month) => Math.abs(amountOf(month.spendingAmount))),
            },
            {
              id: 'net',
              label: t('finance.net'),
              tone: 'input',
              shape: 'line',
              values: months.map((month) => amountOf(month.netAmount)),
            },
          ]}
        />
      ) : null}
      <UnconvertedNote currencyCodes={flow?.unconvertedCurrencyCodes} />
    </SettingsSection>
  )
}
