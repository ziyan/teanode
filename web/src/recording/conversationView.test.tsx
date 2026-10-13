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

it('does not play when the words are clicked', async () => {
  const { passagePlayback } = show([segment('one')])
  fireEvent.click(await screen.findByText('Text of one'))
  expect(passagePlayback.play).not.toHaveBeenCalled()
})

// jsdom has no PointerEvent: a mouse event that carries the pointer's id and
// kind is what React reads.
if (typeof window.PointerEvent === 'undefined') {
  class TestPointerEvent extends MouseEvent {
    pointerId: number
    pointerType: string
    constructor(type: string, init: PointerEventInit = {}) {
      super(type, init)
      this.pointerId = init.pointerId ?? 1
      this.pointerType = init.pointerType ?? 'mouse'
    }
  }
  window.PointerEvent = TestPointerEvent as unknown as typeof PointerEvent
}

// swipe runs a finger across a bubble from (100, 100) through the points given.
const swipe = (bubble: HTMLElement, points: [number, number][]) => {
  const at = (x: number, y: number) => ({ pointerId: 3, pointerType: 'touch', clientX: 100 + x, clientY: 100 + y })
  fireEvent.pointerDown(bubble, at(0, 0))
  for (const [x, y] of points) fireEvent.pointerMove(bubble, at(x, y))
  const [lastX, lastY] = points[points.length - 1]
  fireEvent.pointerUp(bubble, at(lastX, lastY))
}
const bubbleOf = (text: string) => screen.getByText(text).closest('.agent-line') as HTMLElement
// tapBubble is a finger's tap on a bubble's words, which opens its actions.
const tapBubble = (text: string) => {
  const words = screen.getByText(text)
  const at = { pointerId: 7, pointerType: 'touch', clientX: 50, clientY: 50 }
  fireEvent.pointerDown(words, at)
  fireEvent.pointerUp(words, at)
  fireEvent.click(words)
}

it('draws bubbles with the agent chat classes, the owner on the right', async () => {
  show([
    segment('guest', confirmed('Fixture Guest', 'identity-guest')),
    segment('mine', { localSpeakerId: 'local-two', ...confirmed('Fixture Owner', 'identity-owner', true) }),
  ])
  await screen.findByText('Text of guest')
  expect(bubbleOf('Text of guest').className).toContain('agent-line assistant')
  expect(bubbleOf('Text of mine').className).toContain('agent-line user')
  expect(document.querySelector('.recording-bubble')).toBeNull()
})

it('plays the whole bubble on a swipe to the left', async () => {
  const { passagePlayback } = show([segment('one'), segment('two', { segmentStartMS: 4000, segmentEndMS: 6000 })])
  await screen.findByText('Text of one')
  swipe(bubbleOf('Text of one'), [
    [-14, 2],
    [-50, 10],
    [-90, 20],
  ])
  expect(passagePlayback.play).toHaveBeenCalledTimes(1)
  expect(passagePlayback.play).toHaveBeenCalledWith('fixture-media', 2000, 6000, 1000)
})

it('copies the whole bubble on a swipe to the right', async () => {
  const writeText = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
  show([segment('one'), segment('two', { segmentStartMS: 4000, segmentEndMS: 6000 })])
  await screen.findByText('Text of one')
  swipe(bubbleOf('Text of one'), [
    [14, 0],
    [90, 0],
  ])
  await waitFor(() => expect(writeText).toHaveBeenCalledWith('Text of one Text of two'))
})

it('does nothing for a short swipe or a scroll, and a long press opens no menu', async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true })
  const { passagePlayback } = show([segment('one')])
  const passage = await screen.findByText('Text of one')
  swipe(bubbleOf('Text of one'), [[-30, 0]])
  fireEvent.pointerDown(passage, { pointerType: 'touch', pointerId: 5, clientX: 5, clientY: 5 })
  act(() => void vi.advanceTimersByTime(800))
  expect(screen.queryByRole('menu')).toBeNull()
  fireEvent.pointerUp(passage, { pointerType: 'touch', pointerId: 5, clientX: 5, clientY: 5 })
  fireEvent.contextMenu(passage, { clientX: 20, clientY: 20 })
  expect(screen.queryByRole('menu')).toBeNull()
  expect(passagePlayback.play).not.toHaveBeenCalled()
})

