import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { ErrorMessage, Loading, Tag } from '../components/common'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../components/settingsList'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'
import { type RecordingMedia } from './api'
import { RecordingSourceSchedule } from './sourceSchedule'
import { RecordingEditDialog, type ProcessingDialog } from './editDialog'
import { RecordingRowAction } from './rowAction'
import { EraserIcon, MicrophoneIcon, PencilIcon, RefreshIcon, StopIcon, TrashIcon } from '../components/icons'
import {
  passagePlaybackPath,
  recordingOperation,
  recordingSourceTitle,
  type RecordingLearning,
  type SpeakersView,
  type TextCorrection,
  type TranscriptRun,
  type TranscriptionAvailability,
} from './processing'

type ProcessingView = {
  recording: { id: string; userId: string; recordingTitle: string; recordedAt: string | null; sourceGeneration: number }
  transcriptRuns: TranscriptRun[]
}
const caughtText = (caught: unknown) => (caught instanceof Error ? caught.message : String(caught))
const timelineTime = (milliseconds: number) => {
  const remainderMS = milliseconds % 1000
  const fractionalSeconds = remainderMS ? `.${String(remainderMS).padStart(3, '0')}` : ''
  return `${Math.floor(milliseconds / 60000)}:${String(Math.floor(milliseconds / 1000) % 60).padStart(2, '0')}${fractionalSeconds}`
}

