import { Key, Values } from '../../i18n/i18n'
import { addDays, dayText, parseDay } from '../health/healthApi'
import { useOptionalSession } from '../../session'

// What the Journal pages ask the server and the shapes they get back, with the
// small calculations the pages make from them. The operations are the ones in
// internal/api/v1api/apigraph/journal.go, and docs/planning/journal-execplan.md
// has the design. A day is written YYYY-MM-DD in the person's calendar; a week,
// month or year is named by its kind and any day in it.

export type Translate = (key: Key, values?: Values) => string

export type Plural = (count: number, forms: { one: Key; other: Key }, values?: Values) => string

export type PeriodKind = 'week' | 'month' | 'year'

export type JournalMood = {
  moodLevel: number
  moodWords: string
  // person, or health for Apple Health's State of Mind.
  moodSource: string
}

export type JournalWeather = {
  weatherCode: number
  weatherKind: string
  highCelsius: number
  lowCelsius: number
  precipitationMM: number
  placeName: string
}

export type JournalFacts = {
  photoCount: number
  placeCount: number
  walkedMeters: number
  stepCount: number
  sleepMinutes: number
  workoutMinutes: number
  codingSessionCount: number
  // How many projects were worked on; a period adds its days' counts up.
  workProjectCount: number
  codingMinutes: number
  meetingCount: number
  meetingMinutes: number
  recordingCount: number
  conversationCount: number
  chatMessageCount: number
  mailReceivedCount: number
  mailSentCount: number
  documentCount: number
  transactionCount: number
  spentMinorUnits: number
  spentCurrency: string
  shoppingTripCount: number
  memoryPageCount: number
  mainPlaceName: string
}

export type JournalDaySummary = {
  day: string
  isToday: boolean
  mood: JournalMood | null
  weather: JournalWeather | null
  headline: string
  wordsExcerpt: string
  hasWords: boolean
  facts: JournalFacts
  coverPhotoId: string
  photoIds: string[]
  hasNarrative: boolean
}

export type JournalDayList = {
  days: JournalDaySummary[]
  totalCount: number
  today: string
  firstDay: string
}

export type JournalNote = { documentId: string; title: string; text: string }

export type JournalWords = {
  original: string
  modifiedAt: string | null
  isTouchUpOn: boolean
  touchedUp: string
  touchUpHow: string
  isTouchedUpStale: boolean
  touchUpPending: boolean
  journalNotes: JournalNote[]
}

export type JournalRecord = {
  recordKind: string
  recordId: string
  reference: string
  title: string
  happenedAt: string | null
  dashboardPath: string
  isUnavailable: boolean
}

export type JournalCorrection = { correctionText: string; createdAt: string }

export type JournalNarrative = {
  headline: string
  narrative: string
  references: JournalRecord[]
  writtenAt: string | null
  isStale: boolean
  narrativePending: boolean
  isBeyondDepth: boolean
  failure: string
  corrections: JournalCorrection[]
  sourceCount: number
}

export type JournalLink = {
  linkId: string
  record: JournalRecord
  // agent for a choice, person for a pin.
  linkSource: string
  linkReason: string
  isHidden: boolean
}

export type JournalCodingSession = {
  documentId: string
  title: string
  directory: string
  assistant: string
  startedAt: string
  endedAt: string | null
  // How many stretches of work (each ended by a pause) the day had in it.
  stretchCount: number
  linkReason: string
}

export type JournalShipped = {
  documentId: string
  title: string
  // Where the request lives, on the host that holds the repository.
  address: string
  happenedAt: string
  isDraft: boolean
}

// One project worked on in a day: the whole day's coding in a repository or
// folder, in the one place a person reads first.
export type JournalWorkProject = {
  name: string
  firstAt: string
  lastAt: string
  activeMinutes: number
  stretchCount: number
  // The titles of its sessions, each once.
  topics: string[]
  shipped: JournalShipped[]
  // What the agent wrote was done, once the day is written; empty before.
  summary: string
  documentId: string
}

