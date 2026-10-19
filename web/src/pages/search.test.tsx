import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'

import { graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { SessionProvider } from '../session'
import { SearchPage } from './search'

const fixture = vi.hoisted(() => {
  // Read when the page asks, so a test can be a phone.
  const state = { isDesktop: true }
  window.matchMedia = ((query: string) => ({
    matches: state.isDesktop,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
  })) as unknown as typeof window.matchMedia
  return state
})

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))

beforeEach(() => {
  fixture.isDesktop = true
})
afterEach(cleanup)

type Group = {
  searchArea: string
  results: { searchArea: string; targetId: string; titleText: string; detailText: string; happenedAt?: string }[]
  hasMore: boolean
}

function group(searchArea: string, targetId: string, titleText: string, hasMore = false): Group {
  return {
    searchArea,
    results: [
      { searchArea, targetId, titleText, detailText: `About ${titleText}`, happenedAt: '2031-03-04T05:06:07Z' },
    ],
    hasMore,
  }
}

// One result in each area, in the order the server gives them.
const EVERY_AREA: Group[] = [
  group('chats', 'conversation-1', 'Seed order chat', true),
  group('mail', 'mail-1', 'Seed order mail', true),
  group('contacts', 'contact-1', 'Seed Merchant', true),
  group('photos', 'photo-1', 'Seed tray photo', true),
  group('memory', 'projects/garden plan', 'Garden plan', true),
  group('documents', 'document-1', 'notes/seeds.txt', true),
  group('recordings', 'recording-1', 'Seed call', true),
  group('places', 'place-1', 'Seed shed', true),
  group('finance', 'transaction-1', 'Seed supplier', true),
]

function answer(areas: Group[], failedAreas: string[] = []) {
  vi.mocked(graphql).mockImplementation(async (query: string) => {
    if (query.includes('SearchEverything')) {
      return { SearchEverything: { words: 'seed', areas, failedAreas } }
    }
    if (query.includes('GetMailboxItem')) {
      return { GetMailboxItem: { id: 'mail-1', folderId: 'folder-inbox' } }
    }
    throw new Error(`unexpected query ${query}`)
  })
}

function Where() {
  const location = useLocation()
  return <p data-testid="where">{location.pathname + location.search}</p>
}

function renderAt(path: string) {
  render(
    <TranslationProvider>
      <SessionProvider
        value={{
          userId: 'fixture-owner',
          authenticated: true,
          authenticationRequired: true,
          username: 'fixture',
          passkeysEnabled: false,
        }}
      >
        <ToastProvider>
          <MemoryRouter initialEntries={[path]}>
            <Routes>
              <Route path="/search" element={<SearchPage />} />
              <Route path="*" element={<Where />} />
            </Routes>
          </MemoryRouter>
        </ToastProvider>
      </SessionProvider>
    </TranslationProvider>,
  )
}

it('asks for the words with the per-area limit and draws one group per area in the fixed order', async () => {
  // The server's order is fixed, but a page does not depend on it.
  answer([...EVERY_AREA].reverse())
  renderAt('/search?q=seed')
  await screen.findByText('Seed order chat')
  expect(vi.mocked(graphql).mock.calls[0][1]).toEqual({ words: 'seed', perAreaLimit: 5 })
  const names = screen.getAllByRole('heading', { level: 3 }).map((heading) => heading.textContent)
  expect(names).toEqual([
    'Chats',
    'Mail',
    'Contacts',
    'Photos',
    'Knowledge',
    'Documents',
    'Recordings',
    'Places',
    'Finance',
  ])
})

it.each([
  ['Seed order chat', '/chat?conversation=conversation-1'],
  ['Seed Merchant', '/contacts?contact=contact-1'],
  ['Seed tray photo', '/photos/photo-1'],
  ['Garden plan', '/knowledge/projects/garden%20plan'],
  ['notes/seeds.txt', '/knowledge?documents=seed'],
  ['Seed call', '/recordings/recording-1'],
  ['Seed shed', '/location?tab=places&place=place-1'],
  ['Seed supplier', '/finance/transactions?transaction=transaction-1'],
  // A message is opened in the folder it is in, which the result does not
  // say, so the page asks.
  ['Seed order mail', '/mailbox/folder-inbox/mail-1'],
])('opens %s where it lives', async (title, target) => {
  answer(EVERY_AREA)
  renderAt('/search?q=seed')
  fireEvent.click((await screen.findByText(title)).closest('button')!)
  await waitFor(() => expect(screen.getByTestId('where').textContent).toBe(target))
})

