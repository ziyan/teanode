import { type CaptureStore, type StoredCapture, type StoredPiece } from './captureStore'

// CaptureServer is what the queue needs of the server, so that the queue can
// be tested without one.
export interface CaptureServer {
  // uploadPiece makes the piece a media item of its recording, sends its
  // bytes and returns when the server has verified the digest.
  uploadPiece(piece: StoredPiece, signal: AbortSignal): Promise<void>
  // publishStopped tells the server the capture has stopped.
  publishStopped(capture: StoredCapture, signal: AbortSignal): Promise<void>
}

// Pieces older than this are not worth keeping: the recording they belong to
// has long been given up on, or deleted, and a piece the server refuses would
// otherwise be retried for ever.
export const PIECE_RETENTION_MS = 14 * 24 * 60 * 60 * 1000

export const RETRY_DELAY_MINIMUM_MS = 2_000
export const RETRY_DELAY_MAXIMUM_MS = 60_000

export interface CaptureQueueOptions {
  userId: string
  now?: () => number
  // setTimer lets a test run the retries by hand.
  setTimer?: (work: () => void, delayMS: number) => unknown
  clearTimer?: (timer: unknown) => void
  onChange?: (state: { pendingPieceCount: number; errorMessage: string }) => void
}

// CaptureQueue gets what the browser has stored to the server: the pieces of
// recordings, in order, each deleted from the store only once the server has
// verified it, and the word that a capture has stopped. A failure leaves
// everything where it is and tries again later; nothing here ever throws to
// the recorder.
export class CaptureQueue {
  private isRunning = false
  private isRunAgainWanted = false
  private failureCount = 0
  private timer: unknown
  private controller = new AbortController()
  private isDisposed = false
  private pendingPieceCount = 0
  private errorMessage = ''
  private running?: Promise<void>

  constructor(
    private store: CaptureStore,
    private server: CaptureServer,
    private options: CaptureQueueOptions,
  ) {}

  private now() {
    return (this.options.now ?? Date.now)()
  }

  private report(pendingPieceCount: number, errorMessage: string) {
    this.pendingPieceCount = pendingPieceCount
    this.errorMessage = errorMessage
    this.options.onChange?.({ pendingPieceCount, errorMessage })
  }

  getPendingPieceCount() {
    return this.pendingPieceCount
  }

  getErrorMessage() {
    return this.errorMessage
  }

  // kick starts a pass unless one is running, in which case it asks that one
  // to look again when it is done.
  kick(): Promise<void> {
    if (this.isDisposed) return Promise.resolve()
    if (this.timer !== undefined) {
      ;(this.options.clearTimer ?? ((timer) => window.clearTimeout(timer as number)))(this.timer)
      this.timer = undefined
    }
    if (this.isRunning) {
      this.isRunAgainWanted = true
      return this.running ?? Promise.resolve()
    }
    this.running = this.run()
    return this.running
  }

  dispose() {
    this.isDisposed = true
    this.controller.abort()
    if (this.timer !== undefined)
      (this.options.clearTimer ?? ((timer) => window.clearTimeout(timer as number)))(this.timer)
  }

  private async run(): Promise<void> {
    this.isRunning = true
    try {
      do {
        this.isRunAgainWanted = false
        await this.pass()
      } while (this.isRunAgainWanted && !this.isDisposed)
    } finally {
      this.isRunning = false
    }
  }

  private async pass() {
    let hasFailed = false
    let errorMessage = ''
    try {
      const captures = (await this.store.listCaptures()).filter(
        (capture) => capture.userId === this.options.userId && capture.captureState === 'stopRequested',
      )
      for (const capture of captures) {
        try {
          await this.server.publishStopped(capture, this.controller.signal)
          await this.store.deleteCapture(capture.recordingId)
        } catch (caught) {
          hasFailed = true
          errorMessage = caught instanceof Error ? caught.message : String(caught)
        }
      }
      const pieces = (await this.store.listPieces())
        .filter((piece) => piece.userId === this.options.userId)
        .sort((first, second) => first.timelineStartMS - second.timelineStartMS)
      let remainingCount = pieces.length
      this.report(remainingCount, this.errorMessage)
      for (const piece of pieces) {
        if (this.isDisposed) return
        if (this.now() - piece.createdAt > PIECE_RETENTION_MS) {
          await this.store.deletePiece(piece.id)
          remainingCount--
          continue
        }
        try {
          await this.server.uploadPiece(piece, this.controller.signal)
          await this.store.deletePiece(piece.id)
          remainingCount--
          this.report(remainingCount, '')
        } catch (caught) {
          hasFailed = true
          errorMessage = caught instanceof Error ? caught.message : String(caught)
          // The pieces after this one wait for it: the server transcribes in
          // order, and a failure is likely to be the connection.
          break
        }
      }
      this.report(remainingCount, hasFailed ? errorMessage : '')
    } catch (caught) {
      hasFailed = true
      errorMessage = caught instanceof Error ? caught.message : String(caught)
      this.report(this.pendingPieceCount, errorMessage)
    }
    if (hasFailed && !this.isDisposed) {
      this.failureCount++
      const delayMS = Math.min(RETRY_DELAY_MAXIMUM_MS, RETRY_DELAY_MINIMUM_MS * 2 ** (this.failureCount - 1))
      this.timer = (this.options.setTimer ?? ((work, delay) => window.setTimeout(work, delay)))(() => {
        this.timer = undefined
        void this.kick()
      }, delayMS)
    } else {
      this.failureCount = 0
    }
  }
}

// recoverCaptures settles what an earlier visit left behind: a capture that
// is still marked active or paused and is not being recorded by any tab was
// ended by a reload or a closed tab, so it is stopped at the end of the last
// piece it has. The queue then uploads what is left of it.
export async function recoverCaptures(
  store: CaptureStore,
  userId: string,
  isRecordingHere: (recordingId: string) => Promise<boolean>,
): Promise<void> {
  const captures = (await store.listCaptures()).filter((capture) => capture.userId === userId)
  const pieces = await store.listPieces()
  for (const capture of captures) {
    if (capture.captureState !== 'stopRequested') {
      if (await isRecordingHere(capture.recordingId)) continue
      const endOfPiecesMS = Math.max(
        0,
        ...pieces
          .filter((piece) => piece.recordingId === capture.recordingId)
          .map((piece) => piece.timelineStartMS + piece.durationMS),
      )
      await store.putCapture({
        ...capture,
        captureState: 'stopRequested',
        timelineDurationMS: Math.max(capture.timelineDurationMS, endOfPiecesMS),
      })
    }
  }
}

const lockName = (recordingId: string) => `teanode-recording-${recordingId}`

// holdCaptureLock marks a recording as being recorded by this tab for as long
// as the lock is held, so that another tab's recovery leaves it alone. It
// returns the way to let it go.
export function holdCaptureLock(recordingId: string): () => void {
  let release: () => void = () => undefined
  const held = new Promise<void>((resolve) => (release = resolve))
  if (typeof navigator !== 'undefined' && navigator.locks)
    void navigator.locks.request(lockName(recordingId), () => held)
  return release
}

// isCaptureLockHeld says whether some tab is recording this recording now.
export async function isCaptureLockHeld(recordingId: string): Promise<boolean> {
  if (typeof navigator === 'undefined' || !navigator.locks) return false
  return await navigator.locks.request(lockName(recordingId), { ifAvailable: true }, async (lock) => lock === null)
}