export type JournalDocument = {
  documentId: string
  title: string
  sourceName: string
  happenedAt: string
  isChosen: boolean
  linkReason: string
}

export type JournalConversation = {
  conversationId: string
  title: string
  // The person's main conversation, which is named in their own language.
  isMain: boolean
  firstAt: string
  lastAt: string
  messageCount: number
}

export type JournalChat = {
  chatId: string
  chatName: string
  firstAt: string
  lastAt: string
  messageCount: number
}

export type JournalMail = {
  itemId: string
  fromName: string
  subject: string
  receivedAt: string
  isSent: boolean
  isImportant: boolean
  isChosen: boolean
  linkReason: string
  // How many messages of the day it stands for: the same sender and subject again.
  repeatCount: number
}

export type JournalRecording = {
  recordingId: string
  title: string
  recordedAt: string | null
  durationMS: number
  summary: string
}

export type JournalPhoto = { photoId: string; takenAt: string; placeName: string; isVideo: boolean }

export type JournalVisit = {
  visitId: string
  placeId: string
  placeName: string
  arrivedAt: string | null
  departedAt: string | null
  latitude: number
  longitude: number
}

export type JournalEvent = {
  eventId: string
  title: string
  calendarName: string
  startsAt: string
  endsAt: string
  isAllDay: boolean
  location: string
  reference: string
}

export type JournalReminder = { reminderId: string; title: string; dueAt: string | null; doneAt: string | null }

export type JournalTransaction = {
  transactionId: string
  postedOn: string
  transactedAt: string | null
  description: string
  accountName: string
  // Negative for money spent.
  amountMinorUnits: number
  currency: string
}

export type JournalSleepStage = { sleepStage: string; minutes: number }

export type JournalWorkout = {
  sampleId: string
  activityType: string
  startedAt: string
  minutes: number
  meters: number
}

export type JournalHealth = {
  stepCount: number
  stepsByHour: number[]
  sleepMinutes: number
  sleepStages: JournalSleepStage[]
  restingHeartRate: number
  workouts: JournalWorkout[]
}

export type JournalShoppingTrip = {
  tripId: string
  listNames: string[]
  startedAt: string
  finishedAt: string | null
  boughtCount: number
  itemCount: number
}

export type JournalMemoryPage = { nodeId: string; path: string; title: string; factCount: number }

export type JournalMoment = {
  at: string
  recordKind: string
  recordId: string
  title: string
  detail: string
  dashboardPath: string
}

export type JournalDay = {
  day: string
  timeZone: string
  isToday: boolean
  weekday: string
  weekStart: string
  words: JournalWords
  mood: JournalMood | null
  weather: JournalWeather | null
  facts: JournalFacts
  narrative: JournalNarrative
  choices: JournalLink[]
  pins: JournalLink[]
  coverPhotoId: string
  workProjects: JournalWorkProject[]
  codingSessions: JournalCodingSession[]
  documents: JournalDocument[]
  documentTotal: number
  conversations: JournalConversation[]
  chats: JournalChat[]
  mail: JournalMail[]
  mailTotal: number
  recordings: JournalRecording[]
  photos: JournalPhoto[]
  photoTotal: number
  visits: JournalVisit[]
  events: JournalEvent[]
  reminders: JournalReminder[]
  transactions: JournalTransaction[]
  health: JournalHealth | null
  shoppingTrips: JournalShoppingTrip[]
  notes: JournalRecord[]
  memoryPages: JournalMemoryPage[]
  unavailableSections: string[]
  moments: JournalMoment[]
}

export type JournalMonthSummary = {
  periodStart: string
  headline: string
  // The month's days in order, each 0 (no mood) or 1 to 5.
  dayMoods: number[]
  placeCount: number
  photoCount: number
}

export type JournalChapter = { fromDay: string; untilDay: string; title: string; detail: string }

export type JournalPeriod = {
  periodKind: PeriodKind
  periodStart: string
  periodEnd: string
  isCurrent: boolean
  lookingBack: JournalWords
  narrative: JournalNarrative
  choices: JournalLink[]
  days: JournalDaySummary[]
  months: JournalMonthSummary[]
  totals: JournalFacts
  // How many days had each mood, awful first.
  moodCounts: number[]
  dayCount: number
  chapters: JournalChapter[]
}

