import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  AnswerPlayer,
  AnswerSegmenter,
  AnswerVoice,
  EchoGate,
  heardSplit,
  isLikelyEcho,
  speakableText,
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

  it('starts a long first sentence at its first comma', () => {
    const segmenter = new AnswerSegmenter()
    expect(segmenter.push('Tomorrow morning you have the dentist at nine, then')).toEqual([
      'Tomorrow morning you have the dentist at nine',
    ])
    // A short one waits for its end, and only the first is cut so.
    const short = new AnswerSegmenter()
    expect(short.push('Sure, at nine')).toEqual([])
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
  it('leaves a word or two to the loudness, even inside a longer word', () => {
    expect(isLikelyEcho('No', 'I know nothing about November.')).toBe(false)
    expect(isLikelyEcho('Wait', 'Wait for the next train.')).toBe(false)
  })
  it('matches a language written without spaces once it is long enough', () => {
    expect(isLikelyEcho('明日は歯医者', '明日は歯医者の予約があります。')).toBe(true)
    expect(isLikelyEcho('明日', '明日は歯医者の予約があります。')).toBe(false)
  })
  it('lets the person through', () => {
    expect(isLikelyEcho('Wait, move the dentist to Friday.', spoken)).toBe(false)
    expect(isLikelyEcho('Stop.', spoken)).toBe(false)
    expect(isLikelyEcho('anything', '')).toBe(false)
  })
})

describe('EchoGate', () => {
  it('learns how much of the answer reaches the microphone', () => {
    const gate = new EchoGate()
    for (let index = 0; index < 20; index++) gate.learn(0.04, 0.1)
    // The echo again, a little louder: not the person.
    expect(gate.isPersonLouder(0.06, 0.1)).toBe(false)
    // Somebody close to the microphone.
    expect(gate.isPersonLouder(0.3, 0.1)).toBe(true)
  })
  it('wants speech well above the answer before it has heard the echo', () => {
    const gate = new EchoGate()
    expect(gate.isPersonLouder(0.12, 0.1)).toBe(false)
    expect(gate.isPersonLouder(0.3, 0.1)).toBe(true)
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

describe('AnswerPlayer', () => {
  afterEach(() => vi.useRealTimers())

  it('fades an answer in from its first sound, not from when it was asked for', () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'performance'] })
    const { context, gainCalls } = fakeContext()
    const requested: string[] = []
    const player = new AnswerPlayer(
      context as unknown as AudioContext,
      { request: (answerSegmentId) => requested.push(answerSegmentId), cancel: () => undefined },
      () => undefined,
    )
    player.enqueue(['Sure.'])
    expect(gainCalls).toEqual([])
    expect(player.isStarting()).toBe(true)
    // The audio comes a while later: the fade starts with it.
    vi.advanceTimersByTime(800)
    context.currentTime = 0.8
    player.accept(requested[0], new Int16Array(2400))
    expect(gainCalls.find((call) => call.method === 'setValueAtTime')).toMatchObject({ gain: 0.35 })
    expect(gainCalls.find((call) => call.method === 'setValueAtTime')!.time).toBeGreaterThanOrEqual(0.8)
    expect(gainCalls.find((call) => call.method === 'linearRampToValueAtTime')).toMatchObject({ gain: 1 })
    expect(player.isStarting()).toBe(true)
    vi.advanceTimersByTime(1600)
    expect(player.isStarting()).toBe(false)
    player.close()
  })

  it('fades in only as far as ducked', () => {
    const { context, gainCalls } = fakeContext()
    const requested: string[] = []
    const player = new AnswerPlayer(
      context as unknown as AudioContext,
      { request: (answerSegmentId) => requested.push(answerSegmentId), cancel: () => undefined },
      () => undefined,
    )
    player.duck(true)
    player.enqueue(['Sure.'])
    player.accept(requested[0], new Int16Array(2400))
    const ramp = gainCalls.find((call) => call.method === 'linearRampToValueAtTime')
    expect(ramp!.gain).toBeLessThan(0.35)
    player.close()
  })
})

// An audio context that records what is done to the answer's gain.
function fakeContext() {
  const gainCalls: { method: string; gain: number; time: number }[] = []
  const record = (method: string) => (gain: number, time: number) => gainCalls.push({ method, gain, time })
  const context = {
    currentTime: 0,
    destination: {},
    createGain: () => ({
      connect: () => undefined,
      gain: {
        cancelScheduledValues: () => undefined,
        setTargetAtTime: () => undefined,
        setValueAtTime: record('setValueAtTime'),
        linearRampToValueAtTime: record('linearRampToValueAtTime'),
      },
    }),
    createAnalyser: () => ({ fftSize: 0, connect: () => undefined, getFloatTimeDomainData: () => undefined }),
    createBuffer: (_channelCount: number, sampleCount: number) => ({
      getChannelData: () => new Float32Array(sampleCount),
    }),
    createBufferSource: () => ({
      buffer: null,
      onended: null,
      connect: () => undefined,
      start: () => undefined,
      stop: () => undefined,
    }),
  }
  return { context, gainCalls }
}

