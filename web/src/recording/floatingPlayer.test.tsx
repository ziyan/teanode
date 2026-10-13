import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { RecordingFloatingPlayer } from './floatingPlayer'
import { RecordingPassagePlayback } from './playback'

afterEach(cleanup)

function mediaPlayer() {
  const player = new EventTarget() as HTMLMediaElement
  player.currentTime = 0
  player.pause = vi.fn()
  player.play = vi.fn(async () => {})
  return player
}

function show() {
  const playback = new RecordingPassagePlayback()
  const audio = mediaPlayer()
  playback.register('fixture-media', audio)
  playback.configure([{ mediaId: 'fixture-media', startMS: 0, endMS: 60000 }])
  render(
    <TranslationProvider>
      <ToastProvider>
        <input aria-label="Typing" />
        <RecordingFloatingPlayer passagePlayback={playback} timelineEndMS={60000} />
      </ToastProvider>
    </TranslationProvider>,
  )
  return { playback, audio }
}

it('appears when a bubble plays, pauses and plays on to the same end, and closes', async () => {
  const { playback, audio } = show()
  expect(screen.queryByRole('group', { name: 'Playback' })).toBeNull()
  await act(async () => {
    await playback.playRange(10000, 20000)
    audio.dispatchEvent(new Event('play'))
  })
  expect(screen.getByRole('group', { name: 'Playback' })).toBeTruthy()
  expect(screen.getByText('0:10')).toBeTruthy()
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: 'Pause' }))
    audio.dispatchEvent(new Event('pause'))
  })
  // Paused, the player stays so that playing can go on.
  expect(screen.getByRole('button', { name: 'Play' })).toBeTruthy()
  expect(audio.pause).toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: 'Stop playback' }))
  expect(screen.queryByRole('group', { name: 'Playback' })).toBeNull()
})

it('plays and pauses on Space, except while typing', async () => {
  const { playback, audio } = show()
  await act(async () => {
    await playback.playRange(10000, 20000)
    audio.dispatchEvent(new Event('play'))
  })
  vi.mocked(audio.pause).mockClear()
  fireEvent.keyDown(screen.getByLabelText('Typing'), { key: ' ' })
  expect(audio.pause).not.toHaveBeenCalled()
  await act(async () => {
    fireEvent.keyDown(document.body, { key: ' ' })
  })
  expect(audio.pause).toHaveBeenCalled()
})
