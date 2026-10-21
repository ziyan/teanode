// The questions a passkey ceremony asks the person, each in a window of the
// extension's own (a popup window, so a page can neither draw over it nor
// click it). "Save a passkey for example.com as ada?" for a new passkey,
// "Sign in to example.com with your passkey?" for a sign-in; both with "Use
// the browser instead", which hands the request back to the browser. The
// registry here keeps what each open window is waiting for; background.ts
// decides what an answer does.

import type { PasskeyAssertionRequest, PasskeyCreationRequest, PageContext } from './webauthn'
import type { PasskeyReply } from './passkeyOverride'

export const PROMPT_LIFETIME_MILLISECONDS = 5 * 60 * 1000

export interface PendingPrompt {
  id: string
  kind: 'create' | 'get'
  tabId: number
  relyingPartyId: string
  userName: string
  windowId: number | null
  context: PageContext
  creation: { request: PasskeyCreationRequest } | null
  assertion: { request: PasskeyAssertionRequest } | null
  // isDeciding: an answer is being worked out, so a second click is turned away.
  isDeciding: boolean
  resolve: (reply: PasskeyReply) => void
  timer: ReturnType<typeof setTimeout> | null
}

export class PasskeyPrompts {
  private readonly byId = new Map<string, PendingPrompt>()

  add(prompt: PendingPrompt) {
    this.byId.set(prompt.id, prompt)
  }

  get(id: string): PendingPrompt | null {
    return this.byId.get(id) ?? null
  }

  // isPendingForTab: a ceremony in this tab is already waiting for an answer.
  isPendingForTab(tabId: number): boolean {
    return [...this.byId.values()].some((prompt) => prompt.tabId === tabId)
  }

  findByWindow(windowId: number): PendingPrompt | null {
    return [...this.byId.values()].find((prompt) => prompt.windowId === windowId) ?? null
  }

  findByTab(tabId: number): PendingPrompt | null {
    return [...this.byId.values()].find((prompt) => prompt.tabId === tabId) ?? null
  }

  // settle ends the prompt with the reply the page gets. It returns the
  // prompt so the caller can close its window; a prompt already settled
  // returns null and does nothing.
  settle(id: string, reply: PasskeyReply): PendingPrompt | null {
    const prompt = this.byId.get(id)
    if (!prompt) return null
    this.byId.delete(id)
    if (prompt.timer !== null) clearTimeout(prompt.timer)
    prompt.resolve(reply)
    return prompt
  }
}

export const NOT_ALLOWED_REPLY: PasskeyReply = {
  outcome: 'error',
  errorName: 'NotAllowedError',
  message: 'The operation either timed out or was not allowed.',
}
