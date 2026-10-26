import { useCallback } from 'react'
import { Link, Navigate, useParams } from 'react-router-dom'

import { graphql } from '../../api'
import { useBreadcrumbDetail, useBreadcrumbNote } from '../../components/breadcrumb'
import { ErrorMessage, Loading } from '../../components/common'
import { SettingsEmpty, SettingsSection } from '../../components/settingsList'
import { useQuery } from '../../components/useQuery'
import { Key, useTranslation } from '../../i18n/i18n'
import { photoSmallPreviewPath } from '../../photos/api'
import { hasPermission, useSession } from '../../session'
import {
  JournalDaySummary,
  JournalPeriod,
  MOOD_LEVELS,
  MOOD_NAMES,
  PeriodKind,
  READ_JOURNAL_PERIOD,
  addDays,
  amountText,
  dayOf,
  dayPath,
  dayShortText,
  durationText,
  isDayText,
  isMonthText,
  isYearText,
  monthHeadingText,
  monthLength,
  monthNameText,
  monthOf,
  monthShortText,
  moodLevelOf,
  parseDay,
  periodPath,
  temperatureText,
  usesImperialUnits,
  weekdayIndex,
  weekdayNames,
  weekdayShortText,
  wholeNumberText,
  yearOf,
} from './journalApi'
import { NarrativeCard, RecordKindIcon } from './journalNarrative'
import {
  JournalCard,
  MoodDot,
  MoodFace,
  PeriodToolbar,
  browserToday,
  monthStepPaths,
  usePendingPoll,
  weekStartsOnOf,
  weekStepPaths,
  yearStepPaths,
} from './journalParts'
import { JournalWordsCard } from './journalWords'
import { WeatherIcon } from '../../components/icons'

// A week, a month and a year: each with what TeaNode wrote about it, the
// person's own looking back, and the days (or months) it holds. The pages
// share the way they are read, their toolbar and the narrative and looking
// back cards; what they show of the days is each its own.

type Heading = {
  // The page's name, and the larger period it belongs to.
  detail: string
  parent: string | null
  note: string
  // The date the other sizes of period are opened around.
  referenceDay: string
}

function PeriodShell({
  periodKind,
  anchor,
  describe,
  children,
}: {
  periodKind: PeriodKind
  // Any day in the period.
  anchor: string
  describe: (period: JournalPeriod | null) => Heading
  children: (period: JournalPeriod, reload: () => Promise<void>) => React.ReactNode
}) {
  const { t } = useTranslation()
  const query = useQuery(
    async () =>
      (await graphql<{ ReadJournalPeriod: JournalPeriod }>(READ_JOURNAL_PERIOD, { periodKind, day: anchor }))
        .ReadJournalPeriod,
    [periodKind, anchor],
    { refresh: false },
  )
  const reload = useCallback(async () => {
    await query.reload(true)
  }, [query])
  const period = query.data
  usePendingPoll(Boolean(period?.lookingBack.touchUpPending || period?.narrative.narrativePending), reload)

  const heading = describe(period)
  useBreadcrumbDetail(heading.detail, heading.parent)
  useBreadcrumbNote(heading.note)

  const today = browserToday()
  const start = period?.periodStart ?? anchor
  const steps =
    periodKind === 'week'
      ? weekStepPaths(start, today)
      : periodKind === 'month'
        ? monthStepPaths(monthOf(start), today)
        : yearStepPaths(yearOf(start), today)
  const labels: Record<PeriodKind, { current: Key; before: Key; after: Key }> = {
    week: { current: 'journal.thisWeek', before: 'journal.weekBefore', after: 'journal.weekAfter' },
    month: { current: 'journal.thisMonth', before: 'journal.monthBefore', after: 'journal.monthAfter' },
    year: { current: 'journal.thisYear', before: 'journal.yearBefore', after: 'journal.yearAfter' },
  }
  return (
    <>
      <PeriodToolbar
        periodKind={periodKind}
        referenceDay={heading.referenceDay}
        {...steps}
        currentLabel={t(labels[periodKind].current)}
        previousLabel={t(labels[periodKind].before)}
        nextLabel={t(labels[periodKind].after)}
      />
      <ErrorMessage error={query.error} />
      {query.error !== null && !period && !query.loading && (
        <div className="page-actions">
          <button type="button" onClick={() => void query.reload()}>
            {t('common.tryAgain')}
          </button>
        </div>
      )}
      {!period && query.error === null && <Loading />}
      {period && children(period, reload)}
    </>
  )
}

