import { useEffect, useRef, useState, useSyncExternalStore } from 'react'

import { Avatar } from '../components/avatar'
import {
  CameraSwitchIcon,
  DocumentIcon,
  ExpandIcon,
  MicrophoneIcon,
  MicrophoneOffIcon,
  MinimizeIcon,
  PhoneHangUpIcon,
  VideoIcon,
  VideoOffIcon,
} from '../components/icons'
import { useToast } from '../components/toast'
import { Tooltip } from '../components/tooltip'
import { useTranslation } from '../i18n/i18n'
import { useOptionalSession } from '../session'
import { formatCallLength, type ChatCall, type ChatCallMember } from './chatCallApi'
import { useChatCalls, type ActiveChatCall } from './chatCallContext'
import type { ChatCallSession } from './chatCallSession'
import { watchVoice } from './voiceActivity'

// The call this tab is on, drawn three ways from one connection: the sound of
// everybody else (always, whatever else is on screen), the call screen over
// the page, and the small window the screen can be shrunk to.

function useSessionSnapshot(session: ChatCallSession) {
  return useSyncExternalStore(session.subscribe, session.getSnapshot)
}

// useElapsed counts the seconds since a time, once a second, for the clock of
// a call.
function useElapsed(since: string | null): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!since) return
    setNow(Date.now())
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [since])
  return since ? Math.max(0, now - new Date(since).getTime()) : 0
}

// RemoteAudio plays one person's microphone. A stream is not an address, so it
// is set on the element rather than in a src.
function RemoteAudio({ stream }: { stream: MediaStream }) {
  const element = useRef<HTMLAudioElement>(null)
  useEffect(() => {
    if (element.current && element.current.srcObject !== stream) element.current.srcObject = stream
  }, [stream])
  return <audio ref={element} autoPlay hidden />
}

// CallAudio plays everybody else on the call. It is drawn whether the call is
// shown whole or shrunk, so that shrinking the call never cuts the sound.
export function CallAudio() {
  const { activeCall } = useChatCalls()
  if (!activeCall) return null
  return <CallAudioStreams session={activeCall.session} />
}

function CallAudioStreams({ session }: { session: ChatCallSession }) {
  const snapshot = useSessionSnapshot(session)
  return (
    <>
      {Object.entries(snapshot.participants).map(([userId, participant]) =>
        participant.audioStream ? <RemoteAudio key={userId} stream={participant.audioStream} /> : null,
      )}
    </>
  )
}

// CallPicture shows a stream of video. Muted: the sound of a person comes by
// their audio, and a picture that played it too would be heard twice.
function CallPicture({ stream, isMirrored }: { stream: MediaStream; isMirrored: boolean }) {
  const element = useRef<HTMLVideoElement>(null)
  useEffect(() => {
    if (element.current && element.current.srcObject !== stream) element.current.srcObject = stream
  }, [stream])
  return (
    <video
      ref={element}
      className={isMirrored ? 'call-picture mirrored' : 'call-picture'}
      autoPlay
      playsInline
      muted
      aria-hidden="true"
    />
  )
}

// CallTile is one person on the call: their picture while their camera is on
// and it has arrived, otherwise their avatar and name, with a ring that lights
// while they speak.
function CallTile({
  member,
  isSelf,
  voiceStream,
  videoStream,
}: {
  member: ChatCallMember
  isSelf: boolean
  voiceStream: MediaStream | null
  videoStream: MediaStream | null
}) {
  const { t } = useTranslation()
  const [isSpeaking, setSpeaking] = useState(false)
  useEffect(() => {
    if (!voiceStream) return
    return watchVoice(voiceStream, setSpeaking)
  }, [voiceStream])
  const isPictureShown = member.isVideoOn && videoStream !== null
  return (
    <li className={['call-tile', isSpeaking ? 'speaking' : ''].filter(Boolean).join(' ')}>
      {isPictureShown ? (
        <CallPicture stream={videoStream} isMirrored={isSelf} />
      ) : (
        <Avatar name={member.name} avatarUrl={member.avatarUrl} colorIndex={member.avatarColorIndex} size={72} />
      )}
      <span className="call-tile-name">
        {member.name}
        {isSelf ? <span className="muted"> {t('calls.you')}</span> : null}
      </span>
    </li>
  )
}

// callStatusOf is the line under the call's name: how it is going.
function callStatusOf(
  call: ChatCall | undefined,
  isJoining: boolean,
  isConnectionLost: boolean,
  elapsedMS: number,
  t: ReturnType<typeof useTranslation>['t'],
): string {
  if (isJoining) return t('calls.connecting')
  if (isConnectionLost) return t('calls.reconnecting')
  if (call?.answeredAt) return formatCallLength(elapsedMS)
  return t('calls.ringing')
}

