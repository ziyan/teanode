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
    subjectMask: '',
    isSubjectDeleted: false,
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
    subjectMask: '',
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

it('lists a recording and a finance account the person shares, with their levels, links and own stop operations', async () => {
  answerWith(
    [
      made({
        shareKind: 'recording',
        shareId: 'share-recording',
        subjectId: 'recording one',
        subjectName: 'Fixture Standup',
        shareLevel: 'transcript',
      }),
      made({
        shareKind: 'finance',
        shareId: 'share-finance',
        subjectId: 'account-one',
        subjectName: 'Fixture Checking',
        subjectMask: '0001',
        shareLevel: 'spending',
      }),
    ],
    [],
  )
  renderAt('/sharing')

  const recording = (await screen.findByRole('link', { name: 'Fixture Standup' })) as HTMLAnchorElement
  expect(recording.getAttribute('href')).toBe('/recordings/recording%20one')
  expect(within(recording.closest('tr') as HTMLElement).getByText('Transcript')).toBeTruthy()
  const account = (await screen.findByRole('link', { name: 'Fixture Checking ··0001' })) as HTMLAnchorElement
  expect(account.getAttribute('href')).toBe('/settings/agent/finance')
  expect(within(account.closest('tr') as HTMLElement).getByText('Spending')).toBeTruthy()

  for (const operation of ['UnshareRecording', 'UnshareFinanceAccount']) {
    fireEvent.click(
      screen.getAllByRole('button', { name: /stop sharing with Person B/i })[operation === 'UnshareRecording' ? 0 : 1],
    )
    fireEvent.click(screen.getByRole('button', { name: 'Stop sharing' }))
    await waitFor(() => expect(calls(operation)).toHaveLength(1))
  }
  expect(calls('UnshareRecording')[0][1]).toEqual({ recordingShareId: 'share-recording' })
  expect(calls('UnshareFinanceAccount')[0][1]).toEqual({ shareId: 'share-finance' })
})

it('lists a recording and a finance account shared with the person, and removes them by name', async () => {
  answerWith(
    [],
    [
      received({
        shareKind: 'recording',
        shareId: 'share-recording',
        subjectId: 'recording-one',
        subjectName: 'Fixture Standup',
        shareLevel: 'summary',
        shareScopes: [],
      }),
      received({
        shareKind: 'finance',
        shareId: 'share-finance',
        subjectId: 'account-one',
        subjectName: 'Fixture Checking',
        subjectMask: '0001',
        shareLevel: 'balance',
        shareScopes: [],
        isDirect: false,
        canDecline: false,
        granteeCircleId: 'circle-one',
        granteeCircleName: 'Fixture Home',
      }),
    ],
  )
  renderAt('/sharing/with-you')

  const recording = (await screen.findByRole('link', { name: 'Fixture Standup' })) as HTMLAnchorElement
  expect(recording.getAttribute('href')).toBe('/recordings/shared/recording-one')
  expect(within(recording.closest('tr') as HTMLElement).getByText('Summary')).toBeTruthy()
  const account = (await screen.findByRole('link', { name: 'Fixture Checking ··0001' })) as HTMLAnchorElement
  expect(account.getAttribute('href')).toBe('/finance?person=person-b')
  const accountRow = account.closest('tr') as HTMLElement
  expect(within(accountRow).getByText('Balance')).toBeTruthy()
  expect(within(accountRow).queryByRole('button')).toBeNull()

  fireEvent.click(screen.getByRole('button', { name: /remove what Person B shared/i }))
  const removeButtons = screen.getAllByRole('button', { name: 'Remove' })
  fireEvent.click(removeButtons[removeButtons.length - 1])
  await waitFor(() => expect(calls('DeclineShare')).toHaveLength(1))
  expect(calls('DeclineShare')[0][1]).toEqual({ shareKind: 'recording', shareId: 'share-recording' })
})

