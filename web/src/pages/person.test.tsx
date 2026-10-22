import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { APIError, graphql, openAgentConversation } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import type { PersonCard } from '../lib/personCard'
import { SessionProvider } from '../session'
import { PersonPage } from './person'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
  openAgentConversation: vi.fn(),
}))

beforeEach(() => {
  vi.mocked(openAgentConversation).mockReturnValue(true)
})
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

const TODAY = (() => {
  const now = new Date()
  const month = String(now.getMonth() + 1).padStart(2, '0')
  const day = String(now.getDate()).padStart(2, '0')
  return `${now.getFullYear()}-${month}-${day}`
})()

// Sam, who shares a circle with the caller and is the caller's mother. Every
// section is read unless a test says otherwise.
function sam(overrides: Partial<PersonCard> = {}): PersonCard {
  return {
    person: {
      userId: 'person-sam',
      username: 'sam-login',
      name: 'Sam Fixture',
      avatarUrl: null,
      avatarColorIndex: 4,
      memberRole: 'member',
      joinedAt: '2030-01-01T00:00:00Z',
      relationshipKind: 'parent',
      relationshipTermGender: 'feminine',
      relationshipWord: 'mother',
      linkedContactId: 'contact-sam',
    },
    contact: {
      id: 'contact-sam',
      addressBookId: 'book-1',
      name: 'Sam Fixture',
      organization: 'Fixture Works',
      emails: ['sam@example.com', 'sam.second@example.com'],
      phones: ['+1 555 0100'],
      addresses: [{ written: '1 Example Street, Exampletown' }],
      hasPhoto: false,
      accountUserId: 'person-sam',
    },
    sharedCircles: [{ circleId: 'circle-one', circleName: 'Fixture Home', circleKind: 'family' }],
    canMessage: true,
    location: {
      sectionState: 'shown',
      location: {
        userId: 'person-sam',
        username: 'sam-login',
        name: 'Sam Fixture',
        avatarUrl: null,
        avatarColorIndex: 4,
        shareLevel: 'history',
        latestPoint: {
          latitudeDegrees: 1,
          longitudeDegrees: 2,
          accuracyMeters: 5,
          measuredAt: new Date().toISOString(),
        },
        openVisit: {
          id: 'visit-open',
          arrivedAt: new Date().toISOString(),
          place: {
            id: 'place-park',
            placeName: 'Fixture Park',
            isNamedByPerson: true,
            placeLabel: '',
            latitudeDegrees: 1,
            longitudeDegrees: 2,
            radiusMeters: 50,
            osmName: '',
            osmCategory: '',
            addressText: '',
            isAddressTyped: false,
            visitCount: 1,
            lastVisitAt: null,
          },
        },
      },
      hasMoreTodayVisits: false,
      todayVisits: [
        {
          id: 'visit-earlier',
          arrivedAt: new Date(Date.now() - 4 * 3600_000).toISOString(),
          departedAt: new Date(Date.now() - 3 * 3600_000).toISOString(),
          durationSeconds: 3600,
          place: {
            id: 'place-shop',
            placeName: 'Fixture Shop',
            isNamedByPerson: true,
            placeLabel: '',
            latitudeDegrees: 1,
            longitudeDegrees: 2,
            radiusMeters: 50,
            osmName: '',
            osmCategory: '',
            addressText: '',
            isAddressTyped: false,
            visitCount: 1,
            lastVisitAt: null,
          },
        },
      ],
    },
    health: {
      sectionState: 'shown',
      healthTypes: [
        {
          healthType: 'stepCount',
          healthCategory: 'activity',
          typeName: 'Steps',
          valueKind: 'quantity',
          quantityUnit: 'count',
          summaryKind: 'sum',
          firstDay: TODAY,
          lastDay: TODAY,
          sampleCount: 3,
          latestDaySummary: {
            summaryDay: TODAY,
            sumValue: 8214,
            averageValue: null,
            minimumValue: null,
            maximumValue: null,
            sampleCount: 3,
            durationSeconds: null,
          },
        },
      ],
      hasMore: false,
    },
    finance: { sectionState: 'notShared', financeAccounts: [], hasMore: false },
    photos: {
      photosState: 'shown',
      photos: [
        {
          id: 'photo-one',
          photoKind: 'picture',
          photoProcessingState: 'ready',
          smallPreviewUrl: '/previews/photo-one.jpg',
          originalFilename: 'fixture.jpg',
          placeName: '',
          takenAt: '2030-02-03T04:05:06Z',
          createdAt: '2030-02-03T04:05:06Z',
        } as never,
      ],
      hasMorePhotos: false,
      albumsState: 'shown',
      albums: [
        {
          id: 'album-one',
          albumTitle: 'Fixture Trip',
          photoCount: 12,
          coverPreviewUrl: '/previews/cover.jpg',
        } as never,
      ],
      hasMoreAlbums: false,
    },
    mail: {
      sectionState: 'shown',
      mails: [
        {
          mailboxItemId: 'item-one',
          mailboxId: 'mailbox-one',
          folderPath: 'Inbox',
          subject: 'Fixture subject',
          fromAddress: 'sam@example.com',
          fromName: 'Sam Fixture',
          isFromPerson: true,
          isUnread: false,
          receivedAt: '2030-03-04T05:06:07Z',
          snippet: 'Fixture words',
        },
      ],
      hasMore: false,
    },
    recordings: {
      sectionState: 'shown',
      recordings: [
        {
          recordingId: 'recording-one',
          title: 'Fixture call',
          recordedAt: '2030-03-05T05:06:07Z',
          durationMS: 60000,
          spokenPassageCount: 12,
        },
      ],
      hasMore: false,
    },
    memory: {
      sectionState: 'shown',
      pages: [
        {
          nodePath: 'people/sam-fixture',
          nodeName: 'Sam Fixture',
          nodeKind: 'person',
          summary: 'Fixture summary of Sam.',
          updatedAt: '2030-03-06T05:06:07Z',
          isMatchedByName: false,
        },
      ],
      hasMore: false,
    },
    ...overrides,
  }
}

