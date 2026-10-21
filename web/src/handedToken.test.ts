import { readFileSync } from 'node:fs'
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  HAND_TOKEN_FUNCTION,
  forgetHandedToken,
  offerHandedToken,
  peekHandedToken,
  whenTokenIsHanded,
} from './handedToken'

type Offering = Record<string, unknown> & Window

// A window of its own for each test, so that none leaves a function on the real one.
const newWindow = () => ({}) as Offering

afterEach(() => {
  forgetHandedToken()
})

describe('the token the extension hands the drawer', () => {
  it('is taken by the function the worker calls, and kept for the drawer to read', () => {
    const target = newWindow()
    offerHandedToken(target)
    expect(typeof target[HAND_TOKEN_FUNCTION]).toBe('function')
    expect((target[HAND_TOKEN_FUNCTION] as (token: string) => boolean)('example-token')).toBe(true)
    expect(peekHandedToken()).toBe('example-token')
    // The same render twice finds it twice; forgetting is the drawer's to do.
    expect(peekHandedToken()).toBe('example-token')
    forgetHandedToken()
    expect(peekHandedToken()).toBe('')
  })

  it('is taken once: afterwards the function is gone and nothing replaces the token', () => {
    const target = newWindow()
    offerHandedToken(target)
    const hand = target[HAND_TOKEN_FUNCTION] as (token: string) => boolean
    expect(hand('first-token')).toBe(true)
    expect(HAND_TOKEN_FUNCTION in target).toBe(false)
    // Whoever kept the function cannot put another token in its place.
    expect(hand('second-token')).toBe(false)
    expect(peekHandedToken()).toBe('first-token')
  })

  it('is refused when it is not a token', () => {
    const target = newWindow()
    offerHandedToken(target)
    const hand = target[HAND_TOKEN_FUNCTION] as (token: unknown) => boolean
    for (const token of ['', undefined, null, 5, {}]) expect(hand(token)).toBe(false)
    expect(HAND_TOKEN_FUNCTION in target).toBe(true)
    expect(peekHandedToken()).toBe('')
  })

  it('goes to the drawer that was waiting for it, and is not kept', () => {
    const target = newWindow()
    offerHandedToken(target)
    const received = vi.fn()
    const stop = whenTokenIsHanded(received)
    ;(target[HAND_TOKEN_FUNCTION] as (token: string) => boolean)('example-token')
    expect(received).toHaveBeenCalledWith('example-token')
    expect(peekHandedToken()).toBe('')
    stop()
  })

  it('is not told to a drawer that stopped waiting', () => {
    const target = newWindow()
    offerHandedToken(target)
    const received = vi.fn()
    whenTokenIsHanded(received)()
    ;(target[HAND_TOKEN_FUNCTION] as (token: string) => boolean)('example-token')
    expect(received).not.toHaveBeenCalled()
    expect(peekHandedToken()).toBe('example-token')
  })
})

describe('the extension and the drawer agree', () => {
  const read = (path: string) => readFileSync(new URL(path, import.meta.url), 'utf8')

  it('on the name of the function', () => {
    expect(read('../extension/src/reach.js')).toContain(`HAND_TOKEN_FUNCTION = '${HAND_TOKEN_FUNCTION}'`)
    expect(read('../extension/src/reach.js')).toContain(`window.${HAND_TOKEN_FUNCTION}`)
  })

  it('that the token is in no address: not the panel frame, not the drawer', () => {
    const panel = read('../extension/src/panel.js')
    expect(panel).not.toMatch(/token=/)
    expect(panel).not.toMatch(/answer\.token/)
    const app = read('./app.tsx')
    expect(app).not.toContain('location.hash')
    expect(app).not.toContain("get('token')")
  })
})
