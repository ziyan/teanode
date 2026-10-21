import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// The offscreen document answers the clipboard messages of the service worker
// and of nothing else: not a content script (it has a tab), not a page of the
// extension, not another extension.
let listener: (message: any, sender: any, sendResponse: (answer: unknown) => void) => unknown
let copied: string[]

beforeEach(async () => {
  vi.resetModules()
  copied = []
  vi.stubGlobal('chrome', {
    runtime: {
      id: 'ext',
      getURL: (path: string) => `chrome-extension://ext/${path}`,
      onMessage: {
        addListener: (added: typeof listener) => {
          listener = added
        },
      },
    },
  })
  ;(document as any).execCommand = vi.fn((command: string) => {
    if (command === 'copy') copied.push((document.querySelector('textarea') as HTMLTextAreaElement | null)?.value ?? '')
    return true
  })
  await import('./offscreen')
})
afterEach(() => {
  delete (document as any).execCommand
  vi.unstubAllGlobals()
})

const copy = { target: 'offscreen', type: 'copy', text: 'hunter2-plaintext' }

function ask(message: unknown, sender: unknown) {
  return new Promise<{ handled: boolean; answer?: unknown }>((resolve) => {
    const handled = listener(message, sender, (answer: unknown) => resolve({ handled: true, answer }))
    if (handled === false) resolve({ handled: false })
  })
}

describe('who the offscreen document answers', () => {
  it('copies for the service worker, which has no tab and names no page or its own address', async () => {
    expect(await ask(copy, { id: 'ext' })).toEqual({ handled: true, answer: { isCopied: true } })
    expect(await ask(copy, { id: 'ext', url: 'chrome-extension://ext/background.js' })).toEqual({
      handled: true,
      answer: { isCopied: true },
    })
    expect(copied).toEqual(['hunter2-plaintext', 'hunter2-plaintext'])
  })

  it('does not copy for a content script, a frame of a page, or another extension', async () => {
    for (const sender of [
      { id: 'ext', tab: { id: 5 }, url: 'https://shop.example.test/', frameId: 0 },
      { id: 'ext', url: 'https://shop.example.test/' },
      { id: 'ext', frameId: 3 },
      { id: 'someone-else' },
      {},
      undefined,
    ]) {
      expect(await ask(copy, sender), JSON.stringify(sender)).toEqual({ handled: false })
    }
    expect(copied).toEqual([])
  })

  it('does not clear the clipboard for them either', async () => {
    const clear = { target: 'offscreen', type: 'clear-if-unchanged', salt: 'a', hash: 'b' }
    expect(await ask(clear, { id: 'ext', tab: { id: 5 } })).toEqual({ handled: false })
  })

  it('leaves messages that are not for it alone', async () => {
    expect(await ask({ type: 'copy', text: 'x' }, { id: 'ext' })).toEqual({ handled: false })
  })
})
