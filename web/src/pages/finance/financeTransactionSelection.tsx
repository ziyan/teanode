import { useState } from 'react'

import { CloseIcon } from '../../components/icons'
import { Select, SelectOption } from '../../components/select'
import { Tooltip } from '../../components/tooltip'
import { useTranslation } from '../../i18n/i18n'
import { SpendingRuleProposal } from './financeApi'

// The choice that takes the spending category away, as the first option
// of the list in a transaction's row and its details. An empty value is
// the list with nothing chosen yet, so this one needs a value of its own;
// a spending category's id never looks like it.
const UNCATEGORIZED_CHOICE = 'uncategorized'

// chunks cuts a list into pieces of at most size, in order: the server
// takes a few pages of transactions at a time.
export function chunks<Item>(items: Item[], size: number): Item[][] {
  const pieces: Item[][] = []
  for (let start = 0; start < items.length; start += size) {
    pieces.push(items.slice(start, start + size))
  }
  return pieces
}

// mergeProposals adds up the spending rules proposed for each piece of a
// selection: one per match text in any case, its transactions counted
// across the pieces, the most first.
export function mergeProposals(pieces: SpendingRuleProposal[][]): SpendingRuleProposal[] {
  const byText = new Map<string, SpendingRuleProposal>()
  for (const proposal of pieces.flat()) {
    const key = proposal.matchText.toLowerCase()
    const known = byText.get(key)
    byText.set(
      key,
      known
        ? { ...known, financeTransactionCount: known.financeTransactionCount + proposal.financeTransactionCount }
        : proposal,
    )
  }
  return [...byText.values()].sort(
    (left, right) =>
      right.financeTransactionCount - left.financeTransactionCount || left.matchText.localeCompare(right.matchText),
  )
}

// FinanceSelectionToolbar is what can be done to the transactions chosen
// in the list, drawn by the table above it while any are chosen: how many,
// choosing every one loaded, the spending category to give them (the same
// list as a row's, the transfer category in its own group), whether to
// save spending rules so later ones like them are filed the same way, and
// letting go of the selection.
export function FinanceSelectionToolbar({
  selectedTransactionCount,
  loadedTransactionCount,
  categoryOptions,
  isApplying,
  onSelectAllLoaded,
  onClear,
  onApply,
}: {
  selectedTransactionCount: number
  loadedTransactionCount: number
  categoryOptions: SelectOption[]
  isApplying: boolean
  onSelectAllLoaded: () => void
  onClear: () => void
  // An empty spending category takes it away; rules are asked for only
  // with a spending category to file under.
  onApply: (spendingCategoryId: string, shouldSaveSpendingRules: boolean) => void
}) {
  const { t } = useTranslation()
  const [chosen, setChosen] = useState('')
  const [shouldSaveSpendingRules, setShouldSaveSpendingRules] = useState(false)
  const isUncategorizedChosen = chosen === UNCATEGORIZED_CHOICE
  return (
    <div className="finance-selection-toolbar" role="group" aria-label={t('finance.selectionActions')}>
      <span className="muted">{t('finance.selectedTransactions', { count: String(selectedTransactionCount) })}</span>
      {selectedTransactionCount < loadedTransactionCount ? (
        <button type="button" onClick={onSelectAllLoaded}>
          {t('finance.selectAllLoaded', { count: String(loadedTransactionCount) })}
        </button>
      ) : null}
      <Select
        value={chosen}
        label={t('finance.spendingCategory')}
        placeholder={t('finance.chooseBulkSpendingCategory')}
        options={[{ value: UNCATEGORIZED_CHOICE, label: t('finance.uncategorized') }, ...categoryOptions]}
        onChange={setChosen}
      />
      <label className="checkbox">
        <input
          type="checkbox"
          checked={shouldSaveSpendingRules && !isUncategorizedChosen}
          disabled={isUncategorizedChosen}
          onChange={(event) => setShouldSaveSpendingRules(event.target.checked)}
        />
        {t('finance.saveAsSpendingRules')}
      </label>
      <button
        type="button"
        className="primary"
        disabled={!chosen || isApplying}
        onClick={() =>
          onApply(isUncategorizedChosen ? '' : chosen, shouldSaveSpendingRules && !isUncategorizedChosen)
        }
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
