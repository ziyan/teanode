// The authenticator half of a passkey ceremony, apart from the browser: what
// a page asks for is checked and read here, and what comes back is the bytes
// a website verifies. The byte layouts are in web/src/passwords/passkey.ts
// (the plan fixes them); this file adds the parts that belong to a browser
// ceremony: clientDataJSON, the public key in the form getPublicKey() returns,
// and the checks of the relying party against the page.

import {
  concatenateBytes,
  decodeBase64,
  decodeBase64Url,
  encodeBase64,
  encodeBase64Url,
  encodeUtf8,
} from '../../../src/passwords/crypto'
import { PasskeyFields } from '../../../src/passwords/fields'
import {
  assertionAuthenticatorData,
  createPasskey,
  encodeAttestationObject,
  registrationAuthenticatorData,
  signAssertion,
} from '../../../src/passwords/passkey'
import { effectiveDomain, isRelyingPartyOfHost, isSecureContextPage } from './hosts'

// What the extension knows about the page that asked, from the browser's own
// description of the frame that sent the message and never from the page.
export interface PageContext {
  pageUrl: string
  origin: string
  // isCrossOrigin: some page above the frame has another origin.
  isCrossOrigin: boolean
}

// How a request that cannot be served is answered: left to the browser, or
// refused with the error a browser would throw.
export type Refusal =
  | { outcome: 'fallback'; reason: string }
  | { outcome: 'error'; errorName: 'SecurityError' | 'NotSupportedError' | 'TypeError'; reason: string }

export type Parsed<Request> = { outcome: 'ok'; request: Request } | Refusal

export interface PasskeyCreationRequest {
  relyingPartyId: string
  relyingPartyName: string
  // The user handle, user name and display name the site chose, and the
  // challenge and credential ids it sent, as base64url.
  userHandle: string
  userName: string
  userDisplayName: string
  challenge: string
  excludedCredentialIds: string[]
}

export interface PasskeyAssertionRequest {
  relyingPartyId: string
  challenge: string
  // Empty when the site accepts any passkey it can find for the relying party.
  allowedCredentialIds: string[]
}

const BASE64_URL_PATTERN = /^[A-Za-z0-9_-]+$/
const ES256 = -7
// A user handle is at most 64 bytes (WebAuthn section 5.4.3).
const MAXIMUM_USER_HANDLE_BYTES = 64

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function readBase64Url(value: unknown): string | null {
  if (typeof value !== 'string' || value === '' || !BASE64_URL_PATTERN.test(value)) return null
  try {
    decodeBase64Url(value)
  } catch {
    return null
  }
  return value
}

function readCredentialIds(value: unknown): string[] | null {
  if (value === undefined || value === null) return []
  if (!Array.isArray(value)) return null
  const ids: string[] = []
  for (const entry of value) {
    if (!isRecord(entry)) return null
    const id = readBase64Url(entry.id)
    if (id === null) return null
    ids.push(id)
  }
  return ids
}

// readRelyingPartyId takes the id the page named, or the page's own host, and
// checks it is one the page may use. An address as a page has no domain above
// it, which isRelyingPartyOfHost knows.
function readRelyingPartyId(named: unknown, context: PageContext): { id: string } | Refusal {
  if (!isSecureContextPage(context.pageUrl)) {
    return { outcome: 'fallback', reason: 'passkeys need a secure page' }
  }
  const id =
    typeof named === 'string' && named.trim() !== '' ? named.trim().toLowerCase() : effectiveDomain(context.pageUrl)
  if (!isRelyingPartyOfHost(id, context.pageUrl)) {
    return { outcome: 'error', errorName: 'SecurityError', reason: 'the relying party id is not this site' }
  }
  return { id }
}

