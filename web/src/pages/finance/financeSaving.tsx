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

// The month's saving: what the income budgets less the spending budgets
// expect to be left, against what came in less what went out, in the
// reporting currency. The bar is the saving so far against the expected
// saving, with a band to where the month is heading; a month that is over is
// its own figures, with nothing left to head anywhere. Spending and income
// are counted the way the chart of months counts them, budgeted or not.
// A change to reloadKey reads it again: the Budgets section changes it
// when a budget changes under it.
export function SavingSummaryPanel({ month, reloadKey = 0 }: { month: string; reloadKey?: number }) {
  const { t } = useTranslation()
  const words = useFinanceWords()
  const { data, error, loading } = useQuery(
    () => graphql<{ SavingSummary: SavingSummary }>(SAVING_SUMMARY, { month }),
    [month, reloadKey],
    { refresh: false },
  )
  const summary = data?.SavingSummary
  const isPast = month < personMonth()
  const currency = summary?.reportingCurrencyCode || 'USD'
  const hasBudgets = summary ? summary.incomeBudgetCount + summary.spendingBudgetCount > 0 : false
  const meter = summary ? savingMeter(summary, isPast) : null
  const money = (amount: string) => formatMoney(amountOf(amount), currency)
  const said = summary
    ? t('finance.savedOfExpected', { saved: money(summary.savingAmount), expected: money(summary.expectedSavingAmount) })
    : ''
  const difference = summary ? amountOf(summary.savingDifferenceAmount) : 0
  return (
    <SettingsSection
      card
      title={t('finance.savingTitle')}
      description={
        summary
          ? isPast
            ? t('finance.savingHintPast', { month: monthLabel(month, 'long') })
            : t('finance.savingHint', { day: summary.dayOfMonth, days: summary.daysInMonth })
          : undefined
      }
    >
      <ErrorMessage error={error} />
      {loading && !data ? <Loading /> : null}
      {summary && !hasBudgets ? <SettingsEmpty>{t('finance.noSavingBudgets')}</SettingsEmpty> : null}
      {summary && hasBudgets ? (
        <>
          <div className="finance-budget-row">
            <div className="finance-budget-row-head">
              <strong>{isPast ? t('finance.saved') : t('finance.savedSoFar')}</strong>
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
                    {t('finance.forecastHowSaving', {
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
