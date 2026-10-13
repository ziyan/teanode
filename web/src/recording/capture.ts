import { translateNow } from '../i18n/i18n'
import { recordingDigest } from './sha256'
import { CaptureQueue, holdCaptureLock, isCaptureLockHeld, recoverCaptures } from './captureQueue'
import { DashboardCaptureServer } from './captureServer'
import { updateCaptureSnapshot } from './captureSnapshot'
import { openCaptureStore, type CaptureGap, type CaptureStore, type StoredCapture } from './captureStore'
import { CutPolicy } from './cutPolicy'
import { LIVE_SAMPLE_RATE, LiveTranscriptionStream } from './liveStream'
import { levelDecibels, encodeWav, PIECE_SAMPLE_RATE } from './wav'

// Recording from the dashboard: the microphone through an AudioWorklet (the
// same one voice mode uses, run twice), cut into pieces that are kept in the
// browser until the server has verified them, and streamed to the server for
// the words shown while it goes on. The iPhone records the same way.

const WORKLET_ADDRESS = '/assets/voice-capture-worklet.js'
const PROGRESS_INTERVAL_MS = 5_000
const ELAPSED_INTERVAL_MS = 500
// A piece shorter than this, the tail of a recording stopped a moment after a
// cut, is not worth a transcription of its own.
const SHORTEST_PIECE_MS = 300
const LIVE_RETRY_MINIMUM_MS = 1_000
const LIVE_RETRY_MAXIMUM_MS = 30_000

const samplesPerMS = PIECE_SAMPLE_RATE / 1000

// CaptureEngine records one recording. Its timeline is the recording's own:
// milliseconds from the start, a pause counted in, so that a piece and a
// word are placed where they were said.
class CaptureEngine {
  private context?: AudioContext
  private stream?: MediaStream
  private nodes: AudioNode[] = []
  private policy = new CutPolicy()
  private live?: LiveTranscriptionStream
  private liveRetryCount = 0
  private liveRetryTimer?: number
  private isLiveConnected = false
  private chunks: Int16Array[] = []
  private bufferedSampleCount = 0
  // Where on the timeline the first buffered sample is.
  private bufferStartMS = 0
  private pieceIndex = 0
  private pendingWrites: Promise<unknown>[] = []
  private captureGaps: CaptureGap[] = []
  private phase: 'recording' | 'paused' | 'stopped' = 'recording'
  // On the timeline, the moment a pause began and the wall clock when it did.
  private pauseStartedMS = 0
  private pauseStartedWallMS = 0
  private progressTimer?: number
  private elapsedTimer?: number
  private releaseLock: () => void = () => undefined
  private startedAt = Date.now()

  constructor(
    readonly recordingId: string,
    private userId: string,
    private store: CaptureStore,
    private queue: CaptureQueue,
    private server: DashboardCaptureServer,
  ) {}

