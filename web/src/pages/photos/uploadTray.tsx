import { useEffect, useRef, useState } from 'react'
import { CheckIcon, ChevronDownIcon, ChevronUpIcon, CloseIcon, WarningIcon } from '../../components/icons'
import { Tooltip } from '../../components/tooltip'
import { Key, useTranslation } from '../../i18n/i18n'
import { photoUploads, useUploadItems, type UploadItem, type UploadItemState } from '../../photos/uploadQueue'

// The tray at the bottom right: the files on their way to the library, with
// how far they have got. It folds down to its heading when the last file is
// done, and goes when the person clears it. Thousands of files make thousands
// of lines nobody reads, so the tray draws the few that are being worked on,
// the ones that failed (each one has something to say), and counts the rest.

// How many lines of files being sent, and of files that failed, are drawn.
const MOST_ACTIVE_ROWS = 6
const MOST_FAILED_ROWS = 8

const STATE_LABELS: Record<UploadItemState, Key> = {
  waiting: 'photos.stateWaiting',
  hashing: 'photos.stateReading',
  sending: 'photos.stateSending',
  done: 'photos.stateDone',
  skipped: 'photos.stateSkipped',
  inTrash: 'photos.stateInTrash',
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
  const failedCount = items.filter((item) => item.state === 'failed').length
  const wasBusy = useRef(false)

  // Opens when work starts and folds when it ends, so a finished upload does
  // not keep a list over the corner of the photos. What failed stays open: a
  // tray that folded over a failure said only that the upload was finished.
  useEffect(() => {
    if (activeCount > 0 && !wasBusy.current) setIsCollapsed(false)
    if (activeCount === 0 && wasBusy.current) setIsCollapsed(failedCount === 0)
    wasBusy.current = activeCount > 0
  }, [activeCount, failedCount])

  // The toasts on a phone rise above the tray, which is as tall as what is in
  // it, so the tray says how tall that is.
  const tray = useRef<HTMLElement>(null)
  const isShown = items.length > 0
  useEffect(() => {
    const element = tray.current
    if (!isShown || !element) return
    const root = document.documentElement
    const report = () => root.style.setProperty('--photo-tray-height', `${element.offsetHeight + 8}px`)
    report()
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(report)
    observer?.observe(element)
    return () => {
      observer?.disconnect()
      root.style.removeProperty('--photo-tray-height')
    }
  }, [isShown])

  if (items.length === 0) return null
  const finishedCount = items.length - activeCount
  const activeItems = items.filter(isActive)
  const failedItems = items.filter((item) => item.state === 'failed')
  const shownItems = [...activeItems.slice(0, MOST_ACTIVE_ROWS), ...failedItems.slice(0, MOST_FAILED_ROWS)]
  const hiddenActiveCount = activeItems.length - Math.min(activeItems.length, MOST_ACTIVE_ROWS)
  const hiddenFailedCount = failedItems.length - Math.min(failedItems.length, MOST_FAILED_ROWS)
  const quietFinishedCount = finishedCount - failedItems.length
  const title =
    activeCount > 0
      ? t('photos.trayBusy', { done: finishedCount, total: items.length })
      : failedCount > 0
        ? t('photos.trayFailed', { count: failedCount })
        : t('photos.trayDone')
  return (
    <section className="photo-tray" aria-label={title} ref={tray}>
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
          {shownItems.map((item) => (
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
          {hiddenActiveCount > 0 && (
            <li className="photo-tray-item photo-tray-summary muted">
              {t('photos.trayMoreWaiting', { count: hiddenActiveCount })}
            </li>
          )}
          {hiddenFailedCount > 0 && (
            <li className="photo-tray-item photo-tray-summary muted">
              {t('photos.trayMoreFailed', { count: hiddenFailedCount })}
            </li>
          )}
          {quietFinishedCount > 0 && (
            <li className="photo-tray-item photo-tray-summary muted">
              {t('photos.trayFinished', { count: quietFinishedCount })}
            </li>
          )}
        </ul>
      )}
    </section>
  )
}
