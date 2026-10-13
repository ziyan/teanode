import { useEffect, useId, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { ErrorMessage, Loading, Tag } from '../components/common'
import { FormDialog } from '../components/dialog'
import { Pager, usePageInAddress } from '../components/pager'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../components/settingsList'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'
import { type RecordingMedia } from './api'
import { RECORDING_PROCESSING_DOCUMENTS } from './processingDocuments'
import { RecordingSourceSchedule } from './sourceSchedule'
import { overlapsRecordingCitation, readRecordingCitation } from './citation'
import { RecordingRowAction } from './rowAction'
import {
  AddPersonIcon,
  EraserIcon,
  MicrophoneIcon,
  PencilIcon,
  RefreshIcon,
  StopIcon,
  TrashIcon,
} from '../components/icons'
import {
  confirmedSegmentName,
  passagePlaybackPath,
  recordingOperation,
  recordingSourceTitle,
  type RecordingLearning,
  type ReferenceInvalidation,
  type SpeakersView,
  type TextCorrection,
  type TranscriptRun,
  type TranscriptionAvailability,
  type TranscriptView,
  type TranscriptTimeline,
} from './processing'

type DialogField = {
  fieldName: string
  labelKey: string
  inputType?: 'number' | 'text' | 'textarea'
  fieldValue: string
}
type ProcessingDialog = {
  operationName: keyof typeof RECORDING_PROCESSING_DOCUMENTS
  titleKey: string
  variables: Record<string, unknown>
  fields: DialogField[]
  isPaid?: boolean
  isDestructive?: boolean
}
type ProcessingView = {
  recording: { id: string; userId: string; recordingTitle: string; recordedAt: string | null; sourceGeneration: number }
  transcriptRuns: TranscriptRun[]
}
const caughtText = (caught: unknown) => (caught instanceof Error ? caught.message : String(caught))
const timelineTime = (milliseconds: number) => {
  const remainderMS = milliseconds % 1000
  const fractionalSeconds = remainderMS ? `.${String(remainderMS).padStart(3, '0').replace(/0+$/, '')}` : ''
  return `${Math.floor(milliseconds / 60000)}:${String(Math.floor(milliseconds / 1000) % 60).padStart(2, '0')}${fractionalSeconds}`
}

export function RecordingProcessingPanel({
  recordingId,
  userId,
  mediaFiles,
  onMetadataChanged,
  onPlayPassage,
}: {
  recordingId: string
  userId: string
  mediaFiles: RecordingMedia[]
  onMetadataChanged?: () => Promise<unknown>
  onPlayPassage?: (mediaId: string, passageStartMS: number, passageEndMS: number) => void
}) {
  const { t } = useTranslation()
  const speakerNamesListId = useId()
  const [parameters, setParameters] = useSearchParams()
  const [isResumingReading, setIsResumingReading] = useState(false)
  const [resumeReadingError, setResumeReadingError] = useState<string | null>(null)
  const citation = readRecordingCitation(parameters)
  const citationFocusKey = `${userId}/${recordingId}/${citation.requestedRunId}/${citation.passageStartMS}/${citation.passageEndMS}`
  const appliedCitationFocus = useRef('')
  const citedPassageElement = useRef<HTMLSpanElement>(null)
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
  const selectedRun = runs.find((run) => run.id === citation.requestedRunId)
  const page = usePageInAddress({ pageParameter: 'transcriptPage', defaultPageSize: 50 })
  const { setPageIndex } = page
  const citationOffset = useQuery(
    () =>
      selectedRun && citation.passageStartMS !== null && citation.passageEndMS !== null
        ? recordingOperation<number>('ReadRecordingTranscriptPassageOffset', {
            recordingTranscriptRunId: selectedRun.id,
            passageStartMS: citation.passageStartMS,
            passageEndMS: citation.passageEndMS,
          }).then((segmentOffset) => ({
            recordingTranscriptRunId: selectedRun.id,
            passageStartMS: citation.passageStartMS,
            passageEndMS: citation.passageEndMS,
            segmentOffset,
          }))
        : Promise.resolve(null),
    [selectedRun?.id, citation.passageStartMS, citation.passageEndMS, userId],
  )
  const ownedCitationOffset =
    citationOffset.data?.recordingTranscriptRunId === selectedRun?.id &&
    citationOffset.data?.passageStartMS === citation.passageStartMS &&
    citationOffset.data?.passageEndMS === citation.passageEndMS
      ? (citationOffset.data?.segmentOffset ?? null)
      : null
  useEffect(() => {
    if (citation.passageStartMS === null) {
      appliedCitationFocus.current = ''
    } else if (ownedCitationOffset !== null && appliedCitationFocus.current !== citationFocusKey) {
      appliedCitationFocus.current = citationFocusKey
      setPageIndex(Math.floor(ownedCitationOffset / 50), { replace: true })
    }
  }, [citationFocusKey, citation.passageStartMS, ownedCitationOffset, setPageIndex])
  const transcript = useQuery(
    () =>
      selectedRun
        ? recordingOperation<TranscriptView>('ReadRecordingTranscript', {
            recordingTranscriptRunId: selectedRun.id,
            segmentOffset: page.pageIndex * 50,
            pageSize: 50,
          })
        : Promise.resolve(null),
    [selectedRun?.id, page.pageIndex, userId],
  )
  const timeline = useQuery(
    () =>
      recordingOperation<TranscriptTimeline>('ReadRecordingTranscriptTimeline', {
        recordingId,
        segmentOffset: page.pageIndex * 50,
        pageSize: 50,
      }),
    [recordingId, page.pageIndex, userId],
  )
  const speakers = useQuery(
    () => recordingOperation<SpeakersView>('ReadRecordingSpeakers', { recordingId }),
    [recordingId, userId],
  )
  const learning = useQuery(
    () => recordingOperation<RecordingLearning>('ReadRecordingLearning', { recordingId }),
    [recordingId, userId],
  )
  const invalidations = useQuery(
    () => recordingOperation<ReferenceInvalidation[]>('ReadRecordingReferenceInvalidations', { recordingId }),
    [recordingId, userId],
  )
  const textHistory = useQuery(
    () => recordingOperation<TextCorrection[]>('ReadRecordingTranscriptTextCorrections', { recordingId }),
    [recordingId, userId],
  )
  const [dialog, setDialog] = useState<ProcessingDialog | null>(null)
  const [isSaving, setIsSaving] = useState(false),
    [editError, setEditError] = useState<string | null>(null)
  const versionView =
    transcript.data?.transcriptRun.id === selectedRun?.id && transcript.data?.transcriptRun.recordingId === recordingId
      ? transcript.data
      : null
  const effectiveView = citation.requestedRunId
    ? versionView
    : timeline.data
      ? { ...timeline.data, attempts: [] }
      : null
  const displayedRuns = citation.requestedRunId
    ? selectedRun
      ? [selectedRun]
      : []
    : (timeline.data?.transcriptRuns ?? [])
  const citedSegmentId =
    selectedRun && ownedCitationOffset !== null
      ? effectiveView?.segments.find((segment) =>
          overlapsRecordingCitation(segment.segmentStartMS, segment.segmentEndMS, citation),
        )?.id
      : undefined
  useEffect(() => {
    citedPassageElement.current?.scrollIntoView?.({ block: 'center' })
  }, [citedSegmentId, citationFocusKey])
  const refresh = async () => {
    await Promise.all([
      versions.reload(),
      availability.reload(),
      citationOffset.reload(),
      timeline.reload(),
      transcript.reload(),
      speakers.reload(),
      learning.reload(),
      invalidations.reload(),
      textHistory.reload(),
    ])
  }
  useEffect(() => {
    if (!runs.some((run) => ['queued', 'preparing', 'transcribing'].includes(run.recordingProcessingState))) return
    const timer = window.setInterval(() => {
      void versions.reload()
      void transcript.reload()
      void timeline.reload()
      void learning.reload()
    }, 4000)
    return () => window.clearInterval(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [runs.map((run) => `${run.id}:${run.recordingProcessingState}`).join(','), recordingId, userId])
  const show = (nextDialog: ProcessingDialog) => {
    setDialog(nextDialog)
    setEditError(null)
  }
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
  const save = async () => {
    if (!dialog) return
    setIsSaving(true)
    setEditError(null)
    const variables = { ...dialog.variables }
    for (const field of dialog.fields)
      variables[field.fieldName] = field.inputType === 'number' ? Number(field.fieldValue) : field.fieldValue
    if (dialog.operationName === 'UpdateRecordingMetadata') variables.recordedAt = variables.recordedAt || null
    try {
      await recordingOperation(dialog.operationName, variables)
      if (dialog.operationName === 'UpdateRecordingMetadata') await onMetadataChanged?.()
      setDialog(null)
      await refresh()
    } catch (caught) {
      setEditError(caughtText(caught))
    } finally {
      setIsSaving(false)
    }
  }
  const selectRun = (runId: string) =>
    setParameters((previous) => {
      const next = new URLSearchParams(previous)
      next.set('version', runId)
      next.delete('run')
      next.delete('startMS')
      next.delete('endMS')
      next.delete('transcriptPage')
      return next
    })
  return (
    <>
      <SettingsSection
        card
        title={t('recordingProcessing.versions')}
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
                      ? 'recordingProcessing.reprocess'
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
        <button
          type="button"
          onClick={() =>
            setParameters((previous) => {
              const next = new URLSearchParams(previous)
              next.delete('version')
              next.delete('run')
              next.delete('startMS')
              next.delete('endMS')
              next.delete('transcriptPage')
              return next
            })
          }
        >
          {t('recordingProcessing.fullTimeline')}
        </button>
        {runs.map((run) => (
          <SettingsRow
            key={run.id}
            title={
              <button type="button" onClick={() => selectRun(run.id)} aria-pressed={selectedRun?.id === run.id}>
                {t('recordingProcessing.version', { versionNumber: run.versionNumber })}
              </button>
            }
            subtitle={
              <>
                {mediaFiles.find((media) => media.id === run.recordingMediaId)?.originalFilename} ·{' '}
                {run.providerModelName || t('recordingProcessing.modelPending')}
                {run.processingErrorMessage ? <p className="error">{run.processingErrorMessage}</p> : null}
              </>
            }
            badge={
              <Tag
                value={t(`recordingProcessing.progress.${run.recordingProcessingState}` as Parameters<typeof t>[0])}
              />
            }
            actions={
              <div className="row-actions">
                {['queued', 'preparing', 'transcribing'].includes(run.recordingProcessingState) ? (
                  <RecordingRowAction
                    label={t('recordingProcessing.cancelProcessing')}
                    icon={<StopIcon size={16} />}
                    onClick={() =>
                      show({
                        operationName: 'CancelRecordingTranscription',
                        titleKey: 'cancelProcessing',
                        variables: { recordingTranscriptRunId: run.id },
                        fields: [],
                      })
                    }
                  />
                ) : null}
                {!['complete', 'uncertain'].includes(run.recordingProcessingState) ? (
                  <RecordingRowAction
                    label={t('recordingProcessing.retry')}
                    icon={<RefreshIcon size={16} />}
                    onClick={() =>
                      show({
                        operationName: 'RetryRecordingTranscription',
                        titleKey: 'retry',
                        variables: { recordingTranscriptRunId: run.id },
                        fields: [],
                      })
                    }
                  />
                ) : null}
              </div>
            }
          />
        ))}
        <p className="muted">{t('recordingProcessing.retryHint')}</p>
      </SettingsSection>
      {owned ? (
        <SettingsSection card title={t('recordingProcessing.transcript')}>
          <ErrorMessage error={citation.requestedRunId ? transcript.error : timeline.error} />
          {citation.hasInvalidBounds ? (
            <SettingsEmpty>{t('recordingProcessing.citationInvalidBounds')}</SettingsEmpty>
          ) : citation.requestedRunId && !selectedRun && !versions.loading ? (
            <SettingsEmpty>{t('recordingProcessing.citationVersionMissing')}</SettingsEmpty>
          ) : citationOffset.error ? (
            <SettingsEmpty>{caughtText(citationOffset.error)}</SettingsEmpty>
          ) : null}
          {!citation.requestedRunId ? <p className="muted">{t('recordingProcessing.timelineHint')}</p> : null}
          {selectedRun &&
          ownedCitationOffset !== null &&
          citation.passageStartMS !== null &&
          citation.passageEndMS !== null ? (
            <SettingsRow
              title={t('recordingProcessing.citedPassage')}
              subtitle={
                <a
                  href={passagePlaybackPath(
                    selectedRun.recordingMediaId,
                    citation.passageStartMS,
                    citation.passageEndMS,
                    mediaFiles.find((media) => media.id === selectedRun.recordingMediaId)?.timelineStartMS ?? 0,
                  )}
                  onClick={(event) => {
                    if (!onPlayPassage || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
                    if (citation.passageStartMS === null || citation.passageEndMS === null) return
                    event.preventDefault()
                    onPlayPassage(selectedRun.recordingMediaId, citation.passageStartMS, citation.passageEndMS)
                  }}
                  target="_blank"
                  rel="noreferrer"
                >
                  {t('recordingProcessing.playCitedPassage')}{' '}
                  {t('recordingProcessing.passageRange', {
                    startTime: timelineTime(citation.passageStartMS),
                    endTime: timelineTime(citation.passageEndMS),
                  })}
                </a>
              }
            />
          ) : null}
          <ErrorMessage error={invalidations.error} />
          {(invalidations.data ?? [])
            .filter((invalidation) => displayedRuns.some((run) => run.id === invalidation.recordingTranscriptRunId))
            .map((invalidation) => (
              <p key={invalidation.id} className="error">
                {t('recordingProcessing.referenceUncertain')} {invalidation.invalidationMessage}
              </p>
            ))}
          {(selectedRun ? transcript.loading : timeline.loading) && !effectiveView ? <Loading /> : null}
          {effectiveView?.segments.map((segment) => {
            const segmentRun = displayedRuns.find((run) => run.id === segment.recordingTranscriptRunId)
            if (!segmentRun) return null
            const confirmedName = confirmedSegmentName(segment)
            const isCitedPassage = Boolean(
              selectedRun &&
              ownedCitationOffset !== null &&
              overlapsRecordingCitation(segment.segmentStartMS, segment.segmentEndMS, citation),
            )
            const automaticIsUncertain = (invalidations.data ?? []).some(
              (invalidation) =>
                invalidation.recordingTranscriptionAttemptId === segment.recordingTranscriptionAttemptId,
            )
            return (
              <SettingsRow
                key={segment.id}
                badge={
                  isCitedPassage ? (
                    <span ref={segment.id === citedSegmentId ? citedPassageElement : undefined}>
                      <Tag value={t('recordingProcessing.citedPassage')} tone="good" />
                    </span>
                  ) : undefined
                }
                title={
                  <>
                    <a
                      href={passagePlaybackPath(
                        segmentRun.recordingMediaId,
                        segment.segmentStartMS,
                        segment.segmentEndMS,
                        mediaFiles.find((media) => media.id === segmentRun.recordingMediaId)?.timelineStartMS ?? 0,
                      )}
                      onClick={(event) => {
                        if (!onPlayPassage || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
                        event.preventDefault()
                        onPlayPassage(segmentRun.recordingMediaId, segment.segmentStartMS, segment.segmentEndMS)
                      }}
                      target="_blank"
                      rel="noreferrer"
                    >
                      {t('recordingProcessing.passageRange', {
                        startTime: timelineTime(segment.segmentStartMS),
                        endTime: timelineTime(segment.segmentEndMS),
                      })}
                    </a>{' '}
                    {confirmedName ??
                      t(
                        automaticIsUncertain
                          ? 'recordingProcessing.speakerUncertain'
                          : 'recordingProcessing.unknownSpeaker',
                        { localSpeakerId: segment.localSpeakerId },
                      )}
                  </>
                }
                subtitle={
                  <>
                    {segment.hasUnalignedSpeakerCorrection ? (
                      <p className="muted">{t('recordingProcessing.unalignedSpeakerCorrection')}</p>
                    ) : null}
                    {segment.hasUnalignedTextCorrection ? (
                      <p className="muted">{t('recordingProcessing.unalignedTextCorrection')}</p>
                    ) : null}
                    <p className="recording-transcript-text">
                      {segment.effectiveTranscriptText ?? segment.transcriptText}
                    </p>
                    {segment.appliedTextCorrectionId ? (
                      <details>
                        <summary>{t('recordingProcessing.generatedOriginal')}</summary>
                        <p>{segment.transcriptText}</p>
                      </details>
                    ) : null}
                  </>
                }
                actions={
                  <div className="row-actions">
                    <RecordingRowAction
                      label={t('recordingProcessing.nameSpeaker')}
                      icon={<AddPersonIcon size={16} />}
                      disabled={generation === undefined || !segment.localSpeakerId}
                      onClick={() =>
                        show({
                          operationName: 'CorrectRecordingSpeaker',
                          titleKey: 'nameSpeaker',
                          variables: {
                            recordingId,
                            expectedSourceGeneration: generation,
                            clientCorrectionId: crypto.randomUUID(),
                            recordingTranscriptionAttemptId: segment.recordingTranscriptionAttemptId,
                            localSpeakerId: segment.localSpeakerId,
                          },
                          fields: [
                            {
                              fieldName: 'confirmedName',
                              labelKey: 'confirmedName',
                              fieldValue: confirmedName ?? '',
                            },
                            {
                              fieldName: 'passageStartMS',
                              labelKey: 'passageStart',
                              inputType: 'number',
                              fieldValue: String(segment.segmentStartMS),
                            },
                            {
                              fieldName: 'passageEndMS',
                              labelKey: 'passageEnd',
                              inputType: 'number',
                              fieldValue: String(segment.segmentEndMS),
                            },
                          ],
                        })
                      }
                    />
                    <RecordingRowAction
                      label={t('recordingProcessing.editText')}
                      icon={<PencilIcon size={16} />}
                      disabled={generation === undefined}
                      onClick={() =>
                        show({
                          operationName: 'CorrectRecordingTranscriptText',
                          titleKey: 'editText',
                          variables: {
                            recordingId,
                            recordingTranscriptSegmentId: segment.id,
                            expectedSourceGeneration: generation,
                            clientCorrectionId: crypto.randomUUID(),
                          },
                          fields: [
                            {
                              fieldName: 'correctedTranscriptText',
                              labelKey: 'correctedText',
                              inputType: 'textarea',
                              fieldValue: segment.effectiveTranscriptText ?? segment.transcriptText,
                            },
                          ],
                        })
                      }
                    />
                    {segment.appliedTextCorrectionId ? (
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
                              recordingTranscriptTextCorrectionId: segment.appliedTextCorrectionId,
                            },
                            fields: [],
                          })
                        }
                      />
                    ) : null}
                  </div>
                }
              />
            )
          })}
          {effectiveView ? (
            <Pager
              pageIndex={page.pageIndex}
              pageSize={50}
              shownCount={effectiveView.segments.length}
              totalCount={effectiveView.segmentCount}
              hasNextPage={effectiveView.hasMoreSegments}
              isLoading={selectedRun ? transcript.loading : timeline.loading}
              onPageIndex={page.setPageIndex}
            />
          ) : null}
          {(effectiveView?.attempts ?? []).map((attempt) => (
            <SettingsRow
              key={attempt.id}
              title={attempt.providerModelName}
              badge={<Tag value={attempt.recordingAttemptState} />}
              subtitle={
                <>
                  {attempt.attemptErrorMessage ? <p className="error">{attempt.attemptErrorMessage}</p> : null}
                  <p>
                    {t('recordingProcessing.usage', {
                      audioTokens: attempt.inputAudioTokens,
                      textTokens: attempt.inputTextTokens,
                      durationMS: attempt.providerAudioDurationMS,
                    })}
                  </p>
                  <p>
                    {t('recordingProcessing.estimatedCost')}:{' '}
                    {attempt.estimatedCostAmount === null
                      ? t('recordingProcessing.unknownCost')
                      : `${attempt.estimatedCostAmount} ${attempt.currencyCode}`}
                    ; {t('recordingProcessing.actualCost')}:{' '}
                    {attempt.actualCostAmount === null
                      ? t('recordingProcessing.unknownCost')
                      : `${attempt.actualCostAmount} ${attempt.currencyCode}`}
                  </p>
                  {!attempt.hasProviderUsage ? <p>{t('recordingProcessing.noProviderUsage')}</p> : null}
                </>
              }
            />
          ))}
        </SettingsSection>
      ) : null}
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
        <FormDialog
          title={t(`recordingProcessing.${dialog.titleKey}` as Parameters<typeof t>[0])}
          submitLabel={t(dialog.isPaid ? 'recordingProcessing.confirmPaid' : 'common.save')}
          busy={isSaving}
          error={editError}
          canSubmit={dialog.fields.every((field) => field.fieldName === 'recordedAt' || field.fieldValue.trim() !== '')}
          onClose={() => {
            if (!isSaving) setDialog(null)
          }}
          onSubmit={() => void save()}
        >
          {dialog.isPaid ? <p>{t('recordingProcessing.paidHint')}</p> : null}
          {dialog.isDestructive ? <p>{t('recordingProcessing.referenceDeleteHint')}</p> : null}
          {dialog.operationName === 'RetryRecordingTranscription' ? <p>{t('recordingProcessing.retryHint')}</p> : null}
          {dialog.operationName === 'CancelRecordingTranscription' ? (
            <p>{t('recordingProcessing.cancelHint')}</p>
          ) : null}
          {dialog.operationName === 'CorrectRecordingSpeaker' ? <p>{t('recordingProcessing.groupHint')}</p> : null}
          {dialog.fields.map((field, fieldIndex) => (
            <label key={field.fieldName}>
              <span>{t(`recordingProcessing.${field.labelKey}` as Parameters<typeof t>[0])}</span>
              {field.inputType === 'textarea' ? (
                <textarea
                  rows={6}
                  value={field.fieldValue}
                  onChange={(event) =>
                    setDialog({
                      ...dialog,
                      fields: dialog.fields.map((previous, index) =>
                        index === fieldIndex ? { ...previous, fieldValue: event.target.value } : previous,
                      ),
                    })
                  }
                />
              ) : (
                <input
                  type={field.inputType ?? 'text'}
                  list={field.fieldName === 'confirmedName' ? speakerNamesListId : undefined}
                  value={field.fieldValue}
                  onChange={(event) =>
                    setDialog({
                      ...dialog,
                      fields: dialog.fields.map((previous, index) =>
                        index === fieldIndex ? { ...previous, fieldValue: event.target.value } : previous,
                      ),
                    })
                  }
                />
              )}
            </label>
          ))}
          <datalist id={speakerNamesListId}>
            {(speakers.data?.speakerIdentities ?? []).map((identity) => (
              <option key={identity.id} value={identity.confirmedName} />
            ))}
          </datalist>
        </FormDialog>
      ) : null}
    </>
  )
}
