// @vitest-environment-options {"url": "https://shop.example.test/sign-in"}
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

// The service worker's script is driven here as the socket drives it: through
// act, with the browser's APIs stood in for. What is held to is what the agent
// can learn of a tab it filled a password into: nothing from the network, no
// request body ever, and no call meant for a tab it is no longer in.

vi.mock('./passwords/background', () => ({}))
const fillLogin = vi.fn()
const fillOneTimeCode = vi.fn()
vi.mock('./passwords/agentFill', () => ({
  fillLoginForAgent: (...args: unknown[]) => fillLogin(...args),
  fillOneTimeCodeForAgent: (...args: unknown[]) => fillOneTimeCode(...args),
}))

const listeners: Record<string, ((...args: any[]) => void)[]> = {}
const event = (name: string) => ({
  addListener: (listener: (...args: any[]) => void) => {
    ;(listeners[name] ??= []).push(listener)
  },
  removeListener: vi.fn(),
})
const sendCommand = vi.fn(async (_target: unknown, method: string) => (method === 'Network.enable' ? {} : {}))
const executeScript = vi.fn(async () => [{ result: { ok: true, data: 'a result' } }])

let act: (action: string, args: Record<string, unknown>) => Promise<{ ok: boolean; data?: any; error?: string }>

beforeAll(async () => {
  vi.stubGlobal('chrome', {
    storage: {
      session: { get: async () => ({ tabId: 7, ownTabId: 7 }), set: async () => {}, remove: async () => {} },
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
      get: async (id: number) => ({
        id,
        url: 'https://shop.example.test/sign-in',
        title: 'Shop',
        windowId: 1,
        active: true,
      }),
      query: async () => [],
      sendMessage: async () => {},
      onUpdated: event('tabs.onUpdated'),
      onRemoved: event('tabs.onRemoved'),
    },
    debugger: {
      attach: async () => {},
      detach: async () => {},
      sendCommand,
      onEvent: event('debugger.onEvent'),
      onDetach: event('debugger.onDetach'),
    },
    webNavigation: { onCommitted: event('webNavigation.onCommitted') },
    scripting: { executeScript },
  })
  ;({ act } = await import('./background'))
})

const requestEvent = {
  requestId: '1',
  request: {
    url: 'https://shop.example.test/login',
    method: 'POST',
    postData: 'user=ada&password=hunter2',
    hasPostData: true,
  },
}

function debuggerEvent(method: string, params: unknown) {
  for (const listener of listeners['debugger.onEvent']) listener({ tabId: 7 }, method, params)
}

beforeEach(() => {
  fillLogin.mockReset()
  fillOneTimeCode.mockReset()
  executeScript.mockClear()
  sendCommand.mockClear()
})

describe('a call the server checked for one tab', () => {
  it('is not done when the actions go to another tab by now', async () => {
    const answer = await act('evaluate', { expression: '1', expectedTab: '8' })
    expect(answer.ok).toBe(false)
    expect(answer.error).toContain('has changed')
    expect(executeScript).not.toHaveBeenCalled()
    for (const action of ['screenshot', 'storage', 'cdp_events']) {
      expect((await act(action, { expectedTab: 9 })).ok).toBe(false)
    }
    expect((await act('fill_login', { expectedTab: '8', host: 'shop.example.test' })).ok).toBe(false)
    expect(fillLogin).not.toHaveBeenCalled()
  })

  it('is done in the tab it was checked for', async () => {
    expect((await act('evaluate', { expression: '1', expectedTab: '7' })).ok).toBe(true)
    expect((await act('evaluate', { expression: '1', expectedTab: 7 })).ok).toBe(true)
    expect((await act('evaluate', { expression: '1' })).ok).toBe(true)
    expect(executeScript).toHaveBeenCalledTimes(3)
  })
})