export function RecordingProcessingPanel({
  recordingId,
  userId,
  mediaFiles,
  onMetadataChanged,
  onChanged,
  onPlayPassage,
}: {
  recordingId: string
  userId: string
  mediaFiles: RecordingMedia[]
  onMetadataChanged?: () => Promise<unknown>
  // Called after any change saved from this panel, so a view of the same
  // recording elsewhere on the page can read it again.
  onChanged?: () => void
  onPlayPassage?: (mediaId: string, passageStartMS: number, passageEndMS: number) => void
}) {
  const { t } = useTranslation()
  const [isResumingReading, setIsResumingReading] = useState(false)
  const [resumeReadingError, setResumeReadingError] = useState<string | null>(null)
  const availability = useQuery(
    () => recordingOperation<TranscriptionAvailability>('ReadRecordingTranscriptionAvailability', {}),
    [userId],
  )
  const versions = useQuery(
    () => recordingOperation<ProcessingView>('ReadRecordingProcessing', { recordingId }),
    [recordingId, userId],
  )
  const owned =
    versions.data?.recording.id === recordingId && versions.data.recording.userId === userId ? versions.data : null
  const runs = owned?.transcriptRuns ?? []
  const latestRun = [...runs].sort((first, second) => second.versionNumber - first.versionNumber)[0]
  const speakers = useQuery(
    () => recordingOperation<SpeakersView>('ReadRecordingSpeakers', { recordingId }),
    [recordingId, userId],
  )
  const learning = useQuery(
    () => recordingOperation<RecordingLearning>('ReadRecordingLearning', { recordingId }),
    [recordingId, userId],
  )
  const textHistory = useQuery(
    () => recordingOperation<TextCorrection[]>('ReadRecordingTranscriptTextCorrections', { recordingId }),
    [recordingId, userId],
  )
  const [dialog, setDialog] = useState<ProcessingDialog | null>(null)
  const refresh = async () => {
    await Promise.all([
      versions.reload(),
      availability.reload(),
      speakers.reload(),
      learning.reload(),
      textHistory.reload(),
    ])
  }
  useEffect(() => {
    if (!runs.some((run) => ['queued', 'preparing', 'transcribing'].includes(run.recordingProcessingState))) return
    const timer = window.setInterval(() => {
      void versions.reload()
      void learning.reload()
    }, 4000)
    return () => window.clearInterval(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [runs.map((run) => `${run.id}:${run.recordingProcessingState}`).join(','), recordingId, userId])
  const show = (nextDialog: ProcessingDialog) => setDialog(nextDialog)
  const generation = speakers.data?.sourceGeneration ?? owned?.recording.sourceGeneration
  const resumeReading = async () => {
    setIsResumingReading(true)
    setResumeReadingError(null)
    try {
      await recordingOperation('ResumeRecordingLearning', { recordingId, expectedSourceGeneration: generation })
      await learning.reload()
    } catch (caught) {
      setResumeReadingError(caughtText(caught))
    } finally {
      setIsResumingReading(false)
    }
  }
  return (
    <>
      <SettingsSection
        card
        title={t('recordingProcessing.transcription')}
        description={t('recordingProcessing.evidenceHint')}
        action={
          <button type="button" onClick={() => void refresh()}>
            {t('recordingProcessing.refresh')}
          </button>
        }
      >
        <ErrorMessage error={versions.error} />
        <ErrorMessage error={availability.error} />
        {availability.data ? (
          <SettingsRow
            title={t(
              availability.data.isTranscriptionAvailable
                ? 'recordingProcessing.processingAvailable'
                : 'recordingProcessing.processingOff',
            )}
            subtitle={
              availability.data.transcriptionUnavailableMessage || availability.data.transcriptionProviderModelName
            }
          />
        ) : null}
        {versions.loading && !owned ? <Loading /> : null}
        {owned ? (
          <SettingsRow
            title={t('recordingProcessing.occurrence')}
            subtitle={owned.recording.recordedAt ?? t('recordingProcessing.unknownOccurrence')}
            actions={
              <RecordingRowAction
                label={t('recordingProcessing.editMetadata')}
                icon={<PencilIcon size={16} />}
                onClick={() =>
                  show({
                    operationName: 'UpdateRecordingMetadata',
                    titleKey: 'metadata',
                    variables: { recordingId, expectedSourceGeneration: generation },
                    fields: [
                      { fieldName: 'recordingTitle', labelKey: 'title', fieldValue: owned.recording.recordingTitle },
                      {
                        fieldName: 'recordedAt',
                        labelKey: 'occurrenceInput',
                        fieldValue: owned.recording.recordedAt ?? '',
                      },
                    ],
                  })
                }
              />
            }
          />
        ) : null}
        {mediaFiles
          .filter((media) => media.recordingUploadState === 'complete')
          .map((media) => (
            <SettingsRow
              key={media.id}
              title={media.originalFilename}
              subtitle={t('recordingProcessing.timelineStart', { timelineTime: timelineTime(media.timelineStartMS) })}
              actions={
                <RecordingRowAction
                  label={t(
                    runs.some((run) => run.recordingMediaId === media.id)
                      ? 'recordings.transcribeAgain'
                      : 'recordingProcessing.process',
                  )}
                  icon={<MicrophoneIcon size={16} />}
                  disabled={!owned || !availability.data?.isTranscriptionAvailable}
                  onClick={() =>
                    show({
                      operationName: 'RequestRecordingTranscription',
                      titleKey: runs.some((run) => run.recordingMediaId === media.id) ? 'reprocess' : 'process',
                      isPaid: true,
                      variables: {
                        recordingMediaId: media.id,
                        clientRunId: runs.some((run) => run.recordingMediaId === media.id)
                          ? crypto.randomUUID()
                          : 'initial',
                        isReprocessingRequested: runs.some((run) => run.recordingMediaId === media.id),
                      },
                      fields: [],
                    })
                  }
                />
              }
            />
          ))}
        {runs.length === 0 && !versions.loading ? (
          <SettingsEmpty>{t('recordingProcessing.noVersions')}</SettingsEmpty>
        ) : null}
        {latestRun ? (
          <SettingsRow
            title={t('recordingProcessing.latestTranscript')}
            subtitle={
              <>
                {mediaFiles.find((media) => media.id === latestRun.recordingMediaId)?.originalFilename} ·{' '}
                {latestRun.providerModelName || t('recordingProcessing.modelPending')}
                {latestRun.processingErrorMessage ? <p className="error">{latestRun.processingErrorMessage}</p> : null}
              </>
            }
            badge={
              <Tag
                value={t(
                  `recordingProcessing.progress.${latestRun.recordingProcessingState}` as Parameters<typeof t>[0],
                )}
              />
            }
            actions={
              <div className="row-actions">
                {['queued', 'preparing', 'transcribing'].includes(latestRun.recordingProcessingState) ? (
                  <RecordingRowAction
                    label={t('recordingProcessing.cancelProcessing')}
                    icon={<StopIcon size={16} />}
                    onClick={() =>
                      show({
                        operationName: 'CancelRecordingTranscription',
                        titleKey: 'cancelProcessing',
                        variables: { recordingTranscriptRunId: latestRun.id },
                        fields: [],
                      })
                    }
                  />
                ) : null}
                {!['complete', 'uncertain'].includes(latestRun.recordingProcessingState) ? (
                  <RecordingRowAction
                    label={t('recordingProcessing.retry')}
                    icon={<RefreshIcon size={16} />}
                    onClick={() =>
                      show({
                        operationName: 'RetryRecordingTranscription',
                        titleKey: 'retry',
                        variables: { recordingTranscriptRunId: latestRun.id },
                        fields: [],
                      })
                    }
                  />
                ) : null}
              </div>
            }
          />
        ) : null}
        <p className="muted">{t('recordingProcessing.retryHint')}</p>
      </SettingsSection>
      <SettingsSection
        card
        title={t('recordingProcessing.speakers')}
        description={t('recordingProcessing.speakerHint')}
      >
        <ErrorMessage error={speakers.error} />
        {(speakers.data?.speakerIdentities ?? [])
          .filter((identity) =>
            speakers.data?.speakerCorrections.some((correction) => correction.speakerIdentityId === identity.id),
          )
          .map((identity) => (
            <SettingsRow
              key={identity.id}
              title={identity.confirmedName}
              actions={
                <RecordingRowAction
                  label={t('recordingProcessing.renameSpeaker')}
                  icon={<PencilIcon size={16} />}
                  onClick={() =>
                    show({
                      operationName: 'RenameRecordingSpeaker',
                      titleKey: 'renameSpeaker',
                      variables: { recordingId, expectedSourceGeneration: generation, speakerIdentityId: identity.id },
                      fields: [
                        { fieldName: 'confirmedName', labelKey: 'confirmedName', fieldValue: identity.confirmedName },
                      ],
                    })
                  }
                />
              }
            />
          ))}
        {(speakers.data?.speakerCorrections ?? [])
          .filter((correction) => !correction.isRemoved)
          .map((correction) => (
            <SettingsRow
              key={correction.id}
              title={
                speakers.data?.speakerIdentities.find((identity) => identity.id === correction.speakerIdentityId)
                  ?.confirmedName ?? t('recordingProcessing.speakerUncertain')
              }
              subtitle={
                <a
                  href={passagePlaybackPath(
                    correction.recordingMediaId,
                    correction.passageStartMS,
                    correction.passageEndMS,
                    mediaFiles.find((media) => media.id === correction.recordingMediaId)?.timelineStartMS ?? 0,
                  )}
                  onClick={(event) => {
                    if (!onPlayPassage || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
                    event.preventDefault()
                    onPlayPassage(correction.recordingMediaId, correction.passageStartMS, correction.passageEndMS)
                  }}
                  target="_blank"
                  rel="noreferrer"
                >
                  {t('recordingProcessing.passageRange', {
                    startTime: timelineTime(correction.passageStartMS),
                    endTime: timelineTime(correction.passageEndMS),
                  })}
                </a>
              }
              actions={
                <RecordingRowAction
                  label={t('recordingProcessing.resetSpeaker')}
                  icon={<TrashIcon size={16} />}
                  isDestructive
                  onClick={() =>
                    show({
                      operationName: 'RemoveRecordingSpeakerCorrection',
                      titleKey: 'resetSpeaker',
                      variables: {
                        recordingId,
                        expectedSourceGeneration: generation,
                        recordingSpeakerCorrectionId: correction.id,
                      },
                      fields: [],
                    })
                  }
                />
              }
            />
          ))}
        <h3>{t('recordingProcessing.references')}</h3>
        {(speakers.data?.voiceReferences ?? [])
          .filter((reference) => !reference.isRevoked)
          .map((reference) => (
            <SettingsRow
              key={reference.id}
              title={
                speakers.data?.speakerIdentities.find((identity) => identity.id === reference.speakerIdentityId)
                  ?.confirmedName ?? t('recordingProcessing.speakerUncertain')
              }
              subtitle={
                <a
                  href={passagePlaybackPath(
                    reference.recordingMediaId,
                    reference.passageStartMS,
                    reference.passageEndMS,
                    mediaFiles.find((media) => media.id === reference.recordingMediaId)?.timelineStartMS ?? 0,
                  )}
                  onClick={(event) => {
                    if (!onPlayPassage || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
                    event.preventDefault()
                    onPlayPassage(reference.recordingMediaId, reference.passageStartMS, reference.passageEndMS)
                  }}
                  target="_blank"
                  rel="noreferrer"
                >
                  {t('recordingProcessing.playOriginalPassage')}{' '}
                  {t('recordingProcessing.passageRange', {
                    startTime: timelineTime(reference.passageStartMS),
                    endTime: timelineTime(reference.passageEndMS),
                  })}
                </a>
              }
              actions={
                <RecordingRowAction
                  label={t('recordingProcessing.deleteReference')}
                  icon={<TrashIcon size={16} />}
                  isDestructive
                  onClick={() =>
                    show({
                      operationName: 'DeleteRecordingVoiceReference',
                      titleKey: 'deleteReference',
                      isDestructive: true,
                      variables: {
                        recordingId,
                        expectedSourceGeneration: generation,
                        recordingVoiceReferenceId: reference.id,
                      },
                      fields: [],
                    })
                  }
                />
              }
            />
          ))}
        {(speakers.data?.voiceReferences ?? []).every((reference) => reference.isRevoked) ? (
          <SettingsEmpty>{t('recordingProcessing.noReferences')}</SettingsEmpty>
        ) : null}
      </SettingsSection>
      <SettingsSection
        card
        title={t('recordingProcessing.learning')}
        description={t('recordingProcessing.learningHint')}
      >
        <ErrorMessage error={learning.error} />
        <ErrorMessage error={resumeReadingError} />
        {learning.data ? (
          <>
            <p>
              {t('recordingProcessing.learningCounts', {
                pendingCount: learning.data.pendingDocumentCount,
                digestedCount: learning.data.digestedDocumentCount,
              })}
            </p>
            {learning.data.isCurationPending ? <p>{t('recordingProcessing.curationPending')}</p> : null}
            {learning.data.knowledgeSource ? (
              <SettingsRow
                title={
                  <Link to={`/settings/agent/sources#source-${encodeURIComponent(learning.data.knowledgeSource.id)}`}>
                    {recordingSourceTitle(learning.data.knowledgeSource.specification?.settings) ??
                      owned?.recording.recordingTitle ??
                      t('recordingProcessing.untitledRecording')}
                  </Link>
                }
                subtitle={
                  learning.data.knowledgeSource.lastError ||
                  t(
                    learning.data.knowledgeSource.enabled
                      ? 'recordingProcessing.sourceEnabled'
                      : 'recordingProcessing.sourcePaused',
                  )
                }
                actions={
                  <RecordingSourceSchedule
                    sourceId={learning.data.knowledgeSource.id}
                    sourceCron={learning.data.knowledgeSource.cron}
                    sourceTitle={owned?.recording.recordingTitle ?? t('recordingProcessing.untitledRecording')}
                    isIconOnly
                    onSaved={learning.reload}
                  />
                }
              />
            ) : (
              <p>
                {t(
                  learning.data.hasStoppedReading
                    ? 'recordingProcessing.sourceStopped'
                    : learning.data.hasAgent
                      ? 'recordingProcessing.noPublishedSource'
                      : 'recordingProcessing.noAgent',
                )}
              </p>
            )}
            {learning.data.hasStoppedReading ? (
              <button
                type="button"
                disabled={isResumingReading || generation === undefined}
                onClick={() => void resumeReading()}
              >
                {t('recordingProcessing.resumeReading')}
              </button>
            ) : null}
          </>
        ) : null}
      </SettingsSection>
      <SettingsSection
        card
        title={t('recordingProcessing.textHistory')}
        description={t('recordingProcessing.textHistoryHint')}
      >
        <ErrorMessage error={textHistory.error} />
        {!textHistory.loading && !textHistory.error && (textHistory.data ?? []).length === 0 ? (
          <SettingsEmpty>{t('recordingProcessing.noTextCorrections')}</SettingsEmpty>
        ) : null}
        {(textHistory.data ?? []).map((correction) => (
          <details key={correction.id}>
            <summary>
              {t(
                correction.isRemoved
                  ? 'recordingProcessing.correctionRemoved'
                  : 'recordingProcessing.retainedTextCorrection',
              )}{' '}
              {t('recordingProcessing.passageRange', {
                startTime: timelineTime(correction.passageStartMS),
                endTime: timelineTime(correction.passageEndMS),
              })}
            </summary>
            <p className="muted">
              {t('recordingProcessing.generatedOriginal')}: {correction.originalTranscriptText}
            </p>
            <p>
              {t('recordingProcessing.correctedText')}: {correction.correctedTranscriptText}
            </p>
            {!correction.isRemoved ? (
              <RecordingRowAction
                label={t('recordingProcessing.resetText')}
                icon={<EraserIcon size={16} />}
                onClick={() =>
                  show({
                    operationName: 'RemoveRecordingTranscriptTextCorrection',
                    titleKey: 'resetText',
                    variables: {
                      recordingId,
                      expectedSourceGeneration: generation,
                      recordingTranscriptTextCorrectionId: correction.id,
                    },
                    fields: [],
                  })
                }
              />
            ) : null}
          </details>
        ))}
      </SettingsSection>
      {dialog ? (
        <RecordingEditDialog
          dialog={dialog}
          speakerNames={(speakers.data?.speakerIdentities ?? []).map((identity) => identity.confirmedName)}
          onClose={() => setDialog(null)}
          onSaved={async (operationName) => {
            if (operationName === 'UpdateRecordingMetadata') await onMetadataChanged?.()
            await refresh()
            onChanged?.()
          }}
        />
      ) : null}
    </>
  )
}
