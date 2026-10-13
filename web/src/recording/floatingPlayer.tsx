import { useEffect, useState, useSyncExternalStore } from 'react'
import { CloseIcon, PauseIcon, PlayIcon } from '../components/icons'
import { useToast } from '../components/toast'
import { useTranslation } from '../i18n/i18n'
import { formatRecordingLength } from './library'
import { type RecordingPassagePlayback } from './playback'

// RecordingFloatingPlayer is the small player at the foot of the window while
// the recording plays, so that what a bubble started can be paused or stopped
// from wherever the page is scrolled to. It appears when playback starts, stays
// when paused so playing can go on, and goes away when closed.
export function RecordingFloatingPlayer({
  passagePlayback,
  timelineEndMS,
}: {
  passagePlayback: RecordingPassagePlayback
  timelineEndMS: number
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const state = useSyncExternalStore(passagePlayback.subscribe, passagePlayback.getState)
  const [isOpen, setIsOpen] = useState(false)
  useEffect(() => {
    if (state.isPlaying) setIsOpen(true)
  }, [state.isPlaying])
  const toggle = () =>
    passagePlayback.toggle(timelineEndMS).catch((caught) => toast.failure(caught, t('recordings.playerUnavailable')))
  useEffect(() => {
    if (!isOpen) return
    // Space plays and pauses, except where it types or presses a control.
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== ' ' || event.defaultPrevented || event.ctrlKey || event.metaKey || event.altKey) return
      const target = event.target
      if (
        target instanceof HTMLElement &&
        (target.isContentEditable || target.closest('input, textarea, select, button, a, [role="dialog"]'))
      )
        return
      event.preventDefault()
      void passagePlayback.toggle(timelineEndMS).catch(() => {})
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [isOpen, passagePlayback, timelineEndMS])
  if (!isOpen) return null
  const label = state.isPlaying ? t('recordings.pause') : t('recordings.playFromHere')
  return (
    <>
      {/* Room under the last bubble, so the player never covers it. */}
      <div className="recording-floating-player-room" aria-hidden="true" />
      <div className="recording-floating-player" role="group" aria-label={t('recordings.floatingPlayer')}>
        <button type="button" className="icon-action" title={label} aria-label={label} onClick={toggle}>
          {state.isPlaying ? <PauseIcon size={18} /> : <PlayIcon size={18} />}
        </button>
        <span className="muted recording-player-time">{formatRecordingLength(state.positionMS)}</span>
        <button
          type="button"
          className="icon-action"
          title={t('recordings.stopPlayback')}
          aria-label={t('recordings.stopPlayback')}
          onClick={() => {
            passagePlayback.end()
            setIsOpen(false)
          }}
        >
          <CloseIcon size={18} />
        </button>
      </div>
    </>
  )
}
