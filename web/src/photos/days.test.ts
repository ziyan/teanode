import { describe, expect, it } from 'vitest'
import {
  dayHeading,
  dayKey,
  formatByteSize,
  formatDate,
  formatDateRange,
  formatDuration,
  firstSentence,
  formatOffset,
  groupByDay,
  indexRange,
  photoAltText,
  photoDateTime,
} from './days'

const photoAt = (
  takenAt: string | null,
  createdAt = '2030-01-01T00:00:00Z',
  takenAtOffsetMinutes: number | null = null,
) => ({
  takenAt,
  createdAt,
  takenAtOffsetMinutes,
})

describe('days', () => {
  it('puts the same moment on different days in different time zones', () => {
    const moment = new Date('2030-07-12T23:30:00Z')
    expect(dayKey(moment, 'UTC')).toBe('2030-07-12')
    expect(dayKey(moment, 'Asia/Tokyo')).toBe('2030-07-13')
    expect(dayKey(moment, 'America/Los_Angeles')).toBe('2030-07-12')
  })

  it('writes a heading with the weekday in the reader language', () => {
    const moment = new Date('2030-07-13T12:00:00Z')
    expect(dayHeading(moment, 'en', 'UTC')).toBe('Saturday, July 13, 2030')
    expect(dayHeading(moment, 'ja', 'UTC')).toContain('2030')
    expect(dayHeading(moment, 'ja', 'UTC')).toContain('土曜日')
  })

  it('groups newest first photos into runs of one day', () => {
    const photos = [
      photoAt('2030-07-13T01:00:00Z'),
      photoAt('2030-07-12T23:00:00Z'),
      photoAt('2030-07-12T02:00:00Z'),
      photoAt('2030-07-10T09:00:00Z'),
    ]
    const days = groupByDay(photos, 'en', 'UTC')
    expect(days.map((day) => [day.key, day.photos.length])).toEqual([
      ['2030-07-13', 1],
      ['2030-07-12', 2],
      ['2030-07-10', 1],
    ])
  })

  it('moves photos across the day line with the time zone', () => {
    const photos = [photoAt('2030-07-13T01:00:00Z'), photoAt('2030-07-12T23:00:00Z')]
    expect(groupByDay(photos, 'en', 'UTC')).toHaveLength(2)
    expect(groupByDay(photos, 'en', 'Asia/Tokyo')).toHaveLength(1)
  })

  it('uses the wall clock where the photo was taken when the offset is known', () => {
    // 23:30 at +09:00 is 14:30 UTC the same day; 07:30 at +09:00 is 22:30 UTC
    // the day before. Both are the 12th where they were taken, though UTC
    // (and Los Angeles) would split them across two dates.
    const evening = photoAt('2030-07-12T14:30:00Z', undefined, 540)
    const morning = photoAt('2030-07-11T22:30:00Z', undefined, 540)
    const days = groupByDay([evening, morning], 'en', 'America/Los_Angeles')
    expect(days.map((day) => day.key)).toEqual(['2030-07-12'])
    expect(days[0].photos).toHaveLength(2)
  })

  it('keeps a late photo on its own local day when UTC has already turned over', () => {
    // Taken 23:30 at +09:00 on the 12th, which is 14:30 UTC on the 12th; and
    // 00:30 at +09:00 on the 13th, which is 15:30 UTC on the 12th.
    const lateOnTwelfth = photoAt('2030-07-12T14:30:00Z', undefined, 540)
    const earlyOnThirteenth = photoAt('2030-07-12T15:30:00Z', undefined, 540)
    const days = groupByDay([earlyOnThirteenth, lateOnTwelfth], 'en', 'UTC')
    expect(days.map((day) => day.key)).toEqual(['2030-07-13', '2030-07-12'])
  })

  it('reads a photo with no offset in the browser zone', () => {
    const photo = photoAt('2030-07-12T23:30:00Z')
    expect(groupByDay([photo], 'en', 'Asia/Tokyo')[0].key).toBe('2030-07-13')
    expect(groupByDay([photo], 'en', 'UTC')[0].key).toBe('2030-07-12')
  })

  it('writes the time of a photo on the clock where it was taken', () => {
    const photo = photoAt('2030-07-12T14:30:00Z', undefined, 540)
    const text = photoDateTime(photo, 'en', 'America/Los_Angeles')
    expect(text).toContain('Friday, July 12, 2030')
    expect(text).toContain('11:30')
    expect(text).toContain('PM')
    expect(text).toContain('UTC+09:00')
  })

  it('writes an offset', () => {
    expect(formatOffset(540)).toBe('UTC+09:00')
    expect(formatOffset(-210)).toBe('UTC-03:30')
    expect(formatOffset(0)).toBe('UTC+00:00')
  })

  it('places a photo with no date taken at the day it arrived', () => {
    const days = groupByDay([photoAt(null, '2030-03-04T10:00:00Z')], 'en', 'UTC')
    expect(days[0].key).toBe('2030-03-04')
  })

  it('writes the length of a video as m:ss and h:mm:ss', () => {
    expect(formatDuration(0)).toBe('0:00')
    expect(formatDuration(4000)).toBe('0:04')
    expect(formatDuration(4999)).toBe('0:04')
    expect(formatDuration(65_000)).toBe('1:05')
    expect(formatDuration(3_725_000)).toBe('1:02:05')
    expect(formatDuration(-5)).toBe('0:00')
  })

  it('writes a size the way a person says one', () => {
    expect(formatByteSize(0)).toBe('0 B')
    expect(formatByteSize(1023)).toBe('1023 B')
    expect(formatByteSize(1536)).toBe('1.5 KB')
    expect(formatByteSize(13_051_515)).toBe('12.4 MB')
    expect(formatByteSize(8_589_934_592)).toBe('8 GB')
  })

  it('counts a range of cells in either direction', () => {
    expect(indexRange(2, 5)).toEqual([2, 3, 4, 5])
    expect(indexRange(5, 2)).toEqual([2, 3, 4, 5])
    expect(indexRange(3, 3)).toEqual([3])
  })

  it('writes the dates an album spans as a range, or one date for one day', () => {
    // Newer ICU puts thin spaces round the dash; they read the same.
    const range = formatDateRange('2030-07-12T09:00:00Z', '2030-07-14T18:00:00Z', 'en', 'UTC')
    expect(range.replace(/[\u2009\u202f]/g, ' ')).toBe('Jul 12 – 14, 2030')
    expect(formatDateRange('2030-07-12T09:00:00Z', '2030-07-12T18:00:00Z', 'en', 'UTC')).toBe('Jul 12, 2030')
    expect(formatDateRange(null, null, 'en', 'UTC')).toBe('')
  })

  it('writes one moment as a date in the zone it is read in', () => {
    expect(formatDate('2030-07-12T23:30:00Z', 'en', 'UTC')).toBe('July 12, 2030')
    expect(formatDate('2030-07-12T23:30:00Z', 'en', 'Asia/Tokyo')).toBe('July 13, 2030')
    expect(formatDate('not a date', 'en', 'UTC')).toBe('')
  })
})