function useGate(): boolean {
  const session = useSession()
  return hasPermission(session.permissions, 'journal:use')
}

function NotAllowed() {
  const { t } = useTranslation()
  return (
    <SettingsSection card title={t('journal.title')}>
      <SettingsEmpty>{t('journal.off')}</SettingsEmpty>
    </SettingsSection>
  )
}

// The two cards every period page has: the story, and the person's own words.
function StoryAndLookingBack({
  period,
  reload,
  narrativeTitle,
  writeLabel,
  emptyWriteLabel,
}: {
  period: JournalPeriod
  reload: () => Promise<void>
  narrativeTitle: string
  writeLabel: string
  emptyWriteLabel: string
}) {
  const { t } = useTranslation()
  const target = { day: period.periodStart, periodKind: period.periodKind }
  return (
    <>
      <NarrativeCard
        narrative={period.narrative}
        target={target}
        title={narrativeTitle}
        description={
          period.narrative.sourceCount > 0
            ? t('journal.narrativeFromThings', { count: period.narrative.sourceCount })
            : undefined
        }
        writeLabel={writeLabel}
        onChanged={reload}
      />
      <JournalWordsCard
        words={period.lookingBack}
        target={target}
        title={t('journal.lookingBack')}
        onChanged={reload}
        emptyWriteLabel={emptyWriteLabel}
      />
    </>
  )
}

// small bar charts

// MiniBars are a value for each slot as a bar, the tallest the full height.
// A slot with nothing is a hairline, so a missing day reads as nothing and
// not as a gap in the chart.
function MiniBars({ values, labels, description }: { values: number[]; labels: string[]; description: string }) {
  const most = Math.max(...values, 0)
  const slotWidth = 140 / values.length
  return (
    <>
      <svg
        className="journal-bars journal-bars-small"
        viewBox="0 0 140 56"
        preserveAspectRatio="none"
        role="img"
        aria-label={description}
      >
        {values.map((value, index) => {
          const height = value > 0 && most > 0 ? Math.max(2, Math.round((value / most) * 52)) : 1
          return (
            <rect
              key={index}
              x={index * slotWidth + slotWidth * 0.15}
              y={54 - height}
              width={slotWidth * 0.7}
              height={height}
              rx={2}
            />
          )
        })}
      </svg>
      <div
        className="journal-bars-labels muted"
        aria-hidden="true"
        style={{ gridTemplateColumns: `repeat(${labels.length}, minmax(0, 1fr))` }}
      >
        {labels.map((label, index) => (
          <span key={index}>{label}</span>
        ))}
      </div>
    </>
  )
}

// week

export function JournalWeekPage() {
  const { day } = useParams()
  const isAllowed = useGate()
  if (!isDayText(day)) return <Navigate to="/journal" replace />
  if (!isAllowed) return <NotAllowed />
  return <WeekContent key={day} anchor={day} />
}

function WeekContent({ anchor }: { anchor: string }) {
  const { t, language } = useTranslation()
  const today = browserToday()
  return (
    <PeriodShell
      periodKind="week"
      anchor={anchor}
      describe={(period) => {
        const start = period?.periodStart ?? anchor
        return {
          detail: t('journal.weekOf', { date: dayShortText(start, language) }),
          parent: monthHeadingText(monthOf(start), language),
          note: period
            ? t('journal.rangeText', {
                from: dayShortText(period.periodStart, language),
                to: `${dayShortText(period.periodEnd, language)}, ${yearOf(period.periodEnd)}`,
              })
            : '',
          referenceDay: period?.isCurrent ? today : start,
        }
      }}
    >
      {(period, reload) => (
        <>
          <div className="journal-split">
            <div className="journal-split-main">
              <StoryAndLookingBackNarrative period={period} reload={reload} />
            </div>
            <div className="journal-split-side">
              <WeekLookingBack period={period} reload={reload} />
            </div>
          </div>
          <WeekDays period={period} />
          <WeekNumbers period={period} />
          <div className="journal-split">
            <div className="journal-split-main">
              <WeekMoments period={period} />
            </div>
            <div className="journal-split-side">
              <WeekPlaces period={period} />
            </div>
          </div>
        </>
      )}
    </PeriodShell>
  )
}

