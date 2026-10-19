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

export type HealthSamplePage = { samples: HealthSample[]; totalCount: number; nextOffset: number | null }

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

export const LIST_HEALTH_SAMPLES = `query ($healthType: String!, $from: DateTime!, $to: DateTime!, $limitCount: Int,
  $offset: Int) {
  ListHealthSamples(healthType: $healthType, from: $from, to: $to, limitCount: $limitCount, offset: $offset) {
    totalCount nextOffset
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

export function readRange(value: string | null): RangeID {
  return RANGES.find((each) => each.id === value)?.id ?? DEFAULT_RANGE
}

export function readPeriod(value: string | null, range: RangeID): PeriodID {
  return PERIODS.find((each) => each.id === value)?.id ?? defaultPeriodFor(range)
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

export function addDays(day: string, count: number): string {
  const moved = parseDay(day)
  moved.setDate(moved.getDate() + count)
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
  const days = RANGES.find((each) => each.id === range)?.days ?? 0
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

// How a unit HealthKit sends is written beside a number, the same words as the
// command line and the iPhone use. A unit not here is written as it came.
// Steps are HealthKit's plain count, so the dashboard gives them the unit
// STEPS_UNIT where a number stands without the type's name beside it.
export const STEPS_UNIT = 'steps'

const UNIT_LABELS: Record<string, string> = {
  count: '',
  'count/min': 'bpm',
  'count/s': '/s',
  count_per_min: 'bpm',
  kcal: 'kcal',
  'kcal/(kg*hr)': 'kcal/kg·h',
  m: 'm',
  km: 'km',
  mi: 'mi',
  cm: 'cm',
  kg: 'kg',
  g: 'g',
  mg: 'mg',
  mcg: 'mcg',
  lb: 'lb',
  L: 'L',
  mL: 'mL',
  'L/min': 'L/min',
  '%': '%',
  degC: '°C',
  degF: '°F',
  mmHg: 'mmHg',
  'mg/dL': 'mg/dL',
  s: 's',
  min: 'min',
  hr: 'h',
  ms: 'ms',
  'm/s': 'm/s',
  'km/h': 'km/h',
  W: 'W',
  S: 'S',
  IU: 'IU',
  dBASPL: 'dB',
  appleEffortScore: '',
  'ml/(kg*min)': 'mL/kg·min',
  'mL/(kg*min)': 'mL/kg·min',
}

// The units whose readings are whole numbers: a heart rate of 74.3 is read
// as 74.
const WHOLE_NUMBER_UNITS = ['count', 'count/min', 'count_per_min', STEPS_UNIT]

export function unitLabel(unit: string): string {
  return unit in UNIT_LABELS ? UNIT_LABELS[unit] : unit
}

// displayUnit is the unit a type's numbers are written with. Steps are read
// with their word where a number stands alone, as a chart's headline does.
export function displayUnit(healthType: string, quantityUnit: string): string {
  return healthType === 'stepCount' && quantityUnit === 'count' ? STEPS_UNIT : quantityUnit
}

export function numberText(value: number, language: string, mostDigits?: number): string {
  const size = Math.abs(value)
  const digits = mostDigits ?? (size >= 100 ? 0 : size >= 10 ? 1 : 2)
  return new Intl.NumberFormat(language, { maximumFractionDigits: digits }).format(value)
}

// wholeNumberText is a count written for the language, such as 2,578 in
// English.
export function wholeNumberText(value: number, language: string): string {
  return new Intl.NumberFormat(language, { maximumFractionDigits: 0 }).format(value)
}

// shownValue is a value as a person reads it. HealthKit keeps a percentage
// as a fraction (0.97 for 97%), and so does the server.
export function shownValue(value: number, unit: string): number {
  return unit === '%' ? value * 100 : value
}

// A distance from this many meters up is written in kilometers.
const KILOMETER_FROM_METERS = 1000

export function quantityText(value: number, unit: string, language: string, t: Translate): string {
  if (unit === 'm' && Math.abs(value) >= KILOMETER_FROM_METERS) {
    return `${numberText(value / KILOMETER_FROM_METERS, language, 1)} km`
  }
  // A distance in kilometers has one decimal at most, as a long one in meters
  // does.
  const mostDigits = WHOLE_NUMBER_UNITS.includes(unit) ? 0 : unit === 'km' ? 1 : undefined
  const number = numberText(shownValue(value, unit), language, mostDigits)
  if (unit === STEPS_UNIT) return t('health.unitSteps', { number })
  const label = unitLabel(unit)
  if (label === '') return number
  return label.startsWith('/') || label === '%' || label.startsWith('°') ? number + label : `${number} ${label}`
}

export function durationText(seconds: number, t: Translate): string {
  const minutes = Math.round(seconds / 60)
  if (minutes < 1) return t('health.seconds', { seconds: Math.round(seconds) })
  if (minutes < 60) return t('health.minutes', { minutes })
  const hours = Math.floor(minutes / 60)
  return minutes % 60 === 0 ? t('health.hours', { hours }) : t('health.hoursMinutes', { hours, minutes: minutes % 60 })
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
// hours and minutes, a count as a whole number, a quantity with its unit.
export function amountText(
  value: number | null,
  summaryKind: SummaryKind,
  unit: string,
  language: string,
  t: Translate,
): string {
  if (value === null) return t('common.none')
  if (summaryKind === 'duration') return durationText(value, t)
  if (summaryKind === 'count') return numberText(value, language)
  return quantityText(value, unit, language, t)
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

export function workoutName(t: Translate, activityType: string): string {
  const key = `health.workout.${activityType}`
  return key in en ? t(key as Key) : humanize(activityType || 'other')
}

// humanize turns an identifier such as "traditionalStrengthTraining" into
// words.
export function humanize(identifier: string): string {
  const words = identifier.replace(/([a-z0-9])([A-Z])/g, '$1 $2').toLowerCase()
  return words.charAt(0).toUpperCase() + words.slice(1)
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

export function stageName(t: Translate, stage: string): string {
  const key = `health.stage.${stage}`
  return key in en ? t(key as Key) : humanize(stage)
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
    const readings = filled.reduce((count, period) => count + period.sampleCount, 0)
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
    const dayCount = filled.reduce((count, period) => count + summarizedDayCount(period, fromDay, toDay), 0)
    numbers.averagePerDay = dayCount > 0 ? numbers.total / dayCount : null
  }
  return numbers
}
