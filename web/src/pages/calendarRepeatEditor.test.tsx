import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { CalendarPage } from './calendar'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))
vi.mock('../session', () => ({
  useSession: () => ({ userId: 'owner-1', services: { isCalendarEnabled: true, isRemindersEnabled: true } }),
}))

const calendars = [
  {
    id: 'own',
    name: 'Calendar',
    events: 2,
    calendarKind: 'events',
    weekStart: 'monday',
    shareLevel: 'owner',
    ownerUserId: 'owner-1',
    ownerName: 'Owner',
  },
]

const events = [
  {
    id: 'checkup',
    calendarId: 'own',
    uid: 'u1',
    summary: 'Checkup',
    startsAt: '2030-01-03T10:00:00Z',
    endsAt: '2030-01-03T11:00:00Z',
    allDay: false,
    recurring: true,
    occurrence: false,
    isPrivate: false,
  },
  {
    id: 'standup',
    calendarId: 'own',
    uid: 'u2',
    summary: 'Standup',
    startsAt: '2030-01-03T12:00:00Z',
    endsAt: '2030-01-03T13:00:00Z',
    allDay: false,
    recurring: false,
    occurrence: false,
    isPrivate: false,
  },
]

// What opening the first event reads back: a rule another program set, with a
// part the editor does not show.
const openedRecurrence = 'FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1'

beforeEach(() => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
      unobserve() {}
    },
  )
  Element.prototype.scrollTo = Element.prototype.scrollTo ?? (() => {})
  vi.mocked(graphql).mockReset()
  sessionStorage.clear()
  localStorage.clear()
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('ListCalendars')) return { ListCalendars: calendars } as never
    if (document.includes('ListCalendarEvents')) return { ListCalendarEvents: events } as never
    if (document.includes('GetCalendarEvent'))
      return {
        GetCalendarEvent: {
          ...events[0],
          recurring: true,
          recurrence: openedRecurrence,
          location: '',
          description: '',
          attendees: [],
        },
      } as never
    if (document.includes('SaveCalendarEvent')) return { SaveCalendarEvent: { id: 'new' } } as never
    if (document.includes('ListReminders')) return { ListReminders: [] } as never
    return {} as never
  })
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function show(address: string) {
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter initialEntries={[address]}>
          <CalendarPage />
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
}

function savedVariables() {
  return vi.mocked(graphql).mock.calls.find(([document]) => document.includes('SaveCalendarEvent'))?.[1]
}

function openNewEvent() {
  show('/calendar?view=agenda&on=2030-01-03')
  return screen.findByText('Checkup').then(() => {
    fireEvent.click(screen.getByRole('button', { name: 'New event' }))
    return screen.findByRole('dialog', { name: 'New event' })
  })
}

function dayButtons(custom: HTMLElement) {
  return within(custom)
    .getAllByRole('button')
    .filter((button) => button.hasAttribute('aria-pressed'))
}

