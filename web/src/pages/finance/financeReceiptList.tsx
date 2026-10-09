import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { graphql } from '../../api'
import { ErrorMessage, Loading, formatMoney } from '../../components/common'
import { Column, DataTable, Range } from '../../components/dataTable'
import { ConfirmDialog } from '../../components/dialog'
import { LinkIcon, UnlinkIcon } from '../../components/icons'
import { usePageInAddress } from '../../components/pager'
import { SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import {
  CATEGORIZE_TRANSACTION,
  COUNT_TRANSACTION,
  DELETE_RECEIPT,
  FINANCE_ACCOUNTS,
  FINANCE_RECEIPTS,
  FINANCE_TRANSACTIONS,
  FinanceAccount,
  FinanceReceipt,
  FinanceReceiptPage,
  FinanceTransaction,
  FinanceTransactionPage,
  MATCH_RECEIPT,
  MAXIMUM_LISTED_TRANSACTION_COUNT,
  PROPOSE_RECEIPT_MATCHES,
  ReceiptMatchCandidate,
  SPENDING_CATEGORIES,
  SpendingCategory,
  UNDO_COUNT_TRANSACTION,
  UNMATCH_RECEIPT,
  amountOf,
  formatDay,
} from './financeApi'
import { spendingCategoryOptions } from './financeCommon'
import {
  DeleteReceiptConfirmation,
  ReceiptCheckTag,
  ReceiptLines,
  ReceiptPhoto,
  ReceiptSource,
  receiptName,
  useCandidateAgreement,
  useReceiptCheckDirection,
  useReceiptSourceNote,
} from './financeReceipts'
import { FinanceTransactionDialog } from './financeTransactionDialog'
import { useSpendingCategoryDisplayName } from './spendingCategoryName'

// The Receipts section: every receipt read, the newest purchase first and
// those that print no day last, as teanode finance receipts lists them,
// narrowed by the days of purchase, to those matched to no charge, or to
// those that print no day. The server pages them, as it does transactions:
// the page and the rows on it are in the address with the filters, and a
// changed filter starts again at the first page. The server lists undated
// receipts only with no days chosen, so that choice is offered only then.
//
// A receipt's row opens the receipt: its lines, its check, where it came
// from and the charges it explains, with Match, Take off and Delete. A
// charge it explains opens that charge's details. One dialog is open at a
// time (every dialog closes on Escape from a document listener), so
// choosing a charge, confirming a delete and a charge's details each take
// the receipt's place, and the receipt comes back when they close.

type Opened =
  | { dialogKind: 'receipt'; receiptId: string }
  | { dialogKind: 'match'; receiptId: string }
  | { dialogKind: 'delete'; receiptId: string }
  | { dialogKind: 'transaction'; financeTransactionId: string; returnReceiptId: string }

type ReceiptFilters = { from: string; to: string; isUnmatched: boolean; isUndated: boolean }

function receiptFiltersFromSearch(search: URLSearchParams): ReceiptFilters {
  return {
    from: search.get('from') ?? '',
    to: search.get('to') ?? '',
    isUnmatched: search.get('unmatched') === '1',
    isUndated: search.get('undated') === '1',
  }
}

// transactionName is how a charge is named in a line: its merchant, or
// what the bank wrote when it names none.
function transactionName(financeTransaction: FinanceTransaction): string {
  return financeTransaction.merchantName || financeTransaction.description
}

export function FinanceReceiptListSection() {
  const { t, plural, language } = useTranslation()
  const toast = useToast()
  const categoryName = useSpendingCategoryDisplayName()
  const sourceNote = useReceiptSourceNote()
  const fallbackName = t('finance.receiptWithoutMerchant')
  const [search, setSearch] = useSearchParams()
  const filters = useMemo(() => receiptFiltersFromSearch(search), [search])
  // Other filters are other pages, so the page goes back to the first; the
  // address is rewritten in place rather than adding a step to Back.
  const setFilters = (change: Partial<ReceiptFilters>) =>
    setSearch(
      (previous) => {
        const next = new URLSearchParams(previous)
        const merged = { ...receiptFiltersFromSearch(previous), ...change }
        // Days chosen leave the undated receipts out, as the server does.
        if (merged.from || merged.to) merged.isUndated = false
        for (const [parameter, value] of [
          ['from', merged.from],
          ['to', merged.to],
          ['unmatched', merged.isUnmatched ? '1' : ''],
          ['undated', merged.isUndated ? '1' : ''],
        ]) {
          if (value) next.set(parameter, value)
          else next.delete(parameter)
        }
        next.delete('page')
        return next
      },
      { replace: true },
    )

  const hasDays = Boolean(filters.from || filters.to)
  const isUndated = filters.isUndated && !hasDays
  const variables = {
    from: filters.from || undefined,
    to: filters.to || undefined,
    isUnmatched: filters.isUnmatched || undefined,
    isUndated: isUndated || undefined,
  }
  const filterKey = JSON.stringify(variables)
  // The page asked for follows the Transactions section: nothing before the
  // table says its range, and until it says one for these filters the page
  // is the address's, which a changed filter has sent back to the first.
  const filterKeyNow = useRef(filterKey)
  filterKeyNow.current = filterKey
  const [range, setRange] = useState<(Range & { filterKey: string }) | null>(null)
  const onRange = useCallback((next: Range) => setRange({ ...next, filterKey: filterKeyNow.current }), [])
  const { pageIndex } = usePageInAddress()
  const offset = range === null ? null : range.filterKey === filterKey ? range.offset : pageIndex * range.limit
  const receiptsQuery = useQuery(
    async () => {
      if (!range || offset === null) return null
      const answer = await graphql<{ FinanceReceipts?: FinanceReceiptPage | null }>(FINANCE_RECEIPTS, {
        ...variables,
        limit: range.limit,
        offset,
      })
      return answer?.FinanceReceipts ?? { financeReceipts: [], totalCount: 0 }
    },
    [filterKey, offset, range?.limit],
    { refresh: false },
  )
  const shownPage = receiptsQuery.data ?? null
  const totalCount = shownPage?.totalCount ?? 0
  const receipts = useMemo(() => shownPage?.financeReceipts ?? [], [shownPage])

  const [opened, setOpened] = useState<Opened | null>(null)
  // The opened receipt as last seen, kept for when it leaves the page: a
  // receipt matched with only the unmatched ones listed is still open,
  // and shows the charge it now explains.
  const [lastOpenedReceipt, setLastOpenedReceipt] = useState<FinanceReceipt | null>(null)
  const openedReceiptId = opened && opened.dialogKind !== 'transaction' ? opened.receiptId : ''
  const pageOpenedReceipt = openedReceiptId ? receipts.find((receipt) => receipt.id === openedReceiptId) : undefined
  useEffect(() => {
    if (pageOpenedReceipt) setLastOpenedReceipt(pageOpenedReceipt)
  }, [pageOpenedReceipt])
  const openedReceipt =
    pageOpenedReceipt ?? (openedReceiptId && lastOpenedReceipt?.id === openedReceiptId ? lastOpenedReceipt : undefined)

  // The charges the receipts shown explain, and the opened receipt's, read
  // by their ids in one ask, so each can be named in its row and opened.
  const matchedIds = useMemo(
    () =>
      [
        ...new Set(
          [...receipts, ...(lastOpenedReceipt ? [lastOpenedReceipt] : [])].flatMap((receipt) =>
            receipt.receiptMatches.map((match) => match.financeTransactionId),
          ),
        ),
      ].slice(0, MAXIMUM_LISTED_TRANSACTION_COUNT),
    [receipts, lastOpenedReceipt],
  )
  const matchedKey = matchedIds.join(',')
  const transactionsQuery = useQuery(
    async () => {
      if (matchedIds.length === 0) return [] as FinanceTransaction[]
      const answer = await graphql<{ FinanceTransactions: FinanceTransactionPage }>(FINANCE_TRANSACTIONS, {
        financeTransactionIds: matchedIds,
        limit: MAXIMUM_LISTED_TRANSACTION_COUNT,
      })
      return answer.FinanceTransactions.financeTransactions
    },
    [matchedKey],
    { refresh: false },
  )
  const accounts = useQuery(() => graphql<{ FinanceAccounts: FinanceAccount[] }>(FINANCE_ACCOUNTS), [], {
    refresh: false,
  })
  const categories = useQuery(() => graphql<{ SpendingCategories: SpendingCategory[] }>(SPENDING_CATEGORIES), [], {
    refresh: false,
  })
  const accountList = accounts.data?.FinanceAccounts ?? []
  const categoryList = categories.data?.SpendingCategories ?? []

  // What changed on a charge since it was read (its spending category,
  // its annotation), laid over it; and charges opened from another's
  // details that the receipts do not name.
  const [changed, setChanged] = useState<Record<string, Partial<FinanceTransaction>>>({})
  const [openedTransactions, setOpenedTransactions] = useState<Record<string, FinanceTransaction>>({})
  const transactionsById = useMemo(() => {
    const byId: Record<string, FinanceTransaction> = { ...openedTransactions }
    for (const financeTransaction of transactionsQuery.data ?? []) byId[financeTransaction.id] = financeTransaction
    for (const [id, changes] of Object.entries(changed)) {
      if (byId[id]) byId[id] = { ...byId[id], ...changes }
    }
    return byId
  }, [transactionsQuery.data, openedTransactions, changed])

  const [isBusy, setIsBusy] = useState(false)
  const [isCounting, setIsCounting] = useState(false)
  const [detailsRefreshCount, setDetailsRefreshCount] = useState(0)
  useEffect(() => setOpened(null), [filterKey])

  const reload = async () => {
    await receiptsQuery.reload(true)
  }

  const unmatch = async (receipt: FinanceReceipt, financeTransactionId: string) => {
    setIsBusy(true)
    try {
      const answer = await graphql<{ UnmatchReceipt?: FinanceReceipt | null }>(UNMATCH_RECEIPT, {
        receiptId: receipt.id,
        financeTransactionId,
      })
      if (answer?.UnmatchReceipt) setLastOpenedReceipt(answer.UnmatchReceipt)
      toast.done(t('finance.receiptUnmatched', { merchant: receiptName(receipt, fallbackName) }))
      await reload()
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setIsBusy(false)
    }
  }

  const match = async (receipt: FinanceReceipt, candidate: ReceiptMatchCandidate) => {
    setIsBusy(true)
    try {
      const answer = await graphql<{ MatchReceipt?: FinanceReceipt | null }>(MATCH_RECEIPT, {
        receiptId: receipt.id,
        financeTransactionId: candidate.financeTransactionId,
        matchedAmount: candidate.matchedAmount || undefined,
      })
      // What the server answers is the receipt as it is now, which the
      // page may no longer list.
      if (answer?.MatchReceipt) setLastOpenedReceipt(answer.MatchReceipt)
      toast.done(t('finance.receiptMatched', { merchant: receiptName(receipt, fallbackName) }))
      await reload()
      setOpened({ dialogKind: 'receipt', receiptId: receipt.id })
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setIsBusy(false)
    }
  }

  const remove = async (receipt: FinanceReceipt) => {
    setIsBusy(true)
    try {
      await graphql(DELETE_RECEIPT, { receiptId: receipt.id })
      toast.done(t('finance.receiptDeleted', { merchant: receiptName(receipt, fallbackName) }))
      setOpened(null)
      await reload()
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setIsBusy(false)
    }
  }

  const categorize = async (financeTransaction: FinanceTransaction, spendingCategoryId: string) => {
    try {
      const answer = await graphql<{ CategorizeTransaction: { financeTransaction: FinanceTransaction } }>(
        CATEGORIZE_TRANSACTION,
        { financeTransactionId: financeTransaction.id, spendingCategoryId },
      )
      setChanged((previous) => ({
        ...previous,
        [financeTransaction.id]: answer.CategorizeTransaction.financeTransaction,
      }))
      toast.done(t('finance.categorized'))
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    }
  }

  const countThisOne = async (financeTransaction: FinanceTransaction, isCountedByPerson: boolean) => {
    setIsCounting(true)
    try {
      const answer = await graphql<{
        CountTransaction?: FinanceTransaction
        UndoCountTransaction?: FinanceTransaction
      }>(isCountedByPerson ? COUNT_TRANSACTION : UNDO_COUNT_TRANSACTION, {
        financeTransactionId: financeTransaction.id,
      })
      const counted = answer.CountTransaction ?? answer.UndoCountTransaction
      if (counted) setOpenedTransactions((previous) => ({ ...previous, [counted.id]: counted }))
      toast.done(isCountedByPerson ? t('finance.transactionCounted') : t('finance.transactionCountUndone'))
      setDetailsRefreshCount((previous) => previous + 1)
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setIsCounting(false)
    }
  }

  const openTransaction = (financeTransactionId: string, returnReceiptId = '') =>
    setOpened({ dialogKind: 'transaction', financeTransactionId, returnReceiptId })

  // chargeLabel names a charge a receipt explains, by its merchant, day
  // and amount; empty while it has not been read.
  const chargeLabel = (financeTransactionId: string) => {
    const financeTransaction = transactionsById[financeTransactionId]
    return financeTransaction
      ? t('finance.receiptMatchedCharge', {
          name: transactionName(financeTransaction),
          day: formatDay(financeTransaction.postedOn),
          amount: formatMoney(Math.abs(amountOf(financeTransaction.amount)), financeTransaction.currencyCode),
        })
      : ''
  }

  // matchedCell names the charges a receipt explains, each opening its
  // details, or says it explains none.
  const matchedCell = (receipt: FinanceReceipt) => {
    if (receipt.receiptMatches.length === 0) return <span className="muted">{t('finance.receiptNotMatched')}</span>
    return (
      <ul className="finance-receipt-matched">
        {receipt.receiptMatches.map((receiptMatch) => {
          const label = chargeLabel(receiptMatch.financeTransactionId)
          return (
            <li key={receiptMatch.financeTransactionId}>
              <button /* link-button: opens the charge's details in place, inline in a list of them */
                type="button"
                className="link"
                disabled={!label}
                onClick={() => openTransaction(receiptMatch.financeTransactionId)}
              >
                {label || t('finance.receiptMatchedChargeUnread')}
              </button>
            </li>
          )
        })}
      </ul>
    )
  }

  const columns: Column<FinanceReceipt>[] = [
    {
      key: 'purchasedOn',
      header: t('finance.receiptPurchasedOn'),
      value: (receipt) => receipt.purchasedOn ?? '',
      render: (receipt) =>
        receipt.purchasedOn ? (
          formatDay(receipt.purchasedOn)
        ) : (
          <span className="muted">{t('finance.receiptNoDay')}</span>
        ),
    },
    {
      key: 'merchant',
      header: t('finance.merchant'),
      value: (receipt) => receiptName(receipt, fallbackName),
      // Two lines at most on a phone, so the total and the check beside it
      // stay on the screen; what the receipt matched and where it came
      // from are after them, for the table to scroll sideways to.
      render: (receipt) => <span className="finance-receipt-merchant">{receiptName(receipt, fallbackName)}</span>,
    },
    {
      key: 'total',
      header: t('finance.receiptTotal'),
      numeric: true,
      value: (receipt) => receipt.totalAmount,
      render: (receipt) => formatMoney(amountOf(receipt.totalAmount), receipt.currencyCode),
    },
    {
      key: 'check',
      header: t('finance.receiptCheck'),
      render: (receipt) => <ReceiptCheckTag receipt={receipt} />,
    },
    {
      key: 'matched',
      header: t('finance.receiptMatchedColumn'),
      render: (receipt) => matchedCell(receipt),
    },
    {
      key: 'source',
      header: t('finance.receiptSource'),
      render: (receipt) => {
        const note = sourceNote(receipt)
        return note ? (
          <span className="muted">{note}</span>
        ) : (
          <ReceiptSource receipt={receipt} name={receiptName(receipt, fallbackName)} />
        )
      },
    },
  ]

  const receiptCountWords = (count: number) =>
    plural(
      count,
      { one: 'finance.receiptCountOne', other: 'finance.receiptCountOther' },
      { count: count.toLocaleString(language) },
    )

  const openedTransaction =
    opened?.dialogKind === 'transaction' ? transactionsById[opened.financeTransactionId] : undefined

  // The dialog open now, one at a time.
  let dialog: React.ReactNode = null
  if (openedReceipt && opened?.dialogKind === 'receipt') {
    dialog = (
      <FinanceReceiptDialog
        receipt={openedReceipt}
        chargeLabel={chargeLabel}
        isBusy={isBusy}
        onOpenTransaction={(financeTransactionId) => openTransaction(financeTransactionId, openedReceipt.id)}
        onMatch={() => setOpened({ dialogKind: 'match', receiptId: openedReceipt.id })}
        onUnmatch={(financeTransactionId) => void unmatch(openedReceipt, financeTransactionId)}
        onDelete={() => setOpened({ dialogKind: 'delete', receiptId: openedReceipt.id })}
        onClose={() => setOpened(null)}
      />
    )
  } else if (openedReceipt && opened?.dialogKind === 'match') {
    dialog = (
      <ReceiptChargeChooser
        receipt={openedReceipt}
        isBusy={isBusy}
        onMatch={(candidate) => void match(openedReceipt, candidate)}
        onClose={() => setOpened({ dialogKind: 'receipt', receiptId: openedReceipt.id })}
      />
    )
  } else if (openedReceipt && opened?.dialogKind === 'delete') {
    dialog = (
      <DeleteReceiptConfirmation
        receipt={openedReceipt}
        isBusy={isBusy}
        onConfirm={() => void remove(openedReceipt)}
        onClose={() => setOpened({ dialogKind: 'receipt', receiptId: openedReceipt.id })}
      />
    )
  } else if (openedTransaction && opened?.dialogKind === 'transaction') {
    const returnReceiptId = opened.returnReceiptId
    dialog = (
      <FinanceTransactionDialog
        key={openedTransaction.id}
        financeTransaction={openedTransaction}
        financeAccount={accountList.find((candidate) => candidate.id === openedTransaction.financeAccountId)}
        financeAccounts={accountList}
        categoryOptions={spendingCategoryOptions(
          categoryList,
          categoryName,
          openedTransaction.spendingCategoryId,
          t('finance.transferGroup'),
        )}
        isCounting={isCounting}
        refreshCount={detailsRefreshCount}
        onCategorize={(value) => void categorize(openedTransaction, value)}
        onCount={() => void countThisOne(openedTransaction, true)}
        onUndoCount={() => void countThisOne(openedTransaction, false)}
        onOpenTransaction={(financeTransaction) => {
          setOpenedTransactions((previous) => ({ ...previous, [financeTransaction.id]: financeTransaction }))
          openTransaction(financeTransaction.id, returnReceiptId)
        }}
        onTransactionChanged={(changes) =>
          setChanged((previous) => ({
            ...previous,
            [openedTransaction.id]: { ...previous[openedTransaction.id], ...changes },
          }))
        }
        onClose={() => {
          // The charge's details can take receipts on and off it, so the
          // receipts are read again.
          setOpened(returnReceiptId ? { dialogKind: 'receipt', receiptId: returnReceiptId } : null)
          void reload()
        }}
      />
    )
  }

  return (
    <SettingsSection card title={t('finance.receiptsTitle')} description={t('finance.receiptsListHint')}>
      <div className="row finance-filters">
        <label>
          <span>{t('finance.from')}</span>
          <input
            type="date"
            value={filters.from}
            max={filters.to || undefined}
            onChange={(event) => setFilters({ from: event.target.value })}
          />
        </label>
        <label>
          <span>{t('finance.to')}</span>
          <input
            type="date"
            value={filters.to}
            min={filters.from || undefined}
            onChange={(event) => setFilters({ to: event.target.value })}
          />
        </label>
      </div>
      <label className="checkbox">
        <input
          type="checkbox"
          checked={filters.isUnmatched}
          onChange={(event) => setFilters({ isUnmatched: event.target.checked })}
        />
        {t('finance.onlyUnmatchedReceipts')}
      </label>
      {hasDays ? null : (
        <label className="checkbox">
          <input
            type="checkbox"
            checked={isUndated}
            onChange={(event) => setFilters({ isUndated: event.target.checked })}
          />
          {t('finance.onlyUndatedReceipts')}
        </label>
      )}
      {shownPage ? <p className="muted finance-transaction-count">{receiptCountWords(totalCount)}</p> : null}
      <ErrorMessage error={receiptsQuery.error} />
      {/* Drawn before the first answer, since the table is what says which
          page to ask for. */}
      <div className="finance-transactions-table finance-receipts-table">
        <DataTable
          columns={columns}
          rows={receipts}
          rowKey={(receipt) => receipt.id}
          loading={!shownPage}
          remote={{ total: totalCount, onRange }}
          emptyMessage={
            isUndated
              ? t('finance.noUndatedReceipts')
              : filters.isUnmatched
                ? t('finance.noUnmatchedReceipts')
                : t('finance.noReceiptsListed')
          }
          countLabel={receiptCountWords}
          onRowOpen={(receipt) => setOpened({ dialogKind: 'receipt', receiptId: receipt.id })}
          rowOpenLabel={(receipt) => t('finance.receiptDetailsOf', { name: receiptName(receipt, fallbackName) })}
        />
      </div>
      {dialog}
    </SettingsSection>
  )
}

// FinanceReceiptDialog is one receipt: when and where it was bought, its
// check, where it was read from, the charges it explains (each opening
// that charge, each with a way to take the receipt off it), its lines,
// and the buttons to match it to a charge and to delete it.
function FinanceReceiptDialog({
  receipt,
  chargeLabel,
  isBusy,
  onOpenTransaction,
  onMatch,
  onUnmatch,
  onDelete,
  onClose,
}: {
  receipt: FinanceReceipt
  chargeLabel: (financeTransactionId: string) => string
  isBusy: boolean
  onOpenTransaction: (financeTransactionId: string) => void
  onMatch: () => void
  onUnmatch: (financeTransactionId: string) => void
  onDelete: () => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const sourceNote = useReceiptSourceNote()
  const checkDirection = useReceiptCheckDirection()
  const name = receiptName(receipt, t('finance.receiptWithoutMerchant'))
  const note = sourceNote(receipt)
  const direction = checkDirection(receipt)
  return (
    <ConfirmDialog
      wide
      title={name}
      onClose={onClose}
      body={
        <div className="finance-receipt-details">
          <div className="finance-receipt-head">
            <div className="finance-receipt-title">
              <span className="muted">
                {[
                  receipt.purchasedOn ? formatDay(receipt.purchasedOn) : '',
                  receipt.merchantReceiptNumber
                    ? t('finance.receiptNumber', { number: receipt.merchantReceiptNumber })
                    : '',
                  t('finance.receiptTotalOf', {
                    amount: formatMoney(amountOf(receipt.totalAmount), receipt.currencyCode),
                  }),
                ]
                  .filter(Boolean)
                  .join(' · ')}
              </span>{' '}
              <ReceiptCheckTag receipt={receipt} />
            </div>
            <div className="row-actions">
              <ReceiptSource receipt={receipt} name={name} />
            </div>
          </div>
          {direction ? <p className="muted finance-receipt-detail">{direction}</p> : null}
          {note ? <p className="muted finance-receipt-detail">{note}</p> : null}
          <div className="finance-metadata-head">
            <strong>{t('finance.receiptMatchedColumn')}</strong>
          </div>
          {receipt.receiptMatches.length === 0 ? (
            <p className="muted finance-receipt-detail">{t('finance.receiptNotMatched')}</p>
          ) : (
            <ul className="finance-receipt-candidates">
              {receipt.receiptMatches.map((receiptMatch) => {
                const label = chargeLabel(receiptMatch.financeTransactionId)
                return (
                  <li key={receiptMatch.financeTransactionId}>
                    <div>
                      <button /* link-button: opens the charge's details in place, inline in a list of them */
                        type="button"
                        className="link"
                        disabled={!label}
                        onClick={() => onOpenTransaction(receiptMatch.financeTransactionId)}
                      >
                        {label || t('finance.receiptMatchedChargeUnread')}
                      </button>
                      <span className="muted finance-detail-note">
                        {[
                          t('finance.receiptExplainsAmount', {
                            amount: formatMoney(amountOf(receiptMatch.matchedAmount), receipt.currencyCode),
                          }),
                          receiptMatch.receiptMatchSource === 'receipt_matcher'
                            ? t('finance.receiptMatchedAutomatically')
                            : '',
                        ]
                          .filter(Boolean)
                          .join(' · ')}
                      </span>
                    </div>
                    <Tooltip label={t('finance.unmatchReceiptFrom')}>
                      <button
                        type="button"
                        className="icon-action"
                        disabled={isBusy}
                        aria-label={`${label || t('finance.receiptMatchedChargeUnread')}: ${t('finance.unmatchReceiptFrom')}`}
                        onClick={() => onUnmatch(receiptMatch.financeTransactionId)}
                      >
                        <UnlinkIcon size={16} />
                      </button>
                    </Tooltip>
                  </li>
                )
              })}
            </ul>
          )}
          <ReceiptPhoto receipt={receipt} name={name} />
          <ReceiptLines receipt={receipt} />
          <div className="finance-receipt-buttons">
            <button type="button" disabled={isBusy} onClick={onMatch}>
              {t('finance.matchReceiptToCharge')}
            </button>
            <button type="button" className="danger" disabled={isBusy} onClick={onDelete}>
              {t('finance.deleteReceipt')}
            </button>
          </div>
        </div>
      }
    />
  )
}

// ReceiptChargeChooser offers the charges a receipt could explain, as
// ProposeReceiptMatches proposes them, each with what agrees between them
// and how much of it the receipt would explain.
function ReceiptChargeChooser({
  receipt,
  isBusy,
  onMatch,
  onClose,
}: {
  receipt: FinanceReceipt
  isBusy: boolean
  onMatch: (candidate: ReceiptMatchCandidate) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const agreement = useCandidateAgreement()
  const [candidates, setCandidates] = useState<ReceiptMatchCandidate[] | null>(null)
  useEffect(() => {
    let isCancelled = false
    void (async () => {
      try {
        const answer = await graphql<{ ProposeReceiptMatches?: ReceiptMatchCandidate[] | null }>(
          PROPOSE_RECEIPT_MATCHES,
          { receiptId: receipt.id },
        )
        const matchedIds = new Set(receipt.receiptMatches.map((receiptMatch) => receiptMatch.financeTransactionId))
        const proposed = (answer?.ProposeReceiptMatches ?? []).filter(
          (candidate) => !matchedIds.has(candidate.financeTransactionId),
        )
        if (!isCancelled) setCandidates(proposed)
      } catch (caught) {
        if (isCancelled) return
        setCandidates([])
        toast.failure(caught, t('finance.failed'))
      }
    })()
    return () => {
      isCancelled = true
    }
    // Asked once, when the chooser opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [receipt.id])

  return (
    <ConfirmDialog
      wide
      title={t('finance.matchChargeTitle', { merchant: receiptName(receipt, t('finance.receiptWithoutMerchant')) })}
      onClose={onClose}
      body={
        <div className="finance-receipt-chooser">
          <p className="muted">{t('finance.matchChargeHint')}</p>
          {candidates === null ? <Loading /> : null}
          {candidates !== null && candidates.length === 0 ? (
            <p className="muted">{t('finance.noChargeCandidates')}</p>
          ) : null}
          {candidates && candidates.length > 0 ? (
            <ul className="finance-receipt-candidates">
              {candidates.map((candidate) => {
                const financeTransaction = candidate.financeTransaction
                const name = financeTransaction ? transactionName(financeTransaction) : candidate.financeTransactionId
                return (
                  <li key={candidate.financeTransactionId}>
                    <div>
                      <strong>{name}</strong>{' '}
                      {financeTransaction ? (
                        <span className="muted">
                          {formatDay(financeTransaction.postedOn)} ·{' '}
                          {formatMoney(Math.abs(amountOf(financeTransaction.amount)), financeTransaction.currencyCode)}
                        </span>
                      ) : null}
                      <span className="muted finance-detail-note">
                        {[
                          t('finance.candidateExplains', {
                            amount: formatMoney(amountOf(candidate.matchedAmount), receipt.currencyCode),
                          }),
                          agreement(candidate),
                        ]
                          .filter(Boolean)
                          .join(' · ')}
                      </span>
                    </div>
                    <Tooltip label={t('finance.matchToThisCharge')}>
                      <button
                        type="button"
                        className="icon-action"
                        disabled={isBusy}
                        aria-label={`${name}: ${t('finance.matchToThisCharge')}`}
                        onClick={() => onMatch(candidate)}
                      >
                        <LinkIcon size={16} />
                      </button>
                    </Tooltip>
                  </li>
                )
              })}
            </ul>
          ) : null}
        </div>
      }
    />
  )
}
