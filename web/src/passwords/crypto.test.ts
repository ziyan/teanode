import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { createCipheriv, createDecipheriv, createECDH, createHash, createHmac, hkdfSync, pbkdf2Sync } from 'node:crypto'
import { describe, expect, it } from 'vitest'
import {
  accessSignedBytes,
  AGENT_RECIPIENT_ID,
  areBytesEqual,
  decodeBase32,
  decodeBase64,
  derivePassphraseKey,
  deriveProofKey,
  deriveSharedSecret,
  deriveShareKey,
  deriveWrapKey,
  encodeBase32,
  encodeBase64,
  formatRecoveryKey,
  generatePersonKeyPair,
  hashProof,
  isAccessVerified,
  isKeyPairConsistent,
  itemFieldsAssociatedData,
  itemKeyAssociatedData,
  macOfAccess,
  makeRecoveryKey,
  MAXIMUM_RECORD_VERSION,
  MAXIMUM_ITERATION_COUNT,
  MINIMUM_ITERATION_COUNT,
  openAccountKey,
  openForKeyPair,
  openItemFields,
  openItemKey,
  openPrivateKey,
  openValue,
  parseRecoveryKey,
  parseSealedForKeyPair,
  parseSealedValue,
  normalizeSafetyCode,
  safetyCode,
  sealAccountKey,
  sealForKeyPair,
  sealItemFields,
  sealItemKey,
  sealPrivateKey,
  sealValue,
  serializeSealed,
  shareAssociatedData,
  signAccess,
  verifyAccess,
  withSignedAccess,
} from './crypto'
import { emptyCardFields, emptyLoginFields, emptyNoteFields, type ItemAccess } from './fields'
import {
  bytesFilledWith,
  bytesFromText,
  expectBytes,
  sealForKeyPairWithRandomness,
  sealValueWithNonce,
} from './testSupport'

describe('base64', () => {
  it('round trips and refuses text that is not canonical', () => {
    const bytes = new Uint8Array([0, 1, 2, 250, 251, 252, 253])
    expect(decodeBase64(encodeBase64(bytes))).toEqual(bytes)
    expect(() => decodeBase64('AQI')).toThrow() // padding left off
    expect(() => decodeBase64('A*I=')).toThrow()
  })
})

describe('base32', () => {
  // RFC 4648 section 10, without the padding.
  const cases: Array<[string, string]> = [
    ['', ''],
    ['f', 'MY'],
    ['fo', 'MZXQ'],
    ['foo', 'MZXW6'],
    ['foob', 'MZXW6YQ'],
    ['fooba', 'MZXW6YTB'],
    ['foobar', 'MZXW6YTBOI'],
  ]
  it.each(cases)('writes and reads %j', (text, encoded) => {
    expect(encodeBase32(bytesFromText(text))).toBe(encoded)
    expectBytes(decodeBase32(encoded), bytesFromText(text))
  })

  it('reads lower case, spaces, hyphens and padding', () => {
    expectBytes(decodeBase32('mzxw 6ytb-oi======'), bytesFromText('foobar'))
  })

  it('refuses a letter outside the alphabet', () => {
    expect(() => decodeBase32('MZXW1')).toThrow()
  })
})

