// @vitest-environment-options {"url": "https://example.test/sign-in"}
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  classifyField,
  findLoginScope,
  foreignFormActionHost,
  isFieldVisible,
  readSubmittedLogin,
  roleOfFocusedField,
  setFieldValue,
} from './fieldDetection'
import { giveLayout, setLayout } from './testLayout'

function page(html: string): void {
  document.body.innerHTML = html
}

function field(selector: string): HTMLInputElement {
  const found = document.querySelector(selector)
  if (!(found instanceof HTMLInputElement)) throw new Error(`no input for ${selector}`)
  return found
}

beforeEach(() => {
  giveLayout()
})
afterEach(() => {
  document.body.innerHTML = ''
})

describe('classifyField', () => {
  it('follows the autocomplete hint the page gives', () => {
    page(`<form>
      <input id="a" type="text" autocomplete="username">
      <input id="b" type="email" autocomplete="email">
      <input id="c" type="password" autocomplete="current-password">
      <input id="d" type="password" autocomplete="new-password">
      <input id="e" type="text" autocomplete="one-time-code">
    </form>`)
    expect(classifyField(field('#a'))).toBe('userName')
    expect(classifyField(field('#b'))).toBe('userName')
    expect(classifyField(field('#c'))).toBe('currentPassword')
    expect(classifyField(field('#d'))).toBe('newPassword')
    expect(classifyField(field('#e'))).toBe('oneTimeCode')
  })

  it('treats a password field with no hint as a current password', () => {
    page('<input id="a" type="password">')
    expect(classifyField(field('#a'))).toBe('currentPassword')
  })

  it('guesses a user name from the field name only beside a password field', () => {
    page(
      '<form><input id="a" type="text" name="login_email"><input type="password"></form><input id="b" type="text" name="login_email">',
    )
    expect(classifyField(field('#a'), true)).toBe('userName')
    expect(classifyField(field('#a'), false)).toBeNull()
    expect(classifyField(field('#b'), false)).toBeNull()
  })

  it('leaves out search boxes, card fields, disabled and read-only fields', () => {
    page(`<input id="a" type="text" name="search_user">
      <input id="b" type="text" autocomplete="cc-number">
      <input id="c" type="text" autocomplete="username" disabled>
      <input id="d" type="text" autocomplete="username" readonly>
      <input id="e" type="checkbox" autocomplete="username">`)
    for (const id of ['#a', '#b', '#c', '#d', '#e']) expect(classifyField(field(id), true)).toBeNull()
  })

  it('reads a short field called a code as a one-time code', () => {
    page('<input id="a" type="text" name="otp" maxlength="6"><input id="b" type="text" name="otp-note">')
    expect(classifyField(field('#a'))).toBe('oneTimeCode')
  })
})