const TERMS = [
  { term: 'parent', relationshipKind: 'parent', termGender: 'neutral', word: 'parent' },
  { term: 'mother', relationshipKind: 'parent', termGender: 'feminine', word: 'mother' },
]

function answer(card: PersonCard | Error) {
  vi.mocked(graphql).mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('ReadPersonCard')) {
      if (card instanceof Error) throw card
      return { ReadPersonCard: card } as never
    }
    if (document.includes('OpenDirectChat')) return { OpenDirectChat: { id: 'chat-one' } } as never
    if (document.includes('GetMailboxItem')) {
      return { GetMailboxItem: { id: variables?.itemId, folderId: 'folder-inbox' } } as never
    }
    if (document.includes('GetContact')) {
      return {
        GetContact: {
          id: 'contact-sam',
          name: 'Sam Fixture',
          organization: '',
          emails: ['sam@example.com'],
          phones: [],
          note: '',
          addresses: [],
          hasPhoto: false,
        },
      } as never
    }
    if (document.includes('ListAddressBooks')) return { ListAddressBooks: [{ id: 'book-one' }] } as never
    if (document.includes('ListContactAccountSuggestions')) return { ListContactAccountSuggestions: [] } as never
    if (document.includes('ListContacts')) {
      return {
        ListContacts: [
          { id: 'contact-alex', name: 'Alex Fixture', emails: ['alex@example.com'], accountUserId: null },
          { id: 'contact-robin', name: 'Robin Fixture', emails: [], accountUserId: 'person-robin' },
        ],
      } as never
    }
    if (document.includes('SetContactAccount')) return { SetContactAccount: { id: variables?.contactId } } as never
    if (document.includes('ListRelationshipTerms')) return { ListRelationshipTerms: TERMS } as never
    if (document.includes('GetCurrentUser')) return { GetCurrentUser: { id: 'person-a', pronoun: 'she' } } as never
    if (document.includes('ReadRecordMemoryLinks')) return { ReadRecordMemoryLinks: [] } as never
    return {} as never
  })
}

