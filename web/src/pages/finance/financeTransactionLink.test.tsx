import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { FinanceAccount, FinanceTransaction } from './financeApi'
import { FinanceTransactionsSection } from './financeTransactions'

vi.mock('../../api', () => ({ graphql: vi.fn(), askAgentAbout: vi.fn() }))
vi.mock('../../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) => (values ? `${key} ${JSON.stringify(values)}` : key),
    plural: (count: number, forms: { one: string; other: string }, values?: Record<string, string>) =>
      `${count === 1 ? forms.one : forms.other} ${JSON.stringify(values)}`,
    language: 'en',
  }),
}))
vi.mock('../../components/toast', () => ({ useToast: () => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }) }))
const execute = vi.mocked(graphql)
afterEach(() => {
  cleanup()
  execute.mockReset()
})

const account = {
  id: 'account-first',
  accountName: 'First Checking',
  accountKind: 'depository',
  currencyCode: 'USD',
} as FinanceAccount

const charge = (id: string, merchantName: string): FinanceTransaction => ({
  id,
  financeAccountId: 'account-first',
  providerTransactionId: `provider-${id}`,
  postedOn: '2026-09-15',
  amount: '-25.0000',
  currencyCode: 'USD',
  description: merchantName.toUpperCase(),
  merchantName,
  isPending: false,
  createdAt: '2026-09-15T12:00:00Z',
  modifiedAt: '2026-09-15T12:00:00Z',
})

let shownSearch = ''
function SearchProbe() {
  shownSearch = useLocation().search
  return null
}

// A link from the agent's memory names a transaction that is nowhere on the
// page shown: it opens by its identifier, says which pages of memory it is
// in, and the address forgets it when the details close.
it('opens the transaction a link names, however far down the list it is, and forgets the address when it closes', async () => {
  window.matchMedia = ((query: string) => ({
    matches: true,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  })) as unknown as typeof window.matchMedia
  const shown = charge('charge-shown', 'Shown Merchant')
  const named = charge('charge-named', 'Named Merchant')
  execute.mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('FinanceAccounts')) return { FinanceAccounts: [account] }
    if (document.includes('SpendingCategories')) return { SpendingCategories: [] }
    if (document.includes('ReadRecordMemoryLinks')) return { ReadRecordMemoryLinks: [] }
    if (document.includes('FinanceReceipts'))
      return { FinanceReceipts: { financeReceipts: [], totalCount: 0, nextCursor: '' } }
    const ids = variables?.financeTransactionIds as string[] | undefined
    if (ids) {
      return {
        FinanceTransactions: {
          financeTransactions: ids.includes('charge-named') ? [named] : [],
          totalCount: 1,
          leftOutDuplicateCount: 0,
        },
      }
    }
    return { FinanceTransactions: { financeTransactions: [shown], totalCount: 400, leftOutDuplicateCount: 0 } }
  })
  render(
    <MemoryRouter initialEntries={['/finance/transactions?transaction=charge-named&rows=25&page=9']}>
      <FinanceTransactionsSection />
      <SearchProbe />
    </MemoryRouter>,
  )
  const dialog = await screen.findByRole('alertdialog', { name: 'Named Merchant' })
  await waitFor(() =>
    expect(
      execute.mock.calls.some(
        ([document, variables]) =>
          document.includes('ReadRecordMemoryLinks') &&
          (variables as { recordKind: string; recordId: string }).recordKind === 'finance_transaction' &&
          (variables as { recordId: string }).recordId === 'charge-named',
      ),
    ).toBe(true),
  )
  expect(shownSearch).toContain('transaction=charge-named')
  fireEvent.click(within(dialog).getAllByRole('button', { name: 'common.close' })[0])
  await waitFor(() => expect(shownSearch).not.toContain('transaction='))
  expect(shownSearch).toContain('page=9')
})
