// @vitest-environment-options {"url": "https://shop.example.test/sign-in"}
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { FRAME_CONNECT_KIND, FRAME_MESSAGE_CHANNEL } from './frameChannel'
import { PASSKEY_CHANNEL } from './passkeyOverride'
import { giveLayout } from './testLayout'

// The content script is loaded afresh for each test, with the browser's
// extension APIs stood in for. The service worker is a function that answers
// the messages the script sends; the suggestion frame is played by the test,
// through the channel the script hands it.

let sendMessage: ReturnType<typeof vi.fn>
let runtimeListener: (message: any, sender: any, sendResponse: (answer: unknown) => void) => unknown
let isActive: boolean
let isTrusted: boolean
let registered: { target: EventTarget; type: string; listener: EventListenerOrEventListenerObject; capture: boolean }[]
let frameConnect: { load: () => void; posted: unknown[][] }
let contentEnd: MessagePort | null
let frameMessages: Record<string, unknown>[]

const loginAnswer = {
  suggestionState: 'logins',
  host: 'shop.example.test',
  logins: [{ id: 'one', name: 'Shop', userName: 'ada' }],
}

function answerFor(message: Record<string, unknown>): unknown {
  switch (message.type) {
    case 'passwords:suggest':
      return loginAnswer
    case 'passwords:fill':
      return { userName: 'ada', password: 'hunter2-plaintext' }
    case 'passwords:fill-code':
      return { oneTimeCode: '123456' }
    default:
      return {}
  }
}

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
  contentEnd = null
  frameMessages = []
  frameConnect = { load: () => {}, posted: [] }
  trackListeners(document)
  trackListeners(window)
  document.body.innerHTML = `<form id="login" action="/session">
    <input id="user" type="text" autocomplete="username">
    <input id="pass" type="password" autocomplete="current-password">
    <button type="submit" id="go">Sign in</button>
  </form>`
  document.documentElement.querySelectorAll(':scope > div').forEach((each) => each.remove())
  Object.defineProperty(window.navigator, 'userActivation', {
    configurable: true,
    get: () => ({ isActive, hasBeenActive: isActive }),
  })
  sendMessage = vi.fn(async (message: Record<string, unknown>) => answerFor(message))
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
  // The frame's window is a stand-in that hands the test what was posted to
  // it; its load event is fired by the test.
  const frameWindow = { postMessage: (...args: unknown[]) => frameConnect.posted.push(args) }
  vi.spyOn(HTMLIFrameElement.prototype, 'contentWindow', 'get').mockReturnValue(frameWindow as unknown as Window)
  const addIframeListener = HTMLIFrameElement.prototype.addEventListener
  vi.spyOn(HTMLIFrameElement.prototype, 'addEventListener').mockImplementation(function (
    this: HTMLIFrameElement,
    type: string,
    listener: any,
    options?: any,
  ) {
    if (type === 'load') frameConnect.load = () => listener(new Event('load'))
    addIframeListener.call(this, type, listener, options)
  } as typeof HTMLIFrameElement.prototype.addEventListener)
  vi.spyOn(HTMLIFrameElement.prototype, 'focus').mockImplementation(() => {})
  // Nothing is drawn in front of the frame.
  ;(document as any).elementsFromPoint = () => [document.documentElement.lastElementChild]
  const pageDom = await import('./pageDom')
  vi.spyOn(pageDom.trust, 'isTrusted').mockImplementation(() => isTrusted)
  await import('./content')
})

