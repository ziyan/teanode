import { describe, expect, it } from 'vitest'

import {
  anHourAfter,
  foundPlaceNamed,
  isPauseInEffect,
  pathThroughVisits,
  pauseDurationSeconds,
  placeSubtitle,
  placeTitle,
  tomorrowMorning,
  travelDistanceText,
  travelRange,
} from './locationData'
import type { FoundPlace } from './locationData'

const words = (key: string) => (key === 'location.label.home' ? 'Home' : key)

const found: FoundPlace = {
  placeName: 'Maple Park',
  osmType: 'node',
  osmId: 1,
  osmCategory: 'leisure=park',
  addressText: '1 Elm Street',
  latitudeDegrees: 40,
  longitudeDegrees: -75,
  distanceMeters: 10,
  placeId: 'p1',
  placeLabel: 'home',
  isNamedByPerson: false,
}

describe('a found place is called the way a place is', () => {
  it('is the label word with the map name under it', () => {
    const place = foundPlaceNamed(found)
    expect(placeTitle(place, words as never)).toBe('Home')
    expect(placeSubtitle(place, words as never)).toBe('Maple Park')
  })

  it('is the persons own name when they gave one', () => {
    const place = foundPlaceNamed({ ...found, placeName: "Grandma's", isNamedByPerson: true })
    expect(placeTitle(place, words as never)).toBe("Grandma's")
    expect(placeSubtitle(place, words as never)).toBe('')
  })

  it('is the map name for a place that is not theirs', () => {
    const place = foundPlaceNamed({ ...found, placeId: null, placeLabel: '' })
    expect(placeTitle(place, words as never)).toBe('Maple Park')
  })
})

describe('pauses', () => {
  const pause = { id: 'pause-one', startedAt: '2030-03-05T12:00:00Z', endedAt: '2030-03-05T13:00:00Z' }

  it('is in effect from its start until its end, and until resumed when it has no end', () => {
    const start = new Date(pause.startedAt).getTime()
    const end = new Date(pause.endedAt).getTime()
    expect(isPauseInEffect(pause, start - 1)).toBe(false)
    expect(isPauseInEffect(pause, start)).toBe(true)
    expect(isPauseInEffect(pause, end - 1)).toBe(true)
    expect(isPauseInEffect(pause, end)).toBe(false)
    expect(isPauseInEffect({ ...pause, endedAt: null }, end + 1_000_000)).toBe(true)
  })

  it('has a length only once it has ended', () => {
    const end = new Date(pause.endedAt).getTime()
    expect(pauseDurationSeconds(pause, end + 1)).toBe(3600)
    expect(pauseDurationSeconds(pause, end - 1)).toBeNull()
    expect(pauseDurationSeconds({ ...pause, endedAt: null }, end + 1)).toBeNull()
  })

  it('ends tomorrow morning at 08:00 on the local clock, and an hour ahead', () => {
    const evening = new Date(2030, 2, 5, 22, 15)
    expect(tomorrowMorning(evening).getTime()).toBe(new Date(2030, 2, 6, 8, 0).getTime())
    expect(anHourAfter(evening).getTime() - evening.getTime()).toBe(3600_000)
  })

  it('ends this morning when it is asked for before 05:00, and tomorrow from 05:00', () => {
    expect(tomorrowMorning(new Date(2030, 2, 6, 0, 30)).getTime()).toBe(new Date(2030, 2, 6, 8, 0).getTime())
    expect(tomorrowMorning(new Date(2030, 2, 6, 4, 59)).getTime()).toBe(new Date(2030, 2, 6, 8, 0).getTime())
    expect(tomorrowMorning(new Date(2030, 2, 6, 5, 0)).getTime()).toBe(new Date(2030, 2, 7, 8, 0).getTime())
    expect(tomorrowMorning(new Date(2030, 2, 31, 6, 0)).getTime()).toBe(new Date(2030, 3, 1, 8, 0).getTime())
  })
})

describe("a day's path", () => {
  const range = { from: '2030-03-05T00:00:00Z', until: '2030-03-06T00:00:00Z' }
  const at = (latitudeDegrees: number) => ({ latitudeDegrees, longitudeDegrees: 0 })
  const placeAt = (latitudeDegrees: number) => ({ ...found, ...at(latitudeDegrees) }) as never

  it('goes through the places visited, in time order, between few positions', () => {
    const path = pathThroughVisits(
      [
        { ...at(1), measuredAt: '2030-03-05T08:00:00Z' },
        { ...at(5), measuredAt: '2030-03-05T20:00:00Z' },
      ],
      [
        { place: placeAt(3), arrivedAt: '2030-03-05T12:00:00Z', departedAt: '2030-03-05T13:00:00Z' },
        { place: placeAt(2), arrivedAt: '2030-03-04T22:00:00Z', departedAt: '2030-03-05T09:00:00Z' },
      ],
      range,
    )
    // The arrival the day before is outside the day; the departure is in it.
    expect(path.map((point) => point.latitudeDegrees)).toEqual([1, 2, 3, 3, 5])
  })

  it('is the visits alone when no position was sent', () => {
    const path = pathThroughVisits(
      [],
      [{ place: placeAt(4), arrivedAt: '2030-03-05T18:00:00Z', departedAt: null }],
      range,
    )
    expect(path).toEqual([at(4)])
  })
})

describe('travelDistanceText', () => {
  it('writes meters below a tenth of a kilometer and kilometers with one decimal above', () => {
    expect(travelDistanceText(80, false, 'en')).toBe('80 m')
    expect(travelDistanceText(600, false, 'en')).toBe('0.6 km')
    expect(travelDistanceText(11800, false, 'en')).toBe('11.8 km')
    expect(travelDistanceText(3000, false, 'en')).toBe('3 km')
    expect(travelDistanceText(1204000, false, 'en')).toBe('1,204 km')
  })

  it('writes whole feet below a tenth of a mile and miles with at most one decimal above, for US units', () => {
    expect(travelDistanceText(104, true, 'en')).toBe('341 ft')
    expect(travelDistanceText(160, true, 'en')).toBe('525 ft')
    expect(travelDistanceText(161, true, 'en')).toBe('0.1 mi')
    expect(travelDistanceText(11800, true, 'en')).toBe('7.3 mi')
    expect(travelDistanceText(16093.44, true, 'en')).toBe('10 mi')
    expect(travelDistanceText(1204000, true, 'en')).toBe('748.1 mi')
  })
})

describe('travelRange', () => {
  const now = new Date(2030, 2, 6, 15, 30)
  it('ends the newest page of days after today and covers fourteen days', () => {
    expect(travelRange('day', 0, now)).toEqual({
      from: new Date(2030, 1, 21).toISOString(),
      until: new Date(2030, 2, 7).toISOString(),
    })
  })
  it('starts weeks on Monday', () => {
    expect(travelRange('week', 0, now)).toEqual({
      from: new Date(2029, 11, 17).toISOString(),
      until: new Date(2030, 2, 11).toISOString(),
    })
  })
  it('covers twelve months and five years, and a later page ends where the earlier began', () => {
    expect(travelRange('month', 0, now).until).toBe(new Date(2030, 3, 1).toISOString())
    expect(travelRange('month', 0, now).from).toBe(new Date(2029, 3, 1).toISOString())
    expect(travelRange('year', 0, now).from).toBe(new Date(2026, 0, 1).toISOString())
    expect(travelRange('year', 1, now).until).toBe(travelRange('year', 0, now).from)
  })
})
