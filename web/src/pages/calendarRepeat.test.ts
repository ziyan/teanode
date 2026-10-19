import { expect, it } from 'vitest'

import { en } from '../i18n/en'
import { translate } from '../i18n/i18n'
import type { Key, Values } from '../i18n/i18n'
import {
  defaultParts,
  describeRepeat,
  editedRepeat,
  parseRepeat,
  presetOf,
  writeRepeat,
  type RepeatContext,
  type RepeatParts,
} from './calendarRepeat'

// The same table of rules is in TeaNodeKit's CalendarRepeatTests.swift; the
// two editors must read and write a rule alike.

// A Tuesday, in a zone that is eight hours behind UTC in winter.
const timed: RepeatContext = { startDate: '2026-10-20', timeZone: 'America/Los_Angeles', isAllDay: false }
const allDay: RepeatContext = { startDate: '2026-10-20', timeZone: '', isAllDay: true }

function parts(changes: Partial<RepeatParts>): RepeatParts {
  return { ...defaultParts('weekly', timed), ...changes }
}

type Row = { name: string; rule: string; context: RepeatContext; parts: RepeatParts }

const rows: Row[] = [
  { name: 'every day', rule: 'FREQ=DAILY', context: timed, parts: parts({ frequency: 'daily' }) },
  {
    name: 'every 3 days',
    rule: 'FREQ=DAILY;INTERVAL=3',
    context: timed,
    parts: parts({ frequency: 'daily', intervalCount: 3 }),
  },
  { name: 'every week, on the start weekday', rule: 'FREQ=WEEKLY', context: timed, parts: parts({ weekdays: [2] }) },
  {
    name: 'every weekday',
    rule: 'FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR',
    context: timed,
    parts: parts({ weekdays: [1, 2, 3, 4, 5] }),
  },
  {
    name: 'every 2 weeks on Monday, Wednesday and Friday until a date',
    rule: 'FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE,FR;UNTIL=20270101T075959Z',
    context: timed,
    parts: parts({ intervalCount: 2, weekdays: [1, 3, 5], end: { kind: 'date', endDate: '2026-12-31' } }),
  },
  {
    name: 'until a date in summer time',
    rule: 'FREQ=DAILY;UNTIL=20261101T065959Z',
    context: timed,
    parts: parts({ frequency: 'daily', end: { kind: 'date', endDate: '2026-10-31' } }),
  },
  {
    name: 'every week until a date, all day',
    rule: 'FREQ=WEEKLY;BYDAY=SA,SU;UNTIL=20261231',
    context: allDay,
    parts: parts({ weekdays: [6, 7], end: { kind: 'date', endDate: '2026-12-31' } }),
  },
  {
    name: 'every month, on the start day',
    rule: 'FREQ=MONTHLY',
    context: timed,
    parts: parts({ frequency: 'monthly', monthly: { kind: 'dayOfMonth', dayOfMonth: 20 } }),
  },
  {
    name: 'every month on day 15',
    rule: 'FREQ=MONTHLY;BYMONTHDAY=15',
    context: timed,
    parts: parts({ frequency: 'monthly', monthly: { kind: 'dayOfMonth', dayOfMonth: 15 } }),
  },
  {
    name: 'every month on the second Tuesday',
    rule: 'FREQ=MONTHLY;BYDAY=2TU',
    context: timed,
    parts: parts({ frequency: 'monthly', monthly: { kind: 'ordinalWeekday', ordinal: 2, weekday: 2 } }),
  },
  {
    name: 'every 3 months on the last Friday',
    rule: 'FREQ=MONTHLY;INTERVAL=3;BYDAY=-1FR',
    context: timed,
    parts: parts({
      frequency: 'monthly',
      intervalCount: 3,
      monthly: { kind: 'ordinalWeekday', ordinal: -1, weekday: 5 },
    }),
  },
  { name: 'every year', rule: 'FREQ=YEARLY', context: timed, parts: parts({ frequency: 'yearly' }) },
  {
    name: 'ten times',
    rule: 'FREQ=DAILY;COUNT=10',
    context: timed,
    parts: parts({ frequency: 'daily', end: { kind: 'count', occurrenceCount: 10 } }),
  },
]

for (const row of rows) {
  it(`reads and writes ${row.name}`, () => {
    expect(parseRepeat(row.rule, row.context)).toEqual(row.parts)
    expect(writeRepeat(row.parts, row.context)).toBe(row.rule)
  })
}

it('reads lower case and an interval of one, and writes them plainly', () => {
  const read = parseRepeat('freq=weekly;interval=1;byday=mo,we', timed)
  expect(read).toEqual(parts({ weekdays: [1, 3] }))
  expect(writeRepeat(read as RepeatParts, timed)).toBe('FREQ=WEEKLY;BYDAY=MO,WE')
})

it('has nothing to read in an empty rule or in text that is not a rule', () => {
  expect(parseRepeat('', timed)).toBeNull()
  expect(parseRepeat('   ', timed)).toBeNull()
  expect(parseRepeat('FREQ=HOURLY', timed)).toBeNull()
  expect(parseRepeat('FREQ=DAILY;INTERVAL=0', timed)).toBeNull()
  expect(parseRepeat('FREQ=DAILY;COUNT=x', timed)).toBeNull()
  expect(parseRepeat('FREQ=WEEKLY;BYDAY=XX', timed)).toBeNull()
  expect(parseRepeat('every other tuesday', timed)).toBeNull()
})

