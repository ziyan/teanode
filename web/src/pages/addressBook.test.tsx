import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { AddressBookPage } from './addressBook'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))

beforeEach(() => {
  vi.mocked(graphql).mockReset()
})
afterEach(cleanup)

let shownSearch = ''
let shownPath = ''
function SearchProbe() {
  const location = useLocation()
  shownSearch = location.search
  shownPath = location.pathname
  return null
}

function answer(hasPhoto = false) {
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('ListAddressBooks'))
      return { ListAddressBooks: [{ id: 'book-1', name: 'Mine', contacts: 1 }] } as never
    if (document.includes('ListContacts'))
      return {
        ListContacts: [
          { id: 'card-1', uid: 'u1', name: 'Fixture Guest', emails: ['guest@example.com'], phones: [] },
          { id: 'card-2', uid: 'u2', name: 'Linked Guest', emails: ['linked@example.com'], phones: [] },
        ],
      } as never
    if (document.includes('GetContact'))
      return {
        GetContact: {
          id: 'card-2',
          name: 'Linked Guest',
          organization: '',
          emails: ['linked@example.com'],
          phones: [],
          note: '',
          addresses: [],
          hasPhoto,
        },
      } as never
    if (document.includes('RemoveContactPicture'))
      return { RemoveContactPicture: { id: 'card-2', hasPhoto: false } } as never
    if (document.includes('ReadRecordMemoryLinks'))
      return {
        ReadRecordMemoryLinks: [
          {
            id: 'link-1',
            nodePath: 'people/linked-guest',
            nodeName: 'Linked Guest Page',
            nodeKind: 'person',
            recordKind: 'contact',
            recordId: 'card-2',
            recordRelation: 'is_contact',
            linkStatus: 'stated',
            passageStartMS: 0,
            passageEndMS: 0,
            transcriptRunId: '',
            note: '',
            createdAt: '2026-01-01T00:00:00Z',
            recordTitle: 'Linked Guest',
            dashboardPath: '/contacts?contact=card-2',
            isUnavailable: false,
          },
        ],
      } as never
    return { AgentGraphPage: { contact: null } } as never
  })
}

function show(address: string) {
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter initialEntries={[address]}>
          <Routes>
            <Route path="/contacts" element={<AddressBookPage />} />
            <Route path="*" element={null} />
          </Routes>
          <SearchProbe />
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
}

// The contact's form, opened by the pencil on its row.
async function openEditor(name: string) {
  show('/contacts')
  fireEvent.click(await screen.findByRole('button', { name: `${name}: Edit` }))
}

it('sends a link that names a contact to the contact\'s card, in the same place', async () => {
  answer()
  show('/contacts?contact=card-2')
  await waitFor(() => expect(shownPath).toBe('/people/contact/book-1/card-2'))
  expect(screen.queryByRole('dialog')).toBeNull()
})

it('opens a contact\'s card from its name, and its form from the pencil, with the pages it is in', async () => {
  answer()
  show('/contacts')
  const name = await screen.findByRole('link', { name: 'Linked Guest' })
  expect(name.getAttribute('href')).toBe('/people/contact/book-1/card-2')
  fireEvent.click(screen.getByRole('button', { name: 'Linked Guest: Edit' }))
  const dialog = await screen.findByRole('dialog')
  expect((within(dialog).getAllByRole('textbox')[0] as HTMLInputElement).value).toBe('Linked Guest')
  expect(await within(dialog).findByRole('link', { name: 'Linked Guest Page' })).toBeTruthy()
  const askedAbout = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('ReadRecordMemoryLinks'))
  expect(askedAbout?.[1]).toMatchObject({ recordKind: 'contact', recordId: 'card-2' })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
})

it('opens no card unless a link names one', async () => {
  answer()
  show('/contacts')
  await screen.findByText('Fixture Guest')
  expect(screen.queryByRole('dialog')).toBeNull()
  expect(vi.mocked(graphql).mock.calls.some(([document]) => document.includes('GetContact'))).toBe(false)
})

it('asks the server for the words in the address and says so over the table, which takes them off', async () => {
  answer()
  show('/contacts?search=guest')
  await screen.findByText('Fixture Guest')
  const asked = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('ListContacts'))
  expect(asked?.[1]).toMatchObject({ addressBookId: 'book-1', query: 'guest' })
  fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }))
  await waitFor(() => expect(shownSearch).not.toContain('search='))
  await waitFor(() => {
    const lastAsked = vi
      .mocked(graphql)
      .mock.calls.filter(([document]) => document.includes('ListContacts'))
      .pop()
    expect(lastAsked?.[1]).toMatchObject({ query: null })
  })
})

