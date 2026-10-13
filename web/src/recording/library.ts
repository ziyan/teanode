import { graphql } from '../api'
import { type Recording } from './api'

export type RecordingStatus = 'recording' | 'uploading' | 'transcribing' | 'ready' | 'failed' | 'notTranscribed'
export interface RecordingStatusEvidence {
  mediaFiles: { recordingUploadState: string }[]
  transcriptRuns: { versionNumber: number; recordingProcessingState: string }[]
}

const ACTIVE_RUN_STATES = ['queued', 'preparing', 'transcribing']

// One status per recording, whatever the pipeline is doing: the person asked
// only whether it can be opened yet, not which worker holds it.
export function recordingStatus(recording: Recording, evidence: RecordingStatusEvidence | undefined): RecordingStatus {
  if (recording.recordingCaptureState === 'active' || recording.recordingCaptureState === 'paused') return 'recording'
  if (!evidence) return 'ready'
  if (
    evidence.mediaFiles.length === 0 ||
    evidence.mediaFiles.some((media) => media.recordingUploadState !== 'complete')
  )
    return 'uploading'
  const latestRun = [...evidence.transcriptRuns].sort((first, second) => second.versionNumber - first.versionNumber)[0]
  if (!latestRun) return 'notTranscribed'
  if (ACTIVE_RUN_STATES.includes(latestRun.recordingProcessingState)) return 'transcribing'
  if (latestRun.recordingProcessingState === 'complete') return 'ready'
  if (latestRun.recordingProcessingState === 'canceled') return 'notTranscribed'
  return 'failed'
}

// One request for the whole page of the library, because the list itself
// carries no pipeline state and a request per row is twenty-five round trips.
export async function readRecordingStatusEvidence(
  recordingIds: string[],
  signal?: AbortSignal,
): Promise<Record<string, RecordingStatusEvidence>> {
  if (recordingIds.length === 0) return {}
  const variables: Record<string, string> = {}
  recordingIds.forEach((recordingId, index) => (variables[`recordingId${index}`] = recordingId))
  const declarations = recordingIds.map((_, index) => `$recordingId${index}: String!`).join(', ')
  const selections = recordingIds
    .map(
      (_, index) =>
        `r${index}: ReadRecording(recordingId: $recordingId${index}) { mediaFiles { recordingUploadState } transcriptRuns { versionNumber recordingProcessingState } }`,
    )
    .join(' ')
  const response = await graphql<Record<string, RecordingStatusEvidence>>(
    `query (${declarations}) { ${selections} }`,
    variables,
    signal,
  )
  const evidenceByRecordingId: Record<string, RecordingStatusEvidence> = {}
  recordingIds.forEach((recordingId, index) => {
    if (response[`r${index}`]) evidenceByRecordingId[recordingId] = response[`r${index}`]
  })
  return evidenceByRecordingId
}

// recordedAt is when the recording happened; createdAt is when it reached the
// server, which for an import can be years later and is only the tiebreak for
// a recording whose date nobody knows.
export function recordingSortTime(recording: Recording): number {
  const time = Date.parse(recording.recordedAt ?? recording.createdAt)
  return Number.isFinite(time) ? time : 0
}
export const sortRecordingsNewestFirst = (recordings: Recording[]): Recording[] =>
  [...recordings].sort((first, second) => recordingSortTime(second) - recordingSortTime(first))

export function formatRecordedAt(recordedAt: string | null, language: string): string | null {
  const time = recordedAt ? Date.parse(recordedAt) : NaN
  if (!Number.isFinite(time)) return null
  return new Date(time).toLocaleString(language, { dateStyle: 'medium', timeStyle: 'short' })
}

export function formatRecordingLength(durationMS: number): string {
  const totalSeconds = Math.max(0, Math.round(durationMS / 1000))
  const hours = Math.floor(totalSeconds / 3600),
    minutes = Math.floor((totalSeconds % 3600) / 60),
    seconds = totalSeconds % 60
  return hours > 0
    ? `${hours}:${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
    : `${minutes}:${String(seconds).padStart(2, '0')}`
}

export const firstSummaryLine = (recordingSummary: string | undefined): string =>
  (recordingSummary ?? '')
    .split('\n')
    .map((line) => line.trim())
    .find(Boolean) ?? ''
