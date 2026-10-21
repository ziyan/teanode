import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { FRAME_CONNECT_KIND, FRAME_MESSAGE_CHANNEL, frameOriginOf, originOfAddress } from './frameChannel'
import { FrameMessage, HostedFrame } from './hostedFrame'
import { giveLayout } from './testLayout'

// The extension's static origin, which its pages' documents have, and the
// dynamic one chrome.runtime.getURL gives for them (use_dynamic_url): the
// frame's address is on the second, its document on the first.
const STATIC_ORIGIN = 'chrome-extension://abcdefghijklmnop'
const FRAME_ORIGIN = 'chrome-extension://dynamicdynamicdy'

let received: FrameMessage[]
let closedCount: number
let hosted: HostedFrame
let ports: MessagePort[]

function internalFrame(): HTMLIFrameElement {
  return (hosted as any).frame as HTMLIFrameElement
}

function makeHosted(): HostedFrame {
  return new HostedFrame(document, {
    page: 'suggestion.html',
    widthPixels: 300,
    heightPixels: 100,
    anchor: null,
    title: 'TeaNode Passwords',
    onMessage: (message) => received.push(message),
    onClose: () => (closedCount += 1),
  })
}

// load makes the frame finish loading, and returns what the content script
// posted to it: the connect message, the origin it was addressed to and the
// frame's end of the channel.
function load() {
  const frameWindow = internalFrame().contentWindow as Window
  const post = vi.spyOn(frameWindow, 'postMessage')
  internalFrame().dispatchEvent(new Event('load'))
  const [message, origin, transfer] = post.mock.calls[0] as unknown as [Record<string, unknown>, string, MessagePort[]]
  ports.push(transfer[0])
  return { message, origin, frameEnd: transfer[0], post }
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 10))

beforeEach(() => {
  giveLayout()
  vi.stubGlobal('chrome', {
    runtime: {
      id: 'abcdefghijklmnop',
      getURL: (path: string) => (path === '' ? `${STATIC_ORIGIN}/` : `${FRAME_ORIGIN}/${path}`),
    },
  })
  received = []
  closedCount = 0
  ports = []
  hosted = makeHosted()
  hosted.show()
})
afterEach(() => {
  hosted.close()
  for (const port of ports) port.close()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('HostedFrame', () => {
  it('puts the extension page in an iframe inside a closed shadow root', () => {
    expect(hosted.host.isConnected).toBe(true)
    expect(hosted.host.shadowRoot).toBeNull()
    expect(internalFrame().getAttribute('src')).toBe(`${FRAME_ORIGIN}/suggestion.html`)
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

  it("posts a channel to the frame once it has loaded, addressed to the extension's own origin", () => {
    const { message, origin, frameEnd } = load()
    expect(message).toEqual({ channel: FRAME_MESSAGE_CHANNEL, frameMessageKind: FRAME_CONNECT_KIND })
    // The origin the frame's document has, by the extension's fixed id: not the
    // dynamic one the frame's address names (use_dynamic_url), which would
    // never be delivered to.
    expect(origin).toBe(STATIC_ORIGIN)
    expect(origin).not.toBe(FRAME_ORIGIN)
    expect(frameEnd).toBeInstanceOf(MessagePort)
  })

  it('posts the channel once, however many times the frame loads', () => {
    const { post } = load()
    internalFrame().dispatchEvent(new Event('load'))
    expect(post).toHaveBeenCalledTimes(1)
  })

  it('believes what comes through its channel, and nothing the window carries', async () => {
    const { frameEnd } = load()
    // The page\'s own script, posting to its window, and a window claiming the extension\'s origin.
    const message = { frameMessageKind: 'choose', itemId: 'x' }
    window.dispatchEvent(new MessageEvent('message', { source: window, origin: 'https://page.test', data: message }))
    window.dispatchEvent(new MessageEvent('message', { source: window, origin: FRAME_ORIGIN, data: message }))
    window.dispatchEvent(
      new MessageEvent('message', {
        source: internalFrame().contentWindow,
        origin: FRAME_ORIGIN,
        data: { channel: FRAME_MESSAGE_CHANNEL, ...message },
      }),
    )
    await settle()
    expect(received).toEqual([])
    frameEnd.postMessage(message)
    await settle()
    expect(received).toEqual([message])
  })

  it('ignores a message with no kind, and one that is not an object', async () => {
    const { frameEnd } = load()
    frameEnd.postMessage({ itemId: 'x' })
    frameEnd.postMessage('text')
    frameEnd.postMessage(null)
    await settle()
    expect(received).toEqual([])
  })

  it('tells the frame things through the channel', async () => {
    const { frameEnd } = load()
    const heard: unknown[] = []
    frameEnd.onmessage = (event) => heard.push(event.data)
    hosted.send({ frameMessageKind: 'render', answer: 1 })
    await settle()
    expect(heard).toEqual([{ frameMessageKind: 'render', answer: 1 }])
  })

  it('tells nobody before the frame has a channel', async () => {
    expect(() => hosted.send({ frameMessageKind: 'render' })).not.toThrow()
    const { frameEnd } = load()
    const heard: unknown[] = []
    frameEnd.onmessage = (event) => heard.push(event.data)
    await settle()
    expect(heard).toEqual([])
  })

  it('is closed when the frame never answers through the channel, and says so', () => {
    vi.useFakeTimers()
    load()
    vi.advanceTimersByTime(2999)
    expect(hosted.host.isConnected).toBe(true)
    vi.advanceTimersByTime(2)
    expect(hosted.host.isConnected).toBe(false)
    expect(closedCount).toBe(1)
  })

  it('stays open once the frame has said anything through the channel', async () => {
    const { frameEnd } = load()
    frameEnd.postMessage({ frameMessageKind: 'ready' })
    await settle()
    vi.useFakeTimers()
    vi.advanceTimersByTime(10_000)
    expect(hosted.host.isConnected).toBe(true)
    expect(closedCount).toBe(0)
  })

  it('hears nothing once closed, and takes its element off the page', async () => {
    const { frameEnd } = load()
    hosted.close()
    frameEnd.postMessage({ frameMessageKind: 'choose' })
    await settle()
    expect(received).toEqual([])
    expect(hosted.host.isConnected).toBe(false)
    expect(hosted.check().isShown).toBe(false)
    expect(hosted.check().failureReason).toBe('the frame is closed')
  })
})

describe('frameOriginOf', () => {
  it("is the extension's fixed origin, whatever address the frame was given", () => {
    vi.stubGlobal('chrome', { runtime: { id: 'abcdefghijklmnop' } })
    expect(frameOriginOf('chrome-extension://dynamicdynamicdy/suggestion.html')).toBe(
      'chrome-extension://abcdefghijklmnop',
    )
  })

  it('is the origin of the address where the extension has no id', () => {
    vi.stubGlobal('chrome', { runtime: {} })
    expect(frameOriginOf('chrome-extension://dynamicdynamicdy/suggestion.html')).toBe(
      'chrome-extension://dynamicdynamicdy',
    )
    vi.unstubAllGlobals()
  })
})

describe('originOfAddress', () => {
  it('is the origin of the address, and the scheme and host where the browser gives none', () => {
    expect(originOfAddress('https://example.test/page.html')).toBe('https://example.test')
    expect(originOfAddress('chrome-extension://abc/suggestion.html?x=1')).toBe('chrome-extension://abc')
  })
})
