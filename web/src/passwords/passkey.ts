// A software authenticator for passkeys: ES256 only (COSE algorithm -7), keys
// that sync (flags BE and BS set), sign count always 0, attestation "none".
// The byte layouts are the ones docs/planning/passwords-execplan.md fixes, so
// the Go, Swift and TypeScript code make the same bytes from the same inputs.
// The CBOR is written by hand: the two maps involved are small and fixed.

import { concatenateBytes, copyBytes, decodeBase64Url, encodeUtf8, PasswordsCryptoError, randomBytes } from './crypto'

// "TEANODEPASSWORDS" in ASCII.
export const PASSKEY_AAGUID = new Uint8Array([
  0x54, 0x45, 0x41, 0x4e, 0x4f, 0x44, 0x45, 0x50, 0x41, 0x53, 0x53, 0x57, 0x4f, 0x52, 0x44, 0x53,
])

export const PASSKEY_CREDENTIAL_ID_BYTE_COUNT = 16
const COORDINATE_BYTE_COUNT = 32

// Authenticator data flags (WebAuthn section 6.1).
const FLAG_USER_PRESENT = 0x01
const FLAG_USER_VERIFIED = 0x04
const FLAG_BACKUP_ELIGIBLE = 0x08
const FLAG_BACKED_UP = 0x10
const FLAG_ATTESTED_CREDENTIAL_DATA = 0x40

const ECDSA_ALGORITHM = { name: 'ECDSA', namedCurve: 'P-256' } as const

export interface NewPasskey {
  credentialId: Uint8Array
  // privateKey is PKCS#8 DER, what a login's passkey fields keep in base64.
  privateKey: Uint8Array
  publicKeyX: Uint8Array
  publicKeyY: Uint8Array
}

export interface PasskeyPublicKey {
  publicKeyX: Uint8Array
  publicKeyY: Uint8Array
}

function readCoordinate(webKeyValue: string | undefined): Uint8Array {
  if (!webKeyValue) throw new PasswordsCryptoError('The key has no public coordinate')
  const coordinate = decodeBase64Url(webKeyValue)
  const padded = new Uint8Array(COORDINATE_BYTE_COUNT)
  padded.set(coordinate, COORDINATE_BYTE_COUNT - coordinate.length)
  return padded
}

// createPasskey makes a new key pair and a 16 byte credential id. Pass a
// credentialId to fix it (the test vectors do).
export async function createPasskey(credentialId?: Uint8Array): Promise<NewPasskey> {
  const keyPair = await crypto.subtle.generateKey(ECDSA_ALGORITHM, true, ['sign', 'verify'])
  const privateKey = new Uint8Array(await crypto.subtle.exportKey('pkcs8', keyPair.privateKey))
  const publicWebKey = await crypto.subtle.exportKey('jwk', keyPair.publicKey)
  return {
    credentialId: credentialId ?? randomBytes(PASSKEY_CREDENTIAL_ID_BYTE_COUNT),
    privateKey,
    publicKeyX: readCoordinate(publicWebKey.x),
    publicKeyY: readCoordinate(publicWebKey.y),
  }
}

// readPasskeyPublicKey works out the public key from a stored PKCS#8 private
// key; a login keeps only the private key.
export async function readPasskeyPublicKey(privateKey: Uint8Array): Promise<PasskeyPublicKey> {
  const key = await crypto.subtle.importKey('pkcs8', copyBytes(privateKey), ECDSA_ALGORITHM, true, ['sign'])
  const webKey = await crypto.subtle.exportKey('jwk', key)
  return { publicKeyX: readCoordinate(webKey.x), publicKeyY: readCoordinate(webKey.y) }
}

// ---------------------------------------------------------------------------
// CBOR (RFC 8949), only what the two structures need

const MAJOR_BYTE_STRING = 2
const MAJOR_TEXT_STRING = 3
const MAJOR_MAP = 5

// cborHead writes a major type and an argument in the shortest form.
function cborHead(majorType: number, argument: number): Uint8Array {
  const type = majorType << 5
  if (argument < 24) return new Uint8Array([type | argument])
  if (argument < 0x100) return new Uint8Array([type | 24, argument])
  if (argument < 0x10000) return new Uint8Array([type | 25, argument >> 8, argument & 0xff])
  throw new PasswordsCryptoError('A CBOR item is too long')
}