// The week's story and its looking back sit side by side, so they are two
// cards here and not the pair the month and year stack.
function StoryAndLookingBackNarrative({ period, reload }: { period: JournalPeriod; reload: () => Promise<void> }) {
  const { t } = useTranslation()
  return (
    <NarrativeCard
      narrative={period.narrative}
      target={{ day: period.periodStart, periodKind: 'week' }}
      title={t('journal.weekAsSeen')}
      description={
        period.narrative.sourceCount > 0
          ? t('journal.narrativeFromThings', { count: period.narrative.sourceCount })
          : undefined
      }
      writeLabel={t('journal.writeThisWeek')}
      onChanged={reload}
    />
  )
}

function WeekLookingBack({ period, reload }: { period: JournalPeriod; reload: () => Promise<void> }) {
  const { t } = useTranslation()
  return (
    <JournalWordsCard
      words={period.lookingBack}
      target={{ day: period.periodStart, periodKind: 'week' }}
      title={t('journal.lookingBack')}
      onChanged={reload}
      emptyWriteLabel={t('journal.writeAboutWeek')}
    />
  )
}

// The seven days side by side, each opening its page. A day with nothing yet
// is still a column, so the week keeps its shape.
function WeekDays({ period }: { period: JournalPeriod }) {
  const { t, language } = useTranslation()
  const isImperial = usesImperialUnits()
  const today = browserToday()
  const byDay = new Map(period.days.map((summary) => [summary.day, summary]))
  return (
    <section className="journal-week-days" aria-label={t('journal.sevenDays')}>
      {Array.from({ length: 7 }, (_, index) => {
        const day = addDays(period.periodStart, index)
        const summary = byDay.get(day)
        const level = moodLevelOf(summary?.mood)
        const isFuture = day > today
        const content = (
          <>
            <span className="journal-day-column-head">
              <span>
                <span className="journal-date-weekday muted">{weekdayShortText(day, language)}</span>
                <span className="journal-day-column-number">{Number(day.slice(8, 10))}</span>
              </span>
              {!isFuture && (
                <span
                  role="img"
                  aria-label={
                    level > 0 ? t('journal.moodOfDay', { mood: t(MOOD_NAMES[level]) }) : t('journal.noMoodYet')
                  }
                >
                  <MoodFace level={level} size={22} isFilled={level > 0} />
                </span>
              )}
            </span>
            {summary?.coverPhotoId && (
              <img
                className="journal-day-column-photo"
                src={photoSmallPreviewPath(summary.coverPhotoId)}
                alt=""
                loading="lazy"
                draggable={false}
              />
            )}
            {summary?.weather && (
              <span className="journal-day-column-weather muted">
                <WeatherIcon kind={summary.weather.weatherKind} size={14} />
                {temperatureText(summary.weather.highCelsius, isImperial)} /{' '}
                {temperatureText(summary.weather.lowCelsius, isImperial)}
              </span>
            )}
            {summary?.headline && <span className="journal-prose journal-day-column-headline">{summary.headline}</span>}
          </>
        )
        return isFuture ? (
          <div key={day} className="journal-day-column is-future" aria-hidden="true">
            {content}
          </div>
        ) : (
          <Link key={day} className="journal-day-column" to={dayPath(day)}>
            {content}
          </Link>
        )
      })}
    </section>
  )
}

