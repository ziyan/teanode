import { beforeEach, expect, it, vi } from 'vitest'
import { LiveTranscriptionStream, liveRetryDelayMS, liveSocketAddress, type LiveStreamCallbacks } from './liveStream'

class FakeSocket {
  static sockets: FakeSocket[] = []
  readyState: number = WebSocket.OPEN
  bufferedAmount = 0
  binaryType = ''
  sent: (string | ArrayBuffer)[] = []
  onopen: (() => void) | null = null
  onmessage: ((message: { data: string }) => void) | null = null
  onerror: (() => void) | null = null
  onclose: (() => void) | null = null
  constructor(readonly address: string) {
    FakeSocket.sockets.push(this)
  }
  send(data: string | ArrayBuffer) {
    this.sent.push(data)
  }
  close() {
    this.readyState = WebSocket.CLOSED
  }
  say(event: Record<string, unknown>) {
    this.onmessage?.({ data: JSON.stringify(event) })
  }
}

const callbacks = (): LiveStreamCallbacks => ({
  onReady: vi.fn(),
  onClosed: vi.fn(),
  onWords: vi.fn(),
  onUtteranceEnd: vi.fn(),
})
const frame = (byte: number) => new Uint8Array([byte]).buffer
const open = (streamStartMS: number, handlers = callbacks()) => {
  const stream = new LiveTranscriptionStream(
    'fixture-recording',
    streamStartMS,
    handlers,
    (address) => new FakeSocket(address) as unknown as WebSocket,
  )
  stream.open()
  const socket = FakeSocket.sockets[FakeSocket.sockets.length - 1]
  socket.onopen?.()
  return { stream, socket, handlers }
}
beforeEach(() => {
  FakeSocket.sockets = []
})

it('addresses the socket of the recording on this server', () => {
  expect(liveSocketAddress({ protocol: 'https:', host: 'example.net' }, 'fixture-recording')).toBe(
    'wss://example.net/api/v1/recordings/fixture-recording/live',
  )
  expect(liveSocketAddress({ protocol: 'http:', host: 'localhost:3000' }, 'a b')).toBe(
    'ws://localhost:3000/api/v1/recordings/a%20b/live',
  )
})

it('says where on the timeline the stream starts, then holds audio until the server is ready', () => {
  const { stream, socket, handlers } = open(61_234.6)
  expect(JSON.parse(socket.sent[0] as string)).toMatchObject({ voiceEvent: 'hello', streamStartMS: 61235 })
  stream.append(frame(1))
  stream.append(frame(2))
  expect(socket.sent).toHaveLength(1)
  socket.say({ voiceEvent: 'ready' })
  expect(socket.sent.slice(1)).toEqual([frame(1), frame(2)])
  stream.append(frame(3))
  expect(socket.sent).toHaveLength(4)
  expect(handlers.onReady).toHaveBeenCalledTimes(1)
})

it('reports where an utterance ended and the words as they are heard', () => {
  const { socket, handlers } = open(0)
  socket.say({ voiceEvent: 'speechStopped', utteranceEndMS: 24_500 })
  socket.say({ voiceEvent: 'transcriptDelta', utteranceId: 'u1', transcriptText: 'hello ' })
  socket.say({ voiceEvent: 'transcriptDelta', utteranceId: 'u1', transcriptText: 'there' })
  socket.say({ voiceEvent: 'transcriptFinal', utteranceId: 'u1', transcriptText: 'Hello there.' })
  expect(handlers.onUtteranceEnd).toHaveBeenCalledWith(24_500)
  expect(vi.mocked(handlers.onWords).mock.calls).toEqual([
    ['u1', 'hello ', false],
    ['u1', 'hello there', false],
    ['u1', 'Hello there.', true],
  ])
})

it('closes, once, when the server refuses or the connection is too slow, never continuing on a gap', () => {
  const refused = open(0)
  refused.socket.say({ voiceEvent: 'refused', errorMessage: 'live transcription is off' })
  expect(refused.handlers.onClosed).toHaveBeenCalledWith('live transcription is off', true)
  refused.socket.onclose?.()
  expect(refused.handlers.onClosed).toHaveBeenCalledTimes(1)

  const slow = open(0)
  slow.socket.say({ voiceEvent: 'ready' })
  slow.socket.bufferedAmount = 2 << 20
  slow.stream.append(frame(1))
  expect(slow.handlers.onClosed).toHaveBeenCalledWith('the connection is too slow', false)
  slow.stream.append(frame(2))
  expect(slow.socket.sent.filter((sent) => typeof sent !== 'string')).toEqual([])
})

it('tells the server to stop when closed by the caller, without calling back', () => {
  const { stream, socket, handlers } = open(0)
  socket.say({ voiceEvent: 'ready' })
  stream.close()
  expect(JSON.parse(socket.sent[socket.sent.length - 1] as string)).toEqual({ voiceEvent: 'stop' })
  expect(handlers.onClosed).not.toHaveBeenCalled()
})

it('stops asking after a refusal and keeps asking, slowly, after a drop', () => {
  const refused = open(0)
  refused.socket.say({ voiceEvent: 'refused', errorMessage: 'that recording is not being captured' })
  expect(refused.handlers.onClosed).toHaveBeenCalledWith('that recording is not being captured', true)
  const dropped = open(0)
  dropped.socket.onclose?.()
  expect(dropped.handlers.onClosed).toHaveBeenCalledWith('the connection closed', false)
  const ended = open(0)
  ended.socket.say({ voiceEvent: 'error', errorMessage: 'the session reached its end; stream again' })
  expect(ended.handlers.onClosed).toHaveBeenCalledWith('the session reached its end; stream again', false)

  expect(liveRetryDelayMS(0, true)).toBeNull()
  expect(liveRetryDelayMS(7, true)).toBeNull()
  expect([0, 1, 2, 10].map((retryCount) => liveRetryDelayMS(retryCount, false))).toEqual([1_000, 2_000, 4_000, 30_000])
})
