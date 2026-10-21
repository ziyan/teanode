import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import {
  classifyField,
  findLoginScope,
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
