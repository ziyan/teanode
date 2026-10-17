import { describe, expect, it } from 'vitest'

import { personLabel, personName } from './personLabel'

describe('personLabel', () => {
  it('writes the name and the username, so two people with one name differ', () => {
    expect(personLabel({ name: 'Ada Example', username: 'ada' })).toBe('Ada Example (ada)')
    expect(personLabel({ name: 'Ada Example', username: 'ada2' })).toBe('Ada Example (ada2)')
  })

  it('writes the username alone when there is no name, or the name is the username', () => {
    expect(personLabel({ name: '', username: 'ada' })).toBe('ada')
    expect(personLabel({ name: '  ', username: 'ada' })).toBe('ada')
    expect(personLabel({ name: 'ada', username: 'ada' })).toBe('ada')
  })
})

describe('personName', () => {
  it('is the name, or the username without one', () => {
    expect(personName({ name: 'Ada Example', username: 'ada' })).toBe('Ada Example')
    expect(personName({ name: '', username: 'ada' })).toBe('ada')
  })
})