function Where() {
  const location = useLocation()
  return <p data-testid="where">{location.pathname + location.search}</p>
}

function renderAt(path: string, permissions = ['circle:use', 'contacts:use', 'mail:read']) {
  return render(
    <TranslationProvider>
      <SessionProvider
        value={{
          userId: 'person-a',
          authenticated: true,
          authenticationRequired: true,
          username: 'person-a-login',
          passkeysEnabled: false,
          permissions: { everywhere: permissions, byDomain: [] },
          services: { isContactsEnabled: true, isMailboxEnabled: true } as never,
        }}
      >
        <ToastProvider>
          <MemoryRouter initialEntries={[path]}>
            <Routes>
              <Route path="/people/contact/:addressBookId/:contactId" element={<PersonPage />} />
              <Route path="/people/:userId" element={<PersonPage />} />
              <Route path="*" element={<Where />} />
            </Routes>
          </MemoryRouter>
        </ToastProvider>
      </SessionProvider>
    </TranslationProvider>,
  )
}

const asked = () => vi.mocked(graphql).mock.calls.find(([document]) => document.includes('ReadPersonCard'))

it('asks for the account in the address and draws the head and every section', async () => {
  answer(sam())
  renderAt('/people/person-sam')
  await screen.findByText('Your mother')
  expect(asked()?.[1]).toEqual({ userId: 'person-sam', addressBookId: null, contactId: null, itemLimit: null })
  expect(screen.getByText('sam-login', { exact: false })).toBeTruthy()

  // Each section is a panel with its name.
  const names = screen.getAllByRole('heading', { level: 3 }).map((heading) => heading.textContent)
  expect(names).toEqual([
    'Location',
    'Health',
    'Finance',
    'Photos',
    'Mail',
    'Recordings',
    'What it knows',
    'Contact details',
    'Circles',
    'Relationship',
  ])

  // Location: the visit they are in, and the earlier ones of today.
  expect(screen.getByText(/At Fixture Park since/)).toBeTruthy()
  expect(screen.getByText('Fixture Shop')).toBeTruthy()
  // Health: the latest day's value, said as today.
  expect(screen.getByText('Steps')).toBeTruthy()
  expect(screen.getByText('8,214 steps')).toBeTruthy()
  expect(screen.getByText('Today')).toBeTruthy()
  // Mail, recordings, memory.
  expect(screen.getByText('Fixture subject')).toBeTruthy()
  expect(screen.getByText('From Sam Fixture')).toBeTruthy()
  expect(screen.getByText('Fixture words')).toBeTruthy()
  expect(screen.getByText('Fixture call')).toBeTruthy()
  expect(screen.getByText('speaks in 12 passages')).toBeTruthy()
  expect(screen.getByText('Fixture summary of Sam.')).toBeTruthy()
  // Contact details.
  expect(screen.getByRole('link', { name: 'sam@example.com' }).getAttribute('href')).toBe('mailto:sam@example.com')
  expect(screen.getByText('1 Example Street, Exampletown')).toBeTruthy()
})

it('opens each part from its Show all and each item where it lives', async () => {
  answer(sam())
  renderAt('/people/person-sam')
  await screen.findByText('Your mother')
  const hrefs = screen.getAllByRole('link', { name: 'Show all' }).map((link) => link.getAttribute('href'))
  expect(hrefs).toEqual([
    '/location?person=person-sam',
    '/health?person=person-sam',
    '/photos?person=person-sam',
    `/mailbox?from=${encodeURIComponent('sam@example.com')}&everywhere=true`,
  ])
  expect(screen.getByRole('link', { name: /Fixture Trip/ }).getAttribute('href')).toBe('/photos/albums/album-one')
  expect(screen.getByRole('link', { name: /Fixture call/ }).getAttribute('href')).toBe('/recordings/recording-one')
  expect(screen.getByRole('link', { name: /Fixture summary of Sam/ }).getAttribute('href')).toBe(
    '/knowledge/people/sam-fixture',
  )
  expect(screen.getByRole('link', { name: /Fixture Home/ }).getAttribute('href')).toBe('/circles/circle-one')
  expect(screen.getByRole('link', { name: 'Open fixture.jpg' }).getAttribute('href')).toBe('/photos/photo-one')
})

