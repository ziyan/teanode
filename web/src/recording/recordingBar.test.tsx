import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { TranslationProvider } from '../i18n/i18n'
import {
  IDLE_SNAPSHOT,
  getCaptureSnapshot,
  isCaptureBusy,
  subscribeToCaptureSnapshot,
  updateCaptureSnapshot,
  type CaptureSnapshot,
} from './captureSnapshot'
import { RecordingBarView } from './recordingBar'

afterEach(cleanup)

function show(changes: Partial<CaptureSnapshot>) {
  const handlers = { onPause: vi.fn(), onResume: vi.fn(), onStop: vi.fn(), onDismissError: vi.fn() }
  const { container } = render(
    <TranslationProvider>
      <MemoryRouter>
        <RecordingBarView snapshot={{ ...IDLE_SNAPSHOT, ...changes }} {...handlers} />
      </MemoryRouter>
    </TranslationProvider>,
  )
  return { container, ...handlers }
}

it('draws nothing when no recording is being made or sent', () => {
  expect(show({}).container.firstChild).toBeNull()
})

it('while recording shows the time and the newest words, and links to the live page', () => {
  show({ phase: 'recording', recordingId: 'fixture-recording', elapsedMS: 125_000, newestWords: 'and then we agreed' })
  expect(screen.getByText('2:05')).toBeTruthy()
  expect(screen.getByText('and then we agreed')).toBeTruthy()
  expect(screen.getByRole('link', { name: 'Open the recording that is being made' }).getAttribute('href')).toBe(
    '/recordings/fixture-recording',
  )
  expect(screen.getByRole('region', { name: 'Recording in progress' }).getAttribute('data-phase')).toBe('recording')
})

it('says it is listening before anything has been heard', () => {
  show({ phase: 'recording', recordingId: 'fixture-recording' })
  expect(screen.getByText('Listening…')).toBeTruthy()
})

it('pauses and stops with its two buttons', () => {
  const { onPause, onStop } = show({ phase: 'recording', recordingId: 'fixture-recording' })
  fireEvent.click(screen.getByRole('button', { name: 'Pause' }))
  fireEvent.click(screen.getByRole('button', { name: 'Stop recording' }))
  expect(onPause).toHaveBeenCalledTimes(1)
  expect(onStop).toHaveBeenCalledTimes(1)
})

it('while paused says so, keeps the time, and offers Resume instead of Pause', () => {
  const { onResume } = show({
    phase: 'paused',
    recordingId: 'fixture-recording',
    elapsedMS: 61_000,
    newestWords: 'old words',
  })
  expect(screen.getByText('Paused')).toBeTruthy()
  expect(screen.getByText('1:01')).toBeTruthy()
  expect(screen.queryByText('old words')).toBeNull()
  expect(screen.queryByRole('button', { name: 'Pause' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Resume' }))
  expect(onResume).toHaveBeenCalledTimes(1)
})

it('while saving the last piece has no controls to press twice', () => {
  show({ phase: 'stopping', recordingId: 'fixture-recording', elapsedMS: 9_000 })
  expect(screen.getByText('Saving the recording…')).toBeTruthy()
  expect(screen.queryByRole('button')).toBeNull()
})

it('after the stop keeps saying the recording is being uploaded until the server has every piece', () => {
  show({ phase: 'idle', pendingPieceCount: 2 })
  expect(screen.getByText('Uploading the recording…')).toBeTruthy()
  expect(screen.queryByRole('link')).toBeNull()
  expect(screen.queryByRole('button')).toBeNull()
})

it('warns when the browser cannot keep the pieces if the tab is closed', () => {
  show({ phase: 'idle', pendingPieceCount: 1, isStorageDurable: false })
  expect(screen.getByText(/cannot keep the recording/)).toBeTruthy()
})

it('shows an error that stopped a recording from starting, and lets it be dismissed', () => {
  const { onDismissError } = show({ errorMessage: 'The microphone could not be used.' })
  expect(screen.getByText('The microphone could not be used.')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Close' }))
  expect(onDismissError).toHaveBeenCalledTimes(1)
})

it('counts a recording in progress, and pieces not yet sent, as something that would be lost by leaving', () => {
  expect(isCaptureBusy(IDLE_SNAPSHOT)).toBe(false)
  expect(isCaptureBusy({ ...IDLE_SNAPSHOT, phase: 'paused' })).toBe(true)
  expect(isCaptureBusy({ ...IDLE_SNAPSHOT, pendingPieceCount: 1 })).toBe(true)
})

it('keeps one snapshot for the bar and tells nobody when nothing changed', () => {
  const listener = vi.fn()
  const unsubscribe = subscribeToCaptureSnapshot(listener)
  updateCaptureSnapshot({ newestWords: 'first' })
  updateCaptureSnapshot({ newestWords: 'first' })
  expect(getCaptureSnapshot().newestWords).toBe('first')
  expect(listener).toHaveBeenCalledTimes(1)
  unsubscribe()
  updateCaptureSnapshot(IDLE_SNAPSHOT)
})
