import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  AnswerSegmenter,
  AnswerVoice,
  heardSplit,
  isLikelyEcho,
  speakableText,
  type AnswerPlayer,
  type InterruptedAnswer,
} from './voiceAnswer'

describe('speakableText', () => {
  it('says the words, not the marks', () => {
    expect(speakableText('## Tomorrow\n- **Dentist** at `9:00`, see [the booking](https://example.com/b)')).toBe(
      'Tomorrow Dentist at 9:00, see the booking',
    )
    expect(speakableText('Read more at https://example.com/a.')).toBe('Read more at.')
    expect(speakableText('a_variable_name stays')).toBe('a_variable_name stays')
  })
})

describe('AnswerSegmenter', () => {
  it('starts with the first sentence, then groups the rest', () => {
    const segmenter = new AnswerSegmenter()
    expect(segmenter.push('Sure. You have')).toEqual(['Sure.'])
    expect(segmenter.push(' a dentist at nine. Then lunch.')).toEqual([])
    expect(
      segmenter.push(' After that the afternoon is free, apart from a call with the builder at four. And'),
    ).toEqual([
      'You have a dentist at nine. Then lunch. After that the afternoon is free, apart from a call with the builder at four.',
    ])
    expect(segmenter.flush()).toEqual(['And'])
  })

  it('waits for the end of a sentence, even after a full stop in a number', () => {
    const segmenter = new AnswerSegmenter()
    expect(segmenter.push('It costs 3.50')).toEqual([])
    expect(segmenter.push(' a month.')).toEqual([])
    expect(segmenter.flush()).toEqual(['It costs 3.50 a month.'])
  })

  it('leaves code out', () => {
    const segmenter = new AnswerSegmenter()
    expect(segmenter.push('Run this:\n```\nmake test')).toEqual(['Run this:'])
    expect(segmenter.push('\n```\nIt takes a minute.')).toEqual([])
    expect(segmenter.flush()).toEqual(['It takes a minute.'])
  })
})

describe('isLikelyEcho', () => {
  const spoken = 'Tomorrow you have a dentist appointment at nine, then lunch with the team at noon.'
  it('recognizes the answer heard back', () => {
    expect(isLikelyEcho('a dentist appointment at nine then lunch', spoken)).toBe(true)
    expect(isLikelyEcho('Lunch with the team.', spoken)).toBe(true)
  })
  it('lets the person through', () => {
    expect(isLikelyEcho('Wait, move the dentist to Friday.', spoken)).toBe(false)
    expect(isLikelyEcho('Stop.', spoken)).toBe(false)
    expect(isLikelyEcho('anything', '')).toBe(false)
  })
})

describe('heardSplit', () => {
  it('counts a piece cut off only up to its last whole word', () => {
    expect(
      heardSplit([
        { text: 'Sure.', playedFraction: 1 },
        { text: 'You have a dentist at nine.', playedFraction: 0.5 },
        { text: 'Then lunch.', playedFraction: 0 },
      ]),
    ).toEqual({ heardText: 'Sure. You have a', unheardText: 'dentist at nine. Then lunch.' })
  })
})

// A player that records what it is asked, for the policy above it.
function fakePlayer() {
  const enqueued: string[] = []
  let isActive = false
  const player = {
    enqueue: (texts: string[]) => {
      enqueued.push(...texts)
      if (texts.length > 0) isActive = true
    },
    isActive: () => isActive,
    wasRecentlyActive: () => isActive,
    recentText: () => enqueued.join(' '),
    duck: vi.fn(),
    pause: vi.fn(),
    resume: vi.fn(),
    interrupt: vi.fn((notSegmentedText: string): InterruptedAnswer => {
      isActive = false
      return { heardText: enqueued[0] ?? '', unheardText: [...enqueued.slice(1), notSegmentedText].join(' ').trim() }
    }),
    close: vi.fn(),
  }
  return { player, enqueued }
}

