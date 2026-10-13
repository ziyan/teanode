import { useCallback, useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { graphql } from '../api'
import { ErrorMessage, Loading, Tag } from '../components/common'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { useBreadcrumbDetail } from '../components/breadcrumb'
import { TrashIcon } from '../components/icons'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../components/settingsList'
import { Pager } from '../components/pager'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'
import { useSession } from '../session'
import { RecordingProcessingPanel } from '../recording/processingPanel'
import { RecordingPassagePlayback } from '../recording/playback'
import {
  importRecording,
  readRecording,
  recordingMediaPath,
  RECORDING_DOCUMENTS,
  type Recording,
  type RecordingMedia,
} from '../recording/api'

const errorText = (caught: unknown) => (caught instanceof Error ? caught.message : String(caught))

export function RecordingsPage() {
  const session = useSession()
  return <AccountRecordingsPage key={session.userId ?? ''} />
}

function AccountRecordingsPage() {
  const { t } = useTranslation(),
    session = useSession(),
    navigate = useNavigate()
  const { recordingId } = useParams()
  const passagePlayback = useRef(new RecordingPassagePlayback())
  const [passagePlaybackError, setPassagePlaybackError] = useState<string | null>(null)
  useEffect(() => {
    const playback = passagePlayback.current
    setPassagePlaybackError(null)
    return () => playback.stop()
  }, [recordingId])
  const [pageParameters, setPageParameters] = useSearchParams()
  const pageCursors = ['', ...(pageParameters.get('before')?.split(',').filter(Boolean) ?? [])]
  const setPageCursors = (nextCursors: string[]) => {
    const nextParameters = new URLSearchParams(pageParameters)
    if (nextCursors.length > 1) nextParameters.set('before', nextCursors.slice(1).join(','))
    else nextParameters.delete('before')
    setPageParameters(nextParameters)
  }
  const beforeRecordingId = pageCursors[pageCursors.length - 1]
  const library = useQuery(
    () =>
      graphql<{ ListRecordings: Recording[] }>(RECORDING_DOCUMENTS.ListRecordings, { beforeRecordingId, pageSize: 26 }),
    [beforeRecordingId, session.userId],
  )
  const detail = useQuery(
    async () => (recordingId ? await readRecording(recordingId) : null),
    [recordingId, session.userId],
  )
  const [isImporting, setIsImporting] = useState(false),
    [originalFile, setOriginalFile] = useState<File | null>(null)
  const [resumeMedia, setResumeMedia] = useState<RecordingMedia | null>(null)
  const [recordingTitle, setRecordingTitle] = useState(''),
    [isUploading, setIsUploading] = useState(false)
  const [uploadProgress, setUploadProgress] = useState<{
    completedBytes: number
    totalBytes: number
    isHashing: boolean
  } | null>(null)
  const [uploadError, setUploadError] = useState<string | null>(null),
    [isDeleting, setIsDeleting] = useState(false),
    [deleteError, setDeleteError] = useState<string | null>(null),
    [isDeletingBusy, setIsDeletingBusy] = useState(false)
  const uploadController = useRef<AbortController | null>(null)
  useEffect(() => () => uploadController.current?.abort(), [session.userId])
  useBreadcrumbDetail(
    detail.data && detail.data.recording.id === recordingId && detail.data.recording.userId === session.userId
      ? detail.data.recording.recordingTitle
      : null,
  )
  const upload = async () => {
    if (!originalFile || !session.userId) return
    const controller = new AbortController()
    uploadController.current = controller
    setIsUploading(true)
    setUploadError(null)
    try {
      const view = await importRecording(
        originalFile,
        recordingTitle || originalFile.name,
        session.userId,
        controller.signal,
        (completedBytes, totalBytes, isHashing) => setUploadProgress({ completedBytes, totalBytes, isHashing }),
        resumeMedia,
      )
      setIsImporting(false)
      setOriginalFile(null)
      setUploadProgress(null)
      await library.reload()
      navigate(`/recordings/${view.recording.id}`)
    } catch (caught) {
      setUploadError(controller.signal.aborted ? t('recordings.uploadStopped') : errorText(caught))
      await library.reload()
      await detail.reload()
    } finally {
      setIsUploading(false)
      uploadController.current = null
    }
  }
  const remove = async () => {
    if (!recordingId) return
    setIsDeletingBusy(true)
    setDeleteError(null)
    try {
      await graphql(RECORDING_DOCUMENTS.DeleteRecording, { recordingId })
      setIsDeleting(false)
      await library.reload()
      navigate('/recordings')
    } catch (caught) {
      setDeleteError(errorText(caught))
    } finally {
      setIsDeletingBusy(false)
    }
  }
  const showImport = (media?: RecordingMedia) => {
    setResumeMedia(media ?? null)
    setOriginalFile(null)
    setIsImporting(true)
    setUploadError(null)
    setUploadProgress(null)
  }
  const recordings = (library.data?.ListRecordings ?? []).filter((recording) => recording.userId === session.userId)
  const view =
    detail.data && detail.data.recording.id === recordingId && detail.data.recording.userId === session.userId
      ? detail.data
      : null
  return (
    <div className="recording-page">
      <ErrorMessage error={recordingId ? detail.error : library.error} />
      {!recordingId ? (
        <SettingsSection
          card
          description={t('recordings.hint')}
          action={
            <button type="button" className="primary" onClick={() => showImport()}>
              {t('recordings.import')}
            </button>
          }
        >
          {library.loading && !library.data ? (
            <Loading />
          ) : recordings.length === 0 ? (
            <SettingsEmpty>{t('recordings.empty')}</SettingsEmpty>
          ) : (
            recordings
              .slice(0, 25)
              .map((recording) => (
                <SettingsRow
                  key={recording.id}
                  title={<Link to={`/recordings/${recording.id}`}>{recording.recordingTitle}</Link>}
                  subtitle={
                    recording.recordingOrigin === 'capture' ? t('recordings.captured') : t('recordings.imported')
                  }
                  badge={
                    <Tag
                      value={t(`recordings.capture.${recording.recordingCaptureState}` as Parameters<typeof t>[0])}
                    />
                  }
                />
              ))
          )}
          {recordings.length > 0 && (
            <Pager
              pageIndex={pageCursors.length - 1}
              pageSize={25}
              shownCount={Math.min(recordings.length, 25)}
              totalCount={(pageCursors.length - 1) * 25 + recordings.length}
              isTotalCountLowerBound={recordings.length > 25}
              hasNextPage={recordings.length > 25}
              isLoading={library.loading}
              onPageIndex={(nextPageIndex) =>
                setPageCursors(
                  nextPageIndex < pageCursors.length - 1
                    ? pageCursors.slice(0, -1)
                    : [...pageCursors, recordings[24].id],
                )
              }
            />
          )}
        </SettingsSection>
      ) : detail.loading && !view ? (
        <Loading />
      ) : (
        view && (
          <>
            <SettingsSection
              card
              title={t('recordings.originals')}
              description={t('recordings.originalsHint')}
              action={
                <button
                  type="button"
                  className="icon-action danger"
                  title={t('common.delete')}
                  aria-label={t('common.delete')}
                  onClick={() => {
                    setIsDeleting(true)
                    setDeleteError(null)
                  }}
                >
                  <TrashIcon size={16} />
                </button>
              }
            >
              {view.mediaFiles.length === 0 ? (
                <SettingsEmpty>{t('recordings.noOriginals')}</SettingsEmpty>
              ) : (
                view.mediaFiles.map((media) => (
                  <div className="recording-original" key={media.id}>
                    <SettingsRow
                      title={media.originalFilename}
                      subtitle={t('recordings.size', { byteCount: media.originalSizeBytes.toLocaleString() })}
                      badge={
                        <Tag
                          value={
                            media.recordingUploadState === 'complete'
                              ? t('recordings.verified')
                              : t('recordings.pending')
                          }
                          tone={media.recordingUploadState === 'complete' ? 'good' : 'warn'}
                        />
                      }
                    />
                    {media.recordingUploadState === 'complete' ? (
                      <>
                        <RecordingOriginalPlayer media={media} passagePlayback={passagePlayback.current} />
                        <a className="button" href={recordingMediaPath(media.id)} download={media.originalFilename}>
                          {t('recordings.download')}
                        </a>
                      </>
                    ) : (
                      <>
                        <p className="muted">{t('recordings.resumeHint')}</p>
                        <button type="button" onClick={() => showImport(media)}>
                          {t('recordings.resume')}
                        </button>
                      </>
                    )}
                  </div>
                ))
              )}
            </SettingsSection>
            {view.recording.captureGaps.length > 0 && (
              <SettingsSection card title={t('recordings.gaps')}>
                {view.recording.captureGaps.map((gap) => (
                  <SettingsRow
                    key={gap.gapId}
                    title={gap.recordingGapReason === 'paused' ? t('recordings.paused') : t('recordings.interrupted')}
                    subtitle={t('recordings.gapRange', {
                      startSeconds: Math.floor(gap.gapStartMS / 1000),
                      endSeconds: Math.floor(gap.gapEndMS / 1000),
                    })}
                  />
                ))}
              </SettingsSection>
            )}
            <RecordingProcessingPanel
              key={view.recording.id}
              recordingId={view.recording.id}
              userId={session.userId ?? ''}
              mediaFiles={view.mediaFiles}
              onMetadataChanged={detail.reload}
              onPlayPassage={(mediaId, passageStartMS, passageEndMS) => {
                const media = view.mediaFiles.find((original) => original.id === mediaId)
                setPassagePlaybackError(null)
                if (!media) return
                void passagePlayback.current
                  .play(mediaId, passageStartMS, passageEndMS, media.timelineStartMS)
                  .catch((caught) => setPassagePlaybackError(errorText(caught)))
              }}
            />
            <ErrorMessage error={passagePlaybackError} />
            <div className="page-actions">
              <button type="button" onClick={() => navigate('/recordings')}>
                {t('recordings.back')}
              </button>
            </div>
          </>
        )
      )}
      {isImporting && (
        <FormDialog
          title={resumeMedia ? t('recordings.resume') : t('recordings.import')}
          submitLabel={resumeMedia ? t('recordings.resume') : t('recordings.upload')}
          busy={isUploading}
          canSubmit={Boolean(originalFile)}
          error={uploadError}
          onSubmit={() => void upload()}
          onClose={() => {
            uploadController.current?.abort()
            setIsImporting(false)
          }}
          otherAction={
            isUploading ? (
              <button type="button" onClick={() => uploadController.current?.abort()}>
                {t('recordings.stopUpload')}
              </button>
            ) : undefined
          }
        >
          <p className="muted">{t('recordings.importHint')}</p>
          <label>
            {t('recordings.file')}
            <input
              type="file"
              accept="audio/*,video/*,.caf,.m4a,.mkv"
              disabled={isUploading}
              onChange={(event) => {
                const file = event.target.files?.[0] ?? null
                setOriginalFile(file)
                if (file) setRecordingTitle(file.name)
              }}
            />
          </label>
          {!resumeMedia && (
            <label>
              {t('recordings.name')}
              <input
                value={recordingTitle}
                disabled={isUploading}
                onChange={(event) => setRecordingTitle(event.target.value)}
              />
            </label>
          )}
          {resumeMedia && <p className="muted">{resumeMedia.originalFilename}</p>}
          {uploadProgress && (
            <div role="status">
              <p>{uploadProgress.isHashing ? t('recordings.hashing') : t('recordings.uploading')}</p>
              <progress
                className="recording-progress"
                max={uploadProgress.totalBytes}
                value={uploadProgress.completedBytes}
              />
            </div>
          )}
        </FormDialog>
      )}
      {isDeleting && (
        <ConfirmDialog
          title={t('recordings.delete')}
          body={t('recordings.deleteHint')}
          confirmLabel={t('common.delete')}
          busy={isDeletingBusy}
          error={deleteError}
          onConfirm={() => void remove()}
          onClose={() => setIsDeleting(false)}
        />
      )}
    </div>
  )
}

function RecordingOriginalPlayer({
  media,
  passagePlayback,
}: {
  media: RecordingMedia
  passagePlayback: RecordingPassagePlayback
}) {
  const { t } = useTranslation()
  const registerPlayer = useCallback(
    (player: HTMLMediaElement | null) => passagePlayback.register(media.id, player),
    [media.id, passagePlayback],
  )
  const [playbackError, setPlaybackError] = useState<string | null>(null)
  const onError = () => setPlaybackError(t('recordings.playbackError'))
  return (
    <>
      {media.originalContentType.startsWith('video/') ? (
        <video
          ref={registerPlayer}
          className="recording-player"
          controls
          preload="metadata"
          src={recordingMediaPath(media.id)}
          onError={onError}
          onLoadedMetadata={(event) => {
            if (event.currentTarget.videoWidth === 0) setPlaybackError(t('recordings.videoUnavailable'))
          }}
        />
      ) : (
        <audio
          ref={registerPlayer}
          className="recording-player"
          controls
          preload="metadata"
          src={recordingMediaPath(media.id)}
          onError={onError}
        />
      )}
      <ErrorMessage error={playbackError} />
    </>
  )
}
