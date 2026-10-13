import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { type RecordingMedia } from './api'
import { RecordingConversation } from './conversationView'
import { RecordingDetailsDrawer } from './detailsDrawer'
import { recordingOperation } from './processing'
import { RecordingProcessingPanel } from './processingPanel'
import { type RecordingPassagePlayback } from './playback'

vi.mock('./processing', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./processing')>()),
  recordingOperation: vi.fn(),
}))
afterEach(() => {
  cleanup()
  vi.useRealTimers()
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
const media = {
  id: 'fixture-media',
  originalFilename: 'fixture.wav',
  recordingUploadState: 'complete',
  timelineStartMS: 1000,
} as RecordingMedia

function show(segments: unknown[], route = '/') {
  const passagePlayback = { play: vi.fn().mockResolvedValue(undefined) }
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
      ReadRecordingTranscript: {
        transcriptRun: { id: 'older-run', recordingMediaId: 'fixture-media' },
        segments,
        attempts: [],
        segmentCount: segments.length,
        hasMoreSegments: false,
      },
      ReadRecordingTranscriptPassageOffset: 0,
      ReadRecordingSpeakers: {
        sourceGeneration: 4,
        speakerIdentities: [],
        speakerCorrections: [],
        voiceReferences: [],
      },
    }
    return responses[operationName] as never
  })
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter initialEntries={[route]}>
          <RecordingConversation
            recordingId="fixture-recording"
            userId="fixture-owner"
            mediaFiles={[media]}
            passagePlayback={passagePlayback as unknown as RecordingPassagePlayback}
            reloadToken={0}
            onChanged={onChanged}
          />
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
  return { passagePlayback, onChanged }
}
const confirmed = (name: string, identity: string, isAccountOwner = false) => ({
  hasUserConfirmedSpeaker: true,
  effectiveSpeakerIdentityId: identity,
  effectiveSpeakerName: name,
  isAccountOwner,
})
const calls = (operationName: string) =>
  vi.mocked(recordingOperation).mock.calls.filter(([name]) => name === operationName)

it('groups consecutive passages, names others above and puts the owner on the right without a name', async () => {
  show([
    segment('one', confirmed('Fixture Guest', 'identity-guest')),
    segment('two', confirmed('Fixture Guest', 'identity-guest')),
    segment('three', { localSpeakerId: 'local-two', ...confirmed('Fixture Owner', 'identity-owner', true) }),
    segment('four', { localSpeakerId: 'local-three' }),
  ])
  await screen.findByText('Text of one')
  const turns = Array.from(document.querySelectorAll('.recording-turn')) as HTMLElement[]
  expect(turns).toHaveLength(3)
  expect(within(turns[0]).getByText('Fixture Guest')).toBeTruthy()
  expect(within(turns[0]).getByText('Text of two')).toBeTruthy()
  expect(turns[1].className).toContain('own')
  expect(within(turns[1]).queryByText('Fixture Owner')).toBeNull()
  expect(within(turns[2]).getByText('Speaker A').className).toContain('muted')
})

it('plays a passage from its start when clicked', async () => {
  const { passagePlayback } = show([segment('one')])
  fireEvent.click(await screen.findByText('Text of one'))
  expect(passagePlayback.play).toHaveBeenCalledWith('fixture-media', 2000, 4000, 1000)
})

it('opens the passage menu on right-click and runs the chosen action', async () => {
  const { passagePlayback } = show([segment('one')])
  fireEvent.contextMenu(await screen.findByText('Text of one'), { clientX: 20, clientY: 20 })
  const menu = screen.getByRole('menu')
  expect(
    within(menu)
      .getAllByRole('menuitem')
      .map((item) => item.textContent),
  ).toEqual(['Play from here', 'Copy', 'Correct text', 'Name speaker', 'This is me'])
  fireEvent.click(within(menu).getByRole('menuitem', { name: 'Play from here' }))
  expect(passagePlayback.play).toHaveBeenCalled()
  expect(screen.queryByRole('menu')).toBeNull()
})

it('corrects text through the shared dialog', async () => {
  const { onChanged } = show([segment('one')])
  fireEvent.contextMenu(await screen.findByText('Text of one'), { clientX: 20, clientY: 20 })
  fireEvent.click(screen.getByRole('menuitem', { name: 'Correct text' }))
  const dialog = screen.getByRole('dialog')
  fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: 'Better words' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(calls('CorrectRecordingTranscriptText')).toHaveLength(1))
  expect(calls('CorrectRecordingTranscriptText')[0][1]).toMatchObject({
    recordingTranscriptSegmentId: 'one',
    correctedTranscriptText: 'Better words',
    expectedSourceGeneration: 4,
  })
  await waitFor(() => expect(onChanged).toHaveBeenCalled())
})

