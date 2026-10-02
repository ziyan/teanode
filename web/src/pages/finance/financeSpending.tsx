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
  IncomeCategoryBudgetStatus,
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
import { reachTone, spendingForecastParts } from './budgetGroups'
import { Money, UnconvertedNote, compactMoney, useFinanceWords } from './financeCommon'
import {
  SpendingPeriod,
  TransactionFilters,
  lastDayOfMonth,
  latestMonthOfYear,
  monthRange,
  searchFromSpendingPeriod,
  spendingPeriodFromSearch,
  transactionsPath,
  yearRange,
} from './financeFilters'
import { SavingSummaryPanel } from './financeSaving'
import { SpendingByYearPanel, SpendingPeriodPicker, useSpendingHistory } from './financeSpendingYear'
import { yearStartLabel } from './spendingYear'
import { ForecastDetail } from './forecastDetail'
import { SpendingGroupBy, SummaryAmounts, spendingLines, spendingTotals } from './spendingLines'
import { useSpendingCategoryDisplayName } from './spendingCategoryName'
import { RING_SLICE_COUNT, SpendingRing, foldIntoOther, ringSliceClass } from './spendingRing'

// The Spending section, a month or a year at a time. A month: a year of
// spending a bar a month, and under it the month chosen there: its
// spending day by day against the month before, each spending category
// against its budget, and what went where, each group opening its
// transactions. A year: every year's cash flow a group a year, and for the
// year chosen there its saving and budgets added up month by month as each
// was budgeted, and what went where over the whole of it. All of it
// converted into the reporting currency where there is an exchange rate,
// with what could not be converted named rather than quietly left out. The
// period is in the address, so coming Back from a category's transactions
// lands on it, and a year can be linked to. Month or Year and the period
// are one row above the panels, in the same place whichever is shown, so
// the control never moves as the panels under it change.
export function FinanceSpendingSection() {
  const { t } = useTranslation()
  const [search, setSearch] = useSearchParams()
  const currentMonth = personMonth()
  const period = spendingPeriodFromSearch(search, currentMonth)
  // The years history is Year mode's; Month mode reads it only once the
  // person reaches for the month menu.
  const [isHistoryWanted, setHistoryWanted] = useState(false)
  const history = useSpendingHistory(currentMonth, period.spendingPeriodKind === 'year' || isHistoryWanted)
  const selectPeriod = (chosen: SpendingPeriod, isKindChange: boolean) =>
    setSearch(searchFromSpendingPeriod(chosen, currentMonth), { replace: !isKindChange })
  const selectMonth = (month: string) =>
    selectPeriod({ spendingPeriodKind: 'month', month, year: month.slice(0, 4) }, period.spendingPeriodKind !== 'month')
  const selectYear = (year: string) =>
    selectPeriod({ spendingPeriodKind: 'year', year, month: latestMonthOfYear(year, currentMonth) }, false)
  const picker = (
    <div className="finance-period-bar">
      <SpendingPeriodPicker
        period={period}
        currentMonth={currentMonth}
        history={history}
        onSelectPeriod={selectPeriod}
        onWantHistory={() => setHistoryWanted(true)}
      />
    </div>
  )
  if (period.spendingPeriodKind === 'year') {
    const today = personToday()
    const isCurrentYear = period.year === currentMonth.slice(0, 4)
    const periodLabel = isCurrentYear
      ? t('finance.yearToDateLabel', { year: period.year, from: yearStartLabel(period.year) })
      : period.year
    return (
      <>
        {picker}
        <SpendingByYearPanel
          year={period.year}
          currentMonth={currentMonth}
          history={history}
          onSelectYear={selectYear}
        />
        <SavingSummaryPanel month={period.month} year={period.year} />
        <BudgetStatusPanel period={period} />
        <SpendingSummaryPanel range={yearRange(period.year, today)} periodLabel={periodLabel} />
      </>
    )
  }
  return (
    <>
      {picker}
      <SpendingByMonthPanel month={period.month} currentMonth={currentMonth} onSelectMonth={selectMonth} />
      <SpendingByDayPanel month={period.month} />
      <SavingSummaryPanel month={period.month} />
      <BudgetStatusPanel period={period} />
      <SpendingSummaryPanel
        range={monthRange(period.month, personToday())}
        periodLabel={monthLabel(period.month, 'long')}
      />
    </>
  )
}

