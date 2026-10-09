import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { uploadFiles } from '../../upload'
import { FinanceAccount, FinanceReceipt, FinanceTransaction, ReceiptMatchCandidate } from './financeApi'
import { receiptPolling, receiptQuantityText, unexplainedAmount } from './financeReceipts'
import { FinanceTransactionDialog } from './financeTransactionDialog'

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

const savedPolling = { ...receiptPolling }
beforeEach(() => {
  receiptPolling.intervalMS = 5
  receiptPolling.attemptCount = 20
})
afterEach(() => {
  cleanup()
  execute.mockReset()
  upload.mockReset()
  toast.done.mockReset()
  toast.failed.mockReset()
  toast.failure.mockReset()
  Object.assign(receiptPolling, savedPolling)
})

const account = {
  id: 'account-invented',
  accountName: 'Invented Card',
  accountKind: 'credit',
  currencyCode: 'USD',
} as FinanceAccount

const charge: FinanceTransaction = {
  id: 'transaction-grocer',
  financeAccountId: 'account-invented',
  providerTransactionId: 'provider-grocer',
  postedOn: '2026-05-14',
  amount: '-20.0000',
  currencyCode: 'USD',
  description: 'MAPLE LANE GROCER 0042',
  merchantName: 'Maple Lane Grocer',
  isPending: false,
  receiptCount: 1,
  createdAt: '2026-05-14T12:00:00Z',
  modifiedAt: '2026-05-14T12:00:00Z',
}

// An invented grocery receipt that adds up: four items, a promotion under
// the peaches, a weighed item, and two taxes by their marks.
const grocerReceipt: FinanceReceipt = {
  id: 'receipt-grocer',
  receiptSourceKind: 'attachment',
  agentAttachmentId: 'attachment-grocer',
  merchantName: 'Maple Lane Grocer',
  merchantReceiptNumber: '7731',
  purchasedOn: '2026-05-13',
  currencyCode: 'USD',
  subtotalAmount: '16.2900',
  totalAmount: '16.9700',
  receiptCheckState: 'balanced',
  checkDifferenceAmount: '0',
  isFeeAfterSubtotal: false,
  receiptLines: [
    {
      id: 'line-1',
      lineNumber: 1,
      receiptLineKind: 'item',
      description: 'CHICKEN STOCK',
      lineAmount: '3.2900',
      taxClassCode: 't',
    },
    {
      id: 'line-2',
      lineNumber: 2,
      receiptLineKind: 'item',
      description: 'TOOTHPASTE',
      lineAmount: '6.9900',
      taxClassCode: 'T',
    },
    {
      id: 'line-3',
      lineNumber: 3,
      receiptLineKind: 'item',
      description: 'PEACHES',
      lineAmount: '3.9900',
      taxClassCode: 't',
    },
    {
      id: 'line-4',
      lineNumber: 4,
      receiptLineKind: 'discount',
      description: 'Promotion',
      lineAmount: '-2.0000',
      taxClassCode: 't',
      discountedLineNumber: 3,
      discountedLineId: 'line-3',
    },
    {
      id: 'line-5',
      lineNumber: 5,
      receiptLineKind: 'item',
      description: 'ONIONS',
      quantity: '2.53',
      quantityUnit: 'lb',
      unitPriceAmount: '1.5900',
      lineAmount: '4.0200',
      taxClassCode: 't',
    },
    {
      id: 'line-6',
      lineNumber: 6,
      receiptLineKind: 'tax',
      description: 'Sales Tax',
      lineAmount: '0.4900',
      taxClassCode: 'T',
    },
    {
      id: 'line-7',
      lineNumber: 7,
      receiptLineKind: 'tax',
      description: 'Food Tax',
      lineAmount: '0.1900',
      taxClassCode: 't',
    },
  ],
  receiptMatches: [
    {
      receiptId: 'receipt-grocer',
      financeTransactionId: 'transaction-grocer',
      matchedAmount: '16.9700',
      receiptMatchSource: 'receipt_matcher',
      matchConfidence: '0.85',
      createdAt: '2026-05-14T12:00:00Z',
    },
  ],
}

