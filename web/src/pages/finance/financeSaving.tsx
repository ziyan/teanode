import { graphql } from '../../api'
import { ErrorMessage, Loading, Tag, formatMoney } from '../../components/common'
import { MeterBar } from '../../components/budgetBar'
import { SettingsEmpty, SettingsSection } from '../../components/settingsList'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { formatSigned, reachTone, savingMeter } from './budgetGroups'
import { SAVING_SUMMARY, SavingSummary, amountOf, monthLabel, personMonth } from './financeApi'
import { UnconvertedNote, useFinanceWords } from './financeCommon'
import { ForecastDetail } from './forecastDetail'
import { yearStartLabel } from './spendingYear'

// YearCoverage is the months of a year its saving counts, when that is
// fewer than all twelve: how many, and the first and last as the reader
// writes a month.
type YearCoverage = { budgetedMonthCount: number; from: string; to: string }

function yearCoverage(budgetedMonths: string[]): YearCoverage | null {
  if (budgetedMonths.length === 0 || budgetedMonths.length >= 12) return null
  return {
    budgetedMonthCount: budgetedMonths.length,
    from: monthLabel(budgetedMonths[0]),
    to: monthLabel(budgetedMonths[budgetedMonths.length - 1]),
  }
}

type Translate = ReturnType<typeof useTranslation>['t']

// savingYearHint is the year's description: over the whole year, or over
// the months with budgets when those are fewer, named.
function savingYearHint(t: Translate, year: string, isPast: boolean, coverage: YearCoverage | null): string {
  if (!coverage) {
    return isPast
      ? t('finance.savingYearHintPast', { year })
      : t('finance.savingYearHint', { year, from: yearStartLabel(year) })
  }
  if (coverage.budgetedMonthCount === 1) {
    return isPast
      ? t('finance.savingYearMonthHintPast', { year, month: coverage.from })
      : t('finance.savingYearMonthHint', { year, month: coverage.from })
  }
  const values = { year, count: coverage.budgetedMonthCount, from: coverage.from, to: coverage.to }
  return isPast ? t('finance.savingYearMonthsHintPast', values) : t('finance.savingYearMonthsHint', values)
}

// savedLabel heads the saving's bar, naming the months it counts when
// they are not the whole year.
function savedLabel(t: Translate, isPast: boolean, coverage: YearCoverage | null): string {
  if (!coverage) return isPast ? t('finance.saved') : t('finance.savedSoFar')
  if (coverage.budgetedMonthCount === 1) {
    return isPast
      ? t('finance.savedInMonth', { month: coverage.from })
      : t('finance.savedSoFarInMonth', { month: coverage.from })
  }
  const values = { from: coverage.from, to: coverage.to }
  return isPast ? t('finance.savedInMonths', values) : t('finance.savedSoFarInMonths', values)
}

