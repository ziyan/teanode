import { en } from '../../i18n/en'
import { Key, Values } from '../../i18n/i18n'

// What the Health page asks the server and the shapes it gets back, with the
// small calculations the page makes from them. The operations are the ones in
// docs/planning/health-execplan.md; the command line (internal/client/health.go)
// asks the same ones.

export type Translate = (key: Key, values?: Values) => string

// The Health categories in the order the Health app lists them, with the one
// for types it has no group for last. A category the server sends that is not
// here goes after these.
export const HEALTH_CATEGORIES = [
  'activity',
  'bodyMeasurements',
  'cycleTracking',
  'hearing',
  'heart',
  'mentalWellbeing',
  'mobility',
  'nutrition',
  'respiratory',
  'sleep',
  'symptoms',
  'vitals',
  'other',
] as const

export type SummaryKind = 'sum' | 'average' | 'duration' | 'count'

export type HealthDaySummary = {
  summaryDay: string
  sumValue: number | null
  averageValue: number | null
  minimumValue: number | null
  maximumValue: number | null
  sampleCount: number
  durationSeconds: number | null
}

export type HealthType = {
  healthType: string
  healthCategory: string
  typeName: string
  valueKind: string
  quantityUnit: string
  summaryKind: SummaryKind
  firstDay: string | null
  lastDay: string | null
  sampleCount: number
  latestDaySummary: HealthDaySummary | null
}

export type HealthPeriod = {
  startDay: string
  endDay: string
  sumValue: number | null
  averageValue: number | null
  minimumValue: number | null
  maximumValue: number | null
  sampleCount: number
  // How many days of the period have a day summary: what a week's or month's
  // amount is divided by to be read per day.
  dayCount?: number
  durationSeconds: number | null
  categorySeconds: string | Record<string, number> | null
}

export type HealthSummary = {
  healthType: string
  quantityUnit: string
  summaryKind: SummaryKind
  periods: HealthPeriod[]
}

export type HealthSample = {
  healthSampleId: string
  healthType: string
  startedAt: string
  endedAt: string
  quantityValue: number | null
  quantityUnit: string
  categoryValue: number | null
  workoutActivityType: string
  sourceName: string
  hasRoute: boolean
}

export type HealthSamplePage = {
  samples: HealthSample[]
  totalCount: number
  isTotalCountCapped: boolean
  nextOffset: number | null
}

// The deepest a page of readings may start (the server refuses more), and so
// the most readings the list can reach, newest first.
export const HEALTH_SAMPLE_OFFSET_MOST = 100_000

export type HealthRoute = { healthSampleId: string; routePoints: number[][] }

// A fact about the person that is set in the Health app, such as the date of
// birth, as the iPhone sent it.
export type HealthCharacteristic = { healthType: string; characteristicValue: string }

export type HealthSettingsResult = {
  canAgentReadHealth: boolean
  healthErasedAt: string | null
  sampleCount: number
  lastReceivedAt: string | null
  modelProviderName: string
  characteristics: HealthCharacteristic[] | null
}

export type HealthShare = {
  id: string
  ownerUserId: string
  ownerUsername: string
  ownerName: string
  granteeUserId: string | null
  granteeUsername: string
  granteeCircleId: string | null
  granteeCircleName: string
  healthCategories: string[]
}

const DAY_FIELDS = 'sumValue averageValue minimumValue maximumValue sampleCount durationSeconds'

export const LIST_HEALTH_TYPES = `query ($ownerUserId: String) {
  ListHealthTypes(ownerUserId: $ownerUserId) {
    healthType healthCategory typeName valueKind quantityUnit summaryKind firstDay lastDay sampleCount
    latestDaySummary { summaryDay ${DAY_FIELDS} }
  }
}`

