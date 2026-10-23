// @vitest-environment-options {"url": "https://shop.example.test/sign-in"}
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { SCRIPTED_ORIGIN_REFUSAL } from './lockedTab'

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
const executeScript = vi.fn(async (_injection?: unknown) => [
  { result: { ok: true, data: 'a result' }, documentId: 'doc-run' } as Record<string, unknown>,
])
const queryTabs = vi.fn(async (_filter?: unknown) => [] as Record<string, unknown>[])
// The server the agent works for: without its name no request can be told
// from a request to it, and none is shown.
const storedLocal: Record<string, unknown> = { server: 'https://mail.example.com' }
let tabUrl = 'https://shop.example.test/sign-in'

let act: (action: string, args: Record<string, unknown>) => Promise<{ ok: boolean; data?: any; error?: string }>
let clearScriptedOrigins: () => Promise<void>

beforeAll(async () => {
  vi.stubGlobal('chrome', {
    storage: {
      session: { get: async () => ({ tabId: 7, ownTabId: 7 }), set: async () => {}, remove: async () => {} },
      local: {
        get: async (key: string) => (key in storedLocal ? { [key]: storedLocal[key] } : {}),
        set: async (values: Record<string, unknown>) => void Object.assign(storedLocal, values),
      },
      onChanged: event('storage.onChanged'),
    },
    runtime: {
      onStartup: event('onStartup'),
      onInstalled: event('onInstalled'),
      onMessage: event('onMessage'),
      openOptionsPage: vi.fn(),
      id: 'extension-id',
      getURL: (path: string) => `chrome-extension://extension-id/${path}`,
    },
    action: { setBadgeText: vi.fn(), setBadgeBackgroundColor: vi.fn(), onClicked: event('onClicked') },
    tabs: {
      get: async (id: number) => ({
        id,
        url: tabUrl,
        title: 'Shop',
        windowId: 1,
        active: true,
      }),
      query: queryTabs,
      sendMessage: async () => {},
      onUpdated: event('tabs.onUpdated'),
      onRemoved: event('tabs.onRemoved'),
    },
    debugger: {
      attach: async () => {},
      detach: vi.fn(async () => {}),
      sendCommand,
      onEvent: event('debugger.onEvent'),
      onDetach: event('debugger.onDetach'),
    },
    webNavigation: { onCommitted: event('webNavigation.onCommitted') },
    scripting: { executeScript },
  })
  ;({ act, clearScriptedOrigins } = await import('./background'))
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

beforeEach(async () => {
  // Each test starts with no site the agent ran a script on.
  await clearScriptedOrigins()
  tabUrl = 'https://shop.example.test/sign-in'
  queryTabs.mockClear()
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
    // Through the debugger: a content script may not compile text into code.
    expect(sendCommand.mock.calls.filter(([, method]) => method === 'Runtime.evaluate')).toHaveLength(3)
    expect(executeScript).not.toHaveBeenCalled()
  })

  it("lets the debugger go after evaluating, unless the agent's cdp work holds it", async () => {
    const detach = (globalThis as any).chrome.debugger.detach as ReturnType<typeof vi.fn>
    detach.mockClear()
    expect((await act('evaluate', { expression: '1' })).ok).toBe(true)
    expect(detach).toHaveBeenCalledTimes(1)
    detach.mockClear()
    expect((await act('cdp', { method: 'Network.enable', params: {}, isScript: false })).ok).toBe(true)
    expect((await act('evaluate', { expression: '1' })).ok).toBe(true)
    expect(detach).not.toHaveBeenCalled()
    await act('cdp_stop', {})
  })

  it('answers an action on the browser error page in plain words', async () => {
    executeScript.mockRejectedValueOnce(new Error('Frame with ID 0 is showing error page'))
    const answer = await act('snapshot', {})
    expect(answer.ok).toBe(false)
    expect(answer.error).toContain('the page did not load')
  })

  it('evaluates through the debugger, waits for a promise and says what it threw', async () => {
    sendCommand.mockImplementationOnce(async () => ({ result: { type: 'string', value: 'a title' } }))
    expect(await act('evaluate', { expression: 'document.title' })).toMatchObject({
      ok: true,
      data: { value: 'a title' },
    })
    expect(sendCommand).toHaveBeenLastCalledWith({ tabId: 7 }, 'Runtime.evaluate', {
      expression: 'document.title',
      returnByValue: true,
      awaitPromise: true,
      replMode: true,
    })
    sendCommand.mockImplementationOnce(async () => ({
      result: { type: 'object' },
      exceptionDetails: { text: 'Uncaught', exception: { description: 'ReferenceError: nothing is not defined' } },
    }))
    expect(await act('evaluate', { expression: 'nothing' })).toEqual({
      ok: false,
      error: 'ReferenceError: nothing is not defined',
    })
  })
})

