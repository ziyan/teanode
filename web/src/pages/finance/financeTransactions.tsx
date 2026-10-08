import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { graphql } from '../../api'
import { ErrorMessage, Tag } from '../../components/common'
import { usePageInAddress } from '../../components/pager'
import { Column, DataTable, Range } from '../../components/dataTable'
import { ConfirmDialog } from '../../components/dialog'
import { ReceiptIcon } from '../../components/icons'
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
  FINANCE_TRANSACTION_IDS,
  FinanceAccount,
  FinanceTransaction,
  FinanceTransactionPage,
  ConfirmedSpendingRule,
  MAXIMUM_CATEGORIZED_TRANSACTION_COUNT,
  MAXIMUM_LISTED_TRANSACTION_COUNT,
  MAXIMUM_PROPOSED_TRANSACTION_COUNT,
  MAXIMUM_SPENDING_RULE_PROPOSAL_COUNT,
  PROPOSE_SPENDING_RULES,
  SPENDING_CATEGORIES,
  SpendingCategory,
  SpendingRuleProposals,
  UNDO_COUNT_TRANSACTION,
  formatDay,
} from './financeApi'
import { Money, accountLabel, spendingCategoryLabel, spendingCategoryOptions, useFinanceWords } from './financeCommon'
import { FinanceTransactionDialog } from './financeTransactionDialog'
import { FinanceSelectionToolbar, chunks, confirmedSpendingRules } from './financeTransactionSelection'
import { useSpendingCategoryDisplayName } from './spendingCategoryName'
import { TransactionFilters, searchFromTransactionFilters, transactionFiltersFromSearch } from './financeFilters'

// The most transactions Select all chooses: every one of them can then
// be given a spending category with rules, which is proposed for at most
// this many at once. Past it the filters are the way to a smaller set.
const MAXIMUM_SELECTED_TRANSACTION_COUNT = MAXIMUM_PROPOSED_TRANSACTION_COUNT

// useLeftOutLines says how many match texts a proposal left out and why,
// one line each, for the confirmation and for the toast when nothing is
// left to confirm.
function useLeftOutLines(): (proposals: SpendingRuleProposals) => string[] {
  const { t, plural } = useTranslation()
  return (proposals) => {
    const lines: string[] = []
    if (proposals.tooGenericMatchTextCount > 0) {
      lines.push(
        plural(proposals.tooGenericMatchTextCount, {
          one: 'finance.leftOutTooGenericOne',
          other: 'finance.leftOutTooGenericOther',
        }),
      )
    }
    if (proposals.changingNumberMatchTextCount > 0) {
      lines.push(
        plural(proposals.changingNumberMatchTextCount, {
          one: 'finance.leftOutChangingNumberOne',
          other: 'finance.leftOutChangingNumberOther',
        }),
      )
    }
    if (proposals.overLimitMatchTextCount > 0) {
      lines.push(
        t('finance.leftOutOverLimit', {
          count: String(proposals.overLimitMatchTextCount),
          maximum: String(MAXIMUM_SPENDING_RULE_PROPOSAL_COUNT),
        }),
      )
    }
    return lines
  }
}

