// One-time codes (RFC 6238) from an otpauth:// address. The secret stays in
// the login's oneTimeCodeUri; a code is worked out on the device each time it
// is shown, never stored.

import { decodeBase32, PasswordsCryptoError } from './crypto'

export type OneTimeCodeAlgorithm = 'SHA1' | 'SHA256' | 'SHA512'

export const DEFAULT_ONE_TIME_CODE_DIGIT_COUNT = 6
export const DEFAULT_ONE_TIME_CODE_PERIOD_SECONDS = 30

const HASH_NAMES: Record<OneTimeCodeAlgorithm, string> = { SHA1: 'SHA-1', SHA256: 'SHA-256', SHA512: 'SHA-512' }

export interface OneTimeCodeSettings {
  secret: Uint8Array
  algorithm: OneTimeCodeAlgorithm
  digitCount: number
  periodSeconds: number
  // issuer and accountName are for display only.
  issuer: string
  accountName: string
}

// The longest period read: far longer than any site uses, and a number every
// language here holds exactly.
const MAXIMUM_ONE_TIME_CODE_PERIOD_SECONDS = 2147483647

// hasControlCharacter is true for an ASCII control character, including a
// delete.
function hasControlCharacter(text: string): boolean {
  for (let index = 0; index < text.length; index++) {
    const code = text.charCodeAt(index)
    if (code < 0x20 || code === 0x7f) return true
  }
  return false
}

