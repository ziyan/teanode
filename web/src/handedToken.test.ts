import { readFileSync } from 'node:fs'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { forgetHandedToken, listenForHandedToken, peekHandedToken, whenTokenIsHanded } from './handedToken'

const extensionOrigin = 'chrome-extension://abcdefghijklmnopabcdefghijklmnop'

type Listener = (event: MessageEvent) => void

// A window of its own for each test, framed by a parent of its own, so that
// none of them leaves a listener on the real one.
function newFrame(ancestorOrigins: string[] | null = [extensionOrigin]) {
  const parent = {}
  const listeners: Listener[] = []
  const frame = {
    parent,
    top: parent,
    location: { ancestorOrigins: ancestorOrigins ?? undefined },
    addEventListener: (_type: string, listener: Listener) => listeners.push(listener),
  }
  listenForHandedToken(frame as never)
  const send = (data: unknown, overrides: { source?: unknown; origin?: string } = {}) => {
    const event = { data, source: parent, origin: extensionOrigin, ...overrides } as unknown as MessageEvent
    for (const listener of listeners) listener(event)
  }
  return { frame, parent, send }
}

afterEach(() => {
  forgetHandedToken()
})

describe('the token the extension hands the drawer', () => {
  it('is taken from the extension page that frames the drawer, and kept for the drawer to read', () => {
    const { send } = newFrame()
    send({ teanode: 'token', token: 'example-token' })
    expect(peekHandedToken()).toBe('example-token')
    // The same render twice finds it twice; forgetting is the drawer's to do.
    expect(peekHandedToken()).toBe('example-token')
    forgetHandedToken()
    expect(peekHandedToken()).toBe('')
  })

  it('is taken once: a second message does not replace the token', () => {
    const { send } = newFrame()
    send({ teanode: 'token', token: 'first-token' })
    send({ teanode: 'token', token: 'second-token' })
    expect(peekHandedToken()).toBe('first-token')
    // Not even after the drawer forgot it: the frame signs in once.
    forgetHandedToken()
    send({ teanode: 'token', token: 'third-token' })
    expect(peekHandedToken()).toBe('')
  })

  it('is refused from a page that is not an extension page, whatever it says', () => {
    for (const origin of ['https://shop.example.test', 'null', 'http://chrome-extension.example.test', '']) {
      const { send } = newFrame([origin])
      send({ teanode: 'token', token: 'example-token' }, { origin })
      expect(peekHandedToken(), origin).toBe('')
    }
  })

  it('is refused when the frame has no single ancestor, or the browser does not say', () => {
    for (const ancestors of [
      null,
      [],
      [extensionOrigin, 'https://shop.example.test'],
      ['https://shop.example.test', extensionOrigin],
    ]) {
      const { send } = newFrame(ancestors)
      send({ teanode: 'token', token: 'example-token' })
      expect(peekHandedToken()).toBe('')
    }
  })

  it('is refused when the page is not framed', () => {
    const { frame, send } = newFrame()
    frame.top = frame as never
    send({ teanode: 'token', token: 'example-token' })
    expect(peekHandedToken()).toBe('')
  })

  it('is refused from a window that is not the parent, or from another origin than the parent', () => {
    const { send } = newFrame()
    send({ teanode: 'token', token: 'example-token' }, { source: {} })
    send({ teanode: 'token', token: 'example-token' }, { source: null })
    send(
      { teanode: 'token', token: 'example-token' },
      { origin: 'chrome-extension://zyxwvutsrqponmlkzyxwvutsrqponmlk' },
    )
    send({ teanode: 'token', token: 'example-token' }, { origin: 'https://shop.example.test' })
    expect(peekHandedToken()).toBe('')
  })

  it('is refused when it is not a token, and a refused message does not use up the one allowed', () => {
    const { send } = newFrame()
    for (const data of [
      null,
      undefined,
      'token',
      5,
      {},
      { teanode: 'theme', token: 'example-token' },
      { teanode: 'token' },
    ]) {
      send(data)
    }
    for (const token of ['', 5, null, {}, ['example-token']]) send({ teanode: 'token', token })
    expect(peekHandedToken()).toBe('')
    send({ teanode: 'token', token: 'example-token' })
    expect(peekHandedToken()).toBe('example-token')
  })

  it('goes to the drawer that was waiting for it, and is not kept', () => {
    const { send } = newFrame()
    const received = vi.fn()
    const stop = whenTokenIsHanded(received)
    send({ teanode: 'token', token: 'example-token' })
    expect(received).toHaveBeenCalledWith('example-token')
    expect(peekHandedToken()).toBe('')
    stop()
  })

  it('is not told to a drawer that stopped waiting', () => {
    const { send } = newFrame()
    const received = vi.fn()
    whenTokenIsHanded(received)()
    send({ teanode: 'token', token: 'example-token' })
    expect(received).not.toHaveBeenCalled()
    expect(peekHandedToken()).toBe('example-token')
  })
})

describe('the token is in no address', () => {
  const read = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')

  it('not the side panel frame, not the drawer', () => {
    const sidePanel = read('../extension/src/sidepanel.js')
    expect(sidePanel).not.toMatch(/token=/)
    expect(sidePanel).not.toMatch(/\?.*token/)
    const app = read('./app.tsx')
    expect(app).not.toContain('location.hash')
    expect(app).not.toContain("get('token')")
  })
})
