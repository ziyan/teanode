import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { SessionProvider } from '../session'
import { Circle, CircleInvitation, CirclePage, CirclesPage } from './circles'

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
    memberRole: 'admin',
    members: [],
    pendingInvitations: [],
    ...overrides,
  }
}

function anInvitation(overrides: Partial<CircleInvitation> = {}): CircleInvitation {
  return {
    id: 'invitation-one',
    circleId: 'circle-one',
    circleName: 'Fixture Home',
    circleKind: 'family',
    invitedByName: 'Person A',
    invitedUsername: 'person-c-login',
    memberRole: 'member',
    createdAt: '2030-01-01T00:00:00Z',
    expiresAt: '2030-02-01T00:00:00Z',
    ...overrides,
  }
}

function answer(shown: { read?: Circle; invitations?: CircleInvitation[]; calendars?: unknown[] }) {
  vi.mocked(graphql).mockImplementation(async (document) => {
    if (document.includes('ListCalendars')) return { ListCalendars: shown.calendars ?? [] } as never
    if (document.includes('ListCircleInvitations')) return { ListCircleInvitations: shown.invitations ?? [] } as never
    if (document.includes('ListCircles')) return { ListCircles: [] } as never
    if (document.includes('ReadCircle')) return { ReadCircle: shown.read } as never
    return {} as never
  })
}

