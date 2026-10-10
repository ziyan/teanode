import { useRef, useState, type ReactNode } from 'react'
import { CopyIcon, ReplyIcon } from './icons'

// How far a message is swiped before letting go acts on it, and the
// farthest it follows the finger.
export const SWIPE_ACTION_PX = 56
const SWIPE_FARTHEST_PX = 80

// swipeOffset is how far a line is drawn across for a finger that has
// moved acrossPx to the right and downPx down since it touched: negative
// to the left, positive to the right, and nothing for a move more down
// than across, which is a scroll.
export function swipeOffset(acrossPx: number, downPx: number): number {
  if (Math.abs(downPx) > Math.abs(acrossPx)) return 0
  return Math.max(-SWIPE_FARTHEST_PX, Math.min(SWIPE_FARTHEST_PX, acrossPx))
}

// SwipeableLine is a line of the conversation that can be answered or
// copied. On a touch screen it follows a finger: swiped to the left and let
// go far enough it replies, to the right it copies, as chat apps do. With a
// mouse, Copy and Reply buttons show in its corner on hover, and they are
// reached by keyboard too. The browser keeps the vertical scroll
// (touch-action: pan-y), and a scroll it starts cancels the swipe.
export function SwipeableLine({
  className,
  replyLabel,
  copyLabel,
  onReply,
  onCopy,
  children,
}: {
  className: string
  replyLabel: string
  copyLabel: string
  onReply: () => void
  onCopy: () => void
  children: ReactNode
}) {
  const touched = useRef<{ pointerId: number; x: number; y: number } | null>(null)
  const offsetRef = useRef(0)
  const [offset, setOffset] = useState(0)
  const move = (next: number) => {
    // A small buzz where letting go would act, on a phone that has one.
    if (Math.abs(offsetRef.current) < SWIPE_ACTION_PX && Math.abs(next) >= SWIPE_ACTION_PX) navigator.vibrate?.(8)
    offsetRef.current = next
    setOffset(next)
  }
  const end = (isActing: boolean) => {
    const reached = offsetRef.current
    touched.current = null
    move(0)
    if (!isActing) return
    if (reached <= -SWIPE_ACTION_PX) onReply()
    else if (reached >= SWIPE_ACTION_PX) onCopy()
  }
  return (
    <div
      className={[className, 'agent-line-swipeable', offset !== 0 ? 'swiping' : ''].filter(Boolean).join(' ')}
      style={offset !== 0 ? { transform: `translateX(${offset}px)` } : undefined}
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
          className="agent-swipe-action reply"
          aria-hidden="true"
          style={{ opacity: Math.min(1, -offset / SWIPE_ACTION_PX) }}
        >
          <ReplyIcon size={16} />
        </span>
      )}
      {offset > 0 && (
        <span
          className="agent-swipe-action copy"
          aria-hidden="true"
          style={{ opacity: Math.min(1, offset / SWIPE_ACTION_PX) }}
        >
          <CopyIcon size={16} />
        </span>
      )}
      <span className="agent-line-actions">
        <button type="button" className="icon-action" title={copyLabel} aria-label={copyLabel} onClick={onCopy}>
          <CopyIcon size={14} />
        </button>
        <button type="button" className="icon-action" title={replyLabel} aria-label={replyLabel} onClick={onReply}>
          <ReplyIcon size={14} />
        </button>
      </span>
    </div>
  )
}
