import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { uploadFiles } from '../../upload'
import { TaxFormEntryDialog } from './financeTaxFormEntry'

const toast = vi.hoisted(() => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }))
vi.mock('../../api', () => ({ graphql: vi.fn(), askAgentAbout: vi.fn() }))
vi.mock('../../upload', () => ({ uploadFiles: vi.fn() }))
vi.mock('../../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) => (values ? `${key} ${JSON.stringify(values)}` : key),
    plural: (count: number, forms: { one: string; other: string }) => (count === 1 ? forms.one : forms.other),
    language: 'en',
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
    if (document.includes('RecordTaxForm')) {
      return Promise.resolve({
        RecordTaxForm: { financeTaxForm: { id: 'form-invented' }, taxFormFindings: [] },
      })
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
      <TaxFormEntryDialog taxYear={2025} onRecorded={onRecorded} onClose={onClose} />
    </MemoryRouter>,
  )
  return { onRecorded, onClose }
}

describe('TaxFormEntryDialog', () => {
  it('records the form with its boxes and the file as a tax document', async () => {
    answers()
    const { onRecorded, onClose } = renderDialog()
    const submit = screen.getByRole('button', { name: 'finance.entryRecordTaxForm' })
    expect(submit.hasAttribute('disabled')).toBe(true)
    // The year of the page is the form's year until another is typed.
    expect((screen.getByLabelText('finance.taxYear') as HTMLInputElement).value).toBe('2025')

    fireEvent.change(screen.getByLabelText('finance.taxFormKind'), { target: { value: '1099_int' } })
    fireEvent.change(screen.getByLabelText('finance.entryPayerName'), { target: { value: 'Sample Bank' } })
    fireEvent.change(screen.getByLabelText('finance.entryRecipientName'), { target: { value: 'Sample Person' } })
    fireEvent.change(screen.getByLabelText('finance.entryRecipientTaxIdMask'), { target: { value: '4321' } })
    const first = screen.getByRole('group', { name: 'finance.entryBoxNumber {"number":1}' })
    fireEvent.change(within(first).getByLabelText('finance.entryBoxCode'), { target: { value: '1' } })
    fireEvent.change(within(first).getByLabelText('finance.entryBoxAmount'), { target: { value: '45.10' } })
    fireEvent.click(screen.getByRole('button', { name: 'finance.entryAddBox' }))
    const second = screen.getByRole('group', { name: 'finance.entryBoxNumber {"number":2}' })
    fireEvent.change(within(second).getByLabelText('finance.entryBoxCode'), { target: { value: '12' } })
    fireEvent.change(within(second).getByLabelText('finance.entryBoxQualifier'), { target: { value: 'D' } })
    fireEvent.click(within(second).getByLabelText('finance.entryBoxIsChecked'))
    fireEvent.change(screen.getByTestId('entry-source-file'), {
      target: { files: [new File(['pdf'], 'form.pdf', { type: 'application/pdf' })] },
    })
    await waitFor(() => expect(submit.hasAttribute('disabled')).toBe(false))
    expect(upload.mock.calls[0][1]).toContain('isTaxDocument=true')

    fireEvent.click(submit)
    await waitFor(() => expect(onRecorded).toHaveBeenCalled())
    const call = execute.mock.calls.find(([document]) => document.includes('RecordTaxForm'))
    expect(call?.[1]).toEqual({
      agentAttachmentId: 'attachment-invented',
      taxFormKind: '1099_int',
      taxYear: 2025,
      isCorrected: false,
      payerName: 'Sample Bank',
      recipientName: 'Sample Person',
      recipientTaxIdMask: '4321',
      taxFormEntries: [
        { boxCode: '1', amount: '45.10' },
        { boxCode: '12', boxQualifier: 'D', isChecked: true },
      ],
    })
    expect(toast.done).toHaveBeenCalledWith(expect.stringContaining('Sample Bank'))
    expect(onClose).toHaveBeenCalled()
  })

  it('says a refusal in a toast and stays open', async () => {
    answers()
    execute.mockImplementation(() => Promise.reject(new Error('the form is refused')))
    const { onClose } = renderDialog()
    fireEvent.change(screen.getByLabelText('finance.entryPayerName'), { target: { value: 'Sample Bank' } })
    fireEvent.change(screen.getByLabelText('finance.entryRecipientName'), { target: { value: 'Sample Person' } })
    fireEvent.change(screen.getByLabelText('finance.entryBoxCode'), { target: { value: '1' } })
    fireEvent.change(screen.getByTestId('entry-source-file'), {
      target: { files: [new File(['pdf'], 'form.pdf', { type: 'application/pdf' })] },
    })
    const submit = screen.getByRole('button', { name: 'finance.entryRecordTaxForm' })
    await waitFor(() => expect(submit.hasAttribute('disabled')).toBe(false))
    fireEvent.click(submit)
    await waitFor(() => expect(toast.failure).toHaveBeenCalled())
    expect(onClose).not.toHaveBeenCalled()
  })

  it('adds the findings that disagree to the toast', async () => {
    answers()
    execute.mockImplementation(() =>
      Promise.resolve({
        RecordTaxForm: {
          financeTaxForm: { id: 'form-invented' },
          taxFormFindings: [
            {
              taxFormFindingCode: 'a',
              isAgreeing: true,
              expectedAmount: '1',
              printedAmount: '1',
              findingText: 'Fine.',
            },
            {
              taxFormFindingCode: 'b',
              isAgreeing: false,
              expectedAmount: '1',
              printedAmount: '2',
              findingText: 'Medicare tax is off by 1.00.',
            },
          ],
        },
      }),
    )
    const { onClose } = renderDialog()
    fireEvent.change(screen.getByLabelText('finance.entryPayerName'), { target: { value: 'Sample Bank' } })
    fireEvent.change(screen.getByLabelText('finance.entryRecipientName'), { target: { value: 'Sample Person' } })
    fireEvent.change(screen.getByLabelText('finance.entryBoxCode'), { target: { value: '1' } })
    fireEvent.change(screen.getByTestId('entry-source-file'), {
      target: { files: [new File(['pdf'], 'form.pdf', { type: 'application/pdf' })] },
    })
    const submit = screen.getByRole('button', { name: 'finance.entryRecordTaxForm' })
    await waitFor(() => expect(submit.hasAttribute('disabled')).toBe(false))
    fireEvent.click(submit)
    await waitFor(() => expect(onClose).toHaveBeenCalled())
    const message = toast.done.mock.calls[0][0] as string
    expect(message).toContain('Medicare tax is off by 1.00.')
    expect(message).not.toContain('Fine.')
  })

  it('shows each box its number and ignores a close while a request is in flight', async () => {
    answers()
    const { onClose } = renderDialog()
    const box = screen.getByRole('group', { name: 'finance.entryBoxNumber {"number":1}' })
    expect(within(box).getByText('finance.entryBoxNumber {"number":1}')).toBeTruthy()
    fireEvent.change(screen.getByLabelText('finance.entryPayerName'), { target: { value: 'Sample Bank' } })
    fireEvent.change(screen.getByLabelText('finance.entryRecipientName'), { target: { value: 'Sample Person' } })
    fireEvent.change(screen.getByLabelText('finance.entryBoxCode'), { target: { value: '1' } })
    fireEvent.change(screen.getByTestId('entry-source-file'), {
      target: { files: [new File(['pdf'], 'form.pdf', { type: 'application/pdf' })] },
    })
    const submit = screen.getByRole('button', { name: 'finance.entryRecordTaxForm' })
    await waitFor(() => expect(submit.hasAttribute('disabled')).toBe(false))
    execute.mockImplementation(() => new Promise(() => {}))
    fireEvent.click(submit)
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).not.toHaveBeenCalled()
  })
})