// WeekNumbers are four small charts of the week, a bar for each day.
function WeekNumbers({ period }: { period: JournalPeriod }) {
  const { t, plural, language } = useTranslation()
  const byDay = new Map(period.days.map((summary) => [summary.day, summary]))
  const days: (JournalDaySummary | undefined)[] = Array.from({ length: 7 }, (_, index) =>
    byDay.get(addDays(period.periodStart, index)),
  )
  // The names of the seven days in the order this week runs.
  const letters = Array.from({ length: 7 }, (_, index) =>
    new Intl.DateTimeFormat(language, { weekday: 'narrow' }).format(parseDay(addDays(period.periodStart, index))),
  )
  const longDay = (index: number) =>
    new Intl.DateTimeFormat(language, { weekday: 'long' }).format(parseDay(addDays(period.periodStart, index)))
  const totals = period.totals

  const sleep = days.map((day) => day?.facts.sleepMinutes ?? 0)
  const nights = sleep.filter((minutes) => minutes > 0)
  const steps = days.map((day) => day?.facts.stepCount ?? 0)
  const work = days.map((day) => day?.facts.codingMinutes ?? 0)
  const spent = days.map((day) => day?.facts.spentMinorUnits ?? 0)

  const indexOfMost = (values: number[]) => values.indexOf(Math.max(...values))
  const indexOfLeastNight = () => {
    const least = Math.min(...nights)
    return sleep.indexOf(least)
  }

  const charts: {
    id: string
    label: string
    value: string
    unit: string
    detail: string
    description: string
    values: number[]
    isShown: boolean
  }[] = [
    {
      id: 'sleep',
      label: t('journal.tileSleep'),
      value:
        nights.length > 0 ? durationText(nights.reduce((sum, minutes) => sum + minutes, 0) / nights.length, t) : '',
      unit: t('journal.aNight'),
      detail:
        nights.length > 1
          ? t('journal.shortestOn', {
              weekday: longDay(indexOfLeastNight()),
              value: durationText(Math.min(...nights), t),
            })
          : '',
      description: t('journal.sleepEachNight'),
      values: sleep,
      isShown: nights.length > 0,
    },
    {
      id: 'steps',
      label: t('journal.tileSteps'),
      value: wholeNumberText(totals.stepCount, language),
      unit: t('journal.inAll'),
      detail:
        totals.stepCount > 0
          ? t('journal.mostOn', {
              weekday: longDay(indexOfMost(steps)),
              value: wholeNumberText(Math.max(...steps), language),
            })
          : '',
      description: t('journal.stepsEachDay'),
      values: steps,
      isShown: totals.stepCount > 0,
    },
    {
      id: 'work',
      label: t('journal.tileWork'),
      value: durationText(totals.codingMinutes, t),
      unit: plural(totals.codingSessionCount, { one: 'journal.sessionsOne', other: 'journal.sessionsOther' }),
      detail:
        totals.codingMinutes > 0
          ? t('journal.mostOn', { weekday: longDay(indexOfMost(work)), value: durationText(Math.max(...work), t) })
          : '',
      description: t('journal.workEachDay'),
      values: work,
      isShown: totals.codingMinutes > 0,
    },
    {
      id: 'spent',
      label: t('journal.tileSpent'),
      value: amountText(totals.spentMinorUnits, totals.spentCurrency, language),
      unit: plural(totals.transactionCount, { one: 'journal.transactionsOne', other: 'journal.transactionsOther' }),
      detail:
        totals.spentMinorUnits > 0
          ? t('journal.mostOn', {
              weekday: longDay(indexOfMost(spent)),
              value: amountText(Math.max(...spent), totals.spentCurrency, language),
            })
          : '',
      description: t('journal.spentEachDay'),
      values: spent,
      isShown: totals.spentMinorUnits > 0,
    },
  ]
  const shown = charts.filter((chart) => chart.isShown)
  if (shown.length === 0) return null
  return (
    <section className="journal-week-numbers" aria-label={t('journal.weekNumbers')}>
      {shown.map((chart) => (
        <div key={chart.id} className={`card journal-chart-card journal-chart-${chart.id}`}>
          <div className="tile-label">{chart.label}</div>
          <div className="journal-chart-value">
            {chart.value} <span className="muted">{chart.unit}</span>
          </div>
          <MiniBars values={chart.values} labels={letters} description={chart.description} />
          {chart.detail && <div className="muted journal-chart-detail">{chart.detail}</div>}
        </div>
      ))}
    </section>
  )
}

// WeekMoments are what the agent chose to remember of the week, each opening
// the thing itself.
function WeekMoments({ period }: { period: JournalPeriod }) {
  const { t, language } = useTranslation()
  const choices = period.choices.filter((choice) => !choice.isHidden)
  if (choices.length === 0) return null
  return (
    <JournalCard title={t('journal.weekMoments')} description={t('journal.weekMomentsHint')} isFlush>
      {choices.map((choice) => {
        const record = choice.record
        const happened = record.happenedAt ? new Date(record.happenedAt) : null
        return (
          <div className="journal-row" key={choice.linkId}>
            <span className="journal-row-time muted journal-moment-day">
              {happened ? new Intl.DateTimeFormat(language, { weekday: 'short' }).format(happened) : ''}
            </span>
            <span className="journal-row-icon">
              <RecordKindIcon recordKind={record.recordKind} size={16} />
            </span>
            <span className="journal-row-body">
              <span className="journal-row-title">
                {record.isUnavailable || !record.dashboardPath.startsWith('/') ? (
                  <span>{record.title || t('journal.pinGone')}</span>
                ) : (
                  <Link to={record.dashboardPath}>{record.title}</Link>
                )}
              </span>
              {choice.linkReason && <span className="muted journal-row-detail">{choice.linkReason}</span>}
            </span>
          </div>
        )
      })}
    </JournalCard>
  )
}