export type JournalSearchHit = { day: string; periodKind: string; headline: string; snippet: string }

export type JournalSearchPage = { hits: JournalSearchHit[]; totalCount: number }

export type JournalSettings = {
  isWeatherOn: boolean
  isEveningQuestionOn: boolean
  eveningQuestionTime: string
  weekStartsOn: string
  narrativeDepthDays: number
  modelProviderName: string
}

// operations

// The selections are strings with their braces, used as `Field(...) ${NAME}`,
// the way the session's are in api.ts.
const MOOD_FIELDS = '{ moodLevel moodWords moodSource }'
const WEATHER_FIELDS = '{ weatherCode weatherKind highCelsius lowCelsius precipitationMM placeName }'
const FACTS_FIELDS =
  '{ photoCount placeCount walkedMeters stepCount sleepMinutes workoutMinutes codingSessionCount workProjectCount ' +
  'codingMinutes ' +
  'meetingCount meetingMinutes recordingCount conversationCount chatMessageCount mailReceivedCount mailSentCount ' +
  'documentCount transactionCount spentMinorUnits spentCurrency shoppingTripCount memoryPageCount mainPlaceName }'
const DAY_SUMMARY_FIELDS =
  '{ day isToday mood ' +
  MOOD_FIELDS +
  ' weather ' +
  WEATHER_FIELDS +
  ' headline wordsExcerpt hasWords facts ' +
  FACTS_FIELDS +
  ' coverPhotoId photoIds hasNarrative }'
const WORDS_FIELDS =
  '{ original modifiedAt isTouchUpOn touchedUp touchUpHow isTouchedUpStale touchUpPending ' +
  'journalNotes { documentId title text } }'
const RECORD_FIELDS = '{ recordKind recordId reference title happenedAt dashboardPath isUnavailable }'
const NARRATIVE_FIELDS =
  '{ headline narrative references ' +
  RECORD_FIELDS +
  ' writtenAt isStale narrativePending isBeyondDepth failure corrections { correctionText createdAt } sourceCount }'
const LINK_FIELDS = '{ linkId record ' + RECORD_FIELDS + ' linkSource linkReason isHidden }'

const DAY_FIELDS =
  '{ day timeZone isToday weekday weekStart words ' +
  WORDS_FIELDS +
  ' mood ' +
  MOOD_FIELDS +
  ' weather ' +
  WEATHER_FIELDS +
  ' facts ' +
  FACTS_FIELDS +
  ' narrative ' +
  NARRATIVE_FIELDS +
  ' choices ' +
  LINK_FIELDS +
  ' pins ' +
  LINK_FIELDS +
  ' coverPhotoId ' +
  'workProjects { name firstAt lastAt activeMinutes stretchCount topics ' +
  'shipped { documentId title address happenedAt isDraft } summary documentId } ' +
  'codingSessions { documentId title directory assistant startedAt endedAt stretchCount linkReason } ' +
  'documents { documentId title sourceName happenedAt isChosen linkReason } documentTotal ' +
  'conversations { conversationId title isMain firstAt lastAt messageCount } ' +
  'chats { chatId chatName firstAt lastAt messageCount } ' +
  'mail { itemId fromName subject receivedAt isSent isImportant isChosen linkReason repeatCount } mailTotal ' +
  'recordings { recordingId title recordedAt durationMS summary } ' +
  'photos { photoId takenAt placeName isVideo } photoTotal ' +
  'visits { visitId placeId placeName arrivedAt departedAt latitude longitude } ' +
  'events { eventId title calendarName startsAt endsAt isAllDay location reference } ' +
  'reminders { reminderId title dueAt doneAt } ' +
  'transactions { transactionId postedOn transactedAt description accountName amountMinorUnits currency } ' +
  'health { stepCount stepsByHour sleepMinutes sleepStages { sleepStage minutes } restingHeartRate ' +
  'workouts { sampleId activityType startedAt minutes meters } } ' +
  'shoppingTrips { tripId listNames startedAt finishedAt boughtCount itemCount } ' +
  'notes ' +
  RECORD_FIELDS +
  ' memoryPages { nodeId path title factCount } unavailableSections ' +
  'moments { at recordKind recordId title detail dashboardPath } }'

