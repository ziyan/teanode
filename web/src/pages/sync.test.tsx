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
          expiresAt: '2030-01-01T00:00:00Z',
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
