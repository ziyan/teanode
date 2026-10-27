import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useSearchParams } from 'react-router-dom'

import { graphql } from '../api'
import { useSession } from '../session'
import { useCalendarMutation } from '../hooks/useCalendarMutation'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { Avatar } from '../components/avatar'
import { FindATimeDialog } from '../components/calendarFindTime'
import { CalendarRenameDialog } from '../components/calendarRename'
import { CalendarShareDialog } from '../components/calendarShare'
import { ChevronLeftIcon, ChevronRightIcon, EyeIcon, LockIcon, PencilIcon, TrashIcon } from '../components/icons'
import { Tooltip } from '../components/tooltip'
import { Tabs } from '../components/tabs'
import { RemindersView } from '../components/reminders'
import { useQuery } from '../components/useQuery'
import { useToast } from '../components/toast'
import type { WeekStart } from '../components/weekdayButtons'
import { useTranslation } from '../i18n/i18n'
import { CALENDAR_TABS, shownTabs } from '../pageTabs'
import { InMemory } from '../memoryLinks'
import {
  describeRepeat,
  parseRepeat,
  presetOf,
  presetRule,
  type RepeatContext,
  type RepeatPreset,
} from './calendarRepeat'
import { RepeatDialog } from './calendarRepeatDialog'

// The calendar: what somebody has on, and the form for putting something in.
//
// An event is an iCalendar file. The form here fills in the handful of boxes
// most events need, and the server applies them to the file it already holds,
// so that an alarm or a conferencing link put there by a phone survives
// somebody correcting a title in a browser.
//
// The server answers "what is on between these two moments" with one entry
// per time something happens, so a weekly meeting arrives once for each week
// in view and this page never has to know what a repeat rule means.

const CALENDARS = `query { ListCalendars { id name description colour timezone weekStart events calendarKind
  shareLevel ownerUserId ownerName ownerAvatarUrl ownerAvatarColorIndex circleId circleName isCircleCalendar colorIndex } }`

const EVENTS = `
  query ($calendarId: String!, $from: String!, $until: String!) {
    ListCalendarEvents(calendarId: $calendarId, from: $from, until: $until) {
      id calendarId uid summary location startsAt endsAt allDay recurring occurrence status isBusyOnly isPrivate
    }
  }`

const GET = `
  query ($calendarId: String!, $id: String!) {
    GetCalendarEvent(calendarId: $calendarId, id: $id) {
      id uid etag summary location description startsAt endsAt allDay
      recurring recurrence status timezone isPrivate organizer attendees { address name participation role }
    }
  }`

// What the caller may do with a calendar: owner for their own, then how much
// of someone else's was shared, or edit for a circle's.
export type ShareLevel = 'owner' | 'busy' | 'titles' | 'details' | 'edit'

export type Calendar = {
  id: string
  name: string
  description?: string
  colour?: string
  timezone?: string
  weekStart?: string
  events: number
  calendarKind: 'events' | 'reminders'
  shareLevel?: ShareLevel
  // Whose it is: null for a circle's calendar, which belongs to the circle.
  ownerUserId?: string | null
  ownerName?: string
  ownerAvatarUrl?: string | null
  ownerAvatarColorIndex?: number
  circleId?: string | null
  circleName?: string
  isCircleCalendar?: boolean
  // Which of eight colors it is painted in when it has none of its own.
  colorIndex?: number
}

// The caller's own calendar, whatever an older answer left out.
const isOwn = (calendar: Calendar) => !calendar.shareLevel || calendar.shareLevel === 'owner'
// Whether the events in it may be changed from here.
const canWrite = (calendar: Calendar) => isOwn(calendar) || calendar.shareLevel === 'edit'

type Attendee = { address: string; name?: string; participation?: string; role?: string }

type CalendarEvent = {
  id: string
  calendarId: string
  uid: string
  etag?: string
  summary?: string
  location?: string
  description?: string
  startsAt: string
  endsAt: string
  allDay: boolean
  recurring: boolean
  recurrence?: string
  occurrence: boolean
  status?: string
  timezone?: string
  organizer?: string
  attendees?: Attendee[]
  // All the caller is told of it is that the owner is busy.
  isBusyOnly?: boolean
  // The owner marked it private: everyone the calendar is shared with sees
  // only that they are busy. Only the owner's own list carries it as true.
  isPrivate?: boolean
}

type View = 'month' | 'week' | 'workweek' | 'day' | 'agenda' | 'reminders'

// A form's worth of one event. The times are held as the two halves a browser
// edits them in -- a date and a clock time -- because that is what the native
// controls take, and joining them only when the form is sent keeps a person
// changing the day from having the time cleared underneath them.
type Draft = {
  id: string
  summary: string
  location: string
  description: string
  startDate: string
  startTime: string
  endDate: string
  endTime: string
  allDay: boolean
  recurrence: string
  status: string
  isPrivate: boolean
  // One address a line, which is how somebody with four guests expects to
  // type them and avoids a row of controls for adding and removing lines.
  attendees: string
  // The calendar it goes in, which is the caller's own unless they chose a
  // circle's or one shared with them at edit.
  calendarId: string
}

// The repeats the Repeat list offers by name, in the order it lists them.
// Anything else, whether the person made it with Custom or another program
// set it, is "Custom" there, with the sentence it makes beside it.
const REPEAT_PRESETS: RepeatPreset[] = ['never', 'daily', 'weekdays', 'weekly', 'monthly', 'yearly']

// The column views and how wide each is. A day is one column, the working
// week five, the week seven -- all the same drawing, differing only in how
// many days it starts from and how many it shows.
const COLUMNS: Partial<Record<View, number>> = { day: 1, workweek: 5, week: 7 }

// How tall an hour is drawn, in pixels, and where the grid opens.
//
// A day is twenty-four hours and almost nobody has anything in the first
// seven, so the grid is scrolled to the morning rather than made shorter:
// something at six is still there to scroll to, which a grid that started at
// eight would have hidden.
const HOUR = 48
// How far above its line an hour's label reaches (0.55rem, and a margin).
const HOUR_LABEL_OVERHANG = 12
const OPENS_AT = 7

// minutesInto is how far into a day a moment falls, clamped to it. An event
// that began yesterday and runs into today starts at the top rather than
// above it.
function minutesInto(day: Date, at: Date): number {
  const start = startOfDay(day).getTime()
  return Math.max(0, Math.min(24 * 60, (at.getTime() - start) / 60000))
}

// placed is one event with the slot it occupies: how far down, how tall, and
// -- when things overlap -- which of the side-by-side columns it takes.
type Placed = { event: CalendarEvent; top: number; height: number; column: number; columns: number }

// layOut puts a day's timed events side by side where they overlap.
//
// A calendar that draws overlapping events on top of one another hides the
// one underneath, and the whole reason to look at a day is to see the clash.
// Events are swept in start order; anything that begins before the running
// group has finished joins that group, and the group is then shared out.
function layOut(day: Date, events: CalendarEvent[]): Placed[] {
  const spans = events
    .filter((event) => !event.allDay)
    .map((event) => {
      const starts = new Date(event.startsAt)
      const ends = new Date(event.endsAt)
      const from = minutesInto(day, starts)
      // A moment-long event still has to be clickable, so every one is
      // drawn at least twenty minutes tall.
      const until = Math.max(from + 20, minutesInto(day, ends))
      return { event, from, until }
    })
    .sort((first, second) => first.from - second.from || first.until - second.until)

  const placed: Placed[] = []
  let group: typeof spans = []
  let groupEnds = -1

  const settle = () => {
    // Within a group, each event takes the first column that is free.
    const columns: number[] = []
    const taken = group.map((span) => {
      let index = columns.findIndex((until) => until <= span.from)
      if (index < 0) {
        index = columns.length
      }
      columns[index] = span.until
      return index
    })
    group.forEach((span, index) => {
      placed.push({
        event: span.event,
        top: (span.from / 60) * HOUR,
        height: ((span.until - span.from) / 60) * HOUR,
        column: taken[index],
        columns: columns.length,
      })
    })
    group = []
    groupEnds = -1
  }

  for (const span of spans) {
    if (group.length > 0 && span.from >= groupEnds) {
      settle()
    }
    group.push(span)
    groupEnds = Math.max(groupEnds, span.until)
  }
  if (group.length > 0) {
    settle()
  }
  return placed
}