afterEach(() => {
  for (const { target, type, listener, capture } of registered) target.removeEventListener(type, listener, capture)
  contentEnd?.close()
  document.documentElement.querySelectorAll(':scope > div').forEach((each) => each.remove())
  delete (document as any).elementsFromPoint
  delete (window.navigator as any).userActivation
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

function field(id: string): HTMLInputElement {
  return document.getElementById(id) as HTMLInputElement
}

// connectFrame plays the frame: it loads, takes the channel the script posted
// to it, and says it is ready.
async function connectFrame() {
  frameConnect.load()
  const [message, origin, transfer] = frameConnect.posted[0] as [Record<string, unknown>, string, MessagePort[]]
  expect(message).toEqual({ channel: FRAME_MESSAGE_CHANNEL, frameMessageKind: FRAME_CONNECT_KIND })
  expect(origin).toBe('chrome-extension://dynamicdynamicdy')
  contentEnd = transfer[0]
  contentEnd.onmessage = (event) => frameMessages.push(event.data)
  contentEnd.postMessage({ frameMessageKind: 'ready' })
  await settle()
  await settle()
}

async function showSuggestion(id = 'user') {
  field(id).focus()
  await settle()
  await connectFrame()
}

describe('showing the suggestion', () => {
  it('asks for one when a field gets the focus right after a key or a click', async () => {
    field('user').focus()
    await settle()
    expect(countOf('passwords:suggest')).toBe(1)
    expect(document.documentElement.querySelector(':scope > div')).not.toBeNull()
  })

  it('shows nothing when a script focused the field with nobody touching anything', async () => {
    isActive = false
    field('user').focus()
    await settle()
    expect(countOf('passwords:suggest')).toBe(0)
    expect(document.documentElement.querySelector(':scope > div')).toBeNull()
  })

  it('shows one when the person presses on a field that already has the focus', async () => {
    isActive = false
    field('user').focus()
    await settle()
    expect(countOf('passwords:suggest')).toBe(0)
    field('user').dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await settle()
    expect(countOf('passwords:suggest')).toBe(1)
  })

  it('does not take a press a script made for the person pressing', async () => {
    isActive = false
    isTrusted = false
    field('user').focus()
    field('user').dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await settle()
    expect(countOf('passwords:suggest')).toBe(0)
  })

  it('is not shown to a frame that never answers', async () => {
    vi.useFakeTimers()
    field('user').focus()
    await vi.advanceTimersByTimeAsync(5)
    frameConnect.load()
    await vi.advanceTimersByTimeAsync(3500)
    expect(document.documentElement.querySelector(':scope > div')).toBeNull()
    vi.useRealTimers()
  })
})

describe('filling from the suggestion', () => {
  it('fills what the service worker hands over, after the frame reports a click', async () => {
    await showSuggestion()
    expect(frameMessages.map((message) => message.frameMessageKind)).toEqual(['render'])
    contentEnd?.postMessage({ frameMessageKind: 'choose', itemId: 'one', isConfirmed: false })
    await settle()
    await settle()
    expect(field('user').value).toBe('ada')
    expect(field('pass').value).toBe('hunter2-plaintext')
  })

  it('renders the frame only when the frame says it is ready, with the facts of the form', async () => {
    field('login').setAttribute('action', 'https://collect.example.test/session')
    await showSuggestion()
    const rendered = await new Promise<Record<string, unknown>>((resolve) => {
      contentEnd!.onmessage = (event) => resolve(event.data)
      contentEnd!.postMessage({ frameMessageKind: 'ready' })
    })
    expect(rendered).toMatchObject({
      frameMessageKind: 'render',
      formActionHost: 'collect.example.test',
      isCrossOrigin: false,
      host: 'shop.example.test',
    })
  })

  it('fills a form that sends elsewhere only after the person said yes in the frame', async () => {
    field('login').setAttribute('action', 'https://collect.example.test/session')
    await showSuggestion()
    contentEnd?.postMessage({ frameMessageKind: 'choose', itemId: 'one', isConfirmed: false })
    await settle()
    await settle()
    expect(countOf('passwords:fill')).toBe(0)
    expect(field('pass').value).toBe('')
    contentEnd?.postMessage({ frameMessageKind: 'choose', itemId: 'one', isConfirmed: true })
    await settle()
    await settle()
    expect(countOf('passwords:fill')).toBe(1)
    expect(field('pass').value).toBe('hunter2-plaintext')
  })

  it('reads the form action from the attribute, not from a field named action', async () => {
    document.getElementById('login')!.insertAdjacentHTML('beforeend', '<input name="action" value="x">')
    field('login').setAttribute('action', 'https://collect.example.test/session')
    await showSuggestion()
    contentEnd?.postMessage({ frameMessageKind: 'choose', itemId: 'one', isConfirmed: false })
    await settle()
    await settle()
    expect(countOf('passwords:fill')).toBe(0)
  })

  it('does nothing for a click when something is drawn over the frame', async () => {
    await showSuggestion()
    ;(document as any).elementsFromPoint = () => [document.body]
    contentEnd?.postMessage({ frameMessageKind: 'choose', itemId: 'one', isConfirmed: false })
    await settle()
    await settle()
    expect(countOf('passwords:fill')).toBe(0)
    expect(document.documentElement.querySelector(':scope > div')).toBeNull()
  })
})

describe('the keyboard', () => {
  it('moves the focus into the frame on the down arrow, from the field with the suggestion open', async () => {
    await showSuggestion()
    const event = new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true, cancelable: true })
    field('user').dispatchEvent(event)
    await settle()
    expect(event.defaultPrevented).toBe(true)
    expect(HTMLIFrameElement.prototype.focus).toHaveBeenCalled()
    expect(frameMessages.map((message) => message.frameMessageKind)).toContain('focus')
  })

  it('ignores a down arrow a script pressed, and one in another field', async () => {
    await showSuggestion()
    isTrusted = false
    field('user').dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
    isTrusted = true
    field('pass').dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
    await settle()
    expect(HTMLIFrameElement.prototype.focus).not.toHaveBeenCalled()
  })

  it('closes the suggestion and gives the focus back to the field on Escape in the frame, without opening it again', async () => {
    await showSuggestion()
    expect(countOf('passwords:suggest')).toBe(1)
    contentEnd?.postMessage({ frameMessageKind: 'escape' })
    await settle()
    await settle()
    expect(document.documentElement.querySelector(':scope > div')).toBeNull()
    expect(document.activeElement).toBe(field('user'))
    expect(countOf('passwords:suggest')).toBe(1)
  })

  it('does the same when the person tabs out of the frame', async () => {
    await showSuggestion()
    contentEnd?.postMessage({ frameMessageKind: 'leave' })
    await settle()
    await settle()
    expect(document.documentElement.querySelector(':scope > div')).toBeNull()
    expect(document.activeElement).toBe(field('user'))
    expect(countOf('passwords:suggest')).toBe(1)
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

describe('filling from the window', () => {
  const fields = {
    type: 'passwords:fill-fields',
    origin: 'https://shop.example.test',
    isForeignActionConfirmed: false,
    userName: 'ada',
    password: 'hunter2-plaintext',
    oneTimeCode: '',
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
