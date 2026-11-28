import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { graphql, openAgentConversation } from '../../api'
import { FinanceImport } from './financeApi'
import { FinanceImportsSection } from './financeImports'

vi.mock('../../api', () => ({
  graphql: vi.fn(),
  subscribe: vi.fn(() => () => undefined),
  openAgentConversation: vi.fn(() => true),
}))
vi.mock('../../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) => (values ? `${key} ${JSON.stringify(values)}` : key),
    plural: (count: number, forms: { one: string; other: string }) => (count === 1 ? forms.one : forms.other),
    language: 'en',
  }),
}))
vi.mock('../../components/toast', () => ({ useToast: () => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }) }))
const execute = vi.mocked(graphql)

beforeEach(() => {
  window.matchMedia = ((query: string) => ({
    matches: true,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  })) as unknown as typeof window.matchMedia
})
afterEach(() => {
  cleanup()
  execute.mockReset()
})

const longOutcome =
  'Added 12 transactions to Example Bank checking, 3 were already there. The running balance matched the last row, so nothing needed your check. This sentence keeps going so that a cut-off would show.'

const finished: FinanceImport = {
  agentJobId: 'job-1',
  financeImportKind: 'statement',
  financeImportSource: 'upload',
  agentJobStatus: 'done',
  fileName: 'picture-1.jpg',
  queuedAt: '2026-06-03T12:00:00Z',
  finishedAt: '2026-06-03T12:01:00Z',
  outcomeText: longOutcome,
  runConversationId: 'conversation-1',
}
const waiting: FinanceImport = {
  agentJobId: 'job-2',
  financeImportKind: 'receipt',
  financeImportSource: 'photo',
  agentJobStatus: 'queued',
  fileName: '',
  queuedAt: '2026-06-03T12:05:00Z',
  finishedAt: '',
  outcomeText: '',
  runConversationId: '',
}

function answerWith(financeImports: FinanceImport[], totalCount = financeImports.length) {
  execute.mockImplementation(async (_document, variables) => {
    const offset = Number(variables?.offset ?? 0)
    return {
      FinanceImports: {
        financeImports: financeImports.slice(offset, offset + Number(variables?.limit ?? 50)),
        hasMore: false,
        totalCount,
      },
    }
  })
}

function renderSection(address = '/finance/imports') {
  render(
    <MemoryRouter initialEntries={[address]}>
      <FinanceImportsSection />
    </MemoryRouter>,
  )
}

describe('the Imports section', () => {
  it('shows what each file was read as, where it came from, its status and the whole outcome', async () => {
    answerWith([finished, waiting])
    renderSection()
    expect(await screen.findByText('picture-1.jpg')).toBeTruthy()
    expect(screen.getByText(/finance\.importKind\.statement/)).toBeTruthy()
    expect(screen.getByText(/finance\.importSource\.upload/)).toBeTruthy()
    expect(screen.getByText('finance.importState.done')).toBeTruthy()
    // The outcome is whole, not cut with an ellipsis.
    expect(screen.getByText(longOutcome)).toBeTruthy()
    // A file with no name and no outcome yet says so.
    expect(screen.getByText('finance.importNoFileName')).toBeTruthy()
    expect(screen.getByText('finance.importState.queued')).toBeTruthy()
    expect(screen.getByText('finance.importNoOutcome')).toBeTruthy()
  })

  it('opens the run of a file that has been read, and only of one', async () => {
    answerWith([finished, waiting])
    renderSection()
    await screen.findByText('picture-1.jpg')
    const buttons = screen.getAllByRole('button', { name: /finance\.importOpenRunOf/ })
    expect(buttons).toHaveLength(1)
    fireEvent.click(buttons[0])
    expect(vi.mocked(openAgentConversation)).toHaveBeenCalledWith('conversation-1')
  })

  it('says so in one line when nothing has been read', async () => {
    answerWith([])
    renderSection()
    expect(await screen.findByText('finance.importsEmpty')).toBeTruthy()
  })

  it('asks for the page the address names', async () => {
    const many = Array.from({ length: 120 }, (_unused, index) => ({
      ...finished,
      agentJobId: `job-${index}`,
      fileName: `picture-${index}.jpg`,
    }))
    answerWith(many)
    renderSection('/finance/imports?page=2')
    await waitFor(() =>
      expect(execute).toHaveBeenCalledWith(expect.stringContaining('FinanceImports'), { limit: 50, offset: 50 }),
    )
    expect(await screen.findByText('picture-50.jpg')).toBeTruthy()
    expect(screen.queryByText('picture-0.jpg')).toBeNull()
  })

  it('moves to the next page and writes it in the address', async () => {
    const many = Array.from({ length: 120 }, (_unused, index) => ({
      ...finished,
      agentJobId: `job-${index}`,
      fileName: `picture-${index}.jpg`,
    }))
    answerWith(many)
    renderSection()
    await screen.findByText('picture-0.jpg')
    fireEvent.click(screen.getByRole('button', { name: /next/i }))
    expect(await screen.findByText('picture-50.jpg')).toBeTruthy()
  })
})
