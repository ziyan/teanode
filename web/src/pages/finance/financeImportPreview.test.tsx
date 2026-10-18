import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { TransactionRowsPreview } from './financeEntryApi'
import { TransactionRowsDialog } from './financeImportPreview'

const toast = vi.hoisted(() => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }))
vi.mock('../../api', () => ({ graphql: vi.fn(), askAgentAbout: vi.fn() }))
vi.mock('../../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) => (values ? `${key} ${JSON.stringify(values)}` : key),
    plural: (count: number, forms: { one: string; other: string }, values?: Record<string, string>) =>
      `${count === 1 ? forms.one : forms.other}${values ? ` ${JSON.stringify(values)}` : ''}`,
  }),
}))
vi.mock('../../components/toast', () => ({ useToast: () => toast }))
const execute = vi.mocked(graphql)

afterEach(() => {
  cleanup()
  execute.mockReset()
  toast.done.mockReset()
  toast.failure.mockReset()
})

const preview: TransactionRowsPreview = {
  financeAccountId: null,
  accountName: 'Sample Bank Checking',
  isNewAccount: true,
  accountMatch: 'new_account',
  currencyCode: 'USD',
  newTransactionRows: [
    {
      rowNumber: 1,
      postedOn: '2026-04-02',
      description: 'Corner Market',
      amount: '-12.5000',
      hasNearbyStoredTransaction: false,
    },
    {
      rowNumber: 2,
      postedOn: '2026-04-03',
      description: 'Paycheck',
      amount: '900.0000',
      hasNearbyStoredTransaction: true,
    },
  ],
  presentTransactionRows: [
    {
      rowNumber: 3,
      postedOn: '2026-04-01',
      description: 'Rent',
      amount: '-500.0000',
      hasNearbyStoredTransaction: false,
    },
  ],
  firstPostedOn: '2026-04-02',
  lastPostedOn: '2026-04-03',
  moneyInAmount: '900.0000',
  moneyOutAmount: '-12.5000',
  verificationSummary: 'No running balances to check.',
}

function answers() {
  execute.mockImplementation((document: string) => {
    if (document.includes('ReportingCurrency')) {
      return Promise.resolve({ ReportingCurrency: { reportingCurrencyCode: 'USD', isChosen: true } })
    }
    if (document.includes('FinanceAccounts')) {
      return Promise.resolve({
        FinanceAccounts: [
          {
            id: 'account-statement',
            providerKind: 'statement',
            institutionName: 'Sample Bank',
            accountName: 'Sample Bank Savings',
            accountMask: '9876',
            accountKind: 'depository',
            currencyCode: 'EUR',
          },
          {
            id: 'account-linked',
            providerKind: 'plaid',
            accountName: 'Linked Checking',
            accountKind: 'depository',
            currencyCode: 'USD',
          },
        ],
      })
    }
    if (document.includes('ExchangeRate')) {
      return Promise.resolve({
        ExchangeRate: {
          fromCurrencyCode: 'EUR',
          toCurrencyCode: 'USD',
          rate: '1.08',
          rateOn: '2026-05-14',
          rateSource: 'ecb',
        },
      })
    }
    if (document.includes('PreviewImportTransactions')) return Promise.resolve({ PreviewImportTransactions: preview })
    if (document.includes('ImportTransactions')) {
      return Promise.resolve({
        ImportTransactions: {
          importedAt: '2026-05-14T12:00:00Z',
          statementImportOrigin: 'transaction_rows',
          statementFileNames: [],
          addedTransactionCount: 2,
          updatedTransactionCount: 0,
          unchangedTransactionCount: 1,
          skippedTransactionCount: 0,
          transactionWithoutFitIdCount: 0,
          financeAccountIds: ['account-new'],
          financeAccountNames: ['Sample Bank Checking'],
        },
      })
    }
    return Promise.reject(new Error(`unexpected ${document}`))
  })
}

function renderDialog() {
  const onImported = vi.fn()
  const onClose = vi.fn()
  render(
    <MemoryRouter>
      <TransactionRowsDialog onImported={onImported} onClose={onClose} />
    </MemoryRouter>,
  )
  return { onImported, onClose }
}

