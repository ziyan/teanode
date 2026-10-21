// A frame of the extension's own page (suggestion.html, bar.html) drawn over
// a website. The page around it is hostile as far as this goes: it can see the
// element the frame sits in, and style it. So the frame sits in a closed
// shadow root inside one host element that is placed in the top layer where
// the browser supports it, the host's styles are set as important inline
// declarations, and nothing the frame says is believed until the content
// script has checked the frame is really shown (frameVisibility.ts).
//
// The frame and the content script talk over a MessageChannel. Once the
// frame has loaded, the content script posts one end of the channel to it,
// addressed to the extension's own origin (so a frame the page navigated
// elsewhere gets nothing), and the page's window carries no other
// message of ours. A frame that does not answer through the channel within a
// moment is closed. If a page managed to hand the frame a channel of its own
// first, the frame would be talking to the page and not to the content
// script, which then hears nothing, believes nothing and acts on nothing.

import { FRAME_CONNECT_KIND, FRAME_MESSAGE_CHANNEL, frameOriginOf } from './frameChannel'
import { checkFrameShown, FrameCheck } from './frameVisibility'

export interface FrameMessage {
  frameMessageKind: string
  [key: string]: unknown
}

export interface HostedFrameOptions {
  // page is the extension page the frame shows, for example "suggestion.html".
  page: string
  widthPixels: number
  heightPixels: number
  // anchor: drawn under this element; null draws it at the top right of the page.
  anchor: HTMLElement | null
  title: string
  onMessage: (message: FrameMessage) => void
  // onClose is called when the frame closes itself because it never answered.
  onClose?: () => void
}

const MAXIMUM_Z_INDEX = '2147483647'
const EDGE_PIXELS = 8
// How long a frame has, from when it was handed its channel, to say it is ready.
const CONNECT_TIMEOUT_MILLISECONDS = 3000

export class HostedFrame {
  readonly host: HTMLElement
  private readonly frame: HTMLIFrameElement
  private readonly frameOrigin: string
  private port: MessagePort | null = null
  private connectTimer: ReturnType<typeof setTimeout> | null = null
  private anchor: HTMLElement | null
  private heightPixels: number
  private widthPixels: number
  private isClosed = false

  constructor(
    private readonly document: Document,
    private readonly options: HostedFrameOptions,
  ) {
    this.anchor = options.anchor
    this.heightPixels = options.heightPixels
    this.widthPixels = options.widthPixels
    this.host = document.createElement('div')
    const shadow = this.host.attachShadow({ mode: 'closed' })
    this.frame = document.createElement('iframe')
    this.frame.src = chrome.runtime.getURL(options.page)
    // The origin the frame's document really has: the extension's own, by its
    // fixed id (see frameOriginOf for why the frame's address is not asked).
    this.frameOrigin = frameOriginOf(this.frame.src)
    this.frame.title = options.title
    this.frame.setAttribute('allow', '')
    const style = document.createElement('style')
    style.textContent = 'iframe { border: 0; width: 100%; height: 100%; display: block; color-scheme: light dark; }'
    shadow.append(style, this.frame)
    this.applyHostStyles()
    this.frame.addEventListener('load', () => this.connect())
  }

  private applyHostStyles() {
    const important = (name: string, value: string) => this.host.style.setProperty(name, value, 'important')
    this.host.style.cssText = ''
    important('all', 'initial')
    important('position', 'fixed')
    important('margin', '0')
    important('padding', '0')
    important('border', '0')
    important('background', 'transparent')
    important('opacity', '1')
    important('visibility', 'visible')
    important('display', 'block')
    important('overflow', 'visible')
    important('transform', 'none')
    important('filter', 'none')
    important('pointer-events', 'auto')
    important('z-index', MAXIMUM_Z_INDEX)
    important('color-scheme', 'light dark')
    this.place()
  }

  private place() {
    const view = this.document.defaultView
    if (!view) return
    const width = Math.min(this.widthPixels, view.innerWidth - 2 * EDGE_PIXELS)
    let left = view.innerWidth - width - EDGE_PIXELS
    let top = EDGE_PIXELS
    if (this.anchor) {
      const rect = this.anchor.getBoundingClientRect()
      left = Math.max(EDGE_PIXELS, Math.min(rect.left, view.innerWidth - width - EDGE_PIXELS))
      top = rect.bottom + 4
      if (top + this.heightPixels > view.innerHeight && rect.top - this.heightPixels - 4 >= 0) {
        top = rect.top - this.heightPixels - 4
      }
    }
    const put = (name: string, value: string) => this.host.style.setProperty(name, value, 'important')
    put('left', `${Math.round(left)}px`)
    put('top', `${Math.round(top)}px`)
    put('width', `${Math.round(width)}px`)
    put('height', `${Math.round(this.heightPixels)}px`)
  }

  // show puts the frame on the page.
  show() {
    this.document.documentElement.append(this.host)
    try {
      this.host.setAttribute('popover', 'manual')
      ;(this.host as any).showPopover?.()
    } catch {
      // No popover support: the frame stays an ordinary fixed element.
    }
  }

  reposition() {
    if (!this.isClosed) this.place()
  }

  resize(heightPixels: number) {
    this.heightPixels = Math.max(40, Math.min(heightPixels, 480))
    this.place()
  }

  anchoredTo(): HTMLElement | null {
    return this.anchor
  }

  // check says whether the frame is really shown right now.
  check(): FrameCheck {
    return this.isClosed ? { isShown: false, failureReason: 'the frame is closed' } : checkFrameShown(this.host)
  }

  // connect hands the frame one end of a new channel, once. Messages go
  // through the other end from then on.
  private connect() {
    const frameWindow = this.frame.contentWindow
    if (this.isClosed || this.port !== null || !frameWindow) return
    const channel = new MessageChannel()
    this.port = channel.port1
    channel.port1.onmessage = (event: MessageEvent) => this.receive(event.data)
    frameWindow.postMessage(
      { channel: FRAME_MESSAGE_CHANNEL, frameMessageKind: FRAME_CONNECT_KIND },
      this.frameOrigin,
      [channel.port2],
    )
    this.connectTimer = setTimeout(() => {
      this.close()
      this.options.onClose?.()
    }, CONNECT_TIMEOUT_MILLISECONDS)
  }

  // send tells the frame something. Before the frame has a channel there is
  // nobody to tell.
  send(message: Record<string, unknown>) {
    this.port?.postMessage(message)
  }

  // focusFrame moves the keyboard focus into the frame.
  focusFrame() {
    if (!this.isClosed) this.frame.focus()
  }

  private receive(data: unknown) {
    if (this.isClosed) return
    const message = data as FrameMessage | null
    if (!message || typeof message !== 'object' || typeof message.frameMessageKind !== 'string') return
    if (this.connectTimer !== null) {
      clearTimeout(this.connectTimer)
      this.connectTimer = null
    }
    this.options.onMessage(message)
  }

  close() {
    if (this.isClosed) return
    this.isClosed = true
    if (this.connectTimer !== null) clearTimeout(this.connectTimer)
    this.connectTimer = null
    this.port?.close()
    this.port = null
    try {
      ;(this.host as any).hidePopover?.()
    } catch {
      // Not shown as a popover.
    }
    this.host.remove()
  }
}
