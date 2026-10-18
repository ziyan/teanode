import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { APIError, graphql } from '../api'
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

// The person's own calendar, with everything the server keeps about it, and
// the one a circle keeps, which is not theirs to rename.
const calendars = [
  {
    id: 'own',
    name: 'Calendar',
    description: 'Things I do',
    colour: '#336699',
    timezone: 'Europe/Berlin',
    weekStart: 'monday',
    events: 0,
    calendarKind: 'events',
    shareLevel: 'owner',
    ownerUserId: 'owner-1',
    ownerName: 'Owner',
  },
  {
    id: 'family',
    name: 'The household',
    events: 0,
    calendarKind: 'events',
    shareLevel: 'edit',
    ownerUserId: null,
    isCircleCalendar: true,
    circleId: 'circle-1',
    circleName: 'Home',
  },
]

function calls(name: string) {
  return vi.mocked(graphql).mock.calls.filter(([document]) => document.includes(name))
}

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
    if (document.includes('SaveCalendar(')) return { SaveCalendar: { id: 'own', name: 'Work' } } as never
    if (document.includes('ListCalendars')) return { ListCalendars: calendars } as never
    if (document.includes('ListCalendarEvents')) return { ListCalendarEvents: [] } as never
    return {} as never
  })
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function show() {
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter initialEntries={['/calendar?view=agenda&on=2030-01-02']}>
          <CalendarPage />
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
}

it('renames the calendar and sends back everything else it holds', async () => {
  show()
  fireEvent.click(await screen.findByRole('button', { name: 'Rename' }))
  const dialog = await screen.findByRole('dialog', { name: 'Rename your calendar' })
  const save = within(dialog).getByRole('button', { name: 'Save' })
  // Nothing to save until the name is something else.
  expect((save as HTMLButtonElement).disabled).toBe(true)

  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: '  Work  ' } })
  expect((save as HTMLButtonElement).disabled).toBe(false)
  fireEvent.click(save)

  // The server replaces every field it is sent, so the description, color,
  // zone and week start go back as they were.
  await waitFor(() => expect(calls('SaveCalendar(')).toHaveLength(1))
  expect(calls('SaveCalendar(')[0][1]).toEqual({
    id: 'own',
    name: 'Work',
    description: 'Things I do',
    colour: '#336699',
    timezone: 'Europe/Berlin',
    weekStart: 'monday',
  })
  // And the calendars are read again, so the new name is on the page.
  await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Rename your calendar' })).toBeNull())
  await waitFor(() => expect(calls('ListCalendars').length).toBeGreaterThan(1))
})

it('keeps the dialog open and says why in a toast when the server refuses', async () => {
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('SaveCalendar(')) throw new APIError('that calendar is not yours')
    if (document.includes('ListCalendars')) return { ListCalendars: calendars } as never
    if (document.includes('ListCalendarEvents')) return { ListCalendarEvents: [] } as never
    return {} as never
  })
  show()
  fireEvent.click(await screen.findByRole('button', { name: 'Rename' }))
  const dialog = await screen.findByRole('dialog', { name: 'Rename your calendar' })
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Work' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

  expect(await screen.findByText('that calendar is not yours')).toBeTruthy()
  // What was typed is still there to be corrected.
  expect((within(dialog).getByLabelText('Name') as HTMLInputElement).value).toBe('Work')
})
