import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { FinanceReceipt, FinanceTransaction, ReceiptMatchCandidate } from './financeApi'
import { FinanceReceiptListSection } from './financeReceiptList'

const toast = vi.hoisted(() => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }))
vi.mock('../../api', () => ({ graphql: vi.fn(), askAgentAbout: vi.fn() }))
vi.mock('../../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) => (values ? `${key} ${JSON.stringify(values)}` : key),
    plural: (count: number, forms: { one: string; other: string }, values?: Record<string, string>) =>
      `${count === 1 ? forms.one : forms.other}${values ? ` ${JSON.stringify(values)}` : ''}`,
    language: 'en',
  }),
}))
vi.mock('../../components/toast', () => ({ useToast: () => toast }))
const execute = vi.mocked(graphql)

afterEach(() => {
  cleanup()
  execute.mockReset()
  toast.done.mockReset()
  toast.failed.mockReset()
  toast.failure.mockReset()
})

// An invented charge at an invented hardware store, and another one.
const hardwareCharge: FinanceTransaction = {
  id: 'transaction-hardware',
  financeAccountId: 'account-invented',
  providerTransactionId: 'provider-hardware',
  postedOn: '2026-06-03',
  amount: '-12.5000',
  currencyCode: 'USD',
  description: 'BIRCH STREET HARDWARE 17',
  merchantName: 'Birch Street Hardware',
  isPending: false,
  receiptCount: 1,
  createdAt: '2026-06-03T12:00:00Z',
  modifiedAt: '2026-06-03T12:00:00Z',
}

const florist: FinanceTransaction = {
  ...hardwareCharge,
  id: 'transaction-florist',
  providerTransactionId: 'provider-florist',
  postedOn: '2026-06-05',
  amount: '-8.2500',
  description: 'PETAL AND STEM 0099',
  merchantName: 'Petal and Stem',
  receiptCount: 0,
}

// An invented receipt matched to the hardware charge, read from a photo.
const hardwareReceipt: FinanceReceipt = {
  id: 'receipt-hardware',
  receiptSourceKind: 'attachment',
  agentAttachmentId: 'attachment-hardware',
  merchantName: 'Birch Street Hardware',
  purchasedOn: '2026-06-02',
  currencyCode: 'USD',
  totalAmount: '12.5000',
  receiptCheckState: 'balanced',
  checkDifferenceAmount: '0',
  isFeeAfterSubtotal: false,
  receiptLines: [
    { id: 'line-h1', lineNumber: 1, receiptLineKind: 'item', description: 'WOOD SCREWS', lineAmount: '12.5000' },
  ],
  receiptMatches: [
    {
      receiptId: 'receipt-hardware',
      financeTransactionId: 'transaction-hardware',
      matchedAmount: '12.5000',
      receiptMatchSource: 'receipt_matcher',
      createdAt: '2026-06-03T12:00:00Z',
    },
  ],
}

// An invented receipt read from a message, matched to nothing, off by a
// quarter.
const floristReceipt: FinanceReceipt = {
  id: 'receipt-florist',
  receiptSourceKind: 'mail',
  mailId: 'mail-florist',
  mailboxItemId: 'item-florist',
  merchantName: 'Petal and Stem',
  purchasedOn: '2026-06-04',
  currencyCode: 'USD',
  totalAmount: '8.2500',
  receiptCheckState: 'unbalanced',
  checkDifferenceAmount: '0.2500',
  isFeeAfterSubtotal: false,
  receiptLines: [
    { id: 'line-f1', lineNumber: 1, receiptLineKind: 'item', description: 'TULIPS', lineAmount: '8.5000' },
  ],
  receiptMatches: [],
}

const floristCandidate: ReceiptMatchCandidate = {
  financeTransactionId: 'transaction-florist',
  financeTransaction: florist,
  matchedAmount: '8.2500',
  isExactAmount: true,
  isSameAccount: false,
  isMerchantNameShared: true,
  dayDistanceCount: 1,
  isAutomatic: false,
}

