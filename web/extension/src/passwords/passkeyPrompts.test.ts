import { describe, expect, it, vi } from 'vitest'
import { NOT_ALLOWED_REPLY, PasskeyPrompts, PendingPrompt } from './passkeyPrompts'

function prompt(id: string, tabId: number, windowId: number | null = null): PendingPrompt {
  return {
    id,
    kind: 'get',
    tabId,
    relyingPartyId: 'example.com',
    userName: '',
    windowId,
    context: { pageUrl: 'https://example.com/', origin: 'https://example.com', isCrossOrigin: false },
    creation: null,
    assertion: null,
    isDeciding: false,
    resolve: vi.fn(),
    timer: null,
  }
}

describe('PasskeyPrompts', () => {
  it('knows which tab is waiting and finds a prompt by its window', () => {
    const prompts = new PasskeyPrompts()
    prompts.add(prompt('a', 1, 10))
    expect(prompts.isPendingForTab(1)).toBe(true)
    expect(prompts.isPendingForTab(2)).toBe(false)
    expect(prompts.findByWindow(10)?.id).toBe('a')
    expect(prompts.findByTab(1)?.id).toBe('a')
    expect(prompts.findByWindow(11)).toBeNull()
  })

  it('answers a prompt once', () => {
    const prompts = new PasskeyPrompts()
    const waiting = prompt('a', 1)
    prompts.add(waiting)
    expect(prompts.settle('a', NOT_ALLOWED_REPLY)?.id).toBe('a')
    expect(waiting.resolve).toHaveBeenCalledWith(NOT_ALLOWED_REPLY)
    expect(prompts.settle('a', { outcome: 'fallback' })).toBeNull()
    expect(waiting.resolve).toHaveBeenCalledTimes(1)
    expect(prompts.isPendingForTab(1)).toBe(false)
  })

  it("stops the prompt's timer when it is answered", () => {
    vi.useFakeTimers()
    const prompts = new PasskeyPrompts()
    const waiting = prompt('a', 1)
    const timeout = vi.fn()
    waiting.timer = setTimeout(timeout, 1000) as unknown as PendingPrompt['timer']
    prompts.add(waiting)
    prompts.settle('a', { outcome: 'fallback' })
    vi.advanceTimersByTime(2000)
    expect(timeout).not.toHaveBeenCalled()
    vi.useRealTimers()
  })
})
