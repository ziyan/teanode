import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { type RecordingMedia } from './api'
import { RecordingTimelinePlayer } from './timelinePlayer'
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
        <RecordingTimelinePlayer mediaFiles={[original]} timelineDurationMS={5000} passagePlayback={playback} />
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
  // Expected, not an error: a quiet hint and the button, never the red message.
  expect(screen.queryByText(/Download it to use a compatible player/)).toBeNull()
  expect(screen.getByText(/An audio preview plays here instead/)).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Play compatible audio' }))
  await waitFor(() =>
    expect(view.container.querySelector('audio')?.getAttribute('src')).toBe(playbackReceipt.playbackPath),
  )
  fireEvent.loadedMetadata(view.container.querySelector('audio')!)
  await waitFor(() => expect(HTMLMediaElement.prototype.play).toHaveBeenCalledOnce())
  expect(graphql).toHaveBeenCalledOnce()
  expect(screen.queryByRole('button', { name: 'Use original' })).toBeNull()
  expect(screen.queryByRole('link', { name: 'Download original' })).toBeNull()
  // The preview only plays: nothing to download, and no note about it.
  expect(screen.queryByRole('link', { name: 'Download audio preview' })).toBeNull()
  expect(screen.queryByText(/video images are not included/)).toBeNull()
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
  expect(screen.queryByRole('link', { name: 'Download original' })).toBeNull()
  expect(pause).toHaveBeenCalled()
  expect(graphql).toHaveBeenCalledOnce()
})

it('shows one player for a recording made in pieces, with no divider, and seeks across them', async () => {
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue()
  vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {})
  vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably')
  const playback = new RecordingPassagePlayback()
  const seekTo = vi.spyOn(playback, 'seekTo')
  const pieces = [
    { ...media, id: 'piece-one', timelineStartMS: 0, mediaDurationMS: 60000 },
    { ...media, id: 'piece-two', timelineStartMS: 70000, mediaDurationMS: 30000 },
  ]
  const view = render(
    <TranslationProvider>
      <ToastProvider>
        <RecordingTimelinePlayer mediaFiles={pieces} timelineDurationMS={100000} passagePlayback={playback} />
      </ToastProvider>
    </TranslationProvider>,
  )
  expect(screen.getAllByRole('slider')).toHaveLength(1)
  expect(view.container.querySelectorAll('.recording-original')).toHaveLength(1)
  expect(view.container.querySelectorAll('.recording-scrubber-gap')).toHaveLength(1)
  expect(screen.getByText('1:40')).toBeTruthy()
  const slider = screen.getByRole('slider') as HTMLInputElement
  expect(slider.max).toBe('100000')
  fireEvent.change(slider, { target: { value: '75000' } })
  expect(seekTo).toHaveBeenCalledWith(75000, false)
  await waitFor(() => expect(playback.getState()).toMatchObject({ activeMediaId: 'piece-two', positionMS: 75000 }))
  fireEvent.click(screen.getByRole('button', { name: 'Play' }))
  await waitFor(() => expect(HTMLMediaElement.prototype.play).toHaveBeenCalledOnce())
})

it('loads only the active piece ahead of time', () => {
  const second: RecordingMedia = {
    ...media,
    id: 'fixture-media-2',
    clientMediaId: 'fixture-original-2',
    timelineStartMS: 6000,
  }
  const view = render(
    <TranslationProvider>
      <ToastProvider>
        <RecordingTimelinePlayer
          mediaFiles={[media, second]}
          timelineDurationMS={8000}
          passagePlayback={new RecordingPassagePlayback()}
        />
      </ToastProvider>
    </TranslationProvider>,
  )
  const preloads = [...view.container.querySelectorAll('audio')].map((element) => element.getAttribute('preload'))
  expect(preloads).toEqual(['metadata', 'none'])
})
