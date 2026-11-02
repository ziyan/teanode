import { useMemo, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { graphql } from '../../api'
import { ErrorMessage, Loading, formatMoney } from '../../components/common'
import { ChevronLeftIcon, ChevronRightIcon } from '../../components/icons'
import { Select } from '../../components/select'
import { SeriesChart } from '../../components/seriesChart'
import { SettingsEmpty, SettingsSection } from '../../components/settingsList'
import { Tabs } from '../../components/tabs'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { FINANCE_TABS } from '../../pageTabs'
import { amountOf, monthBefore, monthLabel, personMonth } from './financeApi'
import { Money, compactMoney } from './financeCommon'
import { FinanceShared, SharedTransactions } from './financeShared'
import {
  AllFinanceShareLevel,
  LIST_SHARED_ALL_FINANCE_SPENDING,
  SharedFinanceAccount,
  SharedFinanceSpendingRow,
} from './financeShareApi'
import { RING_SLICE_COUNT, SpendingRing, foldIntoOther, ringSliceClass } from './spendingRing'

// What somebody who shares all of their finance shows the person, under
// Showing on the Finance page: one combined view across every account they
// hold, now and later, like the Spending page of their own. Spending is money
// out and in by month and spending category; at the transactions level there
// is a Transactions tab with the combined page. No account name, mask or
// balance is anywhere in it: those stay with per-account shares, which are
// listed in an Accounts tab when the same person also shares some. The tab is
// the path (/finance/spending, /finance/transactions, /finance/accounts), as
// on the person's own Finance page. docs/subsystems/finance.md ("Sharing")
// says what each level shows.

export function FinanceSharedAll({
  ownerUserId,
  ownerName,
  level,
  accounts,
  section,
}: {
  ownerUserId: string
  ownerName: string
  level: AllFinanceShareLevel
  // The accounts they also share one by one, as the per-account view shows them.
  accounts: SharedFinanceAccount[]
  section: string | undefined
}) {
  const navigate = useNavigate()
  const [parameters] = useSearchParams()
  const tabs = FINANCE_TABS.filter(
    (tab) =>
      tab.id === 'spending' ||
      (tab.id === 'transactions' && level === 'transactions') ||
      (tab.id === 'accounts' && accounts.length > 0),
  )
  // A section this person has no tab for is the first one.
  const active = tabs.find((tab) => tab.id === section)?.id ?? 'spending'
  const selectTab = (id: string) => {
    // The person stays chosen; a month, a page or an account belongs to the
    // tab left.
    const written = new URLSearchParams()
    const person = parameters.get('person')
    if (person) written.set('person', person)
    navigate({ pathname: `/finance/${id}`, search: written.toString() ? `?${written.toString()}` : '' })
  }
  return (
    <>
      {tabs.length > 1 ? <Tabs items={tabs} active={active} onSelect={selectTab} /> : null}
      {active === 'spending' ? <SharedAllSpending ownerUserId={ownerUserId} ownerName={ownerName} /> : null}
      {active === 'transactions' ? (
        <SharedTransactions key={`transactions:${ownerUserId}`} ownerUserId={ownerUserId} />
      ) : null}
      {active === 'accounts' ? <FinanceShared ownerName={ownerName} accounts={accounts} /> : null}
    </>
  )
}

type MonthTotal = { moneyOut: number; moneyIn: number }

// SharedAllSpending is the combined Spending page: a year of money out and in
// a column a month, and under it the month chosen there, by spending
// category. Every month the person has is read in one answer, so choosing a
// month is not another read. The month is in the address.
function SharedAllSpending({ ownerUserId, ownerName }: { ownerUserId: string; ownerName: string }) {
  const { t } = useTranslation()
  const [search, setSearch] = useSearchParams()
  const currentMonth = personMonth()
  const spending = useQuery(
    async () =>
      (
        await graphql<{ ListSharedFinanceSpending: SharedFinanceSpendingRow[] | null }>(
          LIST_SHARED_ALL_FINANCE_SPENDING,
          { ownerUserId },
        )
      ).ListSharedFinanceSpending ?? [],
    [ownerUserId],
    { refresh: false },
  )
  const rows = spending.data
  const currencies = useMemo(() => {
    // Each currency by how much went out in it, largest first: money in two
    // currencies cannot be added up, so one is shown at a time.
    const totals = new Map<string, number>()
    for (const row of rows ?? []) {
      totals.set(row.currencyCode, (totals.get(row.currencyCode) ?? 0) + amountOf(row.moneyOut))
    }
    return [...totals.entries()].sort((first, second) => second[1] - first[1]).map(([code]) => code)
  }, [rows])
  const requestedCurrency = search.get('currency') ?? ''
  const currency = currencies.includes(requestedCurrency) ? requestedCurrency : (currencies[0] ?? '')
  const monthsOfCurrency = useMemo(() => {
    const months = new Map<string, MonthTotal>()
    for (const row of rows ?? []) {
      if (row.currencyCode !== currency) continue
      const total = months.get(row.spendingMonth) ?? { moneyOut: 0, moneyIn: 0 }
      total.moneyOut += amountOf(row.moneyOut)
      total.moneyIn += amountOf(row.moneyIn)
      months.set(row.spendingMonth, total)
    }
    return months
  }, [rows, currency])
  // Newest first: this month back to the first month with money, and at
  // least a year of them.
  const firstMonth = [...(rows ?? [])].map((row) => row.spendingMonth).sort()[0]
  const oldest = firstMonth && firstMonth < currentMonth ? firstMonth : monthBefore(currentMonth, 11)
  const monthOptions: string[] = []
  for (let month = currentMonth; month >= oldest && monthOptions.length < 1200; month = monthBefore(month, 1)) {
    monthOptions.push(month)
  }
  const requestedMonth = search.get('month') ?? ''
  const month = monthOptions.includes(requestedMonth) ? requestedMonth : currentMonth
  const select = (written: Record<string, string>) =>
    setSearch(
      (previous) => {
        const next = new URLSearchParams(previous)
        for (const [name, value] of Object.entries(written)) {
          if (value) next.set(name, value)
          else next.delete(name)
        }
        return next
      },
      { replace: true },
    )
  const selectMonth = (chosen: string) => select({ month: chosen === currentMonth ? '' : chosen })
  const index = monthOptions.indexOf(month)
  const earlier = index >= 0 && index < monthOptions.length - 1 ? monthOptions[index + 1] : null
  const later = index > 0 ? monthOptions[index - 1] : null

  const toMonth = month >= monthBefore(currentMonth, 11) ? currentMonth : month
  const chartMonths = Array.from({ length: 12 }, (_, offset) => monthBefore(toMonth, 11 - offset))
  const chosen = monthsOfCurrency.get(month)
  const hasMoney = (rows ?? []).length > 0

  const picker = hasMoney ? (
    <div className="finance-period-bar">
      <div className="finance-period">
        {currencies.length > 1 ? (
          <label className="shrink">
            <span className="visually-hidden">{t('finance.sharedCurrency')}</span>
            <Select
              value={currency}
              label={t('finance.sharedCurrency')}
              options={currencies.map((code) => ({ value: code, label: code }))}
              onChange={(code) => select({ currency: code === currencies[0] ? '' : code })}
            />
          </label>
        ) : null}
        <div className="finance-period-step">
          <Tooltip label={t('finance.previousMonth')}>
            <button
              type="button"
              className="icon-button"
              aria-label={t('finance.previousMonth')}
              disabled={earlier === null}
              onClick={() => earlier && selectMonth(earlier)}
            >
              <ChevronLeftIcon size={16} />
            </button>
          </Tooltip>
          <Select
            className="finance-period-select"
            value={month}
            label={t('finance.month')}
            options={monthOptions.map((option) => ({ value: option, label: monthLabel(option, 'long') }))}
            onChange={selectMonth}
          />
          <Tooltip label={t('finance.nextMonth')}>
            <button
              type="button"
              className="icon-button"
              aria-label={t('finance.nextMonth')}
              disabled={later === null}
              onClick={() => later && selectMonth(later)}
            >
              <ChevronRightIcon size={16} />
            </button>
          </Tooltip>
        </div>
      </div>
    </div>
  ) : null

  return (
    <>
      {picker}
      <SettingsSection
        card
        title={t('finance.sharedAllTitle', { name: ownerName })}
        description={t('finance.sharedAllHint')}
      >
        <ErrorMessage error={spending.error} />
        {spending.loading && !spending.data ? <Loading /> : null}
        {rows && !hasMoney ? <SettingsEmpty>{t('finance.sharedAllNone', { name: ownerName })}</SettingsEmpty> : null}
        {hasMoney ? (
          <SeriesChart
            label={t('finance.sharedAllTitle', { name: ownerName })}
            keys={chartMonths}
            keyLabel={(key) => monthLabel(key)}
            isEveryDipShown
            format={(value) => formatMoney(value, currency)}
            axisFormat={(value, precision) => compactMoney(value, currency, precision)}
            headline={formatMoney(chosen?.moneyOut ?? 0, currency)}
            caption={t('finance.sharedMoneyOutInMonth', { month: monthLabel(month, 'long') })}
            series={[
              {
                id: 'moneyIn',
                label: t('finance.sharedMoneyIn'),
                tone: 'output',
                shape: 'column',
                values: chartMonths.map((key) => monthsOfCurrency.get(key)?.moneyIn ?? 0),
              },
              {
                id: 'moneyOut',
                label: t('finance.sharedMoneyOut'),
                tone: 'cached',
                shape: 'column',
                values: chartMonths.map((key) => monthsOfCurrency.get(key)?.moneyOut ?? 0),
              },
            ]}
            selectedKey={month}
            onSelectKey={selectMonth}
          />
        ) : null}
      </SettingsSection>
      {hasMoney ? (
        <SharedAllCategories
          rows={(rows ?? []).filter((row) => row.spendingMonth === month)}
          currency={currency}
          month={month}
        />
      ) : null}
    </>
  )
}

// SharedAllCategories is where the month's money went, a ring of the largest
// spending categories and under it the table that is its legend: every
// category, what went out and what came in, and the transactions in it.
function SharedAllCategories({
  rows,
  currency,
  month,
}: {
  rows: SharedFinanceSpendingRow[]
  currency: string
  month: string
}) {
  const { t } = useTranslation()
  const [highlightedKey, setHighlightedKey] = useState<string | null>(null)
  const lines = rows
    .filter((row) => row.currencyCode === currency)
    .map((row) => ({
      key: row.spendingCategoryName || '\u0000none',
      name: row.spendingCategoryName || t('finance.sharedNoCategory'),
      moneyOut: amountOf(row.moneyOut),
      moneyIn: amountOf(row.moneyIn),
      financeTransactionCount: row.financeTransactionCount,
    }))
    .sort((first, second) => second.moneyOut - first.moneyOut || first.name.localeCompare(second.name))
  const totalOut = lines.reduce((sum, line) => sum + line.moneyOut, 0)
  const totalIn = lines.reduce((sum, line) => sum + line.moneyIn, 0)
  const totalCount = lines.reduce((sum, line) => sum + line.financeTransactionCount, 0)
  const slices = foldIntoOther(
    lines.map((line) => ({ key: line.key, label: line.name, amount: line.moneyOut })),
    RING_SLICE_COUNT,
  ).map((slice) =>
    slice.isOther ? { ...slice, label: t('finance.otherCategories', { count: slice.foldedCount }) } : slice,
  )
  const sliceIndexes = new Map(slices.map((slice, position) => [slice.key, position]))
  const otherIndex = slices.findIndex((slice) => slice.isOther)
  const sliceOf = (key: string): { className: string; sliceKey: string } | null => {
    const position = sliceIndexes.get(key)
    if (position !== undefined) return { className: ringSliceClass(slices[position], position), sliceKey: key }
    return otherIndex >= 0 ? { className: 'other', sliceKey: slices[otherIndex].key } : null
  }
  const percent = new Intl.NumberFormat(undefined, { style: 'percent', maximumFractionDigits: 0 })
  return (
    <SettingsSection
      card
      title={t('finance.summaryTitle')}
      description={t('finance.sharedAllCategoriesHint', { month: monthLabel(month, 'long') })}
    >
      {lines.length === 0 ? <SettingsEmpty>{t('finance.sharedSpendingNone')}</SettingsEmpty> : null}
      {slices.length > 0 ? (
        <SpendingRing
          slices={slices}
          currency={currency}
          label={t('finance.ringLabel', { month: monthLabel(month, 'long') })}
          totalLabel={t('finance.sharedMoneyOut')}
          totalAmount={totalOut}
          highlightedKey={highlightedKey}
        />
      ) : null}
      {lines.length > 0 ? (
        <div className="table-wrap">
          <table className="numbers-table finance-table finance-summary-table">
            <thead>
              <tr>
                <th>{t('finance.spendingCategory')}</th>
                <th className="numeric">{t('finance.sharedMoneyOut')}</th>
                <th className="numeric">{t('finance.sharedMoneyIn')}</th>
                <th className="numeric optional">{t('finance.transactionCount')}</th>
              </tr>
            </thead>
            <tbody>
              {lines.map((line) => {
                const slice = sliceOf(line.key)
                const fraction = slice && totalOut > 0 && line.moneyOut > 0 ? line.moneyOut / totalOut : null
                // A sliver is "<1%" rather than a "0%" that reads as nothing.
                const share =
                  fraction === null ? null : fraction < 0.005 ? `<${percent.format(0.01)}` : percent.format(fraction)
                return (
                  <tr
                    key={line.key}
                    onPointerEnter={slice ? () => setHighlightedKey(slice.sliceKey) : undefined}
                    onPointerLeave={slice ? () => setHighlightedKey(null) : undefined}
                  >
                    <td className="finance-group-cell">
                      {slice ? <i className={`spending-ring-swatch ${slice.className}`} aria-hidden="true" /> : null}
                      <span className="finance-group-name" title={line.name}>
                        {line.name}
                      </span>
                    </td>
                    <td className="numeric">
                      <Money amount={line.moneyOut} currency={currency} />
                      {share ? <span className="muted finance-share">{share}</span> : null}
                    </td>
                    <td className="numeric">
                      <Money amount={line.moneyIn} currency={currency} />
                    </td>
                    <td className="numeric optional">{line.financeTransactionCount}</td>
                  </tr>
                )
              })}
            </tbody>
            <tfoot>
              <tr>
                <th>{t('finance.totalIn', { currency })}</th>
                <th className="numeric">
                  <Money amount={totalOut} currency={currency} />
                </th>
                <th className="numeric">
                  <Money amount={totalIn} currency={currency} />
                </th>
                <th className="numeric optional">{totalCount}</th>
              </tr>
            </tfoot>
          </table>
        </div>
      ) : null}
    </SettingsSection>
  )
}
