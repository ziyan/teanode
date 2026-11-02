// Voice messages: a recording made while a button is held, sent to a chat as
// one file. The browser's MediaRecorder writes it; the server turns whatever
// it was into AAC in an MP4 container (internal/voicemessage), so the type
// chosen here only has to be one the browser can write.

// SHORTEST_VOICE_MESSAGE_MS is the shortest recording that is sent: a hold
// shorter than this was a tap.
export const SHORTEST_VOICE_MESSAGE_MS = 500

// LONGEST_VOICE_MESSAGE_MS is where a recording is sent on its own, and
// COUNTDOWN_VOICE_MESSAGE_MS is where the time left starts to show.
export const LONGEST_VOICE_MESSAGE_MS = 120_000
export const COUNTDOWN_VOICE_MESSAGE_MS = 110_000

// ASKING_MIN_MS is how long a microphone request takes before it is taken
// to have shown the browser's permission prompt, in a browser that cannot
// say beforehand whether it would.
const ASKING_MIN_MS = 800

// recordingTypeOf is the type MediaRecorder is asked to write: AAC in MP4
// where the browser can (Safari, recent Chrome), else Opus in WebM.
export function recordingTypeOf(): string {
  if (typeof MediaRecorder === 'undefined') return ''
  const preferred = ['audio/mp4;codecs=mp4a.40.2', 'audio/webm;codecs=opus']
  for (const type of preferred) {
    try {
      if (MediaRecorder.isTypeSupported(type)) return type
    } catch {
      // A browser that throws for a type does not write it.
    }
  }
  return ''
}

// canRecordVoiceMessages is whether this browser can record at all.
export function canRecordVoiceMessages(): boolean {
  return (
    typeof MediaRecorder !== 'undefined' &&
    typeof navigator !== 'undefined' &&
    Boolean(navigator.mediaDevices?.getUserMedia)
  )
}

// RecordedVoiceMessage is a finished recording.
export type RecordedVoiceMessage = {
  file: File
  durationMS: number
}

// RecordingStart says how starting went: recording; only asked, because the
// browser put up its permission prompt for this press; or canceled before
// the microphone arrived.
export type RecordingStart = 'recording' | 'asked' | 'canceled'

// microphonePermission is what the browser says about the microphone, or
// '' where it does not say.
async function microphonePermission(): Promise<string> {
  try {
    const status = await navigator.permissions?.query({ name: 'microphone' as PermissionName })
    return status?.state ?? ''
  } catch {
    return ''
  }
}

// fileTypeOf is the recording's type without its codecs, and the extension
// that goes with it.
function fileTypeOf(recordingType: string): { contentType: string; extension: string } {
  const contentType = recordingType.split(';')[0] || 'audio/webm'
  return { contentType, extension: contentType === 'audio/mp4' ? 'm4a' : 'webm' }
}

// VoiceMessageRecorder is one recording, from the press to the release.
export class VoiceMessageRecorder {
  private stream?: MediaStream
  private recorder?: MediaRecorder
  private context?: AudioContext
  private analyser?: AnalyserNode
  private samples?: Float32Array<ArrayBuffer>
  private chunks: Blob[] = []
  private startedAt = 0
  private isCanceled = false
  private recordingType = ''

  // start asks for the microphone and begins recording. The press that makes
  // the browser ask is not a recording: the person is answering a prompt,
  // not talking, so the stream is let go and 'asked' is returned.
  async start(): Promise<RecordingStart> {
    this.recordingType = recordingTypeOf()
    // Asked before the microphone is, and without waiting: Safari wants the
    // microphone requested in the press that began it.
    let permission = ''
    const asking = microphonePermission().then((state) => {
      permission = state
    })
    const requestedAt = performance.now()
    const stream = await navigator.mediaDevices.getUserMedia({
      audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true, autoGainControl: true },
    })
    await asking
    if (this.isCanceled) {
      stream.getTracks().forEach((track) => track.stop())
      return 'canceled'
    }
    const wasAsked = permission ? permission === 'prompt' : performance.now() - requestedAt > ASKING_MIN_MS
    if (wasAsked) {
      stream.getTracks().forEach((track) => track.stop())
      return 'asked'
    }
    this.stream = stream
    this.listen(stream)
    this.recorder = new MediaRecorder(stream, this.recordingType ? { mimeType: this.recordingType } : undefined)
    this.recorder.ondataavailable = (event) => {
      if (event.data && event.data.size > 0) this.chunks.push(event.data)
    }
    this.recorder.start(250)
    this.startedAt = performance.now()
    return 'recording'
  }

  // listen reads how loud the microphone is, for the level the strip draws.
  private listen(stream: MediaStream) {
    if (typeof AudioContext === 'undefined') return
    try {
      this.context = new AudioContext()
      this.analyser = this.context.createAnalyser()
      this.analyser.fftSize = 512
      this.samples = new Float32Array(this.analyser.fftSize)
      this.context.createMediaStreamSource(stream).connect(this.analyser)
    } catch {
      this.analyser = undefined
    }
  }

  // elapsedMS is how long the recording has gone on.
  elapsedMS(): number {
    return this.startedAt ? performance.now() - this.startedAt : 0
  }

  // level is how loud the microphone is now, from 0 to 1.
  level = (): number => {
    if (!this.analyser || !this.samples) return 0
    this.analyser.getFloatTimeDomainData(this.samples)
    let sum = 0
    for (const sample of this.samples) sum += sample * sample
    return Math.min(1, Math.sqrt(sum / this.samples.length) * 6)
  }

  // finish ends the recording and gives the file.
  finish(): Promise<RecordedVoiceMessage | null> {
    const recorder = this.recorder
    if (!recorder || this.isCanceled) {
      this.release()
      return Promise.resolve(null)
    }
    const durationMS = this.elapsedMS()
    return new Promise((resolve) => {
      const done = () => {
        this.release()
        const { contentType, extension } = fileTypeOf(recorder.mimeType || this.recordingType)
        const blob = new Blob(this.chunks, { type: contentType })
        resolve({ file: new File([blob], `voice-message.${extension}`, { type: contentType }), durationMS })
      }
      if (recorder.state === 'inactive') {
        done()
        return
      }
      recorder.onstop = done
      recorder.stop()
    })
  }

  // cancel throws the recording away, whether or not it has started.
  cancel() {
    this.isCanceled = true
    const recorder = this.recorder
    if (recorder) {
      recorder.ondataavailable = null
      recorder.onstop = null
      if (recorder.state !== 'inactive') {
        try {
          recorder.stop()
        } catch {
          // Already stopped.
        }
      }
    }
    this.chunks = []
    this.release()
  }

  private release() {
    this.stream?.getTracks().forEach((track) => track.stop())
    this.stream = undefined
    void this.context?.close().catch(() => undefined)
    this.context = undefined
    this.analyser = undefined
  }
}

// clockOf writes a length of time as minutes and seconds, 1:05.
export function clockOf(milliseconds: number): string {
  const seconds = Math.max(0, Math.floor(milliseconds / 1000))
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`
}
