// Copying a secret and clearing it again. A service worker has no clipboard,
// so an offscreen document (offscreen.html) does the copying and the reading
// back; this file asks it to, and keeps the hash of what was copied in
// chrome.storage.session until the alarm that clears it goes off.

import { hashClipboardText, makeClipboardSalt } from './clipboardHash'

export const CLEAR_CLIPBOARD_ALARM = 'passwords-clear-clipboard'
export const CLIPBOARD_CLEAR_MILLISECONDS = 30_000
const CLIPBOARD_STORAGE_KEY = 'passwordsClipboard'

// Only one offscreen document can exist, and a copy and the clearing of an
// earlier copy can come at once. Each piece of work with the document (make
// it, talk to it, close it) runs to the end before the next one starts, so
// one never closes the document another is about to use, and two never both
// try to make it.
let offscreenQueue: Promise<unknown> = Promise.resolve()

function inOffscreenQueue<Result>(work: () => Promise<Result>): Promise<Result> {
  const result = offscreenQueue.then(work, work)
  offscreenQueue = result.catch(() => undefined)
  return result
}

async function ensureOffscreenDocument(): Promise<void> {
  const contexts = await chrome.runtime.getContexts({ contextTypes: ['OFFSCREEN_DOCUMENT'] })
  if (contexts.length > 0) return
  await chrome.offscreen.createDocument({
    url: 'offscreen.html',
    reasons: ['CLIPBOARD'],
    justification: 'Copy a password or code, and clear the clipboard again if nothing else was copied.',
  })
}

async function closeOffscreenDocument(): Promise<void> {
  try {
    await chrome.offscreen.closeDocument()
  } catch {
    // Already closed.
  }
}

// copySecret puts text on the clipboard and arranges for it to be cleared in
// 30 seconds if it is still there.
export function copySecret(text: string): Promise<void> {
  return inOffscreenQueue(() => copySecretNow(text))
}

async function copySecretNow(text: string): Promise<void> {
  await ensureOffscreenDocument()
  const answer = await chrome.runtime.sendMessage({ target: 'offscreen', type: 'copy', text })
  if (!answer || answer.isCopied !== true) {
    await closeOffscreenDocument()
    throw new Error('The browser did not allow copying.')
  }
  const salt = makeClipboardSalt()
  await chrome.storage.session.set({ [CLIPBOARD_STORAGE_KEY]: { salt, hash: await hashClipboardText(salt, text) } })
  await chrome.alarms.create(CLEAR_CLIPBOARD_ALARM, { when: Date.now() + CLIPBOARD_CLEAR_MILLISECONDS })
  await closeOffscreenDocument()
}

// clearClipboardIfUnchanged empties the clipboard if it still holds what was
// copied, and forgets the hash either way.
export function clearClipboardIfUnchanged(): Promise<void> {
  return inOffscreenQueue(clearClipboardIfUnchangedNow)
}

async function clearClipboardIfUnchangedNow(): Promise<void> {
  const stored = (await chrome.storage.session.get(CLIPBOARD_STORAGE_KEY))[CLIPBOARD_STORAGE_KEY]
  await chrome.storage.session.remove(CLIPBOARD_STORAGE_KEY)
  if (!stored || typeof stored.salt !== 'string' || typeof stored.hash !== 'string') return
  try {
    await ensureOffscreenDocument()
    await chrome.runtime.sendMessage({
      target: 'offscreen',
      type: 'clear-if-unchanged',
      salt: stored.salt,
      hash: stored.hash,
    })
  } finally {
    await closeOffscreenDocument()
  }
}
