import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { SessionProvider } from '../session'
import { Circle, CirclePage } from './circles'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

function aCircle(overrides: Partial<Circle> = {}): Circle {
  return {
    id: 'circle-one',
    name: 'Fixture Home',
    circleKind: 'family',
    createdAt: '2030-01-01T00:00:00Z',
    memberRole: 'member',
    members: [],
    pendingInvitations: [],
    ...overrides,
  }
}

function answer(shown: { read?: Circle; books?: unknown[] }) {
  vi.mocked(graphql).mockImplementation(async (document) => {
    if (document.includes('ListReachableAddressBooks')) return { ListReachableAddressBooks: shown.books ?? [] } as never
    if (document.includes('ListCircles')) return { ListCircles: [] } as never
    if (document.includes('ReadCircle')) return { ReadCircle: shown.read } as never
    return {} as never
  })
}

function renderAt(entry: string, permissions = ['circle:use', 'contacts:use']) {
  return render(
    <TranslationProvider>
      <ToastProvider>
        <SessionProvider
          value={{
            userId: 'person-a',
            authenticated: true,
            authenticationRequired: true,
            username: 'person-a-login',
            passkeysEnabled: false,
            permissions: { everywhere: permissions, byDomain: [] },
          }}
        >
          <MemoryRouter initialEntries={[entry]}>
            <Routes>
              <Route path="/circles/:circleId" element={<CirclePage />} />
            </Routes>
          </MemoryRouter>
        </SessionProvider>
      </ToastProvider>
    </TranslationProvider>,
  )
}

function calls(operation: string) {
  return vi.mocked(graphql).mock.calls.filter(([document]) => document.includes(operation))
}

it('offers any member of a family an address book for it', async () => {
  answer({ read: aCircle() })
  renderAt('/circles/circle-one')
  fireEvent.click(await screen.findByRole('button', { name: 'Make a family address book' }))
  await waitFor(() => expect(calls('CreateCircleAddressBook')).toHaveLength(1))
  expect(calls('CreateCircleAddressBook')[0][1]).toEqual({ circleId: 'circle-one' })
})

it('calls it a shared address book for friends and colleagues', async () => {
  answer({ read: aCircle({ circleKind: 'friends' }) })
  renderAt('/circles/circle-one')
  expect(await screen.findByRole('button', { name: 'Make a shared address book' })).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Make a family address book' })).toBeNull()
})

it("opens the circle's book once it is made, in the address book page", async () => {
  answer({
    read: aCircle(),
    books: [
      { id: 'own', isCircleBook: false },
      { id: 'book-family', circleId: 'circle-one', isCircleBook: true },
      { id: 'book-other', circleId: 'circle-two', isCircleBook: true },
    ],
  })
  renderAt('/circles/circle-one')
  const link = (await screen.findByRole('link', { name: 'Open the address book' })) as HTMLAnchorElement
  expect(link.getAttribute('href')).toBe('/contacts?book=book-family')
  expect(screen.queryByRole('button', { name: /Make a/ })).toBeNull()
})

it('says nothing of address books to somebody who cannot keep contacts', async () => {
  answer({ read: aCircle() })
  renderAt('/circles/circle-one', ['circle:use'])
  await screen.findByText('Members')
  expect(screen.queryByRole('button', { name: /Make a .* address book/ })).toBeNull()
  expect(calls('ListReachableAddressBooks')).toHaveLength(0)
})
