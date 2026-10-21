import { beforeAll, describe, expect, it } from 'vitest'
import {
  encodeBase64,
  generatePersonKeyPair,
  openItemFields,
  openItemKey,
  parseRecoveryKey,
  parseSealedValue,
  PersonKeyPair,
} from '../../../src/passwords/crypto'
import { LoginFields, PasskeyFields } from '../../../src/passwords/fields'
import { PasswordItemRecord } from '../../../src/pages/passwords/passwordsApi'
import { PendingSetUp, sealItemKeyFor } from '../../../src/pages/passwords/passwordsProtocol'
import { PasswordsError } from '../../../src/passwords/passwordsError'
import {
  addPasskeySite,
  currentOneTimeCode,
  forgetPasskeySites,
  isPasskeySiteKnown,
  loginsForPage,
  loginsOf,
  lockPasswords,
  openItems,
  passkeysForRelyingParty,
  readPasswordsState,
  releaseItems,
  saveCapturedLogin,
  savePasskey,
  summaryOf,
  unlockPasswords,
  PasswordsEnvironment,
  whatToDoWithCapturedLogin,
  withOpenedKeys,
} from './openedKeys'
import {
  createFakeServer,
  makeFixture,
  makeLogin,
  memoryArea,
  ownRecord as makeOwnRecord,
  PASSPHRASE,
  USER_ID,
} from './testServer'
import { credentialIdOf } from './webauthn'

let fixture: PendingSetUp
let ownerKeyPair: PersonKeyPair

beforeAll(async () => {
  fixture = await makeFixture()
  ownerKeyPair = await generatePersonKeyPair()
}, 60_000)

function setup(records: PasswordItemRecord[] = []) {
  const server = createFakeServer(fixture, records)
  let nowMilliseconds = 1_000_000
  const session = memoryArea()
  const local = memoryArea()
  const environment: PasswordsEnvironment = {
    session,
    local,
    now: () => nowMilliseconds,
    call: (query, variables) => server.call(query, variables),
  }
  return { environment, server, session, local, advance: (minutes: number) => (nowMilliseconds += minutes * 60_000) }
}

const login = makeLogin

function ownRecord(id: string, fields: LoginFields, revision = 1): Promise<PasswordItemRecord> {
  return makeOwnRecord(fixture, id, fields, revision)
}

async function unlocked(records: PasswordItemRecord[] = []) {
  const made = setup(records)
  await unlockPasswords(made.environment, { secretText: PASSPHRASE, isRecoveryKey: false })
  return made
}

async function readFields(input: Record<string, any>): Promise<LoginFields> {
  const itemKey = await openItemKey(fixture.accountKey, parseSealedValue(input.itemKey), input.id)
  return (await openItemFields(itemKey, parseSealedValue(input.encryptedFields), input.id, 'login')) as LoginFields
}

