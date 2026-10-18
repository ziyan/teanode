import { afterEach, describe, expect, it, vi } from 'vitest'
import { graphql } from '../api'
import type { PhotoFilter } from './api'
import {
  countPhotosNewerThan,
  formatYearRange,
  mapWithLimit,
  narrowFilter,
  pageIndexOfPlace,
  parseOverviewScale,
  periodBounds,
  readOverviewPeriod,
  readOverviewYears,
  timelineMoment,
  yearsBetween,
} from './overview'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))

afterEach(() => {
  vi.mocked(graphql).mockReset()
})

const library: PhotoFilter = { listKind: 'library', searchText: '' }

// The server's lists and counts, over photos that are only a moment each.
const serve = (moments: string[]) => {
  const photos = moments
    .map((moment, index) => ({ id: `photo-${index}`, takenAt: moment, createdAt: moment }))
    .sort((first, second) => second.takenAt.localeCompare(first.takenAt))
  const narrowed = (variables: Record<string, unknown>) =>
    photos.filter(
      (photo) =>
        (!variables.takenAfter || new Date(photo.takenAt) >= new Date(String(variables.takenAfter))) &&
        (!variables.takenBefore || new Date(photo.takenAt) < new Date(String(variables.takenBefore))),
    )
  vi.mocked(graphql).mockImplementation((async (document: string, variables: Record<string, unknown>) => {
    if (document.includes('CountPhotos(')) return { CountPhotos: narrowed(variables).length }
    const offset = Number(variables.offset)
    return { ListPhotos: narrowed(variables).slice(offset, offset + Number(variables.pageSize)) }
  }) as never)
}