// Cash flow for the twelve months to this one: income and spending side by
// side a month, what was left (income less spending, below zero in a month
// that spent more than came in) as a line through them, the chosen month
// standing out, and under it that month's three figures. A month further
// back than the chart reaches, chosen in the month menu, moves the chart to
// the twelve months ending there.
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
  // Spending as the server counts it, refunds taken off: a month whose
  // refunds outweigh what was spent is below zero, not spent.
  const spentOf = (amount?: string) => amountOf(amount)
  const hasSpending = months.some(
    (candidate) => amountOf(candidate.spendingAmount) !== 0 || amountOf(candidate.incomeAmount) !== 0,
  )
  return (
    <SettingsSection card title={t('finance.cashFlowByMonthTitle')} description={t('finance.cashFlowByMonthHint')}>
      <ErrorMessage error={error} />
      {loading && !data ? <Loading /> : null}
      {flow && !hasSpending ? <SettingsEmpty>{t('finance.noCashFlow')}</SettingsEmpty> : null}
      {hasSpending ? (
        <SeriesChart
          label={t('finance.cashFlowByMonthTitle')}
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
              id: 'income',
              label: t('finance.income'),
              tone: 'output',
              shape: 'column',
              values: months.map((candidate) => amountOf(candidate.incomeAmount)),
            },
            {
              id: 'spending',
              label: t('finance.spending'),
              tone: 'cached',
              shape: 'column',
              values: months.map((candidate) => spentOf(candidate.spendingAmount)),
            },
            {
              id: 'left',
              label: t('finance.leftOver'),
              tone: 'input',
              shape: 'line',
              values: months.map((candidate) => amountOf(candidate.netAmount)),
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
// the line is where last month stood on the same day. No headline of its
// own: what the month spent is the cash flow chart's, one card up.
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
  // A slot for every day of the chosen month, whether or not it has come
  // yet, lined up by the day of the month with the month before: the
  // columns stop at today in the month in progress, and the line stops at
  // the end of a shorter month before, or is cut at this month's last day.
  const dayCount = Number(lastDayOfMonth(month).slice(8, 10))
  const keys = Array.from({ length: dayCount }, (_, index) => String(index + 1))
  const cumulative = (days: SpendingDay[], index: number): number | null =>
    days[index] ? amountOf(days[index].cumulativeSpendingAmount) : null
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

// The budgets of the period: a month's, or a year's, each of a year's
// rows adding up the months that had its budget as it was then.
function BudgetStatusPanel({ period }: { period: SpendingPeriod }) {
  const { t } = useTranslation()
  const isYear = period.spendingPeriodKind === 'year'
  const variables = isYear ? { year: period.year } : { month: period.month }
  const asked = isYear ? `year ${period.year}` : `month ${period.month}`
  const {
    data: answered,
    error,
    loading,
  } = useQuery(
    () => graphql<{ BudgetStatus: BudgetStatus }>(BUDGET_STATUS, variables).then((answer) => ({ ...answer, asked })),
    [asked],
    { refresh: false },
  )
  // A month's rows under a year's heading would be wrong for a moment.
  const data = answered?.asked === asked ? answered : undefined
  const status = data?.BudgetStatus
  const rows = status?.spendingCategories ?? []
  const incomeRows = status?.incomeCategories ?? []
  // A month or a year that is over has nowhere left to head: it is the
  // whole of it, with no projection and no repeat charges still to come.
  const isPast = isYear ? period.year < personMonth().slice(0, 4) : period.month < personMonth()
  // The groups are headed only once there is income to tell apart.
  const hasBoth = rows.length > 0 && incomeRows.length > 0
  // The months most of a year's budgets were in force, named once in the
  // description; a row names its own only when they differ.
  const usualMonths = usualBudgetedMonths(isYear ? [...rows, ...incomeRows] : [])
  const description = !status
    ? undefined
    : isYear
      ? budgetStatusYearHint(t, period.year, isPast, usualMonths)
      : isPast
        ? t('finance.budgetStatusHintPast', { month: monthLabel(period.month, 'long') })
        : t('finance.budgetStatusHint', { day: status.dayOfMonth, days: status.daysInMonth })
  return (
    <SettingsSection
      card
      title={isYear ? t('finance.budgetStatusYearTitle') : t('finance.budgetStatusTitle')}
      description={description}
    >
      <ErrorMessage error={error} />
      {(loading || (answered && !error)) && !data ? <Loading /> : null}
      {status && rows.length === 0 && incomeRows.length === 0 ? (
        <SettingsEmpty>{t('finance.noBudgetStatus')}</SettingsEmpty>
      ) : null}
      {rows.length > 0 ? (
        <div className="finance-budget-group">
          {hasBoth ? <h4 className="finance-group-heading">{t('finance.spending')}</h4> : null}
          <div className="finance-budget-bars">
            {rows.map((row) => (
              <BudgetStatusRow
                key={row.spendingCategoryId}
                row={row}
                isPast={isPast}
                isYear={isYear}
                usualMonths={usualMonths}
              />
            ))}
          </div>
        </div>
      ) : null}
      {incomeRows.length > 0 ? (
        <div className="finance-budget-group">
          {hasBoth ? <h4 className="finance-group-heading">{t('finance.income')}</h4> : null}
          <div className="finance-budget-bars">
            {incomeRows.map((row) => (
              <IncomeStatusRow
                key={row.spendingCategoryId}
                row={row}
                isPast={isPast}
                isYear={isYear}
                usualMonths={usualMonths}
              />
            ))}
          </div>
        </div>
      ) : null}
    </SettingsSection>
  )
}

// IncomeStatusRow is an income budget: what came in against what was
// expected, the bar filling toward the month's (or the year's) expected
// income, and the band to where it is expected to end. Falling short is
// what is colored, not going past, so a band past the end has no tone of
// its own.
function IncomeStatusRow({
  row,
  isPast,
  isYear,
  usualMonths,
}: {
  row: IncomeCategoryBudgetStatus
  isPast: boolean
  isYear: boolean
  usualMonths: BudgetedMonths
}) {
  const { t } = useTranslation()
  const words = useFinanceWords()
  const categoryName = useSpendingCategoryDisplayName()
  const expected = amountOf(row.budgetAmount)
  const received = amountOf(row.incomeAmount)
  const projected = amountOf(row.projectedAmount)
  const tone = reachTone(row.incomePace)
  const said = t('finance.receivedOfExpected', {
    received: formatMoney(received, row.currencyCode),
    expected: formatMoney(expected, row.currencyCode),
  })
  const headingFor = t('finance.projected', { amount: formatMoney(projected, row.currencyCode) })
  return (
    <div className="finance-budget-row">
      <div className="finance-budget-row-head">
        <strong>{categoryName(row.spendingCategoryName)}</strong>
        <Tag value={words.incomePace(row.incomePace)} tone={tone} />
        <span className="finance-budget-row-said">{said}</span>
      </div>
      <MeterBar
        fraction={expected > 0 ? received / expected : 0}
        tone={tone}
        label={said}
        forecast={expected > 0 && !isPast ? projected / expected : null}
        forecastLabel={isPast ? undefined : headingFor}
      />
      {isYear ? <BudgetedMonthsNote months={row} usualMonths={usualMonths} /> : null}
      {isPast ? null : isYear ? (
        <ForecastDetail
          name={categoryName(row.spendingCategoryName)}
          line={
            <>
              {headingFor}
              {' · '}
              {t('finance.expectedByToday', {
                amount: formatMoney(amountOf(row.expectedByTodayAmount), row.currencyCode),
              })}
            </>
          }
          explanation={<p>{t('finance.forecastHowIncomeYear')}</p>}
        />
      ) : (
        <ForecastDetail
          name={categoryName(row.spendingCategoryName)}
          line={
            <>
              {headingFor}
              {' · '}
              {t('finance.expectedByToday', {
                amount: formatMoney(amountOf(row.expectedByTodayAmount), row.currencyCode),
              })}
              {' · '}
              {t('finance.sameDayLastMonth', {
                amount: formatMoney(amountOf(row.incomeBySameDayLastMonthAmount), row.currencyCode),
              })}
            </>
          }
          explanation={<p>{t('finance.forecastHowIncome', { expected: formatMoney(expected, row.currencyCode) })}</p>}
        />
      )}
      {row.unconvertedIncome.length > 0 ? (
        <p className="muted field-hint">
          {t('finance.unconvertedSpending', {
            amounts: row.unconvertedIncome
              .map((unconverted) => formatMoney(amountOf(unconverted.amount), unconverted.currencyCode))
              .join(', '),
          })}
        </p>
      ) : null}
    </div>
  )
}

// BudgetedMonths is the months of a year a budget was in force: how
// many, and the first and last of them, "2006-01".
export type BudgetedMonths = { budgetedMonthCount: number; firstBudgetedMonth: string; lastBudgetedMonth: string }

const ALL_MONTHS: BudgetedMonths = { budgetedMonthCount: 12, firstBudgetedMonth: '', lastBudgetedMonth: '' }

const isSameMonths = (left: BudgetedMonths, right: BudgetedMonths) =>
  left.budgetedMonthCount === right.budgetedMonthCount &&
  (left.budgetedMonthCount >= 12 ||
    (left.firstBudgetedMonth === right.firstBudgetedMonth && left.lastBudgetedMonth === right.lastBudgetedMonth))

// usualBudgetedMonths is the months of a year most of its budgets were in
// force: of two as common, the more months, then the earlier start. All
// twelve when every budget covered the whole year, or there is none.
export function usualBudgetedMonths(rows: BudgetedMonths[]): BudgetedMonths {
  const candidates: { months: BudgetedMonths; rowCount: number }[] = []
  for (const row of rows) {
    const found = candidates.find((candidate) => isSameMonths(candidate.months, row))
    if (found) found.rowCount++
    else candidates.push({ months: row, rowCount: 1 })
  }
  let usual: { months: BudgetedMonths; rowCount: number } = { months: ALL_MONTHS, rowCount: 0 }
  for (const candidate of candidates) {
    const isMore =
      candidate.rowCount > usual.rowCount ||
      (candidate.rowCount === usual.rowCount &&
        (candidate.months.budgetedMonthCount > usual.months.budgetedMonthCount ||
          (candidate.months.budgetedMonthCount === usual.months.budgetedMonthCount &&
            candidate.months.firstBudgetedMonth < usual.months.firstBudgetedMonth)))
    if (isMore) usual = candidate
  }
  return usual.months.budgetedMonthCount >= 12 ? ALL_MONTHS : usual.months
}

type Translate = ReturnType<typeof useTranslation>['t']

// budgetStatusYearHint is the year's budgets' description: over the whole
// year, or over the months most budgets were in force when those are
// fewer, named, as the year's saving names its months.
export function budgetStatusYearHint(t: Translate, year: string, isPast: boolean, usualMonths: BudgetedMonths): string {
  if (usualMonths.budgetedMonthCount >= 12 || !usualMonths.firstBudgetedMonth) {
    return isPast
      ? t('finance.budgetStatusYearHintPast', { year })
      : t('finance.budgetStatusYearHint', { year, from: yearStartLabel(year) })
  }
  const from = monthLabel(usualMonths.firstBudgetedMonth)
  if (usualMonths.budgetedMonthCount === 1) {
    return isPast
      ? t('finance.budgetStatusYearMonthHintPast', { year, month: from })
      : t('finance.budgetStatusYearMonthHint', { year, month: from })
  }
  const values = { year, count: usualMonths.budgetedMonthCount, from, to: monthLabel(usualMonths.lastBudgetedMonth) }
  return isPast ? t('finance.budgetStatusYearMonthsHintPast', values) : t('finance.budgetStatusYearMonthsHint', values)
}

// budgetedMonthsNote is what a year's budget row says of its months when
// they are not the ones the panel's description names, so its amount and
// spending are read as of its own months; null when they are.
export function budgetedMonthsNote(t: Translate, months: BudgetedMonths, usualMonths: BudgetedMonths): string | null {
  if (isSameMonths(months, usualMonths)) return null
  if (months.budgetedMonthCount >= 12) return t('finance.budgetedAllMonths')
  if (!months.firstBudgetedMonth) return t('finance.budgetedMonthCount', { count: months.budgetedMonthCount })
  const from = monthLabel(months.firstBudgetedMonth)
  if (months.budgetedMonthCount === 1) return t('finance.budgetedOneMonth', { month: from })
  return t('finance.budgetedMonths', {
    count: months.budgetedMonthCount,
    from,
    to: monthLabel(months.lastBudgetedMonth),
  })
}

function BudgetedMonthsNote({ months, usualMonths }: { months: BudgetedMonths; usualMonths: BudgetedMonths }) {
  const { t } = useTranslation()
  const note = budgetedMonthsNote(t, months, usualMonths)
  if (!note) return null
  return <div className="muted finance-budget-row-detail">{note}</div>
}

// BudgetStatusRow is a spending budget: what was spent against the
// budget, and a band to where the month (or the year) is heading, which
// turns the bad tone once the pace says it is heading over. Under a
// month's bar the line gives that forecast as the sum it is, and opens how
// it is made and which repeat charges it counts; under a year's, the
// budget for the days so far, and how the year is carried on.
function BudgetStatusRow({
  row,
  isPast,
  isYear,
  usualMonths,
}: {
  row: SpendingCategoryBudgetStatus
  isPast: boolean
  isYear: boolean
  usualMonths: BudgetedMonths
}) {
  const { t } = useTranslation()
  const words = useFinanceWords()
  const categoryName = useSpendingCategoryDisplayName()
  const budget = amountOf(row.budgetAmount)
  const spent = amountOf(row.spendingAmount)
  const projected = amountOf(row.projectedAmount)
  const { repeatChargesAmount, atPaceAmount } = spendingForecastParts(row)
  const money = (amount: number) => formatMoney(amount, row.currencyCode)
  const forecastParts = [t('finance.forecastSpent', { amount: money(spent) })]
  if (repeatChargesAmount > 0)
    forecastParts.push(t('finance.forecastRepeatCharges', { amount: money(repeatChargesAmount) }))
  if (atPaceAmount > 0) forecastParts.push(t('finance.forecastAtPace', { amount: money(atPaceAmount) }))
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
        forecast={budget > 0 && !isPast ? projected / budget : null}
        forecastLabel={isPast ? undefined : t('finance.projected', { amount: money(projected) })}
        overTone={row.budgetPace === 'at_risk' ? 'bad' : undefined}
      />
      {isYear ? <BudgetedMonthsNote months={row} usualMonths={usualMonths} /> : null}
      {isPast ? null : isYear ? (
        <ForecastDetail
          name={categoryName(row.spendingCategoryName)}
          line={
            <>
              {t('finance.projected', { amount: money(projected) })}
              {' · '}
              {t('finance.budgetToDate', { amount: money(amountOf(row.budgetToDateAmount)) })}
            </>
          }
          explanation={
            <>
              <p>{t('finance.forecastHowSpendingYear')}</p>
              {repeatChargesAmount > 0 ? (
                <p>{t('finance.forecastRepeatChargesThisMonth', { amount: money(repeatChargesAmount) })}</p>
              ) : null}
            </>
          }
        />
      ) : (
        <ForecastDetail
          name={categoryName(row.spendingCategoryName)}
          line={
            <>
              {t('finance.forecastSum', { amount: money(projected), parts: forecastParts.join(' + ') })}
              {' · '}
              {t('finance.sameDayLastMonth', { amount: money(amountOf(row.spendingBySameDayLastMonthAmount)) })}
            </>
          }
          explanation={
            <>
              <p>{t('finance.forecastHowSpending')}</p>
              {row.expectedRepeatCharges.length > 0 ? (
                <>
                  <p>{t('finance.forecastRepeatChargesListed')}</p>
                  <ul className="finance-forecast-charges">
                    {row.expectedRepeatCharges.map((charge) => (
                      <li key={`${charge.merchantName} ${charge.currencyCode}`}>
                        <span>{charge.merchantName}</span>
                        <span className="numeric">
                          {formatMoney(amountOf(charge.expectedAmount), charge.currencyCode)}
                        </span>
                      </li>
                    ))}
                  </ul>
                </>
              ) : (
                <p>{t('finance.forecastNoRepeatCharges')}</p>
              )}
            </>
          }
        />
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

// The ways the period's spending is grouped. By month is not one of them:
// the chart at the section's top is by month, in a year as in a month.
const GROUP_BY: SpendingGroupBy[] = ['spendingCategory', 'merchant', 'financeAccount']

// groupTransactionFilters is how a group's transactions are found on the
// Transactions section, over the month's or the year's days: a spending
// category (or none), a finance account, or a merchant's words.
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

// What went where in the month or the year, counted as the chart above
// counts it (see spendingLines), so the table's total is the headline: one
// amount a group, what it spent, and how many transactions it holds.
// How many groups a long grouping shows before "Show all": a month has
// dozens of merchants, and the few at the top are what the table is read
// for.
const SHORT_GROUP_COUNT = 20

function SpendingSummaryPanel({ range, periodLabel }: { range: { from: string; to: string }; periodLabel: string }) {
  const { t } = useTranslation()
  const categoryName = useSpendingCategoryDisplayName()
  const [groupBy, setGroupBy] = useState<SpendingGroupBy>('spendingCategory')
  const [isShowingAll, setIsShowingAll] = useState(false)
  const [highlightedKey, setHighlightedKey] = useState<string | null>(null)
  useEffect(() => setIsShowingAll(false), [groupBy, range.from, range.to])
  // Each answer says what it was asked for: useQuery keeps the last answer
  // while the next is on its way, and last month's rows under this month's
  // range, or category ids read as merchants, would be wrong for a moment.
  const asked = `${range.from}|${range.to}|${groupBy}`
  const {
    data: answered,
    error,
    loading,
  } = useQuery(
    () =>
      graphql<{ FinanceSpendingSummary: SpendingSummary }>(SPENDING_SUMMARY, {
        from: range.from,
        to: range.to,
        groupBy,
      }).then((answer) => ({ ...answer, asked })),
    [range.from, range.to, groupBy],
    { refresh: false },
  )
  const data = answered?.asked === asked ? answered : undefined
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
  // A spending category's name as the reader reads it; merchants and
  // accounts as they came.
  const lineName = (label: string) =>
    !label ? t('finance.uncategorized') : groupBy === 'spendingCategory' ? categoryName(label) : label
  // The ring is the table's own numbers, where they can be added up: in
  // the reporting currency, whichever way the month is grouped. What does
  // not fit is one slice, named for what it folds.
  const otherLabel = (count: number) =>
    groupBy === 'merchant'
      ? t('finance.otherMerchants', { count })
      : groupBy === 'financeAccount'
        ? t('finance.otherFinanceAccounts', { count })
        : t('finance.otherCategories', { count })
  const slices = summary?.reportingCurrencyCode
    ? foldIntoOther(
        lines.map((line) => ({ key: line.key, label: lineName(line.label), amount: line.spendingAmount })),
        RING_SLICE_COUNT,
      ).map((slice) => (slice.isOther ? { ...slice, label: otherLabel(slice.foldedCount) } : slice))
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
  const isLongGrouping = groupBy === 'merchant'
  const shownLines = isLongGrouping && !isShowingAll ? lines.slice(0, SHORT_GROUP_COUNT) : lines

  return (
    <SettingsSection
      card
      title={t('finance.summaryTitle')}
      description={t('finance.summaryHint', { month: periodLabel })}
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
      {((loading || (answered && !error)) && !data) || (categories.loading && !categories.data) ? <Loading /> : null}
      {summary && lines.length === 0 ? <SettingsEmpty>{t('finance.noSpending')}</SettingsEmpty> : null}
      {slices.length > 0 && summary?.reportingCurrencyCode ? (
        <SpendingRing
          slices={slices}
          currency={summary.reportingCurrencyCode}
          label={t('finance.ringLabel', { month: periodLabel })}
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
                // A group whose refunds outweigh its spending has no share
                // of the month: no "-0%".
                const fraction =
                  slice && reportingTotal && reportingTotal.spendingAmount > 0 && line.spendingAmount > 0
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
                  ? {
                      onPointerEnter: () => setHighlightedKey(slice.sliceKey),
                      onPointerLeave: () => setHighlightedKey(null),
                    }
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
          <button type="button" onClick={() => setIsShowingAll((previous) => !previous)}>
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
