import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { FinanceTransaction, SpendingRuleProposals } from './financeApi'
import { chunks, confirmedSpendingRules } from './financeTransactionSelection'
import { FinanceTransactionsSection } from './financeTransactions'

const toast = vi.hoisted(() => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }))

vi.mock('../../api', () => ({ graphql: vi.fn() }))
vi.mock('../../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) => (values ? `${key} ${JSON.stringify(values)}` : key),
    plural: (count: number, forms: { one: string; other: string }) => `${count === 1 ? forms.one : forms.other} ${count}`,
  }),
}))
vi.mock('../../components/toast', () => ({ useToast: () => toast }))
// Two at a time rather than five hundred, so a selection of three is two
// pieces and one of them can fail on its own: the path a selection of more
// than five hundred takes.
vi.mock('./financeApi', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./financeApi')>()),
  MAXIMUM_CATEGORIZED_TRANSACTION_COUNT: 2,
}))
const execute = vi.mocked(graphql)

beforeEach(() => {
  window.matchMedia = ((query: string) => ({
    matches: true,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  })) as unknown as typeof window.matchMedia
})
afterEach(() => {
  cleanup()
  execute.mockReset()
  toast.done.mockReset()
  toast.failed.mockReset()
  toast.failure.mockReset()
})

describe('chunks', () => {
  it('cuts a list into pieces in order', () => {
    expect(chunks(['a1', 'a2', 'a3', 'a4', 'a5'], 2)).toEqual([['a1', 'a2'], ['a3', 'a4'], ['a5']])
    expect(chunks([], 2)).toEqual([])
  })
})

// What the whole selection was proposed, the person confirmed.
const proposed: SpendingRuleProposals = {
  spendingRuleProposals: [
    {
      matchText: 'Invented Bistro',
      spendingCategoryId: 'category-dining',
      financeTransactionCount: 1,
      changedTransactionCount: 3,
      aheadOfSpendingRule: { id: 'rule-bistro', matchText: 'bistro', spendingCategoryId: 'category-groceries' },
    },
    { matchText: 'Noodle Place', spendingCategoryId: 'category-dining', financeTransactionCount: 1, changedTransactionCount: 0 },
  ],
  tooGenericMatchTextCount: 0,
  changingNumberMatchTextCount: 2,
  overLimitMatchTextCount: 0,
}

describe('confirmedSpendingRules', () => {
  // Sent back as proposed: the words, the spending category and the rule
  // each goes ahead of.
  it('is each proposal as the server takes it back', () => {
    expect(confirmedSpendingRules(proposed.spendingRuleProposals)).toEqual([
      { matchText: 'Invented Bistro', spendingCategoryId: 'category-dining', aheadOfSpendingRuleId: 'rule-bistro' },
      { matchText: 'Noodle Place', spendingCategoryId: 'category-dining' },
    ])
  })
})

const purchase = (id: string, merchantName: string, change: Partial<FinanceTransaction> = {}): FinanceTransaction => ({
  id,
  financeAccountId: 'account-one',
  providerTransactionId: `provider-${id}`,
  postedOn: '2026-09-15',
  amount: '-12.0000',
  currencyCode: 'USD',
  description: merchantName.toUpperCase(),
  merchantName,
  isPending: false,
  createdAt: '2026-09-15T12:00:00Z',
  modifiedAt: '2026-09-15T12:00:00Z',
  ...change,
})

const firstPage = [
  purchase('purchase-one', 'Invented Bistro'),
  purchase('purchase-two', 'Noodle Place'),
  purchase('purchase-three', 'Kiosk Example'),
  purchase('purchase-four', 'Corner Shop Example', { duplicateOfTransactionId: 'purchase-elsewhere' }),
]
const secondPage = [purchase('purchase-five', 'Late Example')]

