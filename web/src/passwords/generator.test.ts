import { describe, expect, it } from 'vitest'
import {
  DEFAULT_PASSWORD_OPTIONS,
  generatePassword,
  MAXIMUM_PASSWORD_LENGTH,
  randomIndex,
  type PasswordOptions,
  type RandomFiller,
} from './generator'

function fillerFrom(numbers: number[]): { fill: RandomFiller; callCount: () => number } {
  let callCount = 0
  return {
    fill: (values) => {
      values[0] = numbers[callCount % numbers.length]
      callCount++
    },
    callCount: () => callCount,
  }
}

describe('randomIndex', () => {
  it('draws again for a number in the uneven tail instead of using a remainder', () => {
    // 2^32 = 429496729 * 10 + 6, so the last six numbers would favor 0 to 5.
    const filler = fillerFrom([0xffffffff, 0xfffffffa, 0xfffffff9])
    expect(randomIndex(10, filler.fill)).toBe(9)
    expect(filler.callCount()).toBe(3)
  })

  it('maps a number below the tail by remainder', () => {
    expect(randomIndex(10, fillerFrom([123456787]).fill)).toBe(7)
    expect(randomIndex(1, fillerFrom([0xffffffff]).fill)).toBe(0)
  })

  it('reaches every index from the browser source', () => {
    const seen = new Set<number>()
    for (let draw = 0; draw < 500; draw++) seen.add(randomIndex(5))
    expect([...seen].sort()).toEqual([0, 1, 2, 3, 4])
  })

  it('refuses a limit it cannot honor', () => {
    expect(() => randomIndex(0)).toThrow(RangeError)
    expect(() => randomIndex(1.5)).toThrow(RangeError)
  })
})

describe('generatePassword', () => {
  it('makes the default 20 characters of every kind', () => {
    const password = generatePassword()
    expect(password).toHaveLength(20)
    expect(password).toMatch(/[a-z]/)
    expect(password).toMatch(/[A-Z]/)
    expect(password).toMatch(/[0-9]/)
    expect(password).toMatch(/[^A-Za-z0-9]/)
  })

  it('keeps one of each kind asked for even in a short password', () => {
    for (let attempt = 0; attempt < 200; attempt++) {
      const password = generatePassword({ ...DEFAULT_PASSWORD_OPTIONS, length: 4 })
      expect(password).toMatch(/[a-z]/)
      expect(password).toMatch(/[A-Z]/)
      expect(password).toMatch(/[0-9]/)
      expect(password).toMatch(/[^A-Za-z0-9]/)
    }
  })

  it('leaves out the kinds turned off', () => {
    const lettersOnly: PasswordOptions = {
      ...DEFAULT_PASSWORD_OPTIONS,
      shouldIncludeDigits: false,
      shouldIncludeSymbols: false,
      length: 64,
    }
    expect(generatePassword(lettersOnly)).toMatch(/^[A-Za-z]{64}$/)
    const digitsOnly: PasswordOptions = {
      ...DEFAULT_PASSWORD_OPTIONS,
      shouldIncludeLetters: false,
      shouldIncludeSymbols: false,
      length: 12,
    }
    expect(generatePassword(digitsOnly)).toMatch(/^[0-9]{12}$/)
  })

  it('avoids look-alikes when asked', () => {
    for (let attempt = 0; attempt < 100; attempt++) {
      expect(generatePassword({ ...DEFAULT_PASSWORD_OPTIONS, length: 64, shouldAvoidLookAlikes: true })).not.toMatch(
        /[Il1|O0o]/,
      )
    }
  })

  it('refuses a request it cannot meet', () => {
    expect(() =>
      generatePassword({
        ...DEFAULT_PASSWORD_OPTIONS,
        shouldIncludeLetters: false,
        shouldIncludeDigits: false,
        shouldIncludeSymbols: false,
      }),
    ).toThrow(/at least one kind/)
    expect(() => generatePassword({ ...DEFAULT_PASSWORD_OPTIONS, length: 3 })).toThrow(RangeError)
    expect(() => generatePassword({ ...DEFAULT_PASSWORD_OPTIONS, length: MAXIMUM_PASSWORD_LENGTH + 1 })).toThrow(
      RangeError,
    )
    expect(() => generatePassword({ ...DEFAULT_PASSWORD_OPTIONS, length: 8.5 })).toThrow(RangeError)
  })
})