it('opens the mail search when the folder of a message cannot be found', async () => {
  vi.mocked(graphql).mockImplementation(async (query: string) => {
    if (query.includes('GetMailboxItem')) throw new Error('gone')
    return { SearchEverything: { words: 'seed', areas: [EVERY_AREA[1]], failedAreas: [] } }
  })
  renderAt('/search?q=seed')
  fireEvent.click((await screen.findByText('Seed order mail')).closest('button')!)
  await waitFor(() => expect(screen.getByTestId('where').textContent).toBe('/mailbox?q=seed&everywhere=true'))
})

it('shows all in an area with the words in the parameter that page reads', async () => {
  answer(EVERY_AREA)
  renderAt('/search?q=tea+garden')
  await screen.findByText('Seed order chat')
  const hrefOf = (area: string) => screen.getByRole('link', { name: `Show all in ${area}` }).getAttribute('href')
  expect(hrefOf('Chats')).toBe('/chat?search=tea+garden')
  expect(hrefOf('Mail')).toBe('/mailbox?q=tea+garden&everywhere=true')
  expect(hrefOf('Contacts')).toBe('/contacts?search=tea+garden')
  expect(hrefOf('Photos')).toBe('/photos?search=tea+garden')
  expect(hrefOf('Knowledge')).toBe('/knowledge?search=tea+garden')
  expect(hrefOf('Documents')).toBe('/knowledge?documents=tea+garden')
  expect(hrefOf('Recordings')).toBe('/recordings?search=tea+garden')
  expect(hrefOf('Places')).toBe('/location?tab=places&placesQuery=tea+garden')
  expect(hrefOf('Finance')).toBe('/finance/transactions?text=tea+garden')
})

it('offers show all only for an area with more than it shows', async () => {
  answer([group('mail', 'mail-1', 'Seed order mail', false), group('chats', 'conversation-1', 'Seed order chat', true)])
  renderAt('/search?q=seed')
  await screen.findByText('Seed order mail')
  expect(screen.getByRole('link', { name: 'Show all in Chats' })).toBeTruthy()
  expect(screen.queryByRole('link', { name: 'Show all in Mail' })).toBeNull()
})

it('says nothing was found in one short line', async () => {
  answer([])
  renderAt('/search?q=seed')
  expect(await screen.findByText('Nothing found for "seed".')).toBeTruthy()
  expect(screen.queryByRole('heading', { level: 3 })).toBeNull()
})

it('tells a failed area in a toast and keeps the groups that answered', async () => {
  answer([EVERY_AREA[2]], ['places', 'mail'])
  renderAt('/search?q=seed')
  expect(await screen.findByText('Could not search Mail, Places.')).toBeTruthy()
  const contacts = screen.getByRole('region', { name: 'Contacts' })
  expect(within(contacts).getByText('Seed Merchant')).toBeTruthy()
  expect(screen.queryByText('Nothing found for "seed".')).toBeNull()
})

it('asks for words when the address has none, without asking the server', async () => {
  answer(EVERY_AREA)
  renderAt('/search')
  expect(await screen.findByText(/Type some words/)).toBeTruthy()
  expect(graphql).not.toHaveBeenCalled()
})

it('has a box of its own on a phone, where the rail is behind a button', async () => {
  fixture.isDesktop = false
  answer(EVERY_AREA)
  renderAt('/search?q=seed')
  const box = (await screen.findByRole('searchbox', { name: 'Search' })) as HTMLInputElement
  expect(box.value).toBe('seed')
  fireEvent.change(box, { target: { value: 'tea garden' } })
  fireEvent.submit(box.closest('form')!)
  await waitFor(() =>
    expect(vi.mocked(graphql).mock.calls[vi.mocked(graphql).mock.calls.length - 1][1]).toEqual({
      words: 'tea garden',
      perAreaLimit: 5,
    }),
  )
})

it('has no box of its own on a wide screen, where the rail has one', async () => {
  answer(EVERY_AREA)
  renderAt('/search?q=seed')
  await screen.findByText('Seed order chat')
  expect(screen.queryByRole('searchbox')).toBeNull()
})
