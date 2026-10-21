// Typing a login into the sign-in form around a field, only into fields a
// person can see. Used by the content script for a click in the suggestion,
// for the keyboard shortcut and for the context menu.

import { findLoginScope, isFieldVisible, roleOfFocusedField, setFieldValue } from './fieldDetection'

export interface Secrets {
  userName?: string
  password?: string
  oneTimeCode?: string
}

// fillSignInForm types into the visible fields of the form (or page) around
// the anchor field and returns how many it filled. The user name goes into
// the user name field, the password into the current password field (a field
// the page marks as a new password is never given the saved one), the code
// into the code field.
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
  return filledCount
}

// findFormToFill is the field to start from when nothing is focused (the
// keyboard shortcut, the context menu): the field last used if it is still
// there, else the first visible password field, else the first visible field
// that looks like a user name.
export function findFormToFill(document: Document, lastField: HTMLInputElement | null): HTMLInputElement | null {
  if (lastField && lastField.isConnected && isFieldVisible(lastField)) return lastField
  const inputs = Array.from(document.querySelectorAll('input')).filter(isFieldVisible)
  const password = inputs.find((input) => input.type.toLowerCase() === 'password')
  if (password) return password
  return inputs.find((input) => roleOfFocusedField(input) !== null) ?? null
}