// The places the week's days were mostly spent at, once each.
function WeekPlaces({ period }: { period: JournalPeriod }) {
  const { t, plural } = useTranslation()
  const counts = new Map<string, number>()
  for (const day of period.days) {
    if (day.facts.mainPlaceName) counts.set(day.facts.mainPlaceName, (counts.get(day.facts.mainPlaceName) ?? 0) + 1)
  }
  if (counts.size === 0) return null
  return (
    <JournalCard
      title={t('journal.places')}
      description={plural(period.totals.placeCount, { one: 'journal.placesOne', other: 'journal.placesOther' })}
    >
      <div className="journal-chips">
        {[...counts.entries()].map(([place, count]) => (
          <Link key={place} className="journal-chip" to="/location">
            {count > 1 ? t('journal.placeDays', { place, count }) : place}
          </Link>
        ))}
      </div>
    </JournalCard>
  )
}

// month

export function JournalMonthPage() {
  const { month } = useParams()
  const isAllowed = useGate()
  if (!isMonthText(month)) return <Navigate to="/journal" replace />
  if (!isAllowed) return <NotAllowed />
  return <MonthContent key={month} month={month} />
}

function MonthContent({ month }: { month: string }) {
  const { t, language } = useTranslation()
  const today = browserToday()
  return (
    <PeriodShell
      periodKind="month"
      anchor={`${month}-01`}
      describe={(period) => ({
        detail: monthHeadingText(month, language),
        parent: yearOf(month),
        note: '',
        referenceDay: period?.isCurrent ? today : `${month}-01`,
      })}
    >
      {(period, reload) => (
        <div className="journal-columns">
          <section
            className="journal-main"
            aria-label={t('journal.daysOfMonth', { month: monthNameText(month, language) })}
          >
            <MonthGrid month={month} period={period} />
          </section>
          <aside className="journal-aside">
            <StoryAndLookingBack
              period={period}
              reload={reload}
              narrativeTitle={t('journal.monthAsSeen')}
              writeLabel={t('journal.writeThisMonth')}
              emptyWriteLabel={t('journal.writeAboutMonth', { month: monthNameText(month, language) })}
            />
            <MonthMoods period={period} />
            <Chapters period={period} />
          </aside>
        </div>
      )}
    </PeriodShell>
  )
}

function MonthGrid({ month, period }: { month: string; period: JournalPeriod }) {
  const { t, language } = useTranslation()
  const isImperial = usesImperialUnits()
  const today = browserToday()
  const weekStartsOn = weekStartsOnOf(null, language)
  const byDay = new Map(period.days.map((summary) => [summary.day, summary]))
  const length = monthLength(month)
  const leading = weekdayIndex(`${month}-01`, weekStartsOn)
  const trailing = (7 - ((leading + length) % 7)) % 7
  return (
    <>
      <div className="journal-month-table">
        <div className="journal-month-head muted">
          {weekdayNames(weekStartsOn, language, 'short').map((name, index) => (
            <div key={index}>{name}</div>
          ))}
        </div>
        <div className="journal-month-cells">
          {Array.from({ length: leading }, (_, index) => (
            <div key={`before-${index}`} className="journal-month-cell is-outside" />
          ))}
          {Array.from({ length }, (_, index) => {
            const day = `${month}-${String(index + 1).padStart(2, '0')}`
            const summary = byDay.get(day)
            const level = moodLevelOf(summary?.mood)
            const isFuture = day > today
            const label = dayShortText(day, language)
            const content = (
              <>
                {summary?.coverPhotoId && (
                  <img
                    className="journal-month-photo"
                    src={photoSmallPreviewPath(summary.coverPhotoId)}
                    alt=""
                    loading="lazy"
                    draggable={false}
                  />
                )}
                <span className="journal-month-cell-head">
                  <span className={day === today ? 'journal-month-number is-today' : 'journal-month-number'}>
                    {index + 1}
                  </span>
                  <span className="journal-month-cell-marks muted">
                    {summary?.weather && (
                      <span className="journal-month-high">
                        {temperatureText(summary.weather.highCelsius, isImperial)}
                      </span>
                    )}
                    {level > 0 && <MoodDot level={level} />}
                  </span>
                </span>
                {summary?.headline && <span className="muted journal-month-headline">{summary.headline}</span>}
              </>
            )
            return isFuture ? (
              <div key={day} className="journal-month-cell is-future" aria-hidden="true">
                {content}
              </div>
            ) : (
              <Link
                key={day}
                className="journal-month-cell"
                to={dayPath(day)}
                aria-label={level > 0 ? `${label}: ${t(MOOD_NAMES[level])}` : label}
              >
                {content}
              </Link>
            )
          })}
          {Array.from({ length: trailing }, (_, index) => (
            <div key={`after-${index}`} className="journal-month-cell is-outside" />
          ))}
        </div>
      </div>
      <div className="journal-legend muted">
        <span>{t('journal.monthLegend')}</span>
        {MOOD_LEVELS.map((level) => (
          <span key={level} className="journal-legend-item">
            <MoodDot level={level} />
            {t(MOOD_NAMES[level])}
          </span>
        ))}
      </div>
    </>
  )
}

