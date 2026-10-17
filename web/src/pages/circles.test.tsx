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

// The signed-in person holds user:manage when they are an operator.
function renderAt(entry: string, options: { isOperator?: boolean } = {}) {
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
            permissions: { everywhere: options.isOperator ? ['user:manage', 'circle:use'] : ['circle:use'], byDomain: [] },
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
  // On a fresh server nobody else has an account until somebody who can make
  // one does: say who to ask.
  expect(screen.getByText(/ask whoever runs this server to make their account/)).toBeTruthy()
  // Somebody who cannot make accounts is not told they can add people without one.
  expect(screen.queryByText(/with or without accounts yet/)).toBeNull()
})

it('tells an operator with no circle that people can be added with or without accounts', async () => {
  answerWith({})
  renderAt('/circles', { isOperator: true })
  expect(await screen.findByText(/add the people who use this server, with or without accounts yet/)).toBeTruthy()
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

// Opens the operator's way in on the circle's page and fills the dialog.
async function fillAccount(values: { username: string; name: string; password: string; isAdmin?: boolean }) {
  fireEvent.click(await screen.findByRole('button', { name: 'Make an account for someone' }))
  const dialog = await screen.findByRole('dialog', { name: 'Make an account for someone' })
  fireEvent.change(within(dialog).getByLabelText('Username'), { target: { value: values.username } })
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: values.name } })
  fireEvent.change(within(dialog).getByLabelText('Password'), { target: { value: values.password } })
  if (values.isAdmin) {
    fireEvent.click(within(dialog).getByLabelText('Make them an admin of Fixture Home'))
  }
  return dialog
}

it('offers an operator who is an admin the way to make an account for someone, and says what they can do', async () => {
  answerWith({ read: circle() })
  renderAt('/circles/circle-one', { isOperator: true })

  expect(await screen.findByRole('button', { name: 'Make an account for someone' })).toBeTruthy()
  expect(screen.getByText(/can change the password themselves in their settings/)).toBeTruthy()
  // The invitation form is still there beside it.
  expect(screen.getByRole('button', { name: 'Send invitation' })).toBeTruthy()
})

it('does not offer making an account to an admin who cannot manage users', async () => {
  answerWith({ read: circle() })
  renderAt('/circles/circle-one')

  expect(await screen.findByRole('button', { name: 'Send invitation' })).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Make an account for someone' })).toBeNull()
  expect(screen.queryByText(/can change the password themselves/)).toBeNull()
})

it('makes the account and then puts it in the circle directly, with no invitation', async () => {
  answerWith({ read: circle() })
  renderAt('/circles/circle-one', { isOperator: true })

  const dialog = await fillAccount({
    username: 'person-d-login',
    name: 'Person D',
    password: 'fixture-password',
    isAdmin: true,
  })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Create' }))

  await waitFor(() => expect(mutationCalls('AddCircleMember')).toHaveLength(1))
  expect(mutationCalls('CreateUser')).toHaveLength(1)
  expect(mutationCalls('CreateUser')[0][1]).toEqual({
    username: 'person-d-login',
    password: 'fixture-password',
    name: 'Person D',
  })
  expect(mutationCalls('AddCircleMember')[0][1]).toEqual({
    circleId: 'circle-one',
    username: 'person-d-login',
    memberRole: 'admin',
  })
  expect(mutationCalls('InviteToCircle')).toHaveLength(0)
  // The account was made before it was put in.
  const operations = vi.mocked(graphql).mock.calls.map(([document]) => document)
  expect(operations.findIndex((document) => document.includes('CreateUser'))).toBeLessThan(
    operations.findIndex((document) => document.includes('AddCircleMember')),
  )
  expect(await screen.findByText('Person D has an account and is in the circle.')).toBeTruthy()
  await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Make an account for someone' })).toBeNull())
})

it('needs a username and a password before an account can be made', async () => {
  answerWith({ read: circle() })
  renderAt('/circles/circle-one', { isOperator: true })

  fireEvent.click(await screen.findByRole('button', { name: 'Make an account for someone' }))
  const dialog = await screen.findByRole('dialog', { name: 'Make an account for someone' })
  const create = within(dialog).getByRole('button', { name: 'Create' }) as HTMLButtonElement
  expect(create.disabled).toBe(true)
  fireEvent.change(within(dialog).getByLabelText('Username'), { target: { value: 'person-d-login' } })
  expect(create.disabled).toBe(true)
  fireEvent.change(within(dialog).getByLabelText('Password'), { target: { value: 'fixture-password' } })
  expect(create.disabled).toBe(false)
})

