import { useEffect } from 'react'
import { Link } from 'react-router-dom'
import { CloseIcon, PauseIcon, PlayIcon, StopIcon } from '../components/icons'
import { Tooltip } from '../components/tooltip'
import { useToast } from '../components/toast'
import { useTranslation } from '../i18n/i18n'
import { useSession } from '../session'
import { isCaptureBusy, updateCaptureSnapshot, useCaptureSnapshot, type CaptureSnapshot } from './captureSnapshot'
import { openCaptureStore } from './captureStore'
import { formatRecordingLength } from './library'

// RecordingBarView is the small bar fixed at the foot of the dashboard while a
// recording is made, or while what was recorded is still being sent. It is a
// link to the recording's page, which shows the conversation as it is heard,
// with the controls beside it.
export function RecordingBarView({
  snapshot,
  onPause,
  onResume,
  onStop,
  onDismissError,
}: {
  snapshot: CaptureSnapshot
  onPause: () => void
  onResume: () => void
  onStop: () => void
  onDismissError: () => void
}) {
  const { t } = useTranslation()
  const isCapturing = snapshot.phase === 'recording' || snapshot.phase === 'paused' || snapshot.phase === 'stopping'
  if (!isCapturing && snapshot.phase !== 'starting' && snapshot.pendingPieceCount === 0 && !snapshot.errorMessage)
    return null
  const isPaused = snapshot.phase === 'paused'
  // What the bar says beside the time: the words, or why there are none.
  const statusText =
    snapshot.phase === 'starting'
      ? t('recordingBar.starting')
      : snapshot.phase === 'stopping'
        ? t('recordingBar.saving')
        : isPaused
          ? t('recordingBar.paused')
          : isCapturing
            ? snapshot.newestWords || t('recordingBar.listening')
            : snapshot.pendingPieceCount > 0
              ? t('recordingBar.uploading')
              : snapshot.errorMessage
  const body = (
    <>
      <span className={`recording-bar-dot ${snapshot.phase}`} aria-hidden="true" />
      {isCapturing ? <span className="recording-bar-time">{formatRecordingLength(snapshot.elapsedMS)}</span> : null}
      <span className="recording-bar-words">{statusText}</span>
    </>
  )
  return (
    <div className="recording-bar" role="region" aria-label={t('recordingBar.label')} data-phase={snapshot.phase}>
      {isCapturing && snapshot.recordingId ? (
        <Link
          className="recording-bar-main"
          to={`/recordings/${snapshot.recordingId}`}
          aria-label={t('recordingBar.open')}
        >
          {body}
        </Link>
      ) : (
        <div className="recording-bar-main">{body}</div>
      )}
      {snapshot.phase === 'recording' || isPaused ? (
        <div className="row-actions">
          <Tooltip label={isPaused ? t('recordingBar.resume') : t('recordingBar.pause')}>
            <button
              type="button"
              className="icon-action"
              aria-label={isPaused ? t('recordingBar.resume') : t('recordingBar.pause')}
              onClick={isPaused ? onResume : onPause}
            >
              {isPaused ? <PlayIcon size={16} /> : <PauseIcon size={16} />}
            </button>
          </Tooltip>
          <Tooltip label={t('recordingBar.stop')}>
            <button type="button" className="icon-action danger" aria-label={t('recordingBar.stop')} onClick={onStop}>
              <StopIcon size={16} />
            </button>
          </Tooltip>
        </div>
      ) : null}
      {!isCapturing && snapshot.pendingPieceCount === 0 && snapshot.errorMessage ? (
        <Tooltip label={t('common.close')}>
          <button type="button" className="icon-action" aria-label={t('common.close')} onClick={onDismissError}>
            <CloseIcon size={16} />
          </button>
        </Tooltip>
      ) : null}
      {snapshot.pendingPieceCount > 0 && !snapshot.isStorageDurable ? (
        <p className="recording-bar-note muted">{t('recordingBar.notKept')}</p>
      ) : null}
    </div>
  )
}

// RecordingBar is the bar for this tab's recording. The recorder itself is
// fetched when it is first needed (starting a recording, or finding pieces an
// earlier visit left in the browser), so a dashboard that never records does
// not download it.
export function RecordingBar() {
  const session = useSession()
  const snapshot = useCaptureSnapshot()
  const toast = useToast()
  const { t } = useTranslation()
  const userId = session.userId ?? ''
  // Pieces an earlier visit left behind are sent now, and a capture that visit
  // never finished is stopped.
  useEffect(() => {
    if (!userId) return
    let isCurrent = true
    void (async () => {
      const { store } = await openCaptureStore()
      if (!isCurrent) return
      const [pieces, captures] = await Promise.all([store.listPieces(), store.listCaptures()])
      if (!isCurrent || (pieces.length === 0 && captures.length === 0)) return
      const { recoverRecordings } = await import(/* webpackChunkName: "recordings" */ './capture')
      if (isCurrent) await recoverRecordings(userId)
    })().catch(() => undefined)
    return () => {
      isCurrent = false
    }
  }, [userId])
  // Leaving the page ends the recording's microphone and, in a browser that
  // cannot keep pieces, loses what has not been sent: ask first.
  const isBusy = isCaptureBusy(snapshot)
  useEffect(() => {
    if (!isBusy) return
    const ask = (event: BeforeUnloadEvent) => {
      event.preventDefault()
      event.returnValue = ''
    }
    window.addEventListener('beforeunload', ask)
    return () => window.removeEventListener('beforeunload', ask)
  }, [isBusy])
  useEffect(() => {
    document.body.classList.toggle('has-recording-bar', isBusy || Boolean(snapshot.errorMessage))
    return () => document.body.classList.remove('has-recording-bar')
  }, [isBusy, snapshot.errorMessage])
  const control = (action: 'pauseRecording' | 'resumeRecording' | 'stopRecording') => async () => {
    try {
      const capture = await import(/* webpackChunkName: "recordings" */ './capture')
      await capture[action]()
    } catch (caught) {
      toast.failure(caught, t('recordingBar.controlFailed'))
    }
  }
  return (
    <RecordingBarView
      snapshot={snapshot}
      onPause={() => void control('pauseRecording')()}
      onResume={() => void control('resumeRecording')()}
      onStop={() => void control('stopRecording')()}
      onDismissError={() => updateCaptureSnapshot({ errorMessage: '' })}
    />
  )
}