// MonthMoods is how the month's days were, as one bar, with its numbers
// under it.
function MonthMoods({ period }: { period: JournalPeriod }) {
  const { t, plural, language } = useTranslation()
  const isImperial = usesImperialUnits()
  const counts = MOOD_LEVELS.map((level) => period.moodCounts[level - 1] ?? 0)
  const total = counts.reduce((sum, count) => sum + count, 0)
  const totals = period.totals
  const nights = period.days.filter((day) => day.facts.sleepMinutes > 0)
  const stepDays = period.days.filter((day) => day.facts.stepCount > 0)
  const rows: { label: Key; value: string }[] = []
  if (nights.length > 0) {
    rows.push({
      label: 'journal.tileSleep',
      value: t('journal.sleepAverage', { duration: durationText(totals.sleepMinutes / nights.length, t) }),
    })
  }
  if (stepDays.length > 0) {
    rows.push({
      label: 'journal.tileSteps',
      value: t('journal.stepsAverage', { count: wholeNumberText(totals.stepCount / stepDays.length, language) }),
    })
  }
  if (totals.codingSessionCount > 0) {
    rows.push({
      label: 'journal.tileWork',
      value: `${durationText(totals.codingMinutes, t)}, ${plural(totals.codingSessionCount, { one: 'journal.sessionsOne', other: 'journal.sessionsOther' })}`,
    })
  }
  if (totals.photoCount > 0)
    rows.push({ label: 'journal.tilePhotos', value: wholeNumberText(totals.photoCount, language) })
  if (totals.spentMinorUnits > 0) {
    rows.push({ label: 'journal.tileSpent', value: amountText(totals.spentMinorUnits, totals.spentCurrency, language) })
  }
  const rainy = period.days.filter((day) =>
    day.weather ? ['drizzle', 'rain', 'showers', 'thunderstorm'].includes(day.weather.weatherKind) : false,
  ).length
  const warmest = period.days.reduce<JournalDaySummary | null>(
    (best, day) => (day.weather && (!best?.weather || day.weather.highCelsius > best.weather.highCelsius) ? day : best),
    null,
  )
  if (warmest?.weather) {
    rows.push({
      label: 'journal.weather',
      value: t('journal.weatherSummary', {
        rain: plural(rainy, { one: 'journal.rainDaysOne', other: 'journal.rainDaysOther' }),
        date: dayShortText(warmest.day, language),
        temperature: temperatureText(warmest.weather.highCelsius, isImperial),
      }),
    })
  }
  if (total === 0 && rows.length === 0) return null
  return (
    <JournalCard title={t('journal.moodMonth')}>
      {total > 0 && (
        <>
          <div
            className="journal-mood-bar"
            role="img"
            aria-label={MOOD_LEVELS.filter((level) => counts[level - 1] > 0)
              .map((level) => `${counts[level - 1]} ${t(MOOD_NAMES[level]).toLowerCase()}`)
              .join(', ')}
          >
            {MOOD_LEVELS.map((level) =>
              counts[level - 1] > 0 ? (
                <span key={level} data-level={level} style={{ flexGrow: counts[level - 1] }} />
              ) : null,
            )}
          </div>
          <div className="journal-mood-bar-labels muted" aria-hidden="true">
            {MOOD_LEVELS.filter((level) => counts[level - 1] > 0).map((level) => (
              <span key={level}>
                {counts[level - 1]} {t(MOOD_NAMES[level]).toLowerCase()}
              </span>
            ))}
          </div>
        </>
      )}
      {rows.length > 0 && (
        <dl className="journal-numbers">
          {rows.map((row) => (
            <div key={row.label}>
              <dt className="muted">{t(row.label)}</dt>
              <dd>{row.value}</dd>
            </div>
          ))}
        </dl>
      )}
    </JournalCard>
  )
}

