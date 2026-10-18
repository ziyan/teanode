import { useCallback, useEffect, useRef, useState } from 'react'

// Dragging a photo down to close the viewer, on a touch screen: the photo
// follows the finger, shrinks a little, and the black behind it fades; let go
// far enough down, or fast enough, and the viewer closes, otherwise the photo
// springs back. A mouse does not do this (it has the Close button and Escape),
// and a photo that is zoomed in does not either: there a drag pans.

// How far a finger has to travel before it is known which way it is going.
export const DRAG_DECISION_PIXELS = 10
// How far down a drag must end to close the viewer on distance alone.
export const CLOSE_DISTANCE_PIXELS = 120
// A quick flick closes it from a shorter way, at least this far...
export const FLICK_MINIMUM_PIXELS = 40
// ...when the finger was moving down at least this fast as it let go.
export const FLICK_SPEED_PIXELS_PER_MS = 0.6
// How long the photo takes to leave, or to spring back.
export const DRAG_SETTLE_MS = 180
// A finger that has been still this long when it lets go was not flicking.
const FLICK_STALE_MS = 100

export function shouldCloseAfterDrag(distancePixels: number, speedPixelsPerMS: number): boolean {
  return (
    distancePixels >= CLOSE_DISTANCE_PIXELS ||
    (distancePixels >= FLICK_MINIMUM_PIXELS && speedPixelsPerMS >= FLICK_SPEED_PIXELS_PER_MS)
  )
}

// Which way a drag that has gone far enough is going: down closes, anything
// else (sideways, which turns the page, or up) is left to other handlers.
export function dragDirection(acrossPixels: number, downPixels: number): 'undecided' | 'down' | 'other' {
  if (Math.hypot(acrossPixels, downPixels) < DRAG_DECISION_PIXELS) return 'undecided'
  return downPixels > 0 && downPixels > Math.abs(acrossPixels) * 1.2 ? 'down' : 'other'
}

// How far along a close the drag is, from 0 (not moved) to 1 (a good way
// down the screen): what the photo's size and the fade follow.
export function dragProgress(downPixels: number, heightPixels: number): number {
  if (heightPixels <= 0) return 0
  return Math.min(1, Math.max(0, downPixels) / (heightPixels * 0.4))
}

export interface DragPosition {
  acrossPixels: number
  downPixels: number
  // How the drag has settled: still being made, going back, or leaving.
  phase: 'dragging' | 'returning' | 'leaving'
}

interface DragStart {
  pointerId: number
  x: number
  y: number
  lastY: number
  lastTime: number
  speedPixelsPerMS: number
  direction: 'undecided' | 'down' | 'other'
}

function prefersReducedMotion(): boolean {
  try {
    return window.matchMedia('(prefers-reduced-motion: reduce)').matches
  } catch {
    return false
  }
}

// useDragToClose gives the handlers to put on the element the finger drags
// on, and where the photo is while it is dragged (null when it is at rest).
// isEnabled is false while the photo is zoomed, or is not a picture.
export function useDragToClose({ isEnabled, onClose }: { isEnabled: boolean; onClose: () => void }) {
  const [position, setPosition] = useState<DragPosition | null>(null)
  const drag = useRef<DragStart | null>(null)
  const timer = useRef<number | null>(null)
  const closeRef = useRef(onClose)
  closeRef.current = onClose

  useEffect(
    () => () => {
      if (timer.current !== null) window.clearTimeout(timer.current)
    },
    [],
  )
  // Zooming in or turning to a video in the middle of a drag drops it.
  useEffect(() => {
    if (!isEnabled) {
      drag.current = null
      setPosition(null)
    }
  }, [isEnabled])

  const onPointerDown = useCallback(
    (event: React.PointerEvent<HTMLElement>) => {
      if (!isEnabled || (event.pointerType !== 'touch' && event.pointerType !== 'pen')) return
      // A second finger is a pinch or a mistake, not a drag; a control or the
      // video's own bar takes its own touches.
      if (drag.current || !event.isPrimary) return
      if (event.target instanceof Element && event.target.closest('button, a, video, input')) return
      if (timer.current !== null) {
        window.clearTimeout(timer.current)
        timer.current = null
      }
      drag.current = {
        pointerId: event.pointerId,
        x: event.clientX,
        y: event.clientY,
        lastY: event.clientY,
        lastTime: event.timeStamp,
        speedPixelsPerMS: 0,
        direction: 'undecided',
      }
    },
    [isEnabled],
  )

  const onPointerMove = useCallback((event: React.PointerEvent<HTMLElement>) => {
    const current = drag.current
    if (!current || current.pointerId !== event.pointerId) return
    const across = event.clientX - current.x
    const down = event.clientY - current.y
    if (current.direction === 'undecided') {
      current.direction = dragDirection(across, down)
      if (current.direction === 'down') {
        try {
          event.currentTarget.setPointerCapture?.(event.pointerId)
        } catch {
          // A pointer that has already gone: the drag ends with its own up.
        }
      }
    }
    if (current.direction !== 'down') return
    const elapsed = event.timeStamp - current.lastTime
    if (elapsed > 0) current.speedPixelsPerMS = (event.clientY - current.lastY) / elapsed
    current.lastY = event.clientY
    current.lastTime = event.timeStamp
    setPosition({ acrossPixels: across, downPixels: Math.max(0, down), phase: 'dragging' })
  }, [])

  const finish = useCallback((event: React.PointerEvent<HTMLElement>, isCancelled: boolean) => {
    const current = drag.current
    if (!current || current.pointerId !== event.pointerId) return
    drag.current = null
    if (current.direction !== 'down') return
    const down = Math.max(0, event.clientY - current.y)
    const speed = event.timeStamp - current.lastTime > FLICK_STALE_MS ? 0 : current.speedPixelsPerMS
    const across = event.clientX - current.x
    if (!isCancelled && shouldCloseAfterDrag(down, speed)) {
      if (prefersReducedMotion()) {
        setPosition(null)
        closeRef.current()
        return
      }
      setPosition({ acrossPixels: across, downPixels: window.innerHeight, phase: 'leaving' })
      timer.current = window.setTimeout(() => {
        timer.current = null
        closeRef.current()
      }, DRAG_SETTLE_MS)
      return
    }
    setPosition({ acrossPixels: 0, downPixels: 0, phase: 'returning' })
    timer.current = window.setTimeout(
      () => {
        timer.current = null
        setPosition(null)
      },
      prefersReducedMotion() ? 0 : DRAG_SETTLE_MS,
    )
  }, [])

  return {
    position,
    handlers: {
      onPointerDown,
      onPointerMove,
      onPointerUp: (event: React.PointerEvent<HTMLElement>) => finish(event, false),
      onPointerCancel: (event: React.PointerEvent<HTMLElement>) => finish(event, true),
    },
  }
}
