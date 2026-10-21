import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { FRAME_CONNECT_KIND, FRAME_MESSAGE_CHANNEL } from './frameChannel'

// The suggestion page runs when it is loaded, so each test loads it afresh
// into a page of its own. The content script's side is played by the test: it
// hands the page one end of a channel and speaks through the other.
let sent: Record<string, unknown>[]
let contentEnd: MessagePort | null
let isVisible: boolean

beforeEach(() => {
  vi.resetModules()
  document.body.className = ''
  document.body.innerHTML = '<div id="app"></div>'
  sent = []
  contentEnd = null
  isVisible = true
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      constructor(private callback: (entries: unknown[]) => void) {}
      observe() {
        this.callback([{ isVisible, intersectionRatio: 1 }])
      }
    },
  )
  vi.spyOn(performance, 'now').mockReturnValue(1000)
})
afterEach(() => {
  contentEnd?.close()
  vi.unstubAllGlobals()
})

const settle = () => new Promise((resolve) => setTimeout(resolve, 10))

// The modules are loaded after the reset, so the page and the test share one
// stand-in for the browser's check.
let trust: typeof import('./pageDom').trust

async function load(): Promise<void> {
  trust = (await import('./pageDom')).trust
  await import('./suggestion')
}

// connect is the content script handing the page its end of a channel.
async function connect(
  options: { source?: Window | null; ports?: MessagePort[]; kind?: string } = {},
): Promise<MessagePort> {
  const channel = new MessageChannel()
  contentEnd = channel.port1
  channel.port1.onmessage = (event) => sent.push(event.data)
  window.dispatchEvent(
    new MessageEvent('message', {
      source: options.source === undefined ? window.parent : options.source,
      data: { channel: FRAME_MESSAGE_CHANNEL, frameMessageKind: options.kind ?? FRAME_CONNECT_KIND },
      ports: options.ports ?? [channel.port2],
    }),
  )
  await settle()
  return channel.port1
}

async function render(data: Record<string, unknown>) {
  contentEnd?.postMessage({ frameMessageKind: 'render', formActionHost: '', ...data })
  // To the page and back.
  await settle()
  await settle()
}

// click presses a button. jsdom's own events are never trusted, so a trusted
// press is one the stand-in for the browser's check is told to believe.
async function click(element: Element, isTrusted: boolean) {
  vi.spyOn(trust, 'isTrusted').mockReturnValue(isTrusted)
  element.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await settle()
}

async function press(key: string, options: { shiftKey?: boolean } = {}) {
  vi.spyOn(trust, 'isTrusted').mockReturnValue(true)
  document.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...options }))
  await settle()
}

const kinds = () => sent.map((message) => message.frameMessageKind)

const logins = [
  {
    id: 'one',
    name: 'Example',
    userName: 'ada',
    siteAddress: 'https://example.test',
    hasPassword: true,
    hasOneTimeCode: false,
    passkeyCount: 0,
    isOwnedByMe: true,
  },
  {
    id: 'two',
    name: 'Other',
    userName: 'grace',
    siteAddress: 'https://example.test',
    hasPassword: true,
    hasOneTimeCode: false,
    passkeyCount: 0,
    isOwnedByMe: true,
  },
]

const sameSite = { role: 'currentPassword', isCrossOrigin: false, topHost: '' }

