import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  OWN_PAGES_REFUSAL,
  REQUEST_BODY_REFUSAL,
} from './reach.js'

// The extension's worker is loaded against a browser that is a few lists
// and recorders, signed in to a server at mail.example.com, and spoken to
// over a socket that is a fake: what the server asks of the worker is
// delivered as a message, and what the worker answers is read off the
// socket.

const serverOrigin = 'https://mail.example.com'

// Passwords' half of the worker is tested on its own (see agentTabActions.test.ts
// and the files in passwords/); here it is stood in for.
vi.mock('./passwords/background', () => ({}))
vi.mock('./passwords/agentFill', () => ({
  fillLoginForAgent: vi.fn(),
  fillOneTimeCodeForAgent: vi.fn(),
}))

class FakeSocket {
  static OPEN = 1
  static instances = []
  constructor(address) {
    this.address = address
    this.readyState = 1
    this.sent = []
    FakeSocket.instances.push(this)
  }
  send(text) {
    this.sent.push(JSON.parse(text))
  }
  close() {
    this.readyState = 3
  }
}

function listenerSet() {
  return { addListener: vi.fn(), removeListener: vi.fn() }
}

// load starts the worker with these tabs open (by id, with their address),
// the person's own tab being the first, and the agent's tab the second when
// there is one.
async function load(
  tabs,
  {
    current = tabs[0].id,
    opened = [],
    runPage = false,
    storedServer = serverOrigin,
    syncedServer,
  } = {},
) {
  FakeSocket.instances = []
  const byId = new Map(
    tabs.map((tab) => [tab.id, { windowId: 1, active: true, status: 'complete', title: 'A page', ...tab }]),
  )
  const calls = { debuggerSent: [], debuggerAttached: [], scripts: [], created: [], updated: [] }
  globalThis.WebSocket = FakeSocket
  // The session storage keeps what the worker puts in it.
  const session = {
    ownTabId: tabs[0].id,
    tabId: current,
    opened,
  }
  const local = {}
  globalThis.chrome = {
    storage: {
      session: {
        get: vi.fn(async () => ({ ...session })),
        set: vi.fn(async (values) => void Object.assign(session, values)),
        remove: vi.fn(async () => {}),
      },
      local: {
        get: vi.fn(async () => ({ token: 'a-token', ...(storedServer ? { server: storedServer } : {}), ...local })),
        set: vi.fn(async (values) => void Object.assign(local, values)),
      },
      sync: {
        get: vi.fn(async () => (syncedServer ? { server: syncedServer } : {})),
      },
      onChanged: listenerSet(),
    },
    action: { setBadgeText: vi.fn(), setBadgeBackgroundColor: vi.fn() },
    runtime: {
      getURL: (path) => 'chrome-extension://extension-id/' + path,
      sendMessage: vi.fn(async () => {}),
      onStartup: listenerSet(),
      onInstalled: listenerSet(),
      onMessage: listenerSet(),
      openOptionsPage: vi.fn(),
      id: 'extension-id',
    },
    tabs: {
      get: vi.fn(async (id) => {
        if (!byId.has(id)) throw new Error('no tab ' + id)
        return byId.get(id)
      }),
      query: vi.fn(async () => [...byId.values()]),
      create: vi.fn(async (properties) => {
        calls.created.push(properties)
        const made = { id: 100 + calls.created.length, windowId: 1, status: 'complete', title: '', url: properties.url }
        byId.set(made.id, made)
        return made
      }),
      update: vi.fn(async (id, properties) => {
        calls.updated.push([id, properties])
        return byId.get(id)
      }),
      remove: vi.fn(async () => {}),
      group: vi.fn(async () => 1),
      sendMessage: vi.fn(async () => {}),
      captureVisibleTab: vi.fn(async () => 'data:image/png;base64,AAAA'),
      onUpdated: listenerSet(),
      onRemoved: listenerSet(),
    },
    tabGroups: { get: vi.fn(async () => null), query: vi.fn(async () => []), update: vi.fn(async () => {}) },
    windows: { getLastFocused: vi.fn(async () => ({ id: 1 })), create: vi.fn(async () => ({ id: 2 })) },
    scripting: {
      executeScript: vi.fn(async (injection) => {
        calls.scripts.push(injection)
        if (runPage) return [{ result: await injection.func(...injection.args) }]
        return [{ result: { ok: true, data: { ran: injection.args[0] } } }]
      }),
    },
    debugger: {
      attach: vi.fn(async (target) => {
        calls.debuggerAttached.push(target)
      }),
      detach: vi.fn(async () => {}),
      sendCommand: vi.fn(async (target, method, params) => {
        calls.debuggerSent.push([method, params])
        return {}
      }),
      onEvent: listenerSet(),
      onDetach: listenerSet(),
    },
  }
  vi.resetModules()
  await import('./background.js')
  // Connected: the socket opens, says hello, and is welcomed.
  await vi.waitFor(() => expect(FakeSocket.instances).toHaveLength(1))
  const socket = FakeSocket.instances[0]
  await socket.onopen()
  await socket.onmessage({ data: JSON.stringify({ type: 'welcome', protocol: 5 }) })
  let next = 1
  const ask = async (action, args = {}) => {
    const id = next++
    await socket.onmessage({ data: JSON.stringify({ type: 'act', id, action, args }) })
    await vi.waitFor(() =>
      expect(socket.sent.some((message) => message.type === 'result' && message.id === id)).toBe(true),
    )
    return socket.sent.find((message) => message.type === 'result' && message.id === id)
  }
  // What a page of the extension, or a script in a page, says to the worker,
  // answered on the callback. The side panel is the default sender: a page of
  // the extension itself, with no tab.
  const fromSender = (message, sender = {}) =>
    new Promise((resolve) => {
      const full = { id: 'extension-id', url: 'chrome-extension://extension-id/sidepanel.html', ...sender }
      // The worker has more than one listener for messages; the one that
      // answers keeps the channel open or answers at once.
      let isAnswered = false
      const answer = (value) => {
        isAnswered = true
        resolve(value)
      }
      for (const [listener] of globalThis.chrome.runtime.onMessage.addListener.mock.calls) {
        if (listener(message, full, answer) === true) return
        if (isAnswered) return
      }
      resolve(undefined)
    })
  return { ask, calls, chrome: globalThis.chrome, socket, fromSender }
}

