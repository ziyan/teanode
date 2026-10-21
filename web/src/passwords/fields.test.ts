import { describe, expect, it } from 'vitest'
import {
  accessWithAgentUse,
  accessWithGrantees,
  emptyCardFields,
  emptyLoginFields,
  emptyNoteFields,
  fillMissingFields,
  isReachedByAccess,
  ITEM_KINDS,
  type ItemAccess,
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

describe('the access record', () => {
  const access: ItemAccess = { isUsableByAgent: true, grantees: [{ userId: 'user-ben' }, { circleId: 'circle-1' }] }

  it('is left out when there is none, and kept through parsing and an edit', () => {
    expect('access' in parseItemFields('note', { name: 'x' })!).toBe(false)
    expect('access' in parseItemFields('note', { name: 'x', access: null })!).toBe(false)
    for (const itemKind of ITEM_KINDS) {
      const parsed = parseItemFields(itemKind, { name: 'x', access })!
      expect(parsed.access).toEqual(access)
      const edited = parseItemFields(itemKind, JSON.parse(JSON.stringify({ ...parsed, name: 'edited' })))!
      expect(edited.access).toEqual(access)
      expect(edited.name).toBe('edited')
    }
    expect(fillMissingFields('login', { name: 'x', access }).access).toEqual(access)
  })

  it('refuses any other shape, which makes the item unreadable', () => {
    const wrong: unknown[] = [
      'yes',
      5,
      true,
      [],
      {},
      { isUsableByAgent: true },
      { grantees: [] },
      { isUsableByAgent: 'true', grantees: [] },
      { isUsableByAgent: true, grantees: {} },
      { isUsableByAgent: true, grantees: null },
      { isUsableByAgent: true, grantees: ['user-ben'] },
      { isUsableByAgent: true, grantees: [null] },
      { isUsableByAgent: true, grantees: [{}] },
      { isUsableByAgent: true, grantees: [{ userId: 5 }] },
      { isUsableByAgent: true, grantees: [{ userId: '' }] },
      { isUsableByAgent: true, grantees: [{ circleId: null }] },
      { isUsableByAgent: true, grantees: [{ userId: 'user-ben', circleId: 'circle-1' }] },
      { isUsableByAgent: true, grantees: [{ groupId: 'group-1' }] },
      { isUsableByAgent: true, grantees: [{ userId: 'user-ben', extra: true }] },
      { isUsableByAgent: true, grantees: [], extra: true },
    ]
    for (const record of wrong) {
      for (const itemKind of ITEM_KINDS) {
        expect(parseItemFields(itemKind, { name: 'x', access: record }), JSON.stringify(record)).toBeNull()
      }
    }
  })

  it('says whether a recipient is reached by a person or a circle grantee', () => {
    expect(isReachedByAccess(access, 'user-ben', [])).toBe(true)
    expect(isReachedByAccess(access, 'user-cy', ['circle-2', 'circle-1'])).toBe(true)
    expect(isReachedByAccess(access, 'user-cy', ['circle-2'])).toBe(false)
    expect(isReachedByAccess(access, 'circle-1', [])).toBe(false)
    expect(isReachedByAccess(access, '', ['circle-1'])).toBe(false)
    expect(isReachedByAccess(undefined, 'user-ben', ['circle-1'])).toBe(false)
    expect(isReachedByAccess({ isUsableByAgent: false, grantees: [] }, 'user-ben', ['circle-1'])).toBe(false)
  })

  it('builds a new record without changing the one it was given', () => {
    const narrowed = accessWithGrantees(access, [])
    expect(narrowed).toEqual({ isUsableByAgent: true, grantees: [] })
    const agentOff = accessWithAgentUse(access, false)
    expect(agentOff).toEqual({ isUsableByAgent: false, grantees: access.grantees })
    agentOff.grantees[0] = { userId: 'user-cy' }
    expect(access.grantees[0]).toEqual({ userId: 'user-ben' })
    expect(accessWithGrantees(undefined, [{ circleId: 'circle-1' }])).toEqual({
      isUsableByAgent: false,
      grantees: [{ circleId: 'circle-1' }],
    })
    expect(accessWithAgentUse(undefined, true)).toEqual({ isUsableByAgent: true, grantees: [] })
  })
})