describe('the suggestion page', () => {
  it('says it is ready, then lists the logins by name and user name', async () => {
    await load()
    await connect()
    expect(sent[0]).toEqual({ frameMessageKind: 'ready' })
    await render({
      answer: { suggestionState: 'logins', host: 'example.test', logins },
      host: 'example.test',
      ...sameSite,
    })
    expect(document.body.textContent).toContain('Example')
    expect(document.querySelector('.detail')?.textContent).toBe('ada')
    expect(kinds()).toContain('size')
  })

  it('takes the first channel that arrives from the window around it, and no other', async () => {
    await load()
    // Not from the window around the frame, and with no port: neither is taken.
    await connect({ source: null })
    await connect({ ports: [] })
    expect(sent).toEqual([])
    const first = await connect()
    expect(sent[0]).toEqual({ frameMessageKind: 'ready' })
    // A second channel, say a page's own, is not taken: what the page hears
    // and says goes to the first only.
    const second = new MessageChannel()
    const heardBySecond: unknown[] = []
    second.port1.onmessage = (event) => heardBySecond.push(event.data)
    window.dispatchEvent(
      new MessageEvent('message', {
        source: window.parent,
        data: { channel: FRAME_MESSAGE_CHANNEL, frameMessageKind: FRAME_CONNECT_KIND },
        ports: [second.port2],
      }),
    )
    first.postMessage({ frameMessageKind: 'render', answer: { suggestionState: 'locked', host: 'x' }, ...sameSite })
    await settle()
    expect(document.querySelector('button.primary')).not.toBeNull()
    expect(heardBySecond).toEqual([])
    second.port1.close()
  })

  it('draws nothing that was posted to its window instead of its channel', async () => {
    await load()
    await connect()
    window.dispatchEvent(
      new MessageEvent('message', {
        source: window.parent,
        data: {
          channel: FRAME_MESSAGE_CHANNEL,
          frameMessageKind: 'render',
          answer: { suggestionState: 'locked', host: 'x' },
          ...sameSite,
        },
      }),
    )
    await settle()
    expect(document.querySelector('button.primary')).toBeNull()
  })

  it('acts on a trusted click once the browser says the frame was visible, and on nothing else', async () => {
    await load()
    await connect()
    await render({
      answer: { suggestionState: 'logins', host: 'example.test', logins },
      host: 'example.test',
      ...sameSite,
    })
    const choice = document.querySelector('button.choice') as HTMLButtonElement
    // A script's click does nothing.
    await click(choice, false)
    // Too soon after being shown.
    await click(choice, true)
    expect(kinds()).not.toContain('choose')
    vi.spyOn(performance, 'now').mockReturnValue(2000)
    await click(choice, true)
    expect(sent.filter((message) => message.frameMessageKind === 'choose')).toEqual([
      { frameMessageKind: 'choose', itemId: 'one', isConfirmed: false },
    ])
  })

  it('never acts when the browser says the frame is not visible', async () => {
    isVisible = false
    await load()
    await connect()
    await render({
      answer: { suggestionState: 'logins', host: 'example.test', logins },
      host: 'example.test',
      ...sameSite,
    })
    vi.spyOn(performance, 'now').mockReturnValue(5000)
    await click(document.querySelector('button.choice') as Element, true)
    expect(kinds()).not.toContain('choose')
  })

  it('asks before filling a frame from another site, and only then chooses', async () => {
    await load()
    await connect()
    await render({
      answer: { suggestionState: 'logins', host: 'idp.example.test', logins },
      host: 'idp.example.test',
      role: 'currentPassword',
      isCrossOrigin: true,
      topHost: 'shop.example.test',
    })
    vi.spyOn(performance, 'now').mockReturnValue(2000)
    await click(document.querySelector('button.choice') as Element, true)
    expect(kinds()).not.toContain('choose')
    expect(document.body.textContent).toContain('idp.example.test')
    expect(document.body.textContent).toContain('shop.example.test')
    await click(document.querySelector('button.primary') as Element, true)
    expect(sent.filter((message) => message.frameMessageKind === 'choose')).toEqual([
      { frameMessageKind: 'choose', itemId: 'one', isConfirmed: true },
    ])
  })

  it('asks before filling a form that sends to another host than the page, and says which', async () => {
    await load()
    await connect()
    await render({
      answer: { suggestionState: 'logins', host: 'example.test', logins },
      host: 'example.test',
      formActionHost: 'collect.example.test',
      ...sameSite,
    })
    vi.spyOn(performance, 'now').mockReturnValue(2000)
    await click(document.querySelector('button.choice') as Element, true)
    expect(kinds()).not.toContain('choose')
    expect(document.body.textContent).toContain('collect.example.test')
    expect(document.body.textContent).toContain('example.test')
    await click(document.querySelector('button.primary') as Element, true)
    expect(sent.filter((message) => message.frameMessageKind === 'choose')).toEqual([
      { frameMessageKind: 'choose', itemId: 'one', isConfirmed: true },
    ])
  })

  it('offers to unlock when locked, and a generated password for a new password field', async () => {
    await load()
    await connect()
    vi.spyOn(performance, 'now').mockReturnValue(2000)
    await render({ answer: { suggestionState: 'locked', host: 'example.test' }, host: 'example.test', ...sameSite })
    await click(document.querySelector('button.primary') as Element, true)
    expect(kinds()).toContain('unlock')
    await render({
      answer: { suggestionState: 'generate', host: 'example.test' },
      host: 'example.test',
      ...sameSite,
      role: 'newPassword',
    })
    await click(document.querySelector('button.primary') as Element, true)
    expect(sent.find((message) => message.frameMessageKind === 'generate')).toEqual({
      frameMessageKind: 'generate',
      isConfirmed: true,
    })
  })
})

