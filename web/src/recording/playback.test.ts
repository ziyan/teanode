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

const twoPieces = () => {
  const playback = new RecordingPassagePlayback()
  const first = mediaPlayer()
  const second = mediaPlayer()
  playback.register('first', first)
  playback.register('second', second)
  // A gap of ten seconds between the pieces: 0-60 s, then 70-100 s.
  playback.configure([
    { mediaId: 'first', startMS: 0, endMS: 60000 },
    { mediaId: 'second', startMS: 70000, endMS: 100000 },
  ])
  return { playback, first, second }
}

it('seeks one timeline across pieces, to the right piece and offset', async () => {
  const { playback, first, second } = twoPieces()
  await playback.seekTo(75000, true)
  expect(second.currentTime).toBe(5)
  expect(second.play).toHaveBeenCalledOnce()
  expect(first.play).not.toHaveBeenCalled()
  expect(playback.getState()).toMatchObject({ activeMediaId: 'second', positionMS: 75000 })
  await playback.seekTo(20000, true)
  expect(first.currentTime).toBe(20)
  expect(second.pause).toHaveBeenCalled()
  expect(playback.getState()).toMatchObject({ activeMediaId: 'first', positionMS: 20000 })
})

it('skips the gap: a position in it lands at the start of the next piece', async () => {
  const { playback, second } = twoPieces()
  await playback.seekTo(65000, false)
  expect(playback.getState()).toMatchObject({ activeMediaId: 'second', positionMS: 70000, isPlaying: false })
  expect(second.currentTime).toBe(0)
  expect(second.play).not.toHaveBeenCalled()
})

it('goes on into the next piece when one ends, skipping the gap, and stops after the last', async () => {
  const { playback, first, second } = twoPieces()
  await playback.seekTo(55000, true)
  first.currentTime = 60
  first.dispatchEvent(new Event('ended'))
  await vi.waitFor(() => expect(second.play).toHaveBeenCalledOnce())
  expect(second.currentTime).toBe(0)
  expect(playback.getState().positionMS).toBe(70000)
  second.currentTime = 12
  second.dispatchEvent(new Event('timeupdate'))
  expect(playback.getState().positionMS).toBe(82000)
  second.dispatchEvent(new Event('ended'))
  await Promise.resolve()
  expect(first.play).toHaveBeenCalledTimes(1)
  expect(playback.getState().isPlaying).toBe(false)
})

it('does not continue into the next piece after a passage played from the processing panel', async () => {
  const { playback, first, second } = twoPieces()
  await playback.play('first', 58000, 60000, 0)
  first.dispatchEvent(new Event('ended'))
  await Promise.resolve()
  expect(second.play).not.toHaveBeenCalled()
})

it('refuses to seek when there are no pieces', async () => {
  const playback = new RecordingPassagePlayback()
  await expect(playback.seekTo(1000, true)).rejects.toThrow()
})

it('toggles between pausing and playing on from the position, and restarts after the end', async () => {
  const { playback, first } = twoPieces()
  await playback.seekTo(10000, true)
  first.dispatchEvent(new Event('play'))
  expect(playback.getState().isPlaying).toBe(true)
  await playback.toggle(100000)
  expect(first.pause).toHaveBeenCalled()
  first.dispatchEvent(new Event('pause'))
  expect(playback.getState().isPlaying).toBe(false)
  await playback.toggle(100000)
  expect(first.currentTime).toBe(10)
  await playback.seekTo(1000000, false)
  expect(playback.getState().positionMS).toBe(100000)
  await playback.toggle(100000)
  expect(first.currentTime).toBe(0)
})

it('plays a range from its start and stops at its end, without going on into the next piece', async () => {
  const { playback, first, second } = twoPieces()
  await playback.playRange(20000, 25000)
  expect(first.currentTime).toBe(20)
  first.currentTime = 25.2
  first.dispatchEvent(new Event('timeupdate'))
  expect(first.pause).toHaveBeenCalled()
  expect(first.currentTime).toBe(25)
  first.dispatchEvent(new Event('ended'))
  await Promise.resolve()
  expect(second.play).not.toHaveBeenCalled()
})

it('stops a range that runs past its piece at the end of that piece', async () => {
  const { playback, first, second } = twoPieces()
  await playback.playRange(50000, 90000)
  first.currentTime = 60
  first.dispatchEvent(new Event('timeupdate'))
  expect(first.pause).toHaveBeenCalled()
  first.dispatchEvent(new Event('ended'))
  await Promise.resolve()
  expect(second.play).not.toHaveBeenCalled()
})

it('plays a paused range on to the same end, and a seek frees it', async () => {
  const { playback, first } = twoPieces()
  await playback.playRange(20000, 25000)
  first.dispatchEvent(new Event('play'))
  first.currentTime = 22
  first.dispatchEvent(new Event('timeupdate'))
  await playback.toggle(100000)
  first.dispatchEvent(new Event('pause'))
  await playback.toggle(100000)
  expect(first.currentTime).toBe(22)
  first.currentTime = 25.1
  first.dispatchEvent(new Event('timeupdate'))
  expect(first.currentTime).toBe(25)
  await playback.seekTo(30000, true)
  first.currentTime = 40
  first.dispatchEvent(new Event('timeupdate'))
  expect(first.currentTime).toBe(40)
})
