import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { memoryArea } from './testServer'

// The offscreen document is stood in for the way the browser behaves: only
// one can exist, making a second is an error, and each step takes a moment.
let events: string[]
let hasDocument: boolean

const delay = () => new Promise((resolve) => setTimeout(resolve, 5))

beforeEach(() => {
  vi.resetModules()
  events = []
  hasDocument = false
  const session = memoryArea()
  vi.stubGlobal('chrome', {
    runtime: {
      getContexts: vi.fn(async () => {
        await delay()
        return hasDocument ? [{}] : []
      }),
      sendMessage: vi.fn(async (message: { type: string }) => {
        events.push(`send ${message.type}${hasDocument ? '' : ' (no document)'}`)
        await delay()
        return message.type === 'copy' ? { isCopied: true } : { isCleared: true }
      }),
    },
    offscreen: {
      createDocument: vi.fn(async () => {
        await delay()
        if (hasDocument) throw new Error('Only a single offscreen document may be created.')
        hasDocument = true
        events.push('create')
      }),
      closeDocument: vi.fn(async () => {
        await delay()
        if (!hasDocument) throw new Error('No offscreen document.')
        hasDocument = false
        events.push('close')
      }),
    },
    storage: { session },
    alarms: { create: vi.fn(async () => undefined) },
  })
})
afterEach(() => {
  vi.unstubAllGlobals()
})

describe('copying a secret', () => {
  it('makes the document, copies, and closes it', async () => {
    const { copySecret } = await import('./clipboard')
    await copySecret('hunter2-plaintext')
    expect(events).toEqual(['create', 'send copy', 'close'])
  })

  it('runs two copies at once one after the other, never closing the document under the other', async () => {
    const { copySecret } = await import('./clipboard')
    await Promise.all([copySecret('first'), copySecret('second'), copySecret('third')])
    expect(events).toEqual([
      'create',
      'send copy',
      'close',
      'create',
      'send copy',
      'close',
      'create',
      'send copy',
      'close',
    ])
  })

  it('runs the clearing of an earlier copy after a copy that is under way, not in it', async () => {
    const { copySecret, clearClipboardIfUnchanged } = await import('./clipboard')
    await copySecret('first')
    events.length = 0
    await Promise.all([copySecret('second'), clearClipboardIfUnchanged(), copySecret('third')])
    const sequence = events.filter((event) => event !== 'create' && event !== 'close')
    // Every message went to a document that existed.
    expect(sequence.every((event) => !event.includes('no document'))).toBe(true)
    // Every document made was closed before the next was made.
    const lifecycle = events.filter((event) => event === 'create' || event === 'close')
    expect(lifecycle.filter((event, index) => event === (index % 2 === 0 ? 'create' : 'close'))).toEqual(lifecycle)
  })

  it('carries on after a copy that failed', async () => {
    const { copySecret } = await import('./clipboard')
    ;(chrome.runtime.sendMessage as any).mockImplementationOnce(async () => ({ isCopied: false }))
    await expect(copySecret('first')).rejects.toThrow('did not allow copying')
    await copySecret('second')
    expect(hasDocument).toBe(false)
  })
})