const PERIOD_FIELDS =
  '{ periodKind periodStart periodEnd isCurrent lookingBack ' +
  WORDS_FIELDS +
  ' narrative ' +
  NARRATIVE_FIELDS +
  ' choices ' +
  LINK_FIELDS +
  ' days ' +
  DAY_SUMMARY_FIELDS +
  ' months { periodStart headline dayMoods placeCount photoCount } totals ' +
  FACTS_FIELDS +
  ' moodCounts dayCount chapters { fromDay untilDay title detail } }'

const SETTINGS_FIELDS =
  '{ isWeatherOn isEveningQuestionOn eveningQuestionTime weekStartsOn narrativeDepthDays modelProviderName }'

export const LIST_JOURNAL_DAYS = `query ($fromDay: String, $untilDay: String, $first: Int, $offset: Int) {
  ListJournalDays(fromDay: $fromDay, untilDay: $untilDay, first: $first, offset: $offset) {
    days ${DAY_SUMMARY_FIELDS} totalCount today firstDay
  }
}`

export const READ_JOURNAL_DAY = `query ($day: String!) { ReadJournalDay(day: $day) ${DAY_FIELDS} }`

export const READ_JOURNAL_PERIOD = `query ($periodKind: String!, $day: String!) {
  ReadJournalPeriod(periodKind: $periodKind, day: $day) ${PERIOD_FIELDS}
}`

export const SEARCH_JOURNAL = `query ($query: String!, $first: Int, $offset: Int) {
  SearchJournal(query: $query, first: $first, offset: $offset) {
    hits { day periodKind headline snippet } totalCount
  }
}`

export const READ_JOURNAL_SETTINGS = `query { ReadJournalSettings ${SETTINGS_FIELDS} }`

export const UPDATE_JOURNAL_SETTINGS = `mutation ($isWeatherOn: Boolean, $isEveningQuestionOn: Boolean,
  $eveningQuestionTime: String) {
  UpdateJournalSettings(isWeatherOn: $isWeatherOn, isEveningQuestionOn: $isEveningQuestionOn,
    eveningQuestionTime: $eveningQuestionTime) ${SETTINGS_FIELDS}
}`

export const ERASE_JOURNAL = `mutation { EraseJournal { dayCount periodCount linkCount } }`

export const SET_JOURNAL_WORDS = `mutation ($day: String!, $words: String!, $baseModifiedAt: DateTime) {
  SetJournalWords(day: $day, words: $words, baseModifiedAt: $baseModifiedAt) { day }
}`

export const SET_JOURNAL_LOOKING_BACK = `mutation ($periodKind: String!, $day: String!, $lookingBack: String!,
  $baseModifiedAt: DateTime) {
  SetJournalLookingBack(periodKind: $periodKind, day: $day, lookingBack: $lookingBack,
    baseModifiedAt: $baseModifiedAt) { periodKind }
}`

export const SET_JOURNAL_MOOD = `mutation ($day: String!, $moodLevel: Int, $moodWords: String) {
  SetJournalMood(day: $day, moodLevel: $moodLevel, moodWords: $moodWords) { day }
}`

export const TOUCH_UP_JOURNAL_WORDS = `mutation ($day: String!, $periodKind: String, $how: String) {
  TouchUpJournalWords(day: $day, periodKind: $periodKind, how: $how) { day }
}`

export const STOP_JOURNAL_TOUCH_UP = `mutation ($day: String!, $periodKind: String) {
  StopJournalTouchUp(day: $day, periodKind: $periodKind) { day }
}`

export const CORRECT_JOURNAL_NARRATIVE = `mutation ($day: String!, $periodKind: String, $correctionText: String!) {
  CorrectJournalNarrative(day: $day, periodKind: $periodKind, correctionText: $correctionText) { headline }
}`

