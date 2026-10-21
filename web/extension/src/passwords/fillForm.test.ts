import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { fillSignInForm, findFormToFill, foreignFillActionHost } from './fillForm'
import { giveLayout } from './testLayout'

function page(html: string): void {
  document.body.innerHTML = html
}

function field(selector: string): HTMLInputElement {
  return document.querySelector(selector) as HTMLInputElement
}

beforeEach(() => {
  giveLayout()
})
afterEach(() => {
  document.body.innerHTML = ''
})

describe('fillSignInForm', () => {
  it('types the user name and the password into the visible fields', () => {
    page(`<form><input id="user" autocomplete="username"><input id="pass" type="password"></form>`)
    const filled = fillSignInForm(field('#pass'), { userName: 'ada', password: 'hunter2' })
    expect(filled).toBe(2)
    expect(field('#user').value).toBe('ada')
    expect(field('#pass').value).toBe('hunter2')
  })

  it('never types into a field the page hid', () => {
    page(`<form>
      <input id="trap" autocomplete="username" style="opacity:0">
      <input id="hiddenPass" type="password" style="display:none">
      <input id="user" autocomplete="username">
      <input id="pass" type="password">
    </form>`)
    fillSignInForm(field('#pass'), { userName: 'ada', password: 'hunter2' })
    expect(field('#trap').value).toBe('')
    expect(field('#hiddenPass').value).toBe('')
    expect(field('#user').value).toBe('ada')
  })

  it('does not type a saved password into a new password field', () => {
    page(
      `<form><input id="user" autocomplete="username"><input id="new" type="password" autocomplete="new-password"></form>`,
    )
    expect(fillSignInForm(field('#user'), { userName: 'ada', password: 'hunter2' })).toBe(1)
    expect(field('#new').value).toBe('')
  })

  it('fills only the user name on the first step of a sign-in', () => {
    page(`<form><input id="user" autocomplete="username"></form>`)
    expect(fillSignInForm(field('#user'), { userName: 'ada', password: 'hunter2' })).toBe(1)
    expect(field('#user').value).toBe('ada')
  })

  it('fills a one-time code field', () => {
    page(`<form><input id="code" autocomplete="one-time-code"></form>`)
    expect(fillSignInForm(field('#code'), { oneTimeCode: '123456' })).toBe(1)
    expect(field('#code').value).toBe('123456')
  })

  it('leaves out what it was not given', () => {
    page(`<form><input id="user" autocomplete="username" value="keep"><input id="pass" type="password"></form>`)
    fillSignInForm(field('#pass'), { password: 'hunter2' })
    expect(field('#user').value).toBe('keep')
  })
})

describe('findFormToFill', () => {
  it('prefers the field last used, if it is still shown', () => {
    page(`<input id="a" type="password"><input id="b" type="password">`)
    expect(findFormToFill(document, field('#b'))?.id).toBe('b')
    field('#b').style.display = 'none'
    expect(findFormToFill(document, field('#b'))?.id).toBe('a')
  })

  it('then the first visible password field, then a user name field', () => {
    page(
      `<input id="hidden" type="password" style="display:none"><input id="user" autocomplete="username"><input id="pass" type="password">`,
    )
    expect(findFormToFill(document, null)?.id).toBe('pass')
    field('#pass').remove()
    expect(findFormToFill(document, null)?.id).toBe('user')
    field('#user').remove()
    expect(findFormToFill(document, null)).toBeNull()
  })
})

describe('foreignFillActionHost', () => {
  it('names the foreign form of a field filled from the whole page when the anchor has no form', () => {
    page(`
      <div><input id="user" type="text" name="username" autocomplete="username"></div>
      <form action="https://collect.example.test/steal"><input id="pass" type="password" autocomplete="current-password"></form>`)
    expect(foreignFillActionHost(field('#user'))).toBe('collect.example.test')
    expect(foreignFillActionHost(field('#pass'))).toBe('collect.example.test')
  })

  it('names the foreign formaction of a button in the form of a field that will be filled', () => {
    page(`
      <div><input id="user" autocomplete="username"></div>
      <form id="other"><input id="pass" type="password" autocomplete="current-password"></form>
      <button form="other" formaction="https://collect.example.test/steal">Go</button>`)
    expect(foreignFillActionHost(field('#user'))).toBe('collect.example.test')
  })

  it('is empty when every field that will be filled is in a form that stays on the page', () => {
    page(`
      <div><input id="user" autocomplete="username"></div>
      <form action="/login"><input id="pass" type="password" autocomplete="current-password"></form>`)
    expect(foreignFillActionHost(field('#user'))).toBe('')
  })
})