// dayKey names a day the way a date input does, in local time. Not the ISO
// string, which is in UTC and so is the wrong day for anybody east or west of
// it for part of every day.
function dayKey(at: Date): string {
  const month = `${at.getMonth() + 1}`.padStart(2, '0')
  const day = `${at.getDate()}`.padStart(2, '0')
  return `${at.getFullYear()}-${month}-${day}`
}

// A whole-day event belongs to its date everywhere, so its day is read from
// the parts the server wrote rather than from what those parts become in the
// reader's own zone.
//
// The server keeps it as a date -- midnight UTC -- and turning that into a
// local time lands on the day before for anybody west of Greenwich. This is
// the same mistake the format itself invites, and the reason a birthday is
// written as a date and not as a moment; getting it right on the server and
// then undoing it here would have shown every all-day event on the wrong day.
function dayOf(event: { startsAt: string; allDay: boolean }): string {
  const at = new Date(event.startsAt)
  if (event.allDay) {
    const month = `${at.getUTCMonth() + 1}`.padStart(2, '0')
    const day = `${at.getUTCDate()}`.padStart(2, '0')
    return `${at.getUTCFullYear()}-${month}-${day}`
  }
  return dayKey(at)
}

// dayShifted is a date box's value moved by so many days, in dates rather
// than in moments, so the answer does not depend on the reader's zone.
function dayShifted(day: string, by: number): string {
  const at = new Date(`${day}T00:00:00Z`)
  if (Number.isNaN(at.getTime())) return day
  at.setUTCDate(at.getUTCDate() + by)
  const month = `${at.getUTCMonth() + 1}`.padStart(2, '0')
  const date = `${at.getUTCDate()}`.padStart(2, '0')
  return `${at.getUTCFullYear()}-${month}-${date}`
}

// lastDayOf is the last day a whole-day event is on, which is the day before
// the one the file names: the format writes the end of a date range as the
// morning after. An event written with both ends on the same date -- which a
// client is allowed to send -- is on that date, not the day before it.
function lastDayOf(event: { startsAt: string; endsAt: string }): string {
  const first = dayOf({ startsAt: event.startsAt, allDay: true })
  const last = dayShifted(dayOf({ startsAt: event.endsAt, allDay: true }), -1)
  return last < first ? first : last
}

// noonOf is the event's day as something a date formatter can be handed.
// Midday, so that formatting it in any zone still names the right date.
function noonOf(event: { startsAt: string; allDay: boolean }): Date {
  const [year, month, day] = dayOf(event).split('-').map(Number)
  return new Date(year, month - 1, day, 12)
}

// The small lock on the person's own private events, in month, week, day and
// agenda alike. It says in words what it is for a screen reader.
function PrivateMark() {
  const { t } = useTranslation()
  return (
    <span className="calendar-lock" role="img" aria-label={t('calendar.private')}>
      <LockIcon size={12} />
    </span>
  )
}

function clockKey(at: Date): string {
  const hour = `${at.getHours()}`.padStart(2, '0')
  const minute = `${at.getMinutes()}`.padStart(2, '0')
  return `${hour}:${minute}`
}

// startOfDay and friends work in the browser's own zone, which is the zone
// the person is reading the page in.
function startOfDay(at: Date): Date {
  return new Date(at.getFullYear(), at.getMonth(), at.getDate())
}

function addDays(at: Date, days: number): Date {
  const moved = new Date(at)
  moved.setDate(moved.getDate() + days)
  return moved
}

// startOfWeek is the first day of the week a day falls in, counted from
// whichever day this calendar starts its weeks on. Where a week begins is a
// local convention rather than a fact -- Sunday here, Monday elsewhere -- so
// it is read from the calendar rather than decided in this file.
function startOfWeek(at: Date, weekStart: WeekStart): Date {
  const day = startOfDay(at)
  const first = weekStart === 'monday' ? 1 : 0
  return addDays(day, -((day.getDay() - first + 7) % 7))
}

// The working week is Monday to Friday wherever a week is drawn from. Five
// days counted from a Sunday start would be Sunday to Thursday, which is not
// a working week anywhere this server is likely to be read.
function startOfWorkWeek(at: Date): Date {
  return startOfWeek(at, 'monday')
}

// The six weeks a month view draws: whole weeks, so the grid is rectangular
// and the days either side of the month are shown greyed rather than blank.
function monthGrid(at: Date, weekStart: WeekStart): Date[] {
  const first = new Date(at.getFullYear(), at.getMonth(), 1)
  const start = startOfWeek(first, weekStart)
  const days: Date[] = []
  for (let index = 0; index < 42; index += 1) days.push(addDays(start, index))
  return days
}

function parseDay(value: string | null): Date {
  if (value) {
    const parsed = new Date(`${value}T00:00:00`)
    if (!Number.isNaN(parsed.getTime())) return startOfDay(parsed)
  }
  return startOfDay(new Date())
}

// The calendars a person hid, remembered in this browser only: whether the
// family's calendar is in the way on this screen is not something to carry to
// their phone. Wrapped in try, because storage is not always there.
const HIDDEN_KEY = 'teanode.calendar.hidden'

function readHidden(ownerId: string): string[] {
  try {
    const stored = JSON.parse(localStorage.getItem(`${HIDDEN_KEY}.${ownerId}`) ?? '[]')
    return Array.isArray(stored) ? stored.filter((each): each is string => typeof each === 'string') : []
  } catch {
    return []
  }
}

function writeHidden(ownerId: string, hidden: string[]) {
  try {
    localStorage.setItem(`${HIDDEN_KEY}.${ownerId}`, JSON.stringify(hidden))
  } catch {
    // Not remembered; it still holds for as long as the page is open.
  }
}

// The class and the style that paint something in a calendar's color. The
// caller's own calendar keeps the accent unless it was given a color of its
// own; every other one takes the color its identifier hashes to, so that the
// same calendar is the same color here and in the app.
function toneOf(calendar: Calendar | undefined): { className: string; style?: React.CSSProperties } {
  if (!calendar) return { className: '' }
  if (calendar.colour && /^#[0-9a-fA-F]{6}$/.test(calendar.colour)) {
    return { className: 'calendar-toned', style: { ['--leaf' as string]: calendar.colour } }
  }
  if (isOwn(calendar)) return { className: '' }
  return { className: `calendar-toned calendar-tone-${(calendar.colorIndex ?? 0) % 8}` }
}

type Translate = ReturnType<typeof useTranslation>['t']

// What a calendar is called in the list: its own name for the caller's and
// for a circle's, and whose it is for another person's, since everybody's is
// called "Calendar".
function calendarLabel(calendar: Calendar, t: Translate): string {
  if (isOwn(calendar) || calendar.isCircleCalendar || !calendar.ownerName) return calendar.name
  return t('calendar.ownersCalendar', { name: calendar.ownerName })
}

