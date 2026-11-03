import { useEffect, useRef, useState } from 'react'

import { useTranslation } from '../i18n/i18n'
import { clockOf } from '../voice/voiceMessageRecorder'
import { PauseIcon, PlayIcon } from './icons'
import { useToast } from './toast'
import { Tooltip } from './tooltip'

// A voice message in a chat: a play button, the shape of the recording, the
// time left and, under them, the words the server heard in it.

// WAVEFORM_BAR_COUNT is how many levels the server sends for a recording,
// and how many bars are drawn.
export const WAVEFORM_BAR_COUNT = 48

// SEEK_STEP_SECONDS is how far an arrow key moves the playing position.
const SEEK_STEP_SECONDS = 5

// FLAT_LEVEL is the height, from 0 to 255, of a bar when the recording came
// with no waveform.
const FLAT_LEVEL = 96

// waveformLevels reads the base64 levels (0 to 255) the server sends into
// one level per bar. A missing or damaged waveform is drawn flat.
export function waveformLevels(waveform: string): number[] {
  let bytes: number[] = []
  try {
    bytes = Array.from(atob(waveform), (character) => character.charCodeAt(0))
  } catch {
    bytes = []
  }
  return Array.from({ length: WAVEFORM_BAR_COUNT }, (_, index) =>
    bytes.length > 0 ? bytes[Math.floor((index * bytes.length) / WAVEFORM_BAR_COUNT)] : FLAT_LEVEL,
  )
}

// The one recording playing: starting another stops it.
let playingAudio: HTMLAudioElement | null = null

// VoiceMessageBubble is what a voice message is drawn as in place of a chip
// for its file. The sound is fetched only when it is played.
export function VoiceMessageBubble({
  source,
  durationMS,
  waveform,
  transcriptText,
  transcriptState,
  onTranscribe,
}: {
  // The address of the recording: signed when the drawer is framed
  // elsewhere, so null until it is.
  source: string | null
  durationMS: number
  waveform: string
  transcriptText: string
  // 'waiting', 'done', 'failed', or 'off' and empty for no transcript.
  transcriptState: string
  // Asks for the words of a message that has none, paid by the reader's
  // agent; left out where that cannot be asked. The words come back as an
  // edit of the message, to every member.
  onTranscribe?: () => Promise<void>
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const audio = useRef<HTMLAudioElement>(null)
  const [isPlaying, setPlaying] = useState(false)
  const [positionSeconds, setPositionSeconds] = useState(0)
  const [loadedSeconds, setLoadedSeconds] = useState(0)
  const [isTranscribing, setTranscribing] = useState(false)
  const canTranscribe = onTranscribe !== undefined && ['off', 'failed', ''].includes(transcriptState)
  const transcribe = async () => {
    if (!onTranscribe) return
    setTranscribing(true)
    try {
      await onTranscribe()
    } catch (caught) {
      toast.failure(caught, t('chat.transcriptFailed'))
    } finally {
      setTranscribing(false)
    }
  }
  const levels = waveformLevels(waveform)
  const totalSeconds = durationMS > 0 ? durationMS / 1000 : loadedSeconds
  const progress = totalSeconds > 0 ? Math.min(1, positionSeconds / totalSeconds) : 0
  const secondsLeft = Math.max(0, totalSeconds - positionSeconds)

  // Gone from the screen, it stops.
  useEffect(() => {
    const element = audio.current
    return () => {
      if (element && !element.paused) element.pause()
      if (playingAudio === element) playingAudio = null
    }
  }, [])

  const toggle = () => {
    const element = audio.current
    if (!element || !source) return
    if (!element.paused) {
      element.pause()
      return
    }
    if (playingAudio && playingAudio !== element) playingAudio.pause()
    playingAudio = element
    Promise.resolve(element.play()).catch(() => toast.failed(t('chat.voiceMessageNotPlayed')))
  }

  const seekTo = (seconds: number) => {
    const element = audio.current
    if (!element || totalSeconds <= 0) return
    const target = Math.max(0, Math.min(totalSeconds, seconds))
    element.currentTime = target
    setPositionSeconds(target)
  }

  const label = isPlaying ? t('chat.voiceMessagePause') : t('chat.voiceMessagePlay')
  return (
    <div className="voice-bubble">
      <div className="voice-bubble-player">
        <Tooltip label={label}>
          <button
            type="button"
            className="icon-button voice-bubble-play"
            aria-label={label}
            disabled={!source}
            onClick={toggle}
          >
            {isPlaying ? <PauseIcon size={16} /> : <PlayIcon size={16} />}
          </button>
        </Tooltip>
        <div
          className="voice-bubble-wave"
          role="slider"
          tabIndex={0}
          aria-label={t('chat.voiceMessageSeek')}
          aria-valuemin={0}
          aria-valuemax={Math.round(totalSeconds)}
          aria-valuenow={Math.round(positionSeconds)}
          aria-valuetext={clockOf(positionSeconds * 1000)}
          onClick={(event) => {
            const box = event.currentTarget.getBoundingClientRect()
            if (box.width > 0) seekTo(((event.clientX - box.left) / box.width) * totalSeconds)
          }}
          onKeyDown={(event) => {
            if (event.key === 'ArrowRight') seekTo(positionSeconds + SEEK_STEP_SECONDS)
            else if (event.key === 'ArrowLeft') seekTo(positionSeconds - SEEK_STEP_SECONDS)
            else if (event.key === 'Home') seekTo(0)
            else if (event.key === 'End') seekTo(totalSeconds)
            else return
            event.preventDefault()
          }}
        >
          {levels.map((level, index) => (
            <span
              key={index}
              className={['voice-bubble-bar', (index + 0.5) / WAVEFORM_BAR_COUNT <= progress ? 'is-played' : '']
                .filter(Boolean)
                .join(' ')}
              style={{ height: `${12 + (level / 255) * 88}%` }}
            />
          ))}
        </div>
        <span className="voice-bubble-time">{clockOf(secondsLeft * 1000)}</span>
        <audio
          ref={audio}
          preload="none"
          src={source ?? undefined}
          onPlay={() => setPlaying(true)}
          onPause={() => setPlaying(false)}
          onEnded={() => {
            setPlaying(false)
            setPositionSeconds(0)
            if (playingAudio === audio.current) playingAudio = null
          }}
          onTimeUpdate={(event) => setPositionSeconds(event.currentTarget.currentTime)}
          onLoadedMetadata={(event) => {
            const loaded = event.currentTarget.duration
            if (Number.isFinite(loaded)) setLoadedSeconds(loaded)
          }}
        />
      </div>
      <div className="voice-bubble-transcript" aria-live="polite">
        {transcriptState === 'waiting' || isTranscribing
          ? t('chat.transcribing')
          : transcriptState === 'failed'
            ? t('chat.transcriptFailed')
            : transcriptState === 'done' && transcriptText.trim()
              ? transcriptText
              : null}
      </div>
      {canTranscribe && !isTranscribing ? (
        <button type="button" className="voice-bubble-transcribe" onClick={() => void transcribe()}>
          {t('chat.transcribe')}
        </button>
      ) : null}
    </div>
  )
}
