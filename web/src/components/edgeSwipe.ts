import { useEffect, useRef } from 'react'

// How near the left edge a swipe that opens the side menu starts, and how far
// it travels before it counts. Mostly sideways: a swipe that moves more down
// than across is the page scrolling.
const EDGE_PX = 24
const TRAVEL_PX = 56

// useEdgeSwipe opens the side menu on a phone when a finger drags in from
// the left edge, as the iPhone app's menu opens, and closes it when one drags
// left while it is open. A browser that takes the edge for going back (Safari
// with a page to go back to) keeps the gesture, so the menu button still
// matters.
export function useEdgeSwipe(isEnabled: boolean, isOpen: boolean, onOpen: () => void, onClose: () => void) {
  const handlers = useRef({ onOpen, onClose, isOpen })
  handlers.current = { onOpen, onClose, isOpen }
  useEffect(() => {
    if (!isEnabled) return
    let start: { x: number; y: number; isFromEdge: boolean } | null = null
    const onStart = (event: TouchEvent) => {
      if (event.touches.length !== 1) {
        start = null
        return
      }
      const touch = event.touches[0]
      start = { x: touch.clientX, y: touch.clientY, isFromEdge: touch.clientX <= EDGE_PX }
    }
    const onMove = (event: TouchEvent) => {
      if (!start) return
      const touch = event.touches[0]
      const across = touch.clientX - start.x
      const down = Math.abs(touch.clientY - start.y)
      if (down > Math.abs(across)) {
        start = null
        return
      }
      if (!handlers.current.isOpen && start.isFromEdge && across >= TRAVEL_PX) {
        start = null
        handlers.current.onOpen()
      } else if (handlers.current.isOpen && across <= -TRAVEL_PX) {
        start = null
        handlers.current.onClose()
      }
    }
    const onEnd = () => {
      start = null
    }
    document.addEventListener('touchstart', onStart, { passive: true })
    document.addEventListener('touchmove', onMove, { passive: true })
    document.addEventListener('touchend', onEnd, { passive: true })
    document.addEventListener('touchcancel', onEnd, { passive: true })
    return () => {
      document.removeEventListener('touchstart', onStart)
      document.removeEventListener('touchmove', onMove)
      document.removeEventListener('touchend', onEnd)
      document.removeEventListener('touchcancel', onEnd)
    }
  }, [isEnabled])
}