it('shows what it can of a rule with parts it cannot show, and keeps the rule', () => {
  const rule = 'FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1'
  const read = parseRepeat(rule, timed) as RepeatParts
  expect(read.hasUnshownParts).toBe(true)
  expect(read.frequency).toBe('monthly')
  // Left as it was shown, the rule is kept exactly.
  expect(editedRepeat(rule, read, read, timed)).toBe(rule)
  // Changed, it becomes what the editor shows.
  expect(editedRepeat(rule, read, { ...read, intervalCount: 2 }, timed)).toBe('FREQ=MONTHLY;INTERVAL=2')
  // A rule the editor cannot read at all is kept the same way.
  const unreadable = defaultParts('weekly', timed)
  expect(editedRepeat('FREQ=SECONDLY', unreadable, unreadable, timed)).toBe('FREQ=SECONDLY')
})

it('marks the other parts it does not show', () => {
  for (const rule of [
    'FREQ=YEARLY;BYMONTH=10',
    'FREQ=MONTHLY;BYMONTHDAY=1,15',
    'FREQ=MONTHLY;BYMONTHDAY=-1',
    'FREQ=WEEKLY;WKST=SU',
    'FREQ=DAILY;BYHOUR=9',
    'FREQ=WEEKLY;BYDAY=1MO',
    'FREQ=DAILY;COUNT=3;UNTIL=20261231',
  ]) {
    expect(parseRepeat(rule, timed)?.hasUnshownParts, rule).toBe(true)
  }
})

it('names the presets, whatever order another program wrote them in', () => {
  expect(presetOf('')).toBe('never')
  expect(presetOf('FREQ=DAILY')).toBe('daily')
  expect(presetOf('freq=weekly')).toBe('weekly')
  expect(presetOf('FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR')).toBe('weekdays')
  expect(presetOf('BYDAY=FR,MO,TU,WE,TH;FREQ=WEEKLY')).toBe('weekdays')
  expect(presetOf('FREQ=MONTHLY')).toBe('monthly')
  expect(presetOf('FREQ=YEARLY')).toBe('yearly')
  expect(presetOf('FREQ=WEEKLY;INTERVAL=2')).toBe('custom')
  expect(presetOf('FREQ=WEEKLY;BYDAY=MO,TU,WE,TH')).toBe('custom')
})

const english = {
  language: 'en' as const,
  t: (key: Key, values?: Values) => translate(en, key, values),
  plural: (count: number, forms: { one: Key; other: Key }, values?: Values) =>
    translate(en, new Intl.PluralRules('en').select(count) === 'one' ? forms.one : forms.other, {
      count,
      ...values,
    }),
}

it('describes a rule in a sentence', () => {
  const sentence = (rule: string, context: RepeatContext = timed) =>
    describeRepeat(parseRepeat(rule, context) as RepeatParts, english)
  expect(sentence('FREQ=DAILY')).toBe('Repeats every day')
  expect(sentence('FREQ=DAILY;INTERVAL=3')).toBe('Repeats every 3 days')
  expect(sentence('FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR')).toBe(
    'Repeats every week on Monday, Tuesday, Wednesday, Thursday, and Friday',
  )
  expect(sentence('FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE,FR;UNTIL=20270101T075959Z')).toBe(
    'Repeats every 2 weeks on Monday, Wednesday, and Friday, until December 31, 2026',
  )
  expect(sentence('FREQ=MONTHLY;BYMONTHDAY=15')).toBe('Repeats every month on day 15')
  expect(sentence('FREQ=MONTHLY;BYDAY=2TU')).toBe('Repeats every month on the second Tuesday')
  expect(sentence('FREQ=MONTHLY;INTERVAL=3;BYDAY=-1FR')).toBe('Repeats every 3 months on the last Friday')
  expect(sentence('FREQ=YEARLY')).toBe('Repeats every year')
  expect(sentence('FREQ=DAILY;COUNT=10')).toBe('Repeats every day, 10 times')
  expect(sentence('FREQ=DAILY;COUNT=1')).toBe('Repeats every day, 1 time')
  expect(sentence('FREQ=WEEKLY;UNTIL=20261231', allDay)).toBe('Repeats every week on Tuesday, until December 31, 2026')
})

it('writes the end of the chosen day in the event zone for a timed event', () => {
  const winter = writeRepeat(parts({ end: { kind: 'date', endDate: '2026-12-31' } }), timed)
  expect(winter).toContain('UNTIL=20270101T075959Z')
  const summer = writeRepeat(parts({ end: { kind: 'date', endDate: '2026-07-01' } }), timed)
  expect(summer).toContain('UNTIL=20260702T065959Z')
  const east = writeRepeat(parts({ end: { kind: 'date', endDate: '2026-12-31' } }), {
    ...timed,
    timeZone: 'Asia/Tokyo',
  })
  expect(east).toContain('UNTIL=20261231T145959Z')
})
