// The encryption of Passwords, byte for byte as docs/planning/passwords-execplan.md
// ("Encryption") writes it. The Go and Swift code follow the same text, and one
// file of test vectors (internal/passwords/testdata/vectors.json) pins all of
// them. WebCrypto only: this file runs in the dashboard, the extension's
// service worker and its pages.
//
// Key material is a Uint8Array at every boundary. Every exported function that
// seals draws its own nonce and ephemeral key pair. The test vectors fix those
// through sealValueWithNonce and sealForKeyPairWithRandomness, which are for
// tests only (testSupport.ts re-exports them, and a test checks that no other
// file imports them).

import type { ItemAccess, ItemFields, ItemFieldsByKind, ItemGrantee, ItemKind } from './fields'

export const SEALED_VALUE_VERSION = 1

// The recipient id of the server's agent key in a share's associated data.
export const AGENT_RECIPIENT_ID = 'agent'

export const PASSPHRASE_ITERATION_COUNT = 600000
// The count comes back from the server with the wrapped key. A server that
// sent a tiny one would make the proof cheap to guess from, and one that sent
// an enormous one would hang the browser, so a count outside these is refused.
// Go and Swift use the same bounds.
export const MINIMUM_ITERATION_COUNT = 600000
export const MAXIMUM_ITERATION_COUNT = 10000000
export const PASSPHRASE_SALT_BYTE_COUNT = 16
export const KEY_BYTE_COUNT = 32
export const NONCE_BYTE_COUNT = 12
export const PUBLIC_KEY_BYTE_COUNT = 65

const WRAP_KEY_INFO = 'teanode passwords wrap v1'
const PROOF_KEY_INFO = 'teanode passwords proof v1'
const SHARE_KEY_INFO = 'teanode passwords share v1'

export const ACCOUNT_KEY_ASSOCIATED_DATA = 'account-key'
export const PRIVATE_KEY_ASSOCIATED_DATA = 'private-key'

const BASE32_ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
const RECOVERY_KEY_GROUP_LENGTH = 4

// A value sealed with AES-256-GCM. nonce and ciphertext are standard base64;
// the ciphertext carries the 16 byte tag at its end.
export interface SealedValue {
  version: number
  nonce: string
  ciphertext: string
}

// A value sealed for a key pair: the sender's one-time public key, then the
// same nonce and ciphertext as a SealedValue.
export interface SealedForKeyPair {
  version: number
  ephemeralPublicKey: string
  nonce: string
  ciphertext: string
}

// A P-256 key pair as bytes: the 32 byte scalar and the 65 byte uncompressed
// point (04 || X || Y).
export interface PersonKeyPair {
  privateKey: Uint8Array
  publicKey: Uint8Array
}

export class PasswordsCryptoError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'PasswordsCryptoError'
  }
}

// ---------------------------------------------------------------------------
// Bytes and text

// copyBytes returns a copy backed by a plain ArrayBuffer, which is what
// WebCrypto's types ask for and what keeps a caller's view from being shared.
export function copyBytes(bytes: Uint8Array): Uint8Array<ArrayBuffer> {
  return new Uint8Array(bytes)
}

export function encodeUtf8(text: string): Uint8Array {
  return copyBytes(new TextEncoder().encode(text))
}

export function decodeUtf8(bytes: Uint8Array): string {
  return new TextDecoder('utf-8', { fatal: true }).decode(bytes)
}

export function encodeBase64(bytes: Uint8Array): string {
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return btoa(binary)
}

export function decodeBase64(text: string): Uint8Array {
  let binary: string
  try {
    binary = atob(text)
  } catch {
    throw new PasswordsCryptoError('The text is not base64')
  }
  const bytes = new Uint8Array(binary.length)
  for (let index = 0; index < binary.length; index++) bytes[index] = binary.charCodeAt(index)
  // atob accepts text without its padding; the format is standard base64 with
  // padding, so a value that does not round-trip was not written by us.
  if (encodeBase64(bytes) !== text) throw new PasswordsCryptoError('The text is not canonical base64')
  return bytes
}

