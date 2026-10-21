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

import { doesHostMatch, isSecureContextPage, normalizeHost } from './hosts'

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
  kind: FieldKind
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
function fillInPage(host: string, port: number, fields: FieldToFill[]): { ok: boolean; error?: string } {
  const normalize = (name: string): string => {
    let lowered = name.toLowerCase().replace(/\.+$/, '')
    if (lowered.startsWith('www.') && lowered.slice(4).includes('.')) lowered = lowered.slice(4)
    return lowered
  }
  const pagePort = Number(location.port || (location.protocol === 'https:' ? 443 : 80))
  if (normalize(location.hostname) !== host || pagePort !== port)
    return { ok: false, error: 'the page is not on the site this login is for' }
  const references: Map<unknown, unknown> | undefined = (window as any).__teanodeRefs
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set
  const isVisible = (node: HTMLElement): boolean => {
    let opacity = 1
    for (let element: Element | null = node; element; element = element.parentElement) {
      const style = getComputedStyle(element)
      if (style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse') return false
      opacity *= Number.parseFloat(style.opacity === '' ? '1' : style.opacity)
    }
    const rect = node.getBoundingClientRect()
    return opacity >= 0.1 && rect.width >= 5 && rect.height >= 5
  }
  const isKind = (node: HTMLInputElement, kind: FieldKind): boolean => {
    const type = (node.type || 'text').toLowerCase()
    const tokens = (node.getAttribute('autocomplete') || '').toLowerCase().split(/\s+/)
    const has = (...names: string[]): boolean => names.some((name) => tokens.includes(name))
    const isTextual = ['text', 'email', 'tel', 'number', 'search', 'url', 'password'].includes(type)
    switch (kind) {
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
  const targets: { node: HTMLInputElement; value: string; kind: FieldKind }[] = []
  for (const field of fields) {
    const node = references && (references.get(Number(field.ref)) ?? references.get(field.ref))
    if (!(node instanceof Element) || !node.isConnected) {
      return { ok: false, error: `the ${field.label} field is not on the page; take a snapshot first` }
    }
    if (!(node instanceof HTMLInputElement)) {
      return {
        ok: false,
        error: `the ${field.label} field is not an input; a textarea, a select and an editable region are never filled`,
      }
    }
    if (!isKind(node, field.kind)) {
      return { ok: false, error: `the ${field.label} field is not ${wanted[field.kind]}` }
    }
    if (node.type === 'hidden' || node.disabled || node.readOnly || !isVisible(node)) {
      return { ok: false, error: `the ${field.label} field cannot be typed into` }
    }
    targets.push({ node, value: field.value, kind: field.kind })
  }
  for (const target of targets) {
    if (target.kind !== 'userName') {
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
  return { ok: true }
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
    args: [normalizeHost(named), namedPort, fields],
  })
  const answer = result?.result as { ok: boolean; error?: string } | undefined
  if (!answer) return { ok: false, error: 'the page did not answer' }
  if (!answer.ok) return { ok: false, error: answer.error }
  const documentId = typeof result.documentId === 'string' ? result.documentId : undefined
  return { ok: true, data: documentId ? { isFilled: true, documentId } : { isFilled: true } }
}

// fillLoginForAgent types the user name and password into the fields the
// snapshot references name. Either reference may be empty.
export async function fillLoginForAgent(tabId: number, args: Record<string, unknown>): Promise<AgentFillAnswer> {
  const fields: FieldToFill[] = []
  if (hasRef(args.userNameRef))
    fields.push({ ref: args.userNameRef, value: textOf(args.userName), label: 'user name', kind: 'userName' })
  if (hasRef(args.passwordRef))
    fields.push({ ref: args.passwordRef, value: textOf(args.password), label: 'password', kind: 'password' })
  if (fields.length === 0) return { ok: false, error: 'name the user name or the password field to fill' }
  if (fields.some((field) => field.value === ''))
    return { ok: false, error: 'there is nothing to type into a field that was named' }
  return fillFields(tabId, args.host, args.port, fields)
}

// fillOneTimeCodeForAgent types a one-time code into the field the snapshot
// reference names.
export async function fillOneTimeCodeForAgent(tabId: number, args: Record<string, unknown>): Promise<AgentFillAnswer> {
  if (!hasRef(args.ref)) return { ok: false, error: 'name the one-time code field to fill' }
  if (textOf(args.code) === '') return { ok: false, error: 'there is no code to type' }
  return fillFields(tabId, args.host, args.port, [
    { ref: args.ref, value: textOf(args.code), label: 'one-time code', kind: 'oneTimeCode' },
  ])
}
