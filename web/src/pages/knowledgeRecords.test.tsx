import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import type { RecordLink } from '../memoryLinks'
import { LinkedRecords } from './knowledgeRecords'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))

beforeEach(() => {
  vi.mocked(graphql).mockReset()
})
afterEach(cleanup)

function link(overrides: Partial<RecordLink> = {}): RecordLink {
  return {
    id: 'link-1',
    nodePath: 'people/fixture-guest',
    nodeName: 'Fixture Guest',
    nodeKind: 'person',
    recordKind: 'mail',
    recordId: 'item-1',
    recordRelation: 'wrote',
    linkStatus: 'stated',
    passageStartMS: 0,
    passageEndMS: 0,
    transcriptRunId: '',
    passageCount: 0,
    note: '',
    createdAt: '2026-01-01T00:00:00Z',
    recordTitle: 'Fixture subject',
    dashboardPath: '/mailbox/starred/item-1',
    isUnavailable: false,
    ...overrides,
  }
}

function show(initialLinks: RecordLink[], kind = 'person', contactId = '', linkCount = initialLinks.length) {
  const onChanged = vi.fn()
  const onDone = vi.fn()
  const onFailed = vi.fn()
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter>
          <LinkedRecords
            path="people/fixture-guest"
            kind={kind}
            contactId={contactId}
            initialLinks={initialLinks}
            linkCount={linkCount}
            onChanged={onChanged}
            onDone={onDone}
            onFailed={onFailed}
          />
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
  return { onChanged, onDone, onFailed }
}

const links = [
  link({
    id: 'card',
    recordKind: 'contact',
    recordId: 'card-1',
    recordRelation: 'is_contact',
    recordTitle: 'Fixture Guest',
    dashboardPath: '/contacts?contact=card-1',
  }),
  link({ id: 'mail-1', recordRelation: 'wrote', recordHappenedAt: '2026-03-04T10:00:00Z' }),
  link({
    id: 'event-1',
    recordKind: 'calendar_event',
    recordId: 'event-1',
    recordRelation: 'attended',
    linkStatus: 'proposed',
    recordTitle: 'Fixture planning',
    dashboardPath: '/calendar?event=event-1',
  }),
  link({
    id: 'gone',
    recordKind: 'finance_receipt',
    recordId: 'receipt-1',
    recordRelation: 'receipt_for',
    recordTitle: '',
    dashboardPath: '/finance/receipts?receipt=receipt-1',
    isUnavailable: true,
  }),
]

it('groups the records by kind, each opening the record, a guess marked and a lost record not a link', () => {
  show(links)
  expect(screen.getByRole('heading', { name: /Linked records/ })).toBeTruthy()
  const headings = screen.getAllByRole('heading', { level: 4 }).map((heading) => heading.textContent)
  expect(headings).toEqual(['Contact card1', 'Mail1', 'Calendar event1', 'Receipt1'])
  expect(screen.getByRole('link', { name: 'Fixture subject' }).getAttribute('href')).toBe('/mailbox/starred/item-1')
  expect(screen.getByRole('link', { name: 'Fixture planning' }).getAttribute('href')).toBe('/calendar?event=event-1')
  expect(screen.getByRole('link', { name: 'Fixture Guest' }).getAttribute('href')).toBe('/contacts?contact=card-1')
  // Only the guess says Perhaps, and only it can be confirmed.
  expect(screen.getAllByText('Perhaps')).toHaveLength(1)
  expect(screen.getAllByRole('button', { name: /Confirm link/ })).toHaveLength(1)
  expect(screen.getAllByRole('button', { name: /Remove link/ })).toHaveLength(4)
  expect(screen.getByText('No longer available').closest('a')).toBeNull()
})

