import { expect, it } from 'vitest'
import { type Recording } from './api'
import {
  firstSummaryLine,
  formatRecordedAt,
  formatRecordingLength,
  recordingStatus,
  sortRecordingsNewestFirst,
} from './library'

const recordingFixture = (overrides: Partial<Recording>): Recording =>
  ({
    id: 'fixture',
    createdAt: '2026-01-01T00:00:00Z',
    recordedAt: null,
    recordingCaptureState: 'stopped',
    ...overrides,
  }) as Recording

it('sorts by when the recording was made, falling back to when it was created', () => {
  const sorted = sortRecordingsNewestFirst([
    recordingFixture({
      id: 'made-early-uploaded-late',
      recordedAt: '2026-02-01T00:00:00Z',
      createdAt: '2026-09-01T00:00:00Z',
    }),
    recordingFixture({ id: 'undated-created-middle', recordedAt: null, createdAt: '2026-04-01T00:00:00Z' }),
    recordingFixture({ id: 'made-late', recordedAt: '2026-06-01T00:00:00Z', createdAt: '2026-06-02T00:00:00Z' }),
  ])
  expect(sorted.map((recording) => recording.id)).toEqual([
    'made-late',
    'undated-created-middle',
    'made-early-uploaded-late',
  ])
})

it('never turns a missing recorded date into the upload date', () => {
  expect(formatRecordedAt(null, 'en')).toBeNull()
  expect(formatRecordedAt('not a date', 'en')).toBeNull()
  expect(formatRecordedAt('2026-10-13T09:41:00Z', 'en')).toMatch(/2026/)
})

it('derives one status from the uploads and the latest transcription', () => {
  const recording = recordingFixture({})
  const complete = { recordingUploadState: 'complete' }
  expect(recordingStatus(recordingFixture({ recordingCaptureState: 'active' }), undefined)).toBe('recording')
  expect(recordingStatus(recording, { mediaFiles: [{ recordingUploadState: 'pending' }], transcriptRuns: [] })).toBe(
    'uploading',
  )
  expect(recordingStatus(recording, { mediaFiles: [complete], transcriptRuns: [] })).toBe('notTranscribed')
  const runs = (older: string, newer: string) => [
    { versionNumber: 1, recordingProcessingState: older },
    { versionNumber: 2, recordingProcessingState: newer },
  ]
  expect(recordingStatus(recording, { mediaFiles: [complete], transcriptRuns: runs('complete', 'transcribing') })).toBe(
    'transcribing',
  )
  expect(recordingStatus(recording, { mediaFiles: [complete], transcriptRuns: runs('complete', 'failed') })).toBe(
    'failed',
  )
  expect(recordingStatus(recording, { mediaFiles: [complete], transcriptRuns: runs('failed', 'complete') })).toBe(
    'ready',
  )
})

it('formats lengths and takes the first line of a summary', () => {
  expect(formatRecordingLength(65_000)).toBe('1:05')
  expect(formatRecordingLength(3_725_000)).toBe('1:02:05')
  expect(firstSummaryLine('\n  First line.\nSecond line.')).toBe('First line.')
  expect(firstSummaryLine(undefined)).toBe('')
})
