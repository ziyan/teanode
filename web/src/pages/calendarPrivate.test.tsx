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
    recurring: false,
    occurrence: false,
    isPrivate: true,
  },
  {
    id: 'lunch',
    calendarId: 'own',
    uid: 'u2',
    summary: 'Lunch',
    startsAt: '2030-01-03T12:00:00Z',
    endsAt: '2030-01-03T13:00:00Z',
    allDay: false,
    recurring: false,
    occurrence: false,
    isPrivate: false,
  },
]

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
      return { GetCalendarEvent: { ...events[0], location: '', description: '', attendees: [] } } as never
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

it('puts a lock on the person’s own private events, and on no other', async () => {
  for (const view of ['month', 'week', 'agenda']) {
    cleanup()
    show(`/calendar?view=${view}&on=2030-01-03`)
    const checkup = await screen.findByText('Checkup')
    expect(within(checkup.parentElement as HTMLElement).getByRole('img', { name: 'Private' })).toBeTruthy()
    const lunch = await screen.findByText('Lunch')
    expect(within(lunch.parentElement as HTMLElement).queryByRole('img', { name: 'Private' })).toBeNull()
  }
})

it('marks a new event private with a line saying who sees what', async () => {
  show('/calendar?view=agenda&on=2030-01-03')
  await screen.findByText('Checkup')
  fireEvent.click(screen.getByRole('button', { name: 'New event' }))
  const dialog = await screen.findByRole('dialog')
  const box = within(dialog).getByRole('checkbox', { name: 'Private' }) as HTMLInputElement
  expect(box.checked).toBe(false)
  expect(within(dialog).getByText('Others you share your calendar with see only that you are busy.')).toBeTruthy()
  fireEvent.click(box)
  fireEvent.change(within(dialog).getByLabelText('Title'), { target: { value: 'Appointment' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Create' }))
  await waitFor(() => expect(savedVariables()).toBeTruthy())
  expect(savedVariables()).toMatchObject({ summary: 'Appointment', isPrivate: true })
})

it('opens a private event with the box ticked and lets the person take the mark off', async () => {
  show('/calendar?view=agenda&on=2030-01-03')
  const row = (await screen.findByText('Checkup')).closest('.calendar-agenda-row') as HTMLElement
  fireEvent.click(within(row).getByRole('button', { name: 'Edit' }))
  const dialog = await screen.findByRole('dialog')
  const box = within(dialog).getByRole('checkbox', { name: 'Private' }) as HTMLInputElement
  expect(box.checked).toBe(true)
  fireEvent.click(box)
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(savedVariables()).toBeTruthy())
  expect(savedVariables()).toMatchObject({ id: 'checkup', isPrivate: false })
})

function stubPhone(isPhone: boolean) {
  const listeners = new Set<() => void>()
  let matches = isPhone
  vi.stubGlobal('matchMedia', (query: string) => ({
    get matches() {
      return query === '(max-width: 600px)' && matches
    },
    media: query,
    addEventListener: (_name: string, listener: () => void) => listeners.add(listener),
    removeEventListener: (_name: string, listener: () => void) => listeners.delete(listener),
  }))
  window.matchMedia = (globalThis as unknown as { matchMedia: typeof window.matchMedia }).matchMedia
  return (next: boolean) => {
    matches = next
    listeners.forEach((listener) => listener())
  }
}

it('on a phone opens on the agenda, names the month dots and opens the day on a tap', async () => {
  const setPhone = stubPhone(true)
  show('/calendar')
  await screen.findByText('Checkup')
  expect(document.querySelector('.calendar-agenda-row')).toBeTruthy()

  cleanup()
  show('/calendar?view=month&on=2030-01-03')
  const dot = await screen.findByRole('button', { name: /^Checkup/ })
  expect(dot.getAttribute('tabindex')).toBe('-1')
  // A rotation to a wide window brings the dots back into the tab order,
  // and the chip's own words, its time first, are its name again.
  setPhone(false)
  await waitFor(() => expect(screen.getByRole('button', { name: /Checkup/ }).getAttribute('tabindex')).toBeNull())
  setPhone(true)
  await waitFor(() => expect(screen.getByRole('button', { name: /^Checkup/ }).getAttribute('tabindex')).toBe('-1'))

  // A press on an empty day opens the day, it does not start a new event.
  fireEvent.click(screen.getAllByRole('button', { name: /^Open / })[0])
  expect(screen.queryByRole('dialog')).toBeNull()
})
