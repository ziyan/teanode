import { describe, expect, it } from 'vitest'

import {
  anHourAfter,
  foundPlaceNamed,
  isPauseInEffect,
  pauseDurationSeconds,
  placeSubtitle,
  placeTitle,
  tomorrowMorning,
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
    expect(tomorrowMorning(new Date(2030, 2, 31, 3, 0)).getTime()).toBe(new Date(2030, 3, 1, 8, 0).getTime())
    expect(anHourAfter(evening).getTime() - evening.getTime()).toBe(3600_000)
  })
})
