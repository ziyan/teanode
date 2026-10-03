import { useState } from 'react'

import { CloseIcon } from '../../components/icons'
import { Select, SelectOption } from '../../components/select'
import { Tooltip } from '../../components/tooltip'
import { useTranslation } from '../../i18n/i18n'
import { ConfirmedSpendingRule, SpendingRuleProposal } from './financeApi'

// chunks cuts a list into pieces of at most size, in order: the server
// takes a few pages of transactions at a time.
export function chunks<Item>(items: Item[], size: number): Item[][] {
  const pieces: Item[][] = []
  for (let start = 0; start < items.length; start += size) {
    pieces.push(items.slice(start, start + size))
  }
  return pieces
}

// confirmedSpendingRules is the proposed spending rules as
// CATEGORIZE_TRANSACTIONS takes them back, so the server saves exactly
// what the person read: the rules are proposed once over the whole
// selection, never per piece, and sent once.
export function confirmedSpendingRules(proposals: SpendingRuleProposal[]): ConfirmedSpendingRule[] {
  return proposals.map((proposal) => ({
    matchText: proposal.matchText,
    spendingCategoryId: proposal.spendingCategoryId,
    ...(proposal.aheadOfSpendingRule ? { aheadOfSpendingRuleId: proposal.aheadOfSpendingRule.id } : {}),
  }))
}

// FinanceSelectionToolbar is what can be done to the transactions chosen
// in the list, drawn by the table above it while any are chosen: how many,
// on every page, choosing every one the filters match, the spending
// category to give them (the same list as a row's, the transfer category
// in its own group, and no choice of none: what fits nothing goes to
// other), whether to save spending rules so later ones like them are
// filed the same way, and letting go of the selection.
export function FinanceSelectionToolbar({
  selectedTransactionCount,
  matchingTransactionCount,
  categoryOptions,
  isApplying,
  onSelectAll,
  onClear,
  onApply,
}: {
  selectedTransactionCount: number
  matchingTransactionCount: number
  categoryOptions: SelectOption[]
  isApplying: boolean
  onSelectAll: () => void
  onClear: () => void
  onApply: (spendingCategoryId: string, shouldSaveSpendingRules: boolean) => void
}) {
  const { t, language } = useTranslation()
  const [chosen, setChosen] = useState('')
  const [shouldSaveSpendingRules, setShouldSaveSpendingRules] = useState(false)
  return (
    <div className="finance-selection-toolbar" role="group" aria-label={t('finance.selectionActions')}>
      <span className="muted">{t('finance.selectedTransactions', { count: selectedTransactionCount.toLocaleString(language) })}</span>
      {selectedTransactionCount < matchingTransactionCount ? (
        <button type="button" disabled={isApplying} onClick={onSelectAll}>
          {t('finance.selectAllMatching', { count: matchingTransactionCount.toLocaleString(language) })}
        </button>
      ) : null}
      <Select
        value={chosen}
        label={t('finance.spendingCategory')}
        placeholder={t('finance.chooseBulkSpendingCategory')}
        options={categoryOptions}
        onChange={setChosen}
      />
      <label className="checkbox">
        <input
          type="checkbox"
          checked={shouldSaveSpendingRules}
          onChange={(event) => setShouldSaveSpendingRules(event.target.checked)}
        />
        {t('finance.saveAsSpendingRules')}
      </label>
      <button
        type="button"
        className="primary"
        disabled={!chosen || isApplying}
        onClick={() => onApply(chosen, shouldSaveSpendingRules)}
      >
        {t('finance.applySpendingCategory')}
      </button>
      <Tooltip label={t('finance.clearSelection')}>
        <button
          type="button"
          className="icon-button"
          aria-label={t('finance.clearSelection')}
          disabled={isApplying}
          onClick={onClear}
        >
          <CloseIcon size={16} />
        </button>
      </Tooltip>
    </div>
  )
}
