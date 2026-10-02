import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { MailboxPage } from './mailbox'

const fixture = vi.hoisted(() => {
  // Read when the modules load, before any test runs.
  window.matchMedia = ((query: string) => ({
    matches: true,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
  })) as unknown as typeof window.matchMedia
  const inbox = { id: 'folder-inbox', mailboxId: 'mailbox-one', name: 'Inbox', kind: 'inbox', unread: 0, total: 120 }
  const view = {
    mailbox: { id: 'mailbox-one', addresses: [{ address: 'reader@example.com' }] },
    folders: [inbox],
    unread: 0,
    starredUnread: 0,
    priorityUnread: 0,
  }
  return { inbox, view }
})
vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
  setAgentViewing: vi.fn(),
}))
vi.mock('../mailboxes', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../mailboxes')>()),
  useMailboxes: () => ({
    views: [fixture.view],
    current: fixture.view,
    loaded: true,
    error: null,
    refresh: vi.fn(async () => {}),
    setCurrentId: vi.fn(),
  }),
}))
vi.mock('../session', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../session')>()),
  useSession: () => ({ name: 'reader', permissions: null }),
}))
vi.mock('./agent', () => ({ useAgent: () => ({ data: null, loading: false, error: null, reload: vi.fn() }) }))
vi.mock('../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, unknown>) => (values ? `${key} ${JSON.stringify(values)}` : key),
    plural: (count: number, forms: { one: string; other: string }) =>
      `${count === 1 ? forms.one : forms.other} ${count}`,
    language: 'en',
  }),
}))
vi.mock('../components/toast', () => ({ useToast: () => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }) }))
const execute = vi.mocked(graphql)

// A conversation of the invented inbox, its subject saying where it is.
function thread(index: number) {
  return {
    threadId: `thread-${index}`,
    item: {
      id: `item-${index}`,
      folderId: 'folder-inbox',
      mailId: `mail-${index}`,
      mail: { id: `mail-${index}`, subject: `Invented subject ${index}`, from: 'writer@example.net' },
      uid: index,
      seen: true,
      flagged: false,
      answered: false,
      forwarded: false,
      draft: false,
      addedAt: '2026-09-15T12:00:00Z',
    },
    count: 1,
    unread: 0,
    flagged: false,
    participants: ['writer@example.net'],
    itemIds: [`item-${index}`],
    hasDraft: false,
  }
}

function Address() {
  const location = useLocation()
  return <output data-testid="address">{location.pathname + location.search}</output>
}
const address = () => screen.getByTestId('address').textContent ?? ''

// threadReads is the offset and search of every read of the list.
const threadReads = () =>
  execute.mock.calls
    .filter(([document]) => document.includes('ListMailboxThreads('))
    .map(([, variables]) => [variables?.offset, variables?.first, variables?.search])

beforeEach(() => {
  execute.mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('ListMailboxThreads(')) {
      const offset = Number(variables?.offset ?? 0)
      const total = variables?.search ? 3 : 120
      const count = Math.max(0, Math.min(50, total - offset))
      return {
        ListMailboxThreads: { threads: Array.from({ length: count }, (_, index) => thread(offset + index)), total },
      }
    }
    return {}
  })
})
afterEach(() => {
  cleanup()
  execute.mockReset()
})

function renderAt(entry: string) {
  render(
    <MemoryRouter initialEntries={[entry]}>
      <Routes>
        <Route path="/mailbox/:folderId" element={<MailboxPage />} />
        <Route path="/mailbox/:folderId/:itemId" element={<MailboxPage />} />
      </Routes>
      <Address />
    </MemoryRouter>,
  )
}

// The list pages rather than growing: the page is in the address, the
// range says which rows of how many, previous and next move between pages,
// and a message opened from a page keeps the page beside it.
it('pages the mailbox list with the page in the address', async () => {
  renderAt('/mailbox/folder-inbox')
  expect(await screen.findByText('Invented subject 0')).toBeTruthy()
  expect(screen.getByText('table.range {"first":"1","last":"50","total":"120"}')).toBeTruthy()
  expect((screen.getByRole('button', { name: 'table.previous' }) as HTMLButtonElement).disabled).toBe(true)

  fireEvent.click(screen.getByRole('button', { name: 'table.next' }))
  expect(await screen.findByText('Invented subject 50')).toBeTruthy()
  expect(screen.queryByText('Invented subject 0')).toBeNull()
  expect(address()).toBe('/mailbox/folder-inbox?page=2')
  expect(screen.getByText('table.range {"first":"51","last":"100","total":"120"}')).toBeTruthy()

  fireEvent.click(screen.getByRole('button', { name: 'table.next' }))
  expect(await screen.findByText('Invented subject 100')).toBeTruthy()
  expect((screen.getByRole('button', { name: 'table.next' }) as HTMLButtonElement).disabled).toBe(true)

  fireEvent.click(screen.getByRole('button', { name: 'table.previous' }))
  expect(await screen.findByText('Invented subject 50')).toBeTruthy()
  expect(threadReads().map(([offset, first]) => [offset, first])).toEqual([
    [0, 50],
    [50, 50],
    [100, 50],
    [50, 50],
  ])
  const row = screen.getByText('Invented subject 51').closest('a')
  expect(row?.getAttribute('href')).toBe('/mailbox/folder-inbox/item-51?page=2')
})

// A page past the end, from an old link, goes to the last page; a search
// starts again at the first.
it('keeps the page in range and starts a search at the first page', async () => {
  renderAt('/mailbox/folder-inbox?page=9')
  await waitFor(() => expect(address()).toBe('/mailbox/folder-inbox?page=3'))
  expect(await screen.findByText('Invented subject 100')).toBeTruthy()

  const box = screen.getByRole('searchbox') as HTMLInputElement
  fireEvent.change(box, { target: { value: 'invented' } })
  fireEvent.submit(box.closest('form') as HTMLFormElement)
  await waitFor(() => expect(address()).toBe('/mailbox/folder-inbox?q=invented'))
  expect(await screen.findByText('table.range {"first":"1","last":"3","total":"3"}')).toBeTruthy()
  expect(threadReads()[threadReads().length - 1]).toEqual([0, 50, 'invented'])
})
