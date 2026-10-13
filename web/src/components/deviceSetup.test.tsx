import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { graphql } from '../api'
import { TranslationProvider } from '../i18n/i18n'
import { ToastProvider } from './toast'
import { DeviceSetupDialog, type DeviceSetupCapabilities } from './deviceSetup'

vi.mock('../api', () => ({ graphql: vi.fn() }))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
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
function successfulProfile() {
  vi.mocked(graphql).mockResolvedValue({
    CreateDeviceSetupProfile: {
      downloadLink: 'https://setup.example.com/profile/fixture-link',
      qrCodeSvg: '<svg xmlns="http://www.w3.org/2000/svg"/>',
      expiresAt: new Date(Date.now() + 60_000).toISOString(),
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
