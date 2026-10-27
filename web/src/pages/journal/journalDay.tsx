import { useCallback, useState } from 'react'
import { Link, Navigate, useParams } from 'react-router-dom'

import { graphql } from '../../api'
import { useBreadcrumbDetail, useBreadcrumbNote } from '../../components/breadcrumb'
import { useIsDesktop } from '../../components/sidebar'
import { ErrorMessage, Loading } from '../../components/common'
import {
  ActivityIcon,
  CalendarIcon,
  CodeIcon,
  LocationIcon,
  MoonIcon,
  PictureIcon,
  WalletIcon,
  WeatherIcon,
} from '../../components/icons'
import { SettingsEmpty, SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { hasPermission, useSession } from '../../session'
import { CalendarCard, HealthCard, MomentsCard, PlacesCard } from './journalDayAside'
import {
  ConversationsSection,
  KnowledgeSection,
  MailSection,
  MoneySection,
  PhotosSection,
  PinsSection,
  RecordingsSection,
  ShoppingSection,
  WorkChatSection,
  WorkSection,
} from './journalDaySections'
import {
  JournalDay,
  READ_JOURNAL_DAY,
  SET_JOURNAL_MOOD,
  amountText,
  dayCompactText,
  dayHeadingText,
  dayShortText,
  distanceText,
  durationText,
  isDayText,
  moodLevelOf,
  temperatureText,
  useIsImperial,
  weatherNameKey,
  wholeNumberText,
  APPLE_HEALTH_MOOD_SOURCE,
} from './journalApi'
import { NarrativeCard } from './journalNarrative'
import { PhotoLightbox } from './journalOpen'
import { MoodRadios, PeriodToolbar, browserToday, dayStepPaths, useKeptScroll, usePendingPoll } from './journalParts'
import { JournalWordsCard } from './journalWords'

// One day: the person's mood and the weather, the day's numbers, their own
// words, what TeaNode wrote about it, and everything that happened, each
// linking to its own page. The address is /journal/YYYY-MM-DD.

export function JournalDayPage() {
  const { day } = useParams()
  const session = useSession()
  const { t } = useTranslation()
  if (!isDayText(day)) return <Navigate to="/journal" replace />
  if (!hasPermission(session.permissions, 'journal:use')) {
    return (
      <SettingsSection card title={t('journal.title')}>
        <SettingsEmpty>{t('journal.off')}</SettingsEmpty>
      </SettingsSection>
    )
  }
  return <JournalDayContent key={`${session.userId ?? ''}:${day}`} day={day} />
}

function JournalDayContent({ day: dayText }: { day: string }) {
  const { t, language } = useTranslation()
  const query = useQuery(
    async () => (await graphql<{ ReadJournalDay: JournalDay }>(READ_JOURNAL_DAY, { day: dayText })).ReadJournalDay,
    [dayText],
    { refresh: false },
  )
  const reload = useCallback(async () => {
    await query.reload(true)
  }, [query])
  const day = query.data
  usePendingPoll(Boolean(day?.words.touchUpPending || day?.narrative.narrativePending), reload)
  useKeptScroll(Boolean(day))

  // The phone's bar holds the trail and the page's name in one line: there
  // the day is its short date (Mon, Oct 26) after Journal alone, where the
  // full name and its year were cut short.
  const isDesktop = useIsDesktop()
  useBreadcrumbDetail(
    isDesktop ? dayHeadingText(dayText, language) : dayCompactText(dayText, language),
    isDesktop && day?.weekStart ? t('journal.weekOf', { date: dayShortText(day.weekStart, language) }) : null,
  )
  useBreadcrumbNote(isDesktop ? dayText.slice(0, 4) : null)

  return (
    <>
      <PeriodToolbar
        periodKind="day"
        referenceDay={dayText}
        {...dayStepPaths(dayText, browserToday())}
        currentLabel={t('journal.today')}
        previousLabel={t('journal.dayBefore')}
        nextLabel={t('journal.dayAfter')}
      />
      <ErrorMessage error={query.error} />
      {query.error !== null && !day && !query.loading && (
        <div className="page-actions">
          <button type="button" onClick={() => void query.reload()}>
            {t('common.tryAgain')}
          </button>
        </div>
      )}
      {!day && query.error === null && <Loading />}
      {day && <DayBody day={day} onChanged={reload} />}
    </>
  )
}

function DayBody({ day, onChanged }: { day: JournalDay; onChanged: () => Promise<void> }) {
  const { t } = useTranslation()
  const isEmpty = isEmptyDay(day)
  return (
    <PhotoLightbox photos={day.photos} timeZone={day.timeZone}>
      <section className="journal-glance" aria-label={t('journal.glance')}>
        <MoodCard day={day} onChanged={onChanged} />
        <WeatherCard day={day} />
      </section>
      <FactTiles day={day} />
      {isEmpty && (
        <SettingsSection card>
          <SettingsEmpty>{t('journal.dayEmpty')}</SettingsEmpty>
        </SettingsSection>
      )}
      <div className="journal-columns">
        <div className="journal-main">
          <JournalWordsCard
            words={day.words}
            target={{ day: day.day, periodKind: null }}
            title={t('journal.yourWords')}
            onChanged={onChanged}
          />
          <NarrativeCard
            narrative={day.narrative}
            target={{ day: day.day, periodKind: null }}
            title={t('journal.dayAsSeen')}
            description={
              day.narrative.sourceCount > 0
                ? t('journal.narrativeFromThings', { count: day.narrative.sourceCount })
                : undefined
            }
            isToday={day.isToday}
            writeLabel={day.isToday ? t('journal.writeTodaySoFar') : t('journal.writeThisDay')}
            onChanged={onChanged}
          />
          <PinsSection day={day} onChanged={onChanged} />
          <WorkSection day={day} onChanged={onChanged} />
          <WorkChatSection day={day} />
          <MailSection day={day} onChanged={onChanged} />
          <PhotosSection day={day} />
          <div className="journal-pair">
            <RecordingsSection day={day} />
            <ConversationsSection day={day} />
          </div>
          <MoneySection day={day} />
          <div className="journal-pair">
            <ShoppingSection day={day} />
            <KnowledgeSection day={day} />
          </div>
        </div>
        <aside className="journal-aside">
          <PlacesCard day={day} />
          <MomentsCard day={day} onChanged={onChanged} />
          <HealthCard day={day} />
          <CalendarCard day={day} />
        </aside>
      </div>
    </PhotoLightbox>
  )
}

// isEmptyDay says there is nothing yet on the day to show: no words, no mood
// and no record from any source.
function isEmptyDay(day: JournalDay): boolean {
  const { facts } = day
  return (
    day.words.original.trim() === '' &&
    day.words.journalNotes.length === 0 &&
    moodLevelOf(day.mood) === 0 &&
    day.moments.length === 0 &&
    day.pins.length === 0 &&
    facts.photoCount === 0 &&
    facts.placeCount === 0 &&
    facts.stepCount === 0 &&
    facts.sleepMinutes === 0 &&
    facts.codingSessionCount === 0 &&
    facts.meetingCount === 0 &&
    facts.transactionCount === 0
  )
}

// MoodCard is the five faces and a few words. A mood from Apple Health shows
// as the day's until the person chooses their own.
function MoodCard({ day, onChanged }: { day: JournalDay; onChanged: () => Promise<void> }) {
  const { t } = useTranslation()
  const toast = useToast()
  const [isBusy, setIsBusy] = useState(false)
  const level = moodLevelOf(day.mood)
  const savedWords = day.mood?.moodWords ?? ''

  async function save(moodLevel: number | null, moodWords: string) {
    setIsBusy(true)
    try {
      await graphql(SET_JOURNAL_MOOD, { day: day.day, moodLevel, moodWords })
      toast.done(moodLevel === null ? t('journal.moodCleared') : t('journal.moodSaved'))
      await onChanged()
    } catch (caught) {
      toast.failure(caught, t('journal.moodFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  return (
    <div className="card journal-mood-card">
      <div className="journal-mood-faces">
        <div className="journal-field-label">{t('journal.moodQuestion')}</div>
        <MoodRadios level={level} isBusy={isBusy} onChoose={(chosen) => void save(chosen, savedWords)} />
        {day.mood?.moodSource === APPLE_HEALTH_MOOD_SOURCE && level > 0 && (
          <span className="tag journal-source-tag">{t('journal.fromAppleHealth')}</span>
        )}
      </div>
      <MoodWords
        // The text starts again from what the server holds after a save.
        key={savedWords}
        savedWords={savedWords}
        isDisabled={level === 0 || isBusy}
        onCommit={(moodWords) => void save(level, moodWords)}
        onClear={level > 0 && day.mood?.moodSource !== APPLE_HEALTH_MOOD_SOURCE ? () => void save(null, '') : undefined}
      />
    </div>
  )
}

function MoodWords({
  savedWords,
  isDisabled,
  onCommit,
  onClear,
}: {
  savedWords: string
  isDisabled: boolean
  onCommit: (moodWords: string) => void
  onClear?: () => void
}) {
  const { t } = useTranslation()
  const [words, setWords] = useState(savedWords)
  // Saved when the person leaves the field or presses Enter, and only when
  // there is something new to save.
  const commit = () => {
    const trimmed = words.trim()
    if (trimmed !== savedWords) onCommit(trimmed)
  }
  return (
    <div className="journal-mood-words">
      <label htmlFor="journal-mood-words" className="journal-field-label">
        {t('journal.moodWords')}
      </label>
      <input
        id="journal-mood-words"
        value={words}
        disabled={isDisabled}
        placeholder={isDisabled ? t('journal.moodWordsNeedFace') : ''}
        maxLength={200}
        onChange={(event) => setWords(event.target.value)}
        onBlur={commit}
        onKeyDown={(event) => {
          if (event.key === 'Enter') {
            event.preventDefault()
            commit()
          }
        }}
      />
      {onClear && (
        <button type="button" className="button-quiet journal-clear" onClick={onClear}>
          {t('journal.moodClear')}
        </button>
      )}
    </div>
  )
}

function WeatherCard({ day }: { day: JournalDay }) {
  const { t } = useTranslation()
  const isImperial = useIsImperial()
  const weather = day.weather
  if (!weather) return null
  const name = t(weatherNameKey(weather.weatherKind))
  return (
    <div className="card journal-weather-card">
      <span className="journal-weather-icon">
        <WeatherIcon kind={weather.weatherKind} size={40} />
      </span>
      <div>
        <div className="journal-weather-temperature">
          {temperatureText(weather.highCelsius, isImperial)}{' '}
          <span className="muted">/ {temperatureText(weather.lowCelsius, isImperial)}</span>
        </div>
        <div className="muted journal-weather-words">
          {weather.placeName ? t('journal.weatherAt', { weather: name, place: weather.placeName }) : name}
        </div>
      </div>
    </div>
  )
}

type Tile = {
  id: string
  label: string
  value: string
  unit?: string
  detail?: string
  to: string
  icon: React.ReactNode
}

// The day's numbers as tiles: only the ones that are more than nothing, each
// opening the page that holds them.
function FactTiles({ day }: { day: JournalDay }) {
  const { t, plural, language } = useTranslation()
  const { facts } = day
  const isImperial = useIsImperial()
  const tiles: Tile[] = []
  if (facts.stepCount > 0) {
    tiles.push({
      id: 'steps',
      label: t('journal.tileSteps'),
      value: wholeNumberText(facts.stepCount, language),
      detail:
        facts.walkedMeters > 0
          ? t('journal.walked', { distance: distanceText(facts.walkedMeters, isImperial, language) })
          : undefined,
      to: '/health',
      icon: <ActivityIcon size={14} />,
    })
  }
  if (facts.sleepMinutes > 0) {
    tiles.push({
      id: 'sleep',
      label: t('journal.tileSleep'),
      value: durationText(facts.sleepMinutes, t),
      to: '/health',
      icon: <MoonIcon size={14} />,
    })
  }
  if (facts.codingMinutes > 0 || facts.codingSessionCount > 0) {
    tiles.push({
      id: 'work',
      label: t('journal.tileWork'),
      value: durationText(facts.codingMinutes, t),
      detail:
        facts.workProjectCount > 0
          ? plural(facts.workProjectCount, { one: 'journal.projectsOne', other: 'journal.projectsOther' })
          : undefined,
      to: '/coding',
      icon: <CodeIcon size={14} />,
    })
  }
  if (facts.meetingCount > 0) {
    tiles.push({
      id: 'meetings',
      label: t('journal.tileMeetings'),
      value: wholeNumberText(facts.meetingCount, language),
      detail: facts.meetingMinutes > 0 ? durationText(facts.meetingMinutes, t) : undefined,
      to: `/calendar?view=day&on=${day.day}`,
      icon: <CalendarIcon size={14} />,
    })
  }
  if (facts.placeCount > 0) {
    tiles.push({
      id: 'places',
      label: t('journal.tilePlaces'),
      value: wholeNumberText(facts.placeCount, language),
      detail: facts.mainPlaceName ? t('journal.mostlyAt', { place: facts.mainPlaceName }) : undefined,
      to: `/location?day=${day.day}`,
      icon: <LocationIcon size={14} />,
    })
  }
  if (facts.photoCount > 0) {
    tiles.push({
      id: 'photos',
      label: t('journal.tilePhotos'),
      value: wholeNumberText(facts.photoCount, language),
      to: '/photos',
      icon: <PictureIcon size={14} />,
    })
  }
  if (facts.spentMinorUnits > 0) {
    tiles.push({
      id: 'spent',
      label: t('journal.tileSpent'),
      value: amountText(facts.spentMinorUnits, facts.spentCurrency, language),
      detail: plural(facts.transactionCount, { one: 'journal.transactionsOne', other: 'journal.transactionsOther' }),
      to: '/finance/transactions',
      icon: <WalletIcon size={14} />,
    })
  }
  if (tiles.length === 0) return null
  return (
    <section className="tile-grid journal-tiles" aria-label={t('journal.numbers')}>
      {tiles.map((tile) => (
        <Link key={tile.id} className="tile journal-tile" to={tile.to}>
          <div className="tile-label journal-tile-label">
            {tile.icon}
            {tile.label}
          </div>
          <div className="tile-value">
            {tile.value}
            {tile.unit && <span className="tile-unit">{tile.unit}</span>}
          </div>
          <div className="tile-detail">{tile.detail ?? ' '}</div>
        </Link>
      ))}
    </section>
  )
}