// A misread receipt from a message, off by forty cents.
const misreadReceipt: FinanceReceipt = {
  id: 'receipt-bakery',
  receiptSourceKind: 'mail',
  mailId: 'mail-bakery',
  mailboxItemId: 'item-bakery',
  merchantName: 'Copper Kettle Bakery',
  purchasedOn: '2026-05-13',
  currencyCode: 'USD',
  totalAmount: '2.6000',
  receiptCheckState: 'unbalanced',
  checkDifferenceAmount: '0.4000',
  isFeeAfterSubtotal: false,
  receiptLines: [
    {
      id: 'line-b1',
      lineNumber: 1,
      receiptLineKind: 'item',
      description: 'RYE LOAF',
      quantity: '1',
      lineAmount: '3.0000',
    },
  ],
  receiptMatches: [
    {
      receiptId: 'receipt-bakery',
      financeTransactionId: 'transaction-grocer',
      matchedAmount: '2.6000',
      receiptMatchSource: 'person',
      createdAt: '2026-05-14T12:00:00Z',
    },
  ],
}

type Answers = {
  receipts?: () => FinanceReceipt[]
  unmatched?: FinanceReceipt[]
  candidates?: ReceiptMatchCandidate[]
}

// answer plays the server: the document says which operation is asked.
function answer({ receipts = () => [grocerReceipt], unmatched = [], candidates = [] }: Answers) {
  execute.mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('FinanceReceipts(')) {
      const listed = variables?.isUnmatched ? unmatched : receipts()
      return { FinanceReceipts: { financeReceipts: listed, nextCursor: '', totalCount: listed.length } }
    }
    if (document.includes('ProposeReceiptMatches(')) return { ProposeReceiptMatches: candidates }
    if (document.includes('AnnotateTransaction(')) {
      return { AnnotateTransaction: { ...charge, annotation: variables?.annotation, annotatedBy: 'person' } }
    }
    if (document.includes('MatchReceipt(') || document.includes('UnmatchReceipt(')) return {}
    if (document.includes('DeleteReceipt(')) return { DeleteReceipt: true }
    if (document.includes('ReadReceipt(')) return { ReadReceipt: { agentJobId: 'job-invented' } }
    return { FinanceTransactions: { financeTransactions: [], totalCount: 0, leftOutDuplicateCount: 0 } }
  })
}

function renderDialog(financeTransaction: FinanceTransaction = charge) {
  const onTransactionChanged = vi.fn()
  render(
    <MemoryRouter>
      <FinanceTransactionDialog
        financeTransaction={financeTransaction}
        financeAccount={account}
        financeAccounts={[account]}
        categoryOptions={[]}
        onCategorize={vi.fn()}
        onCount={vi.fn()}
        onUndoCount={vi.fn()}
        onOpenTransaction={vi.fn()}
        onTransactionChanged={onTransactionChanged}
        onClose={vi.fn()}
      />
    </MemoryRouter>,
  )
  return { onTransactionChanged }
}

function calledWith(operation: string) {
  return execute.mock.calls.filter(([document]) => document.includes(`${operation}(`)).map(([, variables]) => variables)
}

