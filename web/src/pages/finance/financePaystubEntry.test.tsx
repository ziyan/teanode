import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { uploadFiles } from '../../upload'
import { PaystubEntryDialog } from './financePaystubEntry'

const toast = vi.hoisted(() => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }))
vi.mock('../../api', () => ({ graphql: vi.fn(), askAgentAbout: vi.fn() }))
vi.mock('../../upload', () => ({ uploadFiles: vi.fn() }))
vi.mock('../../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) => (values ? `${key} ${JSON.stringify(values)}` : key),
    plural: (count: number, forms: { one: string; other: string }) => (count === 1 ? forms.one : forms.other),
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
  toast.failure.mockReset()
})

function answers() {
  execute.mockImplementation((document: string) => {
    if (document.includes('ReportingCurrency')) {
      return Promise.resolve({ ReportingCurrency: { reportingCurrencyCode: 'USD', isChosen: true } })
    }
    if (document.includes('RecordPaystub')) {
      return Promise.resolve({ RecordPaystub: { id: 'paystub-invented', employerName: 'Example Works' } })
    }
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
      <PaystubEntryDialog onRecorded={onRecorded} onClose={onClose} />
    </MemoryRouter>,
  )
  return { onRecorded, onClose }
}

describe('PaystubEntryDialog', () => {
  it('records the paystub with its lines and the file as a tax document', async () => {
    answers()
    const { onRecorded, onClose } = renderDialog()
    await waitFor(() => expect(screen.getByRole('combobox', { name: 'finance.currency' }).textContent).toContain('USD'))
    const submit = screen.getByRole('button', { name: 'finance.entryRecordPaystub' })
    expect(submit.hasAttribute('disabled')).toBe(true)

    fireEvent.change(screen.getByLabelText('finance.entryEmployerName'), { target: { value: 'Example Works' } })
    fireEvent.change(screen.getByLabelText('finance.entryEmployeeName'), { target: { value: 'Sample Person' } })
    fireEvent.change(screen.getByLabelText('finance.entryEmployeeTaxIdMask'), { target: { value: '1234' } })
    fireEvent.change(screen.getByLabelText('finance.entryPayDate'), { target: { value: '2026-03-15' } })
    fireEvent.change(screen.getByLabelText('finance.entryGrossAmount'), { target: { value: '2000.00' } })
    fireEvent.change(screen.getByLabelText('finance.entryNetAmount'), { target: { value: '1500.00' } })
    const earning = screen.getByRole('group', { name: 'finance.entryLineNumber {"number":1}' })
    fireEvent.change(within(earning).getByLabelText('finance.entryLineDescription'), { target: { value: 'Regular' } })
    fireEvent.change(within(earning).getByLabelText('finance.entryPaystubHours'), { target: { value: '80' } })
    fireEvent.change(within(earning).getByLabelText('finance.entryPaystubCurrentAmount'), {
      target: { value: '2000.00' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'finance.entryAddLine' }))
    const tax = screen.getByRole('group', { name: 'finance.entryLineNumber {"number":2}' })
    fireEvent.change(within(tax).getByLabelText('finance.entryLineKind'), { target: { value: 'tax' } })
    fireEvent.change(within(tax).getByLabelText('finance.entryPaystubTaxKind'), { target: { value: 'medicare' } })
    fireEvent.change(within(tax).getByLabelText('finance.entryPaystubCurrentAmount'), { target: { value: '29.00' } })
    fireEvent.change(screen.getByTestId('entry-source-file'), {
      target: { files: [new File(['pdf'], 'paystub.pdf', { type: 'application/pdf' })] },
    })
    await waitFor(() => expect(submit.hasAttribute('disabled')).toBe(false))
    expect(upload.mock.calls[0][1]).toContain('isTaxDocument=true')

    fireEvent.click(submit)
    await waitFor(() => expect(onRecorded).toHaveBeenCalled())
    const call = execute.mock.calls.find(([document]) => document.includes('RecordPaystub'))
    expect(call?.[1]).toEqual({
      agentAttachmentId: 'attachment-invented',
      employerName: 'Example Works',
      employeeName: 'Sample Person',
      employeeTaxIdMask: '1234',
      payDate: '2026-03-15',
      currencyCode: 'USD',
      grossAmount: '2000.00',
      netAmount: '1500.00',
      paystubLines: [
        { paystubLineKind: 'earning', description: 'Regular', hours: '80', currentAmount: '2000.00' },
        { paystubLineKind: 'tax', paystubTaxKind: 'medicare', currentAmount: '29.00' },
      ],
      isUnbalancedAccepted: false,
    })
    expect(toast.done).toHaveBeenCalledWith(expect.stringContaining('Example Works'))
    expect(onClose).toHaveBeenCalled()
  })

  it('sends only the fields that fit a line kind', async () => {
    answers()
    renderDialog()
    const line = screen.getByRole('group', { name: 'finance.entryLineNumber {"number":1}' })
    expect(within(line).queryByLabelText('finance.entryPaystubTaxKind')).toBeNull()
    fireEvent.change(within(line).getByLabelText('finance.entryPaystubHours'), { target: { value: '40' } })
    fireEvent.change(within(line).getByLabelText('finance.entryLineKind'), { target: { value: 'deduction' } })
    expect(within(line).queryByLabelText('finance.entryPaystubHours')).toBeNull()
    expect(within(line).getByLabelText('finance.entryPaystubDeductionKind')).toBeTruthy()
  })

  it('says a refusal in a toast and stays open', async () => {
    answers()
    const { onClose } = renderDialog()
    execute.mockImplementation((document: string) =>
      document.includes('ReportingCurrency')
        ? Promise.resolve({ ReportingCurrency: { reportingCurrencyCode: 'USD', isChosen: true } })
        : Promise.reject(new Error('the lines do not add up')),
    )
    fireEvent.change(screen.getByLabelText('finance.entryEmployerName'), { target: { value: 'Example Works' } })
    fireEvent.change(screen.getByLabelText('finance.entryEmployeeName'), { target: { value: 'Sample Person' } })
    fireEvent.change(screen.getByLabelText('finance.entryPayDate'), { target: { value: '2026-03-15' } })
    fireEvent.change(screen.getByLabelText('finance.entryGrossAmount'), { target: { value: '10' } })
    fireEvent.change(screen.getByLabelText('finance.entryNetAmount'), { target: { value: '9' } })
    fireEvent.change(screen.getByTestId('entry-source-file'), {
      target: { files: [new File(['pdf'], 'paystub.pdf', { type: 'application/pdf' })] },
    })
    const submit = screen.getByRole('button', { name: 'finance.entryRecordPaystub' })
    await waitFor(() => expect(submit.hasAttribute('disabled')).toBe(false))
    fireEvent.click(submit)
    await waitFor(() => expect(toast.failure).toHaveBeenCalled())
    expect(onClose).not.toHaveBeenCalled()
  })

  it('offers to record an unbalanced paystub only after the server refused it, and takes the offer back when the form changes', async () => {
    answers()
    renderDialog()
    expect(screen.queryByLabelText('finance.entryUnbalancedAccepted')).toBeNull()
    execute.mockImplementation((document: string) =>
      document.includes('ReportingCurrency')
        ? Promise.resolve({ ReportingCurrency: { reportingCurrencyCode: 'USD', isChosen: true } })
        : Promise.reject(
            new Error(
              'the tax document was not recorded: its lines do not add up to its printed totals: misses by 1.00',
            ),
          ),
    )
    fireEvent.change(screen.getByLabelText('finance.entryEmployerName'), { target: { value: 'Example Works' } })
    fireEvent.change(screen.getByLabelText('finance.entryEmployeeName'), { target: { value: 'Sample Person' } })
    fireEvent.change(screen.getByLabelText('finance.entryPayDate'), { target: { value: '2026-03-15' } })
    fireEvent.change(screen.getByLabelText('finance.entryGrossAmount'), { target: { value: '10' } })
    fireEvent.change(screen.getByLabelText('finance.entryNetAmount'), { target: { value: '9' } })
    fireEvent.change(screen.getByTestId('entry-source-file'), {
      target: { files: [new File(['pdf'], 'paystub.pdf', { type: 'application/pdf' })] },
    })
    const submit = screen.getByRole('button', { name: 'finance.entryRecordPaystub' })
    await waitFor(() => expect(submit.hasAttribute('disabled')).toBe(false))
    fireEvent.click(submit)
    const checkbox = await screen.findByLabelText('finance.entryUnbalancedAccepted')
    fireEvent.click(checkbox)
    expect((checkbox as HTMLInputElement).checked).toBe(true)
    fireEvent.change(screen.getByLabelText('finance.entryNetAmount'), { target: { value: '8' } })
    expect(screen.queryByLabelText('finance.entryUnbalancedAccepted')).toBeNull()
  })

  it('shows each line its number and ignores a close while a request is in flight', async () => {
    answers()
    const { onClose } = renderDialog()
    const line = screen.getByRole('group', { name: 'finance.entryLineNumber {"number":1}' })
    expect(within(line).getByText('finance.entryLineNumber {"number":1}')).toBeTruthy()
    fireEvent.change(screen.getByLabelText('finance.entryEmployerName'), { target: { value: 'Example Works' } })
    fireEvent.change(screen.getByLabelText('finance.entryEmployeeName'), { target: { value: 'Sample Person' } })
    fireEvent.change(screen.getByLabelText('finance.entryPayDate'), { target: { value: '2026-03-15' } })
    fireEvent.change(screen.getByLabelText('finance.entryGrossAmount'), { target: { value: '10' } })
    fireEvent.change(screen.getByLabelText('finance.entryNetAmount'), { target: { value: '9' } })
    fireEvent.change(screen.getByTestId('entry-source-file'), {
      target: { files: [new File(['pdf'], 'paystub.pdf', { type: 'application/pdf' })] },
    })
    const submit = screen.getByRole('button', { name: 'finance.entryRecordPaystub' })
    await waitFor(() => expect(submit.hasAttribute('disabled')).toBe(false))
    execute.mockImplementation(() => new Promise(() => {}))
    fireEvent.click(submit)
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).not.toHaveBeenCalled()
  })
})
