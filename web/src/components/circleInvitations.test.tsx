import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { TranslationProvider } from '../i18n/i18n'
import { SessionProvider } from '../session'
import { CircleInvitationMark } from './circleInvitationMark'
import { announceCirclesChanged, useLabelWithInvitationCount, useOpenCircleInvitationCount } from './circleInvitations'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

function Count() {
  return <p data-testid="count">{useOpenCircleInvitationCount()}</p>
}

function Label() {
  return <p data-testid="label">{useLabelWithInvitationCount('Account')}</p>
}

function renderShown(permissions: string[]) {
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
        }}
      >
        <Count />
        <Label />
        <CircleInvitationMark />
      </SessionProvider>
    </TranslationProvider>,
  )
}

function readsOfInvitations() {
  return vi.mocked(graphql).mock.calls.filter(([document]) => document.includes('ListCircleInvitations'))
}

it('reads the invitations once for the number, the label and the mark together, and again when told', async () => {
  let waiting = [{ id: 'one' }, { id: 'two' }]
  vi.mocked(graphql).mockImplementation(async () => ({ ListCircleInvitations: waiting }) as never)
  renderShown(['circle:use'])
  await waitFor(() => expect(screen.getByTestId('count').textContent).toBe('2'))
  expect(screen.getByTestId('label').textContent).toContain('Account')
  expect(screen.getByTestId('label').textContent).not.toBe('Account')
  expect(document.querySelector('.circle-invitation-mark')?.textContent).toBe('2')
  expect(readsOfInvitations()).toHaveLength(1)

  // Answering one on the Circles page changes all three at once.
  waiting = [{ id: 'two' }]
  act(() => announceCirclesChanged())
  await waitFor(() => expect(screen.getByTestId('count').textContent).toBe('1'))
  expect(document.querySelector('.circle-invitation-mark')?.textContent).toBe('1')
  expect(readsOfInvitations()).toHaveLength(2)
})

it('asks nothing of somebody who may not use circles', async () => {
  vi.mocked(graphql).mockResolvedValue({ ListCircleInvitations: [{ id: 'one' }] } as never)
  renderShown(['mail:use'])
  expect(screen.getByTestId('count').textContent).toBe('0')
  expect(screen.getByTestId('label').textContent).toBe('Account')
  expect(document.querySelector('.circle-invitation-mark')).toBeNull()
  expect(readsOfInvitations()).toHaveLength(0)
})
