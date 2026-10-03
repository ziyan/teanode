import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { en } from '../../i18n/en'
import { ja } from '../../i18n/ja'
import { zh } from '../../i18n/zh'
import { FinanceAccount, FinanceTransaction } from './financeApi'
import { FinanceTransactionDialog } from './financeTransactionDialog'

vi.mock('../../api', () => ({ graphql: vi.fn(), askAgentAbout: vi.fn() }))
vi.mock('../../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) => (values ? `${key} ${JSON.stringify(values)}` : key),
    plural: (count: number, forms: { one: string; other: string }) => (count === 1 ? forms.one : forms.other),
  }),
}))
vi.mock('../../components/toast', () => ({ useToast: () => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }) }))
const execute = vi.mocked(graphql)
afterEach(() => {
  cleanup()
  execute.mockReset()
})

const account = {
  id: 'account-invented',
  accountName: 'Invented Checking',
  accountKind: 'depository',
  currencyCode: 'USD',
} as FinanceAccount

const waiting: FinanceTransaction = {
  id: 'transaction-invented',
  financeAccountId: 'account-invented',
  providerTransactionId: 'provider-invented',
  postedOn: '2026-06-09',
  amount: '-18.0000',
  currencyCode: 'USD',
  description: 'INVENTED KIOSK',
  merchantName: 'Invented Kiosk',
  isPending: false,
  createdAt: '2026-06-09T12:00:00Z',
  modifiedAt: '2026-06-09T12:00:00Z',
}

// No spending category is a transaction still to be decided: the details
// say it needs a category, and the list to choose from has no choice of
// none, only the spending categories, other among them.
it('says a waiting transaction needs a category and offers no choice of none', () => {
  execute.mockResolvedValue({
    FinanceTransactions: { financeTransactions: [], totalCount: 0, leftOutDuplicateCount: 0 },
  })
  const onCategorize = vi.fn()
  render(
    <FinanceTransactionDialog
      financeTransaction={waiting}
      financeAccount={account}
      financeAccounts={[account]}
      categoryOptions={[
        { value: 'category-dining', label: 'Invented Dining' },
        { value: 'category-other', label: 'Other' },
      ]}
      onCategorize={onCategorize}
      onCount={vi.fn()}
      onUndoCount={vi.fn()}
      onOpenTransaction={vi.fn()}
      onClose={vi.fn()}
    />,
  )
  const picker = screen.getByRole('combobox', { name: 'finance.spendingCategory' })
  expect(picker.textContent).toBe('finance.uncategorized')
  fireEvent.click(picker)
  expect(screen.getAllByRole('option').map((option) => option.textContent)).toEqual(['Invented Dining', 'Other'])
  fireEvent.click(screen.getByRole('option', { name: 'Other' }))
  expect(onCategorize).toHaveBeenCalledWith('category-other')
})

// The words for what still needs a category, in the filter, the lists and
// the summary, in every language.
it('names what is waiting as needing a category', () => {
  expect(en['finance.onlyUncategorized']).toBe('Needs a category')
  expect(en['finance.uncategorized']).toBe('Needs a category')
  for (const catalog of [en, zh, ja]) {
    expect(catalog['finance.uncategorized']).not.toMatch(/No spending category|无支出类别|支出カテゴリなし/)
  }
})
