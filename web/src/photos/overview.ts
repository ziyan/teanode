import { countPhotos, listPhotos, type Photo, type PhotoFilter } from './api'

// The overviews of the library: Years and Months, one picture for each year or
// month that has photos in it, and a way from a picture into the grid at that
// place. The server lists and counts; it does not group. So an overview asks
// it one small question for each year or month ("the newest photo taken in
// it, if any"), which is as many questions as there are periods and not as
// many as there are photos, however large the library.
//
// A period is a calendar year or month on the clock of this browser, which is
// also the clock the date filter of the library is written in.

// The scales the switch offers besides the grid itself.
export type OverviewScale = 'years' | 'months'

// The scale in the address, or nothing (the grid) when it is not one.
export function parseOverviewScale(text: string | null): OverviewScale | '' {
  return text === 'years' || text === 'months' ? text : ''
}

// One year or month and the photo that stands for it: the newest taken in it.
export interface OverviewPeriod {
  // 2030 for a year, 2030-07 for a month.
  key: string
  year: number
  // 0 for January; null for a whole year.
  monthIndex: number | null
  // The first moment of the period and the first moment after it, as the
  // server takes the two ends of a date range.
  takenAfter: string
  takenBefore: string
  photo: Photo
}

// The moment a photo sits at in the timeline: when it was taken, or when it
// arrived if nothing says when it was taken, as the server places it.
export function timelineMoment(photo: Pick<Photo, 'takenAt' | 'createdAt'>): Date {
  return new Date(photo.takenAt || photo.createdAt)
}

// The two ends of a year (monthIndex null) or of one month of it.
export function periodBounds(year: number, monthIndex: number | null): { takenAfter: string; takenBefore: string } {
  // new Date(year, ...) reads two-digit years as 19xx; setFullYear does not.
  const at = (yearValue: number, monthValue: number) => {
    const moment = new Date(2000, 0, 1)
    moment.setFullYear(yearValue, monthValue, 1)
    moment.setHours(0, 0, 0, 0)
    return moment.toISOString()
  }
  return monthIndex === null
    ? { takenAfter: at(year, 0), takenBefore: at(year + 1, 0) }
    : { takenAfter: at(year, monthIndex), takenBefore: at(year, monthIndex + 1) }
}

// The years an overview spans as the reader's language writes a range, "2029
// to 2031", or the one year when they are the same year. Written from the
// first moment of each year in the browser's own zone, as the periods are.
export function formatYearRange(oldestYear: number, newestYear: number, language: string): string {
  const format = new Intl.DateTimeFormat(language, { year: 'numeric' })
  const startOf = (year: number) => new Date(periodBounds(year, null).takenAfter)
  const first = startOf(oldestYear)
  const last = startOf(newestYear)
  if (oldestYear >= newestYear) return format.format(last)
  // formatRange is in every browser the dashboard supports but not yet in the
  // TypeScript library's types.
  const withRange = format as Intl.DateTimeFormat & { formatRange?: (start: Date, end: Date) => string }
  return withRange.formatRange ? withRange.formatRange(first, last) : `${format.format(first)} - ${format.format(last)}`
}

// The years from the newest to the oldest, both included.
export function yearsBetween(newestYear: number, oldestYear: number): number[] {
  const years: number[] = []
  for (let year = newestYear; year >= oldestYear; year--) years.push(year)
  return years
}

// The months of a year from December back to January: the order a list that
// runs newest first has them in.
export const MONTH_INDEXES_NEWEST_FIRST = [11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0]

