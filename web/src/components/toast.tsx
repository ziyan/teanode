import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
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

// A burst of them — a rule applied to fifty messages, a page of failures —
// must not fill the window. The oldest go.
const MOST = 3

type Speak = {
  done: (message: string, action?: ToastAction) => void
  failed: (message: string) => void
  // Whatever went wrong, said in the words it arrived with. Anything that is
  // not an Error has no message worth showing, so it gets the general one.
  failure: (caught: unknown, fallback: string) => void
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
      failed: (message) => add('failed', message),
      failure: (caught, fallback) =>
        add('failed', caught instanceof Error && caught.message ? caught.message : fallback),
    }),
    [add],
  )

  return (
    <Context.Provider value={speak}>
      {children}
      {createPortal(
        <div className="toasts" role="status" aria-live="polite">
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

  // The clock stops while the pointer is on it. Somebody reaching for "undo"
  // should not have it taken away as they arrive.
  useEffect(() => {
    if (held) {
      return
    }
    const timer = window.setTimeout(onDismiss, LINGER[toast.kind])
    return () => window.clearTimeout(timer)
  }, [held, toast.kind, onDismiss])

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