// parseOneTimeCodeUri reads otpauth://totp/<label>?secret=...; counter based
// codes (otpauth://hotp) are refused, as no site this is for uses them.
//
// The reading is strict, and the same as ParseOneTimeCodeURI in
// internal/passwords/onetimecode.go; both read the cases in
// internal/passwords/testdata/onetimecode.json. The host is exactly "totp" (no
// port), the query has no malformed percent escape and no semicolon, the
// secret holds only base32 letters (case does not matter) with spaces and
// hyphens ignored and padding "=" only at the end, and the digit count and
// the period are plain ASCII digits (no sign, no space) with a period of at
// most MAXIMUM_ONE_TIME_CODE_PERIOD_SECONDS.
export function parseOneTimeCodeUri(uri: string): OneTimeCodeSettings {
  // Only ASCII white space is trimmed from either end (Go's TrimSpace and
  // this language's trim differ on a few others). A control character inside
  // is refused: the address parser would drop a tab or a line break wherever
  // it is, which would let one hide inside a secret. A malformed percent
  // escape is refused, as Go's address parser does.
  const text = uri.replace(/^[ \t\n\r\f\v]+|[ \t\n\r\f\v]+$/g, '')
  if (hasControlCharacter(text) || /%(?![0-9A-Fa-f]{2})/.test(text)) {
    throw new PasswordsCryptoError('The one-time code address is not a web address')
  }
  let address: URL
  try {
    address = new URL(text)
  } catch {
    throw new PasswordsCryptoError('The one-time code address is not a web address')
  }
  if (address.protocol !== 'otpauth:')
    throw new PasswordsCryptoError('The one-time code address must start with otpauth://')
  // The text between "//" and the next "/", "?" or "#" is exactly "totp": no
  // user name, no port, not even an empty one.
  const authority = /^[A-Za-z][A-Za-z0-9+.-]*:\/\/([^/?#]*)/.exec(text)?.[1] ?? ''
  if (authority.toLowerCase() !== 'totp') throw new PasswordsCryptoError('Only time based one-time codes are supported')
  // URLSearchParams keeps a semicolon as text; the Go reading refuses it.
  if (address.search.includes(';')) throw new PasswordsCryptoError('The one-time code address has a malformed query')

  const secretText = (address.searchParams.get('secret') ?? '').replace(/[ -]/g, '').replace(/=+$/, '')
  if (!secretText) throw new PasswordsCryptoError('The one-time code address has no secret')
  if (!/^[A-Za-z2-7]+$/.test(secretText)) throw new PasswordsCryptoError('The one-time code secret is not base32')
  const secret = decodeBase32(secretText)
  if (secret.length === 0) throw new PasswordsCryptoError('The one-time code secret is empty')

  const algorithmText = (address.searchParams.get('algorithm') || 'SHA1').toUpperCase()
  if (!Object.prototype.hasOwnProperty.call(HASH_NAMES, algorithmText)) {
    throw new PasswordsCryptoError(`Unsupported one-time code algorithm ${algorithmText}`)
  }

  const digitCount = readWholeNumber(address.searchParams.get('digits'), DEFAULT_ONE_TIME_CODE_DIGIT_COUNT, 'digits')
  if (digitCount !== 6 && digitCount !== 8) throw new PasswordsCryptoError('A one-time code has 6 or 8 digits')

  const periodSeconds = readWholeNumber(
    address.searchParams.get('period'),
    DEFAULT_ONE_TIME_CODE_PERIOD_SECONDS,
    'period',
  )
  if (periodSeconds < 1 || periodSeconds > MAXIMUM_ONE_TIME_CODE_PERIOD_SECONDS) {
    throw new PasswordsCryptoError('The one-time code period must be from 1 second to 2147483647 seconds')
  }

  // The label is "Issuer:account" or "account", percent-encoded after the slash.
  let label: string
  try {
    label = decodeURIComponent(address.pathname.replace(/^\//, ''))
  } catch {
    label = address.pathname.replace(/^\//, '')
  }
  const separatorIndex = label.indexOf(':')
  const labelIssuer = separatorIndex >= 0 ? label.slice(0, separatorIndex).trim() : ''
  const accountName = (separatorIndex >= 0 ? label.slice(separatorIndex + 1) : label).trim()

  return {
    secret,
    algorithm: algorithmText as OneTimeCodeAlgorithm,
    digitCount,
    periodSeconds,
    issuer: address.searchParams.get('issuer') ?? labelIssuer,
    accountName,
  }
}

function readWholeNumber(text: string | null, fallback: number, name: string): number {
  if (text === null || text === '') return fallback
  if (!/^[0-9]+$/.test(text)) throw new PasswordsCryptoError(`The one-time code ${name} is not a number`)
  return Number(text)
}

// oneTimeCode is the code for the moment unixSeconds (whole seconds since 1970).
export async function oneTimeCode(settings: OneTimeCodeSettings, unixSeconds: number): Promise<string> {
  const counter = Math.floor(unixSeconds / settings.periodSeconds)
  const message = new Uint8Array(8)
  const view = new DataView(message.buffer)
  view.setUint32(0, Math.floor(counter / 0x100000000))
  view.setUint32(4, counter % 0x100000000)

  const key = await crypto.subtle.importKey(
    'raw',
    new Uint8Array(settings.secret),
    { name: 'HMAC', hash: HASH_NAMES[settings.algorithm] },
    false,
    ['sign'],
  )
  const digest = new Uint8Array(await crypto.subtle.sign('HMAC', key, message))

  // Dynamic truncation (RFC 4226 section 5.3): four bytes at an offset the
  // last nibble picks, without the top bit.
  const offset = digest[digest.length - 1] & 0x0f
  const truncated =
    ((digest[offset] & 0x7f) << 24) | (digest[offset + 1] << 16) | (digest[offset + 2] << 8) | digest[offset + 3]
  return String(truncated % 10 ** settings.digitCount).padStart(settings.digitCount, '0')
}

// oneTimeCodeSecondsLeft is how long the code for unixSeconds still holds, from
// periodSeconds down to 1.
export function oneTimeCodeSecondsLeft(settings: OneTimeCodeSettings, unixSeconds: number): number {
  return settings.periodSeconds - (Math.floor(unixSeconds) % settings.periodSeconds)
}
