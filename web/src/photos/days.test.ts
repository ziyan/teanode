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
  groupByTrashDay,
  indexRange,
  photoAltText,
  photoDateTime,
  photoDayRange,
  photoViewerTitle,
  trashDaysLeft,
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

  it('writes the days the photos span as one range', () => {
    const photos = [photoAt('2030-07-06T01:00:00Z'), photoAt('2030-07-04T23:00:00Z'), photoAt('2030-07-03T02:00:00Z')]
    expect(photoDayRange(photos, 'en', 'UTC').replace(/\s/g, ' ')).toBe('Jul 3 \u2013 6, 2030')
  })

  it('writes one date when every photo is on one day, and nothing for none', () => {
    const photos = [photoAt('2030-07-12T01:00:00Z'), photoAt('2030-07-12T23:00:00Z')]
    expect(photoDayRange(photos, 'en', 'UTC')).toBe('Jul 12, 2030')
    expect(photoDayRange([], 'en', 'UTC')).toBe('')
  })

  it('writes a range that crosses a month and a year with both ends said', () => {
    const photos = [photoAt('2031-01-02T10:00:00Z'), photoAt('2030-12-30T10:00:00Z')]
    const text = photoDayRange(photos, 'en', 'UTC')
    expect(text).toContain('Dec 30, 2030')
    expect(text).toContain('Jan 2, 2031')
  })

  it('finds the first and last day whatever order the photos are in', () => {
    const photos = [photoAt('2030-07-04T10:00:00Z'), photoAt('2030-07-09T10:00:00Z'), photoAt('2030-07-06T10:00:00Z')]
    expect(photoDayRange(photos, 'en', 'UTC').replace(/\s/g, ' ')).toBe('Jul 4 \u2013 9, 2030')
  })

  it('moves a photo across the day line with the time zone', () => {
    const photos = [photoAt('2030-07-13T01:00:00Z'), photoAt('2030-07-12T23:00:00Z')]
    expect(photoDayRange(photos, 'en', 'UTC').replace(/\s/g, ' ')).toBe('Jul 12 \u2013 13, 2030')
    expect(photoDayRange(photos, 'en', 'Asia/Tokyo')).toBe('Jul 13, 2030')
  })

  it('reads the days on the clock where each photo was taken when the offset is known', () => {
    // 23:30 at +09:00 is 14:30 UTC the same day; 07:30 at +09:00 is 22:30 UTC
    // the day before. Both are the 12th where they were taken, though UTC
    // (and Los Angeles) would split them across two dates.
    const evening = photoAt('2030-07-12T14:30:00Z', undefined, 540)
    const morning = photoAt('2030-07-11T22:30:00Z', undefined, 540)
    expect(photoDayRange([evening, morning], 'en', 'America/Los_Angeles')).toBe('Jul 12, 2030')
  })

  it('keeps a late photo on its own local day when UTC has already turned over', () => {
    // Taken 23:30 at +09:00 on the 12th, which is 14:30 UTC on the 12th; and
    // 00:30 at +09:00 on the 13th, which is 15:30 UTC on the 12th.
    const lateOnTwelfth = photoAt('2030-07-12T14:30:00Z', undefined, 540)
    const earlyOnThirteenth = photoAt('2030-07-12T15:30:00Z', undefined, 540)
    expect(photoDayRange([earlyOnThirteenth, lateOnTwelfth], 'en', 'UTC').replace(/\s/g, ' ')).toBe(
      'Jul 12 \u2013 13, 2030',
    )
  })

  it('places a photo with no date taken where it arrived', () => {
    expect(photoDayRange([photoAt(null, '2030-03-05T08:00:00Z')], 'en', 'UTC')).toBe('Mar 5, 2030')
  })

  it('titles the viewer with the day over the time, the year only when it is not this one', () => {
    const photo = photoAt('2030-07-12T19:52:00Z', undefined, 0)
    const sameYear = photoViewerTitle(photo, 'en', 'UTC', new Date('2030-12-01T00:00:00Z'))
    expect(sameYear.dateText).toBe('July 12')
    expect(sameYear.timeText).toBe('7:52 PM')
    expect(sameYear.isoText).toBe('2030-07-12T19:52')
    const otherYear = photoViewerTitle(photo, 'en', 'UTC', new Date('2031-01-02T00:00:00Z'))
    expect(otherYear.dateText).toBe('July 12, 2030')
  })

  it('titles the viewer on the clock where the photo was taken', () => {
    // 19:52 at +09:00 is 10:52 UTC; a reader in Los Angeles still sees 19:52.
    const photo = photoAt('2030-07-12T10:52:00Z', undefined, 540)
    const title = photoViewerTitle(photo, 'en', 'America/Los_Angeles', new Date('2030-07-20T00:00:00Z'))
    expect(title.dateText).toBe('July 12')
    expect(title.timeText).toBe('7:52 PM')
    expect(title.isoText).toBe('2030-07-12T19:52')
  })

  it('titles the viewer in the language of the reader', () => {
    const photo = photoAt('2030-07-12T09:05:00Z', undefined, 0)
    const title = photoViewerTitle(photo, 'ja', 'UTC', new Date('2030-07-20T00:00:00Z'))
    expect(title.dateText).toBe('7月12日')
    expect(title.timeText).toContain('9:05')
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

describe('the Trash', () => {
  const now = new Date('2030-07-20T12:00:00Z')

  it('says how many whole days a photo has left, and none when the server keeps it for no set time', () => {
    expect(trashDaysLeft('2030-07-20T08:00:00Z', 30, now)).toBe(30)
    expect(trashDaysLeft('2030-07-10T08:00:00Z', 30, now)).toBe(20)
    expect(trashDaysLeft('2030-06-01T08:00:00Z', 30, now)).toBe(0)
    expect(trashDaysLeft('2030-07-10T08:00:00Z', 0, now)).toBeNull()
    expect(trashDaysLeft(null, 30, now)).toBeNull()
  })

  it('groups photos under the day they were trashed, in the order the server listed them', () => {
    const photos = [
      { id: 'b', trashedAt: '2030-07-19T08:00:00Z' },
      { id: 'c', trashedAt: '2030-07-10T09:00:00Z' },
      { id: 'a', trashedAt: '2030-07-10T08:00:00Z' },
    ]
    const days = groupByTrashDay(photos, 30, now, 'UTC')
    expect(days.map((day) => day.photos.map((photo) => photo.id))).toEqual([['b'], ['c', 'a']])
    expect(days.map((day) => day.daysLeft)).toEqual([29, 20])
  })

  it('says how long a day has as its oldest photo has, which goes first', () => {
    // Trashed late and early on one day: the late one has 21 days left, the
    // early one 20, and the day must not promise 21.
    const days = groupByTrashDay(
      [
        { id: 'late', trashedAt: '2030-07-10T23:00:00Z' },
        { id: 'early', trashedAt: '2030-07-10T01:00:00Z' },
      ],
      30,
      now,
      'UTC',
    )
    expect(days.map((day) => day.daysLeft)).toEqual([20])
  })
})
