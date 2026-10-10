import { useState } from 'react'

import { graphql } from '../../api'
import { ErrorMessage, Loading, formatMoney } from '../../components/common'
import { ChevronLeftIcon, ChevronRightIcon } from '../../components/icons'
import { SeriesChart } from '../../components/seriesChart'
import { Select } from '../../components/select'
import { SettingsEmpty, SettingsSection } from '../../components/settingsList'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { CASH_FLOW, CashFlow, monthBefore, monthLabel } from './financeApi'
import { UnconvertedNote, compactMoney } from './financeCommon'
import { SpendingPeriod, SpendingPeriodKind, latestMonthOfYear } from './financeFilters'
import {
  YearCashFlow,
  cashFlowYears,
  firstCashFlowMonth,
  firstIncomeMonth,
  historyRange,
  incomeStartMonthWithin,
  monthOptions,
  monthStartLabel,
  partialYearStartMonth,
  yearEndLabel,
  yearOptions,
  yearStartLabel,
} from './spendingYear'

// SpendingHistory is the cash flow of every month there is to choose from,
// read once for the section: the years the chart of years draws and the
// year menu lists, and the first month the month menu reaches back to.
// windowStartMonth is the first month the history was read from, set when
// that month already has cash flow: there may be more before it, not
// read.
export type SpendingHistory = {
  flow?: CashFlow
  error?: unknown
  loading: boolean
  years: YearCashFlow[]
  firstMonth: string | null
  incomeMonth: string | null
  windowStartMonth: string | null
}

// useSpendingHistory reads the history once it is wanted, and keeps it
// from then on. Twenty years of cash flow is a long read for the server
// (each day converted where an account is in another currency), so Month
// mode, whose chart reads its own twelve months, asks for it only when the
// person reaches for the month menu, and Year mode, whose chart of years
// is drawn from it, as it opens.
export function useSpendingHistory(currentMonth: string, isWanted: boolean): SpendingHistory {
  const range = historyRange(currentMonth)
  const [isRequested, setRequested] = useState(isWanted)
  if (isWanted && !isRequested) setRequested(true)
  const { data, error, loading } = useQuery(
    () => (isRequested ? graphql<{ CashFlow: CashFlow }>(CASH_FLOW, range) : Promise.resolve(null)),
    [range.fromMonth, range.toMonth, isRequested],
    { refresh: false },
  )
  const flow = data?.CashFlow
  const months = flow?.cashFlowMonths ?? []
  const firstMonth = firstCashFlowMonth(months)
  return {
    flow,
    error,
    loading: isRequested && loading && !data,
    years: cashFlowYears(months, currentMonth.slice(0, 4)),
    firstMonth,
    incomeMonth: firstIncomeMonth(months),
    windowStartMonth: firstMonth !== null && firstMonth <= range.fromMonth ? range.fromMonth : null,
  }
}

