import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react'
import { ErrorMessage } from '../components/common'
import { PauseIcon, PlayIcon } from '../components/icons'
import { useTranslation } from '../i18n/i18n'
import { recordingMediaPath, type RecordingMedia } from './api'
import { formatRecordingLength } from './library'
import { RecordingPassagePlayback } from './playback'
import { prepareRecordingPlayback, type RecordingPlayback } from './playbackPreview'
import { buildTimelinePieces, timelineGaps, timelineLengthMS } from './timeline'

// RecordingTimelinePlayer is the one player of a recording. Its time axis is the
// recording's timeline, however many pieces (recording media) it was made in:
// the pieces' media elements sit behind it, only the active one is shown (a
// video), and the controls below are the same whichever piece plays. A piece the
// browser cannot play shows its audio preview preparation here, not in a player
// of its own.
export function RecordingTimelinePlayer({
  mediaFiles,
  timelineDurationMS,
  passagePlayback,
}: {
  mediaFiles: RecordingMedia[]
  timelineDurationMS: number
  passagePlayback: RecordingPassagePlayback
}) {
  const pieces = useMemo(() => buildTimelinePieces(mediaFiles, timelineDurationMS), [mediaFiles, timelineDurationMS])
  const piecesKey = pieces.map((piece) => `${piece.mediaId}@${piece.startMS}-${piece.endMS}`).join(',')
  useEffect(() => {
    passagePlayback.configure(pieces)
    // pieces is rebuilt on every render of the page; what it says is piecesKey.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [passagePlayback, piecesKey])
  const activeMediaId = useSyncExternalStore(passagePlayback.subscribe, () => passagePlayback.getState().activeMediaId)
  const lengthMS = timelineLengthMS(pieces, timelineDurationMS)
  return (
    <div className="recording-original">
      {mediaFiles
        .filter((media) => pieces.some((piece) => piece.mediaId === media.id))
        .map((media) => (
          <RecordingPieceMedia
            key={media.id}
            media={media}
            isActive={media.id === (activeMediaId ?? pieces[0]?.mediaId)}
            passagePlayback={passagePlayback}
          />
        ))}
      <RecordingTimelineControls pieces={pieces} lengthMS={lengthMS} passagePlayback={passagePlayback} />
    </div>
  )
}

function RecordingTimelineControls({
  pieces,
  lengthMS,
  passagePlayback,
}: {
  pieces: ReturnType<typeof buildTimelinePieces>
  lengthMS: number
  passagePlayback: RecordingPassagePlayback
}) {
  const { t } = useTranslation()
  const state = useSyncExternalStore(passagePlayback.subscribe, passagePlayback.getState)
  const [seekError, setSeekError] = useState<string | null>(null)
  const percent = (valueMS: number) => (lengthMS > 0 ? Math.min(100, Math.max(0, (valueMS / lengthMS) * 100)) : 0)
  const report = (caught: unknown) => setSeekError(caught instanceof Error ? caught.message : String(caught))
  return (
    <>
      <div className="recording-player-controls">
        <button
          type="button"
          className="icon-action"
          title={state.isPlaying ? t('recordings.pause') : t('recordings.playFromHere')}
          aria-label={state.isPlaying ? t('recordings.pause') : t('recordings.playFromHere')}
          disabled={pieces.length === 0}
          onClick={() => {
            setSeekError(null)
            passagePlayback.toggle(lengthMS).catch(report)
          }}
        >
          {state.isPlaying ? <PauseIcon size={18} /> : <PlayIcon size={18} />}
        </button>
        <span className="muted recording-player-time">{formatRecordingLength(state.positionMS)}</span>
        <div className="recording-scrubber">
          <div className="recording-scrubber-track">
            <span className="recording-scrubber-fill" style={{ width: `${percent(state.positionMS)}%` }} />
            {timelineGaps(pieces).map((gap) => (
              <span
                key={gap.gapStartMS}
                className="recording-scrubber-gap"
                style={{ left: `${percent(gap.gapStartMS)}%`, width: `${percent(gap.gapEndMS - gap.gapStartMS)}%` }}
              />
            ))}
            <span className="recording-scrubber-thumb" style={{ left: `${percent(state.positionMS)}%` }} />
          </div>
          <input
            type="range"
            min={0}
            max={lengthMS}
            step={100}
            value={Math.min(state.positionMS, lengthMS)}
            aria-label={t('recordings.seekPosition')}
            aria-valuetext={`${formatRecordingLength(state.positionMS)} / ${formatRecordingLength(lengthMS)}`}
            disabled={pieces.length === 0}
            onChange={(event) => {
              setSeekError(null)
              passagePlayback.seekTo(Number(event.currentTarget.value), state.isPlaying).catch(report)
            }}
          />
        </div>
        <span className="muted recording-player-time">{formatRecordingLength(lengthMS)}</span>
      </div>
      <ErrorMessage error={seekError ?? state.errorMessage} />
    </>
  )
}

// RecordingPieceMedia holds one piece's media element, which the player plays
// and seeks. It draws nothing of its own but the video of the active piece and,
// for the active piece, what is needed when the browser cannot play it.
function RecordingPieceMedia({
  media,
  isActive,
  passagePlayback,
}: {
  media: RecordingMedia
  isActive: boolean
  passagePlayback: RecordingPassagePlayback
}) {
  const { t } = useTranslation()
  const mediaId = media.id
  const timelineStartMS = media.timelineStartMS
  const [playback, setPlayback] = useState<RecordingPlayback | null>(null)
  const [isUsingPreview, setIsUsingPreview] = useState(false)
  const [isPreparing, setIsPreparing] = useState(false)
  const [playbackError, setPlaybackError] = useState<string | null>(null)
  // Whether this browser cannot play the original: it says so up front for a
  // type it has no decoder for, or by the media element's error event. The
  // audio preview is offered only then; a browser that plays the original has
  // no use for it.
  const [isOriginalUnplayable, setIsOriginalUnplayable] = useState(
    () =>
      media.originalContentType !== '' && document.createElement('video').canPlayType(media.originalContentType) === '',
  )
  const isMounted = useRef(false)
  const hasUnsupportedOriginalReference = useRef(false)
  // The same, for drawing: the reference is read by playback, which is not
  // rendered, and a hint without an error would otherwise not redraw.
  const [hasUnsupportedOriginal, setHasUnsupportedOriginal] = useState(false)
  const previewPlayer = useRef<HTMLMediaElement | null>(null)
  const preparation = useRef<Promise<HTMLMediaElement> | null>(null)
  const preparationController = useRef<AbortController | null>(null)
  const previewReady = useRef<{
    resolve: (player: HTMLMediaElement) => void
    reject: (caught: Error) => void
  } | null>(null)
  const prepare = useCallback(() => {
    if (previewPlayer.current && previewPlayer.current.readyState >= 1 && !previewPlayer.current.error)
      return Promise.resolve(previewPlayer.current)
    if (preparation.current) return preparation.current
    const controller = new AbortController()
    preparationController.current = controller
    setIsPreparing(true)
    setPlaybackError(null)
    setPlayback(null)
    setIsUsingPreview(false)
    const pendingPreparation = prepareRecordingPlayback({ id: mediaId, timelineStartMS }, controller.signal)
      .then(async (compatiblePlayback) => {
        if (!isMounted.current || controller.signal.aborted) throw new DOMException('Playback canceled', 'AbortError')
        const readyPlayer = new Promise<HTMLMediaElement>((resolve, reject) => {
          previewReady.current = { resolve, reject }
        })
        setPlayback(compatiblePlayback)
        setIsUsingPreview(true)
        return readyPlayer
      })
      .finally(() => {
        preparation.current = null
        if (isMounted.current) setIsPreparing(false)
      })
    preparation.current = pendingPreparation
    return pendingPreparation
  }, [mediaId, timelineStartMS])
  useEffect(() => {
    isMounted.current = true
    passagePlayback.registerFallback(media.id, {
      prepare,
      isOriginalUnsupported: () => hasUnsupportedOriginalReference.current && !previewPlayer.current,
    })
    return () => {
      isMounted.current = false
      preparationController.current?.abort()
      previewReady.current?.reject(new DOMException('Playback canceled', 'AbortError'))
      previewReady.current = null
      passagePlayback.registerFallback(media.id, null)
    }
  }, [media.id, passagePlayback, prepare])
  const registerPlayer = useCallback(
    (player: HTMLMediaElement | null) => passagePlayback.register(media.id, player),
    [media.id, passagePlayback],
  )
  const registerPreview = useCallback(
    (player: HTMLMediaElement | null) => {
      previewPlayer.current = player
      passagePlayback.register(media.id, player)
    },
    [media.id, passagePlayback],
  )
  // An original this browser has no decoder for is expected, not an error:
  // the player offers the audio preview with a quiet hint and plays it when
  // asked. A video that loads without a picture says so.
  const unsupportedOriginal = (errorMessage: string | null) => {
    hasUnsupportedOriginalReference.current = true
    setHasUnsupportedOriginal(true)
    setIsOriginalUnplayable(true)
    setPlaybackError(errorMessage)
  }
  const originalError = (player: HTMLMediaElement) => {
    if (player.error?.code === 4) unsupportedOriginal(null)
    else {
      setIsOriginalUnplayable(true)
      setPlaybackError(t('recordings.playbackError'))
    }
  }
  const playCompatible = async () => {
    try {
      await passagePlayback.playCompatible(media.id)
    } catch (caught) {
      if (isMounted.current) setPlaybackError(caught instanceof Error ? caught.message : String(caught))
    }
  }
  // Only the active piece loads ahead of time. A long recording is made of many
  // pieces, and loading each one's metadata up front sent a request per piece
  // when the page opened; another piece loads when it is played.
  const preload = isActive ? 'metadata' : 'none'
  return (
    <>
      {isUsingPreview && playback ? (
        <>
          <audio
            ref={registerPreview}
            hidden
            preload="metadata"
            src={playback.playbackPath}
            onLoadedMetadata={(event) => {
              previewReady.current?.resolve(event.currentTarget)
              previewReady.current = null
            }}
            onError={() => {
              const error = new Error(t('recordings.previewPlaybackError'))
              setPlaybackError(error.message)
              previewReady.current?.reject(error)
              previewReady.current = null
            }}
          />
        </>
      ) : media.originalContentType.startsWith('video/') ? (
        <video
          ref={registerPlayer}
          className="recording-player"
          hidden={!isActive}
          playsInline
          preload={preload}
          src={recordingMediaPath(media.id)}
          onError={(event) => originalError(event.currentTarget)}
          onLoadedMetadata={(event) => {
            if (event.currentTarget.videoWidth === 0) unsupportedOriginal(t('recordings.videoUnavailable'))
          }}
        />
      ) : (
        <audio
          ref={registerPlayer}
          hidden
          preload={preload}
          src={recordingMediaPath(media.id)}
          onError={(event) => originalError(event.currentTarget)}
        />
      )}
      {isActive && playback && isUsingPreview && !hasUnsupportedOriginal ? (
        <div className="page-actions">
          <button
            type="button"
            disabled={isPreparing}
            onClick={() => {
              passagePlayback.stop()
              setIsUsingPreview(false)
              setPlaybackError(null)
            }}
          >
            {t('recordings.useOriginal')}
          </button>
        </div>
      ) : null}
      {isActive && isOriginalUnplayable && (!isUsingPreview || playbackError) ? (
        <>
          {!playbackError ? <p className="muted">{t('recordings.previewHint')}</p> : null}
          <div className="page-actions">
            <button type="button" disabled={isPreparing} onClick={() => void playCompatible()}>
              {t(
                isPreparing
                  ? 'recordings.preparingAudio'
                  : hasUnsupportedOriginal
                    ? 'recordings.playCompatibleAudio'
                    : 'recordings.useAudioPreview',
              )}
            </button>
          </div>
        </>
      ) : null}
      {isActive ? <ErrorMessage error={playbackError} /> : null}
    </>
  )
}