// The category split of a period is a JSON object, such as a night's seconds
// in each stage.
export const READ_HEALTH_SUMMARY = `query ($healthType: String!, $fromDay: String!, $toDay: String!, $period: String!,
  $ownerUserId: String) {
  ReadHealthSummary(healthType: $healthType, fromDay: $fromDay, toDay: $toDay, period: $period,
    ownerUserId: $ownerUserId) {
    healthType quantityUnit summaryKind
    periods { startDay endDay dayCount ${DAY_FIELDS} categorySeconds }
  }
}`

// Without from and to the count is exact, whatever the number of readings.
export const LIST_HEALTH_SAMPLES = `query ($healthType: String!, $from: DateTime, $to: DateTime, $limitCount: Int,
  $offset: Int) {
  ListHealthSamples(healthType: $healthType, from: $from, to: $to, limitCount: $limitCount, offset: $offset) {
    totalCount isTotalCountCapped nextOffset
    samples { healthSampleId healthType startedAt endedAt quantityValue quantityUnit categoryValue
      workoutActivityType sourceName hasRoute }
  }
}`

export const READ_HEALTH_ROUTE = `query ($healthSampleId: String!) {
  ReadHealthRoute(healthSampleId: $healthSampleId) { healthSampleId routePoints }
}`

const SETTINGS_FIELDS = `canAgentReadHealth healthErasedAt sampleCount lastReceivedAt modelProviderName
  characteristics { healthType characteristicValue }`

export const READ_HEALTH_SETTINGS = `query { ReadHealthSettings { ${SETTINGS_FIELDS} } }`

export const UPDATE_HEALTH_SETTINGS = `mutation ($canAgentReadHealth: Boolean!) {
  UpdateHealthSettings(canAgentReadHealth: $canAgentReadHealth) { ${SETTINGS_FIELDS} }
}`

export const ERASE_HEALTH_DATA = `mutation ($healthType: String) {
  EraseHealthData(healthType: $healthType) { erasedSampleCount }
}`

export const LIST_HEALTH_SHARES = `query {
  ListHealthShares { id ownerUserId ownerUsername ownerName granteeUserId granteeUsername granteeCircleId
    granteeCircleName healthCategories }
}`

export const SET_HEALTH_SHARE = `mutation ($granteeUserId: String, $granteeCircleId: String, $healthCategories: [String!]!) {
  SetHealthShare(granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId,
    healthCategories: $healthCategories) { id }
}`

// ranges and periods

export const RANGES = [
  { id: '30', days: 30, label: 'health.range30' },
  { id: '90', days: 90, label: 'health.range90' },
  { id: '365', days: 365, label: 'health.range365' },
  { id: 'all', days: 0, label: 'health.rangeAll' },
] as const

export type RangeID = (typeof RANGES)[number]['id']

export const PERIODS = [
  { id: 'day', label: 'health.periodDay' },
  { id: 'week', label: 'health.periodWeek' },
  { id: 'month', label: 'health.periodMonth' },
] as const

export type PeriodID = (typeof PERIODS)[number]['id']

export const DEFAULT_RANGE: RangeID = '30'

// The period a range is drawn by when the address names none: a bar a day
// would be thinner than a pixel over a year.
export function defaultPeriodFor(range: RangeID): PeriodID {
  if (range === 'all') return 'month'
  return range === '365' ? 'week' : 'day'
}

// The server answers at most this many periods.
const MOST_PERIODS = 400
// A week the range starts inside still counts as one of them, so a chart by
// week reaches back one fewer.
const MOST_WEEKS = MOST_PERIODS - 1

export function readRange(addressValue: string | null): RangeID {
  return RANGES.find((rangeChoice) => rangeChoice.id === addressValue)?.id ?? DEFAULT_RANGE
}

export function readPeriod(addressValue: string | null, range: RangeID): PeriodID {
  return PERIODS.find((periodChoice) => periodChoice.id === addressValue)?.id ?? defaultPeriodFor(range)
}