it('shows the finance Show all only when more accounts are shared than the card lists', async () => {
  const account = {
    financeAccountId: 'account-one',
    financeShareLevel: 'balance',
    accountName: 'Fixture Checking',
    accountMask: '1234',
    currencyCode: 'USD',
    currentBalance: '10.00',
    creditUsage: null,
  }
  const financeLinks = () =>
    screen
      .queryAllByRole('link', { name: 'Show all' })
      .map((link) => link.getAttribute('href'))
      .filter((href) => href?.startsWith('/finance'))
  answer(sam({ finance: { sectionState: 'shown', financeAccounts: [account], hasMore: false } }))
  renderAt('/people/person-sam')
  await screen.findByText('Fixture Checking ··1234')
  expect(financeLinks()).toEqual([])
  cleanup()

  answer(sam({ finance: { sectionState: 'shown', financeAccounts: [account], hasMore: true } }))
  renderAt('/people/person-sam')
  await screen.findByText('Fixture Checking ··1234')
  expect(financeLinks()).toEqual(['/finance/spending?person=person-sam'])
})

it('opens a mail in the folder it is in', async () => {
  answer(sam())
  renderAt('/people/person-sam')
  fireEvent.click(await screen.findByRole('button', { name: /Fixture subject/ }))
  await waitFor(() => expect(screen.getByTestId('where').textContent).toBe('/mailbox/folder-inbox/item-one'))
})

it('opens the direct chat in the drawer from Message, and composes to the first address from Send mail', async () => {
  answer(sam())
  renderAt('/people/person-sam')
  fireEvent.click(await screen.findByRole('button', { name: 'Message' }))
  await waitFor(() => expect(openAgentConversation).toHaveBeenCalledWith('chat-one'))
  const opened = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('OpenDirectChat'))
  expect(opened?.[1]).toEqual({ userId: 'person-sam' })

  expect(screen.getByRole('link', { name: 'Send mail' }).getAttribute('href')).toBe(
    `/mailbox/compose?to=${encodeURIComponent('sam@example.com')}`,
  )
})

it('says it when the chat cannot be shown in the drawer', async () => {
  answer(sam())
  vi.mocked(openAgentConversation).mockReturnValue(false)
  renderAt('/people/person-sam')
  fireEvent.click(await screen.findByRole('button', { name: 'Message' }))
  expect(await screen.findByText('Could not open the chat.')).toBeTruthy()
})

it('leaves out Message when a chat cannot be opened, and Send mail when there is no address or no mail', async () => {
  answer(sam({ canMessage: false, contact: { ...sam().contact!, emails: [] } }))
  renderAt('/people/person-sam')
  await screen.findByText('Your mother')
  expect(screen.queryByRole('button', { name: 'Message' })).toBeNull()
  expect(screen.queryByRole('link', { name: 'Send mail' })).toBeNull()
  cleanup()

  answer(sam())
  renderAt('/people/person-sam', ['circle:use', 'contacts:use'])
  await screen.findByText('Your mother')
  expect(screen.queryByRole('link', { name: 'Send mail' })).toBeNull()
})

it('says in a line what a section cannot show, and leaves out one that is unavailable', async () => {
  answer(
    sam({
      location: { sectionState: 'notShared', location: null, todayVisits: [], hasMoreTodayVisits: false },
      health: { sectionState: 'failed', healthTypes: [], hasMore: false },
      mail: { sectionState: 'unavailable', mails: [], hasMore: false },
      recordings: null,
      memory: { sectionState: 'notShared', pages: [], hasMore: false },
    }),
  )
  renderAt('/people/person-sam')
  expect(await screen.findByText('Sam Fixture does not share where they are.')).toBeTruthy()
  expect(screen.getByText('Could not read this just now.')).toBeTruthy()
  expect(screen.queryByRole('heading', { name: 'Mail' })).toBeNull()
  expect(screen.queryByRole('heading', { name: 'Recordings' })).toBeNull()
  // A part that is not shown has no Show all.
  expect(screen.queryByRole('link', { name: 'Show all' })).toBeTruthy()
  expect(screen.getAllByRole('link', { name: 'Show all' }).map((link) => link.getAttribute('href'))).toEqual([
    '/photos?person=person-sam',
  ])
})

