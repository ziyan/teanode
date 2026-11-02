import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { ToastProvider } from '../../components/toast'
import { TranslationProvider } from '../../i18n/i18n'
import { SessionProvider } from '../../session'
import { FinancePage } from '../financePage'
import { FinanceSharingSection } from './financeSharing'

// Sharing all of one's finance: the panel that starts, changes and ends it,
// and the Showing view of somebody who shares all of theirs, which reads by
// the owner and never shows an account.

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
}))
vi.mock('./financePresence', () => ({
  useAgentFinancePresence: () => ({ isShown: true, hasSources: true, isShared: false }),
}))
vi.mock('./financeSpending', () => ({ FinanceSpendingSection: () => <p>Own spending</p> }))
vi.mock('./financeTransactions', () => ({ FinanceTransactionsSection: () => null }))
vi.mock('./financeReceiptList', () => ({ FinanceReceiptListSection: () => null }))
vi.mock('./financeTaxes', () => ({ FinanceTaxesSection: () => null }))
vi.mock('./financeAccounts', () => ({ FinanceAccountsSection: () => null }))
vi.mock('./financeBudgets', () => ({ FinanceBudgetsSection: () => null }))
vi.mock('./financeNetWorth', () => ({ FinanceNetWorthSection: () => null }))
vi.mock('./financeSavingsTargets', () => ({ FinanceSavingsTargetsSection: () => null }))
vi.mock('./financeCommon', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./financeCommon')>()),
  usePersonZone: () => true,
}))
// The month the page is on is a fixed one, so the rows below are in it.
vi.mock('./financeApi', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./financeApi')>()),
  personMonth: () => '2026-09',
}))

// The chart measures itself, which the test environment cannot.
beforeEach(() => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
      unobserve() {}
    },
  )
})

afterEach(() => {
  cleanup()
  vi.resetAllMocks()
  vi.unstubAllGlobals()
})

function calls(operation: string) {
  return vi.mocked(graphql).mock.calls.filter(([document]) => document.includes(operation))
}

function Address() {
  const location = useLocation()
  return <p data-testid="address">{location.pathname + location.search}</p>
}

function renderAt(element: React.ReactNode, entry: string) {
  return render(
    <TranslationProvider>
      <ToastProvider>
        <SessionProvider
          value={{
            userId: 'person-a',
            authenticated: true,
            authenticationRequired: true,
            username: 'person-a-login',
            passkeysEnabled: false,
            permissions: { everywhere: ['agent:use', 'circle:use'], byDomain: [] },
          }}
        >
          <MemoryRouter initialEntries={[entry]}>
            <Routes>
              <Route path="/finance/:section" element={element} />
              <Route path="/settings/agent/finance" element={element} />
            </Routes>
            <Address />
          </MemoryRouter>
        </SessionProvider>
      </ToastProvider>
    </TranslationProvider>,
  )
}

const ALL_SHARE = {
  id: 'share-all-one',
  granteeUserId: 'person-tobias',
  granteeUsername: 'tobias',
  granteeName: 'Tobias',
  granteeCircleId: null,
  granteeCircleName: '',
  financeShareLevel: 'spending',
  createdAt: '2026-09-01T00:00:00Z',
}

function answerPanel(shares: unknown[]) {
  vi.mocked(graphql).mockImplementation(async (document) => {
    if (document.includes('ListFinanceAccountShares')) return { ListFinanceAccountShares: [] } as never
    if (document.includes('ListFinanceShares')) return { ListFinanceShares: shares } as never
    if (document.includes('FinanceAccounts')) {
      return { FinanceAccounts: [{ id: 'account-one', accountName: 'Example Checking', accountMask: '0001' }] } as never
    }
    if (document.includes('ListCirclePeople')) {
      return {
        ListCirclePeople: [
          { userId: 'person-tobias', username: 'tobias', name: 'Tobias' },
          { userId: 'person-mira', username: 'mira', name: 'Mira' },
        ],
      } as never
    }
    if (document.includes('ListCircles')) {
      return {
        ListCircles: [{ id: 'circle-one', name: 'Fixture Family', members: [{ userId: 'person-mira' }] }],
      } as never
    }
    if (document.includes('UnshareFinance(')) return { UnshareFinance: true } as never
    if (document.includes('ShareFinance(')) return { ShareFinance: ALL_SHARE } as never
    return {} as never
  })
}

async function openShareAll() {
  fireEvent.click(await screen.findByRole('button', { name: 'Share all finance' }))
  return screen.findByRole('dialog')
}

