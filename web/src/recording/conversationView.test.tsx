import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { type RecordingMedia } from './api'
import { RecordingConversation } from './conversationView'
import { RecordingDetailsDrawer } from './detailsDrawer'
import { recordingOperation } from './processing'
import { RecordingProcessingPanel } from './processingPanel'
import { type RecordingPassagePlayback } from './playback'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))
vi.mock('./processing', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./processing')>()),
  recordingOperation: vi.fn(),
}))
afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.unstubAllGlobals()
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

// The server reads the passages from an offset, a page at a time.
const pageOf = (segments: unknown[], variables?: Record<string, unknown>) => {
  const segmentOffset = Number(variables?.segmentOffset ?? 0)
  const pageSize = Number(variables?.pageSize ?? segments.length)
  const page = segments.slice(segmentOffset, segmentOffset + pageSize)
  return {
    segments: page,
    segmentCount: segments.length,
    hasMoreSegments: segmentOffset + page.length < segments.length,
  }
}

function show(segments: unknown[], route = '/', passageOffset = 0) {
  const passagePlayback = {
    play: vi.fn().mockResolvedValue(undefined),
    seekTo: vi.fn().mockResolvedValue(undefined),
    playRange: vi.fn().mockResolvedValue(undefined),
  }
  const onChanged = vi.fn()
  vi.mocked(recordingOperation).mockImplementation(async (operationName, variables) => {
    const responses: Record<string, unknown> = {
      ReadRecordingTranscriptTimeline: {
        sourceGeneration: 4,
        transcriptRuns: [{ id: 'fixture-run', recordingMediaId: 'fixture-media' }],
        // The server keeps the passages nobody has named when asked to.
        ...pageOf(
          variables?.isUnnamedOnly
            ? segments.filter(
                (candidate) => !(candidate as { effectiveSpeakerIdentityId?: string }).effectiveSpeakerIdentityId,
              )
            : segments,
          variables,
        ),
      },
      ReadRecordingTranscript: {
        transcriptRun: { id: 'older-run', recordingMediaId: 'fixture-media' },
        attempts: [],
        ...pageOf(segments, variables),
      },
      ReadRecordingTranscriptPassageOffset: passageOffset,
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
  expect(passagePlayback.seekTo).not.toHaveBeenCalled()
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
  // The one player plays from where the bubble begins on the recording's
  // timeline to where its last passage ends, and stops there.
  expect(passagePlayback.playRange).toHaveBeenCalledTimes(1)
  expect(passagePlayback.playRange).toHaveBeenCalledWith(2000, 6000)
  expect(passagePlayback.seekTo).not.toHaveBeenCalled()
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
  expect(passagePlayback.seekTo).not.toHaveBeenCalled()
})

it('offers its actions in the corner, and in a pill a tap opens, as the agent chat does', async () => {
  show([segment('one')])
  await screen.findByText('Text of one')
  for (const label of ['In memory', 'Correct text', 'Name speaker', 'This is me'])
    expect(screen.getByRole('button', { name: label })).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'More' })).toBeNull()
  tapBubble('Text of one')
  const pill = screen.getByRole('menu')
  expect(
    within(pill)
      .getAllByRole('menuitem')
      .map((item) => item.getAttribute('aria-label')),
  ).toEqual(['Play', 'Copy', 'In memory', 'Correct text', 'Name speaker', 'This is me'])
  fireEvent.keyDown(document, { key: 'Escape' })
  expect(screen.queryByRole('menu')).toBeNull()
})