it('offers to link a contact when the sections need one, and links the one chosen', async () => {
  const card = sam({
    contact: null,
    photos: {
      photosState: 'needsContact',
      photos: [],
      hasMorePhotos: false,
      albumsState: 'shown',
      albums: [],
      hasMoreAlbums: false,
    },
    mail: { sectionState: 'needsContact', mails: [], hasMore: false },
    recordings: { sectionState: 'needsContact', recordings: [], hasMore: false },
    memory: { sectionState: 'needsContact', pages: [], hasMore: false },
  })
  answer(card)
  renderAt('/people/person-sam')
  await screen.findByText('Your mother')
  expect(screen.queryByRole('heading', { name: 'Contact details' })).toBeNull()
  expect(screen.queryByRole('button', { name: 'Edit contact' })).toBeNull()
  expect(screen.getAllByText('Link one of your contacts to Sam Fixture to see this.').length).toBeGreaterThan(0)

  fireEvent.click(screen.getAllByRole('button', { name: 'Link a contact' })[0])
  const dialog = await screen.findByRole('dialog')
  // A contact that is somebody else already is not offered.
  expect(await within(dialog).findByText('Alex Fixture')).toBeTruthy()
  expect(within(dialog).queryByText('Robin Fixture')).toBeNull()
  fireEvent.click(within(dialog).getByRole('radio'))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Link' }))
  await waitFor(() =>
    expect(vi.mocked(graphql).mock.calls.find(([document]) => document.includes('SetContactAccount'))?.[1]).toEqual({
      addressBookId: 'book-one',
      contactId: 'contact-alex',
      accountUserId: 'person-sam',
    }),
  )
  // The card is read again.
  await waitFor(() =>
    expect(vi.mocked(graphql).mock.calls.filter(([document]) => document.includes('ReadPersonCard'))).toHaveLength(2),
  )
})

it('opens the contact form from Edit contact and reads the card again when it is saved', async () => {
  answer(sam())
  renderAt('/people/person-sam')
  fireEvent.click(await screen.findByRole('button', { name: 'Edit contact' }))
  const dialog = await screen.findByRole('dialog')
  expect((within(dialog).getAllByRole('textbox')[0] as HTMLInputElement).value).toBe('Sam Fixture')
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('SaveContact')) return { SaveContact: { id: 'contact-sam', name: 'Sam Fixture' } } as never
    if (document.includes('ReadPersonCard')) return { ReadPersonCard: sam() } as never
    return {} as never
  })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  expect(vi.mocked(graphql).mock.calls.some(([document]) => document.includes('SaveContact'))).toBe(true)
})

it('asks for a contact by its book and id, and leaves out what hangs on an account it does not have', async () => {
  answer(
    sam({
      person: null,
      canMessage: false,
      sharedCircles: [],
      location: null,
      health: { sectionState: 'unavailable', healthTypes: [], hasMore: false },
      photos: {
        photosState: 'shown',
        photos: [],
        hasMorePhotos: false,
        albumsState: 'unavailable',
        albums: [],
        hasMoreAlbums: false,
      },
    }),
  )
  renderAt('/people/contact/book-one/contact-sam')
  await screen.findByRole('heading', { name: 'Contact details' })
  expect(asked()?.[1]).toEqual({ userId: null, addressBookId: 'book-one', contactId: 'contact-sam', itemLimit: null })
  expect(screen.queryByRole('heading', { name: 'Location' })).toBeNull()
  expect(screen.queryByRole('heading', { name: 'Health' })).toBeNull()
  expect(screen.queryByRole('heading', { name: 'Circles' })).toBeNull()
  expect(screen.queryByRole('heading', { name: 'Relationship' })).toBeNull()
  expect(screen.queryByRole('button', { name: 'Message' })).toBeNull()
  expect(screen.queryByRole('button', { name: 'Link a contact' })).toBeNull()
  // The photos of them have nowhere to go without an account, and no albums part is drawn.
  expect(screen.getByText('No photos of Sam Fixture yet.')).toBeTruthy()
  expect(screen.queryByText(/Albums .* shared with you/)).toBeNull()
  // The mail is still from their address.
  expect(screen.getByRole('link', { name: 'Show all' }).getAttribute('href')).toBe(
    `/mailbox?from=${encodeURIComponent('sam@example.com')}&everywhere=true`,
  )
})

