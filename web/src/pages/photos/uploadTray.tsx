import { useEffect, useRef, useState } from 'react'
import { CheckIcon, ChevronDownIcon, ChevronUpIcon, CloseIcon, WarningIcon } from '../../components/icons'
import { Tooltip } from '../../components/tooltip'
import { Key, useTranslation } from '../../i18n/i18n'
import { photoUploads, useUploadItems, type UploadItem, type UploadItemState } from '../../photos/uploadQueue'

// The tray at the bottom right: every file on its way to the library, with
// how far it has got. It folds down to its heading when the last file is
// done, and goes when the person clears it.

const STATE_LABELS: Record<UploadItemState, Key> = {
  waiting: 'photos.stateWaiting',
  hashing: 'photos.stateReading',
  sending: 'photos.stateSending',
  done: 'photos.stateDone',
  skipped: 'photos.stateSkipped',
  failed: 'photos.stateFailed',
  canceled: 'photos.stateCanceled',
}

function isActive(item: UploadItem): boolean {
  return item.state === 'waiting' || item.state === 'hashing' || item.state === 'sending'
}

function fractionOf(item: UploadItem): number {
  if (item.sizeBytes <= 0) return item.state === 'waiting' ? 0 : 1
  const bytes = item.state === 'hashing' ? item.hashedBytes : item.sentBytes
  return Math.min(1, bytes / item.sizeBytes)
}

export function UploadTray() {
  const { t } = useTranslation()
  const items = useUploadItems()
  const [isCollapsed, setIsCollapsed] = useState(false)
  const activeCount = items.filter(isActive).length
  const wasBusy = useRef(false)

  // Opens when work starts and folds when it ends, so a finished upload does
  // not keep a list over the corner of the photos.
  useEffect(() => {
    if (activeCount > 0 && !wasBusy.current) setIsCollapsed(false)
    if (activeCount === 0 && wasBusy.current) setIsCollapsed(true)
    wasBusy.current = activeCount > 0
  }, [activeCount])

  if (items.length === 0) return null
  const finishedCount = items.length - activeCount
  const title =
    activeCount > 0 ? t('photos.trayBusy', { done: finishedCount, total: items.length }) : t('photos.trayDone')
  return (
    <section className="photo-tray" aria-label={title}>
      <header className="photo-tray-head">
        <strong role="status">{title}</strong>
        <span className="photo-viewer-spacer" />
        <Tooltip label={isCollapsed ? t('photos.trayExpand') : t('photos.trayCollapse')}>
          <button
            type="button"
            className="icon-button"
            aria-label={isCollapsed ? t('photos.trayExpand') : t('photos.trayCollapse')}
            aria-expanded={!isCollapsed}
            onClick={() => setIsCollapsed((previous) => !previous)}
          >
            {isCollapsed ? <ChevronUpIcon size={16} /> : <ChevronDownIcon size={16} />}
          </button>
        </Tooltip>
        {activeCount === 0 && (
          <Tooltip label={t('photos.trayClear')}>
            <button
              type="button"
              className="icon-button"
              aria-label={t('photos.trayClear')}
              onClick={() => photoUploads.clearFinished()}
            >
              <CloseIcon size={16} />
            </button>
          </Tooltip>
        )}
      </header>
      {!isCollapsed && (
        <ul className="photo-tray-list">
          {items.map((item) => (
            <li key={item.id} className={`photo-tray-item is-${item.state}`}>
              <span className="photo-tray-name" title={item.name}>
                {item.name}
              </span>
              <span className="photo-tray-state muted" title={item.errorMessage || undefined}>
                {item.state === 'done' && <CheckIcon size={13} />}
                {item.state === 'failed' && <WarningIcon size={13} />}
                <span>
                  {item.state === 'failed' && item.errorMessage ? item.errorMessage : t(STATE_LABELS[item.state])}
                </span>
              </span>
              {isActive(item) && (
                <>
                  <progress
                    className={item.state === 'hashing' ? 'photo-tray-bar is-reading' : 'photo-tray-bar'}
                    max={1}
                    // No value says that something is happening and that
                    // there is nothing to measure yet: a part is on its way
                    // and the server has not yet said it has it.
                    value={fractionOf(item) > 0 ? fractionOf(item) : undefined}
                    aria-label={item.name}
                  />
                  <Tooltip label={t('photos.trayCancel', { name: item.name })}>
                    <button
                      type="button"
                      className="icon-action"
                      aria-label={t('photos.trayCancel', { name: item.name })}
                      onClick={() => photoUploads.cancel(item.id)}
                    >
                      <CloseIcon size={14} />
                    </button>
                  </Tooltip>
                </>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
