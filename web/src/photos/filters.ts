import type { PhotoKind } from './api'

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
