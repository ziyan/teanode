import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { type RecordingMedia } from './api'
import { RecordingOriginalPlayer } from './originalPlayer'
import { RecordingPassagePlayback } from './playback'

vi.mock('../api', () => ({ graphql: vi.fn() }))
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.resetAllMocks()
})
const media: RecordingMedia = {
  id: 'fixture-media',
  recordingId: 'fixture-recording',
  clientMediaId: 'fixture-original',
  originalFilename: 'fixture.caf',
  originalContentType: 'audio/x-caf',
  originalSizeBytes: 32000,
  originalSha256: 'a'.repeat(64),
  timelineStartMS: 4000,
  mediaDurationMS: 1000,
  partCount: 1,
  recordingUploadState: 'complete',
  verifiedAt: new Date().toISOString(),
}
const playbackReceipt = {
  recordingMediaId: media.id,
  playbackPath: '/api/v1/recordings/media/fixture-media/playback',
  playbackContentType: 'audio/wav',
  playbackSizeBytes: 32044,
  playbackSha256: 'b'.repeat(64),
  playbackDurationMS: 1000,
  timelineStartMS: 4000,
}
function player(playback = new RecordingPassagePlayback(), original = media) {
  return (
    <TranslationProvider>
      <ToastProvider>
        <RecordingOriginalPlayer media={original} passagePlayback={playback} />
      </ToastProvider>
    </TranslationProvider>
  )
}
function unsupportedOriginal(container: HTMLElement) {
  const original = container.querySelector('audio')!
  Object.defineProperty(original, 'error', { value: { code: 4 } })
  fireEvent.error(original)
}

it('does not prepare on mount, metadata loading or unsupported-original detection, only an explicit play action', async () => {
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue()
  vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {})
  vi.mocked(graphql).mockResolvedValue({ PrepareRecordingPlayback: playbackReceipt })
  const view = render(player())
  expect(graphql).not.toHaveBeenCalled()
  unsupportedOriginal(view.container)
  expect(graphql).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: 'Play compatible audio' }))
  await waitFor(() =>
    expect(view.container.querySelector('audio')?.getAttribute('src')).toBe(playbackReceipt.playbackPath),
  )
  fireEvent.loadedMetadata(view.container.querySelector('audio')!)
  await waitFor(() => expect(HTMLMediaElement.prototype.play).toHaveBeenCalledOnce())
  expect(graphql).toHaveBeenCalledOnce()
  expect(screen.queryByRole('button', { name: 'Use original' })).toBeNull()
  expect(screen.queryByRole('link', { name: 'Download original' })).toBeNull()
  expect(screen.getByRole('link', { name: 'Download audio preview' }).getAttribute('href')).toBe(
    playbackReceipt.playbackPath,
  )
  expect(screen.getByRole('link', { name: 'Download audio preview' }).getAttribute('download')).toBe(
    'fixture.preview.wav',
  )
  expect(screen.getByText(/original remains unchanged; video images are not included/)).toBeTruthy()
})
it('keeps the original and explicit retry after a refused preparation, without guessing a paid transcription', async () => {
  vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {})
  vi.mocked(graphql).mockRejectedValue(new Error('Storage limit reached'))
  const view = render(player())
  unsupportedOriginal(view.container)
  fireEvent.click(screen.getByRole('button', { name: 'Play compatible audio' }))
  await screen.findByText('Storage limit reached')
  expect(view.container.querySelector('audio')?.getAttribute('src')).toBe('/api/v1/recordings/media/fixture-media')
  expect((screen.getByRole('button', { name: 'Play compatible audio' }) as HTMLButtonElement).disabled).toBe(false)
  expect(graphql).toHaveBeenCalledOnce()
})
it('refuses mismatched paths or timeline offsets before displaying a preview player', async () => {
  vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {})
  vi.mocked(graphql).mockResolvedValue({ PrepareRecordingPlayback: { ...playbackReceipt, timelineStartMS: 0 } })
  const view = render(player())
  unsupportedOriginal(view.container)
  fireEvent.click(screen.getByRole('button', { name: 'Play compatible audio' }))
  await screen.findByText(/Compatible audio does not match this original/)
  expect(view.container.querySelector('audio')?.getAttribute('src')).not.toContain('/playback')
})
it('aborts pending preparation on account or recording navigation without playing its late response', async () => {
  const play = vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue()
  vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {})
  let resolvePreparation: (response: unknown) => void = () => {}
  vi.mocked(graphql).mockImplementation(
    () =>
      new Promise((resolve) => {
        resolvePreparation = resolve
      }),
  )
  const view = render(player())
  unsupportedOriginal(view.container)
  fireEvent.click(screen.getByRole('button', { name: 'Play compatible audio' }))
  const signal = vi.mocked(graphql).mock.calls[0][2] as AbortSignal
  view.unmount()
  expect(signal.aborted).toBe(true)
  await act(async () => {
    resolvePreparation({ PrepareRecordingPlayback: playbackReceipt })
  })
  expect(play).not.toHaveBeenCalled()
})
it('keeps a supported video original by default and offers only an explicit audio-only conversion', () => {
  vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {})
  const view = render(player(new RecordingPassagePlayback(), { ...media, originalContentType: 'video/mp4' }))
  const original = view.container.querySelector('video')!
  Object.defineProperty(original, 'videoWidth', { value: 1920 })
  fireEvent.loadedMetadata(original)
  expect(view.container.querySelector('audio')).toBeNull()
  expect(screen.queryByRole('button', { name: 'Play compatible audio' })).toBeNull()
  expect(graphql).not.toHaveBeenCalled()
})