async function fillRows() {
  await waitFor(() => expect(screen.getByRole('combobox', { name: 'finance.currency' }).textContent).toContain('USD'))
  fireEvent.change(screen.getByLabelText('finance.institution'), { target: { value: 'Sample Bank' } })
  fireEvent.change(screen.getByLabelText('finance.entryAccountName'), { target: { value: 'Sample Bank Checking' } })
  fireEvent.change(screen.getByLabelText('finance.entryAccountNumber'), { target: { value: '4455' } })
  fireEvent.click(screen.getByLabelText('finance.entryIsAccountNumberPartial'))
  const first = screen.getByRole('group', { name: 'finance.entryRowNumber {"number":1}' })
  fireEvent.change(within(first).getByLabelText('finance.postedOn'), { target: { value: '2026-04-02' } })
  fireEvent.change(within(first).getByLabelText('finance.entryRowDescription'), { target: { value: 'Corner Market' } })
  fireEvent.change(within(first).getByLabelText('finance.amount'), { target: { value: '-12.50' } })
  fireEvent.click(screen.getByRole('button', { name: 'finance.entryAddRow' }))
  const second = screen.getByRole('group', { name: 'finance.entryRowNumber {"number":2}' })
  fireEvent.change(within(second).getByLabelText('finance.postedOn'), { target: { value: '2026-04-03' } })
  fireEvent.change(within(second).getByLabelText('finance.entryRowDescription'), { target: { value: 'Paycheck' } })
  fireEvent.change(within(second).getByLabelText('finance.amount'), { target: { value: '900.00' } })
}

const expectedVariables = {
  isNewAccount: true,
  institutionName: 'Sample Bank',
  accountName: 'Sample Bank Checking',
  accountNumber: '4455',
  isAccountNumberPartial: true,
  statementAccountKind: 'bank',
  currencyCode: 'USD',
  transactionRows: [
    { postedOn: '2026-04-02', description: 'Corner Market', amount: '-12.50' },
    { postedOn: '2026-04-03', description: 'Paycheck', amount: '900.00' },
  ],
}

