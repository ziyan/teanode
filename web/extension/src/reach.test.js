import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  ACTIONS_OUTSIDE_A_PAGE,
  CDP_ALLOWED,
  CDP_FILE_METHODS,
  OWN_PAGES_REFUSAL,
  PANEL_KEYS_REFUSAL,
  PANEL_REFUSAL,
  addressRefusal,
  cdpParametersRefusal,
  fetchRefusal,
  handOverToken,
  REQUESTS_KEPT,
  REQUEST_BODY_REFUSAL,
  isRequestShown,
  networkEventsShown,
  newRequestRecord,
  noteRequestEvent,
  isPanelFrame,
  cdpRefusal,
  isOwnPage,
  overlapsPanel,
  pageRefusal,
  panelProbe,
  panelRefusal,
  serverHostname,
} from './reach.js'

const server = 'https://mail.example.com'

describe('the DevTools methods the agent may send', () => {
  it('allows what driving, reading and watching a page need', () => {
    for (const method of [
      'Input.dispatchMouseEvent',
      'Input.dispatchKeyEvent',
      'Input.insertText',
      'Network.enable',
      'Network.getResponseBody',
      'Page.getLayoutMetrics',
      'Runtime.evaluate',
      'DOM.getDocument',
      'Accessibility.getFullAXTree',
    ]) {
      expect(cdpRefusal(method), method).toBe('')
    }
  })

  it('refuses the methods that put a file of the person into a page, by name', () => {
    for (const method of CDP_FILE_METHODS) {
      expect(cdpRefusal(method), method).toContain('does not choose files')
    }
    expect(CDP_FILE_METHODS).toEqual(
      expect.arrayContaining([
        'DOM.setFileInputFiles',
        'DOM.getFileInfo',
        'Input.dispatchDragEvent',
        'Input.setInterceptDrags',
      ]),
    )
  })

  it('refuses a method that is not on the list, however close its name', () => {
    for (const method of [
      'Browser.setDownloadBehavior',
      'Page.setDownloadBehavior',
      'Network.clearBrowserCookies',
      'Storage.clearDataForOrigin',
      'Target.attachToTarget',
      'Fetch.enable',
      'DOM.setAttributeValue',
      'Input.dispatchDragEventX',
      'DOMSnapshot.captureSnapshot',
      'DOMSnapshot.enable',
      'Page.captureSnapshot',
      'Page.getFrameTree',
      'DOM.getNodeForLocation',
      'DOM.performSearch',
      'CSS.getComputedStyleForNode',
      'Input.',
      'DOM.',
      'input.dispatchMouseEvent',
      ' Input.dispatchMouseEvent',
      'Input.dispatchMouseEvent ',
      'Input.dispatchMouseEvent\n',
    ]) {
      expect(cdpRefusal(method), JSON.stringify(method)).not.toBe('')
    }
    expect(cdpRefusal('')).toContain('a method is needed')
    expect(cdpRefusal(undefined)).toContain('a method is needed')
    expect(cdpRefusal({ toString: () => 'Input.dispatchMouseEvent' })).toContain('a method is needed')
  })

  it('lists every method by its whole name, so that no prefix lets a new one through', () => {
    for (const method of CDP_ALLOWED) {
      expect(method).toMatch(/^[A-Z][A-Za-z]*\.[a-zA-Z][A-Za-z0-9]*$/)
      expect(CDP_FILE_METHODS).not.toContain(method)
    }
    expect(new Set(CDP_ALLOWED).size).toBe(CDP_ALLOWED.length)
  })
})