describe('isFieldVisible', () => {
  it('is true for a field with a size on the page', () => {
    page('<input id="a" type="text">')
    expect(isFieldVisible(field('#a'))).toBe(true)
  })

  it('is false for everything a page does to hide a field', () => {
    page(`
      <input id="displayNone" style="display:none">
      <input id="visibilityHidden" style="visibility:hidden">
      <input id="invisible" style="opacity:0">
      <div style="opacity:0"><input id="fadedParent"></div>
      <div style="display:none"><input id="hiddenParent"></div>
      <input id="hiddenType" type="hidden">
      <input id="hiddenAttribute" hidden>
      <input id="tiny">
      <input id="offLeft">
      <input id="offTop">
    `)
    setLayout(field('#tiny'), { width: 1, height: 1 })
    setLayout(field('#offLeft'), { left: -9999, width: 200 })
    setLayout(field('#offTop'), { top: -9999, height: 30 })
    for (const id of [
      'displayNone',
      'visibilityHidden',
      'invisible',
      'fadedParent',
      'hiddenParent',
      'hiddenType',
      'hiddenAttribute',
      'tiny',
      'offLeft',
      'offTop',
    ]) {
      expect(isFieldVisible(field(`#${id}`)), id).toBe(false)
    }
  })

  it('is false for a field that is not on the page', () => {
    const detached = document.createElement('input')
    expect(isFieldVisible(detached)).toBe(false)
  })

  it("wants at least half of the opacity, the field's and every element around it together", () => {
    page(`
      <input id="mostly" style="opacity:0.6">
      <input id="half" style="opacity:0.5">
      <input id="faint" style="opacity:0.4">
      <div style="opacity:0.7"><input id="stacked" style="opacity:0.8"></div>
      <div style="opacity:0.7"><div style="opacity:0.6"><input id="fadedTwice"></div></div>
    `)
    expect(isFieldVisible(field('#mostly'))).toBe(true)
    expect(isFieldVisible(field('#half'))).toBe(true)
    expect(isFieldVisible(field('#stacked'))).toBe(true)
    expect(isFieldVisible(field('#faint'))).toBe(false)
    // 0.7 times 0.6 is 0.42.
    expect(isFieldVisible(field('#fadedTwice'))).toBe(false)
  })

  // jsdom keeps few properties of a style; the ones it does not are given
  // to getComputedStyle by hand.
  function withComputedStyle(extra: (element: Element) => Record<string, string>) {
    const original = window.getComputedStyle.bind(window)
    vi.spyOn(window, 'getComputedStyle').mockImplementation(((element: Element) => {
      const style = original(element)
      return new Proxy(style, {
        get(target, name) {
          const given = extra(element)
          return typeof name === 'string' && name in given ? given[name] : (target as any)[name]
        },
      })
    }) as typeof window.getComputedStyle)
  }

  it("is false for a field cut off by a clip path or a clip, its own or an element's around it", () => {
    page(`
      <input id="clipPath">
      <div id="clippedParent"><input id="inClippedParent"></div>
      <input id="srOnly">
      <input id="clearPath">
    `)
    withComputedStyle((element): Record<string, string> => {
      if (element.id === 'clipPath' || element.id === 'clippedParent') return { clipPath: 'inset(50%)' }
      if (element.id === 'srOnly') return { position: 'absolute', clip: 'rect(0px, 0px, 0px, 0px)' }
      if (element.id === 'clearPath') return { clipPath: 'none', clip: 'auto' }
      return {}
    })
    expect(isFieldVisible(field('#clipPath'))).toBe(false)
    expect(isFieldVisible(field('#inClippedParent'))).toBe(false)
    expect(isFieldVisible(field('#srOnly'))).toBe(false)
    expect(isFieldVisible(field('#clearPath'))).toBe(true)
  })

  it('is false for a field an overflow around it cuts off, and true when it shows enough', () => {
    page(`
      <div id="box" style="overflow:hidden"><input id="inBox"></div>
      <div id="scroller" style="overflow:auto"><input id="inScroller"></div>
      <div id="open" style="overflow:visible"><input id="inOpen"></div>
    `)
    // The field is at (10, 10) and 200 by 30. The box is far to the right of it.
    setLayout(document.getElementById('box') as Element, { left: 600, width: 100, height: 100 })
    // The scroller shows the field.
    setLayout(document.getElementById('scroller') as Element, { left: 0, top: 0, width: 400, height: 300 })
    setLayout(document.getElementById('open') as Element, { left: 600 })
    expect(isFieldVisible(field('#inBox'))).toBe(false)
    expect(isFieldVisible(field('#inScroller'))).toBe(true)
    expect(isFieldVisible(field('#inOpen'))).toBe(true)
  })

  it('is false when another element is drawn over the middle of the field, and true when it is the field or inside it', () => {
    page('<input id="a"><input id="b"><div id="cover"></div><input id="offscreen">')
    setLayout(field('#offscreen'), { left: 50_000 })
    const lookup = vi.fn((horizontal: number, vertical: number): Element | null => {
      expect([horizontal, vertical]).toEqual([110, 25])
      return field('#a')
    })
    ;(document as any).elementFromPoint = lookup
    expect(isFieldVisible(field('#a'))).toBe(true)
    lookup.mockReturnValue(document.getElementById('cover'))
    expect(isFieldVisible(field('#b'))).toBe(false)
    // A middle that is off the screen cannot be looked at: the field is not counted as covered.
    lookup.mockClear()
    expect(isFieldVisible(field('#offscreen'))).toBe(true)
    expect(lookup).not.toHaveBeenCalled()
    delete (document as any).elementFromPoint
  })

  it("looks inside a shadow root with that root's own lookup", () => {
    const host = document.createElement('div')
    document.body.append(host)
    const root = host.attachShadow({ mode: 'open' })
    const inner = document.createElement('input')
    root.append(inner)
    ;(root as any).elementFromPoint = vi.fn(() => document.createElement('div'))
    ;(document as any).elementFromPoint = vi.fn(() => inner)
    expect(isFieldVisible(inner)).toBe(false)
    delete (document as any).elementFromPoint
  })
})

