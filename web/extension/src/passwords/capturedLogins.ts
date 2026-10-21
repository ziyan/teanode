// The login a person just typed into a form, kept in the service worker's
// memory (a Map, nowhere else) for two minutes so the page they land on can
// offer to save it. It holds a password, so it is never written to storage;
// if the worker stops in between, the offer is lost, which is the safe way
// to lose it.

import { normalizeHost } from './hosts'

export const CAPTURE_LIFETIME_MILLISECONDS = 2 * 60 * 1000

export interface CapturedLogin {
  pageUrl: string
  userName: string
  // password is empty after the first step of a user name first sign-in.
  password: string
  isNewPassword: boolean
  capturedAtMilliseconds: number
  // hasBeenOffered: the bar was shown once; it is not shown again on the
  // next page.
  hasBeenOffered: boolean
}

export class CapturedLogins {
  private readonly byTab = new Map<number, CapturedLogin>()

  constructor(
    private readonly now: () => number = Date.now,
    private readonly lifetimeMilliseconds: number = CAPTURE_LIFETIME_MILLISECONDS,
  ) {}

  // capture keeps what was typed. A form that held only a password (the
  // second step of a user name first sign-in) takes the user name from the
  // first step if it was on the same host a moment ago.
  capture(tabId: number, input: { pageUrl: string; userName: string; password: string; isNewPassword: boolean }) {
    const earlier = this.peek(tabId)
    let userName = input.userName
    if (
      userName === '' &&
      earlier &&
      earlier.userName !== '' &&
      normalizeHost(earlier.pageUrl) === normalizeHost(input.pageUrl)
    ) {
      userName = earlier.userName
    }
    // A user name alone never replaces a password already captured.
    if (input.password === '' && earlier && earlier.password !== '') return
    this.byTab.set(tabId, {
      pageUrl: input.pageUrl,
      userName,
      password: input.password,
      isNewPassword: input.isNewPassword,
      capturedAtMilliseconds: this.now(),
      hasBeenOffered: false,
    })
  }

  // peek is the capture for a tab, or null if there is none or it expired.
  peek(tabId: number): CapturedLogin | null {
    const kept = this.byTab.get(tabId)
    if (!kept) return null
    if (this.now() - kept.capturedAtMilliseconds >= this.lifetimeMilliseconds) {
      this.byTab.delete(tabId)
      return null
    }
    return kept
  }

  markOffered(tabId: number) {
    const kept = this.peek(tabId)
    if (kept) kept.hasBeenOffered = true
  }

  forget(tabId: number) {
    this.byTab.delete(tabId)
  }

  forgetAll() {
    this.byTab.clear()
  }
}
