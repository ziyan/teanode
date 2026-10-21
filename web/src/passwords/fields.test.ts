import { describe, expect, it } from 'vitest'
import {
  emptyCardFields,
  emptyLoginFields,
  emptyNoteFields,
  fillMissingFields,
  ITEM_KINDS,
  parseItemFields,
} from './fields'

describe('item fields', () => {
  it('names the fields of each kind as the plan does', () => {
    expect(Object.keys(emptyLoginFields())).toEqual([
      'name',
      'userName',
      'password',
      'sites',
      'oneTimeCodeUri',
      'notes',
      'passkeys',
      'folder',
    ])
    expect(Object.keys(emptyCardFields())).toEqual([
      'name',
      'cardholderName',
      'number',
      'expiryMonth',
      'expiryYear',
      'securityCode',
      'brand',
      'notes',
      'folder',
    ])
    expect(Object.keys(emptyNoteFields())).toEqual(['name', 'text', 'folder'])
    expect(ITEM_KINDS).toEqual(['login', 'card', 'note'])
  })

  it('gives every empty login its own lists', () => {
    expect(emptyLoginFields().sites).not.toBe(emptyLoginFields().sites)
  })

  it('fills the keys another client left out', () => {
    expect(fillMissingFields('login', { name: 'Example', password: 'secret' })).toEqual({
      ...emptyLoginFields(),
      name: 'Example',
      password: 'secret',
    })
    expect(fillMissingFields('note', { text: 'hello' })).toEqual({ name: '', text: 'hello', folder: '' })
  })

  describe('parseItemFields', () => {
    const passkey = {
      credentialId: 'AAAA',
      relyingPartyId: 'example.test',
      userHandle: 'AAAA',
      userName: 'ada',
      userDisplayName: 'Ada',
      privateKey: 'AAAA',
      createdAt: '2026-10-01T10:00:00Z',
    }

    it('returns the fields with every key of the kind', () => {
      expect(parseItemFields('login', { name: 'Example', passkeys: [passkey] })).toEqual({
        ...emptyLoginFields(),
        name: 'Example',
        passkeys: [passkey],
      })
      expect(parseItemFields('note', { text: 'hello' })).toEqual({ name: '', text: 'hello', folder: '' })
      expect(parseItemFields('card', { number: '4111' })).toMatchObject({ number: '4111', brand: '' })
    })

    it('reads a null list, as a program writes an empty one, as empty', () => {
      expect(parseItemFields('login', { name: 'x', sites: null, passkeys: null })).toEqual({
        ...emptyLoginFields(),
        name: 'x',
      })
    })

    it('keeps keys the kind does not define, so a newer client fields survive an edit', () => {
      expect(parseItemFields('note', { name: 'x', colour: 'green' })).toMatchObject({ name: 'x', colour: 'green' })
    })

    it('refuses a value that is not an object', () => {
      for (const value of [null, undefined, 'hello', 5, [], true]) {
        expect(parseItemFields('login', value)).toBeNull()
      }
    })

    it('refuses a field of the wrong type', () => {
      const wrong: [string, unknown][] = [
        ['name', 5],
        ['name', {}],
        ['name', ['x']],
        ['password', true],
        ['sites', 'https://example.test'],
        ['sites', [5]],
        ['sites', [null]],
        ['sites', {}],
        ['passkeys', 'x'],
        ['passkeys', ['x']],
        ['passkeys', [null]],
        ['passkeys', [[]]],
        ['passkeys', [{ ...passkey, credentialId: 5 }]],
        ['passkeys', [{ ...passkey, privateKey: {} }]],
        ['oneTimeCodeUri', 5],
      ]
      for (const [fieldName, value] of wrong) {
        expect(
          parseItemFields('login', { ...emptyLoginFields(), [fieldName]: value }),
          `${fieldName}: ${JSON.stringify(value)}`,
        ).toBeNull()
      }
      expect(parseItemFields('card', { ...emptyCardFields(), number: 4111 })).toBeNull()
      expect(parseItemFields('note', { ...emptyNoteFields(), text: ['a'] })).toBeNull()
    })
  })
})
