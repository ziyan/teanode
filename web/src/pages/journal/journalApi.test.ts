import { expect, it } from 'vitest'

import {
  addMonths,
  amountText,
  dayPath,
  durationText,
  hitPath,
  isDayText,
  isMonthText,
  isYearText,
  momentPaths,
  monthLength,
  narrativeParagraphs,
  periodPath,
  temperatureText,
  textParagraphs,
  usesImperialUnits,
  weekStartOf,
  weekdayIndex,
} from './journalApi'

// The small calculations the pages make. The words are the English catalog's.
const t = (key: string, values?: Record<string, string | number>) =>
  ({
    'journal.durationMinutes': '{minutes} min',
    'journal.durationHours': '{hours} h',
    'journal.durationHoursMinutes': '{hours} h {minutes} min',
  })[key]!.replace(/\{(\w+)\}/g, (_, name: string) => String(values?.[name]))

it('splits a story at its blank lines into words and mentions', () => {
  const paragraphs = narrativeParagraphs(
    'You were at the [[agent_document:doc-1]] at noon.\n\n  \nThen [[calendar_occurrence:event-1@2026-10-22T09:30:00Z]] and [[photo:p-2]].',
  )
  expect(paragraphs).toHaveLength(2)
  expect(paragraphs[0]).toEqual([
    { isReference: false, text: 'You were at the ' },
    { isReference: true, reference: 'agent_document:doc-1' },
    { isReference: false, text: ' at noon.' },
  ])
  expect(
    paragraphs[1].filter((piece) => piece.isReference).map((piece) => (piece as { reference: string }).reference),
  ).toEqual(['calendar_occurrence:event-1@2026-10-22T09:30:00Z', 'photo:p-2'])
})

it('leaves brackets that are not a mention as they are', () => {
  expect(narrativeParagraphs('A [[note]] and [[ x:y]] and [one:two].')).toEqual([
    [{ isReference: false, text: 'A [[note]] and [[ x:y]] and [one:two].' }],
  ])
})

it('splits the person’s words into paragraphs', () => {
  expect(textParagraphs('one\ntwo\n\nthree\n\n\n')).toEqual(['one\ntwo', 'three'])
})

it('knows which of a week’s days is the first, by the day a week starts on', () => {
  // 2026-10-22 is a Thursday.
  expect(weekStartOf('2026-10-22', 'monday')).toBe('2026-10-19')
  expect(weekStartOf('2026-10-22', 'sunday')).toBe('2026-10-18')
  expect(weekStartOf('2026-10-19', 'monday')).toBe('2026-10-19')
  expect(weekStartOf('2026-10-18', 'monday')).toBe('2026-10-12')
  expect(weekdayIndex('2026-10-25', 'monday')).toBe(6)
  expect(weekdayIndex('2026-10-25', 'sunday')).toBe(0)
  // A week that crosses a year.
  expect(weekStartOf('2027-01-01', 'monday')).toBe('2026-12-28')
})

it('moves a month and counts its days', () => {
  expect(addMonths('2026-10', 1)).toBe('2026-11')
  expect(addMonths('2026-01', -1)).toBe('2025-12')
  expect(addMonths('2026-12', 14)).toBe('2028-02')
  expect(monthLength('2028-02')).toBe(29)
  expect(monthLength('2026-02')).toBe(28)
})

it('accepts only dates, months and years that exist', () => {
  expect(isDayText('2026-10-22')).toBe(true)
  expect(isDayText('2026-02-30')).toBe(false)
  expect(isDayText('yesterday')).toBe(false)
  expect(isDayText(undefined)).toBe(false)
  expect(isMonthText('2026-10')).toBe(true)
  expect(isMonthText('2026-13')).toBe(false)
  expect(isYearText('2026')).toBe(true)
  expect(isYearText('26')).toBe(false)
})

it('names the pages of a day and of the periods around it', () => {
  expect(dayPath('2026-10-22')).toBe('/journal/2026-10-22')
  expect(periodPath('week', '2026-10-22')).toBe('/journal/week/2026-10-22')
  expect(periodPath('month', '2026-10-22')).toBe('/journal/month/2026-10')
  expect(periodPath('year', '2026-10-22')).toBe('/journal/year/2026')
  expect(hitPath({ day: '2026-10-19', periodKind: 'week', headline: '', snippet: '' })).toBe('/journal/week/2026-10-19')
  expect(hitPath({ day: '2026-10-22', periodKind: '', headline: '', snippet: '' })).toBe('/journal/2026-10-22')
})

it('writes durations as hours and minutes', () => {
  expect(durationText(40, t)).toBe('40 min')
  expect(durationText(120, t)).toBe('2 h')
  expect(durationText(370, t)).toBe('6 h 10 min')
  expect(durationText(-5, t)).toBe('0 min')
})

it('writes money by the places its currency has, not by hundredths', () => {
  expect(amountText(8640, 'USD', 'en')).toBe('$86.40')
  expect(amountText(1500, 'JPY', 'en')).toBe('¥1,500')
  expect(amountText(5, '', 'en')).toBe('$0.05')
})

it('uses degrees Fahrenheit and miles only where the locale does', () => {
  expect(usesImperialUnits('en-US')).toBe(true)
  expect(usesImperialUnits('en')).toBe(true)
  expect(usesImperialUnits('en-GB')).toBe(false)
  expect(usesImperialUnits('ja-JP')).toBe(false)
  expect(usesImperialUnits('zh-CN')).toBe(false)
  expect(usesImperialUnits('not a locale')).toBe(false)
  expect(temperatureText(14.4, true)).toBe('58°')
  expect(temperatureText(14.4, false)).toBe('14°')
})

it('finds the page the server gave for a record of the day', () => {
  const found = momentPaths([
    { at: '', recordKind: 'agent_document', recordId: 'a', title: '', detail: '', dashboardPath: '/coding' },
    { at: '', recordKind: 'photo', recordId: 'b', title: '', detail: '', dashboardPath: 'https://elsewhere.example/b' },
  ])
  expect(found.get('agent_document:a')).toBe('/coding')
  expect(found.has('photo:b')).toBe(false)
})
