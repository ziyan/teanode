import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { askAgentAbout, graphql } from '../../api'
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
const ask = vi.mocked(askAgentAbout)
afterEach(() => {
  cleanup()
  execute.mockReset()
  ask.mockReset()
  vi.unstubAllGlobals()
})

const account = {
  id: 'account-invented',
  accountName: 'Invented Brokerage',
  accountKind: 'investment',
  currencyCode: 'USD',
} as FinanceAccount

const fee: FinanceTransaction = {
  id: 'transaction-invented',
  financeAccountId: 'account-invented',
  providerTransactionId: 'provider-invented',
  postedOn: '2026-06-09',
  amount: '-50.0000',
  currencyCode: 'USD',
  description: 'INVENTED BROKERAGE MONTHLY FEE',
  merchantName: 'Invented Brokerage',
  isPending: false,
  createdAt: '2026-06-09T12:00:00Z',
  modifiedAt: '2026-06-09T12:00:00Z',
}

function renderDialog() {
  execute.mockResolvedValue({
    FinanceTransactions: { financeTransactions: [], totalCount: 0, leftOutDuplicateCount: 0 },
  })
  const onClose = vi.fn()
  render(
    <FinanceTransactionDialog
      financeTransaction={fee}
      financeAccount={account}
      financeAccounts={[account]}
      categoryOptions={[]}
      onCategorize={vi.fn()}
      onCount={vi.fn()}
      onUndoCount={vi.fn()}
      onOpenTransaction={vi.fn()}
      onClose={onClose}
    />,
  )
  return { onClose }
}

// Ask the agent hands the drawer the transaction with what its chip shows,
// and closes the dialog so its scrim does not cover the drawer.
it('points the agent at the transaction and closes', () => {
  ask.mockReturnValue(true)
  const { onClose } = renderDialog()
  const button = screen.getByRole('button', { name: /Invented Brokerage.*finance\.askAgent/ })
  fireEvent.click(button)
  expect(ask).toHaveBeenCalledWith({
    financeTransactionId: 'transaction-invented',
    postedOn: '2026-06-09',
    amount: '-50.0000',
    currencyCode: 'USD',
    merchantName: 'Invented Brokerage',
    description: 'INVENTED BROKERAGE MONTHLY FEE',
  })
  expect(onClose).toHaveBeenCalled()
})

// With no drawer to take it, the person is sent to the agent page.
it('goes to the agent page when there is no drawer', () => {
  ask.mockReturnValue(false)
  const assign = vi.fn()
  vi.stubGlobal('location', { ...window.location, assign })
  renderDialog()
  fireEvent.click(screen.getByRole('button', { name: /finance\.askAgent/ }))
  expect(assign).toHaveBeenCalledWith('/settings/agent')
})

// The close button, not the far-side icon, is where the keyboard lands.
it('still opens with the close button focused', () => {
  renderDialog()
  expect(document.activeElement?.textContent).toBe('common.close')
})
