import { useCallback, useEffect, useId, useRef, useState } from 'react'
import { createPortal } from 'react-dom'

// A tooltip the dashboard draws itself, rather than the browser's.
//
// The native `title` attribute is what every one of these used to be, and it
// has three problems that matter here: it waits about a second before it
// appears, it is drawn in the operating system's colors rather than the
// page's, and its text cannot wrap or be styled — a timestamp with a zone
// name in it comes out as one long line in a font nobody chose.
//
// It is deliberately small. There is no arrow, no rich content and no click
// behavior: it says one line about the thing under the pointer.

// How long the pointer has to rest before it appears. Long enough that
// running the pointer down a list of fifty rows shows nothing, short enough
// that stopping on one feels answered rather than delayed.
const DELAY = 350

// Room kept between the tooltip and the edge of the window, and between it
// and the thing it describes.
const MARGIN = 8

type Position = { top: number; left: number; below: boolean }

// Where a run of text is, for an anchor that wraps words rather than a
// button.
function rangeOver(element: Element): Range {
  const range = document.createRange()
  range.selectNodeContents(element)
  return range
}

export function Tooltip({ label, children }: { label: string; children: React.ReactNode }) {
  const anchor = useRef<HTMLSpanElement>(null)
  const timer = useRef<number | null>(null)
  const [position, setPosition] = useState<Position | null>(null)
  const id = useId()

  const hide = useCallback(() => {
    if (timer.current !== null) {
      window.clearTimeout(timer.current)
      timer.current = null
    }
    setPosition(null)
  }, [])

  // Measured from the anchor rather than positioned relative to it, and
  // rendered into the body: a tooltip inside the mailbox list would be
  // clipped by the scrolling box that holds the rows.
  const place = useCallback(() => {
    const element = anchor.current
    if (!element) {
      return
    }
    // What is being described, which is not the anchor. The anchor is
    // display: contents so that it does not become a flex item of the row it
    // sits in — and an element with no box of its own measures as zeros, so
    // measuring it put every tooltip in the top left corner of the window,
    // half of it off the edge. A range covers the case where what is wrapped
    // is text rather than an element.
    const box = element.firstElementChild
      ? element.firstElementChild.getBoundingClientRect()
      : rangeOver(element).getBoundingClientRect()
    // Above by default, because the thing being described is usually at the
    // end of a row and the pointer is coming from the left.
    const below = box.top < 40
    setPosition({
      top: below ? box.bottom + MARGIN : box.top - MARGIN,
      left: box.left + box.width / 2,
      below,
    })
  }, [])

  // Nothing to say, nothing to show. A caller that has a label only
  // sometimes — a tab that carries an explanation, a control that says why it
  // is disabled — passes the empty string the rest of the time, and an empty
  // grey box on hover is worse than no tooltip.
  const silent = label.trim() === ''

  const show = useCallback(() => {
    if (timer.current !== null) {
      window.clearTimeout(timer.current)
    }
    timer.current = window.setTimeout(() => {
      timer.current = null
      place()
    }, DELAY)
  }, [place])

  useEffect(() => hide, [hide])

  // A page that scrolls or resizes under an open tooltip leaves it pointing
  // at nothing, and there is no cheap way to keep it attached, so it goes.
  useEffect(() => {
    if (!position) {
      return
    }
    window.addEventListener('scroll', hide, true)
    window.addEventListener('resize', hide)
    return () => {
      window.removeEventListener('scroll', hide, true)
      window.removeEventListener('resize', hide)
    }
  }, [position, hide])

  return (
    <>
      <span
        ref={anchor}
        className="tooltip-anchor"
        // Described rather than labelled: the tooltip adds to what the
        // element says, it does not replace it.
        aria-describedby={position ? id : undefined}
        onMouseEnter={silent ? undefined : show}
        onMouseLeave={hide}
        onFocus={silent ? undefined : place}
        onBlur={hide}
      >
        {children}
      </span>
      {!silent &&
        position &&
        createPortal(
          <span
            id={id}
            role="tooltip"
            className={position.below ? 'tooltip below' : 'tooltip'}
            style={{ top: position.top, left: position.left }}
          >
            {label}
          </span>,
          document.body,
        )}
    </>
  )
}