describe('sealed values', () => {
  const key = bytesFilledWith(32, 7)
  const nonce = bytesFilledWith(12, 3)

  it('matches AES-256-GCM as another implementation makes it', () => {
    return sealValueWithNonce(key, bytesFromText('hello'), 'some-data', nonce).then((sealed) => {
      expect(sealed.version).toBe(1)
      expect(sealed.nonce).toBe(encodeBase64(nonce))
      const cipher = createCipheriv('aes-256-gcm', key, nonce)
      cipher.setAAD(Buffer.from('some-data'))
      const expected = Buffer.concat([cipher.update('hello'), cipher.final(), cipher.getAuthTag()])
      expect(sealed.ciphertext).toBe(expected.toString('base64'))

      const decipher = createDecipheriv('aes-256-gcm', key, nonce)
      decipher.setAAD(Buffer.from('some-data'))
      const bytes = decodeBase64(sealed.ciphertext)
      decipher.setAuthTag(Buffer.from(bytes.slice(bytes.length - 16)))
      expect(Buffer.concat([decipher.update(bytes.slice(0, bytes.length - 16)), decipher.final()]).toString()).toBe(
        'hello',
      )
    })
  })

  it('opens what it sealed and draws a fresh nonce every time', async () => {
    const first = await sealValue(key, bytesFromText('hello'), 'data')
    const second = await sealValue(key, bytesFromText('hello'), 'data')
    expect(first.nonce).not.toBe(second.nonce)
    expectBytes(await openValue(key, first, 'data'), bytesFromText('hello'))
  })

  it('refuses the wrong associated data, key, version or a changed byte', async () => {
    const sealed = await sealValueWithNonce(key, bytesFromText('hello'), 'data', nonce)
    await expect(openValue(key, sealed, 'other')).rejects.toThrow(/could not be opened/)
    await expect(openValue(bytesFilledWith(32, 8), sealed, 'data')).rejects.toThrow(/could not be opened/)
    await expect(openValue(key, { ...sealed, version: 2 }, 'data')).rejects.toThrow(/version/)
    const bytes = decodeBase64(sealed.ciphertext)
    bytes[0] ^= 1
    await expect(openValue(key, { ...sealed, ciphertext: encodeBase64(bytes) }, 'data')).rejects.toThrow(
      /could not be opened/,
    )
  })

  it('refuses a key or nonce of the wrong length', async () => {
    await expect(sealValue(new Uint8Array(16), bytesFromText('x'), 'data')).rejects.toThrow(/32 bytes/)
    await expect(sealValueWithNonce(key, bytesFromText('x'), 'data', new Uint8Array(8))).rejects.toThrow(/12 bytes/)
  })

  it('is written to and read from the text of a column', async () => {
    const sealed = await sealValueWithNonce(key, bytesFromText('hello'), 'data', nonce)
    expect(parseSealedValue(serializeSealed(sealed))).toEqual(sealed)
    expect(() => parseSealedValue('{"version":1}')).toThrow()
    expect(() => parseSealedValue('[]')).toThrow()
    expect(() => parseSealedForKeyPair('{"version":1,"nonce":"","ciphertext":""}')).toThrow()
  })
})

describe('keys from the passphrase and the recovery key', () => {
  const salt = bytesFilledWith(16, 9)

  it('derives the passphrase key as PBKDF2-SHA256 of the NFC text', async () => {
    const composed = 'café 密码'
    const decomposed = 'café 密码'
    const expected = pbkdf2Sync(composed, salt, MINIMUM_ITERATION_COUNT, 32, 'sha256')
    expectBytes(await derivePassphraseKey(composed, salt, MINIMUM_ITERATION_COUNT), expected)
    expectBytes(await derivePassphraseKey(decomposed, salt, MINIMUM_ITERATION_COUNT), expected)
  })

  it('refuses an iteration count below the minimum', async () => {
    await expect(derivePassphraseKey('passphrase', salt, MINIMUM_ITERATION_COUNT - 1)).rejects.toThrow(/at least/)
    await expect(derivePassphraseKey('passphrase', salt, 1000)).rejects.toThrow(/at least/)
    await expect(derivePassphraseKey('passphrase', salt, 100000)).rejects.toThrow(/at least/)
    await expect(derivePassphraseKey('passphrase', salt, 600000.5)).rejects.toThrow(/at least/)
  })

  it('refuses an iteration count above the maximum', async () => {
    await expect(derivePassphraseKey('passphrase', salt, MAXIMUM_ITERATION_COUNT + 1)).rejects.toThrow(/at most/)
  })

  it('derives the wrap and proof keys as HKDF-SHA256 with an empty salt', async () => {
    const secret = bytesFilledWith(32, 5)
    const wrapKey = await deriveWrapKey(secret)
    const proofKey = await deriveProofKey(secret)
    expectBytes(wrapKey, new Uint8Array(hkdfSync('sha256', secret, new Uint8Array(0), 'teanode passwords wrap v1', 32)))
    expectBytes(
      proofKey,
      new Uint8Array(hkdfSync('sha256', secret, new Uint8Array(0), 'teanode passwords proof v1', 32)),
    )
    expect(areBytesEqual(wrapKey, proofKey)).toBe(false)
  })

  it('hashes a proof as base64 SHA-256', async () => {
    const proof = bytesFilledWith(32, 1)
    expect(await hashProof(proof)).toBe(createHash('sha256').update(proof).digest('base64'))
  })

  it('wraps the account key under the wrap key and nothing else', async () => {
    const accountKey = bytesFilledWith(32, 2)
    const wrapKey = await deriveWrapKey(bytesFilledWith(32, 5))
    const sealed = await sealAccountKey(wrapKey, accountKey)
    expectBytes(await openAccountKey(wrapKey, sealed), accountKey)
    await expect(openValue(wrapKey, sealed, 'private-key')).rejects.toThrow()
  })
})

