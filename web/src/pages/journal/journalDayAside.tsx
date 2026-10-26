import { useMemo } from 'react'
import { Link } from 'react-router-dom'

import { LocationMap, MapPathPoint, MapPlace } from '../../components/locationMap'
import { Key, useTranslation } from '../../i18n/i18n'
import { workoutName } from '../health/healthApi'
import { RecordKindIcon } from './journalNarrative'
import {
  JournalDay,
  JournalHealth,
  JournalSleepStage,
  clockText,
  dayOf,
  distanceText,
  durationText,
  useIsImperial,
  wholeNumberText,
} from './journalApi'
import { JournalCard } from './journalParts'
import { isDashboardPath } from './journalPaths'

// The column down the right of a day: where the person was, everything in the
// order it happened, their Health and their calendar.

const NO_PEOPLE: never[] = []

// hasPosition says a visit knows where it was. A visit with no coordinates is
// at 0, 0, which is the sea and not a place.
const hasPosition = (visit: { latitude: number; longitude: number }) => visit.latitude !== 0 || visit.longitude !== 0

export function PlacesCard({ day }: { day: JournalDay }) {
  const { t, plural, language } = useTranslation()
  const isImperial = useIsImperial()
  const clock = (value: string | null | undefined) => clockText(value, day.timeZone, language)
  const positioned = useMemo(() => day.visits.filter(hasPosition), [day.visits])
  // One mark per place, however many visits it had, and the path through the
  // visits in the order they happened.
  const places = useMemo(() => {
    const found = new Map<string, MapPlace>()
    for (const visit of positioned) {
      const id = visit.placeId || visit.visitId
      if (!found.has(id)) {
        found.set(id, {
          id,
          placeName: visit.placeName,
          placeLabel: '',
          latitudeDegrees: visit.latitude,
          longitudeDegrees: visit.longitude,
          radiusMeters: 0,
        })
      }
    }
    return [...found.values()]
  }, [positioned])
  const path = useMemo<MapPathPoint[]>(
    () => positioned.map((visit) => ({ latitudeDegrees: visit.latitude, longitudeDegrees: visit.longitude })),
    [positioned],
  )
  if (day.visits.length === 0) return null
  const walked =
    day.facts.walkedMeters > 0
      ? `, ${t('journal.walked', { distance: distanceText(day.facts.walkedMeters, isImperial, language) })}`
      : ''
  return (
    <section className="settings-section card journal-card journal-map-card">
      {positioned.length > 0 && (
        <div className="journal-map">
          <LocationMap
            people={NO_PEOPLE}
            places={places}
            path={path}
            isCompact
            isScrollZoomEnabled={false}
            mapLabel={t('journal.mapLabel')}
          />
        </div>
      )}
      <div className="journal-map-body">
        <div className="settings-section-head">
          <div>
            <h3>{t('journal.places')}</h3>
            <p className="muted">
              {plural(day.visits.length, { one: 'journal.visitsOne', other: 'journal.visitsOther' })}
              {walked}
            </p>
          </div>
          <Link className="button-quiet" to={`/location?day=${dayOf(day.day)}`}>
            {t('journal.openInLocation')}
          </Link>
        </div>
        <ol className="journal-visits">
          {day.visits.map((visit, index) => (
            <li key={visit.visitId}>
              <span className="journal-visit-number" aria-hidden="true">
                {index + 1}
              </span>
              <Link to={visit.placeId ? `/location?place=${encodeURIComponent(visit.placeId)}` : '/location'}>
                {visit.placeName || t('journal.unnamedPlace')}
              </Link>
              <span className="muted journal-row-time">{visitTimes(visit, clock, t)}</span>
            </li>
          ))}
        </ol>
      </div>
    </section>
  )
}

function visitTimes(
  visit: JournalDay['visits'][number],
  clock: (value: string | null | undefined) => string,
  t: ReturnType<typeof useTranslation>['t'],
): string {
  const arrived = clock(visit.arrivedAt)
  const departed = clock(visit.departedAt)
  if (arrived && departed) return t('journal.timeRange', { start: arrived, end: departed })
  if (arrived) return t('journal.visitFrom', { start: arrived })
  if (departed) return t('journal.visitUntil', { end: departed })
  return ''
}

