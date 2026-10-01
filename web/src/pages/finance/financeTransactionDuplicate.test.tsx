import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { FinanceAccount, FinanceTransaction } from './financeApi'
import { FinanceTransactionDialog } from './financeTransactionDialog'

vi.mock('../../api', () => ({ graphql: vi.fn() }))
vi.mock('../../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) => (values ? `${key} ${JSON.stringify(values)}` : key),
    plural: (count: number, forms: { one: string; other: string }, values?: Record<string, string>) =>
      `${count === 1 ? forms.one : forms.other} ${JSON.stringify(values)}`,
  }),
}))
vi.mock('../../components/toast', () => ({ useToast: () => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }) }))
const execute = vi.mocked(graphql)
afterEach(() => {
  cleanup()
  execute.mockReset()
})

const account = (id: string, accountName: string): FinanceAccount =>
  ({ id, accountName, accountKind: 'investment', currencyCode: 'USD' }) as FinanceAccount

const accounts = [account('account-first', 'First Brokerage'), account('account-second', 'Second Brokerage')]

const fee = (id: string, financeAccountId: string, change: Partial<FinanceTransaction> = {}): FinanceTransaction => ({
  id,
  financeAccountId,
  providerTransactionId: `provider-${id}`,
  postedOn: '2026-09-15',
  amount: '-25.0000',
  currencyCode: 'USD',
  description: 'ACCOUNT FEE',
  isPending: false,
  createdAt: '2026-09-15T12:00:00Z',
  modifiedAt: '2026-09-15T12:00:00Z',
  ...change,
})

function renderDialog(financeTransaction: FinanceTransaction, handlers: Partial<Record<string, () => void>> = {}) {
  const onOpenTransaction = vi.fn()
  render(
    <FinanceTransactionDialog
      financeTransaction={financeTransaction}
      financeAccount={accounts.find((candidate) => candidate.id === financeTransaction.financeAccountId)}
      financeAccounts={accounts}
      categoryOptions={[]}
      onCategorize={vi.fn()}
      onCount={handlers.onCount ?? vi.fn()}
      onUndoCount={handlers.onUndoCount ?? vi.fn()}
      onOpenTransaction={onOpenTransaction}
      onClose={vi.fn()}
    />,
  )
  return { onOpenTransaction }
}

// A duplicate names the counted copy by its account and day, from that
// day's transactions, opens it, and offers Count this one as a real
// button; its amount is struck through.
it('says what a duplicate mirrors and counts it on request', async () => {
  const counted = fee('fee-counted', 'account-first')
  execute.mockResolvedValue({ FinanceTransactions: { financeTransactions: [counted], nextCursor: null } })
  const onCount = vi.fn()
  const { onOpenTransaction } = renderDialog(
    fee('fee-copy', 'account-second', { duplicateOfTransactionId: 'fee-counted', duplicateDecidedBy: 'mirror_detection' }),
    { onCount },
  )
  expect(screen.getByText('finance.duplicateOf')).toBeTruthy()
  const link = await screen.findByText(/finance\.copyOnAccount .*First Brokerage/)
  expect(execute).toHaveBeenCalledWith(expect.any(String), { from: '2026-09-15', to: '2026-09-15', limit: 200 })
  fireEvent.click(link)
  expect(onOpenTransaction).toHaveBeenCalledWith(counted)
  const countButton = screen.getByText('finance.countThisOne')
  expect(countButton.className).not.toContain('link')
  fireEvent.click(countButton)
  expect(onCount).toHaveBeenCalled()
  expect(document.querySelector('.finance-duplicate-amount')).toBeTruthy()
})

// The counted copy lists its duplicates and offers nothing to count.
it('names the duplicates of a counted copy', async () => {
  execute.mockResolvedValue({
    FinanceTransactions: {
      financeTransactions: [fee('fee-copy', 'account-second', { duplicateOfTransactionId: 'fee-counted' })],
      nextCursor: null,
    },
  })
  renderDialog(fee('fee-counted', 'account-first'))
  expect(await screen.findByText(/finance\.copyOnAccount .*Second Brokerage/)).toBeTruthy()
  expect(execute).toHaveBeenCalledWith(expect.any(String), { duplicateOfTransactionId: 'fee-counted', limit: 200 })
  expect(screen.getByText('finance.duplicates')).toBeTruthy()
  expect(screen.queryByText('finance.countThisOne')).toBeNull()
  expect(document.querySelector('.finance-duplicate-amount')).toBeNull()
})

// One the person counted says so, and hands it back to detection.
it('takes back the person counting a copy', async () => {
  execute.mockResolvedValue({ FinanceTransactions: { financeTransactions: [], nextCursor: null } })
  const onUndoCount = vi.fn()
  renderDialog(fee('fee-copy', 'account-second', { duplicateDecidedBy: 'person' }), { onUndoCount })
  expect(screen.getByText('finance.countedByPerson')).toBeTruthy()
  fireEvent.click(screen.getByText('finance.letDetectionDecide'))
  expect(onUndoCount).toHaveBeenCalled()
})
