import { useEffect, useState } from 'react'

import { graphql } from '../../api'
import { Loading, SaveRow, formatMoney } from '../../components/common'
import { SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { useTranslation } from '../../i18n/i18n'
import {
  CONVERT_CURRENCY,
  CurrencyConversion,
  SET_REPORTING_CURRENCY,
  amountOf,
  formatDay,
  isDecimal,
  isoDay,
} from './financeApi'
import { CurrencyPicker, useReportingCurrency } from './financeCommon'

// The Finance settings: the one currency totals are shown in, and a
// converter that asks the same exchange rates the totals use, so a number
// on the Spending section can be checked by hand.
export function FinanceSettingsSection() {
  const { t } = useTranslation()
  const settings = useReportingCurrency()
  const stored = settings.reportingCurrencyCode
  const [currencyCode, setCurrencyCode] = useState('')
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [problem, setProblem] = useState<unknown>(null)
  useEffect(() => setCurrencyCode(stored), [stored])

  const save = async (event: React.FormEvent) => {
    event.preventDefault()
    setBusy(true)
    setSaved(false)
    setProblem(null)
    try {
      await graphql(SET_REPORTING_CURRENCY, { currencyCode })
      await settings.reload()
      setSaved(true)
    } catch (caught) {
      setProblem(caught)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <SettingsSection
        card
        title={t('finance.reportingCurrencyTitle')}
        description={t('finance.reportingCurrencyHint')}
      >
        {!settings.isLoaded ? (
          <Loading />
        ) : (
          <form onSubmit={(event) => void save(event)}>
            <div className="form-narrow">
              <label>
                <span>{t('finance.reportingCurrency')}</span>
                <CurrencyPicker
                  value={currencyCode}
                  label={t('finance.reportingCurrency')}
                  onChange={(value) => {
                    setSaved(false)
                    setCurrencyCode(value)
                  }}
                />
              </label>
            </div>
            <SaveRow
              busy={busy}
              saved={saved}
              problem={problem}
              canSave={/^[A-Z]{3}$/.test(currencyCode) && currencyCode !== stored}
            />
          </form>
        )}
      </SettingsSection>
      <ConverterPanel reportingCurrencyCode={stored} />
    </>
  )
}

function ConverterPanel({ reportingCurrencyCode }: { reportingCurrencyCode: string }) {
  const { t } = useTranslation()
  const toast = useToast()
  const [amount, setAmount] = useState('100')
  const [fromCurrencyCode, setFromCurrencyCode] = useState('EUR')
  const [toCurrencyCode, setToCurrencyCode] = useState('')
  const [rateOn, setRateOn] = useState(() => isoDay(new Date()))
  const [busy, setBusy] = useState(false)
  const [converted, setConverted] = useState<CurrencyConversion | null>(null)
  const target = toCurrencyCode || reportingCurrencyCode || 'USD'

  const convert = async (event: React.FormEvent) => {
    event.preventDefault()
    setBusy(true)
    try {
      const answer = await graphql<{ ConvertCurrency: CurrencyConversion }>(CONVERT_CURRENCY, {
        amount: amount.trim(),
        fromCurrencyCode,
        toCurrencyCode: target,
        rateOn: rateOn || undefined,
      })
      setConverted(answer.ConvertCurrency)
    } catch (caught) {
      setConverted(null)
      toast.failure(caught, t('finance.failed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <SettingsSection card title={t('finance.converterTitle')} description={t('finance.converterHint')}>
      <form onSubmit={(event) => void convert(event)}>
        <div className="row">
          <label>
            <span>{t('finance.amount')}</span>
            <input inputMode="decimal" value={amount} onChange={(event) => setAmount(event.target.value)} />
          </label>
          <label>
            <span>{t('finance.fromCurrency')}</span>
            <CurrencyPicker value={fromCurrencyCode} label={t('finance.fromCurrency')} onChange={setFromCurrencyCode} />
          </label>
          <label>
            <span>{t('finance.toCurrency')}</span>
            <CurrencyPicker value={target} label={t('finance.toCurrency')} onChange={setToCurrencyCode} />
          </label>
          <label>
            <span>{t('finance.rateOn')}</span>
            <input
              type="date"
              value={rateOn}
              max={isoDay(new Date())}
              onChange={(event) => setRateOn(event.target.value)}
            />
          </label>
        </div>
        <div className="page-actions">
          <button type="submit" disabled={busy || !isDecimal(amount)}>
            {t('finance.convert')}
          </button>
        </div>
      </form>
      {converted ? (
        <p className="finance-converted">
          <strong>{formatMoney(amountOf(converted.convertedAmount), converted.toCurrencyCode)}</strong>{' '}
          <span className="muted">
            {t('finance.convertedAt', {
              rate: converted.rate,
              day: formatDay(converted.rateOn),
              source: converted.rateSource.toUpperCase(),
            })}
          </span>
        </p>
      ) : null}
    </SettingsSection>
  )
}