describe('what is kept of the network', () => {
  it('never holds the body of a request', async () => {
    await act('cdp', { method: 'Network.enable' })
    debuggerEvent('Network.requestWillBeSent', requestEvent)
    const answer = await act('cdp_events', { forget: true })
    expect(answer.data.events).toHaveLength(1)
    expect(JSON.stringify(answer.data.events)).not.toContain('hunter2')
    expect(JSON.stringify(answer.data.events)).not.toContain('postData')
    expect(JSON.stringify(answer.data.events)).not.toContain('hasPostData')
    expect(answer.data.events[0].params.request.url).toBe('https://shop.example.test/login')
  })

  it('cannot be asked for a request body', async () => {
    const answer = await act('cdp', { method: 'Network.getRequestPostData', params: { requestId: '1' } })
    expect(answer.ok).toBe(false)
    expect(answer.error).toContain('is not one of the methods')
    expect(sendCommand.mock.calls.filter(([, method]) => method === 'Network.getRequestPostData')).toHaveLength(0)
  })

  it('navigates only to an http or https address, and reloads with nothing but ignoreCache', async () => {
    const countOf = (method: string) => sendCommand.mock.calls.filter(([, called]) => called === method).length
    for (const url of ['javascript:alert(1)', 'data:text/html,x', 'file:///srv/page.html', 'not an address', '']) {
      const answer = await act('cdp', { method: 'Page.navigate', params: { url } })
      expect(answer.ok, url).toBe(false)
      expect(answer.error).toContain('http or https')
    }
    expect((await act('cdp', { method: 'Page.navigate', params: {} })).ok).toBe(false)
    expect((await act('cdp', { method: 'Page.navigate' })).ok).toBe(false)
    expect(countOf('Page.navigate')).toBe(0)
    expect((await act('cdp', { method: 'Page.navigate', params: { url: 'https://shop.example.test/home' } })).ok).toBe(
      true,
    )
    expect((await act('cdp', { method: 'Page.navigate', params: { url: 'http://shop.example.test/home' } })).ok).toBe(
      true,
    )
    expect(countOf('Page.navigate')).toBe(2)

    const refused = await act('cdp', { method: 'Page.reload', params: { scriptToEvaluateOnLoad: 'alert(1)' } })
    expect(refused.ok).toBe(false)
    expect(refused.error).toContain('ignoreCache')
    expect(countOf('Page.reload')).toBe(0)
    expect((await act('cdp', { method: 'Page.reload', params: { ignoreCache: true } })).ok).toBe(true)
    expect((await act('cdp', { method: 'Page.reload' })).ok).toBe(true)
    expect(countOf('Page.reload')).toBe(2)
  })

  it('is turned off and forgotten before a password goes in, and nothing is kept of that document', async () => {
    await act('cdp', { method: 'Network.enable' })
    debuggerEvent('Network.requestWillBeSent', requestEvent)
    debuggerEvent('Runtime.consoleAPICalled', { args: [{ value: 'before the fill' }] })
    fillLogin.mockResolvedValue({ ok: true, data: { isFilled: true, documentId: 'doc-1' } })
    sendCommand.mockClear()

    const answer = await act('fill_login', {
      host: 'shop.example.test',
      passwordRef: 2,
      password: 'hunter2',
      expectedTab: '7',
    })
    expect(answer).toEqual({
      ok: true,
      data: { isFilled: true, documentId: 'doc-1', tab: 7, commitCount: expect.any(Number) },
    })
    const disabledAt = sendCommand.mock.calls.findIndex(([, method]) => method === 'Network.disable')
    expect(disabledAt).toBeGreaterThanOrEqual(0)
    // Turned off before the fill, which is the call that types.
    expect(sendCommand.mock.invocationCallOrder[disabledAt]).toBeLessThan(fillLogin.mock.invocationCallOrder[0])

    // What was kept before is gone.
    let events = (await act('cdp_events', {})).data.events
    expect(events).toEqual([])
    // And nothing of the document it filled is kept: the page's requests, its
    // console, whatever it echoes.
    debuggerEvent('Network.requestWillBeSent', requestEvent)
    debuggerEvent('Runtime.consoleAPICalled', { args: [{ value: 'hunter2' }] })
    events = (await act('cdp_events', {})).data.events
    expect(events).toEqual([])

    // A new document of the tab: events are kept again, still without bodies.
    for (const listener of listeners['webNavigation.onCommitted']) {
      await listener({
        tabId: 7,
        frameId: 0,
        documentId: 'doc-2',
        url: 'https://shop.example.test/account',
        transitionQualifiers: [],
      })
    }
    debuggerEvent('Network.requestWillBeSent', requestEvent)
    events = (await act('cdp_events', {})).data.events
    expect(events).toHaveLength(1)
    expect(JSON.stringify(events)).not.toContain('hunter2')
  })

  it('is not kept again for a page restored from the history’s cache', async () => {
    fillLogin.mockResolvedValue({ ok: true, data: { isFilled: true, documentId: 'doc-5' } })
    await act('fill_login', { host: 'shop.example.test', passwordRef: 2, password: 'hunter2' })
    for (const listener of listeners['webNavigation.onCommitted']) {
      await listener({
        tabId: 7,
        frameId: 0,
        documentId: 'doc-6',
        url: 'https://shop.example.test/account',
        transitionQualifiers: [],
      })
      await listener({
        tabId: 7,
        frameId: 0,
        documentId: 'doc-5',
        url: 'https://shop.example.test/sign-in',
        transitionQualifiers: ['forward_back'],
      })
    }
    debuggerEvent('Network.requestWillBeSent', requestEvent)
    expect((await act('cdp_events', {})).data.events).toEqual([])
  })
})

