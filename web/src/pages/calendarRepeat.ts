import { weekdayName } from '../components/weekdayButtons'
import type { Key, Translation, Values } from '../i18n/i18n'

// How an event repeats, read from and written as an RFC 5545 recurrence rule
// (the RRULE text without its property name, such as
// "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE,FR;UNTIL=20261231T235959Z").
//
// The server takes any rule as text and expands it, so the editor only has to
// build and read the parts a person can choose. The iPhone app has the same
// logic in TeaNodeKit (CalendarRepeat.swift), tested against the same table
// of rules; change them together.

export type RepeatFrequency = 'daily' | 'weekly' | 'monthly' | 'yearly'

// A day of the week is a number from 1 (Monday) to 7 (Sunday), as the morning
// brief's days are.
export const WEEKDAY_NUMBERS = [1, 2, 3, 4, 5, 6, 7]

const WEEKDAY_CODES = ['MO', 'TU', 'WE', 'TH', 'FR', 'SA', 'SU']

// The ordinals a month can name a weekday by: the first to the fifth, and the
// last, which is -1 in a rule.
export const MONTHLY_ORDINALS = [1, 2, 3, 4, 5, -1]

const ORDINAL_KEYS: Record<number, Key> = {
  1: 'calendar.repeatOrdinal.first',
  2: 'calendar.repeatOrdinal.second',
  3: 'calendar.repeatOrdinal.third',
  4: 'calendar.repeatOrdinal.fourth',
  5: 'calendar.repeatOrdinal.fifth',
  [-1]: 'calendar.repeatOrdinal.last',
}

export type RepeatEnd =
  | { kind: 'never' }
  // The last day it may happen on, a date in the event's own zone.
  | { kind: 'date'; endDate: string }
  | { kind: 'count'; occurrenceCount: number }

export type RepeatMonthly =
  { kind: 'dayOfMonth'; dayOfMonth: number } | { kind: 'ordinalWeekday'; ordinal: number; weekday: number }

export type RepeatParts = {
  frequency: RepeatFrequency
  intervalCount: number
  // Weekly only: the days it happens on, in order.
  weekdays: number[]
  // Monthly only.
  monthly: RepeatMonthly
  end: RepeatEnd
  // The rule has something the editor cannot show (BYSETPOS, BYMONTH, several
  // days of a month, a time of day). It is kept as it is unless the person
  // changes what the editor does show.
  hasUnshownParts: boolean
}

// What a rule is read against: the event's own first day, the zone its days
// are counted in, and whether it is a whole-day event, which writes its end
// as a date and not as a moment.
export type RepeatContext = {
  startDate: string
  timeZone: string
  isAllDay: boolean
}

export type RepeatPreset = 'never' | 'daily' | 'weekdays' | 'weekly' | 'monthly' | 'yearly' | 'custom'

const PRESET_RULES: Record<Exclude<RepeatPreset, 'custom'>, string> = {
  never: '',
  daily: 'FREQ=DAILY',
  weekdays: 'FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR',
  weekly: 'FREQ=WEEKLY',
  monthly: 'FREQ=MONTHLY',
  yearly: 'FREQ=YEARLY',
}

export function presetRule(preset: Exclude<RepeatPreset, 'custom'>): string {
  return PRESET_RULES[preset]
}

// The rule with its parts in a fixed order and its days sorted, so that the
// same repeat written by another program in another order is still the same
// repeat.
function normalizedRule(rule: string): string {
  const entries = rule
    .trim()
    .toUpperCase()
    .split(';')
    .map((entry) => entry.trim())
    .filter(Boolean)
    .map((entry) => {
      const [key, value = ''] = entry.split('=')
      if (key !== 'BYDAY') return `${key}=${value}`
      const days = value
        .split(',')
        .sort((first, second) => WEEKDAY_CODES.indexOf(first) - WEEKDAY_CODES.indexOf(second))
      return `${key}=${days.join(',')}`
    })
    .sort()
  return entries.join(';')
}

// presetOf names the rule if it is one the Repeat list offers by name.
export function presetOf(rule: string): RepeatPreset {
  const normalized = normalizedRule(rule)
  for (const [preset, presetText] of Object.entries(PRESET_RULES)) {
    if (normalizedRule(presetText) === normalized) return preset as RepeatPreset
  }
  return 'custom'
}

// weekdayOf is the day of the week of a date, 1 for Monday to 7 for Sunday,
// or 0 for text that is not a date.
export function weekdayOf(date: string): number {
  const at = dateOf(date)
  if (!at) return 0
  return ((at.getUTCDay() + 6) % 7) + 1
}