// answer plays the server: the document says which operation is asked.
// Receipts come a page at a time, as the server pages them: the undated
// ones after every dated one, and only with no days chosen.
function answer(receipts: () => FinanceReceipt[]) {
  execute.mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('FinanceReceipts(')) {
      if (variables?.isUndated && (variables?.from || variables?.to)) {
        throw new Error('isUndated cannot be given with from or to')
      }
      const listed = receipts()
        .filter((receipt) => !variables?.isUnmatched || receipt.receiptMatches.length === 0)
        .filter((receipt) => !variables?.isUndated || !receipt.purchasedOn)
        .filter((receipt) => !(variables?.from || variables?.to) || receipt.purchasedOn)
      const offset = Number(variables?.offset ?? 0)
      const limit = Number(variables?.limit ?? 50)
      const shown = listed.slice(offset, offset + limit)
      return {
        FinanceReceipts: {
          financeReceipts: shown,
          nextCursor: offset + limit < listed.length ? `cursor-${offset + limit}` : '',
          totalCount: listed.length,
        },
      }
    }
    if (document.includes('ProposeReceiptMatches(')) return { ProposeReceiptMatches: [floristCandidate] }
    if (document.includes('MatchReceipt(') || document.includes('UnmatchReceipt(')) return {}
    if (document.includes('DeleteReceipt(')) return { DeleteReceipt: true }
    if (document.includes('FinanceAccounts')) return { FinanceAccounts: [] }
    if (document.includes('SpendingCategories')) return { SpendingCategories: [] }
    if (document.includes('FinanceTransactions(')) {
      const ids = (variables?.financeTransactionIds as string[] | undefined) ?? []
      return {
        FinanceTransactions: {
          financeTransactions: [hardwareCharge, florist].filter((charge) => ids.includes(charge.id)),
          totalCount: 0,
          leftOutDuplicateCount: 0,
        },
      }
    }
    return {}
  })
}

function calledWith(operation: string) {
  return execute.mock.calls.filter(([document]) => document.includes(`${operation}(`)).map(([, variables]) => variables)
}

let shownSearch = ''
function SearchProbe() {
  shownSearch = useLocation().search
  return null
}

function renderSection(address = '/finance/receipts') {
  render(
    <MemoryRouter initialEntries={[address]}>
      <FinanceReceiptListSection />
      <SearchProbe />
    </MemoryRouter>,
  )
}

