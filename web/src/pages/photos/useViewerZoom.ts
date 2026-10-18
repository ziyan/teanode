import { useCallback, useEffect, useRef, useState } from 'react'
import {
  DOUBLE_TAP_SCALE,
  FITTED_VIEW,
  clampView,
  distanceBetween,
  fittedSize,
  midpointOf,
  panBy,
  pinchView,
  scaleAfterWheel,
  zoomAround,
  type PinchStart,
  type Point,
  type Size,
  type ZoomView,
} from './zoom'

// Zooming the photo in the viewer by hand, around where the pointer or the
// fingers are: the wheel (and a trackpad pinch, which a browser sends as a wheel
// with ctrlKey set), two fingers pinching, a double click or double tap, and
// dragging a zoomed photo to move it. The arithmetic is in zoom.ts; this is the
// events, and what to remember between them.
//
// The photo is drawn by the caller as the view says (viewTransform in zoom.ts)
// on an element that fills the stage, and the handlers go on the stage.

// How far a finger or the mouse may move and still be a tap or a click.
const TAP_SLOP_PIXELS = 10
// How long a tap may last, and how long after one the next is a double tap.
const TAP_MAXIMUM_MS = 300
const DOUBLE_TAP_MS = 300
// How near the second tap must be to the first.
const DOUBLE_TAP_DISTANCE_PIXELS = 40
// The double click a browser makes out of a double tap, which the tap has
// already answered.
const SYNTHESIZED_CLICK_MS = 700

function prefersReducedMotion(): boolean {
  try {
    return window.matchMedia('(prefers-reduced-motion: reduce)').matches
  } catch {
    return false
  }
}

// What a press on these is for: a button, a link or a video's own controls
// take their own touches and clicks.
const OWN_CONTROLS = 'button, a, video, input'

type Gesture =
  | { kind: 'pan'; pointerId: number; startPoint: Point; startView: ZoomView }
  | { kind: 'pinch'; pointerIds: [number, number]; start: PinchStart }

interface Tap {
  pointerId: number
  point: Point
  startedAt: number
}

