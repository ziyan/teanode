import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { type RecordingMedia } from './api'
import { RecordingConversation } from './conversationView'
import { recordingOperation } from './processing'
import { type RecordingPassagePlayback } from './playback'

vi.mock('./processing', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./processing')>()),
  recordingOperation: vi.fn(),
}))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

const segment = (id: string, overrides: Record<string, unknown> = {}) => ({
  id,
  recordingTranscriptRunId: 'fixture-run',
  recordingTranscriptionAttemptId: 'fixture-attempt',
  localSpeakerId: 'local-one',
  segmentStartMS: 2000,
  segmentEndMS: 4000,
  transcriptText: `Text of ${id}`,
  effectiveTranscriptText: `Text of ${id}`,
  hasUserConfirmedSpeaker: false,
  effectiveSpeakerIdentityId: '',
  effectiveSpeakerName: '',
  ...overrides,
})
const live = (utteranceId: string, utteranceStartMS: number) => ({
  utteranceId,
  utteranceStartMS,
  utteranceEndMS: utteranceStartMS + 1000,
  transcriptText: `Heard ${utteranceId}`,
})

function showLive(segments: unknown[], liveUtterances: unknown[], isLive = true) {
  const onChanged = vi.fn()
  vi.mocked(recordingOperation).mockImplementation(async (operationName) => {
    const responses: Record<string, unknown> = {
      ReadRecordingTranscriptTimeline: {
        sourceGeneration: 4,
        segments,
        transcriptRuns: [{ id: 'fixture-run', recordingMediaId: 'fixture-media' }],
        segmentCount: segments.length,
        hasMoreSegments: false,
      },
      ReadRecordingLiveTail: { liveUtterances },
      ReadRecordingSpeakers: {
        sourceGeneration: 4,
        speakerIdentities: [],
        speakerCorrections: [],
        voiceReferences: [],
      },
      NameRecordingTemporarySpeaker: true,
    }
    return responses[operationName] as never
  })
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter>
          <RecordingConversation
            recordingId="fixture-recording"
            userId="fixture-owner"
            mediaFiles={[{ id: 'fixture-media', timelineStartMS: 0 } as RecordingMedia]}
            passagePlayback={{ play: vi.fn() } as unknown as RecordingPassagePlayback}
            reloadToken={0}
            isLive={isLive}
            onChanged={onChanged}
          />
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
  return { onChanged }
}
const calls = (operationName: string) =>
  vi.mocked(recordingOperation).mock.calls.filter(([name]) => name === operationName)

it('draws live words as a neutral bubble on the left with no name and no menu', async () => {
  showLive([segment('one', { temporarySpeakerLabel: 'Speaker 1' })], [live('u1', 30000), live('u2', 32000)])
  const bubble = (await screen.findByText('Heard u1 Heard u2')).closest('.recording-turn') as HTMLElement
  expect(bubble.className).toContain('live')
  expect(bubble.className).not.toContain('own')
  expect(bubble.querySelector('.recording-turn-name')).toBeNull()
  expect(within(bubble).queryByRole('button')).toBeNull()
})

it('does not ask for live words when the recording is not being captured', async () => {
  showLive([segment('one')], [live('u1', 30000)], false)
  await screen.findByText('Text of one')
  expect(calls('ReadRecordingLiveTail')).toHaveLength(0)
  expect(screen.queryByText('Heard u1')).toBeNull()
})

it('shows a speaker nobody has named as "Speaker N"', async () => {
  showLive([segment('one', { temporarySpeakerLabel: 'Speaker 2' })], [])
  expect((await screen.findByText('Speaker 2')).className).toContain('muted')
})

it('names a temporary speaker by its label, for the whole recording', async () => {
  const { onChanged } = showLive([segment('one', { temporarySpeakerLabel: 'Speaker 2' })], [])
  fireEvent.click(await screen.findByRole('button', { name: 'Name speaker' }))
  const dialog = screen.getByRole('dialog')
  fireEvent.change(within(dialog).getByRole('combobox'), { target: { value: 'Fixture Guest' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(calls('NameRecordingTemporarySpeaker')).toHaveLength(1))
  expect(calls('NameRecordingTemporarySpeaker')[0][1]).toEqual({
    recordingId: 'fixture-recording',
    speakerLabel: 'Speaker 2',
    isAccountOwner: false,
    confirmedName: 'Fixture Guest',
  })
  await waitFor(() => expect(onChanged).toHaveBeenCalled())
})

it('marks a temporary speaker as the person with an empty name and the owner flag', async () => {
  const { onChanged } = showLive([segment('one', { temporarySpeakerLabel: 'Speaker 1' })], [])
  fireEvent.click(await screen.findByRole('button', { name: 'This is me' }))
  await waitFor(() => expect(calls('NameRecordingTemporarySpeaker')).toHaveLength(1))
  expect(calls('NameRecordingTemporarySpeaker')[0][1]).toEqual({
    recordingId: 'fixture-recording',
    speakerLabel: 'Speaker 1',
    confirmedName: '',
    isAccountOwner: true,
  })
  expect(calls('CorrectRecordingSpeakerOwner')).toHaveLength(0)
  await waitFor(() => expect(onChanged).toHaveBeenCalled())
})

it('says nothing has been heard when a capture has neither passages nor words', async () => {
  showLive([], [])
  expect(await screen.findByText('Nothing has been heard yet.')).toBeTruthy()
})