it('opens the relationship dialog and saves only the pronoun when only the pronoun changed', async () => {
  answer(sam({ person: { ...sam().person!, relationshipKind: 'parent', relationshipTermGender: 'feminine' } }))
  vi.mocked(graphql).mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('ReadPersonCard')) return { ReadPersonCard: sam() } as never
    if (document.includes('ListRelationshipTerms')) return { ListRelationshipTerms: TERMS } as never
    // The caller has not said how they are described, so the dialog asks.
    if (document.includes('GetCurrentUser')) return { GetCurrentUser: { id: 'person-a', pronoun: '' } } as never
    if (document.includes('UpdateUser')) return { UpdateUser: { id: 'person-a', pronoun: variables?.pronoun } } as never
    return {} as never
  })
  renderAt('/people/person-sam')
  expect(await screen.findByText('Mother')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Sam Fixture: Relationship' }))
  const dialog = await screen.findByRole('dialog')
  const pronoun = await within(dialog).findByLabelText('How should people describe you?')
  fireEvent.change(pronoun, { target: { value: 'she' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(vi.mocked(graphql).mock.calls.some(([document]) => document.includes('UpdateUser'))).toBe(true),
  )
  expect(vi.mocked(graphql).mock.calls.some(([document]) => document.includes('SetPersonRelationship'))).toBe(false)
  expect(vi.mocked(graphql).mock.calls.some(([document]) => document.includes('RemovePersonRelationship'))).toBe(false)
})

it('says a person who is not in the circles is not found, and a failure as a failure', async () => {
  answer(new APIError('api: not found', false, 404))
  renderAt('/people/person-stranger')
  expect(await screen.findByText('We could not find that person.')).toBeTruthy()
  cleanup()

  answer(new Error('the server is down'))
  renderAt('/people/person-sam')
  expect(await screen.findByText('Could not read this card.')).toBeTruthy()
})

// The accounts they share are listed as their levels show them, with how much of
// a card's limit is used, and a way into the Finance page on them.
it('lists the finance accounts they share, and a card with how much of its limit is used', async () => {
  answer(
    sam({
      finance: {
        sectionState: 'shown',
        hasMore: true,
        financeAccounts: [
          {
            financeAccountId: 'account-one',
            financeShareLevel: 'transactions',
            accountName: 'Joint checking',
            accountMask: '7788',
            currencyCode: 'USD',
            currentBalance: '2450.1000',
            creditUsage: null,
          },
          {
            financeAccountId: 'account-two',
            financeShareLevel: 'balance',
            accountName: 'Store card',
            accountMask: null,
            currencyCode: 'USD',
            currentBalance: '-100.0000',
            creditUsage: { usageShare: 0.25 },
          },
        ],
      },
    }),
  )
  renderAt('/people/person-sam')
  expect(await screen.findByText('Joint checking ··7788')).toBeTruthy()
  expect(screen.getByText('$2,450.10')).toBeTruthy()
  expect(screen.getByText('Store card')).toBeTruthy()
  expect(screen.getByText(/25% used/)).toBeTruthy()
  expect(screen.getAllByRole('link', { name: 'Show all' }).map((link) => link.getAttribute('href'))).toContain(
    '/finance/spending?person=person-sam',
  )
})
