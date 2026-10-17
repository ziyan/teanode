import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { SessionProvider } from '../session'
import { Circle, CircleInvitation, CircleMember, CirclePage, CirclesPage } from './circles'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

function member(userId: string, memberRole: 'admin' | 'member', name: string): CircleMember {
  return {
    userId,
    username: `${userId}-login`,
    name,
    avatarUrl: null,
    avatarColorIndex: 3,
    memberRole,
    joinedAt: '2030-01-01T00:00:00Z',
  }
}

function invitation(overrides: Partial<CircleInvitation> = {}): CircleInvitation {
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

// The caller is person-a, an admin, unless a test says otherwise.
function circle(overrides: Partial<Circle> = {}): Circle {
  return {
    id: 'circle-one',
    name: 'Fixture Home',
    circleKind: 'family',
    createdAt: '2030-01-01T00:00:00Z',
    memberRole: 'admin',
    members: [member('person-a', 'admin', 'Person A'), member('person-b', 'member', 'Person B')],
    pendingInvitations: [invitation({ id: 'pending-one', invitedUsername: 'person-c-login' })],
    ...overrides,
  }
}

function answerWith(shown: { circles?: Circle[]; invitations?: CircleInvitation[]; read?: Circle }) {
  vi.mocked(graphql).mockImplementation(async (document) => {
    if (document.includes('ListCircles')) return { ListCircles: shown.circles ?? [] } as never
    if (document.includes('ListCircleInvitations')) return { ListCircleInvitations: shown.invitations ?? [] } as never
    if (document.includes('ReadCircle')) return { ReadCircle: shown.read } as never
    if (document.includes('CreateCircle')) return { CreateCircle: { id: 'circle-new' } } as never
    return {} as never
  })
}

function Where() {
  const location = useLocation()
  return <p data-testid="where">{location.pathname}</p>
}

function renderAt(entry: string) {
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
          }}
        >
          <MemoryRouter initialEntries={[entry]}>
            <Routes>
              <Route path="/circles" element={<CirclesPage />} />
              <Route path="/circles/:circleId" element={<CirclePage />} />
            </Routes>
            <Where />
          </MemoryRouter>
        </SessionProvider>
      </ToastProvider>
    </TranslationProvider>,
  )
}

function mutationCalls(operation: string) {
  return vi.mocked(graphql).mock.calls.filter(([document]) => document.includes(operation))
}

it('lists the circles with their kind and how many people are in them', async () => {
  answerWith({ circles: [circle()] })
  renderAt('/circles')

  const card = (await screen.findByRole('link', { name: /Fixture Home/ })) as HTMLAnchorElement
  expect(card.getAttribute('href')).toBe('/circles/circle-one')
  expect(within(card).getByText('Family')).toBeTruthy()
  expect(within(card).getByText('2 people')).toBeTruthy()
})

it('says so when the person is in no circle', async () => {
  answerWith({})
  renderAt('/circles')
  expect(await screen.findByText(/You are not in a circle yet/)).toBeTruthy()
})