// A player that records what it is asked, for the policy above it.
function fakePlayer() {
  const enqueued: string[] = []
  let isActive = false
  let soundingSince = 0
  const player = {
    enqueue: (texts: string[]) => {
      enqueued.push(...texts)
      if (texts.length > 0 && !isActive) soundingSince = performance.now()
      if (texts.length > 0) isActive = true
    },
    isActive: () => isActive,
    isStarting: () => performance.now() - soundingSince < 1500,
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

  it('drops the answer heard back by its words, and goes on', () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'performance'] })
    const { player } = fakePlayer()
    const levels = { microphoneRms: 0.01, answerRms: 0.1 }
    const voice = new AnswerVoice(player as unknown as AnswerPlayer, { confirmationNeeded: '' }, () => levels)
    voice.follow({ kind: 'asked', runId: 'run', sequence: 1 })
    voice.follow({ kind: 'text', runId: 'run', sequence: 2, text: 'You have a dentist at nine tomorrow. ' })
    voice.answering(true)
    // Past the answer's start, with its quiet echo learned.
    vi.advanceTimersByTime(1600)
    levels.microphoneRms = 0.6
    voice.speechStarted('u1')
    vi.advanceTimersByTime(700)
    // Loud enough to be taken for the person: quietened and paused.
    expect(player.duck).toHaveBeenLastCalledWith(true)
    expect(player.pause).toHaveBeenCalled()
    voice.speechStopped()
    expect(voice.transcript('a dentist at nine tomorrow', 'u1')).toEqual({ isEcho: true })
    expect(player.resume).toHaveBeenCalled()
    expect(player.duck).toHaveBeenLastCalledWith(false)
    expect(player.interrupt).not.toHaveBeenCalled()
  })

  it('wants the person louder still while the answer is starting', () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'performance'] })
    const { player } = fakePlayer()
    const levels = { microphoneRms: 0.3, answerRms: 0.1 }
    const heard: string[] = []
    const voice = new AnswerVoice(
      player as unknown as AnswerPlayer,
      { confirmationNeeded: '' },
      () => levels,
      () => heard.push('person'),
    )
    voice.follow({ kind: 'asked', runId: 'run', sequence: 1 })
    voice.follow({ kind: 'text', runId: 'run', sequence: 2, text: 'You have a dentist at nine tomorrow. ' })
    voice.answering(true)
    // Three times the answer, the moment it starts: its unlearned echo.
    voice.speechStarted('u1')
    expect(voice.isEchoUtterance('u1')).toBe(true)
    vi.advanceTimersByTime(700)
    voice.speechStopped()
    expect(player.duck).not.toHaveBeenCalled()
    expect(heard).toEqual([])
    expect(voice.isEchoUtterance('u1')).toBe(true)
    // The same, later in the answer, over a quiet echo: the person.
    levels.microphoneRms = 0.01
    vi.advanceTimersByTime(1000)
    levels.microphoneRms = 0.3
    voice.speechStarted('u2')
    vi.advanceTimersByTime(500)
    expect(heard).toEqual(['person'])
    expect(voice.isEchoUtterance('u2')).toBe(false)
    expect(player.duck).toHaveBeenLastCalledWith(true)
  })

  it('drops speech no louder than the echo without even pausing', () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'performance'] })
    const { player } = fakePlayer()
    const levels = { microphoneRms: 0.04, answerRms: 0.1 }
    const voice = new AnswerVoice(player as unknown as AnswerPlayer, { confirmationNeeded: '' }, () => levels)
    voice.follow({ kind: 'asked', runId: 'run', sequence: 1 })
    voice.follow({ kind: 'text', runId: 'run', sequence: 2, text: 'You have a dentist at nine tomorrow. ' })
    voice.answering(true)
    // Only the answer sounding: the echo is learned.
    vi.advanceTimersByTime(1000)
    levels.microphoneRms = 0.06
    voice.speechStarted('u1')
    vi.advanceTimersByTime(700)
    expect(player.duck).not.toHaveBeenCalled()
    expect(player.pause).not.toHaveBeenCalled()
    voice.speechStopped()
    // Whatever the words came out as, it was the echo.
    expect(voice.transcript('Hey, a tennis tomorrow', 'u1')).toEqual({ isEcho: true })
    expect(player.interrupt).not.toHaveBeenCalled()
  })

  it('cuts the answer at a tap, and tells the next words how much was heard', () => {
    const { player, enqueued } = fakePlayer()
    const voice = new AnswerVoice(player as unknown as AnswerPlayer, { confirmationNeeded: '' })
    voice.follow({ kind: 'asked', runId: 'run', sequence: 1 })
    voice.follow({ kind: 'text', runId: 'run', sequence: 2, text: 'Sure. You have a dentist at nine and' })
    expect(enqueued).toEqual(['Sure.'])
    voice.cutByTap()
    expect(player.interrupt).toHaveBeenCalled()
    expect(voice.transcript('Move it to Friday.', 'u2')).toEqual({
      isEcho: false,
      interruptedAnswer: { heardText: 'Sure.', unheardText: 'You have a dentist at nine and' },
    })
    // Said once: the words after those go on their own.
    expect(voice.transcript('And Saturday.', 'u3')).toEqual({ isEcho: false, interruptedAnswer: undefined })
  })

  it('speaks an adopted turn from its latest event, not from a replay', () => {
    const { player, enqueued } = fakePlayer()
    const voice = new AnswerVoice(player as unknown as AnswerPlayer, { confirmationNeeded: '' })
    voice.follow({ kind: 'text', runId: 'run', sequence: 5, text: 'Already shown.' })
    voice.adopt(['run'])
    voice.follow({ kind: 'text', runId: 'run', sequence: 5, text: 'Already shown.' })
    voice.follow({ kind: 'message', runId: 'run', sequence: 6, text: 'Next part.' })
    expect(enqueued).toEqual(['Next part.'])
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
    voice.follow({
      kind: 'message',
      runId: 'run',
      sequence: 4,
      text: 'Sure. You have a dentist at nine and lunch at noon.',
    })
    // The next message, after what they said was read, is.
    voice.follow({ kind: 'text', runId: 'run', sequence: 5, text: 'Moved to Friday.' })
    voice.follow({ kind: 'done', runId: 'run', sequence: 6 })
    expect(enqueued.slice(1)).toEqual(['Moved to Friday.'])
  })
})
