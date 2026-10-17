import { describe, expect, it } from 'vitest'
import { parseDay, parsePhotoKind, startOfDay, startOfNextDay } from './filters'

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
