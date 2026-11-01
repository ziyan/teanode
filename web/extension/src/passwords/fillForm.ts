// Typing a login into the sign-in form around a field, only into fields a
// person can see. Used by the content script when the person picks a login,
// or a generated password, in the toolbar button's dropdown.

import {
  findLoginScope,
  foreignFormActionHost,
  isFieldVisible,
  roleOfFocusedField,
  setFieldValue,
} from './fieldDetection'

export interface Secrets {
  userName?: string
  password?: string
  oneTimeCode?: string
  // newPassword is a password the generator made, for the form's new
  // password field and its confirmation.
  newPassword?: string
}

// fillSignInForm types into the visible fields of the form (or page) around
// the anchor field and returns how many it filled. The user name goes into
// the user name field, the password into the current password field (a field
// the page marks as a new password is never given the saved one), the code
// into the code field, and a generated password into every new password field.
export function fillSignInForm(anchor: HTMLInputElement, secrets: Secrets): number {
  const scope = findLoginScope(anchor)
  let filledCount = 0
  const fill = (field: HTMLInputElement | null, value: string | undefined) => {
    if (!field || !value) return
    setFieldValue(field, value)
    filledCount += 1
  }
  fill(scope.userNameField, secrets.userName)
  fill(scope.currentPasswordField, secrets.password)
  const anchorRole = roleOfFocusedField(anchor)
  fill(scope.oneTimeCodeField ?? (anchorRole === 'oneTimeCode' ? anchor : null), secrets.oneTimeCode)
  for (const field of scope.newPasswordFields) fill(field, secrets.newPassword)
  return filledCount
}

// foreignFillActionHost is the host a form sends to, when that is not the
// page's own origin, for any of the fields fillSignInForm would type into
// around this anchor, else an empty text. On a page with no form around the
// anchor the fields come from the whole page, and each one may sit in a form of
// its own that sends elsewhere, so every field is asked for its own form and
// the first foreign host found is named. The anchor itself is asked too.
export function foreignFillActionHost(anchor: HTMLInputElement): string {
  const scope = findLoginScope(anchor)
  const anchorRole = roleOfFocusedField(anchor)
  const fields = [
    anchor,
    scope.userNameField,
    scope.currentPasswordField,
    scope.oneTimeCodeField ?? (anchorRole === 'oneTimeCode' ? anchor : null),
  ]
  for (const field of fields) {
    if (!field) continue
    const host = foreignFormActionHost(field)
    if (host !== '') return host
  }
  return ''
}

// findFormToFill is the field to start from, the page having given the focus
// to the dropdown: the field last used if it is still
// there, else the first visible password field, else the first visible field
// that looks like a user name.
export function findFormToFill(document: Document, lastField: HTMLInputElement | null): HTMLInputElement | null {
  if (lastField && lastField.isConnected && isFieldVisible(lastField)) return lastField
  const inputs = Array.from(document.querySelectorAll('input')).filter(isFieldVisible)
  const password = inputs.find((input) => input.type.toLowerCase() === 'password')
  if (password) return password
  return inputs.find((input) => roleOfFocusedField(input) !== null) ?? null
}
