import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { FinanceAccount, FinanceTransaction } from './financeApi'
import { FinanceTransactionDialog } from './financeTransactionDialog'
import { FinanceTransactionsSection } from './financeTransactions'

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

// A duplicate names the counted copy by its account and day, asked for by
// its id so a busy day cannot hide it, opens it, and offers Count this one as a real
// button; its amount is struck through, and it says its spending category
// counts for nothing while it is a duplicate.
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
  expect(execute).toHaveBeenCalledWith(expect.any(String), { financeTransactionIds: ['fee-counted'] })
  fireEvent.click(link)
  expect(onOpenTransaction).toHaveBeenCalledWith(counted)
  expect(screen.getByText('finance.duplicateCategoryNotCounted')).toBeTruthy()
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
  expect(screen.queryByText('finance.duplicateCategoryNotCounted')).toBeNull()
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

// isDuplicateTagged says whether the row showing this merchant carries the
// Duplicate tag.
function isDuplicateTagged(merchantName: string): boolean {
  const description = Array.from(document.querySelectorAll('.finance-description')).find(
    (candidate) => candidate.getAttribute('title') === merchantName + ' · ACCOUNT FEE',
  )
  if (!description) throw new Error(`no row shows ${merchantName}`)
  return Array.from(description.querySelectorAll('.tag')).some((tag) => tag.textContent === 'finance.duplicate')
}

// Handing a copy back to detection can make it the counted copy and the
// one that was counted a duplicate of it, so every page read so far is
// read again, the second page from the same cursor, and not only the row
// clicked; and the open details read again the duplicates they name.
it('reads every loaded page and the details again after the person counts or takes it back', async () => {
  window.matchMedia = ((query: string) => ({
    matches: true,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  })) as unknown as typeof window.matchMedia
  const second = fee('fee-second', 'account-second', { merchantName: 'Fee on second', duplicateDecidedBy: 'person' })
  let firstPage = [second, fee('fee-first', 'account-first', { merchantName: 'Fee on first' })]
  let secondPage = [fee('fee-third', 'account-first', { merchantName: 'Fee on third', duplicateOfTransactionId: 'fee-first' })]
  const undone = { ...second, duplicateDecidedBy: '' }
  let isUndone = false
  execute.mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('UndoCountTransaction')) {
      isUndone = true
      // The copy stored first counts again, and the others are its duplicates.
      firstPage = [
        { ...second, duplicateDecidedBy: '' },
        fee('fee-first', 'account-first', {
          merchantName: 'Fee on first',
          duplicateOfTransactionId: 'fee-second',
          duplicateDecidedBy: 'mirror_detection',
        }),
      ]
      secondPage = [
        fee('fee-third', 'account-first', {
          merchantName: 'Fee on third',
          duplicateOfTransactionId: 'fee-second',
          duplicateDecidedBy: 'mirror_detection',
        }),
      ]
      return { UndoCountTransaction: undone }
    }
    if (document.includes('FinanceAccounts')) return { FinanceAccounts: accounts }
    if (document.includes('SpendingCategories')) return { SpendingCategories: [] }
    if (variables?.duplicateOfTransactionId === 'fee-second') {
      return { FinanceTransactions: { financeTransactions: isUndone ? [firstPage[1], secondPage[0]] : [], nextCursor: null } }
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
  fireEvent.click(await screen.findByText('finance.loadMore'))
  expect(await screen.findByText('Fee on third')).toBeTruthy()
  expect(isDuplicateTagged('Fee on first')).toBe(false)

  fireEvent.click(screen.getByText('Fee on second'))
  fireEvent.click(await screen.findByText('finance.letDetectionDecide'))
  await waitFor(() => expect(isDuplicateTagged('Fee on first')).toBe(true))
  expect(isDuplicateTagged('Fee on third')).toBe(true)
  expect(isDuplicateTagged('Fee on second')).toBe(false)
  // Still on the copy taken back, which counts now: its details name the
  // two that became its duplicates, though nothing about it changed.
  expect(await screen.findByText('finance.duplicates')).toBeTruthy()
  expect(screen.getAllByText(/finance\.copyOnAccount .*First Brokerage/).length).toBe(2)
  expect(execute).toHaveBeenCalledWith(expect.stringContaining('UndoCountTransaction'), { financeTransactionId: 'fee-second' })
  const secondPageReads = execute.mock.calls.filter(([, variables]) => variables?.after === 'cursor-second-page')
  expect(secondPageReads.length).toBe(2)
})
