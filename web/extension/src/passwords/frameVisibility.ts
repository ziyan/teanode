// Is the extension's frame really in front of the person? A page can style
// the element the extension's frame sits in, and a published attack made a
// manager's fill menu invisible and then tricked a click onto it. So before a
// choice made in the frame is acted on, the content script checks that the
// frame is shown the way the extension drew it: not hidden or faded by any
// element around it, not filtered, clipped or blended, not covered by
// another element, and no popover, modal dialog or full screen element is
// open above it. The frame page makes a second check of its own, with the
// browser's visibility tracking (see hostedFrame.ts and suggestion.ts).

export interface FrameCheck {
  isShown: boolean
  // failureReason says what failed, for a test and for the console; the
  // person is told nothing, since the frame is simply closed.
  failureReason: string
}

const MINIMUM_SIDE_PIXELS = 20
// Points are tried inset from the corners, since a corner is rounded or has a
// shadow, and the middle.
const INSET_PIXELS = 6

function parentOf(element: Element): Element | null {
  if (element.assignedSlot) return element.assignedSlot
  if (element.parentElement) return element.parentElement
  const root = element.getRootNode()
  return root instanceof ShadowRoot ? root.host : null
}

function shownByStyle(element: Element, view: Window): string {
  const style = view.getComputedStyle(element)
  if (style.display === 'none') return 'an element around the frame is not displayed'
  if (style.visibility !== 'visible') return 'an element around the frame is not visible'
  if (style.contentVisibility === 'hidden') return 'an element around the frame hides its content'
  if (Number.parseFloat(style.opacity === '' ? '1' : style.opacity) < 0.99)
    return 'an element around the frame is see-through'
  if (style.filter !== 'none' && style.filter !== '') return 'an element around the frame has a filter'
  if (style.clipPath !== 'none' && style.clipPath !== '') return 'an element around the frame is clipped'
  if (style.maskImage && style.maskImage !== 'none') return 'an element around the frame is masked'
  if (style.mixBlendMode && style.mixBlendMode !== 'normal') return 'an element around the frame is blended'
  return ''
}

// openTopLayerElement reports an open popover, modal dialog or full screen
// element other than the frame's own. Each lookup is guarded: a selector the browser does not know
// throws, and then there is no such thing to be open.
function openTopLayerElement(document: Document, host: HTMLElement): string {
  // The frame's own element may itself be a popover; any other is another
  // thing in the top layer, which can sit over it.
  const selectors: [string, string][] = [
    [':popover-open', 'a popover is open'],
    ['dialog:modal', 'a modal dialog is open'],
  ]
  for (const [selector, openReason] of selectors) {
    try {
      if (Array.from(document.querySelectorAll(selector)).some((element) => element !== host)) return openReason
    } catch {
      // This browser does not know the selector.
    }
  }
  if (document.fullscreenElement) return 'an element is full screen'
  return ''
}

// checkFrameShown looks at the element that holds the extension's frame (the
// shadow host the content script made) and at everything around it.
export function checkFrameShown(host: HTMLElement): FrameCheck {
  const document = host.ownerDocument
  const view = document.defaultView
  if (!view || !host.isConnected) return { isShown: false, failureReason: 'the frame is not on the page' }
  for (let element: Element | null = host; element; element = parentOf(element)) {
    const styleReason = shownByStyle(element, view)
    if (styleReason !== '') return { isShown: false, failureReason: styleReason }
  }
  const rect = host.getBoundingClientRect()
  if (rect.width < MINIMUM_SIDE_PIXELS || rect.height < MINIMUM_SIDE_PIXELS) {
    return { isShown: false, failureReason: 'the frame is too small to read' }
  }
  const topLayerReason = openTopLayerElement(document, host)
  if (topLayerReason !== '') return { isShown: false, failureReason: topLayerReason }

  const viewWidth = view.innerWidth
  const viewHeight = view.innerHeight
  const points = [
    [rect.left + rect.width / 2, rect.top + rect.height / 2],
    [rect.left + INSET_PIXELS, rect.top + INSET_PIXELS],
    [rect.right - INSET_PIXELS, rect.top + INSET_PIXELS],
    [rect.left + INSET_PIXELS, rect.bottom - INSET_PIXELS],
    [rect.right - INSET_PIXELS, rect.bottom - INSET_PIXELS],
  ].filter(([x, y]) => x >= 0 && y >= 0 && x < viewWidth && y < viewHeight)
  // A frame with none of its corners or its middle on the screen is not one
  // anybody is looking at.
  if (points.length === 0) return { isShown: false, failureReason: 'the frame is off the screen' }
  for (const [horizontalPixels, verticalPixels] of points) {
    const stack = document.elementsFromPoint(horizontalPixels, verticalPixels)
    if (stack[0] !== host) return { isShown: false, failureReason: 'another element covers the frame' }
  }
  return { isShown: true, failureReason: '' }
}
