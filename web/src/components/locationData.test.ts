import { describe, expect, it } from 'vitest'

import { foundPlaceNamed, placeSubtitle, placeTitle } from './locationData'
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
