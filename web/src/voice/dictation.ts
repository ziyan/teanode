import { authorization } from '../api'
import { TranscriptTracker, voiceSocketAddress, type VoiceEvent } from './voiceSession'

// Dictation: the person speaking into a box instead of typing. The
// microphone streams to the same socket the call with the agent uses
// (internal/api/v1api/apigraph/agent_voice.go), whose hello says the
// purpose is dictation. The server transcribes and starts no turn: each
// finished utterance is handed back here once, and the box puts it at the
// cursor. Nothing is sent from here.

// DictationPlace is where the words are going, which the server words its
// transcription prompt by.
export type DictationPlace = 'agent' | 'chat' | 'journal'

// DictationCallbacks are what a dictation tells the box.
export type DictationCallbacks = {
  // The server and the provider are ready for speech.
  onListening: () => void
  // The words of the utterance being heard, so far; empty clears them.
  onCaption: (captionText: string) => void
  // A finished utterance: once each, in the order spoken.
  onWords: (words: string) => void
  // Something the person should be told. Dictation has ended after it.
  onProblem: (problemText: string) => void
  // Listening stopped, for whatever reason.
  onEnded: () => void
}

// SILENCE_STOPS_AFTER_MS is how long dictation goes on with nobody
// speaking before it stops itself: a microphone left on is not acceptable.
export const SILENCE_STOPS_AFTER_MS = 30_000

// LAST_WORDS_WAIT_MS is how long a stop waits for the utterance being
// spoken to be transcribed, with the microphone silent so that the server
// hears it end.
export const LAST_WORDS_WAIT_MS = 3_000

// MAXIMUM_BUFFERED_BYTES is how much audio may wait to be sent before
// frames are dropped rather than queued.
const MAXIMUM_BUFFERED_BYTES = 1 << 20

// Dictation is one stretch of dictating, from the button pressed to the
// button pressed again.
export class Dictation {
  private socket?: WebSocket
  private stream?: MediaStream
  private context?: AudioContext
  private checkTimer?: number
  private lastSpeechAt = 0
  private tracker = new TranscriptTracker()
  // The utterances the server has started hearing and not yet finished.
  private openUtterances = new Set<string>()
  private isReady = false
  private isEnded = false
  private isStopping = false
  private stopTimer?: number

  constructor(
    private callbacks: DictationCallbacks,
    private messages: { microphoneRefused: string; connectionLost: string },
  ) {}

  // start begins listening. It is called from the person's tap: Safari on a
  // phone starts an audio context only within the gesture that asked for
  // it, so the context is made and resumed before anything is waited on.
  async start(sampleRate: number, place: DictationPlace) {
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
    const capture = new AudioWorkletNode(context, 'voice-capture', {
      numberOfInputs: 1,
      numberOfOutputs: 1,
      processorOptions: { targetRate: sampleRate },
    })
    // WebKit renders only what reaches the speakers, so the capture is
    // connected to them through a gain of nothing.
    const silent = context.createGain()
    silent.gain.value = 0
    source.connect(capture)
    capture.connect(silent)
    silent.connect(context.destination)

    const socket = new WebSocket(voiceSocketAddress(window.location))
    socket.binaryType = 'arraybuffer'
    this.socket = socket
    const granted = this.stream.getAudioTracks()[0]?.getSettings() ?? {}
    socket.onopen = () => {
      socket.send(
        JSON.stringify({
          voiceEvent: 'hello',
          authorization: authorization().Authorization,
          purpose: 'dictation',
          dictationPlace: place,
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
      this.accept(event)
    }
    socket.onclose = () => this.end(this.isEnded ? undefined : this.messages.connectionLost)
  }

  // accept reads one thing the server said.
  accept(event: VoiceEvent) {
    if (this.isEnded) return
    switch (event.voiceEvent) {
      case 'ready':
        this.isReady = true
        this.lastSpeechAt = performance.now()
        this.checkTimer = window.setInterval(() => this.checkSilence(), 1000)
        this.callbacks.onListening()
        return
      case 'problem':
        // Something the provider minded, which dictation goes on after.
        if (event.errorMessage) this.callbacks.onProblem(event.errorMessage)
        return
      case 'refused':
      case 'error':
        this.end(event.errorMessage ?? this.messages.connectionLost)
        return
    }
    if (event.voiceEvent === 'speechStarted' && event.utteranceId) this.openUtterances.add(event.utteranceId)
    if (
      event.voiceEvent === 'speechStarted' ||
      event.voiceEvent === 'speechStopped' ||
      event.voiceEvent === 'transcriptDelta'
    ) {
      this.lastSpeechAt = performance.now()
    }
    const changed = this.tracker.accept(event)
    if (event.voiceEvent === 'transcriptFinal' || event.voiceEvent === 'transcriptFailed') {
      this.openUtterances.delete(event.utteranceId ?? '')
    }
    if (changed.captionText !== undefined && !this.isStopping) this.callbacks.onCaption(changed.captionText)
    if (changed.transcriptText) this.callbacks.onWords(changed.transcriptText)
    // The last words are in: nothing more to wait for.
    if (this.isStopping && this.openUtterances.size === 0) this.finishStopping()
  }

  // checkSilence ends dictation that nobody has spoken into for a while. An
  // utterance still being transcribed is not silence.
  private checkSilence() {
    if (this.openUtterances.size > 0) return
    if (performance.now() - this.lastSpeechAt > SILENCE_STOPS_AFTER_MS) this.stop()
  }

  // stop ends dictation at the person's word, after the words being spoken
  // have been transcribed.
  stop() {
    if (this.isEnded || this.isStopping) return
    this.isStopping = true
    this.callbacks.onCaption('')
    if (this.openUtterances.size === 0 || !this.isReady) {
      this.finishStopping()
      return
    }
    // The microphone goes on, silent, so that the server hears the
    // utterance end; the track is the one thing the person can see.
    for (const track of this.stream?.getAudioTracks() ?? []) track.enabled = false
    this.stopTimer = window.setTimeout(() => this.finishStopping(), LAST_WORDS_WAIT_MS)
  }

  // abort ends dictation at once, and the words not yet transcribed are
  // dropped: the box they were for is gone or has been sent.
  abort() {
    this.isStopping = true
    this.finishStopping()
  }

  private finishStopping() {
    window.clearTimeout(this.stopTimer)
    if (this.socket?.readyState === WebSocket.OPEN) this.socket.send(JSON.stringify({ voiceEvent: 'stop' }))
    this.end()
  }

  private end(problemText?: string) {
    if (this.isEnded) return
    this.isEnded = true
    this.release()
    if (problemText) this.callbacks.onProblem(problemText)
    this.callbacks.onCaption('')
    this.callbacks.onEnded()
  }

  private release() {
    window.clearInterval(this.checkTimer)
    window.clearTimeout(this.stopTimer)
    this.socket?.close()
    this.stream?.getTracks().forEach((track) => track.stop())
    void this.context?.close().catch(() => undefined)
  }
}