describe('the Receipts section', () => {
  it('lists receipts with their check, what they explain and their source', async () => {
    answer(() => [floristReceipt, hardwareReceipt])
    renderSection()
    const hardwareRow = (await screen.findByText('Birch Street Hardware')).closest('tr')!
    expect(within(hardwareRow).getByText('finance.receiptBalanced')).toBeTruthy()
    expect(
      await within(hardwareRow).findByRole('button', {
        name: /^finance\.receiptMatchedCharge .*Birch Street Hardware.*\}$/,
      }),
    ).toBeTruthy()
    expect(
      within(hardwareRow)
        .getByRole('link', { name: /finance\.openReceiptFile/ })
        .getAttribute('href'),
    ).toBe('/api/v1/agent/attachments/attachment-hardware')
    const floristRow = screen.getByText('Petal and Stem').closest('tr')!
    expect(within(floristRow).getByText(/finance\.receiptUnbalanced .*0\.25/)).toBeTruthy()
    expect(within(floristRow).getByText('finance.receiptNotMatched')).toBeTruthy()
    expect(within(floristRow).getByRole('button', { name: /finance\.openReceiptMessage/ })).toBeTruthy()
    expect(calledWith('FinanceReceipts')[0]).toEqual({ limit: 50, offset: 0 })
    // The charges are read by their ids, in one ask.
    expect(calledWith('FinanceTransactions')).toContainEqual({
      financeTransactionIds: ['transaction-hardware'],
      limit: 200,
    })
  })

  it('narrows to unmatched receipts and to days, in the address', async () => {
    answer(() => [floristReceipt, hardwareReceipt])
    renderSection('/finance/receipts?page=2')
    await screen.findByText('Birch Street Hardware')
    fireEvent.click(screen.getByRole('checkbox', { name: 'finance.onlyUnmatchedReceipts' }))
    await waitFor(() => expect(screen.queryByText('Birch Street Hardware')).toBeNull())
    expect(screen.getByText('Petal and Stem')).toBeTruthy()
    expect(calledWith('FinanceReceipts')).toContainEqual({ isUnmatched: true, limit: 50, offset: 0 })
    // Another filter starts again at the first page.
    expect(shownSearch).toBe('?unmatched=1')
    fireEvent.change(screen.getByLabelText('finance.from'), { target: { value: '2026-06-01' } })
    await waitFor(() =>
      expect(calledWith('FinanceReceipts')).toContainEqual({
        from: '2026-06-01',
        isUnmatched: true,
        limit: 50,
        offset: 0,
      }),
    )
    expect(shownSearch).toBe('?unmatched=1&from=2026-06-01')
  })

  it('pages on the server with the page in the address, and says how many there are', async () => {
    // Sixty invented receipts, one a day.
    const many = Array.from({ length: 60 }, (_, index) => ({
      ...floristReceipt,
      id: `receipt-${index}`,
      merchantName: `Invented Stall ${index}`,
      purchasedOn: `2026-0${1 + Math.floor(index / 28)}-${String(1 + (index % 28)).padStart(2, '0')}`,
    }))
    answer(() => many)
    renderSection()
    expect(await screen.findByText('Invented Stall 0')).toBeTruthy()
    expect(screen.queryByText('Invented Stall 50')).toBeNull()
    expect(screen.getByText('table.range {"first":"1","last":"50","total":"60"}')).toBeTruthy()
    expect(screen.getAllByText('finance.receiptCountOther {"count":"60"}').length).toBeGreaterThan(0)
    expect(screen.queryByText(/finance\.receiptsCut/)).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'table.next' }))
    expect(await screen.findByText('Invented Stall 50')).toBeTruthy()
    expect(screen.queryByText('Invented Stall 0')).toBeNull()
    expect(shownSearch).toBe('?page=2')
    expect(calledWith('FinanceReceipts')).toEqual([
      { limit: 50, offset: 0 },
      { limit: 50, offset: 50 },
    ])
    expect(screen.getByText('table.range {"first":"51","last":"60","total":"60"}')).toBeTruthy()

    // A filter goes back to the first page.
    fireEvent.click(screen.getByRole('checkbox', { name: 'finance.onlyUnmatchedReceipts' }))
    await waitFor(() =>
      expect(calledWith('FinanceReceipts')).toContainEqual({ isUnmatched: true, limit: 50, offset: 0 }),
    )
    expect(shownSearch).toBe('?unmatched=1')
  })

  it('lists the receipts that print no day, only with no days chosen', async () => {
    const undated = {
      ...floristReceipt,
      id: 'receipt-undated',
      merchantName: 'Lantern Night Market',
      purchasedOn: null,
    }
    answer(() => [hardwareReceipt, undated])
    renderSection()
    // Listed after the dated ones, saying they print no day.
    const undatedRow = (await screen.findByText('Lantern Night Market')).closest('tr')!
    expect(within(undatedRow).getByText('finance.receiptNoDay')).toBeTruthy()
    fireEvent.click(screen.getByRole('checkbox', { name: 'finance.onlyUndatedReceipts' }))
    await waitFor(() => expect(screen.queryByText('Birch Street Hardware')).toBeNull())
    expect(screen.getByText('Lantern Night Market')).toBeTruthy()
    expect(calledWith('FinanceReceipts')).toContainEqual({ isUndated: true, limit: 50, offset: 0 })
    expect(shownSearch).toBe('?undated=1')

    // Choosing days leaves the undated out, as the server does, and the
    // choice is not offered while days are chosen.
    fireEvent.change(screen.getByLabelText('finance.from'), { target: { value: '2026-06-01' } })
    await waitFor(() =>
      expect(calledWith('FinanceReceipts')).toContainEqual({ from: '2026-06-01', limit: 50, offset: 0 }),
    )
    expect(shownSearch).toBe('?from=2026-06-01')
    expect(screen.queryByRole('checkbox', { name: 'finance.onlyUndatedReceipts' })).toBeNull()
    expect(calledWith('FinanceReceipts').some((variables) => variables?.isUndated && variables?.from)).toBe(false)
  })

  it('never asks for undated receipts within days, even from a link that says both', async () => {
    answer(() => [hardwareReceipt])
    renderSection('/finance/receipts?undated=1&from=2026-06-01')
    await screen.findByText('Birch Street Hardware')
    expect(calledWith('FinanceReceipts')).toEqual([{ from: '2026-06-01', limit: 50, offset: 0 }])
  })

  it('says when no receipt prints no day', async () => {
    answer(() => [hardwareReceipt])
    renderSection('/finance/receipts?undated=1')
    expect(await screen.findByText('finance.noUndatedReceipts')).toBeTruthy()
  })

  it('says by how much and which way an unbalanced receipt is off', async () => {
    const short = { ...floristReceipt, checkDifferenceAmount: '-2.2000' }
    answer(() => [short])
    renderSection()
    const row = (await screen.findByText('Petal and Stem')).closest('tr')!
    // The tag says the amount without a sign; its tooltip says which way.
    const tag = within(row).getByText('finance.receiptUnbalanced {"amount":"$2.20"}')
    expect(tag.closest('[title]')?.getAttribute('title')).toBe('finance.receiptLinesLess {"amount":"$2.20"}')
    fireEvent.click(screen.getByRole('row', { name: /finance\.receiptDetailsOf .*Petal and Stem/ }))
    const receipt = await screen.findByRole('alertdialog', { name: 'Petal and Stem' })
    expect(within(receipt).getByText('finance.receiptLinesLess {"amount":"$2.20"}')).toBeTruthy()
  })

  it('says when every receipt is matched', async () => {
    answer(() => [hardwareReceipt])
    renderSection('/finance/receipts?unmatched=1')
    expect(await screen.findByText('finance.noUnmatchedReceipts')).toBeTruthy()
  })

  it('opens a receipt with its lines, and a charge it explains from there', async () => {
    answer(() => [hardwareReceipt])
    renderSection()
    fireEvent.click(await screen.findByRole('row', { name: /finance\.receiptDetailsOf .*Birch Street Hardware/ }))
    const receipt = await screen.findByRole('alertdialog', { name: 'Birch Street Hardware' })
    expect(within(receipt).getByText('WOOD SCREWS')).toBeTruthy()
    expect(within(receipt).getByText('finance.receiptBalanced')).toBeTruthy()
    expect(within(receipt).getByText(/finance\.receiptMatchedAutomatically/)).toBeTruthy()
    fireEvent.click(
      within(receipt).getByRole('button', { name: /^finance\.receiptMatchedCharge .*Birch Street Hardware.*\}$/ }),
    )
    // The charge's details take the receipt's place, and give it back.
    const details = await screen.findByRole('alertdialog', { name: 'Birch Street Hardware' })
    expect(within(details).getByText('BIRCH STREET HARDWARE 17')).toBeTruthy()
    fireEvent.click(within(details).getByRole('button', { name: 'common.close' }))
    expect(await screen.findByText('WOOD SCREWS')).toBeTruthy()
  })

  it('opens a charge from its row', async () => {
    answer(() => [hardwareReceipt])
    renderSection()
    fireEvent.click(
      await screen.findByRole('button', { name: /^finance\.receiptMatchedCharge .*Birch Street Hardware.*\}$/ }),
    )
    const details = await screen.findByRole('alertdialog', { name: 'Birch Street Hardware' })
    expect(within(details).getByText('BIRCH STREET HARDWARE 17')).toBeTruthy()
  })

  it('matches an unmatched receipt to a proposed charge', async () => {
    let isMatched = false
    answer(() => [
      isMatched
        ? {
            ...floristReceipt,
            receiptMatches: [
              {
                receiptId: 'receipt-florist',
                financeTransactionId: 'transaction-florist',
                matchedAmount: '8.2500',
                receiptMatchSource: 'person',
                createdAt: '2026-06-05T12:00:00Z',
              },
            ],
          }
        : floristReceipt,
    ])
    renderSection()
    fireEvent.click(await screen.findByRole('row', { name: /finance\.receiptDetailsOf .*Petal and Stem/ }))
    const receipt = await screen.findByRole('alertdialog', { name: 'Petal and Stem' })
    expect(within(receipt).getByText('finance.receiptNotMatched')).toBeTruthy()
    fireEvent.click(within(receipt).getByRole('button', { name: 'finance.matchReceiptToCharge' }))
    const chooser = await screen.findByRole('alertdialog', { name: /finance\.matchChargeTitle/ })
    expect(calledWith('ProposeReceiptMatches')).toEqual([{ receiptId: 'receipt-florist' }])
    expect(await within(chooser).findByText(/finance\.candidateExactAmount/)).toBeTruthy()
    isMatched = true
    fireEvent.click(within(chooser).getByRole('button', { name: 'Petal and Stem: finance.matchToThisCharge' }))
    await waitFor(() => expect(toast.done).toHaveBeenCalledWith('finance.receiptMatched {"merchant":"Petal and Stem"}'))
    expect(calledWith('MatchReceipt')).toEqual([
      { receiptId: 'receipt-florist', financeTransactionId: 'transaction-florist', matchedAmount: '8.2500' },
    ])
    // Back to the receipt, now naming the charge it explains.
    const matched = await screen.findByRole('alertdialog', { name: 'Petal and Stem' })
    expect(
      await within(matched).findByRole('button', { name: /^finance\.receiptMatchedCharge .*Petal and Stem.*\}$/ }),
    ).toBeTruthy()
  })

  it('takes a receipt off a charge', async () => {
    let isMatched = true
    answer(() => [isMatched ? hardwareReceipt : { ...hardwareReceipt, receiptMatches: [] }])
    renderSection()
    fireEvent.click(await screen.findByRole('row', { name: /finance\.receiptDetailsOf .*Birch Street Hardware/ }))
    const receipt = await screen.findByRole('alertdialog', { name: 'Birch Street Hardware' })
    isMatched = false
    fireEvent.click(await within(receipt).findByRole('button', { name: /: finance\.unmatchReceiptFrom$/ }))
    await waitFor(() =>
      expect(toast.done).toHaveBeenCalledWith('finance.receiptUnmatched {"merchant":"Birch Street Hardware"}'),
    )
    expect(calledWith('UnmatchReceipt')).toEqual([
      { receiptId: 'receipt-hardware', financeTransactionId: 'transaction-hardware' },
    ])
    expect(await within(receipt).findByText('finance.receiptNotMatched')).toBeTruthy()
  })

  it('deletes a receipt after asking', async () => {
    let isKept = true
    answer(() => (isKept ? [hardwareReceipt] : []))
    renderSection()
    fireEvent.click(await screen.findByRole('row', { name: /finance\.receiptDetailsOf .*Birch Street Hardware/ }))
    const receipt = await screen.findByRole('alertdialog', { name: 'Birch Street Hardware' })
    fireEvent.click(within(receipt).getByRole('button', { name: 'finance.deleteReceipt' }))
    const confirmation = await screen.findByRole('alertdialog', {
      name: 'finance.deleteReceiptTitle {"merchant":"Birch Street Hardware"}',
    })
    expect(calledWith('DeleteReceipt')).toEqual([])
    isKept = false
    fireEvent.click(within(confirmation).getByRole('button', { name: 'common.delete' }))
    await waitFor(() =>
      expect(toast.done).toHaveBeenCalledWith('finance.receiptDeleted {"merchant":"Birch Street Hardware"}'),
    )
    expect(calledWith('DeleteReceipt')).toEqual([{ receiptId: 'receipt-hardware' }])
    expect(await screen.findByText('finance.noReceiptsListed')).toBeTruthy()
    expect(screen.queryByRole('alertdialog')).toBeNull()
  })

  it('says why a delete failed, and keeps the receipt', async () => {
    answer(() => [hardwareReceipt])
    const refusal = new Error('no such receipt')
    const played = execute.getMockImplementation()!
    execute.mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
      if (document.includes('DeleteReceipt(')) throw refusal
      return played(document, variables)
    })
    renderSection()
    fireEvent.click(await screen.findByRole('row', { name: /finance\.receiptDetailsOf .*Birch Street Hardware/ }))
    fireEvent.click(await screen.findByRole('button', { name: 'finance.deleteReceipt' }))
    const confirmation = await screen.findByRole('alertdialog', { name: /finance\.deleteReceiptTitle/ })
    fireEvent.click(within(confirmation).getByRole('button', { name: 'common.delete' }))
    await waitFor(() => expect(toast.failure).toHaveBeenCalledWith(refusal, 'finance.failed'))
    expect(screen.getByRole('alertdialog', { name: /finance\.deleteReceiptTitle/ })).toBeTruthy()
  })
})
