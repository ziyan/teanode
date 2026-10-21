import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  ACTIONS_OUTSIDE_A_PAGE,
  CDP_ALLOWED,
  CDP_FILE_METHODS,
  OWN_PAGES_REFUSAL,
  PANEL_KEYS_REFUSAL,
  PANEL_REFUSAL,
  addressRefusal,
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
      'Page.captureSnapshot',
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