describe('AnswerVoice', () => {
  afterEach(() => vi.useRealTimers())

  it('speaks each turn once, whatever the feed repeats', () => {
    const { player, enqueued } = fakePlayer()
    const voice = new AnswerVoice(player as unknown as AnswerPlayer, { confirmationNeeded: 'Approve:' })
    voice.follow({ kind: 'text', runId: 'other', sequence: 1, text: 'Not followed.' })
    voice.follow({ kind: 'asked', runId: 'run', sequence: 1 })
    voice.follow({ kind: 'text', runId: 'run', sequence: 2, text: 'Sure. You have a dentist' })
    voice.follow({ kind: 'text', runId: 'run', sequence: 2, text: 'Sure. You have a dentist' })
    voice.follow({ kind: 'message', runId: 'run', sequence: 3, text: 'Sure. You have a dentist at nine.' })
    voice.follow({ kind: 'done', runId: 'run', sequence: 4 })
    expect(enqueued).toEqual(['Sure.', 'You have a dentist at nine.'])
  })

  it('speaks a message that came without deltas, and a question card', () => {
    const { player, enqueued } = fakePlayer()
    const voice = new AnswerVoice(player as unknown as AnswerPlayer, { confirmationNeeded: 'Approve:' })
    voice.adopt(['run'])
    voice.follow({ kind: 'message', runId: 'run', sequence: 7, text: 'Done.' })
    voice.follow({ kind: 'question', runId: 'run', sequence: 8, note: 'Which **one**?' })
    voice.follow({ kind: 'confirmation', runId: 'run', sequence: 9, note: 'send the reply' })
    expect(enqueued).toEqual(['Done.', 'Which one?', 'Approve: send the reply'])
  })

  it('drops the answer heard back, and goes on', () => {
    vi.useFakeTimers()
    const { player } = fakePlayer()
    const voice = new AnswerVoice(player as unknown as AnswerPlayer, { confirmationNeeded: '' })
    voice.follow({ kind: 'asked', runId: 'run', sequence: 1 })
    voice.follow({ kind: 'text', runId: 'run', sequence: 2, text: 'You have a dentist at nine tomorrow. ' })
    voice.speechStarted()
    expect(player.duck).toHaveBeenLastCalledWith(true)
    vi.advanceTimersByTime(700)
    expect(player.pause).toHaveBeenCalled()
    voice.speechStopped()
    expect(voice.transcript('a dentist at nine tomorrow')).toEqual({ isEcho: true })
    expect(player.resume).toHaveBeenCalled()
    expect(player.duck).toHaveBeenLastCalledWith(false)
    expect(player.interrupt).not.toHaveBeenCalled()
  })

  it('ends the answer when the person cuts in, and says how much they heard', () => {
    const { player, enqueued } = fakePlayer()
    const voice = new AnswerVoice(player as unknown as AnswerPlayer, { confirmationNeeded: '' })
    voice.follow({ kind: 'asked', runId: 'run', sequence: 1 })
    voice.follow({ kind: 'text', runId: 'run', sequence: 2, text: 'Sure. You have a dentist at nine and' })
    voice.speechStarted()
    const verdict = voice.transcript('Move it to Friday.')
    expect(verdict.isEcho).toBe(false)
    expect(verdict.interruptedAnswer).toEqual({ heardText: 'Sure.', unheardText: 'You have a dentist at nine and' })
    // The rest of the message being written then is not spoken.
    voice.follow({ kind: 'text', runId: 'run', sequence: 3, text: ' lunch at noon.' })
    voice.follow({ kind: 'message', runId: 'run', sequence: 4, text: 'Sure. You have a dentist at nine and lunch at noon.' })
    // The next message, after what they said was read, is.
    voice.follow({ kind: 'text', runId: 'run', sequence: 5, text: 'Moved to Friday.' })
    voice.follow({ kind: 'done', runId: 'run', sequence: 6 })
    expect(enqueued.slice(1)).toEqual(['Moved to Friday.'])
  })
})