export const REMOVE_JOURNAL_CORRECTION = `mutation ($day: String!, $periodKind: String, $correctionIndex: Int!) {
  RemoveJournalCorrection(day: $day, periodKind: $periodKind, correctionIndex: $correctionIndex) { headline }
}`

export const WRITE_JOURNAL_NARRATIVE = `mutation ($day: String!, $periodKind: String) {
  WriteJournalNarrative(day: $day, periodKind: $periodKind) { headline }
}`

export const REMOVE_JOURNAL_LINK = `mutation ($linkId: String!) { RemoveJournalLink(linkId: $linkId) }`

// What a day or a period is called to the server: the day alone for a day, and
// the kind with any day in the period for a period.
export type JournalTarget = { day: string; periodKind: PeriodKind | null }

export function targetVariables(target: JournalTarget): { day: string; periodKind: string | null } {
  return { day: target.day, periodKind: target.periodKind }
}

// moods

export const MOOD_LEVELS = [1, 2, 3, 4, 5] as const

export const MOOD_NAMES: Record<number, Key> = {
  1: 'journal.moodAwful',
  2: 'journal.moodLow',
  3: 'journal.moodOkay',
  4: 'journal.moodGood',
  5: 'journal.moodGreat',
}

export const APPLE_HEALTH_MOOD_SOURCE = 'health'

// moodLevelOf is a level the faces can show, or 0 for none.
export function moodLevelOf(mood: JournalMood | null | undefined): number {
  const level = mood?.moodLevel ?? 0
  return level >= 1 && level <= 5 ? level : 0
}

// days and periods

