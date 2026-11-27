import { expect, it } from 'vitest'

import { feedClockOf, feedClockTextOf, writtenOffsetMinutes, zoneNameOf } from './feedClock'

it('reads the offset a time was written with', () => {
  expect(writtenOffsetMinutes('2030-03-04T14:02:11+09:00')).toBe(540)
  expect(writtenOffsetMinutes('2030-03-04T14:02:11-05:30')).toBe(-330)
  expect(writtenOffsetMinutes('2030-03-04T14:02:11Z')).toBeNull()
  expect(writtenOffsetMinutes('2030-03-04T14:02:11+00:00')).toBe(0)
  expect(writtenOffsetMinutes('2030-03-04T14:02:11')).toBeNull()
})

it('names a zone as UTC with its offset', () => {
  expect(zoneNameOf(0)).toBe('UTC')
  expect(zoneNameOf(540)).toBe('UTC+9')
  expect(zoneNameOf(-300)).toBe('UTC-5')
  expect(zoneNameOf(330)).toBe('UTC+5:30')
})

it('says a time in the zone it was written in and names it when it is not the viewer', () => {
  // The viewer is at UTC-5; the time was written at UTC+9.
  const other = feedClockOf('2030-03-04T14:02:11+09:00', { isSecondShown: true, isHourPadded: true }, -300)
  expect(other?.zoneName).toBe('UTC+9')
  expect(other?.clockText).toMatch(/^0?2:02:11/)
  expect(other?.clockText).not.toMatch(/12:02/)
})

it('leaves the zone out when the time was written in the viewer zone', () => {
  const same = feedClockOf('2030-03-04T14:02:11+09:00', { isSecondShown: true }, 540)
  expect(same?.zoneName).toBe('')
  expect(feedClockTextOf('2030-03-04T14:02:11+09:00', { isSecondShown: true }, 540)).toMatch(/2:02:11/)
  expect(feedClockTextOf('2030-03-04T14:02:11+09:00', {}, 540)).not.toContain('UTC')
})

it('says nothing for a time that is not one', () => {
  expect(feedClockOf('soon')).toBeNull()
  expect(feedClockTextOf('soon')).toBe('')
})

it('shows the hour alone for a forecast hour', () => {
  const text = feedClockTextOf('2030-03-04T15:00:00+09:00', { isHourOnly: true }, -300)
  expect(text).toMatch(/UTC\+9$/)
  expect(text).not.toMatch(/:00/)
})
