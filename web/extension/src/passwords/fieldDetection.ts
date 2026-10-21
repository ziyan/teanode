// Finding the fields of a sign-in form on a page, and telling a field a
// person can see from one a page hid. The suggestion appears under a field
// only if it is visible, and a login is typed only into visible fields: a
// page that hides a user name or password field and waits for a manager to
// fill it is the oldest trick against managers.

export type FieldRole = 'userName' | 'currentPassword' | 'newPassword' | 'oneTimeCode'

const TEXT_LIKE_TYPES = new Set(['', 'text', 'email', 'tel', 'number', 'url'])
const IGNORED_NAME_PATTERN = /search|captcha|coupon|promo|zip|postal|city|address|company|first|last|middle/i
const USER_NAME_PATTERN = /user|login|log-in|email|e-mail|account|identifier|member|phone|mobile|id$/i
const ONE_TIME_CODE_PATTERN = /otp|one.?time|verification.?code|auth.?code|2fa|mfa|totp|security.?code/i

function autocompleteTokens(input: HTMLInputElement): string[] {
  return (input.getAttribute('autocomplete') ?? '')
    .toLowerCase()
    .split(/\s+/)
    .filter((token) => token !== '')
}

function nameText(input: HTMLInputElement): string {
  return [input.name, input.id, input.getAttribute('aria-label'), input.placeholder].filter(Boolean).join(' ')
}

// classifyField says what a field is for, or null when it is none of the
// things a manager fills. The page's own autocomplete hint wins; the field's
// name is a guess that only counts for a user name, and only where a password
// field sits beside it (see hasPasswordFieldNear).
export function classifyField(input: HTMLInputElement, hasPasswordFieldNear = false): FieldRole | null {
  if (input.disabled || input.readOnly) return null
  const type = input.type.toLowerCase()
  const tokens = autocompleteTokens(input)
  if (type === 'password') {
    return tokens.includes('new-password') ? 'newPassword' : 'currentPassword'
  }
  if (!TEXT_LIKE_TYPES.has(type)) return null
  if (tokens.includes('one-time-code')) return 'oneTimeCode'
  if (tokens.includes('username') || tokens.includes('email')) return 'userName'
  if (tokens.includes('off') || tokens.some((token) => token.startsWith('cc-'))) return null
  const name = nameText(input)
  if (IGNORED_NAME_PATTERN.test(name)) return null
  if (ONE_TIME_CODE_PATTERN.test(name) && (input.maxLength === -1 || input.maxLength <= 10)) return 'oneTimeCode'
  if (hasPasswordFieldNear && (type === '' || type === 'text' || type === 'email') && USER_NAME_PATTERN.test(name)) {
    return 'userName'
  }
  return null
}

