import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { TranslationProvider } from '../i18n/i18n'
import { uploadFiles } from '../upload'
import { CHAT_COMPOSER_MODE_KEY, ChatVoiceComposer, useChatComposerMode } from './chatVoiceComposer'
import { ToastProvider } from './toast'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))
vi.mock('../upload', () => ({ uploadFiles: vi.fn() }))

// A microphone that records ten bytes, and the clock the recorder reads.
let now = 0
const stopTrack = vi.fn()
const getUserMedia = vi.fn()

class FakeMediaRecorder {
  static isTypeSupported(type: string) {
    return type.startsWith('audio/webm')
  }
  state: 'inactive' | 'recording' = 'inactive'
  mimeType = 'audio/webm;codecs=opus'
  ondataavailable: ((event: { data: Blob }) => void) | null = null
  onstop: (() => void) | null = null
  start() {
    this.state = 'recording'
  }
  stop() {
    this.state = 'inactive'
    this.ondataavailable?.({ data: new Blob(['0123456789']) })
    this.onstop?.()
  }
}

beforeEach(() => {
  now = 1000
  vi.spyOn(performance, 'now').mockImplementation(() => now)
  stopTrack.mockReset()
  getUserMedia.mockReset()
  getUserMedia.mockResolvedValue({ getTracks: () => [{ stop: stopTrack }], getAudioTracks: () => [] })
  Object.defineProperty(navigator, 'mediaDevices', { configurable: true, value: { getUserMedia } })
  Object.defineProperty(navigator, 'permissions', { configurable: true, value: undefined })
  vi.stubGlobal('MediaRecorder', FakeMediaRecorder)
  vi.mocked(uploadFiles).mockReset()
  vi.mocked(uploadFiles).mockReturnValue({
    promise: Promise.resolve({ attachments: [{ id: 'voice-1' }] }),
    cancel: () => undefined,
  })
  vi.mocked(graphql).mockReset()
  vi.mocked(graphql).mockResolvedValue({ AskAgent: { runId: '', conversationId: 'chat-1' } } as never)
  localStorage.clear()
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const onSent = vi.fn()
const onKeyboard = vi.fn()

function renderComposer(conversationId = 'chat-1') {
  onSent.mockReset()
  onKeyboard.mockReset()
  return render(
    <TranslationProvider>
      <ToastProvider>
        <ChatVoiceComposer
          conversationId={conversationId}
          references={[{ agentMessageId: 'message-7', quotedRole: 'assistant', quotedText: 'Dinner at six?' }]}
          surface="drawer"
          drawerId="drawer-1"
          onSent={onSent}
          onKeyboard={onKeyboard}
        />
      </ToastProvider>
    </TranslationProvider>,
  )
}

const holdButton = () => screen.getByRole('button', { name: 'Hold to talk' })

// The recording has started once the strip above the composer is drawn.
const recording = () => screen.findByText('Slide up to cancel')

it('keeps the composer mode in this browser, for every chat', () => {
  function Harness() {
    const [mode, setMode] = useChatComposerMode()
    return <button onClick={() => setMode(mode === 'voice' ? 'keyboard' : 'voice')}>{mode}</button>
  }
  const first = render(<Harness />)
  expect(screen.getByRole('button').textContent).toBe('keyboard')
  fireEvent.click(screen.getByRole('button'))
  expect(screen.getByRole('button').textContent).toBe('voice')
  expect(localStorage.getItem(CHAT_COMPOSER_MODE_KEY)).toBe('voice')
  first.unmount()
  render(<Harness />)
  expect(screen.getByRole('button').textContent).toBe('voice')
})

it('works where the browser keeps nothing', () => {
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
    throw new Error('blocked')
  })
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
    throw new Error('blocked')
  })
  function Harness() {
    const [mode, setMode] = useChatComposerMode()
    return <button onClick={() => setMode('voice')}>{mode}</button>
  }
  render(<Harness />)
  fireEvent.click(screen.getByRole('button'))
  expect(screen.getByRole('button').textContent).toBe('voice')
})

it('uploads once on release and asks with the recording', async () => {
  renderComposer()
  fireEvent.pointerDown(holdButton(), { pointerId: 1, clientY: 400, button: 0 })
  await recording()
  now += 1500
  fireEvent.pointerUp(holdButton(), { pointerId: 1, clientY: 400 })
  await waitFor(() => expect(graphql).toHaveBeenCalledTimes(1))
  expect(uploadFiles).toHaveBeenCalledTimes(1)
  const [method, address, files] = vi.mocked(uploadFiles).mock.calls[0]
  expect(method).toBe('POST')
  expect(address).toBe('/api/v1/agent/attachments?isVoiceMessage=true')
  expect(files).toHaveLength(1)
  expect(files[0].type).toBe('audio/webm')
  const [document, variables] = vi.mocked(graphql).mock.calls[0]
  expect(document).toContain('voiceMessageAttachmentId')
  expect(variables).toMatchObject({
    conversationId: 'chat-1',
    message: '',
    voiceMessageAttachmentId: 'voice-1',
    // The reply being composed goes with it.
    references: [{ agentMessageId: 'message-7' }],
  })
  await waitFor(() => expect(onSent).toHaveBeenCalledWith('chat-1'))
})