export function isDayText(value: string | undefined): value is string {
  if (!value || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return false
  return dayText(parseDay(value)) === value
}

export function isMonthText(value: string | undefined): value is string {
  return Boolean(value) && /^\d{4}-(0[1-9]|1[0-2])$/.test(value as string)
}

export function isYearText(value: string | undefined): value is string {
  return Boolean(value) && /^\d{4}$/.test(value as string)
}

export const dayOf = (anyDay: string) => anyDay.slice(0, 10)
export const monthOf = (anyDay: string) => anyDay.slice(0, 7)
export const yearOf = (anyDay: string) => anyDay.slice(0, 4)

// addMonths moves a month (YYYY-MM) by whole months.
export function addMonths(month: string, count: number): string {
  const [year, number] = month.split('-').map(Number)
  const moved = new Date(year, number - 1 + count, 1)
  return `${moved.getFullYear()}-${String(moved.getMonth() + 1).padStart(2, '0')}`
}

export function addYears(year: string, count: number): string {
  return String(Number(year) + count).padStart(4, '0')
}

// How many days a month holds, for the calendar grids.
export function monthLength(month: string): number {
  const [year, number] = month.split('-').map(Number)
  return new Date(year, number, 0).getDate()
}

// weekdayIndex is how many days a day is after the first day of its week:
// 0 for the day a week starts on, up to 6.
export function weekdayIndex(day: string, weekStartsOn: string): number {
  const dayOfWeek = parseDay(day).getDay()
  return (dayOfWeek - (weekStartsOn === 'sunday' ? 0 : 1) + 7) % 7
}

export function weekStartOf(day: string, weekStartsOn: string): string {
  return addDays(day, -weekdayIndex(day, weekStartsOn))
}

// The week a week-starting day names is any of its days to the server; this
// is the day the pages hand it.
export function periodAnchor(periodKind: PeriodKind, periodStart: string): string {
  return periodKind === 'year'
    ? `${periodStart.slice(0, 4)}-01-01`
    : periodKind === 'month'
      ? `${periodStart.slice(0, 7)}-01`
      : periodStart
}

export function dayPath(day: string): string {
  return `/journal/${day}`
}

export function periodPath(periodKind: PeriodKind, anchorDay: string): string {
  if (periodKind === 'week') return `/journal/week/${dayOf(anchorDay)}`
  if (periodKind === 'month') return `/journal/month/${monthOf(anchorDay)}`
  return `/journal/year/${yearOf(anchorDay)}`
}

// The page of a search hit: its day, or its period.
export function hitPath(hit: JournalSearchHit): string {
  const kind = hit.periodKind as PeriodKind | ''
  return kind === 'week' || kind === 'month' || kind === 'year' ? periodPath(kind, hit.day) : dayPath(hit.day)
}

// How a day is written in words, in the reader's language. The date is
// built in the reader's own zone from its parts, so it never moves a day.
export function dayHeadingText(day: string, language: string): string {
  return new Intl.DateTimeFormat(language, { weekday: 'long', month: 'long', day: 'numeric' }).format(parseDay(day))
}

// dayCompactText is a day as a narrow bar fits it: Mon, Oct 26.
export function dayCompactText(day: string, language: string): string {
  return new Intl.DateTimeFormat(language, { weekday: 'short', month: 'short', day: 'numeric' }).format(parseDay(day))
}

export function dayShortText(day: string, language: string): string {
  return new Intl.DateTimeFormat(language, { month: 'long', day: 'numeric' }).format(parseDay(day))
}

export function dayLongText(day: string, language: string): string {
  return new Intl.DateTimeFormat(language, { dateStyle: 'full' }).format(parseDay(day))
}

export function monthHeadingText(month: string, language: string): string {
  return new Intl.DateTimeFormat(language, { month: 'long', year: 'numeric' }).format(parseDay(`${month}-01`))
}

export function monthShortText(month: string, language: string): string {
  return new Intl.DateTimeFormat(language, { month: 'short' }).format(parseDay(`${month}-01`))
}

export function monthNameText(month: string, language: string): string {
  return new Intl.DateTimeFormat(language, { month: 'long' }).format(parseDay(`${month}-01`))
}

// weekdayNames are the seven weekday names starting with the one a week
// starts on, in the width given. 2024-01-01 was a Monday.
export function weekdayNames(weekStartsOn: string, language: string, width: 'narrow' | 'short'): string[] {
  const firstDay = weekStartsOn === 'sunday' ? '2023-12-31' : '2024-01-01'
  const format = new Intl.DateTimeFormat(language, { weekday: width })
  return Array.from({ length: 7 }, (_, index) => format.format(parseDay(addDays(firstDay, index))))
}

export function weekdayShortText(day: string, language: string): string {
  return new Intl.DateTimeFormat(language, { weekday: 'short' }).format(parseDay(day))
}

// clockText is a moment as the time on the clock where the day was lived, so
// a photo at 11:30 PM stays at 11:30 PM wherever the reader is.
export function clockText(value: string | null | undefined, timeZone: string, language: string): string {
  if (!value) return ''
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return ''
  const shape: Intl.DateTimeFormatOptions = { hour: 'numeric', minute: '2-digit' }
  try {
    return new Intl.DateTimeFormat(language, { ...shape, timeZone: timeZone || undefined }).format(parsed)
  } catch {
    return new Intl.DateTimeFormat(language, shape).format(parsed)
  }
}

// units

// Fahrenheit and miles for the places that use them. A person's own choice
// (metric or us) is the answer; when they have made none, the browser's
// region is, since the server keeps no region of its own.
const IMPERIAL_REGIONS = ['US', 'LR', 'MM']

export function usesImperialUnits(
  measurementSystem: string | undefined,
  locale: string = typeof navigator === 'undefined' ? '' : navigator.language,
): boolean {
  if (measurementSystem === 'us') return true
  if (measurementSystem === 'metric') return false
  try {
    const region = new Intl.Locale(locale).maximize().region
    return region !== undefined && IMPERIAL_REGIONS.includes(region)
  } catch {
    return false
  }
}

// useIsImperial is whether the signed-in person reads Fahrenheit and miles.
export function useIsImperial(): boolean {
  return usesImperialUnits(useOptionalSession()?.measurementSystem)
}

export function temperatureText(celsius: number, isImperial: boolean): string {
  const value = isImperial ? (celsius * 9) / 5 + 32 : celsius
  return `${Math.round(value)}°`
}

const METERS_PER_MILE = 1609.344

export function distanceText(meters: number, isImperial: boolean, language: string): string {
  const format = new Intl.NumberFormat(language, { maximumFractionDigits: 1 })
  if (isImperial) return `${format.format(meters / METERS_PER_MILE)} mi`
  return `${format.format(meters / 1000)} km`
}

export function wholeNumberText(count: number, language: string): string {
  return new Intl.NumberFormat(language).format(Math.round(count))
}

// durationText is minutes as hours and minutes: "6 h 10 min", "40 min".
export function durationText(minutes: number, t: Translate): string {
  const whole = Math.max(0, Math.round(minutes))
  const hours = Math.floor(whole / 60)
  const rest = whole % 60
  if (hours === 0) return t('journal.durationMinutes', { minutes: rest })
  if (rest === 0) return t('journal.durationHours', { hours })
  return t('journal.durationHoursMinutes', { hours, minutes: rest })
}

// workText is the time spent coding and the projects it was spread over, for
// the totals of a week, a month or a year: "6 h 10 min, 3 projects". Never the
// number of sessions, which is how many times the assistant was started and
// says nothing about the work.
// workDaysText is how many days of a period had work: the period's projects
// added up day by day would count one project once a day.
export function workDaysText(days: { facts: { codingMinutes: number } }[], plural: Plural): string {
  const count = days.filter((day) => day.facts.codingMinutes > 0).length
  return count > 0 ? plural(count, { one: 'journal.workDaysOne', other: 'journal.workDaysOther' }) : ''
}

// periodWorkText is a period's work: its hours, and on how many days.
export function periodWorkText(
  totals: { codingMinutes: number },
  days: { facts: { codingMinutes: number } }[],
  t: Translate,
  plural: Plural,
): string {
  return [totals.codingMinutes > 0 ? durationText(totals.codingMinutes, t) : '', workDaysText(days, plural)]
    .filter(Boolean)
    .join(' ')
}

export function workText(
  totals: { codingMinutes: number; workProjectCount: number },
  t: Translate,
  plural: Plural,
): string {
  return [
    totals.codingMinutes > 0 ? durationText(totals.codingMinutes, t) : '',
    totals.workProjectCount > 0
      ? plural(totals.workProjectCount, { one: 'journal.projectsOne', other: 'journal.projectsOther' })
      : '',
  ]
    .filter(Boolean)
    .join(', ')
}

// amountText is money in a currency's minor units, as the currency writes it.
// The number of places comes from the currency, so a yen is not divided by a
// hundred.
export function amountText(minorUnits: number, currency: string, language: string): string {
  const code = (currency || 'USD').toUpperCase()
  try {
    const format = new Intl.NumberFormat(language, { style: 'currency', currency: code })
    const places = format.resolvedOptions().maximumFractionDigits ?? 2
    return format.format(minorUnits / 10 ** places)
  } catch {
    return `${(minorUnits / 100).toFixed(2)} ${code}`
  }
}

// webAddressOf is an address only when it is a web page: the address of a
// pull request comes from a remote host, and a link must never run a script
// or open a file.
export function webAddressOf(address: string): string | null {
  try {
    const parsed = new URL(address)
    return parsed.protocol === 'https:' || parsed.protocol === 'http:' ? parsed.href : null
  } catch {
    return null
  }
}

// How many topics of a project are listed before "and N more".
export const PROJECT_TOPIC_LIMIT = 4

// How many rows of a list are shown before "Show all", and how many moments
// of the day.
export const LIST_ROW_LIMIT = 6
export const MOMENT_LIMIT = 12

// the narrative

// A mention carries the words its sentence shows for it, [[kind:id|words]],
// or none, [[kind:id]], when the record's title stands in.
export type NarrativePiece =
  { isReference: false; text: string } | { isReference: true; reference: string; words: string }

const REFERENCE = /\[\[([a-z_]+:[^\]\s|][^\]|]*)(?:\|([^\]]*))?\]\]/g