// A filter narrowed to a period: the photos the filter keeps that were also
// taken in it. Null when nothing can be, because the filter's own dates and
// the period do not overlap.
export function narrowFilter(
  filter: PhotoFilter,
  bounds: { takenAfter: string; takenBefore: string },
): PhotoFilter | null {
  const later = (first: string | undefined, second: string) =>
    first !== undefined && new Date(first).getTime() > new Date(second).getTime() ? first : second
  const earlier = (first: string | undefined, second: string) =>
    first !== undefined && new Date(first).getTime() < new Date(second).getTime() ? first : second
  const takenAfter = later(filter.takenAfter, bounds.takenAfter)
  const takenBefore = earlier(filter.takenBefore, bounds.takenBefore)
  if (new Date(takenAfter).getTime() >= new Date(takenBefore).getTime()) return null
  return { ...filter, takenAfter, takenBefore }
}

// The newest and the oldest year the filtered library has photos in, or null
// when it has none. Three questions: how many, the first, and the last.
export async function readOverviewYears(
  filter: PhotoFilter,
  signal?: AbortSignal,
): Promise<{ newestYear: number; oldestYear: number } | null> {
  const totalCount = await countPhotos(filter, signal)
  if (totalCount === 0) return null
  const [newest, oldest] = await Promise.all([
    listPhotos(filter, 0, 1, signal),
    totalCount > 1 ? listPhotos(filter, totalCount - 1, 1, signal) : Promise.resolve<Photo[]>([]),
  ])
  if (newest.length === 0) return null
  const newestYear = timelineMoment(newest[0]).getFullYear()
  const oldestYear = timelineMoment(oldest[0] ?? newest[0]).getFullYear()
  return { newestYear, oldestYear: Math.min(newestYear, oldestYear) }
}

// The year or month with the photo that stands for it, or null when the
// filtered library has no photo in it.
export async function readOverviewPeriod(
  filter: PhotoFilter,
  year: number,
  monthIndex: number | null,
  signal?: AbortSignal,
): Promise<OverviewPeriod | null> {
  const bounds = periodBounds(year, monthIndex)
  const narrowed = narrowFilter(filter, bounds)
  if (!narrowed) return null
  const [photo] = await listPhotos(narrowed, 0, 1, signal)
  if (!photo) return null
  return {
    key: monthIndex === null ? String(year) : `${year}-${String(monthIndex + 1).padStart(2, '0')}`,
    year,
    monthIndex,
    ...bounds,
    photo,
  }
}

// How many photos of the filtered library are newer than the whole of a
// period: the place of the period's newest photo in the list, counted from
// zero. The list runs newest first, so these are the ones before it.
export async function countPhotosNewerThan(
  filter: PhotoFilter,
  period: Pick<OverviewPeriod, 'takenBefore'>,
  signal?: AbortSignal,
): Promise<number> {
  const startsAt = new Date(period.takenBefore).getTime()
  const takenAfter =
    filter.takenAfter !== undefined && new Date(filter.takenAfter).getTime() > startsAt
      ? filter.takenAfter
      : period.takenBefore
  // The filter's own end is kept: a photo after it was never in the list.
  if (filter.takenBefore !== undefined && new Date(filter.takenBefore).getTime() <= new Date(takenAfter).getTime()) {
    return 0
  }
  return countPhotos({ ...filter, takenAfter }, signal)
}

// The page a place in the list is on, counted from zero, for a page of
// pageSize photos.
export function pageIndexOfPlace(place: number, pageSize: number): number {
  return pageSize > 0 ? Math.floor(Math.max(0, place) / pageSize) : 0
}

// Runs one asynchronous job for each item, a few at a time, and gives the
// answers in the order of the items: a year of months, or thirty years, asks
// the server in small groups and not all at once.
export async function mapWithLimit<Item, Answer>(
  items: Item[],
  limit: number,
  job: (item: Item) => Promise<Answer>,
): Promise<Answer[]> {
  const answers: Answer[] = new Array(items.length)
  let next = 0
  const worker = async () => {
    while (next < items.length) {
      const index = next++
      answers[index] = await job(items[index])
    }
  }
  await Promise.all(Array.from({ length: Math.min(limit, items.length) }, worker))
  return answers
}
