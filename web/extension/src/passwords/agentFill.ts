// The agent's fill_login and fill_one_time_code actions in the attached tab
// (web/extension/src/background.js calls these from act). The server builds
// the arguments after checking that the item is one the person let the agent
// use and that the tab's site is one of its sites; the model never sees the
// values. The extension checks the tab's address itself as well, because it is
// the one that knows where the tab is now: a page that navigated since the
// server looked gets nothing.
//
// The answer says what was done and never repeats a value.

import { doesHostMatch, isSecureContextPage, normalizeHost } from './hosts'

export interface AgentFillAnswer {
  ok: boolean
  data?: { isFilled: true }
  error?: string
}

interface FieldToFill {
  // ref is a snapshot element reference, as the other act actions take.
  ref: string | number
  value: string
  label: string
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
// types only into a visible, enabled text field.
function fillInPage(host: string, fields: FieldToFill[]): { ok: boolean; error?: string } {
  const normalize = (name: string): string => {
    let lowered = name.toLowerCase().replace(/\.+$/, '')
    if (lowered.startsWith('www.') && lowered.slice(4).includes('.')) lowered = lowered.slice(4)
    return lowered
  }
  if (normalize(location.hostname) !== host)
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
  const targets: { node: HTMLInputElement; value: string }[] = []
  for (const field of fields) {
    const node = references && (references.get(Number(field.ref)) ?? references.get(field.ref))
    if (!(node instanceof HTMLInputElement) || !node.isConnected) {
      return { ok: false, error: `the ${field.label} field is not on the page; take a snapshot first` }
    }
    if (node.type === 'hidden' || node.disabled || node.readOnly || !isVisible(node)) {
      return { ok: false, error: `the ${field.label} field cannot be typed into` }
    }
    targets.push({ node, value: field.value })
  }
  for (const target of targets) {
    target.node.focus()
    if (setter) setter.call(target.node, target.value)
    else target.node.value = target.value
    target.node.dispatchEvent(new Event('input', { bubbles: true }))
    target.node.dispatchEvent(new Event('change', { bubbles: true }))
  }
  return { ok: true }
}

async function fillFields(tabId: number, host: unknown, fields: FieldToFill[]): Promise<AgentFillAnswer> {
  const named = textOf(host)
  if (named === '') return { ok: false, error: 'the site this login is for was not named' }
  const tab = await chrome.tabs.get(tabId)
  const address = textOf(tab.url)
  // Only a page on the named host, over https (or a local test page): a
  // secret is never typed into a plain-text page of a real site.
  if (!doesHostMatch(address, named) || !isSecureContextPage(address)) {
    return { ok: false, error: 'the tab is not on the site this login is for' }
  }
  const [result] = await chrome.scripting.executeScript({
    target: { tabId },
    func: fillInPage,
    args: [normalizeHost(named), fields],
  })
  const answer = result?.result as { ok: boolean; error?: string } | undefined
  if (!answer) return { ok: false, error: 'the page did not answer' }
  return answer.ok ? { ok: true, data: { isFilled: true } } : { ok: false, error: answer.error }
}

// fillLoginForAgent types the user name and password into the fields the
// snapshot references name. Either reference may be empty.
export async function fillLoginForAgent(tabId: number, args: Record<string, unknown>): Promise<AgentFillAnswer> {
  const fields: FieldToFill[] = []
  if (hasRef(args.userNameRef)) fields.push({ ref: args.userNameRef, value: textOf(args.userName), label: 'user name' })
  if (hasRef(args.passwordRef)) fields.push({ ref: args.passwordRef, value: textOf(args.password), label: 'password' })
  if (fields.length === 0) return { ok: false, error: 'name the user name or the password field to fill' }
  if (fields.some((field) => field.value === ''))
    return { ok: false, error: 'there is nothing to type into a field that was named' }
  return fillFields(tabId, args.host, fields)
}

// fillOneTimeCodeForAgent types a one-time code into the field the snapshot
// reference names.
export async function fillOneTimeCodeForAgent(tabId: number, args: Record<string, unknown>): Promise<AgentFillAnswer> {
  if (!hasRef(args.ref)) return { ok: false, error: 'name the one-time code field to fill' }
  if (textOf(args.code) === '') return { ok: false, error: 'there is no code to type' }
  return fillFields(tabId, args.host, [{ ref: args.ref, value: textOf(args.code), label: 'one-time code' }])
}
