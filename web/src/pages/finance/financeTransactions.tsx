import { useEffect, useMemo, useState } from 'react'

import { graphql } from '../../api'
import { ErrorMessage, Loading, Tag } from '../../components/common'
import { Column, DataTable } from '../../components/dataTable'
import { Select } from '../../components/select'
import { SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import {
  CATEGORIZE_TRANSACTION,
  FINANCE_ACCOUNTS,
  FINANCE_TRANSACTIONS,
  FinanceAccount,
  FinanceTransaction,
  FinanceTransactionPage,
  MARK_TRANSFER,
  SPENDING_CATEGORIES,
  SpendingCategory,
  amountOf,
  formatDay,
} from './financeApi'
import { Money, accountLabel, spendingCategoryOptions, useFinanceWords } from './financeCommon'

// How many finance transactions one read brings, and one Load more adds.
const PAGE_SIZE = 100

type Filters = { from: string; to: string; financeAccountId: string; text: string; isUncategorized: boolean }

// The finance transactions, newest first, narrowed on the server by dates,
// a finance account, words and whether a spending category is missing, and
// read a page at a time from where the last page ended. Each one's spending
// category is changed where it is, with the offer to do the same for every
// transaction from that merchant, and each can be marked a transfer, which
// takes it out of spending and income.
export function FinanceTransactionsSection() {
  const { t } = useTranslation()
  const toast = useToast()
  const words = useFinanceWords()
  const [filters, setFilters] = useState<Filters>({
    from: '',
    to: '',
    financeAccountId: '',
    text: '',
    isUncategorized: false,
  })
  // The words are sent once typing pauses, not on every key.
  const [typed, setTyped] = useState('')
  useEffect(() => {
    const timer = window.setTimeout(() => setFilters((previous) => ({ ...previous, text: typed.trim() })), 400)
    return () => window.clearTimeout(timer)
  }, [typed])

  const variables = {
    from: filters.from || undefined,
    to: filters.to || undefined,
    financeAccountId: filters.financeAccountId || undefined,
    text: filters.text || undefined,
    isUncategorized: filters.isUncategorized || undefined,
    limit: PAGE_SIZE,
  }
  const filterKey = JSON.stringify(variables)
  const first = useQuery(
    () => graphql<{ FinanceTransactions: FinanceTransactionPage }>(FINANCE_TRANSACTIONS, variables),
    [filterKey],
    { refresh: false },
  )
  const accounts = useQuery(() => graphql<{ FinanceAccounts: FinanceAccount[] }>(FINANCE_ACCOUNTS), [], {
    refresh: false,
  })
  const categories = useQuery(() => graphql<{ SpendingCategories: SpendingCategory[] }>(SPENDING_CATEGORIES), [], {
    refresh: false,
  })

  // The pages read after the first, and where the next one starts. Held
  // here rather than refetched, so changing one row's spending category
  // does not throw away the pages below it.
  const [more, setMore] = useState<{ rows: FinanceTransaction[]; after: string | null; isLoaded: boolean }>({
    rows: [],
    after: null,
    isLoaded: false,
  })
  const [isLoadingMore, setIsLoadingMore] = useState(false)
  // What changed on a row since it was read, laid over it.
  const [changed, setChanged] = useState<Record<string, Partial<FinanceTransaction>>>({})
  useEffect(() => {
    setMore({ rows: [], after: null, isLoaded: false })
    setChanged({})
  }, [filterKey])

  const firstPage = first.data?.FinanceTransactions
  const after = more.isLoaded ? more.after : (firstPage?.nextCursor ?? null)
  const rows = useMemo(
    () =>
      [...(firstPage?.financeTransactions ?? []), ...more.rows].map((row) =>
        changed[row.id] ? { ...row, ...changed[row.id] } : row,
      ),
    [firstPage, more.rows, changed],
  )
  const accountList = accounts.data?.FinanceAccounts ?? []
  const categoryList = categories.data?.SpendingCategories ?? []

  const loadMore = async () => {
    if (!after) return
    setIsLoadingMore(true)
    try {
      const answer = await graphql<{ FinanceTransactions: FinanceTransactionPage }>(FINANCE_TRANSACTIONS, {
        ...variables,
        after,
      })
      setMore((previous) => ({
        rows: [...previous.rows, ...answer.FinanceTransactions.financeTransactions],
        after: answer.FinanceTransactions.nextCursor ?? null,
        isLoaded: true,
      }))
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setIsLoadingMore(false)
    }
  }

  const categorize = async (row: FinanceTransaction, spendingCategoryId: string) => {
    try {
      const answer = await graphql<{ CategorizeTransaction: { financeTransaction: FinanceTransaction } }>(
        CATEGORIZE_TRANSACTION,
        { financeTransactionId: row.id, spendingCategoryId: spendingCategoryId || null },
      )
      setChanged((previous) => ({ ...previous, [row.id]: answer.CategorizeTransaction.financeTransaction }))
      const merchant = row.merchantName || row.description
      // Offered, not done: one grocery run filed under dining should not
      // move every visit to that shop.
      toast.done(
        t('finance.categorized'),
        spendingCategoryId && merchant
          ? {
              label: t('finance.alwaysForMerchant'),
              run: async () => {
                const made = await graphql<{ CategorizeTransaction: { spendingRule?: { matchText: string } | null } }>(
                  CATEGORIZE_TRANSACTION,
                  { financeTransactionId: row.id, spendingCategoryId, shouldCreateSpendingRule: true },
                )
                toast.done(
                  t('finance.spendingRuleMade', {
                    merchant: made.CategorizeTransaction.spendingRule?.matchText || merchant,
                  }),
                )
                // The rule applies to the merchant's earlier transactions
                // too, so the list is read again from the top.
                setMore({ rows: [], after: null, isLoaded: false })
                setChanged({})
                await first.reload(true)
              },
            }
          : undefined,
      )
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    }
  }

  const markTransfer = async (row: FinanceTransaction, isTransfer: boolean) => {
    try {
      const answer = await graphql<{ MarkTransfer: FinanceTransaction }>(MARK_TRANSFER, {
        financeTransactionId: row.id,
        isTransfer,
      })
      setChanged((previous) => ({ ...previous, [row.id]: answer.MarkTransfer }))
      toast.done(isTransfer ? t('finance.markedTransfer') : t('finance.unmarkedTransfer'))
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    }
  }

  const columns: Column<FinanceTransaction>[] = [
    {
      key: 'postedOn',
      header: t('finance.postedOn'),
      value: (row) => row.postedOn,
      render: (row) => formatDay(row.postedOn),
      sort: (left, right) => left.postedOn.localeCompare(right.postedOn),
    },
    {
      key: 'description',
      header: t('finance.description'),
      value: (row) => [row.merchantName, row.description].filter(Boolean).join(' · '),
      render: (row) => (
        <span className="finance-description">
          {row.merchantName || row.description}
          {row.merchantName && row.description !== row.merchantName ? (
            <span className="muted"> · {row.description}</span>
          ) : null}
          {row.isPending ? (
            <>
              {' '}
              <Tag value={t('finance.pending')} />
            </>
          ) : null}
        </span>
      ),
    },
    {
      key: 'financeAccount',
      header: t('finance.account'),
      value: (row) => {
        const account = accountList.find((candidate) => candidate.id === row.financeAccountId)
        return account ? accountLabel(account) : ''
      },
    },
    {
      key: 'spendingCategory',
      header: t('finance.spendingCategory'),
      render: (row) => (
        <span className="finance-category-cell">
          <Select
            value={row.spendingCategoryId ?? ''}
            label={t('finance.spendingCategory')}
            options={[
              { value: '', label: t('finance.uncategorized') },
              ...spendingCategoryOptions(categoryList, row.spendingCategoryId),
            ]}
            onChange={(value) => void categorize(row, value)}
          />
          {row.categorizedBy && row.categorizedBy !== 'person' && row.spendingCategoryId ? (
            <span className="muted finance-categorized-by">{words.categorizedBy(row.categorizedBy)}</span>
          ) : null}
        </span>
      ),
    },
    {
      key: 'isTransfer',
      header: t('finance.transfer'),
      render: (row) => (
        <input
          type="checkbox"
          checked={row.isTransfer}
          aria-label={`${row.merchantName || row.description}: ${t('finance.transfer')}`}
          onChange={(event) => void markTransfer(row, event.target.checked)}
        />
      ),
    },
    {
      key: 'amount',
      header: t('finance.amount'),
      numeric: true,
      value: (row) => row.amount,
      render: (row) => <Money amount={row.amount} currency={row.currencyCode} />,
      sort: (left, right) => amountOf(left.amount) - amountOf(right.amount),
    },
  ]

  return (
    <SettingsSection card title={t('finance.transactionsTitle')} description={t('finance.transactionsHint')}>
      <div className="row finance-filters">
        <label>
          <span>{t('finance.from')}</span>
          <input
            type="date"
            value={filters.from}
            max={filters.to || undefined}
            onChange={(event) => setFilters((previous) => ({ ...previous, from: event.target.value }))}
          />
        </label>
        <label>
          <span>{t('finance.to')}</span>
          <input
            type="date"
            value={filters.to}
            min={filters.from || undefined}
            onChange={(event) => setFilters((previous) => ({ ...previous, to: event.target.value }))}
          />
        </label>
        <label>
          <span>{t('finance.account')}</span>
          <Select
            block
            value={filters.financeAccountId}
            label={t('finance.account')}
            options={[
              { value: '', label: t('finance.allAccounts') },
              ...accountList.map((account) => ({ value: account.id, label: accountLabel(account) })),
            ]}
            onChange={(value) => setFilters((previous) => ({ ...previous, financeAccountId: value }))}
          />
        </label>
        <label>
          <span>{t('finance.searchText')}</span>
          <input type="search" value={typed} onChange={(event) => setTyped(event.target.value)} />
        </label>
      </div>
      <label className="checkbox">
        <input
          type="checkbox"
          checked={filters.isUncategorized}
          onChange={(event) => setFilters((previous) => ({ ...previous, isUncategorized: event.target.checked }))}
        />
        {t('finance.onlyUncategorized')}
      </label>
      <ErrorMessage error={first.error} />
      {first.loading && !first.data ? <Loading /> : null}
      {first.data ? (
        <DataTable
          columns={columns}
          rows={rows}
          rowKey={(row) => row.id}
          loading={first.loading}
          emptyMessage={t('finance.noTransactions')}
          countLabel={(count) => t('finance.transactionsLoaded', { count: String(count) })}
        />
      ) : null}
      {after ? (
        <div className="page-actions page-actions-end">
          <button type="button" disabled={isLoadingMore} onClick={() => void loadMore()}>
            {t('finance.loadMore')}
          </button>
        </div>
      ) : null}
    </SettingsSection>
  )
}