describe('overview periods', () => {
  it('knows the scales in the address and nothing else', () => {
    expect(parseOverviewScale('years')).toBe('years')
    expect(parseOverviewScale('months')).toBe('months')
    expect(parseOverviewScale('all')).toBe('')
    expect(parseOverviewScale(null)).toBe('')
  })

  it('bounds a month from its first moment to the first moment of the next', () => {
    const july = periodBounds(2030, 6)
    expect(new Date(july.takenAfter)).toEqual(new Date(2030, 6, 1))
    expect(new Date(july.takenBefore)).toEqual(new Date(2030, 7, 1))
    // December ends where the next year starts.
    expect(new Date(periodBounds(2030, 11).takenBefore)).toEqual(new Date(2031, 0, 1))
  })

  it('bounds a year from its first moment to the first moment of the next', () => {
    const year = periodBounds(2030, null)
    expect(new Date(year.takenAfter)).toEqual(new Date(2030, 0, 1))
    expect(new Date(year.takenBefore)).toEqual(new Date(2031, 0, 1))
  })

  it('keeps a year before 100 a year before 100', () => {
    expect(new Date(periodBounds(50, null).takenAfter).getFullYear()).toBe(50)
  })

  it('lists the years from the newest to the oldest, both included', () => {
    expect(yearsBetween(2031, 2029)).toEqual([2031, 2030, 2029])
    expect(yearsBetween(2030, 2030)).toEqual([2030])
  })

  it('writes the years an overview spans as a range, or the one year', () => {
    expect(formatYearRange(2029, 2031, 'en').replace(/\s/g, ' ')).toBe('2029 \u2013 2031')
    expect(formatYearRange(2030, 2030, 'en')).toBe('2030')
    expect(formatYearRange(2029, 2031, 'ja')).toContain('2031')
  })

  it('places a photo at the day it was taken, or the day it arrived', () => {
    expect(timelineMoment({ takenAt: '2030-07-12T09:00:00Z', createdAt: '2031-01-01T00:00:00Z' }).toISOString()).toBe(
      '2030-07-12T09:00:00.000Z',
    )
    expect(timelineMoment({ takenAt: null, createdAt: '2031-01-01T00:00:00Z' }).toISOString()).toBe(
      '2031-01-01T00:00:00.000Z',
    )
  })

  it('narrows a filter to a period and keeps the dates the filter already has', () => {
    const bounds = { takenAfter: '2030-07-01T00:00:00.000Z', takenBefore: '2030-08-01T00:00:00.000Z' }
    expect(narrowFilter(library, bounds)).toEqual({ ...library, ...bounds })
    // A filter from the middle of the month starts later than the month does.
    const narrowed = narrowFilter({ ...library, takenAfter: '2030-07-15T00:00:00.000Z' }, bounds)
    expect(narrowed?.takenAfter).toBe('2030-07-15T00:00:00.000Z')
    expect(narrowed?.takenBefore).toBe(bounds.takenBefore)
    // And one that ends earlier ends the period earlier.
    expect(narrowFilter({ ...library, takenBefore: '2030-07-10T00:00:00.000Z' }, bounds)?.takenBefore).toBe(
      '2030-07-10T00:00:00.000Z',
    )
  })

  it('has no filter for a period the filters have no room for', () => {
    const bounds = { takenAfter: '2030-07-01T00:00:00.000Z', takenBefore: '2030-08-01T00:00:00.000Z' }
    expect(narrowFilter({ ...library, takenAfter: '2030-09-01T00:00:00.000Z' }, bounds)).toBeNull()
    expect(narrowFilter({ ...library, takenBefore: '2030-06-01T00:00:00.000Z' }, bounds)).toBeNull()
  })

  it('finds the newest and the oldest year with three questions', async () => {
    serve(['2031-03-20T12:00:00Z', '2030-07-12T12:00:00Z', '2029-01-15T12:00:00Z'])
    expect(await readOverviewYears(library)).toEqual({ newestYear: 2031, oldestYear: 2029 })
    expect(vi.mocked(graphql)).toHaveBeenCalledTimes(3)
  })

  it('finds one year for a library of one photo, and none for an empty one', async () => {
    serve(['2030-07-12T12:00:00Z'])
    expect(await readOverviewYears(library)).toEqual({ newestYear: 2030, oldestYear: 2030 })
    serve([])
    expect(await readOverviewYears(library)).toBeNull()
  })

  it('stands for a month with its newest photo, and for a month with none with nothing', async () => {
    serve(['2030-07-12T12:00:00Z', '2030-07-02T12:00:00Z', '2030-12-25T12:00:00Z'])
    const july = await readOverviewPeriod(library, 2030, 6)
    expect(july?.key).toBe('2030-07')
    expect(july?.photo.id).toBe('photo-0')
    expect(await readOverviewPeriod(library, 2030, 8)).toBeNull()
    const year = await readOverviewPeriod(library, 2030, null)
    expect(year?.key).toBe('2030')
    expect(year?.photo.id).toBe('photo-2')
  })

  it('asks nothing of the server for a period the filters rule out', async () => {
    serve(['2030-07-12T12:00:00Z'])
    const asked = await readOverviewPeriod({ ...library, takenAfter: '2031-01-01T00:00:00.000Z' }, 2030, 6)
    expect(asked).toBeNull()
    expect(vi.mocked(graphql)).not.toHaveBeenCalled()
  })

  it('counts the photos newer than a period, which is the place of its first photo in the list', async () => {
    serve(['2031-03-20T12:00:00Z', '2030-12-25T12:00:00Z', '2030-07-12T12:00:00Z', '2030-07-02T12:00:00Z'])
    const july = await readOverviewPeriod(library, 2030, 6)
    expect(await countPhotosNewerThan(library, july!)).toBe(2)
    const newest = await readOverviewPeriod(library, 2031, null)
    expect(await countPhotosNewerThan(library, newest!)).toBe(0)
  })

  it('counts newer photos only from the filter’s own start, and none past its end', async () => {
    serve(['2031-03-20T12:00:00Z', '2030-12-25T12:00:00Z', '2030-07-12T12:00:00Z'])
    const july = await readOverviewPeriod(library, 2030, 6)
    // With the filter ending in December, December is the only photo newer than July.
    expect(await countPhotosNewerThan({ ...library, takenBefore: '2030-12-31T00:00:00.000Z' }, july!)).toBe(1)
    // A filter that ends before the period began keeps nothing newer.
    expect(await countPhotosNewerThan({ ...library, takenBefore: '2030-07-01T00:00:00.000Z' }, july!)).toBe(0)
  })

  it('finds the page a place is on', () => {
    expect(pageIndexOfPlace(0, 200)).toBe(0)
    expect(pageIndexOfPlace(199, 200)).toBe(0)
    expect(pageIndexOfPlace(200, 200)).toBe(1)
    expect(pageIndexOfPlace(30, 25)).toBe(1)
    expect(pageIndexOfPlace(-4, 25)).toBe(0)
  })

  it('asks for a few at a time and answers in the order of the items', async () => {
    let running = 0
    let mostRunning = 0
    const answers = await mapWithLimit([5, 4, 3, 2, 1, 0], 2, async (item) => {
      running++
      mostRunning = Math.max(mostRunning, running)
      await new Promise((resolve) => setTimeout(resolve, item))
      running--
      return item * 10
    })
    expect(answers).toEqual([50, 40, 30, 20, 10, 0])
    expect(mostRunning).toBe(2)
    expect(await mapWithLimit([], 3, async (item) => item)).toEqual([])
  })
})
