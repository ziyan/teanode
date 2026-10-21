import { createPublicKey, sign as nodeSign, verify as nodeVerify } from 'node:crypto'
import { describe, expect, it } from 'vitest'
import { concatenateBytes, encodeBase64Url } from './crypto'
import {
  assertionAuthenticatorData,
  convertSignatureFromDer,
  convertSignatureToDer,
  createPasskey,
  encodeAttestationObject,
  encodeCosePublicKey,
  PASSKEY_AAGUID,
  readPasskeyPublicKey,
  registrationAuthenticatorData,
  signAssertion,
  verifyAssertion,
} from './passkey'
import { bytesFilledWith, bytesFromHex, bytesFromText, expectBytes } from './testSupport'

function nodePublicKey(publicKeyX: Uint8Array, publicKeyY: Uint8Array) {
  return createPublicKey({
    key: { kty: 'EC', crv: 'P-256', x: encodeBase64Url(publicKeyX), y: encodeBase64Url(publicKeyY) },
    format: 'jwk',
  })
}

describe('createPasskey', () => {
  it('makes a PKCS#8 key, its public coordinates and a 16 byte credential id', async () => {
    const passkey = await createPasskey()
    expect(passkey.credentialId.length).toBe(16)
    expect(passkey.publicKeyX.length).toBe(32)
    expect(passkey.publicKeyY.length).toBe(32)
    // A P-256 PKCS#8 key is a DER SEQUENCE.
    expect(passkey.privateKey[0]).toBe(0x30)
    const publicKey = await readPasskeyPublicKey(passkey.privateKey)
    expectBytes(publicKey.publicKeyX, passkey.publicKeyX)
    expectBytes(publicKey.publicKeyY, passkey.publicKeyY)
  })

  it('keeps a credential id it is given', async () => {
    const credentialId = bytesFilledWith(16, 2)
    expectBytes((await createPasskey(credentialId)).credentialId, credentialId)
  })
})

describe('encodeCosePublicKey', () => {
  it('writes the canonical map in key order 1, 3, -1, -2, -3', () => {
    const publicKeyX = bytesFilledWith(32, 0xaa)
    const publicKeyY = bytesFilledWith(32, 0xbb)
    const expected = concatenateBytes(
      bytesFromHex('a5010203262001215820'),
      publicKeyX,
      bytesFromHex('225820'),
      publicKeyY,
    )
    expectBytes(encodeCosePublicKey(publicKeyX, publicKeyY), expected)
  })

  it('refuses a coordinate that is not 32 bytes', () => {
    expect(() => encodeCosePublicKey(new Uint8Array(31), new Uint8Array(32))).toThrow()
  })
})

describe('authenticator data', () => {
  const credentialId = bytesFilledWith(16, 0x11)
  const publicKeyX = bytesFilledWith(32, 0xaa)
  const publicKeyY = bytesFilledWith(32, 0xbb)

  async function hashOf(text: string): Promise<Uint8Array> {
    return new Uint8Array(await crypto.subtle.digest('SHA-256', bytesFromText(text)))
  }

  it('has the AAGUID the plan fixes', () => {
    expect(new TextDecoder().decode(PASSKEY_AAGUID)).toBe('TEANODEPASSWORDS')
    expect(PASSKEY_AAGUID.length).toBe(16)
  })

  it('lays out registration data, flags 0x5D when verified and 0x59 when not', async () => {
    const verified = await registrationAuthenticatorData({
      relyingPartyId: 'example.com',
      credentialId,
      publicKeyX,
      publicKeyY,
      isVerified: true,
    })
    const cose = encodeCosePublicKey(publicKeyX, publicKeyY)
    const expected = concatenateBytes(
      await hashOf('example.com'),
      new Uint8Array([0x5d]),
      new Uint8Array(4),
      PASSKEY_AAGUID,
      new Uint8Array([0, 16]),
      credentialId,
      cose,
    )
    expectBytes(verified, expected)
    expect(verified.length).toBe(32 + 1 + 4 + 16 + 2 + 16 + cose.length)

    const unverified = await registrationAuthenticatorData({
      relyingPartyId: 'example.com',
      credentialId,
      publicKeyX,
      publicKeyY,
      isVerified: false,
    })
    expect(unverified[32]).toBe(0x59)
  })

  it('lays out assertion data, flags 0x1D when verified and 0x19 when not', async () => {
    const verified = await assertionAuthenticatorData('example.com', true)
    expectBytes(verified, concatenateBytes(await hashOf('example.com'), new Uint8Array([0x1d]), new Uint8Array(4)))
    expect(verified.length).toBe(37)
    expect((await assertionAuthenticatorData('example.com', false))[32]).toBe(0x19)
  })

  it('wraps authenticator data in an attestation object of format none', () => {
    const authenticatorData = bytesFilledWith(148, 0x42)
    const attestationObject = encodeAttestationObject(authenticatorData)
    // a3, "fmt", "none", "attStmt", {}, "authData", byte string of 148 (0x58 0x94).
    const expectedHead = bytesFromHex('a363666d74646e6f6e656761747453746d74a068617574684461746158' + '94')
    expectBytes(attestationObject.slice(0, expectedHead.length), expectedHead)
    expectBytes(attestationObject.slice(expectedHead.length), authenticatorData)
  })

  it('writes a short byte string head for data under 24 bytes and a two byte length over 255', () => {
    const short = encodeAttestationObject(new Uint8Array(3))
    expect(short[short.length - 4]).toBe(0x43)
    const long = encodeAttestationObject(new Uint8Array(300))
    expectBytes(long.slice(long.length - 303, long.length - 300), bytesFromHex('59012c'))
  })
})