// The stretches of a month or a year that belong together. A year also draws
// them along a line from January to December, each as long as it lasted.
function Chapters({ period, year }: { period: JournalPeriod; year?: string }) {
  const { t, language } = useTranslation()
  if (period.chapters.length === 0) return null
  const first = parseDay(`${year}-01-01`)
  const length = year ? (parseDay(`${Number(year) + 1}-01-01`).getTime() - first.getTime()) / 86_400_000 : 0
  // Where a day falls along the line, as a share of the year.
  const share = (day: string, extraDays: number) =>
    Math.min(1, Math.max(0, ((parseDay(dayOf(day)).getTime() - first.getTime()) / 86_400_000 + extraDays) / length))
  return (
    <JournalCard title={t('journal.chapters')} description={t('journal.chaptersHint')} isFlush>
      {year && (
        <>
          <div className="journal-chapter-strip" role="img" aria-label={t('journal.chaptersLine', { year })}>
            {period.chapters.map((chapter, index) => (
              <span
                key={`${chapter.fromDay}-${index}`}
                data-index={index % 4}
                style={{
                  left: `${share(chapter.fromDay, 0) * 100}%`,
                  width: `${(share(chapter.untilDay, 1) - share(chapter.fromDay, 0)) * 100}%`,
                }}
              />
            ))}
          </div>
          <div className="journal-chapter-months muted" aria-hidden="true">
            {Array.from({ length: 12 }, (_, index) => (
              <span key={index}>{monthShortText(`${year}-${String(index + 1).padStart(2, '0')}`, language)}</span>
            ))}
          </div>
        </>
      )}
      {period.chapters.map((chapter, index) => (
        <Link key={`${chapter.fromDay}-${index}`} className="journal-chapter" to={dayPath(dayOf(chapter.fromDay))}>
          <span className="journal-chapter-swatch" data-index={index % 4} aria-hidden="true" />
          <span className="journal-row-body">
            <span className="journal-row-title">
              <strong>{chapter.title}</strong>
              <span className="muted">
                {t('journal.rangeText', {
                  from: dayShortText(chapter.fromDay, language),
                  to: dayShortText(chapter.untilDay, language),
                })}
              </span>
            </span>
            {chapter.detail && <span className="muted journal-row-detail">{chapter.detail}</span>}
          </span>
        </Link>
      ))}
    </JournalCard>
  )
}

// year

export function JournalYearPage() {
  const { year } = useParams()
  const isAllowed = useGate()
  if (!isYearText(year)) return <Navigate to="/journal" replace />
  if (!isAllowed) return <NotAllowed />
  return <YearContent key={year} year={year} />
}

function YearContent({ year }: { year: string }) {
  const { t, plural } = useTranslation()
  const today = browserToday()
  return (
    <PeriodShell
      periodKind="year"
      anchor={`${year}-01-01`}
      describe={(period) => ({
        detail: year,
        parent: null,
        note: period
          ? plural(period.dayCount, { one: 'journal.daysWrittenOne', other: 'journal.daysWrittenOther' })
          : '',
        referenceDay: period?.isCurrent ? today : `${year}-01-01`,
      })}
    >
      {(period, reload) => (
        <>
          <div className="journal-split">
            <div className="journal-split-main">
              <NarrativeCard
                narrative={period.narrative}
                target={{ day: period.periodStart, periodKind: 'year' }}
                title={t('journal.yearAsSeen')}
                description={
                  period.narrative.sourceCount > 0
                    ? t('journal.narrativeFromThings', { count: period.narrative.sourceCount })
                    : undefined
                }
                writeLabel={t('journal.writeThisYear')}
                onChanged={reload}
              />
            </div>
            <div className="journal-split-side">
              <YearNumbers period={period} />
            </div>
          </div>
          <YearMonths year={year} period={period} />
          <Chapters period={period} year={year} />
          <JournalWordsCard
            words={period.lookingBack}
            target={{ day: period.periodStart, periodKind: 'year' }}
            title={t('journal.lookingBack')}
            onChanged={reload}
            emptyWriteLabel={t('journal.writeAboutYear', { year })}
          />
        </>
      )}
    </PeriodShell>
  )
}

