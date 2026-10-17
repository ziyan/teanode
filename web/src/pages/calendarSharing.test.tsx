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

// The person's own calendar, a circle's, one shared with them at titles, one
// shared at edit and one shared only at busy.
const calendars = [
  {
    id: 'own',
    name: 'Calendar',
    events: 1,
    calendarKind: 'events',
    weekStart: 'monday',
    shareLevel: 'owner',
    ownerUserId: 'owner-1',
    ownerName: 'Owner',
  },
  {
    id: 'family',
    name: 'The household',
    events: 1,
    calendarKind: 'events',
    shareLevel: 'edit',
    ownerUserId: null,
    isCircleCalendar: true,
    circleId: 'circle-1',
    circleName: 'Home',
    colorIndex: 2,
  },
  {
    id: 'ben',
    name: 'Calendar',
    events: 1,
    calendarKind: 'events',
    shareLevel: 'titles',
    ownerUserId: 'ben-id',
    ownerName: 'Ben',
    ownerAvatarColorIndex: 4,
    colorIndex: 5,
  },
  {
    id: 'cora',
    name: 'Calendar',
    events: 0,
    calendarKind: 'events',
    shareLevel: 'busy',
    ownerUserId: 'cora-id',
    ownerName: 'Cora',
    colorIndex: 6,
  },
]