// MomentsCard is everything the day linked to, in the order it happened.
export function MomentsCard({ day }: { day: JournalDay }) {
  const { t, language } = useTranslation()
  if (day.moments.length === 0) return null
  return (
    <JournalCard title={t('journal.throughTheDay')} description={t('journal.throughTheDayHint')}>
      <ol className="journal-moments">
        {day.moments.map((moment, index) => {
          const content = (
            <>
              <span className="journal-moment-time muted">{clockText(moment.at, day.timeZone, language)}</span>
              <span className="journal-moment-mark">
                <span className="journal-moment-icon">
                  <RecordKindIcon recordKind={moment.recordKind} size={14} />
                </span>
                <span className="journal-moment-line" aria-hidden="true" />
              </span>
              <span className="journal-moment-text">
                <span className="journal-moment-title">{moment.title}</span>
                {moment.detail && <span className="muted journal-moment-detail">{moment.detail}</span>}
              </span>
            </>
          )
          return (
            <li key={`${moment.recordKind}:${moment.recordId}:${index}`}>
              {isDashboardPath(moment.dashboardPath) ? (
                <Link className="journal-moment" to={moment.dashboardPath}>
                  {content}
                </Link>
              ) : (
                <div className="journal-moment">{content}</div>
              )}
            </li>
          )
        })}
      </ol>
    </JournalCard>
  )
}

const SLEEP_STAGE_NAMES: Record<string, Key> = {
  deep: 'journal.sleepDeep',
  core: 'journal.sleepCore',
  rem: 'journal.sleepRem',
  awake: 'journal.sleepAwake',
  inBed: 'journal.sleepInBed',
}

// StepBars are the steps of each hour as small bars, most at the tallest.
export function StepBars({ stepsByHour, label }: { stepsByHour: number[]; label: string }) {
  const { language } = useTranslation()
  const most = Math.max(1, ...stepsByHour)
  const hourLabel = (hour: number) =>
    new Intl.DateTimeFormat(language, { hour: 'numeric' }).format(new Date(2000, 0, 1, hour % 24))
  return (
    <>
      <svg className="journal-bars" viewBox="0 0 240 64" preserveAspectRatio="none" role="img" aria-label={label}>
        {stepsByHour.map((count, hour) => {
          const height = count > 0 ? Math.max(2, Math.round((count / most) * 60)) : 1
          return <rect key={hour} x={hour * 10 + 1} y={62 - height} width={7} height={height} rx={1.5} />
        })}
      </svg>
      <div className="journal-bars-axis muted" aria-hidden="true">
        {[0, 6, 12, 18, 24].map((hour) => (
          <span key={hour}>{hourLabel(hour)}</span>
        ))}
      </div>
    </>
  )
}

// SleepBar is the night's stages one after the other, each as long as it
// lasted, in the colors the Health page gives them.
function SleepBar({ stages, label }: { stages: JournalSleepStage[]; label: string }) {
  const { t } = useTranslation()
  const totals = new Map<string, number>()
  for (const stage of stages) totals.set(stage.sleepStage, (totals.get(stage.sleepStage) ?? 0) + stage.minutes)
  return (
    <>
      <div className="journal-sleep" role="img" aria-label={label}>
        {stages
          .filter((stage) => stage.minutes > 0)
          .map((stage, index) => (
            <span
              key={index}
              className={`journal-sleep-stage ${stage.sleepStage}`}
              style={{ flexGrow: stage.minutes }}
            />
          ))}
      </div>
      <div className="journal-sleep-legend muted">
        {[...totals.entries()].map(([sleepStage, minutes]) => (
          <span key={sleepStage}>
            <span className={`journal-sleep-swatch ${sleepStage}`} aria-hidden="true" />
            {SLEEP_STAGE_NAMES[sleepStage] ? t(SLEEP_STAGE_NAMES[sleepStage]) : sleepStage} {durationText(minutes, t)}
          </span>
        ))}
      </div>
    </>
  )
}

function hasHealth(health: JournalHealth | null): health is JournalHealth {
  return (
    health !== null &&
    (health.stepCount > 0 || health.sleepMinutes > 0 || health.workouts.length > 0 || health.restingHeartRate > 0)
  )
}