// serve answers the page's documents; categorize answers
// CategorizeTransactions for one piece, and proposals is what
// ProposeSpendingRules answers for the whole selection.
function serve(categorize?: (variables: Record<string, unknown>) => unknown, proposals: SpendingRuleProposals = proposed) {
  execute.mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('FinanceAccounts')) return { FinanceAccounts: [] }
    if (document.includes('SpendingCategories')) {
      return {
        SpendingCategories: [
          { id: 'category-dining', spendingCategoryName: 'Invented Dining' },
          { id: 'category-groceries', spendingCategoryName: 'Invented Groceries' },
        ],
      }
    }
    if (document.includes('ProposeSpendingRules')) return { ProposeSpendingRules: proposals }
    if (document.includes('CategorizeTransactions')) {
      if (categorize) return categorize(variables ?? {})
      const ids = variables?.financeTransactionIds as string[]
      return {
        CategorizeTransactions: {
          financeTransactions: [...firstPage, ...secondPage]
            .filter((row) => ids.includes(row.id))
            .map((row) => ({ ...row, spendingCategoryId: variables?.spendingCategoryId, categorizedBy: 'person' })),
          spendingRules: ((variables?.spendingRules as { matchText: string }[] | null) ?? []).map((rule, index) => ({
            id: `rule-${index}`,
            matchText: rule.matchText,
          })),
        },
      }
    }
    if (variables?.after === 'cursor-second-page') {
      return { FinanceTransactions: { financeTransactions: secondPage, nextCursor: null } }
    }
    return { FinanceTransactions: { financeTransactions: firstPage, nextCursor: 'cursor-second-page' } }
  })
  render(
    <MemoryRouter>
      <FinanceTransactionsSection />
    </MemoryRouter>,
  )
}

const rowBoxes = () => screen.getAllByLabelText('table.choose') as HTMLInputElement[]
const toolbar = () => document.querySelector<HTMLElement>('.finance-selection-toolbar')
const selectedCount = () => toolbar()?.querySelector('.muted')?.textContent

// chooseCategory picks the spending category in the toolbar's list.
function chooseCategory(label: string) {
  fireEvent.click(within(toolbar() as HTMLElement).getByRole('combobox'))
  fireEvent.click(screen.getByRole('option', { name: label }))
}

it('chooses rows, a run with shift, and every loaded row, and keeps them across Load more', async () => {
  serve()
  await screen.findByText('Invented Bistro')
  expect(toolbar()).toBeNull()

  fireEvent.click(rowBoxes()[0])
  expect(selectedCount()).toBe('finance.selectedTransactions {"count":"1"}')
  // Choosing does not open the row's details.
  expect(document.querySelector('.finance-transaction-details')).toBeNull()

  // Shift chooses the run from the last box clicked.
  fireEvent.click(rowBoxes()[2], { shiftKey: true })
  expect(selectedCount()).toBe('finance.selectedTransactions {"count":"3"}')
  expect(rowBoxes().map((box) => box.checked)).toEqual([true, true, true, false])

  fireEvent.click(screen.getByText('finance.loadMore'))
  await screen.findByText('Late Example')
  expect(selectedCount()).toBe('finance.selectedTransactions {"count":"3"}')

  fireEvent.click(screen.getByText('finance.selectAllLoaded {"count":"5"}'))
  expect(selectedCount()).toBe('finance.selectedTransactions {"count":"5"}')
  expect(screen.queryByText(/finance\.selectAllLoaded/)).toBeNull()

  // The header's box lets go of every row shown.
  fireEvent.click(screen.getByLabelText('table.chooseAll'))
  expect(toolbar()).toBeNull()

  fireEvent.click(rowBoxes()[1])
  fireEvent.click(screen.getByLabelText('finance.clearSelection'))
  expect(toolbar()).toBeNull()
})

it('gives the chosen ones a spending category, a duplicate among them, and lets go of them', async () => {
  serve()
  await screen.findByText('Invented Bistro')
  fireEvent.click(rowBoxes()[0])
  fireEvent.click(rowBoxes()[3])
  const apply = screen.getByText('finance.applySpendingCategory') as HTMLButtonElement
  expect(apply.disabled).toBe(true)
  expect(apply.className).not.toContain('link')
  chooseCategory('Invented Dining')
  fireEvent.click(apply)

  await waitFor(() => expect(toolbar()).toBeNull())
  expect(execute).toHaveBeenCalledWith(expect.stringContaining('CategorizeTransactions('), {
    financeTransactionIds: ['purchase-one', 'purchase-four'],
    spendingCategoryId: 'category-dining',
    spendingRules: null,
  })
  expect(execute.mock.calls.some(([document]) => document.includes('ProposeSpendingRules'))).toBe(false)
  expect(toast.done).toHaveBeenCalledWith(expect.stringContaining('finance.bulkCategorized'))
  expect(toast.done.mock.calls[0][0]).toContain('finance.categorizedCountOther 2')
})