function dateOf(date: string): Date | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date)
  if (!match) return null
  const at = new Date(Date.UTC(Number(match[1]), Number(match[2]) - 1, Number(match[3])))
  return Number.isNaN(at.getTime()) ? null : at
}

function dayOfMonthOf(date: string): number {
  const at = dateOf(date)
  return at ? at.getUTCDate() : 1
}

// The ordinal of a date's weekday within its month: the second Tuesday is 2.
// The fifth, which not every month has, is "the last" instead, since that is
// what somebody who started on one means.
export function ordinalOf(date: string): number {
  const ordinal = Math.ceil(dayOfMonthOf(date) / 7)
  return ordinal === 5 ? -1 : ordinal
}

// The parts of a repeat that has just been chosen, such as the weekly one
// Custom opens on.
export function defaultParts(frequency: RepeatFrequency, context: RepeatContext): RepeatParts {
  const weekday = weekdayOf(context.startDate) || 1
  return {
    frequency,
    intervalCount: 1,
    weekdays: [weekday],
    monthly: { kind: 'dayOfMonth', dayOfMonth: dayOfMonthOf(context.startDate) },
    end: { kind: 'never' },
    hasUnshownParts: false,
  }
}

const KNOWN_KEYS = new Set(['FREQ', 'INTERVAL', 'BYDAY', 'BYMONTHDAY', 'COUNT', 'UNTIL'])

const FREQUENCIES: Record<string, RepeatFrequency> = {
  DAILY: 'daily',
  WEEKLY: 'weekly',
  MONTHLY: 'monthly',
  YEARLY: 'yearly',
}

// parseRepeat reads a rule into the parts the editor shows. It is null for an
// empty rule (it does not repeat) and for text that is not a rule it can
// read, which the caller keeps as it is.
export function parseRepeat(rule: string, context: RepeatContext): RepeatParts | null {
  const text = rule.trim().toUpperCase()
  if (!text) return null
  const values = new Map<string, string>()
  let hasUnshownParts = false
  for (const entry of text.split(';')) {
    if (!entry.trim()) continue
    const separator = entry.indexOf('=')
    if (separator <= 0) return null
    const key = entry.slice(0, separator).trim()
    if (values.has(key)) return null
    values.set(key, entry.slice(separator + 1).trim())
    if (!KNOWN_KEYS.has(key)) hasUnshownParts = true
  }
  const frequency = FREQUENCIES[values.get('FREQ') ?? '']
  if (!frequency) return null

  const parts = defaultParts(frequency, context)
  const intervalText = values.get('INTERVAL')
  if (intervalText !== undefined) {
    if (!/^\d+$/.test(intervalText) || Number(intervalText) < 1) return null
    parts.intervalCount = Number(intervalText)
  }

  const dayEntries = (values.get('BYDAY') ?? '').split(',').filter(Boolean)
  const dayPattern = /^([+-]?\d{1,2})?(MO|TU|WE|TH|FR|SA|SU)$/
  const days = dayEntries.map((entry) => dayPattern.exec(entry))
  if (days.some((day) => day === null)) return null
  const monthDayText = values.get('BYMONTHDAY')

  if (frequency === 'weekly') {
    if (days.length > 0) {
      if (days.some((day) => day?.[1])) {
        hasUnshownParts = true
      } else {
        parts.weekdays = days
          .map((day) => WEEKDAY_CODES.indexOf(day?.[2] ?? '') + 1)
          .sort((first, second) => first - second)
          .filter((day, index, all) => all.indexOf(day) === index)
      }
    }
    if (monthDayText !== undefined) hasUnshownParts = true
  } else if (frequency === 'monthly') {
    const hasMonthDay = monthDayText !== undefined
    if (days.length > 0 && hasMonthDay) {
      hasUnshownParts = true
    } else if (days.length === 1) {
      const ordinal = Number(days[0]?.[1] ?? '')
      if (days[0]?.[1] && MONTHLY_ORDINALS.includes(ordinal)) {
        parts.monthly = {
          kind: 'ordinalWeekday',
          ordinal,
          weekday: WEEKDAY_CODES.indexOf(days[0]?.[2] ?? '') + 1,
        }
      } else {
        hasUnshownParts = true
      }
    } else if (days.length > 1) {
      hasUnshownParts = true
    } else if (hasMonthDay) {
      if (/^\d{1,2}$/.test(monthDayText) && Number(monthDayText) >= 1 && Number(monthDayText) <= 31) {
        parts.monthly = { kind: 'dayOfMonth', dayOfMonth: Number(monthDayText) }
      } else {
        hasUnshownParts = true
      }
    }
  } else if (days.length > 0 || monthDayText !== undefined) {
    // A daily or yearly rule narrowed to some days is not a repeat this
    // editor can draw.
    hasUnshownParts = true
  }

  const countText = values.get('COUNT')
  const untilText = values.get('UNTIL')
  if (countText !== undefined) {
    if (!/^\d+$/.test(countText) || Number(countText) < 1) return null
    parts.end = { kind: 'count', occurrenceCount: Number(countText) }
    // A rule may have one or the other; with both, the count is shown and the
    // rule is kept as it is.
    if (untilText !== undefined) hasUnshownParts = true
  } else if (untilText !== undefined) {
    const endDate = endDateOf(untilText, context)
    if (!endDate) return null
    parts.end = { kind: 'date', endDate }
  }

  parts.hasUnshownParts = hasUnshownParts
  return parts
}