it('offers Every weekday and writes it', async () => {
  const dialog = await openNewEvent()
  fireEvent.change(within(dialog).getByLabelText('Title'), { target: { value: 'Standup' } })
  fireEvent.change(within(dialog).getByLabelText('Repeat'), { target: { value: 'weekdays' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Create' }))
  await waitFor(() => expect(savedVariables()).toBeTruthy())
  expect(savedVariables()).toMatchObject({ recurrence: 'FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR' })
})

it('makes a custom repeat of Monday to Friday and says it in a sentence', async () => {
  const dialog = await openNewEvent()
  fireEvent.change(within(dialog).getByLabelText('Title'), { target: { value: 'Standup' } })
  fireEvent.change(within(dialog).getByLabelText('Repeat'), { target: { value: 'custom' } })
  const custom = await screen.findByRole('dialog', { name: 'Custom repeat' })
  // The week starts on Monday in this calendar, and the event starts on a
  // Thursday, which is chosen already.
  const days = dayButtons(custom)
  expect(days.map((day) => day.getAttribute('aria-label'))).toEqual([
    'Monday',
    'Tuesday',
    'Wednesday',
    'Thursday',
    'Friday',
    'Saturday',
    'Sunday',
  ])
  expect(days.map((day) => day.getAttribute('aria-pressed'))).toEqual([
    'false',
    'false',
    'false',
    'true',
    'false',
    'false',
    'false',
  ])
  for (const name of ['Monday', 'Tuesday', 'Wednesday', 'Friday']) {
    fireEvent.click(within(custom).getByRole('button', { name }))
  }
  const sentence = 'Repeats every week on Monday, Tuesday, Wednesday, Thursday, and Friday'
  expect(within(custom).getByText(sentence)).toBeTruthy()
  fireEvent.click(within(custom).getByRole('button', { name: 'Done' }))

  // The event's form is still open, and now says what the repeat is.
  expect(screen.queryByRole('dialog', { name: 'Custom repeat' })).toBeNull()
  expect((within(dialog).getByLabelText('Repeat') as HTMLSelectElement).value).toBe('weekdays')
  fireEvent.click(within(dialog).getByRole('button', { name: 'Create' }))
  await waitFor(() => expect(savedVariables()).toBeTruthy())
  expect(savedVariables()).toMatchObject({ recurrence: 'FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR' })
})

it('shows a custom repeat as Custom, with its sentence and a way to change it', async () => {
  const dialog = await openNewEvent()
  fireEvent.change(within(dialog).getByLabelText('Repeat'), { target: { value: 'custom' } })
  const custom = await screen.findByRole('dialog', { name: 'Custom repeat' })
  fireEvent.change(within(custom).getByLabelText('Every'), { target: { value: '2' } })
  fireEvent.click(within(custom).getByRole('button', { name: 'Monday' }))
  fireEvent.click(within(custom).getByRole('button', { name: 'Done' }))
  expect((within(dialog).getByLabelText('Repeat') as HTMLSelectElement).value).toBe('custom')
  expect(within(dialog).getByText('Repeats every 2 weeks on Monday and Thursday')).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Change' }))
  const again = await screen.findByRole('dialog', { name: 'Custom repeat' })
  expect(dayButtons(again).map((day) => day.getAttribute('aria-pressed'))).toEqual([
    'true',
    'false',
    'false',
    'true',
    'false',
    'false',
    'false',
  ])
})

it('closes only the custom repeat on Escape, and leaves the repeat as it was', async () => {
  const dialog = await openNewEvent()
  fireEvent.change(within(dialog).getByLabelText('Repeat'), { target: { value: 'custom' } })
  await screen.findByRole('dialog', { name: 'Custom repeat' })
  fireEvent.keyDown(document, { key: 'Escape' })
  expect(screen.queryByRole('dialog', { name: 'Custom repeat' })).toBeNull()
  expect(screen.getByRole('dialog', { name: 'New event' })).toBeTruthy()
  expect((within(dialog).getByLabelText('Repeat') as HTMLSelectElement).value).toBe('never')
})

it('keeps a rule set elsewhere when the editor is opened and closed with Done', async () => {
  show('/calendar?view=agenda&on=2030-01-03')
  const row = (await screen.findByText('Checkup')).closest('.calendar-agenda-row') as HTMLElement
  fireEvent.click(within(row).getByRole('button', { name: 'Edit' }))
  const dialog = await screen.findByRole('dialog', { name: 'Edit event' })
  expect((within(dialog).getByLabelText('Repeat') as HTMLSelectElement).value).toBe('custom')
  expect(within(dialog).getByText(/Set in another program/)).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Change' }))
  const custom = await screen.findByRole('dialog', { name: 'Custom repeat' })
  fireEvent.click(within(custom).getByRole('button', { name: 'Done' }))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(savedVariables()).toBeTruthy())
  expect(savedVariables()).toMatchObject({ recurrence: 'FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1' })
})

it('refuses a weekly repeat on no day', async () => {
  const dialog = await openNewEvent()
  fireEvent.change(within(dialog).getByLabelText('Repeat'), { target: { value: 'custom' } })
  const custom = await screen.findByRole('dialog', { name: 'Custom repeat' })
  fireEvent.click(within(custom).getByRole('button', { name: 'Thursday' }))
  expect(within(custom).getByText('Choose at least one day.')).toBeTruthy()
  expect((within(custom).getByRole('button', { name: 'Done' }) as HTMLButtonElement).disabled).toBe(true)
})
