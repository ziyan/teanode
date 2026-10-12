import { describe, expect, it } from 'vitest'
import { RecordingSha256 } from './sha256'
describe('incremental recording digest', () => {
  it('matches independent SHA-256 across padding and transport boundaries', async () => {
    for (const byteCount of [0, 3, 55, 56, 63, 64, 65, 128, 8193, 8 * 1024 * 1024 + 17]) {
      const content = new Uint8Array(byteCount).map((_, byteIndex) => byteIndex % 251)
      const digest = new RecordingSha256()
      for (let byteOffset = 0; byteOffset < content.length; byteOffset += 77)
        digest.update(content.subarray(byteOffset, byteOffset + 77))
      const expectedDigest = new Uint8Array(await crypto.subtle.digest('SHA-256', content.buffer))
      expect(digest.hex()).toBe(
        Array.from(expectedDigest, (byteValue) => byteValue.toString(16).padStart(2, '0')).join(''),
      )
      expect(() => digest.update(content)).toThrow()
    }
  })
})