describe('unlocking and locking', () => {
  it('keeps the opened keys in the session storage and nowhere else', async () => {
    const { environment, session, local } = setup()
    await unlockPasswords(environment, { secretText: PASSPHRASE, isRecoveryKey: false })
    expect(await readPasswordsState(environment)).toEqual({
      isUnlocked: true,
      lockAfterMinutes: 5,
      wasUserVerifiedRecently: true,
    })
    expect(Object.keys(session.contents)).toEqual(['passwordsKeys'])
    expect(Object.keys(local.contents)).toEqual([])
    const stored = session.contents.passwordsKeys as Record<string, unknown>
    expect(stored.userId).toBe(USER_ID)
    expect(JSON.stringify(session.contents)).not.toContain(PASSPHRASE)
  })

  it('opens with the recovery key', async () => {
    const { environment } = setup()
    expect(parseRecoveryKey(fixture.recoveryKeyText).length).toBe(32)
    await unlockPasswords(environment, { secretText: fixture.recoveryKeyText, isRecoveryKey: true })
    expect((await readPasswordsState(environment)).isUnlocked).toBe(true)
  })

  it('stores nothing when the passphrase is wrong', async () => {
    const { environment, session } = setup()
    await expect(
      unlockPasswords(environment, { secretText: 'another passphrase here', isRecoveryKey: false }),
    ).rejects.toThrow('That passphrase is not right.')
    expect(session.contents).toEqual({})
  })

  it('locks by hand', async () => {
    const { environment, session } = await unlocked()
    await lockPasswords(environment)
    expect(session.contents).toEqual({})
    await expect(withOpenedKeys(environment, async () => 1)).rejects.toMatchObject({ code: 'locked' })
  })

  it('drops the keys after the lock time of no use', async () => {
    const { environment, session, advance } = await unlocked()
    advance(4)
    expect((await readPasswordsState(environment)).isUnlocked).toBe(true)
    advance(1)
    expect((await readPasswordsState(environment)).isUnlocked).toBe(false)
    expect(session.contents).toEqual({})
  })

  it('puts the lock off on use, but not on a look', async () => {
    const { environment, advance } = await unlocked()
    advance(4)
    await withOpenedKeys(environment, async () => undefined)
    advance(4)
    expect((await readPasswordsState(environment)).isUnlocked).toBe(true)
    await withOpenedKeys(environment, async () => undefined, { isUse: false })
    advance(1)
    expect((await readPasswordsState(environment)).isUnlocked).toBe(false)
  })

  it('refuses a step once the lock time ran out, even before anything looked', async () => {
    const { environment, advance } = await unlocked()
    advance(6)
    await expect(withOpenedKeys(environment, async () => 1)).rejects.toBeInstanceOf(PasswordsError)
  })

  it('overwrites the key copies it gave a step', async () => {
    const { environment } = await unlocked()
    let held: Uint8Array | null = null
    await withOpenedKeys(environment, async (keys) => {
      held = keys.accountKey
      expect(held.some((byte) => byte !== 0)).toBe(true)
    })
    expect(held!.every((byte) => byte === 0)).toBe(true)
  })
})

