import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { uploadFiles } from '../../upload'
import { FinanceStatementImportSection, isPictureOrPdf } from './financeStatementImport'

const toast = vi.hoisted(() => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }))
vi.mock('../../api', () => ({
  graphql: vi.fn(),
  askAgentAbout: vi.fn(),
  subscribe: vi.fn(() => () => undefined),
}))
vi.mock('../../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) => (values ? `${key} ${JSON.stringify(values)}` : key),
    plural: (count: number, forms: { one: string; other: string }) => (count === 1 ? forms.one : forms.other),
    language: 'en',
  }),
}))
vi.mock('../../components/toast', () => ({ useToast: () => toast }))
vi.mock('../../upload', () => ({ uploadFiles: vi.fn() }))
vi.mock('./financeImportPreview', () => ({ TransactionRowsDialog: () => null }))
const execute = vi.mocked(graphql)
const upload = vi.mocked(uploadFiles)

afterEach(() => {
  cleanup()
  execute.mockReset()
  upload.mockReset()
  toast.done.mockReset()
  toast.failure.mockReset()
})

function prepare() {
  upload.mockReturnValue({ promise: Promise.resolve({ attachments: [{ id: 'attachment-1' }] }) } as never)
  execute.mockImplementation(async (document) => {
    if (document.includes('ReadStatement('))
      return { ReadStatement: { agentAttachmentId: 'attachment-1', agentJobId: 'job-1' } }
    if (document.includes('ImportStatement(')) {
      return {
        ImportStatement: {
          importedAt: '2026-06-03T12:00:00Z',
          statementImportOrigin: 'upload',
          statementFileNames: ['example.ofx'],
          addedTransactionCount: 1,
          updatedTransactionCount: 0,
          unchangedTransactionCount: 0,
          financeAccountNames: ['Example Bank checking'],
          importErrorMessage: '',
        },
      }
    }
    return {
      StatementImport: {
        sourceId: 'source-1',
        importAddress: 'me+token@example.com',
        isEnabled: true,
        maximumStatementBytes: 1000,
        lastStatementImport: null,
      },
    }
  })
  const { container } = render(
    <MemoryRouter>
      <FinanceStatementImportSection />
    </MemoryRouter>,
  )
  return container
}

async function choose(container: HTMLElement, file: File) {
  await screen.findByText('me+token@example.com')
  const picker = container.querySelector('input[type="file"]') as HTMLInputElement
  fireEvent.change(picker, { target: { files: [file] } })
}

describe('isPictureOrPdf', () => {
  it('tells pictures and PDFs from OFX files, by type and then by name', () => {
    expect(isPictureOrPdf({ name: 'picture-1.jpg', type: 'image/jpeg' })).toBe(true)
    expect(isPictureOrPdf({ name: 'statement-1.pdf', type: 'application/pdf' })).toBe(true)
    expect(isPictureOrPdf({ name: 'statement-1.PDF', type: '' })).toBe(true)
    expect(isPictureOrPdf({ name: 'example.ofx', type: 'application/x-ofx' })).toBe(false)
    expect(isPictureOrPdf({ name: 'example.qfx', type: '' })).toBe(false)
  })
})

describe('the statement import upload', () => {
  it('accepts pictures and PDFs as well as OFX files', () => {
    const container = prepare()
    const accept = (container.querySelector('input[type="file"]') as HTMLInputElement).accept
    expect(accept).toContain('image/*')
    expect(accept).toContain('application/pdf')
    expect(accept).toContain('.ofx')
  })

  it('imports an OFX file on the spot', async () => {
    const container = prepare()
    await choose(container, new File(['x'], 'example.ofx', { type: 'application/x-ofx' }))
    await waitFor(() => expect(toast.done).toHaveBeenCalled())
    expect(execute.mock.calls.some(([document]) => document.includes('ImportStatement('))).toBe(true)
    expect(execute.mock.calls.some(([document]) => document.includes('ReadStatement('))).toBe(false)
  })

  it('queues a picture to be read and says where the outcome shows', async () => {
    const container = prepare()
    // Larger than the limit for OFX files, which does not apply to a picture.
    await choose(container, new File([new Uint8Array(5000)], 'picture-1.jpg', { type: 'image/jpeg' }))
    await waitFor(() => expect(toast.done).toHaveBeenCalledWith('finance.statementReading {"name":"picture-1.jpg"}'))
    expect(execute).toHaveBeenCalledWith(expect.stringContaining('ReadStatement('), {
      agentAttachmentId: 'attachment-1',
    })
    expect(execute.mock.calls.some(([document]) => document.includes('ImportStatement('))).toBe(false)
    expect(toast.failure).not.toHaveBeenCalled()
  })

  it('reads a PDF the same way', async () => {
    const container = prepare()
    await choose(container, new File(['x'], 'statement-1.pdf', { type: 'application/pdf' }))
    await waitFor(() => expect(toast.done).toHaveBeenCalled())
    expect(execute.mock.calls.some(([document]) => document.includes('ReadStatement('))).toBe(true)
  })

  it('reports a refused file in a toast', async () => {
    const container = prepare()
    execute.mockImplementation(async (document) => {
      if (document.includes('ReadStatement(')) throw new Error('not a picture')
      return {
        StatementImport: {
          sourceId: 's',
          importAddress: 'me+token@example.com',
          isEnabled: true,
          maximumStatementBytes: 1000,
          lastStatementImport: null,
        },
      }
    })
    await choose(container, new File(['x'], 'picture-1.jpg', { type: 'image/jpeg' }))
    await waitFor(() => expect(toast.failure).toHaveBeenCalled())
  })
})