export function useViewerZoom({
  isEnabled,
  resetKey,
  aspectRatio,
}: {
  // False for a video, or a photo with no picture yet: nothing zooms.
  isEnabled: boolean
  // A different photo starts fitted again.
  resetKey: string
  // Width over height of the picture as it is drawn, 0 when it is not known.
  aspectRatio: number
}) {
  const stage = useRef<HTMLDivElement>(null)
  const [view, setViewState] = useState<ZoomView>(FITTED_VIEW)
  const viewRef = useRef<ZoomView>(FITTED_VIEW)
  const [isEasing, setIsEasing] = useState(false)
  const [isPanning, setIsPanning] = useState(false)
  const [isPinching, setIsPinching] = useState(false)
  const pointers = useRef(new Map<number, Point>())
  const gesture = useRef<Gesture | null>(null)
  const tap = useRef<Tap | null>(null)
  const lastTap = useRef<{ point: Point; endedAt: number } | null>(null)
  const lastTouchToggleAt = useRef(0)
  const aspectRatioRef = useRef(aspectRatio)
  aspectRatioRef.current = aspectRatio

  const setView = useCallback((next: ZoomView, isEased: boolean) => {
    viewRef.current = next
    setViewState(next)
    setIsEasing(isEased && !prefersReducedMotion())
  }, [])

  // Where the stage is, how big the fitted picture is in it, and a point of the
  // window as measured from the stage's center.
  const measure = useCallback(() => {
    const element = stage.current
    const rectangle = element?.getBoundingClientRect()
    const box: Size = { width: rectangle?.width ?? 0, height: rectangle?.height ?? 0 }
    const center: Point = { x: (rectangle?.left ?? 0) + box.width / 2, y: (rectangle?.top ?? 0) + box.height / 2 }
    return {
      box,
      picture: fittedSize(box, aspectRatioRef.current),
      fromCenter: (point: Point): Point => ({ x: point.x - center.x, y: point.y - center.y }),
    }
  }, [])

  const reset = useCallback(() => {
    pointers.current.clear()
    gesture.current = null
    tap.current = null
    lastTap.current = null
    setIsPanning(false)
    setIsPinching(false)
    setView(FITTED_VIEW, false)
  }, [setView])

  // Each photo starts fitted, and so does one that stops being a picture.
  useEffect(() => {
    reset()
  }, [resetKey, isEnabled, reset])

  // Turns fitted into zoomed in at the anchor, or zoomed back out to fitted.
  const toggle = useCallback(
    (anchorInWindow: Point | null) => {
      if (viewRef.current.scale > 1) {
        setView(FITTED_VIEW, true)
        return
      }
      const { box, picture, fromCenter } = measure()
      const anchor = anchorInWindow ? fromCenter(anchorInWindow) : { x: 0, y: 0 }
      setView(zoomAround(viewRef.current, DOUBLE_TAP_SCALE, anchor, box, picture), true)
    },
    [measure, setView],
  )

  // The wheel is listened to by hand: React's own wheel listener cannot say
  // that the page is not to scroll, or the browser not to zoom the whole page
  // for a pinch.
  useEffect(() => {
    const element = stage.current
    if (!element || !isEnabled) return
    const onWheel = (event: WheelEvent) => {
      event.preventDefault()
      const current = viewRef.current
      const nextScale = scaleAfterWheel(current.scale, event.deltaY, event.deltaMode, event.ctrlKey)
      if (nextScale === current.scale) return
      const { box, picture, fromCenter } = measure()
      setView(zoomAround(current, nextScale, fromCenter({ x: event.clientX, y: event.clientY }), box, picture), false)
    }
    element.addEventListener('wheel', onWheel, { passive: false })
    return () => element.removeEventListener('wheel', onWheel)
  }, [isEnabled, measure, setView])

  const startPan = (pointerId: number, point: Point) => {
    gesture.current = { kind: 'pan', pointerId, startPoint: point, startView: viewRef.current }
    setIsPanning(true)
  }

  const onPointerDown = (event: React.PointerEvent<HTMLElement>) => {
    if (!isEnabled) return
    if (event.pointerType === 'mouse' && event.button !== 0) return
    if (event.target instanceof Element && event.target.closest(OWN_CONTROLS)) return
    const point: Point = { x: event.clientX, y: event.clientY }
    pointers.current.set(event.pointerId, point)
    if (pointers.current.size === 1) {
      // A gesture left over from a press whose end was never heard is over.
      gesture.current = null
      setIsPanning(false)
      setIsPinching(false)
      tap.current = { pointerId: event.pointerId, point, startedAt: event.timeStamp }
      if (viewRef.current.scale > 1) {
        startPan(event.pointerId, point)
        try {
          event.currentTarget.setPointerCapture?.(event.pointerId)
        } catch {
          // A pointer that has already gone: the drag ends with its own up.
        }
      }
    } else if (pointers.current.size === 2) {
      // Two fingers: a pinch, whatever one of them was doing, and not a tap.
      tap.current = null
      lastTap.current = null
      const [firstId, secondId] = Array.from(pointers.current.keys())
      const { fromCenter } = measure()
      const first = fromCenter(pointers.current.get(firstId)!)
      const second = fromCenter(pointers.current.get(secondId)!)
      gesture.current = {
        kind: 'pinch',
        pointerIds: [firstId, secondId],
        start: {
          distancePixels: distanceBetween(first, second),
          midpoint: midpointOf(first, second),
          view: viewRef.current,
        },
      }
      setIsPanning(false)
      setIsPinching(true)
    }
  }

  const onPointerMove = (event: React.PointerEvent<HTMLElement>) => {
    if (!pointers.current.has(event.pointerId)) return
    const point: Point = { x: event.clientX, y: event.clientY }
    pointers.current.set(event.pointerId, point)
    if (tap.current?.pointerId === event.pointerId && distanceBetween(tap.current.point, point) > TAP_SLOP_PIXELS) {
      tap.current = null
    }
    const current = gesture.current
    if (!current) return
    const { box, picture, fromCenter } = measure()
    if (current.kind === 'pinch') {
      const first = pointers.current.get(current.pointerIds[0])
      const second = pointers.current.get(current.pointerIds[1])
      if (!first || !second) return
      setView(pinchView(current.start, fromCenter(first), fromCenter(second), box, picture), false)
    } else if (current.pointerId === event.pointerId) {
      setView(
        panBy(current.startView, point.x - current.startPoint.x, point.y - current.startPoint.y, box, picture),
        false,
      )
    }
  }

  const onPointerEnd = (event: React.PointerEvent<HTMLElement>, isCancelled: boolean) => {
    if (!pointers.current.has(event.pointerId)) return
    pointers.current.delete(event.pointerId)
    const current = gesture.current
    if (current?.kind === 'pinch' && current.pointerIds.includes(event.pointerId)) {
      gesture.current = null
      setIsPinching(false)
      // Pinched back out to the fitted size: fitted it stays. A finger still
      // down carries on as a pan, from where it is now.
      const { box, picture } = measure()
      const settled = clampView(viewRef.current, box, picture)
      setView(settled, false)
      const remaining = Array.from(pointers.current.entries())[0]
      if (remaining && settled.scale > 1) startPan(remaining[0], remaining[1])
      return
    }
    if (current?.kind === 'pan' && current.pointerId === event.pointerId) {
      gesture.current = null
      setIsPanning(false)
    }
    // A double tap, for a finger or a pen: two short taps close together.
    const started = tap.current
    tap.current = null
    if (isCancelled || event.pointerType === 'mouse' || !started || started.pointerId !== event.pointerId) return
    const point: Point = { x: event.clientX, y: event.clientY }
    if (
      event.timeStamp - started.startedAt > TAP_MAXIMUM_MS ||
      distanceBetween(started.point, point) > TAP_SLOP_PIXELS
    ) {
      return
    }
    const previous = lastTap.current
    if (
      previous &&
      event.timeStamp - previous.endedAt <= DOUBLE_TAP_MS &&
      distanceBetween(previous.point, point) <= DOUBLE_TAP_DISTANCE_PIXELS
    ) {
      lastTap.current = null
      lastTouchToggleAt.current = Date.now()
      toggle(point)
    } else {
      lastTap.current = { point, endedAt: event.timeStamp }
    }
  }

  // The double click of a mouse. A touch screen's double tap is already
  // answered above, and the click a browser makes of it is ignored.
  const onDoubleClick = (event: React.MouseEvent<HTMLElement>) => {
    if (!isEnabled || Date.now() - lastTouchToggleAt.current < SYNTHESIZED_CLICK_MS) return
    toggle({ x: event.clientX, y: event.clientY })
  }

  return {
    stage,
    view,
    isZoomed: view.scale > 1,
    isEasing,
    isPanning,
    isPinching,
    // The zoom button: in at the center of the stage, or back out.
    toggleAtCenter: () => toggle(null),
    handlers: {
      onPointerDown,
      onPointerMove,
      onPointerUp: (event: React.PointerEvent<HTMLElement>) => onPointerEnd(event, false),
      onPointerCancel: (event: React.PointerEvent<HTMLElement>) => onPointerEnd(event, true),
      // A pointer taken away from the stage (a window that lost it, a menu
      // opened over it) has ended too.
      onLostPointerCapture: (event: React.PointerEvent<HTMLElement>) => onPointerEnd(event, true),
    },
    onDoubleClick,
  }
}