// dayText is a day written 2006-01-02, for a person's own calendar.
export function dayText(day: Date): string {
  const month = String(day.getMonth() + 1).padStart(2, '0')
  const date = String(day.getDate()).padStart(2, '0')
  return `${day.getFullYear()}-${month}-${date}`
}

export function parseDay(day: string): Date {
  const [year, month, date] = day.slice(0, 10).split('-').map(Number)
  return new Date(year, month - 1, date)
}

export function addDays(day: string, dayCount: number): string {
  const moved = parseDay(day)
  moved.setDate(moved.getDate() + dayCount)
  return dayText(moved)
}

export function daysBetweenCount(first: string, last: string): number {
  const milliseconds = parseDay(last).getTime() - parseDay(first).getTime()
  return Math.round(milliseconds / 86_400_000) + 1
}

// chartDays are the days a chart reaches over: from the range's start (or the
// first day there is data, for all of it) to today. A chart by day or by week
// never asks for more periods than the server answers; isCut says the range
// was shortened for that, and mostPeriods is how many periods are drawn then.
export function chartDays(range: RangeID, period: PeriodID, today: string, firstDay: string | null) {
  const days = RANGES.find((rangeChoice) => rangeChoice.id === range)?.days ?? 0
  let fromDay = days > 0 ? addDays(today, 1 - days) : (firstDay ?? addDays(today, 1 - 365)).slice(0, 10)
  let isCut = false
  const mostDays = period === 'day' ? MOST_PERIODS : period === 'week' ? MOST_WEEKS * 7 : 0
  if (mostDays > 0) {
    const earliest = addDays(today, 1 - mostDays)
    if (fromDay < earliest) {
      fromDay = earliest
      isCut = true
    }
  }
  return { fromDay, toDay: today, isCut, mostPeriods: period === 'week' ? MOST_WEEKS : MOST_PERIODS }
}

// numbers and units

// How a reading is written is the table at the top of internal/health/words.go:
// the command line and the agent's tool follow it in Go, and the dashboard and
// the iPhone app follow it here and in Swift. healthWords.test.ts feeds the
// same cases as words_test.go to these functions, so the two cannot drift
// apart without a test saying so.

// What a count of each kind counts. Its words are health.count.<noun>.one and
// health.count.<noun>.other.
const COUNT_NOUNS: Record<string, string> = {
  stepCount: 'step',
  flightsClimbed: 'flight',
  pushCount: 'push',
  swimmingStrokeCount: 'stroke',
  numberOfTimesFallen: 'fall',
  inhalerUsage: 'puff',
  numberOfAlcoholicBeverages: 'drink',
}

// The kinds measured in count/min that are not beats.
const RATE_UNIT_WORDS: Record<string, string> = {
  cyclingCadence: 'rpm',
  respiratoryRate: 'breaths/min',
}

// HealthKit's unit strings as a person writes them. A unit not here is
// written as it came.
const UNIT_WORDS: Record<string, string> = {
  degC: '°C',
  degF: '°F',
  'ml/(kg*min)': 'mL/kg·min',
  'kcal/(kg*hr)': 'kcal/kg·hr',
  dBASPL: 'dB',
  appleEffortScore: '',
}

// How many decimals a percentage is worth for the kinds whose size or meaning
// decides it (the table at the top of internal/health/words.go): blood oxygen
// and walking steadiness in whole numbers, body fat, walking asymmetry and
// walking double support in one. Every other percentage has the usual
// precision of numberText.
const PERCENT_DECIMALS: Record<string, number> = {
  oxygenSaturation: 0,
  appleWalkingSteadiness: 0,
  bodyFatPercentage: 1,
  walkingAsymmetryPercentage: 1,
  walkingDoubleSupportPercentage: 1,
}

// A distance from this many meters up is written in kilometers.
const KILOMETER_FROM_METERS = 1000

