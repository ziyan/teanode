import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { graphql } from '../api'
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