// parseCreationRequest checks the options of navigator.credentials.create's
// publicKey the main world script copied (binary values as base64url). What
// this authenticator cannot make (no ES256 offered, a security key asked for)
// is left to the browser; what the page has no right to ask for is refused.
export function parseCreationRequest(raw: unknown, context: PageContext): Parsed<PasskeyCreationRequest> {
  if (!isRecord(raw) || !isRecord(raw.rp) || !isRecord(raw.user)) {
    return { outcome: 'error', errorName: 'TypeError', reason: 'the request is missing its relying party or user' }
  }
  const relyingParty = readRelyingPartyId(raw.rp.id, context)
  if ('outcome' in relyingParty) return relyingParty

  const challenge = readBase64Url(raw.challenge)
  const userHandle = readBase64Url(raw.user.id)
  if (challenge === null || userHandle === null || decodeBase64Url(userHandle).length > MAXIMUM_USER_HANDLE_BYTES) {
    return { outcome: 'error', errorName: 'TypeError', reason: 'the challenge or user id is not usable' }
  }
  if (typeof raw.user.name !== 'string' || raw.user.name === '') {
    return { outcome: 'error', errorName: 'TypeError', reason: 'the user has no name' }
  }
  const parameters = Array.isArray(raw.pubKeyCredParams) ? raw.pubKeyCredParams : []
  // No list at all means ES256 and RS256 (WebAuthn section 5.4); an empty
  // one is read the same way.
  const offersEs256 = parameters.length === 0 || parameters.some((entry) => isRecord(entry) && entry.alg === ES256)
  if (!offersEs256) return { outcome: 'fallback', reason: 'this site does not accept ES256 passkeys' }
  const selection = isRecord(raw.authenticatorSelection) ? raw.authenticatorSelection : {}
  if (selection.authenticatorAttachment === 'cross-platform') {
    return { outcome: 'fallback', reason: 'a security key was asked for' }
  }
  const excludedCredentialIds = readCredentialIds(raw.excludeCredentials)
  if (excludedCredentialIds === null) {
    return { outcome: 'error', errorName: 'TypeError', reason: 'the excluded credentials are not usable' }
  }
  return {
    outcome: 'ok',
    request: {
      relyingPartyId: relyingParty.id,
      relyingPartyName: isRecord(raw.rp) && typeof raw.rp.name === 'string' ? raw.rp.name : '',
      userHandle,
      userName: raw.user.name,
      userDisplayName: typeof raw.user.displayName === 'string' ? raw.user.displayName : '',
      challenge,
      excludedCredentialIds,
    },
  }
}

// parseAssertionRequest checks the options of navigator.credentials.get's
// publicKey the same way.
export function parseAssertionRequest(raw: unknown, context: PageContext): Parsed<PasskeyAssertionRequest> {
  if (!isRecord(raw)) return { outcome: 'error', errorName: 'TypeError', reason: 'the request is empty' }
  const relyingParty = readRelyingPartyId(raw.rpId, context)
  if ('outcome' in relyingParty) return relyingParty
  const challenge = readBase64Url(raw.challenge)
  if (challenge === null) return { outcome: 'error', errorName: 'TypeError', reason: 'the challenge is not usable' }
  const allowedCredentialIds = readCredentialIds(raw.allowCredentials)
  if (allowedCredentialIds === null) {
    return { outcome: 'error', errorName: 'TypeError', reason: 'the allowed credentials are not usable' }
  }
  return {
    outcome: 'ok',
    request: { relyingPartyId: relyingParty.id, challenge, allowedCredentialIds },
  }
}

// buildClientDataJson is the text a browser would hand the site as
// clientDataJSON, and what the signature covers through its hash. The
// extension writes it itself from the origin the browser reported; the page's
// own idea of its origin is never used.
export function buildClientDataJson(input: {
  type: 'webauthn.create' | 'webauthn.get'
  challenge: string
  origin: string
  isCrossOrigin: boolean
}): string {
  return JSON.stringify({
    type: input.type,
    challenge: input.challenge,
    origin: input.origin,
    crossOrigin: input.isCrossOrigin,
  })
}

// encodeSpkiPublicKey is the public key in the DER SubjectPublicKeyInfo form
// getPublicKey() returns: the fixed prefix for an uncompressed P-256 key, then
// the 65 byte point.
const SPKI_P256_PREFIX = new Uint8Array([
  0x30, 0x59, 0x30, 0x13, 0x06, 0x07, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x02, 0x01, 0x06, 0x08, 0x2a, 0x86, 0x48, 0xce,
  0x3d, 0x03, 0x01, 0x07, 0x03, 0x42, 0x00, 0x04,
])

export function encodeSpkiPublicKey(publicKeyX: Uint8Array, publicKeyY: Uint8Array): Uint8Array {
  return concatenateBytes(SPKI_P256_PREFIX, publicKeyX, publicKeyY)
}