const ownTab = (url) => ({ id: 7, url })

beforeEach(() => {
  vi.spyOn(console, 'warn').mockImplementation(() => {})
})

afterEach(() => {
  delete globalThis.chrome
  delete globalThis.WebSocket
})

describe('a tab on an ordinary site', () => {
  it('is read and acted in as before', async () => {
    const { ask, calls } = await load([ownTab('https://shop.example.org/cart')])
    expect(await ask('snapshot')).toMatchObject({ ok: true })
    expect(await ask('click', { ref: 1 })).toMatchObject({ ok: true })
    expect(calls.scripts).toHaveLength(2)
    // The script is told which host it must not run on.
    expect(calls.scripts[0].args[2]).toBe('mail.example.com')
  })

  it('is sent the protocol methods that are on the list, and only those', async () => {
    const { ask, calls } = await load([ownTab('https://shop.example.org/cart')])
    expect(
      await ask('cdp', { method: 'Input.dispatchMouseEvent', params: { type: 'mouseMoved', x: 1, y: 2 } }),
    ).toMatchObject({ ok: true })
    expect(await ask('cdp', { method: 'Network.enable' })).toMatchObject({ ok: true })
    expect(calls.debuggerSent.map(([method]) => method)).toEqual(['Input.dispatchMouseEvent', 'Network.enable'])
  })

  it('is never asked to choose a file or take a dropped one', async () => {
    const { ask, calls } = await load([ownTab('https://shop.example.org/cart')])
    for (const method of [
      'DOM.setFileInputFiles',
      'DOM.getFileInfo',
      'Input.dispatchDragEvent',
      'Input.setInterceptDrags',
      'Page.setInterceptFileChooserDialog',
    ]) {
      const answer = await ask('cdp', { method, params: { files: ['~/example/token'], nodeId: 1 } })
      expect(answer.ok, method).toBe(false)
      expect(answer.error, method).toContain('does not choose files')
    }
    expect((await ask('cdp', { method: 'DOM.setAttributeValue' })).ok).toBe(false)
    expect((await ask('cdp', { method: 'Browser.setDownloadBehavior' })).ok).toBe(false)
    expect(calls.debuggerSent).toEqual([])
    expect(calls.debuggerAttached).toEqual([])
  })

  it('is not sent to a file or to the server by the protocol either', async () => {
    const { ask, calls } = await load([ownTab('https://shop.example.org/cart')])
    for (const url of [
      'file:///etc/hosts',
      'https://mail.example.com/',
      'chrome-extension://abcdefgh/options.html',
      'javascript:1',
    ]) {
      const answer = await ask('cdp', { method: 'Page.navigate', params: { url } })
      expect(answer.ok, url).toBe(false)
    }
    expect(await ask('cdp', { method: 'Page.navigate', params: { url: 'https://shop.example.org/' } })).toMatchObject({
      ok: true,
    })
    expect(calls.debuggerSent).toHaveLength(1)
  })

  it('opens another ordinary site beside it', async () => {
    const { ask, calls } = await load([ownTab('https://shop.example.org/cart')])
    expect(await ask('open', { url: 'https://news.example.net/' })).toMatchObject({ ok: true })
    expect(calls.created.map((made) => made.url)).toEqual(['https://news.example.net/'])
  })

  it('navigates to another ordinary site', async () => {
    const { ask, calls } = await load([ownTab('https://shop.example.org/cart')])
    expect(await ask('navigate', { url: 'https://news.example.net/' })).toMatchObject({ ok: true })
    expect(calls.updated).toEqual([[7, { url: 'https://news.example.net/' }]])
  })
})