// narrativeParagraphs splits a narrative at its blank lines, and each
// paragraph into the words and the mentions in them.
export function narrativeParagraphs(narrative: string): NarrativePiece[][] {
  return narrative
    .split(/\n\s*\n/)
    .map((paragraph) => paragraph.trim())
    .filter((paragraph) => paragraph !== '')
    .map((paragraph) => {
      const pieces: NarrativePiece[] = []
      let last = 0
      for (const match of paragraph.matchAll(REFERENCE)) {
        if (match.index > last) pieces.push({ isReference: false, text: paragraph.slice(last, match.index) })
        pieces.push({ isReference: true, reference: match[1], words: (match[2] ?? '').trim() })
        last = match.index + match[0].length
      }
      if (last < paragraph.length) pieces.push({ isReference: false, text: paragraph.slice(last) })
      return pieces
    })
}

// textParagraphs splits the person's words at their blank lines. A single line
// break stays a line break (the stylesheet keeps it), since a diary is written
// that way.
export function textParagraphs(text: string): string[] {
  return text
    .split(/\n\s*\n/)
    .map((paragraph) => paragraph.trim())
    .filter((paragraph) => paragraph !== '')
}

// The word for a day's weather, by the kind the server gives it.
export const WEATHER_NAMES: Record<string, Key> = {
  clear: 'journal.weatherClear',
  partlyCloudy: 'journal.weatherPartlyCloudy',
  cloudy: 'journal.weatherCloudy',
  fog: 'journal.weatherFog',
  drizzle: 'journal.weatherDrizzle',
  rain: 'journal.weatherRain',
  snow: 'journal.weatherSnow',
  showers: 'journal.weatherShowers',
  thunderstorm: 'journal.weatherThunderstorm',
}