it('confirms a guess by stating it, with the record named exactly', async () => {
  vi.mocked(graphql).mockResolvedValue({ LinkAgentRecord: { id: 'link-1' } } as never)
  const { onChanged, onDone } = show(links)
  fireEvent.click(screen.getByRole('button', { name: 'Fixture planning: Confirm link' }))
  await waitFor(() => expect(onChanged).toHaveBeenCalled())
  const [document, variables] = vi.mocked(graphql).mock.calls[0]
  expect(document).toContain('LinkAgentRecord')
  expect(variables).toMatchObject({
    path: 'people/fixture-guest',
    recordKind: 'calendar_event',
    recordId: 'event-1',
    recordRelation: 'attended',
    passageStartMS: null,
    passageEndMS: null,
  })
  expect(onDone).toHaveBeenCalledWith('Link confirmed')
})

it('takes a guess away at once, and asks before taking away what somebody stated', async () => {
  vi.mocked(graphql).mockResolvedValue({ UnlinkAgentRecord: true } as never)
  const { onChanged } = show(links)
  fireEvent.click(screen.getByRole('button', { name: 'Fixture planning: Remove link' }))
  await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(1))
  expect(vi.mocked(graphql).mock.calls[0][1]).toMatchObject({
    recordKind: 'calendar_event',
    recordId: 'event-1',
    recordRelation: 'attended',
  })

  vi.mocked(graphql).mockClear()
  fireEvent.click(screen.getByRole('button', { name: 'Fixture subject: Remove link' }))
  expect(vi.mocked(graphql)).not.toHaveBeenCalled()
  const dialog = await screen.findByRole('alertdialog')
  expect(within(dialog).getByText(/The agent will not make it again/)).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Remove link' }))
  await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(2))
  expect(vi.mocked(graphql).mock.calls[0][1]).toMatchObject({
    recordKind: 'mail',
    recordId: 'item-1',
    recordRelation: 'wrote',
  })
})

function recordingRow(overrides: Partial<RecordLink> = {}): RecordLink {
  return link({
    id: 'passage',
    recordKind: 'recording_passage',
    recordId: 'recording-1',
    recordRelation: 'spoke_in',
    linkStatus: 'proposed',
    passageStartMS: 1500,
    passageEndMS: 4000,
    passageCount: 37,
    transcriptRunId: 'run-1',
    recordTitle: 'Fixture review',
    dashboardPath: '/recordings/recording-1?startMS=1500&endMS=4000&version=run-1',
    ...overrides,
  })
}

it('lists a recording once with how many passages it covers, opening at the first', () => {
  show([recordingRow()])
  expect(screen.getAllByText('Fixture review')).toHaveLength(1)
  expect(screen.getByText('Spoke in · 37 passages')).toBeTruthy()
  expect(screen.getByText('Fixture review').closest('a')?.getAttribute('href')).toBe(
    '/recordings/recording-1?startMS=1500&endMS=4000&version=run-1',
  )
})

it('says nothing of passages for a recording linked at one', () => {
  show([recordingRow({ passageCount: 1 })])
  expect(screen.getByText('Spoke in')).toBeTruthy()
})

it('confirms every passage of a recording with one call that names no range', async () => {
  vi.mocked(graphql).mockResolvedValue({ ConfirmAgentRecord: 37 } as never)
  show([recordingRow()])
  fireEvent.click(screen.getByRole('button', { name: 'Fixture review: Confirm link' }))
  await waitFor(() => expect(vi.mocked(graphql)).toHaveBeenCalled())
  const [document, variables] = vi.mocked(graphql).mock.calls[0]
  expect(document).toContain('ConfirmAgentRecord')
  expect(variables).toEqual({
    path: 'people/fixture-guest',
    recordKind: 'recording_passage',
    recordId: 'recording-1',
    recordRelation: 'spoke_in',
  })
})

