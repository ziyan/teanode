import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { graphql } from '../api'
import { TranslationProvider } from '../i18n/i18n'
import { ToastProvider } from './toast'
import { DeviceSetupDialog, type DeviceSetupCapabilities } from './deviceSetup'

vi.mock('../api', () => ({ graphql: vi.fn() }))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
  vi.useRealTimers()
})
const collectionCapabilities: DeviceSetupCapabilities = {
  canSetUpMail: false,
  canSetUpCalendar: true,
  canSetUpReminders: true,
  canSetUpContacts: false,
  mailboxes: [],
}
function dialog(capabilities = collectionCapabilities, selectedMailboxId?: string) {
  return (
    <TranslationProvider>
      <ToastProvider>
        <DeviceSetupDialog
          capabilities={capabilities}
          selectedMailboxId={selectedMailboxId}
          onClose={vi.fn()}
          onCreated={async () => {}}
        />
      </ToastProvider>
    </TranslationProvider>
  )
}
function successfulProfile(expiryMS = Date.now() + 60_000) {
  vi.mocked(graphql).mockResolvedValue({
    CreateDeviceSetupProfile: {
      downloadLink: 'https://setup.example.com/profile/fixture-link',
      qrCodeSvg: '<svg xmlns="http://www.w3.org/2000/svg"/>',
      expiresAt: new Date(expiryMS).toISOString(),
      mailboxAppPassword: null,
      syncCredential: { id: 'fixture-sync-credential', credentialName: 'Fixture device' },
    },
  })
}
it('creates mailbox-free selected collections without displaying unavailable services or password secrets', async () => {
  successfulProfile()
  render(dialog())
  expect(screen.queryByRole('checkbox', { name: 'Mail' })).toBeNull()
  expect(screen.queryByRole('checkbox', { name: 'Contacts' })).toBeNull()
  fireEvent.change(screen.getByLabelText('Device name'), { target: { value: 'Fixture device' } })
  fireEvent.click(screen.getByRole('checkbox', { name: 'Reminders' }))
  fireEvent.click(screen.getByRole('button', { name: 'Create a device setup profile' }))
  await screen.findByRole('alertdialog')
  expect(vi.mocked(graphql).mock.calls[0][1]).toEqual({
    deviceName: 'Fixture device',
    mailboxId: null,
    hasMailAccess: false,
    hasCalendarAccess: true,
    hasRemindersAccess: false,
    hasContactsAccess: false,
  })
  expect(vi.mocked(graphql).mock.calls[0][0]).not.toMatch(/\bpassword\b/)
  expect(screen.getByRole('link', { name: 'Open setup profile on this device' }).getAttribute('href')).toBe(
    'https://setup.example.com/profile/fixture-link',
  )
})
it('preselects only an explicitly owned mailbox shortcut and never substitutes a different mailbox', () => {
  const capabilities = {
    ...collectionCapabilities,
    canSetUpMail: true,
    mailboxes: [{ id: 'owned-mailbox', name: 'Fixture mailbox', addresses: [{ address: 'fixture@example.com' }] }],
  }
  const view = render(dialog(capabilities, 'owned-mailbox'))
  expect((screen.getByRole('checkbox', { name: 'Mail' }) as HTMLInputElement).checked).toBe(true)
  expect((screen.getByLabelText('Mail mailbox') as HTMLSelectElement).value).toBe('owned-mailbox')
  view.unmount()
  render(dialog(capabilities, 'unavailable-mailbox'))
  expect((screen.getByRole('checkbox', { name: 'Mail' }) as HTMLInputElement).checked).toBe(false)
  expect(screen.getByText(/The requested mailbox is unavailable/)).toBeTruthy()
  fireEvent.click(screen.getByRole('checkbox', { name: 'Mail' }))
  fireEvent.change(screen.getByLabelText('Device name'), { target: { value: 'Fixture device' } })
  expect((screen.getByRole('button', { name: 'Create a device setup profile' }) as HTMLButtonElement).disabled).toBe(
    true,
  )
})
it('drops revoked capabilities before sending a profile request, retaining the visible remaining selection', async () => {
  successfulProfile()
  const view = render(dialog({ ...collectionCapabilities, canSetUpContacts: true }))
  fireEvent.change(screen.getByLabelText('Device name'), { target: { value: 'Fixture device' } })
  view.rerender(dialog(collectionCapabilities))
  expect(screen.queryByRole('checkbox', { name: 'Contacts' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Create a device setup profile' }))
  await waitFor(() => expect(graphql).toHaveBeenCalledOnce())
  expect(vi.mocked(graphql).mock.calls[0][1]).toMatchObject({ hasContactsAccess: false })
})
it('keeps entered fields after a refused profile request and refuses an empty selection', async () => {
  vi.mocked(graphql).mockRejectedValue(new Error('HTTPS is required for this fixture'))
  render(dialog())
  fireEvent.change(screen.getByLabelText('Device name'), { target: { value: 'Fixture device' } })
  fireEvent.click(screen.getByRole('button', { name: 'Create a device setup profile' }))
  await screen.findByText('HTTPS is required for this fixture')
  expect((screen.getByLabelText('Device name') as HTMLInputElement).value).toBe('Fixture device')
  fireEvent.click(screen.getByRole('checkbox', { name: 'Calendar' }))
  fireEvent.click(screen.getByRole('checkbox', { name: 'Reminders' }))
  expect((screen.getByRole('button', { name: 'Create a device setup profile' }) as HTMLButtonElement).disabled).toBe(
    true,
  )
})
it('explains removed services inside the open selector instead of offering a blank form', () => {
  render(
    dialog({
      canSetUpMail: false,
      canSetUpCalendar: false,
      canSetUpReminders: false,
      canSetUpContacts: false,
      mailboxes: [],
    }),
  )
  expect(
    screen.getByText(
      'No services are available for device setup. An operator can enable Mail, Calendar, Reminders or Contacts and grant access.',
    ),
  ).toBeTruthy()
  expect((screen.getByRole('button', { name: 'Create a device setup profile' }) as HTMLButtonElement).disabled).toBe(
    true,
  )
})

it('keeps an already expired receipt and access details while disabling its download and QR', async () => {
  successfulProfile(Date.now() - 1)
  render(dialog())
  fireEvent.change(screen.getByLabelText('Device name'), { target: { value: 'Fixture device' } })
  fireEvent.click(screen.getByRole('button', { name: 'Create a device setup profile' }))
  await screen.findByRole('alertdialog')
  expect(screen.queryByRole('link', { name: 'Open setup profile on this device' })).toBeNull()
  expect(
    (screen.getByRole('button', { name: 'Open setup profile on this device' }) as HTMLButtonElement).disabled,
  ).toBe(true)
  expect(screen.queryByRole('img')).toBeNull()
  expect(screen.getByText(/This setup link has expired/)).toBeTruthy()
  expect(screen.getByText('https://setup.example.com/profile/fixture-link')).toBeTruthy()
  expect(screen.getByText(/Mail access can be revoked/)).toBeTruthy()
  expect(graphql).toHaveBeenCalledOnce()
})
it('expires the visible receipt without regenerating it and rejects a click if a background timer is delayed', async () => {
  vi.useFakeTimers()
  successfulProfile(Date.now() + 1000)
  render(dialog())
  fireEvent.change(screen.getByLabelText('Device name'), { target: { value: 'Fixture device' } })
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: 'Create a device setup profile' }))
  })
  const download = screen.getByRole('link', { name: 'Open setup profile on this device' })
  vi.setSystemTime(Date.now() + 1001)
  expect(fireEvent.click(download)).toBe(false)
  expect(screen.queryByRole('link', { name: 'Open setup profile on this device' })).toBeNull()
  expect(
    (screen.getByRole('button', { name: 'Open setup profile on this device' }) as HTMLButtonElement).disabled,
  ).toBe(true)
  expect(graphql).toHaveBeenCalledOnce()
})
it('updates expiry on its timer and keeps an unexpired receipt downloadable after service refresh', async () => {
  vi.useFakeTimers()
  successfulProfile(Date.now() + 1000)
  const view = render(dialog())
  fireEvent.change(screen.getByLabelText('Device name'), { target: { value: 'Fixture device' } })
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: 'Create a device setup profile' }))
  })
  view.rerender(
    dialog({
      canSetUpMail: false,
      canSetUpCalendar: false,
      canSetUpReminders: false,
      canSetUpContacts: false,
      mailboxes: [],
    }),
  )
  const download = screen.getByRole('link', { name: 'Open setup profile on this device' })
  expect(download.getAttribute('target')).toBe('_blank')
  expect(download.getAttribute('rel')).toBe('noopener noreferrer')
  await act(async () => {
    vi.advanceTimersByTime(1001)
  })
  expect(screen.queryByRole('link', { name: 'Open setup profile on this device' })).toBeNull()
  expect(screen.getByText('https://setup.example.com/profile/fixture-link')).toBeTruthy()
  expect(graphql).toHaveBeenCalledOnce()
})
