import { useRef, useState } from 'react'

import { graphql } from '../../api'
import { useToast } from '../../components/toast'
import { useQuery } from '../../components/useQuery'
import { Key, useTranslation } from '../../i18n/i18n'
import { uploadFiles } from '../../upload'
import { isDecimal } from './financeApi'
import { useReportingCurrency } from './financeCommon'
import { CurrencyPairRate, EXCHANGE_RATE } from './financeEntryApi'

// What the dialogs that record a receipt, a paystub, a tax form or the rows
// of a statement by hand have in common: the file that is the record's
// source, the current exchange rate under a currency, and reading what was
// typed into the arguments the server takes.

// blankToUndefined is what was typed without its surrounding spaces, and
// nothing for a field left empty, so an optional argument is left out of
// the request rather than sent empty.
export function blankToUndefined(typed: string): string | undefined {
  const trimmed = typed.trim()
  return trimmed === '' ? undefined : trimmed
}

// isOptionalDecimal says an optional amount is empty or a number the server
// takes.
export function isOptionalDecimal(typed: string): boolean {
  return typed.trim() === '' || isDecimal(typed)
}

// isDay says what was typed is a day written 2006-01-02.
export function isDay(typed: string): boolean {
  return /^\d{4}-\d{2}-\d{2}$/.test(typed.trim())
}

// ExchangeRateHint says what one unit of the chosen currency is worth in the
// person's reporting currency, "1 EUR = 1.08 USD", under the currency
// field; nothing for the reporting currency itself, for a code that is not
// three letters yet, or when the rate cannot be read.
export function ExchangeRateHint({ currencyCode }: { currencyCode: string }) {
  const { t } = useTranslation()
  const { reportingCurrencyCode } = useReportingCurrency()
  const code = currencyCode.trim().toUpperCase()
  const isForeign = /^[A-Z]{3}$/.test(code) && reportingCurrencyCode !== '' && code !== reportingCurrencyCode
  const rate = useQuery(
    async (): Promise<CurrencyPairRate | null> => {
      if (!isForeign) return null
      try {
        const answer = await graphql<{ ExchangeRate?: CurrencyPairRate | null }>(EXCHANGE_RATE, {
          fromCurrencyCode: code,
          toCurrencyCode: reportingCurrencyCode,
        })
        return answer?.ExchangeRate ?? null
      } catch {
        return null
      }
    },
    [code, reportingCurrencyCode, isForeign],
    { refresh: false },
  )
  const found = isForeign ? rate.data : null
  const number = found ? Number(found.rate) : NaN
  // The rate loaded for the previous code is still held while the new one loads.
  if (!found || found.fromCurrencyCode !== code || !Number.isFinite(number) || number <= 0) return null
  return (
    <p className="muted field-hint finance-entry-rate">
      {t('finance.entryExchangeRate', {
        from: found.fromCurrencyCode,
        rate: new Intl.NumberFormat(undefined, { maximumSignificantDigits: 6 }).format(number),
        to: found.toCurrencyCode,
      })}
    </p>
  )
}

export type SourceFile = { agentAttachmentId: string; name: string }

// SourceFileField is the file a record is kept against: the photo, PDF or
// screenshot it was typed from. It goes up the way a file goes to the
// agent, as soon as it is chosen, and is named by its id in the record.
export function SourceFileField({
  label,
  hint,
  accept,
  uploadPath,
  notStoredKey,
  isDisabled,
  onChange,
}: {
  label: Key
  hint?: Key
  accept: string
  uploadPath: string
  notStoredKey: Key
  isDisabled?: boolean
  onChange: (sourceFile: SourceFile | null) => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const picker = useRef<HTMLInputElement | null>(null)
  const [isUploading, setIsUploading] = useState(false)

  const upload = async (file: File) => {
    onChange(null)
    setIsUploading(true)
    try {
      const uploaded = (await uploadFiles('POST', uploadPath, [file], () => undefined).promise) as {
        attachments?: { id: string }[]
      }
      const agentAttachmentId = uploaded?.attachments?.[0]?.id
      if (!agentAttachmentId) throw new Error(t(notStoredKey))
      onChange({ agentAttachmentId, name: file.name })
    } catch (caught) {
      if (picker.current) picker.current.value = ''
      toast.failure(caught, t('finance.failed'))
    } finally {
      setIsUploading(false)
    }
  }

  return (
    <label>
      <span>{t(label)}</span>
      <input
        ref={picker}
        type="file"
        accept={accept}
        disabled={isDisabled || isUploading}
        data-testid="entry-source-file"
        onChange={(event) => {
          const file = event.target.files?.[0]
          if (file) void upload(file)
          else onChange(null)
        }}
      />
      {hint ? <span className="muted field-hint">{t(hint)}</span> : null}
    </label>
  )
}
