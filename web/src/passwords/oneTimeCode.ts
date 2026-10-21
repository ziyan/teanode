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

// parseOneTimeCodeUri reads otpauth://totp/<label>?secret=...; counter based
// codes (otpauth://hotp) are refused, as no site this is for uses them.
export function parseOneTimeCodeUri(uri: string): OneTimeCodeSettings {
  let address: URL
  try {
    address = new URL(uri.trim())
  } catch {
    throw new PasswordsCryptoError('The one-time code address is not a web address')
  }
  if (address.protocol !== 'otpauth:')
    throw new PasswordsCryptoError('The one-time code address must start with otpauth://')
  if (address.hostname.toLowerCase() !== 'totp')
    throw new PasswordsCryptoError('Only time based one-time codes are supported')

  const secretText = address.searchParams.get('secret')
  if (!secretText) throw new PasswordsCryptoError('The one-time code address has no secret')
  const secret = decodeBase32(secretText)
  if (secret.length === 0) throw new PasswordsCryptoError('The one-time code secret is empty')

  const algorithmText = (address.searchParams.get('algorithm') ?? 'SHA1').toUpperCase()
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
  if (periodSeconds < 1) throw new PasswordsCryptoError('The one-time code period must be at least 1 second')

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
  if (!/^\d+$/.test(text)) throw new PasswordsCryptoError(`The one-time code ${name} is not a number`)
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
