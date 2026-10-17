import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'

import { CloseIcon } from './icons'
import { useTranslation } from '../i18n/i18n'

// Where the program says what just happened.
//
// Before this, an action that worked said nothing at all and one that failed
// left a red block above the list until something else replaced it. Both are
// wrong in the same way: what happened to what you just did is a passing fact,
// and a passing fact belongs somewhere it can pass.
//
// A toast carries at most one action, which is how undo arrives — "Archived.
// Undo." is the difference between a program you can use quickly and one you
// have to use carefully, because the cost of the wrong click stops being
// "find where that went".
//
// What does not belong here: anything the reader has to act on. A form field
// that is wrong, a page whose query failed — those stay on the page. A
// message that takes itself away is no place for something that must be dealt
// with.

export type ToastKind = 'done' | 'failed'

export type ToastAction = {
  label: string
  run: () => void | Promise<void>
}

type Toast = {
  id: number
  kind: ToastKind
  message: string
  action?: ToastAction
}

// How long each kind stays. A failure is read; something that worked is only
// noticed, and a reader who was not looking has lost nothing.
const LINGER: Record<ToastKind, number> = { done: 4500, failed: 9000 }
const ACTION_LINGER = 12000

// A burst of them — a rule applied to fifty messages, a page of failures —
// must not fill the window. The oldest go.
const MOST = 3

// The room the strip of toasts has at the foot of the window. It starts at
// the content area (the rail's width is --rail-width) and ends before the
// agent's open drawer and the upload tray, which stand at the right and would
// otherwise cover a message. Left of the drawer there can be too little room
// to read a message in, as at 800 pixels with the rail open; the strip then
// starts at the window's margin instead, over the rail, lifted above the
// account row at the rail's foot.
const OBSTACLES = '.agent-drawer:not(.standalone), .photo-tray'
const MARGIN = 16
const GAP = 12
const LEAST_ROOM = 240
const FALLBACK_LIFT = 72

export type ToastRoom = {
  // Where the strip ends, in pixels from the window's left edge.
  edge: number
  // True when the strip starts at the window's margin, over the rail.
  isOverRail: boolean
}

// toastRoom works out where the strip may end. railWidth is where the content
// begins (0 when the rail is a drawer), and the obstacles are the boxes of
// whatever stands at the right of the window.
export function toastRoom(
  viewportWidth: number,
  railWidth: number,
  obstacles: { left: number; right: number }[],
): ToastRoom {
  const windowEdge = viewportWidth - MARGIN
  let edge = windowEdge
  for (const obstacle of obstacles) {
    if (obstacle.right > obstacle.left && obstacle.left < edge + GAP) edge = Math.min(edge, obstacle.left - GAP)
  }
  const contentLeft = railWidth + MARGIN
  return { edge, isOverRail: railWidth > 0 && edge - contentLeft < LEAST_ROOM }
}

type Speak = {
  done: (message: string, action?: ToastAction) => void
  failed: (message: string, action?: ToastAction) => void
  // Whatever went wrong, said in the words it arrived with. Anything that is
  // not an Error has no message worth showing, so it gets the general one.
  failure: (caught: unknown, fallback: string) => void
  // Takes every toast down at once, for a page starting over whose earlier
  // failure no longer applies.
  clear: () => void
}

const Context = createContext<Speak | null>(null)

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])
  const next = useRef(1)

  const dismiss = useCallback((id: number) => {
    setToasts((previous) => previous.filter((toast) => toast.id !== id))
  }, [])

  const add = useCallback((kind: ToastKind, message: string, action?: ToastAction) => {
    const id = next.current++
    setToasts((previous) => [...previous, { id, kind, message, action }].slice(-MOST))
  }, [])

  const speak = useMemo<Speak>(
    () => ({
      done: (message, action) => add('done', message, action),
      failed: (message, action) => add('failed', message, action),
      failure: (caught, fallback) =>
        add('failed', caught instanceof Error && caught.message ? caught.message : fallback),
      clear: () => setToasts([]),
    }),
    [add],
  )

  const strip = useRef<HTMLDivElement>(null)
  useToastRoom(strip, toasts.length > 0)

  return (
    <Context.Provider value={speak}>
      {children}
      {createPortal(
        <div className="toasts" role="status" aria-live="polite" ref={strip}>
          {toasts.map((toast) => (
            <ToastRow
              key={toast.id}
              toast={toast}
              onDismiss={() => dismiss(toast.id)}
              onFailed={(caught) => speak.failure(caught, toast.message)}
            />
          ))}
        </div>,
        document.body,
      )}
    </Context.Provider>
  )
}

