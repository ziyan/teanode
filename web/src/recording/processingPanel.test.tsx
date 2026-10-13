import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { recordingOperation, type TranscriptSegment } from './processing'
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
function showFixture(
  extraSegments: Partial<TranscriptSegment>[] = [],
  overrides: Record<string, unknown> = {},
  initialRoute = '/?version=fixture-run',
) {
  vi.mocked(recordingOperation).mockImplementation(async (operationName) => {
    const responses: Record<string, unknown> = {
      ReadRecordingTranscriptionAvailability: {
        isTranscriptionAvailable: true,
        transcriptionProviderModelName: 'fixture:audio',
        transcriptionUnavailableMessage: '',
      },
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
          ...extraSegments.map((segment) => ({
            id: 'extra-segment',
            recordingTranscriptRunId: 'fixture-run',
            recordingTranscriptionAttemptId: 'fixture-attempt',
            localSpeakerId: 'local-one',
            segmentStartMS: 2000,
            segmentEndMS: 4000,
            transcriptText: 'Separate overlapping speech',
            effectiveTranscriptText: 'Separate overlapping speech',
            appliedTextCorrectionId: '',
            ...segment,
          })),
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
    return (
      Object.prototype.hasOwnProperty.call(overrides, operationName)
        ? overrides[operationName]
        : responses[operationName]
    ) as never
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
        <MemoryRouter initialEntries={[initialRoute]}>
          <RecordingProcessingPanel recordingId="fixture-recording" userId="fixture-owner" mediaFiles={[media]} />
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
}
it('requires explicit paid confirmation and retains the new version request key on retry', async () => {
  showFixture()
  fireEvent.click(await screen.findByRole('button', { name: 'Transcribe again' }))
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

it('keeps evidence editing available while paid processing is off', async () => {
  showFixture([], {
    ReadRecordingTranscriptionAvailability: {
      isTranscriptionAvailable: false,
      transcriptionUnavailableMessage: 'Processing is off for this fixture',
      transcriptionProviderModelName: '',
    },
  })
  expect(await screen.findByText('Processing is off for this fixture')).toBeTruthy()
  expect((screen.getByRole('button', { name: 'Transcribe again' }) as HTMLButtonElement).disabled).toBe(true)
})
it('limits rename controls to linked identities while retaining all account names for confirmation', async () => {
  showFixture([], {
    ReadRecordingSpeakers: {
      sourceGeneration: 4,
      speakerIdentities: [
        { id: 'linked-identity', confirmedName: 'Linked fixture' },
        { id: 'other-identity', confirmedName: 'Other fixture' },
      ],
      speakerCorrections: [{ id: 'removed-correction', speakerIdentityId: 'linked-identity', isRemoved: true }],
      voiceReferences: [],
    },
  })
  expect(await screen.findByText('Linked fixture')).toBeTruthy()
  expect(screen.getAllByRole('button', { name: 'Rename speaker' })).toHaveLength(1)
  expect(screen.queryByText('Other fixture')).toBeNull()
  fireEvent.click(await screen.findByRole('button', { name: 'Rename speaker' }))
  expect(
    Array.from(document.querySelectorAll('datalist option')).map((option) => option.getAttribute('value')),
  ).toEqual(['Linked fixture', 'Other fixture'])
})

it('resumes explicitly stopped reading directly without requesting paid processing or adding a gate', async () => {
  showFixture([], {
    ReadRecordingLearning: {
      hasAgent: true,
      hasStoppedReading: true,
      pendingDocumentCount: 0,
      digestedDocumentCount: 0,
      knowledgeSource: null,
    },
  })
  fireEvent.click(await screen.findByRole('button', { name: 'Resume reading' }))
  await waitFor(() =>
    expect(
      vi
        .mocked(recordingOperation)
        .mock.calls.some(
          ([operationName, variables]) =>
            operationName === 'ResumeRecordingLearning' &&
            variables.recordingId === 'fixture-recording' &&
            variables.expectedSourceGeneration === 4,
        ),
    ).toBe(true),
  )
  expect(screen.queryByRole('dialog')).toBeNull()
  expect(
    vi
      .mocked(recordingOperation)
      .mock.calls.some(([operationName]) => operationName === 'RequestRecordingTranscription'),
  ).toBe(false)
})
it('explains empty text history', async () => {
  showFixture()
  expect(
    await screen.findByText('No text corrections yet. Use Correct text on a passage of the conversation.'),
  ).toBeTruthy()
})

it('shows only the latest transcription, with no version list or timeline toggle', async () => {
  showFixture([], {
    ReadRecordingProcessing: {
      recording: { id: 'fixture-recording', userId: 'fixture-owner', sourceGeneration: 4, recordingTitle: 'Fixture' },
      transcriptRuns: [
        {
          id: 'older-run',
          recordingId: 'fixture-recording',
          recordingMediaId: 'fixture-media',
          versionNumber: 1,
          recordingProcessingState: 'complete',
        },
        {
          id: 'newer-run',
          recordingId: 'fixture-recording',
          recordingMediaId: 'fixture-media',
          versionNumber: 2,
          recordingProcessingState: 'failed',
          processingErrorMessage: 'Fixture failure',
        },
      ],
    },
  })
  expect(await screen.findByText('Latest transcript')).toBeTruthy()
  expect(screen.getByText('Fixture failure')).toBeTruthy()
  expect(screen.queryByText('Version 1')).toBeNull()
  expect(screen.queryByText('Version 2')).toBeNull()
  expect(screen.queryByRole('button', { name: 'Full recording timeline' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Retry this version' }))
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(
      vi
        .mocked(recordingOperation)
        .mock.calls.some(
          ([operationName, variables]) =>
            operationName === 'RetryRecordingTranscription' && variables.recordingTranscriptRunId === 'newer-run',
        ),
    ).toBe(true),
  )
})
