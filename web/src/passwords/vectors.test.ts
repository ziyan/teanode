// The shared test vectors (internal/passwords/testdata/vectors.json), written
// by the Go tests and read here, in TeaNodeKit and in Go itself. Every derived
// value is worked out again from the inputs, every sealed value is opened, and
// the signature Go made is verified.
//
// While the file is missing the suite is skipped, except in CI, where a missing
// file is a failure: a skipped check there would pass for the wrong reason.

import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import {
  decodeBase64,
  derivePassphraseKey,
  deriveProofKey,
  deriveSharedSecret,
  deriveShareKey,
  deriveWrapKey,
  formatRecoveryKey,
  generatePersonKeyPair,
  hashProof,
  openForKeyPair,
  openItemFields,
  openValue,
  normalizeSafetyCode,
  parseRecoveryKey,
  safetyCode,
  type SealedForKeyPair,
  type SealedValue,
} from './crypto'
import { type ItemKind, parseItemFields } from './fields'
import { oneTimeCode, parseOneTimeCodeUri } from './oneTimeCode'
import {
  assertionAuthenticatorData,
  encodeAttestationObject,
  encodeCosePublicKey,
  readPasskeyPublicKey,
  registrationAuthenticatorData,
  signAssertion,
  verifyAssertion,
} from './passkey'
import { expectBytes, sealForKeyPairWithRandomness, sealValueWithNonce } from './testSupport'

interface Vectors {
  passphrase: {
    passphrase: string
    salt: string
    iterationCount: number
    passphraseKey: string
    wrapKey: string
    proof: string
    proofHash: string
  }
  recoveryKey: {
    bytes: string
    text: string
    acceptedTexts: string[]
    wrapKey: string
    proof: string
    proofHash: string
  }
  recoveryKeyInvalid: string[]
  sealed: Array<{
    name: string
    key: string
    nonce: string
    associatedData: string
    plaintext: string
    sealed: SealedValue
  }>
  personKey: {
    privateKey: string
    publicKey: string
    userId: string
    safetyCode: string
    typedSafetyCodes: string[]
    invalidSafetyCodes: string[]
  }
  share: {
    ownerId: string
    recipientId: string
    itemId: string
    senderPrivateKey: string
    senderPublicKey: string
    recipientPrivateKey: string
    recipientPublicKey: string
    ephemeralPrivateKey: string
    ephemeralPublicKey: string
    ephemeralShared: string
    ownerShared: string
    key: string
    itemKey: string
    nonce: string
    sealed: SealedForKeyPair
  }
  oneTimeCodes: Array<{ uri: string; unixSeconds: number; code: string }>
  passkey: {
    privateKey: string
    publicKeyX: string
    publicKeyY: string
    relyingPartyId: string
    credentialId: string
    isVerified: boolean
    cosePublicKey: string
    registrationAuthenticatorData: string
    attestationObject: string
    assertionAuthenticatorData: string
    clientDataHash: string
    signature: string
  }
}

const vectorsPath = resolve(import.meta.dirname, '../../../internal/passwords/testdata/vectors.json')
const hasVectors = existsSync(vectorsPath)
const isRequired = Boolean(process.env.CI)

// bytesOf reads the spaced lower case hex ("68 2a b9") the file writes every
// byte value in. Only the sealed objects stay base64, since that is their
// format.
function bytesOf(text: string): Uint8Array {
  const pairs = text.trim() === '' ? [] : text.trim().split(/ +/)
  return new Uint8Array(
    pairs.map((pair) => {
      if (!/^[0-9a-f]{2}$/.test(pair)) throw new Error(`not a hex byte: ${pair}`)
      return parseInt(pair, 16)
    }),
  )
}