function cborByteString(bytes: Uint8Array): Uint8Array {
  return concatenateBytes(cborHead(MAJOR_BYTE_STRING, bytes.length), bytes)
}

function cborTextString(text: string): Uint8Array {
  const bytes = encodeUtf8(text)
  return concatenateBytes(cborHead(MAJOR_TEXT_STRING, bytes.length), bytes)
}

// encodeCosePublicKey is the COSE_Key map {1: 2, 3: -7, -1: 1, -2: X, -3: Y}
// (key type EC2, algorithm ES256, curve P-256) with the keys in the canonical
// order 1, 3, -1, -2, -3.
export function encodeCosePublicKey(publicKeyX: Uint8Array, publicKeyY: Uint8Array): Uint8Array {
  if (publicKeyX.length !== COORDINATE_BYTE_COUNT || publicKeyY.length !== COORDINATE_BYTE_COUNT) {
    throw new PasswordsCryptoError('A P-256 coordinate is 32 bytes')
  }
  return concatenateBytes(
    cborHead(MAJOR_MAP, 5),
    new Uint8Array([0x01, 0x02]), // 1 (kty): 2 (EC2)
    new Uint8Array([0x03, 0x26]), // 3 (alg): -7 (ES256)
    new Uint8Array([0x20, 0x01]), // -1 (crv): 1 (P-256)
    new Uint8Array([0x21]),
    cborByteString(publicKeyX), // -2 (x)
    new Uint8Array([0x22]),
    cborByteString(publicKeyY), // -3 (y)
  )
}

// ---------------------------------------------------------------------------
// Authenticator data

async function hashRelyingPartyId(relyingPartyId: string): Promise<Uint8Array> {
  return new Uint8Array(await crypto.subtle.digest('SHA-256', copyBytes(encodeUtf8(relyingPartyId))))
}

// The sign count is always 0: a passkey that syncs has no single counter.
const SIGN_COUNT_ZERO = new Uint8Array(4)

function baseFlags(isVerified: boolean): number {
  return FLAG_USER_PRESENT | FLAG_BACKUP_ELIGIBLE | FLAG_BACKED_UP | (isVerified ? FLAG_USER_VERIFIED : 0)
}

export interface RegistrationAuthenticatorDataInput {
  relyingPartyId: string
  credentialId: Uint8Array
  publicKeyX: Uint8Array
  publicKeyY: Uint8Array
  // isVerified: the person proved who they are (a passphrase or Face ID) for
  // this ceremony.
  isVerified: boolean
}

// registrationAuthenticatorData: SHA-256(relying party id) || flags (0x5D, or
// 0x59 when not verified) || sign count || AAGUID || credential id length ||
// credential id || COSE public key.
export async function registrationAuthenticatorData(input: RegistrationAuthenticatorDataInput): Promise<Uint8Array> {
  const credentialIdLength = new Uint8Array([input.credentialId.length >> 8, input.credentialId.length & 0xff])
  return concatenateBytes(
    await hashRelyingPartyId(input.relyingPartyId),
    new Uint8Array([baseFlags(input.isVerified) | FLAG_ATTESTED_CREDENTIAL_DATA]),
    SIGN_COUNT_ZERO,
    PASSKEY_AAGUID,
    credentialIdLength,
    input.credentialId,
    encodeCosePublicKey(input.publicKeyX, input.publicKeyY),
  )
}

// encodeAttestationObject is the CBOR map {"fmt": "none", "attStmt": {},
// "authData": <bytes>} in that key order.
export function encodeAttestationObject(authenticatorData: Uint8Array): Uint8Array {
  return concatenateBytes(
    cborHead(MAJOR_MAP, 3),
    cborTextString('fmt'),
    cborTextString('none'),
    cborTextString('attStmt'),
    cborHead(MAJOR_MAP, 0),
    cborTextString('authData'),
    cborByteString(authenticatorData),
  )
}

// assertionAuthenticatorData: SHA-256(relying party id) || flags (0x1D, or
// 0x19 when not verified) || sign count. 37 bytes.
export async function assertionAuthenticatorData(relyingPartyId: string, isVerified: boolean): Promise<Uint8Array> {
  return concatenateBytes(
    await hashRelyingPartyId(relyingPartyId),
    new Uint8Array([baseFlags(isVerified)]),
    SIGN_COUNT_ZERO,
  )
}