it('adds an account that was made but could not be put in the circle, without making it again', async () => {
  answerWith({ read: circle() })
  const base = vi.mocked(graphql).getMockImplementation()!
  let isFirstAdd = true
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('AddCircleMember') && isFirstAdd) {
      isFirstAdd = false
      throw new Error('circle not found')
    }
    return base(document, variables)
  })
  renderAt('/circles/circle-one', { isOperator: true })

  const dialog = await fillAccount({ username: 'person-d-login', name: '', password: 'fixture-password' })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Create' }))

  expect(await screen.findByText(/The account person-d-login was made, but .*circle not found/)).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Create' }))

  await waitFor(() => expect(mutationCalls('AddCircleMember')).toHaveLength(2))
  expect(mutationCalls('CreateUser')).toHaveLength(1)
  expect(mutationCalls('AddCircleMember')[1][1]).toEqual({
    circleId: 'circle-one',
    username: 'person-d-login',
    memberRole: 'member',
  })
})

it('puts Leave at the foot of the page, after the members and the changes to the circle', async () => {
  answerWith({ read: circle() })
  const { container } = renderAt('/circles/circle-one')

  const leave = await screen.findByRole('button', { name: 'Leave' })
  const firstCard = container.querySelector('.settings-section.card') as HTMLElement
  expect(firstCard.contains(leave)).toBe(false)
  // After every card, as the last thing on the page.
  const cards = Array.from(container.querySelectorAll('.settings-section.card'))
  expect(leave.compareDocumentPosition(cards[cards.length - 1]) & Node.DOCUMENT_POSITION_PRECEDING).toBeTruthy()
  // The rename and delete icons are in the first card's head row.
  const head = firstCard.querySelector('.settings-section-head') as HTMLElement
  expect(within(head).getByRole('button', { name: 'Fixture Home: Rename' })).toBeTruthy()
  expect(within(head).getByRole('button', { name: 'Fixture Home: Delete circle' })).toBeTruthy()
  expect(head.querySelector('.head-icons')).toBeTruthy()
})

it('says in the Leave confirmation that the last one out deletes the circle and the last admin must hand over', async () => {
  answerWith({ read: circle() })
  renderAt('/circles/circle-one')

  fireEvent.click(await screen.findByRole('button', { name: 'Leave' }))
  const dialog = await screen.findByRole('alertdialog', { name: 'Leave Fixture Home?' })
  expect(within(dialog).getByText(/deleted when its last member leaves/)).toBeTruthy()
  expect(within(dialog).getByText(/only admin, make somebody else an admin first/)).toBeTruthy()
})

it('leaves out the warning about being the only admin when somebody else is an admin too', async () => {
  answerWith({
    read: circle({
      members: [member('person-a', 'admin', 'Person A'), member('person-b', 'admin', 'Person B')],
    }),
  })
  renderAt('/circles/circle-one')

  fireEvent.click(await screen.findByRole('button', { name: 'Leave' }))
  const dialog = await screen.findByRole('alertdialog', { name: 'Leave Fixture Home?' })
  expect(within(dialog).getByText(/deleted when its last member leaves/)).toBeTruthy()
  expect(within(dialog).queryByText(/only admin/)).toBeNull()
})

it('says circles are not on, and offers nothing that would fail, to a person without circle:use', async () => {
  answerWith({ circles: [circle()], read: circle() })
  render(
    <TranslationProvider>
      <ToastProvider>
        <SessionProvider
          value={{
            userId: 'person-a',
            authenticated: true,
            authenticationRequired: true,
            username: 'person-a-login',
            passkeysEnabled: false,
            permissions: { everywhere: ['mail:use'], byDomain: [] },
          }}
        >
          <MemoryRouter initialEntries={['/circles']}>
            <Routes>
              <Route path="/circles" element={<CirclesPage />} />
              <Route path="/circles/:circleId" element={<CirclePage />} />
            </Routes>
          </MemoryRouter>
        </SessionProvider>
      </ToastProvider>
    </TranslationProvider>,
  )
  expect(await screen.findByText(/Circles are not on for your account/)).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'New circle' })).toBeNull()
  // Nothing was asked of the server, which would have refused it all.
  expect(vi.mocked(graphql)).not.toHaveBeenCalled()
})

it('shows the server’s refusal in the Leave confirmation and stays on the page', async () => {
  answerWith({ read: circle() })
  const base = vi.mocked(graphql).getMockImplementation()!
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('LeaveCircle')) {
      throw new Error('a circle needs at least one admin: make somebody else an admin first, or delete the circle')
    }
    return base(document, variables)
  })
  renderAt('/circles/circle-one')

  fireEvent.click(await screen.findByRole('button', { name: 'Leave' }))
  const dialog = await screen.findByRole('alertdialog', { name: 'Leave Fixture Home?' })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Leave' }))

  expect(await screen.findByText(/a circle needs at least one admin/)).toBeTruthy()
  // Still asking, still on the circle: nothing was left.
  expect(screen.getByRole('alertdialog', { name: 'Leave Fixture Home?' })).toBeTruthy()
  expect(screen.getByTestId('where').textContent).toBe('/circles/circle-one')
})