describe('the addresses the agent may open', () => {
  it('opens an ordinary site', () => {
    expect(addressRefusal('https://shop.example.org/cart?id=3', server)).toBe('')
    expect(addressRefusal('http://example.net:8080/', server)).toBe('')
  })

  it('refuses a scheme that is not http or https', () => {
    for (const address of [
      'file:///etc/hosts',
      'javascript:alert(1)',
      'data:text/html,hi',
      'chrome://settings',
      'about:blank',
      'ftp://example.com/',
      'example.com',
      '',
    ]) {
      expect(addressRefusal(address, server), address).toContain('only http and https')
    }
  })

  it('refuses the server and the extension, however the address is written', () => {
    for (const address of [
      'https://mail.example.com/',
      'http://mail.example.com/dashboard',
      'https://MAIL.Example.COM/',
      'https://mail.example.com:8443/x',
      'https://mail.example.com./x',
      'https://someone@mail.example.com/x',
      'https://mail.example.com\\@example.org/',
      'https://example.org@\tmail.example.com/',
      'https://mail.example.com%2e/x',
    ]) {
      expect(addressRefusal(address, server), address).toBe(OWN_PAGES_REFUSAL)
    }
    expect(addressRefusal('chrome-extension://abcdefgh/options.html', server)).toContain('only http and https')
  })

  it('compares a server written without a scheme, or with a path', () => {
    expect(addressRefusal('https://mail.example.com/', 'mail.example.com')).toBe(OWN_PAGES_REFUSAL)
    expect(addressRefusal('https://mail.example.com/', 'https://mail.example.com/some/path/')).toBe(OWN_PAGES_REFUSAL)
    expect(serverHostname('')).toBe('')
  })

  it('does not take another site for the server', () => {
    expect(addressRefusal('https://mail.example.com.example.org/', server)).toBe('')
    expect(addressRefusal('https://notmail.example.com/', server)).toBe('')
    expect(addressRefusal('https://example.com/', server)).toBe('')
  })
})

describe('the pages the agent does not drive', () => {
  it('knows the server, the extension and a blob made by either', () => {
    expect(isOwnPage('https://mail.example.com/drawer', server)).toBe(true)
    expect(isOwnPage('chrome-extension://abcdefgh/options.html', server)).toBe(true)
    expect(isOwnPage('blob:https://mail.example.com/1234', server)).toBe(true)
    expect(isOwnPage('blob:chrome-extension://abcdefgh/1234', server)).toBe(true)
    expect(isOwnPage('https://shop.example.org/', server)).toBe(false)
    expect(isOwnPage('', server)).toBe(false)
    expect(isOwnPage(undefined, server)).toBe(false)
    expect(isOwnPage('about:blank', server)).toBe(false)
  })

  it('refuses every action in such a tab but the ones that are not in a page', () => {
    const everyAction = [
      'navigate',
      'snapshot',
      'screenshot',
      'click',
      'hover',
      'select',
      'type',
      'press',
      'scroll',
      'wait',
      'back',
      'evaluate',
      'fetch',
      'storage',
      'cdp',
      'cdp_events',
    ]
    for (const url of ['https://mail.example.com/', 'chrome-extension://abcdefgh/options.html']) {
      for (const action of everyAction) {
        expect(pageRefusal(action, { url }, server), `${action} ${url}`).toBe(OWN_PAGES_REFUSAL)
      }
      // A page on its way there is as good as there.
      expect(pageRefusal('click', { url: 'https://shop.example.org/', pendingUrl: url }, server)).toBe(
        OWN_PAGES_REFUSAL,
      )
    }
    expect([...ACTIONS_OUTSIDE_A_PAGE].sort()).toEqual(['cdp_stop', 'close', 'open', 'switch', 'tabs'])
    for (const action of ACTIONS_OUTSIDE_A_PAGE) {
      expect(pageRefusal(action, { url: 'https://mail.example.com/' }, server)).toBe('')
    }
  })

  it('does every action in an ordinary site', () => {
    for (const action of ['navigate', 'snapshot', 'click', 'evaluate', 'cdp', 'fetch', 'storage']) {
      expect(pageRefusal(action, { url: 'https://shop.example.org/' }, server), action).toBe('')
    }
  })
})

