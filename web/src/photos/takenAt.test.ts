import { expect, it } from 'vitest'
import { browserOffsetMinutes, formatOffset, localFieldValue, takenAtFromField } from './takenAt'

it('writes an offset the way RFC 3339 does, with +00:00 for UTC and the half hours of other zones', () => {
  expect(formatOffset(0)).toBe('+00:00')
  expect(formatOffset(540)).toBe('+09:00')
  expect(formatOffset(-210)).toBe('-03:30')
  expect(formatOffset(345)).toBe('+05:45')
})

it('shows a moment as the wall clock at the offset shows it, to the minute', () => {
  expect(localFieldValue('2030-07-12T09:00:00Z', 0)).toBe('2030-07-12T09:00')
  expect(localFieldValue('2030-07-12T09:00:00Z', 540)).toBe('2030-07-12T18:00')
  // Across midnight, in both directions.
  expect(localFieldValue('2030-07-12T20:30:00Z', 540)).toBe('2030-07-13T05:30')
  expect(localFieldValue('2030-07-12T02:30:00Z', -300)).toBe('2030-07-11T21:30')
  expect(localFieldValue('not a date', 0)).toBe('')
})

it('writes the field as RFC 3339 at the offset, and refuses what is not a date and time', () => {
  expect(takenAtFromField('2030-07-13T07:30', 540)).toBe('2030-07-13T07:30:00+09:00')
  expect(takenAtFromField('2030-07-13T07:30:15', -210)).toBe('2030-07-13T07:30:15-03:30')
  expect(takenAtFromField('', 0)).toBeNull()
  expect(takenAtFromField('2030-07-13', 0)).toBeNull()
  expect(takenAtFromField('2030-13-45T07:30', 0)).toBeNull()
})

it('reads the browser’s offset east of UTC, the opposite sign of getTimezoneOffset', () => {
  const moment = new Date('2030-07-12T09:00:00Z')
  expect(browserOffsetMinutes(moment)).toBe(-moment.getTimezoneOffset())
})
