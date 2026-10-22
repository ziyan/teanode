import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { AGENT_ASK_EVENT, graphql, type AgentAskDetail } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { SessionProvider } from '../session'
import { importRecording, readRecording, type RecordingView } from '../recording/api'
import { RecordingsPage } from './recordings'
vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))
vi.mock('../recording/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../recording/api')>()),
  readRecording: vi.fn(),
  importRecording: vi.fn(),
}))
afterEach(cleanup)
it('resumes the existing native original without offering an unapplied rename', async () => {
  const view = {
    recording: {
      id: 'fixture-native-recording',
      userId: 'fixture-owner',
      recordingTitle: 'Fixture capture',
      captureGaps: [],
    },
    mediaFiles: [
      {
        id: 'fixture-native-media',
        recordingId: 'fixture-native-recording',
        originalFilename: 'fixture.wav',
        originalContentType: 'audio/wav',
        originalSizeBytes: 4,
        recordingUploadState: 'pending',
      },
    ],
    uploadParts: [],
  } as unknown as RecordingView
  vi.mocked(graphql).mockResolvedValue({ ListRecordings: [] })
  vi.mocked(readRecording).mockResolvedValue(view)
  vi.mocked(importRecording).mockResolvedValue(view)
  render(
    <TranslationProvider>
      <SessionProvider
        value={{
          userId: 'fixture-owner',
          authenticated: true,
          authenticationRequired: true,
          username: 'fixture',
          passkeysEnabled: false,
        }}
      >
        <ToastProvider>
          <MemoryRouter initialEntries={['/recordings/fixture-native-recording']}>
            <Routes>
              <Route path="/recordings/:recordingId" element={<RecordingsPage />} />
            </Routes>
          </MemoryRouter>
        </ToastProvider>
      </SessionProvider>
    </TranslationProvider>,
  )
  fireEvent.click(await screen.findByRole('button', { name: 'Resume upload' }))
  expect(screen.queryByRole('textbox', { name: 'Recording name' })).toBeNull()
  fireEvent.change(screen.getByLabelText('Original audio or video file'), {
    target: { files: [new File(['abcd'], 'fixture.wav', { type: 'audio/wav' })] },
  })
  fireEvent.click(screen.getByRole('dialog').querySelector('button[type="submit"]')!)
  await waitFor(() => expect(importRecording).toHaveBeenCalled())
  expect(vi.mocked(importRecording).mock.calls[0][5]).toEqual(view.mediaFiles[0])
})

// The agent is pointed at the recording on its page: the drawer is told its
// id and the title shown, and a page with no drawer to open goes to the
// agent's settings.
it('points the agent at the recording', async () => {
  const view = {
    recording: {
      id: 'fixture-recording',
      userId: 'fixture-owner',
      recordingTitle: 'Quarterly planning',
      captureGaps: [],
    },
    mediaFiles: [],
    uploadParts: [],
  } as unknown as RecordingView
  vi.mocked(graphql).mockResolvedValue({ ListRecordings: [] })
  vi.mocked(readRecording).mockResolvedValue(view)
  const references: AgentAskDetail['reference'][] = []
  const listener = (event: Event) => {
    const detail = (event as CustomEvent<AgentAskDetail>).detail
    references.push(detail.reference)
    detail.handled = true
  }
  window.addEventListener(AGENT_ASK_EVENT, listener)
  try {
    render(
      <TranslationProvider>
        <SessionProvider
          value={{
            userId: 'fixture-owner',
            authenticated: true,
            authenticationRequired: true,
            username: 'fixture',
            passkeysEnabled: false,
          }}
        >
          <ToastProvider>
            <MemoryRouter initialEntries={['/recordings/fixture-recording']}>
              <Routes>
                <Route path="/recordings/:recordingId" element={<RecordingsPage />} />
              </Routes>
            </MemoryRouter>
          </ToastProvider>
        </SessionProvider>
      </TranslationProvider>,
    )
    fireEvent.click(await screen.findByRole('button', { name: 'Ask the agent about this recording' }))
    expect(references).toEqual([{ recordingId: 'fixture-recording', recordingTitle: 'Quarterly planning' }])
  } finally {
    window.removeEventListener(AGENT_ASK_EVENT, listener)
  }
})

