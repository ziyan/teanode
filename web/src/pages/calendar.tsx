import { useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { graphql } from '../api'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { ChevronLeftIcon, ChevronRightIcon, PencilIcon, TrashIcon } from '../components/icons'
import { Tooltip } from '../components/tooltip'
import { useQuery } from '../components/useQuery'
import { useToast } from '../components/toast'
import { useTranslation } from '../i18n/i18n'

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

const CALENDARS = `query { ListCalendars { id name description colour timezone events } }`

const EVENTS = `
  query ($calendarId: String!, $from: String!, $until: String!) {
    ListCalendarEvents(calendarId: $calendarId, from: $from, until: $until) {
      id calendarId uid summary location startsAt endsAt allDay recurring occurrence status
    }
  }`

const GET = `
  query ($calendarId: String!, $id: String!) {
    GetCalendarEvent(calendarId: $calendarId, id: $id) {
      id uid etag summary location description startsAt endsAt allDay
      recurring recurrence status timezone organizer attendees { address name participation role }
    }
  }`

const SAVE = `
  mutation ($calendarId: String!, $id: String, $summary: String, $location: String,
            $description: String, $startsAt: String, $endsAt: String, $allDay: Boolean,
            $timezone: String, $recurrence: String, $status: String, $attendees: [String!]) {
    SaveCalendarEvent(calendarId: $calendarId, id: $id, summary: $summary, location: $location,
                      description: $description, startsAt: $startsAt, endsAt: $endsAt, allDay: $allDay,
                      timezone: $timezone, recurrence: $recurrence, status: $status,
                      attendees: $attendees) { id uid summary }
  }`

const DELETE = `
  mutation ($calendarId: String!, $id: String!) { DeleteCalendarEvent(calendarId: $calendarId, id: $id) }`

type Calendar = {
  id: string
  name: string
  description?: string
  colour?: string
  timezone?: string
  events: number
}

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
}

type View = 'month' | 'week' | 'workweek' | 'day' | 'agenda'

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
  // One address a line, which is how somebody with four guests expects to
  // type them and avoids a row of controls for adding and removing lines.
  attendees: string
}

// The repeats worth offering as a list, each with a name a catalogue can
// carry. Anything else a phone or another program set is kept and shown as it
// is, because a rule this page cannot name is still a rule the person meant.
const REPEATS = [
  { name: 'never', rule: '' },
  { name: 'daily', rule: 'FREQ=DAILY' },
  { name: 'weekly', rule: 'FREQ=WEEKLY' },
  { name: 'monthly', rule: 'FREQ=MONTHLY' },
  { name: 'yearly', rule: 'FREQ=YEARLY' },
] as const

function namedRepeat(rule: string): boolean {
  return REPEATS.some((repeat) => repeat.rule === rule)
}

// The column views and how wide each is. A day is one column, the working
// week five, the week seven -- all the same drawing, differing only in how
// many days it starts from and how many it shows.
const COLUMNS: Partial<Record<View, number>> = { day: 1, workweek: 5, week: 7 }