it('shares all finance with a person at the chosen level', async () => {
  answerPanel([])
  renderAt(<FinanceSharingSection />, '/settings/agent/finance')
  expect(await screen.findByText('You do not share all your finance with anybody.')).toBeTruthy()

  const dialog = await openShareAll()
  // Each level says what it shows in one sentence.
  expect(within(dialog).getByText(/No account names or balances\./)).toBeTruthy()
  expect(within(dialog).getByText("And each transaction's day, amount, merchant and category.")).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('combobox', { name: 'Share with' }))
  fireEvent.click(await screen.findByRole('option', { name: /Tobias/ }))
  fireEvent.click(within(dialog).getByRole('radio', { name: /Transactions/ }))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Share' }))

  await waitFor(() => expect(calls('ShareFinance(').length).toBeGreaterThan(0))
  expect(calls('ShareFinance(')[0][1]).toEqual({
    granteeUserId: 'person-tobias',
    granteeCircleId: null,
    financeShareLevel: 'transactions',
  })
  // It is not a share of one account.
  expect(calls('ShareFinanceAccount')).toHaveLength(0)
})

it('shares all finance with a circle, starting at the spending level', async () => {
  answerPanel([])
  renderAt(<FinanceSharingSection />, '/settings/agent/finance')
  const dialog = await openShareAll()
  fireEvent.click(within(dialog).getByRole('combobox', { name: 'Share with' }))
  fireEvent.click(await screen.findByRole('option', { name: /Everyone in Fixture Family/ }))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Share' }))
  await waitFor(() => expect(calls('ShareFinance(').length).toBeGreaterThan(0))
  expect(calls('ShareFinance(')[0][1]).toEqual({
    granteeUserId: null,
    granteeCircleId: 'circle-one',
    financeShareLevel: 'spending',
  })
})

it('lists the shares of all finance, changes a level and stops one after asking', async () => {
  answerPanel([ALL_SHARE])
  renderAt(<FinanceSharingSection />, '/settings/agent/finance')
  expect(await screen.findByText('All accounts, now and later')).toBeTruthy()

  // Changing keeps who it is with and sends the new level.
  fireEvent.click(screen.getByRole('button', { name: 'Change what Tobias sees of all your finance' }))
  const dialog = await screen.findByRole('dialog')
  fireEvent.click(within(dialog).getByRole('radio', { name: /Transactions/ }))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(calls('ShareFinance(').length).toBeGreaterThan(0))
  expect(calls('ShareFinance(')[0][1]).toEqual({
    granteeUserId: 'person-tobias',
    granteeCircleId: null,
    financeShareLevel: 'transactions',
  })

  // Stopping asks first, and nothing is sent until it is confirmed.
  fireEvent.click(screen.getByRole('button', { name: 'Stop sharing all your finance with Tobias' }))
  const confirmation = await screen.findByRole('alertdialog')
  expect(within(confirmation).getByText('Stop sharing with Tobias?')).toBeTruthy()
  expect(calls('UnshareFinance(')).toHaveLength(0)
  fireEvent.click(within(confirmation).getByRole('button', { name: 'Stop sharing' }))
  await waitFor(() => expect(calls('UnshareFinance(')).toHaveLength(1))
  expect(calls('UnshareFinance(')[0][1]).toEqual({ financeShareId: 'share-all-one' })
  expect(calls('UnshareFinanceAccount')).toHaveLength(0)
})

const SPENDING = [
  {
    spendingMonth: '2026-09',
    spendingCategoryName: 'Groceries',
    currencyCode: 'USD',
    moneyOut: '120.5000',
    moneyIn: '0.0000',
    financeTransactionCount: 3,
  },
  {
    spendingMonth: '2026-09',
    spendingCategoryName: 'Travel',
    currencyCode: 'USD',
    moneyOut: '80.0000',
    moneyIn: '20.0000',
    financeTransactionCount: 2,
  },
  {
    spendingMonth: '2026-08',
    spendingCategoryName: 'Dining',
    currencyCode: 'USD',
    moneyOut: '33.2500',
    moneyIn: '0.0000',
    financeTransactionCount: 1,
  },
]
const TRANSACTIONS = {
  nextCursor: '',
  totalCount: 1,
  financeTransactions: [
    {
      id: 'transaction-one',
      postedOn: '2026-09-10',
      amount: '-42.1700',
      currencyCode: 'USD',
      merchantName: 'Example Grocer',
      spendingCategoryName: 'Groceries',
    },
  ],
}

