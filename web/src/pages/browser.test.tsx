import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { ToastProvider } from '../components/toast'
import { TranslationProvider } from '../i18n/i18n'
import { BrowserPage } from './browser'

vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))
const execute = vi.mocked(graphql)

// FakeWebSocket records what the page sends and lets a test deliver what the
// server would.
class FakeWebSocket {
  static readonly OPEN = 1
  static instances: FakeWebSocket[] = []
  readyState = 0
  sent: Record<string, unknown>[] = []
  onopen: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  onerror: (() => void) | null = null
  onclose: (() => void) | null = null

  constructor(readonly url: string) {
    FakeWebSocket.instances.push(this)
  }
  send(text: string) {
    this.sent.push(JSON.parse(text))
  }
  close() {
    this.readyState = 3
    act(() => this.onclose?.())
  }
  open() {
    this.readyState = FakeWebSocket.OPEN
    act(() => this.onopen?.())
  }
  deliver(message: Record<string, unknown>) {
    act(() => this.onmessage?.({ data: JSON.stringify(message) }))
  }
  // sentOfType is what was sent with that type, in order.
  sentOfType(type: string) {
    return this.sent.filter((message) => message.type === type)
  }
}

function isConnected(isBrowserConnected: boolean, request = '') {
  execute.mockResolvedValue({
    ReadAgentTab: {
      connected: isBrowserConnected,
      attached: isBrowserConnected,
      title: 'Orders',
      url: 'https://shop.example.com/orders',
    },
    ReadAgentWaiting: {
      interactions: request
        ? [
            { interactionKind: 'question', interactionText: 'Which color?' },
            { interactionKind: 'take_over', interactionText: request },
          ]
        : [],
    },
  })
}

// openSocket renders the page with a connected browser and opens the viewer
// socket, which has sent its hello by the time it returns.
async function openSocket(): Promise<FakeWebSocket> {
  isConnected(true)
  render(
    <TranslationProvider>
      <ToastProvider>
        <BrowserPage />
      </ToastProvider>
    </TranslationProvider>,
  )
  await waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1))
  const socket = FakeWebSocket.instances[0]
  socket.open()
  return socket
}

const tabs = [
  { tab: 7, title: 'Reports', url: 'https://agent.example.net/reports', isOwn: false, isCurrent: true },
  { tab: 3, title: 'Orders', url: 'https://shop.example.com/orders', isOwn: true, isCurrent: false },
]

function stateOf(fields: Record<string, unknown> = {}) {
  return { type: 'state', isConnected: true, canCast: true, tabs, watching: 7, controller: 'agent', ...fields }
}

function frameOf(fields: Record<string, unknown> = {}) {
  return { type: 'frame', tab: 7, data: 'AAAA', width: 1280, height: 720, ...fields }
}