it('asks which pages of memory are linked to the bubble, by its range, only when asked', async () => {
  vi.mocked(graphql).mockResolvedValue({
    ReadRecordMemoryLinks: [
      {
        id: 'link-1',
        nodePath: 'people/fixture-guest',
        nodeName: 'Fixture Guest',
        nodeKind: 'person',
        recordKind: 'recording_passage',
        recordId: 'fixture-recording',
        recordRelation: 'spoke_in',
        linkStatus: 'proposed',
        passageStartMS: 2000,
        passageEndMS: 4000,
        transcriptRunId: 'fixture-run',
        note: '',
        createdAt: '2026-01-01T00:00:00Z',
        recordTitle: '',
        dashboardPath: '',
        isUnavailable: false,
      },
    ],
  } as never)
  show([
    segment('one', { segmentStartMS: 2000, segmentEndMS: 4000 }),
    segment('two', { segmentStartMS: 4000, segmentEndMS: 9000 }),
  ])
  await screen.findByText('Text of one')
  expect(vi.mocked(graphql)).not.toHaveBeenCalled()
  fireEvent.click(screen.getAllByRole('button', { name: 'In memory' })[0])
  const link = await screen.findByRole('link', { name: 'Fixture Guest' })
  expect(link.getAttribute('href')).toBe('/knowledge/people/fixture-guest')
  // A guess is said to be one.
  expect(screen.getByText('Perhaps')).toBeTruthy()
  const [, variables] = vi.mocked(graphql).mock.calls[0]
  expect(variables).toMatchObject({
    recordKind: 'recording_passage',
    recordId: 'fixture-recording',
    passageStartMS: 2000,
  })
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

it('takes a naming back: Not me on your own bubble, Clear name on anyone else', async () => {
  const { onChanged } = show([
    segment('mine', {
      ...confirmed('Fixture Owner', 'identity-owner', true),
      appliedSpeakerCorrectionId: 'naming-own',
    }),
    segment('theirs', {
      ...confirmed('Fixture Guest', 'identity-guest'),
      localSpeakerId: 'local-two',
      appliedSpeakerCorrectionId: 'naming-guest',
    }),
  ])
  await screen.findByText('Text of theirs')
  fireEvent.click(within(bubbleOf('Text of mine')).getByRole('button', { name: 'Not me' }))
  await waitFor(() => expect(calls('UndoRecordingSpeakerNaming')).toHaveLength(1))
  expect(calls('UndoRecordingSpeakerNaming')[0][1]).toMatchObject({
    expectedSourceGeneration: 4,
    recordingSpeakerCorrectionId: 'naming-own',
  })
  // No name form: taking a naming back needs nobody else's name.
  expect(screen.queryByRole('dialog')).toBeNull()
  fireEvent.click(within(bubbleOf('Text of theirs')).getByRole('button', { name: 'Clear name' }))
  await waitFor(() => expect(calls('UndoRecordingSpeakerNaming')).toHaveLength(2))
  expect(calls('UndoRecordingSpeakerNaming')[1][1]).toMatchObject({ recordingSpeakerCorrectionId: 'naming-guest' })
  await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(2))
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

// A fake IntersectionObserver the test can fire: the end of the transcript coming near the screen.
function observeEnd() {
  const observers: { callback: IntersectionObserverCallback; isDisconnected: boolean }[] = []
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      entry: { callback: IntersectionObserverCallback; isDisconnected: boolean }
      constructor(callback: IntersectionObserverCallback) {
        this.entry = { callback, isDisconnected: false }
        observers.push(this.entry)
      }
      observe() {}
      disconnect() {
        this.entry.isDisconnected = true
      }
    },
  )
  return () =>
    act(() => {
      const current = observers.filter((observer) => !observer.isDisconnected).pop()
      current?.callback([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver)
    })
}
const manySegments = (count: number) =>
  Array.from({ length: count }, (_, index) =>
    segment(`passage-${index}`, {
      segmentStartMS: index * 1000,
      segmentEndMS: index * 1000 + 900,
      // Passages 98 to 102 are one speaker's, across the edge of the first page.
      localSpeakerId: index >= 98 && index <= 102 ? 'local-two' : `local-${index}`,
    }),
  )

it('reads a hundred passages and appends the next hundred when the end comes near', async () => {
  const fireEnd = observeEnd()
  show(manySegments(250))
  await screen.findByText('Text of passage-0')
  expect(calls('ReadRecordingTranscriptTimeline')[0][1]).toMatchObject({ segmentOffset: 0, pageSize: 100 })
  expect(screen.queryByText('Text of passage-100')).toBeNull()
  expect(document.querySelector('.pager')).toBeNull()
  fireEnd()
  await screen.findByText('Text of passage-100', undefined, { timeout: 10_000 })
  expect(calls('ReadRecordingTranscriptTimeline')[1][1]).toMatchObject({ segmentOffset: 100, pageSize: 100 })
  expect(screen.getByText('Text of passage-0')).toBeTruthy()
  fireEnd()
  await screen.findByText('Text of passage-249')
  expect(calls('ReadRecordingTranscriptTimeline')).toHaveLength(3)
  // Nothing is left to read, so nothing more is asked for.
  expect(document.querySelector('.recording-more-sentinel')).toBeNull()
})

it("keeps one speaker's bubble together across the edge of two pages", async () => {
  const fireEnd = observeEnd()
  show(manySegments(250))
  await screen.findByText('Text of passage-99')
  fireEnd()
  await screen.findByText('Text of passage-102')
  const turn = bubbleOf('Text of passage-98').closest('.recording-turn') as HTMLElement
  for (const index of [98, 99, 100, 101, 102]) expect(within(turn).getByText(`Text of passage-${index}`)).toBeTruthy()
})

it('reads on until a cited passage is present and scrolls to it', async () => {
  const scrollIntoView = vi.fn()
  Element.prototype.scrollIntoView = scrollIntoView
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      observe() {}
      disconnect() {}
    },
  )
  show(manySegments(250), '/?version=older-run&startMS=205000&endMS=205900', 205)
  // Several pages are read first: a slow runner needs longer than the default second.
  await screen.findByText('Text of passage-205', undefined, { timeout: 10_000 })
  expect(calls('ReadRecordingTranscript').map(([, variables]) => variables?.segmentOffset)).toEqual([0, 100, 200])
  await waitFor(() => expect(scrollIntoView).toHaveBeenCalled())
  expect(document.querySelector('.recording-passage.cited')?.textContent).toContain('Text of passage-205')
})

it('narrows the conversation to the speakers nobody has named, and says how many passages are left', async () => {
  show([
    segment('named', { ...confirmed('Fixture Guest', 'identity-guest'), segmentStartMS: 1000 }),
    segment('unnamed-one', { localSpeakerId: 'local-two', segmentStartMS: 3000 }),
    segment('unnamed-two', { localSpeakerId: 'local-three', segmentStartMS: 5000 }),
  ])
  await screen.findByText('Text of named')
  expect(calls('ReadRecordingTranscriptTimeline')[0][1]).not.toHaveProperty('isUnnamedOnly')
  fireEvent.click(screen.getByRole('button', { name: 'Unnamed speakers' }))
  await screen.findByText('2 passages left to name')
  expect(calls('ReadRecordingTranscriptTimeline').at(-1)?.[1]).toMatchObject({ isUnnamedOnly: true, segmentOffset: 0 })
  expect(screen.queryByText('Text of named')).toBeNull()
  expect(screen.getByText('Text of unnamed-one')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Unnamed speakers' }).getAttribute('aria-pressed')).toBe('true')
  fireEvent.click(screen.getByRole('button', { name: 'Unnamed speakers' }))
  await screen.findByText('Text of named')
})

it('says every speaker is named when none is left', async () => {
  show([segment('named', confirmed('Fixture Guest', 'identity-guest'))], '/?speakers=unnamed')
  await screen.findByText('Every speaker is named.')
})