// The month's saving: what the income budgets less the spending budgets
// expect to be left, against what came in less what went out, in the
// reporting currency. The bar is the saving so far against the expected
// saving, with a band to where the month is heading; a month that is over is
// its own figures, with nothing left to head anywhere. Spending and income
// are counted the way the chart of months counts them, budgeted or not.
// A change to reloadKey reads it again: the Budgets section changes it
// when a budget changes under it. Given a year, it is the year's saving
// instead, its months added up as each was budgeted, and month is not read:
// only the months that had a budget count, budgeted and actual alike, and
// the panel names them. A year with no budget at all is its income,
// spending and what was left, with nothing expected to set them against.
export function SavingSummaryPanel({
  month,
  year,
  reloadKey = 0,
}: {
  month: string
  year?: string
  reloadKey?: number
}) {
  const { t } = useTranslation()
  const words = useFinanceWords()
  const asked = year ? `year ${year}` : `month ${month}`
  const {
    data: answered,
    error,
    loading,
  } = useQuery(
    () =>
      graphql<{ SavingSummary: SavingSummary }>(SAVING_SUMMARY, year ? { year } : { month }).then((answer) => ({
        ...answer,
        asked,
      })),
    [asked, reloadKey],
    { refresh: false },
  )
  // A month's figures under a year's heading would be wrong for a moment.
  const data = answered?.asked === asked ? answered : undefined
  const summary = data?.SavingSummary
  const isPast = year ? year < personMonth().slice(0, 4) : month < personMonth()
  const currency = summary?.reportingCurrencyCode || 'USD'
  const hasBudgets = summary
    ? year
      ? summary.budgetedMonthCount > 0
      : summary.incomeBudgetCount + summary.spendingBudgetCount > 0
    : false
  const coverage = year && summary ? yearCoverage(summary.budgetedMonths ?? []) : null
  const meter = summary ? savingMeter(summary, isPast) : null
  const money = (amount: string) => formatMoney(amountOf(amount), currency)
  const said = summary
    ? t('finance.savedOfExpected', {
        saved: money(summary.savingAmount),
        expected: money(summary.expectedSavingAmount),
      })
    : ''
  const difference = summary ? amountOf(summary.savingDifferenceAmount) : 0
  return (
    <SettingsSection
      card
      title={year ? t('finance.savingYearTitle') : t('finance.savingTitle')}
      description={
        !summary
          ? undefined
          : year
            ? savingYearHint(t, year, isPast, coverage)
            : isPast
              ? t('finance.savingHintPast', { month: monthLabel(month, 'long') })
              : t('finance.savingHint', { day: summary.dayOfMonth, days: summary.daysInMonth })
      }
    >
      <ErrorMessage error={error} />
      {(loading || (answered && !error)) && !data ? <Loading /> : null}
      {summary && !hasBudgets && !year ? <SettingsEmpty>{t('finance.noSavingBudgets')}</SettingsEmpty> : null}
      {summary && !hasBudgets && year ? (
        <>
          <p className="muted">{t('finance.noSavingBudgetsYear', { year })}</p>
          <div className="table-wrap">
            <table className="numbers-table finance-table finance-saving-table">
              <thead>
                <tr>
                  <th />
                  <th className="numeric">{isPast ? t('finance.actual') : t('finance.soFar')}</th>
                </tr>
              </thead>
              <tbody>
                <tr>
                  <th scope="row">{t('finance.income')}</th>
                  <td className="numeric">{money(summary.incomeAmount)}</td>
                </tr>
                <tr>
                  <th scope="row">{t('finance.spending')}</th>
                  <td className="numeric">{money(summary.spendingAmount)}</td>
                </tr>
              </tbody>
              <tfoot>
                <tr>
                  <th scope="row">{t('finance.leftOver')}</th>
                  <th className="numeric">{money(summary.savingAmount)}</th>
                </tr>
              </tfoot>
            </table>
          </div>
        </>
      ) : null}
      {summary && hasBudgets ? (
        <>
          <div className="finance-budget-row">
            <div className="finance-budget-row-head">
              <strong>{savedLabel(t, isPast, coverage)}</strong>
              <Tag value={words.savingPace(summary.savingPace)} tone={reachTone(summary.savingPace)} />
              <span className="finance-budget-row-said">{said}</span>
            </div>
            {meter ? (
              <MeterBar
                fraction={meter.fraction}
                tone={reachTone(summary.savingPace)}
                label={said}
                forecast={meter.forecast}
                forecastLabel={
                  isPast ? undefined : t('finance.projected', { amount: money(summary.projectedSavingAmount) })
                }
                overTone="good"
              />
            ) : null}
            {isPast ? (
              <div className="muted finance-budget-row-detail">
                {t('finance.savingEndedAgainstExpected', { amount: formatSigned(difference, currency) })}
              </div>
            ) : (
              <ForecastDetail
                name={t('finance.savedSoFar')}
                line={t('finance.savingHeadingAgainstExpected', {
                  amount: money(summary.projectedSavingAmount),
                  difference: formatSigned(difference, currency),
                })}
                explanation={
                  <p>
                    {t(year ? 'finance.forecastHowSavingYear' : 'finance.forecastHowSaving', {
                      income: money(summary.projectedIncomeAmount),
                      spending: money(summary.projectedSpendingAmount),
                    })}
                  </p>
                }
              />
            )}
          </div>
          <div className="table-wrap">
            <table className="numbers-table finance-table finance-saving-table">
              <thead>
                <tr>
                  <th />
                  <th className="numeric">{t('finance.budgeted')}</th>
                  <th className="numeric">{isPast ? t('finance.actual') : t('finance.soFar')}</th>
                  {isPast ? null : <th className="numeric">{t('finance.headingFor')}</th>}
                </tr>
              </thead>
              <tbody>
                <tr>
                  <th scope="row">{t('finance.income')}</th>
                  <td className="numeric">{money(summary.expectedIncomeAmount)}</td>
                  <td className="numeric">{money(summary.incomeAmount)}</td>
                  {isPast ? null : <td className="numeric">{money(summary.projectedIncomeAmount)}</td>}
                </tr>
                <tr>
                  <th scope="row">{t('finance.spending')}</th>
                  <td className="numeric">{money(summary.expectedSpendingAmount)}</td>
                  <td className="numeric">{money(summary.spendingAmount)}</td>
                  {isPast ? null : <td className="numeric">{money(summary.projectedSpendingAmount)}</td>}
                </tr>
              </tbody>
              <tfoot>
                <tr>
                  <th scope="row">{t('finance.saving')}</th>
                  <th className="numeric">{money(summary.expectedSavingAmount)}</th>
                  <th className="numeric">{money(summary.savingAmount)}</th>
                  {isPast ? null : <th className="numeric">{money(summary.projectedSavingAmount)}</th>}
                </tr>
              </tfoot>
            </table>
          </div>
          {summary.incomeBudgetCount === 0 ? <p className="muted field-hint">{t('finance.noIncomeBudget')}</p> : null}
        </>
      ) : null}
      <UnconvertedNote currencyCodes={summary?.unconvertedCurrencyCodes} />
    </SettingsSection>
  )
}
