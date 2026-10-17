import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { graphql } from '../api'
import { TranslationProvider } from '../i18n/i18n'
import { SessionProvider } from '../session'
import { ToastProvider } from '../components/toast'
import { SyncPage } from './sync'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})
it('retains the one-time setup receipt while refreshing credentials after profile creation', async () => {
  let credentialReadCount = 0
  let finishCredentialRefresh: ((answer: unknown) => void) | undefined
  vi.mocked(graphql).mockImplementation(async (document) => {
    if (document.includes('ListSyncCredentials')) {
      credentialReadCount++
      if (credentialReadCount === 1) return { ListSyncCredentials: [] } as never
      return (await new Promise<unknown>((resolve) => {
        finishCredentialRefresh = resolve
      })) as never
    }
    if (document.includes('GetDeviceSetupCapabilities'))
      return {
        GetDeviceSetupCapabilities: {
          canSetUpMail: false,
          canSetUpCalendar: true,
          canSetUpReminders: false,
          canSetUpContacts: false,
          mailboxes: [],
        },
      } as never
    if (document.includes('CreateDeviceSetupProfile'))
      return {
        CreateDeviceSetupProfile: {
          downloadLink: '/synthetic-receipt',
          qrCodeSvg: '<svg xmlns="http://www.w3.org/2000/svg"/>',
          expiresAt: new Date(Date.now() + 60_000).toISOString(),
          mailboxAppPassword: null,
          syncCredential: { id: 'fixture-sync', credentialName: 'Fixture device' },
        },
      } as never
    throw new Error('Unexpected fixture query')
  })
  render(
    <TranslationProvider>
      <ToastProvider>
        <SessionProvider
          value={{
            userId: 'fixture-owner',
            authenticated: true,
            authenticationRequired: true,
            username: 'fixture',
            passkeysEnabled: false,
          }}
        >
          <MemoryRouter initialEntries={['/settings/sync?setup=1']}>
            <SyncPage />
          </MemoryRouter>
        </SessionProvider>
      </ToastProvider>
    </TranslationProvider>,
  )
  fireEvent.change(await screen.findByLabelText('Device name'), { target: { value: 'Fixture device' } })
  fireEvent.click(screen.getByRole('button', { name: 'Create a device setup profile' }))
  await waitFor(() => expect(credentialReadCount).toBe(2))
  expect(await screen.findByRole('alertdialog', { name: 'Set up Fixture device' })).toBeTruthy()
  expect(screen.getByRole('link', { name: 'Open setup profile on this device' }).getAttribute('href')).toBe(
    '/synthetic-receipt',
  )
  finishCredentialRefresh?.({ ListSyncCredentials: [] })
  expect(await screen.findByRole('alertdialog', { name: 'Set up Fixture device' })).toBeTruthy()
})

it('lists the newest password first with its created time and last use', async () => {
  const fixtureCredential = (id: string, createdAt: string, lastUsedAt: string | null) => ({
    id,
    credentialName: 'iPhone',
    hasCalendarAccess: true,
    hasRemindersAccess: false,
    hasContactsAccess: false,
    hasPhotosAccess: false,
    createdAt,
    lastUsedAt,
  })
  vi.mocked(graphql).mockImplementation(async (document) => {
    if (document.includes('ListSyncCredentials'))
      return {
        ListSyncCredentials: [
          fixtureCredential('older', new Date(Date.now() - 5 * 86_400_000).toISOString(), null),
          fixtureCredential(
            'newer',
            new Date(Date.now() - 86_400_000).toISOString(),
            new Date(Date.now() - 5 * 60_000).toISOString(),
          ),
        ],
      } as never
    if (document.includes('GetDeviceSetupCapabilities'))
      return {
        GetDeviceSetupCapabilities: {
          canSetUpMail: false,
          canSetUpCalendar: true,
          canSetUpReminders: false,
          canSetUpContacts: false,
          mailboxes: [],
        },
      } as never
    throw new Error('Unexpected fixture query')
  })
  render(
    <TranslationProvider>
      <ToastProvider>
        <SessionProvider
          value={{
            userId: 'fixture-owner',
            authenticated: true,
            authenticationRequired: true,
            username: 'fixture',
            passkeysEnabled: false,
          }}
        >
          <MemoryRouter initialEntries={['/settings/sync']}>
            <SyncPage />
          </MemoryRouter>
        </SessionProvider>
      </ToastProvider>
    </TranslationProvider>,
  )
  await screen.findAllByText('iPhone')
  const rows = Array.from(document.querySelectorAll('.settings-row-subtitle'))
  expect(rows).toHaveLength(2)
  expect(rows[0].textContent).toContain('Last used 5 minutes ago')
  expect(rows[0].textContent).not.toContain('safe to delete')
  expect(rows[1].textContent).toContain('Never used')
  expect(rows[1].textContent).toContain('May be safe to delete')
})
