import type { Photo } from './api'

// The pure parts of the timeline: which day a photo belongs under, how a day
// is written, and how long a video is.

// The time zone the days of a photo with no known offset are drawn in: the
// one this browser is in, which is also the one it tells the server about
// with every call.
export function personTimeZone(): string | undefined {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || undefined
  } catch {
    return undefined
  }
}

type PlacedPhoto = Pick<Photo, 'takenAt' | 'createdAt'> & { takenAtOffsetMinutes?: number | null }

// A moment and the time zone to read its calendar fields in.
export interface WallClock {
  moment: Date
  timeZone: string | undefined
}

// photoWallClock is the clock on the wall where and when a photo was taken.
// The server keeps the offset the photo was taken at, so a trip abroad sits
// under the dates the people there lived: the moment is shifted by the offset
// and read as UTC. A photo with no offset, or no date taken at all (it is then
// placed where it arrived), is read in the browser's zone.
export function photoWallClock(photo: PlacedPhoto, browserTimeZone: string | undefined): WallClock {
  if (photo.takenAt && photo.takenAtOffsetMinutes !== null && photo.takenAtOffsetMinutes !== undefined) {
    return {
      moment: new Date(new Date(photo.takenAt).getTime() + photo.takenAtOffsetMinutes * 60_000),
      timeZone: 'UTC',
    }
  }
  return { moment: new Date(photo.takenAt || photo.createdAt), timeZone: browserTimeZone }
}

// dayKey names the calendar day a moment falls on in a time zone, as
// 2025-07-12. Two photos are under one heading exactly when their keys are
// equal, so a late night photo and an early morning one are two days even
// though they are minutes apart in UTC.
export function dayKey(moment: Date, timeZone?: string): string {
  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).formatToParts(moment)
  const part = (type: string) => parts.find((candidate) => candidate.type === type)?.value ?? ''
  return `${part('year')}-${part('month')}-${part('day')}`
}

// photoReferenceTitle is what the chip of a photo says in the agent's
// drawer: the place and the day it was taken, or its file name, the way the
// server words it when the question is sent. A photo with none of them has no
// title, and the chip says "Photo".
export function photoReferenceTitle(
  photo: Pick<Photo, 'takenAt' | 'placeName' | 'originalFilename'> & PlacedPhoto,
  browserTimeZone?: string,
): string {
  const parts: string[] = []
  if (photo.placeName.trim()) parts.push(photo.placeName.trim())
  if (photo.takenAt) {
    const { moment, timeZone } = photoWallClock(photo, browserTimeZone)
    if (!Number.isNaN(moment.getTime())) parts.push(dayKey(moment, timeZone))
  }
  return parts.length > 0 ? parts.join(' · ') : photo.originalFilename.trim()
}

// dayHeading is a day written the way the reader's language writes one, with
// the weekday: "Saturday, July 12, 2025".
export function dayHeading(moment: Date, language: string, timeZone?: string): string {
  return new Intl.DateTimeFormat(language, {
    timeZone,
    weekday: 'long',
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  }).format(moment)
}

export interface PhotoDay<T> {
  key: string
  heading: string
  photos: T[]
}

// groupByDay splits photos, which arrive newest first, into runs under one
// heading each. A run is a day: photos on one day that the list does not
// keep together (a page boundary, a search) get a heading each, which is
// what a reader of a page expects.
export function groupByDay<T extends PlacedPhoto>(
  photos: T[],
  language: string,
  browserTimeZone?: string,
): PhotoDay<T>[] {
  const days: PhotoDay<T>[] = []
  for (const photo of photos) {
    const { moment, timeZone } = photoWallClock(photo, browserTimeZone)
    const key = dayKey(moment, timeZone)
    const last = days[days.length - 1]
    if (last && last.key === key) {
      last.photos.push(photo)
    } else {
      days.push({ key, heading: dayHeading(moment, language, timeZone), photos: [photo] })
    }
  }
  return days
}

const DAY_MS = 24 * 60 * 60 * 1000