// unitWords is the word that follows a number of this kind, without a space
// when it is a symbol: "bpm", "kg", "°C", "%". A count has no word here: its
// noun is the language's, and quantityText writes it.
export function unitWords(healthType: string, quantityUnit: string): string {
  if (quantityUnit === 'count/min') return RATE_UNIT_WORDS[healthType] ?? 'bpm'
  if (quantityUnit === '' || quantityUnit === 'count') return ''
  if (quantityUnit === '%') return '%'
  return quantityUnit in UNIT_WORDS ? UNIT_WORDS[quantityUnit] : quantityUnit
}

// decimalsText is a number with up to this many decimals, trailing zeros
// dropped and thousands separated for the language. A tie is rounded half away
// from zero on the number as it is written in its shortest decimal form
// (0.35 to one decimal is 0.4), which is what Intl.NumberFormat does and what
// the Go and Swift words do; do not round first with Math.round.
export function decimalsText(measuredValue: number, language: string, mostDecimals: number): string {
  const text = new Intl.NumberFormat(language, {
    minimumFractionDigits: 0,
    maximumFractionDigits: mostDecimals,
  }).format(measuredValue)
  // A tiny negative number is zero, not "-0".
  return text === '-0' ? '0' : text
}

// numberText writes a reading with only as many decimals as the size of the
// number is worth: none from 1,000, one from 100, else two (8,412, 150.3,
// 72.4, 0.35).
export function numberText(measuredValue: number, language: string): string {
  const magnitude = Math.abs(measuredValue)
  return decimalsText(measuredValue, language, magnitude >= 1000 ? 0 : magnitude >= 100 ? 1 : 2)
}

// wholeNumberText is a count written for the language, such as 2,578 in
// English.
export function wholeNumberText(measuredValue: number, language: string): string {
  return decimalsText(measuredValue, language, 0)
}

// shownValue is a value as a person reads it. HealthKit keeps a percentage
// as a fraction (0.97 for 97%), and so does the server.
export function shownValue(measuredValue: number, quantityUnit: string): number {
  return quantityUnit === '%' ? measuredValue * 100 : measuredValue
}

// withUnit joins a number and its unit: a degree sign touches the number, any
// other word does not.
function withUnit(numberWords: string, unitWord: string): string {
  if (unitWord === '') return numberWords
  return unitWord.startsWith('°') ? numberWords + unitWord : `${numberWords} ${unitWord}`
}

// quantityText is a reading with its unit, as a person says it: 8,412 steps,
// 72.4 kg, 74 bpm, 97%, 36.6°C, 5.2 km.
export function quantityText(
  measuredValue: number,
  quantityUnit: string,
  healthType: string,
  language: string,
  t: Translate,
): string {
  if (quantityUnit === 'm' && Math.abs(measuredValue) >= KILOMETER_FROM_METERS) {
    return `${decimalsText(measuredValue / KILOMETER_FROM_METERS, language, 1)} km`
  }
  if (quantityUnit === '%') {
    const shown = shownValue(measuredValue, quantityUnit)
    const decimals = PERCENT_DECIMALS[healthType]
    return (decimals === undefined ? numberText(shown, language) : decimalsText(shown, language, decimals)) + '%'
  }
  const countNoun = quantityUnit === 'count' ? COUNT_NOUNS[healthType] : undefined
  if (quantityUnit === 'count/min' || countNoun !== undefined) {
    // What is counted, and every rate, is a whole number. It is rounded by
    // the number format alone, half away from zero (Math.round sends -0.5
    // to 0), and 1 or -1 once written is the singular.
    const wholeWords = wholeNumberText(measuredValue, language)
    if (countNoun !== undefined) {
      const isOne = wholeWords === wholeNumberText(1, language) || wholeWords === wholeNumberText(-1, language)
      const form = isOne ? 'one' : 'other'
      return t(`health.count.${countNoun}.${form}` as Key, { number: wholeWords })
    }
    return withUnit(wholeWords, unitWords(healthType, quantityUnit))
  }
  // A count that counts nothing in particular (body mass index, UV index)
  // keeps one decimal.
  if (quantityUnit === 'count') return decimalsText(measuredValue, language, 1)
  return withUnit(numberText(measuredValue, language), unitWords(healthType, quantityUnit))
}

