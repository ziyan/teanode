// The mark a press leaves behind.
//
// A circle spreading from where the pointer went down, on buttons and on the
// rows of a menu. It says the press landed, which matters most where the thing
// pressed does not visibly change — a menu item that navigates, a toolbar
// button whose effect is somewhere else on the page.
//
// Drawn in a layer of its own rather than inside the element pressed. The
// obvious way — a span inside the button, with overflow: hidden to clip it —
// changes the layout of every button in the program to get an effect, and
// clips anything a control legitimately draws outside itself. This clips the
// circle to a copy of the target's own box and corner radius instead, so
// nothing about the control changes at all.

const DURATION = 450

let layer: HTMLElement | null = null

function surface(): HTMLElement {
  if (layer && layer.isConnected) {
    return layer
  }
  layer = document.createElement('div')
  layer.className = 'ripple-layer'
  layer.setAttribute('aria-hidden', 'true')
  document.body.appendChild(layer)
  return layer
}

// What takes a ripple: the things a person presses. Not every clickable thing
// — a link inside a sentence is read, not operated — and nothing disabled,
// which is a press that does nothing and should look like it.
const PRESSABLE = [
  'button',
  '[role="menuitem"]',
  '.sidebar a',
  // The rows of a list, and of any table whose rows go somewhere.
  '.subscription-row',
  '.mailbox-row',
  'tr.linked',
  // A tile with somewhere to go. One that is only a number is not pressed.
  'a.tile',
].join(', ')

// What never takes one, whatever it is built from. A tab is a button, so
// naming buttons above catches it: it is drawn as a word with a line under
// the chosen one and no box of its own, and a mark clipped to its bounds is a
// rectangle appearing around something that has no rectangle — which reads as
// a mistake rather than as a press.

// Rows where the whole width is the thing being pressed.
const ROWS = '.subscription-row, .mailbox-row'

// The part of such a row that is the row's own label rather than a separate
// control sitting inside it.
const ROW_LABELS = '.subscription-row-link, .mailbox-row-link'

function pressable(target: EventTarget | null): HTMLElement | null {
  const element = (target as HTMLElement | null)?.closest?.(PRESSABLE) as HTMLElement | null
  if (!element || element.hasAttribute('disabled') || element.getAttribute('aria-disabled') === 'true') {
    return null
  }

  if (element.closest('.tabs')) {
    return null
  }

  // An icon button with no border is an icon: there is nothing drawn for the
  // mark to fill, so it appears as a box around a shape that never had one.
  // The ones drawn with a border are buttons, and those take it.
  if (element.classList.contains('icon-button') && !bordered(element)) {
    return null
  }

  // A row's label is the row. The label is a button, so the nearest match is
  // the label — and the mark then stopped at the label's edge, two thirds of
  // the way across, which reads as the row having been half pressed. What the
  // press means is "open this row", and that is the whole row.
  //
  // A control that is its own thing inside the row — the star, a checkbox, an
  // action — is still itself: pressing it does something to the row rather
  // than opening it, and it should look like the smaller act it is.
  const row = element.closest(ROWS) as HTMLElement | null
  if (row && (element === row || element.matches(ROW_LABELS))) {
    return row
  }
  return element
}

// bordered says whether a control is drawn with a border on any side.
function bordered(element: HTMLElement): boolean {
  const style = window.getComputedStyle(element)
  const sides = [
    [style.borderTopStyle, style.borderTopWidth],
    [style.borderRightStyle, style.borderRightWidth],
    [style.borderBottomStyle, style.borderBottomWidth],
    [style.borderLeftStyle, style.borderLeftWidth],
  ]
  return sides.some(([kind, width]) => kind !== 'none' && kind !== 'hidden' && parseFloat(width) > 0)
}

export function startRipples() {
  // Somebody who has asked for less movement gets none. The press still does
  // what it does; it simply does not draw.
  if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
    return
  }

  document.addEventListener(
    'pointerdown',
    (event) => {
      if (event.button !== 0) {
        return
      }
      const element = pressable(event.target)
      if (!element) {
        return
      }
      const box = element.getBoundingClientRect()
      if (box.width === 0 || box.height === 0) {
        return
      }

      // Big enough to reach the far corner from wherever it started, so the
      // circle covers the control rather than stopping short of it.
      const x = event.clientX - box.left
      const y = event.clientY - box.top
      const reach = Math.max(
        Math.hypot(x, y),
        Math.hypot(box.width - x, y),
        Math.hypot(x, box.height - y),
        Math.hypot(box.width - x, box.height - y),
      )

      const clip = document.createElement('span')
      clip.className = 'ripple-clip'
      clip.style.left = `${box.left}px`
      clip.style.top = `${box.top}px`
      clip.style.width = `${box.width}px`
      clip.style.height = `${box.height}px`
      // The control's own corners, so the circle stops where the control does.
      clip.style.borderRadius = window.getComputedStyle(element).borderRadius

      const circle = document.createElement('span')
      circle.className = 'ripple'
      circle.style.left = `${x}px`
      circle.style.top = `${y}px`
      circle.style.width = `${reach * 2}px`
      circle.style.height = `${reach * 2}px`

      clip.appendChild(circle)
      surface().appendChild(clip)
      window.setTimeout(() => clip.remove(), DURATION)
    },
    // Passive: this only draws, and must never delay what the press does.
    { passive: true },
  )
}
