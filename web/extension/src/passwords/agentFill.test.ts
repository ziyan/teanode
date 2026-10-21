// @vitest-environment-options {"url": "https://login.example.test/sign-in"}
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fillLoginForAgent, fillOneTimeCodeForAgent } from './agentFill'
import { giveLayout, setLayout } from './testLayout'

let tabUrl: string

function field(id: string): HTMLInputElement {
  return document.getElementById(id) as HTMLInputElement
}

beforeEach(() => {
  giveLayout()
  tabUrl = 'https://login.example.test/sign-in'
  document.body.innerHTML = `<form>
    <input id="user" type="text">
    <input id="pass" type="password">
    <input id="code" type="text">
    <input id="hidden" type="password" style="display:none">
    <input id="locked" type="password" disabled>
    <input id="email" type="email">
    <input id="phone" type="tel">
    <input id="number" type="number">
    <input id="checkbox" type="checkbox">
    <input id="file" type="file">
    <input id="submit" type="submit" value="Go">
    <input id="hint-password" type="text" autocomplete="current-password">
    <input id="hint-new-password" type="text" autocomplete="section-login new-password">
    <input id="hint-user" type="text" autocomplete="username">
    <input id="hint-email" type="search" autocomplete="email">
    <input id="hint-code" type="search" autocomplete="one-time-code">
    <input id="password-hint-user" type="password" autocomplete="username">
    <input id="password-hint-code" type="password" autocomplete="one-time-code">
    <textarea id="area"></textarea>
    <div id="editable" contenteditable="true"></div>
    <select id="choice"><option value="a">a</option></select>
  </form>`
  ;(window as any).__teanodeRefs = new Map([
    [1, field('user')],
    [2, field('pass')],
    [3, field('code')],
    [4, field('hidden')],
    [5, field('locked')],
    ...[
      'email',
      'phone',
      'number',
      'checkbox',
      'file',
      'submit',
      'hint-password',
      'hint-new-password',
      'hint-user',
      'hint-email',
      'hint-code',
      'password-hint-user',
      'password-hint-code',
      'area',
      'editable',
      'choice',
    ].map((id, index) => [100 + index, document.getElementById(id)] as [number, Element | null]),
  ])
  vi.stubGlobal('chrome', {
    tabs: { get: async () => ({ url: tabUrl }) },
    scripting: {
      // Runs the function the way the browser does: in the page, with the
      // arguments it was given, and answers what it returned.
      executeScript: async (injection: { func: (...args: unknown[]) => unknown; args: unknown[] }) => [
        { result: injection.func(...injection.args), documentId: 'document-1' },
      ],
    },
  })
})
afterEach(() => {
  delete (window as any).__teanodeRefs
  delete (window as any).__teanodeFilled
  vi.unstubAllGlobals()
})

