import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { FinanceAccount } from './financeApi'
import { FinanceAccountsSection } from './financeAccounts'

vi.mock('../../api', () => ({ graphql: vi.fn() }))
vi.mock('../../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) => (values ? `${key} ${JSON.stringify(values)}` : key),
    plural: (count: number, forms: { one: string; other: string }, values?: Record<string, string>) =>
      `${count === 1 ? forms.one : forms.other} ${JSON.stringify(values)}`,
  }),
}))
const toast = vi.hoisted(() => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }))
vi.mock('../../components/toast', () => ({ useToast: () => toast }))
// The panels above and below the table read their own data; they are not
// what is tested here.
vi.mock('./financeCreditUsage', () => ({ FinanceCreditUsageSection: () => null }))
vi.mock('./financeStatementImport', () => ({ FinanceStatementImportSection: () => null }))
const execute = vi.mocked(graphql)
afterEach(() => {
  cleanup()
  execute.mockReset()
  toast.done.mockReset()
  toast.failure.mockReset()
})

const account = (id: string, accountName: string, providerKind: FinanceAccount['providerKind']): FinanceAccount => ({
  id,
  sourceId: `source-${providerKind}`,
  providerKind,
  accountName,
  accountMask: '4567',
  accountKind: 'depository',
  currencyCode: 'JPY',
  isSignInRequired: false,
})

const accounts = [
  account('account-linked', 'Example Checking', 'plaid'),
  account('account-imported', 'Example Bank', 'statement'),
]

function answer(document: string, variables?: Record<string, unknown>) {
  if (document.includes('RenameStatementAccount')) {
    return Promise.resolve({
      RenameStatementAccount: { id: variables?.financeAccountId, accountName: variables?.accountName },
    })
  }
  if (document.includes('DeleteStatementAccount')) {
    return Promise.resolve({
      DeleteStatementAccount: {
        financeAccountId: variables?.financeAccountId,
        deletedTransactionCount: 12,
        deletedAssetCount: 1,
      },
    })
  }
  return Promise.resolve({ FinanceAccounts: accounts })
}

// Only an account from statements has the pencil and the trash: a
// provider's account would come back as it was with its next sync.
it('offers rename and delete on accounts from statements only', async () => {
  execute.mockImplementation(
    (document: string, variables?: Record<string, unknown>) => answer(document, variables) as never,
  )
  render(<FinanceAccountsSection />)
  expect(await screen.findByLabelText('Example Bank ··4567: common.rename')).toBeTruthy()
  expect(screen.getByLabelText('Example Bank ··4567: common.delete')).toBeTruthy()
  expect(screen.queryByLabelText('Example Checking ··4567: common.rename')).toBeNull()
  expect(screen.queryByLabelText('Example Checking ··4567: common.delete')).toBeNull()
})

// The pencil opens a dialog with the name in it; saving sends the new
// name, says so in a toast and reads the accounts again.
it('renames an account from statements', async () => {
  execute.mockImplementation(
    (document: string, variables?: Record<string, unknown>) => answer(document, variables) as never,
  )
  render(<FinanceAccountsSection />)
  fireEvent.click(await screen.findByLabelText('Example Bank ··4567: common.rename'))
  const field = screen.getByDisplayValue('Example Bank')
  fireEvent.change(field, { target: { value: 'Rainy day fund' } })
  fireEvent.click(screen.getByText('common.save'))
  await waitFor(() =>
    expect(execute).toHaveBeenCalledWith(expect.stringContaining('RenameStatementAccount'), {
      financeAccountId: 'account-imported',
      accountName: 'Rainy day fund',
    }),
  )
  await waitFor(() => expect(toast.done).toHaveBeenCalledWith('finance.accountRenamed {"name":"Rainy day fund"}'))
  expect(execute.mock.calls.filter(([document]) => String(document).includes('FinanceAccounts')).length).toBe(2)
})

// The trash asks first, saying what is lost; confirming deletes the
// account and the toast says how many transactions went with it.
it('deletes an account from statements after asking', async () => {
  execute.mockImplementation(
    (document: string, variables?: Record<string, unknown>) => answer(document, variables) as never,
  )
  render(<FinanceAccountsSection />)
  fireEvent.click(await screen.findByLabelText('Example Bank ··4567: common.delete'))
  expect(screen.getByText('finance.deleteAccountBody')).toBeTruthy()
  expect(execute.mock.calls.some(([document]) => String(document).includes('DeleteStatementAccount'))).toBe(false)
  fireEvent.click(screen.getByText('finance.deleteAccountConfirm'))
  await waitFor(() =>
    expect(execute).toHaveBeenCalledWith(expect.stringContaining('DeleteStatementAccount'), {
      financeAccountId: 'account-imported',
    }),
  )
  await waitFor(() =>
    expect(toast.done).toHaveBeenCalledWith('finance.accountDeletedOther {"name":"Example Bank ··4567","count":"12"}'),
  )
})