// isFieldVisible says whether a person can see and use the field: not hidden
// by display, visibility, opacity (the field's own or an ancestor's), size or
// position far off the page. It looks at what the browser computed, so a
// style a page set by script counts the same as one in its stylesheet.
export function isFieldVisible(input: HTMLInputElement): boolean {
  if (input.type.toLowerCase() === 'hidden' || input.hidden || !input.isConnected) return false
  const view = input.ownerDocument.defaultView
  if (!view) return false
  let opacity = 1
  for (let element: Element | null = input; element; element = parentOf(element)) {
    const style = view.getComputedStyle(element)
    if (style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse') return false
    if (style.contentVisibility === 'hidden') return false
    opacity *= Number.parseFloat(style.opacity === '' ? '1' : style.opacity)
  }
  if (opacity < 0.1) return false
  const rect = input.getBoundingClientRect()
  if (rect.width < 5 || rect.height < 5) return false
  // Pushed off the left or top of the page, where nobody scrolls to.
  if (rect.right + view.scrollX <= 0 || rect.bottom + view.scrollY <= 0) return false
  return true
}

function parentOf(element: Element): Element | null {
  if (element.parentElement) return element.parentElement
  const root = element.getRootNode()
  return root instanceof ShadowRoot ? root.host : null
}

// The scope of a field: its form, or, on a page with no form, the whole
// document, which is how many single page sites are built.
export function scopeOf(input: HTMLInputElement): ParentNode {
  return input.form ?? input.ownerDocument.body ?? input.ownerDocument
}

function inputsIn(scope: ParentNode): HTMLInputElement[] {
  return Array.from(scope.querySelectorAll('input'))
}

export interface LoginScope {
  userNameField: HTMLInputElement | null
  currentPasswordField: HTMLInputElement | null
  newPasswordFields: HTMLInputElement[]
  oneTimeCodeField: HTMLInputElement | null
}

// findLoginScope reads the sign-in fields around a field: the visible ones in
// its form (or on its page). The user name is the last user-name-like field
// before the first password field, or the focused field itself when that is
// one.
export function findLoginScope(focused: HTMLInputElement): LoginScope {
  const scope = scopeOf(focused)
  const inputs = inputsIn(scope).filter(isFieldVisible)
  const hasPasswordField = inputs.some((input) => input.type.toLowerCase() === 'password')
  const roles = new Map<HTMLInputElement, FieldRole>()
  for (const input of inputs) {
    const role = classifyField(input, hasPasswordField)
    if (role) roles.set(input, role)
  }
  const passwordFields = inputs.filter((input) => input.type.toLowerCase() === 'password')
  const currentPasswordField = passwordFields.find((input) => roles.get(input) === 'currentPassword') ?? null
  const newPasswordFields = passwordFields.filter((input) => roles.get(input) === 'newPassword')
  const firstPasswordField = passwordFields[0] ?? null
  const candidates = inputs.filter((input) => roles.get(input) === 'userName')
  const before = firstPasswordField
    ? candidates.filter((input) => input.compareDocumentPosition(firstPasswordField) & Node.DOCUMENT_POSITION_FOLLOWING)
    : candidates
  const userNameField =
    roles.get(focused) === 'userName' ? focused : (before[before.length - 1] ?? candidates[0] ?? null)
  const oneTimeCodeField = inputs.find((input) => roles.get(input) === 'oneTimeCode') ?? null
  return { userNameField, currentPasswordField, newPasswordFields, oneTimeCodeField }
}

// isLoginField says whether a focused field is one the extension offers a
// suggestion under, and what for.
export function roleOfFocusedField(input: HTMLInputElement): FieldRole | null {
  if (!isFieldVisible(input)) return null
  const hasPasswordField = inputsIn(scopeOf(input)).some(
    (other) => other.type.toLowerCase() === 'password' && isFieldVisible(other),
  )
  return classifyField(input, hasPasswordField)
}

export interface SubmittedLogin {
  userName: string
  password: string
  // isNewPassword: the password was typed into a field the page marked as a
  // new one, or typed twice (a sign-up or a password change).
  isNewPassword: boolean
}

// readSubmittedLogin reads what a person typed in a form being sent: the
// password (the new one when the form changes it) and the user name beside
// it. It is null when no visible password field holds a value. A form with
// a user name and no password is a first step; its user name is returned
// with an empty password, for the page that asks for the password next.
export function readSubmittedLogin(scope: ParentNode, focused?: HTMLInputElement): SubmittedLogin | null {
  const inputs = inputsIn(scope).filter(isFieldVisible)
  const anchor = focused ?? inputs.find((input) => input.type.toLowerCase() === 'password') ?? inputs[0] ?? null
  const passwordFields = inputs.filter((input) => input.type.toLowerCase() === 'password' && input.value !== '')
  const userNameField = anchor ? findLoginScope(anchor).userNameField : null
  const userName = userNameField?.value.trim() ?? ''
  if (passwordFields.length === 0) {
    const hasAnyPasswordField = inputs.some((input) => input.type.toLowerCase() === 'password')
    if (hasAnyPasswordField || userName === '') return null
    return { userName, password: '', isNewPassword: false }
  }
  const declaredNew = passwordFields.filter((input) => classifyField(input) === 'newPassword')
  if (declaredNew.length > 0) return { userName, password: declaredNew[0].value, isNewPassword: true }
  const values = passwordFields.map((input) => input.value)
  const repeated = values.find((value, index) => values.indexOf(value) !== index)
  if (repeated !== undefined) return { userName, password: repeated, isNewPassword: true }
  return { userName, password: values[values.length - 1], isNewPassword: false }
}

// setFieldValue types a value the way a person would, so a page built on a
// framework notices: the native setter (a framework may have replaced the
// property on the element), then input and change events.
export function setFieldValue(input: HTMLInputElement, value: string): void {
  const view = input.ownerDocument.defaultView
  const setter = view ? Object.getOwnPropertyDescriptor(view.HTMLInputElement.prototype, 'value')?.set : undefined
  input.focus()
  if (setter) setter.call(input, value)
  else input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
  input.dispatchEvent(new Event('change', { bubbles: true }))
}