describe('recovery key', () => {
  it('is 32 random bytes written as 13 groups of 4 letters', () => {
    const recoveryKey = makeRecoveryKey()
    expect(recoveryKey.length).toBe(32)
    expect(formatRecoveryKey(recoveryKey)).toMatch(/^([A-Z2-7]{4} ){12}[A-Z2-7]{4}$/)
  })

  it('reads back what it wrote, ignoring case, spaces and hyphens', () => {
    const recoveryKey = makeRecoveryKey()
    const text = formatRecoveryKey(recoveryKey)
    expectBytes(parseRecoveryKey(text), recoveryKey)
    expectBytes(parseRecoveryKey(text.toLowerCase().replace(/ /g, '-')), recoveryKey)
    expectBytes(parseRecoveryKey(`  ${text.replace(/ /g, '')}\n`), recoveryKey)
  })

  it('refuses a typo, a short key and a letter outside the alphabet', () => {
    const text = formatRecoveryKey(bytesFilledWith(32, 0))
    expect(() => parseRecoveryKey(text.slice(0, -2))).toThrow()
    expect(() => parseRecoveryKey(text + 'AAAA')).toThrow()
    expect(() => parseRecoveryKey('1' + text.slice(1))).toThrow()
    // The last letter carries four bits that are always zero; a key with
    // them set was not written by formatRecoveryKey.
    expect(() => parseRecoveryKey(text.slice(0, -1) + 'B')).toThrow()
  })

  it('reads ASCII only: no case mapping of other letters, no other spaces or dashes, no padding', () => {
    const text = formatRecoveryKey(bytesFilledWith(32, 0))
    const letters = text.replace(/ /g, '')
    for (const other of ['\u00df', '\u017f', '\u0131', '\u00a0', '\u200b', '\u2013', '\uff21']) {
      expect(() => parseRecoveryKey(letters.slice(0, 10) + other + letters.slice(10)), other).toThrow()
      expect(() => parseRecoveryKey(other + letters.slice(1)), other).toThrow()
    }
    expect(() => parseRecoveryKey(letters + '====')).toThrow()
    expect(() => parseRecoveryKey(letters.slice(0, 20) + '=' + letters.slice(21))).toThrow()
    // Tabs and line breaks are white space.
    expectBytes(parseRecoveryKey(`\t${text.replace(/ /g, '\r\n')}\n`), bytesFilledWith(32, 0))
  })
})