it('throws the recording away when the pointer slides up before release', async () => {
  renderComposer()
  fireEvent.pointerDown(holdButton(), { pointerId: 1, clientY: 400, button: 0 })
  await recording()
  // A little way up is still a hold.
  fireEvent.pointerMove(holdButton(), { pointerId: 1, clientY: 380 })
  expect(screen.getByText('Slide up to cancel')).toBeTruthy()
  fireEvent.pointerMove(holdButton(), { pointerId: 1, clientY: 330 })
  expect(screen.getByText('Release to cancel')).toBeTruthy()
  now += 2000
  fireEvent.pointerUp(holdButton(), { pointerId: 1, clientY: 330 })
  await waitFor(() => expect(screen.queryByText('Release to cancel')).toBeNull())
  expect(uploadFiles).not.toHaveBeenCalled()
  expect(graphql).not.toHaveBeenCalled()
  expect(stopTrack).toHaveBeenCalled()
})

it('records while Space is held, ignoring repeats, and sends on release', async () => {
  renderComposer()
  holdButton().focus()
  fireEvent.keyDown(holdButton(), { key: ' ' })
  fireEvent.keyDown(holdButton(), { key: ' ', repeat: true })
  fireEvent.keyDown(holdButton(), { key: ' ', repeat: true })
  await recording()
  expect(getUserMedia).toHaveBeenCalledTimes(1)
  now += 1200
  fireEvent.keyUp(holdButton(), { key: ' ' })
  await waitFor(() => expect(graphql).toHaveBeenCalledTimes(1))
  expect(uploadFiles).toHaveBeenCalledTimes(1)
})

it('records while Enter is held and sends on release', async () => {
  renderComposer()
  fireEvent.keyDown(holdButton(), { key: 'Enter' })
  await recording()
  now += 1200
  fireEvent.keyUp(holdButton(), { key: 'Enter' })
  await waitFor(() => expect(graphql).toHaveBeenCalledTimes(1))
})

it('cancels on Escape', async () => {
  renderComposer()
  fireEvent.keyDown(holdButton(), { key: ' ' })
  await recording()
  now += 2000
  fireEvent.keyDown(holdButton(), { key: 'Escape' })
  await waitFor(() => expect(screen.queryByText('Slide up to cancel')).toBeNull())
  fireEvent.keyUp(holdButton(), { key: ' ' })
  expect(uploadFiles).not.toHaveBeenCalled()
  expect(graphql).not.toHaveBeenCalled()
})

it('sends nothing under half a second and tells how to record', async () => {
  renderComposer()
  fireEvent.pointerDown(holdButton(), { pointerId: 1, clientY: 400, button: 0 })
  await recording()
  now += 200
  fireEvent.pointerUp(holdButton(), { pointerId: 1, clientY: 400 })
  expect(await screen.findByText('Hold to record, release to send')).toBeTruthy()
  expect(uploadFiles).not.toHaveBeenCalled()
  expect(graphql).not.toHaveBeenCalled()
})

it('only asks when the press made the browser ask for the microphone', async () => {
  Object.defineProperty(navigator, 'permissions', {
    configurable: true,
    value: { query: async () => ({ state: 'prompt' }) },
  })
  renderComposer()
  fireEvent.pointerDown(holdButton(), { pointerId: 1, clientY: 400, button: 0 })
  expect(await screen.findByText('Hold again to talk')).toBeTruthy()
  expect(screen.queryByText('Slide up to cancel')).toBeNull()
  fireEvent.pointerUp(holdButton(), { pointerId: 1, clientY: 400 })
  expect(uploadFiles).not.toHaveBeenCalled()
  expect(stopTrack).toHaveBeenCalled()
})

it('sends on its own at two minutes and counts down from 1:50', async () => {
  renderComposer()
  fireEvent.pointerDown(holdButton(), { pointerId: 1, clientY: 400, button: 0 })
  await recording()
  now += 112_000
  expect(await screen.findByText('0:08')).toBeTruthy()
  now += 9_000
  await waitFor(() => expect(graphql).toHaveBeenCalledTimes(1), { timeout: 2000 })
  expect(uploadFiles).toHaveBeenCalledTimes(1)
})

it('goes back to the keyboard', () => {
  renderComposer()
  fireEvent.click(screen.getByRole('button', { name: 'Use the keyboard' }))
  expect(onKeyboard).toHaveBeenCalledTimes(1)
})

it('lets go of the microphone when the chat changes', async () => {
  const view = renderComposer()
  fireEvent.pointerDown(holdButton(), { pointerId: 1, clientY: 400, button: 0 })
  await recording()
  act(() => {
    view.rerender(
      <TranslationProvider>
        <ToastProvider>
          <ChatVoiceComposer
            conversationId="chat-2"
            references={[]}
            surface="drawer"
            drawerId="drawer-1"
            onSent={onSent}
            onKeyboard={onKeyboard}
          />
        </ToastProvider>
      </TranslationProvider>,
    )
  })
  await waitFor(() => expect(screen.queryByText('Slide up to cancel')).toBeNull())
  expect(stopTrack).toHaveBeenCalled()
  expect(uploadFiles).not.toHaveBeenCalled()
})