describe('reading logins', () => {
  it('opens the items and finds the logins for a page', async () => {
    const { environment } = await unlocked([
      await ownRecord('00000000-0000-4000-8000-000000000001', login({ name: 'Example', sites: ['example.com'] })),
      await ownRecord('00000000-0000-4000-8000-000000000002', login({ name: 'Other', sites: ['https://other.test'] })),
      await ownRecord('00000000-0000-4000-8000-000000000003', login({ name: 'Plain', sites: ['http://plain.test'] })),
    ])
    await withOpenedKeys(environment, async (keys) => {
      const items = await openItems(environment, keys)
      const logins = loginsOf(items)
      expect(logins.map((item) => item.fields.name).sort()).toEqual(['Example', 'Other', 'Plain'])
      expect(loginsForPage(logins, 'https://www.example.com/sign-in').map((item) => item.fields.name)).toEqual([
        'Example',
      ])
      expect(loginsForPage(logins, 'https://login.example.com/').length).toBe(0)
      expect(loginsForPage(logins, 'http://example.com/').length).toBe(0)
      expect(loginsForPage(logins, 'http://plain.test/').length).toBe(1)
      releaseItems(items)
    })
  })

  it('leaves out deleted items and items that would not open', async () => {
    const good = await ownRecord('00000000-0000-4000-8000-000000000001', login({}))
    const deleted = {
      ...(await ownRecord('00000000-0000-4000-8000-000000000002', login({ name: 'Gone' }))),
      deletedAt: '2026-10-19T00:00:00Z',
    }
    const broken = {
      ...(await ownRecord('00000000-0000-4000-8000-000000000003', login({ name: 'Broken' }))),
      encryptedFields: good.itemKey,
    }
    const { environment } = await unlocked([good, deleted, broken])
    await withOpenedKeys(environment, async (keys) => {
      const items = await openItems(environment, keys)
      expect(items.map((item) => item.fields.name)).toEqual(['Example'])
    })
  })

  it('summarizes a login without its password, code secret or private key', async () => {
    const record = await ownRecord(
      '00000000-0000-4000-8000-000000000001',
      login({
        oneTimeCodeUri: 'otpauth://totp/x?secret=' + ['GEZDGNBV', 'GY3TQOJQ'].join(''),
        passkeys: [
          {
            credentialId: 'AA==',
            relyingPartyId: 'example.com',
            userHandle: '',
            userName: 'ada',
            userDisplayName: '',
            privateKey: 'PRIVATE',
            createdAt: '',
          },
        ],
      }),
    )
    const { environment } = await unlocked([record])
    await withOpenedKeys(environment, async (keys) => {
      const [item] = loginsOf(await openItems(environment, keys))
      const summary = summaryOf(item)
      expect(summary).toEqual({
        id: item.id,
        name: 'Example',
        userName: 'ada@example.test',
        siteAddress: 'https://example.com',
        hasPassword: true,
        hasOneTimeCode: true,
        passkeyCount: 1,
        isOwnedByMe: true,
      })
      expect(JSON.stringify(summary)).not.toContain('hunter2')
      expect(JSON.stringify(summary)).not.toContain('PRIVATE')
    })
  })

  it('keeps nothing decrypted in storage', async () => {
    const { environment, session, local } = await unlocked([
      await ownRecord('00000000-0000-4000-8000-000000000001', login({})),
    ])
    await withOpenedKeys(environment, async (keys) => {
      releaseItems(await openItems(environment, keys))
    })
    for (const text of [JSON.stringify(session.contents), JSON.stringify(local.contents)]) {
      expect(text).not.toContain('hunter2-plaintext')
      expect(text).not.toContain('ada@example.test')
    }
  })

  it('opens an item shared by another person with the owner key it knows, and remembers the first one', async () => {
    const id = '00000000-0000-4000-8000-0000000000aa'
    const itemKey = crypto.getRandomValues(new Uint8Array(32))
    const { sealItemFields, serializeSealed } = await import('../../../src/passwords/crypto')
    const encryptedFields = serializeSealed(
      await sealItemFields(itemKey, login({ name: 'Shared', sites: ['example.com'] }), id, 'login'),
    )
    const shared: PasswordItemRecord = {
      ...(await ownRecord(id, login({}))),
      ownerUserId: 'owner-b',
      ownerName: 'Bo',
      ownerPublicKey: encodeBase64(ownerKeyPair.publicKey),
      isSharedWithMe: true,
      encryptedFields,
      itemKey: await sealItemKeyFor(itemKey, ownerKeyPair, fixture.input.publicKey, id, 'owner-b', USER_ID),
    }
    const { environment, local, server } = await unlocked([shared])
    await withOpenedKeys(environment, async (keys) => {
      const items = await openItems(environment, keys)
      expect(items.map((item) => [item.fields.name, item.isOwnedByMe])).toEqual([['Shared', false]])
    })
    expect(local.contents['passwordsPins:' + USER_ID]).toEqual({ 'owner-b': encodeBase64(ownerKeyPair.publicKey) })
    expect(JSON.stringify(local.contents)).not.toContain('Shared')

    // The owner's key changes: the item is no longer opened.
    const otherKeyPair = await generatePersonKeyPair()
    server.records = [{ ...shared, ownerPublicKey: encodeBase64(otherKeyPair.publicKey) }]
    await withOpenedKeys(environment, async (keys) => {
      const report = { ownerKeyChangedCount: 0 }
      expect(await openItems(environment, keys, report)).toEqual([])
      // Not dropped without a word: it is counted, for the window to say so.
      expect(report).toEqual({ ownerKeyChangedCount: 1 })
    })
  })
})