describe('an address that is TeaNode, the extension, or not a website', () => {
  it('is not opened and not navigated to', async () => {
    const { ask, calls } = await load([ownTab('https://shop.example.org/cart')])
    for (const url of [
      'https://mail.example.com/',
      'https://mail.example.com:8443/settings',
      'http://MAIL.example.com/',
      'https://mail.example.com\\@example.org/',
    ]) {
      expect(await ask('open', { url }), url).toEqual(expect.objectContaining({ ok: false, error: OWN_PAGES_REFUSAL }))
      expect(await ask('navigate', { url }), url).toEqual(
        expect.objectContaining({ ok: false, error: OWN_PAGES_REFUSAL }),
      )
    }
    for (const url of [
      'chrome-extension://abcdefgh/options.html',
      'file:///etc/hosts',
      'javascript:1',
      'data:text/html,hi',
      '',
    ]) {
      expect((await ask('open', { url })).ok, url).toBe(false)
      expect((await ask('navigate', { url })).ok, url).toBe(false)
    }
    expect(calls.created).toEqual([])
    expect(calls.updated).toEqual([])
  })
})

describe('a tab that shows TeaNode or an extension page', () => {
  const everyAction = [
    ['navigate', { url: 'https://shop.example.org/' }],
    ['snapshot', {}],
    ['screenshot', {}],
    ['click', { ref: 1 }],
    ['hover', { ref: 1 }],
    ['select', { ref: 1, value: 'a' }],
    ['type', { ref: 1, text: 'a' }],
    ['press', { key: 'Enter' }],
    ['scroll', {}],
    ['wait', {}],
    ['back', {}],
    ['evaluate', { expression: '1' }],
    ['fetch', { url: '/' }],
    ['storage', {}],
    ['cdp', { method: 'Input.dispatchMouseEvent', params: {} }],
    ['cdp', { method: 'Runtime.evaluate', params: { expression: '1' } }],
    ['cdp_events', {}],
  ]

  for (const url of [
    'https://mail.example.com/',
    'https://mail.example.com:8443/inbox',
    'chrome-extension://abcdefgh/options.html',
  ]) {
    it(`is not read or acted in: ${url}`, async () => {
      const { ask, calls, chrome } = await load([ownTab(url)])
      for (const [action, args] of everyAction) {
        const answer = await ask(action, args)
        expect(answer, action).toEqual(expect.objectContaining({ ok: false, error: OWN_PAGES_REFUSAL }))
      }
      expect(calls.scripts).toEqual([])
      expect(calls.debuggerAttached).toEqual([])
      expect(calls.debuggerSent).toEqual([])
      expect(chrome.tabs.captureVisibleTab).not.toHaveBeenCalled()
      expect(chrome.tabs.update).not.toHaveBeenCalled()
    })
  }

  it('is still listed, and a page of the agent can still be opened beside it', async () => {
    const { ask, calls } = await load([ownTab('https://mail.example.com/')])
    const listed = await ask('tabs')
    expect(listed.ok).toBe(true)
    expect(listed.data.tabs).toHaveLength(1)
    expect(await ask('open', { url: 'https://shop.example.org/' })).toMatchObject({ ok: true })
    expect(calls.created).toHaveLength(1)
  })

  it('is not switched to, by number, by its address or by default', async () => {
    const { ask, chrome } = await load(
      [
        ownTab('https://shop.example.org/'),
        { id: 8, url: 'https://mail.example.com/drawer', title: 'Your agent' },
        { id: 9, url: 'chrome-extension://abcdefgh/options.html', title: 'Options' },
      ],
      { opened: [8, 9] },
    )
    for (const args of [
      { tab: 8 },
      { tab: 9 },
      { url: 'mail.example.com' },
      { url: 'abcdefgh' },
      { text: 'Your agent' },
    ]) {
      const answer = await ask('switch', args)
      expect(answer, JSON.stringify(args)).toEqual(expect.objectContaining({ ok: false, error: OWN_PAGES_REFUSAL }))
    }
    expect(chrome.tabs.update).not.toHaveBeenCalled()
  })

  it("is not gone back to when it is the person's own tab", async () => {
    const { ask, chrome } = await load(
      [ownTab('https://mail.example.com/'), { id: 8, url: 'https://shop.example.org/' }],
      { current: 8, opened: [8] },
    )
    expect(await ask('switch')).toEqual(expect.objectContaining({ ok: false, error: OWN_PAGES_REFUSAL }))
    expect(chrome.tabs.update).not.toHaveBeenCalled()
    // The other tab is still the agent's to use.
    expect(await ask('snapshot')).toMatchObject({ ok: true })
  })

  it('is not an excuse to run a script in a page that went there meanwhile', async () => {
    const { ask, calls } = await load([ownTab('https://shop.example.org/')])
    expect(await ask('snapshot')).toMatchObject({ ok: true })
    // What the injected script checks first is where it is running.
    const [, , ownHostname, refusal] = calls.scripts[0].args
    expect(ownHostname).toBe('mail.example.com')
    expect(refusal).toBe(OWN_PAGES_REFUSAL)
    // Run where the page is on that host, it does nothing; elsewhere it reads.
    const injected = calls.scripts[0].func
    expect(await injected('storage', {}, location.hostname, refusal)).toEqual({ ok: false, error: OWN_PAGES_REFUSAL })
    expect(await injected('storage', {}, 'mail.example.com', refusal)).toMatchObject({ ok: true })
  })
})

