import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { RecordingShareDialog } from './shareDialog'
import { SharedRecordingsSection } from './sharedList'
import { SharedRecordingPage } from './sharedPage'
import { groupSharedPassages, type SharedRecording } from './sharing'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))

const summaryOnly: SharedRecording = {
  recordingId: 'recording-1',
  ownerUserId: 'alex-id',
  ownerUsername: 'alex',
  ownerName: 'Alex',
  recordingTitle: 'Standup',
  createdAt: '2030-01-02T10:00:00Z',
  recordedAt: '2030-01-02T09:00:00Z',
  timelineDurationMS: 125000,
  recordingSummary: 'Two items were agreed.',
  recordingShareLevel: 'summary',
  speakerNames: ['Alex'],
  passages: [],
  passageCount: 0,
  hasMorePassages: false,
}
const withTranscript: SharedRecording = {
  ...summaryOnly,
  recordingShareLevel: 'transcript',
  passages: [
    {
      segmentStartMS: 0,
      segmentEndMS: 1000,
      speakerName: 'Alex',
      transcriptText: 'We ship on Friday.',
      isFiller: false,
    },
    { segmentStartMS: 1000, segmentEndMS: 1500, speakerName: 'Alex', transcriptText: 'Mm-hmm.', isFiller: true },
    {
      segmentStartMS: 2000,
      segmentEndMS: 3000,
      speakerName: 'Alex',
      transcriptText: 'After the review.',
      isFiller: false,
    },
    {
      segmentStartMS: 65000,
      segmentEndMS: 66000,
      speakerName: 'Speaker 1',
      transcriptText: 'Agreed.',
      isFiller: false,
    },
  ],
  passageCount: 120,
  hasMorePassages: true,
}

let answers: Record<string, unknown> = {}

beforeEach(() => {
  vi.mocked(graphql).mockReset()
  answers = {}
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    for (const operationName of [
      'ListRecordingShares',
      'ShareRecording',
      'UnshareRecording',
      'ListSharedRecordings',
      'ReadSharedRecording',
      'ListCirclePeople',
      'ListCircles',
    ])
      if (document.includes(operationName + '(') || document.includes(operationName + ' ')) {
        return { [operationName]: answers[operationName] } as never
      }
    return {} as never
  })
})
afterEach(cleanup)

function inRouter(element: React.ReactNode, address = '/') {
  return render(
    <TranslationProvider>
      <ToastProvider>
        <MemoryRouter initialEntries={[address]}>{element}</MemoryRouter>
      </ToastProvider>
    </TranslationProvider>,
  )
}

it('says before anything is chosen that the others who speak did not agree', async () => {
  answers.ListRecordingShares = []
  answers.ListCirclePeople = [{ userId: 'sam-id', username: 'sam', name: 'Sam', avatarUrl: null, avatarColorIndex: 1 }]
  answers.ListCircles = [{ id: 'circle-1', name: 'Family', members: [{ userId: 'a' }, { userId: 'b' }] }]
  inRouter(<RecordingShareDialog recordingId="recording-1" onClose={() => {}} />)
  expect(await screen.findByText(/have not agreed to it being shared/)).toBeTruthy()
  expect(await screen.findByText('This recording is not shared with anybody yet.')).toBeTruthy()
  // The circle is told from another of the same name by how many people it is.
  expect(await screen.findByRole('option', { name: 'Everyone in Family (2 people)' })).toBeTruthy()
})

