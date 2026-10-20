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

// What a member is to the caller, as the server sends it: the kind and the
// gender of the word, empty when nothing has been said.
function member(
  userId: string,
  memberRole: 'admin' | 'member',
  name: string,
  relationship: { kind: string; gender: string; word: string } = { kind: '', gender: '', word: '' },
): CircleMember {
  return {
    userId,
    username: `${userId}-login`,
    name,
    avatarUrl: null,
    avatarColorIndex: 3,
    memberRole,
    joinedAt: '2030-01-01T00:00:00Z',
    relationshipKind: relationship.kind,
    relationshipTermGender: relationship.gender,
    relationshipWord: relationship.word,
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

// The words a relationship can be said with, as the server lists them.
const TERMS = [
  { term: 'spouse', relationshipKind: 'spouse', termGender: 'neutral', word: 'spouse' },
  { term: 'husband', relationshipKind: 'spouse', termGender: 'masculine', word: 'husband' },
  { term: 'wife', relationshipKind: 'spouse', termGender: 'feminine', word: 'wife' },
  { term: 'parent', relationshipKind: 'parent', termGender: 'neutral', word: 'parent' },
  { term: 'father', relationshipKind: 'parent', termGender: 'masculine', word: 'father' },
  { term: 'mother', relationshipKind: 'parent', termGender: 'feminine', word: 'mother' },
  { term: 'child', relationshipKind: 'child', termGender: 'neutral', word: 'child' },
  { term: 'son', relationshipKind: 'child', termGender: 'masculine', word: 'son' },
  { term: 'daughter', relationshipKind: 'child', termGender: 'feminine', word: 'daughter' },
  { term: 'cousin', relationshipKind: 'cousin', termGender: 'neutral', word: 'cousin' },
  { term: 'friend', relationshipKind: 'friend', termGender: 'neutral', word: 'friend' },
  { term: 'colleague', relationshipKind: 'colleague', termGender: 'neutral', word: 'colleague' },
]

function answerWith(shown: {
  circles?: Circle[]
  invitations?: CircleInvitation[]
  read?: Circle
  // How the signed-in person is described; empty until they say.
  pronoun?: string
}) {
  vi.mocked(graphql).mockImplementation(async (document) => {
    if (document.includes('ListRelationshipTerms')) return { ListRelationshipTerms: TERMS } as never
    if (document.includes('GetCurrentUser')) {
      return { GetCurrentUser: { id: 'person-a', pronoun: shown.pronoun ?? '' } } as never
    }
    if (document.includes('ListCircles')) return { ListCircles: shown.circles ?? [] } as never
    if (document.includes('ListCircleInvitations')) return { ListCircleInvitations: shown.invitations ?? [] } as never
    if (document.includes('ReadCircle')) return { ReadCircle: shown.read } as never
    if (document.includes('CreateCircle')) return { CreateCircle: { id: 'circle-new' } } as never
    return {} as never
  })
}

function Where() {
  const location = useLocation()
  return (
    <>
      <p data-testid="where">{location.pathname}</p>
      <p data-testid="search">{location.search}</p>
    </>
  )
}

// The signed-in person holds user:manage when they are an operator.
function renderAt(entry: string, options: { isOperator?: boolean; canUsePhotos?: boolean } = {}) {
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
            permissions: {
              everywhere: [
                ...(options.isOperator ? ['user:manage', 'circle:use'] : ['circle:use']),
                ...(options.canUsePhotos ? ['photos:use'] : []),
              ],
              byDomain: [],
            },
            services: options.canUsePhotos ? ({ isPhotosEnabled: true } as never) : undefined,
          }}
        >
          <MemoryRouter initialEntries={[entry]}>
            <Routes>
              <Route path="/circles" element={<CirclesPage />} />
              <Route path="/circles/:circleId" element={<CirclePage />} />
              <Route path="/photos" element={<p>The photos page</p>} />
              <Route path="/people/:userId" element={<p>The card page</p>} />
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

it("opens another member's card from their name, and not the signed-in person's own", async () => {
  answerWith({ read: circle() })
  renderAt('/circles/circle-one')

  const name = await screen.findByRole('link', { name: 'Person B' })
  expect(name.getAttribute('href')).toBe('/people/person-b')
  // The caller's own name opens nothing, and the menu beside the other stays.
  expect(screen.queryByRole('link', { name: 'Person A' })).toBeNull()
  expect(screen.getByRole('button', { name: 'Person B: Actions' })).toBeTruthy()
  fireEvent.click(name)
  await waitFor(() => expect(screen.getByTestId('where').textContent).toBe('/people/person-b'))
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
  expect(screen.queryByRole('button', { name: /Rename/ })).toBeNull()
  expect(screen.queryByRole('button', { name: /Delete circle/ })).toBeNull()
  expect(screen.getByRole('button', { name: 'Leave' })).toBeTruthy()
  // The row menu is there for the relationship, and offers nothing else.
  fireEvent.click(screen.getByRole('button', { name: /Person B: Actions/ }))
  expect(await screen.findByRole('menuitem', { name: 'Relationship' })).toBeTruthy()
  expect(screen.queryByRole('menuitem', { name: 'Make member' })).toBeNull()
  expect(screen.queryByRole('menuitem', { name: 'Remove from the circle' })).toBeNull()
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

// A circle in which Person B is whatever the test says, and the caller is Person A.
function circleWithB(relationship?: { kind: string; gender: string; word: string }) {
  return circle({
    members: [member('person-a', 'admin', 'Person A'), member('person-b', 'member', 'Person B', relationship)],
  })
}

async function openRelationshipDialog() {
  fireEvent.click(await screen.findByRole('button', { name: /Person B: Actions/ }))
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Relationship' }))
  const dialog = await screen.findByRole('dialog', { name: 'Relationship with Person B' })
  // The terms arrive after the dialog opens.
  await within(dialog).findByRole('option', { name: 'Father' })
  return dialog
}

it('shows what a member is to the person under their name, and nothing for the person themselves', async () => {
  answerWith({ read: circleWithB({ kind: 'parent', gender: 'masculine', word: 'father' }) })
  renderAt('/circles/circle-one')

  expect(await screen.findByText(/^Father/)).toBeTruthy()
  expect(screen.getAllByText(/^Father/)).toHaveLength(1)
})

it('words the relationship for a kind with one word whatever the gender', async () => {
  answerWith({ read: circleWithB({ kind: 'cousin', gender: 'neutral', word: 'cousin' }) })
  renderAt('/circles/circle-one')

  expect(await screen.findByText(/^Cousin/)).toBeTruthy()
})

it('sets a relationship with the chosen term, and says what the other person will see', async () => {
  answerWith({ read: circleWithB(), pronoun: 'she' })
  renderAt('/circles/circle-one')

  const dialog = await openRelationshipDialog()
  fireEvent.change(within(dialog).getByLabelText('Person B is your'), { target: { value: 'father' } })
  // How the person is described words the other side: a daughter, not a child.
  expect(within(dialog).getByText('Person B will see you as their daughter.')).toBeTruthy()
  // She has said how she is described, so she is not asked.
  expect(within(dialog).queryByLabelText('How should people describe you?')).toBeNull()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

  await waitFor(() => expect(mutationCalls('SetPersonRelationship')).toHaveLength(1))
  expect(mutationCalls('SetPersonRelationship')[0][1]).toEqual({ userId: 'person-b', relationshipTerm: 'father' })
  expect(mutationCalls('UpdateUser')).toHaveLength(0)
  expect(await screen.findByText('Saved how you are related to Person B.')).toBeTruthy()
  await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Relationship with Person B' })).toBeNull())
})

it('asks how the person is described, only when they have not said and the other word has genders', async () => {
  answerWith({ read: circleWithB(), pronoun: '' })
  renderAt('/circles/circle-one')

  const dialog = await openRelationshipDialog()
  // Nothing chosen: nothing to ask about.
  expect(within(dialog).queryByLabelText('How should people describe you?')).toBeNull()

  fireEvent.change(within(dialog).getByLabelText('Person B is your'), { target: { value: 'father' } })
  expect(within(dialog).getByText('Person B will see you as their child.')).toBeTruthy()
  fireEvent.change(within(dialog).getByLabelText('How should people describe you?'), { target: { value: 'he' } })
  expect(within(dialog).getByText('Person B will see you as their son.')).toBeTruthy()

  // A cousin is a cousin whoever says it: no question.
  fireEvent.change(within(dialog).getByLabelText('Person B is your'), { target: { value: 'cousin' } })
  expect(within(dialog).queryByLabelText('How should people describe you?')).toBeNull()
  fireEvent.change(within(dialog).getByLabelText('Person B is your'), { target: { value: 'friend' } })
  expect(within(dialog).queryByLabelText('How should people describe you?')).toBeNull()
  fireEvent.change(within(dialog).getByLabelText('Person B is your'), { target: { value: 'colleague' } })
  expect(within(dialog).queryByLabelText('How should people describe you?')).toBeNull()
})

it('saves the pronoun before the relationship', async () => {
  answerWith({ read: circleWithB(), pronoun: '' })
  renderAt('/circles/circle-one')

  const dialog = await openRelationshipDialog()
  fireEvent.change(within(dialog).getByLabelText('Person B is your'), { target: { value: 'father' } })
  fireEvent.change(within(dialog).getByLabelText('How should people describe you?'), { target: { value: 'she' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

  await waitFor(() => expect(mutationCalls('SetPersonRelationship')).toHaveLength(1))
  expect(mutationCalls('UpdateUser')).toHaveLength(1)
  expect(mutationCalls('UpdateUser')[0][1]).toEqual({ userId: 'person-a', pronoun: 'she' })
  const operations = vi.mocked(graphql).mock.calls.map(([document]) => document)
  expect(operations.findIndex((document) => document.includes('UpdateUser'))).toBeLessThan(
    operations.findIndex((document) => document.includes('SetPersonRelationship')),
  )
})

it('leaves the pronoun alone when the question is left unanswered', async () => {
  answerWith({ read: circleWithB(), pronoun: '' })
  renderAt('/circles/circle-one')

  const dialog = await openRelationshipDialog()
  fireEvent.change(within(dialog).getByLabelText('Person B is your'), { target: { value: 'mother' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

  await waitFor(() => expect(mutationCalls('SetPersonRelationship')).toHaveLength(1))
  expect(mutationCalls('UpdateUser')).toHaveLength(0)
})

it('keeps the word the person chose when only the pronoun question is answered', async () => {
  // Person B is shown as "mother" because of how they are described; the
  // person may have said "parent", which re-saving the shown term would undo.
  answerWith({ read: circleWithB({ kind: 'parent', gender: 'feminine', word: 'mother' }), pronoun: '' })
  renderAt('/circles/circle-one')

  const dialog = await openRelationshipDialog()
  fireEvent.change(within(dialog).getByLabelText('How should people describe you?'), { target: { value: 'he' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

  await waitFor(() => expect(mutationCalls('UpdateUser')).toHaveLength(1))
  expect(mutationCalls('SetPersonRelationship')).toHaveLength(0)
  expect(mutationCalls('RemovePersonRelationship')).toHaveLength(0)
})

it('shows the term the person has now, and None removes it', async () => {
  answerWith({ read: circleWithB({ kind: 'parent', gender: 'masculine', word: 'father' }) })
  renderAt('/circles/circle-one')

  const dialog = await openRelationshipDialog()
  const select = within(dialog).getByLabelText('Person B is your') as HTMLSelectElement
  expect(select.value).toBe('father')
  // Nothing changed yet, so nothing to save.
  expect((within(dialog).getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)

  fireEvent.change(select, { target: { value: '' } })
  expect(within(dialog).getByText('This also removes it for Person B.')).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

  await waitFor(() => expect(mutationCalls('RemovePersonRelationship')).toHaveLength(1))
  expect(mutationCalls('RemovePersonRelationship')[0][1]).toEqual({ userId: 'person-b' })
  expect(mutationCalls('SetPersonRelationship')).toHaveLength(0)
  expect(await screen.findByText('Removed how you are related to Person B.')).toBeTruthy()
})

it('says the refusal in a toast and keeps the dialog open when the relationship cannot be saved', async () => {
  answerWith({ read: circleWithB(), pronoun: 'they' })
  const base = vi.mocked(graphql).getMockImplementation()!
  vi.mocked(graphql).mockImplementation(async (document, variables) => {
    if (document.includes('SetPersonRelationship')) throw new Error('you share no circle with them')
    return base(document, variables)
  })
  renderAt('/circles/circle-one')

  const dialog = await openRelationshipDialog()
  fireEvent.change(within(dialog).getByLabelText('Person B is your'), { target: { value: 'friend' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

  expect(await screen.findByText(/you share no circle with them/)).toBeTruthy()
  expect(screen.getByRole('dialog', { name: 'Relationship with Person B' })).toBeTruthy()
})

it('offers Photos in the menu of a member the person has a contact for, and opens that person in the library', async () => {
  const linked = { ...member('person-b', 'member', 'Person B'), linkedContactId: 'contact-1' }
  answerWith({ read: circle({ members: [member('person-a', 'admin', 'Person A'), linked] }) })
  renderAt('/circles/circle-one', { canUsePhotos: true })

  fireEvent.click(await screen.findByRole('button', { name: /Person B: Actions/ }))
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Photos' }))
  await waitFor(() => expect(screen.getByTestId('where').textContent).toBe('/photos'))
})

it('opens the library with the member as the person in the address', async () => {
  const linked = { ...member('person-b', 'member', 'Person B'), linkedContactId: 'contact-1' }
  answerWith({ read: circle({ members: [member('person-a', 'admin', 'Person A'), linked] }) })
  renderAt('/circles/circle-one', { canUsePhotos: true })
  expect(await screen.findByText('Person B')).toBeTruthy()
  fireEvent.click(await screen.findByRole('button', { name: /Person B: Actions/ }))
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Photos' }))
  await waitFor(() => expect(screen.getByTestId('search').textContent).toBe('?person=person-b'))
})

it('offers no Photos for a member with no contact of the person, or to a person who cannot use Photos', async () => {
  const linked = { ...member('person-b', 'member', 'Person B'), linkedContactId: 'contact-1' }
  answerWith({ read: circle({ members: [member('person-a', 'admin', 'Person A'), linked] }) })
  renderAt('/circles/circle-one')
  fireEvent.click(await screen.findByRole('button', { name: /Person B: Actions/ }))
  await screen.findByRole('menuitem', { name: 'Relationship' })
  expect(screen.queryByRole('menuitem', { name: 'Photos' })).toBeNull()
  cleanup()

  answerWith({
    read: circle({ members: [member('person-a', 'admin', 'Person A'), { ...linked, linkedContactId: '' }] }),
  })
  renderAt('/circles/circle-one', { canUsePhotos: true })
  fireEvent.click(await screen.findByRole('button', { name: /Person B: Actions/ }))
  await screen.findByRole('menuitem', { name: 'Relationship' })
  expect(screen.queryByRole('menuitem', { name: 'Photos' })).toBeNull()
})
