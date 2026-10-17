import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'

import { failureText, graphql } from '../api'
import { FormDialog } from './dialog'
import { FreeStretch, BusyPeriod, freeByDay, minutesOf } from './freeTime'
import { personLabel } from './personLabel'
import { useQuery } from './useQuery'
import { useTranslation } from '../i18n/i18n'

// Finding a time when several people are all free: the people who shared their
// calendar with the person (at busy or more, whatever else), a run of days and
// the hours of the day to offer, and the stretches nobody has anything booked
// in. The server says when each is busy (ListCalendarFreeBusy, which the
// command line's `calendar free --with` and the agent's free_together ask too)
// and the sum is freeByDay, the same kind of sum they make, with the days and
// the hours in the browser's zone and not the calendar's.

type Person = { userId: string; username: string; name: string }

type FreeBusy = {
  userId: string
  username: string
  name: string
  isShared: boolean
  periods: BusyPeriod[]
}

const LIST_PEOPLE = `query { ListCirclePeople { userId username name } }`
const FREE_BUSY = `query ($userIds: [String!]!, $from: String!, $until: String!) {
  ListCalendarFreeBusy(userIds: $userIds, from: $from, until: $until) { userId username name isShared periods { startsAt endsAt } }
}`

// The most people one question may be about (the server's limit is 20, and the
// person is one of them).
const MOST_PEOPLE_PER_QUESTION = 19

const dayText = (date: Date) =>
  `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`

// dayOf reads a day typed or chosen as YYYY-MM-DD, in the browser's zone.
function dayOf(text: string): Date | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(text)
  return match ? new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3])) : null
}

// freeBusyOf asks when the people are busy, a question at a time when there
// are more of them than one question may hold.
async function freeBusyOf(userIds: string[], from: Date, until: Date): Promise<FreeBusy[]> {
  const answers: FreeBusy[] = []
  for (let start = 0; start < userIds.length; start += MOST_PEOPLE_PER_QUESTION) {
    const asked = await graphql<{ ListCalendarFreeBusy: FreeBusy[] }>(FREE_BUSY, {
      userIds: userIds.slice(start, start + MOST_PEOPLE_PER_QUESTION),
      from: from.toISOString(),
      until: until.toISOString(),
    })
    answers.push(...asked.ListCalendarFreeBusy)
  }
  return answers
}

