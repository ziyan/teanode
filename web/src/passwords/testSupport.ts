// Helpers the Passwords tests share.

import { expect } from 'vitest'
import { encodeBase64 } from './crypto'

// The two ways to seal with a nonce or an ephemeral key pair the test fixes.
// They are exported from crypto.ts only so this file can reach them; the
// tests import them from here, and crypto.test.ts checks that no other file
// of the dashboard or the extension uses them.
export { sealForKeyPairWithRandomness, sealValueWithNonce } from './crypto'

// expectBytes compares by base64 so a failure prints something readable, and
// so a Node Buffer and a browser Uint8Array compare equal.
export function expectBytes(actual: Uint8Array, expected: Uint8Array, message?: string): void {
  expect(encodeBase64(new Uint8Array(actual)), message).toBe(encodeBase64(new Uint8Array(expected)))
}

export function bytesFromText(text: string): Uint8Array<ArrayBuffer> {
  return new Uint8Array(new TextEncoder().encode(text))
}

export function bytesFromHex(hex: string): Uint8Array {
  const bytes = new Uint8Array(hex.length / 2)
  for (let index = 0; index < bytes.length; index++) bytes[index] = parseInt(hex.slice(index * 2, index * 2 + 2), 16)
  return bytes
}

export function bytesFilledWith(byteCount: number, value: number): Uint8Array {
  return new Uint8Array(byteCount).fill(value)
}
