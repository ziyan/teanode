import { describe, expect, it } from 'vitest'
import { CutPolicy, MAXIMUM_PIECE_MS, MINIMUM_PIECE_MS, QUIET_HOLD_MS } from './cutPolicy'

// Feeds frames of 100 ms at the given level from fromMS to untilMS and answers
// the first cut asked for, or null.
function feed(policy: CutPolicy, fromMS: number, untilMS: number, levelAt: (endMS: number) => number, isLive = false) {
  for (let endMS = fromMS + 100; endMS <= untilMS; endMS += 100) {
    const cutMS = policy.observeFrame(endMS, levelAt(endMS), isLive)
    if (cutMS !== null) return cutMS
  }
  return null
}

describe('the cut policy', () => {
  it('does not cut a short piece, however quiet it is', () => {
    const policy = new CutPolicy()
    policy.startPiece(0)
    expect(feed(policy, 0, MINIMUM_PIECE_MS - 100, () => -80)).toBeNull()
  })

  it('cuts at the next utterance end once the piece is long enough, and not before', () => {
    const policy = new CutPolicy()
    policy.startPiece(5_000)
    expect(policy.observeUtteranceEnd(24_900, 26_000)).toBeNull()
    expect(policy.observeUtteranceEnd(25_000, 26_000)).toBe(25_000)
  })

  it('does not cut at an utterance end past the audio it holds', () => {
    const policy = new CutPolicy()
    policy.startPiece(0)
    expect(policy.observeUtteranceEnd(30_000, 28_000)).toBeNull()
  })

  it('does not cut on a pause in the level while the live stream is connected', () => {
    const policy = new CutPolicy()
    policy.startPiece(0)
    expect(feed(policy, 0, 40_000, () => -80, true)).toBeNull()
  })

  it('without the live stream, cuts at the first quarter second within 6 dB of the lowest level', () => {
    const policy = new CutPolicy()
    policy.startPiece(0)
    // Talking at -20 dB with a breath at -60 dB each second, then quiet from 25 s.
    const levelAt = (endMS: number) => (endMS < 25_000 ? (endMS % 1000 === 0 ? -60 : -20) : -60)
    const cutMS = feed(policy, 0, 40_000, levelAt)
    // The quiet begins with the frame that ends at 25,000 and has to last 250 ms,
    // which three frames of 100 ms do.
    expect(cutMS).toBe(25_000 + 3 * 100)
    expect(cutMS).toBeGreaterThanOrEqual(25_000 + QUIET_HOLD_MS)
  })

  it('does not cut on one quiet frame between words', () => {
    const policy = new CutPolicy()
    policy.startPiece(0)
    // Mostly speech with a single quiet frame every second.
    const levelAt = (endMS: number) => (endMS % 1000 === 0 ? -60 : -20)
    expect(feed(policy, 0, 40_000, levelAt)).toBeNull()
  })

  it('cuts a piece that never goes quiet at 60 seconds', () => {
    const policy = new CutPolicy()
    policy.startPiece(10_000)
    // Steady noise: every frame is within 6 dB of the lowest, so the level
    // rule would cut at 20 seconds; with a rising level it never does.
    const cutMS = feed(policy, 10_000, 90_000, (endMS) => -80 + (endMS - 10_000) / 100)
    expect(cutMS).toBe(10_000 + MAXIMUM_PIECE_MS)
  })

  it('measures the next piece from where the last was cut', () => {
    const policy = new CutPolicy()
    policy.startPiece(0)
    expect(policy.observeUtteranceEnd(21_000, 22_000)).toBe(21_000)
    policy.startPiece(21_000)
    expect(policy.observeUtteranceEnd(40_000, 41_000)).toBeNull()
    expect(policy.observeUtteranceEnd(41_000, 42_000)).toBe(41_000)
  })
})