describe('person key pair', () => {
  it('has a 32 byte scalar and a 65 byte uncompressed point that belong together', async () => {
    const keyPair = await generatePersonKeyPair()
    expect(keyPair.privateKey.length).toBe(32)
    expect(keyPair.publicKey.length).toBe(65)
    expect(keyPair.publicKey[0]).toBe(4)
    const reference = createECDH('prime256v1')
    reference.setPrivateKey(keyPair.privateKey)
    expectBytes(keyPair.publicKey, reference.getPublicKey())
  })

  it('agrees on the X coordinate with another implementation, from both sides', async () => {
    const first = await generatePersonKeyPair()
    const second = await generatePersonKeyPair()
    const reference = createECDH('prime256v1')
    reference.setPrivateKey(first.privateKey)
    const expected = reference.computeSecret(second.publicKey)
    expectBytes(await deriveSharedSecret(first, second.publicKey), expected)
    expectBytes(await deriveSharedSecret(second, first.publicKey), expected)
  })

  it('writes a safety code as eight groups of four digits from the SHA-256 of the label and key', async () => {
    const keyPair = await generatePersonKeyPair()
    const digest = createHash('sha256')
      .update(Buffer.concat([Buffer.from('teanode passwords safety code v2\0'), keyPair.publicKey]))
      .digest()
    const expected = [0, 1, 2, 3, 4, 5, 6, 7]
      .map((index) => String(digest.readUInt16BE(index * 2) % 10000).padStart(4, '0'))
      .join(' ')
    expect(await safetyCode(keyPair.publicKey)).toBe(expected)
    expect(expected).toMatch(/^\d{4}( \d{4}){7}$/)
    expect(await safetyCode((await generatePersonKeyPair()).publicKey)).not.toBe(expected)
  })

  it('reads a typed safety code with or without spaces and refuses anything else', () => {
    const code = '0123 4567 8901 2345 6789 0123 4567 8901'
    expect(normalizeSafetyCode(code)).toBe(code)
    expect(normalizeSafetyCode(code.replace(/ /g, ''))).toBe(code)
    expect(normalizeSafetyCode(`  ${code}\n`)).toBe(code)
    expect(normalizeSafetyCode('0123\u00a04567 89012345 67890123 4567 8901')).toBe(code)
    for (const text of [
      '',
      '0123 4567 8901 2345 6789',
      `${code} 2`,
      code.replace(/ /g, '-'),
      `${code.slice(0, -1)}x`,
      `\u0663${code.slice(1)}`,
    ]) {
      expect(normalizeSafetyCode(text), JSON.stringify(text)).toBeNull()
    }
  })

  it('refuses a public key that is not an uncompressed point', async () => {
    await expect(safetyCode(new Uint8Array(33))).rejects.toThrow(/65 byte/)
  })
})

describe('sealed for a key pair', () => {
  const itemKey = bytesFilledWith(32, 4)
  const nonce = bytesFilledWith(12, 6)

  it('opens with the recipient key pair and the owner key, and matches an independent derivation', async () => {
    const owner = await generatePersonKeyPair()
    const recipient = await generatePersonKeyPair()
    const ephemeral = await generatePersonKeyPair()
    const sealed = await sealForKeyPairWithRandomness(
      itemKey,
      owner,
      recipient.publicKey,
      'item-1',
      'owner-1',
      'person-1',
      ephemeral,
      nonce,
    )
    expect(sealed.ephemeralPublicKey).toBe(encodeBase64(ephemeral.publicKey))
    expectBytes(await openForKeyPair(sealed, recipient, owner.publicKey, 'item-1', 'owner-1', 'person-1'), itemKey)

    const ephemeralReference = createECDH('prime256v1')
    ephemeralReference.setPrivateKey(ephemeral.privateKey)
    const ephemeralShared = ephemeralReference.computeSecret(recipient.publicKey)
    const ownerReference = createECDH('prime256v1')
    ownerReference.setPrivateKey(owner.privateKey)
    const ownerShared = ownerReference.computeSecret(recipient.publicKey)
    const key = Buffer.from(
      hkdfSync(
        'sha256',
        Buffer.concat([ephemeralShared, ownerShared]),
        recipient.publicKey,
        'teanode passwords share v1',
        32,
      ),
    )
    expectBytes(await deriveShareKey(ephemeralShared, ownerShared, recipient.publicKey), key)
    const cipher = createCipheriv('aes-256-gcm', key, nonce)
    cipher.setAAD(Buffer.from('share:item-1:owner-1:person-1'))
    const expected = Buffer.concat([cipher.update(itemKey), cipher.final(), cipher.getAuthTag()])
    expect(sealed.ciphertext).toBe(expected.toString('base64'))
  })

  it('draws a fresh ephemeral key each time', async () => {
    const owner = await generatePersonKeyPair()
    const recipient = await generatePersonKeyPair()
    const first = await sealForKeyPair(itemKey, owner, recipient.publicKey, 'item-1', 'owner-1', AGENT_RECIPIENT_ID)
    const second = await sealForKeyPair(itemKey, owner, recipient.publicKey, 'item-1', 'owner-1', AGENT_RECIPIENT_ID)
    expect(first.ephemeralPublicKey).not.toBe(second.ephemeralPublicKey)
    expectBytes(
      await openForKeyPair(first, recipient, owner.publicKey, 'item-1', 'owner-1', AGENT_RECIPIENT_ID),
      itemKey,
    )
  })

  it('refuses another recipient, item, owner id, recipient id, owner key and a changed ephemeral key', async () => {
    const owner = await generatePersonKeyPair()
    const recipient = await generatePersonKeyPair()
    const stranger = await generatePersonKeyPair()
    const sealed = await sealForKeyPair(itemKey, owner, recipient.publicKey, 'item-1', 'owner-1', 'person-1')
    const open = (
      overrides: Partial<{
        sealed: typeof sealed
        recipient: typeof recipient
        ownerPublicKey: Uint8Array
        itemId: string
        ownerId: string
        recipientId: string
      }> = {},
    ) =>
      openForKeyPair(
        overrides.sealed ?? sealed,
        overrides.recipient ?? recipient,
        overrides.ownerPublicKey ?? owner.publicKey,
        overrides.itemId ?? 'item-1',
        overrides.ownerId ?? 'owner-1',
        overrides.recipientId ?? 'person-1',
      )
    expectBytes(await open(), itemKey)
    await expect(open({ recipient: stranger })).rejects.toThrow()
    await expect(open({ itemId: 'item-2' })).rejects.toThrow()
    await expect(open({ ownerId: 'owner-2' })).rejects.toThrow()
    await expect(open({ recipientId: 'person-2' })).rejects.toThrow()
    // A share the server made without the owner's private key does not open
    // with the owner's real public key, and a real share does not open with
    // another owner's key.
    await expect(open({ ownerPublicKey: stranger.publicKey })).rejects.toThrow()
    await expect(
      open({ sealed: { ...sealed, ephemeralPublicKey: encodeBase64(stranger.publicKey) } }),
    ).rejects.toThrow()
  })

  it('does not open a share a stranger sealed in the name of the owner', async () => {
    const owner = await generatePersonKeyPair()
    const stranger = await generatePersonKeyPair()
    const recipient = await generatePersonKeyPair()
    const forged = await sealForKeyPair(itemKey, stranger, recipient.publicKey, 'item-1', 'owner-1', 'person-1')
    await expect(openForKeyPair(forged, recipient, owner.publicKey, 'item-1', 'owner-1', 'person-1')).rejects.toThrow()
    expectBytes(await openForKeyPair(forged, recipient, stranger.publicKey, 'item-1', 'owner-1', 'person-1'), itemKey)
  })

  it('says whether a public key belongs to the private scalar', async () => {
    const first = await generatePersonKeyPair()
    const second = await generatePersonKeyPair()
    expect(await isKeyPairConsistent(first)).toBe(true)
    expect(await isKeyPairConsistent({ privateKey: first.privateKey, publicKey: second.publicKey })).toBe(false)
    expect(await isKeyPairConsistent({ privateKey: first.privateKey, publicKey: new Uint8Array(65) })).toBe(false)
  })
})

