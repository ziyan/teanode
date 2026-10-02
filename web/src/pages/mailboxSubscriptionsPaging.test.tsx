import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { MailboxSubscriptionsPage } from './mailboxSubscriptions'

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
  const view = {
    mailbox: { id: 'mailbox-one', addresses: [{ address: 'reader@example.com' }] },
    folders: [{ id: 'folder-inbox', mailboxId: 'mailbox-one', name: 'Inbox', kind: 'inbox', unread: 0, total: 0 }],
    unread: 0,
    starredUnread: 0,
    priorityUnread: 0,
  }
  return { view }
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

// A mailing list of the invented mailbox, its name saying where it is.
function subscription(index: number) {
  return {
    id: `0000000000000000000000${String(index).padStart(4, '0')}`,
    key: `list-${index}.example.net`,
    name: `Invented list ${index}`,
    from: `news@list-${index}.example.net`,
    count: 3,
    unread: 0,
    lastAt: '2026-09-15T12:00:00Z',
    lastItemId: `item-${index}`,
    oneClick: true,
    unsubscribe: [],
    logoDomain: '',
  }
}

function Address() {
  const location = useLocation()
  return <output data-testid="address">{location.pathname + location.search}</output>
}
const address = () => screen.getByTestId('address').textContent ?? ''

const listReads = () =>
  execute.mock.calls
    .filter(([document]) => document.includes('ListMailboxSubscriptions('))
    .map(([, variables]) => [variables?.offset, variables?.first, variables?.matching])

beforeEach(() => {
  execute.mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('ListMailboxSubscriptions(')) {
      const offset = Number(variables?.offset ?? 0)
      const total = variables?.matching ? 2 : 70
      const count = Math.max(0, Math.min(50, total - offset))
      return {
        ListMailboxSubscriptions: {
          total,
          subscribed: total,
          left: 0,
          subscriptions: Array.from({ length: count }, (_, index) => subscription(offset + index)),
        },
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
        <Route path="/mailbox/subscriptions" element={<MailboxSubscriptionsPage />} />
        <Route path="/mailbox/subscriptions/:key" element={<MailboxSubscriptionsPage />} />
      </Routes>
      <Address />
    </MemoryRouter>,
  )
}

// The lists page with the page in the address, opening one keeps the page
// to come back to, and a search starts again at the first page.
it('pages the mailing lists with the page in the address', async () => {
  renderAt('/mailbox/subscriptions')
  expect(await screen.findByText('Invented list 0')).toBeTruthy()
  expect(screen.getByText('table.range {"first":"1","last":"50","total":"70"}')).toBeTruthy()

  fireEvent.click(screen.getByRole('button', { name: 'table.next' }))
  expect(await screen.findByText('Invented list 50')).toBeTruthy()
  expect(screen.queryByText('Invented list 0')).toBeNull()
  expect(address()).toBe('/mailbox/subscriptions?page=2')
  expect((screen.getByRole('button', { name: 'table.next' }) as HTMLButtonElement).disabled).toBe(true)

  fireEvent.click(screen.getByText('Invented list 55'))
  await waitFor(() => expect(address()).toBe(`/mailbox/subscriptions/${subscription(55).id}?page=2`))
  expect(screen.getByText('Invented list 52')).toBeTruthy()

  fireEvent.click(screen.getByRole('button', { name: 'table.previous' }))
  expect(await screen.findByText('Invented list 0')).toBeTruthy()

  const box = screen.getByRole('searchbox') as HTMLInputElement
  fireEvent.change(box, { target: { value: 'invented' } })
  fireEvent.submit(box.closest('form') as HTMLFormElement)
  expect(await screen.findByText('table.range {"first":"1","last":"2","total":"2"}')).toBeTruthy()
  expect(address()).toBe('/mailbox/subscriptions?matching=invented')
  expect(listReads()).toEqual([
    [0, 50, ''],
    [50, 50, ''],
    [0, 50, ''],
    [0, 50, 'invented'],
  ])
})
