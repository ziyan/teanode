import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { ToastProvider } from '../../components/toast'
import { TranslationProvider } from '../../i18n/i18n'
import { SessionProvider } from '../../session'
import { FinancePage } from '../financePage'
import { FinanceShared } from './financeShared'
import { FinanceSharingSection } from './financeSharing'
import { SharedFinanceAccount } from './financeShareApi'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
}))
// What the Finance page draws for the person's own finance is not what is
// tested here.
const presence = vi.hoisted(() => ({ isShown: true }))
vi.mock('./financePresence', () => ({
  useAgentFinancePresence: () => ({ isShown: presence.isShown, hasSources: presence.isShown, isShared: false }),
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

afterEach(() => {
  cleanup()
  vi.resetAllMocks()
  presence.isShown = true
})

// The address, for a test that asks whether the page kept it.
function Address() {
  return <p data-testid="address">{useLocation().search}</p>
}

const sharedAccount = (overrides: Partial<SharedFinanceAccount>): SharedFinanceAccount => ({
  financeAccountId: 'account-one',
  ownerUserId: 'person-b',
  ownerUsername: 'person-b-login',
  ownerName: 'Person B',
  financeShareLevel: 'balance',
  accountName: 'Example Checking',
  accountMask: '0001',
  accountKind: 'depository',
  currencyCode: 'USD',
  currentBalance: '1200.5000',
  balanceAt: null,
  creditUsage: null,
  ...overrides,
})

const SPENDING = [
  {
    spendingMonth: '2026-09',
    spendingCategoryName: 'Groceries',
    currencyCode: 'USD',
    moneyOut: '42.1700',
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

function calls(operation: string) {
  return vi.mocked(graphql).mock.calls.filter(([document]) => document.includes(operation))
}

function renderWithin(
  element: React.ReactNode,
  entry = '/finance/spending',
  permissions = ['agent:use', 'circle:use'],
) {
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
            permissions: { everywhere: permissions, byDomain: [] },
          }}
        >
          <MemoryRouter initialEntries={[entry]}>
            <Routes>
              <Route path="/finance/:section" element={element} />
              <Route path="/settings/agent/finance" element={element} />
            </Routes>
          </MemoryRouter>
        </SessionProvider>
      </ToastProvider>
    </TranslationProvider>,
  )
}

function answerWith(shared: SharedFinanceAccount[]) {
  vi.mocked(graphql).mockImplementation(async (document) => {
    if (document.includes('ListSharedFinanceAccounts')) return { ListSharedFinanceAccounts: shared } as never
    if (document.includes('ListSharedFinanceSpending')) return { ListSharedFinanceSpending: SPENDING } as never
    if (document.includes('ListSharedFinanceTransactions'))
      return { ListSharedFinanceTransactions: TRANSACTIONS } as never
    return {} as never
  })
}

// An account shared at the balance level shows its balance and nothing more
// is asked for: no months, no transactions.
it('shows only the balance of an account shared at the balance level', async () => {
  answerWith([])
  renderWithin(<FinanceShared ownerName="Person B" accounts={[sharedAccount({})]} />)
  expect(await screen.findByText('Example Checking ··0001')).toBeTruthy()
  expect(screen.getByText('$1,200.50')).toBeTruthy()
  // The column and the level are both called Balance.
  expect(screen.getAllByText('Balance')).toHaveLength(2)
  expect(calls('ListSharedFinanceSpending')).toHaveLength(0)
  expect(calls('ListSharedFinanceTransactions')).toHaveLength(0)
})

it('adds the months at the spending level, and the transactions at the transactions level', async () => {
  answerWith([])
  const { unmount } = renderWithin(
    <FinanceShared ownerName="Person B" accounts={[sharedAccount({ financeShareLevel: 'spending' })]} />,
  )
  expect(await screen.findByText('Groceries')).toBeTruthy()
  expect(screen.getByText('$42.17')).toBeTruthy()
  expect(calls('ListSharedFinanceSpending')).toHaveLength(1)
  expect(calls('ListSharedFinanceTransactions')).toHaveLength(0)
  unmount()

  vi.mocked(graphql).mockClear()
  renderWithin(<FinanceShared ownerName="Person B" accounts={[sharedAccount({ financeShareLevel: 'transactions' })]} />)
  expect(await screen.findByText('Example Grocer')).toBeTruthy()
  expect(calls('ListSharedFinanceSpending')).toHaveLength(1)
  await waitFor(() => expect(calls('ListSharedFinanceTransactions').length).toBeGreaterThan(0))
  // A page of the account chosen, by its number.
  expect(calls('ListSharedFinanceTransactions')[0][1]).toMatchObject({ financeAccountId: 'account-one', offset: 0 })
})

it('says what a credit card owes against its limit', async () => {
  answerWith([])
  renderWithin(
    <FinanceShared
      ownerName="Person B"
      accounts={[
        sharedAccount({
          accountName: 'Example Card',
          accountKind: 'credit',
          creditUsage: {
            owedAmount: '100.0000',
            creditLimitAmount: '400.0000',
            creditLimitSource: 'provider',
            usageShare: 0.25,
          },
        }),
      ]}
    />,
  )
  expect(await screen.findByText('25%')).toBeTruthy()
})

// The page offers Showing only when somebody shares, shows the chosen
// person's accounts instead of the person's own sections, and takes a link to
// somebody who does not share out of the address with a word.
it('offers Showing when somebody shares and shows their accounts in place of the tabs', async () => {
  answerWith([sharedAccount({})])
  renderWithin(<FinancePage />, '/finance/spending?person=person-b')
  expect(await screen.findByText('Example Checking ··0001')).toBeTruthy()
  expect(screen.getByRole('combobox', { name: 'Showing' })).toBeTruthy()
  expect(screen.queryByText('Own spending')).toBeNull()
  expect(calls('ListSharedFinanceAccounts')).toHaveLength(1)
})

it('shows the person their own sections when nobody shares', async () => {
  answerWith([])
  renderWithin(<FinancePage />, '/finance/spending')
  expect(await screen.findByText('Own spending')).toBeTruthy()
  expect(screen.queryByRole('combobox', { name: 'Showing' })).toBeNull()
})

it('drops a link to somebody who does not share, and shows the person their own', async () => {
  answerWith([])
  renderWithin(<FinancePage />, '/finance/spending?person=person-z')
  expect(await screen.findByText('Own spending')).toBeTruthy()
  expect(await screen.findByText('That person does not share a finance account with you.')).toBeTruthy()
})

// Somebody with no finance of their own still has the page when others share
// with them: the Showing choice, with no "you" in it, and the first person's
// accounts.
it('shows the accounts others share to a person who has no finance of their own', async () => {
  presence.isShown = false
  answerWith([sharedAccount({})])
  renderWithin(<FinancePage />, '/finance/spending')
  expect(await screen.findByText('Example Checking ··0001')).toBeTruthy()
  expect(screen.queryByRole('link', { name: 'Open your agent page' })).toBeNull()
  expect(screen.queryByText('Own spending')).toBeNull()
  fireEvent.click(screen.getByRole('combobox', { name: 'Showing' }))
  expect(screen.queryByRole('option', { name: 'You' })).toBeNull()
  expect(screen.getByRole('option', { name: 'Person B' })).toBeTruthy()
})

it('says there is nothing to show when the person has no finance and nobody shares', async () => {
  presence.isShown = false
  answerWith([])
  renderWithin(<FinancePage />, '/finance/spending')
  expect(await screen.findByRole('link', { name: 'Open your agent page' })).toBeTruthy()
  expect(screen.queryByRole('combobox', { name: 'Showing' })).toBeNull()
})

// A read of the shares that failed does not say the person stopped sharing:
// the address is kept, and the failure is reported.
it('keeps the address and reports the failure when the shares cannot be read', async () => {
  vi.mocked(graphql).mockImplementation(async (document) => {
    if (document.includes('ListSharedFinanceAccounts')) throw new Error('The server is unreachable.')
    return {} as never
  })
  renderWithin(
    <>
      <FinancePage />
      <Address />
    </>,
    '/finance/spending?person=person-b',
  )
  expect(await screen.findByText('Own spending')).toBeTruthy()
  expect(await screen.findByText('The server is unreachable.')).toBeTruthy()
  expect(screen.queryByText('That person does not share a finance account with you.')).toBeNull()
  expect(screen.getByTestId('address').textContent).toBe('?person=person-b')
})

// The Sharing panel lists the shares, ends one, and shares an account with
// someone at a level.
it('lists shares, ends one and shares an account at a level', async () => {
  vi.mocked(graphql).mockImplementation(async (document) => {
    if (document.includes('ListFinanceAccountShares')) {
      return {
        ListFinanceAccountShares: [
          {
            id: 'share-one',
            financeAccountId: 'account-one',
            accountName: 'Example Checking',
            accountMask: '0001',
            granteeUserId: 'person-b',
            granteeUsername: 'person-b-login',
            granteeName: 'Person B',
            granteeCircleId: null,
            granteeCircleName: '',
            financeShareLevel: 'spending',
          },
        ],
      } as never
    }
    if (document.includes('FinanceAccounts')) {
      return {
        FinanceAccounts: [
          { id: 'account-one', accountName: 'Example Checking', accountMask: '0001' },
          { id: 'account-two', accountName: 'Example Card', accountMask: '' },
        ],
      } as never
    }
    if (document.includes('ListCirclePeople')) {
      return { ListCirclePeople: [{ userId: 'person-b', username: 'person-b-login', name: 'Person B' }] } as never
    }
    if (document.includes('ListCircles')) {
      return { ListCircles: [{ id: 'circle-one', name: 'Fixture Family', members: [{ userId: 'person-b' }] }] } as never
    }
    if (document.includes('UnshareFinanceAccount')) return { UnshareFinanceAccount: true } as never
    if (document.includes('ShareFinanceAccount')) return { ShareFinanceAccount: { id: 'share-two' } } as never
    return {} as never
  })
  renderWithin(<FinanceSharingSection />, '/settings/agent/finance')

  expect(await screen.findByText('Shared with Person B')).toBeTruthy()
  expect(screen.getByText('Spending')).toBeTruthy()

  // Ending a share asks first, and nothing is sent until it is confirmed.
  fireEvent.click(screen.getByRole('button', { name: 'Stop sharing Example Checking ··0001 with Person B' }))
  const confirmation = await screen.findByRole('alertdialog')
  expect(within(confirmation).getByText('Stop sharing with Person B?')).toBeTruthy()
  expect(calls('UnshareFinanceAccount')).toHaveLength(0)
  fireEvent.click(within(confirmation).getByRole('button', { name: 'Stop sharing' }))
  await waitFor(() => expect(calls('UnshareFinanceAccount')).toHaveLength(1))
  expect(calls('UnshareFinanceAccount')[0][1]).toEqual({ shareId: 'share-one' })

  fireEvent.click(screen.getByRole('button', { name: 'Share an account' }))
  const dialog = await screen.findByRole('dialog')
  fireEvent.click(within(dialog).getByRole('combobox', { name: 'Account' }))
  fireEvent.click(await screen.findByRole('option', { name: 'Example Card' }))
  fireEvent.click(within(dialog).getByRole('combobox', { name: 'Share with' }))
  fireEvent.click(await screen.findByRole('option', { name: /Everyone in Fixture Family/ }))
  fireEvent.click(within(dialog).getByRole('combobox', { name: 'How much' }))
  fireEvent.click(await screen.findByRole('option', { name: 'Transactions' }))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Share' }))
  await waitFor(() => expect(calls('ShareFinanceAccount').length).toBeGreaterThan(0))
  expect(calls('ShareFinanceAccount')[0][1]).toEqual({
    financeAccountId: 'account-two',
    granteeUserId: null,
    granteeCircleId: 'circle-one',
    financeShareLevel: 'transactions',
  })
})