export function encodeBase64Url(bytes: Uint8Array): string {
  return encodeBase64(bytes).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

// decodeBase64Url reads the unpadded base64url of a JSON web key.
export function decodeBase64Url(text: string): Uint8Array {
  const standard = text.replace(/-/g, '+').replace(/_/g, '/')
  const padded = standard + '='.repeat((4 - (standard.length % 4)) % 4)
  return decodeBase64(padded)
}

export function concatenateBytes(...parts: Uint8Array[]): Uint8Array {
  const total = parts.reduce((sum, part) => sum + part.length, 0)
  const joined = new Uint8Array(total)
  let offset = 0
  for (const part of parts) {
    joined.set(part, offset)
    offset += part.length
  }
  return joined
}

export function areBytesEqual(first: Uint8Array, second: Uint8Array): boolean {
  if (first.length !== second.length) return false
  let difference = 0
  for (let index = 0; index < first.length; index++) difference |= first[index] ^ second[index]
  return difference === 0
}

export function randomBytes(byteCount: number): Uint8Array {
  const bytes = new Uint8Array(byteCount)
  crypto.getRandomValues(bytes)
  return bytes
}

// ---------------------------------------------------------------------------
// Base32 (RFC 4648 section 6), written by hand: the recovery key and the
// one-time code secrets use it, and WebCrypto has none.

export function encodeBase32(bytes: Uint8Array): string {
  let text = ''
  let bitBuffer = 0
  let bitCount = 0
  for (const byte of bytes) {
    bitBuffer = (bitBuffer << 8) | byte
    bitCount += 8
    while (bitCount >= 5) {
      text += BASE32_ALPHABET[(bitBuffer >>> (bitCount - 5)) & 31]
      bitCount -= 5
    }
    bitBuffer &= (1 << bitCount) - 1
  }
  if (bitCount > 0) text += BASE32_ALPHABET[(bitBuffer << (5 - bitCount)) & 31]
  return text
}

// decodeBase32 reads upper or lower case letters, ignoring spaces, hyphens and
// trailing "=" padding. Bits left over after the last whole byte are dropped,
// as authenticator apps do with the secrets sites hand out.
export function decodeBase32(text: string): Uint8Array {
  const cleaned = text.replace(/[\s-]/g, '').replace(/=+$/, '').toUpperCase()
  const bytes: number[] = []
  let bitBuffer = 0
  let bitCount = 0
  for (const letter of cleaned) {
    const value = BASE32_ALPHABET.indexOf(letter)
    if (value < 0) throw new PasswordsCryptoError('The text is not base32')
    bitBuffer = (bitBuffer << 5) | value
    bitCount += 5
    if (bitCount >= 8) {
      bytes.push((bitBuffer >>> (bitCount - 8)) & 255)
      bitCount -= 8
    }
    bitBuffer &= (1 << bitCount) - 1
  }
  return new Uint8Array(bytes)
}

// ---------------------------------------------------------------------------
// Associated data: the text bound into every sealed value, so a value cannot
// be moved to another item, kind or recipient and still open.

export function itemKeyAssociatedData(itemId: string): string {
  return `item-key:${itemId}`
}

export function itemFieldsAssociatedData(itemId: string, itemKind: ItemKind): string {
  return `item:${itemId}:${itemKind}`
}

// shareAssociatedData: ownerId is the user id of the person who owns the item
// and sealed its key; recipientId is the recipient's user id, or
// AGENT_RECIPIENT_ID for the server's agent key.
export function shareAssociatedData(itemId: string, ownerId: string, recipientId: string): string {
  return `share:${itemId}:${ownerId}:${recipientId}`
}

// ---------------------------------------------------------------------------
// Sealed values

function readKey(key: Uint8Array, purpose: string): Uint8Array {
  if (key.length !== KEY_BYTE_COUNT) throw new PasswordsCryptoError(`${purpose} must be ${KEY_BYTE_COUNT} bytes`)
  return key
}

function readNonce(nonce: Uint8Array): Uint8Array {
  if (nonce.length !== NONCE_BYTE_COUNT) throw new PasswordsCryptoError(`The nonce must be ${NONCE_BYTE_COUNT} bytes`)
  return nonce
}

async function importAesKey(key: Uint8Array, usage: 'encrypt' | 'decrypt'): Promise<CryptoKey> {
  readKey(key, 'The key')
  return crypto.subtle.importKey('raw', copyBytes(key), 'AES-GCM', false, [usage])
}

async function encryptBytes(
  key: Uint8Array,
  plaintext: Uint8Array,
  associatedData: string,
  nonce: Uint8Array,
): Promise<Uint8Array> {
  const aesKey = await importAesKey(key, 'encrypt')
  const ciphertext = await crypto.subtle.encrypt(
    { name: 'AES-GCM', iv: copyBytes(nonce), additionalData: copyBytes(encodeUtf8(associatedData)), tagLength: 128 },
    aesKey,
    copyBytes(plaintext),
  )
  return new Uint8Array(ciphertext)
}

async function decryptBytes(
  key: Uint8Array,
  nonce: Uint8Array,
  ciphertext: Uint8Array,
  associatedData: string,
): Promise<Uint8Array> {
  const aesKey = await importAesKey(key, 'decrypt')
  try {
    const plaintext = await crypto.subtle.decrypt(
      { name: 'AES-GCM', iv: copyBytes(nonce), additionalData: copyBytes(encodeUtf8(associatedData)), tagLength: 128 },
      aesKey,
      copyBytes(ciphertext),
    )
    return new Uint8Array(plaintext)
  } catch {
    // WebCrypto reports a bad tag as a bare OperationError. Say what it means.
    throw new PasswordsCryptoError('The value could not be opened: the key is wrong or the data was changed')
  }
}

// sealValueWithNonce is for tests only: it seals with a nonce the caller
// fixes, so the vectors reproduce. Reusing a nonce under one key breaks
// AES-GCM, so nothing outside the tests may call it.
export async function sealValueWithNonce(
  key: Uint8Array,
  plaintext: Uint8Array,
  associatedData: string,
  nonce: Uint8Array,
): Promise<SealedValue> {
  const usedNonce = readNonce(nonce)
  const ciphertext = await encryptBytes(key, plaintext, associatedData, usedNonce)
  return { version: SEALED_VALUE_VERSION, nonce: encodeBase64(usedNonce), ciphertext: encodeBase64(ciphertext) }
}

// sealValue encrypts plaintext with AES-256-GCM under a fresh random nonce.
export function sealValue(key: Uint8Array, plaintext: Uint8Array, associatedData: string): Promise<SealedValue> {
  return sealValueWithNonce(key, plaintext, associatedData, randomBytes(NONCE_BYTE_COUNT))
}

export async function openValue(key: Uint8Array, sealed: SealedValue, associatedData: string): Promise<Uint8Array> {
  if (sealed.version !== SEALED_VALUE_VERSION)
    throw new PasswordsCryptoError(`Unknown sealed value version ${sealed.version}`)
  return decryptBytes(key, readNonce(decodeBase64(sealed.nonce)), decodeBase64(sealed.ciphertext), associatedData)
}

// serializeSealed is the text kept in a database column and sent to the server.
export function serializeSealed(sealed: SealedValue | SealedForKeyPair): string {
  return JSON.stringify(sealed)
}

export function parseSealedValue(text: string): SealedValue {
  const parsed = parseJsonObject(text)
  if (typeof parsed.version !== 'number' || typeof parsed.nonce !== 'string' || typeof parsed.ciphertext !== 'string') {
    throw new PasswordsCryptoError('Not a sealed value')
  }
  return { version: parsed.version, nonce: parsed.nonce, ciphertext: parsed.ciphertext }
}

export function parseSealedForKeyPair(text: string): SealedForKeyPair {
  const parsed = parseJsonObject(text)
  if (
    typeof parsed.version !== 'number' ||
    typeof parsed.ephemeralPublicKey !== 'string' ||
    typeof parsed.nonce !== 'string' ||
    typeof parsed.ciphertext !== 'string'
  ) {
    throw new PasswordsCryptoError('Not a value sealed for a key pair')
  }
  return {
    version: parsed.version,
    ephemeralPublicKey: parsed.ephemeralPublicKey,
    nonce: parsed.nonce,
    ciphertext: parsed.ciphertext,
  }
}

function parseJsonObject(text: string): Record<string, unknown> {
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    throw new PasswordsCryptoError('Not JSON')
  }
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed))
    throw new PasswordsCryptoError('Not a JSON object')
  return parsed as Record<string, unknown>
}