// SpendingPeriodPicker chooses what the Spending section shows: Month or
// Year, and beside it the month or the year in a menu with a step either
// way, the same shape for both. It stays in one place, the section's own
// row above its panels, whichever is shown. A change of kind is a step in
// the browser's history, so Back returns to the month or the year that was
// being read; choosing another month or year replaces the address, as the
// month always has.
export function SpendingPeriodPicker({
  period,
  currentMonth,
  history,
  onSelectPeriod,
  onWantHistory,
}: {
  period: SpendingPeriod
  currentMonth: string
  history: SpendingHistory
  onSelectPeriod: (period: SpendingPeriod, isKindChange: boolean) => void
  // onWantHistory is called as the person reaches for the control, so the
  // month menu can reach back to the first month with cash flow.
  onWantHistory?: () => void
}) {
  const { t } = useTranslation()
  const isYear = period.spendingPeriodKind === 'year'
  const selectKind = (spendingPeriodKind: SpendingPeriodKind) => {
    if (spendingPeriodKind === period.spendingPeriodKind) return
    // From a month to its year, and from a year to its latest month begun.
    const month = spendingPeriodKind === 'month' ? latestMonthOfYear(period.year, currentMonth) : period.month
    onSelectPeriod({ spendingPeriodKind, month, year: month.slice(0, 4) }, true)
  }
  const select = (value: string) =>
    onSelectPeriod(
      isYear
        ? { spendingPeriodKind: 'year', year: value, month: latestMonthOfYear(value, currentMonth) }
        : { spendingPeriodKind: 'month', month: value, year: value.slice(0, 4) },
      false,
    )
  // Newest first, so the step back is the next option down the menu.
  const options = isYear
    ? yearOptions(
        history.years.map((year) => year.year),
        period.year,
      )
    : monthOptions(history.firstMonth, currentMonth, period.month)
  const chosen = isYear ? period.year : period.month
  const index = options.indexOf(chosen)
  // Until the history has been read the month menu does not know where the
  // months with money begin, so a month can always step back.
  const earlier =
    index >= 0 && index < options.length - 1
      ? options[index + 1]
      : !isYear && !history.flow
        ? monthBefore(period.month, 1)
        : null
  const later = index > 0 ? options[index - 1] : null
  const kinds: SpendingPeriodKind[] = ['month', 'year']
  return (
    <div className="finance-period" onPointerEnter={onWantHistory} onFocus={onWantHistory}>
      <div className="segmented" role="group" aria-label={t('finance.periodKind')}>
        {kinds.map((kind) => (
          <button
            key={kind}
            type="button"
            className={period.spendingPeriodKind === kind ? 'active' : ''}
            aria-pressed={period.spendingPeriodKind === kind}
            onClick={() => selectKind(kind)}
          >
            {kind === 'month' ? t('finance.month') : t('finance.year')}
          </button>
        ))}
      </div>
      <div className="finance-period-step">
        <Tooltip label={isYear ? t('finance.previousYear') : t('finance.previousMonth')}>
          <button
            type="button"
            className="icon-button"
            aria-label={isYear ? t('finance.previousYear') : t('finance.previousMonth')}
            disabled={earlier === null}
            onClick={() => earlier && select(earlier)}
          >
            <ChevronLeftIcon size={16} />
          </button>
        </Tooltip>
        <Select
          className="finance-period-select"
          value={chosen}
          label={isYear ? t('finance.year') : t('finance.month')}
          options={options.map((option) => ({ value: option, label: isYear ? option : monthLabel(option, 'long') }))}
          onChange={select}
        />
        <Tooltip label={isYear ? t('finance.nextYear') : t('finance.nextMonth')}>
          <button
            type="button"
            className="icon-button"
            aria-label={isYear ? t('finance.nextYear') : t('finance.nextMonth')}
            disabled={later === null}
            onClick={() => later && select(later)}
          >
            <ChevronRightIcon size={16} />
          </button>
        </Tooltip>
      </div>
    </div>
  )
}

