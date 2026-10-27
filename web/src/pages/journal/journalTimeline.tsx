import { useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { graphql } from '../../api'
import { PageHeadingActions } from '../../components/breadcrumb'
import { ErrorMessage, Loading } from '../../components/common'
import {
  ActivityIcon,
  CalendarIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  CloseIcon,
  CodeIcon,
  LocationIcon,
  MoonIcon,
  SearchIcon,
  WalletIcon,
  WeatherIcon,
} from '../../components/icons'
import { Pager, usePageInAddress } from '../../components/pager'
import { SettingsEmpty, SettingsSection } from '../../components/settingsList'
import { Tabs } from '../../components/tabs'
import { useQuery } from '../../components/useQuery'
import { Tooltip } from '../../components/tooltip'
import { Key, useTranslation } from '../../i18n/i18n'
import { JOURNAL_TABS } from '../../pageTabs'
import { photoSmallPreviewPath } from '../../photos/api'
import { hasPermission, useSession } from '../../session'
import {
  JournalDayList,
  JournalDaySummary,
  JournalFacts,
  JournalPeriod,
  JournalSearchPage,
  JournalSettings,
  LIST_JOURNAL_DAYS,
  MOOD_LEVELS,
  MOOD_NAMES,
  READ_JOURNAL_PERIOD,
  READ_JOURNAL_SETTINGS,
  SEARCH_JOURNAL,
  TIMELINE_PAGE_SIZE,
  addDays,
  addMonths,
  amountText,
  dayPath,
  dayShortText,
  dayText,
  durationText,
  workText,
  hitPath,
  monthHeadingText,
  monthLength,
  monthNameText,
  monthOf,
  moodLevelOf,
  narrativeParagraphs,
  parseDay,
  periodPath,
  temperatureText,
  useIsImperial,
  wholeNumberText,
  weekdayIndex,
  weekdayNames,
  weekdayShortText,
  weatherNameKey,
  weekStartOf,
  yearOf,
} from './journalApi'
import { JournalSettingsTab } from './journalSettings'
import { JournalCard, MoodDot, MoodFace, browserToday, weekStartsOnOf } from './journalParts'

// The timeline: the days newest first, grouped into weeks with what each week
// was about, and beside them a month to jump around, a year ago today and the
// week so far. At the top, the way into a day, week, month or year, the search
// and writing today. In Settings, what the journal keeps and does.

export function JournalTimelinePage() {
  const session = useSession()
  const { t } = useTranslation()
  if (!hasPermission(session.permissions, 'journal:use')) {
    return (
      <SettingsSection card title={t('journal.title')}>
        <SettingsEmpty>{t('journal.off')}</SettingsEmpty>
      </SettingsSection>
    )
  }
  return <TimelineContent key={session.userId ?? ''} />
}

function TimelineContent() {
  const { language } = useTranslation()
  const [parameters, setParameters] = useSearchParams()
  const tab = parameters.get('tab') === 'settings' ? 'settings' : 'timeline'
  const searched = (parameters.get('q') ?? '').trim()
  const settings = useQuery(
    async () => (await graphql<{ ReadJournalSettings: JournalSettings }>(READ_JOURNAL_SETTINGS)).ReadJournalSettings,
    [],
    { refresh: false },
  )
  const weekStartsOn = weekStartsOnOf(settings.data, language)

  function changeParameters(changes: Record<string, string | null>) {
    setParameters((previous) => {
      const written = new URLSearchParams(previous)
      for (const [name, value] of Object.entries(changes)) {
        if (value === null) written.delete(name)
        else written.set(name, value)
      }
      return written
    })
  }

  return (
    <>
      <TimelineToolbar
        searched={searched}
        onSearch={(words) =>
          changeParameters({ q: words === '' ? null : words, page: null, searchPage: null, tab: null })
        }
      />
      <Tabs
        items={JOURNAL_TABS}
        active={tab}
        onSelect={(id) => changeParameters({ tab: id === 'timeline' ? null : id, q: null, page: null })}
      />
      {tab === 'settings' ? (
        <JournalSettingsTab settings={settings.data} onChanged={settings.reload} error={settings.error} />
      ) : searched !== '' ? (
        <SearchResults query={searched} />
      ) : (
        <TimelineBody weekStartsOn={weekStartsOn} />
      )}
    </>
  )
}

// The controls beside the heading: into a day, week, month or year, the
// search, and writing today.
function TimelineToolbar({ searched, onSearch }: { searched: string; onSearch: (words: string) => void }) {
  const { t } = useTranslation()
  const today = browserToday()
  const [typed, setTyped] = useState(searched)
  return (
    <PageHeadingActions>
      <div className="journal-toolbar">
        <div className="segmented journal-zoom" role="group" aria-label={t('journal.zoomGroup')}>
          <Link to={dayPath(today)}>{t('journal.zoomDay')}</Link>
          <Link to={periodPath('week', today)}>{t('journal.zoomWeek')}</Link>
          <Link to={periodPath('month', today)}>{t('journal.zoomMonth')}</Link>
          <Link to={periodPath('year', today)}>{t('journal.zoomYear')}</Link>
        </div>
        <form
          className="journal-search"
          role="search"
          onSubmit={(event) => {
            event.preventDefault()
            onSearch(typed.trim())
          }}
        >
          <SearchIcon size={16} />
          <input
            type="search"
            aria-label={t('journal.searchLabel')}
            placeholder={t('journal.searchPlaceholder')}
            value={typed}
            onChange={(event) => setTyped(event.target.value)}
          />
          {searched !== '' && (
            <Tooltip label={t('journal.searchClear')}>
              <button
                type="button"
                className="icon-action"
                aria-label={t('journal.searchClear')}
                onClick={() => {
                  setTyped('')
                  onSearch('')
                }}
              >
                <CloseIcon size={14} />
              </button>
            </Tooltip>
          )}
        </form>
        <Link className="button primary" to={dayPath(today)}>
          {t('journal.writeToday')}
        </Link>
      </div>
    </PageHeadingActions>
  )
}

// plainSummary is the first paragraph of a narrative as words, each mention
// replaced by the words the story wrote for it, or the title of what it points
// at; one the reader can no longer see by nothing.
function plainSummary(period: JournalPeriod | null): string {
  if (!period || period.narrative.narrative.trim() === '') return ''
  const titles = new Map(
    period.narrative.references
      .filter((reference) => !reference.isUnavailable)
      .map((reference) => [reference.reference, reference.title]),
  )
  const first = narrativeParagraphs(period.narrative.narrative)[0] ?? []
  return first
    .map((piece) => {
      if (!piece.isReference) return piece.text
      const title = titles.get(piece.reference)
      return title === undefined ? '' : piece.words || title
    })
    .join('')
}

type Week = { weekStart: string; days: JournalDaySummary[] }

function groupIntoWeeks(days: JournalDaySummary[], weekStartsOn: string): Week[] {
  const weeks: Week[] = []
  for (const day of days) {
    const weekStart = weekStartOf(day.day, weekStartsOn)
    const last = weeks[weeks.length - 1]
    if (last && last.weekStart === weekStart) last.days.push(day)
    else weeks.push({ weekStart, days: [day] })
  }
  return weeks
}

function TimelineBody({ weekStartsOn }: { weekStartsOn: string }) {
  const { t, language } = useTranslation()
  const paging = usePageInAddress({ defaultPageSize: TIMELINE_PAGE_SIZE })
  const [parameters] = useSearchParams()
  const today = browserToday()
  const month = /^\d{4}-\d{2}$/.test(parameters.get('month') ?? '')
    ? (parameters.get('month') as string)
    : monthOf(today)

  const days = useQuery(
    async () =>
      (
        await graphql<{ ListJournalDays: JournalDayList }>(LIST_JOURNAL_DAYS, {
          first: paging.pageSize,
          offset: paging.offset,
        })
      ).ListJournalDays,
    [paging.pageSize, paging.offset],
    { refresh: false },
  )
  const weeks = useMemo(() => groupIntoWeeks(days.data?.days ?? [], weekStartsOn), [days.data, weekStartsOn])

  // What each week on screen was about, asked for those weeks only. The week
  // that holds today has no story yet.
  const finishedWeeks = weeks.map((week) => week.weekStart).filter((weekStart) => addDays(weekStart, 6) < today)
  const weekKey = finishedWeeks.join(',')
  const summaries = useQuery(
    async () => {
      // A plain object: the answer is compared by its JSON, and a Map has
      // none, so a new one with weeks in it would look the same as the empty one.
      const found: Record<string, JournalPeriod> = {}
      await Promise.all(
        finishedWeeks.map(async (weekStart) => {
          try {
            const answer = await graphql<{ ReadJournalPeriod: JournalPeriod }>(READ_JOURNAL_PERIOD, {
              periodKind: 'week',
              day: weekStart,
            })
            found[weekStart] = answer.ReadJournalPeriod
          } catch {
            // A week with nothing to say is a week without a summary, not a
            // reason to fail the page.
          }
        }),
      )
      return found
    },
    [weekKey],
    { refresh: false },
  )

  const data = days.data
  const shown = data?.days ?? []
  return (
    <div className="journal-timeline">
      <div className="journal-timeline-main">
        <ErrorMessage error={days.error} />
        {days.error !== null && !data && !days.loading && (
          <div className="page-actions">
            <button type="button" onClick={() => void days.reload()}>
              {t('common.tryAgain')}
            </button>
          </div>
        )}
        {!data && days.error === null && <Loading />}
        {data && shown.length === 0 && (
          <SettingsSection card title={t('journal.emptyTitle')} description={t('journal.emptyBody')}>
            <div className="page-actions">
              <Link className="button primary" to={dayPath(today)}>
                {t('journal.writeToday')}
              </Link>
            </div>
          </SettingsSection>
        )}
        {weeks.map((week) => {
          const period = summaries.data?.[week.weekStart] ?? null
          const isThisWeek = week.weekStart === weekStartOf(today, weekStartsOn)
          const summary = plainSummary(period)
          return (
            <section key={week.weekStart} className="journal-week">
              <div className="journal-week-head">
                <h2>
                  {isThisWeek
                    ? t('journal.thisWeekOf', { date: dayShortText(week.weekStart, language) })
                    : t('journal.weekOf', { date: dayShortText(week.weekStart, language) })}
                </h2>
                <Link className="button-quiet" to={periodPath('week', week.weekStart)}>
                  {t('journal.readTheWeek')}
                </Link>
              </div>
              {(period?.narrative.headline || summary) && (
                <p className="journal-week-summary">
                  {period?.narrative.headline && <strong>{period.narrative.headline}</strong>}
                  {period?.narrative.headline && summary && ' '}
                  {summary}
                </p>
              )}
              <div className="journal-day-cards">
                {week.days.map((day) => (
                  <DayCard key={day.day} day={day} />
                ))}
              </div>
            </section>
          )
        })}
        {data && shown.length > 0 && (
          <div className="list-foot journal-list-foot">
            <span>
              {t('journal.rangeText', {
                from: dayShortText(shown[shown.length - 1].day, language),
                to: `${dayShortText(shown[0].day, language)}, ${yearOf(shown[0].day)}`,
              })}
            </span>
            <span className="journal-pager">
              <button
                type="button"
                disabled={paging.pageIndex === 0}
                onClick={() => paging.setPageIndex(paging.pageIndex - 1)}
              >
                {t('journal.newer')}
              </button>
              <button
                type="button"
                disabled={paging.offset + shown.length >= data.totalCount}
                onClick={() => paging.setPageIndex(paging.pageIndex + 1)}
              >
                {t('journal.older')}
              </button>
            </span>
          </div>
        )}
      </div>
      <aside className="journal-timeline-aside">
        <MonthCalendar month={month} weekStartsOn={weekStartsOn} today={data?.today ?? today} />
        <YearAgo today={data?.today ?? today} />
        <WeekSoFar today={data?.today ?? today} />
      </aside>
    </div>
  )
}

const FACT_LIMIT = 5

type Fact = { id: string; icon: React.ReactNode; text: string }

// The line that stands in for a headline on a day that has none: the first
// few of its numbers that are more than nothing, such as "3 projects, 4
// places, 3 photos". Never the date again, which the block beside the card
// already says.
function dayNumbersLine(
  day: JournalDaySummary,
  t: ReturnType<typeof useTranslation>['t'],
  plural: ReturnType<typeof useTranslation>['plural'],
): string {
  const { facts } = day
  const parts: string[] = []
  if (facts.workProjectCount > 0)
    parts.push(plural(facts.workProjectCount, { one: 'journal.projectsOne', other: 'journal.projectsOther' }))
  if (facts.placeCount > 0)
    parts.push(plural(facts.placeCount, { one: 'journal.placesOne', other: 'journal.placesOther' }))
  if (facts.photoCount > 0)
    parts.push(plural(facts.photoCount, { one: 'journal.photosCountOne', other: 'journal.photosCountOther' }))
  return parts.length > 0 ? parts.join(', ') : t('journal.dayQuiet')
}

// The chips under a day on the timeline: its weather, then the first few of
// its numbers that are more than nothing. A day with no headline already
// says its places in the line that stands for one.
function dayFacts(
  day: JournalDaySummary,
  t: ReturnType<typeof useTranslation>['t'],
  plural: ReturnType<typeof useTranslation>['plural'],
  language: string,
  isPlacesSaid: boolean,
): Fact[] {
  const facts: JournalFacts = day.facts
  const found: Fact[] = []
  if (facts.codingMinutes > 0)
    found.push({ id: 'work', icon: <CodeIcon size={14} />, text: durationText(facts.codingMinutes, t) })
  if (facts.meetingCount > 0) {
    found.push({
      id: 'meetings',
      icon: <CalendarIcon size={14} />,
      text: plural(facts.meetingCount, { one: 'journal.meetingsOne', other: 'journal.meetingsOther' }),
    })
  }
  if (facts.placeCount > 0 && !isPlacesSaid) {
    found.push({
      id: 'places',
      icon: <LocationIcon size={14} />,
      text: plural(facts.placeCount, { one: 'journal.placesOne', other: 'journal.placesOther' }),
    })
  }
  if (facts.stepCount > 0) {
    found.push({
      id: 'steps',
      icon: <ActivityIcon size={14} />,
      text: t('journal.stepsCount', { count: wholeNumberText(facts.stepCount, language) }),
    })
  }
  if (facts.sleepMinutes > 0)
    found.push({ id: 'sleep', icon: <MoonIcon size={14} />, text: durationText(facts.sleepMinutes, t) })
  if (facts.spentMinorUnits > 0) {
    found.push({
      id: 'spent',
      icon: <WalletIcon size={14} />,
      text: amountText(facts.spentMinorUnits, facts.spentCurrency, language),
    })
  }
  return found.slice(0, FACT_LIMIT)
}

function DayCard({ day }: { day: JournalDaySummary }) {
  const { t, plural, language } = useTranslation()
  const isImperial = useIsImperial()
  const level = moodLevelOf(day.mood)
  const facts = dayFacts(day, t, plural, language, day.headline === '')
  const photoIds = day.photoIds.slice(0, 4)
  const morePhotos = day.facts.photoCount - photoIds.length
  return (
    <article className={day.isToday ? 'journal-day-card is-today' : 'journal-day-card'}>
      <div className="journal-date-block">
        <div className="journal-date-weekday muted">{weekdayShortText(day.day, language)}</div>
        <div className="journal-date-number">{Number(day.day.slice(8, 10))}</div>
        {/* A day with no mood shows none: an empty face read as Okay. */}
        {level > 0 && (
          <span
            className="journal-mood-badge"
            role="img"
            aria-label={t('journal.moodOfDay', { mood: t(MOOD_NAMES[level]) })}
          >
            <MoodFace level={level} size={22} />
          </span>
        )}
      </div>
      <div className="journal-day-body">
        {day.isToday && (
          <div className="journal-today-label">
            <span className="journal-today-dot" aria-hidden="true" />
            {t('journal.todaySoFar')}
          </div>
        )}
        <h3 className={day.headline ? 'journal-day-headline' : 'journal-day-headline is-numbers'}>
          <Link className="journal-day-link" to={dayPath(day.day)}>
            {day.headline || dayNumbersLine(day, t, plural)}
          </Link>
        </h3>
        {day.hasWords && day.wordsExcerpt && <blockquote className="journal-excerpt">{day.wordsExcerpt}</blockquote>}
        {day.isToday && !day.hasWords && (
          <div className="journal-add-words">
            <Link className="button" to={dayPath(day.day)}>
              {t('journal.addYourWords')}
            </Link>
          </div>
        )}
        {photoIds.length > 0 && (
          <div className="journal-day-photos" aria-hidden="true">
            {photoIds.map((photoId) => (
              <img key={photoId} src={photoSmallPreviewPath(photoId)} alt="" loading="lazy" draggable={false} />
            ))}
            {morePhotos > 0 && <span className="journal-day-photos-more">+{morePhotos}</span>}
          </div>
        )}
        {(day.weather || facts.length > 0) && (
          <div className="journal-facts">
            {day.weather && (
              <span className="journal-fact">
                <WeatherIcon kind={day.weather.weatherKind} size={14} />
                {temperatureText(day.weather.highCelsius, isImperial)} /{' '}
                {temperatureText(day.weather.lowCelsius, isImperial)},{' '}
                {t(weatherNameKey(day.weather.weatherKind)).toLowerCase()}
              </span>
            )}
            {facts.map((fact) => (
              <span key={fact.id} className="journal-fact">
                {fact.icon}
                {fact.text}
              </span>
            ))}
          </div>
        )}
      </div>
    </article>
  )
}

// MonthCalendar is a month with each day in the color of its mood, to jump
// to a day from. The month is in the address, so a reload and a copied link
// keep it.
function MonthCalendar({ month, weekStartsOn, today }: { month: string; weekStartsOn: string; today: string }) {
  const { t, language } = useTranslation()
  const [, setParameters] = useSearchParams()
  const lastDay = `${month}-${String(monthLength(month)).padStart(2, '0')}`
  const days = useQuery(
    async () =>
      (
        await graphql<{ ListJournalDays: JournalDayList }>(LIST_JOURNAL_DAYS, {
          fromDay: `${month}-01`,
          untilDay: lastDay,
          first: 62,
        })
      ).ListJournalDays.days,
    [month],
    { refresh: false },
  )
  const moods = new Map((days.data ?? []).map((day) => [day.day, moodLevelOf(day.mood)]))
  const leadingBlanks = weekdayIndex(`${month}-01`, weekStartsOn)
  const goToMonth = (next: string) =>
    setParameters(
      (previous) => {
        const written = new URLSearchParams(previous)
        if (next === monthOf(today)) written.delete('month')
        else written.set('month', next)
        return written
      },
      { replace: true },
    )
  const previousMonth = addMonths(month, -1)
  const nextMonth = addMonths(month, 1)
  return (
    <JournalCard
      title={<Link to={periodPath('month', `${month}-01`)}>{monthHeadingText(month, language)}</Link>}
      action={
        <span className="journal-month-steps">
          <Tooltip label={monthNameText(previousMonth, language)}>
            <button
              type="button"
              className="icon-button"
              aria-label={monthNameText(previousMonth, language)}
              onClick={() => goToMonth(previousMonth)}
            >
              <ChevronLeftIcon size={16} />
            </button>
          </Tooltip>
          <Tooltip label={monthNameText(nextMonth, language)}>
            <button
              type="button"
              className="icon-button"
              aria-label={monthNameText(nextMonth, language)}
              disabled={nextMonth > monthOf(today)}
              onClick={() => goToMonth(nextMonth)}
            >
              <ChevronRightIcon size={16} />
            </button>
          </Tooltip>
        </span>
      }
    >
      <div className="journal-month-grid journal-month-weekdays muted" aria-hidden="true">
        {weekdayNames(weekStartsOn, language, 'narrow').map((name, index) => (
          <span key={index}>{name}</span>
        ))}
      </div>
      <div className="journal-month-grid">
        {Array.from({ length: leadingBlanks }, (_, index) => (
          <span key={`blank-${index}`} />
        ))}
        {Array.from({ length: monthLength(month) }, (_, index) => {
          const day = `${month}-${String(index + 1).padStart(2, '0')}`
          const level = moods.get(day) ?? 0
          const isFuture = day > today
          const label = dayShortText(day, language)
          return isFuture ? (
            <span key={day} className="journal-month-day is-future" aria-hidden="true">
              {index + 1}
            </span>
          ) : (
            <Link
              key={day}
              to={dayPath(day)}
              className={day === today ? 'journal-month-day is-today' : 'journal-month-day'}
              data-level={level > 0 ? level : undefined}
              aria-label={level > 0 ? `${label}: ${t(MOOD_NAMES[level])}` : label}
            >
              {index + 1}
            </Link>
          )
        })}
      </div>
      <div className="journal-legend muted">
        <span>{t('journal.mood')}</span>
        {MOOD_LEVELS.map((level) => (
          <MoodDot key={level} level={level} />
        ))}
        <span>{t('journal.moodRange')}</span>
      </div>
    </JournalCard>
  )
}

// YearAgo is the same date a year earlier, if the journal has anything on it.
function YearAgo({ today }: { today: string }) {
  const { t, language } = useTranslation()
  const [year, month, date] = today.split('-').map(Number)
  const dayAYearAgo = dayText(new Date(year - 1, month - 1, date))
  const found = useQuery(
    async () =>
      (
        await graphql<{ ListJournalDays: JournalDayList }>(LIST_JOURNAL_DAYS, {
          fromDay: dayAYearAgo,
          untilDay: dayAYearAgo,
          first: 1,
        })
      ).ListJournalDays.days[0] ?? null,
    [dayAYearAgo],
    { refresh: false },
  )
  const day = found.data
  if (!day) return null
  const words = day.headline || day.wordsExcerpt
  return (
    <JournalCard
      title={t('journal.yearAgo')}
      description={new Intl.DateTimeFormat(language, { dateStyle: 'full' }).format(parseDay(day.day))}
      isPaper
    >
      <div className="journal-year-ago">
        {(day.coverPhotoId || day.photoIds[0]) && (
          <img
            src={photoSmallPreviewPath(day.coverPhotoId || day.photoIds[0])}
            alt=""
            loading="lazy"
            draggable={false}
          />
        )}
        {words && <p className="journal-prose">{words}</p>}
      </div>
      <div className="page-actions">
        <Link className="button-quiet" to={dayPath(day.day)}>
          {t('journal.openTheDay')}
        </Link>
      </div>
    </JournalCard>
  )
}

// WeekSoFar adds up the week that holds today.
function WeekSoFar({ today }: { today: string }) {
  const { t, plural, language } = useTranslation()
  const week = useQuery(
    async () =>
      (await graphql<{ ReadJournalPeriod: JournalPeriod }>(READ_JOURNAL_PERIOD, { periodKind: 'week', day: today }))
        .ReadJournalPeriod,
    [today],
    { refresh: false },
  )
  const period = week.data
  if (!period || period.dayCount === 0) return null
  const totals = period.totals
  const sleepNights = period.days.filter((day) => day.facts.sleepMinutes > 0).length
  const moodText = [...MOOD_LEVELS]
    .reverse()
    .filter((level) => (period.moodCounts[level - 1] ?? 0) > 0)
    .map((level) => `${period.moodCounts[level - 1]} ${t(MOOD_NAMES[level]).toLowerCase()}`)
    .join(', ')
  const rows: { label: Key; value: string }[] = []
  if (moodText) rows.push({ label: 'journal.mood', value: moodText })
  if (sleepNights > 0) {
    rows.push({
      label: 'journal.tileSleep',
      value: t('journal.sleepAverage', { duration: durationText(totals.sleepMinutes / sleepNights, t) }),
    })
  }
  if (totals.codingMinutes > 0 || totals.workProjectCount > 0) {
    rows.push({ label: 'journal.tileWork', value: workText(totals, t, plural) })
  }
  if (totals.spentMinorUnits > 0) {
    rows.push({ label: 'journal.tileSpent', value: amountText(totals.spentMinorUnits, totals.spentCurrency, language) })
  }
  if (totals.placeCount > 0)
    rows.push({ label: 'journal.tilePlaces', value: wholeNumberText(totals.placeCount, language) })
  if (rows.length === 0) return null
  return (
    <JournalCard title={t('journal.weekSoFar')}>
      <dl className="journal-numbers">
        {rows.map((row) => (
          <div key={row.label}>
            <dt className="muted">{t(row.label)}</dt>
            <dd>{row.value}</dd>
          </div>
        ))}
      </dl>
      <div className="journal-card-foot">
        <span className="journal-card-foot-actions">
          <Link className="button-quiet" to={periodPath('week', today)}>
            {t('journal.readTheWeek')}
          </Link>
        </span>
      </div>
    </JournalCard>
  )
}

// SearchResults are the days and periods whose words, headline or narrative
// hold what was searched for, a page at a time.
function SearchResults({ query }: { query: string }) {
  const { t, language } = useTranslation()
  const paging = usePageInAddress({ pageParameter: 'searchPage', defaultPageSize: 25 })
  const found = useQuery(
    async () =>
      (
        await graphql<{ SearchJournal: JournalSearchPage }>(SEARCH_JOURNAL, {
          query,
          first: paging.pageSize,
          offset: paging.offset,
        })
      ).SearchJournal,
    [query, paging.pageSize, paging.offset],
    { refresh: false },
  )
  const page = found.data
  return (
    <>
      <ErrorMessage error={found.error} />
      {!page && found.error === null && <Loading />}
      {page && page.hits.length === 0 && (
        <SettingsSection card title={t('journal.searchTitle', { query })}>
          <SettingsEmpty>{t('journal.searchNothing')}</SettingsEmpty>
        </SettingsSection>
      )}
      {page && page.hits.length > 0 && (
        <SettingsSection card title={t('journal.searchTitle', { query })}>
          <ul className="journal-hits">
            {page.hits.map((hit) => (
              <li key={`${hit.periodKind}:${hit.day}`}>
                <Link to={hitPath(hit)}>
                  <span className="journal-hit-title">
                    {hit.periodKind === 'week'
                      ? t('journal.weekOf', { date: dayShortText(hit.day, language) })
                      : hit.periodKind === 'month'
                        ? monthHeadingText(monthOf(hit.day), language)
                        : hit.periodKind === 'year'
                          ? yearOf(hit.day)
                          : new Intl.DateTimeFormat(language, { dateStyle: 'full' }).format(parseDay(hit.day))}
                  </span>
                  {hit.headline && <span className="journal-hit-headline">{hit.headline}</span>}
                  {hit.snippet && <span className="muted journal-hit-snippet">{hit.snippet}</span>}
                </Link>
              </li>
            ))}
          </ul>
          <div className="list-foot journal-list-foot">
            <Pager
              pageIndex={paging.pageIndex}
              pageSize={paging.pageSize}
              shownCount={page.hits.length}
              totalCount={page.totalCount}
              onPageIndex={paging.setPageIndex}
            />
          </div>
        </SettingsSection>
      )}
    </>
  )
}