beforeEach(() => {
  execute.mockReset()
  FakeWebSocket.instances = []
  vi.stubGlobal('WebSocket', FakeWebSocket)
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

it('says how to connect the extension, and opens no live view, while the browser is not connected', async () => {
  isConnected(false)
  render(
    <TranslationProvider>
      <ToastProvider>
        <BrowserPage />
      </ToastProvider>
    </TranslationProvider>,
  )
  await screen.findByText(/TeaNode extension in Chrome/)
  expect(FakeWebSocket.instances).toHaveLength(0)
})

it('opens the viewer socket on the same origin and sends hello', async () => {
  const socket = await openSocket()
  expect(socket.url).toBe(`ws://${window.location.host}/api/v1/agent/tab/view`)
  expect(socket.sent[0]).toEqual({ type: 'hello' })
  expect(screen.getByText('Connecting…')).toBeTruthy()
  socket.deliver(stateOf())
  expect(screen.queryByText('Connecting…')).toBeNull()
})

it("lists the person's own tab first, marks the agent's, and highlights the watched one", async () => {
  const socket = await openSocket()
  socket.deliver(stateOf())
  const rows = screen.getAllByRole('button', { name: /Orders|Reports/ })
  expect(rows.map((row) => row.querySelector('strong')?.textContent)).toEqual(['Orders', 'Reports'])
  expect(rows[0].textContent).toContain('Yours')
  expect(rows[1].textContent).toContain('Your agent is here')
  expect(rows[0].getAttribute('aria-current')).toBeNull()
  expect(rows[1].getAttribute('aria-current')).toBe('true')
})

it('watches the tab the agent is in when nothing is watched yet, and another when one is chosen', async () => {
  const socket = await openSocket()
  socket.deliver(stateOf({ watching: null }))
  expect(socket.sentOfType('watch')).toEqual([{ type: 'watch', tab: 7 }])
  // Another state that still names nothing does not ask again.
  socket.deliver(stateOf({ watching: null }))
  expect(socket.sentOfType('watch')).toHaveLength(1)
  socket.deliver(stateOf({ watching: 7 }))
  fireEvent.click(screen.getByRole('button', { name: /Orders/ }))
  expect(socket.sentOfType('watch')[1]).toEqual({ type: 'watch', tab: 3 })
})

it("draws the watched tab's frames as a picture and no other's", async () => {
  const socket = await openSocket()
  socket.deliver(stateOf())
  socket.deliver(frameOf({ tab: 3, data: 'ZZZZ' }))
  expect(screen.queryByRole('img')).toBeNull()
  socket.deliver(frameOf())
  const picture = screen.getByRole('img') as HTMLImageElement
  expect(picture.getAttribute('src')).toBe('data:image/jpeg;base64,AAAA')
  socket.deliver(frameOf({ data: 'BBBB' }))
  expect(picture.getAttribute('src')).toBe('data:image/jpeg;base64,BBBB')
})

it('asks for an updated extension instead of a picture when the tab cannot be cast', async () => {
  const socket = await openSocket()
  socket.deliver(stateOf({ canCast: false }))
  expect(screen.getByText('Update the TeaNode extension to watch this tab')).toBeTruthy()
  expect(screen.queryByRole('img')).toBeNull()
})

it('says who controls the tab and sends take_over from the one button', async () => {
  const socket = await openSocket()
  socket.deliver(stateOf())
  screen.getByText('Your agent is using this tab')
  fireEvent.click(screen.getByRole('button', { name: 'Take over' }))
  expect(socket.sentOfType('take_over')).toEqual([{ type: 'take_over' }])
  socket.deliver(stateOf({ controller: 'person' }))
  screen.getByText('You have control')
  expect(screen.queryByRole('button', { name: 'Take over' })).toBeNull()
})

// pictureOf delivers a frame and gives the drawn picture a size: 640 by 360
// at the window's corner, for a page of 1280 by 720 CSS pixels.
function pictureOf(socket: FakeWebSocket): HTMLImageElement {
  socket.deliver(frameOf())
  const picture = screen.getByRole('img') as HTMLImageElement
  vi.spyOn(picture, 'getBoundingClientRect').mockReturnValue({
    left: 0,
    top: 0,
    width: 640,
    height: 360,
    right: 640,
    bottom: 360,
    x: 0,
    y: 0,
    toJSON: () => ({}),
  })
  return picture
}

it('sends no input while the agent controls the tab, and does after the person takes over', async () => {
  const socket = await openSocket()
  socket.deliver(stateOf())
  const picture = pictureOf(socket)
  fireEvent.pointerDown(picture, { clientX: 320, clientY: 180, button: 0 })
  fireEvent.pointerUp(picture, { clientX: 320, clientY: 180, button: 0 })
  fireEvent.keyDown(picture, { key: 'a', code: 'KeyA' })
  fireEvent.paste(picture, { clipboardData: { getData: () => 'hello' } })
  expect(socket.sent.map((message) => message.type)).toEqual(['hello'])

  socket.deliver(stateOf({ controller: 'person' }))
  fireEvent.pointerDown(picture, { clientX: 320, clientY: 180, button: 0 })
  fireEvent.pointerUp(picture, { clientX: 320, clientY: 180, button: 0 })
  expect(socket.sentOfType('pointer')).toEqual([
    { type: 'pointer', event: 'down', x: 640, y: 360, button: 'left', clickCount: 1, modifiers: 0 },
    { type: 'pointer', event: 'up', x: 640, y: 360, button: 'left', clickCount: 1, modifiers: 0 },
  ])

  fireEvent.keyDown(picture, { key: 'Enter', code: 'Enter' })
  fireEvent.keyUp(picture, { key: 'Enter', code: 'Enter' })
  expect(socket.sentOfType('key')).toEqual([
    { type: 'key', event: 'down', key: 'Enter', code: 'Enter', keyCode: 13, text: '\r', modifiers: 0 },
    { type: 'key', event: 'up', key: 'Enter', code: 'Enter', keyCode: 13, text: '', modifiers: 0 },
  ])

  fireEvent.paste(picture, { clipboardData: { getData: () => 'hello' } })
  expect(socket.sentOfType('text')).toEqual([{ type: 'text', text: 'hello' }])
})

it('sends the wheel while the person controls the tab, and stops the dashboard scrolling', async () => {
  const socket = await openSocket()
  socket.deliver(stateOf({ controller: 'person' }))
  const picture = pictureOf(socket)
  const wheel = new WheelEvent('wheel', { clientX: 160, clientY: 90, deltaX: 0, deltaY: 120, cancelable: true })
  picture.dispatchEvent(wheel)
  expect(wheel.defaultPrevented).toBe(true)
  expect(socket.sentOfType('wheel')).toEqual([{ type: 'wheel', x: 320, y: 180, deltaX: 0, deltaY: 120 }])
})

it('sends the text typed in the phone keyboard field, and its Enter and Backspace as keys', async () => {
  const socket = await openSocket()
  socket.deliver(stateOf({ controller: 'person' }))
  pictureOf(socket)
  fireEvent.click(screen.getByRole('button', { name: 'Keyboard' }))
  const field = screen.getByRole('textbox', { name: 'Keyboard' }) as HTMLTextAreaElement
  expect(document.activeElement).toBe(field)
  field.value = 'hi'
  fireEvent.input(field)
  expect(socket.sentOfType('text')).toEqual([{ type: 'text', text: 'hi' }])
  expect(field.value).toBe('')
  fireEvent.keyDown(field, { key: 'Backspace', code: 'Backspace' })
  expect(socket.sentOfType('key')).toEqual([expect.objectContaining({ event: 'down', key: 'Backspace', keyCode: 8 })])
})

it('hands back with the typed note', async () => {
  const socket = await openSocket()
  socket.deliver(stateOf({ controller: 'person' }))
  fireEvent.click(screen.getByRole('button', { name: 'Hand back' }))
  const handBacks = () => execute.mock.calls.filter(([document]) => String(document).includes('HandBackAgentTab'))
  expect(handBacks()).toEqual([])
  fireEvent.change(screen.getByPlaceholderText('Anything the agent should know'), {
    target: { value: '  Signed in, carry on  ' },
  })
  fireEvent.click(screen.getByRole('button', { name: 'Hand back' }))
  await waitFor(() => expect(handBacks()).toHaveLength(1))
  expect(handBacks()[0][1]).toEqual({ note: 'Signed in, carry on' })
})

it('says why the view was refused and does not reconnect', async () => {
  vi.spyOn(Math, 'random').mockReturnValue(0)
  const socket = await openSocket()
  socket.deliver({ type: 'refused', reason: 'Attaching tabs is off on this server' })
  await screen.findByText('Attaching tabs is off on this server')
  socket.close()
  await new Promise((resolve) => window.setTimeout(resolve, 1200))
  expect(FakeWebSocket.instances).toHaveLength(1)
})

it('reconnects after the socket drops, and says it is connecting meanwhile', async () => {
  vi.spyOn(Math, 'random').mockReturnValue(0)
  const socket = await openSocket()
  socket.deliver(stateOf())
  socket.close()
  expect(screen.getByText('Connecting…')).toBeTruthy()
  await waitFor(() => expect(FakeWebSocket.instances).toHaveLength(2), { timeout: 2500 })
  FakeWebSocket.instances[1].open()
  expect(FakeWebSocket.instances[1].sent[0]).toEqual({ type: 'hello' })
})

it("shows the agent's request to take the tab over above the picture, and no Take over for an old extension", async () => {
  isConnected(true, 'Sign in with the code sent to your phone')
  render(
    <TranslationProvider>
      <ToastProvider>
        <BrowserPage />
      </ToastProvider>
    </TranslationProvider>,
  )
  await waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1))
  const socket = FakeWebSocket.instances[0]
  socket.open()
  socket.deliver(stateOf({ canCast: false }))
  expect(await screen.findByText('Sign in with the code sent to your phone')).toBeTruthy()
  expect(screen.queryByText('Which color?')).toBeNull()
  expect(screen.queryByRole('button', { name: 'Take over' })).toBeNull()
})

it("says the picture's size while the tab is held from a phone, and nothing once it is let go", async () => {
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: query.includes('max-width'),
    addEventListener() {},
    removeEventListener() {},
  }))
  const socket = await openSocket()
  socket.deliver(stateOf({ controller: 'person' }))
  pictureOf(socket)
  await waitFor(() => expect(socket.sentOfType('viewport').length).toBeGreaterThan(0))
  expect(socket.sentOfType('viewport')[0]).toMatchObject({ type: 'viewport' })
  socket.deliver(stateOf({ controller: 'agent' }))
  await waitFor(() => expect(socket.sentOfType('viewport').slice(-1)[0]).toEqual({ type: 'viewport', width: 0, height: 0 }))
})