it('removes every passage of a recording with one call that names no range', async () => {
  vi.mocked(graphql).mockResolvedValue({ UnlinkAgentRecord: true } as never)
  show([recordingRow()])
  fireEvent.click(screen.getByRole('button', { name: 'Fixture review: Remove link' }))
  await waitFor(() => expect(vi.mocked(graphql)).toHaveBeenCalled())
  expect(vi.mocked(graphql).mock.calls[0][1]).toMatchObject({
    recordKind: 'recording_passage',
    recordId: 'recording-1',
    recordRelation: 'spoke_in',
    passageStartMS: null,
    passageEndMS: null,
  })
})

it('says nothing for a page that is not about a person and has no records, and offers a card for one that is', () => {
  const { unmount } = render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter>
          <LinkedRecords
            path="topics/fixture"
            kind="topic"
            initialLinks={[]}
            linkCount={0}
            onChanged={vi.fn()}
            onDone={vi.fn()}
            onFailed={vi.fn()}
          />
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
  expect(screen.queryByRole('heading', { name: /Linked records/ })).toBeNull()
  unmount()
  show([], 'person')
  expect(screen.getByText('Nothing of yours is linked to this page yet.')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Link contact' })).toBeTruthy()
})

it('links a contact chosen from the picker, and unlinks the one it has', async () => {
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('ListAddressBooks')) return { ListAddressBooks: [{ id: 'book-1', name: 'Mine' }] } as never
    if (document.includes('ListContacts'))
      return {
        ListContacts: [
          { id: 'card-1', name: 'Fixture Guest', organization: '', emails: ['guest@example.com'] },
          { id: 'card-2', name: 'Other Guest', organization: '', emails: [] },
        ],
      } as never
    return { SetAgentNodeContact: true } as never
  })
  const { onDone } = show([], 'person')
  fireEvent.click(screen.getByRole('button', { name: 'Link contact' }))
  const dialog = await screen.findByRole('dialog')
  const choice = await within(dialog).findByLabelText(/Other Guest/)
  fireEvent.click(choice)
  fireEvent.click(within(dialog).getByRole('button', { name: 'Link contact' }))
  await waitFor(() => expect(onDone).toHaveBeenCalledWith('Contact linked'))
  const call = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('SetAgentNodeContact'))
  expect(call?.[1]).toEqual({ path: 'people/fixture-guest', contactId: 'card-2' })
})

it('clears the contact when asked to unlink it', async () => {
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('ListAddressBooks')) return { ListAddressBooks: [{ id: 'book-1', name: 'Mine' }] } as never
    if (document.includes('ListContacts')) return { ListContacts: [] } as never
    return { SetAgentNodeContact: true } as never
  })
  const { onDone } = show([], 'person', 'card-1')
  fireEvent.click(screen.getByRole('button', { name: 'Link contact' }))
  const dialog = await screen.findByRole('dialog')
  fireEvent.click(within(dialog).getByRole('button', { name: 'Unlink contact' }))
  await waitFor(() => expect(onDone).toHaveBeenCalledWith('Contact unlinked'))
  const call = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('SetAgentNodeContact'))
  expect(call?.[1]).toEqual({ path: 'people/fixture-guest', contactId: null })
})

it('reads a later part of a long list from the server', async () => {
  vi.mocked(graphql).mockResolvedValue({
    ListAgentRecordLinks: {
      recordLinks: [link({ id: 'later', recordTitle: 'Later subject' })],
      recordLinkCount: 120,
      nextOffset: 0,
    },
  } as never)
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter initialEntries={['/knowledge/people/fixture-guest?links=2']}>
          <LinkedRecords
            path="people/fixture-guest"
            kind="person"
            initialLinks={[link({ id: 'first', recordTitle: 'First subject' })]}
            linkCount={120}
            onChanged={vi.fn()}
            onDone={vi.fn()}
            onFailed={vi.fn()}
          />
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
  expect(await screen.findByRole('link', { name: 'Later subject' })).toBeTruthy()
  expect(screen.queryByRole('link', { name: 'First subject' })).toBeNull()
  expect(vi.mocked(graphql).mock.calls[0][1]).toMatchObject({ path: 'people/fixture-guest', first: 50, offset: 50 })
})
