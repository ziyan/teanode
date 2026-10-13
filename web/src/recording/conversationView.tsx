import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { ErrorMessage, Loading } from '../components/common'
import { MoreIcon, PlayIcon } from '../components/icons'
import { MenuButton } from '../components/menuButton'
import { Pager, usePageInAddress } from '../components/pager'
import { SettingsEmpty } from '../components/settingsList'
import { SwipeableLine } from '../components/swipeableLine'
import { useToast } from '../components/toast'
import { Tooltip } from '../components/tooltip'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'
import { type RecordingMedia } from './api'
import { overlapsRecordingCitation, readRecordingCitation } from './citation'
import { distinctSpeakerCount, groupConversation, type ConversationGroup } from './conversation'
import { RecordingEditDialog, type ProcessingDialog } from './editDialog'
import { formatRecordingLength } from './library'
import { type RecordingPassagePlayback } from './playback'
import {
  recordingOperation,
  type SpeakersView,
  type TranscriptSegment,
  type TranscriptTimeline,
  type TranscriptView,
} from './processing'

const PAGE_SIZE = 50

// RecordingConversation is the transcript read as a chat: one bubble per run of
// passages by the same speaker, other people on the left with their name above,
// the account owner on the right in the accent color. The bubbles are the
// agent chat's (`agent-line`), swiped the same way: to the left plays the
// recording from the bubble, to the right copies it. The rest of what is done
// to a passage is behind the More button in the bubble's corner.
export function RecordingConversation({
  recordingId,
  userId,
  mediaFiles,
  passagePlayback,
  reloadToken,
  onChanged,
  onSpeakerCount,
}: {
  recordingId: string
  userId: string
  mediaFiles: RecordingMedia[]
  passagePlayback: RecordingPassagePlayback
  reloadToken: number
  onChanged: () => void
  onSpeakerCount?: (speakerCount: number) => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [parameters, setParameters] = useSearchParams()
  const citation = readRecordingCitation(parameters)
  const olderRunId = citation.requestedRunId
  const page = usePageInAddress({ pageParameter: 'transcriptPage', defaultPageSize: PAGE_SIZE })
  const { setPageIndex } = page
  const citedElement = useRef<HTMLSpanElement>(null)
  const appliedCitation = useRef('')
  const citationKey = `${recordingId}/${olderRunId}/${citation.passageStartMS}/${citation.passageEndMS}`

  const citationOffset = useQuery(
    () =>
      olderRunId && citation.passageStartMS !== null && citation.passageEndMS !== null
        ? recordingOperation<number>('ReadRecordingTranscriptPassageOffset', {
            recordingTranscriptRunId: olderRunId,
            passageStartMS: citation.passageStartMS,
            passageEndMS: citation.passageEndMS,
          })
        : Promise.resolve(null),
    [olderRunId, citation.passageStartMS, citation.passageEndMS, userId],
  )
  useEffect(() => {
    if (citation.passageStartMS === null) appliedCitation.current = ''
    else if (citationOffset.data !== null && appliedCitation.current !== citationKey) {
      appliedCitation.current = citationKey
      setPageIndex(Math.floor(citationOffset.data / PAGE_SIZE), { replace: true })
    }
  }, [citationKey, citation.passageStartMS, citationOffset.data, setPageIndex])

  const timeline = useQuery(
    () =>
      olderRunId
        ? recordingOperation<TranscriptView>('ReadRecordingTranscript', {
            recordingTranscriptRunId: olderRunId,
            segmentOffset: page.pageIndex * PAGE_SIZE,
            pageSize: PAGE_SIZE,
          }).then((view): TranscriptTimeline => ({
            ...view,
            sourceGeneration: 0,
            transcriptRuns: [view.transcriptRun],
          }))
        : recordingOperation<TranscriptTimeline>('ReadRecordingTranscriptTimeline', {
            recordingId,
            segmentOffset: page.pageIndex * PAGE_SIZE,
            pageSize: PAGE_SIZE,
          }),
    [recordingId, olderRunId, page.pageIndex, userId, reloadToken],
  )
  const speakers = useQuery(
    () => recordingOperation<SpeakersView>('ReadRecordingSpeakers', { recordingId }),
    [recordingId, userId, reloadToken],
  )
  const generation = speakers.data?.sourceGeneration
  const isReadOnly = Boolean(olderRunId)
  const segments = timeline.data?.segments ?? []
  const groups = groupConversation(segments)
  const speakerCount = distinctSpeakerCount(segments)
  useEffect(() => onSpeakerCount?.(speakerCount), [speakerCount, onSpeakerCount])
  const citedSegmentId =
    olderRunId && citationOffset.data !== null
      ? segments.find((segment) => overlapsRecordingCitation(segment.segmentStartMS, segment.segmentEndMS, citation))
          ?.id
      : undefined
  useEffect(() => {
    citedElement.current?.scrollIntoView?.({ block: 'center' })
  }, [citedSegmentId, citationKey])

  const [dialog, setDialog] = useState<ProcessingDialog | null>(null)
  const lastPassageByGroup = useRef(new Map<string, TranscriptSegment>())

  const runs = timeline.data?.transcriptRuns ?? []
  const play = (segment: TranscriptSegment) => {
    const run = runs.find((candidate) => candidate.id === segment.recordingTranscriptRunId)
    const media = mediaFiles.find((candidate) => candidate.id === run?.recordingMediaId)
    if (!media) {
      toast.failed(t('recordings.playerUnavailable'))
      return
    }
    void passagePlayback
      .play(media.id, segment.segmentStartMS, segment.segmentEndMS, media.timelineStartMS)
      .catch((caught) => toast.failure(caught, t('recordings.playerUnavailable')))
  }
  const passageText = (segment: TranscriptSegment) => segment.effectiveTranscriptText || segment.transcriptText
  // copyText says whether the browser took the text, which the swipe needs to
  // show its tick, or to offer a button to tap when a swipe was not allowed to copy.
  const copyText = async (text: string): Promise<boolean> => {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      return false
    }
  }
  const speakerCorrectionVariables = (segment: TranscriptSegment) => ({
    recordingId,
    expectedSourceGeneration: generation,
    clientCorrectionId: crypto.randomUUID(),
    recordingTranscriptionAttemptId: segment.recordingTranscriptionAttemptId,
    localSpeakerId: segment.localSpeakerId,
  })
  const nameSpeaker = (segment: TranscriptSegment) =>
    setDialog({
      operationName: 'CorrectRecordingSpeaker',
      titleKey: 'nameSpeaker',
      variables: speakerCorrectionVariables(segment),
      fields: [
        {
          fieldName: 'confirmedName',
          labelKey: 'confirmedName',
          fieldValue: segment.hasUserConfirmedSpeaker ? segment.effectiveSpeakerName : '',
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
  const correctText = (segment: TranscriptSegment) =>
    setDialog({
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
          fieldValue: passageText(segment),
        },
      ],
    })
  const setOwnership = async (segment: TranscriptSegment) => {
    try {
      await recordingOperation('CorrectRecordingSpeakerOwner', {
        ...speakerCorrectionVariables(segment),
        // Left empty so the server names the speaker after the person.
        confirmedName: '',
        passageStartMS: segment.segmentStartMS,
        passageEndMS: segment.segmentEndMS,
        isAccountOwner: true,
      })
      toast.done(t('recordingProcessing.saved'))
      onChanged()
    } catch (caught) {
      toast.failure(caught, t('recordingProcessing.saved'))
    }
  }

  // The actions that are not a swipe. A version read for its citation is read
  // only, so it has none.
  const passageMenu = (segment: TranscriptSegment) => {
    const canEdit = generation !== undefined
    const canName = canEdit && Boolean(segment.localSpeakerId)
    return (close: () => void) => (
      <>
        <button
          type="button"
          role="menuitem"
          disabled={!canEdit}
          onClick={() => {
            close()
            correctText(segment)
          }}
        >
          {t('recordings.correctText')}
        </button>
        <button
          type="button"
          role="menuitem"
          disabled={!canName}
          onClick={() => {
            close()
            nameSpeaker(segment)
          }}
        >
          {t('recordingProcessing.nameSpeaker')}
        </button>
        <button
          type="button"
          role="menuitem"
          disabled={!canName}
          onClick={() => {
            close()
            // There is no call that unsets the owner: releasing a passage means
            // confirming it as somebody else, so the name form opens.
            if (segment.isAccountOwner) nameSpeaker(segment)
            else void setOwnership(segment)
          }}
        >
          {t(segment.isAccountOwner ? 'recordings.notMe' : 'recordings.thisIsMe')}
        </button>
      </>
    )
  }

  const bubble = (group: ConversationGroup) => {
    const lastSegment = () => lastPassageByGroup.current.get(group.groupKey) ?? group.segments[0]
    const speakerLabel = group.speakerName ?? t('recordings.speakerLabel', { speakerLetter: group.speakerLetter })
    return (
      <div key={group.groupKey} className={group.isAccountOwner ? 'recording-turn own' : 'recording-turn'}>
        {!group.isAccountOwner ? (
          <div className={group.speakerName ? 'recording-turn-name' : 'recording-turn-name muted'}>{speakerLabel}</div>
        ) : null}
        <Tooltip label={formatRecordingLength(group.groupStartMS)}>
          <SwipeableLine
            className={group.isAccountOwner ? 'agent-line user' : 'agent-line assistant'}
            replyLabel={t('recordings.playFromHere')}
            copyLabel={t('common.copy')}
            replyIcon={PlayIcon}
            onReply={() => play(group.segments[0])}
            onCopy={() => copyText(group.segments.map(passageText).join(' '))}
            onCopyFailed={() => toast.failed(t('common.copyFailed'))}
            extraActions={
              isReadOnly ? null : (
                <MenuButton
                  label={t('recordings.passageActions')}
                  className="icon-action recording-line-more"
                  icon={<MoreIcon size={14} />}
                  render={(close) => passageMenu(lastSegment())(close)}
                />
              )
            }
          >
            {group.segments.map((segment) => (
              <span key={segment.id}>
                {segment.id === citedSegmentId ? <span ref={citedElement} /> : null}
                <button
                  type="button"
                  className={segment.id === citedSegmentId ? 'recording-passage cited' : 'recording-passage'}
                  onClick={() => {
                    lastPassageByGroup.current.set(group.groupKey, segment)
                    play(segment)
                  }}
                  onFocus={() => lastPassageByGroup.current.set(group.groupKey, segment)}
                  onPointerEnter={() => lastPassageByGroup.current.set(group.groupKey, segment)}
                >
                  {passageText(segment)}
                </button>{' '}
              </span>
            ))}
          </SwipeableLine>
        </Tooltip>
      </div>
    )
  }

  return (
    <div className="recording-conversation">
      <ErrorMessage error={timeline.error} />
      {olderRunId ? (
        <div className="recording-earlier-notice muted">
          <span>{t('recordings.earlierTranscript')}</span>{' '}
          <button
            type="button"
            onClick={() =>
              setParameters((previous) => {
                const next = new URLSearchParams(previous)
                for (const name of ['version', 'run', 'startMS', 'endMS', 'transcriptPage']) next.delete(name)
                return next
              })
            }
          >
            {t('recordings.showLatestTranscript')}
          </button>
        </div>
      ) : null}
      {timeline.loading && !timeline.data ? <Loading /> : null}
      {timeline.data && groups.length === 0 ? <SettingsEmpty>{t('recordings.noTranscript')}</SettingsEmpty> : null}
      {groups.map(bubble)}
      {timeline.data && groups.length > 0 ? (
        <Pager
          pageIndex={page.pageIndex}
          pageSize={PAGE_SIZE}
          shownCount={segments.length}
          totalCount={timeline.data.segmentCount}
          hasNextPage={timeline.data.hasMoreSegments}
          isLoading={timeline.loading}
          onPageIndex={page.setPageIndex}
        />
      ) : null}
      {dialog ? (
        <RecordingEditDialog
          dialog={dialog}
          speakerNames={(speakers.data?.speakerIdentities ?? []).map((identity) => identity.confirmedName)}
          onClose={() => setDialog(null)}
          onSaved={async () => onChanged()}
        />
      ) : null}
    </div>
  )
}
