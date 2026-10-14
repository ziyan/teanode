import { graphql } from '../api'
import {
  RECORDING_DOCUMENTS,
  isMatchingOriginalManifest,
  uploadRecordingMedia,
  type Recording,
  type RecordingMedia,
} from './api'
import { type CaptureServer } from './captureQueue'
import { type CaptureGap, type StoredCapture, type StoredPiece } from './captureStore'

const pieceFilename = (piece: StoredPiece) => `piece-${String(piece.pieceIndex + 1).padStart(4, '0')}.wav`

// DashboardCaptureServer is the queue's server and the recorder's: it makes a
// recording, uploads pieces and reports the capture's progress, over the same
// calls the iPhone makes.
export class DashboardCaptureServer implements CaptureServer {
  // The generation each recording was last known at. Progress leaves it alone,
  // but naming a speaker moves it, so a report that is refused is tried once
  // more with the generation read again.
  private generations = new Map<string, number>()
  // Reports go one at a time, in the order they were made, so that a stop is
  // never overtaken by the progress that was sent before it.
  private reports: Promise<unknown> = Promise.resolve()

  async createRecording(clientRecordingId: string): Promise<Recording> {
    const { CreateRecording: recording } = await graphql<{ CreateRecording: Recording }>(
      RECORDING_DOCUMENTS.CreateRecording,
      { clientRecordingId, recordingTitle: '', recordingOrigin: 'capture' },
    )
    this.generations.set(recording.id, recording.sourceGeneration)
    return recording
  }

  async uploadPiece(piece: StoredPiece, signal: AbortSignal): Promise<void> {
    const expected = {
      recordingId: piece.recordingId,
      clientMediaId: `piece-${piece.pieceIndex}-${piece.timelineStartMS}-${piece.sha256.slice(0, 16)}`,
      originalFilename: pieceFilename(piece),
      originalContentType: 'audio/wav',
      originalSizeBytes: piece.bytes.byteLength,
      originalSha256: piece.sha256,
    }
    const { CreateRecordingMedia: media } = await graphql<{ CreateRecordingMedia: RecordingMedia }>(
      RECORDING_DOCUMENTS.CreateRecordingMedia,
      { ...expected, timelineStartMS: piece.timelineStartMS, mediaDurationMS: piece.durationMS },
      signal,
    )
    if (!isMatchingOriginalManifest(media, expected)) throw new Error('the server manifest does not match the piece')
    await uploadRecordingMedia(media, new Blob([piece.bytes], { type: 'audio/wav' }), signal)
  }

  publish(
    recordingId: string,
    captureState: 'active' | 'paused' | 'stopped',
    timelineDurationMS: number,
    captureGaps: CaptureGap[],
    signal?: AbortSignal,
  ): Promise<void> {
    const report = this.reports.then(async () => {
      const send = async (expectedGeneration: number) => {
        const { UpdateRecordingCapture: recording } = await graphql<{ UpdateRecordingCapture: Recording }>(
          RECORDING_DOCUMENTS.UpdateRecordingCapture,
          {
            recordingId,
            expectedGeneration,
            recordingCaptureState: captureState,
            timelineDurationMS: Math.round(timelineDurationMS),
            captureGaps: captureGaps.map((gap) => ({
              gapId: gap.gapId,
              gapStartMS: Math.round(gap.gapStartMS),
              gapEndMS: Math.round(gap.gapEndMS),
              recordingGapReason: gap.recordingGapReason,
            })),
          },
          signal,
        )
        this.generations.set(recordingId, recording.sourceGeneration)
      }
      const known = this.generations.get(recordingId)
      try {
        if (known === undefined) throw new Error('the generation is not known yet')
        await send(known)
      } catch (caught) {
        const fresh = (
          await graphql<{ ReadRecording: { recording: { sourceGeneration: number } } }>(
            RECORDING_DOCUMENTS.ReadRecordingGeneration,
            { recordingId },
            signal,
          )
        ).ReadRecording.recording.sourceGeneration
        if (fresh === known) throw caught
        await send(fresh)
      }
    })
    // A report that failed does not stop the ones after it.
    this.reports = report.catch(() => undefined)
    return report
  }

  publishStopped(capture: StoredCapture, signal: AbortSignal): Promise<void> {
    return this.publish(capture.recordingId, 'stopped', capture.timelineDurationMS, capture.captureGaps, signal)
  }
}