describe('what reaches into the frames of a page', () => {
  const ordinary = [ownTab('https://shop.example.org/cart')]

  it('is not sent: pierce, another context, another frame', async () => {
    const { ask, calls } = await load(ordinary)
    for (const [method, params] of [
      ['DOM.getDocument', { depth: -1, pierce: true }],
      ['DOM.describeNode', { nodeId: 1, depth: -1, pierce: true }],
      ['Runtime.evaluate', { expression: '1', contextId: 3 }],
      ['Accessibility.getFullAXTree', { frameId: 'A1' }],
      ['Page.navigate', { url: 'https://shop.example.org/', frameId: 'A1' }],
      ['DOMSnapshot.captureSnapshot', { computedStyles: [] }],
      ['Page.captureSnapshot', {}],
      ['Page.getFrameTree', {}],
    ]) {
      expect((await ask('cdp', { method, params })).ok, `${method} ${JSON.stringify(params)}`).toBe(false)
    }
    expect(calls.debuggerSent).toEqual([])
    expect(await ask('cdp', { method: 'DOM.getDocument', params: { depth: -1 } })).toMatchObject({ ok: true })
  })

  it('does not show the requests the server was sent, or their bodies', async () => {
    const { ask, chrome } = await load(ordinary)
    await ask('cdp', { method: 'Network.enable' })
    const onEvent = chrome.debugger.onEvent.addListener.mock.calls[0][0]
    onEvent({ tabId: 7 }, 'Network.requestWillBeSentExtraInfo', {
      requestId: '1.1',
      headers: { authorization: 'Bearer example-token' },
    })
    onEvent({ tabId: 7 }, 'Network.requestWillBeSent', {
      requestId: '1.1',
      request: { url: 'https://mail.example.com/api/v1/graphql' },
    })
    onEvent({ tabId: 7 }, 'Network.requestWillBeSent', {
      requestId: '2.2',
      request: { url: 'https://cdn.example.net/a.js' },
    })
    const shown = await ask('cdp_events', {})
    expect(JSON.stringify(shown)).not.toContain('example-token')
    expect(JSON.stringify(shown)).not.toContain('mail.example.com')
    expect(JSON.stringify(shown)).toContain('cdn.example.net')
    expect(await ask('cdp', { method: 'Network.getResponseBody', params: { requestId: '1.1' } })).toEqual(
      expect.objectContaining({ ok: false, error: REQUEST_BODY_REFUSAL }),
    )
    // The body a page sent is never read, shown request or not: a form's request
    // is where a typed password travels, so the method is not one that is answered.
    const postData = await ask('cdp', { method: 'Network.getRequestPostData', params: { requestId: '2.2' } })
    expect(postData.ok).toBe(false)
    expect(postData.error).toContain('is not one of the methods')
    expect(await ask('cdp', { method: 'Network.getResponseBody', params: { requestId: '2.2' } })).toMatchObject({
      ok: true,
    })
  })

  it('does not show a connection to the server whose first events the buffer pushed out', async () => {
    const { ask, chrome } = await load(ordinary)
    await ask('cdp', { method: 'Network.enable' })
    const onEvent = chrome.debugger.onEvent.addListener.mock.calls[0][0]
    const tab = { tabId: 7 }
    onEvent(tab, 'Network.webSocketCreated', { requestId: '5.1', url: 'wss://mail.example.com/api/v1/socket' })
    onEvent(tab, 'Network.webSocketCreated', { requestId: '5.2', url: 'wss://live.example.net/feed' })
    // A page that makes more requests than the worker keeps.
    for (let index = 0; index < 1100; index++) {
      onEvent(tab, 'Network.requestWillBeSent', {
        requestId: `9.${index}`,
        request: { url: 'https://cdn.example.net/a.js' },
      })
    }
    for (let round = 0; round < 20; round++) {
      onEvent(tab, 'Network.webSocketFrameSent', {
        requestId: '5.1',
        response: { payloadData: 'Bearer example-token' },
      })
      onEvent(tab, 'Network.webSocketFrameSent', { requestId: '5.2', response: { payloadData: 'hello' } })
      const shown = await ask('cdp_events', { limit: 1000 })
      expect(shown.ok).toBe(true)
      expect(JSON.stringify(shown)).not.toContain('example-token')
      expect(JSON.stringify(shown)).not.toContain('"5.1"')
      expect(JSON.stringify(shown)).toContain('hello')
      expect(typeof shown.data.session).toBe('string')
    }
    expect(await ask('cdp', { method: 'Network.getResponseBody', params: { requestId: '5.1' } })).toEqual(
      expect.objectContaining({ ok: false, error: REQUEST_BODY_REFUSAL }),
    )
  })

  it('does not show an event of a request it never saw created', async () => {
    const { ask, chrome } = await load(ordinary)
    await ask('cdp', { method: 'Network.enable' })
    const onEvent = chrome.debugger.onEvent.addListener.mock.calls[0][0]
    onEvent({ tabId: 7 }, 'Network.webSocketFrameSent', {
      requestId: '5.1',
      response: { payloadData: 'Bearer example-token' },
    })
    onEvent({ tabId: 7 }, 'Network.policyUpdated', {})
    onEvent({ tabId: 7 }, 'Log.entryAdded', { entry: { text: 'a message' } })
    const shown = await ask('cdp_events', {})
    expect(JSON.stringify(shown)).not.toContain('example-token')
    expect(JSON.stringify(shown)).not.toContain('policyUpdated')
    expect(JSON.stringify(shown)).toContain('a message')
  })

  it('starts a new record, and says so, when the agent lets the protocol go', async () => {
    const { ask, chrome } = await load(ordinary)
    await ask('cdp', { method: 'Network.enable' })
    const onEvent = chrome.debugger.onEvent.addListener.mock.calls[0][0]
    onEvent({ tabId: 7 }, 'Network.webSocketCreated', { requestId: '5.2', url: 'wss://live.example.net/feed' })
    const before = await ask('cdp_events', {})
    expect(JSON.stringify(before)).toContain('live.example.net')
    await ask('cdp_stop')
    await ask('cdp', { method: 'Network.enable' })
    onEvent({ tabId: 7 }, 'Network.webSocketFrameSent', { requestId: '5.2', response: { payloadData: 'late' } })
    const after = await ask('cdp_events', {})
    expect(JSON.stringify(after)).not.toContain('late')
    expect(after.data.session).not.toBe(before.data.session)
  })
})