// writeRepeat is the rule for the parts: the plainest one that means them. A
// part that is what the event's own start already says (a weekly repeat on
// the weekday it starts on, a monthly one on the day it starts on) is left
// out, so that "every week" is written as every week.
export function writeRepeat(parts: RepeatParts, context: RepeatContext): string {
  const pieces: string[] = [`FREQ=${parts.frequency.toUpperCase()}`]
  if (parts.intervalCount > 1) pieces.push(`INTERVAL=${parts.intervalCount}`)
  if (parts.frequency === 'weekly') {
    const weekdays = [...new Set(parts.weekdays)].sort((first, second) => first - second)
    const startWeekday = weekdayOf(context.startDate)
    const isStartWeekdayOnly = weekdays.length === 1 && weekdays[0] === startWeekday
    if (weekdays.length > 0 && !isStartWeekdayOnly) {
      pieces.push(`BYDAY=${weekdays.map((day) => WEEKDAY_CODES[day - 1]).join(',')}`)
    }
  } else if (parts.frequency === 'monthly') {
    if (parts.monthly.kind === 'ordinalWeekday') {
      pieces.push(`BYDAY=${parts.monthly.ordinal}${WEEKDAY_CODES[parts.monthly.weekday - 1]}`)
    } else if (parts.monthly.dayOfMonth !== dayOfMonthOf(context.startDate)) {
      pieces.push(`BYMONTHDAY=${parts.monthly.dayOfMonth}`)
    }
  }
  if (parts.end.kind === 'count') {
    pieces.push(`COUNT=${parts.end.occurrenceCount}`)
  } else if (parts.end.kind === 'date') {
    pieces.push(`UNTIL=${untilOf(parts.end.endDate, context)}`)
  }
  return pieces.join(';')
}

export function isSameRepeat(first: RepeatParts, second: RepeatParts): boolean {
  return JSON.stringify(first) === JSON.stringify(second)
}

// editedRepeat is the rule to keep after the editor was opened on `original`,
// showing `initial`, and left showing `edited`. Nothing changed keeps the
// rule exactly as it was, so that opening the editor and pressing Done never
// loses what another program set (a part the editor cannot show, or a rule it
// cannot read at all).
export function editedRepeat(
  original: string,
  initial: RepeatParts,
  edited: RepeatParts,
  context: RepeatContext,
): string {
  if (isSameRepeat(initial, edited)) return original.trim()
  return writeRepeat({ ...edited, hasUnshownParts: false }, context)
}

// untilOf writes the last day as RFC 5545 asks: a date for a whole-day event,
// and for a timed one the end of that day in the event's zone, as a moment in
// UTC.
function untilOf(endDate: string, context: RepeatContext): string {
  if (context.isAllDay) return endDate.replace(/-/g, '')
  const at = endOfDay(endDate, context.timeZone)
  return at
    .toISOString()
    .replace(/[-:]/g, '')
    .replace(/\.\d{3}/, '')
}

// endDateOf reads an UNTIL back to the day it names in the event's zone.
function endDateOf(until: string, context: RepeatContext): string | null {
  const match = /^(\d{4})(\d{2})(\d{2})(?:T(\d{2})(\d{2})(\d{2})(Z)?)?$/.exec(until)
  if (!match) return null
  const [, year, month, day, hour, minute, second, zulu] = match
  if (!zulu || context.isAllDay) return `${year}-${month}-${day}`
  const at = new Date(
    Date.UTC(Number(year), Number(month) - 1, Number(day), Number(hour), Number(minute), Number(second)),
  )
  return dateInZone(at, context.timeZone)
}

// The offset of a zone from UTC at a moment, in minutes: how far its clocks
// are ahead.
function zoneOffsetMinutes(at: Date, timeZone: string): number {
  const fields = clockFields(at, timeZone)
  const wall = Date.UTC(fields.year, fields.month - 1, fields.day, fields.hour, fields.minute, fields.second)
  return Math.round((wall - Math.floor(at.getTime() / 1000) * 1000) / 60000)
}