export function HealthCard({ day }: { day: JournalDay }) {
  const { t, language } = useTranslation()
  const isImperial = useIsImperial()
  const health = day.health
  if (!hasHealth(health)) return null
  const stepsByHour = health.stepsByHour.length === 24 ? health.stepsByHour : []
  const mostHour = stepsByHour.indexOf(Math.max(...stepsByHour))
  return (
    <JournalCard
      title={t('journal.health')}
      action={
        <Link className="button-quiet" to="/health">
          {t('journal.openInHealth')}
        </Link>
      }
    >
      {health.stepCount > 0 && (
        <div className="journal-health-block">
          <div className="journal-health-label muted">
            {t('journal.stepsByHour', { count: wholeNumberText(health.stepCount, language) })}
          </div>
          {stepsByHour.length > 0 && stepsByHour.some((count) => count > 0) && (
            <StepBars
              stepsByHour={stepsByHour}
              label={t('journal.stepsByHourMost', {
                time: new Intl.DateTimeFormat(language, { hour: 'numeric' }).format(new Date(2000, 0, 1, mostHour)),
              })}
            />
          )}
        </div>
      )}
      {health.sleepMinutes > 0 && (
        <div className="journal-health-block">
          <div className="journal-health-label muted">
            {t('journal.sleepNight', { duration: durationText(health.sleepMinutes, t) })}
          </div>
          {health.sleepStages.length > 0 && (
            <SleepBar stages={health.sleepStages} label={t('journal.sleepStagesLabel')} />
          )}
        </div>
      )}
      {(health.workouts.length > 0 || health.restingHeartRate > 0) && (
        <div className="journal-health-facts">
          {health.workouts.map((workout) => (
            <div key={workout.sampleId}>
              <span>
                {workoutName(t, workout.activityType)}, {durationText(workout.minutes, t)}
              </span>
              {workout.meters > 0 && (
                <span className="muted">{distanceText(workout.meters, isImperial, language)}</span>
              )}
            </div>
          ))}
          {health.restingHeartRate > 0 && (
            <div>
              <span>{t('journal.restingHeartRate')}</span>
              <span className="muted">
                {t('journal.beatsPerMinute', { count: Math.round(health.restingHeartRate) })}
              </span>
            </div>
          )}
        </div>
      )}
    </JournalCard>
  )
}

export function CalendarCard({ day }: { day: JournalDay }) {
  const { t, language } = useTranslation()
  const clock = (value: string | null | undefined) => clockText(value, day.timeZone, language)
  if (day.events.length === 0 && day.reminders.length === 0) return null
  return (
    <JournalCard
      title={t('journal.calendar')}
      action={
        <Link className="button-quiet" to={`/calendar?view=day&on=${dayOf(day.day)}`}>
          {t('journal.openTheDay')}
        </Link>
      }
      isFlush
    >
      {day.events.map((event) => (
        <Link
          key={`${event.eventId}:${event.startsAt}`}
          className="journal-event"
          to={`/calendar?event=${encodeURIComponent(event.eventId)}`}
        >
          <span className="journal-event-bar" aria-hidden="true" />
          <span className="journal-row-body">
            <span>{event.title}</span>
            {(event.location || event.calendarName) && (
              <span className="muted journal-row-detail">{event.location || event.calendarName}</span>
            )}
          </span>
          <span className="muted journal-row-time">{event.isAllDay ? t('journal.allDay') : clock(event.startsAt)}</span>
        </Link>
      ))}
      {day.reminders.map((reminder) => (
        <Link
          key={reminder.reminderId}
          className="journal-event"
          to={`/reminders?reminder=${encodeURIComponent(reminder.reminderId)}`}
        >
          <span className="journal-event-bar" aria-hidden="true" />
          <span className="journal-row-body">
            <span>{reminder.title}</span>
            <span className="muted journal-row-detail">
              {reminder.doneAt ? t('journal.reminderDone') : t('journal.reminder')}
            </span>
          </span>
          <span className="muted journal-row-time">{clock(reminder.doneAt ?? reminder.dueAt)}</span>
        </Link>
      ))}
    </JournalCard>
  )
}
