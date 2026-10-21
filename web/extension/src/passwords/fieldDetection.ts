// Finding the fields of a sign-in form on a page, and telling a field a
// person can see from one a page hid. The suggestion appears under a field
// only if it is visible, and a login is typed only into visible fields: a
// page that hides a user name or password field and waits for a manager to
// fill it is the oldest trick against managers.

import { normalizeHost } from './hosts'

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

// A field the page fades below this much (its own opacity times that of
// every element around it) is not one a person can be said to see.
const MINIMUM_OPACITY = 0.5
const MINIMUM_SIDE_PIXELS = 5

function isOpenToView(value: string | undefined): boolean {
  return value === undefined || value === '' || value === 'none' || value === 'auto'
}

// isClippedByOverflow says whether a box that cuts off what is outside it
// (an overflow other than visible) leaves too little of the field showing.
function isClippedByOverflow(clipper: Element, rect: DOMRect): boolean {
  const box = clipper.getBoundingClientRect()
  const width = Math.min(rect.right, box.right) - Math.max(rect.left, box.left)
  const height = Math.min(rect.bottom, box.bottom) - Math.max(rect.top, box.top)
  return width < MINIMUM_SIDE_PIXELS || height < MINIMUM_SIDE_PIXELS
}

// isCoveredAtCenter says whether something else is drawn over the middle of
// the field. Where the browser cannot say (the middle is off the screen, or it
// has no such lookup) it is not counted as covered.
function isCoveredAtCenter(input: HTMLInputElement, rect: DOMRect, view: Window): boolean {
  const centerHorizontal = rect.left + rect.width / 2
  const centerVertical = rect.top + rect.height / 2
  if (centerHorizontal < 0 || centerVertical < 0) return false
  if (centerHorizontal >= view.innerWidth || centerVertical >= view.innerHeight) return false
  const root = input.getRootNode()
  const lookup = root instanceof ShadowRoot || root instanceof Document ? root : input.ownerDocument
  if (typeof lookup.elementFromPoint !== 'function') return false
  const hit = lookup.elementFromPoint(centerHorizontal, centerVertical)
  return hit !== null && hit !== input && !input.contains(hit)
}

// isFieldVisible says whether a person can see and use the field: not hidden
// by display, visibility, opacity (the field's own and that of the elements
// around it, together at least MINIMUM_OPACITY), a clip path, a clip, an
// overflow that cuts it off, size, position far off the page, or another
// element drawn over its middle. It looks at what the browser computed, so a
// style a page set by script counts the same as one in its stylesheet.
export function isFieldVisible(input: HTMLInputElement): boolean {
  if (input.type.toLowerCase() === 'hidden' || input.hidden || !input.isConnected) return false
  const view = input.ownerDocument.defaultView
  if (!view) return false
  let opacity = 1
  const clippers: Element[] = []
  for (let element: Element | null = input; element; element = parentOf(element)) {
    const style = view.getComputedStyle(element)
    if (style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse') return false
    if (style.contentVisibility === 'hidden') return false
    opacity *= Number.parseFloat(style.opacity === '' ? '1' : style.opacity)
    if (!isOpenToView(style.clipPath)) return false
    // The old "clip" property cuts an element that is positioned out of the flow.
    if ((style.position === 'absolute' || style.position === 'fixed') && !isOpenToView(style.clip)) return false
    const isRoot = element === input.ownerDocument.body || element === input.ownerDocument.documentElement
    const overflows = [style.overflow, style.overflowX, style.overflowY]
    if (element !== input && !isRoot && overflows.some((value) => !isOpenToView(value) && value !== 'visible')) {
      clippers.push(element)
    }
  }
  if (opacity < MINIMUM_OPACITY) return false
  const rect = input.getBoundingClientRect()
  if (rect.width < MINIMUM_SIDE_PIXELS || rect.height < MINIMUM_SIDE_PIXELS) return false
  // Pushed off the left or top of the page, where nobody scrolls to.
  if (rect.right + view.scrollX <= 0 || rect.bottom + view.scrollY <= 0) return false
  if (clippers.some((clipper) => isClippedByOverflow(clipper, rect))) return false
  return !isCoveredAtCenter(input, rect, view)
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

// foreignFormActionHost is the host the form around a field sends to when that
// is not the host of the page the form is on, else an empty text. A form on a
// secure page that sends to a plain-text address counts too, whatever the host
// (it is named with "not encrypted" after it). The form's "action" and any
// control's "formaction" are read as attributes: a field named "action" inside
// the form would stand in for the form's action property. The controls are all
// those that belong to the form (form.elements), which includes a button
// outside the form that names it with its form attribute, and leaves out a
// button inside it that names another form.
export function foreignFormActionHost(input: HTMLInputElement): string {
  const form = input.form
  if (!form) return ''
  const document = input.ownerDocument
  const pageUrl = document.location?.href ?? ''
  const pageHost = normalizeHost(pageUrl)
  const isPageSecure = /^https:/i.test(pageUrl)
  const attributes = [
    form.getAttribute('action'),
    ...Array.from(form.elements).map((control) => control.getAttribute('formaction')),
  ]
  for (const attribute of attributes) {
    if (attribute === null || attribute.trim() === '') continue
    let resolved: URL
    try {
      resolved = new URL(attribute, document.baseURI)
    } catch {
      continue
    }
    const host = normalizeHost(resolved.href)
    if (host === '') continue
    if (host !== pageHost) return host
    if (isPageSecure && resolved.protocol === 'http:') return `${host} (not encrypted)`
  }
  return ''
}