// The cash flow of every year there is, a group a year: income and spending
// side by side and what was left as a line, this year marked as the year
// to date. Choosing a year shows it in the rest of the section, and the
// line under the chart is the chosen year's three figures. The first year
// usually starts partway, at the first month with money in or out, and is
// labelled with that month the way this year is with "so far": months of
// spending against a few weeks of income would otherwise read as a year
// spent at a loss.
export function SpendingByYearPanel({
  year,
  currentMonth,
  history,
  onSelectYear,
}: {
  year: string
  currentMonth: string
  history: SpendingHistory
  onSelectYear: (year: string) => void
}) {
  const { t } = useTranslation()
  const { flow, error, loading, years, firstMonth, incomeMonth, windowStartMonth } = history
  const currency = flow?.reportingCurrencyCode || 'USD'
  const currentYear = currentMonth.slice(0, 4)
  const isCurrent = year === currentYear
  const chosen = years.find((candidate) => candidate.year === year)
  const hasCashFlow = years.some((candidate) => candidate.incomeAmount !== 0 || candidate.spendingAmount !== 0)
  const yearLabel = (key: string) => {
    // The history read already had money in its first month: the first
    // year may have had more before it, which is not read, so it is not
    // labelled as the year the money began.
    if (windowStartMonth && key === windowStartMonth.slice(0, 4)) {
      return t('finance.yearFromEarlierNotShown', { year: key, month: monthLabel(windowStartMonth) })
    }
    const startMonth = partialYearStartMonth(key, firstMonth)
    if (startMonth) {
      const values = { year: key, month: monthLabel(startMonth) }
      return key === currentYear ? t('finance.yearFromSoFar', values) : t('finance.yearFrom', values)
    }
    // Spending known all year but income only from partway: said, so the
    // year does not read as one of spending with nothing coming in.
    const incomeStart = incomeStartMonthWithin(key, incomeMonth)
    if (incomeStart) {
      const values = { year: key, month: monthLabel(incomeStart) }
      return key === currentYear ? t('finance.yearIncomeFromSoFar', values) : t('finance.yearIncomeFrom', values)
    }
    return key === currentYear ? t('finance.yearSoFar', { year: key }) : key
  }
  const chosenIncomeStart = incomeStartMonthWithin(year, incomeMonth)
  // The chosen year's days, when they are not the whole of it: from the
  // first of January, or of the month its history starts in, to today or
  // to the end of the year.
  const chosenStartMonth = partialYearStartMonth(year, firstMonth)
  const from = chosenStartMonth ? monthStartLabel(chosenStartMonth) : yearStartLabel(year)
  const range = isCurrent
    ? t('finance.yearToDate', { from })
    : chosenStartMonth
      ? t('finance.yearRange', { from, to: yearEndLabel(year) })
      : null
  const caption = isCurrent
    ? t('finance.spentYearToDate', { from })
    : chosenStartMonth
      ? t('finance.spentInRange', { from, to: yearEndLabel(year) })
      : t('finance.spentInYear', { year })
  return (
    <SettingsSection card title={t('finance.cashFlowByYearTitle')} description={t('finance.cashFlowByYearHint')}>
      <ErrorMessage error={error} />
      {loading ? <Loading /> : null}
      {flow && !hasCashFlow ? <SettingsEmpty>{t('finance.noCashFlowYears')}</SettingsEmpty> : null}
      {hasCashFlow ? (
        <SeriesChart
          label={t('finance.cashFlowByYearTitle')}
          keys={years.map((candidate) => candidate.year)}
          // The year alone under its columns: "2024 from Mar, earlier not
          // shown" is too long to fit under one, and is said in full at the
          // top of its tooltip instead.
          keyLabel={(key) => key}
          keyTitle={yearLabel}
          isEveryDipShown
          format={(value) => formatMoney(value, currency)}
          axisFormat={(value, precision) => compactMoney(value, currency, precision)}
          headline={formatMoney(chosen?.spendingAmount ?? 0, currency)}
          caption={caption}
          series={[
            {
              id: 'income',
              label: t('finance.income'),
              tone: 'output',
              shape: 'column',
              values: years.map((candidate) => candidate.incomeAmount),
            },
            {
              id: 'spending',
              label: t('finance.spending'),
              tone: 'cached',
              shape: 'column',
              values: years.map((candidate) => candidate.spendingAmount),
            },
            {
              id: 'left',
              label: t('finance.leftOver'),
              tone: 'input',
              shape: 'line',
              values: years.map((candidate) => candidate.netAmount),
            },
          ]}
          selectedKey={year}
          onSelectKey={onSelectYear}
        />
      ) : null}
      {hasCashFlow ? (
        <p className="muted finance-month-flow">
          {range ? <span>{range}</span> : null}
          <span>
            {t('finance.income')} <strong>{formatMoney(chosen?.incomeAmount ?? 0, currency)}</strong>
          </span>
          <span>
            {t('finance.spending')} <strong>{formatMoney(chosen?.spendingAmount ?? 0, currency)}</strong>
          </span>
          <span>
            {t('finance.leftOver')} <strong>{formatMoney(chosen?.netAmount ?? 0, currency)}</strong>
          </span>
        </p>
      ) : null}
      {hasCashFlow && chosenIncomeStart ? (
        <p className="muted field-hint">{t('finance.incomeKnownFrom', { from: monthStartLabel(chosenIncomeStart) })}</p>
      ) : null}
      <UnconvertedNote currencyCodes={flow?.unconvertedCurrencyCodes} />
    </SettingsSection>
  )
}
