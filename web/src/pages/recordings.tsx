import { useEffect, useRef, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { graphql } from '../api'
import { ErrorMessage, Loading, Tag } from '../components/common'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { useBreadcrumbDetail } from '../components/breadcrumb'
import { MoreIcon } from '../components/icons'
import { MenuButton } from '../components/menuButton'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../components/settingsList'
import { Pager } from '../components/pager'
import { useToast } from '../components/toast'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'
import { useSession } from '../session'
import { RecordingProcessingPanel } from '../recording/processingPanel'
import { RecordingPassagePlayback } from '../recording/playback'
import { RecordingOriginalPlayer } from '../recording/originalPlayer'
import { RecordingConversation } from '../recording/conversationView'
import { RecordingDetailsDrawer } from '../recording/detailsDrawer'
import { RecordingEditDialog, type ProcessingDialog } from '../recording/editDialog'
import { formatRecordingLength, readRecordingStatusEvidence, recordingStatus } from '../recording/library'
import { RecordedAtText, RecordingLibraryTable, RecordingStatusTag } from '../recording/libraryList'
import { graphqlWithOptionalFields } from '../recording/optionalFields'
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
  const { t, plural } = useTranslation(),
    session = useSession(),
    toast = useToast(),
    navigate = useNavigate()
  const { recordingId } = useParams()
  const passagePlayback = useRef(new RecordingPassagePlayback())
  const [conversationReloadToken, setConversationReloadToken] = useState(0),
    [isShowingDetails, setIsShowingDetails] = useState(false),
    [editDialog, setEditDialog] = useState<ProcessingDialog | null>(null),
    [speakerCount, setSpeakerCount] = useState(0)
  useEffect(() => {
    const playback = passagePlayback.current
    setIsShowingDetails(false)
    setSpeakerCount(0)
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
      graphqlWithOptionalFields<{ ListRecordings: Recording[] }>(RECORDING_DOCUMENTS.ListRecordings, {
        beforeRecordingId,
        pageSize: 51,
      }),
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
      ? detail.data.recording.recordingTitle || t('recordings.untitled')
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
      toast.done(t('recordings.deleted'))
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
  const shownRecordings = recordings.slice(0, 50)
  const evidence = useQuery(
    () => readRecordingStatusEvidence(shownRecordings.map((recording) => recording.id)),
    [shownRecordings.map((recording) => recording.id).join(','), session.userId],
  )
  const completeMedia = view?.mediaFiles.filter((media) => media.recordingUploadState === 'complete') ?? []
  const incompleteMedia = view?.mediaFiles.filter((media) => media.recordingUploadState !== 'complete') ?? []
  const detailStatus = view
    ? recordingStatus(view.recording, { mediaFiles: view.mediaFiles, transcriptRuns: view.transcriptRuns ?? [] })
    : null
  // A transcript that is still being made is read again until it is ready, so
  // the page fills in by itself.
  useEffect(() => {
    if (detailStatus !== 'transcribing') return
    const timer = window.setInterval(() => {
      void detail.reload()
      setConversationReloadToken((previous) => previous + 1)
    }, 4000)
    return () => window.clearInterval(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [detailStatus, recordingId])
  const changed = () => {
    void detail.reload()
    setConversationReloadToken((previous) => previous + 1)
  }
  const openEdit = (nextDialog: ProcessingDialog) => setEditDialog(nextDialog)
  const rename = () =>
    view &&
    openEdit({
      operationName: 'UpdateRecordingMetadata',
      titleKey: 'metadata',
      titleText: t('recordings.renameTitle'),
      variables: {
        recordingId: view.recording.id,
        expectedSourceGeneration: view.recording.sourceGeneration,
        recordedAt: view.recording.recordedAt,
      },
      fields: [{ fieldName: 'recordingTitle', labelKey: 'title', fieldValue: view.recording.recordingTitle }],
    })
  const transcribeAgain = () => {
    const media = completeMedia[0]
    if (!view || !media) return
    const hasRun = (view.transcriptRuns ?? []).some((run) => run.recordingMediaId === media.id)
    openEdit({
      operationName: 'RequestRecordingTranscription',
      titleKey: hasRun ? 'reprocess' : 'process',
      titleText: t(hasRun ? 'recordings.transcribeAgain' : 'recordingProcessing.process'),
      isPaid: true,
      variables: {
        recordingMediaId: media.id,
        clientRunId: hasRun ? crypto.randomUUID() : 'initial',
        isReprocessingRequested: hasRun,
      },
      fields: [],
    })
  }
  return (
    <div className="recording-page">
      <ErrorMessage error={recordingId ? detail.error : library.error} />
      {!recordingId ? (
        <>
          <div className="page-actions page-actions-end">
            <button type="button" className="primary" onClick={() => showImport()}>
              {t('recordings.import')}
            </button>
          </div>
          {library.loading && !library.data ? (
            <Loading />
          ) : (
            <RecordingLibraryTable recordings={shownRecordings} evidenceByRecordingId={evidence.data ?? {}} />
          )}
          {(pageCursors.length > 1 || recordings.length > 50) && (
            <Pager
              pageIndex={pageCursors.length - 1}
              pageSize={50}
              shownCount={Math.min(recordings.length, 50)}
              totalCount={(pageCursors.length - 1) * 50 + recordings.length}
              isTotalCountLowerBound={recordings.length > 50}
              hasNextPage={recordings.length > 50}
              isLoading={library.loading}
              onPageIndex={(nextPageIndex) =>
                setPageCursors(
                  nextPageIndex < pageCursors.length - 1
                    ? pageCursors.slice(0, -1)
                    : [...pageCursors, recordings[49].id],
                )
              }
            />
          )}
        </>
      ) : detail.loading && !view ? (
        <Loading />
      ) : (
        view && (
          <div className="recording-detail">
            <div className="recording-header">
              <div className="recording-header-text">
                <h2>{view.recording.recordingTitle || t('recordings.untitled')}</h2>
                <p className="recording-meta">
                  <RecordedAtText recordedAt={view.recording.recordedAt} />
                  {view.recording.timelineDurationMS > 0 ? (
                    <span> · {formatRecordingLength(view.recording.timelineDurationMS)}</span>
                  ) : null}
                  {speakerCount > 0 ? (
                    <span>
                      {' · '}
                      {plural(speakerCount, {
                        one: 'recordings.speakerCountOne',
                        other: 'recordings.speakerCountOther',
                      })}
                    </span>
                  ) : null}
                  {detailStatus && detailStatus !== 'ready' ? (
                    <>
                      {' '}
                      <RecordingStatusTag status={detailStatus} />
                    </>
                  ) : null}
                </p>
              </div>
              <MenuButton
                label={t('recordings.menu')}
                className="icon-button"
                icon={<MoreIcon size={18} />}
                render={(close) => (
                  <>
                    <button
                      type="button"
                      role="menuitem"
                      onClick={() => {
                        close()
                        rename()
                      }}
                    >
                      {t('common.rename')}
                    </button>
                    {completeMedia[0] ? (
                      <a
                        role="menuitem"
                        href={recordingMediaPath(completeMedia[0].id)}
                        download={completeMedia[0].originalFilename}
                        onClick={close}
                      >
                        {t('recordings.download')}
                      </a>
                    ) : null}
                    <button
                      type="button"
                      role="menuitem"
                      disabled={!completeMedia[0]}
                      onClick={() => {
                        close()
                        transcribeAgain()
                      }}
                    >
                      {t('recordings.transcribeAgain')}
                    </button>
                    <button
                      type="button"
                      role="menuitem"
                      onClick={() => {
                        close()
                        setIsShowingDetails(true)
                      }}
                    >
                      {t('recordings.details')}
                    </button>
                    <div className="menu-separator" />
                    <button
                      type="button"
                      role="menuitem"
                      onClick={() => {
                        close()
                        setIsDeleting(true)
                        setDeleteError(null)
                      }}
                    >
                      {t('common.delete')}
                    </button>
                  </>
                )}
              />
            </div>
            {view.recording.recordingSummary ? (
              <section className="recording-summary">
                <h3>{t('recordings.summary')}</h3>
                <p>{view.recording.recordingSummary}</p>
              </section>
            ) : null}
            {completeMedia.map((media) => (
              <div className="recording-original" key={media.id}>
                <RecordingOriginalPlayer media={media} passagePlayback={passagePlayback.current} />
              </div>
            ))}
            {incompleteMedia.map((media) => (
              <div className="recording-original" key={media.id}>
                <p className="muted">
                  {media.originalFilename}: {t('recordings.resumeHint')}
                </p>
                <button type="button" onClick={() => showImport(media)}>
                  {t('recordings.resume')}
                </button>
              </div>
            ))}
            <RecordingConversation
              key={view.recording.id}
              recordingId={view.recording.id}
              userId={session.userId ?? ''}
              mediaFiles={view.mediaFiles}
              passagePlayback={passagePlayback.current}
              reloadToken={conversationReloadToken}
              onChanged={changed}
              onSpeakerCount={setSpeakerCount}
            />
          </div>
        )
      )}
      {view && isShowingDetails && (
        <RecordingDetailsDrawer onClose={() => setIsShowingDetails(false)}>
          <SettingsSection card title={t('recordings.originals')} description={t('recordings.originalsHint')}>
            {view.mediaFiles.length === 0 ? (
              <SettingsEmpty>{t('recordings.noOriginals')}</SettingsEmpty>
            ) : (
              view.mediaFiles.map((media) => (
                <SettingsRow
                  key={media.id}
                  title={media.originalFilename}
                  subtitle={t('recordings.size', { byteCount: media.originalSizeBytes.toLocaleString() })}
                  badge={
                    <Tag
                      value={
                        media.recordingUploadState === 'complete' ? t('recordings.verified') : t('recordings.pending')
                      }
                      tone={media.recordingUploadState === 'complete' ? 'good' : 'warn'}
                    />
                  }
                />
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
            onChanged={changed}
            onPlayPassage={(mediaId, passageStartMS, passageEndMS) => {
              const media = view.mediaFiles.find((original) => original.id === mediaId)
              if (!media) return
              void passagePlayback.current
                .play(mediaId, passageStartMS, passageEndMS, media.timelineStartMS)
                .catch((caught) => toast.failure(caught, errorText(caught)))
            }}
          />
        </RecordingDetailsDrawer>
      )}
      {editDialog && (
        <RecordingEditDialog
          dialog={editDialog}
          speakerNames={[]}
          onClose={() => setEditDialog(null)}
          onSaved={async () => changed()}
        />
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
