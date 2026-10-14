import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { graphql } from '../../api'
import { TranslationProvider } from '../../i18n/i18n'
import { SessionProvider } from '../../session'
import { ToastProvider } from '../../components/toast'
import { RedirectKeepingQuery } from '../../components/redirectKeepingQuery'
import { DevicesPage } from './devices'

vi.mock('../../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../api')>()),
  graphql: vi.fn(),
}))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

function answerEverything(phones: unknown[] = []) {
  vi.mocked(graphql).mockImplementation(async (document) => {
    if (document.includes('ListAgentPhones'))
      return { ListAgentPhones: { phones, isPushEnabled: true, notificationKinds: [] } } as never
    if (document.includes('ListSyncCredentials')) return { ListSyncCredentials: [] } as never
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
}

function Where() {
  const location = useLocation()
  return <p data-testid="where">{location.pathname + location.search}</p>
}

function renderAt(entry: string) {
  return render(
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
          <MemoryRouter initialEntries={[entry]}>
            <Routes>
              <Route path="/settings/devices" element={<DevicesPage />} />
            </Routes>
            <Where />
          </MemoryRouter>
        </SessionProvider>
      </ToastProvider>
    </TranslationProvider>,
  )
}

it('shows the TeaNode app and Built-in apps sections on one page', async () => {
  answerEverything()
  renderAt('/settings/devices')
  expect(await screen.findByRole('heading', { name: 'TeaNode app' })).toBeTruthy()
  expect(screen.getByRole('heading', { name: 'Built-in apps' })).toBeTruthy()
  expect(await screen.findByRole('link', { name: 'Connect a phone' })).toBeTruthy()
  expect(await screen.findByRole('button', { name: /Set up an? (iPhone or Mac|phone)|Set up this/i })).toBeTruthy()
})

it('names a phone by its model as people say it, not by the identifier the phone reports', async () => {
  answerEverything([
    {
      id: 'fixture-phone',
      phoneName: 'Fixture phone',
      phoneModel: 'iPhone18,2',
      phoneModelName: 'iPhone 17 Pro Max',
      notificationKinds: [],
      canBeNotified: true,
      lastSeenAt: null,
      lastDeliveredAt: null,
      lastDeliveryError: '',
    },
  ])
  renderAt('/settings/devices')
  expect(await screen.findByText('iPhone 17 Pro Max · Notifications: all')).toBeTruthy()
  expect(screen.queryByText(/iPhone18,2/)).toBeNull()
})

it('opens the setup dialog from ?setup=1 on the Devices path', async () => {
  answerEverything()
  renderAt('/settings/devices?setup=1&mailboxId=fixture-mailbox')
  expect(await screen.findByLabelText('Device name')).toBeTruthy()
  expect(screen.getByTestId('where').textContent).toBe('/settings/devices?setup=1&mailboxId=fixture-mailbox')
})

it('sends the old Phones and Device setup paths to Devices, keeping the setup query', () => {
  for (const [from, expected] of [
    ['/settings/phones', '/settings/devices'],
    ['/settings/sync', '/settings/devices'],
    ['/settings/sync?setup=1&mailboxId=fixture-mailbox', '/settings/devices?setup=1&mailboxId=fixture-mailbox'],
  ]) {
    cleanup()
    render(
      <MemoryRouter initialEntries={[from]}>
        <Routes>
          <Route path="/settings/phones" element={<RedirectKeepingQuery to="/settings/devices" />} />
          <Route path="/settings/sync" element={<RedirectKeepingQuery to="/settings/devices" />} />
          <Route path="/settings/devices" element={<Where />} />
        </Routes>
      </MemoryRouter>,
    )
    expect(screen.getByTestId('where').textContent).toBe(expected)
  }
})
