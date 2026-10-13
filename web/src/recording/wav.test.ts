import { expect, it } from 'vitest'
import { encodeWav, levelDecibels } from './wav'

it('writes a mono 16-bit PCM WAV file with the samples after a 44 byte header', () => {
  const bytes = encodeWav(new Int16Array([1, -2, 300]), 16000)
  const view = new DataView(bytes)
  const text = (offset: number) => String.fromCharCode(...new Uint8Array(bytes, offset, 4))
  expect(bytes.byteLength).toBe(44 + 6)
  expect([text(0), text(8), text(12), text(36)]).toEqual(['RIFF', 'WAVE', 'fmt ', 'data'])
  expect(view.getUint32(4, true)).toBe(36 + 6)
  expect(view.getUint16(20, true)).toBe(1)
  expect(view.getUint16(22, true)).toBe(1)
  expect(view.getUint32(24, true)).toBe(16000)
  expect(view.getUint32(28, true)).toBe(32000)
  expect(view.getUint16(34, true)).toBe(16)
  expect(view.getUint32(40, true)).toBe(6)
  expect([view.getInt16(44, true), view.getInt16(46, true), view.getInt16(48, true)]).toEqual([1, -2, 300])
})

it('measures the level of a frame in decibels below full scale', () => {
  expect(levelDecibels(new Int16Array(100))).toBe(-100)
  expect(levelDecibels(new Int16Array(100).fill(32767))).toBeCloseTo(0, 1)
  expect(levelDecibels(new Int16Array(100).fill(16384))).toBeCloseTo(-6.02, 1)
})