it('offers its actions in the corner, and in a pill a tap opens, as the agent chat does', async () => {
  show([segment('one')])
  await screen.findByText('Text of one')
  for (const label of ['Correct text', 'Name speaker', 'This is me'])
    expect(screen.getByRole('button', { name: label })).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'More' })).toBeNull()
  tapBubble('Text of one')
  const pill = screen.getByRole('menu')
  expect(
    within(pill)
      .getAllByRole('menuitem')
      .map((item) => item.getAttribute('aria-label')),
  ).toEqual(['Play', 'Copy', 'Correct text', 'Name speaker', 'This is me'])
  fireEvent.keyDown(document, { key: 'Escape' })
  expect(screen.queryByRole('menu')).toBeNull()
})

it('corrects text through the shared dialog', async () => {
  const { onChanged } = show([segment('one')])
  fireEvent.click(await screen.findByRole('button', { name: 'Correct text' }))
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
  fireEvent.click(await screen.findByRole('button', { name: 'Name speaker' }))
  const dialog = screen.getByRole('dialog')
  fireEvent.change(within(dialog).getByRole('combobox'), { target: { value: 'Fixture Guest' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(calls('CorrectRecordingSpeaker')).toHaveLength(1))
  expect(calls('CorrectRecordingSpeaker')[0][1]).toMatchObject({
    confirmedName: 'Fixture Guest',
    localSpeakerId: 'local-one',
    // The voice, every passage of it in the request, not one span of time.
    passageStartMS: -1,
    passageEndMS: -1,
  })
})

it('marks a passage as the account owner, and Not me opens the name form', async () => {
  const { onChanged } = show([segment('mine', confirmed('Fixture Owner', 'identity-owner', true)), segment('other')])
  await screen.findByText('Text of other')
  fireEvent.click(screen.getByRole('button', { name: 'This is me' }))
  await waitFor(() => expect(calls('CorrectRecordingSpeakerOwner')).toHaveLength(1))
  expect(calls('CorrectRecordingSpeakerOwner')[0][1]).toMatchObject({ isAccountOwner: true, confirmedName: '' })
  await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(1))
  // Your own bubble offers one way to say who it was: Not me, not Name speaker too.
  expect(within(bubbleOf('Text of mine')).queryByRole('button', { name: 'Name speaker' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Not me' }))
  // Releasing a passage is confirming it as somebody else: the name form opens.
  expect(within(screen.getByRole('dialog')).getByRole('combobox')).toBeTruthy()
  expect(calls('CorrectRecordingSpeakerOwner')).toHaveLength(1)
})

it('opens an exact older version read-only, without correction actions', async () => {
  show([segment('one')], '/?version=older-run&startMS=2500&endMS=3500')
  await screen.findByText('Text of one')
  expect(calls('ReadRecordingTranscript')[0][1]).toMatchObject({ recordingTranscriptRunId: 'older-run' })
  expect(screen.getByText('You are reading an earlier transcript.')).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Correct text' })).toBeNull()
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

it('corrects a bubble of two passages, saving only the passage that changed', async () => {
  show([segment('one'), segment('two', { segmentStartMS: 4000, segmentEndMS: 6000 })])
  fireEvent.click(await screen.findByRole('button', { name: 'Correct text' }))
  const dialog = screen.getByRole('dialog')
  const boxes = within(dialog).getAllByRole('textbox')
  expect(boxes).toHaveLength(2)
  fireEvent.change(boxes[1], { target: { value: 'Second passage, corrected' } })
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(calls('CorrectRecordingTranscriptText')).toHaveLength(1))
  expect(calls('CorrectRecordingTranscriptText')[0][1]).toMatchObject({
    recordingTranscriptSegmentId: 'two',
    correctedTranscriptText: 'Second passage, corrected',
  })
})
