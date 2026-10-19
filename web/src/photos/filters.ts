import type { Photo, PhotoKind } from './api'

// The filters of the library that live in the address, so that a link keeps
// them: the kind of photo, and the dates it was taken between. The search and
// the list are the page's own. Dates are days, 2025-07-12, as a date field
// writes them, and mean the days of the person's own clock.

export const FILTER_KINDS: PhotoKind[] = ['picture', 'livePhoto', 'video']

// The kind in the address, or nothing when it is not one the server knows.
export function parsePhotoKind(text: string | null): PhotoKind | '' {
  return FILTER_KINDS.find((kind) => kind === text) ?? ''
}

const DAY_PATTERN = /^(\d{4})-(\d{2})-(\d{2})$/

// The day in the address, or nothing when it is not a day that exists:
// 2025-02-30 is not one.
export function parseDay(text: string | null): string {
  const match = text ? DAY_PATTERN.exec(text) : null
  if (!match) return ''
  const [year, month, day] = [Number(match[1]), Number(match[2]), Number(match[3])]
  const date = new Date(year, month - 1, day)
  return date.getFullYear() === year && date.getMonth() === month - 1 && date.getDate() === day ? text! : ''
}

// The first moment of a day on the clock of this browser, as the server takes
// a date: "photos taken on or after this".
export function startOfDay(day: string): string | undefined {
  const match = DAY_PATTERN.exec(day)
  if (!match) return undefined
  return new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3])).toISOString()
}

// The first moment of the day after: "photos taken before this" includes the
// whole of the last day the person chose.
export function startOfNextDay(day: string): string | undefined {
  const match = DAY_PATTERN.exec(day)
  if (!match) return undefined
  return new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3]) + 1).toISOString()
}

// Whether a search found its photos by what they mean and not by any word of
// the search. The server does not say which way a photo was found, so this
// looks for the words where the server looks for them: in each photo's place,
// file name, camera, description and the words read in it, and in the names of
// the people and albums the person has. One word found in any of them is a
// word match, so that when this is unsure it says nothing rather than saying
// the closest are only the closest.
export function isFoundByMeaningOnly(
  searchText: string,
  photos: Pick<
    Photo,
    'placeName' | 'originalFilename' | 'cameraMake' | 'cameraModel' | 'photoDescription' | 'descriptionVisibleText'
  >[],
  otherNames: string[],
): boolean {
  const words = searchText.toLowerCase().split(/\s+/).filter(Boolean)
  if (words.length === 0 || photos.length === 0) return false
  const hasWord = (text: string | undefined) => {
    const lowered = (text ?? '').toLowerCase()
    return words.some((word) => lowered.includes(word))
  }
  return (
    !photos.some((photo) =>
      [
        photo.placeName,
        photo.originalFilename,
        photo.cameraMake,
        photo.cameraModel,
        photo.photoDescription,
        photo.descriptionVisibleText,
      ].some(hasWord),
    ) && !otherNames.some(hasWord)
  )
}

// How near is near: a kilometer, as a person says "around here".
export const NEARBY_KILOMETERS = 1

// A point in the address, "10.5,20.25", or nothing when it is not one on
// the earth.
export function parseNear(text: string | null): { latitude: number; longitude: number } | null {
  const match = text ? /^(-?\d+(?:\.\d+)?),(-?\d+(?:\.\d+)?)$/.exec(text) : null
  if (!match) return null
  const [latitude, longitude] = [Number(match[1]), Number(match[2])]
  return latitude >= -90 && latitude <= 90 && longitude >= -180 && longitude <= 180 ? { latitude, longitude } : null
}

// The point of a photo as the address writes it: to about a meter, so that the
// address is short and the same photo always writes the same one.
export function nearText(latitude: number, longitude: number): string {
  return `${latitude.toFixed(5)},${longitude.toFixed(5)}`
}

// Whether a day of the calendar exists, whatever clock reads it.
function isCalendarDay(day: string): boolean {
  const [year, month, date] = day.split('-').map(Number)
  const parsed = new Date(Date.UTC(year, month - 1, date))
  return parsed.getUTCFullYear() === year && parsed.getUTCMonth() === month - 1 && parsed.getUTCDate() === date
}

// A moment in the address, 2030-03-05T09:50:00.000Z, or nothing when it is not
// one. Unlike a day it is exact: a visit's window starts and ends at the
// minute the person arrived and left.
export function parseInstant(text: string | null): string | undefined {
  if (!text || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}/.test(text) || !isCalendarDay(text.slice(0, 10))) {
    return undefined
  }
  const moment = new Date(text)
  return Number.isNaN(moment.getTime()) ? undefined : moment.toISOString()
}

// The two ends of the window the address names, as words: "Mar 5, 9:50 AM"
// and "10:59 AM" when both are on one day, a date on each when not. An end the
// address does not name is empty.
export function windowEnds(
  after: string | undefined,
  before: string | undefined,
  language: string,
  timeZone?: string,
): { start: string; end: string } {
  const dayAndTime = new Intl.DateTimeFormat(language, {
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
    timeZone,
  })
  const timeOnly = new Intl.DateTimeFormat(language, { hour: 'numeric', minute: '2-digit', timeZone })
  const dayOnly = new Intl.DateTimeFormat(language, { year: 'numeric', month: 'numeric', day: 'numeric', timeZone })
  const start = after ? new Date(after) : null
  const end = before ? new Date(before) : null
  const isSameDay = start && end && dayOnly.format(start) === dayOnly.format(end)
  return {
    start: start ? dayAndTime.format(start) : '',
    end: end ? (isSameDay ? timeOnly : dayAndTime).format(end) : '',
  }
}
