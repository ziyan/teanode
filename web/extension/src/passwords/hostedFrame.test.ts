import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { FRAME_MESSAGE_CHANNEL } from './frameChannel'
import { FrameMessage, HostedFrame } from './hostedFrame'
import { giveLayout } from './testLayout'

const EXTENSION_ORIGIN = 'chrome-extension://abcdefghijklmnop'

let received: FrameMessage[]
let hosted: HostedFrame

function internalFrame(): HTMLIFrameElement {
  return (hosted as any).frame as HTMLIFrameElement
}

function post(source: unknown, origin: string, data: unknown) {
  window.dispatchEvent(new MessageEvent('message', { source: source as Window, origin, data }))
}

beforeEach(() => {
  giveLayout()
  vi.stubGlobal('chrome', { runtime: { getURL: (path: string) => `${EXTENSION_ORIGIN}/${path}` } })
  received = []
  hosted = new HostedFrame(document, {
    page: 'suggestion.html',
    widthPixels: 300,
    heightPixels: 100,
    anchor: null,
    title: 'TeaNode Passwords',
    onMessage: (message) => received.push(message),
  })
  hosted.show()
})
afterEach(() => {
  hosted.close()
  vi.unstubAllGlobals()
})

describe('HostedFrame', () => {
  it('puts the extension page in an iframe inside a closed shadow root', () => {
    expect(hosted.host.isConnected).toBe(true)
    expect(hosted.host.shadowRoot).toBeNull()
    expect(internalFrame().getAttribute('src')).toBe(`${EXTENSION_ORIGIN}/suggestion.html`)
  })

  it('sets the styles that keep it in view as important declarations', () => {
    for (const [name, value] of [
      ['opacity', '1'],
      ['visibility', 'visible'],
      ['display', 'block'],
      ['filter', 'none'],
      ['position', 'fixed'],
    ]) {
      expect(hosted.host.style.getPropertyValue(name)).toBe(value)
      expect(hosted.host.style.getPropertyPriority(name)).toBe('important')
    }
  })

  it("believes a message only from its own frame's window and the extension's origin", () => {
    const message = { channel: FRAME_MESSAGE_CHANNEL, kind: 'choose', itemId: 'x' }
    const frameWindow = internalFrame().contentWindow
    expect(frameWindow).not.toBeNull()
    // The page\'s own script, posting to its window.
    post(window, 'https://page.test', message)
    // Another window claiming the extension\'s origin cannot be the frame.
    post(window, EXTENSION_ORIGIN, message)
    // The frame\'s window from some other origin (it was navigated away).
    post(frameWindow, 'https://page.test', message)
    expect(received).toEqual([])
    post(frameWindow, EXTENSION_ORIGIN, message)
    expect(received).toEqual([message])
  })

  it('ignores a message of another channel, or one with no kind', () => {
    const frameWindow = internalFrame().contentWindow
    post(frameWindow, EXTENSION_ORIGIN, { channel: 'other', kind: 'choose' })
    post(frameWindow, EXTENSION_ORIGIN, { channel: FRAME_MESSAGE_CHANNEL })
    post(frameWindow, EXTENSION_ORIGIN, 'text')
    expect(received).toEqual([])
  })

  it('hears nothing once closed, and takes its element off the page', () => {
    const frameWindow = internalFrame().contentWindow
    hosted.close()
    post(frameWindow, EXTENSION_ORIGIN, { channel: FRAME_MESSAGE_CHANNEL, kind: 'choose' })
    expect(received).toEqual([])
    expect(hosted.host.isConnected).toBe(false)
    expect(hosted.check().isShown).toBe(false)
  })
})
