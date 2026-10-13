import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { ErrorMessage, Loading } from '../components/common'
import { LineActionPill, type LineAction } from '../components/agentReactions'
import { CopyIcon, PencilIcon, PeopleIcon, PlayIcon, UserIcon } from '../components/icons'
import { Pager, usePageInAddress } from '../components/pager'
import { SettingsEmpty } from '../components/settingsList'
import { SwipeableLine } from '../components/swipeableLine'
import { useToast } from '../components/toast'
import { Tooltip } from '../components/tooltip'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'
import { type RecordingMedia } from './api'
import { overlapsRecordingCitation, readRecordingCitation } from './citation'
import {
  distinctSpeakerCount,
  groupConversation,
  liveRangeOfPage,
  temporarySpeakerNumber,
  type ConversationGroup,
} from './conversation'
import { BubbleTextDialog } from './bubbleTextDialog'
import { RecordingEditDialog, type ProcessingDialog } from './editDialog'
import { formatRecordingLength } from './library'
import { type RecordingPassagePlayback } from './playback'
import {
  recordingOperation,
  type LiveUtterance,
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
  isLive = false,
  onChanged,
  onSpeakerCount,
}: {
  recordingId: string
  userId: string
  mediaFiles: RecordingMedia[]
  passagePlayback: RecordingPassagePlayback
  reloadToken: number
  // isLive is a capture still in progress: the words heard so far are drawn
  // at the end, without a speaker, until the transcript with speakers covers them.
  isLive?: boolean
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
  // Only the first page of the timeline carries the words heard live, so a
  // later page asks for them on their own.
  const liveTail = useQuery(
    () =>
      isLive
        ? recordingOperation<{ liveUtterances: LiveUtterance[] }>('ReadRecordingLiveTail', { recordingId })
        : Promise.resolve(null),
    [recordingId, isLive, userId, reloadToken],
  )
  const speakers = useQuery(
    () => recordingOperation<SpeakersView>('ReadRecordingSpeakers', { recordingId }),
    [recordingId, userId, reloadToken],
  )
  const generation = speakers.data?.sourceGeneration
  const isReadOnly = Boolean(olderRunId)
  const segments = timeline.data?.segments ?? []
  const liveUtterances = isLive && !isReadOnly ? (liveTail.data?.liveUtterances ?? []) : []
  const isLastPage = timeline.data ? !timeline.data.hasMoreSegments : false
  const groups = groupConversation(
    segments,
    liveUtterances,
    liveRangeOfPage(segments, page.pageIndex === 0, isLastPage),
  )
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
  const [actingOnGroupKey, setActingOnGroupKey] = useState<string | null>(null)
  const [editingGroup, setEditingGroup] = useState<ConversationGroup | null>(null)

  // A capture in progress grows at the end. A reader who is looking at the
  // end is kept there as it grows; one who has scrolled up is left alone.
  const endOfConversation = useRef<HTMLDivElement>(null)
  const wasAtEnd = useRef(true)
  useEffect(() => {
    const keepTrack = () => {
      wasAtEnd.current = window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 160
    }
    window.addEventListener('scroll', keepTrack, { passive: true })
    return () => window.removeEventListener('scroll', keepTrack)
  }, [])
  const liveSignature = `${groups.length}/${liveUtterances.length}/${segments.length}`
  useEffect(() => {
    if (isLive && wasAtEnd.current) endOfConversation.current?.scrollIntoView?.({ block: 'end' })
  }, [isLive, liveSignature])

  const runs = timeline.data?.transcriptRuns ?? []
  // play plays a whole bubble: from its first passage to the last one taken
  // from the same original, which is every passage unless the bubble spans two.
  const play = (segments: TranscriptSegment[]) => {
    const firstSegment = segments[0]
    const run = runs.find((candidate) => candidate.id === firstSegment.recordingTranscriptRunId)
    const media = mediaFiles.find((candidate) => candidate.id === run?.recordingMediaId)
    if (!media) {
      toast.failed(t('recordings.playerUnavailable'))
      return
    }
    const sameOriginal = segments.filter(
      (segment) => segment.recordingTranscriptRunId === firstSegment.recordingTranscriptRunId,
    )
    const endMS = Math.max(...sameOriginal.map((segment) => segment.segmentEndMS))
    void passagePlayback
      .play(media.id, firstSegment.segmentStartMS, endMS, media.timelineStartMS)
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
  // Naming a speaker names the voice: every passage it spoke in the same
  // transcription request (-1 and -1 select that group), not one span of time.
  const nameSpeaker = (segment: TranscriptSegment) =>
    setDialog({
      operationName: 'CorrectRecordingSpeaker',
      titleKey: 'nameSpeaker',
      variables: { ...speakerCorrectionVariables(segment), passageStartMS: -1, passageEndMS: -1 },
      fields: [
        {
          fieldName: 'confirmedName',
          labelKey: 'confirmedName',
          fieldValue: segment.hasUserConfirmedSpeaker ? segment.effectiveSpeakerName : '',
        },
      ],
    })
  const nameTemporarySpeaker = (speakerLabel: string) =>
    setDialog({
      operationName: 'NameRecordingTemporarySpeaker',
      titleKey: 'nameSpeaker',
      variables: { recordingId, speakerLabel, isAccountOwner: false },
      fields: [{ fieldName: 'confirmedName', labelKey: 'confirmedName', fieldValue: '' }],
    })
  const setTemporaryOwnership = async (speakerLabel: string) => {
    try {
      // Left empty so the server names the speaker after the person.
      await recordingOperation('NameRecordingTemporarySpeaker', {
        recordingId,
        speakerLabel,
        confirmedName: '',
        isAccountOwner: true,
      })
      toast.done(t('recordingProcessing.saved'))
      onChanged()
    } catch (caught) {
      toast.failure(caught, t('recordingProcessing.saved'))
    }
  }
  const setOwnership = async (segment: TranscriptSegment) => {
    try {
      await recordingOperation('CorrectRecordingSpeakerOwner', {
        ...speakerCorrectionVariables(segment),
        // Left empty so the server names the speaker after the person.
        confirmedName: '',
        passageStartMS: -1,
        passageEndMS: -1,
        isAccountOwner: true,
      })
      toast.done(t('recordingProcessing.saved'))
      onChanged()
    } catch (caught) {
      toast.failure(caught, t('recordingProcessing.saved'))
    }
  }

  // What can be done to a bubble, the same set in its corner under a mouse
  // and in the pill a tap opens on a touch screen, as the agent's messages
  // have: play it, copy it, and, unless this is a version read for its
  // citation, correct its words and say who spoke it, over the whole bubble.
  const bubbleActions = (group: ConversationGroup): LineAction[] => {
    const firstSegment = group.segments[0]
    const temporarySpeakerLabel = group.temporarySpeakerLabel
    const canEdit = !isReadOnly && generation !== undefined
    const canName = canEdit && Boolean(firstSegment.localSpeakerId) && !temporarySpeakerLabel
    const actions: LineAction[] = [
      {
        actionKey: 'play',
        label: t('recordings.playFromHere'),
        icon: <PlayIcon size={16} />,
        onAct: () => play(group.segments),
      },
      {
        actionKey: 'copy',
        label: t('common.copy'),
        icon: <CopyIcon size={16} />,
        onAct: () =>
          void copyText(group.segments.map(passageText).join(' ')).then((isCopied) =>
            isCopied ? toast.done(t('common.copied')) : toast.failed(t('common.copyFailed')),
          ),
      },
    ]
    if (canEdit)
      actions.push({
        actionKey: 'edit',
        label: t('recordings.correctText'),
        icon: <PencilIcon size={16} />,
        onAct: () => setEditingGroup(group),
      })
    // A speaker heard while the recording is still going, not yet named, is
    // named for the whole recording at once.
    if (!isReadOnly && temporarySpeakerLabel)
      actions.push(
        {
          actionKey: 'name',
          label: t('recordingProcessing.nameSpeaker'),
          icon: <PeopleIcon size={16} />,
          onAct: () => nameTemporarySpeaker(temporarySpeakerLabel),
        },
        {
          actionKey: 'owner',
          label: t('recordings.thisIsMe'),
          icon: <UserIcon size={16} />,
          onAct: () => void setTemporaryOwnership(temporarySpeakerLabel),
        },
      )
    // Your own bubble has one way to say who it was instead: Not me, which
    // opens the name form, since there is no call that unsets the owner.
    if (canName && firstSegment.isAccountOwner)
      actions.push({
        actionKey: 'owner',
        label: t('recordings.notMe'),
        icon: <UserIcon size={16} />,
        onAct: () => nameSpeaker(firstSegment),
      })
    else if (canName)
      actions.push(
        {
          actionKey: 'name',
          label: t('recordingProcessing.nameSpeaker'),
          icon: <PeopleIcon size={16} />,
          onAct: () => nameSpeaker(firstSegment),
        },
        {
          actionKey: 'owner',
          label: t('recordings.thisIsMe'),
          icon: <UserIcon size={16} />,
          onAct: () => void setOwnership(firstSegment),
        },
      )
    return actions
  }

  const bubble = (group: ConversationGroup) => {
    // Words heard live have no speaker yet: a bubble on the left with no name,
    // which turns into the speakers' own when the transcript covers it.
    if (group.isLive)
      return (
        <div key={group.groupKey} className="recording-turn live">
          <Tooltip label={formatRecordingLength(group.groupStartMS)}>
            <div className="agent-line assistant recording-live-line">
              {group.liveUtterances.map((utterance) => utterance.transcriptText).join(' ')}
            </div>
          </Tooltip>
        </div>
      )
    const temporaryNumber = group.temporarySpeakerLabel ? temporarySpeakerNumber(group.temporarySpeakerLabel) : null
    const speakerLabel =
      group.speakerName ??
      (group.temporarySpeakerLabel
        ? temporaryNumber
          ? t('recordings.temporarySpeakerLabel', { speakerNumber: temporaryNumber })
          : group.temporarySpeakerLabel
        : t('recordings.speakerLabel', { speakerLetter: group.speakerLetter }))
    const actions = bubbleActions(group)
    // Play and Copy are the swipe's own buttons in the corner; the rest follow.
    const cornerActions = actions.filter((action) => action.actionKey !== 'play' && action.actionKey !== 'copy')
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
            onReply={() => play(group.segments)}
            onCopy={() => copyText(group.segments.map(passageText).join(' '))}
            onCopyFailed={() => toast.failed(t('common.copyFailed'))}
            onTap={() => setActingOnGroupKey((previous) => (previous === group.groupKey ? null : group.groupKey))}
            extraActions={cornerActions.map((action) => (
              <button
                key={action.actionKey}
                type="button"
                className="icon-action"
                title={action.label}
                aria-label={action.label}
                onClick={action.onAct}
              >
                {action.icon}
              </button>
            ))}
          >
            {group.segments.map((segment) => (
              <span
                key={segment.id}
                className={segment.id === citedSegmentId ? 'recording-passage cited' : 'recording-passage'}
              >
                {segment.id === citedSegmentId ? <span ref={citedElement} /> : null}
                {passageText(segment)}{' '}
              </span>
            ))}
            {actingOnGroupKey === group.groupKey ? (
              <LineActionPill
                label={t('recordings.passageActions')}
                actions={actions}
                onClose={() => setActingOnGroupKey(null)}
              />
            ) : null}
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
      {timeline.data && groups.length === 0 ? (
        <SettingsEmpty>{t(isLive ? 'recordings.noWordsYet' : 'recordings.noTranscript')}</SettingsEmpty>
      ) : null}
      {isLive && timeline.data?.hasMoreSegments ? (
        <div className="recording-earlier-notice muted">
          <button
            type="button"
            onClick={() => page.setPageIndex(Math.floor((timeline.data!.segmentCount - 1) / PAGE_SIZE))}
          >
            {t('recordings.showLatestWords')}
          </button>
        </div>
      ) : null}
      {groups.map(bubble)}
      <div ref={endOfConversation} />
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
      {editingGroup ? (
        <BubbleTextDialog
          recordingId={recordingId}
          segments={editingGroup.segments}
          passageText={passageText}
          onClose={() => setEditingGroup(null)}
          onSaved={onChanged}
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