describe('the sealing that fixes its randomness', () => {
  // sealValueWithNonce and sealForKeyPairWithRandomness reuse a nonce on
  // request, which breaks AES-GCM. Only tests may import them, through
  // testSupport.ts.
  function sourceFiles(directory: string): string[] {
    return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
      const path = join(directory, entry.name)
      if (entry.isDirectory()) return sourceFiles(path)
      return /\.(ts|tsx)$/.test(entry.name) ? [path] : []
    })
  }

  it('are used by no file but the tests and testSupport.ts', () => {
    const roots = [join(import.meta.dirname, '..'), join(import.meta.dirname, '../../extension')]
    const offenders: string[] = []
    for (const root of roots) {
      for (const file of sourceFiles(root)) {
        if (
          /\.test\.tsx?$/.test(file) ||
          file.endsWith('/passwords/testSupport.ts') ||
          file.endsWith('/passwords/crypto.ts')
        ) {
          continue
        }
        if (/sealValueWithNonce|sealForKeyPairWithRandomness/.test(readFileSync(file, 'utf8'))) offenders.push(file)
      }
    }
    expect(offenders).toEqual([])
  })
})

describe('the values of an account and its items', () => {
  const accountKey = bytesFilledWith(32, 1)
  const itemKey = bytesFilledWith(32, 2)
  const itemId = '11111111-2222-4333-8444-555555555555'

  it('names the associated data as the plan does', () => {
    expect(itemKeyAssociatedData(itemId)).toBe(`item-key:${itemId}`)
    expect(itemFieldsAssociatedData(itemId, 'card')).toBe(`item:${itemId}:card`)
    expect(shareAssociatedData(itemId, 'owner-1', AGENT_RECIPIENT_ID)).toBe(`share:${itemId}:owner-1:agent`)
  })

  it('seals the private key and an item key under the account key', async () => {
    const privateKey = bytesFilledWith(32, 9)
    expectBytes(await openPrivateKey(accountKey, await sealPrivateKey(accountKey, privateKey)), privateKey)
    const sealed = await sealItemKey(accountKey, itemKey, itemId)
    expectBytes(await openItemKey(accountKey, sealed, itemId), itemKey)
    await expect(openItemKey(accountKey, sealed, '99999999-2222-4333-8444-555555555555')).rejects.toThrow()
    await expect(openPrivateKey(accountKey, sealed)).rejects.toThrow()
  })

  it('seals the fields of each kind and ties them to the item and its kind', async () => {
    const login = {
      ...emptyLoginFields(),
      name: 'Example',
      userName: 'ada',
      password: 'päss',
      sites: ['https://example.com'],
    }
    const sealedLogin = await sealItemFields(itemKey, login, itemId, 'login')
    expect(await openItemFields(itemKey, sealedLogin, itemId, 'login')).toEqual(login)
    await expect(openItemFields(itemKey, sealedLogin, itemId, 'note')).rejects.toThrow()

    const card = { ...emptyCardFields(), name: 'Card', number: '4111 1111 1111 1111' }
    expect(await openItemFields(itemKey, await sealItemFields(itemKey, card, itemId, 'card'), itemId, 'card')).toEqual(
      card,
    )
    const note = { ...emptyNoteFields(), name: 'Note', text: 'line one\nline two' }
    expect(await openItemFields(itemKey, await sealItemFields(itemKey, note, itemId, 'note'), itemId, 'note')).toEqual(
      note,
    )
  })
})