// FindATimeDialog is the question and its answer in one dialog: the answer is
// listed under the question, which stays as it was typed, so a different set
// of hours or days is one change away.
export function FindATimeDialog({ ownUserId, onClose }: { ownUserId: string; onClose: () => void }) {
  const { t } = useTranslation()
  const today = useMemo(() => new Date(), [])
  const sharers = useQuery(
    async () => {
      const people = (await graphql<{ ListCirclePeople: Person[] }>(LIST_PEOPLE)).ListCirclePeople
      if (people.length === 0) return { people: [] as Person[], isAnyoneLeftOut: false }
      // Who shared is learned by asking about the next day, which costs one
      // question and says isShared for each.
      const tomorrow = new Date(today.getFullYear(), today.getMonth(), today.getDate() + 1)
      const asked = await freeBusyOf(
        people.map((person) => person.userId),
        today,
        tomorrow,
      )
      const sharing = new Set(asked.filter((each) => each.isShared).map((each) => each.userId))
      return {
        people: people.filter((person) => sharing.has(person.userId)),
        isAnyoneLeftOut: sharing.size < people.length,
      }
    },
    [today],
    { refresh: false },
  )
  const [chosen, setChosen] = useState<string[]>([])
  const [from, setFrom] = useState(dayText(today))
  const [to, setTo] = useState(dayText(new Date(today.getFullYear(), today.getMonth(), today.getDate() + 6)))
  const [earliest, setEarliest] = useState('09:00')
  const [latest, setLatest] = useState('17:00')
  const [isBusy, setIsBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  const [found, setFound] = useState<FreeStretch[] | null>(null)

  const people = sharers.data?.people ?? []
  const dayFormat = useMemo(
    () => new Intl.DateTimeFormat(undefined, { weekday: 'long', day: 'numeric', month: 'long' }),
    [],
  )
  const timeFormat = useMemo(() => new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' }), [])

  async function find() {
    const first = dayOf(from)
    const last = dayOf(to)
    const opens = minutesOf(earliest)
    const closes = minutesOf(latest)
    if (!first || !last || last < first) {
      setProblem(t('calendar.findTimeBadDays'))
      return
    }
    if (opens < 0 || closes <= opens) {
      setProblem(t('calendar.findTimeBadHours'))
      return
    }
    setIsBusy(true)
    setProblem(null)
    try {
      const dayAfterLast = new Date(last.getFullYear(), last.getMonth(), last.getDate() + 1)
      const answers = await freeBusyOf([ownUserId, ...chosen], first, dayAfterLast)
      // Somebody left out is a time that does not suit them, so a person who
      // stopped sharing since the list was made is said and nothing is offered.
      const missing = chosen.filter((userId) => !answers.some((each) => each.userId === userId && each.isShared))
      if (missing.length > 0) {
        setFound(null)
        setProblem(
          t('calendar.findTimeNotShared', {
            names: missing
              .map((userId) => people.find((person) => person.userId === userId)?.username ?? '')
              .join(', '),
          }),
        )
        return
      }
      const busyPeriods = answers.flatMap((each) => each.periods)
      setFound(freeByDay(busyPeriods, first, dayAfterLast, opens, closes))
    } catch (failure) {
      setProblem(failureText(failure, t('calendar.failed')))
    } finally {
      setIsBusy(false)
    }
  }

  const days: Array<{ day: Date; stretches: FreeStretch[] }> = []
  for (const stretch of found ?? []) {
    const last = days[days.length - 1]
    if (last && last.day.getTime() === stretch.day.getTime()) last.stretches.push(stretch)
    else days.push({ day: stretch.day, stretches: [stretch] })
  }

  return (
    <FormDialog
      title={t('calendar.findTimeTitle')}
      submitLabel={t('calendar.findTimeSubmit')}
      closeLabel={t('common.done')}
      busy={isBusy}
      error={problem}
      canSubmit={chosen.length > 0}
      onClose={onClose}
      onSubmit={() => void find()}
    >
      <p className="muted">{t('calendar.findTimeIntro')}</p>
      {sharers.data && people.length === 0 ? (
        <>
          <p className="muted">{t('calendar.findTimeNobody')}</p>
          <p>
            <Link to="/circles" onClick={onClose}>
              {t('circles.open')}
            </Link>
          </p>
        </>
      ) : (
        <>
          <div className="calendar-find-people" role="group" aria-label={t('calendar.findTimeWith')}>
            <span>{t('calendar.findTimeWith')}</span>
            {people.map((person) => (
              <label key={person.userId} className="checkbox">
                <input
                  type="checkbox"
                  checked={chosen.includes(person.userId)}
                  onChange={(event) =>
                    setChosen(
                      event.target.checked
                        ? [...chosen, person.userId]
                        : chosen.filter((userId) => userId !== person.userId),
                    )
                  }
                />
                {personLabel(person)}
              </label>
            ))}
            {sharers.data?.isAnyoneLeftOut && <span className="muted">{t('calendar.findTimeLeftOut')}</span>}
          </div>
          <div className="calendar-find-range">
            <label>
              {t('calendar.findTimeFrom')}
              <input type="date" value={from} onChange={(event) => setFrom(event.target.value)} />
            </label>
            <label>
              {t('calendar.findTimeTo')}
              <input type="date" value={to} onChange={(event) => setTo(event.target.value)} />
            </label>
            <label>
              {t('calendar.findTimeEarliest')}
              <input type="time" value={earliest} onChange={(event) => setEarliest(event.target.value)} />
            </label>
            <label>
              {t('calendar.findTimeLatest')}
              <input type="time" value={latest} onChange={(event) => setLatest(event.target.value)} />
            </label>
          </div>
        </>
      )}
      {found && days.length === 0 && <p className="muted">{t('calendar.findTimeNone')}</p>}
      {days.length > 0 && (
        <div className="calendar-shares" aria-label={t('calendar.findTimeResult')}>
          {days.map(({ day, stretches }) => (
            <div key={day.getTime()} className="calendar-free-day">
              <span className="calendar-free-day-name">{dayFormat.format(day)}</span>
              <span>
                {stretches
                  .map((stretch) => `${timeFormat.format(stretch.startsAt)} - ${timeFormat.format(stretch.endsAt)}`)
                  .join(', ')}
              </span>
            </div>
          ))}
        </div>
      )}
    </FormDialog>
  )
}