it('says it is not on for an account without circles, and sends an unknown tab to the first', async () => {
  answerWith([], [])
  renderAt('/sharing', [])
  expect(await screen.findByText(/Circles are not on for your account/)).toBeTruthy()
  cleanup()

  renderAt('/sharing/nonsense')
  await waitFor(() => expect(screen.getByTestId('where').textContent).toBe('/sharing'))
})

it('words a location shared with the person as where the other person is, and the person’s own as where they are', async () => {
  answerWith(
    [made({ shareKind: 'location', shareId: 'share-mine', subjectId: '', subjectName: '', shareLevel: 'current' })],
    [
      received({
        shareKind: 'location',
        shareId: 'share-theirs',
        subjectId: '',
        subjectName: '',
        shareScopes: [],
        shareLevel: 'history',
      }),
    ],
  )
  renderAt('/sharing')

  expect(await screen.findByText('Where I am now')).toBeTruthy()
  fireEvent.click(await screen.findByRole('button', { name: 'Shared with you' }))
  expect(await screen.findByText('Where they are and where they have been')).toBeTruthy()
  expect(screen.queryByText('Where I am and where I have been')).toBeNull()
})

it('words a scope by the kind it belongs to, so a skill’s tool named quotes stays as it is', async () => {
  answerWith(
    [
      made({
        shareKind: 'skill',
        shareId: 'share-skill',
        subjectId: 'fixture-skill',
        subjectName: 'fixture-skill',
        shareLevel: '',
        shareScopes: ['quotes'],
      }),
      made({
        shareKind: 'memory_page',
        shareId: 'share-page',
        subjectId: 'people/kim',
        subjectName: 'Kim',
        shareLevel: '',
        shareScopes: ['quotes'],
      }),
    ],
    [],
  )
  renderAt('/sharing')

  const skillRow = (await screen.findByRole('link', { name: 'fixture-skill' })).closest('tr') as HTMLElement
  expect(within(skillRow).getByText('quotes')).toBeTruthy()
  expect(within(skillRow).queryByText('With quotes')).toBeNull()
  const pageRow = screen.getByRole('link', { name: 'Kim' }).closest('tr') as HTMLElement
  expect(within(pageRow).getByText('With quotes')).toBeTruthy()
})

it('lists a shared page that is gone as deleted, with no link, and still stops it', async () => {
  answerWith(
    [
      made({
        shareKind: 'memory_page',
        shareId: 'share-gone',
        subjectId: 'people/gone',
        subjectName: 'people/gone',
        isSubjectDeleted: true,
        shareLevel: '',
      }),
    ],
    [],
  )
  renderAt('/sharing')

  const cell = await screen.findByText('Page deleted')
  expect(cell.closest('a')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: /stop sharing with Person B/i }))
  // The question names the page as this page, not as "Page deleted".
  expect(screen.getByText('Person B can no longer see this page.')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Stop sharing' }))
  await waitFor(() => expect(calls('UnshareAgentPage')).toHaveLength(1))
  expect(calls('UnshareAgentPage')[0][1]).toEqual({ shareId: 'share-gone' })
})

it('lists a shopping list the person shares, opening it in Shopping, and stops it with its own operation', async () => {
  answerWith(
    [
      made({
        shareKind: 'shopping_list',
        shareId: 'share-shopping',
        subjectId: 'list-1',
        subjectName: 'Groceries',
        shareLevel: '',
      }),
    ],
    [],
  )
  renderAt('/sharing')

  const what = (await screen.findByRole('link', { name: 'Groceries' })) as HTMLAnchorElement
  expect(what.getAttribute('href')).toBe('/shopping/lists/list-1')
  const row = what.closest('tr') as HTMLElement
  expect(within(row).getByText('Shopping list')).toBeTruthy()

  fireEvent.click(screen.getByRole('button', { name: /stop sharing with Person B/i }))
  fireEvent.click(screen.getByRole('button', { name: 'Stop sharing' }))

  await waitFor(() => expect(calls('UnshareShoppingList')).toHaveLength(1))
  expect(calls('UnshareShoppingList')[0][1]).toEqual({ shoppingListShareId: 'share-shopping' })
})