describe('the lock time and when the person was last verified', () => {
  it('keeps the lock time the server named, but never longer than a day', async () => {
    for (const [named, kept] of [
      [5, 5],
      [1440, 1440],
      [1441, 1440],
      [100_000_000, 1440],
      [0, 15],
    ]) {
      const { environment, server, session } = setup()
      const call = server.call
      server.call = async (query, variables) => {
        const answer = await call(query, variables)
        if (answer.ReadPasswordsSetup) answer.ReadPasswordsSetup.lockAfterMinutes = named
        return answer
      }
      await unlockPasswords(environment, { secretText: PASSPHRASE, isRecoveryKey: false })
      expect((session.contents.passwordsKeys as { lockAfterMinutes: number }).lockAfterMinutes, String(named)).toBe(
        kept,
      )
      expect((await readPasswordsState(environment)).lockAfterMinutes).toBe(kept)
    }
  })

  it('caps a longer lock time already stored, so the keys do not outlast a day', async () => {
    const { environment, session, advance } = await unlocked()
    session.contents.passwordsKeys = { ...(session.contents.passwordsKeys as object), lockAfterMinutes: 100_000 }
    expect((await readPasswordsState(environment)).lockAfterMinutes).toBe(1440)
    advance(1439)
    expect((await readPasswordsState(environment)).isUnlocked).toBe(true)
    advance(2)
    expect((await readPasswordsState(environment)).isUnlocked).toBe(false)
  })

  it('counts the person as verified for a minute after the passphrase, whatever is done with the keys', async () => {
    const { environment, advance } = await unlocked()
    expect((await readPasswordsState(environment)).wasUserVerifiedRecently).toBe(true)
    advance(0.5)
    // Using the keys is not typing the passphrase.
    await withOpenedKeys(environment, async () => 1)
    expect((await readPasswordsState(environment)).wasUserVerifiedRecently).toBe(true)
    advance(0.6)
    await withOpenedKeys(environment, async () => 1)
    expect((await readPasswordsState(environment)).isUnlocked).toBe(true)
    expect((await readPasswordsState(environment)).wasUserVerifiedRecently).toBe(false)
  })

  it('counts keys stored before this was kept as not verified', async () => {
    const { environment, session } = await unlocked()
    const older = { ...(session.contents.passwordsKeys as Record<string, unknown>) }
    delete older.verifiedAtMilliseconds
    session.contents.passwordsKeys = older
    expect(await readPasswordsState(environment)).toMatchObject({ isUnlocked: true, wasUserVerifiedRecently: false })
  })
})

describe('which sites have a passkey, known while locked', () => {
  const passkey = (relyingPartyId: string): PasskeyFields => ({
    credentialId: 'AQID',
    relyingPartyId,
    userHandle: 'AQID',
    userName: 'ada',
    userDisplayName: 'Ada',
    privateKey: 'AQID',
    createdAt: '2026-10-20T00:00:00.000Z',
  })

  it('keeps salted hashes of the relying party ids, when the items are opened, and no name', async () => {
    const record = await ownRecord(
      '00000000-0000-4000-8000-000000000001',
      login({ passkeys: [passkey('example.test'), passkey('another.test')] }),
    )
    const { environment, local } = await unlocked([record])
    expect(await isPasskeySiteKnown(environment, 'example.test')).toBe(false)
    await withOpenedKeys(environment, async (keys) => releaseItems(await openItems(environment, keys)))
    expect(await isPasskeySiteKnown(environment, 'example.test')).toBe(true)
    expect(await isPasskeySiteKnown(environment, 'EXAMPLE.test')).toBe(true)
    expect(await isPasskeySiteKnown(environment, 'another.test')).toBe(true)
    expect(await isPasskeySiteKnown(environment, 'third.test')).toBe(false)
    const text = JSON.stringify(local.contents)
    expect(text).not.toContain('example.test')
    expect(text).not.toContain('another.test')
  })

  it('follows what is saved and what is removed, and can be forgotten', async () => {
    const record = await ownRecord(
      '00000000-0000-4000-8000-000000000001',
      login({ passkeys: [passkey('example.test')] }),
    )
    const { environment, server } = await unlocked([record])
    await withOpenedKeys(environment, async (keys) => releaseItems(await openItems(environment, keys)))
    server.records = [await ownRecord('00000000-0000-4000-8000-000000000001', login({ passkeys: [] }))]
    await withOpenedKeys(environment, async (keys) => releaseItems(await openItems(environment, keys)))
    expect(await isPasskeySiteKnown(environment, 'example.test')).toBe(false)
    await addPasskeySite(environment, USER_ID, 'later.test')
    expect(await isPasskeySiteKnown(environment, 'later.test')).toBe(true)
    await forgetPasskeySites(environment)
    expect(await isPasskeySiteKnown(environment, 'later.test')).toBe(false)
  })
})