function renderAt(entry: string, permissions = ['circle:use', 'calendar:use']) {
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
              <Route path="/circles" element={<CirclesPage />} />
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

it('offers an admin of a family a family calendar, and says what it is', async () => {
  answer({ read: aCircle() })
  renderAt('/circles/circle-one')
  const make = await screen.findByRole('button', { name: 'Make a family calendar' })
  fireEvent.click(make)
  await waitFor(() => expect(calls('CreateCircleCalendar')).toHaveLength(1))
  expect(calls('CreateCircleCalendar')[0][1]).toEqual({ circleId: 'circle-one' })
})

it('calls it a shared calendar for friends and colleagues', async () => {
  answer({ read: aCircle({ circleKind: 'colleagues' }) })
  renderAt('/circles/circle-one')
  expect(await screen.findByRole('button', { name: 'Make a shared calendar' })).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Make a family calendar' })).toBeNull()
})

it('tells a member that an admin can make it, and shows the calendar once it is made', async () => {
  answer({ read: aCircle({ memberRole: 'member' }) })
  renderAt('/circles/circle-one')
  expect(await screen.findByText('An admin of this circle can make a calendar for it.')).toBeTruthy()
  expect(screen.queryByRole('button', { name: /Make a/ })).toBeNull()

  cleanup()
  answer({
    read: aCircle(),
    calendars: [
      { id: 'cal', calendarKind: 'events', shareLevel: 'edit', circleId: 'circle-one', isCircleCalendar: true },
    ],
  })
  renderAt('/circles/circle-one')
  const link = (await screen.findByRole('link', { name: 'Open the calendar' })) as HTMLAnchorElement
  expect(link.getAttribute('href')).toBe('/calendar')
  expect(screen.queryByRole('button', { name: /Make a/ })).toBeNull()
})

it('says nothing of calendars to somebody who cannot use them', async () => {
  answer({ read: aCircle() })
  renderAt('/circles/circle-one', ['circle:use'])
  await screen.findByText('Members')
  expect(screen.queryByRole('button', { name: /Make a/ })).toBeNull()
  expect(calls('ListCalendars')).toHaveLength(0)
})

it('offers to share the calendar once, as a person joins a family, and shares at the level chosen', async () => {
  answer({
    invitations: [anInvitation()],
    calendars: [{ id: 'own', calendarKind: 'events', shareLevel: 'owner' }],
  })
  renderAt('/circles')
  fireEvent.click(await screen.findByRole('button', { name: 'Accept' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share your calendar with Fixture Home?' })
  fireEvent.change(within(dialog).getByLabelText(/^How much/), { target: { value: 'titles' } })
  // Not the level that lets them change it.
  expect(within(dialog).queryByRole('option', { name: /can edit/ })).toBeNull()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Share' }))
  await waitFor(() => expect(calls('mutation ($calendarId: String!, $granteeCircleId: String!')).toHaveLength(1))
  expect(calls('$granteeCircleId: String!')[0][1]).toEqual({
    calendarId: 'own',
    granteeCircleId: 'circle-one',
    shareLevel: 'titles',
  })
  await waitFor(() => expect(screen.queryByRole('dialog', { name: /Share your calendar/ })).toBeNull())
})

it('lets a person say not now, and does not offer it for friends', async () => {
  answer({ invitations: [anInvitation()], calendars: [{ id: 'own', calendarKind: 'events', shareLevel: 'owner' }] })
  renderAt('/circles')
  fireEvent.click(await screen.findByRole('button', { name: 'Accept' }))
  const dialog = await screen.findByRole('dialog', { name: /Share your calendar with/ })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Not now' }))
  expect(screen.queryByRole('dialog')).toBeNull()
  expect(calls('$granteeCircleId: String!')).toHaveLength(0)

  cleanup()
  vi.mocked(graphql).mockClear()
  answer({ invitations: [anInvitation({ circleKind: 'friends' })] })
  renderAt('/circles')
  fireEvent.click(await screen.findByRole('button', { name: 'Accept' }))
  await waitFor(() => expect(calls('AnswerCircleInvitation')).toHaveLength(1))
  expect(screen.queryByRole('dialog')).toBeNull()
})

it('offers a family where the person is as well, and shares it at the level chosen', async () => {
  answer({ invitations: [anInvitation()], calendars: [{ id: 'own', calendarKind: 'events', shareLevel: 'owner' }] })
  renderAt('/circles', ['circle:use', 'calendar:use', 'location:use'])
  fireEvent.click(await screen.findByRole('button', { name: 'Accept' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share your calendar with Fixture Home?' })
  fireEvent.change(within(dialog).getByLabelText(/^Share where I am/), { target: { value: 'history' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Share' }))
  await waitFor(() => expect(calls('ShareLocation(')).toHaveLength(1))
  expect(calls('ShareLocation(')[0][1]).toEqual({ granteeCircleId: 'circle-one', shareLevel: 'history' })
  expect(calls('$granteeCircleId: String!').length).toBeGreaterThan(0)
})

it('asks about location alone when calendars are not available, and not for colleagues', async () => {
  answer({ invitations: [anInvitation()] })
  renderAt('/circles', ['circle:use', 'location:use'])
  fireEvent.click(await screen.findByRole('button', { name: 'Accept' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share where you are with Fixture Home?' })
  // Nothing is chosen yet, so there is nothing to share.
  expect((within(dialog).getByRole('button', { name: 'Share' }) as HTMLButtonElement).disabled).toBe(true)
  fireEvent.click(within(dialog).getByRole('button', { name: 'Not now' }))
  expect(calls('ShareLocation(')).toHaveLength(0)

  cleanup()
  vi.mocked(graphql).mockClear()
  answer({ invitations: [anInvitation({ circleKind: 'colleagues' })] })
  renderAt('/circles', ['circle:use', 'location:use'])
  fireEvent.click(await screen.findByRole('button', { name: 'Accept' }))
  await waitFor(() => expect(calls('AnswerCircleInvitation')).toHaveLength(1))
  expect(screen.queryByRole('dialog')).toBeNull()
})

it('lets a person share only where they are, with Not now for the calendar, and Share waits for one choice', async () => {
  answer({ invitations: [anInvitation()], calendars: [{ id: 'own', calendarKind: 'events', shareLevel: 'owner' }] })
  renderAt('/circles', ['circle:use', 'calendar:use', 'location:use'])
  fireEvent.click(await screen.findByRole('button', { name: 'Accept' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share your calendar with Fixture Home?' })
  const share = within(dialog).getByRole('button', { name: 'Share' }) as HTMLButtonElement

  // The calendar says Not now and the location is not chosen: nothing to share.
  fireEvent.change(within(dialog).getByLabelText(/^How much/), { target: { value: 'none' } })
  expect(share.disabled).toBe(true)

  fireEvent.change(within(dialog).getByLabelText(/^Share where I am/), { target: { value: 'current' } })
  expect(share.disabled).toBe(false)
  fireEvent.click(share)
  await waitFor(() => expect(calls('ShareLocation(')).toHaveLength(1))
  expect(calls('ShareLocation(')[0][1]).toEqual({ granteeCircleId: 'circle-one', shareLevel: 'current' })
  // The calendar was not shared, nor looked up.
  expect(calls('$granteeCircleId: String!')).toHaveLength(0)
  expect(calls('ListCalendars')).toHaveLength(0)
})

it('shares the location before the calendar, so a retry after a calendar failure does not share the calendar twice', async () => {
  answer({ invitations: [anInvitation()], calendars: [{ id: 'own', calendarKind: 'events', shareLevel: 'owner' }] })
  renderAt('/circles', ['circle:use', 'calendar:use', 'location:use'])
  fireEvent.click(await screen.findByRole('button', { name: 'Accept' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share your calendar with Fixture Home?' })
  fireEvent.change(within(dialog).getByLabelText(/^Share where I am/), { target: { value: 'history' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Share' }))
  await waitFor(() => expect(calls('mutation ($calendarId: String!, $granteeCircleId: String!')).toHaveLength(1))

  const locationOrder =
    vi.mocked(graphql).mock.invocationCallOrder[
      vi.mocked(graphql).mock.calls.findIndex(([document]) => document.includes('ShareLocation('))
    ]
  const calendarOrder =
    vi.mocked(graphql).mock.invocationCallOrder[
      vi.mocked(graphql).mock.calls.findIndex(([document]) => document.includes('ShareCalendar('))
    ]
  expect(locationOrder).toBeLessThan(calendarOrder)
})