// trashDaysLeft is how many whole days a photo trashed at a moment stays in
// the Trash, counting from now, when the server keeps it for retentionDays.
// Zero from the last day on; null when the server keeps it for no set time or
// the photo has no moment.
export function trashDaysLeft(trashedAt: string | null, retentionDays: number, now: Date): number | null {
  if (!trashedAt || retentionDays <= 0) return null
  const trashed = new Date(trashedAt)
  if (Number.isNaN(trashed.getTime())) return null
  const elapsedDays = Math.floor((now.getTime() - trashed.getTime()) / DAY_MS)
  return Math.max(0, retentionDays - elapsedDays)
}

export interface TrashDay<T> {
  key: string
  heading: Date
  daysLeft: number | null
  photos: T[]
}

// groupByTrashDay splits photos in the Trash, which the server lists newest
// trashed first, into runs
// under the day they were moved there, each with how long it has left: what a
// person looking at the Trash asks is when each photo goes, not when it was
// taken. The days are the person's own, as the timeline's are.
export function groupByTrashDay<T extends { trashedAt: string | null }>(
  photos: T[],
  retentionDays: number,
  now: Date,
  browserTimeZone?: string,
): TrashDay<T>[] {
  const days: TrashDay<T>[] = []
  for (const photo of photos) {
    const moment = new Date(photo.trashedAt ?? 0)
    const key = dayKey(moment, browserTimeZone)
    const last = days[days.length - 1]
    if (last && last.key === key) {
      last.photos.push(photo)
      // The list runs newest first, so the photo just added is the group's
      // oldest, which is deleted first: the group has as long as it has.
      last.daysLeft = trashDaysLeft(photo.trashedAt, retentionDays, now)
    } else {
      days.push({ key, heading: moment, daysLeft: trashDaysLeft(photo.trashedAt, retentionDays, now), photos: [photo] })
    }
  }
  return days
}

// photoDateTime is the date and time of a photo for its details, on the
// clock where it was taken, with the offset said when it is known so that a
// reader sees it is not their own clock.
export function photoDateTime(photo: PlacedPhoto, language: string, browserTimeZone?: string): string {
  const { moment, timeZone } = photoWallClock(photo, browserTimeZone)
  const text = new Intl.DateTimeFormat(language, {
    timeZone,
    weekday: 'long',
    year: 'numeric',
    month: 'long',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  }).format(moment)
  const offset = photo.takenAt ? photo.takenAtOffsetMinutes : null
  return offset === null || offset === undefined ? text : `${text} (${formatOffset(offset)})`
}

// +09:00, -03:30, +00:00.
export function formatOffset(offsetMinutes: number): string {
  const sign = offsetMinutes < 0 ? '-' : '+'
  const absolute = Math.abs(offsetMinutes)
  const twoDigits = (count: number) => String(count).padStart(2, '0')
  return `UTC${sign}${twoDigits(Math.floor(absolute / 60))}:${twoDigits(absolute % 60)}`
}

// formatDuration is the length of a video as m:ss, or h:mm:ss from an hour
// up: 4000 milliseconds is 0:04 and 3,725,000 is 1:02:05. A fraction of a
// second is dropped rather than rounded, as a player does, so a video does
// not claim to be longer than it is.
export function formatDuration(durationMS: number): string {
  const totalSeconds = Math.max(0, Math.floor(durationMS / 1000))
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  const twoDigits = (count: number) => String(count).padStart(2, '0')
  return hours > 0 ? `${hours}:${twoDigits(minutes)}:${twoDigits(seconds)}` : `${minutes}:${twoDigits(seconds)}`
}

// The indexes from one cell to another, both included, in either order: what
// a shift-click selects.
export function indexRange(firstIndex: number, secondIndex: number): number[] {
  const start = Math.min(firstIndex, secondIndex)
  const end = Math.max(firstIndex, secondIndex)
  return Array.from({ length: end - start + 1 }, (_, offset) => start + offset)
}