// ---------------------------------------------------------------------------
// The keys derived from the passphrase and the recovery key

// derivePassphraseKey is PBKDF2-HMAC-SHA256 over the NFC-normalized
// passphrase. The salt and the iteration count are stored beside the wrapped
// account key, so the count can be raised later.
export async function derivePassphraseKey(
  passphrase: string,
  salt: Uint8Array,
  iterationCount: number = PASSPHRASE_ITERATION_COUNT,
): Promise<Uint8Array> {
  if (!Number.isInteger(iterationCount) || iterationCount < MINIMUM_ITERATION_COUNT) {
    throw new PasswordsCryptoError(`The iteration count must be at least ${MINIMUM_ITERATION_COUNT}`)
  }
  if (iterationCount > MAXIMUM_ITERATION_COUNT) {
    throw new PasswordsCryptoError(`The iteration count must be at most ${MAXIMUM_ITERATION_COUNT}`)
  }
  const baseKey = await crypto.subtle.importKey(
    'raw',
    copyBytes(encodeUtf8(passphrase.normalize('NFC'))),
    'PBKDF2',
    false,
    ['deriveBits'],
  )
  const bits = await crypto.subtle.deriveBits(
    { name: 'PBKDF2', hash: 'SHA-256', salt: copyBytes(salt), iterations: iterationCount },
    baseKey,
    KEY_BYTE_COUNT * 8,
  )
  return new Uint8Array(bits)
}