// What the toast says after an event was kept: that it was added or saved, and,
// when it is in somebody else's calendar or a circle's, which.
function savedWords(draft: { id?: string; summary: string }, calendar: Calendar | undefined, t: Translate): string {
  const name = draft.summary.trim()
  if (!calendar || isOwn(calendar)) {
    return draft.id ? t('calendar.saidSaved', { name }) : t('calendar.saidAdded', { name })
  }
  const calendarName = calendarLabel(calendar, t)
  return draft.id
    ? t('calendar.saidSavedIn', { name, calendar: calendarName })
    : t('calendar.saidAddedIn', { name, calendar: calendarName })
}

// Whose an event is, for a list that mixes several calendars: the person, or
// the circle.
function whoseCalendar(calendar: Calendar | undefined, t: Translate): string {
  if (!calendar || isOwn(calendar)) return ''
  if (calendar.isCircleCalendar) return calendar.circleName ?? calendar.name
  return calendar.ownerName ?? t('calendar.busy')
}

// Whose the busy time or the shared event is, to be said beside it: the
// person's name for a calendar they shared, and nothing for the caller's own.
// Blocks of several people's calendars sit in one grid, and a gray "Busy"
// would not say whose.
function ownerOfCalendar(calendar: Calendar | undefined): string {
  if (!calendar || isOwn(calendar)) return ''
  if (calendar.isCircleCalendar) return calendar.circleName ?? calendar.name
  return calendar.ownerName ?? ''
}

export function CalendarPage() {
  const session = useSession()
  return <CalendarPageForAccount key={session.userId ?? ''} ownerId={session.userId ?? ''} />
}
// The width at which the month's seven columns no longer hold a day's events:
// the same breakpoint the stylesheet turns them into dots at.
const PHONE_QUERY = '(max-width: 600px)'

function useIsPhone(): boolean {
  // matchMedia is not there under every test environment, and a missing one
  // is a wide screen.
  const [isPhone, setPhone] = useState(() => window.matchMedia?.(PHONE_QUERY).matches ?? false)
  useEffect(() => {
    if (!window.matchMedia) return
    const query = window.matchMedia(PHONE_QUERY)
    const listen = () => setPhone(query.matches)
    query.addEventListener('change', listen)
    return () => query.removeEventListener('change', listen)
  }, [])
  return isPhone
}