describe('fillLoginForAgent', () => {
  it('types the user name and password into the referenced fields and answers without a value', async () => {
    const seen: string[] = []
    field('pass').addEventListener('input', () => seen.push('input'))
    field('pass').addEventListener('change', () => seen.push('change'))
    const answer = await fillLoginForAgent(7, {
      host: 'login.example.test',
      userNameRef: 1,
      passwordRef: '2',
      userName: 'ada',
      password: 'hunter2-secret',
    })
    expect(answer).toEqual({ ok: true, data: { isFilled: true, documentId: 'document-1' } })
    expect(field('user').value).toBe('ada')
    expect(field('pass').value).toBe('hunter2-secret')
    expect(seen).toEqual(['input', 'change'])
    expect(JSON.stringify(answer)).not.toContain('hunter2')
  })

  it('replaces what was in the field', async () => {
    field('user').value = 'someone else'
    await fillLoginForAgent(7, { host: 'login.example.test', userNameRef: 1, userName: 'ada', password: 'x' })
    expect(field('user').value).toBe('ada')
  })

  it('fills only the field it was given a reference for', async () => {
    const answer = await fillLoginForAgent(7, {
      host: 'login.example.test',
      passwordRef: 2,
      userName: 'ada',
      password: 'hunter2',
    })
    expect(answer.ok).toBe(true)
    expect(field('user').value).toBe('')
    expect(field('pass').value).toBe('hunter2')
  })

  it('treats www. as the same host', async () => {
    tabUrl = 'https://www.login.example.test/sign-in'
    // The page itself is at login.example.test in this test; the tab address is what the first check reads.
    const answer = await fillLoginForAgent(7, { host: 'WWW.login.example.test', passwordRef: 2, password: 'hunter2' })
    expect(answer.ok).toBe(true)
  })

  it('refuses a tab on another host, and says nothing of the value', async () => {
    tabUrl = 'https://evil.test/sign-in'
    const answer = await fillLoginForAgent(7, {
      host: 'login.example.test',
      passwordRef: 2,
      password: 'hunter2-secret',
    })
    expect(answer).toEqual({ ok: false, error: 'the tab is not on the site this login is for' })
    expect(field('pass').value).toBe('')
  })

  it('refuses a name that only ends alike', async () => {
    tabUrl = 'https://login.example.test.evil.test/'
    expect((await fillLoginForAgent(7, { host: 'login.example.test', passwordRef: 2, password: 'x' })).ok).toBe(false)
    tabUrl = 'https://evillogin.example.test/'
    expect((await fillLoginForAgent(7, { host: 'login.example.test', passwordRef: 2, password: 'x' })).ok).toBe(false)
  })

  it('refuses a plain-text page of the host', async () => {
    tabUrl = 'http://login.example.test/sign-in'
    const answer = await fillLoginForAgent(7, { host: 'login.example.test', passwordRef: 2, password: 'hunter2' })
    expect(answer.ok).toBe(false)
    expect(field('pass').value).toBe('')
  })

  it('refuses a page that has moved since the tab address was read', async () => {
    // The tab said it was on the host, but the page the script runs in is elsewhere.
    const answer = await fillLoginForAgent(7, { host: 'other.example.test', passwordRef: 2, password: 'hunter2' })
    expect(answer.ok).toBe(false)
    tabUrl = 'https://other.example.test/'
    const moved = await fillLoginForAgent(7, { host: 'other.example.test', passwordRef: 2, password: 'hunter2' })
    expect(moved).toEqual({ ok: false, error: 'the page is not on the site this login is for' })
    expect(field('pass').value).toBe('')
  })

  it('names what failed when a field is not there, hidden or disabled', async () => {
    const args = { host: 'login.example.test', password: 'hunter2' }
    expect(await fillLoginForAgent(7, { ...args, passwordRef: 99 })).toEqual({
      ok: false,
      error: 'the password field is not on the page; take a snapshot first',
    })
    expect(await fillLoginForAgent(7, { ...args, passwordRef: 4 })).toEqual({
      ok: false,
      error: 'the password field cannot be typed into',
    })
    expect(await fillLoginForAgent(7, { ...args, passwordRef: 5 })).toEqual({
      ok: false,
      error: 'the password field cannot be typed into',
    })
    setLayout(field('pass'), { width: 1, height: 1 })
    expect((await fillLoginForAgent(7, { ...args, passwordRef: 2 })).ok).toBe(false)
  })

  it('types into none of them when one of them cannot be typed into', async () => {
    const answer = await fillLoginForAgent(7, {
      host: 'login.example.test',
      userNameRef: 1,
      passwordRef: 4,
      userName: 'ada',
      password: 'hunter2',
    })
    expect(answer.ok).toBe(false)
    expect(field('user').value).toBe('')
  })

  it('refuses when nothing is named or a value is missing', async () => {
    expect((await fillLoginForAgent(7, { host: 'login.example.test', userName: 'ada', password: 'x' })).ok).toBe(false)
    expect((await fillLoginForAgent(7, { host: 'login.example.test', passwordRef: 2, password: '' })).ok).toBe(false)
    expect((await fillLoginForAgent(7, { passwordRef: 2, password: 'x' })).ok).toBe(false)
  })

  it('does not answer a snapshot that was lost with the page', async () => {
    delete (window as any).__teanodeRefs
    const answer = await fillLoginForAgent(7, { host: 'login.example.test', passwordRef: 2, password: 'x' })
    expect(answer.error).toContain('take a snapshot first')
  })
})

