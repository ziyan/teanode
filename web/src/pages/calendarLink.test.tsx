import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
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
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

let shownSearch = ''
function SearchProbe() {
  shownSearch = useLocation().search
  return null
}

function answer() {
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('ListCalendars'))
      return {
        ListCalendars: [{ id: 'calendar-1', name: 'Work', events: 1, calendarKind: 'events', weekStart: 'monday' }],
      } as never
    if (document.includes('ListCalendarEvents')) return { ListCalendarEvents: [] } as never
    if (document.includes('GetCalendarEvent'))
      return {
        GetCalendarEvent: {
          id: 'event-1',
          uid: 'uid-1',
          summary: 'Fixture planning',
          startsAt: '2026-03-04T10:00:00Z',
          endsAt: '2026-03-04T11:00:00Z',
          allDay: false,
          recurring: false,
          attendees: [],
        },
      } as never
    if (document.includes('ReadRecordMemoryLinks'))
      return {
        ReadRecordMemoryLinks: [
          {
            id: 'link-1',
            nodePath: 'projects/fixture-project',
            nodeName: 'Fixture Project',
            nodeKind: 'project',
            recordKind: 'calendar_event',
            recordId: 'event-1',
            recordRelation: 'about',
            linkStatus: 'proposed',
            passageStartMS: 0,
            passageEndMS: 0,
            transcriptRunId: '',
            note: '',
            createdAt: '2026-01-01T00:00:00Z',
            recordTitle: 'Fixture planning',
            dashboardPath: '/calendar?event=event-1',
            isUnavailable: false,
          },
        ],
      } as never
    if (document.includes('ListReminders')) return { ListReminders: [] } as never
    return {} as never
  })
}

function show(address: string) {
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter initialEntries={[address]}>
          <CalendarPage />
          <SearchProbe />
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
}

it('opens the event a link from memory names, with the pages it is in, and forgets the address when it closes', async () => {
  answer()
  show('/calendar?event=event-1&view=week')
  const dialog = await screen.findByRole('dialog')
  expect((within(dialog).getAllByRole('textbox')[0] as HTMLInputElement).value).toBe('Fixture planning')
  expect(await within(dialog).findByRole('link', { name: 'Fixture Project' })).toBeTruthy()
  // A guess says so.
  expect(within(dialog).getByText('Perhaps')).toBeTruthy()
  const getCall = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('GetCalendarEvent'))
  expect(getCall?.[1]).toEqual({ calendarId: 'calendar-1', id: 'event-1' })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
  await waitFor(() => expect(shownSearch).not.toContain('event='))
  expect(shownSearch).toContain('view=week')
})

it('shows the reminders list at the reminders address, which a link to a reminder relies on', async () => {
  answer()
  show('/reminders')
  await waitFor(() =>
    expect(vi.mocked(graphql).mock.calls.some(([document]) => document.includes('ListReminders'))).toBe(true),
  )
  expect(vi.mocked(graphql).mock.calls.some(([document]) => document.includes('ListCalendarEvents'))).toBe(false)
})
