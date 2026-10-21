// The offscreen document: the only place the extension touches the clipboard
// (a service worker has none). It copies on request and, thirty seconds
// later, reads the clipboard back and empties it if it still holds what was
// copied. The copy and the read use a text area and execCommand, which is how
// an offscreen document with the clipboard reason is meant to do it.

import { hashClipboardText } from './clipboardHash'

function copyText(text: string): boolean {
  const area = document.createElement('textarea')
  area.value = text
  document.body.append(area)
  area.select()
  const isCopied = document.execCommand('copy')
  area.remove()
  return isCopied
}

function readText(): string {
  const area = document.createElement('textarea')
  document.body.append(area)
  area.focus()
  document.execCommand('paste')
  const text = area.value
  area.remove()
  return text
}

// isFromServiceWorker says a message came from this extension's service
// worker and from nowhere else: not a content script (which has a tab), not a
// frame of a page, not another extension. The worker is the only one that
// asks for the clipboard.
export function isFromServiceWorker(sender: { id?: string; tab?: unknown; frameId?: number; url?: string }): boolean {
  if (!sender || sender.id !== chrome.runtime.id || sender.tab !== undefined) return false
  if (sender.frameId !== undefined && sender.frameId !== 0) return false
  // The worker's address, or none (a worker has no page to name).
  return sender.url === undefined || sender.url === chrome.runtime.getURL('background.js')
}

chrome.runtime.onMessage.addListener(
  (
    message: { target?: string; type?: string; text?: string; salt?: string; hash?: string },
    sender: { id?: string; tab?: unknown; frameId?: number; url?: string },
    sendResponse: (answer: unknown) => void,
  ) => {
    if (!message || message.target !== 'offscreen') return false
    if (!isFromServiceWorker(sender)) return false
    if (message.type === 'copy' && typeof message.text === 'string') {
      sendResponse({ isCopied: copyText(message.text) })
      return false
    }
    if (message.type === 'clear-if-unchanged' && typeof message.salt === 'string' && typeof message.hash === 'string') {
      const salt = message.salt
      const expected = message.hash
      void hashClipboardText(salt, readText()).then((hash) => {
        // A single space, because copying nothing leaves the clipboard as it was.
        if (hash === expected) copyText(' ')
        sendResponse({ isCleared: hash === expected })
      })
      return true
    }
    return false
  },
)
