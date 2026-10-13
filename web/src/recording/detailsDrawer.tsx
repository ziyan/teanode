import { useEffect, useRef } from 'react'
import { useTranslation } from '../i18n/i18n'
import { CloseIcon } from '../components/icons'

// RecordingDetailsDrawer slides in from the right with the advanced panels:
// the transcript's status, speakers and voice references, learning and the
// correction history. They are rarely wanted, so they are one step away rather
// than under the conversation.
export function RecordingDetailsDrawer({ onClose, children }: { onClose: () => void; children: React.ReactNode }) {
  const { t } = useTranslation()
  const closeButton = useRef<HTMLButtonElement>(null)
  const closeReference = useRef(onClose)
  closeReference.current = onClose
  useEffect(() => {
    closeButton.current?.focus()
    const onKeyDown = (event: KeyboardEvent) => {
      // A form open over the drawer takes the key first.
      if (event.key === 'Escape' && !event.defaultPrevented && !document.querySelector('.dialog-scrim'))
        closeReference.current()
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [])
  return (
    <div className="recording-drawer-scrim" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
      <aside className="recording-drawer" role="dialog" aria-modal="true" aria-label={t('recordings.detailsTitle')}>
        <div className="recording-drawer-head">
          <h3>{t('recordings.detailsTitle')}</h3>
          <button
            type="button"
            className="icon-button"
            ref={closeButton}
            aria-label={t('common.close')}
            onClick={onClose}
          >
            <CloseIcon size={18} />
          </button>
        </div>
        <div className="recording-drawer-body">{children}</div>
      </aside>
    </div>
  )
}