// dayKey names a day the way a date input does, in local time. Not the ISO
// string, which is in UTC and so is the wrong day for anybody east or west of
// it for part of every day.
function dayKey(at: Date): string {
  const month = `${at.getMonth() + 1}`.padStart(2, '0')
  const day = `${at.getDate()}`.padStart(2, '0')
  return `${at.getFullYear()}-${month}-${day}`
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

// startOfWeek is the Monday on or before a day. Monday because the rest of
// this server's dates are written the way most of the world writes them.
function startOfWeek(at: Date): Date {
  const day = startOfDay(at)
  const weekday = (day.getDay() + 6) % 7
  return addDays(day, -weekday)
}

// The six weeks a month view draws: whole weeks, so the grid is rectangular
// and the days either side of the month are shown greyed rather than blank.
function monthGrid(at: Date): Date[] {
  const first = new Date(at.getFullYear(), at.getMonth(), 1)
  const start = startOfWeek(first)
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

export function CalendarPage() {
  const { t } = useTranslation()
  const toast = useToast()
  const [parameters, setParameters] = useSearchParams()

  // The view and the day are in the address, so that a link to a week is a
  // link to that week and the back button goes where it looks like it goes.
  const view = (parameters.get('view') as View) || 'month'
  const on = parseDay(parameters.get('on'))
  const move = (next: { view?: View; on?: Date }) => {
    const updated = new URLSearchParams(parameters)
    if (next.view) updated.set('view', next.view)
    if (next.on) updated.set('on', dayKey(next.on))
    setParameters(updated, { replace: false })
  }

  const calendars = useQuery(() => graphql<{ ListCalendars: Calendar[] }>(CALENDARS), [], { refresh: false })
  const calendar = calendars.data?.ListCalendars?.[0] ?? null
  const calendarId = calendar?.id ?? ''

  // The window asked for is whole weeks for a month, the week for a week, and
  // a month ahead for an agenda.
  const [from, until] = useMemo<[Date, Date]>(() => {
    const columns = COLUMNS[view]
    if (columns) {
      const start = columns === 1 ? startOfDay(on) : startOfWeek(on)
      return [start, addDays(start, columns)]
    }
    if (view === 'agenda') return [startOfDay(on), addDays(startOfDay(on), 31)]
    const grid = monthGrid(on)
    return [grid[0], addDays(grid[41], 1)]
  }, [view, on.getTime()])

  const events = useQuery(
    () =>
      calendarId
        ? graphql<{ ListCalendarEvents: CalendarEvent[] }>(EVENTS, {
            calendarId,
            from: from.toISOString(),
            until: until.toISOString(),
          })
        : Promise.resolve(null),
    [calendarId, from.getTime(), until.getTime()],
    { refresh: false },
  )

  const [draft, setDraft] = useState<Draft | null>(null)
  const [deleting, setDeleting] = useState<CalendarEvent | null>(null)
  const [busy, setBusy] = useState(false)
  const [opening, setOpening] = useState('')
  const [problem, setProblem] = useState<string | null>(null)

  // Whether it worked, so a dialog closes only on success. What happened is
  // said in a toast either way; the dialog also shows the reason, because
  // that is where the person is looking when a submit is refused.
  const run = async (action: () => Promise<unknown>, done: string): Promise<boolean> => {
    setBusy(true)
    try {
      await action()
      setProblem(null)
      await Promise.all([events.reload(), calendars.reload()])
      toast.done(done)
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
      attendees: '',
    }
  }

  // Editing reads the whole event first, because the list does not carry the
  // description or the repeat. Read it before showing the form, never
  // alongside it: the form sends every box and an empty box means "clear
  // this", so a dialog opened before the description had arrived would delete
  // the description of anybody quick enough to save.
  const edit = async (event: CalendarEvent) => {
    setProblem(null)
    setOpening(event.id)
    try {
      const answer = await graphql<{ GetCalendarEvent: CalendarEvent }>(GET, {
        calendarId: event.calendarId,
        id: event.id,
      })
      const full = answer.GetCalendarEvent
      const starts = new Date(full.startsAt)
      const ends = new Date(full.endsAt)
      setDraft({
        id: full.id,
        summary: full.summary ?? '',
        location: full.location ?? '',
        description: full.description ?? '',
        startDate: dayKey(starts),
        startTime: clockKey(starts),
        endDate: dayKey(ends),
        endTime: clockKey(ends),
        allDay: full.allDay,
        recurrence: full.recurrence ?? '',
        status: full.status ?? '',
        attendees: (full.attendees ?? []).map((attendee) => attendee.address).join('\n'),
      })
    } catch (failure) {
      toast.failure(failure, t('calendar.failed'))
    } finally {
      setOpening('')
    }
  }

  const found = events.data?.ListCalendarEvents ?? []

  // Grouped by the day they start on, which is how every view here reads
  // them. An event running over several days is listed on the day it begins;
  // the grid says so by drawing it to the edge.
  const byDay = useMemo(() => {
    const grouped = new Map<string, CalendarEvent[]>()
    for (const event of found) {
      const key = dayKey(new Date(event.startsAt))
      const already = grouped.get(key)
      if (already) already.push(event)
      else grouped.set(key, [event])
    }
    for (const list of grouped.values()) {
      list.sort((first, second) => {
        if (first.allDay !== second.allDay) return first.allDay ? -1 : 1
        return first.startsAt.localeCompare(second.startsAt)
      })
    }
    return grouped
  }, [found])

  const dayFormat = useMemo(
    () => new Intl.DateTimeFormat(undefined, { weekday: 'short', day: 'numeric', month: 'short' }),
    [],
  )
  const timeFormat = useMemo(() => new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' }), [])
  const titleFormat = useMemo(() => new Intl.DateTimeFormat(undefined, { month: 'long', year: 'numeric' }), [])
  const weekdayFormat = useMemo(() => new Intl.DateTimeFormat(undefined, { weekday: 'short' }), [])

  const heading =
    view === 'day'
      ? new Intl.DateTimeFormat(undefined, {
          weekday: 'long',
          day: 'numeric',
          month: 'long',
          year: 'numeric',
        }).format(on)
      : view === 'week' || view === 'workweek'
        ? t('calendar.weekOf', { day: dayFormat.format(startOfWeek(on)) })
        : view === 'agenda'
          ? t('calendar.agendaFrom', { day: dayFormat.format(on) })
          : titleFormat.format(on)

  const step = (direction: number) => {
    const columns = COLUMNS[view]
    // A day moves by a day; the working week and the week move by a whole
    // week, so that Friday's "next" is the following Monday rather than the
    // weekend the view does not draw.
    if (columns === 1) return move({ on: addDays(on, direction) })
    if (columns) return move({ on: addDays(startOfWeek(on), direction * 7) })
    if (view === 'agenda') return move({ on: addDays(on, direction * 31) })
    return move({ on: new Date(on.getFullYear(), on.getMonth() + direction, 1) })
  }

  const today = dayKey(new Date())
  const loading = (events.loading && !events.data) || (calendars.loading && !calendars.data)

  const entry = (event: CalendarEvent) => (
    <button
      key={event.id + event.startsAt}
      type="button"
      className={`calendar-entry${event.allDay ? ' all-day' : ''}${opening === event.id ? ' busy' : ''}`}
      onClick={() => void edit(event)}
      title={event.summary || t('calendar.untitled')}
    >
      {!event.allDay && <span className="calendar-entry-time">{timeFormat.format(new Date(event.startsAt))}</span>}
      <span className="calendar-entry-title">{event.summary || t('calendar.untitled')}</span>
    </button>
  )

  return (
    <>
      <h3>{t('calendar.title')}</h3>
      <p className="muted">{t('calendar.hint')}</p>

      <div className="calendar-bar">
        <div className="calendar-move">
          <Tooltip label={t('calendar.previous')}>
            <button type="button" className="icon-button" onClick={() => step(-1)} aria-label={t('calendar.previous')}>
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
        <div className="calendar-views">
          {(['month', 'week', 'workweek', 'day', 'agenda'] as View[]).map((which) => (
            <button
              key={which}
              type="button"
              className={view === which ? 'chosen' : undefined}
              onClick={() => move({ view: which })}
            >
              {t(`calendar.view${which[0].toUpperCase()}${which.slice(1)}` as Parameters<typeof t>[0])}
            </button>
          ))}
          <button
            className="primary"
            type="button"
            disabled={!calendarId}
            onClick={() => {
              setProblem(null)
              setDraft(blank(on))
            }}
          >
            {t('calendar.new')}
          </button>
        </div>
      </div>

      {loading && <p className="muted">{t('common.loading')}</p>}

      {!loading && view === 'month' && (
        <div className="calendar-month-scroll">
          <div className="calendar-month" role="grid" aria-label={heading}>
            {monthGrid(on)
              .slice(0, 7)
              .map((day) => (
                <div key={`head-${day.getTime()}`} className="calendar-weekday" role="columnheader">
                  {weekdayFormat.format(day)}
                </div>
              ))}
            {monthGrid(on).map((day) => {
              const key = dayKey(day)
              const outside = day.getMonth() !== on.getMonth()
              return (
                <div
                  key={key}
                  role="gridcell"
                  className={`calendar-day${outside ? ' outside' : ''}${key === today ? ' today' : ''}`}
                >
                  <button
                    type="button"
                    className="calendar-day-number"
                    onClick={() => {
                      setProblem(null)
                      setDraft(blank(day))
                    }}
                    title={t('calendar.newOn', { day: dayFormat.format(day) })}
                  >
                    {day.getDate()}
                  </button>
                  <div className="calendar-day-entries">{(byDay.get(key) ?? []).map(entry)}</div>
                </div>
              )
            })}
          </div>
        </div>
      )}

      {!loading && COLUMNS[view] && (
        <div className="calendar-week-scroll">
          <div className="calendar-week" style={{ gridTemplateColumns: `repeat(${COLUMNS[view]}, minmax(0, 1fr))` }}>
            {Array.from({ length: COLUMNS[view] as number }, (_, index) =>
              addDays(COLUMNS[view] === 1 ? startOfDay(on) : startOfWeek(on), index),
            ).map((day) => {
              const key = dayKey(day)
              return (
                <div key={key} className={`calendar-day${key === today ? ' today' : ''}`}>
                  <button
                    type="button"
                    className="calendar-day-number"
                    onClick={() => {
                      setProblem(null)
                      setDraft(blank(day))
                    }}
                    title={t('calendar.newOn', { day: dayFormat.format(day) })}
                  >
                    {dayFormat.format(day)}
                  </button>
                  <div className="calendar-day-entries">{(byDay.get(key) ?? []).map(entry)}</div>
                </div>
              )
            })}
          </div>
        </div>
      )}

      {!loading && view === 'agenda' && (
        <div className="calendar-agenda">
          {found.length === 0 && <p className="muted">{t('calendar.empty')}</p>}
          {found.map((event) => (
            <div key={event.id + event.startsAt} className="calendar-agenda-row">
              <div className="calendar-agenda-when">
                <span>{dayFormat.format(new Date(event.startsAt))}</span>
                <span className="muted">
                  {event.allDay ? t('calendar.allDay') : timeFormat.format(new Date(event.startsAt))}
                </span>
              </div>
              <div className="calendar-agenda-what">
                <span className="calendar-agenda-title">{event.summary || t('calendar.untitled')}</span>
                {event.location && <span className="muted">{event.location}</span>}
                {event.recurring && <span className="muted">{t('calendar.repeats')}</span>}
              </div>
              <div className="row-actions">
                <Tooltip label={t('common.edit')}>
                  <button
                    type="button"
                    className="icon-button"
                    disabled={opening === event.id}
                    onClick={() => void edit(event)}
                    aria-label={t('common.edit')}
                  >
                    <PencilIcon />
                  </button>
                </Tooltip>
                <Tooltip label={t('common.delete')}>
                  <button
                    type="button"
                    className="icon-button danger"
                    onClick={() => setDeleting(event)}
                    aria-label={t('common.delete')}
                  >
                    <TrashIcon />
                  </button>
                </Tooltip>
              </div>
            </div>
          ))}
        </div>
      )}

      {draft && (
        <FormDialog
          title={draft.id ? t('calendar.edit') : t('calendar.new')}
          submitLabel={draft.id ? t('common.save') : t('common.create')}
          busy={busy}
          error={problem}
          canSubmit={draft.summary.trim().length > 0 && draft.startDate.length > 0}
          onClose={() => setDraft(null)}
          onSubmit={() =>
            void run(
              () =>
                // Every box, including the empty ones. The server treats a
                // field left out as "leave it alone" and a field given empty
                // as "clear it", and this form shows all of them.
                graphql(SAVE, {
                  calendarId,
                  id: draft.id || null,
                  summary: draft.summary.trim(),
                  location: draft.location.trim(),
                  description: draft.description.trim(),
                  startsAt: joined(draft.startDate, draft.allDay ? '00:00' : draft.startTime),
                  endsAt: joined(draft.endDate || draft.startDate, draft.allDay ? '00:00' : draft.endTime),
                  allDay: draft.allDay,
                  // The calendar's own zone, so that a repeat keeps its
                  // hour when the clocks change. An all-day event is a
                  // date and has no zone at all.
                  timezone: draft.allDay ? '' : calendar?.timezone || browserZone(),
                  recurrence: draft.recurrence.trim(),
                  status: draft.status.trim(),
                  // Every line, including none: an empty box means nobody
                  // is coming, which is a different thing from the meeting
                  // being called off.
                  attendees: draft.attendees
                    .split('\n')
                    .map((line) => line.trim())
                    .filter(Boolean),
                }),
              draft.id
                ? t('calendar.saidSaved', { name: draft.summary.trim() })
                : t('calendar.saidAdded', { name: draft.summary.trim() }),
            ).then((done) => done && setDraft(null))
          }
        >
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
              value={namedRepeat(draft.recurrence) ? draft.recurrence : 'other'}
              onChange={(event) =>
                setDraft({
                  ...draft,
                  recurrence: event.target.value === 'other' ? draft.recurrence : event.target.value,
                })
              }
            >
              {REPEATS.map((repeat) => (
                <option key={repeat.name} value={repeat.rule}>
                  {t(`calendar.repeatEvery.${repeat.name}` as Parameters<typeof t>[0])}
                </option>
              ))}
              {!namedRepeat(draft.recurrence) && <option value="other">{t('calendar.repeatOther')}</option>}
            </select>
            {/* A rule another program set is shown as it is rather than
                quietly replaced by the nearest one on the list. */}
            {!namedRepeat(draft.recurrence) && <span className="mono muted">{draft.recurrence}</span>}
          </label>
          <label>
            {t('calendar.guests')}
            <textarea
              rows={2}
              value={draft.attendees}
              onChange={(event) => setDraft({ ...draft, attendees: event.target.value })}
            />
            <span className="muted">{t('calendar.guestsHint')}</span>
          </label>
          <label>
            {t('calendar.description')}
            <textarea
              rows={3}
              value={draft.description}
              onChange={(event) => setDraft({ ...draft, description: event.target.value })}
            />
          </label>
          {draft.id && (
            <div className="page-actions">
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
            </div>
          )}
        </FormDialog>
      )}

      {deleting && (
        <ConfirmDialog
          title={t('calendar.deleteTitle')}
          body={t('calendar.deleteBody', { name: deleting.summary || t('calendar.untitled') })}
          confirmLabel={t('common.delete')}
          busy={busy}
          onClose={() => setDeleting(null)}
          onConfirm={() =>
            void run(
              () => graphql(DELETE, { calendarId: deleting.calendarId, id: deleting.id }),
              t('calendar.saidDeleted', { name: deleting.summary || t('calendar.untitled') }),
            ).then((done) => done && setDeleting(null))
          }
        />
      )}
    </>
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