export function CallScreen() {
  const { activeCall } = useChatCalls()
  if (!activeCall) return null
  return <CallScreenView activeCall={activeCall} />
}

function CallScreenView({ activeCall }: { activeCall: ActiveChatCall }) {
  const { t } = useTranslation()
  const toast = useToast()
  const session = useOptionalSession()
  const { calls, setMinimized, leaveCall, askTranscription, answerTranscription } = useChatCalls()
  const snapshot = useSessionSnapshot(activeCall.session)
  const call = calls.find((each) => each.id === activeCall.callId)
  const elapsedMS = useElapsed(call?.answeredAt ?? null)
  const ownUserId = session?.userId ?? ''

  // Escape puts the call away to a small window, not an end to it.
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !event.defaultPrevented) setMinimized(true)
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [setMinimized])

  // The people on the call; this person is always there, even before the
  // server's picture of the call has them.
  const people: ChatCallMember[] = (call?.members ?? []).filter((member) => member.isOn)
  if (ownUserId && !people.some((member) => member.userId === ownUserId)) {
    people.unshift({
      userId: ownUserId,
      name: session?.name || session?.username || '',
      avatarUrl: session?.avatarUrl ?? null,
      avatarColorIndex: session?.avatarColorIndex ?? 0,
      ringState: 'answered',
      isOn: true,
      isVideoOn: snapshot.isVideoOn,
      transcriptionConsent: '',
      joinedAt: null,
      leftAt: null,
    })
  }

  const asker = call?.members.find((member) => member.userId === call.transcriptionAskedByUserId)
  const own = call?.members.find((member) => member.userId === ownUserId)
  const isAsked = Boolean(call?.transcriptionAskedByUserId)
  const isConsentWaiting = isAsked && own !== undefined && own.transcriptionConsent === ''
  const isTranscribing = isAsked && (call?.members ?? []).some((member) => member.transcriptionConsent === 'yes')

  const tellCameraFailure = (caught: unknown) => toast.failure(caught, t('calls.cameraFailed'))

  return (
    <div className="call-screen-scrim">
      <div className="call-screen" role="dialog" aria-label={t('calls.callIn', { title: activeCall.chatTitle })}>
        <div className="call-screen-head">
          <div className="call-screen-title">
            <strong>{activeCall.chatTitle}</strong>
            <span className="muted call-screen-status">
              {callStatusOf(call, snapshot.phase === 'joining', snapshot.isConnectionLost, elapsedMS, t)}
            </span>
          </div>
          {isTranscribing ? (
            <span className="call-transcribing">
              <span className="call-transcribing-dot" aria-hidden="true" />
              {t('calls.transcribing')}
            </span>
          ) : null}
          <Tooltip label={t('calls.minimize')}>
            <button
              type="button"
              className="icon-button"
              aria-label={t('calls.minimize')}
              onClick={() => setMinimized(true)}
            >
              <MinimizeIcon size={18} />
            </button>
          </Tooltip>
        </div>

        {isConsentWaiting ? (
          <div
            className="call-consent"
            role="group"
            aria-label={t('calls.transcriptionAsked', { name: asker?.name ?? t('chat.someone') })}
          >
            <div className="call-consent-words">
              <strong>{t('calls.transcriptionAsked', { name: asker?.name ?? t('chat.someone') })}</strong>
              <span className="muted">{t('calls.transcriptionHint')}</span>
            </div>
            <div className="call-consent-actions">
              <button type="button" className="primary" onClick={() => void answerTranscription(true)}>
                {t('calls.transcriptionAllow')}
              </button>
              <button type="button" onClick={() => void answerTranscription(false)}>
                {t('calls.transcriptionRefuse')}
              </button>
            </div>
          </div>
        ) : null}

        <ul className="call-grid" data-count={Math.min(people.length, 9)}>
          {people.map((member) => {
            const isSelf = member.userId === ownUserId
            const received = snapshot.participants[member.userId]
            return (
              <CallTile
                key={member.userId}
                member={isSelf ? { ...member, isVideoOn: snapshot.isVideoOn } : member}
                isSelf={isSelf}
                voiceStream={isSelf ? snapshot.localAudioStream : (received?.audioStream ?? null)}
                videoStream={isSelf ? snapshot.localVideoStream : (received?.videoStream ?? null)}
              />
            )
          })}
        </ul>

        <div className="call-bar" role="toolbar" aria-label={t('calls.controls')}>
          <Tooltip label={snapshot.isMuted ? t('calls.unmute') : t('calls.mute')}>
            <button
              type="button"
              className={['call-control', snapshot.isMuted ? 'is-off' : ''].filter(Boolean).join(' ')}
              aria-label={snapshot.isMuted ? t('calls.unmute') : t('calls.mute')}
              aria-pressed={snapshot.isMuted}
              onClick={() => activeCall.session.setMuted(!snapshot.isMuted)}
            >
              {snapshot.isMuted ? <MicrophoneOffIcon size={20} /> : <MicrophoneIcon size={20} />}
            </button>
          </Tooltip>
          <Tooltip label={snapshot.isVideoOn ? t('calls.cameraOff') : t('calls.cameraOn')}>
            <button
              type="button"
              className={['call-control', snapshot.isVideoOn ? '' : 'is-off'].filter(Boolean).join(' ')}
              aria-label={snapshot.isVideoOn ? t('calls.cameraOff') : t('calls.cameraOn')}
              aria-pressed={snapshot.isVideoOn}
              onClick={() => void activeCall.session.setVideo(!snapshot.isVideoOn).catch(tellCameraFailure)}
            >
              {snapshot.isVideoOn ? <VideoIcon size={20} /> : <VideoOffIcon size={20} />}
            </button>
          </Tooltip>
          {snapshot.canSwitchCamera ? (
            <Tooltip label={t('calls.switchCamera')}>
              <button
                type="button"
                className="call-control"
                aria-label={t('calls.switchCamera')}
                onClick={() => void activeCall.session.switchCamera().catch(tellCameraFailure)}
              >
                <CameraSwitchIcon size={20} />
              </button>
            </Tooltip>
          ) : null}
          {call && !isAsked ? (
            <Tooltip label={t('calls.transcribe')}>
              <button
                type="button"
                className="call-control"
                aria-label={t('calls.transcribe')}
                onClick={() => void askTranscription()}
              >
                <DocumentIcon size={20} />
              </button>
            </Tooltip>
          ) : null}
          <Tooltip label={t('calls.leave')}>
            <button
              type="button"
              className="call-control is-leaving"
              aria-label={t('calls.leave')}
              onClick={() => void leaveCall()}
            >
              <PhoneHangUpIcon size={20} />
            </button>
          </Tooltip>
        </div>
      </div>
    </div>
  )
}

