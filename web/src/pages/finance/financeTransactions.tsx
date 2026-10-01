import { useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { graphql } from '../../api'
import { ErrorMessage, Loading, Tag } from '../../components/common'
import { Column, DataTable } from '../../components/dataTable'
import { Select } from '../../components/select'
import { SettingsSection } from '../../components/settingsList'
import { useIsDesktop } from '../../components/sidebar'
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
  SPENDING_CATEGORIES,
  SpendingCategory,
  amountOf,
  formatDay,
} from './financeApi'
import {
  Money,
  accountLabel,
  spendingCategoryLabel,
  spendingCategoryOptions,
  useFinanceWords,
} from './financeCommon'
import { FinanceTransactionDialog } from './financeTransactionDialog'
import { useSpendingCategoryDisplayName } from './spendingCategoryName'
import { TransactionFilters, searchFromTransactionFilters, transactionFiltersFromSearch } from './financeFilters'

// How many finance transactions one read brings, and one Load more adds.
const PAGE_SIZE = 100

// The finance transactions, newest first, narrowed on the server by dates,
// a finance account, a spending category, words and whether a spending
// category is missing, and read a page at a time from where the last page
// ended. The filters are the address's, so the Spending section can link
// to a category's month and a narrowed list can be shared; changing one
// here rewrites the address in place rather than adding a step to Back. Each one's spending
// category is changed where it is, with the offer to do the same for every
// transaction from that merchant. A transfer is the transfer category,
// chosen the same way, which takes it out of spending and income.
export function FinanceTransactionsSection() {
  const { t } = useTranslation()
  const toast = useToast()
  const words = useFinanceWords()
  const isDesktop = useIsDesktop()
  const categoryName = useSpendingCategoryDisplayName()
  const [search, setSearch] = useSearchParams()
  const filters = useMemo(() => transactionFiltersFromSearch(search), [search])
  const setFilters = (change: (previous: TransactionFilters) => TransactionFilters) =>
    setSearch(
      (previous) => {
        const next = searchFromTransactionFilters(change(transactionFiltersFromSearch(previous)))
        // An address already saying this is left alone, so the first
        // pause in typing does not rewrite it with the same filters.
        return next.toString() === searchFromTransactionFilters(transactionFiltersFromSearch(previous)).toString()
          ? previous
          : next
      },
      { replace: true },
    )
  // The words are sent once typing pauses, not on every key.
  const [typed, setTyped] = useState(filters.text)
  useEffect(() => {
    const timer = window.setTimeout(() => setFilters((previous) => ({ ...previous, text: typed.trim() })), 400)
    return () => window.clearTimeout(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [typed])
  // Words that arrive by the address, from Back or a link, are put in the
  // box; words being typed are not overwritten by the address catching up.
  useEffect(() => {
    setTyped((previous) => (previous.trim() === filters.text ? previous : filters.text))
  }, [filters.text])

  const variables = {
    from: filters.from || undefined,
    to: filters.to || undefined,
    financeAccountId: filters.financeAccountId || undefined,
    spendingCategoryId: filters.spendingCategoryId || undefined,
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
  // The transaction whose details are open, by id, so a change made in the
  // dialog shows there as it does in its row; and what had the focus when
  // it opened, to give it back when it closes.
  const [detailedId, setDetailedId] = useState<string | null>(null)
  const openedFrom = useRef<HTMLElement | null>(null)
  const openDetails = (row: FinanceTransaction) => {
    openedFrom.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    setDetailedId(row.id)
  }
  const closeDetails = () => {
    setDetailedId(null)
    openedFrom.current?.focus()
  }
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
  const detailed = detailedId ? rows.find((row) => row.id === detailedId) : undefined
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

  const columns: Column<FinanceTransaction>[] = [
    {
      key: 'postedOn',
      header: t('finance.postedOn'),
      value: (row) => row.postedOn,
      render: (row) => formatDay(row.postedOn),
      sort: (left, right) => left.postedOn.localeCompare(right.postedOn),
    },
    // Second, beside the day: on a phone the table scrolls sideways, and
    // the amount is what a row is read for.
    {
      key: 'amount',
      header: t('finance.amount'),
      numeric: true,
      value: (row) => row.amount,
      render: (row) => <Money amount={row.amount} currency={row.currencyCode} />,
      sort: (left, right) => amountOf(left.amount) - amountOf(right.amount),
    },
    {
      key: 'description',
      header: t('finance.description'),
      value: (row) => [row.merchantName, row.description].filter(Boolean).join(' · '),
      render: (row) => (
        <span
          className="finance-description"
          title={[row.merchantName, row.description].filter(Boolean).join(' · ')}
        >
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
      optional: true,
      value: (row) => {
        const account = accountList.find((candidate) => candidate.id === row.financeAccountId)
        return account ? accountLabel(account) : ''
      },
    },
    {
      key: 'spendingCategory',
      header: t('finance.spendingCategory'),
      // Who chose the category is the cell's title and a line in the
      // details, not a line under the control: stacked there it made every
      // row twice the height of its text.
      render: (row) => (
        <span
          className="finance-category-cell"
          title={
            row.categorizedBy && row.categorizedBy !== 'person' && row.spendingCategoryId
              ? words.categorizedBy(row.categorizedBy)
              : undefined
          }
        >
          <Select
            value={row.spendingCategoryId ?? ''}
            label={t('finance.spendingCategory')}
            options={[
              { value: '', label: t('finance.uncategorized') },
              ...spendingCategoryOptions(categoryList, categoryName, row.spendingCategoryId, t('finance.transferGroup')),
            ]}
            onChange={(value) => void categorize(row, value)}
          />
        </span>
      ),
    },
  ]

  // The filters that hold, said in a line: what the closed filters say on
  // a phone, where five fields would fill the first screen.
  const account = accountList.find((candidate) => candidate.id === filters.financeAccountId)
  const category = categoryList.find((candidate) => candidate.id === filters.spendingCategoryId)
  const filterWords = [
    filters.from && filters.to
      ? t('finance.dayRange', { from: formatDay(filters.from), to: formatDay(filters.to) })
      : filters.from
        ? t('finance.fromDay', { day: formatDay(filters.from) })
        : filters.to
          ? t('finance.untilDay', { day: formatDay(filters.to) })
          : '',
    account ? accountLabel(account) : '',
    category ? spendingCategoryLabel(category, categoryList, categoryName) : '',
    filters.isUncategorized ? t('finance.uncategorized') : '',
    filters.text ? t('finance.containingWords', { text: filters.text }) : '',
  ].filter(Boolean)

  const filterControls = (
    <>
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
          <span>{t('finance.spendingCategory')}</span>
          <Select
            block
            value={filters.spendingCategoryId}
            label={t('finance.spendingCategory')}
            options={[
              { value: '', label: t('finance.allSpendingCategories') },
              ...spendingCategoryOptions(categoryList, categoryName, filters.spendingCategoryId, t('finance.transferGroup')),
            ]}
            // A spending category and "only those without one" cannot both
            // hold, so choosing one lets go of the other.
            onChange={(value) =>
              setFilters((previous) => ({
                ...previous,
                spendingCategoryId: value,
                isUncategorized: value ? false : previous.isUncategorized,
              }))
            }
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
          onChange={(event) =>
            setFilters((previous) => ({
              ...previous,
              isUncategorized: event.target.checked,
              spendingCategoryId: event.target.checked ? '' : previous.spendingCategoryId,
            }))
          }
        />
        {t('finance.onlyUncategorized')}
      </label>
    </>
  )

  return (
    <SettingsSection card title={t('finance.transactionsTitle')} description={t('finance.transactionsHint')}>
      {isDesktop ? (
        filterControls
      ) : (
        <details className="finance-filter-disclosure">
          <summary>
            <strong>{t('finance.filtersLabel')}</strong>
            <span className="muted">{filterWords.length > 0 ? filterWords.join(' · ') : t('finance.noFilters')}</span>
          </summary>
          {filterControls}
        </details>
      )}
      {first.data ? (
        <p className="muted finance-transaction-count">
          {t('finance.transactionsLoaded', { count: String(rows.length) })}
          {after ? ` · ${t('finance.moreToLoad')}` : ''}
        </p>
      ) : null}
      <ErrorMessage error={first.error} />
      {first.loading && !first.data ? <Loading /> : null}
      {first.data ? (
        <div className="finance-transactions-table">
          <DataTable
            columns={columns}
            rows={rows}
            rowKey={(row) => row.id}
            loading={first.loading}
            emptyMessage={t('finance.noTransactions')}
            countLabel={(count) => t('finance.transactionsLoaded', { count: String(count) })}
            onRowOpen={openDetails}
            rowOpenLabel={(row) => t('finance.transactionDetailsOf', { name: row.merchantName || row.description })}
          />
        </div>
      ) : null}
      {detailed ? (
        <FinanceTransactionDialog
          financeTransaction={detailed}
          financeAccount={accountList.find((candidate) => candidate.id === detailed.financeAccountId)}
          categoryOptions={spendingCategoryOptions(categoryList, categoryName, detailed.spendingCategoryId, t('finance.transferGroup'))}
          onCategorize={(value) => void categorize(detailed, value)}
          onClose={closeDetails}
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
