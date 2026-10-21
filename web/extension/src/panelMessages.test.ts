// @vitest-environment-options {"url": "https://shop.example.test/sign-in"}
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { memoryArea } from './passwords/testServer'

// What the panel script, which runs inside a web page, may ask of the service
// worker. It is let have the token only in the moments after the person
// pressed the extension's button on that tab, and the other things only from
// a tab that has the panel in its page; any other script of the extension in
// a page, and any frame, is refused.

vi.mock('./passwords/background', () => ({}))
vi.mock('./passwords/agentFill', () => ({ fillLoginForAgent: vi.fn(), fillOneTimeCodeForAgent: vi.fn() }))

const listeners: Record<string, ((...args: any[]) => any)[]> = {}
const event = (name: string) => ({
  addListener: (listener: (...args: any[]) => any) => {
    ;(listeners[name] ??= []).push(listener)
  },
  removeListener: vi.fn(),
})

let session: ReturnType<typeof memoryArea>
let local: ReturnType<typeof memoryArea>
let executeScript: ReturnType<typeof vi.fn>
let openOptionsPage: ReturnType<typeof vi.fn>

beforeAll(async () => {
  session = memoryArea()
  local = memoryArea()
  executeScript = vi.fn(async () => [])
  openOptionsPage = vi.fn()
  const sync = memoryArea()
  await sync.set({ server: 'https://tea.example.test/' })
  await local.set({ token: 'secret-token', username: 'ada' })
  vi.stubGlobal('chrome', {
    storage: { session, sync, local, onChanged: event('storage.onChanged') },
    runtime: {
      id: 'ext',
      onStartup: event('onStartup'),
      onInstalled: event('onInstalled'),
      onMessage: event('onMessage'),
      openOptionsPage,
    },
    action: { setBadgeText: vi.fn(), setBadgeBackgroundColor: vi.fn(), onClicked: event('onClicked') },
    tabs: {
      get: async (id: number) => ({ id, url: 'https://shop.example.test/', windowId: 1, active: true }),
      query: async () => [],
      sendMessage: async () => {},
      onUpdated: event('tabs.onUpdated'),
      onRemoved: event('tabs.onRemoved'),
    },
    debugger: {
      attach: async () => {},
      detach: async () => {},
      sendCommand: vi.fn(),
      onEvent: event('debugger.onEvent'),
      onDetach: event('debugger.onDetach'),
    },
    webNavigation: { onCommitted: event('webNavigation.onCommitted') },
    scripting: { executeScript },
  })
  await import('./background')
})

afterEach(() => {
  vi.useRealTimers()
})

beforeEach(() => {
  executeScript.mockClear()
  openOptionsPage.mockClear()
})

function ask(message: Record<string, unknown>, sender: Record<string, unknown>) {
  return new Promise<any>((resolve) => {
    let isAnswered = false
    for (const listener of listeners.onMessage) {
      if (listener(message, sender, (answer: unknown) => ((isAnswered = true), resolve(answer))) === true) return
    }
    if (!isAnswered) resolve(undefined)
  })
}

const fromPanel = (tabId: number, extra: Record<string, unknown> = {}) => ({
  id: 'ext',
  tab: { id: tabId },
  frameId: 0,
  url: 'https://shop.example.test/sign-in',
  ...extra,
})

async function pressButton(tabId: number) {
  for (const listener of listeners['onClicked']) await listener({ id: tabId })
}