it('shares with a person at the level chosen, and stops a share', async () => {
  answers.ListRecordingShares = [
    {
      id: 'share-1',
      recordingId: 'recording-1',
      granteeUserId: 'sam-id',
      granteeUsername: 'sam',
      granteeName: 'Sam',
      granteeCircleId: null,
      granteeCircleName: '',
      recordingShareLevel: 'summary',
    },
  ]
  answers.ListCirclePeople = [{ userId: 'kim-id', username: 'kim', name: 'Kim', avatarUrl: null, avatarColorIndex: 1 }]
  answers.ListCircles = []
  answers.ShareRecording = { id: 'share-2' }
  answers.UnshareRecording = true
  inRouter(<RecordingShareDialog recordingId="recording-1" onClose={() => {}} />)
  expect(await screen.findByText('Sam')).toBeTruthy()

  // Ending a share asks first, and nothing is sent until it is confirmed.
  fireEvent.click(screen.getByRole('button', { name: 'Stop sharing with Sam' }))
  const confirmation = await screen.findByRole('alertdialog')
  expect(vi.mocked(graphql)).not.toHaveBeenCalledWith(expect.stringContaining('UnshareRecording'), expect.anything())
  fireEvent.click(within(confirmation).getByRole('button', { name: 'Stop sharing' }))
  await waitFor(() =>
    expect(vi.mocked(graphql)).toHaveBeenCalledWith(expect.stringContaining('UnshareRecording'), {
      recordingShareId: 'share-1',
    }),
  )

  const selects = await screen.findAllByRole('combobox')
  fireEvent.change(selects[0], { target: { value: 'user:kim-id' } })
  fireEvent.change(selects[1], { target: { value: 'transcript' } })
  fireEvent.click(screen.getByRole('button', { name: 'Share' }))
  await waitFor(() =>
    expect(vi.mocked(graphql)).toHaveBeenCalledWith(expect.stringContaining('ShareRecording('), {
      recordingId: 'recording-1',
      granteeUserId: 'kim-id',
      granteeCircleId: null,
      recordingShareLevel: 'transcript',
    }),
  )
})

it('draws nothing in the library when nobody shared a recording, and lists what was shared otherwise', async () => {
  answers.ListSharedRecordings = []
  const { container } = inRouter(<SharedRecordingsSection userId="me" />)
  await waitFor(() => expect(vi.mocked(graphql)).toHaveBeenCalled())
  expect(container.querySelector('.settings-section')).toBeNull()
  cleanup()

  answers.ListSharedRecordings = [summaryOnly]
  inRouter(<SharedRecordingsSection userId="me" />)
  const link = await screen.findByRole('link', { name: 'Standup' })
  expect(link.getAttribute('href')).toBe('/recordings/shared/recording-1')
  expect(screen.getByText(/Shared by Alex/)).toBeTruthy()
  expect(screen.getByText('Summary')).toBeTruthy()
})

function readPage(shared: SharedRecording) {
  answers.ReadSharedRecording = shared
  return inRouter(
    <Routes>
      <Route path="/recordings/shared/:recordingId" element={<SharedRecordingPage />} />
    </Routes>,
    '/recordings/shared/recording-1',
  )
}

it('shows a recording shared at summary with its summary and speakers and no transcript', async () => {
  readPage(summaryOnly)
  expect(await screen.findByText('Two items were agreed.')).toBeTruthy()
  expect(screen.getByText('Speaker: Alex')).toBeTruthy()
  expect(screen.getByText('The transcript was not shared.')).toBeTruthy()
  expect(document.querySelector('.recording-turn')).toBeNull()
  // The page only reads: nothing to play, name or change.
  expect(screen.queryByRole('button', { name: /play/i })).toBeNull()
})

it('shows a recording shared at transcript as turns without the filler, and pages it', async () => {
  readPage(withTranscript)
  expect(await screen.findByText('We ship on Friday. After the review.')).toBeTruthy()
  expect(screen.queryByText(/Mm-hmm/)).toBeNull()
  const turns = document.querySelectorAll('.recording-turn')
  expect(turns).toHaveLength(2)
  expect(within(turns[1] as HTMLElement).getByText('Speaker 1')).toBeTruthy()
  expect(within(turns[1] as HTMLElement).getByText('1:05')).toBeTruthy()
  // More than one page: the way on is offered, and asks for the next passages.
  fireEvent.click(await screen.findByRole('button', { name: 'Next page' }))
  await waitFor(() =>
    expect(vi.mocked(graphql)).toHaveBeenCalledWith(expect.stringContaining('ReadSharedRecording('), {
      recordingId: 'recording-1',
      passageOffset: 50,
      pageSize: 50,
    }),
  )
})

it('says a recording that is not shared is not found', async () => {
  vi.mocked(graphql).mockRejectedValue(new Error('not found'))
  inRouter(
    <Routes>
      <Route path="/recordings/shared/:recordingId" element={<SharedRecordingPage />} />
    </Routes>,
    '/recordings/shared/nobody',
  )
  expect(await screen.findByText('Not found.')).toBeTruthy()
})

it('groups the passages of one speaker in a row and leaves the filler out', () => {
  const turns = groupSharedPassages(withTranscript.passages)
  expect(turns.map((turn) => [turn.speakerName, turn.passages.length])).toEqual([
    ['Alex', 2],
    ['Speaker 1', 1],
  ])
  expect(groupSharedPassages([])).toEqual([])
})
