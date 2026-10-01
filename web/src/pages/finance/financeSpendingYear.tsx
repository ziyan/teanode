import { graphql } from '../../api'
import { ErrorMessage, Loading, formatMoney } from '../../components/common'
import { ChevronLeftIcon, ChevronRightIcon } from '../../components/icons'
import { SeriesChart } from '../../components/seriesChart'
import { Select } from '../../components/select'
import { SettingsEmpty, SettingsSection } from '../../components/settingsList'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { CASH_FLOW, CashFlow, amountOf, monthLabel } from './financeApi'
import { UnconvertedNote, compactMoney } from './financeCommon'
import { SpendingPeriod, SpendingPeriodKind, latestMonthOfYear } from './financeFilters'
import { yearAfter, yearBefore, yearCashFlowTotals, yearMonths, yearOptions, yearStartLabel } from './spendingYear'

// SpendingPeriodPicker chooses what the Spending section shows: Month or
// Year, and beside it the month, or the year with a step either way. A
// change of kind is a step in the browser's history, so Back returns to
// the month or the year that was being read; choosing another month or
// year replaces the address, as the month always has.
export function SpendingPeriodPicker({
  period,
  currentMonth,
  onSelectPeriod,
}: {
  period: SpendingPeriod
  currentMonth: string
  onSelectPeriod: (period: SpendingPeriod, isKindChange: boolean) => void
}) {
  const { t } = useTranslation()
  const currentYear = currentMonth.slice(0, 4)
  const selectKind = (spendingPeriodKind: SpendingPeriodKind) => {
    if (spendingPeriodKind === period.spendingPeriodKind) return
    // From a month to its year, and from a year to its latest month begun.
    const month = spendingPeriodKind === 'month' ? latestMonthOfYear(period.year, currentMonth) : period.month
    onSelectPeriod({ spendingPeriodKind, month, year: month.slice(0, 4) }, true)
  }
  const selectYear = (year: string) =>
    onSelectPeriod({ spendingPeriodKind: 'year', year, month: latestMonthOfYear(year, currentMonth) }, false)
  const kinds: SpendingPeriodKind[] = ['month', 'year']
  return (
    <div className="finance-period">
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
      {period.spendingPeriodKind === 'month' ? (
        <label className="finance-month">
          <span>{t('finance.month')}</span>
          <input
            type="month"
            value={period.month}
            max={currentMonth}
            onChange={(event) =>
              event.target.value &&
              onSelectPeriod(
                { spendingPeriodKind: 'month', month: event.target.value, year: event.target.value.slice(0, 4) },
                false,
              )
            }
          />
        </label>
      ) : (
        <div className="finance-year">
          <Tooltip label={t('finance.previousYear')}>
            <button
              type="button"
              className="icon-button"
              aria-label={t('finance.previousYear')}
              onClick={() => selectYear(yearBefore(period.year))}
            >
              <ChevronLeftIcon size={16} />
            </button>
          </Tooltip>
          <Select
            value={period.year}
            label={t('finance.year')}
            options={yearOptions(currentYear, period.year).map((year) => ({ value: year, label: year }))}
            onChange={selectYear}
          />
          <Tooltip label={t('finance.nextYear')}>
            <button
              type="button"
              className="icon-button"
              aria-label={t('finance.nextYear')}
              disabled={period.year >= currentYear}
              onClick={() => selectYear(yearAfter(period.year))}
            >
              <ChevronRightIcon size={16} />
            </button>
          </Tooltip>
        </div>
      )}
    </div>
  )
}

// The year's cash flow a bar a month: income and spending side by side and
// what was left as a line, the months still to come empty, and under it
// the year's three figures added up from those months. Choosing a month
// opens it in Month.
export function SpendingByYearPanel({
  year,
  currentMonth,
  picker,
  onOpenMonth,
}: {
  year: string
  currentMonth: string
  picker: React.ReactNode
  onOpenMonth: (month: string) => void
}) {
  const { t } = useTranslation()
  const months = yearMonths(year)
  const { data, error, loading } = useQuery(
    () => graphql<{ CashFlow: CashFlow }>(CASH_FLOW, { fromMonth: months[0], toMonth: months[11] }),
    [year],
    { refresh: false },
  )
  const flow = data?.CashFlow
  const flowMonths = flow?.cashFlowMonths ?? []
  const currency = flow?.reportingCurrencyCode || 'USD'
  const totals = yearCashFlowTotals(flowMonths, year)
  const isCurrent = year === currentMonth.slice(0, 4)
  const byMonth = new Map(flowMonths.map((month) => [month.cashFlowMonth, month]))
  // A month that has not begun is no bar at all, rather than a zero.
  const valuesOf = (field: 'incomeAmount' | 'spendingAmount' | 'netAmount') =>
    months.map((month) => (month > currentMonth ? null : amountOf(byMonth.get(month)?.[field])))
  const hasCashFlow = flowMonths.some(
    (month) => amountOf(month.spendingAmount) !== 0 || amountOf(month.incomeAmount) !== 0,
  )
  const caption = isCurrent
    ? t('finance.spentYearToDate', { from: yearStartLabel(year) })
    : t('finance.spentInYear', { year })
  return (
    <SettingsSection card title={t('finance.cashFlowByYearTitle')} description={t('finance.cashFlowByYearHint')}>
      <ErrorMessage error={error} />
      {loading && !data ? <Loading /> : null}
      {flow && !hasCashFlow ? (
        <>
          <div className="finance-month-alone">{picker}</div>
          <SettingsEmpty>{t('finance.noCashFlowInYear', { year })}</SettingsEmpty>
        </>
      ) : null}
      {hasCashFlow ? (
        <SeriesChart
          label={t('finance.cashFlowByYearTitle')}
          keys={months}
          keyLabel={(key) => monthLabel(key)}
          format={(value) => formatMoney(value, currency)}
          axisFormat={(value) => compactMoney(value, currency)}
          headline={formatMoney(totals.spendingAmount, currency)}
          caption={caption}
          series={[
            {
              id: 'income',
              label: t('finance.income'),
              tone: 'output',
              shape: 'column',
              values: valuesOf('incomeAmount'),
            },
            {
              id: 'spending',
              label: t('finance.spending'),
              tone: 'cached',
              shape: 'column',
              values: valuesOf('spendingAmount'),
            },
            { id: 'left', label: t('finance.leftOver'), tone: 'input', shape: 'line', values: valuesOf('netAmount') },
          ]}
          onSelectKey={(month) => month <= currentMonth && onOpenMonth(month)}
          headAction={picker}
        />
      ) : null}
      {hasCashFlow ? (
        <p className="muted finance-month-flow">
          {isCurrent ? <span>{t('finance.yearToDate', { from: yearStartLabel(year) })}</span> : null}
          <span>
            {t('finance.income')} <strong>{formatMoney(totals.incomeAmount, currency)}</strong>
          </span>
          <span>
            {t('finance.spending')} <strong>{formatMoney(totals.spendingAmount, currency)}</strong>
          </span>
          <span>
            {t('finance.leftOver')} <strong>{formatMoney(totals.netAmount, currency)}</strong>
          </span>
        </p>
      ) : null}
      <UnconvertedNote currencyCodes={flow?.unconvertedCurrencyCodes} />
    </SettingsSection>
  )
}
