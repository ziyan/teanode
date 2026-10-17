import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { ToastProvider } from '../../components/toast'
import { TranslationProvider } from '../../i18n/i18n'
import { SessionProvider } from '../../session'
import { UsersTab } from './users'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
}))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

function account(id: string, name: string) {
  return {
    id,
    username: `${id}-login`,
    name,
    email: '',
    avatarUrl: null,
    avatarColorIndex: 2,
    disabledAt: null,
    hasPassword: true,
    groupIds: [],
    createdAt: '2030-01-01T00:00:00Z',
  }
}

function answerWith(circles: { id: string; name: string; circleKind: string }[]) {
  vi.mocked(graphql).mockImplementation(async (document) => {
    if (document.includes('ListUsers')) {
      return { ListUsers: [account('person-a', 'Person A'), account('person-b', 'Person B')] } as never
    }
    if (document.includes('ListGroups')) return { ListGroups: [] } as never
    if (document.includes('ListCircles')) return { ListCircles: circles } as never
    return {} as never
  })
}

function renderUsers(permissions: string[]) {
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
          <MemoryRouter>
            <UsersTab />
          </MemoryRouter>
        </SessionProvider>
      </ToastProvider>
    </TranslationProvider>,
  )
}

function mutationCalls(operation: string) {
  return vi.mocked(graphql).mock.calls.filter(([document]) => document.includes(operation))
}

it('offers each account a menu of the operator’s circles, and choosing one adds the account to it', async () => {
  answerWith([
    { id: 'circle-one', name: 'Fixture Home', circleKind: 'family' },
    { id: 'circle-two', name: 'Fixture Team', circleKind: 'colleagues' },
  ])
  renderUsers(['user:manage'])

  fireEvent.click(await screen.findByRole('button', { name: 'person-b-login: Add to a circle' }))
  expect(await screen.findByRole('menuitem', { name: 'Fixture Home' })).toBeTruthy()
  fireEvent.click(screen.getByRole('menuitem', { name: 'Fixture Team' }))

  await waitFor(() => expect(mutationCalls('AddCircleMember')).toHaveLength(1))
  expect(mutationCalls('AddCircleMember')[0][1]).toEqual({
    circleId: 'circle-two',
    username: 'person-b-login',
    memberRole: 'member',
  })
  expect(await screen.findByText('person-b-login is now in Fixture Team.')).toBeTruthy()
})

it('points an operator who is in no circle to the Circles page', async () => {
  answerWith([])
  renderUsers(['user:manage'])

  fireEvent.click(await screen.findByRole('button', { name: 'person-b-login: Add to a circle' }))
  const link = (await screen.findByRole('menuitem', { name: /not in a circle yet/ })) as HTMLAnchorElement
  expect(link.getAttribute('href')).toBe('/circles')
  expect(mutationCalls('AddCircleMember')).toHaveLength(0)
})

it('does not ask for circles or offer the menu to somebody who cannot manage users', async () => {
  answerWith([{ id: 'circle-one', name: 'Fixture Home', circleKind: 'family' }])
  renderUsers([])

  expect(await screen.findByText('Person B')).toBeTruthy()
  expect(screen.queryByRole('button', { name: /Add to a circle/ })).toBeNull()
  expect(vi.mocked(graphql).mock.calls.some(([document]) => document.includes('ListCircles'))).toBe(false)
})
