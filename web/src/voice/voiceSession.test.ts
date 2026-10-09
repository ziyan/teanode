import { describe, expect, it } from 'vitest'

import { MicrophoneMute, TranscriptTracker, voiceSocketAddress } from './voiceSession'

describe('TranscriptTracker', () => {
  it('shows the words as they come and lets each finished utterance through once', () => {
    const tracker = new TranscriptTracker()
    expect(tracker.accept({ voiceEvent: 'speechStarted', utteranceId: 'a' })).toEqual({ captionText: '' })
    expect(tracker.accept({ voiceEvent: 'transcriptDelta', utteranceId: 'a', transcriptText: 'What is' })).toEqual({
      captionText: 'What is',
    })
    expect(tracker.accept({ voiceEvent: 'transcriptDelta', utteranceId: 'a', transcriptText: ' on' })).toEqual({
      captionText: 'What is on',
    })
    expect(
      tracker.accept({
        voiceEvent: 'transcriptFinal',
        utteranceId: 'a',
        utteranceSequence: 1,
        transcriptText: ' What is on? ',
      }),
    ).toEqual({ captionText: '', transcriptText: 'What is on?' })
    // Said again, after a reconnect or a repeat: not sent twice.
    expect(tracker.accept({ voiceEvent: 'transcriptFinal', utteranceId: 'a', transcriptText: 'What is on?' })).toEqual(
      {},
    )
    expect(tracker.accept({ voiceEvent: 'transcriptDelta', utteranceId: 'a', transcriptText: 'late' })).toEqual({})
  })

  it('keeps the next utterance on screen when an earlier one finishes', () => {
    const tracker = new TranscriptTracker()
    tracker.accept({ voiceEvent: 'speechStarted', utteranceId: 'a' })
    tracker.accept({ voiceEvent: 'transcriptDelta', utteranceId: 'a', transcriptText: 'first' })
    tracker.accept({ voiceEvent: 'speechStarted', utteranceId: 'b' })
    tracker.accept({ voiceEvent: 'transcriptDelta', utteranceId: 'b', transcriptText: 'second' })
    expect(tracker.accept({ voiceEvent: 'transcriptFinal', utteranceId: 'a', transcriptText: 'first' })).toEqual({
      captionText: 'second',
      transcriptText: 'first',
    })
  })

  it('sends nothing for silence, and says when an utterance was not heard', () => {
    const tracker = new TranscriptTracker()
    expect(tracker.accept({ voiceEvent: 'transcriptFinal', utteranceId: 'a', transcriptText: '  ' })).toEqual({
      captionText: '',
    })
    expect(tracker.accept({ voiceEvent: 'transcriptFailed', utteranceId: 'b', errorMessage: 'x' })).toEqual({
      captionText: '',
      isNotHeard: true,
    })
  })
})

describe('voiceSocketAddress', () => {
  it('follows the page: wss on https, ws on http', () => {
    expect(voiceSocketAddress({ protocol: 'https:', host: 'mail.example.com' } as Location)).toBe(
      'wss://mail.example.com/api/v1/agent/voice',
    )
    expect(voiceSocketAddress({ protocol: 'http:', host: 'localhost:10081' } as Location)).toBe(
      'ws://localhost:10081/api/v1/agent/voice',
    )
  })
})

describe('the capture worklet', () => {
  it('turns 48 kHz float samples into 24 kHz 16-bit frames of a tenth of a second', async () => {
    const posted: ArrayBuffer[] = []
    let processor: { new (options: unknown): { process: (inputs: Float32Array[][]) => boolean } } | undefined
    const scope = globalThis as Record<string, unknown>
    scope.sampleRate = 48000
    scope.AudioWorkletProcessor = class {
      port = { postMessage: (buffer: ArrayBuffer) => posted.push(buffer) }
    }
    scope.registerProcessor = (_name: string, constructor: typeof processor) => {
      processor = constructor
    }
    await import('./voiceCaptureWorklet.js')
    const capture = new processor!({ processorOptions: { targetRate: 24000 } })
    // A fifth of a second of a 440 Hz tone at half scale, in blocks of 128.
    const total = 48000 / 5
    for (let start = 0; start < total; start += 128) {
      const block = new Float32Array(128)
      for (let index = 0; index < 128; index++)
        block[index] = 0.5 * Math.sin((2 * Math.PI * 440 * (start + index)) / 48000)
      capture.process([[block]])
    }
    // 9,600 input samples make 4,800 output samples: two whole frames.
    expect(posted.length).toBe(2)
    const frame = new Int16Array(posted[0])
    expect(frame.length).toBe(2400)
    // Every other input sample, scaled to 16 bits.
    for (const index of [0, 1, 7, 100, 2399]) {
      const expected = 0.5 * Math.sin((2 * Math.PI * 440 * index * 2) / 48000) * 0x7fff
      expect(Math.abs(frame[index] - expected)).toBeLessThan(40)
    }
  })
})

describe('MicrophoneMute', () => {
  it('sends every frame until muted', () => {
    const mute = new MicrophoneMute()
    expect(mute.frame(0)).toBe('audio')
    expect(mute.frame(100_000)).toBe('audio')
    expect(mute.isMuted()).toBe(false)
  })

  it('sends silence for a moment once muted, then no audio, and says now and then that the call goes on', () => {
    const mute = new MicrophoneMute()
    mute.setMuted(true, 1_000)
    expect(mute.isMuted()).toBe(true)
    // What was being said ends as an utterance.
    expect(mute.frame(1_100)).toBe('audio')
    expect(mute.frame(6_900)).toBe('audio')
    expect(mute.frame(7_000)).toBe('nothing')
    expect(mute.frame(26_800)).toBe('nothing')
    // Well inside the minute the server waits.
    expect(mute.frame(26_900)).toBe('stillHere')
    expect(mute.frame(27_000)).toBe('nothing')
    expect(mute.frame(46_900)).toBe('stillHere')
  })

  it('sends audio again at once when unmuted, and starts the silence afresh when muted again', () => {
    const mute = new MicrophoneMute()
    mute.setMuted(true, 0)
    expect(mute.frame(10_000)).toBe('nothing')
    mute.setMuted(false, 11_000)
    expect(mute.frame(11_000)).toBe('audio')
    mute.setMuted(true, 12_000)
    expect(mute.frame(17_900)).toBe('audio')
    expect(mute.frame(18_000)).toBe('nothing')
  })

  it('keeps when it was muted if told again', () => {
    const mute = new MicrophoneMute()
    mute.setMuted(true, 0)
    mute.setMuted(true, 5_000)
    expect(mute.frame(6_000)).toBe('nothing')
  })
})
