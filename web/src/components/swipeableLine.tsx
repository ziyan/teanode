import { useEffect, useRef, useState, type ReactNode } from 'react'
import { CheckIcon, CopyIcon, ReplyIcon } from './icons'

// How far a finger has to move sideways before it is a swipe rather than a
// tap or the start of a scroll (as a mailbox row's), how far a message is
// carried before letting go acts on it, and the farthest it goes.
export const SWIPE_STARTS_PX = 12
export const SWIPE_ACTION_PX = 56
const SWIPE_FARTHEST_PX = 96

// How long a copied line stays out, its mark a tick, before it goes back;
// and how long one whose copy the browser refused waits, its mark a button,
// for the tap that copies it.
export const COPIED_HOLD_MS = 900
export const COPY_TAP_HOLD_MS = 4000

// swipeOffset is how far a line is drawn across for a finger that has moved
// acrossPx since the swipe began: one for one up to where letting go acts,
// then heavier, so the line says it has gone far enough.
export function swipeOffset(acrossPx: number): number {
  const distance = Math.abs(acrossPx)
  const carried = distance <= SWIPE_ACTION_PX ? distance : SWIPE_ACTION_PX + (distance - SWIPE_ACTION_PX) * 0.35
  return Math.sign(acrossPx) * Math.min(carried, SWIPE_FARTHEST_PX)
}