it('offers to remove the picture of a contact that has one, and only then', async () => {
  answer(false)
  await openEditor('Linked Guest')
  const dialog = await screen.findByRole('dialog')
  expect(within(dialog).queryByRole('button', { name: 'Remove picture' })).toBeNull()
  cleanup()

  answer(true)
  await openEditor('Linked Guest')
  const withPicture = await screen.findByRole('dialog')
  const removedPicture = () =>
    vi
      .mocked(graphql)
      .mock.calls.some(([document, variables]) => document.includes('RemoveContactPicture') && variables?.id === 'card-2')
  // It asks first, and a no leaves the picture where it was.
  fireEvent.click(await within(withPicture).findByRole('button', { name: 'Remove picture' }))
  const question = await screen.findByRole('alertdialog')
  expect(within(question).getByText(/Devices synchronizing your address book will lose it too/)).toBeTruthy()
  expect(removedPicture()).toBe(false)
  fireEvent.click(within(question).getByRole('button', { name: 'Cancel' }))
  await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull())
  expect(removedPicture()).toBe(false)
  expect(screen.getByRole('dialog')).toBeTruthy()

  fireEvent.click(await within(withPicture).findByRole('button', { name: 'Remove picture' }))
  fireEvent.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Remove picture' }))
  await waitFor(() => expect(removedPicture()).toBe(true))
  expect(await screen.findByText('Removed the picture of Linked Guest')).toBeTruthy()
  await waitFor(() => expect(within(withPicture).queryByRole('button', { name: 'Remove picture' })).toBeNull())
})

// ---- People in the circles ----

const sam = { userId: 'user-sam', username: 'sam', name: 'Sam Example', avatarUrl: null, avatarColorIndex: 2 }
const robin = { userId: 'user-robin', username: 'robin', name: 'Robin Example', avatarUrl: null, avatarColorIndex: 3 }

type CircleFixture = {
  linkedAccount?: typeof sam | null
  otherLinkedAccount?: typeof sam | null
  suggestions?: unknown[]
}

function answerWithCircles(fixture: CircleFixture = {}) {
  vi.mocked(graphql).mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('ListAddressBooks'))
      return { ListAddressBooks: [{ id: 'book-1', name: 'Mine', contacts: 2 }] } as never
    if (document.includes('ListContactAccountSuggestions'))
      return { ListContactAccountSuggestions: fixture.suggestions ?? [] } as never
    if (document.includes('ListContacts'))
      return {
        ListContacts: [
          {
            id: 'card-1',
            uid: 'u1',
            name: 'Fixture Guest',
            emails: ['guest@example.com'],
            phones: [],
            accountUserId: fixture.linkedAccount?.userId ?? null,
            account: fixture.linkedAccount ?? null,
          },
          {
            id: 'card-2',
            uid: 'u2',
            name: 'Second Guest',
            emails: [],
            phones: [],
            accountUserId: fixture.otherLinkedAccount?.userId ?? null,
            account: fixture.otherLinkedAccount ?? null,
          },
        ],
      } as never
    if (document.includes('GetContact'))
      return {
        GetContact: {
          id: 'card-1',
          name: 'Fixture Guest',
          organization: '',
          emails: ['guest@example.com'],
          phones: [],
          note: '',
          addresses: [],
          hasPhoto: false,
          accountUserId: fixture.linkedAccount?.userId ?? null,
          account: fixture.linkedAccount ?? null,
        },
      } as never
    if (document.includes('ListCirclePeople')) return { ListCirclePeople: [sam, robin] } as never
    if (document.includes('SetContactAccount')) return { SetContactAccount: { id: variables?.contactId } } as never
    if (document.includes('ReadRecordMemoryLinks')) return { ReadRecordMemoryLinks: [] } as never
    return { AgentGraphPage: { contact: null } } as never
  })
}

const setAccountCalls = () =>
  vi.mocked(graphql).mock.calls.filter(([document]) => document.includes('SetContactAccount'))

it('links a contact to a person in the circles from a picker, leaving out people another contact already is', async () => {
  answerWithCircles({ otherLinkedAccount: sam })
  window.localStorage.clear()
  await openEditor('Fixture Guest')
  const dialog = await screen.findByRole('dialog')
  fireEvent.click(await within(dialog).findByRole('button', { name: 'Is in your circles as…' }))
  const picker = (await screen.findAllByRole('dialog'))[1]
  expect(await within(picker).findByText('Robin Example (robin)')).toBeTruthy()
  expect(within(picker).queryByText('Sam Example (sam)')).toBeNull()
  fireEvent.click(within(picker).getByRole('radio'))
  fireEvent.click(within(picker).getByRole('button', { name: 'Link' }))
  await waitFor(() => expect(setAccountCalls()).toHaveLength(1))
  expect(setAccountCalls()[0][1]).toEqual({ addressBookId: 'book-1', contactId: 'card-1', accountUserId: 'user-robin' })
  expect(await screen.findByText('Fixture Guest is in your circles as Robin Example')).toBeTruthy()
  // The dialog now says who the contact is.
  expect(await within(dialog).findByText('In your circles as Robin Example')).toBeTruthy()
})