describe('what a fill answers', () => {
  it('names the tab and the commits so far, and nothing it was given', async () => {
    fillOneTimeCode.mockResolvedValue({ ok: true, data: { isFilled: true, documentId: 'doc-9' } })
    const answer = await act('fill_one_time_code', { host: 'shop.example.test', ref: 3, code: '123456' })
    expect(answer.ok).toBe(true)
    expect(answer.data).toMatchObject({ isFilled: true, documentId: 'doc-9', tab: 7 })
    expect(JSON.stringify(answer)).not.toContain('123456')
  })

  it('passes a refusal on as it is', async () => {
    fillLogin.mockResolvedValue({ ok: false, error: 'the password field is not a password input' })
    expect(await act('fill_login', { host: 'shop.example.test', passwordRef: 2, password: 'x' })).toEqual({
      ok: false,
      error: 'the password field is not a password input',
    })
  })
})

describe('a tab showing a document the agent filled', () => {
  const commit = async (documentId: string, transitionQualifiers: string[] = []) => {
    for (const listener of listeners['webNavigation.onCommitted']) {
      await listener({
        tabId: 7,
        frameId: 0,
        documentId,
        url: 'https://shop.example.test/page',
        transitionQualifiers,
      })
    }
  }

  async function fillDocument(documentId: string) {
    fillLogin.mockResolvedValue({ ok: true, data: { isFilled: true, documentId } })
    expect((await act('fill_login', { host: 'shop.example.test', passwordRef: 2, password: 'hunter2' })).ok).toBe(true)
  }

  it('is not read back, whatever the server has been told', async () => {
    await commit('doc-20')
    expect((await act('evaluate', { expression: '1' })).ok).toBe(true)
    await fillDocument('doc-20')
    for (const action of ['evaluate', 'screenshot', 'storage']) {
      const answer = await act(action, { expression: '1' })
      expect(answer.ok, action).toBe(false)
      expect(answer.error, action).toContain('filled with a password or code')
    }
    const readers = [
      ['Runtime.evaluate', { expression: 'document.body.innerText' }],
      ['DOM.getDocument', {}],
      ['Page.captureSnapshot', {}],
      ['Network.getResponseBody', { requestId: '1' }],
      ['Input.dispatchDragEvent', { type: 'drop', x: 1, y: 1, data: { items: [] } }],
    ] as const
    for (const [method, params] of readers) {
      expect((await act('cdp', { method, params })).ok, method).toBe(false)
    }
    expect(sendCommand.mock.calls.filter(([, method]) => method !== 'Network.disable')).toHaveLength(0)
  })

  it('takes plain input and refuses a key event that copies or pastes', async () => {
    await commit('doc-21')
    await fillDocument('doc-21')
    sendCommand.mockClear()
    expect(
      (await act('cdp', { method: 'Input.dispatchKeyEvent', params: { type: 'keyDown', key: 'a', text: 'a' } })).ok,
    ).toBe(true)
    expect(
      (await act('cdp', { method: 'Input.dispatchMouseEvent', params: { type: 'mousePressed', button: 'left' } })).ok,
    ).toBe(true)
    expect(sendCommand.mock.calls.map(([, method]) => method)).toEqual([
      'Input.dispatchKeyEvent',
      'Input.dispatchMouseEvent',
    ])
    sendCommand.mockClear()
    for (const [method, params] of [
      ['Input.dispatchKeyEvent', { type: 'keyDown', key: 'a', commands: ['selectAll', 'copy', 'paste'] }],
      ['Input.dispatchKeyEvent', { type: 'rawKeyDown', key: 'v', modifiers: 2 }],
      ['Input.dispatchMouseEvent', { type: 'mouseMoved', button: 'left', buttons: 1, x: 5, y: 5 }],
    ] as const) {
      const answer = await act('cdp', { method, params })
      expect(answer.ok, JSON.stringify(params)).toBe(false)
      expect(answer.error).toContain('Copying')
    }
    expect(sendCommand).not.toHaveBeenCalled()
  })

  it('answers a selector with a refusal and a ref with the page, and tells the page it is filled', async () => {
    await commit('doc-22')
    await fillDocument('doc-22')
    executeScript.mockClear()
    for (const [action, args] of [
      ['click', { selector: 'input[type=password][value^="a"]' }],
      ['wait', { for: 'selector', selector: 'input[value^="a"]', timeout_ms: 10 }],
    ] as const) {
      const answer = await act(action, args)
      expect(answer.ok, action).toBe(false)
      expect(answer.error, action).toContain('only refs')
    }
    expect(executeScript).not.toHaveBeenCalled()
    expect((await act('snapshot', {})).ok).toBe(true)
    // What the worker says about the document is not what the caller says.
    expect((await act('click', { ref: 4, isFilledDocument: false })).ok).toBe(true)
    const sentArgs = executeScript.mock.calls.map(
      (call) => (call as unknown as [{ args: [string, Record<string, unknown>] }])[0].args[1],
    )
    expect(sentArgs.map((each) => each.isFilledDocument)).toEqual([true, true])
  })

  it('is read again once the tab shows another document, but not when it comes back from the history', async () => {
    await commit('doc-23')
    await fillDocument('doc-23')
    expect((await act('evaluate', { expression: '1' })).ok).toBe(false)
    await commit('doc-24')
    expect((await act('evaluate', { expression: '1' })).ok).toBe(true)
    expect((await act('click', { selector: 'button' })).ok).toBe(true)
    // Back to the filled one: refused at once, before the server could say so.
    await commit('doc-23', ['forward_back'])
    for (const action of ['evaluate', 'screenshot', 'storage']) {
      expect((await act(action, { expression: '1' })).ok, action).toBe(false)
    }
    expect((await act('cdp', { method: 'Runtime.evaluate', params: { expression: '1' } })).ok).toBe(false)
    await commit('doc-25')
    expect((await act('evaluate', { expression: '1' })).ok).toBe(true)
  })
})