describe('alternative text', () => {
  const words = {
    placeAndDate: (place: string, date: string) => `${place} on ${date}`,
    dateOnly: (date: string) => `taken ${date}`,
  }
  const photo = { takenAt: '2030-07-12T09:00:00Z', createdAt: '2030-07-13T00:00:00Z', takenAtOffsetMinutes: 0 }

  it('is the first sentence of the description', () => {
    expect(firstSentence('A red kite over a field. It is windy. Nobody is there.')).toBe('A red kite over a field.')
    expect(firstSentence('A lens of 3.5 mm. Wide.')).toBe('A lens of 3.5 mm.')
    expect(firstSentence('  one\n\nline   only  ')).toBe('one line only')
    expect(firstSentence('')).toBe('')
  })

  it('ends a sentence at the full stop of Chinese and Japanese, which no space follows', () => {
    expect(firstSentence('一只红色的风筝飞过田野。风很大。')).toBe('一只红色的风筝飞过田野。')
    expect(firstSentence('赤い凧が野原の上を飛んでいます。風が強いです。')).toBe('赤い凧が野原の上を飛んでいます。')
  })

  it('cuts a very long sentence at a word', () => {
    const long = `${'word '.repeat(100)}end.`
    const cut = firstSentence(long)
    expect(cut.length).toBeLessThanOrEqual(161)
    expect(cut.endsWith('…')).toBe(true)
    expect(cut).not.toMatch(/wor…$/)
  })

  it('falls back to the place and the date, then to the date', () => {
    const named = { ...photo, placeName: 'Fixtureville' }
    expect(photoAltText(named, 'A kite. More.', 'en', words)).toBe('A kite.')
    expect(photoAltText(named, '', 'en', words)).toBe('Fixtureville on July 12, 2030')
    expect(photoAltText({ ...photo, placeName: '' }, '  ', 'en', words)).toBe('taken July 12, 2030')
  })
})
