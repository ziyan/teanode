import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { uploadFiles } from '../../upload'
import { ReceiptEntryPreview, RecordedReceipt } from './financeEntryApi'
import { ReceiptEntryDialog, purchasedAtOf } from './financeReceiptEntry'

const toast = vi.hoisted(() => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }))
vi.mock('../../api', () => ({ graphql: vi.fn(), askAgentAbout: vi.fn() }))
vi.mock('../../upload', () => ({ uploadFiles: vi.fn() }))
vi.mock('../../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) => (values ? `${key} ${JSON.stringify(values)}` : key),
    plural: (count: number, forms: { one: string; other: string }, values?: Record<string, string>) =>
      `${count === 1 ? forms.one : forms.other}${values ? ` ${JSON.stringify(values)}` : ''}`,
  }),
}))
vi.mock('../../components/toast', () => ({ useToast: () => toast }))
const execute = vi.mocked(graphql)
const upload = vi.mocked(uploadFiles)

afterEach(() => {
  cleanup()
  execute.mockReset()
  upload.mockReset()
  toast.done.mockReset()
  toast.failed.mockReset()
  toast.failure.mockReset()
})

const balancedPreview: ReceiptEntryPreview = {
  receiptCheckState: 'balanced',
  checkDifferenceAmount: '0',
  receiptCheckSummary: 'The lines add up to the printed total.',
  isReplacing: false,
  receiptMatchCandidates: [
    {
      financeTransactionId: 'transaction-invented',
      financeTransaction: {
        id: 'transaction-invented',
        postedOn: '2026-05-14',
        amount: '-7.5000',
        currencyCode: 'USD',
        description: 'CORNER MARKET 12',
        merchantName: 'Corner Market',
      } as never,
      matchedAmount: '7.5000',
      isExactAmount: true,
      isSameAccount: false,
      isMerchantNameShared: true,
      dayDistanceCount: 1,
      isAutomatic: true,
      matchConfidence: 'high',
    },
  ],
}

const recorded: RecordedReceipt = {
  financeReceipt: { id: 'receipt-invented', merchantName: 'Corner Market' },
  receiptCheckSummary: 'The lines add up to the printed total.',
  receiptMatchCandidates: [],
  isReplaced: false,
  droppedReceiptMatchReasons: [],
}

// answers is the server as the dialog sees it: the person's currency, a
// rate for euros, the preview and the recording.
function answers(overrides: { preview?: ReceiptEntryPreview; rateFails?: boolean } = {}) {
  execute.mockImplementation((document: string) => {
    if (document.includes('ReportingCurrency')) {
      return Promise.resolve({ ReportingCurrency: { reportingCurrencyCode: 'USD', isChosen: true } })
    }
    if (document.includes('ExchangeRate')) {
      return overrides.rateFails
        ? Promise.reject(new Error('no rate'))
        : Promise.resolve({
            ExchangeRate: {
              fromCurrencyCode: 'EUR',
              toCurrencyCode: 'USD',
              rate: '1.0800',
              rateOn: '2026-05-14',
              rateSource: 'ecb',
            },
          })
    }
    if (document.includes('PreviewRecordReceipt')) {
      return Promise.resolve({ PreviewRecordReceipt: overrides.preview ?? balancedPreview })
    }
    if (document.includes('RecordReceipt')) return Promise.resolve({ RecordReceipt: recorded })
    return Promise.reject(new Error(`unexpected ${document}`))
  })
  upload.mockReturnValue({
    promise: Promise.resolve({ attachments: [{ id: 'attachment-invented' }] }),
    cancel: vi.fn(),
  })
}

function renderDialog() {
  const onRecorded = vi.fn()
  const onClose = vi.fn()
  render(
    <MemoryRouter>
      <ReceiptEntryDialog onRecorded={onRecorded} onClose={onClose} />
    </MemoryRouter>,
  )
  return { onRecorded, onClose }
}

async function fillReceipt() {
  await waitFor(() => expect(screen.getByRole('combobox', { name: 'finance.currency' }).textContent).toContain('USD'))
  fireEvent.change(screen.getByLabelText('finance.entryMerchantName'), { target: { value: 'Corner Market' } })
  fireEvent.change(screen.getByLabelText('finance.entryPurchasedOn'), { target: { value: '2026-05-13' } })
  const line = screen.getByRole('group', { name: 'finance.entryLineNumber {"number":1}' })
  fireEvent.change(within(line).getByLabelText('finance.entryLineDescription'), { target: { value: 'Apples' } })
  fireEvent.change(within(line).getByLabelText('finance.entryLineAmount'), { target: { value: '7.50' } })
  fireEvent.change(screen.getByLabelText('finance.entryTotalAmount'), { target: { value: '7.50' } })
  fireEvent.change(screen.getByTestId('entry-source-file'), {
    target: { files: [new File(['picture'], 'receipt.jpg', { type: 'image/jpeg' })] },
  })
  await waitFor(() => expect(upload).toHaveBeenCalled())
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'finance.entryPreview' }).hasAttribute('disabled')).toBe(false),
  )
}