describe("a fetch with the page's session", () => {
  it("is not sent to the server, however it is written, and is sent to the page's own site", async () => {
    const { ask, calls } = await load([ownTab('https://app.example.com/page')])
    for (const url of [
      'https://mail.example.com/api/v1/graphql',
      '//mail.example.com/x',
      'http://MAIL.example.com:8443/x',
      'file:///etc/hosts',
    ]) {
      expect(await ask('fetch', { url }), url).toEqual(expect.objectContaining({ ok: false }))
    }
    expect((await ask('fetch', { url: 'https://mail.example.com/x' })).error).toBe(OWN_PAGES_REFUSAL)
    expect(calls.scripts).toEqual([])
    expect(await ask('fetch', { url: '/statements.pdf' })).toMatchObject({ ok: true })
    expect(calls.scripts).toHaveLength(1)
  })
})

describe('the server address the token is sent to', () => {
  const ordinary = [ownTab('https://shop.example.org/cart')]
  const attacker = 'https://attacker.example.net'

  // Tells the worker's storage listeners that a key changed in an area.
  const changed = (chrome, changes, area) => {
    for (const [listener] of chrome.storage.onChanged.addListener.mock.calls) listener(changes, area)
  }

  it('is read from the local storage, where the token is', async () => {
    const { socket } = await load(ordinary)
    expect(socket.address).toBe('wss://mail.example.com/api/v1/agent/tab')
  })

  it('does not follow a change of the synced storage, which another browser can write', async () => {
    const { chrome, socket } = await load(ordinary)
    changed(chrome, { server: { newValue: attacker } }, 'sync')
    await new Promise((resolve) => setTimeout(resolve, 20))
    expect(socket.readyState).toBe(1)
    expect(FakeSocket.instances).toHaveLength(1)
  })

  it('is brought over once from the synced storage, where the last build kept it', async () => {
    await load(ordinary, { storedServer: '', syncedServer: 'https://mail.example.com' })
    await vi.waitFor(() => expect(FakeSocket.instances).toHaveLength(1))
    expect(FakeSocket.instances[0].address).toBe('wss://mail.example.com/api/v1/agent/tab')
  })

  it('is not brought over from the synced storage when this browser has its own', async () => {
    const { socket } = await load(ordinary, { syncedServer: attacker })
    expect(socket.address).toBe('wss://mail.example.com/api/v1/agent/tab')
  })

  it('follows a change in the local storage, where only the options page writes it', async () => {
    const { chrome, socket } = await load(ordinary)
    await chrome.storage.local.set({ server: 'https://other.example.com' })
    changed(chrome, { server: { newValue: 'https://other.example.com' } }, 'local')
    await vi.waitFor(() => expect(FakeSocket.instances).toHaveLength(2))
    expect(socket.readyState).toBe(3)
    expect(FakeSocket.instances[1].address).toBe('wss://other.example.com/api/v1/agent/tab')
  })
})

