// @vitest-environment-options {"url": "https://shop.example.test/sign-in"}
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PASSKEY_CHANNEL } from './passkeyOverride'
import { giveLayout } from './testLayout'

// The content script is loaded afresh for each test, with the browser's
// extension APIs stood in for. The service worker is a function that answers
// the messages the script sends.

let sendMessage: ReturnType<typeof vi.fn>
let runtimeListener: (message: any, sender: any, sendResponse: (answer: unknown) => void) => unknown
let isActive: boolean
let isTrusted: boolean
let registered: { target: EventTarget; type: string; listener: EventListenerOrEventListenerObject; capture: boolean }[]

const settle = () => new Promise((resolve) => setTimeout(resolve, 10))
const sentTypes = () => sendMessage.mock.calls.map(([message]) => message.type)
const countOf = (type: string) => sentTypes().filter((each) => each === type).length

function trackListeners(target: EventTarget) {
  const original = target.addEventListener.bind(target)
  vi.spyOn(target, 'addEventListener').mockImplementation(((type: string, listener: any, options?: any) => {
    registered.push({ target, type, listener, capture: options === true || options?.capture === true })
    original(type, listener, options)
  }) as typeof target.addEventListener)
}

beforeEach(async () => {
  vi.resetModules()
  giveLayout()
  registered = []
  isActive = true
  isTrusted = true
  trackListeners(document)
  trackListeners(window)
  document.body.innerHTML = `<form id="login" action="/session">
    <input id="user" type="text" autocomplete="username">
    <input id="pass" type="password" autocomplete="current-password">
    <input id="note" type="text" name="note">
    <button type="submit" id="go">Sign in</button>
  </form>`
  Object.defineProperty(window.navigator, 'userActivation', {
    configurable: true,
    get: () => ({ isActive, hasBeenActive: isActive }),
  })
  sendMessage = vi.fn(async () => ({}))
  vi.stubGlobal('chrome', {
    runtime: {
      id: 'ext',
      getURL: (path: string) => `chrome-extension://dynamicdynamicdy/${path}`,
      sendMessage,
      onMessage: {
        addListener: (listener: typeof runtimeListener) => {
          runtimeListener = listener
        },
      },
    },
  })
  const pageDom = await import('./pageDom')
  vi.spyOn(pageDom.trust, 'isTrusted').mockImplementation(() => isTrusted)
  await import('./content')
})

