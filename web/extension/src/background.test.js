import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { OWN_PAGES_REFUSAL, PANEL_ELEMENT_REFUSAL, PANEL_KEYS_REFUSAL, PANEL_REFUSAL } from './reach.js'

// The extension's worker is loaded against a browser that is a few lists
// and recorders, signed in to a server at mail.example.com, and spoken to
// over a socket that is a fake: what the server asks of the worker is
// delivered as a message, and what the worker answers is read off the
// socket.

const serverOrigin = 'https://mail.example.com'

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
async function load(tabs, { current = tabs[0].id, opened = [], panel = { isOpen: false }, runPage = false } = {}) {
  FakeSocket.instances = []
  const byId = new Map(
    tabs.map((tab) => [tab.id, { windowId: 1, active: true, status: 'complete', title: 'A page', ...tab }]),
  )
  const calls = { debuggerSent: [], debuggerAttached: [], scripts: [], created: [], updated: [] }
  globalThis.WebSocket = FakeSocket
  globalThis.chrome = {
    storage: {
      session: {
        get: vi.fn(async () => ({ ownTabId: tabs[0].id, tabId: current, opened })),
        set: vi.fn(async () => {}),
        remove: vi.fn(async () => {}),
      },
      sync: { get: vi.fn(async () => ({ server: serverOrigin })) },
      local: { get: vi.fn(async () => ({ token: 'a-token' })) },
      onChanged: listenerSet(),
    },
    action: { setBadgeText: vi.fn(), setBadgeBackgroundColor: vi.fn(), onClicked: listenerSet() },
    runtime: {
      onStartup: listenerSet(),
      onInstalled: listenerSet(),
      onMessage: listenerSet(),
      openOptionsPage: vi.fn(),
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
        // The worker looking for the panel on the page takes no arguments.
        if (!injection.args) return [{ result: panel }]
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
  await socket.onmessage({ data: JSON.stringify({ type: 'welcome', protocol: 4 }) })
  let next = 1
  const ask = async (action, args = {}) => {
    const id = next++
    await socket.onmessage({ data: JSON.stringify({ type: 'act', id, action, args }) })
    await vi.waitFor(() =>
      expect(socket.sent.some((message) => message.type === 'result' && message.id === id)).toBe(true),
    )
    return socket.sent.find((message) => message.type === 'result' && message.id === id)
  }
  return { ask, calls, chrome: globalThis.chrome }
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

describe("the person's TeaNode panel on the page", () => {
  // The panel is at the right of a 1280 by 800 viewport, and its frame has focus or not.
  const panelAt = (hasFocus = false) => ({ isOpen: true, left: 840, top: 20, right: 1280, bottom: 780, hasFocus })
  const ordinary = [ownTab('https://shop.example.org/cart')]

  it('is not clicked, tapped, scrolled, touched or typed at by the protocol', async () => {
    const { ask, calls } = await load(ordinary, { panel: panelAt() })
    for (const [method, params] of [
      ['Input.dispatchMouseEvent', { type: 'mousePressed', x: 900, y: 100, button: 'left' }],
      ['Input.dispatchMouseEvent', { type: 'mouseWheel', x: 1000, y: 400, deltaX: 0, deltaY: 100 }],
      ['Input.dispatchMouseEvent', { type: 'mouseMoved', x: 840, y: 20 }],
      ['Input.dispatchMouseEvent', { type: 'mouseReleased', x: 1280, y: 780 }],
      ['Input.synthesizeTapGesture', { x: 900, y: 100 }],
      ['Input.synthesizeScrollGesture', { x: 900, y: 100, yDistance: -200 }],
      [
        'Input.dispatchTouchEvent',
        {
          type: 'touchStart',
          touchPoints: [
            { x: 10, y: 10 },
            { x: 900, y: 100 },
          ],
        },
      ],
    ]) {
      const answer = await ask('cdp', { method, params })
      expect(answer, `${method} ${JSON.stringify(params)}`).toEqual(
        expect.objectContaining({ ok: false, error: PANEL_REFUSAL }),
      )
    }
    expect(calls.debuggerSent).toEqual([])
  })

  it('leaves the rest of the page to the agent', async () => {
    const { ask, calls } = await load(ordinary, { panel: panelAt() })
    expect(
      await ask('cdp', {
        method: 'Input.dispatchMouseEvent',
        params: { type: 'mousePressed', x: 400, y: 300, button: 'left' },
      }),
    ).toMatchObject({ ok: true })
    expect(
      await ask('cdp', { method: 'Input.dispatchMouseEvent', params: { type: 'mousePressed', x: 839, y: 300 } }),
    ).toMatchObject({ ok: true })
    expect(
      await ask('cdp', {
        method: 'Input.dispatchTouchEvent',
        params: { type: 'touchStart', touchPoints: [{ x: 10, y: 10 }] },
      }),
    ).toMatchObject({ ok: true })
    expect(
      await ask('cdp', { method: 'Input.dispatchKeyEvent', params: { type: 'keyDown', key: 'a', text: 'a' } }),
    ).toMatchObject({ ok: true })
    expect(await ask('cdp', { method: 'Input.insertText', params: { text: 'hello' } })).toMatchObject({ ok: true })
    expect(await ask('cdp', { method: 'Runtime.evaluate', params: { expression: '1' } })).toMatchObject({ ok: true })
    expect(calls.debuggerSent).toHaveLength(6)
  })

  it('is not typed into by the protocol while its frame has focus', async () => {
    const { ask, calls } = await load(ordinary, { panel: panelAt(true) })
    for (const [method, params] of [
      ['Input.dispatchKeyEvent', { type: 'keyDown', key: 'Enter' }],
      ['Input.insertText', { text: 'yes' }],
    ]) {
      expect(await ask('cdp', { method, params }), method).toEqual(
        expect.objectContaining({ ok: false, error: PANEL_KEYS_REFUSAL }),
      )
    }
    // Points outside it are still the agent's, and focus moves with a click there.
    expect(
      await ask('cdp', { method: 'Input.dispatchMouseEvent', params: { type: 'mousePressed', x: 100, y: 100 } }),
    ).toMatchObject({ ok: true })
    expect(calls.debuggerSent).toHaveLength(1)
  })

  it('does not stop anything when it is closed, or absent', async () => {
    for (const panel of [{ isOpen: false }, undefined]) {
      const { ask } = await load(ordinary, { panel })
      expect(
        await ask('cdp', { method: 'Input.dispatchMouseEvent', params: { type: 'mousePressed', x: 900, y: 100 } }),
      ).toMatchObject({ ok: true })
      expect(await ask('cdp', { method: 'Input.insertText', params: { text: 'a' } })).toMatchObject({ ok: true })
    }
  })

  it('is looked for again at every call, so a panel that moved is where it is now', async () => {
    const { ask, chrome } = await load(ordinary, { panel: panelAt() })
    chrome.scripting.executeScript.mockImplementation(async (injection) =>
      injection.args
        ? [{ result: {} }]
        : [{ result: { isOpen: true, left: 0, top: 0, right: 200, bottom: 200, hasFocus: false } }],
    )
    expect(
      await ask('cdp', { method: 'Input.dispatchMouseEvent', params: { type: 'mousePressed', x: 900, y: 100 } }),
    ).toMatchObject({ ok: true })
    expect(
      await ask('cdp', { method: 'Input.dispatchMouseEvent', params: { type: 'mousePressed', x: 100, y: 100 } }),
    ).toEqual(expect.objectContaining({ ok: false, error: PANEL_REFUSAL }))
  })

  describe('an element under it', () => {
    const under = { left: 900, right: 1000, top: 100, bottom: 130, width: 100, height: 30 }
    const clear = { left: 100, right: 200, top: 100, bottom: 130, width: 100, height: 30 }
    let button

    beforeEach(() => {
      document.body.innerHTML =
        '<button id="b">Approve</button><select id="s"><option value="a">A</option></select><input id="i">'
      button = document.getElementById('b')
      for (const element of document.body.children) {
        element.scrollIntoView = () => {}
        element.getBoundingClientRect = () => under
      }
      button.click = vi.fn()
    })

    afterEach(() => {
      document.body.innerHTML = ''
    })

    const ask = async (action, args, box) => {
      const { ask: asked, calls } = await load(ordinary, { panel: panelAt(), runPage: true })
      for (const element of document.body.children) element.getBoundingClientRect = () => box
      // The page's refs: the worker's script numbers what a snapshot lists.
      window.__teanodeRefs = new Map([
        [1, button],
        [2, document.getElementById('s')],
        [3, document.getElementById('i')],
      ])
      return { answer: await asked(action, args), calls }
    }

    it('is not clicked, hovered, typed into or chosen in', async () => {
      for (const [action, args] of [
        ['click', { ref: 1 }],
        ['hover', { ref: 1 }],
        ['type', { ref: 3, text: 'x' }],
        ['select', { ref: 2, value: 'a' }],
      ]) {
        const { answer } = await ask(action, args, under)
        expect(answer, action).toEqual(expect.objectContaining({ ok: false, error: PANEL_ELEMENT_REFUSAL }))
      }
      expect(button.click).not.toHaveBeenCalled()
      expect(document.getElementById('i').value).toBe('')
    })

    it('is acted in as ever when the panel is clear of it', async () => {
      const { answer } = await ask('click', { ref: 1 }, clear)
      expect(answer).toMatchObject({ ok: true })
      expect(button.click).toHaveBeenCalledTimes(1)
      expect((await ask('type', { ref: 3, text: 'x' }, clear)).answer).toMatchObject({ ok: true })
      expect(document.getElementById('i').value).toBe('x')
    })
  })
})
