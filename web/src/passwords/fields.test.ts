import { describe, expect, it } from 'vitest'
import { emptyCardFields, emptyLoginFields, emptyNoteFields, fillMissingFields, ITEM_KINDS } from './fields'

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
})
