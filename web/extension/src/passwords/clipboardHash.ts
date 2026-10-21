// A copied secret is remembered only as a salted hash, so that thirty seconds
// later the extension can tell whether the clipboard still holds it without
// keeping it.

import { encodeBase64, encodeUtf8, randomBytes } from '../../../src/passwords/crypto'

export function makeClipboardSalt(): string {
  return encodeBase64(randomBytes(16))
}

export async function hashClipboardText(salt: string, text: string): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new Uint8Array(encodeUtf8(salt + '\u0000' + text)))
  return encodeBase64(new Uint8Array(digest))
}
