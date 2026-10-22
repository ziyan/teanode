import { useCallback, useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { graphql } from '../../api'
import { ErrorMessage, Loading, Tag, formatTime } from '../../components/common'
import { Column, DataTable, Range } from '../../components/dataTable'
import { Select } from '../../components/select'
import { SettingsEmpty, SettingsSection } from '../../components/settingsList'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { formatDay, monthLabel } from './financeApi'
import { Money, accountLabel, useFinanceWords } from './financeCommon'
import {
  LIST_SHARED_FINANCE_SPENDING,
  LIST_SHARED_FINANCE_TRANSACTIONS,
  SharedFinanceAccount,
  SharedFinanceSpendingRow,
  SharedFinanceTransaction,
  SharedFinanceTransactionPage,
  isAtLeast,
} from './financeShareApi'
import { levelName } from './financeSharing'

// What somebody shares with the person, which the Finance page shows when
// they are chosen under Showing: their accounts at the level each was shared
// at, then for the one chosen what that level adds: the months by spending
// category, and the transactions. Nothing here can be changed, and the
// agent reads none of it.

export function FinanceShared({ ownerName, accounts }: { ownerName: string; accounts: SharedFinanceAccount[] }) {
  const { t } = useTranslation()
  const [parameters, setParameters] = useSearchParams()
  // The account whose months and transactions are shown: the one in the
  // address if it is shared at the spending level or more, else the first
  // that is. A balance alone has nothing more to show.
  const detailed = accounts.filter((account) => isAtLeast(account.financeShareLevel, 'spending'))
  const chosen =
    detailed.find((account) => account.financeAccountId === parameters.get('account')) ?? detailed[0] ?? null

  return (
    <>
      <SharedAccounts ownerName={ownerName} accounts={accounts} />
      {chosen ? (
        <>
          {detailed.length > 1 ? (
            <div className="finance-toolbar">
              <label>
                <span>{t('finance.sharedAccount')}</span>
                <Select
                  value={chosen.financeAccountId}
                  label={t('finance.sharedAccount')}
                  options={detailed.map((account) => ({
                    value: account.financeAccountId,
                    label: accountLabel(account as { accountName: string; accountMask?: string }),
                  }))}
                  onChange={(next) =>
                    setParameters((previous) => {
                      const written = new URLSearchParams(previous)
                      written.set('account', next)
                      written.delete('page')
                      return written
                    })
                  }
                />
              </label>
            </div>
          ) : null}
          <SharedSpending key={`spending:${chosen.financeAccountId}`} account={chosen} />
          {isAtLeast(chosen.financeShareLevel, 'transactions') ? (
            <SharedTransactions key={`transactions:${chosen.financeAccountId}`} account={chosen} />
          ) : null}
        </>
      ) : null}
    </>
  )
}

function SharedAccounts({ ownerName, accounts }: { ownerName: string; accounts: SharedFinanceAccount[] }) {
  const { t } = useTranslation()
  const words = useFinanceWords()
  return (
    <SettingsSection
      card
      title={t('finance.sharedAccountsTitle', { name: ownerName })}
      description={t('finance.sharedAccountsHint')}
    >
      {accounts.length === 0 ? <SettingsEmpty>{t('finance.sharedNone', { name: ownerName })}</SettingsEmpty> : null}
      {accounts.length > 0 ? (
        <div className="table-wrap">
          <table className="numbers-table finance-table">
            <thead>
              <tr>
                <th>{t('finance.account')}</th>
                <th>{t('finance.accountKindLabel')}</th>
                <th className="numeric">{t('finance.currentBalance')}</th>
                <th>{t('finance.balanceAt')}</th>
                <th className="numeric">{t('finance.creditUsedShare')}</th>
                <th>{t('finance.sharedFor')}</th>
              </tr>
            </thead>
            <tbody>
              {accounts.map((account) => (
                <tr key={account.financeAccountId}>
                  <td>{accountLabel(account as { accountName: string; accountMask?: string })}</td>
                  <td>{words.accountKind(account.accountKind)}</td>
                  <td className="numeric">
                    <Money amount={account.currentBalance} currency={account.currencyCode} />
                  </td>
                  <td className="muted">{account.balanceAt ? formatTime(account.balanceAt) : ''}</td>
                  <td className="numeric">
                    {account.creditUsage?.usageShare !== undefined && account.creditUsage?.usageShare !== null ? (
                      <span title={creditUsageTitle(account, t)}>
                        {Math.round(account.creditUsage.usageShare * 100)}%
                      </span>
                    ) : null}
                  </td>
                  <td>
                    <Tag value={levelName(t, account.financeShareLevel)} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </SettingsSection>
  )
}

// What a card owes against its limit, in words, for the hover over its share.
function creditUsageTitle(account: SharedFinanceAccount, t: ReturnType<typeof useTranslation>['t']): string {
  const usage = account.creditUsage
  if (!usage?.owedAmount || !usage.creditLimitAmount) return ''
  return t('finance.sharedUsageOf', {
    owed: Number(usage.owedAmount).toLocaleString(undefined, { style: 'currency', currency: account.currencyCode }),
    limit: Number(usage.creditLimitAmount).toLocaleString(undefined, {
      style: 'currency',
      currency: account.currencyCode,
    }),
  })
}

function SharedSpending({ account }: { account: SharedFinanceAccount }) {
  const { t } = useTranslation()
  const spending = useQuery(
    async () =>
      (
        await graphql<{ ListSharedFinanceSpending: SharedFinanceSpendingRow[] }>(LIST_SHARED_FINANCE_SPENDING, {
          financeAccountId: account.financeAccountId,
        })
      ).ListSharedFinanceSpending,
    [account.financeAccountId],
    { refresh: false },
  )
  const rows = spending.data ?? []
  return (
    <SettingsSection card title={t('finance.sharedSpendingTitle')} description={t('finance.sharedSpendingHint')}>
      <ErrorMessage error={spending.error} />
      {spending.loading && !spending.data ? <Loading /> : null}
      {spending.data && rows.length === 0 ? <SettingsEmpty>{t('finance.sharedSpendingNone')}</SettingsEmpty> : null}
      {rows.length > 0 ? (
        <div className="table-wrap">
          <table className="numbers-table finance-table">
            <thead>
              <tr>
                <th>{t('finance.month')}</th>
                <th>{t('finance.spendingCategory')}</th>
                <th className="numeric">{t('finance.sharedMoneyOut')}</th>
                <th className="numeric">{t('finance.sharedMoneyIn')}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={`${row.spendingMonth}:${row.spendingCategoryName}:${row.currencyCode}`}>
                  <td>{monthLabel(row.spendingMonth, 'long')}</td>
                  <td>{row.spendingCategoryName || <span className="muted">{t('finance.sharedNoCategory')}</span>}</td>
                  <td className="numeric">
                    <Money amount={row.moneyOut} currency={row.currencyCode} />
                  </td>
                  <td className="numeric">
                    <Money amount={row.moneyIn} currency={row.currencyCode} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </SettingsSection>
  )
}

function SharedTransactions({ account }: { account: SharedFinanceAccount }) {
  const { t, plural, language } = useTranslation()
  // Which page the table shows, as it says once it has read the address:
  // nothing is asked for before then, so a link to page four does not fetch
  // page one first.
  const [range, setRange] = useState<Range | null>(null)
  const onRange = useCallback((next: Range) => setRange(next), [])
  const page = useQuery(
    async () =>
      range
        ? (
            await graphql<{ ListSharedFinanceTransactions: SharedFinanceTransactionPage }>(
              LIST_SHARED_FINANCE_TRANSACTIONS,
              { financeAccountId: account.financeAccountId, limit: range.limit, offset: range.offset },
            )
          ).ListSharedFinanceTransactions
        : null,
    [account.financeAccountId, range?.offset, range?.limit],
    { refresh: false },
  )
  // The count of the last answer outlives a page being read, so the pager
  // does not fall back to nothing between two pages.
  const [totalCount, setTotalCount] = useState(0)
  useEffect(() => {
    if (page.data) setTotalCount(page.data.totalCount)
  }, [page.data])
  const columns: Column<SharedFinanceTransaction>[] = [
    {
      key: 'postedOn',
      header: t('finance.postedOn'),
      value: (row) => row.postedOn,
      render: (row) => formatDay(row.postedOn),
    },
    {
      key: 'amount',
      header: t('finance.amount'),
      numeric: true,
      value: (row) => row.amount,
      render: (row) => <Money amount={row.amount} currency={row.currencyCode} />,
    },
    {
      key: 'merchantName',
      header: t('finance.merchant'),
      value: (row) => row.merchantName ?? '',
      render: (row) => row.merchantName ?? '',
    },
    {
      key: 'spendingCategoryName',
      header: t('finance.spendingCategory'),
      value: (row) => row.spendingCategoryName ?? '',
      render: (row) => row.spendingCategoryName || <span className="muted">{t('finance.sharedNoCategory')}</span>,
    },
  ]
  return (
    <SettingsSection
      card
      title={t('finance.sharedTransactionsTitle')}
      description={t('finance.sharedTransactionsHint')}
    >
      <ErrorMessage error={page.error} />
      <DataTable
        columns={columns}
        rows={page.data?.financeTransactions ?? []}
        rowKey={(row) => row.id}
        loading={!page.data}
        remote={{ total: totalCount, onRange }}
        emptyMessage={t('finance.noTransactions')}
        countLabel={(count) =>
          plural(
            count,
            { one: 'finance.transactionCountOne', other: 'finance.transactionCountOther' },
            { count: count.toLocaleString(language) },
          )
        }
      />
    </SettingsSection>
  )
}