// useToastRoom keeps the strip clear of the drawer and the tray while there
// is a toast to show: they open, move and are resized meanwhile, so it looks
// again a few times a second rather than only when a toast arrives.
function useToastRoom(strip: React.RefObject<HTMLDivElement | null>, isShowing: boolean) {
  useLayoutEffect(() => {
    const element = strip.current
    if (!element) {
      return
    }
    const place = () => {
      // Under 721 pixels the drawer is the whole screen and the toasts are
      // drawn over it, as they are on a phone.
      if (!isShowing || window.innerWidth <= 720) {
        element.style.removeProperty('--toast-edge')
        element.style.removeProperty('--toast-lift')
        element.classList.remove('over-rail')
        return
      }
      const railWidth =
        window.innerWidth <= 760
          ? 0
          : parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--rail-width')) || 0
      const boxes = Array.from(document.querySelectorAll(OBSTACLES)).map((each) => each.getBoundingClientRect())
      const room = toastRoom(window.innerWidth, railWidth, boxes)
      element.style.setProperty('--toast-edge', `${room.edge}px`)
      element.classList.toggle('over-rail', room.isOverRail)
      const account = document.querySelector('.sidebar .account-button')?.getBoundingClientRect()
      element.style.setProperty(
        '--toast-lift',
        `${account ? Math.max(0, window.innerHeight - account.top + 14) : FALLBACK_LIFT}px`,
      )
    }
    place()
    if (!isShowing) {
      return
    }
    const timer = window.setInterval(place, 250)
    window.addEventListener('resize', place)
    return () => {
      window.clearInterval(timer)
      window.removeEventListener('resize', place)
    }
  }, [strip, isShowing])
}

// useToast is how anything says what happened. It throws when there is no
// provider rather than silently saying nothing, because a message nobody sees
// is worse than a crash in development.
export function useToast(): Speak {
  const speak = useContext(Context)
  if (!speak) {
    throw new Error('useToast outside a ToastProvider')
  }
  return speak
}

function ToastRow({
  toast,
  onDismiss,
  onFailed,
}: {
  toast: Toast
  onDismiss: () => void
  onFailed: (caught: unknown) => void
}) {
  const { t } = useTranslation()
  const [held, setHeld] = useState(false)

  // The latest onDismiss, called when the clock runs out. The list passes a
  // new function at every render, and a clock that depended on it started
  // again each time another toast arrived.
  const dismissRef = useRef(onDismiss)
  dismissRef.current = onDismiss

  // The clock stops while the pointer is on it. Somebody reaching for "undo"
  // should not have it taken away as they arrive.
  useEffect(() => {
    if (held) {
      return
    }
    // A toast with something to press stays long enough to be pressed.
    const linger = toast.action ? Math.max(LINGER[toast.kind], ACTION_LINGER) : LINGER[toast.kind]
    const timer = window.setTimeout(() => dismissRef.current(), linger)
    return () => window.clearTimeout(timer)
  }, [held, toast.kind, toast.action])

  return (
    <div
      className={toast.kind === 'failed' ? 'toast failed' : 'toast'}
      onPointerEnter={() => setHeld(true)}
      onPointerLeave={() => setHeld(false)}
    >
      <span className="toast-message">{toast.message}</span>
      {toast.action && (
        <button
          type="button"
          className="toast-action"
          // An action that fails says so. Undo runs outside whatever wrapped
          // the thing it is undoing, so without this a failed undo did
          // nothing and said nothing — the worst of both, since the reader
          // believes it worked.
          onClick={async () => {
            onDismiss()
            try {
              await toast.action?.run()
            } catch (caught) {
              onFailed(caught)
            }
          }}
        >
          {toast.action.label}
        </button>
      )}
      <button type="button" className="toast-close" aria-label={t('common.close')} onClick={onDismiss}>
        <CloseIcon size={14} />
      </button>
    </div>
  )
}