const eventsOf: Record<string, unknown[]> = {
  own: [
    {
      id: 'mine',
      calendarId: 'own',
      uid: 'u1',
      summary: 'My dentist',
      startsAt: '2030-01-03T10:00:00Z',
      endsAt: '2030-01-03T11:00:00Z',
      allDay: false,
      recurring: false,
      occurrence: false,
    },
  ],
  family: [
    {
      id: 'play',
      calendarId: 'family',
      uid: 'u2',
      summary: 'School play',
      startsAt: '2030-01-04T10:00:00Z',
      endsAt: '2030-01-04T11:00:00Z',
      allDay: false,
      recurring: false,
      occurrence: false,
    },
  ],
  ben: [
    {
      id: 'bens',
      calendarId: 'ben',
      uid: 'u3',
      summary: "Ben's lunch",
      startsAt: '2030-01-05T10:00:00Z',
      endsAt: '2030-01-05T11:00:00Z',
      allDay: false,
      recurring: false,
      occurrence: false,
    },
  ],
  cora: [
    {
      id: '',
      calendarId: 'cora',
      uid: '',
      summary: '',
      startsAt: '2030-01-06T10:00:00Z',
      endsAt: '2030-01-06T11:00:00Z',
      allDay: false,
      recurring: false,
      occurrence: false,
      isBusyOnly: true,
    },
  ],
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
  vi.mocked(graphql).mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('ListCalendars')) return { ListCalendars: calendars } as never
    if (document.includes('ListCalendarEvents'))
      return { ListCalendarEvents: eventsOf[String(variables?.calendarId)] ?? [] } as never
    if (document.includes('GetCalendarEvent'))
      return {
        GetCalendarEvent: {
          id: 'bens',
          uid: 'u3',
          summary: "Ben's lunch",
          location: 'The canteen',
          description: 'Bring cash',
          startsAt: '2030-01-05T10:00:00Z',
          endsAt: '2030-01-05T11:00:00Z',
          allDay: false,
          recurring: false,
          attendees: [],
        },
      } as never
    if (document.includes('SaveCalendarEvent')) return { SaveCalendarEvent: { id: 'new' } } as never
    if (document.includes('ListCalendarShares'))
      return {
        ListCalendarShares: [
          {
            id: 'share-1',
            granteeUserId: 'ben-id',
            granteeUsername: 'ben',
            granteeName: 'Ben',
            granteeCircleId: null,
            granteeCircleName: '',
            shareLevel: 'titles',
          },
          {
            id: 'share-2',
            granteeUserId: null,
            granteeUsername: '',
            granteeName: '',
            granteeCircleId: 'circle-1',
            granteeCircleName: 'Home',
            shareLevel: 'busy',
          },
        ],
      } as never
    if (document.includes('ListCirclePeople'))
      return {
        ListCirclePeople: [
          { userId: 'ben-id', username: 'ben', name: 'Ben', avatarUrl: null, avatarColorIndex: 1 },
          { userId: 'cora-id', username: 'cora', name: 'Cora', avatarUrl: null, avatarColorIndex: 2 },
        ],
      } as never
    if (document.includes('ListCircles'))
      return {
        ListCircles: [
          {
            id: 'circle-1',
            name: 'Home',
            members: [{ userId: 'a' }, { userId: 'b' }, { userId: 'c' }, { userId: 'd' }],
          },
          { id: 'circle-2', name: 'Home', members: [{ userId: 'a' }] },
        ],
      } as never
    if (document.includes('ShareCalendar')) return { ShareCalendar: { id: 'share-3' } } as never
    if (document.includes('UnshareCalendar')) return { UnshareCalendar: true } as never
    if (document.includes('ListReminders')) return { ListReminders: [] } as never
    return {} as never
  })
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function show(address = '/calendar?view=agenda&on=2030-01-02') {
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

function eventQueries() {
  return vi
    .mocked(graphql)
    .mock.calls.filter(([document]) => document.includes('ListCalendarEvents'))
    .map(([, variables]) => (variables as { calendarId: string }).calendarId)
}

it('lists every calendar the person reaches, each with whose it is and what they may do', async () => {
  show()
  const list = await screen.findByRole('group', { name: 'Calendars' })
  const names = within(list)
    .getAllByRole('checkbox')
    .map((box) => box.closest('label')?.textContent)
  expect(names).toEqual([
    'Calendar',
    'The householdDetails, and can edit',
    "BBen's calendarTitles",
    "CCora's calendarBusy times only",
  ])
  // Each is read, so that the events of all of them are on the page.
  await waitFor(() => expect(new Set(eventQueries())).toEqual(new Set(['own', 'family', 'ben', 'cora'])))
  expect(await screen.findByText('My dentist')).toBeTruthy()
  expect(screen.getByText('School play')).toBeTruthy()
  expect(screen.getByText("Ben's lunch")).toBeTruthy()
})

it('shows a calendar shared only at busy as a gray Busy block, with nothing to open', async () => {
  show()
  const block = (await screen.findByText('Busy · Cora', { selector: '.calendar-agenda-title' })).closest(
    '.calendar-agenda-row',
  ) as HTMLElement
  // Whose it is is said once, in the block's own words, and not again under it.
  expect(within(block).queryByText('Cora')).toBeNull()
  // Nothing to edit or remove in it.
  expect(within(block).queryByRole('button')).toBeNull()
})

it('hides a calendar and remembers that in this browser only', async () => {
  show()
  const list = await screen.findByRole('group', { name: 'Calendars' })
  await screen.findByText("Ben's lunch")
  fireEvent.click(within(list).getByRole('checkbox', { name: /Ben's calendar/ }))
  await waitFor(() => expect(screen.queryByText("Ben's lunch")).toBeNull())
  expect(JSON.parse(localStorage.getItem('teanode.calendar.hidden.owner-1') ?? '[]')).toEqual(['ben'])
  // The rest are still there, and asked for again without it.
  expect(screen.getByText('My dentist')).toBeTruthy()
  vi.mocked(graphql).mockClear()
  fireEvent.click(within(list).getByRole('checkbox', { name: /The household/ }))
  await waitFor(() => expect(eventQueries().length).toBeGreaterThan(0))
  expect(eventQueries()).not.toContain('ben')
  expect(eventQueries()).not.toContain('family')

  // Remembered on the next visit.
  cleanup()
  show()
  await screen.findByText('My dentist')
  expect(screen.queryByText("Ben's lunch")).toBeNull()
  expect(
    (
      within(await screen.findByRole('group', { name: 'Calendars' })).getByRole('checkbox', {
        name: /Ben's calendar/,
      }) as HTMLInputElement
    ).checked,
  ).toBe(false)
})

it('opens an event in a calendar shared below edit to read, not to change', async () => {
  show()
  const row = (await screen.findByText("Ben's lunch")).closest('.calendar-agenda-row') as HTMLElement
  // Not deletable from here.
  expect(within(row).queryByRole('button', { name: 'Delete' })).toBeNull()
  // It says View, not Edit, since nothing can be changed.
  expect(within(row).queryByRole('button', { name: 'Edit' })).toBeNull()
  fireEvent.click(within(row).getByRole('button', { name: 'View' }))
  const dialog = await screen.findByRole('alertdialog')
  expect(within(dialog).getByText('The canteen')).toBeTruthy()
  expect(within(dialog).getByText('Bring cash')).toBeTruthy()
  expect(within(dialog).getByText(/Shared with you by Ben/)).toBeTruthy()
  // A way out and no form.
  expect(within(dialog).queryByRole('textbox')).toBeNull()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Close' }))
  expect(screen.queryByRole('alertdialog')).toBeNull()
})

it('puts a new event in the calendar chosen, and leaves the guests out of a circle’s', async () => {
  show()
  await screen.findByText('My dentist')
  fireEvent.click(screen.getByRole('button', { name: 'New event' }))
  const dialog = await screen.findByRole('dialog')
  // Own and the circle's, and the one shared at edit when there is one; not the
  // ones shared below it.
  const choice = within(dialog).getByLabelText('Calendar') as HTMLSelectElement
  expect(Array.from(choice.options).map((option) => option.textContent)).toEqual(['Calendar', 'The household'])
  expect(within(dialog).getByLabelText(/Invite/)).toBeTruthy()

  fireEvent.change(choice, { target: { value: 'family' } })
  expect(within(dialog).queryByLabelText(/Invite/)).toBeNull()
  fireEvent.change(within(dialog).getByLabelText('Title'), { target: { value: 'Picnic' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Create' }))
  await waitFor(() =>
    expect(vi.mocked(graphql).mock.calls.some(([document]) => document.includes('SaveCalendarEvent'))).toBe(true),
  )
  const saved = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('SaveCalendarEvent'))
  expect(saved?.[1]).toMatchObject({ calendarId: 'family', summary: 'Picnic', attendees: null })
  // The toast says which calendar it went into, since it was not the person's own.
  expect(await screen.findByText('Picnic is in The household')).toBeTruthy()
})

it('lists the agenda in time order across calendars, not one calendar after another', async () => {
  const later = {
    ...(eventsOf.own[0] as object),
    id: 'later',
    summary: 'Own, a week later',
    startsAt: '2030-01-09T10:00:00Z',
  }
  const earlier = {
    ...(eventsOf.ben[0] as object),
    id: 'earlier',
    summary: "Ben's breakfast",
    startsAt: '2030-01-03T07:00:00Z',
  }
  const wholeDay = {
    ...(eventsOf.family[0] as object),
    id: 'whole',
    summary: 'Family holiday',
    allDay: true,
    startsAt: '2030-01-03T00:00:00Z',
  }
  const before = { own: eventsOf.own, ben: eventsOf.ben, family: eventsOf.family }
  eventsOf.own = [...eventsOf.own, later]
  eventsOf.ben = [...eventsOf.ben, earlier]
  eventsOf.family = [...eventsOf.family, wholeDay]
  try {
    show()
    await screen.findByText('My dentist')
    const titles = Array.from(document.querySelectorAll('.calendar-agenda-title')).map((title) => title.textContent)
    expect(titles).toEqual([
      // The 3rd: the whole day first, then by the hour whichever calendar it is in.
      'Family holiday',
      "Ben's breakfast",
      'My dentist',
      'School play',
      "Ben's lunch",
      'Busy · Cora',
      'Own, a week later',
    ])
  } finally {
    Object.assign(eventsOf, before)
  }
})

it('shares the calendar with a person at a level, and takes a share back', async () => {
  show()
  await screen.findByText('My dentist')
  fireEvent.click(screen.getByRole('button', { name: 'Share' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share your calendar' })
  // Who it is shared with already, under a heading that says so.
  expect(within(dialog).getByRole('heading', { name: 'Shared with' })).toBeTruthy()
  expect(await within(dialog).findByText('Ben', { selector: '.calendar-share-name' })).toBeTruthy()
  expect(within(dialog).getByText('Everyone in Home', { selector: '.calendar-share-name' })).toBeTruthy()
  const listed = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('ListCalendarShares'))
  expect(listed?.[1]).toEqual({ calendarId: 'own' })

  // Another person, at details.
  fireEvent.change(await within(dialog).findByLabelText(/^Share with/), { target: { value: 'user:cora-id' } })
  fireEvent.change(within(dialog).getByLabelText(/^How much/), { target: { value: 'details' } })
  expect(within(dialog).getByText(/everything in each event, including its notes and guests/)).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Share' }))
  await waitFor(() =>
    expect(
      vi
        .mocked(graphql)
        .mock.calls.some(([document]) => document.includes('mutation') && document.includes('ShareCalendar')),
    ).toBe(true),
  )
  const shared = vi
    .mocked(graphql)
    .mock.calls.find(([document]) => document.includes('mutation') && document.includes('ShareCalendar'))
  expect(shared?.[1]).toEqual({
    calendarId: 'own',
    granteeUserId: 'cora-id',
    granteeCircleId: null,
    shareLevel: 'details',
  })

  // Stop sharing with Ben.
  fireEvent.click(within(dialog).getByRole('button', { name: 'Stop sharing with Ben' }))
  await waitFor(() =>
    expect(vi.mocked(graphql).mock.calls.some(([document]) => document.includes('UnshareCalendar'))).toBe(true),
  )
  const stopped = vi.mocked(graphql).mock.calls.find(([document]) => document.includes('UnshareCalendar'))
  expect(stopped?.[1]).toEqual({ shareId: 'share-1' })

  // Shared with a circle.
  fireEvent.change(within(dialog).getByLabelText(/^Share with/), { target: { value: 'circle:circle-1' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Share' }))
  await waitFor(() =>
    expect(
      vi
        .mocked(graphql)
        .mock.calls.filter(([document]) => document.includes('mutation') && document.includes('ShareCalendar')).length,
    ).toBe(2),
  )
  const last = vi
    .mocked(graphql)
    .mock.calls.filter(([document]) => document.includes('mutation') && document.includes('ShareCalendar'))[1]
  expect(last[1]).toMatchObject({ granteeUserId: null, granteeCircleId: 'circle-1', shareLevel: 'details' })
})

it('names each person in the picker with the username in brackets', async () => {
  show()
  await screen.findByText('My dentist')
  fireEvent.click(screen.getByRole('button', { name: 'Share' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share your calendar' })
  const picker = (await within(dialog).findByLabelText(/^Share with/)) as HTMLSelectElement
  expect(Array.from(picker.options).map((option) => option.textContent)).toEqual([
    'Choose a person or a circle',
    'Ben (ben)',
    'Cora (cora)',
    'Everyone in Home (4 people)',
    'Everyone in Home (1 person)',
  ])
})

it('sends a person with nobody to share with to Circles', async () => {
  const answer = vi.mocked(graphql).getMockImplementation()!
  vi.mocked(graphql).mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('ListCirclePeople')) return { ListCirclePeople: [] } as never
    if (document.includes('ListCircles')) return { ListCircles: [] } as never
    return answer(document, variables)
  })
  show()
  await screen.findByText('My dentist')
  fireEvent.click(screen.getByRole('button', { name: 'Share' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share your calendar' })
  expect(await within(dialog).findByText(/You do not share a circle with anybody yet/)).toBeTruthy()
  expect(within(dialog).getByRole('link', { name: 'Open Circles' }).getAttribute('href')).toBe('/circles')
})

it('says a share that did not go through once, with what to do', async () => {
  const answer = vi.mocked(graphql).getMockImplementation()!
  vi.mocked(graphql).mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('mutation') && document.includes('ShareCalendar')) throw new TypeError('Failed to fetch')
    return answer(document, variables)
  })
  show()
  await screen.findByText('My dentist')
  fireEvent.click(screen.getByRole('button', { name: 'Share' }))
  const dialog = await screen.findByRole('dialog', { name: 'Share your calendar' })
  fireEvent.change(await within(dialog).findByLabelText(/^Share with/), { target: { value: 'user:cora-id' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Share' }))
  await waitFor(() =>
    expect(screen.getAllByText('That did not work. Check your connection and try again.')).toHaveLength(1),
  )
})