describe("the person's TeaNode panel", () => {
  const panel = { isOpen: true, left: 840, top: 20, right: 1280, bottom: 780, hasFocus: false }

  it('refuses input at a point inside it, edges included, and only there', () => {
    const at = (x, y) => panelRefusal('Input.dispatchMouseEvent', { type: 'mousePressed', x, y }, panel)
    expect(at(900, 100)).toBe(PANEL_REFUSAL)
    expect(at(840, 20)).toBe(PANEL_REFUSAL)
    expect(at(1280, 780)).toBe(PANEL_REFUSAL)
    expect(at(839.9, 100)).toBe('')
    expect(at(900, 19)).toBe('')
    expect(at(900, 781)).toBe('')
  })

  it('reads the points of a touch, a tap and a scroll gesture as well', () => {
    expect(
      panelRefusal(
        'Input.dispatchTouchEvent',
        {
          touchPoints: [
            { x: 1, y: 1 },
            { x: 900, y: 100 },
          ],
        },
        panel,
      ),
    ).toBe(PANEL_REFUSAL)
    expect(panelRefusal('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] }, panel)).toBe('')
    expect(panelRefusal('Input.synthesizeTapGesture', { x: 900, y: 100 }, panel)).toBe(PANEL_REFUSAL)
    expect(panelRefusal('Input.synthesizeScrollGesture', { x: 900, y: 100, yDistance: 5 }, panel)).toBe(PANEL_REFUSAL)
    expect(panelRefusal('Input.synthesizeScrollGesture', { x: 100, y: 100, yDistance: 5 }, panel)).toBe('')
  })

  it('refuses a point that is not a pair of numbers', () => {
    for (const params of [{}, { x: '900', y: '100' }, { x: NaN, y: 1 }, { x: 1 }, undefined]) {
      expect(panelRefusal('Input.dispatchMouseEvent', params, panel), JSON.stringify(params)).toBe(
        'x and y must be numbers',
      )
    }
  })

  it('refuses keys only while its frame has focus', () => {
    for (const method of ['Input.dispatchKeyEvent', 'Input.insertText']) {
      expect(panelRefusal(method, {}, panel)).toBe('')
      expect(panelRefusal(method, {}, { ...panel, hasFocus: true })).toBe(PANEL_KEYS_REFUSAL)
    }
  })

  it('says nothing about a closed panel, no panel, or what is not input', () => {
    expect(panelRefusal('Input.dispatchMouseEvent', { x: 900, y: 100 }, { isOpen: false })).toBe('')
    expect(panelRefusal('Input.dispatchMouseEvent', { x: 900, y: 100 }, undefined)).toBe('')
    expect(panelRefusal('Runtime.evaluate', { expression: '1' }, panel)).toBe('')
    expect(panelRefusal('Network.enable', {}, panel)).toBe('')
  })

  it('knows when a box touches it', () => {
    expect(overlapsPanel({ left: 900, right: 1000, top: 100, bottom: 130 }, panel)).toBe(true)
    expect(overlapsPanel({ left: 800, right: 840, top: 100, bottom: 130 }, panel)).toBe(true)
    expect(overlapsPanel({ left: 800, right: 839, top: 100, bottom: 130 }, panel)).toBe(false)
    expect(overlapsPanel({ left: 900, right: 1000, top: 100, bottom: 130 }, { isOpen: false })).toBe(false)
  })

  describe('found on the page', () => {
    afterEach(() => {
      document.body.innerHTML = ''
      delete window.__teanodePanel
    })

    const putPanel = (id, box) => {
      const host = document.createElement('div')
      host.id = id
      host.getBoundingClientRect = () => box
      document.body.append(host)
      return host
    }

    it('by the element panel.js kept, whatever the page renamed it to', () => {
      window.__teanodePanel = putPanel('renamed-by-the-page', {
        left: 10,
        top: 20,
        right: 450,
        bottom: 780,
        width: 440,
        height: 760,
      })
      expect(panelProbe()).toEqual({ isOpen: true, left: 10, top: 20, right: 450, bottom: 780, hasFocus: false })
    })

    it('by its id, for a panel put there before the extension was updated', () => {
      putPanel('teanode-agent-panel', { left: 10, top: 20, right: 450, bottom: 780, width: 440, height: 760 })
      expect(panelProbe()).toMatchObject({ isOpen: true, left: 10 })
    })

    it('says whether its frame has focus', () => {
      const host = putPanel('teanode-agent-panel', { left: 0, top: 0, right: 10, bottom: 10, width: 10, height: 10 })
      host.tabIndex = 0
      host.focus()
      expect(panelProbe().hasFocus).toBe(true)
    })

    it('is not open when it is gone, closed, or not showing', () => {
      expect(panelProbe()).toEqual({ isOpen: false })
      const host = putPanel('teanode-agent-panel', { left: 0, top: 0, right: 0, bottom: 0, width: 0, height: 0 })
      expect(panelProbe()).toEqual({ isOpen: false })
      host.getBoundingClientRect = () => ({ left: 0, top: 0, right: 10, bottom: 10, width: 10, height: 10 })
      host.style.visibility = 'hidden'
      expect(panelProbe()).toEqual({ isOpen: false })
      host.style.visibility = ''
      host.remove()
      window.__teanodePanel = host
      expect(panelProbe()).toEqual({ isOpen: false })
    })
  })
})

describe('what reads through frames and contexts', () => {
  it('is refused by the parameters of an allowed method', () => {
    for (const [method, params] of [
      ['DOM.getDocument', { depth: -1, pierce: true }],
      ['DOM.describeNode', { nodeId: 1, pierce: true }],
      ['DOM.querySelectorAll', { nodeId: 1, selector: 'iframe', pierce: true }],
      ['Runtime.evaluate', { expression: '1', contextId: 3 }],
      ['Runtime.evaluate', { expression: '1', uniqueContextId: 'a' }],
      ['Runtime.callFunctionOn', { functionDeclaration: 'f', executionContextId: 3 }],
      ['DOM.resolveNode', { nodeId: 1, executionContextId: 3 }],
      ['Accessibility.getFullAXTree', { frameId: 'A1' }],
      ['Page.navigate', { url: 'https://shop.example.org/', frameId: 'A1' }],
    ]) {
      expect(cdpParametersRefusal(method, params), `${method} ${JSON.stringify(params)}`).not.toBe('')
    }
  })

  it('leaves the ordinary calls alone', () => {
    for (const [method, params] of [
      ['DOM.getDocument', { depth: -1 }],
      ['DOM.getDocument', { depth: -1, pierce: false }],
      ['Runtime.evaluate', { expression: '1' }],
      ['Accessibility.getFullAXTree', undefined],
      ['Page.navigate', { url: 'https://shop.example.org/' }],
    ]) {
      expect(cdpParametersRefusal(method, params), method).toBe('')
    }
  })
})

describe("a fetch with the page's session", () => {
  it('goes only where a tab may go, resolved as the page resolves it', () => {
    const base = 'https://app.example.com/page'
    for (const address of [
      'https://mail.example.com/api',
      '//mail.example.com/x',
      'http://MAIL.example.com:8443/x',
      'file:///etc/hosts',
      'javascript:1',
      '',
    ]) {
      expect(fetchRefusal(address, base, server), address).not.toBe('')
    }
    for (const address of [
      '/statements.pdf',
      'next?id=3',
      'https://app.example.com/x',
      'https://cdn.example.net/a.json',
    ]) {
      expect(fetchRefusal(address, base, server), address).toBe('')
    }
  })
})

describe('what the tab watched', () => {
  // The record is what the worker learns as the events arrive.
  const watch = (events) => {
    const record = newRequestRecord()
    for (const event of events) noteRequestEvent(record, event.method, event.params)
    return record
  }
  const created = (requestId, address) => ({
    method: 'Network.requestWillBeSent',
    params: { requestId, request: { url: address } },
  })
  const socketCreated = (requestId, address) => ({
    method: 'Network.webSocketCreated',
    params: { requestId, url: address },
  })
  const frameSent = (requestId, text) => ({
    method: 'Network.webSocketFrameSent',
    params: { requestId, response: { opcode: 1, payloadData: text } },
  })

  const events = [
    {
      method: 'Network.requestWillBeSentExtraInfo',
      params: { requestId: '1.1', headers: { authorization: 'Bearer example-token' } },
    },
    created('1.1', 'https://mail.example.com/api/v1/graphql'),
    {
      method: 'Network.responseReceived',
      params: { requestId: '1.1', response: { url: 'https://mail.example.com/api/v1/graphql' } },
    },
    created('2.2', 'https://cdn.example.net/a.js'),
    { method: 'Log.entryAdded', params: { entry: { text: 'a message' } } },
  ]

  it('leaves out every request to the server, whichever event of it came first', () => {
    const record = watch(events)
    expect(networkEventsShown(record, events, server).map((event) => event.method)).toEqual([
      'Network.requestWillBeSent',
      'Log.entryAdded',
    ])
    expect(JSON.stringify(networkEventsShown(record, events, server))).not.toContain('example-token')
    expect(isRequestShown(record, '1.1', server)).toBe(false)
    expect(isRequestShown(record, '2.2', server)).toBe(true)
  })

  it("shows the frames of a connection to another host and hides the server's, however many events came between", () => {
    const early = [
      socketCreated('5.1', 'wss://mail.example.com/api/v1/socket'),
      socketCreated('5.2', 'wss://live.example.net/feed'),
    ]
    const record = watch(early)
    // A page that floods the few events the worker keeps pushes both creations out.
    const flood = Array.from({ length: 1100 }, (_, index) => created(`9.${index}`, 'https://cdn.example.net/a.js'))
    for (const event of flood) noteRequestEvent(record, event.method, event.params)
    const kept = [...flood.slice(-500 + 2), frameSent('5.1', 'Bearer example-token'), frameSent('5.2', 'hello')]
    const shown = networkEventsShown(record, kept, server)
    expect(JSON.stringify(shown)).not.toContain('example-token')
    expect(JSON.stringify(shown)).toContain('hello')
  })

  it('hides a connection whose creation the worker never saw', () => {
    const record = newRequestRecord()
    const unseen = [
      frameSent('5.1', 'Bearer example-token'),
      { method: 'Network.dataReceived', params: { requestId: '5.3' } },
    ]
    expect(networkEventsShown(record, unseen, server)).toEqual([])
    expect(isRequestShown(record, '5.1', server)).toBe(false)
    expect(isRequestShown(record, undefined, server)).toBe(false)
  })

  it('hides a network event that names no request, and one with a method that is not text', () => {
    const record = watch(events)
    expect(
      networkEventsShown(
        record,
        [{ method: 'Network.policyUpdated', params: {} }, { method: 7, params: {} }, { params: {} }],
        server,
      ),
    ).toEqual([])
  })

  it('hides a request that was sent on to the server, and one that came from it', () => {
    const redirect = (requestId, address, from) => ({
      method: 'Network.requestWillBeSent',
      params: { requestId, request: { url: address }, redirectResponse: { url: from, status: 302 } },
    })
    const record = watch([
      created('3.1', 'https://cdn.example.net/a'),
      redirect('3.1', 'https://mail.example.com/x', 'https://cdn.example.net/a'),
      created('3.2', 'https://mail.example.com/y'),
      redirect('3.2', 'https://cdn.example.net/z', 'https://mail.example.com/y'),
    ])
    expect(isRequestShown(record, '3.1', server)).toBe(false)
    expect(isRequestShown(record, '3.2', server)).toBe(false)
  })

  it('hides what it cannot place: an extension, an address it cannot read, a server it does not know', () => {
    const record = watch([
      created('4.1', 'chrome-extension://abcdef/panel.html'),
      created('4.2', 'http://[bad/'),
      { method: 'Network.requestWillBeSent', params: { requestId: '4.3', request: {} } },
      created('4.4', 'https://cdn.example.net/ok'),
      created('4.5', 'data:text/plain;base64,AAAA'),
    ])
    for (const requestId of ['4.1', '4.2', '4.3'])
      expect(isRequestShown(record, requestId, server), requestId).toBe(false)
    expect(isRequestShown(record, '4.4', server)).toBe(true)
    expect(isRequestShown(record, '4.5', server)).toBe(true)
    expect(isRequestShown(record, '4.4', '')).toBe(false)
  })

  it('compares the server by name, without the scheme or the port', () => {
    const record = watch([
      created('6.1', 'wss://MAIL.example.com.:8443/socket'),
      created('6.2', 'blob:https://mail.example.com/0f1e'),
    ])
    expect(isRequestShown(record, '6.1', server)).toBe(false)
    expect(isRequestShown(record, '6.2', server)).toBe(false)
  })

  it('forgets the oldest requests past the limit, and a forgotten one is not shown', () => {
    const record = newRequestRecord()
    for (let index = 0; index < REQUESTS_KEPT + 3; index++) {
      noteRequestEvent(record, 'Network.requestWillBeSent', {
        requestId: `7.${index}`,
        request: { url: 'https://cdn.example.net/a' },
      })
    }
    expect(record.size).toBe(REQUESTS_KEPT)
    expect(isRequestShown(record, '7.0', server)).toBe(false)
    expect(isRequestShown(record, `7.${REQUESTS_KEPT + 2}`, server)).toBe(true)
  })

  it('says why a body is not read', () => {
    expect(REQUEST_BODY_REFUSAL).toContain('cdp_events')
  })
})

describe('handing the token to the panel frame', () => {
  // The functions are self-contained, so they run here against a window and a
  // location that are made up for the test, as they run against the frame's.
  const origin = 'https://mail.example.com'
  const nonce = '0b9c5f0e6a534d3e8d0a0a1b2c3d4e5f'
  const top = { name: 'the page the panel is on' }
  const frame = (overrides = {}) => {
    const taken = []
    const window = { top, parent: top, __teanodeHandToken: (token) => (taken.push(token), true), ...overrides.window }
    window.self = window
    const location = { origin, pathname: '/drawer', search: `?handoff=${nonce}`, ...overrides.location }
    return { window, location, taken }
  }
  const run = (func, { window, location }, ...args) =>
    new Function('window', 'location', `return (${func.toString()})`)(window, location)(...args)

  it("is handed to the drawer that is the panel's, and says so", () => {
    const here = frame()
    expect(run(handOverToken, here, 'example-token', origin, nonce)).toBe(true)
    expect(here.taken).toEqual(['example-token'])
    expect(run(isPanelFrame, frame(), origin, nonce)).toBe(true)
  })

  it('is not handed to a document that is somewhere else than the panel, whatever it defines', () => {
    for (const [what, overrides] of [
      ['another origin', { location: { origin: 'https://evil.example.org' } }],
      ['a sibling host of the server', { location: { origin: 'https://app.example.com' } }],
      ['another port', { location: { origin: 'https://mail.example.com:8443' } }],
      ['another path', { location: { pathname: '/login' } }],
      ['a path that only starts like it', { location: { pathname: '/drawer/x' } }],
      ['another nonce', { location: { search: '?handoff=0000000000000000' } }],
      ['no nonce', { location: { search: '' } }],
      ['a nonce among others', { location: { search: `?a=1&handoff=${nonce}x` } }],
      ['a parent that is not the top page', { window: { parent: { name: 'a frame in between' } } }],
      ['being the top page itself', { window: { top: undefined } }],
    ]) {
      const here = frame(overrides)
      if (what === 'being the top page itself') here.window.top = here.window
      expect(run(handOverToken, here, 'example-token', origin, nonce), what).toBe(false)
      expect(here.taken, what).toEqual([])
      expect(run(isPanelFrame, here, origin, nonce), what).toBe(false)
    }
  })

  it('is not handed to a function that is not one, or one that does not say it took the token', () => {
    expect(
      run(handOverToken, frame({ window: { __teanodeHandToken: undefined } }), 'example-token', origin, nonce),
    ).toBe(false)
    expect(
      run(handOverToken, frame({ window: { __teanodeHandToken: () => 'yes' } }), 'example-token', origin, nonce),
    ).toBe(false)
    expect(
      run(handOverToken, frame({ window: { __teanodeHandToken: () => false } }), 'example-token', origin, nonce),
    ).toBe(false)
  })

  it("is not handed on when the frame's own location cannot be read", () => {
    const here = frame()
    Object.defineProperty(here.location, 'origin', {
      get() {
        throw new Error('no access')
      },
    })
    expect(run(handOverToken, here, 'example-token', origin, nonce)).toBe(false)
    expect(here.taken).toEqual([])
  })
})