describe('the suggestion page and the keyboard', () => {
  async function showTwoLogins() {
    await load()
    await connect()
    await render({
      answer: { suggestionState: 'logins', host: 'example.test', logins },
      host: 'example.test',
      ...sameSite,
    })
  }

  it('puts the focus on the first choice when the content script asks', async () => {
    await showTwoLogins()
    vi.spyOn(window, 'focus').mockImplementation(() => {})
    contentEnd?.postMessage({ frameMessageKind: 'focus' })
    await settle()
    const choices = document.querySelectorAll('button.choice')
    expect(document.activeElement).toBe(choices[0])
  })

  it('moves between the buttons with the arrow keys, round the ends', async () => {
    await showTwoLogins()
    const choices = Array.from(document.querySelectorAll('button.choice')) as HTMLButtonElement[]
    choices[0].focus()
    await press('ArrowDown')
    expect(document.activeElement).toBe(choices[1])
    await press('ArrowUp')
    expect(document.activeElement).toBe(choices[0])
  })

  it('fills with Enter on a choice, once the frame has been visible long enough', async () => {
    await showTwoLogins()
    vi.spyOn(performance, 'now').mockReturnValue(2000)
    const choices = Array.from(document.querySelectorAll('button.choice')) as HTMLButtonElement[]
    choices[1].focus()
    // Enter on a button is a click the browser makes; it is trusted.
    await click(choices[1], true)
    expect(sent.filter((message) => message.frameMessageKind === 'choose')).toEqual([
      { frameMessageKind: 'choose', itemId: 'two', isConfirmed: false },
    ])
  })

  it('asks to go back to the field on Escape, and ignores a key a script pressed', async () => {
    await showTwoLogins()
    vi.spyOn(trust, 'isTrusted').mockReturnValue(false)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await settle()
    expect(kinds()).not.toContain('escape')
    await press('Escape')
    expect(kinds()).toContain('escape')
  })

  it('leaves the frame with Tab from the last button, and Shift+Tab from the first', async () => {
    await showTwoLogins()
    const buttons = Array.from(document.querySelectorAll('button')) as HTMLButtonElement[]
    buttons[0].focus()
    await press('Tab')
    expect(kinds()).not.toContain('leave')
    buttons[buttons.length - 1].focus()
    await press('Tab')
    expect(kinds().filter((kind) => kind === 'leave')).toHaveLength(1)
    buttons[0].focus()
    await press('Tab', { shiftKey: true })
    expect(kinds().filter((kind) => kind === 'leave')).toHaveLength(2)
  })
})