// formatByteSize writes a size the way a person says one: 1.5 KB, 12.4 MB,
// 8 GB. A whole number is written without a decimal, and nothing is written
// as "—", which the shared formatter does for zero.
export function formatByteSize(sizeBytes: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = Math.max(0, sizeBytes)
  let unitIndex = 0
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024
    unitIndex++
  }
  const text = unitIndex === 0 ? String(Math.round(value)) : value.toFixed(1).replace(/\.0$/, '')
  return `${text} ${units[unitIndex]}`
}

// formatDateRange writes the dates an album's photos span as the reader's
// language writes a range, "Jul 12 – 14, 2025", or a single date when they
// fall on one day. Empty when the album has no photos. The dates are read in
// the person's own time zone, as the days of the timeline are.
export function formatDateRange(
  firstMoment: string | null,
  lastMoment: string | null,
  language: string,
  timeZone?: string,
): string {
  if (!firstMoment || !lastMoment) return ''
  const first = new Date(firstMoment)
  const last = new Date(lastMoment)
  if (Number.isNaN(first.getTime()) || Number.isNaN(last.getTime())) return ''
  const format = new Intl.DateTimeFormat(language, { timeZone, year: 'numeric', month: 'short', day: 'numeric' })
  // formatRange is in every browser the dashboard supports but not yet in the
  // TypeScript library's types.
  const withRange = format as Intl.DateTimeFormat & { formatRange?: (start: Date, end: Date) => string }
  return withRange.formatRange ? withRange.formatRange(first, last) : format.format(first)
}

// formatDate writes one moment as a date in the reader's language and time
// zone, "July 12, 2030": when a description was written, for one. Empty for
// a moment that is not one.
export function formatDate(moment: string, language: string, timeZone?: string): string {
  const date = new Date(moment)
  if (Number.isNaN(date.getTime())) return ''
  return new Intl.DateTimeFormat(language, { timeZone, year: 'numeric', month: 'long', day: 'numeric' }).format(date)
}

// photoDate is the day a photo was taken, on the clock where it was taken, in
// the reader's language: "July 12, 2025". Without the weekday, which a
// description of a picture does not need.
export function photoDate(photo: PlacedPhoto, language: string, browserTimeZone?: string): string {
  const { moment, timeZone } = photoWallClock(photo, browserTimeZone)
  if (Number.isNaN(moment.getTime())) return ''
  return new Intl.DateTimeFormat(language, { timeZone, year: 'numeric', month: 'long', day: 'numeric' }).format(moment)
}

// The most characters of a description that stand for a picture to a screen
// reader: long enough to say what it shows, short enough to listen to again.
const MOST_ALT_TEXT_CHARACTERS = 160

// firstSentence is the first sentence of a description, cut at a word if even
// that is long. Empty for an empty description.
export function firstSentence(text: string): string {
  const trimmed = text.trim().replace(/\s+/g, ' ')
  // A full stop ends a sentence when a space or the end follows it ("3.5 mm"
  // does not); the full stops of Chinese and Japanese are followed by no space.
  const match = /^(.+?(?:[.!?](?=\s|$)|[\u3002\uff01\uff1f]))/.exec(trimmed)
  const sentence = match ? match[1] : trimmed
  if (sentence.length <= MOST_ALT_TEXT_CHARACTERS) return sentence
  const cut = sentence.slice(0, MOST_ALT_TEXT_CHARACTERS)
  const lastSpace = cut.lastIndexOf(' ')
  return `${(lastSpace > MOST_ALT_TEXT_CHARACTERS / 2 ? cut.slice(0, lastSpace) : cut).trimEnd()}\u2026`
}

// photoAltText is what a picture is called to somebody who cannot see it: the
// first sentence of its description when it has one, else where and when it
// was taken, else only when. The caller supplies the words around the place
// and the date, since they are the language's.
export function photoAltText(
  photo: PlacedPhoto & { placeName: string },
  description: string,
  language: string,
  words: {
    placeAndDate: (place: string, date: string) => string
    dateOnly: (date: string) => string
  },
  browserTimeZone?: string,
): string {
  const sentence = firstSentence(description)
  if (sentence) return sentence
  const date = photoDate(photo, language, browserTimeZone)
  const place = (photo.placeName ?? '').trim()
  if (place && date) return words.placeAndDate(place, date)
  return words.dateOnly(date)
}