describe('foreignFormActionHost', () => {
  it("is empty for a form that sends to the page's own host, a relative address, none, or a field with no form", () => {
    page(`
      <form id="relative" action="/session"><input id="a"></form>
      <form id="same" action="https://www.example.test/session"><input id="b"></form>
      <form id="none"><input id="c"></form>
      <form id="script" action="javascript:void(0)"><input id="d"></form>
      <input id="loose">
    `)
    for (const id of ['a', 'b', 'c', 'd', 'loose']) expect(foreignFormActionHost(field(`#${id}`)), id).toBe('')
  })

  it('names the host a form sends to when it is another, whatever the scheme or port', () => {
    page(`
      <form action="https://collect.example.test/session"><input id="a"></form>
      <form action="//collect.example.test:8443/x"><input id="b"></form>
      <form action="/ok"><input id="c"><button type="submit" formaction="https://collect.example.test/other">Go</button></form>
    `)
    expect(foreignFormActionHost(field('#a'))).toBe('collect.example.test')
    expect(foreignFormActionHost(field('#b'))).toBe('collect.example.test')
    expect(foreignFormActionHost(field('#c'))).toBe('collect.example.test')
  })

  it('finds the address of a button outside the form that names it with its form attribute', () => {
    page(`
      <form id="login" action="/ok"><input id="a"></form>
      <button type="submit" form="login" formaction="https://collect.example.test/other">Go</button>
      <form id="second" action="/ok"><input id="b"></form>
      <form id="third" action="/ok"><input id="c"><button type="submit" form="second" formaction="https://collect.example.test/x">Go</button></form>
    `)
    expect(foreignFormActionHost(field('#a'))).toBe('collect.example.test')
    // A button of another form is not this form's, even inside it.
    expect(foreignFormActionHost(field('#c'))).toBe('')
  })

  it('counts a secure page sending to a plain-text address as sending elsewhere, even to its own host', () => {
    page(`
      <form action="http://example.test/session"><input id="a"></form>
      <form action="/ok"><input id="b"><button type="submit" formaction="http://example.test/x">Go</button></form>
      <form action="https://example.test/session"><input id="c"></form>
    `)
    expect(foreignFormActionHost(field('#a'))).toBe('example.test (not encrypted)')
    expect(foreignFormActionHost(field('#b'))).toBe('example.test (not encrypted)')
    expect(foreignFormActionHost(field('#c'))).toBe('')
  })

  it("reads the attribute, not a field called action that stands in for the form's property", () => {
    page('<form action="https://collect.example.test/session"><input name="action" value="/ok"><input id="a"></form>')
    expect(foreignFormActionHost(field('#a'))).toBe('collect.example.test')
  })
})

describe('findLoginScope', () => {
  it('finds the user name before the password in a form', () => {
    page(`<form>
      <input id="search" type="text" name="search">
      <input id="user" type="text" name="username">
      <input id="pass" type="password">
    </form>`)
    const scope = findLoginScope(field('#pass'))
    expect(scope.userNameField?.id).toBe('user')
    expect(scope.currentPasswordField?.id).toBe('pass')
    expect(scope.newPasswordFields).toEqual([])
  })

  it('works on a page with no form', () => {
    page('<div><input id="user" type="text" autocomplete="username"><input id="pass" type="password"></div>')
    const scope = findLoginScope(field('#user'))
    expect(scope.userNameField?.id).toBe('user')
    expect(scope.currentPasswordField?.id).toBe('pass')
  })

  it('does not see a hidden field', () => {
    page(`<form>
      <input id="trap" type="text" autocomplete="username" style="opacity:0">
      <input id="user" type="text" autocomplete="username">
      <input id="pass" type="password">
      <input id="hiddenPass" type="password" style="display:none">
    </form>`)
    const scope = findLoginScope(field('#pass'))
    expect(scope.userNameField?.id).toBe('user')
    expect(scope.currentPasswordField?.id).toBe('pass')
  })

  it('keeps one form apart from another on the same page', () => {
    page(`<form id="one"><input id="userOne" autocomplete="username"><input id="passOne" type="password"></form>
      <form id="two"><input id="userTwo" autocomplete="username"><input id="passTwo" type="password"></form>`)
    expect(findLoginScope(field('#passTwo')).userNameField?.id).toBe('userTwo')
    expect(findLoginScope(field('#passOne')).userNameField?.id).toBe('userOne')
  })

  it('finds the new password fields and the one-time code field', () => {
    page(`<form>
      <input id="old" type="password" autocomplete="current-password">
      <input id="new" type="password" autocomplete="new-password">
      <input id="again" type="password" autocomplete="new-password">
      <input id="code" type="text" autocomplete="one-time-code">
    </form>`)
    const scope = findLoginScope(field('#old'))
    expect(scope.currentPasswordField?.id).toBe('old')
    expect(scope.newPasswordFields.map((input) => input.id)).toEqual(['new', 'again'])
    expect(scope.oneTimeCodeField?.id).toBe('code')
  })
})