async function deriveHkdfBytes(inputKeyMaterial: Uint8Array, salt: Uint8Array, info: string): Promise<Uint8Array> {
  const baseKey = await crypto.subtle.importKey('raw', copyBytes(inputKeyMaterial), 'HKDF', false, ['deriveBits'])
  const bits = await crypto.subtle.deriveBits(
    { name: 'HKDF', hash: 'SHA-256', salt: copyBytes(salt), info: copyBytes(encodeUtf8(info)) },
    baseKey,
    KEY_BYTE_COUNT * 8,
  )
  return new Uint8Array(bits)
}

// deriveWrapKey: secret is the passphrase key or the recovery key's bytes. The
// result seals the account key.
export function deriveWrapKey(secret: Uint8Array): Promise<Uint8Array> {
  return deriveHkdfBytes(secret, new Uint8Array(0), WRAP_KEY_INFO)
}

// deriveProofKey: the proof is what a device sends to show it knows the secret,
// without sending the wrap key.
export function deriveProofKey(secret: Uint8Array): Promise<Uint8Array> {
  return deriveHkdfBytes(secret, new Uint8Array(0), PROOF_KEY_INFO)
}

// hashProof is what the server stores: base64 of SHA-256 of the proof bytes.
export async function hashProof(proof: Uint8Array): Promise<string> {
  return encodeBase64(new Uint8Array(await crypto.subtle.digest('SHA-256', copyBytes(proof))))
}

// ---------------------------------------------------------------------------
// The recovery key

export function makeRecoveryKey(): Uint8Array {
  return randomBytes(KEY_BYTE_COUNT)
}

// formatRecoveryKey writes the 32 bytes as 13 groups of 4 base32 letters.
export function formatRecoveryKey(recoveryKey: Uint8Array): string {
  readKey(recoveryKey, 'The recovery key')
  const letters = encodeBase32(recoveryKey)
  const groups: string[] = []
  for (let offset = 0; offset < letters.length; offset += RECOVERY_KEY_GROUP_LENGTH) {
    groups.push(letters.slice(offset, offset + RECOVERY_KEY_GROUP_LENGTH))
  }
  return groups.join(' ')
}

// parseRecoveryKey reads what formatRecoveryKey wrote, ignoring spaces, tabs,
// line breaks, hyphens and case. Only ASCII is read: any other character is
// refused, rather than folded into a letter by case mapping (the German sharp
// s upper-cases to "SS", for one) or skipped as white space (a no-break space).
// A "=" is refused as well. A typo is refused rather than turned into another
// key, including a last letter whose unused bits are not zero. Go and Swift
// refuse the same texts (recoveryKeyInvalid in the vectors).
export function parseRecoveryKey(text: string): Uint8Array {
  let letters = ''
  for (let index = 0; index < text.length; index++) {
    const character = text[index]
    if (character === ' ' || character === '\t' || character === '\n' || character === '\r' || character === '-') {
      continue
    }
    if (character >= 'a' && character <= 'z') {
      letters += character.toUpperCase()
    } else if ((character >= 'A' && character <= 'Z') || (character >= '2' && character <= '7')) {
      letters += character
    } else {
      throw new PasswordsCryptoError('The recovery key has a character that is not a letter or a digit from 2 to 7')
    }
  }
  const bytes = decodeBase32(letters)
  // Writing the bytes back refuses a last letter with unused bits set.
  if (bytes.length !== KEY_BYTE_COUNT || encodeBase32(bytes) !== letters) {
    throw new PasswordsCryptoError('The recovery key is not 52 letters and digits')
  }
  return bytes
}

// ---------------------------------------------------------------------------
// The person's key pair

const ECDH_ALGORITHM = { name: 'ECDH', namedCurve: 'P-256' } as const

function leftPad(bytes: Uint8Array, byteCount: number): Uint8Array {
  if (bytes.length >= byteCount) return bytes
  const padded = new Uint8Array(byteCount)
  padded.set(bytes, byteCount - bytes.length)
  return padded
}

function readPublicKey(publicKey: Uint8Array): Uint8Array {
  if (publicKey.length !== PUBLIC_KEY_BYTE_COUNT || publicKey[0] !== 4) {
    throw new PasswordsCryptoError('A public key is the 65 byte uncompressed point')
  }
  return publicKey
}