// CallMiniWindow is the call while the dashboard is being used: who is on it,
// the clock, and the controls that matter without looking: mute and leave.
// Pressing its words brings the whole call back.
export function CallMiniWindow() {
  const { activeCall } = useChatCalls()
  if (!activeCall) return null
  return <CallMiniView activeCall={activeCall} />
}

function CallMiniView({ activeCall }: { activeCall: ActiveChatCall }) {
  const { t } = useTranslation()
  const { calls, setMinimized, leaveCall } = useChatCalls()
  const snapshot = useSessionSnapshot(activeCall.session)
  const call = calls.find((each) => each.id === activeCall.callId)
  const elapsedMS = useElapsed(call?.answeredAt ?? null)
  const status = callStatusOf(call, snapshot.phase === 'joining', snapshot.isConnectionLost, elapsedMS, t)
  return (
    <div className="call-mini" role="group" aria-label={t('calls.callIn', { title: activeCall.chatTitle })}>
      <button
        type="button"
        className="call-mini-main"
        aria-label={t('calls.restore')}
        onClick={() => setMinimized(false)}
      >
        <span className="call-mini-dot" aria-hidden="true" />
        <span className="call-mini-title">{activeCall.chatTitle}</span>
        <span className="muted call-mini-status">{status}</span>
      </button>
      <div className="row-actions">
        <Tooltip label={snapshot.isMuted ? t('calls.unmute') : t('calls.mute')}>
          <button
            type="button"
            className="icon-action"
            aria-label={snapshot.isMuted ? t('calls.unmute') : t('calls.mute')}
            aria-pressed={snapshot.isMuted}
            onClick={() => activeCall.session.setMuted(!snapshot.isMuted)}
          >
            {snapshot.isMuted ? <MicrophoneOffIcon size={16} /> : <MicrophoneIcon size={16} />}
          </button>
        </Tooltip>
        <Tooltip label={t('calls.restore')}>
          <button
            type="button"
            className="icon-action"
            aria-label={t('calls.restore')}
            onClick={() => setMinimized(false)}
          >
            <ExpandIcon size={16} />
          </button>
        </Tooltip>
        <Tooltip label={t('calls.leave')}>
          <button
            type="button"
            className="icon-action danger"
            aria-label={t('calls.leave')}
            onClick={() => void leaveCall()}
          >
            <PhoneHangUpIcon size={16} />
          </button>
        </Tooltip>
      </div>
    </div>
  )
}
