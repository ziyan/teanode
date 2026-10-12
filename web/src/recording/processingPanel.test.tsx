import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { recordingOperation } from './processing'
import { RecordingProcessingPanel } from './processingPanel'
import { type RecordingMedia } from './api'
vi.mock('./processing', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./processing')>()),
  recordingOperation: vi.fn(),
}))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})
function showFixture() {
  vi.mocked(recordingOperation).mockImplementation(async (operationName) => {
    const responses: Record<string, unknown> = {
      ReadRecordingProcessing: {
        recording: {
          id: 'fixture-recording',
          userId: 'fixture-owner',
          sourceGeneration: 4,
          recordingTitle: 'Fixture',
          recordedAt: null,
        },
        transcriptRuns: [
          {
            id: 'fixture-run',
            recordingId: 'fixture-recording',
            recordingMediaId: 'fixture-media',
            versionNumber: 1,
            recordingProcessingState: 'complete',
            providerModelName: 'fixture:audio',
          },
        ],
      },
      ReadRecordingTranscript: {
        transcriptRun: { id: 'fixture-run', recordingId: 'fixture-recording' },
        segments: [
          {
            id: 'fixture-segment',
            recordingTranscriptRunId: 'fixture-run',
            recordingTranscriptionAttemptId: 'fixture-attempt',
            localSpeakerId: 'local-one',
            segmentStartMS: 2000,
            segmentEndMS: 4000,
            transcriptText: 'Generated evidence',
            effectiveTranscriptText: 'Generated evidence',
            appliedTextCorrectionId: '',
          },
        ],
        attempts: [],
        segmentCount: 1,
        hasMoreSegments: false,
      },
      ReadRecordingSpeakers: {
        sourceGeneration: 4,
        speakerIdentities: [],
        speakerCorrections: [],
        voiceReferences: [],
      },
      ReadRecordingLearning: {
        hasAgent: true,
        pendingDocumentCount: 1,
        digestedDocumentCount: 0,
        knowledgeSource: null,
      },
      ReadRecordingReferenceInvalidations: [],
      ReadRecordingTranscriptTextCorrections: [],
    }
    if (operationName === 'CorrectRecordingTranscriptText' || operationName === 'RequestRecordingTranscription')
      throw new Error('Uncertain response; retry with the same key')
    return responses[operationName] as never
  })
  const media = {
    id: 'fixture-media',
    originalFilename: 'fixture.wav',
    recordingUploadState: 'complete',
    timelineStartMS: 0,
  } as RecordingMedia
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter initialEntries={['/?version=fixture-run']}>
          <RecordingProcessingPanel recordingId="fixture-recording" userId="fixture-owner" mediaFiles={[media]} />
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
}
it('retains the exact free text edit and stable correction key after uncertain responses', async () => {
  showFixture()
  fireEvent.click(await screen.findByRole('button', { name: 'Edit recognized text' }))
  const dialog = screen.getByRole('dialog')
  fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: 'Exact user correction' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await screen.findByText('Uncertain response; retry with the same key')
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(
      vi
        .mocked(recordingOperation)
        .mock.calls.filter(([operationName]) => operationName === 'CorrectRecordingTranscriptText'),
    ).toHaveLength(2),
  )
  const calls = vi
    .mocked(recordingOperation)
    .mock.calls.filter(([operationName]) => operationName === 'CorrectRecordingTranscriptText')
  expect(calls[0][1]).toEqual(calls[1][1])
  expect(calls[0][1]).toMatchObject({
    correctedTranscriptText: 'Exact user correction',
    recordingTranscriptSegmentId: 'fixture-segment',
    expectedSourceGeneration: 4,
  })
  expect(screen.getByText('Generated evidence')).toBeTruthy()
})
it('requires explicit paid confirmation and retains the new version request key on retry', async () => {
  showFixture()
  fireEvent.click(await screen.findByRole('button', { name: 'New paid version' }))
  expect(
    vi
      .mocked(recordingOperation)
      .mock.calls.filter(([operationName]) => operationName === 'RequestRecordingTranscription'),
  ).toHaveLength(0)
  const dialog = screen.getByRole('dialog')
  fireEvent.click(within(dialog).getByRole('button', { name: 'Request paid processing' }))
  await screen.findByText('Uncertain response; retry with the same key')
  fireEvent.click(within(dialog).getByRole('button', { name: 'Request paid processing' }))
  await waitFor(() =>
    expect(
      vi
        .mocked(recordingOperation)
        .mock.calls.filter(([operationName]) => operationName === 'RequestRecordingTranscription'),
    ).toHaveLength(2),
  )
  const calls = vi
    .mocked(recordingOperation)
    .mock.calls.filter(([operationName]) => operationName === 'RequestRecordingTranscription')
  expect(calls[0][1]).toEqual(calls[1][1])
  expect(calls[0][1].clientRunId).not.toBe('initial')
  expect(calls[0][1].isReprocessingRequested).toBe(true)
})