// generatePersonKeyPair makes a P-256 key pair for sharing.
export async function generatePersonKeyPair(): Promise<PersonKeyPair> {
  const keyPair = await crypto.subtle.generateKey(ECDH_ALGORITHM, true, ['deriveBits'])
  const publicKey = new Uint8Array(await crypto.subtle.exportKey('raw', keyPair.publicKey))
  const webKey = await crypto.subtle.exportKey('jwk', keyPair.privateKey)
  if (!webKey.d) throw new PasswordsCryptoError('The exported private key has no scalar')
  return { privateKey: leftPad(decodeBase64Url(webKey.d), KEY_BYTE_COUNT), publicKey }
}

// importPrivateKey builds a CryptoKey from the 32 byte scalar and the 65 byte
// public point. WebCrypto cannot import a bare scalar; it wants both halves,
// as a JSON web key.
async function importPrivateKey(keyPair: PersonKeyPair): Promise<CryptoKey> {
  readKey(keyPair.privateKey, 'The private key')
  const publicKey = readPublicKey(keyPair.publicKey)
  return crypto.subtle.importKey(
    'jwk',
    {
      kty: 'EC',
      crv: 'P-256',
      x: encodeBase64Url(publicKey.slice(1, 33)),
      y: encodeBase64Url(publicKey.slice(33, 65)),
      d: encodeBase64Url(keyPair.privateKey),
      ext: true,
    },
    ECDH_ALGORITHM,
    false,
    ['deriveBits'],
  )
}

async function importPublicKey(publicKey: Uint8Array): Promise<CryptoKey> {
  return crypto.subtle.importKey('raw', copyBytes(readPublicKey(publicKey)), ECDH_ALGORITHM, false, [])
}

// deriveSharedSecret is ECDH: the 32 byte X coordinate of the shared point.
export async function deriveSharedSecret(ownKeyPair: PersonKeyPair, otherPublicKey: Uint8Array): Promise<Uint8Array> {
  const privateKey = await importPrivateKey(ownKeyPair)
  const publicKey = await importPublicKey(otherPublicKey)
  return new Uint8Array(await crypto.subtle.deriveBits({ name: 'ECDH', public: publicKey }, privateKey, 256))
}

// isKeyPairConsistent says whether the public point belongs to the private
// scalar. WebCrypto cannot work out a public key from a bare scalar, so it is
// checked by agreement: with a fresh key pair E, the scalar times E's public
// point equals E's scalar times the claimed point exactly when the claimed
// point is the scalar times the generator. A pair that cannot be imported at
// all is not consistent.
export async function isKeyPairConsistent(keyPair: PersonKeyPair): Promise<boolean> {
  try {
    const probe = await generatePersonKeyPair()
    const fromScalar = await deriveSharedSecret(keyPair, probe.publicKey)
    const fromPoint = await deriveSharedSecret(probe, keyPair.publicKey)
    return areBytesEqual(fromScalar, fromPoint)
  } catch {
    return false
  }
}

const SAFETY_CODE_LABEL = 'teanode passwords safety code v2'
const SAFETY_CODE_GROUP_COUNT = 8

// safetyCode is the short form of a public key two people compare: eight groups
// of four digits. It is the SHA-256 of the label, a zero byte and the 65 key
// bytes; the first 16 bytes of the hash are eight big-endian 16 bit numbers,
// each modulo 10000. Nothing the server supplies goes into it, so a server
// cannot pick an identifier to make a search for a matching code easier:
// matching a fixed code means finding a key whose hash matches it, about 2^106
// key generations.
export async function safetyCode(publicKey: Uint8Array): Promise<string> {
  const input = concatenateBytes(encodeUtf8(SAFETY_CODE_LABEL), new Uint8Array([0]), readPublicKey(publicKey))
  const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', copyBytes(input)))
  const view = new DataView(digest.buffer, digest.byteOffset, digest.byteLength)
  const groups: string[] = []
  for (let index = 0; index < SAFETY_CODE_GROUP_COUNT; index++) {
    groups.push(String(view.getUint16(index * 2) % 10000).padStart(4, '0'))
  }
  return groups.join(' ')
}

// normalizeSafetyCode reads a safety code a person typed or pasted: 32 digits
// with white space anywhere or nowhere. It returns the code as safetyCode
// writes it (eight groups of four digits), so two codes compare as text, or
// null for anything else.
export function normalizeSafetyCode(text: string): string | null {
  const digits = text.replace(/\s+/g, '')
  if (!/^[0-9]{32}$/.test(digits)) return null
  const groups: string[] = []
  for (let index = 0; index < digits.length; index += 4) {
    groups.push(digits.slice(index, index + 4))
  }
  return groups.join(' ')
}