describe('the token for the panel', () => {
  it('goes to the tab where the button was just pressed, once', async () => {
    await pressButton(11)
    expect(executeScript).toHaveBeenCalledWith({ target: { tabId: 11 }, files: ['panel.js'] })
    expect(await ask({ type: 'panel:token' }, fromPanel(11))).toEqual({
      server: 'https://tea.example.test',
      token: 'secret-token',
      username: 'ada',
      attached: false,
    })
    // Used up: asking again gets nothing.
    expect(await ask({ type: 'panel:token' }, fromPanel(11))).toEqual({ error: 'Not allowed from here.' })
  })

  it('is not given to a tab where the button was not pressed, or to another extension', async () => {
    expect(await ask({ type: 'panel:token' }, fromPanel(12))).toEqual({ error: 'Not allowed from here.' })
    await pressButton(13)
    expect(await ask({ type: 'panel:token' }, fromPanel(13, { id: 'someone-else' }))).toEqual({
      error: 'Not allowed from here.',
    })
    // A page of the extension without a tab (the options page) is not the panel.
    expect(await ask({ type: 'panel:token' }, { id: 'ext', url: 'chrome-extension://ext/options.html' })).toEqual({
      error: 'Not allowed from here.',
    })
  })

  it('is not given to a frame of the page, nor after a few seconds', async () => {
    await pressButton(14)
    expect(await ask({ type: 'panel:token' }, fromPanel(14, { frameId: 3 }))).toEqual({
      error: 'Not allowed from here.',
    })
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(Date.now() + 11_000)
    expect(await ask({ type: 'panel:token' }, fromPanel(14))).toEqual({ error: 'Not allowed from here.' })
  })

  it('is not given to a tab that loaded another page since, nor to one that was closed', async () => {
    await pressButton(15)
    for (const listener of listeners['tabs.onUpdated']) listener(15, { status: 'loading' })
    await vi.waitFor(async () =>
      expect(await ask({ type: 'panel:token' }, fromPanel(15))).toEqual({ error: 'Not allowed from here.' }),
    )
    await pressButton(16)
    for (const listener of listeners['tabs.onRemoved']) listener(16)
    await vi.waitFor(async () =>
      expect(await ask({ type: 'panel:token' }, fromPanel(16))).toEqual({ error: 'Not allowed from here.' }),
    )
  })

  it('ignores a message that is not for the panel', async () => {
    expect(await ask({ type: 'something-else' }, fromPanel(11))).toBeUndefined()
  })
})

describe('what else the panel asks', () => {
  it('opens the options only for a tab that has the panel', async () => {
    expect(await ask({ type: 'panel:options' }, fromPanel(21))).toEqual({ error: 'Not allowed from here.' })
    expect(openOptionsPage).not.toHaveBeenCalled()
    await pressButton(21)
    expect(await ask({ type: 'panel:options' }, fromPanel(21))).toEqual({})
    expect(openOptionsPage).toHaveBeenCalledTimes(1)
  })

  it('refuses to attach or detach from anywhere else', async () => {
    for (const type of ['panel:attach', 'panel:detach']) {
      expect(await ask({ type }, fromPanel(22)), type).toEqual({ error: 'Not allowed from here.' })
      expect(await ask({ type }, fromPanel(22, { frameId: 2 })), type).toEqual({ error: 'Not allowed from here.' })
    }
  })

  it('keeps where the panel was left in the worker, by the site the tab is on, as numbers only', async () => {
    await pressButton(23)
    expect(await ask({ type: 'panel:geometry-read' }, fromPanel(23))).toEqual({ geometry: null })
    const geometry = { across: 0.5, down: 1, width: 440, height: 760 }
    await ask({ type: 'panel:geometry-write', geometry: { ...geometry, extra: '<script>' } }, fromPanel(23))
    expect(await ask({ type: 'panel:geometry-read' }, fromPanel(23))).toEqual({ geometry })
    expect(local.contents['panel:geometry:https://shop.example.test']).toEqual(geometry)
    // Not numbers: not kept.
    await ask({ type: 'panel:geometry-write', geometry: { across: 'x', down: 1, width: 1, height: 1 } }, fromPanel(23))
    expect(await ask({ type: 'panel:geometry-read' }, fromPanel(23))).toEqual({ geometry })
    // And not for a tab that has no panel.
    expect(await ask({ type: 'panel:geometry-read' }, fromPanel(24))).toEqual({ error: 'Not allowed from here.' })
  })

  it('does not put the panel script into a page it cannot, and does not leave the tab marked', async () => {
    executeScript.mockRejectedValueOnce(new Error('cannot access'))
    await pressButton(25)
    expect(openOptionsPage).toHaveBeenCalled()
    expect(await ask({ type: 'panel:options' }, fromPanel(25))).toEqual({ error: 'Not allowed from here.' })
  })
})