// ---------------------------------------------------------------------------
// Signatures

// convertSignatureToDer turns the r || s pair WebCrypto returns (two 32 byte
// numbers) into the DER SEQUENCE of two INTEGERs a website expects.
export function convertSignatureToDer(signature: Uint8Array): Uint8Array {
  if (signature.length !== COORDINATE_BYTE_COUNT * 2) throw new PasswordsCryptoError('An ES256 signature is 64 bytes')
  const integers = [signature.slice(0, COORDINATE_BYTE_COUNT), signature.slice(COORDINATE_BYTE_COUNT)].map(
    encodeDerInteger,
  )
  const body = concatenateBytes(...integers)
  // Two integers of at most 33 bytes each: the body is under 128 bytes, so the
  // length fits in one byte.
  return concatenateBytes(new Uint8Array([0x30, body.length]), body)
}

function encodeDerInteger(value: Uint8Array): Uint8Array {
  let start = 0
  while (start < value.length - 1 && value[start] === 0) start++
  let trimmed: Uint8Array = value.slice(start)
  // A leading 1 bit would read as negative; DER puts a zero byte before it.
  if (trimmed[0] & 0x80) trimmed = concatenateBytes(new Uint8Array([0]), trimmed)
  return concatenateBytes(new Uint8Array([0x02, trimmed.length]), trimmed)
}

// convertSignatureFromDer is the reverse, for checking a signature another
// program made: 64 bytes, r || s.
export function convertSignatureFromDer(signature: Uint8Array): Uint8Array {
  const result = new Uint8Array(COORDINATE_BYTE_COUNT * 2)
  let offset = 0
  const fail = (): never => {
    throw new PasswordsCryptoError('Not a DER ECDSA signature')
  }
  if (signature[offset++] !== 0x30) fail()
  const bodyLength = signature[offset++]
  if (bodyLength === undefined || bodyLength >= 0x80 || offset + bodyLength !== signature.length) fail()
  for (let part = 0; part < 2; part++) {
    if (signature[offset++] !== 0x02) fail()
    const length = signature[offset++]
    if (length === undefined || length === 0 || length >= 0x80 || offset + length > signature.length) fail()
    let integer = signature.slice(offset, offset + length)
    offset += length
    while (integer.length > 1 && integer[0] === 0) integer = integer.slice(1)
    if (integer.length > COORDINATE_BYTE_COUNT) fail()
    result.set(integer, part * COORDINATE_BYTE_COUNT + COORDINATE_BYTE_COUNT - integer.length)
  }
  if (offset !== signature.length) fail()
  return result
}

// signAssertion signs authenticatorData || clientDataHash with the passkey's
// PKCS#8 private key and returns the DER signature.
export async function signAssertion(
  privateKey: Uint8Array,
  authenticatorData: Uint8Array,
  clientDataHash: Uint8Array,
): Promise<Uint8Array> {
  const key = await crypto.subtle.importKey('pkcs8', copyBytes(privateKey), ECDSA_ALGORITHM, false, ['sign'])
  const signature = await crypto.subtle.sign(
    { name: 'ECDSA', hash: 'SHA-256' },
    key,
    copyBytes(concatenateBytes(authenticatorData, clientDataHash)),
  )
  return convertSignatureToDer(new Uint8Array(signature))
}

// verifyAssertion checks a DER signature against a public key. A website does
// this; here it lets the tests check a signature made by the Go code.
export async function verifyAssertion(
  publicKeyX: Uint8Array,
  publicKeyY: Uint8Array,
  authenticatorData: Uint8Array,
  clientDataHash: Uint8Array,
  signature: Uint8Array,
): Promise<boolean> {
  const key = await crypto.subtle.importKey(
    'raw',
    copyBytes(concatenateBytes(new Uint8Array([4]), publicKeyX, publicKeyY)),
    ECDSA_ALGORITHM,
    false,
    ['verify'],
  )
  return crypto.subtle.verify(
    { name: 'ECDSA', hash: 'SHA-256' },
    key,
    copyBytes(convertSignatureFromDer(signature)),
    copyBytes(concatenateBytes(authenticatorData, clientDataHash)),
  )
}