function answerShowing(level: 'spending' | 'transactions', accounts: unknown[] = []) {
  vi.mocked(graphql).mockImplementation(async (document) => {
    if (document.includes('ListSharedFinanceAccounts')) return { ListSharedFinanceAccounts: accounts } as never
    if (document.includes('ListSharedFinanceSpending')) return { ListSharedFinanceSpending: SPENDING } as never
    if (document.includes('ListSharedFinanceTransactions')) {
      return { ListSharedFinanceTransactions: TRANSACTIONS } as never
    }
    if (document.includes('ListSharedFinance(')) {
      return {
        ListSharedFinance: [
          { ownerUserId: 'person-mira', ownerUsername: 'mira', ownerName: 'Mira', financeShareLevel: level },
        ],
      } as never
    }
    return {} as never
  })
}

it('shows one combined spending page for somebody who shares all their finance, read by the owner', async () => {
  answerShowing('spending')
  renderAt(<FinancePage />, '/finance/spending?person=person-mira')
  expect(await screen.findByText("Mira's finance")).toBeTruthy()
  // The chosen month's categories, from the combined rows.
  expect(await screen.findAllByText('Groceries')).not.toHaveLength(0)
  expect(screen.getAllByText('Travel').length).toBeGreaterThan(0)
  expect(screen.queryByText('Dining')).toBeNull()
  expect(screen.getByRole('combobox', { name: 'Showing' })).toBeTruthy()
  expect(screen.queryByText('Own spending')).toBeNull()

  // Asked by the owner, never by an account, and once.
  const reads = calls('ListSharedFinanceSpending')
  expect(reads).toHaveLength(1)
  expect(reads[0][1]).toEqual({ ownerUserId: 'person-mira' })
  expect(reads[0][0]).not.toContain('financeAccountId')
  expect(calls('ListSharedFinanceTransactions')).toHaveLength(0)
  // No account name, mask or balance anywhere, and no account columns.
  expect(screen.queryByText(/Checking/)).toBeNull()
  expect(screen.queryByText('Current balance')).toBeNull()
  // The spending level has no Transactions tab.
  expect(screen.queryByRole('button', { name: 'Transactions' })).toBeNull()
})

it('steps back a month on the combined spending page', async () => {
  answerShowing('spending')
  renderAt(<FinancePage />, '/finance/spending?person=person-mira')
  expect(await screen.findAllByText('Groceries')).not.toHaveLength(0)
  fireEvent.click(screen.getByRole('button', { name: 'Previous month' }))
  expect(await screen.findAllByText('Dining')).not.toHaveLength(0)
  expect(screen.queryByText('Travel')).toBeNull()
  expect(screen.getByTestId('address').textContent).toContain('month=2026-08')
  // Still one read: every month came in the first answer.
  expect(calls('ListSharedFinanceSpending')).toHaveLength(1)
})

it('adds a Transactions tab at the transactions level, read by the owner', async () => {
  answerShowing('transactions')
  renderAt(<FinancePage />, '/finance/spending?person=person-mira')
  expect(await screen.findAllByText('Groceries')).not.toHaveLength(0)
  fireEvent.click(screen.getByRole('button', { name: 'Transactions' }))
  expect(await screen.findByText('Example Grocer')).toBeTruthy()
  await waitFor(() => expect(calls('ListSharedFinanceTransactions').length).toBeGreaterThan(0))
  expect(calls('ListSharedFinanceTransactions')[0][1]).toMatchObject({ ownerUserId: 'person-mira', offset: 0 })
  expect(calls('ListSharedFinanceTransactions')[0][0]).not.toContain('financeAccountId')
  // The person stays chosen in the address.
  expect(screen.getByTestId('address').textContent).toBe('/finance/transactions?person=person-mira')
  expect(screen.queryByText(/Checking/)).toBeNull()
})

it('keeps accounts shared one by one in an Accounts tab beside the combined view', async () => {
  answerShowing('spending', [
    {
      financeAccountId: 'account-one',
      ownerUserId: 'person-mira',
      ownerUsername: 'mira',
      ownerName: 'Mira',
      financeShareLevel: 'balance',
      accountName: 'Example Checking',
      accountMask: '0001',
      accountKind: 'depository',
      currencyCode: 'USD',
      currentBalance: '1200.5000',
      balanceAt: null,
      creditUsage: null,
    },
  ])
  renderAt(<FinancePage />, '/finance/spending?person=person-mira')
  expect(await screen.findAllByText('Groceries')).not.toHaveLength(0)
  expect(screen.queryByText('Example Checking ··0001')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Accounts' }))
  expect(await screen.findByText('Example Checking ··0001')).toBeTruthy()
})

it('offers a person who shares only all their finance under Showing', async () => {
  answerShowing('spending')
  renderAt(<FinancePage />, '/finance/spending')
  expect(await screen.findByText('Own spending')).toBeTruthy()
  fireEvent.click(await screen.findByRole('combobox', { name: 'Showing' }))
  expect(screen.getByRole('option', { name: 'Mira' })).toBeTruthy()
})