describe('TransactionRowsDialog', () => {
  it('previews the rows the server would add, then imports with the same arguments', async () => {
    answers()
    const { onImported, onClose } = renderDialog()
    await fillRows()
    fireEvent.click(screen.getByRole('button', { name: 'finance.entryPreview' }))

    const region = await screen.findByRole('region', { name: 'finance.entryPreviewTitle' })
    // The count, the account, and every parsed row with its day and amount.
    expect(within(region).getByText(/finance.entryRowsNewOther \{"count":"2"\}/)).toBeTruthy()
    expect(within(region).getByText(/finance.entryRowsPresentOne/)).toBeTruthy()
    expect(within(region).getByText(/finance.entryRowsNewAccount/)).toBeTruthy()
    expect(within(region).getByText('Corner Market')).toBeTruthy()
    expect(within(region).getByText(/Paycheck/)).toBeTruthy()
    expect(within(region).getByText(/finance.entryRowHasNearbyTransaction/)).toBeTruthy()
    expect(within(region).getByText('Rent')).toBeTruthy()
    expect(within(region).getByText('No running balances to check.')).toBeTruthy()
    const previewCall = execute.mock.calls.find(([document]) => document.includes('PreviewImportTransactions'))
    expect(previewCall?.[1]).toEqual(expect.objectContaining(expectedVariables))
    expect(execute.mock.calls.some(([document]) => /mutation[^{]*\{\s*ImportTransactions/.test(document))).toBe(false)

    fireEvent.click(screen.getByRole('button', { name: 'finance.entryImportRows' }))
    await waitFor(() => expect(onImported).toHaveBeenCalled())
    const importCall = execute.mock.calls.find(([document]) => /mutation[^{]*\{\s*ImportTransactions/.test(document))
    expect(importCall?.[1]).toEqual(expect.objectContaining(expectedVariables))
    expect(toast.done).toHaveBeenCalled()
    expect(onClose).toHaveBeenCalled()
  })

  it('asks for a new preview when a row is changed after one', async () => {
    answers()
    renderDialog()
    await fillRows()
    fireEvent.click(screen.getByRole('button', { name: 'finance.entryPreview' }))
    await screen.findByRole('button', { name: 'finance.entryImportRows' })
    const second = screen.getByRole('group', { name: 'finance.entryRowNumber {"number":2}' })
    fireEvent.change(within(second).getByLabelText('finance.amount'), { target: { value: '950.00' } })
    expect(screen.getByRole('button', { name: 'finance.entryPreview' })).toBeTruthy()
  })

  it('imports into an account chosen from the ones imported before, and shows the rate of its currency', async () => {
    answers()
    renderDialog()
    await waitFor(() => expect(screen.getByRole('combobox', { name: 'finance.currency' }).textContent).toContain('USD'))
    fireEvent.click(screen.getByRole('combobox', { name: 'finance.account' }))
    // The linked account is not one rows can go into.
    expect(screen.queryByRole('option', { name: /Linked Checking/ })).toBeNull()
    fireEvent.click(await screen.findByRole('option', { name: /Sample Bank Savings/ }))
    expect((screen.getByLabelText('finance.institution') as HTMLInputElement).value).toBe('Sample Bank')
    expect(await screen.findByText(/finance.entryExchangeRate/)).toBeTruthy()

    const first = screen.getByRole('group', { name: 'finance.entryRowNumber {"number":1}' })
    fireEvent.change(within(first).getByLabelText('finance.postedOn'), { target: { value: '2026-04-02' } })
    fireEvent.change(within(first).getByLabelText('finance.entryRowDescription'), {
      target: { value: 'Corner Market' },
    })
    fireEvent.change(within(first).getByLabelText('finance.amount'), { target: { value: '-12.50' } })
    fireEvent.click(screen.getByRole('button', { name: 'finance.entryPreview' }))
    await screen.findByRole('region', { name: 'finance.entryPreviewTitle' })
    const previewCall = execute.mock.calls.find(([document]) => document.includes('PreviewImportTransactions'))
    expect(previewCall?.[1]).toEqual(
      expect.objectContaining({
        financeAccountId: 'account-statement',
        currencyCode: 'EUR',
        institutionName: 'Sample Bank',
      }),
    )
    expect((previewCall?.[1] as { isNewAccount?: boolean }).isNewAccount).toBeUndefined()
  })

  it('says a refusal in a toast and stays open', async () => {
    answers()
    const { onClose } = renderDialog()
    await fillRows()
    execute.mockImplementation((document: string) =>
      document.includes('ReportingCurrency')
        ? Promise.resolve({ ReportingCurrency: { reportingCurrencyCode: 'USD', isChosen: true } })
        : Promise.reject(new Error('a row is dated in the future')),
    )
    fireEvent.click(screen.getByRole('button', { name: 'finance.entryPreview' }))
    await waitFor(() => expect(toast.failure).toHaveBeenCalled())
    expect(onClose).not.toHaveBeenCalled()
  })

  it('clears the account fields when going back to a new account, and locks them for an existing one', async () => {
    answers()
    renderDialog()
    await waitFor(() => expect(screen.getByRole('combobox', { name: 'finance.currency' }).textContent).toContain('USD'))
    fireEvent.click(screen.getByRole('combobox', { name: 'finance.account' }))
    fireEvent.click(await screen.findByRole('option', { name: /Sample Bank Savings/ }))
    expect((screen.getByLabelText('finance.institution') as HTMLInputElement).disabled).toBe(true)
    expect((screen.getByLabelText('finance.entryAccountName') as HTMLInputElement).disabled).toBe(true)
    expect((screen.getByLabelText('finance.accountKindLabel') as HTMLSelectElement).disabled).toBe(true)

    fireEvent.click(screen.getByRole('combobox', { name: 'finance.account' }))
    fireEvent.click(await screen.findByRole('option', { name: 'finance.entryNewAccount' }))
    const institution = screen.getByLabelText('finance.institution') as HTMLInputElement
    expect(institution.disabled).toBe(false)
    expect(institution.value).toBe('')
    expect((screen.getByLabelText('finance.entryAccountName') as HTMLInputElement).value).toBe('')
    expect((screen.getByLabelText('finance.accountKindLabel') as HTMLSelectElement).value).toBe('bank')
    expect(screen.getByRole('combobox', { name: 'finance.currency' }).textContent).toContain('USD')
  })

  it('shows each row its number and ignores a close while a request is in flight', async () => {
    answers()
    const { onClose } = renderDialog()
    await fillRows()
    const first = screen.getByRole('group', { name: 'finance.entryRowNumber {"number":1}' })
    expect(within(first).getByText('finance.entryRowNumber {"number":1}')).toBeTruthy()
    execute.mockImplementation(() => new Promise(() => {}))
    fireEvent.click(screen.getByRole('button', { name: 'finance.entryPreview' }))
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).not.toHaveBeenCalled()
  })
})