function clockFields(at: Date, timeZone: string) {
  let formatter: Intl.DateTimeFormat
  try {
    formatter = zoneFormatter(timeZone)
  } catch {
    // A zone name this browser does not know is read as UTC rather than
    // refusing to save the event.
    formatter = zoneFormatter('UTC')
  }
  const fields: Record<string, number> = {}
  for (const part of formatter.formatToParts(at)) {
    if (part.type !== 'literal') fields[part.type] = Number(part.value)
  }
  return {
    year: fields.year,
    month: fields.month,
    day: fields.day,
    hour: fields.hour,
    minute: fields.minute,
    second: fields.second,
  }
}

function zoneFormatter(timeZone: string): Intl.DateTimeFormat {
  return new Intl.DateTimeFormat('en-US', {
    timeZone: timeZone || 'UTC',
    hourCycle: 'h23',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

function dateInZone(at: Date, timeZone: string): string {
  const fields = clockFields(at, timeZone)
  const month = `${fields.month}`.padStart(2, '0')
  const day = `${fields.day}`.padStart(2, '0')
  return `${fields.year}-${month}-${day}`
}

// endOfDay is the last second of a date in a zone, as a moment. The zone's
// offset is looked up twice, since the first guess can be on the other side of
// a clock change from the answer.
function endOfDay(date: string, timeZone: string): Date {
  const day = dateOf(date) ?? new Date(0)
  const wall = day.getTime() + (24 * 60 * 60 - 1) * 1000
  let moment = new Date(wall)
  for (let attempt = 0; attempt < 2; attempt++) {
    moment = new Date(wall - zoneOffsetMinutes(moment, timeZone) * 60000)
  }
  return moment
}

// listOf joins names the way the language joins a list: "Monday, Wednesday
// and Friday" in English, with its own conjunction and commas elsewhere.
export function listOf(names: string[], language: string): string {
  return new Intl.ListFormat(language, { style: 'long', type: 'conjunction' }).format(names)
}

function dateText(date: string, language: string): string {
  const at = dateOf(date)
  if (!at) return date
  return new Intl.DateTimeFormat(language, { dateStyle: 'long', timeZone: 'UTC' }).format(at)
}

// describeRepeat is the sentence for the parts, in the reader's language,
// with day names and dates from the browser's own formatters:
// "Repeats every 2 weeks on Monday, Wednesday, and Friday, until December 31, 2026".
export function describeRepeat(
  parts: RepeatParts,
  translation: Pick<Translation, 't' | 'plural' | 'language'>,
): string {
  const { t, plural, language } = translation
  const isEvery = parts.intervalCount > 1
  const interval = parts.intervalCount
  const pick = (single: Key, many: Key, values?: Values) => t(isEvery ? many : single, { interval, ...values })
  let sentence: string
  if (parts.frequency === 'daily') {
    sentence = pick('calendar.repeatSentence.daily', 'calendar.repeatSentence.dailyEvery')
  } else if (parts.frequency === 'yearly') {
    sentence = pick('calendar.repeatSentence.yearly', 'calendar.repeatSentence.yearlyEvery')
  } else if (parts.frequency === 'weekly') {
    const days = listOf(
      [...parts.weekdays].sort((first, second) => first - second).map((day) => weekdayName(day, language, 'long')),
      language,
    )
    sentence = pick('calendar.repeatSentence.weekly', 'calendar.repeatSentence.weeklyEvery', { days })
  } else if (parts.monthly.kind === 'dayOfMonth') {
    sentence = pick('calendar.repeatSentence.monthlyDay', 'calendar.repeatSentence.monthlyDayEvery', {
      day: parts.monthly.dayOfMonth,
    })
  } else {
    sentence = pick('calendar.repeatSentence.monthlyWeekday', 'calendar.repeatSentence.monthlyWeekdayEvery', {
      ordinal: ordinalName(parts.monthly.ordinal, t),
      weekday: weekdayName(parts.monthly.weekday, language, 'long'),
    })
  }
  if (parts.end.kind === 'date') {
    return t('calendar.repeatSentence.until', { sentence, date: dateText(parts.end.endDate, language) })
  }
  if (parts.end.kind === 'count') {
    return plural(
      parts.end.occurrenceCount,
      { one: 'calendar.repeatSentence.timesOne', other: 'calendar.repeatSentence.timesOther' },
      { sentence },
    )
  }
  return sentence
}

// ordinalName is "second" or "last", for the choice of which weekday of a
// month.
export function ordinalName(ordinal: number, t: Translation['t']): string {
  return t(ORDINAL_KEYS[ordinal] ?? 'calendar.repeatOrdinal.first')
}