// durationText writes a time covered in hours and minutes: 7 h 12 min,
// 35 min, 45 s. Seconds are rounded to the nearest second below a minute and
// to the nearest minute from a minute on.
export function durationText(seconds: number, t: Translate): string {
  const wholeSeconds = Math.round(seconds)
  if (wholeSeconds < 60) return t('health.seconds', { seconds: wholeSeconds })
  const totalMinutes = Math.round(seconds / 60)
  const hours = Math.floor(totalMinutes / 60)
  const minutes = totalMinutes % 60
  if (hours === 0) return t('health.minutes', { minutes })
  return minutes === 0 ? t('health.hours', { hours }) : t('health.hoursMinutes', { hours, minutes })
}

// The value a period or day stands for, by how the type's days add up: a sum
// of the readings, their average, the time they cover, or how many there were.
export function summaryValue(
  summary: {
    sumValue: number | null
    averageValue: number | null
    durationSeconds: number | null
    sampleCount: number
  },
  summaryKind: SummaryKind,
): number | null {
  switch (summaryKind) {
    case 'sum':
      return summary.sumValue
    case 'average':
      return summary.averageValue
    case 'duration':
      return summary.durationSeconds
    case 'count':
      return summary.sampleCount
  }
}

// amountText writes a day's or period's value the way it is read: a time as
// hours and minutes, a count of events as a whole number, a quantity with its
// unit.
export function amountText(
  amountValue: number | null,
  summaryKind: SummaryKind,
  quantityUnit: string,
  healthType: string,
  language: string,
  t: Translate,
): string {
  if (amountValue === null) return t('common.none')
  if (summaryKind === 'duration') return durationText(amountValue, t)
  if (summaryKind === 'count') return wholeNumberText(amountValue, language)
  return quantityText(amountValue, quantityUnit, healthType, language, t)
}

// names

// typeName is a health type in the person's language: the catalog's name for
// the types it knows, and the server's English one for the rest.
export function typeName(t: Translate, healthType: string, serverName: string): string {
  const key = `health.type.${healthType}`
  return key in en ? t(key as Key) : serverName || humanize(healthType)
}

export function categoryName(t: Translate, healthCategory: string): string {
  const key = `health.category.${healthCategory}`
  return key in en ? t(key as Key) : humanize(healthCategory)
}

// workoutName is a workout's activity type in the person's language. No
// activity type is the same as "other".
export function workoutName(t: Translate, activityType: string): string {
  const identifier = activityType || 'other'
  const key = `health.workout.${identifier}`
  return key in en ? t(key as Key) : humanize(identifier)
}

// humanize turns an identifier such as "traditionalStrengthTraining" into
// words in sentence case, keeping an acronym as it is ("someREMReading" is
// "Some REM reading"). It is nameFromIdentifier in internal/health/types.go.
export function humanize(identifier: string): string {
  const letters = [...identifier]
  const isUpper = (letter: string) => letter !== letter.toLowerCase()
  const isLower = (letter: string) => letter !== letter.toUpperCase()
  const isDigit = (letter: string) => letter >= '0' && letter <= '9'
  const words: string[] = []
  let word = ''
  letters.forEach((letter, index) => {
    const before = letters[index - 1]
    const after = letters[index + 1]
    const isWordStart =
      index > 0 &&
      isUpper(letter) &&
      (isLower(before) || isDigit(before) || (isUpper(before) && after !== undefined && isLower(after)))
    if (isWordStart) {
      words.push(word)
      word = ''
    }
    word += letter
  })
  if (word !== '') words.push(word)
  return words
    .map((word, index) => {
      if ([...word].length > 1 && word === word.toUpperCase()) return word
      return index === 0 ? word.charAt(0).toUpperCase() + word.slice(1) : word.toLowerCase()
    })
    .join(' ')
}

