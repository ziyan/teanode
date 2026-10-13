import { authorization } from '../api'

// The microphone, streamed to the server for words while a recording goes on.
// The server streams it on to the provider's realtime transcription and tells
// this socket what was heard, with times on the recording's timeline. None of
// it is the recording: the saved pieces are made from the same audio apart
// from this, so a stream that fails costs words on screen and nothing else.

export const LIVE_SAMPLE_RATE = 24_000

// Frames of audio waiting for the server to be ready, or for the connection to
// take them. A tenth of a second each, so this is about five seconds.
const MAXIMUM_WAITING_FRAMES = 50
const MAXIMUM_BUFFERED_BYTES = 1 << 20

const LIVE_RETRY_MINIMUM_MS = 1_000
const LIVE_RETRY_MAXIMUM_MS = 30_000

// liveRetryDelayMS is how long to wait before opening the stream again after it
// closed for the retryCount-th time in a row, or null when it should not be
// opened again: the server refused it. A network drop is tried again at growing
// delays, forever, because words are a convenience and the recording goes on.
export function liveRetryDelayMS(retryCount: number, isRefused: boolean): number | null {
  if (isRefused) return null
  return Math.min(LIVE_RETRY_MAXIMUM_MS, LIVE_RETRY_MINIMUM_MS * 2 ** retryCount)
}

export function liveSocketAddress(location: Pick<Location, 'protocol' | 'host'>, recordingId: string): string {
  return `${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/api/v1/recordings/${encodeURIComponent(recordingId)}/live`
}

export interface LiveEvent {
  voiceEvent: string
  utteranceId?: string
  transcriptText?: string
  utteranceEndMS?: number
  errorMessage?: string
}

export interface LiveStreamCallbacks {
  onReady: () => void
  // onClosed is told once, with why, when the stream ends for any reason but being closed by the caller.
  // isRefused says the server answered that it will not take this recording's stream (live words
  // are off, or the recording is not being captured), which asking again will not change.
  onClosed: (reason: string, isRefused: boolean) => void
  // onWords is told the words of an utterance as they are heard (isFinal false) and when they are settled.
  onWords: (utteranceId: string, transcriptText: string, isFinal: boolean) => void
  // onUtteranceEnd is told where on the timeline the provider decided an utterance ended.
  onUtteranceEnd: (utteranceEndMS: number) => void
}

// LiveTranscriptionStream is one socket. Its first frame of audio is the
// sample at streamStartMS on the recording's timeline, because the server
// places what is heard by counting the audio it was given; a stream that
// loses a frame is closed and a new one opened at the right time, never
// continued.
export class LiveTranscriptionStream {
  private socket?: WebSocket
  private isReady = false
  private isClosed = false
  private waitingFrames: ArrayBuffer[] = []
  private partialByUtterance = new Map<string, string>()

  constructor(
    private recordingId: string,
    private streamStartMS: number,
    private callbacks: LiveStreamCallbacks,
    private createSocket: (address: string) => WebSocket = (address) => new WebSocket(address),
  ) {}

  open() {
    const socket = this.createSocket(liveSocketAddress(window.location, this.recordingId))
    socket.binaryType = 'arraybuffer'
    this.socket = socket
    socket.onopen = () => {
      socket.send(
        JSON.stringify({
          voiceEvent: 'hello',
          authorization: authorization().Authorization,
          streamStartMS: Math.round(this.streamStartMS),
        }),
      )
    }
    socket.onmessage = (message) => {
      let event: LiveEvent
      try {
        event = JSON.parse(String(message.data)) as LiveEvent
      } catch {
        return
      }
      this.accept(event)
    }
    // A browser cannot read the status of a refused upgrade, so a refusal before the socket
    // opened looks like a dropped connection and is asked again, slowly.
    socket.onerror = () => this.fail('the connection failed', false)
    socket.onclose = () => this.fail('the connection closed', false)
  }

  private accept(event: LiveEvent) {
    switch (event.voiceEvent) {
      case 'ready':
        this.isReady = true
        for (const frame of this.waitingFrames) this.socket?.send(frame)
        this.waitingFrames = []
        this.callbacks.onReady()
        return
      case 'refused':
        this.fail(event.errorMessage ?? 'the transcription was refused', true)
        return
      case 'error':
        this.fail(event.errorMessage ?? 'the transcription stopped', false)
        return
      case 'speechStopped':
        if (event.utteranceEndMS) this.callbacks.onUtteranceEnd(event.utteranceEndMS)
        return
      case 'transcriptDelta': {
        if (!event.utteranceId) return
        const transcriptText = (this.partialByUtterance.get(event.utteranceId) ?? '') + (event.transcriptText ?? '')
        this.partialByUtterance.set(event.utteranceId, transcriptText)
        this.callbacks.onWords(event.utteranceId, transcriptText, false)
        return
      }
      case 'transcriptFinal':
        if (!event.utteranceId) return
        this.partialByUtterance.delete(event.utteranceId)
        if (event.transcriptText) this.callbacks.onWords(event.utteranceId, event.transcriptText, true)
        return
    }
  }

  // append gives the stream one frame of 24 kHz mono 16-bit audio.
  append(frame: ArrayBuffer) {
    if (this.isClosed || !this.socket) return
    if (!this.isReady || this.socket.readyState !== WebSocket.OPEN) {
      if (this.waitingFrames.length >= MAXIMUM_WAITING_FRAMES) return this.fail('the connection is too slow', false)
      this.waitingFrames.push(frame)
      return
    }
    if (this.socket.bufferedAmount > MAXIMUM_BUFFERED_BYTES) return this.fail('the connection is too slow', false)
    this.socket.send(frame)
  }

  private fail(reason: string, isRefused: boolean) {
    if (this.isClosed) return
    this.close()
    this.callbacks.onClosed(reason, isRefused)
  }

  // close ends the stream without telling the callbacks.
  close() {
    if (this.isClosed) return
    this.isClosed = true
    const socket = this.socket
    if (!socket) return
    socket.onclose = null
    socket.onerror = null
    socket.onmessage = null
    try {
      if (socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ voiceEvent: 'stop' }))
      socket.close()
    } catch {
      /* A socket that is already gone has nothing to close. */
    }
  }
}
