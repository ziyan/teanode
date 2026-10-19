import { describe, expect, it } from 'vitest'
import {
  isFoundByMeaningOnly,
  parseDay,
  parseInstant,
  parsePhotoKind,
  startOfDay,
  startOfNextDay,
  windowEnds,
} from './filters'

describe('the filters in the address', () => {
  it('knows the kinds the server knows and nothing else', () => {
    expect(parsePhotoKind('video')).toBe('video')
    expect(parsePhotoKind('livePhoto')).toBe('livePhoto')
    expect(parsePhotoKind('gif')).toBe('')
    expect(parsePhotoKind(null)).toBe('')
  })

  it('keeps a day that exists and drops one that does not', () => {
    expect(parseDay('2030-07-12')).toBe('2030-07-12')
    expect(parseDay('2030-02-30')).toBe('')
    expect(parseDay('July 12')).toBe('')
    expect(parseDay(null)).toBe('')
  })

  it('reads a range of days as the first moment of the first and the one after the last', () => {
    const first = new Date(startOfDay('2030-07-12')!)
    const after = new Date(startOfNextDay('2030-07-14')!)
    expect([first.getFullYear(), first.getMonth(), first.getDate(), first.getHours()]).toEqual([2030, 6, 12, 0])
    expect([after.getFullYear(), after.getMonth(), after.getDate(), after.getHours()]).toEqual([2030, 6, 15, 0])
    expect(startOfNextDay('2030-12-31')!.slice(0, 4)).toMatch(/2030|2031/)
  })
})

describe('a window of moments in the address', () => {
  it('keeps a moment that exists as an instant and drops anything else', () => {
    expect(parseInstant('2030-03-05T09:50:00.000Z')).toBe('2030-03-05T09:50:00.000Z')
    expect(parseInstant('2030-03-05T09:50:00+02:00')).toBe('2030-03-05T07:50:00.000Z')
    expect(parseInstant('2030-03-05')).toBeUndefined()
    expect(parseInstant('2030-02-31T10:00:00Z')).toBeUndefined()
    expect(parseInstant('yesterday')).toBeUndefined()
    expect(parseInstant(null)).toBeUndefined()
  })

  it('names the end as a time alone when it is on the same day', () => {
    const same = windowEnds('2030-03-05T09:50:00.000Z', '2030-03-05T10:59:00.000Z', 'en', 'UTC')
    expect(same).toEqual({ start: 'Mar 5, 9:50 AM', end: '10:59 AM' })
    const other = windowEnds('2030-03-05T09:50:00.000Z', '2030-03-06T10:59:00.000Z', 'en', 'UTC')
    expect(other.end).toBe('Mar 6, 10:59 AM')
    expect(windowEnds(undefined, '2030-03-06T10:59:00.000Z', 'en', 'UTC').start).toBe('')
  })
})

describe('a search that found photos only by what they mean', () => {
  const photo = (overrides: Record<string, string> = {}) => ({
    placeName: '',
    originalFilename: 'one.jpg',
    cameraMake: '',
    cameraModel: '',
    photoDescription: '',
    descriptionVisibleText: '',
    ...overrides,
  })

  it('is one where no photo, person or album has a word of the search', () => {
    expect(isFoundByMeaningOnly('zzqx', [photo(), photo({ placeName: 'Harbor' })], ['Ada', 'Picnic'])).toBe(true)
  })

  it('is not one when a word is in a place, a description, or the name of a person or album', () => {
    expect(isFoundByMeaningOnly('harbor', [photo({ placeName: 'Old Harbor' })], [])).toBe(false)
    expect(isFoundByMeaningOnly('kite', [photo({ photoDescription: 'A red kite.' })], [])).toBe(false)
    expect(isFoundByMeaningOnly('ada', [photo()], ['Ada Example'])).toBe(false)
    expect(isFoundByMeaningOnly('picnic', [photo()], ['Picnic'])).toBe(false)
  })

  it('says nothing for no results or no words', () => {
    expect(isFoundByMeaningOnly('zzqx', [], [])).toBe(false)
    expect(isFoundByMeaningOnly('  ', [photo()], [])).toBe(false)
  })
})