it('shows the invitations waiting for an answer, and accepting one calls the mutation', async () => {
  answerWith({ circles: [], invitations: [invitation()] })
  renderAt('/circles')

  expect(await screen.findByText('Invited by Person A')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Accept' }))

  await waitFor(() => expect(mutationCalls('AnswerCircleInvitation')).toHaveLength(1))
  expect(mutationCalls('AnswerCircleInvitation')[0][1]).toEqual({ invitationId: 'invitation-one', isAccepted: true })
})

it('declining an invitation sends isAccepted false', async () => {
  answerWith({ invitations: [invitation()] })
  renderAt('/circles')

  fireEvent.click(await screen.findByRole('button', { name: 'Decline' }))

  await waitFor(() => expect(mutationCalls('AnswerCircleInvitation')).toHaveLength(1))
  expect(mutationCalls('AnswerCircleInvitation')[0][1]).toEqual({ invitationId: 'invitation-one', isAccepted: false })
})

it('makes a circle from a name and a kind and opens it', async () => {
  answerWith({})
  renderAt('/circles')

  fireEvent.click(await screen.findByRole('button', { name: 'New circle' }))
  const dialog = await screen.findByRole('dialog', { name: 'New circle' })
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Fixture Team' } })
  fireEvent.change(within(dialog).getByLabelText('Kind'), { target: { value: 'colleagues' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Create' }))

  await waitFor(() => expect(mutationCalls('CreateCircle')).toHaveLength(1))
  expect(mutationCalls('CreateCircle')[0][1]).toEqual({ name: 'Fixture Team', circleKind: 'colleagues' })
  await waitFor(() => expect(screen.getByTestId('where').textContent).toBe('/circles/circle-new'))
})

it('gives an admin the members, the invitation form, the pending invitations and the changes to the circle', async () => {
  answerWith({ read: circle() })
  renderAt('/circles/circle-one')

  expect(await screen.findByText('Person B')).toBeTruthy()
  // Person A is the caller: marked as an admin, with no menu on their own row.
  expect(screen.getAllByText('Admin').length).toBeGreaterThan(0)
  expect(screen.queryByRole('button', { name: /Person A: Actions/ })).toBeNull()
  expect(screen.getByRole('button', { name: /Person B: Actions/ })).toBeTruthy()

  expect(screen.getByLabelText('Username')).toBeTruthy()
  expect(screen.getByLabelText('Make them an admin')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'person-c-login: Withdraw' })).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Fixture Home: Rename' })).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Fixture Home: Delete circle' })).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Leave' })).toBeTruthy()
})

it('hides every admin control from a member, and leaves Leave', async () => {
  answerWith({
    read: circle({
      memberRole: 'member',
      members: [member('person-a', 'member', 'Person A'), member('person-b', 'admin', 'Person B')],
      pendingInvitations: [],
    }),
  })
  renderAt('/circles/circle-one')

  expect(await screen.findByText('Person B')).toBeTruthy()
  expect(screen.queryByLabelText('Username')).toBeNull()
  expect(screen.queryByRole('button', { name: 'Send invitation' })).toBeNull()
  expect(screen.queryByText('Waiting for an answer')).toBeNull()
  expect(screen.queryByRole('button', { name: /Actions/ })).toBeNull()
  expect(screen.queryByRole('button', { name: /Rename/ })).toBeNull()
  expect(screen.queryByRole('button', { name: /Delete circle/ })).toBeNull()
  expect(screen.getByRole('button', { name: 'Leave' })).toBeTruthy()
})

it('does not claim the invited person was found', async () => {
  answerWith({ read: circle() })
  renderAt('/circles/circle-one')

  fireEvent.change(await screen.findByLabelText('Username'), { target: { value: 'person-d-login' } })
  fireEvent.click(screen.getByLabelText('Make them an admin'))
  fireEvent.click(screen.getByRole('button', { name: 'Send invitation' }))

  expect(await screen.findByText('Invitation sent if that account exists.')).toBeTruthy()
  expect(mutationCalls('InviteToCircle')[0][1]).toEqual({
    circleId: 'circle-one',
    username: 'person-d-login',
    memberRole: 'admin',
  })
})

it('asks before leaving, and leaves only when confirmed', async () => {
  answerWith({ read: circle({ memberRole: 'member', pendingInvitations: [] }) })
  renderAt('/circles/circle-one')

  fireEvent.click(await screen.findByRole('button', { name: 'Leave' }))
  const dialog = await screen.findByRole('alertdialog', { name: 'Leave Fixture Home?' })
  expect(mutationCalls('LeaveCircle')).toHaveLength(0)

  fireEvent.click(within(dialog).getByRole('button', { name: 'Leave' }))
  await waitFor(() => expect(mutationCalls('LeaveCircle')).toHaveLength(1))
  expect(mutationCalls('LeaveCircle')[0][1]).toEqual({ circleId: 'circle-one' })
  await waitFor(() => expect(screen.getByTestId('where').textContent).toBe('/circles'))
})

it('turns a member into an admin from the row menu', async () => {
  answerWith({ read: circle() })
  renderAt('/circles/circle-one')

  fireEvent.click(await screen.findByRole('button', { name: /Person B: Actions/ }))
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Make admin' }))

  await waitFor(() => expect(mutationCalls('SetCircleMemberRole')).toHaveLength(1))
  expect(mutationCalls('SetCircleMemberRole')[0][1]).toEqual({
    circleId: 'circle-one',
    userId: 'person-b',
    memberRole: 'admin',
  })
})