// ---------------------------------------------------------------------------
// The access record's signature

const ACCESS_RECORD_LABEL = 'teanode passwords access record v1'

// The highest record version any implementation accepts: the largest integer a
// JavaScript number holds exactly.
export const MAXIMUM_RECORD_VERSION = Number.MAX_SAFE_INTEGER

// compareBytes orders two byte strings the way Go's sort of strings does, one
// byte at a time. JavaScript's own sort of text compares UTF-16 units, which
// differs from byte order for characters outside the Basic Multilingual Plane.
function compareBytes(first: Uint8Array, second: Uint8Array): number {
  const shared = Math.min(first.length, second.length)
  for (let index = 0; index < shared; index++) {
    if (first[index] !== second[index]) return first[index] - second[index]
  }
  return first.length - second.length
}

// deriveAccessMacKey is the key the access record is signed with: HKDF-SHA256 of
// the account key with no salt and the label as the info. Exported for the test
// vectors.
export function deriveAccessMacKey(accountKey: Uint8Array): Promise<Uint8Array> {
  if (accountKey.length !== KEY_BYTE_COUNT) {
    return Promise.reject(new PasswordsCryptoError(`An account key is ${KEY_BYTE_COUNT} bytes`))
  }
  return deriveHkdfBytes(accountKey, new Uint8Array(0), ACCESS_RECORD_LABEL)
}

// CONTROL_CHARACTER matches a NUL or any other control character (Unicode
// category Cc: U+0000 to U+001F and U+007F to U+009F).
// eslint-disable-next-line no-control-regex
const CONTROL_CHARACTER = /[\u0000-\u001f\u007f-\u009f]/

// accessGranteeText is the text of a grantee in the signed bytes. An id with a
// control character is refused when signing and when verifying: the signed
// bytes separate their parts with a zero byte, so such an id could read as two.
function accessGranteeText(grantee: ItemGrantee): string {
  let text: string
  if (grantee.userId !== undefined && grantee.userId !== '' && grantee.circleId === undefined) {
    text = `u:${grantee.userId}`
  } else if (grantee.circleId !== undefined && grantee.circleId !== '' && grantee.userId === undefined) {
    text = `c:${grantee.circleId}`
  } else {
    throw new PasswordsCryptoError('A grantee needs exactly one non-empty user id or circle id')
  }
  if (CONTROL_CHARACTER.test(text)) throw new PasswordsCryptoError("A grantee's id has a control character")
  return text
}

// accessSignedBytes is what the signature covers: the label, a zero byte, the
// item id, a zero byte, the item kind, a zero byte, the record version in
// decimal, a zero byte, "1" or "0" for agent use, a zero byte, then each
// grantee as "u:" and the user id or "c:" and the circle id, in byte order,
// each followed by a zero byte. Exported for the test vectors.
export function accessSignedBytes(itemId: string, itemKind: ItemKind, access: ItemAccess): Uint8Array {
  const version = access.recordVersion
  if (version === undefined || !Number.isSafeInteger(version) || version < 1) {
    throw new PasswordsCryptoError('The access record needs a positive version to be signed')
  }
  if (itemKind !== 'login' && itemKind !== 'card' && itemKind !== 'note') {
    throw new PasswordsCryptoError('The item kind is not one of the kinds')
  }
  const parts: Uint8Array[] = []
  for (const text of [ACCESS_RECORD_LABEL, itemId, itemKind, String(version), access.isUsableByAgent ? '1' : '0']) {
    parts.push(encodeUtf8(text), new Uint8Array([0]))
  }
  const granteeBytes = access.grantees.map((grantee) => encodeUtf8(accessGranteeText(grantee))).sort(compareBytes)
  for (const bytes of granteeBytes) parts.push(bytes, new Uint8Array([0]))
  return concatenateBytes(...parts)
}

// macOfAccess is the signature of an access record for an item: HMAC-SHA256
// over accessSignedBytes, under HKDF-SHA256 of the account key (no salt, the
// label as the info), as standard base64 with padding. It uses the record's
// recordVersion, which must be set, and ignores its recordMac.
export async function macOfAccess(
  accountKey: Uint8Array,
  itemId: string,
  itemKind: ItemKind,
  access: ItemAccess,
): Promise<string> {
  const signedBytes = accessSignedBytes(itemId, itemKind, access)
  const macKey = await crypto.subtle.importKey(
    'raw',
    copyBytes(await deriveAccessMacKey(accountKey)),
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['sign'],
  )
  return encodeBase64(new Uint8Array(await crypto.subtle.sign('HMAC', macKey, copyBytes(signedBytes))))
}

