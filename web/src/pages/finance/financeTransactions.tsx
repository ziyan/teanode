import { useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { graphql } from '../../api'
import { ErrorMessage, Loading, Tag } from '../../components/common'
import { Column, DataTable } from '../../components/dataTable'
import { ConfirmDialog } from '../../components/dialog'
import { Select } from '../../components/select'
import { SettingsSection } from '../../components/settingsList'
import { useIsDesktop } from '../../components/sidebar'
import { useToast } from '../../components/toast'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import {
  CATEGORIZE_TRANSACTION,
  CATEGORIZE_TRANSACTIONS,
  COUNT_TRANSACTION,
  FINANCE_ACCOUNTS,
  FINANCE_TRANSACTIONS,
  FinanceAccount,
  FinanceTransaction,
  FinanceTransactionPage,
  MAXIMUM_CATEGORIZED_TRANSACTION_COUNT,
  PROPOSE_SPENDING_RULES,
  SPENDING_CATEGORIES,
  SpendingCategory,
  SpendingRuleProposal,
  UNDO_COUNT_TRANSACTION,
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
import { FinanceSelectionToolbar, chunks, mergeProposals } from './financeTransactionSelection'
import { useSpendingCategoryDisplayName } from './spendingCategoryName'
import { TransactionFilters, searchFromTransactionFilters, transactionFiltersFromSearch } from './financeFilters'

// How many finance transactions one read brings, and one Load more adds.
const PAGE_SIZE = 100

// How many proposed spending rules the confirmation names before saying
// how many more there are.
const SHOWN_PROPOSAL_COUNT = 8

// SpendingRulesConfirmation asks before saving spending rules for the
// chosen transactions: which words each matches, and how many of the
// chosen transactions it matches.
function SpendingRulesConfirmation({
  proposals,
  spendingCategoryLabel,
  isApplying,
  onConfirm,
  onClose,
}: {
  proposals: SpendingRuleProposal[]
  spendingCategoryLabel: string
  isApplying: boolean
  onConfirm: () => void
  onClose: () => void
}) {
  const { t, plural } = useTranslation()
  const hiddenCount = proposals.length - SHOWN_PROPOSAL_COUNT
  return (
    <ConfirmDialog
      title={plural(proposals.length, {
        one: 'finance.saveSpendingRulesTitleOne',
        other: 'finance.saveSpendingRulesTitleOther',
      })}
      destructive={false}
      body={
        <>
          <p className="muted">{t('finance.saveSpendingRulesBody', { category: spendingCategoryLabel })}</p>
          <ul className="finance-rule-proposals">
            {proposals.slice(0, SHOWN_PROPOSAL_COUNT).map((proposal) => (
              <li key={proposal.matchText}>
                {proposal.matchText}{' '}
                <span className="muted">
                  {plural(proposal.financeTransactionCount, {
                    one: 'finance.ruleMatchesOne',
                    other: 'finance.ruleMatchesOther',
                  })}
                </span>
              </li>
            ))}
          </ul>
          {hiddenCount > 0 ? (
            <p className="muted">{t('finance.moreSpendingRules', { count: String(hiddenCount) })}</p>
          ) : null}
        </>
      }
      confirmLabel={t('finance.applyAndSaveSpendingRules')}
      busy={isApplying}
      onConfirm={onConfirm}
      onClose={onClose}
    />
  )
}

// The finance transactions, newest first, narrowed on the server by dates,
// a finance account, a spending category, words and whether a spending
// category is missing, and read a page at a time from where the last page
// ended. The filters are the address's, so the Spending section can link
// to a category's month and a narrowed list can be shared; changing one
// here rewrites the address in place rather than adding a step to Back. Each one's spending
// category is changed where it is, with the offer to do the same for every
// transaction from that merchant. A transfer is the transfer category,
// chosen the same way, which takes it out of spending and income. A
// mirrored copy stays in the list, muted, tagged and with its amount struck
// through, since it is left out of every total.
//
// Transactions can be chosen with the boxes at the start of their rows
// (shift chooses the run since the last one), kept while more pages load,
// and given one spending category together, with spending rules for them
// when asked, after saying which. Changing a filter lets go of them.
export function FinanceTransactionsSection() {
  const { t, plural } = useTranslation()
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
  // Transactions opened from another's details (a counted copy and its
  // duplicates) that the pages read so far may not hold.
  const [opened, setOpened] = useState<Record<string, FinanceTransaction>>({})
  const [isCounting, setIsCounting] = useState(false)
  // Raised after the person counts a copy or takes that back, so the open
  // details read again the copies they name.
  const [detailsRefreshCount, setDetailsRefreshCount] = useState(0)
  // The transactions chosen to categorize together, by id; the spending
  // rules about to be saved for them, while the person is asked.
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [isApplying, setIsApplying] = useState(false)
  const [rulesConfirmation, setRulesConfirmation] = useState<{
    financeTransactionIds: string[]
    spendingCategoryId: string
    proposals: SpendingRuleProposal[]
  } | null>(null)
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
    setSelectedIds(new Set())
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
  const detailedRow = detailedId
    ? (rows.find((row) => row.id === detailedId) ?? opened[detailedId])
    : undefined
  const detailed = detailedRow && changed[detailedRow.id] ? { ...detailedRow, ...changed[detailedRow.id] } : detailedRow
  const categoryList = categories.data?.SpendingCategories ?? []
  // Only what is in the list counts as chosen: a page read again may no
  // longer hold one that was.
  const selectedLoaded = useMemo(
    () => new Set(rows.filter((row) => selectedIds.has(row.id)).map((row) => row.id)),
    [rows, selectedIds],
  )

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

  // reloadLoadedPages reads again every page read so far. Counting a copy,
  // or handing it back to detection, can change which of its copies is
  // counted, so rows other than the one clicked change too. It moves no
  // row, so the pages below the first are read again from the same
  // cursors.
  const reloadLoadedPages = async () => {
    const reloadedRows: FinanceTransaction[] = []
    let reloadedAfter = firstPage?.nextCursor ?? null
    while (more.isLoaded && reloadedAfter && reloadedRows.length < more.rows.length) {
      const answer = await graphql<{ FinanceTransactions: FinanceTransactionPage }>(FINANCE_TRANSACTIONS, {
        ...variables,
        after: reloadedAfter,
      })
      reloadedRows.push(...answer.FinanceTransactions.financeTransactions)
      reloadedAfter = answer.FinanceTransactions.nextCursor ?? null
    }
    await first.reload(true)
    if (more.isLoaded) setMore({ rows: reloadedRows, after: reloadedAfter, isLoaded: true })
    setChanged({})
  }

  // countThisOne records or takes back the person's word that a mirrored
  // copy is a charge of its own, then reads the list again.
  const countThisOne = async (row: FinanceTransaction, isCountedByPerson: boolean) => {
    setIsCounting(true)
    try {
      const answer = await graphql<{
        CountTransaction?: FinanceTransaction
        UndoCountTransaction?: FinanceTransaction
      }>(isCountedByPerson ? COUNT_TRANSACTION : UNDO_COUNT_TRANSACTION, { financeTransactionId: row.id })
      const counted = answer.CountTransaction ?? answer.UndoCountTransaction
      // Kept beside the pages, so its details show the answer even when
      // no page read so far holds it, or the pages read again leave it out.
      if (counted) setOpened((previous) => ({ ...previous, [row.id]: counted }))
      toast.done(isCountedByPerson ? t('finance.transactionCounted') : t('finance.transactionCountUndone'))
      await reloadLoadedPages()
      setDetailsRefreshCount((previous) => previous + 1)
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setIsCounting(false)
    }
  }

  // categorizeSelection gives the chosen transactions the spending category
  // as the person's choice, a few pages at a time, each piece all or none.
  // What worked is shown at once and let go of; a piece that failed stays
  // chosen, so trying again acts on exactly what is left. Saved rules can
  // change other rows, so then every page read so far is read again.
  const categorizeSelection = async (
    financeTransactionIds: string[],
    spendingCategoryId: string,
    shouldCreateSpendingRules: boolean,
    isCoveredByRules = false,
  ) => {
    setIsApplying(true)
    const categorizedRows: FinanceTransaction[] = []
    const failedIds: string[] = []
    let savedRuleCount = 0
    let failure: unknown = null
    for (const piece of chunks(financeTransactionIds, MAXIMUM_CATEGORIZED_TRANSACTION_COUNT)) {
      try {
        const answer = await graphql<{
          CategorizeTransactions: { financeTransactions: FinanceTransaction[]; spendingRules: { id: string }[] }
        }>(CATEGORIZE_TRANSACTIONS, {
          financeTransactionIds: piece,
          spendingCategoryId: spendingCategoryId || null,
          shouldCreateSpendingRules,
        })
        categorizedRows.push(...answer.CategorizeTransactions.financeTransactions)
        savedRuleCount += answer.CategorizeTransactions.spendingRules.length
      } catch (caught) {
        failedIds.push(...piece)
        failure = caught
      }
    }
    setChanged((previous) => ({ ...previous, ...Object.fromEntries(categorizedRows.map((row) => [row.id, row])) }))
    setSelectedIds(new Set(failedIds))
    if (savedRuleCount > 0) {
      try {
        await reloadLoadedPages()
      } catch (caught) {
        toast.failure(caught, t('finance.failed'))
      }
    }
    setIsApplying(false)
    const categorized = plural(categorizedRows.length, {
      one: 'finance.categorizedCountOne',
      other: 'finance.categorizedCountOther',
    })
    if (categorizedRows.length === 0) {
      toast.failure(failure, t('finance.failed'))
    } else if (failedIds.length > 0) {
      toast.failed(t('finance.categorizePartlyFailed', { categorized, failedCount: String(failedIds.length) }))
    } else if (savedRuleCount > 0) {
      const saved = plural(savedRuleCount, { one: 'finance.savedRuleCountOne', other: 'finance.savedRuleCountOther' })
      toast.done(t('finance.categorizedAndSaved', { categorized, saved }))
    } else if (isCoveredByRules) {
      toast.done(t('finance.categorizedCoveredByRules', { categorized }))
    } else {
      toast.done(t('finance.bulkCategorized', { categorized }))
    }
  }

  // applyToSelection is the toolbar's Apply. With rules asked for, it
  // first asks the server which it would save and has the person confirm
  // them; when existing rules already cover every one, there is nothing
  // to confirm.
  const applyToSelection = async (spendingCategoryId: string, shouldSaveSpendingRules: boolean) => {
    const financeTransactionIds = [...selectedLoaded]
    if (!shouldSaveSpendingRules) {
      await categorizeSelection(financeTransactionIds, spendingCategoryId, false)
      return
    }
    setIsApplying(true)
    let proposals: SpendingRuleProposal[]
    try {
      const pieces: SpendingRuleProposal[][] = []
      for (const piece of chunks(financeTransactionIds, MAXIMUM_CATEGORIZED_TRANSACTION_COUNT)) {
        const answer = await graphql<{ ProposeSpendingRules: SpendingRuleProposal[] }>(PROPOSE_SPENDING_RULES, {
          financeTransactionIds: piece,
          spendingCategoryId,
        })
        pieces.push(answer.ProposeSpendingRules)
      }
      proposals = mergeProposals(pieces)
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
      setIsApplying(false)
      return
    }
    setIsApplying(false)
    if (proposals.length === 0) {
      await categorizeSelection(financeTransactionIds, spendingCategoryId, false, true)
      return
    }
    setRulesConfirmation({ financeTransactionIds, spendingCategoryId, proposals })
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
      render: (row) =>
        row.duplicateOfTransactionId ? (
          <s className="finance-duplicate-amount" title={t('finance.duplicateNotCounted')}>
            <Money amount={row.amount} currency={row.currencyCode} />
          </s>
        ) : (
          <Money amount={row.amount} currency={row.currencyCode} />
        ),
      sort: (left, right) => amountOf(left.amount) - amountOf(right.amount),
    },
    {
      key: 'description',
      header: t('finance.description'),
      value: (row) => [row.merchantName, row.description].filter(Boolean).join(' · '),
      render: (row) => (
        <span
          className={row.duplicateOfTransactionId ? 'finance-description muted' : 'finance-description'}
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
          {row.duplicateOfTransactionId ? (
            <>
              {' '}
              <Tag value={t('finance.duplicate')} />
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
            selected={selectedLoaded}
            onSelect={setSelectedIds}
            selectionActions={(chosen) => (
              <FinanceSelectionToolbar
                selectedTransactionCount={chosen.length}
                loadedTransactionCount={rows.length}
                categoryOptions={spendingCategoryOptions(categoryList, categoryName, null, t('finance.transferGroup'))}
                isApplying={isApplying}
                onSelectAllLoaded={() => setSelectedIds(new Set(rows.map((row) => row.id)))}
                onClear={() => setSelectedIds(new Set())}
                onApply={(spendingCategoryId, shouldSaveSpendingRules) =>
                  void applyToSelection(spendingCategoryId, shouldSaveSpendingRules)
                }
              />
            )}
          />
        </div>
      ) : null}
      {rulesConfirmation ? (
        <SpendingRulesConfirmation
          proposals={rulesConfirmation.proposals}
          spendingCategoryLabel={(() => {
            const chosen = categoryList.find((candidate) => candidate.id === rulesConfirmation.spendingCategoryId)
            return chosen ? spendingCategoryLabel(chosen, categoryList, categoryName) : ''
          })()}
          isApplying={isApplying}
          onConfirm={() => {
            setRulesConfirmation(null)
            void categorizeSelection(rulesConfirmation.financeTransactionIds, rulesConfirmation.spendingCategoryId, true)
          }}
          onClose={() => setRulesConfirmation(null)}
        />
      ) : null}
      {detailed ? (
        <FinanceTransactionDialog
          financeTransaction={detailed}
          financeAccount={accountList.find((candidate) => candidate.id === detailed.financeAccountId)}
          financeAccounts={accountList}
          categoryOptions={spendingCategoryOptions(categoryList, categoryName, detailed.spendingCategoryId, t('finance.transferGroup'))}
          isCounting={isCounting}
          refreshCount={detailsRefreshCount}
          onCategorize={(value) => void categorize(detailed, value)}
          onCount={() => void countThisOne(detailed, true)}
          onUndoCount={() => void countThisOne(detailed, false)}
          onOpenTransaction={(financeTransaction) => {
            setOpened((previous) => ({ ...previous, [financeTransaction.id]: financeTransaction }))
            setDetailedId(financeTransaction.id)
          }}
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