afterEach(() => {
  for (const { target, type, listener, capture } of registered) target.removeEventListener(type, listener, capture)
  delete (window.navigator as any).userActivation
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

function field(id: string): HTMLInputElement {
  return document.getElementById(id) as HTMLInputElement
}

describe('a sign-in field getting the focus', () => {
  it('tells the service worker this frame holds the form, once for each field, with nothing typed', async () => {
    field('user').focus()
    field('pass').focus()
    field('pass').blur()
    field('pass').focus()
    await settle()
    expect(sendMessage.mock.calls.map(([message]) => message)).toEqual([
      { type: 'passwords:field-focused' },
      { type: 'passwords:field-focused' },
    ])
  })

  it('says nothing for a field that is no sign-in field', async () => {
    field('note').focus()
    await settle()
    expect(countOf('passwords:field-focused')).toBe(0)
  })

  it('draws nothing on the page', async () => {
    const before = document.documentElement.innerHTML
    field('user').focus()
    await settle()
    expect(document.documentElement.innerHTML).toBe(before)
  })
})

describe('remembering a sign-in', () => {
  function type() {
    field('user').value = 'ada'
    field('pass').value = 'hunter2-plaintext'
  }

  it('sends the form that was submitted right after a key or a click', () => {
    type()
    field('login').dispatchEvent(new Event('submit', { bubbles: true }))
    expect(sendMessage).toHaveBeenCalledWith({
      type: 'passwords:captured',
      userName: 'ada',
      password: 'hunter2-plaintext',
      isNewPassword: false,
    })
  })

  it('sends nothing for a form a script submitted with nobody touching anything', () => {
    isActive = false
    type()
    field('login').dispatchEvent(new Event('submit', { bubbles: true }))
    expect(countOf('passwords:captured')).toBe(0)
  })

  it('sends nothing for an Enter or a click a script made, or without a gesture', () => {
    type()
    isActive = false
    field('pass').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    expect(countOf('passwords:captured')).toBe(0)
    isActive = true
    isTrusted = false
    field('pass').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    expect(countOf('passwords:captured')).toBe(0)
    isTrusted = true
    field('pass').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    expect(countOf('passwords:captured')).toBe(1)
  })

  it('sends nothing for a click on a button with no form when no gesture was made', () => {
    document.body.innerHTML = `<input id="user" type="text" autocomplete="username"><input id="pass" type="password"><span role="button" id="go">Go</span>`
    type()
    isActive = false
    document.getElementById('go')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    expect(countOf('passwords:captured')).toBe(0)
    isActive = true
    document.getElementById('go')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    expect(countOf('passwords:captured')).toBe(1)
  })
})

describe('filling from the dropdown', () => {
  const fields = {
    type: 'passwords:fill-fields',
    origin: 'https://shop.example.test',
    isForeignActionConfirmed: false,
    userName: 'ada',
    password: 'hunter2-plaintext',
    oneTimeCode: '',
    newPassword: '',
  }

  function ask(message: Record<string, unknown>) {
    return new Promise<any>((resolve) => runtimeListener(message, { id: 'ext' }, resolve))
  }

  it('fills the form when the page is at the origin the login was approved for', async () => {
    expect(await ask(fields)).toEqual({ filledCount: 2 })
    expect(field('pass').value).toBe('hunter2-plaintext')
  })

  it('fills nothing in a page at another origin (another port, another scheme)', async () => {
    for (const origin of ['https://shop.example.test:8443', 'http://shop.example.test', 'https://other.example.test']) {
      expect(await ask({ ...fields, origin }), origin).toEqual({ filledCount: 0 })
    }
    expect(field('pass').value).toBe('')
  })

  it("names the host a form sends to when it is not the page's, and fills only once told to", async () => {
    field('login').setAttribute('action', 'https://collect.example.test/session')
    expect(await ask(fields)).toEqual({ filledCount: 0, foreignActionHost: 'collect.example.test' })
    expect(field('pass').value).toBe('')
    expect(await ask({ ...fields, isForeignActionConfirmed: true })).toEqual({ filledCount: 2 })
  })

  it('tells the service worker whether a page above the frame has another origin, and the top host', async () => {
    expect(await ask({ type: 'passwords:frame-facts' })).toEqual({ isCrossOrigin: false, topHost: 'shop.example.test' })
  })

  it('answers only the extension', () => {
    expect(runtimeListener(fields, { id: 'someone-else' }, () => {})).toBe(false)
  })
})

describe('passkey requests from the page', () => {
  function post(data: Record<string, unknown>) {
    window.dispatchEvent(new MessageEvent('message', { source: window, data: { channel: PASSKEY_CHANNEL, ...data } }))
  }

  it('passes a request on with the kind of ceremony, and nothing the page said about its origin', async () => {
    post({ direction: 'request', id: 'a', ceremonyKind: 'get', options: { rpId: 'shop.example.test' }, origin: 'evil' })
    await settle()
    expect(sendMessage).toHaveBeenCalledWith({
      type: 'passwords:passkey',
      ceremonyKind: 'get',
      options: { rpId: 'shop.example.test' },
      isCrossOrigin: false,
    })
  })

  it('tells the service worker when the page withdraws a request', async () => {
    post({ direction: 'cancel', id: 'a' })
    await settle()
    expect(sendMessage).toHaveBeenCalledWith({ type: 'passwords:passkey-cancel' })
  })

  it('ignores a message that is not from its own window', async () => {
    window.dispatchEvent(
      new MessageEvent('message', { source: null, data: { channel: PASSKEY_CHANNEL, direction: 'cancel', id: 'a' } }),
    )
    await settle()
    expect(countOf('passwords:passkey-cancel')).toBe(0)
  })
})

describe('a user gesture for a passkey request', () => {
  function post(data: Record<string, unknown>) {
    window.dispatchEvent(new MessageEvent('message', { source: window, data: { channel: PASSKEY_CHANNEL, ...data } }))
  }

  it('is needed: with no key or click just made, the browser handles the request and the service worker hears nothing', async () => {
    const replies: Record<string, unknown>[] = []
    const listener = (event: MessageEvent) => {
      if (event.data?.direction === 'reply') replies.push(event.data)
    }
    window.addEventListener('message', listener)
    isActive = false
    post({ direction: 'request', id: 'a', ceremonyKind: 'get', options: {} })
    await settle()
    window.removeEventListener('message', listener)
    expect(countOf('passwords:passkey')).toBe(0)
    expect(replies).toEqual([
      { channel: PASSKEY_CHANNEL, direction: 'reply', id: 'a', reply: { replyOutcome: 'fallback' } },
    ])
    isActive = true
    post({ direction: 'request', id: 'b', ceremonyKind: 'create', options: {} })
    await settle()
    expect(countOf('passwords:passkey')).toBe(1)
  })
})

describe('a sandboxed document', () => {
  // Its origin is "null" whatever site serves it.
  beforeEach(() => {
    Object.defineProperty(window, 'origin', { configurable: true, value: 'null' })
  })

  afterEach(() => {
    delete (window as any).origin
  })

  it('says nothing about its fields, and remembers no sign-in', async () => {
    field('user').focus()
    await settle()
    expect(countOf('passwords:field-focused')).toBe(0)
    field('user').value = 'ada'
    field('pass').value = 'hunter2-plaintext'
    field('login').dispatchEvent(new Event('submit', { bubbles: true }))
    expect(countOf('passwords:captured')).toBe(0)
  })

  it('is filled with nothing, even when the address is the one a login was approved for', async () => {
    const answer = await new Promise<any>((resolve) =>
      runtimeListener(
        {
          type: 'passwords:fill-fields',
          origin: 'https://shop.example.test',
          isForeignActionConfirmed: false,
          userName: 'ada',
          password: 'hunter2-plaintext',
          oneTimeCode: '',
          newPassword: '',
        },
        { id: 'ext' },
        resolve,
      ),
    )
    expect(answer).toEqual({ filledCount: 0 })
    expect(field('pass').value).toBe('')
  })

  it('sends no passkey request on', async () => {
    window.dispatchEvent(
      new MessageEvent('message', {
        source: window,
        data: { channel: PASSKEY_CHANNEL, direction: 'request', id: 'a', ceremonyKind: 'get', options: {} },
      }),
    )
    await settle()
    expect(countOf('passwords:passkey')).toBe(0)
  })
})

describe('a generated password', () => {
  const fields = {
    type: 'passwords:fill-fields',
    origin: 'https://shop.example.test',
    isForeignActionConfirmed: true,
    userName: '',
    password: '',
    oneTimeCode: '',
    newPassword: 'generated-example',
  }

  beforeEach(() => {
    document.body.innerHTML = `<form id="signup" action="/account">
      <input id="user" type="text" autocomplete="username">
      <input id="new" type="password" autocomplete="new-password">
      <input id="again" type="password" autocomplete="new-password">
    </form>`
  })

  it('goes into the new password field and its confirmation, and nowhere else', async () => {
    field('new').focus()
    const answer = await new Promise<any>((resolve) => runtimeListener(fields, { id: 'ext' }, resolve))
    expect(answer).toEqual({ filledCount: 2 })
    expect(field('new').value).toBe('generated-example')
    expect(field('again').value).toBe('generated-example')
    expect(field('user').value).toBe('')
  })
})