function YearNumbers({ period }: { period: JournalPeriod }) {
  const { t, plural, language } = useTranslation()
  const totals = period.totals
  const rows: { label: Key; value: string }[] = []
  if (totals.placeCount > 0)
    rows.push({ label: 'journal.tilePlaces', value: wholeNumberText(totals.placeCount, language) })
  if (totals.photoCount > 0)
    rows.push({ label: 'journal.tilePhotos', value: wholeNumberText(totals.photoCount, language) })
  if (totals.stepCount > 0)
    rows.push({ label: 'journal.tileSteps', value: wholeNumberText(totals.stepCount, language) })
  if (totals.codingSessionCount > 0) {
    rows.push({
      label: 'journal.tileWork',
      value: `${durationText(totals.codingMinutes, t)}, ${plural(totals.codingSessionCount, { one: 'journal.sessionsOne', other: 'journal.sessionsOther' })}`,
    })
  }
  if (totals.recordingCount > 0) {
    rows.push({ label: 'journal.recordings', value: wholeNumberText(totals.recordingCount, language) })
  }
  if (totals.spentMinorUnits > 0) {
    rows.push({ label: 'journal.tileSpent', value: amountText(totals.spentMinorUnits, totals.spentCurrency, language) })
  }
  return (
    <JournalCard title={t('journal.yearNumbers')}>
      {rows.length === 0 ? (
        <p className="muted">{t('journal.yearNoNumbers')}</p>
      ) : (
        <dl className="journal-numbers">
          {rows.map((row) => (
            <div key={row.label}>
              <dt className="muted">{t(row.label)}</dt>
              <dd>{row.value}</dd>
            </div>
          ))}
        </dl>
      )}
    </JournalCard>
  )
}

// YearMonths are the twelve months as small calendars, each day a square in
// the color of its mood, each month opening its page.
function YearMonths({ year, period }: { year: string; period: JournalPeriod }) {
  const { t, plural, language } = useTranslation()
  const today = browserToday()
  const weekStartsOn = weekStartsOnOf(null, language)
  const byMonth = new Map(period.months.map((summary) => [monthOf(summary.periodStart), summary]))
  return (
    <section className="journal-year-months" aria-label={t('journal.monthsOf', { year })}>
      {Array.from({ length: 12 }, (_, index) => {
        const month = `${year}-${String(index + 1).padStart(2, '0')}`
        const summary = byMonth.get(month)
        const isFuture = `${month}-01` > today
        const moods = summary?.dayMoods ?? []
        const leading = weekdayIndex(`${month}-01`, weekStartsOn)
        return (
          <Link
            key={month}
            className={['journal-year-month', isFuture ? 'is-future' : '', monthOf(today) === month ? 'is-current' : '']
              .filter(Boolean)
              .join(' ')}
            to={periodPath('month', `${month}-01`)}
          >
            <span className="journal-year-month-head">
              <strong>{monthNameText(month, language)}</strong>
              {summary && summary.placeCount > 0 && (
                <span className="muted">
                  {plural(summary.placeCount, { one: 'journal.placesOne', other: 'journal.placesOther' })}
                </span>
              )}
            </span>
            <span className="journal-year-days" aria-hidden="true">
              {Array.from({ length: leading }, (_, blank) => (
                <span key={`blank-${blank}`} />
              ))}
              {Array.from({ length: monthLength(month) }, (_, dayIndex) => {
                const level = moods[dayIndex] ?? 0
                return <span key={dayIndex} className="journal-year-day" data-level={level > 0 ? level : undefined} />
              })}
            </span>
            {summary?.headline && <span className="journal-prose journal-year-headline">{summary.headline}</span>}
          </Link>
        )
      })}
    </section>
  )
}
