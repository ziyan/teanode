// @vitest-environment-options {"url": "https://login.example.test:8443/sign-in"}
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fillLoginForAgent } from './agentFill'
import { foreignFormActionHost } from './fieldDetection'
import { giveLayout } from './testLayout'

// The content script (foreignFormActionHost) and the agent's fill (fillInPage
// in agentFill.ts) each decide whether a form sends what is typed to another
// place than its page. fillInPage runs inside the page with nothing of the
// other file in scope, so the two are written twice; this runs one list of
// cases against both so that they cannot drift apart again.

const CASES: { action: string; isElsewhere: boolean; why: string }[] = [
  { action: '/session', isElsewhere: false, why: 'a relative address' },
  { action: 'https://login.example.test:8443/session', isElsewhere: false, why: "the page's own origin" },
  { action: 'https://login.example.test:8444/session', isElsewhere: true, why: 'another port' },
  { action: 'https://login.example.test/session', isElsewhere: true, why: 'the default port of the same host' },
  { action: 'https://www.login.example.test:8443/session', isElsewhere: true, why: 'a www. in front' },
  { action: 'https://example.test:8443/session', isElsewhere: true, why: 'the parent domain' },
  { action: 'https://collect.example.test:8443/session', isElsewhere: true, why: 'a sibling subdomain' },
  { action: 'http://login.example.test:8443/session', isElsewhere: true, why: 'the plain-text scheme' },
  { action: '//collect.example.test/session', isElsewhere: true, why: 'a scheme-relative address' },
  { action: 'javascript:void(0)', isElsewhere: false, why: 'a script address, which sends nowhere' },
  { action: '', isElsewhere: false, why: 'no address' },
]

beforeEach(() => {
  giveLayout()
  vi.stubGlobal('chrome', {
    tabs: { get: async () => ({ url: 'https://login.example.test:8443/sign-in' }) },
    scripting: {
      executeScript: async (injection: { func: (...args: unknown[]) => unknown; args: unknown[] }) => [
        { result: injection.func(...injection.args), documentId: 'document-1' },
      ],
    },
  })
})
afterEach(() => {
  document.body.innerHTML = ''
  delete (window as any).__teanodeRefs
  delete (window as any).__teanodeFilled
  vi.unstubAllGlobals()
})

describe('a form that sends elsewhere', () => {
  for (const { action, isElsewhere, why } of CASES) {
    it(`is decided the same by the content script and the agent's fill for ${why}`, async () => {
      document.body.innerHTML = `<form><input id="pass" type="password"></form>`
      const form = document.querySelector('form') as HTMLFormElement
      if (action !== '') form.setAttribute('action', action)
      const input = document.getElementById('pass') as HTMLInputElement
      ;(window as any).__teanodeRefs = new Map([[2, input]])
      ;(document as any).elementFromPoint = () => input
      const answer = await fillLoginForAgent(7, {
        host: 'login.example.test',
        port: 8443,
        password: 'hunter2',
        passwordRef: 2,
      })
      delete (document as any).elementFromPoint
      expect(foreignFormActionHost(input) !== '', 'content script').toBe(isElsewhere)
      // The agent's fill refuses exactly these: the page is a secure origin
      // with a visible password field, so nothing else stands in the way.
      expect(answer.ok === false, "agent's fill").toBe(isElsewhere)
    })
  }
})
