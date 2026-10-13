import { expect, it, vi } from 'vitest'
import { RecordingPassagePlayback } from './playback'

function mediaPlayer() {
  const player = new EventTarget() as HTMLMediaElement
  player.currentTime = 0
  player.pause = vi.fn()
  player.play = vi.fn(async () => {})
  return player
}

it('seeks the correct original by its offset and stops at the exact passage end', async () => {
  const playback = new RecordingPassagePlayback()
  const audio = mediaPlayer()
  const video = mediaPlayer()
  playback.register('audio-original', audio)
  playback.register('video-original', video)
  await playback.play('video-original', 6200, 8800, 4000)
  expect(video.currentTime).toBe(2.2)
  expect(video.play).toHaveBeenCalledOnce()
  expect(audio.play).not.toHaveBeenCalled()
  video.currentTime = 4.9
  video.dispatchEvent(new Event('timeupdate'))
  expect(video.currentTime).toBe(4.8)
  expect(video.pause).toHaveBeenCalled()
})

it('cancels stale navigation and superseded rejection without pausing the new selection', async () => {
  const playback = new RecordingPassagePlayback()
  const player = mediaPlayer()
  let rejectPlay: (caught: Error) => void = () => {}
  vi.mocked(player.play).mockImplementationOnce(
    () =>
      new Promise((_resolve, reject) => {
        rejectPlay = reject
      }),
  )
  playback.register('original', player)
  const oldPlayback = playback.play('original', 1000, 2000, 0)
  await playback.play('original', 3000, 4000, 0)
  vi.mocked(player.pause).mockClear()
  rejectPlay(new Error('Aborted superseded play'))
  await oldPlayback
  expect(player.pause).not.toHaveBeenCalled()
  playback.stop()
  player.currentTime = 8
  player.dispatchEvent(new Event('timeupdate'))
  expect(player.currentTime).toBe(8)
  expect(player.pause).toHaveBeenCalledOnce()
})

it('refuses unavailable or invalid passages rather than selecting a different original', async () => {
  const playback = new RecordingPassagePlayback()
  const player = mediaPlayer()
  playback.register('owned-original', player)
  await expect(playback.play('missing-original', 1000, 2000, 0)).rejects.toThrow()
  await expect(playback.play('owned-original', 1000, 2000, 4000)).rejects.toThrow()
  await expect(playback.play('owned-original', 1000, 1000, 0)).rejects.toThrow()
  expect(player.play).not.toHaveBeenCalled()
  playback.register('owned-original', null)
  await expect(playback.play('owned-original', 1000, 2000, 0)).rejects.toThrow()
})

it('uses a prepared preview for a user-selected passage with the same absolute offset and end boundary', async () => {
  const playback = new RecordingPassagePlayback()
  const original = mediaPlayer()
  const preview = mediaPlayer()
  const prepare = vi.fn(async () => preview)
  playback.register('original', original)
  playback.registerFallback('original', { prepare, isOriginalUnsupported: () => true })
  await playback.play('original', 6200, 8800, 4000)
  expect(prepare).toHaveBeenCalledOnce()
  expect(original.play).not.toHaveBeenCalled()
  expect(preview.currentTime).toBe(2.2)
  expect(preview.play).toHaveBeenCalledOnce()
  preview.currentTime = 5
  preview.dispatchEvent(new Event('timeupdate'))
  expect(preview.currentTime).toBe(4.8)
  expect(preview.pause).toHaveBeenCalled()
})
it('does not prepare for a supported original or a network playback failure', async () => {
  const playback = new RecordingPassagePlayback()
  const original = mediaPlayer()
  const prepare = vi.fn(async () => mediaPlayer())
  playback.register('original', original)
  playback.registerFallback('original', { prepare, isOriginalUnsupported: () => false })
  await playback.play('original', 1000, 2000, 0)
  expect(prepare).not.toHaveBeenCalled()
  vi.mocked(original.play).mockRejectedValueOnce(new Error('Network failed'))
  await expect(playback.play('original', 1000, 2000, 0)).rejects.toThrow('Network failed')
  expect(prepare).not.toHaveBeenCalled()
})
it('does not play a prepared preview after another selection or navigation supersedes the user intent', async () => {
  const playback = new RecordingPassagePlayback()
  const original = mediaPlayer()
  const preview = mediaPlayer()
  let resolvePreparation: (player: HTMLMediaElement) => void = () => {}
  const prepare = () =>
    new Promise<HTMLMediaElement>((resolve) => {
      resolvePreparation = resolve
    })
  playback.register('original', original)
  playback.registerFallback('original', { prepare, isOriginalUnsupported: () => true })
  const oldIntent = playback.playCompatible('original')
  playback.stop()
  resolvePreparation(preview)
  await oldIntent
  expect(preview.play).not.toHaveBeenCalled()
})