const expectedVariables = {
  agentAttachmentId: 'attachment-invented',
  merchantName: 'Corner Market',
  purchasedOn: '2026-05-13',
  currencyCode: 'USD',
  totalAmount: '7.50',
  receiptLines: [{ lineNumber: 1, receiptLineKind: 'item', description: 'Apples', lineAmount: '7.50' }],
}

describe('ReceiptEntryDialog', () => {
  it('previews what recording would do, then records with the same arguments', async () => {
    answers()
    const { onRecorded, onClose } = renderDialog()
    await fillReceipt()
    expect(screen.getByRole('button', { name: 'finance.entryPreview' })).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'finance.entryPreview' }))
    const preview = await screen.findByRole('region', { name: 'finance.entryPreviewTitle' })
    expect(within(preview).getByText('The lines add up to the printed total.')).toBeTruthy()
    expect(within(preview).getByText(/Corner Market/)).toBeTruthy()
    const previewCall = execute.mock.calls.find(([document]) => document.includes('PreviewRecordReceipt'))
    expect(previewCall?.[1]).toEqual(expect.objectContaining(expectedVariables))
    expect(execute.mock.calls.some(([document]) => /mutation[^{]*\{\s*RecordReceipt/.test(document))).toBe(false)

    fireEvent.click(screen.getByRole('button', { name: 'finance.entryRecordReceipt' }))
    await waitFor(() => expect(onRecorded).toHaveBeenCalled())
    const recordCall = execute.mock.calls.find(([document]) => /mutation[^{]*\{\s*RecordReceipt/.test(document))
    expect(recordCall?.[1]).toEqual(expect.objectContaining(expectedVariables))
    expect(toast.done).toHaveBeenCalledWith(expect.stringContaining('Corner Market'))
    expect(onClose).toHaveBeenCalled()
  })

  it('asks for a new preview when anything is changed after one', async () => {
    answers()
    renderDialog()
    await fillReceipt()
    fireEvent.click(screen.getByRole('button', { name: 'finance.entryPreview' }))
    await screen.findByRole('button', { name: 'finance.entryRecordReceipt' })
    fireEvent.change(screen.getByLabelText('finance.entryTotalAmount'), { target: { value: '8.00' } })
    expect(screen.getByRole('button', { name: 'finance.entryPreview' })).toBeTruthy()
    expect(screen.queryByRole('region', { name: 'finance.entryPreviewTitle' })).toBeNull()
  })

  it('records a receipt that does not add up only when the person accepts it', async () => {
    answers({
      preview: {
        ...balancedPreview,
        receiptCheckState: 'unbalanced',
        checkDifferenceAmount: '-0.5000',
        receiptCheckSummary: 'The lines come to 0.50 less than printed.',
        receiptMatchCandidates: [],
      },
    })
    renderDialog()
    await fillReceipt()
    fireEvent.click(screen.getByRole('button', { name: 'finance.entryPreview' }))
    await screen.findByText('The lines come to 0.50 less than printed.')
    expect(screen.getByRole('button', { name: 'finance.entryRecordReceipt' }).hasAttribute('disabled')).toBe(true)
    fireEvent.click(screen.getByLabelText('finance.entryUnbalancedAccepted'))
    fireEvent.click(screen.getByRole('button', { name: 'finance.entryRecordReceipt' }))
    await waitFor(() =>
      expect(execute.mock.calls.some(([document]) => /mutation[^{]*\{\s*RecordReceipt/.test(document))).toBe(true),
    )
    const recordCall = execute.mock.calls.find(([document]) => /mutation[^{]*\{\s*RecordReceipt/.test(document))
    expect(recordCall?.[1]).toEqual(expect.objectContaining({ isUnbalancedAccepted: true }))
  })

  it('says a refusal in a toast and stays open', async () => {
    answers()
    const { onClose } = renderDialog()
    await fillReceipt()
    execute.mockImplementation((document: string) =>
      document.includes('ReportingCurrency')
        ? Promise.resolve({ ReportingCurrency: { reportingCurrencyCode: 'USD', isChosen: true } })
        : Promise.reject(new Error('the receipt is refused')),
    )
    fireEvent.click(screen.getByRole('button', { name: 'finance.entryPreview' }))
    await waitFor(() => expect(toast.failure).toHaveBeenCalled())
    expect(onClose).not.toHaveBeenCalled()
  })

  it('shows the current rate under a currency that is not the home currency', async () => {
    answers()
    renderDialog()
    await waitFor(() => expect(screen.getByRole('combobox', { name: 'finance.currency' }).textContent).toContain('USD'))
    expect(screen.queryByText(/finance.entryExchangeRate/)).toBeNull()
    fireEvent.click(screen.getByRole('combobox', { name: 'finance.currency' }))
    fireEvent.click(await screen.findByRole('option', { name: 'EUR' }))
    expect(await screen.findByText(/finance.entryExchangeRate/)).toBeTruthy()
    const rateCall = execute.mock.calls.find(([document]) => document.includes('ExchangeRate'))
    expect(rateCall?.[1]).toEqual({ fromCurrencyCode: 'EUR', toCurrencyCode: 'USD' })
    expect(screen.getByText(/finance.entryExchangeRate/).textContent).toContain('"rate":"1.08"')
  })

  it('shows nothing under the currency when the rate cannot be read', async () => {
    answers({ rateFails: true })
    renderDialog()
    await waitFor(() => expect(screen.getByRole('combobox', { name: 'finance.currency' }).textContent).toContain('USD'))
    fireEvent.click(screen.getByRole('combobox', { name: 'finance.currency' }))
    fireEvent.click(await screen.findByRole('option', { name: 'EUR' }))
    await waitFor(() => expect(execute.mock.calls.some(([document]) => document.includes('ExchangeRate'))).toBe(true))
    expect(screen.queryByText(/finance.entryExchangeRate/)).toBeNull()
  })

  it('adds and removes lines, sending each with its number', async () => {
    answers()
    renderDialog()
    await fillReceipt()
    fireEvent.click(screen.getByRole('button', { name: 'finance.entryAddLine' }))
    const second = screen.getByRole('group', { name: 'finance.entryLineNumber {"number":2}' })
    fireEvent.change(within(second).getByLabelText('finance.entryLineKind'), { target: { value: 'tax' } })
    fireEvent.change(within(second).getByLabelText('finance.entryLineAmount'), { target: { value: '0.60' } })
    fireEvent.click(screen.getByRole('button', { name: 'finance.entryPreview' }))
    await screen.findByRole('region', { name: 'finance.entryPreviewTitle' })
    const previewCall = execute.mock.calls.find(([document]) => document.includes('PreviewRecordReceipt'))
    expect((previewCall?.[1] as { receiptLines: unknown[] }).receiptLines).toEqual([
      expect.objectContaining({ lineNumber: 1, receiptLineKind: 'item', description: 'Apples', lineAmount: '7.50' }),
      expect.objectContaining({ lineNumber: 2, receiptLineKind: 'tax', lineAmount: '0.60' }),
    ])
    fireEvent.click(within(second).getByRole('button', { name: /common.remove/ }))
    expect(screen.queryByRole('group', { name: 'finance.entryLineNumber {"number":2}' })).toBeNull()
  })
})

describe('purchasedAtOf', () => {
  it('writes a day and a time as RFC 3339 with the zone, and nothing when either is missing', () => {
    expect(purchasedAtOf('2026-05-13', '14:05')).toMatch(/^2026-05-13T14:05:00[+-]\d{2}:\d{2}$/)
    expect(purchasedAtOf('2026-05-13', '')).toBeUndefined()
    expect(purchasedAtOf('', '14:05')).toBeUndefined()
  })

  it('shows each line its number and ignores a close while a request is in flight', async () => {
    answers()
    const { onClose } = renderDialog()
    await fillReceipt()
    const line = screen.getByRole('group', { name: 'finance.entryLineNumber {"number":1}' })
    expect(within(line).getByText('finance.entryLineNumber {"number":1}')).toBeTruthy()
    execute.mockImplementation(() => new Promise(() => {}))
    fireEvent.click(screen.getByRole('button', { name: 'finance.entryPreview' }))
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).not.toHaveBeenCalled()
  })

  it('asks again whether to accept the lines when a new preview is made', async () => {
    answers({
      preview: {
        ...balancedPreview,
        receiptCheckState: 'unbalanced',
        checkDifferenceAmount: '-0.5000',
        receiptCheckSummary: 'The lines come to 0.50 less than printed.',
        receiptMatchCandidates: [],
      },
    })
    renderDialog()
    await fillReceipt()
    fireEvent.click(screen.getByRole('button', { name: 'finance.entryPreview' }))
    await screen.findByText('The lines come to 0.50 less than printed.')
    fireEvent.click(screen.getByLabelText('finance.entryUnbalancedAccepted'))
    expect((screen.getByLabelText('finance.entryUnbalancedAccepted') as HTMLInputElement).checked).toBe(true)
    fireEvent.change(screen.getByLabelText('finance.entryTotalAmount'), { target: { value: '8.00' } })
    fireEvent.click(screen.getByRole('button', { name: 'finance.entryPreview' }))
    await screen.findByLabelText('finance.entryUnbalancedAccepted')
    expect((screen.getByLabelText('finance.entryUnbalancedAccepted') as HTMLInputElement).checked).toBe(false)
  })

  it('shows nothing under the currency while the rate of the previous one is still held', async () => {
    answers()
    renderDialog()
    await waitFor(() => expect(screen.getByRole('combobox', { name: 'finance.currency' }).textContent).toContain('USD'))
    fireEvent.click(screen.getByRole('combobox', { name: 'finance.currency' }))
    fireEvent.click(await screen.findByRole('option', { name: 'EUR' }))
    expect(await screen.findByText(/finance.entryExchangeRate/)).toBeTruthy()
    // The answer for euros is still the one held when the code becomes pounds.
    fireEvent.click(screen.getByRole('combobox', { name: 'finance.currency' }))
    fireEvent.click(await screen.findByRole('option', { name: 'GBP' }))
    expect(screen.queryByText(/"from":"EUR"/)).toBeNull()
  })
})
