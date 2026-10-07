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
  // The provider hears somebody talking, or no longer does.
  onHearing: (isHearing: boolean) => void
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
  private analyser?: AnalyserNode
  private samples?: Float32Array<ArrayBuffer>
  private isEnded = false
  private isReady = false
  private tracker = new TranscriptTracker()

  constructor(
    private callbacks: VoiceCallbacks,
    private messages: { microphoneRefused: string; notHeard: string; connectionLost: string },
  ) {}

  // start begins listening. It is called from the person's tap: Safari on a
  // phone starts an audio context only within the gesture that asked for
  // it, so the context is made and resumed before anything is waited on.
  async start(sampleRate: number) {
    const context = new AudioContext()
    this.context = context
    void context.resume().catch(() => undefined)
    try {
      this.stream = await navigator.mediaDevices.getUserMedia({
        audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true, autoGainControl: true },
      })
    } catch (caught) {
      this.end(`${this.messages.microphoneRefused} ${caught instanceof Error ? caught.message : String(caught)}`)
      return
    }
    if (this.isEnded) return this.release()
    await context.audioWorklet.addModule('/assets/voice-capture-worklet.js')
    if (this.isEnded) return this.release()
    if (context.state !== 'running') await context.resume().catch(() => undefined)
    const source = context.createMediaStreamSource(this.stream)
    // How loud the microphone is, for the drawer to draw.
    this.analyser = context.createAnalyser()
    this.analyser.fftSize = 512
    this.samples = new Float32Array(this.analyser.fftSize)
    const capture = new AudioWorkletNode(context, 'voice-capture', {
      numberOfInputs: 1,
      numberOfOutputs: 1,
      processorOptions: { targetRate: sampleRate },
    })
    // WebKit renders only what reaches the speakers, so the capture is
    // connected to them through a gain of nothing: it runs everywhere and
    // plays nothing back.
    const silent = context.createGain()
    silent.gain.value = 0
    source.connect(this.analyser)
    this.analyser.connect(capture)
    capture.connect(silent)
    silent.connect(context.destination)

    const socket = new WebSocket(voiceSocketAddress(window.location))
    socket.binaryType = 'arraybuffer'
    this.socket = socket
    // What the browser granted, which is not always what was asked for:
    // without echo cancellation a spoken answer would be heard back.
    const granted = this.stream.getAudioTracks()[0]?.getSettings() ?? {}
    socket.onopen = () => {
      socket.send(
        JSON.stringify({
          voiceEvent: 'hello',
          authorization: authorization().Authorization,
          captureSettings: {
            echoCancellation: granted.echoCancellation,
            noiseSuppression: granted.noiseSuppression,
            autoGainControl: granted.autoGainControl,
            sampleRate: granted.sampleRate,
          },
        }),
      )
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
      if (event.voiceEvent === 'speechStarted') this.callbacks.onHearing(true)
      if (event.voiceEvent === 'speechStopped') this.callbacks.onHearing(false)
      const changed = this.tracker.accept(event)
      if (changed.captionText !== undefined) this.callbacks.onCaption(changed.captionText)
      if (changed.transcriptText) this.callbacks.onTranscript(changed.transcriptText)
      if (changed.isNotHeard) this.callbacks.onProblem(this.messages.notHeard, false)
    }
    socket.onclose = () => this.end(this.isEnded ? undefined : this.messages.connectionLost)
  }

  // level is how loud the microphone is now, from 0 to 1, for drawing.
  level(): number {
    if (!this.analyser || !this.samples) return 0
    this.analyser.getFloatTimeDomainData(this.samples)
    let sum = 0
    for (const sample of this.samples) sum += sample * sample
    // Speech at a normal distance sits around a tenth in root mean square;
    // scaled so that it fills most of the range.
    return Math.min(1, Math.sqrt(sum / this.samples.length) * 6)
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
    this.callbacks.onHearing(false)
    this.callbacks.onCaption('')
    this.callbacks.onEnded()
  }

  private release() {
    this.socket?.close()
    this.stream?.getTracks().forEach((track) => track.stop())
    void this.context?.close().catch(() => undefined)
  }
}