describe('the access record signature', () => {
  const accountKey = bytesFilledWith(32, 0xa0)
  const access: ItemAccess = {
    isUsableByAgent: true,
    grantees: [{ userId: 'user-ben' }, { circleId: 'circle-1' }],
  }

  it('signs the documented bytes with HMAC-SHA256 under HKDF of the account key', async () => {
    const record: ItemAccess = { ...access, recordVersion: 12 }
    const signedBytes = accessSignedBytes('item-1', 'note', record)
    expect(Buffer.from(signedBytes).toString('latin1')).toBe(
      'teanode passwords access record v1\0item-1\0note\x0012\x001\0c:circle-1\0u:user-ben\0',
    )
    const macKey = Buffer.from(
      hkdfSync('sha256', accountKey, Buffer.alloc(0), 'teanode passwords access record v1', 32),
    )
    const expected = createHmac('sha256', macKey).update(signedBytes).digest('base64')
    expect(await macOfAccess(accountKey, 'item-1', 'note', record)).toBe(expected)
    const off = accessSignedBytes('item-1', 'note', { isUsableByAgent: false, grantees: [], recordVersion: 12 })
    expect(Buffer.from(off).toString('latin1')).toBe('teanode passwords access record v1\0item-1\0note\x0012\x000\0')
  })

  it('puts the grantees in UTF-8 byte order, not UTF-16 order', () => {
    // U+FF5E is three bytes starting 0xef and U+1F600 is four bytes starting
    // 0xf0, so byte order puts the first one first; UTF-16 order would not.
    const bytes = accessSignedBytes('item-1', 'login', {
      isUsableByAgent: false,
      grantees: [{ userId: '\u{1F600}' }, { userId: '\uFF5E' }],
      recordVersion: 1,
    })
    expect(Buffer.from(bytes).toString('utf8').endsWith('u:\uFF5E\0u:\u{1F600}\0')).toBe(true)
  })

  it('signs one more than the highest version seen and ignores the version and signature it was given', async () => {
    const signed = await signAccess(accountKey, 'item-1', 'login', access, 4)
    expect(signed.recordVersion).toBe(5)
    expect(await isAccessVerified(accountKey, 'item-1', 'login', signed)).toBe(true)
    const again = await signAccess(accountKey, 'item-1', 'login', { ...signed, recordVersion: 900, recordMac: 'x' }, 4)
    expect(again).toEqual(signed)
    expect(access).not.toHaveProperty('recordVersion')
    const fromNothing = await signAccess(accountKey, 'item-1', 'login', undefined, 0)
    expect(fromNothing).toMatchObject({ isUsableByAgent: false, grantees: [], recordVersion: 1 })
  })

  it('does not care about the order of the grantees', async () => {
    const signed = await signAccess(accountKey, 'item-1', 'login', access, 0)
    const reversed = { ...signed, grantees: [...signed.grantees].reverse() }
    expect(await isAccessVerified(accountKey, 'item-1', 'login', reversed)).toBe(true)
  })

  it('refuses a record that changed in anything the signature covers', async () => {
    const signed = await signAccess(accountKey, 'item-1', 'login', access, 4)
    const flipped = Buffer.from(signed.recordMac!, 'base64')
    flipped[0] ^= 1
    const cases: Array<[string, Uint8Array, string, 'login' | 'note', ItemAccess | undefined]> = [
      ['another account key', bytesFilledWith(32, 0xb0), 'item-1', 'login', signed],
      ['another item', accountKey, 'item-2', 'login', signed],
      ['another kind', accountKey, 'item-1', 'note', signed],
      ['agent use', accountKey, 'item-1', 'login', { ...signed, isUsableByAgent: false }],
      ['version', accountKey, 'item-1', 'login', { ...signed, recordVersion: 6 }],
      [
        'a grantee',
        accountKey,
        'item-1',
        'login',
        { ...signed, grantees: [...signed.grantees, { userId: 'user-cy' }] },
      ],
      [
        'a user turned into a circle',
        accountKey,
        'item-1',
        'login',
        { ...signed, grantees: [{ circleId: 'user-ben' }, { circleId: 'circle-1' }] },
      ],
      ['a flipped signature bit', accountKey, 'item-1', 'login', { ...signed, recordMac: flipped.toString('base64') }],
      ['a signature that is not base64', accountKey, 'item-1', 'login', { ...signed, recordMac: '%%%' }],
      ['no signature', accountKey, 'item-1', 'login', { ...signed, recordMac: undefined }],
      ['no version', accountKey, 'item-1', 'login', { ...signed, recordVersion: undefined }],
      ['no record', accountKey, 'item-1', 'login', undefined],
    ]
    for (const [name, key, itemId, itemKind, record] of cases) {
      expect(await isAccessVerified(key, itemId, itemKind, record), name).toBe(false)
      expect(await verifyAccess(key, itemId, itemKind, { ...emptyNoteFields(), access: record }), name).toBeNull()
    }
  })

  it('refuses bad input to sign', async () => {
    await expect(signAccess(bytesFilledWith(16, 1), 'item-1', 'login', access, 0)).rejects.toThrow()
    await expect(signAccess(accountKey, 'item-1', 'secret' as 'login', access, 0)).rejects.toThrow()
    await expect(signAccess(accountKey, 'item-1', 'login', access, -1)).rejects.toThrow()
    await expect(signAccess(accountKey, 'item-1', 'login', access, 1.5)).rejects.toThrow()
    await expect(signAccess(accountKey, 'item-1', 'login', access, MAXIMUM_RECORD_VERSION)).rejects.toThrow()
    await expect(
      signAccess(
        accountKey,
        'item-1',
        'login',
        { isUsableByAgent: false, grantees: [{ userId: 'a', circleId: 'b' }] } as unknown as ItemAccess,
        0,
      ),
    ).rejects.toThrow()
  })

  it('puts the signed record into the fields and verifies it from them', async () => {
    const fields = { ...emptyNoteFields(), name: 'Example', text: 'hello', futureKey: [1, 2] }
    const signedFields = await withSignedAccess(accountKey, 'item-1', 'note', fields, access, 2)
    expect(signedFields).toMatchObject({ name: 'Example', futureKey: [1, 2] })
    expect(signedFields.access?.recordVersion).toBe(3)
    expect(await verifyAccess(accountKey, 'item-1', 'note', signedFields)).toEqual(signedFields.access)
    expect(await verifyAccess(accountKey, 'item-2', 'note', signedFields)).toBeNull()
    expect(await verifyAccess(accountKey, 'item-1', 'note', fields)).toBeNull()
    expect(fields).not.toHaveProperty('access')
  })
})
