import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { SessionProvider } from '../session'
import { SchedulesCard } from './agent'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))

const clockSchedule = {
  id: 'clock',
  name: 'Morning round-up',
  cron: '0 8 * * 1-5',
  mailConditions: null,
  prompt: 'Tell me what is on today.',
  deliver: 'drawer',
  enabled: true,
  nextRunAt: null,
}

const mailSchedule = {
  id: 'mail',
  name: 'School notices',
  cron: '',
  mailConditions: [{ field: 'from', header: '', operator: 'contains', value: 'news@school.example' }],
  prompt: 'Add anything dated to my calendar.',
  deliver: 'drawer',
  enabled: true,
  nextRunAt: null,
}

beforeEach(() => {
  vi.mocked(graphql).mockReset()
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('ListAgentSchedules')) return { ListAgentSchedules: [clockSchedule, mailSchedule] } as never
    if (document.includes('SaveAgentSchedule')) return { SaveAgentSchedule: { id: 'saved' } } as never
    return {} as never
  })
})
afterEach(cleanup)

function show() {
  render(
    <TranslationProvider>
      <SessionProvider
        value={{
          authenticated: true,
          authenticationRequired: true,
          passkeysEnabled: false,
          username: 'fixture',
          userId: 'owner',
        }}
      >
        <ToastProvider>
          <SchedulesCard />
        </ToastProvider>
      </SessionProvider>
    </TranslationProvider>,
  )
}

const saves = () => vi.mocked(graphql).mock.calls.filter(([document]) => document.includes('SaveAgentSchedule'))

it('says a mail schedule runs when mail arrives, in place of a cron line', async () => {
  show()
  expect(await screen.findByText(/When mail arrives From contains news@school\.example/)).toBeTruthy()
  expect(screen.getByText('0 8 * * 1-5')).toBeTruthy()
  // The mail schedule has no cron line to show.
  expect(document.querySelectorAll('code.tag')).toHaveLength(1)
})

it('saves a schedule that runs on mail with its conditions and no cron line', async () => {
  show()
  await screen.findByText('School notices')
  fireEvent.click(screen.getByRole('button', { name: 'Add a schedule' }))
  const dialog = await screen.findByRole('dialog')
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Parcel notices' } })
  fireEvent.click(within(dialog).getByRole('combobox', { name: 'When' }))
  fireEvent.click(screen.getByRole('option', { name: 'When mail arrives' }))
  // The cron line is gone; one condition, from contains, is waiting to be filled in.
  expect(within(dialog).queryByText('When, as a cron line')).toBeNull()
  const submit = dialog.querySelector('button[type="submit"]') as HTMLButtonElement
  fireEvent.change(within(dialog).getByLabelText('What to do, as you would say it'), {
    target: { value: 'Tell me which parcel is coming.' },
  })
  expect(submit.disabled).toBe(true)
  fireEvent.change(within(dialog).getByLabelText('What it looks for'), { target: { value: 'orders@shop.example' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Add a condition' }))
  expect(within(dialog).getAllByLabelText('What it looks for')).toHaveLength(2)
  fireEvent.change(within(dialog).getAllByLabelText('What it looks for')[1], { target: { value: 'parcel' } })
  fireEvent.click(submit)
  await waitFor(() => expect(saves()).toHaveLength(1))
  const variables = saves()[0][1] as Record<string, unknown>
  expect(variables.mailConditions).toEqual([
    { field: 'from', header: '', operator: 'contains', value: 'orders@shop.example' },
    { field: 'subject', header: '', operator: 'contains', value: 'parcel' },
  ])
  expect('cron' in variables).toBe(false)
  expect(variables.enabled).toBe(true)
})

it('sends the cron line and no conditions when a mail schedule is switched back to the clock', async () => {
  show()
  await screen.findByText('School notices')
  fireEvent.click(screen.getByRole('button', { name: 'School notices: Edit' }))
  const dialog = await screen.findByRole('dialog')
  expect(within(dialog).getByDisplayValue('news@school.example')).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('combobox', { name: 'When' }))
  fireEvent.click(screen.getByRole('option', { name: 'At set times' }))
  fireEvent.change(within(dialog).getByLabelText('When, as a cron line'), { target: { value: '0 7 * * 1' } })
  fireEvent.click(dialog.querySelector('button[type="submit"]')!)
  await waitFor(() => expect(saves()).toHaveLength(1))
  const variables = saves()[0][1] as Record<string, unknown>
  expect(variables.cron).toBe('0 7 * * 1')
  expect('mailConditions' in variables).toBe(false)
})