it('shows who a contact is in the circles, with Change and Remove, and removing sends no account', async () => {
  answerWithCircles({ linkedAccount: sam })
  await openEditor('Fixture Guest')
  const dialog = await screen.findByRole('dialog')
  expect(await within(dialog).findByText('In your circles as Sam Example')).toBeTruthy()
  expect(within(dialog).queryByRole('button', { name: 'Is in your circles as…' })).toBeNull()
  expect(within(dialog).getByRole('button', { name: 'Change' })).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Remove' }))
  await waitFor(() => expect(setAccountCalls()).toHaveLength(1))
  expect(setAccountCalls()[0][1]).toEqual({ addressBookId: 'book-1', contactId: 'card-1', accountUserId: null })
  expect(await screen.findByText('Fixture Guest is no longer linked to someone in your circles.')).toBeTruthy()
  expect(await within(dialog).findByRole('button', { name: 'Is in your circles as…' })).toBeTruthy()
})

it('says a refused link in a toast and keeps the dialog open', async () => {
  answerWithCircles()
  await openEditor('Fixture Guest')
  const dialog = await screen.findByRole('dialog')
  fireEvent.click(await within(dialog).findByRole('button', { name: 'Is in your circles as…' }))
  const picker = (await screen.findAllByRole('dialog'))[1]
  const normal = vi.mocked(graphql).getMockImplementation()!
  vi.mocked(graphql).mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('SetContactAccount')) throw new Error('Another contact is already linked to that person')
    return normal(document, variables)
  })
  fireEvent.click(await within(picker).findByText('Sam Example (sam)'))
  fireEvent.click(within(picker).getByRole('button', { name: 'Link' }))
  expect(await screen.findByText('Another contact is already linked to that person')).toBeTruthy()
  expect(screen.getAllByRole('dialog')).toHaveLength(2)
  // The refusal is the toast alone: no dialog shows it as inline text as well.
  expect(within(picker).queryByText('Another contact is already linked to that person')).toBeNull()
  expect(within(dialog).queryByText('Another contact is already linked to that person')).toBeNull()
})

it('offers suggestions at the top, links one, and remembers Not now for a contact in this browser only', async () => {
  window.localStorage.clear()
  const suggestions = [
    {
      addressBookId: 'book-1',
      contact: { id: 'card-1', name: 'Fixture Guest', emails: ['guest@example.com'] },
      account: sam,
    },
    { addressBookId: 'book-2', contact: { id: 'card-9', name: 'Second Guest', emails: [] }, account: robin },
  ]
  answerWithCircles({ suggestions })
  show('/contacts')
  const notice = await screen.findByRole('region', { name: 'Contacts that may be people in your circles' })
  expect(within(notice).getByText('Fixture Guest may be Sam Example in your circles')).toBeTruthy()
  expect(within(notice).getByText('Second Guest may be Robin Example in your circles')).toBeTruthy()

  // Not now hides that contact's offer and writes nothing to the server.
  const callsBefore = vi.mocked(graphql).mock.calls.length
  fireEvent.click(within(notice).getByRole('button', { name: 'Not now for Second Guest' }))
  await waitFor(() => expect(within(notice).queryByText(/Second Guest may be/)).toBeNull())
  expect(vi.mocked(graphql).mock.calls.length).toBe(callsBefore)
  expect(JSON.parse(window.localStorage.getItem('teanode.addressBook.dismissedSuggestions') ?? '[]')).toEqual([
    'card-9',
  ])

  fireEvent.click(within(notice).getByRole('button', { name: 'Link Fixture Guest' }))
  await waitFor(() => expect(setAccountCalls()).toHaveLength(1))
  expect(setAccountCalls()[0][1]).toEqual({ addressBookId: 'book-1', contactId: 'card-1', accountUserId: 'user-sam' })
  expect(await screen.findByText('Fixture Guest is in your circles as Sam Example')).toBeTruthy()
})

it('does not offer a suggestion the person already said Not now to, and works without browser storage', async () => {
  window.localStorage.setItem('teanode.addressBook.dismissedSuggestions', JSON.stringify(['card-1']))
  answerWithCircles({
    suggestions: [
      { addressBookId: 'book-1', contact: { id: 'card-1', name: 'Fixture Guest', emails: [] }, account: sam },
    ],
  })
  show('/contacts')
  await screen.findByText('Second Guest')
  expect(screen.queryByRole('region', { name: 'Contacts that may be people in your circles' })).toBeNull()
  cleanup()

  const getItem = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
    throw new Error('storage is blocked')
  })
  answerWithCircles({
    suggestions: [
      { addressBookId: 'book-1', contact: { id: 'card-1', name: 'Fixture Guest', emails: [] }, account: sam },
    ],
  })
  show('/contacts')
  expect(await screen.findByText('Fixture Guest may be Sam Example in your circles')).toBeTruthy()
  getItem.mockRestore()
})
