// A recorded piece is written as a WAV file of 16-bit mono PCM: the server
// already decodes it, and it needs no encoder in the browser.
export const PIECE_SAMPLE_RATE = 16_000

export function encodeWav(samples: Int16Array, sampleRate = PIECE_SAMPLE_RATE): ArrayBuffer {
  const dataBytes = samples.length * 2
  const buffer = new ArrayBuffer(44 + dataBytes)
  const view = new DataView(buffer)
  const writeText = (offset: number, text: string) => {
    for (let index = 0; index < text.length; index++) view.setUint8(offset + index, text.charCodeAt(index))
  }
  writeText(0, 'RIFF')
  view.setUint32(4, 36 + dataBytes, true)
  writeText(8, 'WAVE')
  writeText(12, 'fmt ')
  view.setUint32(16, 16, true)
  view.setUint16(20, 1, true) // PCM
  view.setUint16(22, 1, true) // mono
  view.setUint32(24, sampleRate, true)
  view.setUint32(28, sampleRate * 2, true)
  view.setUint16(32, 2, true)
  view.setUint16(34, 16, true)
  writeText(36, 'data')
  view.setUint32(40, dataBytes, true)
  new Int16Array(buffer, 44, samples.length).set(samples)
  return buffer
}

// levelDecibels is how loud a frame is, in decibels below full scale; silence
// is bounded so a run of exact zeros does not become minus infinity.
export function levelDecibels(samples: Int16Array): number {
  if (samples.length === 0) return -100
  let sumOfSquares = 0
  for (const sample of samples) sumOfSquares += sample * sample
  const rootMeanSquare = Math.sqrt(sumOfSquares / samples.length) / 32768
  return Math.max(-100, 20 * Math.log10(Math.max(rootMeanSquare, 1e-9)))
}