it('names a speaker through the shared dialog', async () => {
  show([segment('one')])
  fireEvent.contextMenu(await screen.findByText('Text of one'), { clientX: 20, clientY: 20 })
  fireEvent.click(screen.getByRole('menuitem', { name: 'Name speaker' }))
  const dialog = screen.getByRole('dialog')
  fireEvent.change(within(dialog).getByRole('combobox'), { target: { value: 'Fixture Guest' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(calls('CorrectRecordingSpeaker')).toHaveLength(1))
  expect(calls('CorrectRecordingSpeaker')[0][1]).toMatchObject({
    confirmedName: 'Fixture Guest',
    localSpeakerId: 'local-one',
    passageStartMS: 2000,
    passageEndMS: 4000,
  })
})

it('marks a passage as the account owner and releases it again', async () => {
  const { onChanged } = show([segment('mine', confirmed('Fixture Owner', 'identity-owner', true)), segment('other')])
  fireEvent.contextMenu(await screen.findByText('Text of other'), { clientX: 20, clientY: 20 })
  fireEvent.click(screen.getByRole('menuitem', { name: 'This is me' }))
  await waitFor(() => expect(calls('CorrectRecordingSpeakerOwner')).toHaveLength(1))
  expect(calls('CorrectRecordingSpeakerOwner')[0][1]).toMatchObject({ isAccountOwner: true, confirmedName: '' })
  await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(1))
  fireEvent.contextMenu(screen.getByText('Text of mine'), { clientX: 20, clientY: 20 })
  fireEvent.click(screen.getByRole('menuitem', { name: 'Not me' }))
  await waitFor(() => expect(calls('CorrectRecordingSpeakerOwner')).toHaveLength(2))
  expect(calls('CorrectRecordingSpeakerOwner')[1][1]).toMatchObject({
    isAccountOwner: false,
    confirmedName: 'Fixture Owner',
  })
})

it('opens the menu after a half second press on touch and not before', async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true })
  const { passagePlayback } = show([segment('one')])
  const passage = await screen.findByText('Text of one')
  fireEvent.pointerDown(passage, { pointerType: 'touch', clientX: 5, clientY: 5 })
  act(() => void vi.advanceTimersByTime(300))
  expect(screen.queryByRole('menu')).toBeNull()
  act(() => void vi.advanceTimersByTime(250))
  expect(screen.getByRole('menu')).toBeTruthy()
  fireEvent.pointerUp(passage)
  fireEvent.click(passage)
  expect(passagePlayback.play).not.toHaveBeenCalled()
})

it('is reachable by keyboard through the More button', async () => {
  show([segment('one')])
  fireEvent.click(await screen.findByRole('button', { name: 'Actions for this passage' }))
  expect(screen.getByRole('menu')).toBeTruthy()
  fireEvent.keyDown(document, { key: 'Escape' })
  expect(screen.queryByRole('menu')).toBeNull()
})

it('opens an exact older version read-only, without correction actions', async () => {
  show([segment('one')], '/?version=older-run&startMS=2500&endMS=3500')
  await screen.findByText('Text of one')
  expect(calls('ReadRecordingTranscript')[0][1]).toMatchObject({ recordingTranscriptRunId: 'older-run' })
  expect(screen.getByText('You are reading an earlier transcript.')).toBeTruthy()
  fireEvent.contextMenu(screen.getByText('Text of one'), { clientX: 20, clientY: 20 })
  expect(screen.getAllByRole('menuitem').map((item) => item.textContent)).toEqual(['Play from here', 'Copy'])
})

it('shows the advanced panels in the details drawer and closes with Escape', async () => {
  vi.mocked(recordingOperation).mockImplementation(async (operationName) => {
    const responses: Record<string, unknown> = {
      ReadRecordingTranscriptionAvailability: {
        isTranscriptionAvailable: true,
        transcriptionProviderModelName: '',
        transcriptionUnavailableMessage: '',
      },
      ReadRecordingProcessing: {
        recording: { id: 'fixture-recording', userId: 'fixture-owner', sourceGeneration: 4 },
        transcriptRuns: [],
      },
      ReadRecordingSpeakers: {
        sourceGeneration: 4,
        speakerIdentities: [],
        speakerCorrections: [],
        voiceReferences: [],
      },
      ReadRecordingLearning: {
        hasAgent: true,
        pendingDocumentCount: 0,
        digestedDocumentCount: 0,
        knowledgeSource: null,
      },
      ReadRecordingTranscriptTextCorrections: [],
    }
    return responses[operationName] as never
  })
  const onClose = vi.fn()
  render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter>
          <RecordingDetailsDrawer onClose={onClose}>
            <RecordingProcessingPanel recordingId="fixture-recording" userId="fixture-owner" mediaFiles={[media]} />
          </RecordingDetailsDrawer>
        </MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
  const drawer = screen.getByRole('dialog', { name: 'Recording details' })
  expect(await within(drawer).findByText('Speakers and references')).toBeTruthy()
  expect(within(drawer).getByText('Learning')).toBeTruthy()
  expect(within(drawer).getByText('Text correction history')).toBeTruthy()
  fireEvent.keyDown(document, { key: 'Escape' })
  expect(onClose).toHaveBeenCalled()
})