  // open starts the microphone. It is called inside the person's click: Safari
  // on a phone starts an audio context only within the gesture that asked for
  // it, so the context is made and resumed before anything is waited on.
  static async openMicrophone() {
    const audioSession = (navigator as Navigator & { audioSession?: { type: string } }).audioSession
    if (audioSession) {
      try {
        audioSession.type = 'play-and-record'
      } catch {
        /* Older Safari decides for itself. */
      }
    }
    const context = new AudioContext()
    void context.resume().catch(() => undefined)
    try {
      // Nothing is played back, so echo cancellation has nothing to cancel, and
      // noise suppression would take away the quiet voice in a room, which is
      // what the recording is for.
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: { channelCount: 1, echoCancellation: false, noiseSuppression: false, autoGainControl: true },
      })
      await context.audioWorklet.addModule(WORKLET_ADDRESS)
      if (context.state !== 'running') await context.resume().catch(() => undefined)
      return { context, stream }
    } catch (caught) {
      void context.close().catch(() => undefined)
      throw caught
    }
  }

  begin(context: AudioContext, stream: MediaStream) {
    this.context = context
    this.stream = stream
    const source = context.createMediaStreamSource(stream)
    // Two readers of one microphone: the pieces at the rate the server decodes
    // them at, the live stream at the rate the provider takes.
    const pieceNode = new AudioWorkletNode(context, 'voice-capture', {
      numberOfInputs: 1,
      numberOfOutputs: 1,
      processorOptions: { targetRate: PIECE_SAMPLE_RATE },
    })
    const liveNode = new AudioWorkletNode(context, 'voice-capture', {
      numberOfInputs: 1,
      numberOfOutputs: 1,
      processorOptions: { targetRate: LIVE_SAMPLE_RATE },
    })
    // WebKit renders only what reaches the speakers, so the readers are
    // connected to them through a gain of nothing: they run everywhere and
    // play nothing back.
    const silent = context.createGain()
    silent.gain.value = 0
    source.connect(pieceNode)
    source.connect(liveNode)
    pieceNode.connect(silent)
    liveNode.connect(silent)
    silent.connect(context.destination)
    this.nodes = [source, pieceNode, liveNode, silent]
    pieceNode.port.onmessage = (event: MessageEvent<ArrayBuffer>) => this.takePieceFrame(new Int16Array(event.data))
    liveNode.port.onmessage = (event: MessageEvent<ArrayBuffer>) => {
      if (this.phase === 'recording') this.live?.append(event.data)
    }
    this.releaseLock = holdCaptureLock(this.recordingId)
    this.policy.startPiece(0)
    this.openLive()
    this.progressTimer = window.setInterval(() => this.reportProgress(), PROGRESS_INTERVAL_MS)
    this.elapsedTimer = window.setInterval(
      () => updateCaptureSnapshot({ elapsedMS: this.timelineMS() }),
      ELAPSED_INTERVAL_MS,
    )
    updateCaptureSnapshot({ phase: 'recording', recordingId: this.recordingId, elapsedMS: 0, newestWords: '' })
  }

  // timelineMS is where the recording is now: the audio taken, plus the time
  // spent paused.
  private timelineMS(): number {
    if (this.phase === 'paused') return this.pauseStartedMS + (performance.now() - this.pauseStartedWallMS)
    return this.bufferStartMS + this.bufferedSampleCount / samplesPerMS
  }

  private takePieceFrame(samples: Int16Array) {
    if (this.phase !== 'recording') return
    this.chunks.push(samples)
    this.bufferedSampleCount += samples.length
    const frameEndMS = this.bufferStartMS + this.bufferedSampleCount / samplesPerMS
    const cutMS = this.policy.observeFrame(frameEndMS, levelDecibels(samples), this.isLiveConnected)
    if (cutMS !== null) this.cutPiece(cutMS)
  }

  // cutPiece closes the piece that ends at cutMS on the timeline and starts
  // the next with whatever audio is past it. The buffers are swapped before
  // anything is waited on, so a frame arriving meanwhile goes to the next.
  private cutPiece(cutMS: number) {
    const all = new Int16Array(this.bufferedSampleCount)
    let offset = 0
    for (const chunk of this.chunks) {
      all.set(chunk, offset)
      offset += chunk.length
    }
    const cutSampleCount = Math.min(all.length, Math.max(0, Math.round((cutMS - this.bufferStartMS) * samplesPerMS)))
    if (cutSampleCount === 0) return
    const closed = all.slice(0, cutSampleCount)
    const rest = all.slice(cutSampleCount)
    const pieceStartMS = this.bufferStartMS
    this.chunks = rest.length > 0 ? [rest] : []
    this.bufferedSampleCount = rest.length
    this.bufferStartMS = pieceStartMS + cutSampleCount / samplesPerMS
    this.policy.startPiece(this.bufferStartMS)
    this.storePiece(closed, pieceStartMS)
  }

  private storePiece(samples: Int16Array, pieceStartMS: number) {
    const durationMS = samples.length / samplesPerMS
    if (durationMS < SHORTEST_PIECE_MS) return
    const pieceIndex = this.pieceIndex++
    const write = (async () => {
      const bytes = encodeWav(samples)
      await this.store.putPiece({
        id: crypto.randomUUID(),
        userId: this.userId,
        recordingId: this.recordingId,
        pieceIndex,
        timelineStartMS: Math.round(pieceStartMS),
        durationMS: Math.round(durationMS),
        bytes,
        sha256: recordingDigest(new Uint8Array(bytes)),
        createdAt: Date.now(),
      })
      void this.queue.kick()
    })()
    this.pendingWrites.push(write.catch(() => undefined))
  }

  // flushBuffer closes the piece in progress wherever it is, for a pause or a stop.
  private flushBuffer() {
    this.cutPiece(this.bufferStartMS + this.bufferedSampleCount / samplesPerMS)
  }

  private storedCapture(captureState: StoredCapture['captureState'], timelineDurationMS: number): StoredCapture {
    return {
      recordingId: this.recordingId,
      userId: this.userId,
      captureState,
      timelineDurationMS: Math.round(timelineDurationMS),
      captureGaps: this.captureGaps.map((gap) => ({ ...gap })),
      startedAt: this.startedAt,
    }
  }

  async save(captureState: StoredCapture['captureState']) {
    await this.store.putCapture(this.storedCapture(captureState, this.timelineMS())).catch(() => undefined)
  }

  private reportProgress() {
    if (this.phase !== 'recording') return
    const durationMS = this.timelineMS()
    void this.save('active')
    void this.server.publish(this.recordingId, 'active', durationMS, this.captureGaps).catch(() => undefined)
  }

  private openLive() {
    window.clearTimeout(this.liveRetryTimer)
    this.live?.close()
    const stream = new LiveTranscriptionStream(this.recordingId, this.timelineMS(), {
      onReady: () => {
        this.isLiveConnected = true
        this.liveRetryCount = 0
        updateCaptureSnapshot({ isLiveConnected: true })
      },
      onClosed: () => {
        if (this.live !== stream) return
        this.isLiveConnected = false
        updateCaptureSnapshot({ isLiveConnected: false })
        if (this.phase !== 'recording') return
        // Words are a convenience: a stream that cannot be had is tried again
        // later, quietly, and the recording goes on without it.
        const delayMS = Math.min(LIVE_RETRY_MAXIMUM_MS, LIVE_RETRY_MINIMUM_MS * 2 ** this.liveRetryCount++)
        this.liveRetryTimer = window.setTimeout(() => {
          if (this.phase === 'recording') this.openLive()
        }, delayMS)
      },
      onWords: (_utteranceId, transcriptText) => updateCaptureSnapshot({ newestWords: transcriptText }),
      onUtteranceEnd: (utteranceEndMS) => {
        if (this.phase !== 'recording') return
        const bufferedEndMS = this.bufferStartMS + this.bufferedSampleCount / samplesPerMS
        const cutMS = this.policy.observeUtteranceEnd(utteranceEndMS, bufferedEndMS)
        if (cutMS !== null) this.cutPiece(cutMS)
      },
    })
    this.live = stream
    stream.open()
  }

  private closeLive() {
    window.clearTimeout(this.liveRetryTimer)
    const stream = this.live
    this.live = undefined
    stream?.close()
    this.isLiveConnected = false
    updateCaptureSnapshot({ isLiveConnected: false })
  }

  async pause() {
    if (this.phase !== 'recording') return
    this.flushBuffer()
    this.closeLive()
    this.pauseStartedMS = this.bufferStartMS
    this.pauseStartedWallMS = performance.now()
    this.phase = 'paused'
    this.captureGaps = [
      ...this.captureGaps,
      {
        gapId: crypto.randomUUID(),
        gapStartMS: this.pauseStartedMS,
        gapEndMS: this.pauseStartedMS,
        recordingGapReason: 'paused',
      },
    ]
    updateCaptureSnapshot({ phase: 'paused', elapsedMS: this.pauseStartedMS })
    await this.save('paused')
    void this.server.publish(this.recordingId, 'paused', this.pauseStartedMS, this.captureGaps).catch(() => undefined)
  }

  async resume() {
    if (this.phase !== 'paused') return
    const resumedMS = this.timelineMS()
    this.captureGaps = this.captureGaps.map((gap, index) =>
      index === this.captureGaps.length - 1 ? { ...gap, gapEndMS: resumedMS } : gap,
    )
    this.phase = 'recording'
    this.chunks = []
    this.bufferedSampleCount = 0
    this.bufferStartMS = resumedMS
    this.policy.startPiece(resumedMS)
    this.openLive()
    updateCaptureSnapshot({ phase: 'recording', elapsedMS: resumedMS })
    await this.save('active')
    void this.server.publish(this.recordingId, 'active', resumedMS, this.captureGaps).catch(() => undefined)
  }

  // stop ends the recording: the last piece is kept, the stop is written down
  // so that a failed report is made again, and the queue takes it from there.
  async stop() {
    if (this.phase === 'stopped') return
    updateCaptureSnapshot({ phase: 'stopping' })
    if (this.phase === 'paused') {
      const resumedMS = this.timelineMS()
      this.captureGaps = this.captureGaps.map((gap, index) =>
        index === this.captureGaps.length - 1 ? { ...gap, gapEndMS: resumedMS } : gap,
      )
      this.bufferStartMS = resumedMS
    } else {
      this.flushBuffer()
    }
    const durationMS = this.phase === 'paused' ? this.bufferStartMS : this.timelineMS()
    this.phase = 'stopped'
    this.closeLive()
    window.clearInterval(this.progressTimer)
    window.clearInterval(this.elapsedTimer)
    for (const node of this.nodes) node.disconnect()
    for (const track of this.stream?.getTracks() ?? []) track.stop()
    void this.context?.close().catch(() => undefined)
    await Promise.all(this.pendingWrites)
    await this.store.putCapture(this.storedCapture('stopRequested', durationMS))
    this.releaseLock()
    void this.queue.kick()
    updateCaptureSnapshot({ phase: 'idle', recordingId: null, elapsedMS: 0, newestWords: '' })
  }
}

