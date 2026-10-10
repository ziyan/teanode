import { useRef, useState, type ReactNode } from 'react'
import { ReplyIcon } from './icons'

// How far a message is swiped to the left before letting go replies to
// it, and the farthest it follows the finger.
export const SWIPE_REPLY_PX = 56
const SWIPE_FARTHEST_PX = 80

// swipeOffset is how far a line is drawn to the left for a finger that has
// moved acrossPx to the right and downPx down since it touched: nothing for
// a move to the right, or one more down than across, which is a scroll.
export function swipeOffset(acrossPx: number, downPx: number): number {
  if (acrossPx >= 0 || Math.abs(downPx) > Math.abs(acrossPx)) return 0
  return Math.max(acrossPx, -SWIPE_FARTHEST_PX)
}

// SwipeToReply is a line of the conversation that can be answered: on a
// touch screen it follows a finger swiped to the left, and letting go far
// enough replies to it, as chat apps do; with a mouse a Reply button shows
// in its corner on hover, and it is reached by keyboard too. The browser
// keeps the vertical scroll (touch-action: pan-y), and a scroll it starts
// cancels the swipe.
export function SwipeToReply({
  className,
  replyLabel,
  onReply,
  children,
}: {
  className: string
  replyLabel: string
  onReply: () => void
  children: ReactNode
}) {
  const touched = useRef<{ pointerId: number; x: number; y: number } | null>(null)
  const offsetRef = useRef(0)
  const [offset, setOffset] = useState(0)
  const move = (next: number) => {
    // A small buzz where letting go would reply, on a phone that has one.
    if (offsetRef.current > -SWIPE_REPLY_PX && next <= -SWIPE_REPLY_PX) navigator.vibrate?.(8)
    offsetRef.current = next
    setOffset(next)
  }
  const end = (isReplying: boolean) => {
    const isFarEnough = offsetRef.current <= -SWIPE_REPLY_PX
    touched.current = null
    move(0)
    if (isReplying && isFarEnough) onReply()
  }
  return (
    <div
      className={[className, 'agent-line-replyable', offset < 0 ? 'swiping' : ''].filter(Boolean).join(' ')}
      style={offset < 0 ? { transform: `translateX(${offset}px)` } : undefined}
      onPointerDown={(event) => {
        if (event.pointerType !== 'touch') return
        touched.current = { pointerId: event.pointerId, x: event.clientX, y: event.clientY }
      }}
      onPointerMove={(event) => {
        const from = touched.current
        if (!from || from.pointerId !== event.pointerId) return
        move(swipeOffset(event.clientX - from.x, event.clientY - from.y))
      }}
      onPointerUp={(event) => {
        if (touched.current?.pointerId === event.pointerId) end(true)
      }}
      onPointerCancel={() => end(false)}
    >
      {children}
      {offset < 0 && (
        <span
          className="agent-swipe-reply"
          aria-hidden="true"
          style={{ opacity: Math.min(1, -offset / SWIPE_REPLY_PX) }}
        >
          <ReplyIcon size={16} />
        </span>
      )}
      <button
        type="button"
        className="agent-line-reply icon-action"
        title={replyLabel}
        aria-label={replyLabel}
        onClick={onReply}
      >
        <ReplyIcon size={14} />
      </button>
    </div>
  )
}