// The stages of a night, from the deepest up, with the colors the chart gives
// them. In bed is left out: it covers the time asleep.
export const SLEEP_STAGES = [
  { id: 'asleepDeep', tone: 'deep' },
  { id: 'asleepCore', tone: 'core' },
  { id: 'asleepREM', tone: 'rem' },
  { id: 'asleepUnspecified', tone: 'cached' },
  { id: 'awake', tone: 'awake' },
] as const

// The raw values of HealthKit's sleep analysis category, in the stage names
// the summaries use.
export const SLEEP_VALUE_NAMES: Record<number, string> = {
  0: 'inBed',
  1: 'asleepUnspecified',
  2: 'awake',
  3: 'asleepCore',
  4: 'asleepDeep',
  5: 'asleepREM',
}

// stageName is a stage of sleep in the person's language; a key that is not a
// stage is written as it came.
export function stageName(t: Translate, stage: string): string {
  const key = `health.stage.${stage}`
  return key in en ? t(key as Key) : stage
}

// categorySecondsOf reads a period's seconds in each category value.
export function categorySecondsOf(period: HealthPeriod): Record<string, number> {
  const raw = period.categorySeconds
  if (!raw) return {}
  try {
    const parsed: unknown = typeof raw === 'string' ? JSON.parse(raw) : raw
    if (parsed === null || typeof parsed !== 'object') return {}
    return Object.fromEntries(
      Object.entries(parsed as Record<string, unknown>).filter(
        (entry): entry is [string, number] => typeof entry[1] === 'number',
      ),
    )
  } catch {
    return {}
  }
}

// The facts about the person that Health keeps, in the order they are shown.
export const CHARACTERISTIC_TYPES = [
  'dateOfBirth',
  'biologicalSex',
  'bloodType',
  'fitzpatrickSkinType',
  'wheelchairUse',
  'activityMoveMode',
] as const

// The words of a fact about the person whose value is one of a fixed list,
// by the value the phone sends.
const SEX_KEYS: Record<string, Key> = {
  female: 'health.sex.female',
  male: 'health.sex.male',
  other: 'health.sex.other',
}
const WHEELCHAIR_KEYS: Record<string, Key> = {
  yes: 'health.wheelchair.yes',
  no: 'health.wheelchair.no',
}
const BLOOD_TYPE = /^(a|b|ab|o)(Positive|Negative)$/

// ageInYears is the age in whole years on a day of someone born on a day
// written YYYY-MM-DD; null for text that is not a day, or a day after it.
export function ageInYears(dateOfBirth: string, today: Date): number | null {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(dateOfBirth)) return null
  const born = parseDay(dateOfBirth)
  if (dayText(born) !== dateOfBirth || dateOfBirth > dayText(today)) return null
  const hasHadBirthday =
    today.getMonth() > born.getMonth() || (today.getMonth() === born.getMonth() && today.getDate() >= born.getDate())
  return today.getFullYear() - born.getFullYear() - (hasHadBirthday ? 0 : 1)
}

