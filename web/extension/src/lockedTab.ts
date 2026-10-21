// What the extension refuses on its own for a tab that shows a document the
// agent filled a password or a one-time code into. The server locks such a tab
// too, but only when its update arrives, and a page restored from the history's
// cache is readable before that; the worker already knows locally (see
// tabDocuments.ts), so these checks do not wait for the server.
//
// The line is the same as the server's: while the tab shows a filled document
// the agent may drive it with real input and move it, and nothing may read it
// back. A key event that carries editing commands or a control, alt or meta
// modifier copies a filled field's text into an ordinary field, where nothing
// recognizes it as the secret; a pointer that moves with a button held drags
// the selected text into another field.

// The DevTools methods still answered on a tab showing a filled document.
const ALLOWED_METHODS = [
  'Input.dispatchMouseEvent',
  'Input.dispatchKeyEvent',
  'Input.dispatchTouchEvent',
  'Input.insertText',
  'Input.imeSetComposition',
  'Page.navigate',
  'Page.reload',
  'Page.getNavigationHistory',
  'Page.getFrameTree',
  'Page.getLayoutMetrics',
  'Page.enable',
  'Page.disable',
]

// The actions that can read a filled field, or what was sent from it.
const REFUSED_ACTIONS = new Set(['evaluate', 'screenshot', 'storage'])

// The modifier bits of a DevTools key event: alt (1), control (2) and meta (4).
// Shift (8) alone only changes which character is typed.
const KEY_MODIFIER_MASK = 1 | 2 | 4
const SHIFT_MODIFIER = 8

export const LOCKED_ACTION_REFUSAL =
  'This page was filled with a password or code. Scripts, pictures and storage are not read from it until the tab loads a new page. Click and type the rest of the sign-in.'
export const LOCKED_SELECTOR_REFUSAL =
  'A CSS selector answers yes or no about what a page holds, so on a page filled with a password or code only refs from a snapshot are used until the tab loads a new page. Take a snapshot and act by ref.'
const LOCKED_METHOD_REFUSAL =
  'is not answered on a page filled with a password or code until the tab loads a new page. Real input and moving the page still are.'
const LOCKED_CLIPBOARD_REFUSAL =
  'Copying, cutting, pasting and dragging are not done on a page filled with a password or code, until the tab loads a new page. Type the rest of the sign-in.'

function modifiersOf(params: Record<string, unknown>): number | null {
  const value = params.modifiers
  if (value === undefined || value === null) return 0
  return typeof value === 'number' && Number.isInteger(value) ? value : null
}

function isClipboardKeyName(name: unknown): boolean {
  return typeof name === 'string' && ['insert', 'copy', 'cut', 'paste'].includes(name.trim().toLowerCase())
}

function isKeyEventAllowed(params: Record<string, unknown>): boolean {
  if ('commands' in params) return false
  const modifiers = modifiersOf(params)
  if (modifiers === null || (modifiers & KEY_MODIFIER_MASK) !== 0) return false
  if (isClipboardKeyName(params.key) || isClipboardKeyName(params.code)) return false
  // Insert is also virtual key 45, and Shift with Delete cuts.
  if (params.windowsVirtualKeyCode === 45) return false
  if ((modifiers & SHIFT_MODIFIER) !== 0 && typeof params.key === 'string' && params.key.toLowerCase() === 'delete') {
    return false
  }
  return true
}

function isMouseEventAllowed(params: Record<string, unknown>): boolean {
  const button = typeof params.button === 'string' ? params.button : ''
  // The middle button pastes the selection on some systems.
  if (button.toLowerCase() === 'middle') return false
  if (params.type === 'mouseMoved') {
    if (button !== '' && button.toLowerCase() !== 'none') return false
    if (params.buttons !== undefined && params.buttons !== null && params.buttons !== 0) return false
  }
  return true
}

// lockedActionRefusal is why an action is not done on a tab showing a filled
// document, or an empty text. A selector is a yes or no question about the
// page (an attribute selector can match the start of a field's value), so
// only refs are used, and waiting for a selector is one too.
export function lockedActionRefusal(action: string, args: Record<string, unknown>): string {
  if (REFUSED_ACTIONS.has(action)) return LOCKED_ACTION_REFUSAL
  if (action === 'wait' && args.for === 'selector') return LOCKED_SELECTOR_REFUSAL
  const isSelectorAction = ['click', 'hover', 'type', 'select', 'scroll'].includes(action)
  if (isSelectorAction && args.selector && !args.ref) return LOCKED_SELECTOR_REFUSAL
  return ''
}

// lockedMethodRefusal is why a DevTools method, with these parameters, is not
// sent to a tab showing a filled document, or an empty text. The general
// rules for a navigation (cdpRefusal in background.js) apply to every tab.
export function lockedMethodRefusal(method: string, params: unknown): string {
  const asked = String(method || '').trim()
  const record = typeof params === 'object' && params !== null ? (params as Record<string, unknown>) : {}
  if (!ALLOWED_METHODS.includes(asked)) return `${asked} ${LOCKED_METHOD_REFUSAL}`
  if (asked === 'Input.dispatchKeyEvent' && !isKeyEventAllowed(record)) return LOCKED_CLIPBOARD_REFUSAL
  if (asked === 'Input.dispatchMouseEvent' && !isMouseEventAllowed(record)) return LOCKED_CLIPBOARD_REFUSAL
  if (asked === 'Page.navigate' && Object.keys(record).some((name) => name !== 'url')) {
    return 'Page.navigate takes only url on a page filled with a password or code'
  }
  return ''
}