describe('receipt helpers', () => {
  it('writes a quantity as the receipt prints it', () => {
    const line = grocerReceipt.receiptLines[4]
    expect(receiptQuantityText(line, 'en-US')).toBe('2.53 lb @ 1.59')
    expect(receiptQuantityText({ ...line, quantityUnit: '', quantity: '3', unitPriceAmount: '0.9900' }, 'en-US')).toBe(
      '3 @ 0.99',
    )
    // A price to a tenth of a cent keeps its places.
    expect(receiptQuantityText({ ...line, unitPriceAmount: '1.5990' }, 'en-US')).toBe('2.53 lb @ 1.599')
    // A count of one says nothing.
    expect(receiptQuantityText(misreadReceipt.receiptLines[0], 'en-US')).toBe('')
  })

  it('says what of the charge the matched receipts leave unexplained', () => {
    expect(unexplainedAmount(charge, [grocerReceipt])).toBeCloseTo(3.03, 4)
    expect(unexplainedAmount(charge, [grocerReceipt, misreadReceipt])).toBeCloseTo(0.43, 4)
    expect(unexplainedAmount({ ...charge, amount: '-16.9700' }, [grocerReceipt])).toBe(0)
  })
})

describe('the order of the details', () => {
  it('puts the annotation and the receipts right after the spending category', async () => {
    answer({})
    renderDialog({ ...charge, providerCategoryPrimary: 'FOOD_AND_DRINK', pendingProviderTransactionId: 'pending-1' })
    await screen.findByRole('article', { name: 'Maple Lane Grocer' })
    const details = document.querySelector('.finance-transaction-details')!
    const terms = [...details.querySelectorAll('dt, section.finance-receipts')].map((element) =>
      element.tagName === 'SECTION' ? 'receipts' : element.textContent,
    )
    const category = terms.indexOf('finance.spendingCategory')
    expect(terms.slice(category, category + 3)).toEqual(['finance.spendingCategory', 'finance.annotation', 'receipts'])
    for (const later of [
      'finance.providerCategoryPrimary',
      'finance.pending',
      'finance.replacedPending',
      'finance.providerTransactionId',
      'finance.createdAt',
      'finance.modifiedAt',
    ]) {
      expect(terms.indexOf(later)).toBeGreaterThan(category + 2)
    }
  })

  it('says what to upload in a line, and what cannot be read on the upload button', async () => {
    answer({})
    renderDialog()
    await screen.findByRole('article', { name: 'Maple Lane Grocer' })
    expect(screen.getByText('finance.receiptsHint')).toBeTruthy()
    const uploadButton = screen.getByRole('button', { name: 'finance.uploadReceipt' })
    fireEvent.pointerEnter(uploadButton.closest('.tooltip-anchor')!)
    expect(await screen.findByText('finance.uploadReceiptHint')).toBeTruthy()
  })

  it('puts Save and Clear right under the annotation box', () => {
    answer({})
    renderDialog({ ...charge, annotation: 'Picnic supplies', annotatedBy: 'agent' })
    const field = screen.getByRole('textbox', { name: 'finance.annotation' })
    const buttons = field.nextElementSibling!
    expect(buttons.textContent).toContain('common.save')
    expect(buttons.textContent).toContain('finance.clearAnnotation')
    expect(buttons.nextElementSibling?.textContent).toBe('finance.annotatedByAgent')
  })
})

describe('the annotation', () => {
  it('says the agent wrote it, and saves what the person types', async () => {
    answer({})
    const { onTransactionChanged } = renderDialog({ ...charge, annotation: 'Picnic supplies', annotatedBy: 'agent' })
    expect(screen.getByText('finance.annotatedByAgent')).toBeTruthy()
    const field = screen.getByRole('textbox', { name: 'finance.annotation' }) as HTMLTextAreaElement
    expect(field.value).toBe('Picnic supplies')
    const save = screen.getByRole('button', { name: 'common.save' }) as HTMLButtonElement
    expect(save.disabled).toBe(true)
    fireEvent.change(field, { target: { value: '  Picnic for the reading group ' } })
    fireEvent.click(save)
    await waitFor(() => expect(toast.done).toHaveBeenCalledWith('finance.annotationSaved'))
    expect(calledWith('AnnotateTransaction')).toEqual([
      { financeTransactionId: 'transaction-grocer', annotation: 'Picnic for the reading group' },
    ])
    expect(onTransactionChanged).toHaveBeenCalledWith(
      expect.objectContaining({ annotation: 'Picnic for the reading group', annotatedBy: 'person' }),
    )
  })

  it('clears it', async () => {
    answer({})
    renderDialog({ ...charge, annotation: 'Picnic supplies', annotatedBy: 'person' })
    expect(screen.queryByText('finance.annotatedByAgent')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'finance.clearAnnotation' }))
    await waitFor(() => expect(toast.done).toHaveBeenCalledWith('finance.annotationCleared'))
    expect(calledWith('AnnotateTransaction')).toEqual([{ financeTransactionId: 'transaction-grocer', annotation: '' }])
  })
})

