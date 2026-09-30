import { useState } from 'react'

import { graphql } from '../../api'
import { formatMoney } from '../../components/common'
import { compact } from '../../components/seriesChart'
import { Select } from '../../components/select'
import { useToast } from '../../components/toast'
import { Key, useTranslation } from '../../i18n/i18n'
import { useQuery } from '../../components/useQuery'
import {
  CURRENCY_CODES,
  FinanceAccount,
  PERSON_ZONE,
  PersonZoneAnswer,
  REPORTING_CURRENCY,
  ReportingCurrencyAnswer,
  SpendingCategory,
  amountOf,
  hasAmount,
  setPersonZone,
} from './financeApi'

// What the Finance page's sections share: running a change and saying how
// it went, an amount in its currency, and the pickers several sections ask
// the same question with.

// usePersonZone reads the zone the person's agent keeps and sets it for the
// sections' default days and months, and says when that is done. The
// Finance page and the agent page's Finance tab wait for it before drawing
// a section. A failure to read it leaves the browser's zone rather than no
// page.
export function usePersonZone(): boolean {
  const zone = useQuery(() => graphql<PersonZoneAnswer>(PERSON_ZONE), [], { refresh: false })
  if (!zone.data && !zone.error) return false
  setPersonZone(zone.data?.ReadAgent?.timezone ?? '')
  return true
}

// useAct runs a change, says it worked or why it did not in a toast, and
// then reads the section again. Busy while it runs, so a second press
// does not send it twice.
export function useAct(reload?: () => Promise<unknown> | void) {
  const { t } = useTranslation()
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const act = async (
    work: () => Promise<unknown>,
    done: string,
    action?: { label: string; run: () => void | Promise<void> },
  ): Promise<boolean> => {
    setBusy(true)
    try {
      await work()
      toast.done(done, action)
      await reload?.()
      return true
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
      return false
    } finally {
      setBusy(false)
    }
  }
  const run = (document: string, variables: Record<string, unknown>, done: string) =>
    act(() => graphql(document, variables), done)
  return { busy, act, run }
}

// useReportingCurrency is the currency totals are shown in, as the server
// works it out: the person's choice, or the currency of their first
// finance account, else of their first asset (isChosen says which). Empty
// only when there is nothing to take one from. With a way to read it again.
export function useReportingCurrency(): {
  reportingCurrencyCode: string
  isChosen: boolean
  isLoaded: boolean
  reload: () => Promise<void>
} {
  const { data, reload } = useQuery(() => graphql<ReportingCurrencyAnswer>(REPORTING_CURRENCY), [], { refresh: false })
  return {
    reportingCurrencyCode: data?.ReportingCurrency.reportingCurrencyCode ?? '',
    isChosen: data?.ReportingCurrency.isChosen ?? false,
    isLoaded: data !== null,
    reload: () => reload(true),
  }
}

// Money is an amount in its currency, the way every amount on the
// dashboard is written: money out is negative and says so with its minus
// sign, uncoloured, as the usage and budget figures are. Nothing reported
// is a dash, not a zero.
export function Money({ amount, currency }: { amount?: string | number | null; currency?: string | null }) {
  if (amount === undefined || amount === null || (typeof amount === 'string' && !hasAmount(amount))) {
    return <span className="muted">—</span>
  }
  return <span>{formatMoney(typeof amount === 'number' ? amount : amountOf(amount), currency)}</span>
}

// compactMoney is an amount in a few characters, for a chart's axis: the
// currency's sign and a count to the thousand, $7.5k rather than
// $7,500.00, so the axis stays narrow and the plot keeps its width.
export function compactMoney(amount: number, currency?: string | null): string {
  const code = (currency || 'USD').toUpperCase()
  let sign = `${code} `
  try {
    const parts = new Intl.NumberFormat(undefined, { style: 'currency', currency: code }).formatToParts(0)
    sign = parts.find((part) => part.type === 'currency')?.value ?? sign
  } catch {
    // A code the browser does not know is written out.
  }
  return `${amount < 0 ? '-' : ''}${sign}${compact(Math.abs(amount))}`
}

// accountLabel names a finance account the way its statement does: its
// name and the last digits of its number, when the provider gave them.
export function accountLabel(account: Pick<FinanceAccount, 'accountName' | 'accountMask'>): string {
  return account.accountMask ? `${account.accountName} ··${account.accountMask}` : account.accountName
}

// spendingCategoryLabel names a spending category under its parent, so
// two called "Other" under different parents can be told apart.
export function spendingCategoryLabel(category: SpendingCategory, categories: SpendingCategory[]): string {
  const parent = category.parentSpendingCategoryId
    ? categories.find((candidate) => candidate.id === category.parentSpendingCategoryId)
    : undefined
  return parent ? `${parent.spendingCategoryName} › ${category.spendingCategoryName}` : category.spendingCategoryName
}

// spendingCategoryOptions is every spending category that can be chosen,
// sorted by the name shown, hidden ones left out unless already chosen.
export function spendingCategoryOptions(
  categories: SpendingCategory[],
  chosen?: string | null,
): { value: string; label: string }[] {
  return categories
    .filter((category) => !category.isHidden || category.id === chosen)
    .map((category) => ({ value: category.id, label: spendingCategoryLabel(category, categories) }))
    .sort((left, right) => left.label.localeCompare(right.label))
}

// CurrencyPicker is a currency code: one of those with an exchange rate,
// or any other three letters typed, since an account can be held in one
// the European Central Bank does not publish.
export function CurrencyPicker({
  value,
  label,
  onChange,
  disabled,
}: {
  value: string
  label: string
  onChange: (value: string) => void
  disabled?: boolean
}) {
  const codes = CURRENCY_CODES.includes(value) || !value ? CURRENCY_CODES : [value, ...CURRENCY_CODES]
  return (
    <Select
      block
      searchable
      allowCustom
      value={value}
      label={label}
      disabled={disabled}
      options={codes.map((code) => ({ value: code, label: code }))}
      onChange={(next) => onChange(next.trim().toUpperCase())}
    />
  )
}

// UnconvertedNote names the currencies a total left out for want of an
// exchange rate, where there are any. Totals never add currencies without
// converting them, so saying what was left out is the honest part.
export function UnconvertedNote({ currencyCodes }: { currencyCodes?: string[] | null }) {
  const { t } = useTranslation()
  if (!currencyCodes || currencyCodes.length === 0) return null
  return <p className="muted field-hint">{t('finance.unconverted', { currencies: currencyCodes.join(', ') })}</p>
}

// Words for the closed vocabularies the server speaks, falling through to
// the word itself for one added since.
export function useFinanceWords() {
  const { t } = useTranslation()
  const word = (prefix: string, value?: string | null): string => {
    if (!value) return ''
    const said = t(`${prefix}.${value}` as Key) as string | undefined
    return said || value
  }
  return {
    provider: (value?: string | null) => word('finance.provider', value),
    accountKind: (value?: string | null) => word('finance.accountKind', value),
    assetKind: (value?: string | null) => word('finance.assetKind', value),
    valuationSource: (value?: string | null) => word('finance.valuationSource', value),
    budgetPace: (value?: string | null) => word('finance.budgetPace', value),
    categorizedBy: (value?: string | null) => word('finance.categorizedBy', value),
    targetMeasure: (value?: string | null) => word('finance.targetMeasure', value),
  }
}
