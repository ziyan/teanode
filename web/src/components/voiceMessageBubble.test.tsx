import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { TranslationProvider } from '../i18n/i18n'
import { chatLineOf, linesWithTranscript } from './agentChat'
import { ToastProvider } from './toast'
import { VoiceMessageBubble, waveformLevels } from './voiceMessageBubble'

// 48 levels rising from 0 to 235, as the server sends them.
const waveform = btoa(String.fromCharCode(...Array.from({ length: 48 }, (_, index) => index * 5)))

const show = (
  transcriptState: string,
  transcriptText = '',
  source: string | null = '/api/v1/agent/attachments/a-1',
  onTranscribe?: () => Promise<void>,
) =>
  render(
    <TranslationProvider>
      <ToastProvider>
        <VoiceMessageBubble
          source={source}
          durationMS={4200}
          waveform={waveform}
          transcriptText={transcriptText}
          transcriptState={transcriptState}
          onTranscribe={onTranscribe}
        />
      </ToastProvider>
    </TranslationProvider>,
  )

// jsdom does not play sound: a pause and a play that say so the way a
// browser does.
const play = vi.fn()
const pause = vi.fn()
beforeEach(() => {
  play.mockReset()
  pause.mockReset()
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockImplementation(function (this: HTMLMediaElement) {
    play(this)
    Object.defineProperty(this, 'paused', { configurable: true, value: false })
    this.dispatchEvent(new Event('play'))
    return Promise.resolve()
  })
  vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(function (this: HTMLMediaElement) {
    pause(this)
    Object.defineProperty(this, 'paused', { configurable: true, value: true })
    this.dispatchEvent(new Event('pause'))
  })
})
afterEach(cleanup)

it('offers to transcribe a message with no words, and says so while it is', async () => {
  let finish: () => void = () => {}
  const onTranscribe = vi.fn(() => new Promise<void>((resolve) => (finish = resolve)))
  show('off', '', '/api/v1/agent/attachments/a-1', onTranscribe)

  fireEvent.click(screen.getByRole('button', { name: 'Transcribe' }))

  expect(onTranscribe).toHaveBeenCalledTimes(1)
  expect(screen.getByText('Transcribing…')).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Transcribe' })).toBeNull()
  await act(async () => finish())
})

it('offers no transcribing of a message that has its words', () => {
  show('done', 'Running ten minutes late', '/api/v1/agent/attachments/a-1', async () => {})
  expect(screen.queryByRole('button', { name: 'Transcribe' })).toBeNull()
})

it('shows the words once they are heard', () => {
  show('done', 'Running ten minutes late, save me a seat')
  expect(screen.getByText('Running ten minutes late, save me a seat')).toBeTruthy()
})

it('says the words are coming while they are waited for', () => {
  show('waiting')
  expect(screen.getByText('Transcribing…')).toBeTruthy()
})

it('says when the words could not be made', () => {
  show('failed')
  expect(screen.getByText('Could not transcribe')).toBeTruthy()
})

it('shows no words at all when there is no transcript', () => {
  const { container } = show('off', 'left over')
  expect(screen.queryByText('left over')).toBeNull()
  expect(container.querySelector('.voice-bubble-transcript')?.textContent).toBe('')
})

it('shows how long it is and draws one bar for each level', () => {
  const { container } = show('done', 'Hello')
  expect(screen.getByText('0:04')).toBeTruthy()
  expect(container.querySelectorAll('.voice-bubble-bar')).toHaveLength(48)
  expect(waveformLevels(waveform)[47]).toBe(235)
  // A recording with no waveform is drawn flat, not left out.
  expect(waveformLevels('')).toHaveLength(48)
})

it('plays from a source that is fetched only when played, and fills as it plays', () => {
  const { container } = show('done', 'Hello')
  const audio = container.querySelector('audio') as HTMLAudioElement
  expect(audio.getAttribute('preload')).toBe('none')
  expect(audio.getAttribute('src')).toBe('/api/v1/agent/attachments/a-1')
  fireEvent.click(screen.getByRole('button', { name: 'Play the voice message' }))
  expect(play).toHaveBeenCalledTimes(1)
  expect(screen.getByRole('button', { name: 'Pause the voice message' })).toBeTruthy()
  audio.currentTime = 2.1
  fireEvent.timeUpdate(audio)
  expect(container.querySelectorAll('.voice-bubble-bar.is-played')).toHaveLength(24)
  expect(screen.getByText('0:02')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Pause the voice message' }))
  expect(pause).toHaveBeenCalledTimes(1)
})

it('plays one at a time', () => {
  const { container } = render(
    <TranslationProvider>
      <ToastProvider>
        {['a-1', 'a-2'].map((attachmentId) => (
          <VoiceMessageBubble
            key={attachmentId}
            source={`/api/v1/agent/attachments/${attachmentId}`}
            durationMS={3000}
            waveform={waveform}
            transcriptText=""
            transcriptState="off"
          />
        ))}
      </ToastProvider>
    </TranslationProvider>,
  )
  const [first, second] = Array.from(container.querySelectorAll('audio'))
  const buttons = screen.getAllByRole('button', { name: 'Play the voice message' })
  fireEvent.click(buttons[0])
  fireEvent.click(buttons[1])
  expect(pause).toHaveBeenCalledWith(first)
  expect(pause).not.toHaveBeenCalledWith(second)
})

it('seeks where the waveform is clicked', () => {
  const { container } = show('done', 'Hello')
  const audio = container.querySelector('audio') as HTMLAudioElement
  const wave = screen.getByRole('slider', { name: 'Position in the voice message' })
  vi.spyOn(wave, 'getBoundingClientRect').mockReturnValue({ left: 100, width: 200 } as DOMRect)
  fireEvent.click(wave, { clientX: 200 })
  expect(audio.currentTime).toBeCloseTo(2.1, 1)
})

it('cannot be played until a framed drawer has its signed address', () => {
  show('done', 'Hello', null)
  expect((screen.getByRole('button', { name: 'Play the voice message' }) as HTMLButtonElement).disabled).toBe(true)
})

it('puts the words on the line when the message comes back as edited, without marking it edited', () => {
  const line = chatLineOf({
    id: 'message-1',
    role: 'user',
    content: '',
    authorKind: 'person',
    isVoiceMessage: true,
    transcriptText: '',
    transcriptState: 'waiting',
    attachments: [{ id: 'a-1', name: 'voice-message.m4a', contentType: 'audio/mp4', size: 10, audioDurationMS: 4200 }],
  })
  expect(line.isVoiceMessage).toBe(true)
  expect(line.transcriptState).toBe('waiting')
  const [updated] = linesWithTranscript([line], 'message-1', 'See you at six', 'done')
  expect(updated).toMatchObject({ transcriptText: 'See you at six', transcriptState: 'done', editedAt: null })
  // Another message is left alone.
  expect(linesWithTranscript([line], 'message-2', 'x', 'done')[0]).toBe(line)
})
