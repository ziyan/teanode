// A frame of the extension's own page (suggestion.html, bar.html) drawn over
// a website. The page around it is hostile as far as this goes: it can see the
// element the frame sits in, and style it. So the frame sits in a closed
// shadow root inside one host element that is placed in the top layer where
// the browser supports it, the host's styles are set as important inline
// declarations, and nothing the frame says is believed until the content
// script has checked the frame is really shown (frameVisibility.ts). A
// message is believed only if it comes from the frame's own window and the
// extension's own origin.

import { FRAME_MESSAGE_CHANNEL } from './frameChannel'
import { checkFrameShown, FrameCheck } from './frameVisibility'

export interface FrameMessage {
  channel: typeof FRAME_MESSAGE_CHANNEL
  kind: string
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
}

const MAXIMUM_Z_INDEX = '2147483647'
const EDGE_PIXELS = 8

export class HostedFrame {
  readonly host: HTMLElement
  private readonly frame: HTMLIFrameElement
  private readonly extensionOrigin: string
  private anchor: HTMLElement | null
  private heightPixels: number
  private widthPixels: number
  private isClosed = false
  private readonly listener: (event: MessageEvent) => void

  constructor(
    private readonly document: Document,
    private readonly options: HostedFrameOptions,
  ) {
    this.anchor = options.anchor
    this.heightPixels = options.heightPixels
    this.widthPixels = options.widthPixels
    // The extension's origin is its address without the trailing slash.
    this.extensionOrigin = chrome.runtime.getURL('').replace(/\/$/, '')
    this.host = document.createElement('div')
    const shadow = this.host.attachShadow({ mode: 'closed' })
    this.frame = document.createElement('iframe')
    this.frame.src = chrome.runtime.getURL(options.page)
    this.frame.title = options.title
    this.frame.setAttribute('allow', '')
    const style = document.createElement('style')
    style.textContent = 'iframe { border: 0; width: 100%; height: 100%; display: block; color-scheme: light dark; }'
    shadow.append(style, this.frame)
    this.applyHostStyles()
    this.listener = (event: MessageEvent) => this.receive(event)
    document.defaultView?.addEventListener('message', this.listener)
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
    return this.isClosed ? { isShown: false, reason: 'the frame is closed' } : checkFrameShown(this.host)
  }

  send(message: Record<string, unknown>) {
    this.frame.contentWindow?.postMessage({ channel: FRAME_MESSAGE_CHANNEL, ...message }, this.extensionOrigin)
  }

  private receive(event: MessageEvent) {
    // Only the frame's own window, from the extension's own origin. A page
    // script can post to this window but is neither.
    if (this.isClosed || event.source !== this.frame.contentWindow || event.origin !== this.extensionOrigin) return
    const data = event.data as FrameMessage | null
    if (!data || data.channel !== FRAME_MESSAGE_CHANNEL || typeof data.kind !== 'string') return
    this.options.onMessage(data)
  }

  close() {
    if (this.isClosed) return
    this.isClosed = true
    this.document.defaultView?.removeEventListener('message', this.listener)
    try {
      ;(this.host as any).hidePopover?.()
    } catch {
      // Not shown as a popover.
    }
    this.host.remove()
  }
}
