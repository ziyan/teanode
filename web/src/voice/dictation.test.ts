import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { Dictation, LAST_WORDS_WAIT_MS, SILENCE_STOPS_AFTER_MS, type DictationCallbacks } from './dictation'

class FakeSocket {
  static OPEN = 1
  static instances: FakeSocket[] = []
  readyState = 1
  binaryType = ''
  bufferedAmount = 0
  sent: unknown[] = []
  onopen: (() => void) | null = null
  onmessage: ((message: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  constructor(public address: string) {
    FakeSocket.instances.push(this)
  }
  send(data: unknown) {
    this.sent.push(data)
  }
  close() {
    this.readyState = 3
  }
  say(event: object) {
    this.onmessage?.({ data: JSON.stringify(event) })
  }
}

class FakeAudioContext {
  state = 'running'
  destination = {}
  audioWorklet = { addModule: vi.fn(async () => undefined) }
  resume = async () => undefined
  close = async () => undefined
  createMediaStreamSource = () => ({ connect: vi.fn() })
  createGain = () => ({ gain: { value: 1 }, connect: vi.fn() })
}

class FakeWorkletNode {
  port: { onmessage: ((event: { data: ArrayBuffer }) => void) | null } = { onmessage: null }
  static last: FakeWorkletNode
  constructor() {
    FakeWorkletNode.last = this
  }
  connect = vi.fn()
}

const track = { stop: vi.fn(), enabled: true, getSettings: () => ({ echoCancellation: true }) }

function callbacks() {
  const heard = {
    listening: vi.fn(),
    captions: [] as string[],
    words: [] as string[],
    problems: [] as string[],
    ended: vi.fn(),
  }
  const given: DictationCallbacks = {
    onListening: heard.listening,
    onCaption: (text) => heard.captions.push(text),
    onWords: (words) => heard.words.push(words),
    onProblem: (text) => heard.problems.push(text),
    onEnded: heard.ended,
  }
  return { heard, given }
}

const messages = { microphoneRefused: 'The microphone could not be used.', connectionLost: 'Connection lost.' }

beforeEach(() => {
  vi.useFakeTimers()
  FakeSocket.instances = []
  track.stop.mockReset()
  track.enabled = true
  vi.stubGlobal('WebSocket', FakeSocket)
  vi.stubGlobal('AudioContext', FakeAudioContext)
  vi.stubGlobal('AudioWorkletNode', FakeWorkletNode)
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: { getUserMedia: vi.fn(async () => ({ getTracks: () => [track], getAudioTracks: () => [track] })) },
  })
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

async function started(place: 'agent' | 'chat' | 'journal' = 'journal') {
  const { heard, given } = callbacks()
  const dictation = new Dictation(given, messages)
  await dictation.start(24000, place)
  const socket = FakeSocket.instances[0]
  socket.onopen?.()
  socket.say({ voiceEvent: 'ready' })
  return { dictation, socket, heard }
}

it('says in its hello that it is dictation, and where the words are going', async () => {
  const { socket, heard } = await started('journal')
  const hello = JSON.parse(String(socket.sent[0]))
  expect(hello).toMatchObject({ voiceEvent: 'hello', purpose: 'dictation', dictationPlace: 'journal' })
  expect(socket.address).toContain('/api/v1/agent/voice')
  expect(heard.listening).toHaveBeenCalledTimes(1)
})

it('streams the microphone only once the server is ready', async () => {
  const { dictation, socket } = await started()
  void dictation
  const frame = new ArrayBuffer(4)
  FakeWorkletNode.last.port.onmessage?.({ data: frame })
  expect(socket.sent[socket.sent.length - 1]).toBe(frame)
})

it('hands over each finished utterance once, with the words being heard before it', async () => {
  const { socket, heard } = await started()
  socket.say({ voiceEvent: 'speechStarted', utteranceId: 'u1' })
  socket.say({ voiceEvent: 'transcriptDelta', utteranceId: 'u1', transcriptText: 'water the' })
  socket.say({ voiceEvent: 'transcriptFinal', utteranceId: 'u1', transcriptText: ' water the plants ' })
  socket.say({ voiceEvent: 'transcriptFinal', utteranceId: 'u1', transcriptText: ' water the plants ' })
  expect(heard.words).toEqual(['water the plants'])
  expect(heard.captions).toContain('water the')
})

it('waits for the words being spoken when stopped, then ends', async () => {
  const { dictation, socket, heard } = await started()
  socket.say({ voiceEvent: 'speechStarted', utteranceId: 'u1' })
  dictation.stop()
  expect(heard.ended).not.toHaveBeenCalled()
  // The microphone is silenced so that the server hears the utterance end.
  expect(track.enabled).toBe(false)
  socket.say({ voiceEvent: 'transcriptFinal', utteranceId: 'u1', transcriptText: 'last words' })
  expect(heard.words).toEqual(['last words'])
  expect(heard.ended).toHaveBeenCalledTimes(1)
  expect(socket.sent.some((sent) => typeof sent === 'string' && sent.includes('"stop"'))).toBe(true)
})

it('gives up on the words being spoken after a few seconds', async () => {
  const { dictation, socket, heard } = await started()
  socket.say({ voiceEvent: 'speechStarted', utteranceId: 'u1' })
  dictation.stop()
  vi.advanceTimersByTime(LAST_WORDS_WAIT_MS + 10)
  expect(heard.ended).toHaveBeenCalledTimes(1)
})

it('ends at once when aborted, dropping the words not yet in', async () => {
  const { dictation, socket, heard } = await started()
  socket.say({ voiceEvent: 'speechStarted', utteranceId: 'u1' })
  dictation.abort()
  socket.say({ voiceEvent: 'transcriptFinal', utteranceId: 'u1', transcriptText: 'too late' })
  expect(heard.words).toEqual([])
  expect(heard.ended).toHaveBeenCalledTimes(1)
  expect(track.stop).toHaveBeenCalled()
})

it('stops itself after thirty seconds without speech', async () => {
  const { socket, heard } = await started()
  // Speech keeps it going.
  vi.advanceTimersByTime(SILENCE_STOPS_AFTER_MS - 5000)
  socket.say({ voiceEvent: 'speechStarted', utteranceId: 'u1' })
  socket.say({ voiceEvent: 'transcriptFinal', utteranceId: 'u1', transcriptText: 'hello' })
  vi.advanceTimersByTime(SILENCE_STOPS_AFTER_MS - 5000)
  expect(heard.ended).not.toHaveBeenCalled()
  vi.advanceTimersByTime(10_000)
  expect(heard.ended).toHaveBeenCalledTimes(1)
})

it('says why when the server refuses', async () => {
  const { socket, heard } = await started()
  socket.say({ voiceEvent: 'refused', errorMessage: 'voice is off' })
  expect(heard.problems).toEqual(['voice is off'])
  expect(heard.ended).toHaveBeenCalledTimes(1)
})

it('says so when the microphone is refused', async () => {
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: {
      getUserMedia: vi.fn(async () => {
        throw new Error('denied')
      }),
    },
  })
  const { heard, given } = callbacks()
  await new Dictation(given, messages).start(24000, 'agent')
  expect(heard.problems).toEqual(['The microphone could not be used. denied'])
  expect(heard.ended).toHaveBeenCalledTimes(1)
})