describe('one-time codes', () => {
  it('works out the code and the time it has left', async () => {
    const record = await ownRecord(
      '00000000-0000-4000-8000-000000000001',
      login({ oneTimeCodeUri: 'otpauth://totp/Example:ada?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&digits=8' }),
    )
    const { environment } = await unlocked([record])
    await withOpenedKeys(environment, async (keys) => {
      const [item] = loginsOf(await openItems(environment, keys))
      // RFC 6238 appendix B: at 59 seconds the SHA-1 code is 94287082.
      expect(await currentOneTimeCode(item, 59_000)).toEqual({ oneTimeCode: '94287082', secondsLeft: 1 })
    })
  })

  it('is null for a login with no code', async () => {
    const { environment } = await unlocked([await ownRecord('00000000-0000-4000-8000-000000000001', login({}))])
    await withOpenedKeys(environment, async (keys) => {
      const [item] = loginsOf(await openItems(environment, keys))
      expect(await currentOneTimeCode(item, 0)).toBeNull()
    })
  })
})

describe('saving a login after a sign-in', () => {
  const pageUrl = 'https://www.example.com/sign-in'

  it('offers to save a login that is new, and saves it encrypted', async () => {
    const { environment, server } = await unlocked()
    const saveOutcome = await withOpenedKeys(environment, async (keys) => {
      const items = await openItems(environment, keys)
      expect(
        whatToDoWithCapturedLogin(loginsOf(items), { pageUrl, userName: 'ada', password: 'first-pass' }).offerAction,
      ).toBe('save')
      return saveCapturedLogin(environment, keys, loginsOf(items), { pageUrl, userName: 'ada', password: 'first-pass' })
    })
    expect(saveOutcome).toBe('saved')
    expect(server.saved.length).toBe(1)
    const input = server.saved[0].input
    expect(input).toMatchObject({ itemKind: 'login', baseRevision: null })
    expect(input.id).toMatch(/^[0-9a-f-]{36}$/)
    // The server is sent sealed values only.
    expect(JSON.stringify(input)).not.toContain('first-pass')
    expect(await readFields(input)).toMatchObject({
      name: 'example.com',
      userName: 'ada',
      password: 'first-pass',
      sites: ['https://www.example.com'],
    })
  })

  it('offers to update when the password changed, and keeps the item key and revision', async () => {
    const id = '00000000-0000-4000-8000-000000000001'
    const { environment, server } = await unlocked([
      await ownRecord(id, login({ userName: 'Ada', password: 'old-pass', sites: ['example.com'] }), 4),
    ])
    const saveOutcome = await withOpenedKeys(environment, async (keys) => {
      const logins = loginsOf(await openItems(environment, keys))
      expect(whatToDoWithCapturedLogin(logins, { pageUrl, userName: 'ada', password: 'new-pass' }).offerAction).toBe(
        'update',
      )
      return saveCapturedLogin(environment, keys, logins, { pageUrl, userName: 'ada', password: 'new-pass' })
    })
    expect(saveOutcome).toBe('updated')
    expect(server.saved[0].input).toMatchObject({ id, baseRevision: 4 })
    expect(await readFields(server.saved[0].input)).toMatchObject({
      userName: 'Ada',
      password: 'new-pass',
      name: 'Example',
    })
  })

  it('offers nothing when the login is already saved', async () => {
    const { environment, server } = await unlocked([
      await ownRecord(
        '00000000-0000-4000-8000-000000000001',
        login({ userName: 'ada', password: 'same-pass', sites: ['example.com'] }),
      ),
    ])
    const saveOutcome = await withOpenedKeys(environment, async (keys) => {
      const logins = loginsOf(await openItems(environment, keys))
      return saveCapturedLogin(environment, keys, logins, { pageUrl, userName: 'ada', password: 'same-pass' })
    })
    expect(saveOutcome).toBe('unchanged')
    expect(server.saved).toEqual([])
  })

  it('saves a different user name as another login', async () => {
    const { environment, server } = await unlocked([
      await ownRecord('00000000-0000-4000-8000-000000000001', login({ userName: 'ada', sites: ['example.com'] })),
    ])
    await withOpenedKeys(environment, async (keys) => {
      const logins = loginsOf(await openItems(environment, keys))
      expect(
        await saveCapturedLogin(environment, keys, logins, { pageUrl, userName: 'grace', password: 'g-pass' }),
      ).toBe('saved')
    })
    expect(server.records.length).toBe(2)
  })
})