describe('signatures', () => {
  it('converts r and s to DER, trimming leading zeros and padding a high first bit', () => {
    const lowBits = concatenateBytes(new Uint8Array([0, 0, 0x7f]), bytesFilledWith(29, 1))
    const highBit = bytesFilledWith(32, 0x80)
    const signature = concatenateBytes(lowBits, highBit)
    const der = convertSignatureToDer(signature)
    // r loses two zero bytes (30 bytes); s gains one (33 bytes).
    expect(der[0]).toBe(0x30)
    expect(der[1]).toBe(2 + 30 + 2 + 33)
    expect(der[2]).toBe(0x02)
    expect(der[3]).toBe(30)
    expect(der[4 + 30]).toBe(0x02)
    expect(der[5 + 30]).toBe(33)
    expect(der[6 + 30]).toBe(0)
    expectBytes(convertSignatureFromDer(der), signature)
  })

  it('keeps a single zero for a zero number and refuses malformed DER', () => {
    const der = convertSignatureToDer(new Uint8Array(64))
    expectBytes(der, bytesFromHex('3006020100020100'))
    expectBytes(convertSignatureFromDer(der), new Uint8Array(64))
    expect(() => convertSignatureFromDer(new Uint8Array([0x31, 0]))).toThrow()
    expect(() => convertSignatureFromDer(bytesFromHex('3006020100020200'))).toThrow()
    expect(() => convertSignatureFromDer(bytesFromHex('300602010002010000'))).toThrow()
    expect(() => convertSignatureToDer(new Uint8Array(63))).toThrow()
  })

  it('signs in DER, verifiable by another implementation and by itself', async () => {
    const passkey = await createPasskey()
    const authenticatorData = await assertionAuthenticatorData('example.com', true)
    const clientDataHash = bytesFilledWith(32, 0x33)
    const signed = concatenateBytes(authenticatorData, clientDataHash)
    for (let attempt = 0; attempt < 20; attempt++) {
      const signature = await signAssertion(passkey.privateKey, authenticatorData, clientDataHash)
      expect(signature[0]).toBe(0x30)
      expect(
        nodeVerify(
          'sha256',
          signed,
          { key: nodePublicKey(passkey.publicKeyX, passkey.publicKeyY), dsaEncoding: 'der' },
          signature,
        ),
      ).toBe(true)
      expect(
        await verifyAssertion(passkey.publicKeyX, passkey.publicKeyY, authenticatorData, clientDataHash, signature),
      ).toBe(true)
    }
  })

  it('verifies a signature another implementation made, and refuses a changed message', async () => {
    const passkey = await createPasskey()
    const { createPrivateKey } = await import('node:crypto')
    const nodeKey = createPrivateKey({ key: Buffer.from(passkey.privateKey), format: 'der', type: 'pkcs8' })
    const authenticatorData = await assertionAuthenticatorData('example.com', false)
    const clientDataHash = bytesFilledWith(32, 0x44)
    const signature = new Uint8Array(
      nodeSign('sha256', concatenateBytes(authenticatorData, clientDataHash), { key: nodeKey, dsaEncoding: 'der' }),
    )
    expect(
      await verifyAssertion(passkey.publicKeyX, passkey.publicKeyY, authenticatorData, clientDataHash, signature),
    ).toBe(true)
    expect(
      await verifyAssertion(
        passkey.publicKeyX,
        passkey.publicKeyY,
        authenticatorData,
        bytesFilledWith(32, 0x45),
        signature,
      ),
    ).toBe(false)
    const other = await createPasskey()
    expect(
      await verifyAssertion(other.publicKeyX, other.publicKeyY, authenticatorData, clientDataHash, signature),
    ).toBe(false)
  })
})
