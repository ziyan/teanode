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
    const box = element.getBoundingClientRect()
    // Above by default, because the thing being described is usually at the
    // end of a row and the pointer is coming from the left.
    const below = box.top < 40
    setPosition({
      top: below ? box.bottom + MARGIN : box.top - MARGIN,
      left: box.left + box.width / 2,
      below,
    })
  }, [])

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
        onMouseEnter={show}
        onMouseLeave={hide}
        onFocus={place}
        onBlur={hide}
      >
        {children}
      </span>
      {position &&
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
