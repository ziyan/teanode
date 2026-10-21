import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { FRAME_MESSAGE_CHANNEL } from './frameChannel'

// The suggestion page runs when it is loaded, so each test loads it afresh
// into a page of its own.
let sent: Record<string, unknown>[]
let isVisible: boolean

beforeEach(() => {
  vi.resetModules()
  document.body.className = ''
  document.body.innerHTML = '<div id="app"></div>'
  sent = []
  isVisible = true
  vi.spyOn(window, 'postMessage').mockImplementation(((message: Record<string, unknown>) => {
    sent.push(message)
  }) as typeof window.postMessage)
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
  vi.unstubAllGlobals()
})

// The modules are loaded after the reset, so the page and the test share one
// stand-in for the browser's check.
let trust: typeof import('./pageDom').trust

async function load(): Promise<void> {
  trust = (await import('./pageDom')).trust
  await import('./suggestion')
}

function render(data: Record<string, unknown>) {
  window.dispatchEvent(
    new MessageEvent('message', {
      source: window.parent,
      data: { channel: FRAME_MESSAGE_CHANNEL, kind: 'render', ...data },
    }),
  )
}

// click presses a button. jsdom's own events are never trusted, so a trusted
// press is one the stand-in for the browser's check is told to believe.
function click(element: Element, isTrusted: boolean) {
  vi.spyOn(trust, 'isTrusted').mockReturnValue(isTrusted)
  element.dispatchEvent(new MouseEvent('click', { bubbles: true }))
}

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
]

describe('the suggestion page', () => {
  it('says it is ready, then lists the logins by name and user name', async () => {
    await load()
    expect(sent[0]).toMatchObject({ channel: FRAME_MESSAGE_CHANNEL, kind: 'ready' })
    render({
      answer: { state: 'logins', host: 'example.test', logins },
      role: 'currentPassword',
      host: 'example.test',
      isCrossOrigin: false,
      topHost: '',
    })
    expect(document.body.innerText ?? document.body.textContent).toContain('Example')
    expect(document.querySelector('.detail')?.textContent).toBe('ada')
    expect(sent.some((message) => message.kind === 'size')).toBe(true)
  })

  it('acts on a trusted click once the browser says the frame was visible, and on nothing else', async () => {
    await load()
    render({
      answer: { state: 'logins', host: 'example.test', logins },
      role: 'currentPassword',
      host: 'example.test',
      isCrossOrigin: false,
      topHost: '',
    })
    const choice = document.querySelector('button.choice') as HTMLButtonElement
    // A script's click does nothing.
    click(choice, false)
    // Too soon after being shown.
    click(choice, true)
    expect(sent.some((message) => message.kind === 'choose')).toBe(false)
    vi.spyOn(performance, 'now').mockReturnValue(2000)
    click(choice, true)
    expect(sent.filter((message) => message.kind === 'choose')).toEqual([
      { channel: FRAME_MESSAGE_CHANNEL, kind: 'choose', itemId: 'one', isConfirmed: false },
    ])
  })

  it('never acts when the browser says the frame is not visible', async () => {
    isVisible = false
    await load()
    render({
      answer: { state: 'logins', host: 'example.test', logins },
      role: 'currentPassword',
      host: 'example.test',
      isCrossOrigin: false,
      topHost: '',
    })
    vi.spyOn(performance, 'now').mockReturnValue(5000)
    click(document.querySelector('button.choice') as Element, true)
    expect(sent.some((message) => message.kind === 'choose')).toBe(false)
  })

  it('asks before filling a frame from another site, and only then chooses', async () => {
    await load()
    render({
      answer: { state: 'logins', host: 'idp.example.test', logins },
      role: 'currentPassword',
      host: 'idp.example.test',
      isCrossOrigin: true,
      topHost: 'shop.example.test',
    })
    vi.spyOn(performance, 'now').mockReturnValue(2000)
    click(document.querySelector('button.choice') as Element, true)
    expect(sent.some((message) => message.kind === 'choose')).toBe(false)
    expect(document.body.textContent).toContain('idp.example.test')
    expect(document.body.textContent).toContain('shop.example.test')
    click(document.querySelector('button.primary') as Element, true)
    expect(sent.filter((message) => message.kind === 'choose')).toEqual([
      { channel: FRAME_MESSAGE_CHANNEL, kind: 'choose', itemId: 'one', isConfirmed: true },
    ])
  })

  it('offers to unlock when locked, and a generated password for a new password field', async () => {
    await load()
    vi.spyOn(performance, 'now').mockReturnValue(2000)
    render({
      answer: { state: 'locked', host: 'example.test' },
      role: 'currentPassword',
      host: 'example.test',
      isCrossOrigin: false,
      topHost: '',
    })
    click(document.querySelector('button.primary') as Element, true)
    expect(sent.some((message) => message.kind === 'unlock')).toBe(true)
    render({
      answer: { state: 'generate', host: 'example.test' },
      role: 'newPassword',
      host: 'example.test',
      isCrossOrigin: false,
      topHost: '',
    })
    click(document.querySelector('button.primary') as Element, true)
    expect(sent.some((message) => message.kind === 'generate')).toBe(true)
  })

  it('ignores a render that did not come from the window around it', async () => {
    await load()
    window.dispatchEvent(
      new MessageEvent('message', {
        source: null,
        data: {
          channel: FRAME_MESSAGE_CHANNEL,
          kind: 'render',
          answer: { state: 'locked', host: 'x' },
          role: 'userName',
          host: 'x',
          isCrossOrigin: false,
          topHost: '',
        },
      }),
    )
    expect(document.querySelector('button')).toBeNull()
  })
})