// SpendingRulesConfirmation asks before saving spending rules for the
// chosen transactions, listing every one that will be saved: its words,
// how many of the chosen transactions it matches, how many other
// transactions it would change, and the rule it goes ahead of; then what
// was left out and why.
function SpendingRulesConfirmation({
  proposals,
  spendingCategoryLabel,
  categoryLabelOf,
  isApplying,
  onConfirm,
  onClose,
}: {
  proposals: SpendingRuleProposals
  spendingCategoryLabel: string
  categoryLabelOf: (spendingCategoryId: string) => string
  isApplying: boolean
  onConfirm: () => void
  onClose: () => void
}) {
  const { t, plural } = useTranslation()
  const leftOutLines = useLeftOutLines()
  const listed = proposals.spendingRuleProposals
  return (
    <ConfirmDialog
      title={plural(listed.length, {
        one: 'finance.saveSpendingRulesTitleOne',
        other: 'finance.saveSpendingRulesTitleOther',
      })}
      destructive={false}
      body={
        <>
          <p className="muted">{t('finance.saveSpendingRulesBody', { category: spendingCategoryLabel })}</p>
          <ul className="finance-rule-proposals">
            {listed.map((proposal) => (
              <li key={proposal.matchText}>
                {proposal.matchText}{' '}
                <span className="muted">
                  {plural(proposal.financeTransactionCount, {
                    one: 'finance.ruleMatchesOne',
                    other: 'finance.ruleMatchesOther',
                  })}
                </span>
                <span className="muted finance-rule-proposal-detail">
                  {plural(proposal.changedTransactionCount, {
                    one: 'finance.ruleChangesOne',
                    other: 'finance.ruleChangesOther',
                  })}
                </span>
                {proposal.aheadOfSpendingRule ? (
                  <span className="muted finance-rule-proposal-detail">
                    {t('finance.ruleGoesAheadOf', {
                      matchText: proposal.aheadOfSpendingRule.matchText,
                      category: categoryLabelOf(proposal.aheadOfSpendingRule.spendingCategoryId),
                    })}
                  </span>
                ) : null}
              </li>
            ))}
          </ul>
          {leftOutLines(proposals).map((line) => (
            <p key={line} className="muted">
              {line}
            </p>
          ))}
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
// category is missing, and read a page at a time by its number, with how
// many match on every page. The filters and the page are the address's,
// so the Spending section can link to a category's month, a narrowed list
// can be shared, and Back goes to the page before; changing a filter
// rewrites the address in place rather than adding a step to Back, and
// starts again at the first page. Each one's spending category is changed
// where it is, with the offer to do the same for every transaction from
// that merchant. A transfer is the transfer category, chosen the same way,
// which takes it out of spending and income. A mirrored copy is left out,
// as every total leaves it out, and the line above the table says how
// many were; Show duplicates lists them, muted, tagged and with the amount
// struck through.
//
// Transactions can be chosen with the boxes at the start of their rows
// (shift chooses the run since the last one, the header's box the whole
// page), kept by id from page to page, and given one spending category
// together, with spending rules for them when asked, after saying which.
// Select all chooses every transaction the filters match, on every page.
// Changing a filter lets go of them.
export function FinanceTransactionsSection() {
  const { t, plural, language } = useTranslation()
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
        // pause in typing does not rewrite it with the same filters, nor
        // send the list back to its first page.
        if (next.toString() === searchFromTransactionFilters(transactionFiltersFromSearch(previous)).toString()) {
          return previous
        }
        // Other filters are other pages, so the page is not carried over;
        // how many rows a page holds is the person's and is.
        const pageSize = previous.get('rows')
        if (pageSize) next.set('rows', pageSize)
        return next
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
    isDuplicateIncluded: filters.isDuplicateIncluded || undefined,
  }
  const filterKey = JSON.stringify(variables)
  // The filters in force now, for an answer that arrives after they
  // changed, and for the range below to say which filters it was told for.
  const filterKeyNow = useRef(filterKey)
  filterKeyNow.current = filterKey
  // Which page the table shows, as it says once it has read the address:
  // nothing is asked for before then, so a link to page four does not
  // fetch page one first.
  const [range, setRange] = useState<(Range & { filterKey: string }) | null>(null)
  const onRange = useCallback((next: Range) => setRange({ ...next, filterKey: filterKeyNow.current }), [])
  // Other filters are drawn once before the table says its new range, and
  // the range held then is the old filters' page: asking with it sent the
  // new filters at the old offset, and then again at the right one. Until
  // the table has said a range for these filters, the page is the
  // address's, which the change of filters has already sent to the first.
  const { pageIndex } = usePageInAddress()
  const offset = range === null ? null : range.filterKey === filterKey ? range.offset : pageIndex * range.limit
  const first = useQuery(
    () =>
      range && offset !== null
        ? graphql<{ FinanceTransactions: FinanceTransactionPage }>(FINANCE_TRANSACTIONS, {
            ...variables,
            limit: range.limit,
            offset,
          })
        : Promise.resolve(null),
    [filterKey, offset, range?.limit],
    { refresh: false },
  )
  const accounts = useQuery(() => graphql<{ FinanceAccounts: FinanceAccount[] }>(FINANCE_ACCOUNTS), [], {
    refresh: false,
  })
  const categories = useQuery(() => graphql<{ SpendingCategories: SpendingCategory[] }>(SPENDING_CATEGORIES), [], {
    refresh: false,
  })

  // What changed on a row since it was read, laid over it, so changing one
  // row's spending category does not read the page again.
  const [changed, setChanged] = useState<Record<string, Partial<FinanceTransaction>>>({})
  // The transaction whose details are open, by id, so a change made in the
  // dialog shows there as it does in its row; and what had the focus when
  // it opened, to give it back when it closes.
  const [detailedId, setDetailedId] = useState<string | null>(null)
  // Transactions opened from another's details (a counted copy and its
  // duplicates) that the page shown may not hold.
  const [opened, setOpened] = useState<Record<string, FinanceTransaction>>({})
  const [isCounting, setIsCounting] = useState(false)
  // Raised after the person counts a copy or takes that back, so the open
  // details read again the copies they name.
  const [detailsRefreshCount, setDetailsRefreshCount] = useState(0)
  // The transactions chosen to categorize together, by id, on any page;
  // the spending rules about to be saved for them, while the person is
  // asked.
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [isSelectingAll, setIsSelectingAll] = useState(false)
  const [isApplying, setIsApplying] = useState(false)
  const [rulesConfirmation, setRulesConfirmation] = useState<{
    financeTransactionIds: string[]
    spendingCategoryId: string
    proposals: SpendingRuleProposals
  } | null>(null)
  const leftOutLines = useLeftOutLines()
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
    setChanged({})
    setSelectedIds(new Set())
  }, [filterKey])
  // Another page is read afresh, so what was laid over the last one goes.
  useEffect(() => {
    setChanged({})
  }, [range?.offset, range?.limit])

  const shownPage = first.data?.FinanceTransactions
  const totalCount = shownPage?.totalCount ?? 0
  const leftOutDuplicateCount = shownPage?.leftOutDuplicateCount ?? 0
  const rows = useMemo(
    () => (shownPage?.financeTransactions ?? []).map((row) => (changed[row.id] ? { ...row, ...changed[row.id] } : row)),
    [shownPage, changed],
  )
  const accountList = accounts.data?.FinanceAccounts ?? []
  const detailedRow = detailedId ? (rows.find((row) => row.id === detailedId) ?? opened[detailedId]) : undefined
  const detailed = detailedRow && changed[detailedRow.id] ? { ...detailedRow, ...changed[detailedRow.id] } : detailedRow
  const categoryList = categories.data?.SpendingCategories ?? []
  // categoryLabelOf is a spending category's label by its id, empty for
  // one the list does not hold.
  const categoryLabelOf = (spendingCategoryId: string) => {
    const found = categoryList.find((candidate) => candidate.id === spendingCategoryId)
    return found ? spendingCategoryLabel(found, categoryList, categoryName) : ''
  }

  // selectAll chooses every transaction the filters match, on every page,
  // by reading their ids alone from the server a few pages at a time, the
  // way the list itself is ordered. More than can be given rules at once is
  // refused rather than cut short, since the person would not see which
  // were left out.
  const selectAll = async () => {
    if (totalCount > MAXIMUM_SELECTED_TRANSACTION_COUNT) {
      toast.failed(t('finance.tooManyToSelect', { count: MAXIMUM_SELECTED_TRANSACTION_COUNT.toLocaleString(language) }))
      return
    }
    const asked = filterKey
    setIsSelectingAll(true)
    try {
      const ids: string[] = []
      let after: string | null = null
      do {
        const answer: { FinanceTransactions: { financeTransactions: { id: string }[]; nextCursor?: string | null } } =
          await graphql(FINANCE_TRANSACTION_IDS, { ...variables, limit: MAXIMUM_LISTED_TRANSACTION_COUNT, after })
        ids.push(...answer.FinanceTransactions.financeTransactions.map((row) => row.id))
        after = answer.FinanceTransactions.nextCursor ?? null
      } while (after && ids.length < MAXIMUM_SELECTED_TRANSACTION_COUNT)
      // The filters changed while the ids were read: they are another
      // list's, and that list starts with nothing chosen.
      if (asked === filterKeyNow.current) setSelectedIds(new Set(ids))
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setIsSelectingAll(false)
    }
  }

  const categorize = async (row: FinanceTransaction, spendingCategoryId: string) => {
    try {
      const answer = await graphql<{ CategorizeTransaction: { financeTransaction: FinanceTransaction } }>(
        CATEGORIZE_TRANSACTION,
        { financeTransactionId: row.id, spendingCategoryId },
      )
      setChanged((previous) => ({ ...previous, [row.id]: answer.CategorizeTransaction.financeTransaction }))
      const merchant = row.merchantName || row.description
      // Offered, not done: one grocery run filed under dining should not
      // move every visit to that shop.
      toast.done(
        t('finance.categorized'),
        merchant
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
                // The rule applies to the merchant's other transactions
                // too, so the page is read again.
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

  // reloadPage reads the page shown again. Counting a copy, or handing it
  // back to detection, can change which of its copies is counted, and
  // saved spending rules can change other rows, so rows other than the
  // ones acted on change too.
  const reloadPage = async () => {
    await first.reload(true)
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
      // Kept beside the page, so its details show the answer even when the
      // page shown does not hold it, or the page read again leaves it out.
      if (counted) setOpened((previous) => ({ ...previous, [row.id]: counted }))
      toast.done(isCountedByPerson ? t('finance.transactionCounted') : t('finance.transactionCountUndone'))
      await reloadPage()
      setDetailsRefreshCount((previous) => previous + 1)
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setIsCounting(false)
    }
  }

  // categorizeSelection gives the chosen transactions the spending category
  // as the person's choice, a few pages at a time, each piece all or none.
  // The confirmed spending rules go with the first piece only, once, so
  // they are saved exactly as confirmed and never once per piece; if that
  // piece fails they are not saved, and its transactions stay chosen.
  // What worked is shown at once and let go of; a piece that failed stays
  // chosen, so trying again acts on exactly what is left. Saved rules can
  // change other rows, so then the page shown is read again.
  const categorizeSelection = async (
    financeTransactionIds: string[],
    spendingCategoryId: string,
    spendingRules: ConfirmedSpendingRule[],
    noRulesNote = '',
  ) => {
    setIsApplying(true)
    const categorizedRows: FinanceTransaction[] = []
    const failedIds: string[] = []
    let savedRuleCount = 0
    let failure: unknown = null
    const pieces = chunks(financeTransactionIds, MAXIMUM_CATEGORIZED_TRANSACTION_COUNT)
    for (const [index, piece] of pieces.entries()) {
      try {
        const answer = await graphql<{
          CategorizeTransactions: { financeTransactions: FinanceTransaction[]; spendingRules: { id: string }[] }
        }>(CATEGORIZE_TRANSACTIONS, {
          financeTransactionIds: piece,
          spendingCategoryId,
          spendingRules: index === 0 && spendingRules.length > 0 ? spendingRules : null,
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
        await reloadPage()
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
    } else if (noRulesNote) {
      toast.done(t('finance.categorizedNoRules', { categorized, note: noRulesNote }))
    } else {
      toast.done(t('finance.bulkCategorized', { categorized }))
    }
  }

  // applyToSelection is the toolbar's Apply. With rules asked for, it
  // first asks the server, once for the whole selection, which it would
  // save and has the person confirm them; when there is none to save
  // (existing rules already cover every one, or what is left out is all
  // there was) there is nothing to confirm, and the toast says why.
  const applyToSelection = async (spendingCategoryId: string, shouldSaveSpendingRules: boolean) => {
    const financeTransactionIds = [...selectedIds]
    if (!shouldSaveSpendingRules) {
      await categorizeSelection(financeTransactionIds, spendingCategoryId, [])
      return
    }
    if (financeTransactionIds.length > MAXIMUM_PROPOSED_TRANSACTION_COUNT) {
      toast.failed(t('finance.tooManyForSpendingRules', { count: String(MAXIMUM_PROPOSED_TRANSACTION_COUNT) }))
      return
    }
    setIsApplying(true)
    let proposals: SpendingRuleProposals
    try {
      const answer = await graphql<{ ProposeSpendingRules: SpendingRuleProposals }>(PROPOSE_SPENDING_RULES, {
        financeTransactionIds,
        spendingCategoryId,
      })
      proposals = answer.ProposeSpendingRules
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
      setIsApplying(false)
      return
    }
    setIsApplying(false)
    if (proposals.spendingRuleProposals.length === 0) {
      const leftOut = leftOutLines(proposals)
      await categorizeSelection(
        financeTransactionIds,
        spendingCategoryId,
        [],
        leftOut.length > 0 ? leftOut.join('. ') : t('finance.coveredByRules'),
      )
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
    },
    {
      key: 'description',
      header: t('finance.description'),
      value: (row) => [row.merchantName, row.description, row.annotation].filter(Boolean).join(' · '),
      render: (row) => (
        <>
          <span
            className={row.duplicateOfTransactionId ? 'finance-description muted' : 'finance-description'}
            title={[row.merchantName, row.description].filter(Boolean).join(' · ')}
          >
            {row.merchantName || row.description}
            {row.merchantName && row.description !== row.merchantName ? (
              <span className="muted"> · {row.description}</span>
            ) : null}
            {(row.receiptCount ?? 0) > 0 ? (
              <>
                {' '}
                <span
                  className="finance-receipt-mark"
                  role="img"
                  aria-label={t('finance.hasReceipt')}
                  title={t('finance.hasReceipt')}
                >
                  <ReceiptIcon size={14} />
                </span>
              </>
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
          {row.annotation ? (
            <span className="finance-annotation-line" title={row.annotation}>
              {row.annotation}
            </span>
          ) : null}
        </>
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
          {/* No spending category is a transaction still to be decided, not a
              choice: it is the placeholder, and the list offers other for
              what fits nothing. */}
          <Select
            value={row.spendingCategoryId ?? ''}
            label={t('finance.spendingCategory')}
            placeholder={t('finance.uncategorized')}
            options={spendingCategoryOptions(
              categoryList,
              categoryName,
              row.spendingCategoryId,
              t('finance.transferGroup'),
            )}
            onChange={(value) => void categorize(row, value)}
          />
        </span>
      ),
    },
  ]

  // transactionCountWords is how many transactions, grouped the way the
  // reader's language groups a number: "1,234 transactions".
  const transactionCountWords = (count: number) =>
    plural(
      count,
      { one: 'finance.transactionCountOne', other: 'finance.transactionCountOther' },
      { count: count.toLocaleString(language) },
    )

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
    filters.isDuplicateIncluded ? t('finance.withDuplicates') : '',
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
              ...spendingCategoryOptions(
                categoryList,
                categoryName,
                filters.spendingCategoryId,
                t('finance.transferGroup'),
              ),
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
      <label className="checkbox">
        <input
          type="checkbox"
          checked={filters.isDuplicateIncluded}
          onChange={(event) => setFilters((previous) => ({ ...previous, isDuplicateIncluded: event.target.checked }))}
        />
        {t('finance.showDuplicates')}
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
      {shownPage ? (
        <p className="muted finance-transaction-count">
          {transactionCountWords(totalCount)}
          {leftOutDuplicateCount > 0
            ? ` · ${plural(
                leftOutDuplicateCount,
                { one: 'finance.duplicatesLeftOutOne', other: 'finance.duplicatesLeftOutOther' },
                { count: leftOutDuplicateCount.toLocaleString(language) },
              )}`
            : ''}
        </p>
      ) : null}
      <ErrorMessage error={first.error} />
      {/* Drawn before the first answer, since the table is what says which
          page to ask for; until then it holds no rows and claims none. */}
      <div className="finance-transactions-table">
        <DataTable
          columns={columns}
          rows={rows}
          rowKey={(row) => row.id}
          loading={!shownPage}
          remote={{ total: totalCount, onRange }}
          emptyMessage={t('finance.noTransactions')}
          countLabel={transactionCountWords}
          onRowOpen={openDetails}
          rowOpenLabel={(row) => t('finance.transactionDetailsOf', { name: row.merchantName || row.description })}
          selected={selectedIds}
          onSelect={setSelectedIds}
          selectionActions={(chosen) => (
            <FinanceSelectionToolbar
              selectedTransactionCount={chosen.length}
              matchingTransactionCount={totalCount}
              categoryOptions={spendingCategoryOptions(categoryList, categoryName, null, t('finance.transferGroup'))}
              isApplying={isApplying || isSelectingAll}
              onSelectAll={() => void selectAll()}
              onClear={() => setSelectedIds(new Set())}
              onApply={(spendingCategoryId, shouldSaveSpendingRules) =>
                void applyToSelection(spendingCategoryId, shouldSaveSpendingRules)
              }
            />
          )}
        />
      </div>
      {rulesConfirmation ? (
        <SpendingRulesConfirmation
          proposals={rulesConfirmation.proposals}
          spendingCategoryLabel={categoryLabelOf(rulesConfirmation.spendingCategoryId)}
          categoryLabelOf={categoryLabelOf}
          isApplying={isApplying}
          onConfirm={() => {
            setRulesConfirmation(null)
            void categorizeSelection(
              rulesConfirmation.financeTransactionIds,
              rulesConfirmation.spendingCategoryId,
              confirmedSpendingRules(rulesConfirmation.proposals.spendingRuleProposals),
            )
          }}
          onClose={() => setRulesConfirmation(null)}
        />
      ) : null}
      {detailed ? (
        <FinanceTransactionDialog
          financeTransaction={detailed}
          financeAccount={accountList.find((candidate) => candidate.id === detailed.financeAccountId)}
          financeAccounts={accountList}
          categoryOptions={spendingCategoryOptions(
            categoryList,
            categoryName,
            detailed.spendingCategoryId,
            t('finance.transferGroup'),
          )}
          isCounting={isCounting}
          refreshCount={detailsRefreshCount}
          onCategorize={(value) => void categorize(detailed, value)}
          onCount={() => void countThisOne(detailed, true)}
          onUndoCount={() => void countThisOne(detailed, false)}
          onOpenTransaction={(financeTransaction) => {
            setOpened((previous) => ({ ...previous, [financeTransaction.id]: financeTransaction }))
            setDetailedId(financeTransaction.id)
          }}
          onTransactionChanged={(changes) =>
            setChanged((previous) => ({ ...previous, [detailed.id]: { ...previous[detailed.id], ...changes } }))
          }
          onClose={closeDetails}
        />
      ) : null}
    </SettingsSection>
  )
}
