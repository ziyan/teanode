// What the Browser page sends to a tab: where a click lands, which keys
// were held and what a key press carries. Pure functions, so the mapping is
// tested without a browser. The shapes are the viewer messages in
// docs/planning/tab-take-over-execplan.md.

// PictureRect is the drawn picture's box on the screen, as
// getBoundingClientRect gives it.
export interface PictureRect {
  left: number
  top: number
  width: number
  height: number
}

// pointToTab is where a point on the picture is in the tab: CSS pixels in the
// tab's viewport. frameWidth and frameHeight are the page's CSS size from the
// last frame, which the picture is scaled from. A point outside the picture
// (a drag that left it) lands on the nearest edge of the page.
export function pointToTab(
  clientX: number,
  clientY: number,
  pictureRect: PictureRect,
  frameWidth: number,
  frameHeight: number,
): { x: number; y: number } {
  const clamp = (value: number, most: number) => Math.min(Math.max(value, 0), most)
  if (pictureRect.width <= 0 || pictureRect.height <= 0) return { x: 0, y: 0 }
  return {
    x: clamp(((clientX - pictureRect.left) / pictureRect.width) * frameWidth, frameWidth),
    y: clamp(((clientY - pictureRect.top) / pictureRect.height) * frameHeight, frameHeight),
  }
}

// ModifierState is the part of a keyboard or pointer event that says which
// modifier keys were down.
export interface ModifierState {
  altKey: boolean
  ctrlKey: boolean
  metaKey: boolean
  shiftKey: boolean
}

// modifiersOf is the DevTools modifier bit field: Alt 1, Control 2, Meta 4,
// Shift 8.
export function modifiersOf(event: ModifierState): number {
  return (event.altKey ? 1 : 0) | (event.ctrlKey ? 2 : 0) | (event.metaKey ? 4 : 0) | (event.shiftKey ? 8 : 0)
}

// KeyEventLike is what keyMessage reads of a keyboard event, so a test can
// hand it a plain object.
export interface KeyEventLike extends ModifierState {
  key: string
  code: string
  keyCode?: number
}

export interface KeyMessage {
  type: 'key'
  event: 'down' | 'up'
  key: string
  code: string
  keyCode: number
  text: string
  modifiers: number
}

// The Windows virtual key codes of the keys that are not a letter or a digit.
const KEY_CODES: Record<string, number> = {
  Backspace: 8,
  Tab: 9,
  Enter: 13,
  Shift: 16,
  Control: 17,
  Alt: 18,
  Escape: 27,
  ' ': 32,
  PageUp: 33,
  PageDown: 34,
  End: 35,
  Home: 36,
  ArrowLeft: 37,
  ArrowUp: 38,
  ArrowRight: 39,
  ArrowDown: 40,
  Delete: 46,
  Meta: 91,
}

// keyCodeOf is the virtual key code of a key. A letter or a digit is known
// by its physical key when there is one, so that Shift+1 is still the 1 key
// and a letter typed on another layout is still its own key; the browser's
// own number is the last resort.
function keyCodeOf(event: KeyEventLike): number {
  const named = KEY_CODES[event.key]
  if (named !== undefined) return named
  const physical = /^(?:Key([A-Z])|Digit([0-9]))$/.exec(event.code)
  if (physical) return (physical[1] ?? physical[2]).charCodeAt(0)
  if (/^[a-z0-9]$/i.test(event.key)) return event.key.toUpperCase().charCodeAt(0)
  return event.keyCode ?? 0
}

// textOf is what pressing the key types: the character of a printable key,
// a carriage return for Enter, and nothing on release or with Control or
// Meta held, where the press is a shortcut and not typing.
function textOf(event: KeyEventLike, phase: 'down' | 'up'): string {
  if (phase !== 'down') return ''
  if (event.key === 'Enter') return '\r'
  if (event.ctrlKey || event.metaKey) return ''
  return [...event.key].length === 1 ? event.key : ''
}

// keyMessage builds the `key` message for a key going down or up.
export function keyMessage(event: KeyEventLike, phase: 'down' | 'up'): KeyMessage {
  return {
    type: 'key',
    event: phase,
    key: event.key,
    code: event.code,
    keyCode: keyCodeOf(event),
    text: textOf(event, phase),
    modifiers: modifiersOf(event),
  }
}

// buttonOf is the pointer button a pointer event's `button` number means.
export function buttonOf(button: number): 'left' | 'middle' | 'right' {
  if (button === 1) return 'middle'
  if (button === 2) return 'right'
  return 'left'
}

// ClickRecord is the last press, kept to tell a second press of a double
// click from a new click.
export interface ClickRecord {
  timeMS: number
  x: number
  y: number
  button: 'left' | 'middle' | 'right'
  clickCount: number
}

// A second press counts as part of a double click within this long and this
// far (in the tab's CSS pixels) of the first, as an operating system counts
// it. Pointer events do not carry the count themselves.
const CLICK_WINDOW_MS = 500
const CLICK_DISTANCE = 5

// clickCountOf is the clickCount of a press: one more than the press before
// it when it came soon enough, near enough, on the same button.
export function clickCountOf(
  previous: ClickRecord | null,
  timeMS: number,
  x: number,
  y: number,
  button: 'left' | 'middle' | 'right',
): number {
  if (
    previous &&
    previous.button === button &&
    timeMS - previous.timeMS <= CLICK_WINDOW_MS &&
    Math.abs(x - previous.x) <= CLICK_DISTANCE &&
    Math.abs(y - previous.y) <= CLICK_DISTANCE
  ) {
    return previous.clickCount + 1
  }
  return 1
}