// What goes back to the page, every binary value as base64url. The main world
// script turns it into the object the page expects.
export interface AttestationCredentialJson {
  id: string
  rawId: string
  clientDataJSON: string
  attestationObject: string
  authenticatorData: string
  publicKey: string
  publicKeyAlgorithm: number
  transports: string[]
}

export interface AssertionCredentialJson {
  id: string
  rawId: string
  clientDataJSON: string
  authenticatorData: string
  signature: string
  userHandle: string | null
}

// The passkey is shown to the site as a platform authenticator that syncs
// (a computer or phone, reachable by the browser and by a QR code).
const TRANSPORTS = ['internal', 'hybrid']

export interface MadePasskey {
  credential: AttestationCredentialJson
  // passkeyFields is what the login keeps; the private key is in it.
  passkeyFields: PasskeyFields
}

// makePasskey creates a passkey for a creation request: a new key pair, the
// authenticator data and the attestation object (format "none"). The person
// confirmed in the extension's own window and the passphrase unlocked
// Passwords, so the ceremony counts as user verified.
export async function makePasskey(
  request: PasskeyCreationRequest,
  context: PageContext,
  now: Date = new Date(),
): Promise<MadePasskey> {
  const passkey = await createPasskey()
  const authenticatorData = await registrationAuthenticatorData({
    relyingPartyId: request.relyingPartyId,
    credentialId: passkey.credentialId,
    publicKeyX: passkey.publicKeyX,
    publicKeyY: passkey.publicKeyY,
    isVerified: true,
  })
  const clientDataJson = buildClientDataJson({
    type: 'webauthn.create',
    challenge: request.challenge,
    origin: context.origin,
    isCrossOrigin: context.isCrossOrigin,
  })
  const credentialId = encodeBase64Url(passkey.credentialId)
  return {
    credential: {
      id: credentialId,
      rawId: credentialId,
      clientDataJSON: encodeBase64Url(encodeUtf8(clientDataJson)),
      attestationObject: encodeBase64Url(encodeAttestationObject(authenticatorData)),
      authenticatorData: encodeBase64Url(authenticatorData),
      publicKey: encodeBase64Url(encodeSpkiPublicKey(passkey.publicKeyX, passkey.publicKeyY)),
      publicKeyAlgorithm: ES256,
      transports: TRANSPORTS,
    },
    passkeyFields: {
      credentialId: encodeBase64(passkey.credentialId),
      relyingPartyId: request.relyingPartyId,
      userHandle: encodeBase64(decodeBase64Url(request.userHandle)),
      userName: request.userName,
      userDisplayName: request.userDisplayName,
      privateKey: encodeBase64(passkey.privateKey),
      createdAt: now.toISOString(),
    },
  }
}

// signWithPasskey answers a sign-in request with a passkey the login keeps.
export async function signWithPasskey(
  request: PasskeyAssertionRequest,
  passkey: PasskeyFields,
  context: PageContext,
): Promise<AssertionCredentialJson> {
  const authenticatorData = await assertionAuthenticatorData(request.relyingPartyId, true)
  const clientDataJson = buildClientDataJson({
    type: 'webauthn.get',
    challenge: request.challenge,
    origin: context.origin,
    isCrossOrigin: context.isCrossOrigin,
  })
  const clientDataBytes = encodeUtf8(clientDataJson)
  const clientDataHash = new Uint8Array(await crypto.subtle.digest('SHA-256', new Uint8Array(clientDataBytes)))
  const signature = await signAssertion(decodeBase64(passkey.privateKey), authenticatorData, clientDataHash)
  const credentialId = encodeBase64Url(decodeBase64(passkey.credentialId))
  return {
    id: credentialId,
    rawId: credentialId,
    clientDataJSON: encodeBase64Url(clientDataBytes),
    authenticatorData: encodeBase64Url(authenticatorData),
    signature: encodeBase64Url(signature),
    userHandle: passkey.userHandle === '' ? null : encodeBase64Url(decodeBase64(passkey.userHandle)),
  }
}

// credentialIdOf is a stored passkey's id in the base64url a site uses.
export function credentialIdOf(passkey: PasskeyFields): string {
  return encodeBase64Url(decodeBase64(passkey.credentialId))
}
