// What the two frames drawn over websites (the suggestion and the save bar)
// have in common: listening for what the content script sends, telling it
// things, and keeping its idea of the frame's height right. They send only
// messages to the window around them and call nothing of the extension: the
// service worker is reached by the content script, which checks the frame is
// in front of the person before it believes a click.

import { FRAME_MESSAGE_CHANNEL } from './frameChannel'
import { watchFrameVisibility } from './pageDom'

export interface FramedPage {
  // isClickAllowed: the browser says the frame has been fully visible for a moment.
  isClickAllowed: () => boolean
  tell: (message: Record<string, unknown>) => void
  reportHeight: () => void
}

export function startFramedPage(root: HTMLElement, onRender: (data: Record<string, unknown>) => void): FramedPage {
  document.body.classList.add('framed')
  const isClickAllowed = watchFrameVisibility(document.body)
  // The page around the frame is not known, so the message cannot name its
  // origin. It carries only choices the person made in the frame.
  const tell = (message: Record<string, unknown>) =>
    window.parent.postMessage({ channel: FRAME_MESSAGE_CHANNEL, ...message }, '*')
  // The card, and the room around it that its shadow needs.
  const reportHeight = () => tell({ kind: 'size', heightPixels: root.getBoundingClientRect().height + 16 })
  window.addEventListener('message', (event) => {
    if (event.source !== window.parent) return
    const data = event.data
    if (!data || data.channel !== FRAME_MESSAGE_CHANNEL || data.kind !== 'render') return
    onRender(data)
  })
  tell({ kind: 'ready' })
  return { isClickAllowed, tell, reportHeight }
}
