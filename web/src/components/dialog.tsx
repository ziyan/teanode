import { useEffect, useRef } from 'react'

import { useTranslation } from '../i18n/i18n'

// FormDialog is a real <form> in a modal, so Enter submits and Escape closes —
// both of which people try before reaching for the mouse.
//
// Used for the handful of "give me one value and make a thing" flows. They are
// rare enough that a permanent field on the page is clutter, and short enough
// that a page of their own would be ceremony.
export function FormDialog({
  title,
  submitLabel,
  busy,
  error,
  canSubmit = true,
  onSubmit,
  onClose,
  children,
}: {
  title: string
  submitLabel: string
  busy?: boolean
  error?: string | null
  canSubmit?: boolean
  onSubmit: () => void
  onClose: () => void
  children: React.ReactNode
}) {
  const { t } = useTranslation()
  const form = useRef<HTMLFormElement>(null)

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        onClose()
      }
    }
    document.addEventListener('keydown', onKeyDown)

    // Put the cursor where the reader is about to type. Opening a dialog and
    // then having to click into it is a step nobody wants.
    form.current?.querySelector<HTMLInputElement>('input, select, textarea')?.focus()

    return () => document.removeEventListener('keydown', onKeyDown)
  }, [onClose])

  return (
    <div className="dialog-scrim" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
      <form
        className="dialog"
        ref={form}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        onSubmit={(event) => {
          event.preventDefault()
          onSubmit()
        }}
      >
        <h3>{title}</h3>
        {children}
        {error && <p className="error">{error}</p>}
        <div className="dialog-actions">
          <button type="button" onClick={onClose}>
            {t('common.cancel')}
          </button>
          <button className="primary" type="submit" disabled={busy || !canSubmit}>
            {submitLabel}
          </button>
        </div>
      </form>
    </div>
  )
}

// ConfirmDialog asks before something that is awkward to undo. It replaces
// window.confirm, which cannot be styled, cannot be translated, and on some
// browsers is suppressed entirely — a confirmation nobody sees is one that
// silently says yes.
//
// Destructive by default, because most of these take something away. Signing
// out does not, and colouring it as though it did makes every red button here
// mean less.
export function ConfirmDialog({
  title,
  body,
  confirmLabel,
  busy,
  destructive = true,
  onConfirm,
  onClose,
}: {
  title: string
  body?: React.ReactNode
  confirmLabel: string
  busy?: boolean
  destructive?: boolean
  onConfirm: () => void
  onClose: () => void
}) {
  const { t } = useTranslation()

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        onClose()
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [onClose])

  return (
    <div className="dialog-scrim" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
      <div className="dialog" role="alertdialog" aria-modal="true" aria-label={title}>
        <h3>{title}</h3>
        {typeof body === 'string' ? <p className="muted">{body}</p> : body}
        <div className="dialog-actions">
          <button type="button" onClick={onClose} disabled={busy}>
            {t('common.cancel')}
          </button>
          <button
            className={destructive ? 'primary danger' : 'primary'}
            type="button"
            disabled={busy}
            onClick={onConfirm}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
