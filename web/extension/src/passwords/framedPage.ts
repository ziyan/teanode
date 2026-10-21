// What the two frames drawn over websites (the suggestion and the save bar)
// have in common: getting the channel the content script hands them,
// listening for what it sends, telling it things, and keeping its idea of the
// frame's height right. They send only messages through that channel and call
// nothing of the extension: the service worker is reached by the content
// script, which checks the frame is in front of the person before it believes
// a click.
//
// The channel arrives once, in a message from the window around the frame, and
// the first one that carries a port is taken; the listener is removed then.
// Everything after goes through the port. A page that got a port of its own
// to the frame first would be talking to the frame instead of the content
// script, which hears nothing from it and so acts on nothing.

import { FRAME_CONNECT_KIND, FRAME_MESSAGE_CHANNEL } from './frameChannel'
import { trust, watchFrameVisibility } from './pageDom'

export interface FramedPage {
  // isClickAllowed: the browser says the frame has been fully visible for a moment.
  isClickAllowed: () => boolean
  tell: (message: Record<string, unknown>) => void
  reportHeight: () => void
}

export function startFramedPage(root: HTMLElement, onMessage: (data: Record<string, unknown>) => void): FramedPage {
  document.body.classList.add('framed')
  const isClickAllowed = watchFrameVisibility(document.body)
  let port: MessagePort | null = null
  // What was told before the channel arrived, sent as soon as it does.
  const waiting: Record<string, unknown>[] = []
  const tell = (message: Record<string, unknown>) => {
    if (port) port.postMessage(message)
    else waiting.push(message)
  }
  const onConnect = (event: MessageEvent) => {
    if (port !== null || event.source !== window.parent) return
    const data = event.data
    if (!data || data.channel !== FRAME_MESSAGE_CHANNEL || data.frameMessageKind !== FRAME_CONNECT_KIND) return
    if (event.ports.length !== 1) return
    port = event.ports[0]
    window.removeEventListener('message', onConnect)
    port.onmessage = (portEvent: MessageEvent) => {
      const message = portEvent.data
      if (message && typeof message === 'object' && typeof message.frameMessageKind === 'string') onMessage(message)
    }
    for (const message of waiting.splice(0)) port.postMessage(message)
  }
  window.addEventListener('message', onConnect)
  // The card, and the room around it that its shadow needs.
  const reportHeight = () => tell({ frameMessageKind: 'size', heightPixels: root.getBoundingClientRect().height + 16 })
  tell({ frameMessageKind: 'ready' })
  return { isClickAllowed, tell, reportHeight }
}

// moveFocusWithKeys is the keyboard way through a frame's buttons: the arrow
// keys move between them, Escape asks to go back to the field the frame is
// drawn under, and Tab past the last button (or Shift+Tab before the first)
// leaves the frame. Only keys the person pressed count.
export function moveFocusWithKeys(root: HTMLElement, page: FramedPage): void {
  document.addEventListener('keydown', (event) => {
    if (!trust.isTrusted(event)) return
    const buttons = Array.from(root.querySelectorAll('button')).filter((button) => !button.disabled)
    const index = buttons.indexOf(document.activeElement as HTMLButtonElement)
    if (event.key === 'Escape') {
      event.preventDefault()
      page.tell({ frameMessageKind: 'escape' })
    } else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      if (buttons.length === 0) return
      event.preventDefault()
      const step = event.key === 'ArrowDown' ? 1 : -1
      buttons[(Math.max(index, 0) + step + buttons.length) % buttons.length].focus()
    } else if (event.key === 'Tab') {
      const isLeaving = event.shiftKey ? index <= 0 : index === buttons.length - 1
      if (!isLeaving) return
      event.preventDefault()
      page.tell({ frameMessageKind: 'leave' })
    }
  })
}

// focusFirstChoice puts the focus on the first thing to choose in the frame.
export function focusFirstChoice(root: HTMLElement): void {
  window.focus()
  const first = root.querySelector<HTMLButtonElement>('button.choice, button.primary') ?? root.querySelector('button')
  first?.focus()
}