// characteristicText is a fact about the person as it is told to somebody: a
// date of birth as the date with the age in years after it (1990-04-12 (36
// years)), a sex, blood type, wheelchair use or move mode in its words
// (Female, O+, Yes), a skin type as the Roman numeral the phone sends, and
// "Not set" for what Health has not been given. Anything else, and a date of
// birth that is not a date or is in the future, is written as it was sent.
// dayWords writes the date in the language of the reader.
export function characteristicText(
  t: Translate,
  healthType: string,
  characteristicValue: string,
  today: Date,
  dayWords: (day: Date) => string = dayText,
): string {
  if (characteristicValue === 'notSet') return t('health.notSet')
  switch (healthType) {
    case 'dateOfBirth': {
      const age = ageInYears(characteristicValue, today)
      if (age === null) return characteristicValue
      const ageWords = age === 1 ? t('health.ageOne') : t('health.age', { count: age })
      return `${dayWords(parseDay(characteristicValue))} (${ageWords})`
    }
    case 'biologicalSex':
      return characteristicValue in SEX_KEYS ? t(SEX_KEYS[characteristicValue]) : characteristicValue
    case 'bloodType': {
      const match = BLOOD_TYPE.exec(characteristicValue)
      return match ? `${match[1].toUpperCase()}${match[2] === 'Positive' ? '+' : '-'}` : characteristicValue
    }
    case 'wheelchairUse':
      return characteristicValue in WHEELCHAIR_KEYS ? t(WHEELCHAIR_KEYS[characteristicValue]) : characteristicValue
    case 'activityMoveMode':
      if (characteristicValue === 'activeEnergy') return typeName(t, 'activeEnergyBurned', '')
      if (characteristicValue === 'appleMoveTime') return typeName(t, 'appleMoveTime', '')
      return characteristicValue
    default:
      return characteristicValue
  }
}

// The figures under a chart, worked out from its periods.
export type HealthNumbers = {
  // The average of a type that is read as a level (a rate, a weight), weighted
  // by how many readings each period holds.
  average: number | null
  // The average of a day's amount for a type that adds up, over the days that
  // have any.
  averagePerDay: number | null
  total: number | null
  lowest: number | null
  highest: number | null
}

// periodDayCount is how many days of a period the chart reaches over: the
// current week or month has not ended, and the range may start inside one.
// It is only what a period's dayCount is taken to be when the server sent none.
export function periodDayCount(period: HealthPeriod, fromDay: string, toDay: string): number {
  const first = period.startDay.slice(0, 10) > fromDay ? period.startDay.slice(0, 10) : fromDay
  const last = period.endDay.slice(0, 10) < toDay ? period.endDay.slice(0, 10) : toDay
  return Math.max(1, daysBetweenCount(first, last))
}

// summarizedDayCount is the days of a period that have a day summary, which a
// week's or month's amount is divided by to be read per day: a week the watch
// was worn on three nights is three nights, not seven.
export function summarizedDayCount(period: HealthPeriod, fromDay: string, toDay: string): number {
  return period.dayCount !== undefined && period.dayCount > 0 ? period.dayCount : periodDayCount(period, fromDay, toDay)
}

export function numbersOf(
  periods: HealthPeriod[],
  summaryKind: SummaryKind,
  fromDay: string,
  toDay: string,
): HealthNumbers {
  const filled = periods.filter((period) => period.sampleCount > 0)
  const numbers: HealthNumbers = { average: null, averagePerDay: null, total: null, lowest: null, highest: null }
  if (filled.length === 0) return numbers
  if (summaryKind === 'average') {
    const readings = filled.reduce((readingCount, period) => readingCount + period.sampleCount, 0)
    const weighted = filled.reduce((sum, period) => sum + (period.averageValue ?? 0) * period.sampleCount, 0)
    numbers.average = readings > 0 ? weighted / readings : null
    const lows = filled.flatMap((period) => (period.minimumValue === null ? [] : [period.minimumValue]))
    const highs = filled.flatMap((period) => (period.maximumValue === null ? [] : [period.maximumValue]))
    numbers.lowest = lows.length > 0 ? Math.min(...lows) : null
    numbers.highest = highs.length > 0 ? Math.max(...highs) : null
    return numbers
  }
  numbers.total = filled.reduce((sum, period) => sum + (summaryValue(period, summaryKind) ?? 0), 0)
  if (summaryKind !== 'count') {
    const dayCount = filled.reduce(
      (summarizedDays, period) => summarizedDays + summarizedDayCount(period, fromDay, toDay),
      0,
    )
    numbers.averagePerDay = dayCount > 0 ? numbers.total / dayCount : null
  }
  return numbers
}