let engine: CaptureEngine | undefined
let queue: CaptureQueue | undefined
let queueUserId = ''
const server = new DashboardCaptureServer()

async function queueFor(userId: string): Promise<{ store: CaptureStore; queue: CaptureQueue }> {
  const { store, isDurable } = await openCaptureStore()
  if (!queue || queueUserId !== userId) {
    queue?.dispose()
    queueUserId = userId
    queue = new CaptureQueue(store, server, {
      userId,
      onChange: ({ pendingPieceCount, errorMessage }) =>
        updateCaptureSnapshot({ pendingPieceCount, errorMessage, isStorageDurable: isDurable }),
    })
  }
  updateCaptureSnapshot({ isStorageDurable: isDurable })
  return { store, queue }
}

// startRecording asks for the microphone, makes the recording and begins. It
// is called from the person's click and answers the new recording's identifier.
export async function startRecording(userId: string): Promise<string> {
  if (engine) throw new Error(translateNow('recordingBar.alreadyRecording'))
  updateCaptureSnapshot({ phase: 'starting', errorMessage: '' })
  try {
    const microphone = await CaptureEngine.openMicrophone().catch((caught) => {
      throw new Error(
        `${translateNow('recordingBar.microphoneRefused')} ${caught instanceof Error ? caught.message : String(caught)}`.trim(),
      )
    })
    try {
      const { store, queue: uploads } = await queueFor(userId)
      const recording = await server.createRecording(`capture-${crypto.randomUUID()}`)
      const started = new CaptureEngine(recording.id, userId, store, uploads, server)
      engine = started
      await started.save('active')
      started.begin(microphone.context, microphone.stream)
      void server.publish(recording.id, 'active', 0, []).catch(() => undefined)
      return recording.id
    } catch (caught) {
      for (const track of microphone.stream.getTracks()) track.stop()
      void microphone.context.close().catch(() => undefined)
      engine = undefined
      throw caught
    }
  } catch (caught) {
    updateCaptureSnapshot({ phase: 'idle', recordingId: null })
    throw caught
  }
}

export const pauseRecording = () => engine?.pause()
export const resumeRecording = () => engine?.resume()

export async function stopRecording() {
  const stopping = engine
  if (!stopping) return
  try {
    await stopping.stop()
  } finally {
    engine = undefined
  }
}

// recoverRecordings uploads what an earlier visit left in the browser, and
// stops a capture that visit never finished.
export async function recoverRecordings(userId: string) {
  const { store, queue: uploads } = await queueFor(userId)
  await recoverCaptures(
    store,
    userId,
    async (recordingId) => engine?.recordingId === recordingId || (await isCaptureLockHeld(recordingId)),
  )
  await uploads.kick()
}
