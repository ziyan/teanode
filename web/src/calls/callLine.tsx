import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { useNavigate } from 'react-router-dom'

import { graphql } from '../api'
import { ConfirmDialog } from '../components/dialog'
import { PhoneIcon } from '../components/icons'
import { useTranslation } from '../i18n/i18n'
import {
  READ_CHAT_CALL,
  chatCallLineState,
  formatCallLength,
  type ChatCall,
  type ChatCallDetail,
  type ChatCallSummary,
} from './chatCallApi'
import { useChatCalls } from './chatCallContext'

// timeOfDay is when the call started, as a time of day: the chat's day dividers
// say which day.
function timeOfDay(value: string): string {
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime())
    ? ''
    : parsed.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })
}

// ChatCallLine is a call's line in the chat's transcript: how it went, in a
// few words, and what can be done about it. The message that carries it has
// no words of its own; this is drawn from the call it names, and follows the
// call as ChatCallEvents tells of it, so a line that said "Call going" says
// how long it lasted when it ends.
export function ChatCallLine({
  callId,
  call,
}: {
  callId: string
  // The call as the message came with it.
  call: ChatCallSummary | null | undefined
}) {
  const { t } = useTranslation()
  const { calls, activeCall, isAvailable, isBusy, joinCall } = useChatCalls()
  const [isTranscriptOpen, setTranscriptOpen] = useState(false)
  const live: ChatCall | undefined = calls.find((each) => each.id === callId)
  // An end is final: a call the message already knows is over stays over,
  // whatever this tab last heard of it.
  const summary: ChatCallSummary | null = call?.endedAt ? call : (live ?? call ?? null)
  const state = summary ? chatCallLineState(summary) : null
  const isOnIt = activeCall?.callId === callId || Boolean(live?.isAskerOn)

  let words: string
  switch (state?.kind) {
    case 'going':
      words = t('calls.lineGoing')
      break
    case 'ended':
      words =
        state.durationMS > 0
          ? t('calls.lineEnded', { length: formatCallLength(state.durationMS) })
          : t('calls.lineEndedShort')
      break
    case 'missed':
      words = t('calls.lineMissed')
      break
    case 'declined':
      words = t('calls.lineDeclined')
      break
    case 'failed':
      words = t('calls.lineFailed')
      break
    default:
      words = t('calls.lineEndedShort')
  }

  return (
    <div className="chat-call-line" data-state={state?.kind ?? 'unknown'}>
      <PhoneIcon size={14} />
      <span className="chat-call-words">{words}</span>
      {summary ? <span className="muted chat-call-time">{timeOfDay(summary.startedAt)}</span> : null}
      {state?.kind === 'going' && isAvailable && !isOnIt ? (
        <button type="button" className="primary" disabled={isBusy} onClick={() => void joinCall(callId, false)}>
          {t('calls.join')}
        </button>
      ) : null}
      {summary?.transcriptState === 'done' ? (
        <button type="button" onClick={() => setTranscriptOpen(true)}>
          {t('calls.transcript')}
        </button>
      ) : null}
      {isTranscriptOpen ? <CallTranscriptDialog callId={callId} onClose={() => setTranscriptOpen(false)} /> : null}
    </div>
  )
}

// CallTranscriptDialog reads a call's transcript: each line under the name of
// whoever said it, in the order it was said. The call is a recording, which
// plays it and keeps the corrections; the dialog opens it.
function CallTranscriptDialog({ callId, onClose }: { callId: string; onClose: () => void }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [detail, setDetail] = useState<ChatCallDetail | null>(null)
  const [errorMessage, setErrorMessage] = useState<string | null>(null)
  const transcript = detail?.transcript ?? null
  useEffect(() => {
    let isCurrent = true
    graphql<{ ReadChatCall: ChatCallDetail }>(READ_CHAT_CALL, { callId })
      .then((read) => {
        if (isCurrent) setDetail(read.ReadChatCall)
      })
      .catch((caught) => {
        if (isCurrent) setErrorMessage(caught instanceof Error ? caught.message : String(caught))
      })
    return () => {
      isCurrent = false
    }
  }, [callId])
  return createPortal(
    <ConfirmDialog
      title={t('calls.transcriptTitle')}
      wide
      destructive={false}
      error={errorMessage}
      otherAction={
        detail?.recordingId ? (
          <button
            type="button"
            onClick={() => {
              const id = encodeURIComponent(detail.recordingId ?? '')
              onClose()
              navigate(detail.isRecordingOwned ? `/recordings/${id}` : `/recordings/shared/${id}`)
            }}
          >
            {t('calls.openRecording')}
          </button>
        ) : undefined
      }
      body={
        transcript === null ? (
          errorMessage ? null : (
            <p className="muted">{t('common.loading')}</p>
          )
        ) : transcript.length === 0 ? (
          <p className="muted">{t('calls.transcriptEmpty')}</p>
        ) : (
          <ul className="call-transcript">
            {transcript.map((segment) => (
              <li key={segment.id}>
                <span className="call-transcript-speaker">{segment.speakerName}</span>
                <span className="muted call-transcript-time">{formatCallLength(segment.startMs)}</span>
                <p>{segment.text}</p>
              </li>
            ))}
          </ul>
        )
      }
      onClose={onClose}
    />,
    document.body,
  )
}
