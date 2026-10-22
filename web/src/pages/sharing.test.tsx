import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { SessionProvider } from '../session'
import { SharingMade, SharingReceived, SharingPage } from './sharing'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

function made(overrides: Partial<SharingMade> = {}): SharingMade {
  return {
    shareKind: 'calendar',
    shareId: 'share-one',
    subjectId: 'calendar-one',
    subjectName: 'Fixture Calendar',
    shareLevel: 'titles',
    shareScopes: [],
    passwordItemCount: 0,
    granteeUserId: 'person-b',
    granteeUsername: 'person-b-login',
    granteeName: 'Person B',
    granteeCircleId: null,
    granteeCircleName: '',
    createdAt: '2030-01-02T00:00:00Z',
    ...overrides,
  }
}

function received(overrides: Partial<SharingReceived> = {}): SharingReceived {
  return {
    shareKind: 'memory_page',
    shareId: 'share-two',
    subjectId: 'people/kim',
    subjectName: 'Kim',
    shareLevel: '',
    shareScopes: ['quotes'],
    passwordItemCount: 0,
    ownerUserId: 'person-b',
    ownerUsername: 'person-b-login',
    ownerName: 'Person B',
    granteeCircleId: null,
    granteeCircleName: '',
    isDirect: true,
    canDecline: true,
    createdAt: '2030-01-03T00:00:00Z',
    ...overrides,
  }
}

function answerWith(sharesMade: SharingMade[], sharesReceived: SharingReceived[]) {
  vi.mocked(graphql).mockImplementation(async (document) => {
    if (document.includes('ListSharing')) return { ListSharing: { sharesMade, sharesReceived } } as never
    return {} as never
  })
}

function Where() {
  const location = useLocation()
  return <p data-testid="where">{location.pathname}</p>
}

function renderAt(entry: string, permissions = ['circle:use']) {
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
              <Route path="/sharing" element={<SharingPage />} />
              <Route path="/sharing/:tab" element={<SharingPage />} />
              <Route path="/circles" element={<p>The circles page</p>} />
              <Route path="/calendar" element={<p>The calendar page</p>} />
              <Route path="/people/:userId" element={<p>The card page</p>} />
            </Routes>
            <Where />
          </MemoryRouter>
        </SessionProvider>
      </ToastProvider>
    </TranslationProvider>,
  )
}

function calls(operation: string) {
  return vi.mocked(graphql).mock.calls.filter(([document]) => document.includes(operation))
}

it('lists what the person shares, each row linking to its own page and to the person', async () => {
  answerWith([made()], [])
  renderAt('/sharing')

  const what = (await screen.findByRole('link', { name: 'Fixture Calendar' })) as HTMLAnchorElement
  expect(what.getAttribute('href')).toBe('/calendar')
  const row = what.closest('tr') as HTMLElement
  expect(within(row).getByText('Titles')).toBeTruthy()
  expect((within(row).getByRole('link', { name: 'Person B' }) as HTMLAnchorElement).getAttribute('href')).toBe(
    '/people/person-b',
  )
})

it('stops a share with the kind’s own operation after asking', async () => {
  answerWith([made()], [])
  renderAt('/sharing')

  fireEvent.click(await screen.findByRole('button', { name: /stop sharing with Person B/i }))
  expect(calls('UnshareCalendar')).toHaveLength(0)
  expect(screen.getByText('Stop sharing with Person B?')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Stop sharing' }))

  await waitFor(() => expect(calls('UnshareCalendar')).toHaveLength(1))
  expect(calls('UnshareCalendar')[0][1]).toEqual({ shareId: 'share-one' })
})

it('takes Health back by sharing no categories, with the grantee it was shared with', async () => {
  answerWith(
    [
      made({
        shareKind: 'health',
        shareId: 'share-health',
        subjectId: '',
        subjectName: '',
        shareLevel: '',
        shareScopes: ['activity', 'sleep'],
      }),
    ],
    [],
  )
  renderAt('/sharing')

  expect(await screen.findByText('Activity, Sleep')).toBeTruthy()
  fireEvent.click(await screen.findByRole('button', { name: /stop sharing with Person B/i }))
  fireEvent.click(screen.getByRole('button', { name: 'Stop sharing' }))

  await waitFor(() => expect(calls('SetHealthShare')).toHaveLength(1))
  expect(calls('SetHealthShare')[0][1]).toEqual({
    granteeUserId: 'person-b',
    granteeCircleId: null,
    healthCategories: [],
  })
})

it('counts a share of passwords and offers no way to stop it here', async () => {
  answerWith(
    [
      made({
        shareKind: 'passwords',
        shareId: '',
        subjectId: '',
        subjectName: '',
        shareLevel: '',
        passwordItemCount: 3,
      }),
    ],
    [],
  )
  renderAt('/sharing')

  const link = (await screen.findByRole('link', { name: '3 items' })) as HTMLAnchorElement
  expect(link.getAttribute('href')).toBe('/passwords')
  expect(within(link.closest('tr') as HTMLElement).queryByRole('button')).toBeNull()
})

it('keeps the tab in the address and lists what is shared with the person', async () => {
  answerWith(
    [made()],
    [
      received(),
      received({
        shareKind: 'location',
        shareId: 'share-three',
        subjectId: '',
        subjectName: '',
        shareScopes: [],
        shareLevel: 'current',
        isDirect: false,
        canDecline: false,
        granteeCircleId: 'circle-one',
        granteeCircleName: 'Fixture Home',
      }),
    ],
  )
  renderAt('/sharing')

  fireEvent.click(await screen.findByRole('button', { name: 'Shared with you' }))
  expect(screen.getByTestId('where').textContent).toBe('/sharing/with-you')

  const page = (await screen.findByRole('link', { name: 'Kim' })) as HTMLAnchorElement
  expect(page.getAttribute('href')).toBe('/knowledge/people/kim?owner=person-b-login')
  const pageRow = page.closest('tr') as HTMLElement
  expect(within(pageRow).getByText('By name')).toBeTruthy()
  expect(within(pageRow).getByText('With quotes')).toBeTruthy()

  // A share through a circle names the circle, says it ends with it, and has
  // no button.
  const locationRow = screen.getByText('Fixture Home').closest('tr') as HTMLElement
  expect(within(locationRow).getByText('Ends when you leave the circle')).toBeTruthy()
  expect(within(locationRow).queryByRole('button')).toBeNull()
})

it('removes a share made by name with DeclineShare after asking', async () => {
  answerWith([], [received()])
  renderAt('/sharing/with-you')

  fireEvent.click(await screen.findByRole('button', { name: /remove what Person B shared/i }))
  expect(calls('DeclineShare')).toHaveLength(0)
  const removeButtons = screen.getAllByRole('button', { name: 'Remove' })
  fireEvent.click(removeButtons[removeButtons.length - 1])

  await waitFor(() => expect(calls('DeclineShare')).toHaveLength(1))
  expect(calls('DeclineShare')[0][1]).toEqual({ shareKind: 'memory_page', shareId: 'share-two' })
})

it('says it is not on for an account without circles, and sends an unknown tab to the first', async () => {
  answerWith([], [])
  renderAt('/sharing', [])
  expect(await screen.findByText(/Circles are not on for your account/)).toBeTruthy()
  cleanup()

  renderAt('/sharing/nonsense')
  await waitFor(() => expect(screen.getByTestId('where').textContent).toBe('/sharing'))
})