describe.skipIf(!hasVectors && !isRequired)('the shared test vectors', () => {
  if (!hasVectors) {
    it('exist', () => {
      throw new Error(`${vectorsPath} is missing; run the Go test that writes it`)
    })
    return
  }
  const vectors = JSON.parse(readFileSync(vectorsPath, 'utf8')) as Vectors

  it('derives the keys of the passphrase', async () => {
    const vector = vectors.passphrase
    const passphraseKey = await derivePassphraseKey(vector.passphrase, bytesOf(vector.salt), vector.iterationCount)
    expectBytes(passphraseKey, bytesOf(vector.passphraseKey))
    expectBytes(await deriveWrapKey(passphraseKey), bytesOf(vector.wrapKey))
    const proof = await deriveProofKey(passphraseKey)
    expectBytes(proof, bytesOf(vector.proof))
    expectBytes(decodeBase64(await hashProof(proof)), bytesOf(vector.proofHash))
  })

  it('derives the keys of the recovery key and writes and reads its text', async () => {
    const vector = vectors.recoveryKey
    const bytes = bytesOf(vector.bytes)
    expect(formatRecoveryKey(bytes)).toBe(vector.text)
    expectBytes(parseRecoveryKey(vector.text), bytes)
    expectBytes(await deriveWrapKey(bytes), bytesOf(vector.wrapKey))
    const proof = await deriveProofKey(bytes)
    expectBytes(proof, bytesOf(vector.proof))
    expectBytes(decodeBase64(await hashProof(proof)), bytesOf(vector.proofHash))
  })

  it('reads every accepted spelling of the recovery key and refuses every invalid text', () => {
    const bytes = bytesOf(vectors.recoveryKey.bytes)
    expect(vectors.recoveryKey.acceptedTexts.length).toBeGreaterThan(0)
    for (const text of vectors.recoveryKey.acceptedTexts) {
      expectBytes(parseRecoveryKey(text), bytes, JSON.stringify(text))
    }
    expect(vectors.recoveryKeyInvalid.length).toBeGreaterThan(0)
    for (const text of vectors.recoveryKeyInvalid) {
      expect(() => parseRecoveryKey(text), JSON.stringify(text)).toThrow()
    }
  })

  it('seals every value to the same bytes and opens it', async () => {
    expect(vectors.sealed.length).toBeGreaterThan(0)
    for (const vector of vectors.sealed) {
      const key = bytesOf(vector.key)
      const plaintext = bytesOf(vector.plaintext)
      expect(
        await sealValueWithNonce(key, plaintext, vector.associatedData, bytesOf(vector.nonce)),
        vector.name,
      ).toEqual(vector.sealed)
      expectBytes(await openValue(key, vector.sealed, vector.associatedData), plaintext, vector.name)
      await expect(openValue(key, vector.sealed, `${vector.associatedData}x`), vector.name).rejects.toThrow()
    }
  })

  it('opens the item fields as an item of their kind', async () => {
    const vector = vectors.sealed.find((candidate) => candidate.associatedData.startsWith('item:'))
    expect(vector, 'a sealed item fields vector').toBeDefined()
    const [, itemId, itemKind] = vector!.associatedData.split(':')
    const fields = await openItemFields(bytesOf(vector!.key), vector!.sealed, itemId, itemKind as ItemKind)
    expect(JSON.parse(new TextDecoder().decode(bytesOf(vector!.plaintext)))).toEqual(fields)
  })

  it('reads the access record of the item fields and keeps it through an edit', async () => {
    const vector = vectors.sealed.find((candidate) => candidate.associatedData.startsWith('item:'))!
    const [, itemId, itemKind] = vector.associatedData.split(':')
    const opened = await openItemFields(bytesOf(vector.key), vector.sealed, itemId, itemKind as ItemKind)
    const parsed = parseItemFields(itemKind as ItemKind, opened)
    expect(parsed?.access).toEqual({
      isUsableByAgent: true,
      grantees: [{ userId: vectors.share.recipientId }, { circleId: expect.any(String) }],
    })
    const edited = parseItemFields(itemKind as ItemKind, JSON.parse(JSON.stringify({ ...parsed, name: 'edited' })))
    expect(edited?.access).toEqual(parsed?.access)
  })

  it('has a person key pair whose scalar, point and safety code agree', async () => {
    const vector = vectors.personKey
    const keyPair = { privateKey: bytesOf(vector.privateKey), publicKey: bytesOf(vector.publicKey) }
    expect(await safetyCode(vector.userId, keyPair.publicKey)).toBe(vector.safetyCode)
    expect(await safetyCode(`${vector.userId}x`, keyPair.publicKey)).not.toBe(vector.safetyCode)
    for (const text of [vector.safetyCode, ...vector.typedSafetyCodes]) {
      expect(normalizeSafetyCode(text), JSON.stringify(text)).toBe(vector.safetyCode)
    }
    expect(vector.invalidSafetyCodes.length).toBeGreaterThan(0)
    for (const text of vector.invalidSafetyCodes) {
      expect(normalizeSafetyCode(text), JSON.stringify(text)).toBeNull()
    }
    // Two key pairs agree on a shared secret only if each point belongs to its scalar.
    const other = await generatePersonKeyPair()
    expectBytes(await deriveSharedSecret(keyPair, other.publicKey), await deriveSharedSecret(other, keyPair.publicKey))
  })

  it('shares an item key to the same bytes from the owner and ephemeral keys and opens it', async () => {
    const vector = vectors.share
    const owner = { privateKey: bytesOf(vector.senderPrivateKey), publicKey: bytesOf(vector.senderPublicKey) }
    const recipient = { privateKey: bytesOf(vector.recipientPrivateKey), publicKey: bytesOf(vector.recipientPublicKey) }
    const ephemeral = { privateKey: bytesOf(vector.ephemeralPrivateKey), publicKey: bytesOf(vector.ephemeralPublicKey) }
    const itemKey = bytesOf(vector.itemKey)

    // Both ends reach both secrets.
    const ephemeralShared = await deriveSharedSecret(ephemeral, recipient.publicKey)
    expectBytes(ephemeralShared, bytesOf(vector.ephemeralShared))
    expectBytes(await deriveSharedSecret(recipient, ephemeral.publicKey), bytesOf(vector.ephemeralShared))
    const ownerShared = await deriveSharedSecret(owner, recipient.publicKey)
    expectBytes(ownerShared, bytesOf(vector.ownerShared))
    expectBytes(await deriveSharedSecret(recipient, owner.publicKey), bytesOf(vector.ownerShared))
    expectBytes(await deriveShareKey(ephemeralShared, ownerShared, recipient.publicKey), bytesOf(vector.key))

    const sealed = await sealForKeyPairWithRandomness(
      itemKey,
      owner,
      recipient.publicKey,
      vector.itemId,
      vector.ownerId,
      vector.recipientId,
      ephemeral,
      bytesOf(vector.nonce),
    )
    expect(sealed).toEqual(vector.sealed)
    expectBytes(
      await openForKeyPair(
        vector.sealed,
        recipient,
        owner.publicKey,
        vector.itemId,
        vector.ownerId,
        vector.recipientId,
      ),
      itemKey,
    )
  })

  it('does not open a share with another owner key, owner id, recipient id or item', async () => {
    const vector = vectors.share
    const recipient = { privateKey: bytesOf(vector.recipientPrivateKey), publicKey: bytesOf(vector.recipientPublicKey) }
    const ownerPublicKey = bytesOf(vector.senderPublicKey)
    const otherOwner = await generatePersonKeyPair()
    const open = (publicKey: Uint8Array, itemId: string, ownerId: string, recipientId: string) =>
      openForKeyPair(vector.sealed, recipient, publicKey, itemId, ownerId, recipientId)
    await expect(open(otherOwner.publicKey, vector.itemId, vector.ownerId, vector.recipientId)).rejects.toThrow()
    // The ephemeral key stands in for any other key of the right shape.
    await expect(
      open(bytesOf(vector.ephemeralPublicKey), vector.itemId, vector.ownerId, vector.recipientId),
    ).rejects.toThrow()
    await expect(open(ownerPublicKey, vector.itemId, vector.recipientId, vector.recipientId)).rejects.toThrow()
    await expect(open(ownerPublicKey, vector.itemId, vector.ownerId, vector.ownerId)).rejects.toThrow()
    await expect(open(ownerPublicKey, `${vector.itemId}x`, vector.ownerId, vector.recipientId)).rejects.toThrow()
  })

  it('makes every one-time code', async () => {
    expect(vectors.oneTimeCodes.length).toBeGreaterThan(0)
    for (const vector of vectors.oneTimeCodes) {
      expect(
        await oneTimeCode(parseOneTimeCodeUri(vector.uri), vector.unixSeconds),
        `${vector.uri} at ${vector.unixSeconds}`,
      ).toBe(vector.code)
    }
  })

  describe('passkey', () => {
    const vector = vectors.passkey
    const publicKeyX = bytesOf(vector.publicKeyX)
    const publicKeyY = bytesOf(vector.publicKeyY)

    it('works out the public key from the private key', async () => {
      const publicKey = await readPasskeyPublicKey(bytesOf(vector.privateKey))
      expectBytes(publicKey.publicKeyX, publicKeyX)
      expectBytes(publicKey.publicKeyY, publicKeyY)
    })

    it('writes the same public key, registration data and attestation object', async () => {
      const cosePublicKey = encodeCosePublicKey(publicKeyX, publicKeyY)
      expectBytes(cosePublicKey, bytesOf(vector.cosePublicKey))
      const authenticatorData = await registrationAuthenticatorData({
        relyingPartyId: vector.relyingPartyId,
        credentialId: bytesOf(vector.credentialId),
        publicKeyX,
        publicKeyY,
        isVerified: vector.isVerified,
      })
      expectBytes(authenticatorData, bytesOf(vector.registrationAuthenticatorData))
      expectBytes(encodeAttestationObject(authenticatorData), bytesOf(vector.attestationObject))
    })

    it('writes the same assertion data, verifies the signature Go made and signs on its own', async () => {
      const authenticatorData = await assertionAuthenticatorData(vector.relyingPartyId, vector.isVerified)
      expectBytes(authenticatorData, bytesOf(vector.assertionAuthenticatorData))
      const clientDataHash = bytesOf(vector.clientDataHash)

      expect(
        await verifyAssertion(publicKeyX, publicKeyY, authenticatorData, clientDataHash, bytesOf(vector.signature)),
      ).toBe(true)
      expect(
        await verifyAssertion(publicKeyX, publicKeyY, authenticatorData, new Uint8Array(32), bytesOf(vector.signature)),
      ).toBe(false)

      const signature = await signAssertion(bytesOf(vector.privateKey), authenticatorData, clientDataHash)
      expect(await verifyAssertion(publicKeyX, publicKeyY, authenticatorData, clientDataHash, signature)).toBe(true)
    })
  })
})
