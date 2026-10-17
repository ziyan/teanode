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

// A point in the address, "48.8584,2.2945", or nothing when it is not one on
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