it('offers the audio preview only when the browser cannot play the original', () => {
  const canPlayType = vi.spyOn(HTMLMediaElement.prototype, 'canPlayType')
  canPlayType.mockReturnValue('probably')
  const playable = render(player())
  expect(screen.queryByRole('button', { name: 'Use audio preview' })).toBeNull()
  expect(screen.queryByText(/cannot play the original/)).toBeNull()
  // The element reporting an error is the other way to find out.
  fireEvent.error(playable.container.querySelector('audio')!)
  expect(screen.getByRole('button', { name: 'Use audio preview' })).toBeTruthy()
  playable.unmount()

  canPlayType.mockReturnValue('')
  render(player())
  expect(screen.getByRole('button', { name: 'Use audio preview' })).toBeTruthy()
  expect(screen.getByText(/An audio preview plays here instead/)).toBeTruthy()
})

it('switches video the browser cannot play to explicit audio preview and returns to the original without another preparation', async () => {
  const play = vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue()
  const pause = vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {})
  vi.mocked(graphql).mockResolvedValue({ PrepareRecordingPlayback: playbackReceipt })
  vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('')
  const view = render(player(new RecordingPassagePlayback(), { ...media, originalContentType: 'video/mp4' }))
  const original = view.container.querySelector('video')!
  Object.defineProperty(original, 'videoWidth', { value: 1920 })
  fireEvent.loadedMetadata(original)
  expect(graphql).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: 'Use audio preview' }))
  await waitFor(() =>
    expect(view.container.querySelector('audio')?.getAttribute('src')).toBe(playbackReceipt.playbackPath),
  )
  const returnButton = screen.getByRole('button', { name: 'Use original' }) as HTMLButtonElement
  expect(returnButton.disabled).toBe(true)
  fireEvent.loadedMetadata(view.container.querySelector('audio')!)
  await waitFor(() => expect(returnButton.disabled).toBe(false))
  expect(play).toHaveBeenCalledOnce()
  fireEvent.click(returnButton)
  expect(view.container.querySelector('video')?.getAttribute('src')).toBe('/api/v1/recordings/media/fixture-media')
  expect(view.container.querySelector('audio')).toBeNull()
  expect(screen.getByRole('link', { name: 'Download audio preview' }).getAttribute('href')).toBe(
    playbackReceipt.playbackPath,
  )
  expect(screen.queryByRole('link', { name: 'Download original' })).toBeNull()
  expect(pause).toHaveBeenCalled()
  expect(graphql).toHaveBeenCalledOnce()
})
