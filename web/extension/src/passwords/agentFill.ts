// The agent's fill_login and fill_one_time_code actions in the attached tab
// (web/extension/src/background.js calls these from act). The server builds
// the arguments after checking that the item is one the person let the agent
// use and that the tab's site is one of its sites; the model never sees the
// values. The extension checks the tab's address itself as well, because it is
// the one that knows where the tab is now: a page that navigated since the
// server looked gets nothing.
//
// The answer says what was done and never repeats a value. It also says which
// document it filled (the browser's id for it), so that the server can keep a
// lock on that document and nothing else.

import { doesHostMatch, isSecureContextPage, originOf } from './hosts'

export interface AgentFillAnswer {
  ok: boolean
  data?: { isFilled: true; documentId?: string }
  error?: string
}

// FieldKind is what a field is to hold, which the field must be able to hold.
type FieldKind = 'userName' | 'password' | 'oneTimeCode'

interface FieldToFill {
  // ref is a snapshot element reference, as the other act actions take.
  ref: string | number
  value: string
  label: string
  fieldKind: FieldKind
}

function textOf(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function hasRef(value: unknown): value is string | number {
  return (typeof value === 'string' && value !== '') || (typeof value === 'number' && value > 0)
}

// fillInPage runs inside the page. It is self-contained: nothing from this
// file is in scope there. It checks the page's own address again before it
// types, looks the elements up in the references the last snapshot made, and
// types only into a visible, enabled input of the kind asked for: a password
// goes into a password input (or one whose autocomplete says current-password
// or new-password), a user name into a text, email or telephone input (or one
// whose autocomplete says username or email), a one-time code into a text,
// number or telephone input (or one whose autocomplete says one-time-code).
// A textarea, a select, an editable region and any other input is refused.
// A password or a code that goes in is noted in a set of the page's filled
// nodes, so that a snapshot never shows it, whatever the field's type
// becomes (a show-password button makes it a text input).
function fillInPage(origin: string, fields: FieldToFill[]): { isFilled: boolean; error?: string } {
  // The origin (scheme, host and port) the tab was checked to be on a moment
  // ago; a page that moved since gets nothing.
  if (location.origin !== origin) return { isFilled: false, error: 'the page is not on the site this login is for' }
  const references: Map<unknown, unknown> | undefined = (window as any).__teanodeRefs
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
  // The same test of "a person can see this field" as isFieldVisible in
  // fieldDetection.ts, written out again because this function runs inside the
  // page with nothing of this file in scope: not hidden, not faded below half
  // (the field's opacity with that of every element around it), not cut off
  // by a clip path, a clip or an overflow, big enough, and nothing else drawn
  // over its middle.
  const isOpenToView = (value: string | undefined): boolean =>
    value === undefined || value === '' || value === 'none' || value === 'auto'
  const isVisible = (node: HTMLElement): boolean => {
    let opacity = 1
    const clippers: Element[] = []
    for (let element: Element | null = node; element; element = element.parentElement) {
      const style = getComputedStyle(element)
      if (style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse') return false
      if (style.contentVisibility === 'hidden') return false
      opacity *= Number.parseFloat(style.opacity === '' ? '1' : style.opacity)
      if (!isOpenToView(style.clipPath)) return false
      if ((style.position === 'absolute' || style.position === 'fixed') && !isOpenToView(style.clip)) return false
      const isRoot = element === document.body || element === document.documentElement
      if (
        element !== node &&
        !isRoot &&
        [style.overflow, style.overflowX, style.overflowY].some((value) => !isOpenToView(value) && value !== 'visible')
      ) {
        clippers.push(element)
      }
    }
    const rect = node.getBoundingClientRect()
    if (opacity < 0.5 || rect.width < 5 || rect.height < 5) return false
    for (const clipper of clippers) {
      const box = clipper.getBoundingClientRect()
      const width = Math.min(rect.right, box.right) - Math.max(rect.left, box.left)
      const height = Math.min(rect.bottom, box.bottom) - Math.max(rect.top, box.top)
      if (width < 5 || height < 5) return false
    }
    const centerHorizontal = rect.left + rect.width / 2
    const centerVertical = rect.top + rect.height / 2
    const isCenterOnScreen =
      centerHorizontal >= 0 &&
      centerVertical >= 0 &&
      centerHorizontal < window.innerWidth &&
      centerVertical < window.innerHeight
    if (isCenterOnScreen && typeof document.elementFromPoint === 'function') {
      const hit = document.elementFromPoint(centerHorizontal, centerVertical)
      if (hit !== null && hit !== node && !node.contains(hit)) return false
    }
    return true
  }
  // A form that sends what is typed to another host than the page's own is
  // not filled for the agent: nobody can be asked here. The addresses are
  // those formActionAttributes in fieldDetection.ts reads, written out again
  // because nothing of that file is in scope here.
  const isFormSendingElsewhere = (node: HTMLInputElement): boolean => {
    const form = node.form
    if (!form) return false
    const getAttribute = Element.prototype.getAttribute
    const buttonForm = Object.getOwnPropertyDescriptor(HTMLButtonElement.prototype, 'form')?.get
    const inputForm = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'form')?.get
    const attributes: (string | null)[] = [getAttribute.call(form, 'action')]
    for (const control of Array.from(Document.prototype.querySelectorAll.call(document, '[formaction]'))) {
      let owner: HTMLFormElement | null = null
      if (control instanceof HTMLButtonElement && buttonForm) owner = buttonForm.call(control)
      else if (control instanceof HTMLInputElement && inputForm) owner = inputForm.call(control)
      if (owner === form) attributes.push(getAttribute.call(control, 'formaction'))
    }
    return attributes.some((attribute) => {
      if (attribute === null || attribute.trim() === '') return false
      try {
        const target = new URL(attribute, document.baseURI)
        if (target.protocol !== 'http:' && target.protocol !== 'https:') return false
        // Another host, or a secure page sending to a plain-text address.
        return target.host !== location.host || (location.protocol === 'https:' && target.protocol === 'http:')
      } catch {
        return false
      }
    })
  }
  const isKind = (node: HTMLInputElement, fieldKind: FieldKind): boolean => {
    const type = (node.type || 'text').toLowerCase()
    const tokens = (node.getAttribute('autocomplete') || '').toLowerCase().split(/\s+/)
    const has = (...names: string[]): boolean => names.some((name) => tokens.includes(name))
    const isTextual = ['text', 'email', 'tel', 'number', 'search', 'url', 'password'].includes(type)
    switch (fieldKind) {
      case 'password':
        return type === 'password' || (isTextual && has('current-password', 'new-password'))
      case 'userName':
        return ['text', 'email', 'tel'].includes(type) || (isTextual && type !== 'password' && has('username', 'email'))
      case 'oneTimeCode':
        return ['text', 'number', 'tel'].includes(type) || (isTextual && has('one-time-code'))
    }
  }
  const wanted: Record<FieldKind, string> = {
    password: 'a password input, or one whose autocomplete says current-password or new-password',
    userName: 'a text, email or telephone input, or one whose autocomplete says username or email',
    oneTimeCode: 'a text, number or telephone input, or one whose autocomplete says one-time-code',
  }
  const targets: { node: HTMLInputElement; value: string; fieldKind: FieldKind }[] = []
  for (const field of fields) {
    const node = references && (references.get(Number(field.ref)) ?? references.get(field.ref))
    if (!(node instanceof Element) || !node.isConnected) {
      return { isFilled: false, error: `the ${field.label} field is not on the page; take a snapshot first` }
    }
    if (!(node instanceof HTMLInputElement)) {
      return {
        isFilled: false,
        error: `the ${field.label} field is not an input; a textarea, a select and an editable region are never filled`,
      }
    }
    if (!isKind(node, field.fieldKind)) {
      return { isFilled: false, error: `the ${field.label} field is not ${wanted[field.fieldKind]}` }
    }
    if (node.type === 'hidden' || node.disabled || node.readOnly || !isVisible(node)) {
      return { isFilled: false, error: `the ${field.label} field cannot be typed into` }
    }
    if (isFormSendingElsewhere(node)) {
      return { isFilled: false, error: `the form around the ${field.label} field sends to another site than this page` }
    }
    targets.push({ node, value: field.value, fieldKind: field.fieldKind })
  }
  for (const target of targets) {
    if (target.fieldKind !== 'userName') {
      const filledNodes: WeakSet<Node> =
        (window as any).__teanodeFilled ?? ((window as any).__teanodeFilled = new WeakSet())
      filledNodes.add(target.node)
    }
    target.node.focus()
    if (setter) setter.call(target.node, target.value)
    else target.node.value = target.value
    target.node.dispatchEvent(new Event('input', { bubbles: true }))
    target.node.dispatchEvent(new Event('change', { bubbles: true }))
  }
  return { isFilled: true }
}

// effectivePort is the port an address is on: the one it names, or the default
// of its scheme.
function effectivePort(address: string): number {
  try {
    const parsed = new URL(address)
    if (parsed.port !== '') return Number(parsed.port)
    return parsed.protocol === 'https:' ? 443 : 80
  } catch {
    return 0
  }
}

async function fillFields(
  tabId: number,
  host: unknown,
  port: unknown,
  fields: FieldToFill[],
): Promise<AgentFillAnswer> {
  const named = textOf(host)
  if (named === '') return { ok: false, error: 'the site this login is for was not named' }
  const tab = await chrome.tabs.get(tabId)
  const address = textOf(tab.url)
  // The port the server found the page on; a call that names none means the
  // default port, so the page is on the site's own port or nothing is filled.
  const namedPort = typeof port === 'number' && port > 0 ? port : /^\s*http:/i.test(address) ? 80 : 443
  // Only a page on the named host and port, over https (or a local test
  // page): a secret is never typed into a plain-text page of a real site.
  if (!doesHostMatch(address, named) || !isSecureContextPage(address) || effectivePort(address) !== namedPort) {
    return { ok: false, error: 'the tab is not on the site this login is for' }
  }
  const [result] = await chrome.scripting.executeScript({
    target: { tabId },
    func: fillInPage,
    // The origin just checked, scheme and host and port, which the page is
    // asked to still be at.
    args: [originOf(address), fields],
  })
  const answer = result?.result as { isFilled: boolean; error?: string } | undefined
  if (!answer) return { ok: false, error: 'the page did not answer' }
  if (!answer.isFilled) return { ok: false, error: answer.error }
  const documentId = typeof result.documentId === 'string' ? result.documentId : undefined
  return { ok: true, data: documentId ? { isFilled: true, documentId } : { isFilled: true } }
}

// fillLoginForAgent types the user name and password into the fields the
// snapshot references name. Either reference may be empty.
export async function fillLoginForAgent(
  tabId: number,
  actionArguments: Record<string, unknown>,
): Promise<AgentFillAnswer> {
  const fields: FieldToFill[] = []
  if (hasRef(actionArguments.userNameRef))
    fields.push({
      ref: actionArguments.userNameRef,
      value: textOf(actionArguments.userName),
      label: 'user name',
      fieldKind: 'userName',
    })
  if (hasRef(actionArguments.passwordRef))
    fields.push({
      ref: actionArguments.passwordRef,
      value: textOf(actionArguments.password),
      label: 'password',
      fieldKind: 'password',
    })
  if (fields.length === 0) return { ok: false, error: 'name the user name or the password field to fill' }
  if (fields.some((field) => field.value === ''))
    return { ok: false, error: 'there is nothing to type into a field that was named' }
  return fillFields(tabId, actionArguments.host, actionArguments.port, fields)
}

// fillOneTimeCodeForAgent types a one-time code into the field the snapshot
// reference names.
export async function fillOneTimeCodeForAgent(
  tabId: number,
  actionArguments: Record<string, unknown>,
): Promise<AgentFillAnswer> {
  if (!hasRef(actionArguments.ref)) return { ok: false, error: 'name the one-time code field to fill' }
  if (textOf(actionArguments.code) === '') return { ok: false, error: 'there is no code to type' }
  return fillFields(tabId, actionArguments.host, actionArguments.port, [
    { ref: actionArguments.ref, value: textOf(actionArguments.code), label: 'one-time code', fieldKind: 'oneTimeCode' },
  ])
}