describe('the side panel', () => {
  const ordinary = [ownTab('https://shop.example.org/cart'), { id: 8, url: 'https://shop.example.org/other' }]
  const sidePanelPage = 'chrome-extension://extension-id/sidepanel.html'
  const types = ['sidepanel:state', 'sidepanel:token', 'sidepanel:attach', 'sidepanel:detach', 'sidepanel:options']

  it('is told who is signed in, which tab is attached, and not the token', async () => {
    const { fromSender } = await load(ordinary)
    const answer = await fromSender({ type: 'sidepanel:state' })
    expect(answer).toEqual({ server: serverOrigin, isSignedIn: true, username: '', attachedTabId: 7 })
    expect(JSON.stringify(answer)).not.toContain('a-token')
  })

  it('is given the token itself when it asks for it, as the extension page it is', async () => {
    const { fromSender } = await load(ordinary)
    expect(await fromSender({ type: 'sidepanel:token' })).toEqual({ token: 'a-token' })
    // The address may carry a query or a fragment.
    expect(await fromSender({ type: 'sidepanel:token' }, { url: sidePanelPage + '?x=1#y' })).toEqual({ token: 'a-token' })
    expect(await fromSender({ type: 'sidepanel:token' }, { tab: { id: 7 } })).toEqual({ token: 'a-token' })
  })

  it('is the only sender answered, in a tab or not: not a script in a page, not another page of the extension', async () => {
    const { fromSender, chrome } = await load(ordinary)
    chrome.tabs.get.mockClear()
    const senders = {
      'a content script in a website': { url: 'https://shop.example.org/cart', tab: { id: 7 } },
      'another page of the extension': { url: 'chrome-extension://extension-id/options.html' },
      'a page that only starts like it': { url: sidePanelPage + '.evil.html' },
      'another extension': { id: 'someone-else' },
      'no address at all': { url: undefined },
    }
    for (const [what, sender] of Object.entries(senders)) {
      for (const type of types) {
        expect(await fromSender({ type, tabId: 8 }, sender), `${type} from ${what}`).toEqual({
          error: 'Not allowed from here.',
        })
      }
    }
    expect(chrome.runtime.openOptionsPage).not.toHaveBeenCalled()
    expect(chrome.tabs.get).not.toHaveBeenCalled()
  })

  it('leaves a message that is not for it to the others', async () => {
    const { fromSender } = await load(ordinary)
    expect(await fromSender({ type: 'something-else' })).toBeUndefined()
    expect(await fromSender({ type: 'sidepanel:nothing-known' })).toEqual({ error: 'Unknown request.' })
  })

  it('opens the options on request', async () => {
    const { fromSender, chrome } = await load(ordinary)
    expect(await fromSender({ type: 'sidepanel:options' })).toEqual({})
    expect(chrome.runtime.openOptionsPage).toHaveBeenCalledTimes(1)
  })

  it('attaches the tab it names, looked up in the browser, and says so to every side panel', async () => {
    const { fromSender, chrome } = await load(ordinary)
    chrome.runtime.sendMessage.mockClear()
    expect(await fromSender({ type: 'sidepanel:attach', tabId: 8 })).toEqual({ attachedTabId: 8 })
    expect(chrome.tabs.get).toHaveBeenCalledWith(8)
    expect(chrome.runtime.sendMessage).toHaveBeenCalledWith({ type: 'state', attachedTabId: 8 })
    expect(await fromSender({ type: 'sidepanel:state' })).toMatchObject({ attachedTabId: 8 })
  })

  it('does not attach a tab it was not given a number for, or one that is not there', async () => {
    const { fromSender } = await load(ordinary)
    expect(await fromSender({ type: 'sidepanel:attach', tabId: '8' })).toEqual({ error: 'Unknown tab.' })
    expect(await fromSender({ type: 'sidepanel:attach', tabId: 99 })).toEqual({ error: 'Something went wrong.' })
    expect(await fromSender({ type: 'sidepanel:state' })).toMatchObject({ attachedTabId: 7 })
  })

  it('detaches, and says so to every side panel', async () => {
    const { fromSender, chrome } = await load(ordinary)
    chrome.runtime.sendMessage.mockClear()
    expect(await fromSender({ type: 'sidepanel:detach' })).toEqual({ attachedTabId: null })
    expect(chrome.runtime.sendMessage).toHaveBeenCalledWith({ type: 'state', attachedTabId: null })
  })

  it('says so when the attached tab is closed', async () => {
    const { chrome } = await load(ordinary)
    chrome.runtime.sendMessage.mockClear()
    const closed = chrome.tabs.onRemoved.addListener.mock.calls[0][0]
    closed(7)
    await vi.waitFor(() =>
      expect(chrome.runtime.sendMessage).toHaveBeenCalledWith({ type: 'state', attachedTabId: null }),
    )
  })

  it('does not break attaching when nobody is listening', async () => {
    const { fromSender, chrome } = await load(ordinary)
    chrome.runtime.sendMessage.mockRejectedValue(new Error('Receiving end does not exist.'))
    expect(await fromSender({ type: 'sidepanel:attach', tabId: 8 })).toEqual({ attachedTabId: 8 })
  })
})