describe('fillOneTimeCodeForAgent', () => {
  it('types the code into the referenced field', async () => {
    const answer = await fillOneTimeCodeForAgent(7, { host: 'login.example.test', ref: 3, code: '123456' })
    expect(answer).toEqual({ ok: true, data: { isFilled: true, documentId: 'document-1' } })
    expect(field('code').value).toBe('123456')
    expect(JSON.stringify(answer)).not.toContain('123456')
  })

  it('applies the same host check', async () => {
    tabUrl = 'https://evil.test/'
    expect((await fillOneTimeCodeForAgent(7, { host: 'login.example.test', ref: 3, code: '123456' })).ok).toBe(false)
    expect(field('code').value).toBe('')
  })

  it('needs a field and a code', async () => {
    expect((await fillOneTimeCodeForAgent(7, { host: 'login.example.test', code: '123456' })).ok).toBe(false)
    expect((await fillOneTimeCodeForAgent(7, { host: 'login.example.test', ref: 3 })).ok).toBe(false)
  })
})

// Which snapshot reference each extra field has (see beforeEach).
const extraIds = [
  'email',
  'phone',
  'number',
  'checkbox',
  'file',
  'submit',
  'hint-password',
  'hint-new-password',
  'hint-user',
  'hint-email',
  'hint-code',
  'password-hint-user',
  'password-hint-code',
  'area',
  'editable',
  'choice',
]
const refOfExtra = (id: string): number => 100 + extraIds.indexOf(id)
const args = { host: 'login.example.test' }

describe('the kind of field each value goes into', () => {
  it('puts a password only into a password input or one that says it takes a password', async () => {
    for (const id of ['pass', 'hint-password', 'hint-new-password']) {
      const ref = id === 'pass' ? 2 : refOfExtra(id)
      const answer = await fillLoginForAgent(7, { ...args, passwordRef: ref, password: 'hunter2' })
      expect(answer.ok, id).toBe(true)
      expect((document.getElementById(id) as HTMLInputElement).value, id).toBe('hunter2')
    }
    for (const [id, ref] of [
      ['user', 1],
      ['email', refOfExtra('email')],
      ['number', refOfExtra('number')],
      ['hint-user', refOfExtra('hint-user')],
      ['hint-code', refOfExtra('hint-code')],
      ['checkbox', refOfExtra('checkbox')],
      ['file', refOfExtra('file')],
      ['submit', refOfExtra('submit')],
      ['area', refOfExtra('area')],
      ['editable', refOfExtra('editable')],
      ['choice', refOfExtra('choice')],
    ] as [string, number][]) {
      const answer = await fillLoginForAgent(7, { ...args, passwordRef: ref, password: 'hunter2' })
      expect(answer.ok, id).toBe(false)
      expect(JSON.stringify(answer), id).not.toContain('hunter2')
      expect((document.getElementById(id) as HTMLInputElement).value ?? '', id).not.toBe('hunter2')
    }
    expect(document.getElementById('area')!.textContent).toBe('')
    expect(document.getElementById('editable')!.textContent).toBe('')
  })

  it('puts a user name only into a text, email or telephone input or one that says it takes one', async () => {
    for (const [id, ref] of [
      ['user', 1],
      ['email', refOfExtra('email')],
      ['phone', refOfExtra('phone')],
      ['hint-user', refOfExtra('hint-user')],
      ['hint-email', refOfExtra('hint-email')],
    ] as [string, number][]) {
      const answer = await fillLoginForAgent(7, { ...args, userNameRef: ref, userName: 'ada' })
      expect(answer.ok, id).toBe(true)
      expect((document.getElementById(id) as HTMLInputElement).value, id).toBe('ada')
    }
    for (const [id, ref] of [
      ['pass', 2],
      ['number', refOfExtra('number')],
      ['password-hint-user', refOfExtra('password-hint-user')],
      ['hint-code', refOfExtra('hint-code')],
      ['checkbox', refOfExtra('checkbox')],
      ['file', refOfExtra('file')],
      ['submit', refOfExtra('submit')],
      ['area', refOfExtra('area')],
      ['editable', refOfExtra('editable')],
      ['choice', refOfExtra('choice')],
    ] as [string, number][]) {
      const answer = await fillLoginForAgent(7, { ...args, userNameRef: ref, userName: 'ada' })
      expect(answer.ok, id).toBe(false)
    }
    expect(field('pass').value).toBe('')
    expect(document.getElementById('area')!.textContent).toBe('')
    expect(document.getElementById('editable')!.textContent).toBe('')
  })

  it('puts a one-time code only into a text, number or telephone input or one that says it takes a code', async () => {
    for (const [id, ref] of [
      ['code', 3],
      ['number', refOfExtra('number')],
      ['phone', refOfExtra('phone')],
      ['hint-code', refOfExtra('hint-code')],
      ['password-hint-code', refOfExtra('password-hint-code')],
    ] as [string, number][]) {
      const answer = await fillOneTimeCodeForAgent(7, { ...args, ref, code: '123456' })
      expect(answer.ok, id).toBe(true)
      expect((document.getElementById(id) as HTMLInputElement).value, id).toBe('123456')
    }
    for (const [id, ref] of [
      ['pass', 2],
      ['email', refOfExtra('email')],
      ['checkbox', refOfExtra('checkbox')],
      ['file', refOfExtra('file')],
      ['submit', refOfExtra('submit')],
      ['area', refOfExtra('area')],
      ['editable', refOfExtra('editable')],
      ['choice', refOfExtra('choice')],
    ] as [string, number][]) {
      const answer = await fillOneTimeCodeForAgent(7, { ...args, ref, code: '123456' })
      expect(answer.ok, id).toBe(false)
      expect(JSON.stringify(answer), id).not.toContain('123456')
    }
    expect(field('pass').value).toBe('')
    expect(document.getElementById('area')!.textContent).toBe('')
  })

  it('types into none of the fields when one of them is the wrong kind', async () => {
    const answer = await fillLoginForAgent(7, {
      ...args,
      userNameRef: 1,
      passwordRef: refOfExtra('area'),
      userName: 'ada',
      password: 'hunter2',
    })
    expect(answer.ok).toBe(false)
    expect(field('user').value).toBe('')
  })
})

