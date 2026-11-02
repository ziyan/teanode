import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useRef, useState } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import type { DictationCallbacks } from './dictation'
import { useDictation, useVoiceAvailability, withWordsAt } from './useDictation'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))

// The socket and the microphone are Dictation's own and tested in
// dictation.test.ts; here it is a thing that is told words.
const sessions: {
  callbacks: DictationCallbacks
  place: string
  start: ReturnType<typeof vi.fn>
  stop: ReturnType<typeof vi.fn>
  abort: ReturnType<typeof vi.fn>
}[] = []
vi.mock('./dictation', () => ({
  Dictation: class {
    callbacks: DictationCallbacks
    place = ''
    start = vi.fn(async (_sampleRate: number, place: string) => {
      this.place = place
    })
    stop = vi.fn()
    abort = vi.fn()
    constructor(callbacks: DictationCallbacks) {
      this.callbacks = callbacks
      sessions.push(this)
    }
  },
}))

beforeEach(() => {
  sessions.length = 0
  vi.mocked(graphql).mockReset()
  vi.mocked(graphql).mockResolvedValue({
    ReadAgentVoice: { isVoiceAvailable: true, isVoiceMessageAvailable: true, sampleRate: 24000 },
  } as never)
})
afterEach(cleanup)

// A box with a send button, as the drawer has: the words the person says go
// into it and nothing is sent.
let sent = 0
function Harness({ initial }: { initial: string }) {
  const [text, setText] = useState(initial)
  const input = useRef<HTMLTextAreaElement>(null)
  const voice = useVoiceAvailability()
  const dictation = useDictation({
    place: 'chat',
    input,
    value: text,
    sampleRate: voice.sampleRate,
    onInsert: setText,
  })
  return (
    <div>
      <textarea ref={input} aria-label="Message" value={text} onChange={(event) => setText(event.target.value)} />
      {voice.isDictationAvailable && (
        <button onClick={dictation.toggle}>{dictation.state === 'off' ? 'Dictate' : 'Stop dictating'}</button>
      )}
      <button onClick={() => (dictation.abort(), sent++)}>Send</button>
      <p>{dictation.captionText}</p>
    </div>
  )
}

const show = (initial: string) =>
  render(
    <TranslationProvider>
      <ToastProvider>
        <Harness initial={initial} />
      </ToastProvider>
    </TranslationProvider>,
  )

const box = () => screen.getByLabelText('Message') as HTMLTextAreaElement

it('puts each finished utterance at the cursor with a space, and never sends', async () => {
  sent = 0
  show('Call at six')
  fireEvent.click(await screen.findByRole('button', { name: 'Dictate' }))
  expect(sessions).toHaveLength(1)
  expect(sessions[0].start).toHaveBeenCalledWith(24000, 'chat')
  box().setSelectionRange(4, 4)
  act(() => sessions[0].callbacks.onWords('Mira about the cake'))
  expect(box().value).toBe('Call Mira about the cake at six')
  // The cursor follows the words, so the next utterance continues them.
  await waitFor(() => expect(box().selectionStart).toBe('Call Mira about the cake'.length))
  act(() => sessions[0].callbacks.onWords('before dinner'))
  expect(box().value).toBe('Call Mira about the cake before dinner at six')
  expect(sent).toBe(0)
  expect(vi.mocked(graphql).mock.calls.every(([document]) => !String(document).includes('AskAgent'))).toBe(true)
})

it('shows the words being heard while it listens', async () => {
  show('')
  fireEvent.click(await screen.findByRole('button', { name: 'Dictate' }))
  act(() => sessions[0].callbacks.onListening())
  act(() => sessions[0].callbacks.onCaption('remind me to water'))
  expect(screen.getByText('remind me to water')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Stop dictating' })).toBeTruthy()
})

it('offers no microphone when the server has no voice', async () => {
  vi.mocked(graphql).mockResolvedValue({
    ReadAgentVoice: { isVoiceAvailable: false, isVoiceMessageAvailable: true, sampleRate: 0 },
  } as never)
  show('')
  await waitFor(() => expect(graphql).toHaveBeenCalled())
  expect(screen.queryByRole('button', { name: 'Dictate' })).toBeNull()
})

it('stops on a second press, letting the words being spoken land', async () => {
  show('')
  fireEvent.click(await screen.findByRole('button', { name: 'Dictate' }))
  fireEvent.click(screen.getByRole('button', { name: 'Stop dictating' }))
  expect(sessions[0].stop).toHaveBeenCalledTimes(1)
  expect(sessions[0].abort).not.toHaveBeenCalled()
  act(() => sessions[0].callbacks.onWords('late words'))
  expect(box().value).toBe('late words')
})

it('stops at once on Escape', async () => {
  show('')
  fireEvent.click(await screen.findByRole('button', { name: 'Dictate' }))
  fireEvent.keyDown(document, { key: 'Escape' })
  expect(sessions[0].abort).toHaveBeenCalledTimes(1)
  expect(screen.getByRole('button', { name: 'Dictate' })).toBeTruthy()
})

it('stops when the box is sent', async () => {
  show('hello')
  fireEvent.click(await screen.findByRole('button', { name: 'Dictate' }))
  fireEvent.click(screen.getByRole('button', { name: 'Send' }))
  expect(sessions[0].abort).toHaveBeenCalledTimes(1)
})

it('stops when the box goes away', async () => {
  const view = show('')
  fireEvent.click(await screen.findByRole('button', { name: 'Dictate' }))
  view.unmount()
  expect(sessions[0].abort).toHaveBeenCalledTimes(1)
})

it('puts words where the cursor is, with a space only where one is missing', () => {
  expect(withWordsAt('', 0, 0, 'Hello')).toEqual({ text: 'Hello', caret: 5 })
  expect(withWordsAt('Hello', 5, 5, 'there')).toEqual({ text: 'Hello there', caret: 11 })
  expect(withWordsAt('Hello ', 6, 6, 'there')).toEqual({ text: 'Hello there', caret: 11 })
  expect(withWordsAt('Hello world', 5, 5, 'big')).toEqual({ text: 'Hello big world', caret: 9 })
  expect(withWordsAt('Hello cruel world', 6, 11, 'big')).toEqual({ text: 'Hello big world', caret: 9 })
  expect(withWordsAt('a\n', 2, 2, 'b')).toEqual({ text: 'a\nb', caret: 3 })
})