// signAccess returns a copy of the record with its version set to one more than
// the highest version this device has seen with a valid signature for the item
// (0 if none), and the signature for that version. The version and signature
// the given record has are ignored: a version the server supplied is never a
// reason to count higher.
export async function signAccess(
  accountKey: Uint8Array,
  itemId: string,
  itemKind: ItemKind,
  access: ItemAccess | undefined,
  highestVersionSeen: number,
): Promise<ItemAccess> {
  if (
    !Number.isSafeInteger(highestVersionSeen) ||
    highestVersionSeen < 0 ||
    highestVersionSeen >= MAXIMUM_RECORD_VERSION
  ) {
    throw new PasswordsCryptoError('The highest record version seen is out of range')
  }
  const unsigned: ItemAccess = {
    isUsableByAgent: access?.isUsableByAgent ?? false,
    grantees: (access?.grantees ?? []).map((grantee) => ({ ...grantee })),
    recordVersion: highestVersionSeen + 1,
  }
  return { ...unsigned, recordMac: await macOfAccess(accountKey, itemId, itemKind, unsigned) }
}

// withSignedAccess returns the fields with the given access record signed (see
// signAccess) in place of the one they had. Every other key is kept.
export async function withSignedAccess<Fields extends ItemFields>(
  accountKey: Uint8Array,
  itemId: string,
  itemKind: ItemKind,
  fields: Fields,
  access: ItemAccess | undefined,
  highestVersionSeen: number,
): Promise<Fields> {
  return { ...fields, access: await signAccess(accountKey, itemId, itemKind, access, highestVersionSeen) }
}

// isAccessVerified reports whether the record is signed by the owner's devices
// for this item: it has a version and a signature, and the signature is the one
// macOfAccess gives. A missing record, version or signature, and a wrong
// signature, are all false. The comparison takes the same time whatever the
// bytes.
export async function isAccessVerified(
  accountKey: Uint8Array,
  itemId: string,
  itemKind: ItemKind,
  access: ItemAccess | undefined,
): Promise<boolean> {
  if (!access || access.recordVersion === undefined || access.recordMac === undefined) return false
  try {
    const given = decodeBase64(access.recordMac)
    if (given.length !== KEY_BYTE_COUNT) return false
    return areBytesEqual(given, decodeBase64(await macOfAccess(accountKey, itemId, itemKind, access)))
  } catch {
    return false
  }
}

// verifyAccess returns the fields' access record when it is signed for this
// item (see isAccessVerified), and null when there is none or it is not signed.
// A device treats null as a record that reaches nobody: it re-keys nothing and
// makes no agent copy from the item, and asks the person to set sharing again.
export async function verifyAccess(
  accountKey: Uint8Array,
  itemId: string,
  itemKind: ItemKind,
  fields: ItemFields,
): Promise<ItemAccess | null> {
  return (await isAccessVerified(accountKey, itemId, itemKind, fields.access)) ? (fields.access ?? null) : null
}

// ---------------------------------------------------------------------------
// Sealed for a key pair: a recipient's, or the server's agent key

// deriveShareKey: the two shared secrets, the ephemeral one then the owner's,
// are the HKDF input, and the recipient's public key is its salt, which ties
// the key to the one recipient it was made for.
export function deriveShareKey(
  ephemeralShared: Uint8Array,
  ownerShared: Uint8Array,
  recipientPublicKey: Uint8Array,
): Promise<Uint8Array> {
  return deriveHkdfBytes(
    concatenateBytes(ephemeralShared, ownerShared),
    readPublicKey(recipientPublicKey),
    SHARE_KEY_INFO,
  )
}

// sealForKeyPairWithRandomness is for tests only: it seals with an ephemeral
// key pair and a nonce the caller fixes, so the vectors reproduce.
export async function sealForKeyPairWithRandomness(
  itemKey: Uint8Array,
  ownerKeyPair: PersonKeyPair,
  recipientPublicKey: Uint8Array,
  itemId: string,
  ownerId: string,
  recipientId: string,
  ephemeralKeyPair: PersonKeyPair,
  nonce: Uint8Array,
): Promise<SealedForKeyPair> {
  const ephemeralShared = await deriveSharedSecret(ephemeralKeyPair, recipientPublicKey)
  const ownerShared = await deriveSharedSecret(ownerKeyPair, recipientPublicKey)
  const shareKey = await deriveShareKey(ephemeralShared, ownerShared, recipientPublicKey)
  const sealed = await sealValueWithNonce(shareKey, itemKey, shareAssociatedData(itemId, ownerId, recipientId), nonce)
  return {
    version: sealed.version,
    ephemeralPublicKey: encodeBase64(ephemeralKeyPair.publicKey),
    nonce: sealed.nonce,
    ciphertext: sealed.ciphertext,
  }
}

