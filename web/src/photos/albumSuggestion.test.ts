import { describe, expect, it } from 'vitest'
import { titleNamesPlaces } from './albumSuggestion'

describe('titleNamesPlaces', () => {
  it('is true for a title that is the one place', () => {
    expect(titleNamesPlaces('Fixtureville, Exampleland', ['Fixtureville, Exampleland'])).toBe(true)
  })

  it('is true when two cities share a country said once', () => {
    expect(titleNamesPlaces('Alpha and Beta, Exampleland', ['Alpha, Exampleland', 'Beta, Exampleland'])).toBe(true)
  })

  it('is true when two places in different countries are both named', () => {
    expect(titleNamesPlaces('Alpha, Firstland and Beta, Secondland', ['Alpha, Firstland', 'Beta, Secondland'])).toBe(
      true,
    )
  })

  it('is false when the title stops at and more', () => {
    expect(
      titleNamesPlaces('Alpha, Beta and more, Exampleland', [
        'Alpha, Exampleland',
        'Beta, Exampleland',
        'Gamma, Exampleland',
      ]),
    ).toBe(false)
  })

  it('is false for a title of dates', () => {
    expect(titleNamesPlaces('July 12 to 14, 2030', ['Fixtureville, Exampleland'])).toBe(false)
  })

  it('is false when there are no places', () => {
    expect(titleNamesPlaces('July 12, 2030', [])).toBe(false)
  })
})