export function weatherNameKey(weatherKind: string): Key {
  return WEATHER_NAMES[weatherKind] ?? 'journal.weatherCloudy'
}

// The word for a kind of record, for a mention the reader can no longer open.
export const KIND_NAMES: Record<string, Key> = {
  mail: 'journal.kindMail',
  calendar_event: 'journal.kindCalendarEvent',
  calendar_occurrence: 'journal.kindCalendarEvent',
  photo: 'journal.kindPhoto',
  place_visit: 'journal.kindPlace',
  agent_document: 'journal.kindDocument',
  chat_message: 'journal.kindMessage',
  conversation_message: 'journal.kindMessage',
  recording: 'journal.kindRecording',
  recording_passage: 'journal.kindRecording',
  finance_transaction: 'journal.kindTransaction',
  finance_receipt: 'journal.kindTransaction',
  health_day: 'journal.kindHealth',
  shopping_trip: 'journal.kindShoppingTrip',
  note: 'journal.kindNote',
  reminder: 'journal.kindReminder',
  idea: 'journal.kindIdea',
  goal: 'journal.kindGoal',
  contact: 'journal.kindContact',
}

export function kindNameKey(recordKind: string): Key {
  return KIND_NAMES[recordKind] ?? 'journal.kindOther'
}

// Where a record of each section opens when the server did not say. The
// moments carry the server's own path, and are looked up first.
export function momentPaths(moments: JournalMoment[]): Map<string, string> {
  const found = new Map<string, string>()
  for (const moment of moments) {
    if (moment.dashboardPath.startsWith('/')) found.set(`${moment.recordKind}:${moment.recordId}`, moment.dashboardPath)
  }
  return found
}

// isConflict says whether a save was refused because the words were saved
// somewhere else after this page read them.
export function isConflict(caught: unknown): boolean {
  return caught instanceof Error && /conflict/i.test(caught.message)
}

// The most a correction may hold, and the most a page may carry, as the
// server limits them.
export const CORRECTION_MOST_CHARACTERS = 500
export const CORRECTION_MOST_COUNT = 20

// How often a page asks again while a touched-up text or a narrative is on
// its way, and when it gives up.
export const POLL_INTERVAL_MILLISECONDS = 3000
export const POLL_GIVE_UP_MILLISECONDS = 120_000

export const TIMELINE_PAGE_SIZE = 14

export { addDays, dayText, parseDay }