// sealForKeyPair seals an item key for one recipient, sent by the item's
// owner. The key mixes a secret from a one-time key pair with one from the
// owner's own key pair, so only a holder of the owner's private key can make a
// share that opens: the server cannot plant an item "shared by" somebody.
// ownerId is the owner's user id, and recipientId is the recipient's user id,
// or AGENT_RECIPIENT_ID for the server's agent key.
export async function sealForKeyPair(
  itemKey: Uint8Array,
  ownerKeyPair: PersonKeyPair,
  recipientPublicKey: Uint8Array,
  itemId: string,
  ownerId: string,
  recipientId: string,
): Promise<SealedForKeyPair> {
  return sealForKeyPairWithRandomness(
    itemKey,
    ownerKeyPair,
    recipientPublicKey,
    itemId,
    ownerId,
    recipientId,
    await generatePersonKeyPair(),
    randomBytes(NONCE_BYTE_COUNT),
  )
}

// openForKeyPair opens what sealForKeyPair made, with the recipient's key pair
// and the public key of the owner the share claims to come from. A share made
// with any other owner key does not open.
export async function openForKeyPair(
  sealed: SealedForKeyPair,
  recipientKeyPair: PersonKeyPair,
  ownerPublicKey: Uint8Array,
  itemId: string,
  ownerId: string,
  recipientId: string,
): Promise<Uint8Array> {
  if (sealed.version !== SEALED_VALUE_VERSION)
    throw new PasswordsCryptoError(`Unknown sealed value version ${sealed.version}`)
  const ephemeralShared = await deriveSharedSecret(recipientKeyPair, decodeBase64(sealed.ephemeralPublicKey))
  const ownerShared = await deriveSharedSecret(recipientKeyPair, ownerPublicKey)
  const shareKey = await deriveShareKey(ephemeralShared, ownerShared, recipientKeyPair.publicKey)
  return openValue(
    shareKey,
    { version: sealed.version, nonce: sealed.nonce, ciphertext: sealed.ciphertext },
    shareAssociatedData(itemId, ownerId, recipientId),
  )
}

// ---------------------------------------------------------------------------
// The sealed values of an account and its items, with their associated data
// fixed here so no caller can get it wrong.

export function sealAccountKey(wrapKey: Uint8Array, accountKey: Uint8Array): Promise<SealedValue> {
  return sealValue(wrapKey, readKey(accountKey, 'The account key'), ACCOUNT_KEY_ASSOCIATED_DATA)
}

export function openAccountKey(wrapKey: Uint8Array, sealed: SealedValue): Promise<Uint8Array> {
  return openValue(wrapKey, sealed, ACCOUNT_KEY_ASSOCIATED_DATA)
}

export function sealPrivateKey(accountKey: Uint8Array, privateKey: Uint8Array): Promise<SealedValue> {
  return sealValue(accountKey, readKey(privateKey, 'The private key'), PRIVATE_KEY_ASSOCIATED_DATA)
}

export function openPrivateKey(accountKey: Uint8Array, sealed: SealedValue): Promise<Uint8Array> {
  return openValue(accountKey, sealed, PRIVATE_KEY_ASSOCIATED_DATA)
}

export function sealItemKey(accountKey: Uint8Array, itemKey: Uint8Array, itemId: string): Promise<SealedValue> {
  return sealValue(accountKey, readKey(itemKey, 'The item key'), itemKeyAssociatedData(itemId))
}

export function openItemKey(accountKey: Uint8Array, sealed: SealedValue, itemId: string): Promise<Uint8Array> {
  return openValue(accountKey, sealed, itemKeyAssociatedData(itemId))
}

export function sealItemFields<Kind extends ItemKind>(
  itemKey: Uint8Array,
  fields: ItemFieldsByKind[Kind],
  itemId: string,
  itemKind: Kind,
): Promise<SealedValue> {
  return sealValue(itemKey, encodeUtf8(JSON.stringify(fields)), itemFieldsAssociatedData(itemId, itemKind))
}

export async function openItemFields<Kind extends ItemKind>(
  itemKey: Uint8Array,
  sealed: SealedValue,
  itemId: string,
  itemKind: Kind,
): Promise<ItemFieldsByKind[Kind]> {
  const plaintext = await openValue(itemKey, sealed, itemFieldsAssociatedData(itemId, itemKind))
  return JSON.parse(decodeUtf8(plaintext)) as ItemFieldsByKind[Kind]
}