it('proposes once for the whole selection, says what it will save, and sends it once', async () => {
  serve()
  await screen.findByText('Invented Bistro')
  fireEvent.click(rowBoxes()[0])
  fireEvent.click(rowBoxes()[2], { shiftKey: true })
  chooseCategory('Invented Dining')
  fireEvent.click(screen.getByText('finance.saveAsSpendingRules'))
  fireEvent.click(screen.getByText('finance.applySpendingCategory'))

  // One proposal over all three, though they are categorized in two
  // pieces: each rule with how many others it changes and the rule it goes
  // ahead of, then what was left out.
  const listed = await screen.findByText(/Invented Bistro/, { selector: '.finance-rule-proposals li' })
  expect(listed.textContent).toContain('finance.ruleChangesOther 3')
  expect(listed.textContent).toContain('finance.ruleGoesAheadOf {"matchText":"bistro","category":"Invented Groceries"}')
  expect(screen.getByText(/Noodle Place/, { selector: '.finance-rule-proposals li' })).toBeTruthy()
  expect(screen.getByText('finance.leftOutChangingNumberOther 2')).toBeTruthy()
  const proposals = execute.mock.calls.filter(([document]) => document.includes('ProposeSpendingRules'))
  expect(proposals).toHaveLength(1)
  expect(proposals[0][1]).toEqual({
    financeTransactionIds: ['purchase-one', 'purchase-two', 'purchase-three'],
    spendingCategoryId: 'category-dining',
  })
  expect(execute.mock.calls.some(([document]) => document.includes('CategorizeTransactions('))).toBe(false)

  fireEvent.click(screen.getByText('finance.applyAndSaveSpendingRules'))
  await waitFor(() => expect(toast.done).toHaveBeenCalled())
  // The confirmed rules go with the first piece only.
  expect(execute).toHaveBeenCalledWith(expect.stringContaining('CategorizeTransactions('), {
    financeTransactionIds: ['purchase-one', 'purchase-two'],
    spendingCategoryId: 'category-dining',
    spendingRules: confirmedSpendingRules(proposed.spendingRuleProposals),
  })
  expect(execute).toHaveBeenCalledWith(expect.stringContaining('CategorizeTransactions('), {
    financeTransactionIds: ['purchase-three'],
    spendingCategoryId: 'category-dining',
    spendingRules: null,
  })
  expect(toast.done.mock.calls[0][0]).toContain('finance.categorizedAndSaved')
  expect(toast.done.mock.calls[0][0]).toContain('finance.categorizedCountOther 3')
  expect(toast.done.mock.calls[0][0]).toContain('finance.savedRuleCountOther 2')
  expect(toolbar()).toBeNull()
})

it('says why no rule was saved when every one was left out', async () => {
  serve(undefined, {
    spendingRuleProposals: [],
    tooGenericMatchTextCount: 1,
    changingNumberMatchTextCount: 0,
    overLimitMatchTextCount: 0,
  })
  await screen.findByText('Invented Bistro')
  fireEvent.click(rowBoxes()[0])
  chooseCategory('Invented Dining')
  fireEvent.click(screen.getByText('finance.saveAsSpendingRules'))
  fireEvent.click(screen.getByText('finance.applySpendingCategory'))

  await waitFor(() => expect(toast.done).toHaveBeenCalled())
  expect(document.querySelector('.finance-rule-proposals')).toBeNull()
  expect(toast.done.mock.calls[0][0]).toContain('finance.categorizedNoRules')
  expect(toast.done.mock.calls[0][0]).toContain('finance.leftOutTooGenericOne 1')
})

it('keeps chosen the ones a failed piece held, and says how many', async () => {
  serve((variables) => {
    const ids = variables.financeTransactionIds as string[]
    if (ids.includes('purchase-three')) throw new Error('refused')
    return {
      CategorizeTransactions: {
        financeTransactions: firstPage.filter((row) => ids.includes(row.id)),
        spendingRules: [],
      },
    }
  })
  await screen.findByText('Invented Bistro')
  fireEvent.click(rowBoxes()[0])
  fireEvent.click(rowBoxes()[2], { shiftKey: true })
  chooseCategory('Invented Dining')
  fireEvent.click(screen.getByText('finance.applySpendingCategory'))

  await waitFor(() => expect(toast.failed).toHaveBeenCalled())
  expect(toast.failed.mock.calls[0][0]).toContain('finance.categorizePartlyFailed')
  expect(toast.failed.mock.calls[0][0]).toContain('"failedCount":"1"')
  expect(selectedCount()).toBe('finance.selectedTransactions {"count":"1"}')
  expect(rowBoxes().map((box) => box.checked)).toEqual([false, false, true, false])
})

it('lets go of the selection when a filter changes', async () => {
  serve()
  await screen.findByText('Invented Bistro')
  fireEvent.click(rowBoxes()[0])
  expect(toolbar()).not.toBeNull()
  fireEvent.click(screen.getByText('finance.onlyUncategorized'))
  await waitFor(() => expect(toolbar()).toBeNull())
})
