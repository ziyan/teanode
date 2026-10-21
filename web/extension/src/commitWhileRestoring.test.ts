// @vitest-environment-options {"url": "https://shop.example.test/sign-in"}
import { afterEach, describe, expect, it, vi } from 'vitest'

// A navigation commit can be what wakes the service worker. The tabs the
// agent may act in are read back from the session storage after that, so the
// commit has to wait for them, or it is thought to be in a tab the agent
// cannot act in and is never reported to the server.

vi.mock('./passwords/background', () => ({}))
vi.mock('./passwords/agentFill', () => ({
  fillLoginForAgent: vi.fn(),
  fillOneTimeCodeForAgent: vi.fn(),
}))

afterEach(() => {
  vi.unstubAllGlobals()
  vi.resetModules()
})

async function startWorker(kept: Record<string, unknown>) {
  const listeners: Record<string, ((...args: any[]) => any)[]> = {}
  const event = (name: string) => ({
    addListener: (listener: (...args: any[]) => any) => {
      ;(listeners[name] ??= []).push(listener)
    },
    removeListener: vi.fn(),
  })
  let finishReading: () => void = () => {}
  const reading = new Promise<void>((resolve) => (finishReading = resolve))
  const saved: Record<string, unknown> = {}
  const sent: string[] = []
  class FakeSocket {
    static OPEN = 1
    readyState = 1
    send(text: string) {
      sent.push(text)
    }
    close() {}
    addEventListener() {}
  }
  vi.stubGlobal('WebSocket', FakeSocket)
  vi.stubGlobal('chrome', {
    storage: {
      session: {
        get: async () => {
          await reading
          return kept
        },
        set: async (items: Record<string, unknown>) => Object.assign(saved, items),
        remove: async () => {},
      },
      sync: { get: async () => ({}) },
      local: { get: async () => ({}) },
      onChanged: event('storage.onChanged'),
    },
    runtime: {
      onStartup: event('onStartup'),
      onInstalled: event('onInstalled'),
      onMessage: event('onMessage'),
      openOptionsPage: vi.fn(),
    },
    action: { setBadgeText: vi.fn(), setBadgeBackgroundColor: vi.fn(), onClicked: event('onClicked') },
    tabs: {
      get: async (id: number) => ({ id, url: 'https://shop.example.test/sign-in', title: 'Shop', windowId: 1 }),
      query: async () => [],
      sendMessage: async () => {},
      onUpdated: event('tabs.onUpdated'),
      onRemoved: event('tabs.onRemoved'),
    },
    debugger: {
      attach: async () => {},
      detach: async () => {},
      sendCommand: async () => ({}),
      onEvent: event('debugger.onEvent'),
      onDetach: event('debugger.onDetach'),
    },
    webNavigation: { onCommitted: event('webNavigation.onCommitted') },
    scripting: { executeScript: async () => [] },
  })
  await import('./background')
  const commit = (details: Record<string, unknown>) =>
    Promise.all(listeners['webNavigation.onCommitted'].map((listener) => listener(details)))
  return { finishReading, commit, saved, listeners }
}

describe('a commit that wakes the worker', () => {
  it('waits for the agent’s tabs to be read back, and is then reported for a tab it may act in', async () => {
    const worker = await startWorker({ opened: [9] })
    let isDone = false
    const committing = worker
      .commit({ tabId: 9, frameId: 0, documentId: 'doc-1', url: 'https://shop.example.test/a' })
      .then(() => (isDone = true))
    await new Promise((resolve) => setTimeout(resolve, 20))
    expect(isDone).toBe(false)
    worker.finishReading()
    await committing
    // Kept for the next run of the worker, with the tab's count.
    expect(worker.saved.documents).toMatchObject({ counts: [[9, 1]], current: [[9, 'doc-1']] })
  })

  it('still knows a tab shows a document the agent filled after the worker started again', async () => {
    const worker = await startWorker({
      opened: [9],
      documents: { counts: [[9, 3]], current: [[9, 'doc-5']], filled: [[9, ['doc-5']]] },
    })
    worker.finishReading()
    await worker.commit({ tabId: 9, frameId: 0, documentId: 'doc-5', url: 'https://shop.example.test/a' })
    // The count carries on from where it was, and the filled document is still the filled one.
    expect(worker.saved.documents).toMatchObject({ counts: [[9, 4]], filled: [[9, ['doc-5']]] })
  })
})
