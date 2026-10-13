import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { ErrorMessage, Loading } from '../components/common'
import { Pager, usePageInAddress } from '../components/pager'
import { SettingsEmpty } from '../components/settingsList'
import { useToast } from '../components/toast'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'
import { type RecordingMedia } from './api'
import { overlapsRecordingCitation, readRecordingCitation } from './citation'
import { distinctSpeakerCount, groupConversation, type ConversationGroup } from './conversation'
import { RecordingEditDialog, type ProcessingDialog } from './editDialog'
import { formatRecordingLength } from './library'
import { PassageMenu, type PassageMenuItem } from './passageMenu'
import { type RecordingPassagePlayback } from './playback'
import {
  recordingOperation,
  type SpeakersView,
  type TranscriptSegment,
  type TranscriptTimeline,
  type TranscriptView,
} from './processing'

const LONG_PRESS_MS = 500
const LONG_PRESS_MOVE_PIXELS = 10
const PAGE_SIZE = 50

type OpenMenu = { segment: TranscriptSegment; leftPixels: number; topPixels: number; returnFocus: HTMLElement | null }

// RecordingConversation is the transcript read as a chat: one bubble per run of
// passages by the same speaker, other people on the left with their name above,
// the account owner on the right in the accent color. Everything done to a
// passage is in its menu, so reading is not interrupted by rows of buttons.
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

  const [menu, setMenu] = useState<OpenMenu | null>(null)
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
  const copy = async (segment: TranscriptSegment) => {
    try {
      await navigator.clipboard.writeText(passageText(segment))
      toast.done(t('recordings.copied'))
    } catch {
      toast.failed(t('common.copyFailed'))
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

  const menuItems = (segment: TranscriptSegment): PassageMenuItem[] => {
    const canEdit = !isReadOnly && generation !== undefined
    const canName = canEdit && Boolean(segment.localSpeakerId)
    const items: PassageMenuItem[] = [
      { itemKey: 'play', label: t('recordings.playFromHere'), onSelect: () => play(segment) },
      { itemKey: 'copy', label: t('common.copy'), onSelect: () => void copy(segment) },
    ]
    if (!isReadOnly)
      items.push(
        {
          itemKey: 'correct',
          label: t('recordings.correctText'),
          isDisabled: !canEdit,
          onSelect: () => correctText(segment),
        },
        {
          itemKey: 'name',
          label: t('recordingProcessing.nameSpeaker'),
          isDisabled: !canName,
          onSelect: () => nameSpeaker(segment),
        },
        {
          itemKey: 'owner',
          label: t(segment.isAccountOwner ? 'recordings.notMe' : 'recordings.thisIsMe'),
          isDisabled: !canName,
          // There is no call that unsets the owner: releasing a passage means
          // confirming it as somebody else, so the name form opens.
          onSelect: () => (segment.isAccountOwner ? nameSpeaker(segment) : void setOwnership(segment)),
        },
      )
    return items
  }
  const openMenuAt = (
    segment: TranscriptSegment,
    event: { clientX: number; clientY: number },
    element: HTMLElement,
  ) => {
    // A keyboard's menu key reports no pointer position: open beside the passage.
    const rectangle = element.getBoundingClientRect()
    const hasPointer = event.clientX !== 0 || event.clientY !== 0
    setMenu({
      segment,
      leftPixels: hasPointer ? event.clientX : rectangle.left,
      topPixels: hasPointer ? event.clientY : rectangle.bottom,
      returnFocus: element,
    })
  }

  const pressTimer = useRef<number | null>(null)
  const pressStart = useRef({ clientX: 0, clientY: 0 })
  const hasLongPressed = useRef(false)
  useEffect(() => () => window.clearTimeout(pressTimer.current ?? undefined), [])
  const cancelPress = () => {
    window.clearTimeout(pressTimer.current ?? undefined)
    pressTimer.current = null
  }

  const bubble = (group: ConversationGroup) => {
    const lastSegment = () => lastPassageByGroup.current.get(group.groupKey) ?? group.segments[0]
    const speakerLabel = group.speakerName ?? t('recordings.speakerLabel', { speakerLetter: group.speakerLetter })
    return (
      <div key={group.groupKey} className={group.isAccountOwner ? 'recording-turn own' : 'recording-turn'}>
        {!group.isAccountOwner ? (
          <div className={group.speakerName ? 'recording-turn-name' : 'recording-turn-name muted'}>{speakerLabel}</div>
        ) : null}
        <div className="recording-bubble">
          <p className="recording-bubble-text">
            {group.segments.map((segment) => (
              <span key={segment.id}>
                {segment.id === citedSegmentId ? <span ref={citedElement} /> : null}
                <button
                  type="button"
                  className={segment.id === citedSegmentId ? 'recording-passage cited' : 'recording-passage'}
                  onClick={() => {
                    if (hasLongPressed.current) {
                      hasLongPressed.current = false
                      return
                    }
                    lastPassageByGroup.current.set(group.groupKey, segment)
                    play(segment)
                  }}
                  onFocus={() => lastPassageByGroup.current.set(group.groupKey, segment)}
                  onPointerEnter={() => lastPassageByGroup.current.set(group.groupKey, segment)}
                  onContextMenu={(event) => {
                    event.preventDefault()
                    cancelPress()
                    openMenuAt(segment, event, event.currentTarget)
                  }}
                  onPointerDown={(event) => {
                    if (event.pointerType !== 'touch') return
                    hasLongPressed.current = false
                    pressStart.current = { clientX: event.clientX, clientY: event.clientY }
                    const element = event.currentTarget
                    pressTimer.current = window.setTimeout(() => {
                      hasLongPressed.current = true
                      openMenuAt(segment, pressStart.current, element)
                    }, LONG_PRESS_MS)
                  }}
                  onPointerMove={(event) => {
                    if (
                      pressTimer.current !== null &&
                      Math.hypot(
                        event.clientX - pressStart.current.clientX,
                        event.clientY - pressStart.current.clientY,
                      ) > LONG_PRESS_MOVE_PIXELS
                    )
                      cancelPress()
                  }}
                  onPointerUp={cancelPress}
                  onPointerCancel={cancelPress}
                >
                  {passageText(segment)}
                </button>{' '}
              </span>
            ))}
          </p>
          <div className="recording-bubble-foot">
            <time className="recording-bubble-time">{formatRecordingLength(group.groupStartMS)}</time>
            <button
              type="button"
              className="recording-bubble-more"
              aria-haspopup="menu"
              aria-label={t('recordings.passageActions')}
              onClick={(event) => openMenuAt(lastSegment(), { clientX: 0, clientY: 0 }, event.currentTarget)}
            >
              <span aria-hidden="true">•••</span>
            </button>
          </div>
        </div>
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
      {menu ? (
        <PassageMenu
          label={t('recordings.passageActions')}
          leftPixels={menu.leftPixels}
          topPixels={menu.topPixels}
          items={menuItems(menu.segment)}
          onClose={() => {
            const returnFocus = menu.returnFocus
            setMenu(null)
            returnFocus?.focus()
          }}
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