describe('roleOfFocusedField', () => {
  it('is null for a field that cannot be seen', () => {
    page('<input id="a" type="password" style="display:none"><input id="b" type="password">')
    expect(roleOfFocusedField(field('#a'))).toBeNull()
    expect(roleOfFocusedField(field('#b'))).toBe('currentPassword')
  })

  it('counts only a visible password field as a neighbour', () => {
    page('<form><input id="a" type="text" name="email"><input type="password" style="display:none"></form>')
    expect(roleOfFocusedField(field('#a'))).toBeNull()
  })
})

describe('readSubmittedLogin', () => {
  it('reads the user name and password of a sign-in', () => {
    page(
      `<form id="f"><input id="user" autocomplete="username" value=" ada@example.test "><input id="pass" type="password" value="hunter2"></form>`,
    )
    expect(readSubmittedLogin(document.getElementById('f') as HTMLFormElement)).toEqual({
      userName: 'ada@example.test',
      password: 'hunter2',
      isNewPassword: false,
    })
  })

  it('reads the new password a form marks as new', () => {
    page(
      `<form id="f"><input autocomplete="username" value="ada"><input type="password" autocomplete="current-password" value="old-one"><input type="password" autocomplete="new-password" value="new-one"></form>`,
    )
    expect(readSubmittedLogin(document.getElementById('f') as HTMLFormElement)).toEqual({
      userName: 'ada',
      password: 'new-one',
      isNewPassword: true,
    })
  })

  it('reads a password typed twice as a new one', () => {
    page(
      `<form id="f"><input autocomplete="username" value="ada"><input type="password" value="fresh-one"><input type="password" value="fresh-one"></form>`,
    )
    expect(readSubmittedLogin(document.getElementById('f') as HTMLFormElement)).toEqual({
      userName: 'ada',
      password: 'fresh-one',
      isNewPassword: true,
    })
  })

  it('is null for a form with an empty password and a hidden one is not read', () => {
    page(
      `<form id="f"><input autocomplete="username" value="ada"><input type="password" value=""><input type="password" style="display:none" value="secret"></form>`,
    )
    expect(readSubmittedLogin(document.getElementById('f') as HTMLFormElement)).toBeNull()
  })

  it('returns the user name alone from the first step of a sign-in', () => {
    page(`<form id="f"><input autocomplete="username" value="ada"></form>`)
    expect(readSubmittedLogin(document.getElementById('f') as HTMLFormElement)).toEqual({
      userName: 'ada',
      password: '',
      isNewPassword: false,
    })
  })

  it('reads a page with no form', () => {
    page(
      `<div><input autocomplete="username" value="ada"><input type="password" value="hunter2"><button>Go</button></div>`,
    )
    expect(readSubmittedLogin(document.body)?.password).toBe('hunter2')
  })
})

describe('setFieldValue', () => {
  it('sets the value and tells the page with input and change events', () => {
    page('<input id="a" type="text">')
    const input = field('#a')
    const seen: string[] = []
    input.addEventListener('input', () => seen.push('input:' + input.value))
    input.addEventListener('change', () => seen.push('change:' + input.value))
    setFieldValue(input, 'hunter2')
    expect(input.value).toBe('hunter2')
    expect(seen).toEqual(['input:hunter2', 'change:hunter2'])
  })

  it('gets past a page that replaced the value property on the element', () => {
    page('<input id="a" type="text">')
    const input = field('#a')
    // A framework that watches the value puts its own property on the element
    // and ignores a plain write.
    Object.defineProperty(input, 'value', { get: () => '', set: () => {}, configurable: true })
    setFieldValue(input, 'hunter2')
    const nativeGetter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.get
    expect(nativeGetter?.call(input)).toBe('hunter2')
  })
})
