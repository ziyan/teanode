import { useCallback, useEffect, useRef, useState } from 'react'
import { ErrorMessage } from '../components/common'
import { useTranslation } from '../i18n/i18n'
import { recordingMediaPath, type RecordingMedia } from './api'
import { RecordingPassagePlayback } from './playback'
import { prepareRecordingPlayback, type RecordingPlayback } from './playbackPreview'

export function RecordingOriginalPlayer({
  media,
  passagePlayback,
}: {
  media: RecordingMedia
  passagePlayback: RecordingPassagePlayback
}) {
  const { t } = useTranslation()
  const mediaId = media.id
  const timelineStartMS = media.timelineStartMS
  const [playback, setPlayback] = useState<RecordingPlayback | null>(null)
  const [isPreparing, setIsPreparing] = useState(false)
  const [playbackError, setPlaybackError] = useState<string | null>(null)
  const [hasUnsupportedOriginal, setHasUnsupportedOriginal] = useState(false)
  const isMounted = useRef(false)
  const hasUnsupportedOriginalReference = useRef(false)
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
    const pendingPreparation = prepareRecordingPlayback({ id: mediaId, timelineStartMS }, controller.signal)
      .then(async (compatiblePlayback) => {
        if (!isMounted.current || controller.signal.aborted) throw new DOMException('Playback canceled', 'AbortError')
        const readyPlayer = new Promise<HTMLMediaElement>((resolve, reject) => {
          previewReady.current = { resolve, reject }
        })
        setPlayback(compatiblePlayback)
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
  const unsupportedOriginal = (errorMessage: string) => {
    hasUnsupportedOriginalReference.current = true
    setHasUnsupportedOriginal(true)
    setPlaybackError(errorMessage)
  }
  const originalError = (player: HTMLMediaElement) => {
    if (player.error?.code === 4) unsupportedOriginal(t('recordings.playbackError'))
    else setPlaybackError(t('recordings.playbackError'))
  }
  const playCompatible = async () => {
    try {
      await passagePlayback.playCompatible(media.id)
    } catch (caught) {
      if (isMounted.current) setPlaybackError(caught instanceof Error ? caught.message : String(caught))
    }
  }
  return (
    <>
      {playback ? (
        <>
          <p className="muted">{t('recordings.compatibleAudio')}</p>
          <audio
            ref={registerPreview}
            className="recording-player"
            controls
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
          controls
          preload="metadata"
          src={recordingMediaPath(media.id)}
          onError={(event) => originalError(event.currentTarget)}
          onLoadedMetadata={(event) => {
            if (event.currentTarget.videoWidth === 0) unsupportedOriginal(t('recordings.videoUnavailable'))
          }}
        />
      ) : (
        <audio
          ref={registerPlayer}
          className="recording-player"
          controls
          preload="metadata"
          src={recordingMediaPath(media.id)}
          onError={(event) => originalError(event.currentTarget)}
        />
      )}
      {hasUnsupportedOriginal && (!playback || playbackError) ? (
        <>
          <p className="muted">{t('recordings.previewHint')}</p>
          <div className="page-actions">
            <button type="button" disabled={isPreparing} onClick={() => void playCompatible()}>
              {t(isPreparing ? 'recordings.preparingAudio' : 'recordings.playCompatibleAudio')}
            </button>
          </div>
        </>
      ) : null}
      <div className="page-actions">
        <a className="button" href={recordingMediaPath(media.id)} download={media.originalFilename}>
          {t('recordings.download')}
        </a>
        {playback ? (
          <a
            className="button"
            href={playback.playbackPath}
            download={`${media.originalFilename.replace(/\.[^.]+$/, '')}.preview.wav`}
          >
            {t('recordings.downloadPreview')}
          </a>
        ) : null}
      </div>
      <ErrorMessage error={playbackError} />
    </>
  )
}