describe('the receipts section', () => {
  it('shows a balanced receipt line by line, with discounts and taxes set apart', async () => {
    answer({})
    renderDialog()
    const receipt = await screen.findByRole('article', { name: 'Maple Lane Grocer' })
    expect(within(receipt).getByText('finance.receiptBalanced')).toBeTruthy()
    expect(within(receipt).getByText('2.53 lb @ 1.59')).toBeTruthy()
    const promotion = within(receipt).getByText('Promotion').closest('tr')!
    expect(promotion.className).toContain('finance-receipt-discount')
    expect(within(promotion).getByText('finance.receiptLineKindDiscount')).toBeTruthy()
    const salesTax = within(receipt).getByText('Sales Tax').closest('tr')!
    expect(salesTax.className).toContain('finance-receipt-tax')
    expect(within(salesTax).getByText('finance.receiptLineKindTax')).toBeTruthy()
    // The subtotal comes after the items and their discounts and before the
    // taxes it does not include, as a receipt prints it.
    const rows = within(receipt).getAllByRole('row')
    const subtotalIndex = rows.findIndex((row) => row.textContent?.includes('finance.receiptSubtotal'))
    expect(subtotalIndex).toBeGreaterThan(rows.indexOf(promotion))
    expect(subtotalIndex).toBeLessThan(rows.indexOf(salesTax))
    expect(within(receipt).getByText(/finance\.receiptMatchedAutomatically/)).toBeTruthy()
    // The photo opens from where the agent keeps it.
    const photo = within(receipt).getByRole('link', { name: /finance\.openReceiptFile/ })
    expect(photo.getAttribute('href')).toBe('/api/v1/agent/attachments/attachment-grocer')
    // Twenty charged, 16.97 explained.
    expect(screen.getByText('finance.unexplained').parentElement?.textContent).toMatch(/3\.03/)
  })

  it('shows the photo above the lines, opens it to zoom, and steps aside for a file that is no picture', async () => {
    answer({})
    renderDialog()
    const receipt = await screen.findByRole('article', { name: 'Maple Lane Grocer' })
    const picture = within(receipt).getByRole('img', { name: 'Maple Lane Grocer' })
    expect(picture.getAttribute('src')).toBe('/api/v1/agent/attachments/attachment-grocer')
    expect(
      picture.compareDocumentPosition(within(receipt).getByRole('table')) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
    fireEvent.click(within(receipt).getByRole('link', { name: 'Maple Lane Grocer' }))
    const lightbox = await screen.findByRole('dialog', { name: 'Maple Lane Grocer' })
    expect(within(lightbox).getByText('lightbox.fit')).toBeTruthy()
    fireEvent.click(within(lightbox).getByRole('button', { name: 'common.close' }))
    // A PDF does not load as a picture: the inline view goes, and the file
    // is still a click away.
    fireEvent.error(within(receipt).getByRole('img', { name: 'Maple Lane Grocer' }))
    expect(within(receipt).queryByRole('img', { name: 'Maple Lane Grocer' })).toBeNull()
    expect(within(receipt).getByRole('link', { name: /finance\.openReceiptFile/ })).toBeTruthy()
  })

  it('puts a fee printed after the subtotal with the taxes', async () => {
    // An invented order email: two items, the subtotal, then shipping and
    // tax. 84.00 + 22.50 is 106.50; 106.50 + 7.95 + 8.79 is 123.24.
    const orderReceipt: FinanceReceipt = {
      ...grocerReceipt,
      id: 'receipt-order',
      merchantName: 'Brightwater Outfitters',
      subtotalAmount: '106.5000',
      totalAmount: '123.2400',
      isFeeAfterSubtotal: true,
      receiptLines: [
        { id: 'line-o1', lineNumber: 1, receiptLineKind: 'item', description: 'Trail Jacket', lineAmount: '84.0000' },
        { id: 'line-o2', lineNumber: 2, receiptLineKind: 'item', description: 'Wool Socks', lineAmount: '22.5000' },
        { id: 'line-o3', lineNumber: 3, receiptLineKind: 'fee', description: 'Shipping', lineAmount: '7.9500' },
        { id: 'line-o4', lineNumber: 4, receiptLineKind: 'tax', description: 'Tax', lineAmount: '8.7900' },
      ],
      receiptMatches: [],
    }
    answer({ receipts: () => [orderReceipt] })
    renderDialog({ ...charge, amount: '-123.2400' })
    const receipt = await screen.findByRole('article', { name: 'Brightwater Outfitters' })
    const rows = within(receipt).getAllByRole('row')
    const subtotalIndex = rows.findIndex((row) => row.textContent?.includes('finance.receiptSubtotal'))
    const shipping = within(receipt).getByText('Shipping').closest('tr')!
    expect(shipping.className).toContain('finance-receipt-fee')
    expect(subtotalIndex).toBeGreaterThan(rows.indexOf(within(receipt).getByText('Wool Socks').closest('tr')!))
    expect(rows.indexOf(shipping)).toBeGreaterThan(subtotalIndex)
    expect(rows.indexOf(shipping)).toBeLessThan(rows.indexOf(within(receipt).getByText('Tax').closest('tr')!))
  })

  it('shows how far an unbalanced receipt is off, and opens its message', async () => {
    answer({ receipts: () => [misreadReceipt] })
    renderDialog({ ...charge, amount: '-2.6000' })
    const receipt = await screen.findByRole('article', { name: 'Copper Kettle Bakery' })
    // The tag says by how much, with no sign; which way is its tooltip and
    // the line under the receipt's name. These lines come to more.
    const tag = within(receipt).getByText('finance.receiptUnbalanced {"amount":"$0.40"}')
    expect(tag.closest('[title]')?.getAttribute('title')).toBe('finance.receiptLinesMore {"amount":"$0.40"}')
    expect(within(receipt).getByText(/finance\.receiptLinesMore \{"amount":"\$0\.40"\}/)).toBeTruthy()
    expect(within(receipt).queryByText('finance.receiptBalanced')).toBeNull()
    // Covered in full: nothing unexplained.
    expect(screen.queryByText('finance.unexplained')).toBeNull()
    expect(within(receipt).getByRole('button', { name: /finance\.openReceiptMessage/ })).toBeTruthy()
  })

  it('says an unbalanced receipt whose lines come to less without a minus sign', async () => {
    answer({ receipts: () => [{ ...misreadReceipt, checkDifferenceAmount: '-2.2000' }] })
    renderDialog({ ...charge, amount: '-2.6000' })
    const receipt = await screen.findByRole('article', { name: 'Copper Kettle Bakery' })
    const tag = within(receipt).getByText('finance.receiptUnbalanced {"amount":"$2.20"}')
    expect(tag.closest('[title]')?.getAttribute('title')).toBe('finance.receiptLinesLess {"amount":"$2.20"}')
    expect(within(receipt).queryByText(/-\$2\.20/)).toBeNull()
  })

  it('reads every receipt matched to the charge in one page, and counts them by the server', async () => {
    answer({})
    renderDialog()
    await screen.findByRole('article', { name: 'Maple Lane Grocer' })
    expect(calledWith('FinanceReceipts')[0]).toEqual({ financeTransactionId: 'transaction-grocer', limit: 200 })
  })

  it('says when no receipt is matched yet', async () => {
    answer({ receipts: () => [] })
    renderDialog()
    expect(await screen.findByText('finance.noReceipts')).toBeTruthy()
  })
})

describe('what can be done to receipts', () => {
  it('uploads a photo, has the agent read it, and shows it once read', async () => {
    let isRead = false
    const uploadedReceipt = { ...grocerReceipt, id: 'receipt-uploaded', agentAttachmentId: 'attachment-uploaded' }
    answer({ receipts: () => (isRead ? [uploadedReceipt] : []) })
    upload.mockReturnValue({
      promise: Promise.resolve({ attachments: [{ id: 'attachment-uploaded' }] }),
      cancel: vi.fn(),
    })
    const { onTransactionChanged } = renderDialog({ ...charge, receiptCount: 0 })
    await screen.findByText('finance.noReceipts')
    const photo = new File(['invented'], 'receipt.jpg', { type: 'image/jpeg' })
    fireEvent.change(screen.getByTestId('receipt-file'), { target: { files: [photo] } })
    await waitFor(() => expect(toast.done).toHaveBeenCalledWith('finance.receiptReading'))
    expect(upload).toHaveBeenCalledWith('POST', '/api/v1/agent/attachments', [photo], expect.any(Function))
    expect(calledWith('ReadReceipt')).toEqual([
      { agentAttachmentId: 'attachment-uploaded', financeTransactionId: 'transaction-grocer' },
    ])
    expect(screen.getByRole('status').textContent).toBe('finance.receiptReadingNow')
    isRead = true
    await waitFor(() => expect(toast.done).toHaveBeenCalledWith('finance.receiptRead {"merchant":"Maple Lane Grocer"}'))
    expect(onTransactionChanged).toHaveBeenCalledWith({ receiptCount: 1 })
    expect(await screen.findByRole('article', { name: 'Maple Lane Grocer' })).toBeTruthy()
    expect(screen.queryByRole('status')).toBeNull()
  })

  it('says when the photo was read but the charge refused it, without waiting out the poll', async () => {
    let isRead = false
    const refusedReceipt = {
      ...misreadReceipt,
      id: 'receipt-refused',
      receiptSourceKind: 'attachment' as const,
      mailId: undefined,
      mailboxItemId: undefined,
      agentAttachmentId: 'attachment-refused',
      receiptMatches: [],
    }
    // Never matched to this charge: only the unmatched receipts list it.
    answer({ receipts: () => [], unmatched: [] })
    const played = execute.getMockImplementation()!
    execute.mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
      if (document.includes('FinanceReceipts(') && variables?.isUnmatched) {
        const listed = isRead ? [refusedReceipt] : []
        return { FinanceReceipts: { financeReceipts: listed, nextCursor: '', totalCount: listed.length } }
      }
      return played(document, variables)
    })
    upload.mockReturnValue({
      promise: Promise.resolve({ attachments: [{ id: 'attachment-refused' }] }),
      cancel: vi.fn(),
    })
    renderDialog({ ...charge, receiptCount: 0 })
    await screen.findByText('finance.noReceipts')
    fireEvent.change(screen.getByTestId('receipt-file'), {
      target: { files: [new File(['invented'], 'receipt.jpg', { type: 'image/jpeg' })] },
    })
    await waitFor(() => expect(toast.done).toHaveBeenCalledWith('finance.receiptReading'))
    isRead = true
    await waitFor(() =>
      expect(toast.failed).toHaveBeenCalledWith('finance.receiptReadNotMatched {"merchant":"Copper Kettle Bakery"}'),
    )
    expect(calledWith('FinanceReceipts')).toContainEqual({ isUnmatched: true, limit: 200 })
    expect(screen.queryByRole('status')).toBeNull()
    expect(toast.done).not.toHaveBeenCalledWith('finance.receiptStillReading')
  })

  it('takes only photos', async () => {
    answer({ receipts: () => [] })
    renderDialog()
    expect(screen.getByTestId('receipt-file').getAttribute('accept')).toBe('image/*')
  })

  it('says once when the receipts cannot be read, and not that none is matched', async () => {
    const refusal = new Error('the server is away')
    execute.mockImplementation(async (document: string) => {
      if (document.includes('FinanceReceipts(')) throw refusal
      return { FinanceTransactions: { financeTransactions: [], totalCount: 0, leftOutDuplicateCount: 0 } }
    })
    renderDialog()
    await waitFor(() => expect(toast.failure).toHaveBeenCalledWith(refusal, 'finance.failed'))
    expect(toast.failure).toHaveBeenCalledTimes(1)
    expect(screen.queryByText('finance.noReceipts')).toBeNull()
  })

  it('refuses a photo too large to read, before uploading it', async () => {
    answer({ receipts: () => [] })
    renderDialog()
    const huge = new File(['x'], 'huge.jpg', { type: 'image/jpeg' })
    Object.defineProperty(huge, 'size', { value: 11 << 20 })
    fireEvent.change(screen.getByTestId('receipt-file'), { target: { files: [huge] } })
    expect(toast.failed).toHaveBeenCalled()
    expect(upload).not.toHaveBeenCalled()
  })

  it('matches an unmatched receipt from the proposed candidates', async () => {
    const unmatched = { ...misreadReceipt, receiptMatches: [] }
    answer({
      receipts: () => [grocerReceipt],
      unmatched: [unmatched],
      candidates: [
        {
          financeTransactionId: 'transaction-elsewhere',
          matchedAmount: '2.6000',
          isExactAmount: true,
          isSameAccount: false,
          isMerchantNameShared: false,
          dayDistanceCount: 0,
          isAutomatic: false,
        },
        {
          financeTransactionId: 'transaction-grocer',
          matchedAmount: '2.6000',
          isExactAmount: false,
          isSameAccount: true,
          isMerchantNameShared: false,
          dayDistanceCount: 1,
          isAutomatic: false,
        },
      ],
    })
    const { onTransactionChanged } = renderDialog()
    await screen.findByRole('article', { name: 'Maple Lane Grocer' })
    fireEvent.click(screen.getByRole('button', { name: 'finance.matchReceipt' }))
    expect(await screen.findByRole('alertdialog', { name: 'finance.matchReceiptTitle' })).toBeTruthy()
    // Asked for receipts bought from a week before the charge posted to
    // three days after.
    expect(calledWith('FinanceReceipts')).toContainEqual({
      isUnmatched: true,
      from: '2026-05-07',
      to: '2026-05-17',
      limit: 50,
    })
    expect(await screen.findByText(/finance\.candidateSameCard/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Copper Kettle Bakery: finance\.matchThisReceipt/ }))
    await waitFor(() =>
      expect(toast.done).toHaveBeenCalledWith('finance.receiptMatched {"merchant":"Copper Kettle Bakery"}'),
    )
    expect(calledWith('MatchReceipt')).toEqual([
      { receiptId: 'receipt-bakery', financeTransactionId: 'transaction-grocer', matchedAmount: '2.6000' },
    ])
    expect(onTransactionChanged).toHaveBeenCalledWith({ receiptCount: 1 })
    // Back to the details.
    await waitFor(() => expect(screen.queryByRole('alertdialog', { name: 'finance.matchReceiptTitle' })).toBeNull())
    expect(screen.getByRole('article', { name: 'Maple Lane Grocer' })).toBeTruthy()
  })

  it('offers the receipts that could be asked about when asking about one fails', async () => {
    const bakeryReceipt = { ...misreadReceipt, receiptMatches: [] }
    const brokenReceipt = { ...misreadReceipt, id: 'receipt-broken', merchantName: 'Quill and Ink', receiptMatches: [] }
    answer({ unmatched: [bakeryReceipt, brokenReceipt] })
    const played = execute.getMockImplementation()!
    const refusal = new Error('could not weigh the receipt')
    execute.mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
      if (document.includes('ProposeReceiptMatches(')) {
        if (variables?.receiptId === 'receipt-broken') throw refusal
        return {
          ProposeReceiptMatches: [
            {
              financeTransactionId: 'transaction-grocer',
              matchedAmount: '2.6000',
              isExactAmount: false,
              isSameAccount: true,
              isMerchantNameShared: false,
              dayDistanceCount: 1,
              isAutomatic: false,
            },
          ],
        }
      }
      return played(document, variables)
    })
    renderDialog()
    await screen.findByRole('article', { name: 'Maple Lane Grocer' })
    fireEvent.click(screen.getByRole('button', { name: 'finance.matchReceipt' }))
    expect(await screen.findByRole('button', { name: /Copper Kettle Bakery: finance\.matchThisReceipt/ })).toBeTruthy()
    expect(screen.queryByText('Quill and Ink')).toBeNull()
    expect(toast.failure).toHaveBeenCalledTimes(1)
    expect(toast.failure).toHaveBeenCalledWith(refusal, 'finance.failed')
  })

  it('says when no unmatched receipt could explain the charge', async () => {
    answer({ unmatched: [] })
    renderDialog()
    await screen.findByRole('article', { name: 'Maple Lane Grocer' })
    fireEvent.click(screen.getByRole('button', { name: 'finance.matchReceipt' }))
    expect(await screen.findByText('finance.noReceiptCandidates')).toBeTruthy()
  })

  it('takes a receipt off the charge', async () => {
    let isMatched = true
    answer({ receipts: () => (isMatched ? [grocerReceipt] : []) })
    const { onTransactionChanged } = renderDialog()
    await screen.findByRole('article', { name: 'Maple Lane Grocer' })
    isMatched = false
    fireEvent.click(screen.getByRole('button', { name: 'Maple Lane Grocer: finance.unmatchReceipt' }))
    await waitFor(() =>
      expect(toast.done).toHaveBeenCalledWith('finance.receiptUnmatched {"merchant":"Maple Lane Grocer"}'),
    )
    expect(calledWith('UnmatchReceipt')).toEqual([
      { receiptId: 'receipt-grocer', financeTransactionId: 'transaction-grocer' },
    ])
    expect(onTransactionChanged).toHaveBeenCalledWith({ receiptCount: 0 })
    expect(await screen.findByText('finance.noReceipts')).toBeTruthy()
  })

  it('deletes a receipt after asking', async () => {
    let isKept = true
    answer({ receipts: () => (isKept ? [grocerReceipt] : []) })
    renderDialog()
    await screen.findByRole('article', { name: 'Maple Lane Grocer' })
    fireEvent.click(screen.getByRole('button', { name: 'Maple Lane Grocer: finance.deleteReceipt' }))
    const confirmation = await screen.findByRole('alertdialog', {
      name: 'finance.deleteReceiptTitle {"merchant":"Maple Lane Grocer"}',
    })
    expect(calledWith('DeleteReceipt')).toEqual([])
    isKept = false
    fireEvent.click(within(confirmation).getByRole('button', { name: 'common.delete' }))
    await waitFor(() =>
      expect(toast.done).toHaveBeenCalledWith('finance.receiptDeleted {"merchant":"Maple Lane Grocer"}'),
    )
    expect(calledWith('DeleteReceipt')).toEqual([{ receiptId: 'receipt-grocer' }])
    expect(await screen.findByText('finance.noReceipts')).toBeTruthy()
  })
})