// A link to a recording that was deleted says so on the page, once: not in a
// toast over an empty page, and not in the server's words beside it.
it('says a deleted recording is not found', async () => {
  vi.mocked(graphql).mockResolvedValue({ ListRecordings: [] })
  vi.mocked(readRecording).mockRejectedValue(new Error('db: recording was deleted'))
  render(
    <TranslationProvider>
      <SessionProvider
        value={{
          userId: 'fixture-owner',
          authenticated: true,
          authenticationRequired: true,
          username: 'fixture',
          passkeysEnabled: false,
        }}
      >
        <ToastProvider>
          <MemoryRouter initialEntries={['/recordings/fixture-deleted-recording']}>
            <Routes>
              <Route path="/recordings/:recordingId" element={<RecordingsPage />} />
            </Routes>
          </MemoryRouter>
        </ToastProvider>
      </SessionProvider>
    </TranslationProvider>,
  )
  expect(await screen.findByText('Not found.')).toBeTruthy()
  expect(screen.queryByText(/recording was deleted/)).toBeNull()
})

// Sharing is in the recording's own menu, and opens the dialog that says the
// others who speak did not agree.
it('offers Share in the recording menu', async () => {
  const view = {
    recording: {
      id: 'fixture-recording',
      userId: 'fixture-owner',
      recordingTitle: 'Quarterly planning',
      captureGaps: [],
    },
    mediaFiles: [],
    uploadParts: [],
  } as unknown as RecordingView
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('ListRecordingShares')) return { ListRecordingShares: [] } as never
    return { ListRecordings: [], ListCirclePeople: [], ListCircles: [] } as never
  })
  vi.mocked(readRecording).mockResolvedValue(view)
  render(
    <TranslationProvider>
      <SessionProvider
        value={{
          userId: 'fixture-owner',
          authenticated: true,
          authenticationRequired: true,
          username: 'fixture',
          passkeysEnabled: false,
        }}
      >
        <ToastProvider>
          <MemoryRouter initialEntries={['/recordings/fixture-recording']}>
            <Routes>
              <Route path="/recordings/:recordingId" element={<RecordingsPage />} />
            </Routes>
          </MemoryRouter>
        </ToastProvider>
      </SessionProvider>
    </TranslationProvider>,
  )
  fireEvent.click(await screen.findByRole('button', { name: 'Recording actions' }))
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Share' }))
  expect(await screen.findByText(/have not agreed to it being shared/)).toBeTruthy()
})

// What other people shared is listed under the library, and there is nothing
// to list when nobody did.
it('lists the recordings shared with the person under the library', async () => {
  vi.mocked(graphql).mockImplementation(async (document: string) => {
    if (document.includes('ListSharedRecordings'))
      return {
        ListSharedRecordings: [
          {
            recordingId: 'shared-1',
            ownerUserId: 'alex-id',
            ownerUsername: 'alex',
            ownerName: 'Alex',
            recordingTitle: 'Standup',
            createdAt: '2030-01-02T10:00:00Z',
            recordedAt: null,
            timelineDurationMS: 60000,
            recordingSummary: '',
            recordingShareLevel: 'summary',
          },
        ],
      } as never
    return { ListRecordings: [] } as never
  })
  render(
    <TranslationProvider>
      <SessionProvider
        value={{
          userId: 'fixture-owner',
          authenticated: true,
          authenticationRequired: true,
          username: 'fixture',
          passkeysEnabled: false,
        }}
      >
        <ToastProvider>
          <MemoryRouter initialEntries={['/recordings']}>
            <Routes>
              <Route path="/recordings" element={<RecordingsPage />} />
            </Routes>
          </MemoryRouter>
        </ToastProvider>
      </SessionProvider>
    </TranslationProvider>,
  )
  expect(await screen.findByText('Shared with you')).toBeTruthy()
  expect(screen.getByRole('link', { name: 'Standup' }).getAttribute('href')).toBe('/recordings/shared/shared-1')
})