describe('passkeys in logins', () => {
  const passkey: PasskeyFields = {
    credentialId: encodeBase64(new Uint8Array([1, 2, 3])),
    relyingPartyId: 'example.com',
    userHandle: encodeBase64(new Uint8Array([9])),
    userName: 'ada@example.test',
    userDisplayName: 'Ada',
    privateKey: 'PRIVATE',
    createdAt: '2026-10-20T00:00:00Z',
  }

  it('adds a passkey to the login for the same site and user name', async () => {
    const id = '00000000-0000-4000-8000-000000000001'
    const { environment, server } = await unlocked([await ownRecord(id, login({ sites: ['example.com'] }), 2)])
    const saveOutcome = await withOpenedKeys(environment, async (keys) => {
      const logins = loginsOf(await openItems(environment, keys))
      return savePasskey(environment, keys, logins, {
        pageUrl: 'https://example.com/',
        relyingPartyName: 'Example',
        passkey,
      })
    })
    expect(saveOutcome).toBe('updated')
    const fields = await readFields(server.saved[0].input)
    expect(fields.password).toBe('hunter2-plaintext')
    expect(fields.passkeys).toEqual([passkey])
    expect(server.saved[0].input.baseRevision).toBe(2)
  })

  it('makes a new login for a site with none, named for the relying party', async () => {
    const { environment, server } = await unlocked()
    const saveOutcome = await withOpenedKeys(environment, async (keys) => {
      const logins = loginsOf(await openItems(environment, keys))
      return savePasskey(environment, keys, logins, {
        pageUrl: 'https://login.example.com/',
        relyingPartyName: 'Example Inc',
        passkey,
      })
    })
    expect(saveOutcome).toBe('saved')
    expect(await readFields(server.saved[0].input)).toMatchObject({
      name: 'Example Inc',
      userName: 'ada@example.test',
      sites: ['https://login.example.com'],
      passkeys: [passkey],
    })
  })

  it('replaces a passkey for the same relying party and user handle', async () => {
    const id = '00000000-0000-4000-8000-000000000001'
    const older: PasskeyFields = { ...passkey, credentialId: encodeBase64(new Uint8Array([7])) }
    const { environment, server } = await unlocked([
      await ownRecord(id, login({ sites: ['example.com'], passkeys: [older] })),
    ])
    await withOpenedKeys(environment, async (keys) => {
      const logins = loginsOf(await openItems(environment, keys))
      await savePasskey(environment, keys, logins, {
        pageUrl: 'https://example.com/',
        relyingPartyName: 'Example',
        passkey,
      })
    })
    expect((await readFields(server.saved[0].input)).passkeys).toEqual([passkey])
  })

  it('finds passkeys for a relying party, narrowed to the credentials a site allows', async () => {
    const other: PasskeyFields = { ...passkey, credentialId: encodeBase64(new Uint8Array([8])), userName: 'grace' }
    const elsewhere: PasskeyFields = { ...passkey, relyingPartyId: 'other.test' }
    const { environment } = await unlocked([
      await ownRecord('00000000-0000-4000-8000-000000000001', login({ passkeys: [passkey, other, elsewhere] })),
    ])
    await withOpenedKeys(environment, async (keys) => {
      const logins = loginsOf(await openItems(environment, keys))
      expect(
        passkeysForRelyingParty(logins, 'example.com', [], credentialIdOf).map((found) => found.passkey.userName),
      ).toEqual(['ada@example.test', 'grace'])
      const allowed = credentialIdOf(other)
      expect(
        passkeysForRelyingParty(logins, 'example.com', [allowed], credentialIdOf).map(
          (found) => found.passkey.userName,
        ),
      ).toEqual(['grace'])
      expect(passkeysForRelyingParty(logins, 'none.test', [], credentialIdOf)).toEqual([])
    })
  })
})
