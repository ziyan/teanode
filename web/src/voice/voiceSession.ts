import { authorization } from '../api'

// Voice: the person talking to their agent in the drawer. The microphone
// streams to the server over a websocket as they speak; the server has it
// transcribed and says back what it heard (internal/voice). Each finished
// utterance is handed to the drawer once, in the order spoken, and the
// drawer sends it the way it sends what was typed.

// VoiceEvent is one thing the server says on the voice socket.
export type VoiceEvent = {
  voiceEvent: string
  utteranceId?: string
  utteranceSequence?: number
  transcriptText?: string
  errorMessage?: string
  sampleRate?: number
}

// VoiceCallbacks are what a session tells the drawer.
export type VoiceCallbacks = {
  // Listening: the server and the provider are ready for speech.
  onListening: () => void
  // The words of the utterance being heard, so far; empty clears them.
  onCaption: (captionText: string) => void
  // A finished utterance: once each, in the order spoken.
  onTranscript: (transcriptText: string) => void
  // Something the person should be told; isEnding says listening stopped.
  onProblem: (problemText: string, isEnding: boolean) => void
  // Listening stopped, for whatever reason.
  onEnded: () => void
}

// TranscriptTracker keeps the caption of each utterance being heard and
// lets each finished one through once, whatever the socket repeats.
export class TranscriptTracker {
  private captions = new Map<string, string>()
  private told = new Set<string>()
  private currentId = ''

  // accept reads one event and says what changed: the caption to show
  // now, a finished transcript to send, or a failed utterance.
  accept(event: VoiceEvent): { captionText?: string; transcriptText?: string; isNotHeard?: boolean } {
    const utteranceId = event.utteranceId ?? ''
    switch (event.voiceEvent) {
      case 'speechStarted':
        this.currentId = utteranceId
        if (!this.captions.has(utteranceId)) this.captions.set(utteranceId, '')
        return { captionText: this.captions.get(utteranceId) ?? '' }
      case 'transcriptDelta': {
        if (this.told.has(utteranceId)) return {}
        const captionText = (this.captions.get(utteranceId) ?? '') + (event.transcriptText ?? '')
        this.captions.set(utteranceId, captionText)
        return { captionText }
      }
      case 'transcriptFinal':
      case 'transcriptFailed': {
        if (!utteranceId || this.told.has(utteranceId)) return {}
        this.told.add(utteranceId)
        this.captions.delete(utteranceId)
        const next = this.currentId === utteranceId ? '' : (this.captions.get(this.currentId) ?? '')
        if (event.voiceEvent === 'transcriptFailed') return { captionText: next, isNotHeard: true }
        const transcriptText = (event.transcriptText ?? '').trim()
        return transcriptText ? { captionText: next, transcriptText } : { captionText: next }
      }
    }
    return {}
  }
}

// voiceSocketAddress is the voice websocket on this server.
export function voiceSocketAddress(location: Location): string {
  return `${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/api/v1/agent/voice`
}

// MAXIMUM_BUFFERED_BYTES is how much audio may wait to be sent before
// frames are dropped rather than queued: a connection that slow is better
// told about than heard seconds late.
const MAXIMUM_BUFFERED_BYTES = 1 << 20

// VoiceSession is one stretch of listening, from the button pressed to the
// button pressed again.
export class VoiceSession {
  private socket?: WebSocket
  private stream?: MediaStream
  private context?: AudioContext
  private isEnded = false
  private isReady = false
  private tracker = new TranscriptTracker()

  constructor(
    private callbacks: VoiceCallbacks,
    private messages: { microphoneRefused: string; notHeard: string; connectionLost: string },
  ) {}

  async start(sampleRate: number) {
    try {
      this.stream = await navigator.mediaDevices.getUserMedia({
        audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true, autoGainControl: true },
      })
    } catch (caught) {
      this.end(`${this.messages.microphoneRefused} ${caught instanceof Error ? caught.message : String(caught)}`)
      return
    }
    if (this.isEnded) return this.release()
    this.context = new AudioContext()
    await this.context.audioWorklet.addModule('/assets/voice-capture-worklet.js')
    if (this.isEnded) return this.release()
    const source = this.context.createMediaStreamSource(this.stream)
    // No outputs: it only listens, and plays nothing back.
    const capture = new AudioWorkletNode(this.context, 'voice-capture', {
      numberOfInputs: 1,
      numberOfOutputs: 0,
      processorOptions: { targetRate: sampleRate },
    })
    source.connect(capture)

    const socket = new WebSocket(voiceSocketAddress(window.location))
    socket.binaryType = 'arraybuffer'
    this.socket = socket
    socket.onopen = () => {
      socket.send(JSON.stringify({ voiceEvent: 'hello', authorization: authorization().Authorization }))
    }
    capture.port.onmessage = (event: MessageEvent<ArrayBuffer>) => {
      // Before the server is ready there is nobody to hear it.
      if (!this.isReady || socket.readyState !== WebSocket.OPEN) return
      if (socket.bufferedAmount > MAXIMUM_BUFFERED_BYTES) return
      socket.send(event.data)
    }
    socket.onmessage = (message) => {
      let event: VoiceEvent
      try {
        event = JSON.parse(String(message.data)) as VoiceEvent
      } catch {
        return
      }
      switch (event.voiceEvent) {
        case 'ready':
          this.isReady = true
          this.callbacks.onListening()
          return
        case 'refused':
        case 'error':
          this.end(event.errorMessage ?? this.messages.connectionLost)
          return
      }
      const changed = this.tracker.accept(event)
      if (changed.captionText !== undefined) this.callbacks.onCaption(changed.captionText)
      if (changed.transcriptText) this.callbacks.onTranscript(changed.transcriptText)
      if (changed.isNotHeard) this.callbacks.onProblem(this.messages.notHeard, false)
    }
    socket.onclose = () => this.end(this.isEnded ? undefined : this.messages.connectionLost)
  }

  // stop ends listening at the person's word.
  stop() {
    if (this.socket?.readyState === WebSocket.OPEN) {
      this.socket.send(JSON.stringify({ voiceEvent: 'stop' }))
    }
    this.end()
  }

  private end(problemText?: string) {
    if (this.isEnded) return
    this.isEnded = true
    this.release()
    if (problemText) this.callbacks.onProblem(problemText, true)
    this.callbacks.onCaption('')
    this.callbacks.onEnded()
  }

  private release() {
    this.socket?.close()
    this.stream?.getTracks().forEach((track) => track.stop())
    void this.context?.close().catch(() => undefined)
  }
}
