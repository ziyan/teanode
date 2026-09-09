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
  '.access-pick-row',
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
const ROWS = '.subscription-row, .mailbox-row, .access-pick-row, tr.linked'

// The controls a row carries that are their own act rather than the row's.
// Anything else inside a row is the row's label, however it is named — which
// is the rule, so that a row added later needs nothing added here.
const ROW_ACTIONS = '.icon-button, .icon-action, input, .row-actions, .mailbox-star'

function pressable(target: EventTarget | null): HTMLElement | null {
  const pressed = (target as HTMLElement | null)?.closest?.(PRESSABLE) as HTMLElement | null
  if (!pressed || pressed.hasAttribute('disabled') || pressed.getAttribute('aria-disabled') === 'true') {
    return null
  }

  // What is being pressed is decided before anything is asked about it. A
  // row's label is the row: the label is a button, so the nearest match is the
  // label, and a mark clipped to it stopped two thirds of the way across a row
  // that had been pressed all the way. What the press means there is "open
  // this", and that is the whole row.
  //
  // A control that is its own act — the star, a checkbox, the pencil at the
  // end of a row — stays itself: it does something to the row rather than
  // opening it, and should look like the smaller act it is. Which is the rule
  // here, rather than a list of label names: anything in a row that is not one
  // of the row's own controls is the row.
  //
  // Asking in the other order was the bug this replaced: the label was judged
  // on its own borders, found to have none, and refused before anything
  // noticed it stood for a row.
  const row = pressed.closest(ROWS) as HTMLElement | null
  const element =
    row && (pressed === row || !(pressed.matches(ROW_ACTIONS) || pressed.closest('.row-actions')))
      ? row
      : pressed

  if (element.closest('.tabs')) {
    return null
  }

  // The mark at the head of the rail is a picture that happens to be a link
  // home. A rectangle spreading out of it reads as the logo being pressed
  // rather than as somewhere being gone to.
  if (element.classList.contains('sidebar-brand')) {
    return null
  }

  // A control whose arrow turns has already said the press landed, and said
  // it better: the turn shows which way the thing went. A mark as well is two
  // answers to one press.
  if (element.querySelector('.chevron')) {
    return null
  }

  // A mark fills a box, so a control drawn without one has nothing to fill. A
  // link-like button is a word, an icon button without a border is an icon,
  // and a mark on either is a rectangle appearing around something that never
  // had one. Rows and tiles are exempt: their box is drawn by what they sit
  // in rather than by themselves.
  if (!element.matches(ALWAYS) && !drawn(element)) {
    return null
  }

  return element
}

// Things whose box is drawn by what they sit in rather than by themselves, so
// asking whether they have a border of their own answers the wrong question.
const ALWAYS = ROWS + ', a.tile, .sidebar a'

// drawn says whether a control has a box: a border on any side, or a
// background that is actually painted.
function drawn(element: HTMLElement): boolean {
  const style = window.getComputedStyle(element)

  const sides = [
    [style.borderTopStyle, style.borderTopWidth],
    [style.borderRightStyle, style.borderRightWidth],
    [style.borderBottomStyle, style.borderBottomWidth],
    [style.borderLeftStyle, style.borderLeftWidth],
  ]
  if (sides.some(([kind, width]) => kind !== 'none' && kind !== 'hidden' && parseFloat(width) > 0)) {
    return true
  }

  // A background counts only when it is painted. "transparent" and a colour
  // with no alpha are both nothing on the screen, whatever they are written
  // as, so the alpha is what is read.
  const background = style.backgroundColor
  if (!background || background === 'transparent') {
    return false
  }
  const alpha = background.startsWith('rgba(') ? parseFloat(background.split(',')[3] ?? '1') : 1
  return alpha > 0.01
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

      // The mark is drawn where the control was, not inside it, so it has to
      // be taken away when the control stops being there. Plenty of presses
      // end the thing that was pressed: a dialog button closes the dialog, a
      // menu item closes the menu, "load the pictures" takes away the notice
      // that asked, archiving a row removes the row. Without this the mark
      // hangs in the air for half a second over whatever moved in underneath.
      //
      // Watched by frame rather than by mutation, because the control can also
      // stay and simply move — the list scrolls, a banner above it closes —
      // and a mark left at the old place is as wrong as one left over nothing.
      // The backstop. Frames stop being served to a tab nobody is looking at,
      // so the watch below can simply never run again — and a mark that is
      // only ever removed by a frame would then still be there when the
      // reader comes back to the tab.
      const backstop = window.setTimeout(() => clip.remove(), DURATION + 100)

      const start = performance.now()
      const watch = () => {
        if (!clip.isConnected) {
          return
        }
        const now = element.getBoundingClientRect()
        const gone =
          !element.isConnected ||
          now.width === 0 ||
          Math.abs(now.left - box.left) > 1 ||
          Math.abs(now.top - box.top) > 1 ||
          Math.abs(now.width - box.width) > 1 ||
          Math.abs(now.height - box.height) > 1
        if (gone || performance.now() - start >= DURATION) {
          window.clearTimeout(backstop)
          clip.remove()
          return
        }
        window.requestAnimationFrame(watch)
      }
      window.requestAnimationFrame(watch)
    },
    // Passive: this only draws, and must never delay what the press does.
    { passive: true },
  )
}