function CalendarPageForAccount({ ownerId }: { ownerId: string }) {
  const submission = useCalendarMutation(ownerId)
  const { t } = useTranslation()
  const toast = useToast()
  const [parameters, setParameters] = useSearchParams()

  // The view and the day are in the address, so that a link to a week is a
  // link to that week and the back button goes where it looks like it goes.
  const serviceCapabilities = useSession().services
  // The reminders address is the reminders list, which a link from the agent's
  // memory ("/reminders?reminder=...") relies on.
  const isRemindersAddress = useLocation().pathname.startsWith('/reminders')
  const isPhone = useIsPhone()
  // A phone opens on the agenda, a list that needs no sideways room; the
  // month is a tab away and keeps working there, with dots for the events.
  // This follows the window (a rotation changes it) until the person picks a
  // view, which puts it in the address.
  const requestedView =
    (parameters.get('view') as View) || (isRemindersAddress ? 'reminders' : isPhone ? 'agenda' : 'month')
  const view =
    serviceCapabilities?.isCalendarEnabled === false
      ? 'reminders'
      : requestedView === 'reminders' && serviceCapabilities?.isRemindersEnabled === false
        ? 'month'
        : requestedView
  const selectedDay = parameters.get('on')
  const on = useMemo(() => parseDay(selectedDay), [selectedDay])
  const move = (next: { view?: View; on?: Date }) => {
    const updated = new URLSearchParams(parameters)
    if (next.view) updated.set('view', next.view)
    if (next.on) updated.set('on', dayKey(next.on))
    setParameters(updated, { replace: false })
  }

  const calendars = useQuery(() => graphql<{ ListCalendars: Calendar[] }>(CALENDARS), [], { refresh: false })
  // Every calendar of events the person reaches: their own, their circles',
  // and the ones shared with them. The reminders list beside them is drawn on
  // its own.
  const allCalendars = useMemo(
    () => (calendars.data?.ListCalendars ?? []).filter((found) => found.calendarKind !== 'reminders'),
    [calendars.data],
  )
  // The person's own, which is where a new event goes unless they say otherwise.
  const calendar = allCalendars.find(isOwn) ?? null
  const calendarId = calendar?.id ?? ''
  const calendarsById = useMemo(() => new Map(allCalendars.map((found) => [found.id, found])), [allCalendars])
  // Hidden here, remembered in this browser.
  const [hidden, setHidden] = useState<string[]>(() => readHidden(ownerId))
  const shown = useMemo(() => allCalendars.filter((found) => !hidden.includes(found.id)), [allCalendars, hidden])
  const shownKey = shown.map((found) => found.id).join(',')
  const writable = allCalendars.filter(canWrite)
  const toggleCalendar = (id: string) => {
    const next = hidden.includes(id) ? hidden.filter((each) => each !== id) : [...hidden, id]
    setHidden(next)
    writeHidden(ownerId, next)
  }
  const [isSharing, setSharing] = useState(false)
  const [isRenaming, setRenaming] = useState(false)
  const [isFindingTime, setFindingTime] = useState(false)
  // An event in a calendar shared read only, opened to be read.
  const [viewing, setViewing] = useState<CalendarEvent | null>(null)
  // Sunday unless this calendar says Monday. The server answers with one of
  // the two, so this only has to cope with an answer that has not arrived.
  const weekStart: WeekStart = calendar?.weekStart === 'monday' ? 'monday' : 'sunday'

  // The window asked for is whole weeks for a month, the week for a week, and
  // a month ahead for an agenda.
  const [from, until] = useMemo<[Date, Date]>(() => {
    const columns = COLUMNS[view]
    if (columns) {
      const start =
        columns === 1 ? startOfDay(on) : view === 'workweek' ? startOfWorkWeek(on) : startOfWeek(on, weekStart)
      return [start, addDays(start, columns)]
    }
    if (view === 'agenda') return [startOfDay(on), addDays(startOfDay(on), 31)]
    const grid = monthGrid(on, weekStart)
    return [grid[0], addDays(grid[41], 1)]
  }, [view, on, weekStart])

  // One question per calendar shown, answered together. A calendar shared at
  // busy answers with gray blocks and nothing else.
  const events = useQuery(
    () =>
      shown.length > 0
        ? Promise.all(
            shown.map((each) =>
              graphql<{ ListCalendarEvents: CalendarEvent[] }>(EVENTS, {
                calendarId: each.id,
                from: from.toISOString(),
                until: until.toISOString(),
              }),
            ),
          ).then((answers) => ({ ListCalendarEvents: answers.flatMap((answer) => answer.ListCalendarEvents) }))
        : Promise.resolve(null),
    [shownKey, from.getTime(), until.getTime()],
    { refresh: false },
  )

  const [draft, setDraft] = useState<Draft | null>(null)
  const [deleting, setDeleting] = useState<CalendarEvent | null>(null)
  const [busy, setBusy] = useState(false)
  const [opening, setOpening] = useState('')
  const openingGeneration = useRef(0)
  const [problem, setProblem] = useState<string | null>(null)

  // Whether it worked, so a dialog closes only on success. What happened is
  // said in a toast either way; the dialog also shows the reason, because
  // that is where the person is looking when a submit is refused.
  const run = async <Result,>(
    action: () => Promise<Result>,
    done: string | ((result: Result) => string),
  ): Promise<boolean> => {
    openingGeneration.current++
    setOpening('')
    setBusy(true)
    try {
      const result = await action()
      setDraft(null)
      setDeleting(null)
      setProblem(null)
      toast.done(typeof done === 'function' ? done(result) : done)
      try {
        await Promise.all([events.reload(), calendars.reload()])
      } catch (failure) {
        toast.failure(failure, t('calendar.refreshFailed'))
      }
      return true
    } catch (failure) {
      setProblem(failure instanceof Error ? failure.message : String(failure))
      toast.failure(failure, t('calendar.failed'))
      return false
    } finally {
      setBusy(false)
    }
  }

  const blank = (day: Date): Draft => {
    const at = new Date(day)
    if (at.getHours() === 0 && at.getMinutes() === 0) at.setHours(9, 0, 0, 0)
    const ends = new Date(at.getTime() + 60 * 60 * 1000)
    return {
      id: '',
      summary: '',
      location: '',
      description: '',
      startDate: dayKey(at),
      startTime: clockKey(at),
      endDate: dayKey(ends),
      endTime: clockKey(ends),
      allDay: false,
      recurrence: '',
      status: '',
      isPrivate: false,
      attendees: '',
      calendarId,
    }
  }

  // Editing reads the whole event first, because the list does not carry the
  // description or the repeat. Read it before showing the form, never
  // alongside it: the form sends every box and an empty box means "clear
  // this", so a dialog opened before the description had arrived would delete
  // the description of anybody quick enough to save.
  const edit = async (event: CalendarEvent) => {
    if (busy || submission.pending || submission.isWorking) return
    const generation = ++openingGeneration.current
    setProblem(null)
    setOpening(event.id)
    try {
      const answer = await graphql<{ GetCalendarEvent: CalendarEvent }>(GET, {
        calendarId: event.calendarId,
        id: event.id,
      })
      if (generation !== openingGeneration.current) return
      const full = answer.GetCalendarEvent
      // A calendar shared below edit is read, not edited: the form would only
      // be refused when it was sent.
      const reached = calendarsById.get(event.calendarId)
      if (reached && !canWrite(reached)) {
        setViewing({ ...full, calendarId: event.calendarId })
        return
      }
      const starts = new Date(full.startsAt)
      const ends = new Date(full.endsAt)
      // A whole-day event's dates are read from its own parts, not from what
      // they become in this reader's zone -- opening one west of Greenwich
      // and saving it would otherwise move it a day earlier every time.
      setDraft({
        id: full.id,
        summary: full.summary ?? '',
        location: full.location ?? '',
        description: full.description ?? '',
        startDate: dayOf(full),
        startTime: clockKey(starts),
        // A whole-day event's end is the morning after in the file, which
        // is not what anybody means by "ends". An event on the 14th said
        // it ended on the 15th, and typing the 14th and the 15th to mean
        // two days made one.
        endDate: full.allDay ? lastDayOf(full) : dayKey(ends),
        endTime: clockKey(ends),
        allDay: full.allDay,
        recurrence: full.recurrence ?? '',
        status: full.status ?? '',
        isPrivate: full.isPrivate ?? false,
        attendees: (full.attendees ?? []).map((attendee) => attendee.address).join('\n'),
        calendarId: event.calendarId,
      })
    } catch (failure) {
      toast.failure(failure, t('calendar.failed'))
    } finally {
      if (generation === openingGeneration.current) setOpening('')
    }
  }

  // A link from the agent's memory names the event in the address: the event
  // opens, once, as if it had been chosen, and the address forgets it when
  // the form closes.
  const linkedEventId = parameters.get('event') ?? ''
  const linkedEvent = useRef({ id: '', wasShown: false })
  useEffect(() => {
    if (!linkedEventId || !calendarId || linkedEvent.current.id === linkedEventId) return
    linkedEvent.current = { id: linkedEventId, wasShown: false }
    void edit({ id: linkedEventId, calendarId } as CalendarEvent)
    // edit is made again on every render; the event is opened once.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [linkedEventId, calendarId])
  useEffect(() => {
    if (draft && draft.id === linkedEventId) linkedEvent.current.wasShown = true
    if (!draft && linkedEventId && linkedEvent.current.wasShown) {
      linkedEvent.current = { id: '', wasShown: false }
      setParameters(
        (previous) => {
          const next = new URLSearchParams(previous)
          next.delete('event')
          return next
        },
        { replace: true },
      )
    }
  }, [draft, linkedEventId, setParameters])

  const found = useMemo(() => events.data?.ListCalendarEvents ?? [], [events.data])

  // Which calendar the open form writes to, and whether it is the person's
  // own: a circle's has no owner to send invitations from.
  const draftCalendar = draft ? (calendarsById.get(draft.calendarId) ?? calendar) : null
  const draftIsOwn = !draftCalendar || isOwn(draftCalendar)
  // The zone the event's days are counted in: the calendar's own, so that a
  // repeat keeps its hour when the clocks change. A whole-day event is a date
  // and has no zone at all.
  const draftZone = draft && !draft.allDay ? draftCalendar?.timezone || browserZone() : ''
  // What the event's repeat is read against: where it starts, the zone, and
  // whether it is a whole day.
  const repeatContext: RepeatContext = {
    startDate: draft?.startDate ?? '',
    timeZone: draftZone,
    isAllDay: draft?.allDay ?? false,
  }
  const [isEditingRepeat, setEditingRepeat] = useState(false)

  // Grouped by the day they start on, which is how every view here reads
  // them. An event running over several days is listed on the day it begins;
  // the grid says so by drawing it to the edge.
  const byDay = useMemo(() => {
    const grouped = new Map<string, CalendarEvent[]>()
    for (const event of found) {
      const key = dayOf(event)
      const already = grouped.get(key)
      if (already) already.push(event)
      else grouped.set(key, [event])
    }
    for (const list of grouped.values()) {
      list.sort((first, second) => {
        if (first.allDay !== second.allDay) return first.allDay ? -1 : 1
        // By the moment, not the text: calendars in other zones write the
        // same moment in other offsets.
        return new Date(first.startsAt).getTime() - new Date(second.startsAt).getTime()
      })
    }
    return grouped
  }, [found])

  // The agenda is one list in time order across every calendar shown: the
  // answers come one calendar after another, and read like that it was three
  // lists run together. A day's all-day events come first, as in the grids.
  const agenda = useMemo(
    () =>
      [...byDay.entries()]
        .sort(([firstDay], [secondDay]) => firstDay.localeCompare(secondDay))
        .flatMap(([, events]) => events),
    [byDay],
  )

  const dayFormat = useMemo(
    () => new Intl.DateTimeFormat(undefined, { weekday: 'short', day: 'numeric', month: 'short' }),
    [],
  )
  const timeFormat = useMemo(() => new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' }), [])
  const titleFormat = useMemo(() => new Intl.DateTimeFormat(undefined, { month: 'long', year: 'numeric' }), [])
  const weekdayFormat = useMemo(() => new Intl.DateTimeFormat(undefined, { weekday: 'short' }), [])
  const hourFormat = useMemo(() => new Intl.DateTimeFormat(undefined, { hour: 'numeric' }), [])

  // The line across today moves, so it is state rather than a value read
  // once: a calendar left open all afternoon showing the morning's line is
  // worse than showing none.
  const [now, setNow] = useState(() => new Date())
  useEffect(() => {
    const tick = setInterval(() => setNow(new Date()), 60 * 1000)
    return () => clearInterval(tick)
  }, [])

  // The grid opens at the working day rather than at midnight, and again
  // whenever the view or the day changes -- moving to next week should not
  // land wherever the last one happened to be scrolled.
  const grid = useRef<HTMLDivElement>(null)
  const page = useRef<HTMLDivElement>(null)

  // The page fills what is left of the content column, so the column itself
  // never scrolls and the grid is the only thing that does.
  //
  // Measured rather than set to 100%: the breadcrumb and the page title are
  // siblings inside that column, so a full-height child overshoots by exactly
  // their height and gives the two nested scrollbars this is here to avoid.
  // Watched, because it changes with the window and with a title that wraps.
  useEffect(() => {
    const fill = () => {
      const here = page.current
      const column = here?.closest('main') as HTMLElement | null
      if (!here || !column) return
      const style = getComputedStyle(column)
      const room =
        column.clientHeight -
        (here.getBoundingClientRect().top - column.getBoundingClientRect().top) -
        parseFloat(style.paddingBottom || '0')
      here.style.height = `${Math.max(280, room)}px`
    }
    fill()
    const watch = new ResizeObserver(fill)
    if (page.current) watch.observe(page.current.closest('main') as Element)
    window.addEventListener('resize', fill)
    return () => {
      watch.disconnect()
      window.removeEventListener('resize', fill)
    }
  }, [view])

  const heading =
    view === 'day'
      ? new Intl.DateTimeFormat(undefined, {
          weekday: 'long',
          day: 'numeric',
          month: 'long',
          year: 'numeric',
        }).format(on)
      : view === 'week' || view === 'workweek'
        ? t('calendar.weekOf', {
            day: dayFormat.format(view === 'workweek' ? startOfWorkWeek(on) : startOfWeek(on, weekStart)),
          })
        : view === 'agenda'
          ? t('calendar.agendaFrom', { day: dayFormat.format(on) })
          : titleFormat.format(on)

  const step = (direction: number) => {
    const columns = COLUMNS[view]
    // A day moves by a day; the working week and the week move by a whole
    // week, so that Friday's "next" is the following Monday rather than the
    // weekend the view does not draw.
    if (columns === 1) return move({ on: addDays(on, direction) })
    if (columns) {
      const start = view === 'workweek' ? startOfWorkWeek(on) : startOfWeek(on, weekStart)
      return move({ on: addDays(start, direction * 7) })
    }
    if (view === 'agenda') return move({ on: addDays(on, direction * 31) })
    return move({ on: new Date(on.getFullYear(), on.getMonth() + direction, 1) })
  }

  const today = dayKey(new Date())
  const loading = (events.loading && !events.data) || (calendars.loading && !calendars.data)

  // Scrolled before the first paint rather than after it, so the grid is
  // never shown at midnight and then jumped to the morning. It depends on
  // loading too: while the page is loading there is no grid to scroll, and an
  // effect that only watched the view found nothing and never ran again.
  useLayoutEffect(() => {
    if (loading || !COLUMNS[view] || !grid.current) return
    // A little above the line of the first hour, since its label is drawn
    // centered on the line and the line was flush with the header: the label
    // was half under it.
    grid.current.scrollTop = OPENS_AT * HOUR - HOUR_LABEL_OVERHANG
    const head = grid.current.querySelector('.calendar-grid-head')
    if (head) {
      grid.current.style.setProperty('--calendar-head', `${head.getBoundingClientRect().height}px`)
    }
  }, [view, on, loading])

  // "Busy · Ada" on a block of somebody else's calendar, so that several
  // people's blocks in one grid can be told apart; a shared event with a title
  // keeps its title and says whose it is on hover.
  const busyLabel = (event: CalendarEvent) => {
    const owner = ownerOfCalendar(calendarsById.get(event.calendarId))
    return owner ? t('calendar.busyOf', { name: owner }) : t('calendar.busy')
  }
  const titleWithOwner = (event: CalendarEvent) => {
    const calendar = calendarsById.get(event.calendarId)
    const owner = ownerOfCalendar(calendar)
    const title = event.summary || t('calendar.untitled')
    if (!owner) return title
    // A circle's calendar is the circle's, named as it is, and a person's is
    // theirs.
    return `${title} · ${calendar?.isCircleCalendar ? owner : t('calendar.ownersCalendar', { name: owner })}`
  }

  const canEditEvent = (event: CalendarEvent) => canWrite(calendarsById.get(event.calendarId) ?? ({} as Calendar))

  const entry = (event: CalendarEvent) => {
    // All the caller is told is that the owner is busy then: a gray block
    // that says so, and cannot be opened, because there is nothing in it.
    if (event.isBusyOnly) {
      return (
        <div
          key={event.calendarId + event.id + event.startsAt}
          className={`calendar-entry calendar-busy-block${event.allDay ? ' all-day' : ''}`}
        >
          {!event.allDay && <span className="calendar-entry-time">{timeFormat.format(new Date(event.startsAt))}</span>}
          <span className="calendar-entry-title">{busyLabel(event)}</span>
        </div>
      )
    }
    const tone = toneOf(calendarsById.get(event.calendarId))
    return (
      <Tooltip key={event.calendarId + event.id + event.startsAt} label={titleWithOwner(event)}>
        <button
          type="button"
          className={`calendar-entry${event.allDay ? ' all-day' : ''}${opening === event.id ? ' busy' : ''} ${tone.className}`}
          style={tone.style}
          onClick={() => void edit(event)}
          // The month draws an event as a dot on a phone, with no text, so
          // the name is given here; elsewhere the chip's own words, with its
          // time, are its name. On a phone the day opens the list, so the
          // dot is left out of the tab order.
          aria-label={isPhone && view === 'month' ? titleWithOwner(event) : undefined}
          tabIndex={isPhone && view === 'month' ? -1 : undefined}
          aria-disabled={!!submission.pending || submission.isWorking || busy}
        >
          {!event.allDay && <span className="calendar-entry-time">{timeFormat.format(new Date(event.startsAt))}</span>}
          <span className="calendar-entry-title">
            {event.isPrivate && <PrivateMark />}
            {event.summary || t('calendar.untitled')}
          </span>
        </button>
      </Tooltip>
    )
  }

  return (
    <div className="calendar-page" ref={page}>
      {submission.pending && (
        <div className="card" role="status">
          <h3>{submission.pending.summary || t('calendar.untitled')}</h3>
          <p>{t(submission.pending.operation === 'delete' ? 'calendar.pendingDelete' : 'calendar.pendingSave')}</p>
          {problem && <p className="error">{problem}</p>}
          <div className="page-actions">
            <button
              type="button"
              disabled={busy || submission.isWorking}
              onClick={() =>
                void run(() => submission.execute(), t('calendar.recovered')).then((done) => {
                  if (done) {
                    setDraft(null)
                    setDeleting(null)
                  }
                })
              }
            >
              {t('calendar.retry')}
            </button>
            <button
              type="button"
              disabled={busy || submission.isWorking}
              onClick={() =>
                void run(
                  () => submission.cancel(),
                  (isCompleted) => t(isCompleted ? 'calendar.recovered' : 'calendar.stopped'),
                )
              }
            >
              {t('calendar.stop')}
            </button>
          </div>
          <p className="muted">{t('calendar.stopHint')}</p>
        </div>
      )}

      {/* The views are a row of tabs, the same component the server and
          domain pages use: five of them is what a tab strip is for, and it
          scrolls sideways on a narrow screen rather than being cut off. */}
      <Tabs
        items={shownTabs(CALENDAR_TABS, { services: serviceCapabilities })}
        active={view}
        onSelect={(id) => move({ view: id as View })}
      />

      {view === 'reminders' && (
        <RemindersView
          focusReminderId={parameters.get('reminder') ?? ''}
          onFocused={() =>
            setParameters(
              (previous) => {
                const next = new URLSearchParams(previous)
                next.delete('reminder')
                return next
              },
              { replace: true },
            )
          }
        />
      )}

      {/* Every calendar the person reaches, each in its own color, with a
          check to show or hide it. Only when there is more than their own to
          choose between. */}
      {view !== 'reminders' && allCalendars.length > 1 && (
        <div className="calendar-list" role="group" aria-label={t('calendar.calendars')}>
          {allCalendars.map((each) => {
            const tone = toneOf(each)
            return (
              <label key={each.id} className={`calendar-chip ${tone.className}`} style={tone.style}>
                <input type="checkbox" checked={!hidden.includes(each.id)} onChange={() => toggleCalendar(each.id)} />
                <span className="calendar-chip-swatch" aria-hidden="true" />
                {!isOwn(each) && !each.isCircleCalendar && (
                  <Avatar
                    name={each.ownerName ?? ''}
                    avatarUrl={each.ownerAvatarUrl}
                    colorIndex={each.ownerAvatarColorIndex}
                    size={18}
                  />
                )}
                <span className="calendar-chip-name">{calendarLabel(each, t)}</span>
                {!isOwn(each) && (
                  <span className="muted calendar-chip-level">
                    {t(`calendar.level.${each.shareLevel ?? 'busy'}` as Parameters<typeof t>[0])}
                  </span>
                )}
              </label>
            )
          })}
        </div>
      )}

      {view !== 'reminders' && (
        <div className="calendar-bar">
          <div className="calendar-move">
            <Tooltip label={t('calendar.previous')}>
              <button
                type="button"
                className="icon-button"
                onClick={() => step(-1)}
                aria-label={t('calendar.previous')}
              >
                <ChevronLeftIcon />
              </button>
            </Tooltip>
            <button type="button" onClick={() => move({ on: new Date() })}>
              {t('calendar.today')}
            </button>
            <Tooltip label={t('calendar.next')}>
              <button type="button" className="icon-button" onClick={() => step(1)} aria-label={t('calendar.next')}>
                <ChevronRightIcon />
              </button>
            </Tooltip>
            <span className="calendar-heading">{heading}</span>
          </div>
          <div className="calendar-bar-actions">
            <button type="button" disabled={!calendarId} onClick={() => setFindingTime(true)}>
              {t('calendar.findTime')}
            </button>
            <button type="button" disabled={!calendarId} onClick={() => setSharing(true)}>
              {t('calendar.share')}
            </button>
            <button type="button" disabled={!calendarId} onClick={() => setRenaming(true)}>
              {t('common.rename')}
            </button>
            <button
              className="primary"
              type="button"
              disabled={writable.length === 0 || !!submission.pending || submission.isWorking || busy}
              onClick={() => {
                setProblem(null)
                setDraft(blank(on))
              }}
            >
              {t('calendar.new')}
            </button>
          </div>
        </div>
      )}

      {loading && view !== 'reminders' && <p className="calendar-notice muted">{t('common.loading')}</p>}

      {!loading && view === 'month' && (
        <div className="calendar-month-scroll">
          <div className="calendar-month" role="grid" aria-label={heading}>
            {monthGrid(on, weekStart)
              .slice(0, 7)
              .map((day) => (
                <div key={`head-${day.getTime()}`} className="calendar-weekday" role="columnheader">
                  {weekdayFormat.format(day)}
                </div>
              ))}
            {monthGrid(on, weekStart).map((day) => {
              const key = dayKey(day)
              const outside = day.getMonth() !== on.getMonth()
              return (
                <div
                  key={key}
                  role="gridcell"
                  className={`calendar-day${outside ? ' outside' : ''}${key === today ? ' today' : ''}`}
                >
                  {/* The date opens the day, which is what a date in a month
                      is for. Making something on that day is the empty space
                      below it, so the two are not the same click. */}
                  <Tooltip label={t('calendar.openDay', { day: dayFormat.format(day) })}>
                    <button
                      type="button"
                      className="calendar-day-number"
                      onClick={() => move({ view: 'day', on: day })}
                    >
                      {day.getDate()}
                    </button>
                  </Tooltip>
                  <div className="calendar-day-entries">{(byDay.get(key) ?? []).map(entry)}</div>
                  {/* Behind the date and the entries rather than around them:
                      a button inside a button is not a thing, and the empty
                      part of a day is the natural place to press to put
                      something in it. */}
                  {/* On a phone the events are dots, so a press on the day opens
                      the day, where they are written out. */}
                  <Tooltip
                    label={
                      isPhone
                        ? t('calendar.openDay', { day: dayFormat.format(day) })
                        : t('calendar.newOn', { day: dayFormat.format(day) })
                    }
                  >
                    <button
                      type="button"
                      className="calendar-day-add"
                      disabled={!isPhone && (!!submission.pending || submission.isWorking || busy)}
                      onClick={() => {
                        if (isPhone) return move({ view: 'day', on: day })
                        setProblem(null)
                        if (!busy && !submission.pending && !submission.isWorking) setDraft(blank(day))
                      }}
                      aria-label={
                        isPhone
                          ? t('calendar.openDay', { day: dayFormat.format(day) })
                          : t('calendar.newOn', { day: dayFormat.format(day) })
                      }
                    />
                  </Tooltip>
                </div>
              )
            })}
          </div>
        </div>
      )}

      {!loading && COLUMNS[view] && (
        <div className="calendar-grid-scroll" ref={grid}>
          {(() => {
            const span = COLUMNS[view] as number
            const days = Array.from({ length: span }, (_, index) =>
              addDays(
                span === 1 ? startOfDay(on) : view === 'workweek' ? startOfWorkWeek(on) : startOfWeek(on, weekStart),
                index,
              ),
            )
            const columns = `4rem repeat(${span}, minmax(0, 1fr))`
            const leastWidth = span === 1 ? undefined : `calc(4rem + ${span} * 7.5rem)`
            const anyAllDay = days.some((day) => (byDay.get(dayKey(day)) ?? []).some((event) => event.allDay))
            return (
              <div className="calendar-grid" style={{ gridTemplateColumns: columns, minWidth: leastWidth }}>
                {/* The day names stay put while the hours scroll under them:
                    a week scrolled to the afternoon with no dates on it is a
                    grid of numbers nobody can read. */}
                <div className="calendar-grid-corner" />
                {days.map((day) => {
                  const key = dayKey(day)
                  return (
                    <Tooltip key={`head-${key}`} label={t('calendar.viewDay')}>
                      <button
                        type="button"
                        className={`calendar-grid-head${key === today ? ' today' : ''}`}
                        onClick={() => move({ view: 'day', on: day })}
                      >
                        <span className="calendar-grid-weekday">{weekdayFormat.format(day)}</span>
                        <span className="calendar-grid-date">{day.getDate()}</span>
                      </button>
                    </Tooltip>
                  )
                })}

                {/* Anything that belongs to the whole day sits above the
                    hours rather than pretending to start at midnight. The
                    strip is there only when something is in it. */}
                {anyAllDay && (
                  <div className="calendar-grid-corner calendar-grid-allday-label">{t('calendar.allDay')}</div>
                )}
                {anyAllDay &&
                  days.map((day) => (
                    <div key={`allday-${dayKey(day)}`} className="calendar-grid-allday">
                      {(byDay.get(dayKey(day)) ?? []).filter((event) => event.allDay).map(entry)}
                    </div>
                  ))}

                <div className="calendar-hours">
                  {Array.from({ length: 24 }, (_, hour) => (
                    <div key={hour} className="calendar-hour" style={{ height: HOUR }}>
                      {hour > 0 && <span>{hourFormat.format(new Date(2026, 0, 1, hour))}</span>}
                    </div>
                  ))}
                </div>
                {days.map((day) => {
                  const key = dayKey(day)
                  const placed = layOut(day, byDay.get(key) ?? [])
                  return (
                    <div
                      key={`col-${key}`}
                      className={`calendar-column${key === today ? ' today' : ''}`}
                      style={{ height: 24 * HOUR }}
                      onClick={(clicked) => {
                        // A click on empty space makes something at that
                        // hour, which is what every calendar does and what
                        // stops a person editing the time they were just
                        // pointing at.
                        if (clicked.target !== clicked.currentTarget) return
                        const box = clicked.currentTarget.getBoundingClientRect()
                        const hour = Math.floor(((clicked.clientY - box.top) / HOUR) * 2) / 2
                        const at = new Date(day)
                        at.setHours(Math.floor(hour), hour % 1 ? 30 : 0, 0, 0)
                        setProblem(null)
                        if (!busy && !submission.pending && !submission.isWorking) setDraft(blank(at))
                      }}
                    >
                      {Array.from({ length: 24 }, (_, hour) => (
                        <div key={hour} className="calendar-slot" style={{ height: HOUR }} />
                      ))}
                      {placed.map(({ event, top, height, column, columns: across }) => {
                        const slot = {
                          top,
                          height,
                          left: `calc(${(column / across) * 100}% + 2px)`,
                          width: `calc(${(1 / across) * 100}% - 4px)`,
                        }
                        if (event.isBusyOnly) {
                          return (
                            <div
                              key={event.calendarId + event.id + event.startsAt}
                              className={`calendar-placed calendar-busy-block${height < 34 ? ' compact' : ''}`}
                              style={slot}
                            >
                              <span className="calendar-placed-time">
                                {timeFormat.format(new Date(event.startsAt))}
                              </span>
                              <span className="calendar-placed-title">{busyLabel(event)}</span>
                            </div>
                          )
                        }
                        const tone = toneOf(calendarsById.get(event.calendarId))
                        return (
                          <Tooltip key={event.calendarId + event.id + event.startsAt} label={titleWithOwner(event)}>
                            <button
                              type="button"
                              className={[
                                'calendar-placed',
                                height < 34 ? 'compact' : '',
                                opening === event.id ? 'busy' : '',
                                event.status === 'CANCELLED' ? 'cancelled' : '',
                                tone.className,
                              ]
                                .filter(Boolean)
                                .join(' ')}
                              style={{ ...slot, ...tone.style }}
                              onClick={() => void edit(event)}
                              aria-disabled={!!submission.pending || submission.isWorking || busy}
                            >
                              <span className="calendar-placed-time">
                                {timeFormat.format(new Date(event.startsAt))}
                              </span>
                              <span className="calendar-placed-title">
                                {event.isPrivate && <PrivateMark />}
                                {event.summary || t('calendar.untitled')}
                              </span>
                              {event.location && height > 56 && (
                                <span className="calendar-placed-where">{event.location}</span>
                              )}
                            </button>
                          </Tooltip>
                        )
                      })}
                      {key === today && (
                        <div
                          className="calendar-now"
                          style={{ top: (minutesInto(day, now) / 60) * HOUR }}
                          aria-hidden="true"
                        />
                      )}
                    </div>
                  )
                })}
              </div>
            )
          })()}
        </div>
      )}

      {!loading && view === 'agenda' && (
        <div className="calendar-agenda">
          {agenda.length === 0 && <p className="calendar-notice muted">{t('calendar.empty')}</p>}
          {agenda.map((event) => (
            <div key={event.calendarId + event.id + event.startsAt} className="calendar-agenda-row">
              <div className="calendar-agenda-when">
                <span>{dayFormat.format(noonOf(event))}</span>
                <span className="muted">
                  {event.allDay ? t('calendar.allDay') : timeFormat.format(new Date(event.startsAt))}
                </span>
              </div>
              <div className="calendar-agenda-what">
                <span className="calendar-agenda-title">
                  {event.isPrivate && <PrivateMark />}
                  {event.isBusyOnly ? busyLabel(event) : event.summary || t('calendar.untitled')}
                </span>
                {event.location && <span className="muted">{event.location}</span>}
                {event.recurring && <span className="muted">{t('calendar.repeats')}</span>}
                {/* A busy block already says whose it is, "Busy · name". */}
                {!event.isBusyOnly && !isOwn(calendarsById.get(event.calendarId) ?? ({} as Calendar)) && (
                  <span className="muted">{whoseCalendar(calendarsById.get(event.calendarId), t)}</span>
                )}
              </div>
              {!event.isBusyOnly && (
                <div className="row-actions">
                  {/* An event of a calendar shared below edit is only read, so the
                      action says View, with an eye, rather than promising an edit. */}
                  <Tooltip label={canEditEvent(event) ? t('common.edit') : t('common.view')}>
                    <button
                      type="button"
                      className="icon-button"
                      disabled={opening === event.id || !!submission.pending || submission.isWorking || busy}
                      onClick={() => void edit(event)}
                      aria-disabled={!!submission.pending || submission.isWorking || busy}
                      aria-label={canEditEvent(event) ? t('common.edit') : t('common.view')}
                    >
                      {canEditEvent(event) ? <PencilIcon /> : <EyeIcon />}
                    </button>
                  </Tooltip>
                  {canEditEvent(event) && (
                    <Tooltip label={t('common.delete')}>
                      <button
                        type="button"
                        className="icon-button danger"
                        disabled={!!submission.pending || submission.isWorking || busy}
                        onClick={() => setDeleting(event)}
                        aria-label={t('common.delete')}
                      >
                        <TrashIcon />
                      </button>
                    </Tooltip>
                  )}
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {draft && !submission.pending && (
        <FormDialog
          title={draft.id ? t('calendar.edit') : t('calendar.new')}
          submitLabel={draft.id ? t('common.save') : t('common.create')}
          busy={busy}
          error={problem}
          canSubmit={draft.summary.trim().length > 0 && draft.startDate.length > 0}
          otherAction={
            draft.id ? (
              <button
                type="button"
                className="danger"
                onClick={() => {
                  const held = found.find((event) => event.id === draft.id)
                  setDraft(null)
                  if (held) setDeleting(held)
                }}
              >
                {t('common.delete')}
              </button>
            ) : null
          }
          // The Custom repeat dialog is open over this one, and an Escape
          // that closes it must not close the event under it as well.
          onClose={() => !isEditingRepeat && setDraft(null)}
          onSubmit={() =>
            void run(
              () =>
                // Every box, including the empty ones. The server treats a
                // field left out as "leave it alone" and a field given empty
                // as "clear it", and this form shows all of them.
                submission.execute({
                  operation: 'save',
                  summary: draft.summary.trim(),
                  variables: {
                    calendarId: draft.calendarId || calendarId,
                    id: draft.id || null,
                    summary: draft.summary.trim(),
                    location: draft.location.trim(),
                    description: draft.description.trim(),
                    startsAt: draft.allDay ? `${draft.startDate}T00:00:00Z` : joined(draft.startDate, draft.startTime),
                    endsAt: draft.allDay
                      ? `${dayShifted(draft.endDate || draft.startDate, 1)}T00:00:00Z`
                      : joined(draft.endDate || draft.startDate, draft.endTime),
                    allDay: draft.allDay,
                    timezone: draftZone,
                    recurrence: draft.recurrence.trim(),
                    status: draft.status.trim(),
                    // Only in the person's own calendar: in a circle's, or one
                    // shared with them at edit, every reader is an editor and
                    // sees the event whatever it says.
                    isPrivate: draftIsOwn ? draft.isPrivate : null,
                    // Every line, including none: an empty box means nobody
                    // is coming, which is a different thing from the meeting
                    // being called off.
                    // Only in the person's own calendar: the invitations go
                    // out from, and the answers come back to, the owner, so a
                    // circle's calendar or one shared with them has none.
                    attendees: draftIsOwn
                      ? draft.attendees
                          .split('\n')
                          .map((line) => line.trim())
                          .filter(Boolean)
                      : null,
                  },
                }),
              // Which calendar it went into, when it is not the person's own:
              // "in your calendar" was said of a circle's too.
              savedWords(draft, calendarsById.get(draft.calendarId || calendarId), t),
            ).then((done) => done && setDraft(null))
          }
        >
          {/* Which calendar a new event goes in, when there is a choice: the
              person's own, a circle's, or one shared with them at edit. An
              event already kept stays where it is. */}
          {!draft.id && writable.length > 1 && (
            <label>
              {t('calendar.calendarChoice')}
              <select
                value={draft.calendarId}
                onChange={(event) => setDraft({ ...draft, calendarId: event.target.value })}
              >
                {writable.map((each) => (
                  <option key={each.id} value={each.id}>
                    {calendarLabel(each, t)}
                  </option>
                ))}
              </select>
            </label>
          )}
          <label>
            {t('calendar.summary')}
            <input value={draft.summary} onChange={(event) => setDraft({ ...draft, summary: event.target.value })} />
          </label>
          <label>
            {t('calendar.location')}
            <input value={draft.location} onChange={(event) => setDraft({ ...draft, location: event.target.value })} />
          </label>
          <label className="checkbox">
            <input
              type="checkbox"
              checked={draft.allDay}
              onChange={(event) => setDraft({ ...draft, allDay: event.target.checked })}
            />
            {t('calendar.allDay')}
          </label>
          {draftIsOwn && (
            <>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={draft.isPrivate}
                  onChange={(event) => setDraft({ ...draft, isPrivate: event.target.checked })}
                />
                {t('calendar.private')}
              </label>
              <p className="muted field-hint">{t('calendar.privateHint')}</p>
            </>
          )}
          <div className="field-row">
            <label>
              {t('calendar.starts')}
              <input
                type="date"
                value={draft.startDate}
                onChange={(event) => setDraft({ ...draft, startDate: event.target.value })}
              />
            </label>
            {!draft.allDay && (
              <label>
                {t('calendar.at')}
                <input
                  type="time"
                  value={draft.startTime}
                  onChange={(event) => setDraft({ ...draft, startTime: event.target.value })}
                />
              </label>
            )}
          </div>
          <div className="field-row">
            <label>
              {t('calendar.ends')}
              <input
                type="date"
                value={draft.endDate}
                onChange={(event) => setDraft({ ...draft, endDate: event.target.value })}
              />
            </label>
            {!draft.allDay && (
              <label>
                {t('calendar.at')}
                <input
                  type="time"
                  value={draft.endTime}
                  onChange={(event) => setDraft({ ...draft, endTime: event.target.value })}
                />
              </label>
            )}
          </div>
          <label>
            {t('calendar.repeat')}
            <select
              value={presetOf(draft.recurrence)}
              onChange={(event) => {
                const preset = event.target.value as RepeatPreset
                if (preset === 'custom') setEditingRepeat(true)
                else setDraft({ ...draft, recurrence: presetRule(preset) })
              }}
            >
              {REPEAT_PRESETS.map((preset) => (
                <option key={preset} value={preset}>
                  {t(`calendar.repeatEvery.${preset}` as Parameters<typeof t>[0])}
                </option>
              ))}
              <option value="custom">{t('calendar.repeatCustom')}</option>
            </select>
          </label>
          {presetOf(draft.recurrence) === 'custom' && (
            <RepeatSummary rule={draft.recurrence} context={repeatContext} onChange={() => setEditingRepeat(true)} />
          )}
          {draftIsOwn && (
            <label>
              {t('calendar.guests')}
              <textarea
                rows={2}
                value={draft.attendees}
                onChange={(event) => setDraft({ ...draft, attendees: event.target.value })}
              />
              <span className="muted">{t('calendar.guestsHint')}</span>
            </label>
          )}
          <label>
            {t('calendar.description')}
            <textarea
              rows={3}
              value={draft.description}
              onChange={(event) => setDraft({ ...draft, description: event.target.value })}
            />
          </label>
          {draft.id ? <InMemory recordKind="calendar_event" recordId={draft.id} /> : null}
        </FormDialog>
      )}

      {draft && isEditingRepeat && !submission.pending && (
        <RepeatDialog
          rule={draft.recurrence}
          context={repeatContext}
          weekStart={weekStart}
          onSave={(recurrence) => {
            setDraft({ ...draft, recurrence })
            setEditingRepeat(false)
          }}
          onClose={() => setEditingRepeat(false)}
        />
      )}

      {viewing && (
        <ConfirmDialog
          title={viewing.isBusyOnly ? t('calendar.busy') : viewing.summary || t('calendar.untitled')}
          destructive={false}
          wide
          body={
            <div className="calendar-viewed">
              <p className="muted">
                {t('calendar.readOnlyNote', { name: whoseCalendar(calendarsById.get(viewing.calendarId), t) })}
              </p>
              <p>
                {viewing.allDay
                  ? dayFormat.format(noonOf(viewing))
                  : `${dayFormat.format(new Date(viewing.startsAt))} ${timeFormat.format(new Date(viewing.startsAt))} - ${timeFormat.format(new Date(viewing.endsAt))}`}
              </p>
              {viewing.location && <p>{viewing.location}</p>}
              {viewing.description && <p className="calendar-viewed-notes">{viewing.description}</p>}
              {(viewing.attendees ?? []).length > 0 && (
                <p className="muted">
                  {(viewing.attendees ?? []).map((guest) => guest.name || guest.address).join(', ')}
                </p>
              )}
            </div>
          }
          onClose={() => setViewing(null)}
        />
      )}

      {isSharing && calendar && <CalendarShareDialog calendar={calendar} onClose={() => setSharing(false)} />}
      {isRenaming && calendar && (
        <CalendarRenameDialog
          calendar={calendar}
          onClose={() => setRenaming(false)}
          onSaved={() => calendars.reload()}
        />
      )}
      {isFindingTime && <FindATimeDialog ownUserId={ownerId} onClose={() => setFindingTime(false)} />}

      {deleting && !submission.pending && (
        <ConfirmDialog
          title={t('calendar.deleteTitle')}
          body={t('calendar.deleteBody', { name: deleting.summary || t('calendar.untitled') })}
          confirmLabel={t('common.delete')}
          busy={busy}
          onClose={() => setDeleting(null)}
          onConfirm={() =>
            void run(
              () =>
                submission.execute({
                  operation: 'delete',
                  summary: deleting.summary || t('calendar.untitled'),
                  variables: { calendarId: deleting.calendarId, id: deleting.id },
                }),
              t('calendar.saidDeleted', { name: deleting.summary || t('calendar.untitled') }),
            ).then((done) => done && setDeleting(null))
          }
        />
      )}
    </div>
  )
}

// joined turns the two halves a browser edits into the one moment the server
// takes. Built through Date so that the browser's own zone is applied: the
// person typed ten o'clock where they are, not ten o'clock UTC.
function joined(day: string, clock: string): string {
  if (!day) return ''
  const at = new Date(`${day}T${clock || '00:00'}:00`)
  if (Number.isNaN(at.getTime())) return ''
  return at.toISOString()
}

// RepeatSummary says what a custom repeat is, in a sentence, beside the button
// that changes it. A rule this page cannot read at all has no sentence, only
// the note that it is kept as it is.
function RepeatSummary({ rule, context, onChange }: { rule: string; context: RepeatContext; onChange: () => void }) {
  const translation = useTranslation()
  const { t } = translation
  const parts = parseRepeat(rule, context)
  return (
    <div className="calendar-repeat-summary">
      <div>
        {parts && <p className="muted">{describeRepeat(parts, translation)}</p>}
        {(!parts || parts.hasUnshownParts) && <p className="muted">{t('calendar.repeatKept')}</p>}
      </div>
      <button type="button" className="button-quiet" onClick={onChange}>
        {t('calendar.repeatEdit')}
      </button>
    </div>
  )
}

// browserZone is where the person reading the page is, which is the best
// guess at what they mean when they type a time and their calendar has no
// zone of its own.
function browserZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || ''
  } catch {
    return ''
  }
}