describe('what the agent filled is marked for the snapshot', () => {
  it('notes a password and a code as filled, and a user name not', async () => {
    await fillLoginForAgent(7, { ...args, userNameRef: 1, passwordRef: 2, userName: 'ada', password: 'hunter2' })
    await fillOneTimeCodeForAgent(7, { ...args, ref: 3, code: '123456' })
    const filled = (window as any).__teanodeFilled as WeakSet<Node>
    expect(filled.has(field('pass'))).toBe(true)
    expect(filled.has(field('code'))).toBe(true)
    expect(filled.has(field('user'))).toBe(false)
  })

  it('notes nothing for a field it refused', async () => {
    await fillLoginForAgent(7, { ...args, passwordRef: refOfExtra('area'), password: 'hunter2' })
    expect((window as any).__teanodeFilled).toBeUndefined()
  })
})

describe('the port', () => {
  it('fills a page on the default port when the server names it or names none', async () => {
    expect((await fillLoginForAgent(7, { ...args, port: 443, passwordRef: 2, password: 'x' })).ok).toBe(true)
    expect((await fillLoginForAgent(7, { ...args, passwordRef: 2, password: 'x' })).ok).toBe(true)
  })

  it('refuses a tab on another port than the server found the page on', async () => {
    tabUrl = 'https://login.example.test:8443/sign-in'
    expect((await fillLoginForAgent(7, { ...args, port: 443, passwordRef: 2, password: 'x' })).ok).toBe(false)
    // A call that names no port means the default port.
    expect((await fillLoginForAgent(7, { ...args, passwordRef: 2, password: 'x' })).ok).toBe(false)
    tabUrl = 'https://login.example.test/sign-in'
    expect((await fillLoginForAgent(7, { ...args, port: 8443, passwordRef: 2, password: 'x' })).ok).toBe(false)
    expect(field('pass').value).toBe('')
  })

  it('refuses when the page itself is on another port than the tab said', async () => {
    tabUrl = 'https://login.example.test:8443/sign-in'
    const answer = await fillLoginForAgent(7, { ...args, port: 8443, passwordRef: 2, password: 'x' })
    expect(answer).toEqual({ ok: false, error: 'the page is not on the site this login is for' })
    expect(field('pass').value).toBe('')
  })
})