describe('what is kept of the network', () => {
  it('never holds the body of a request', async () => {
    await act('cdp', { method: 'Network.enable', isScript: false })
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
    await act('cdp', { method: 'Network.enable', isScript: false })
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
    sendCommand.mockClear()
    // A script on the site keeps it from being filled (see below); this test is
    // about what is read after a fill.
    await clearScriptedOrigins()
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
    // Only the calls that carry the action to the page; the one that looks for
    // the person's panel on it has no arguments.
    const sentArgs = executeScript.mock.calls
      .map((call) => (call as unknown as [{ args?: [string, Record<string, unknown>] }])[0].args)
      .filter((args) => args !== undefined)
      .map((args) => args[1])
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

describe('a site the agent ran a script on', () => {
  const commitOn = async (documentId: string, url: string) => {
    for (const listener of listeners['webNavigation.onCommitted']) {
      await listener({ tabId: 7, frameId: 0, documentId, url, transitionQualifiers: [] })
    }
  }
  let documentCount = 0
  beforeEach(async () => {
    // Each test starts on a document nothing was filled into.
    await commitOn(`doc-fresh-${++documentCount}`, 'https://shop.example.test/sign-in')
  })
  const fill = async () => act('fill_login', { host: 'shop.example.test', passwordRef: 2, password: 'hunter2' })
  const messageFrom = async (message: unknown, sender: unknown) => {
    const answers: unknown[] = []
    let isAnswerLater = false
    for (const listener of listeners['onMessage']) {
      if (
        (listener as (...args: unknown[]) => unknown)(message, sender, (answer: unknown) => answers.push(answer)) ===
        true
      )
        isAnswerLater = true
    }
    if (isAnswerLater) await new Promise((resolve) => setTimeout(resolve, 10))
    return answers
  }

  it('is kept in the storage, as origins only, and is not filled in any page of it', async () => {
    fillLogin.mockResolvedValue({ ok: true, data: { isFilled: true, documentId: 'doc-1' } })
    expect((await fill()).ok).toBe(true)
    await commitOn('doc-after-sign-in', 'https://shop.example.test/account')
    sendCommand.mockImplementationOnce(async () => ({ result: { type: 'string', value: 'a result' } }))
    const answer = await act('evaluate', { expression: '1' })
    // The debugger does not say which document it ran in; the one the tab
    // shows when it is done is named (frameState), and here the browser does
    // not say either.
    expect(answer).toEqual({ ok: true, data: { scriptRun: { documentId: '' }, value: 'a result' } })
    expect(storedLocal.scriptedOrigins).toEqual(['https://shop.example.test'])
    fillLogin.mockClear()
    // Another page of the site, and the same site written with www.
    for (const address of ['https://shop.example.test/account', 'https://www.shop.example.test/']) {
      tabUrl = address
      const refused = await fill()
      expect(refused.ok, address).toBe(false)
      expect(refused.error, address).toBe(SCRIPTED_ORIGIN_REFUSAL)
    }
    expect(fillLogin).not.toHaveBeenCalled()
    expect((await act('fill_one_time_code', { host: 'shop.example.test', ref: 2, code: '000000' })).error).toBe(
      SCRIPTED_ORIGIN_REFUSAL,
    )
    // Another site is not marked.
    tabUrl = 'https://bank.example.test/'
    fillLogin.mockResolvedValue({ ok: true, data: { isFilled: true, documentId: 'doc-2' } })
    expect((await fill()).ok).toBe(true)
  })

  it('is allowed again when the person says so, from their options page and no other', async () => {
    await act('evaluate', { expression: '1' })
    const options = { id: 'extension-id', frameId: 0, url: 'chrome-extension://extension-id/options.html' }
    expect(await messageFrom({ type: 'passwords:scripted-origins' }, options)).toEqual([
      { origins: ['https://shop.example.test'] },
    ])
    // A page of a website, a content script, and another page of the extension.
    for (const sender of [
      { id: 'extension-id', frameId: 0, url: 'https://shop.example.test/', tab: { id: 3 } },
      { id: 'extension-id', frameId: 0, url: 'chrome-extension://extension-id/suggestion.html' },
      { id: 'another-extension', frameId: 0, url: 'chrome-extension://extension-id/options.html' },
      { id: 'extension-id', frameId: 2, url: 'chrome-extension://extension-id/options.html' },
    ]) {
      expect(await messageFrom({ type: 'passwords:clear-scripted-origins' }, sender)).toEqual([
        { error: 'Not allowed from here.' },
      ])
    }
    expect(storedLocal.scriptedOrigins).toEqual(['https://shop.example.test'])
    expect(await messageFrom({ type: 'passwords:clear-scripted-origins' }, options)).toEqual([{}])
    expect(storedLocal.scriptedOrigins).toEqual([])
    fillLogin.mockResolvedValue({ ok: true, data: { isFilled: true, documentId: 'doc-3' } })
    expect((await fill()).ok).toBe(true)
  })

  it('follows the page when it moves to another document while the script runs', async () => {
    // The script is sent to the sign-in page's predecessor; the page is replaced
    // by a page of another site, the script runs there and that page moves on.
    sendCommand.mockImplementationOnce(async () => {
      await commitOn('doc-30', 'https://login.example.test/sign-in')
      await commitOn('doc-31', 'https://news.example.test/')
      return { result: { type: 'number', value: 1 } }
    })
    await act('evaluate', { expression: 'new Promise(() => {})' })
    expect(storedLocal.scriptedOrigins).toEqual([
      'https://login.example.test',
      'https://news.example.test',
      'https://shop.example.test',
    ])
    // Even when the script is refused or throws.
    sendCommand.mockRejectedValueOnce(new Error('no'))
    await clearScriptedOrigins()
    expect((await act('evaluate', { expression: '1' })).ok).toBe(false)
    expect(storedLocal.scriptedOrigins).toEqual(['https://shop.example.test'])
  })

  it('names the site of a window the script opened', async () => {
    queryTabs.mockResolvedValue([
      { id: 99, openerTabId: 7, pendingUrl: 'https://popup.example.test/' },
      { id: 98, openerTabId: 5, url: 'https://unrelated.example.test/' },
    ])
    await act('evaluate', { expression: 'window.open()' })
    queryTabs.mockResolvedValue([])
    expect(storedLocal.scriptedOrigins).toEqual(['https://popup.example.test', 'https://shop.example.test'])
  })

  it('is marked by a protocol command that runs script, or one the server does not describe, and not by one it says only looks', async () => {
    await act('cdp', { method: 'Input.dispatchMouseEvent', params: { type: 'mouseMoved' }, isScript: false })
    expect(storedLocal.scriptedOrigins ?? []).toEqual([])
    const answer = await act('cdp', { method: 'Runtime.evaluate', params: { expression: '1' }, isScript: true })
    expect(answer.ok).toBe(true)
    expect(answer.data).toEqual({ scriptRun: { documentId: '' }, value: { result: {} } })
    expect(storedLocal.scriptedOrigins).toEqual(['https://shop.example.test'])
    await clearScriptedOrigins()
    await act('cdp', { method: 'Runtime.callFunctionOn', params: {} })
    expect(storedLocal.scriptedOrigins).toEqual(['https://shop.example.test'])
  })

  it('is refused in the words the server has for it', async () => {
    const source = readFileSync(resolve(__dirname, '../../../internal/browser/passwords.go'), 'utf8')
    const words = /const ScriptedSiteInTabWords = (".*")\n/.exec(source)
    expect(words).not.toBeNull()
    expect(JSON.parse(words![1])).toBe(SCRIPTED_ORIGIN_REFUSAL)
  })
})