export function SwipeableLine({
  className,
  replyLabel,
  copyLabel,
  onReply,
  onCopy,
  onCopyFailed,
  children,
}: {
  className: string
  replyLabel: string
  copyLabel: string
  onReply: () => void
  // onCopy copies the line and says whether it took; onCopyFailed is told
  // when a tap or a click could not copy it either.
  onCopy: () => Promise<boolean>
  onCopyFailed: () => void
  children: ReactNode
}) {
  // Where the finger came down, and whether it has become a swipe: until
  // it moves SWIPE_STARTS_PX across it may still be a tap, and a move more
  // down than across first is a scroll, left to the browser. Once a swipe,
  // the direction is held: drifting down does not drop the line.
  const touched = useRef<{ pointerId: number; x: number; y: number; isSwiping: boolean } | null>(null)
  const hasSwiped = useRef(false)
  const offsetRef = useRef(0)
  const [offset, setOffset] = useState(0)
  // A line just copied: held out where the swipe left it, its mark a tick,
  // until COPIED_HOLD_MS have passed. The button says so too.
  const [isCopied, setCopied] = useState(false)
  // A browser writes to the clipboard only straight after what it counts as
  // a deliberate tap or click, and a swipe does not always count: Safari on
  // a phone never takes one, Chrome not every time. A swipe whose copy was
  // refused leaves the line out with its mark a button, which a tap does
  // count for; only when that fails too is the person told.
  const [isCopyTapWaiting, setCopyTapWaiting] = useState(false)
  const holding = useRef<number | undefined>(undefined)
  useEffect(() => () => window.clearTimeout(holding.current), [])
  const holdThen = (holdMS: number, then: () => void) => {
    window.clearTimeout(holding.current)
    holding.current = window.setTimeout(then, holdMS)
  }
  const copy = (isSwiped: boolean, isHeld: boolean) => {
    void onCopy().then((isTaken) => {
      if (isTaken) {
        setCopyTapWaiting(false)
        setCopied(true)
        holdThen(COPIED_HOLD_MS, () => {
          setCopied(false)
          if (isHeld) move(0)
        })
        return
      }
      if (isSwiped) {
        setCopyTapWaiting(true)
        holdThen(COPY_TAP_HOLD_MS, () => {
          setCopyTapWaiting(false)
          move(0)
        })
        return
      }
      setCopyTapWaiting(false)
      if (isHeld) move(0)
      onCopyFailed()
    })
  }
  const move = (next: number) => {
    // A small buzz where letting go would act, on a phone that has one.
    if (Math.abs(offsetRef.current) < SWIPE_ACTION_PX && Math.abs(next) >= SWIPE_ACTION_PX) navigator.vibrate?.(8)
    offsetRef.current = next
    setOffset(next)
  }
  const end = (isActing: boolean) => {
    const wasSwiping = touched.current?.isSwiping ?? false
    const reached = offsetRef.current
    touched.current = null
    // A tap moves nothing, and leaves a line held out where it is: the tap
    // may be on its copy button.
    if (!wasSwiping) return
    if (isActing && reached >= SWIPE_ACTION_PX) {
      // Settles where letting go acts, and stays there while it copies.
      hasSwiped.current = true
      window.clearTimeout(holding.current)
      move(SWIPE_ACTION_PX)
      copy(true, true)
      return
    }
    // A line held out for a tap goes back now, and its timer with it, or
    // the timer would pull back the next swipe midway.
    window.clearTimeout(holding.current)
    setCopyTapWaiting(false)
    move(0)
    if (!isActing) return
    hasSwiped.current = true
    if (reached <= -SWIPE_ACTION_PX) onReply()
  }
  return (
    <div
      className={[className, 'agent-line-swipeable', offset !== 0 ? 'swiping' : ''].filter(Boolean).join(' ')}
      style={offset !== 0 ? { transform: `translateX(${offset}px)` } : undefined}
      onPointerDown={(event) => {
        if (event.pointerType !== 'touch') return
        touched.current = { pointerId: event.pointerId, x: event.clientX, y: event.clientY, isSwiping: false }
        hasSwiped.current = false
      }}
      onPointerMove={(event) => {
        const from = touched.current
        if (!from || from.pointerId !== event.pointerId) return
        const across = event.clientX - from.x
        const down = event.clientY - from.y
        if (!from.isSwiping) {
          if (Math.abs(down) > SWIPE_STARTS_PX && Math.abs(down) > Math.abs(across)) {
            touched.current = null
            return
          }
          if (Math.abs(across) < SWIPE_STARTS_PX) return
          from.isSwiping = true
          // Moved on from where it began, not from where it became a
          // swipe, would jump the line by the slack.
          from.x += Math.sign(across) * SWIPE_STARTS_PX
          event.currentTarget.setPointerCapture?.(event.pointerId)
        }
        move(swipeOffset(event.clientX - from.x))
      }}
      onPointerUp={(event) => {
        if (touched.current?.pointerId === event.pointerId) end(true)
      }}
      onPointerCancel={() => end(false)}
      // The tap that ends a swipe is not a tap on what is in the line: a
      // link in an answer stays shut.
      onClickCapture={(event) => {
        if (!hasSwiped.current) return
        hasSwiped.current = false
        event.preventDefault()
        event.stopPropagation()
      }}
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
      {offset > 0 &&
        (isCopyTapWaiting ? (
          <button
            type="button"
            className="agent-swipe-action copy waiting icon-action"
            title={copyLabel}
            aria-label={copyLabel}
            onClick={() => copy(false, true)}
          >
            <CopyIcon size={18} />
          </button>
        ) : (
          <span
            className={['agent-swipe-action copy', isCopied ? 'copied' : ''].filter(Boolean).join(' ')}
            aria-hidden="true"
            style={{ opacity: Math.min(1, offset / SWIPE_ACTION_PX) }}
          >
            {isCopied ? <CheckIcon size={18} /> : <CopyIcon size={16} />}
          </span>
        ))}
      <span className="agent-line-actions">
        <button
          type="button"
          className={['icon-action', isCopied ? 'copied' : ''].filter(Boolean).join(' ')}
          title={copyLabel}
          aria-label={copyLabel}
          onClick={() => copy(false, false)}
        >
          {isCopied ? <CheckIcon size={14} /> : <CopyIcon size={14} />}
        </button>
        <button type="button" className="icon-action" title={replyLabel} aria-label={replyLabel} onClick={onReply}>
          <ReplyIcon size={14} />
        </button>
      </span>
    </div>
  )
}
