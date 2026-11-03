import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react'

import { Avatar } from '../components/avatar'
import {
  CameraSwitchIcon,
  CheckIcon,
  CloseIcon,
  DocumentIcon,
  ExpandIcon,
  MicrophoneIcon,
  MicrophoneOffIcon,
  MinimizeIcon,
  PhoneHangUpIcon,
  PinIcon,
  SpeakerIcon,
  VideoIcon,
  VideoOffIcon,
} from '../components/icons'
import { MenuButton } from '../components/menuButton'
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

// canChooseSoundOutput is whether this browser can play an audio element on a
// device other than the default one. Safari and Firefox may not.
export function canChooseSoundOutput(): boolean {
  return typeof HTMLMediaElement !== 'undefined' && 'setSinkId' in HTMLMediaElement.prototype
}

// RemoteAudio plays one person's microphone. A stream is not an address, so it
// is set on the element rather than in a src. It plays on the device the call
// is set to, whenever that changes and on first appearing, so that a person who
// arrives after the choice is heard where the others are.
function RemoteAudio({
  stream,
  soundOutputDeviceId,
  onSoundOutputFailed,
}: {
  stream: MediaStream
  soundOutputDeviceId: string
  onSoundOutputFailed: (caught: unknown) => void
}) {
  const element = useRef<HTMLAudioElement>(null)
  useEffect(() => {
    if (element.current && element.current.srcObject !== stream) element.current.srcObject = stream
  }, [stream])
  useEffect(() => {
    const audio = element.current as (HTMLAudioElement & { setSinkId?: (deviceId: string) => Promise<void> }) | null
    // An element already on the default device is left alone, so that a call
    // that never chose one never asks for it.
    if (!audio?.setSinkId || (audio.sinkId ?? '') === soundOutputDeviceId) return
    audio.setSinkId(soundOutputDeviceId).catch(onSoundOutputFailed)
  }, [soundOutputDeviceId, onSoundOutputFailed])
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
  const { t } = useTranslation()
  const toast = useToast()
  const snapshot = useSessionSnapshot(session)
  // A device that was taken away since it was chosen cannot be played on.
  const tellSoundOutputFailure = useCallback(
    (caught: unknown) => toast.failure(caught, t('calls.soundOutputFailed')),
    [t, toast],
  )
  return (
    <>
      {Object.entries(snapshot.participants).map(([userId, participant]) =>
        participant.audioStream ? (
          <RemoteAudio
            key={userId}
            stream={participant.audioStream}
            soundOutputDeviceId={snapshot.soundOutputDeviceId}
            onSoundOutputFailed={tellSoundOutputFailure}
          />
        ) : null,
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

// FEATURED_FROM is how many people on a call turn the grid into one person
// featured above a row of the rest; choosing somebody to feature does too.
export const FEATURED_FROM = 5

// featuredOf is who is featured: the one chosen, or else whoever is speaking,
// or whoever spoke last while this person speaks, or else the first of the
// others. Never this person: their own picture is in the row.
export function featuredOf(
  people: ChatCallMember[],
  ownUserId: string,
  chosenUserId: string,
  speakingUserId: string,
  lastOtherSpeakerId: string,
): ChatCallMember | null {
  const others = people.filter((member) => member.userId !== ownUserId)
  const named = (userId: string) => (userId ? others.find((member) => member.userId === userId) : undefined)
  return named(chosenUserId) ?? named(speakingUserId) ?? named(lastOtherSpeakerId) ?? others[0] ?? null
}

// CallTile is one person on the call: their picture while their camera is on
// and it has arrived, otherwise their avatar and name, with a ring that lights
// while they speak. Pressing somebody else's features them, or, when they are
// the one chosen, goes back to following whoever speaks.
function CallTile({
  member,
  isSelf,
  isChosen,
  voiceStream,
  videoStream,
  onChoose,
}: {
  member: ChatCallMember
  isSelf: boolean
  isChosen: boolean
  voiceStream: MediaStream | null
  videoStream: MediaStream | null
  onChoose: () => void
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
      {isSelf ? null : (
        <button
          type="button"
          className="call-tile-choose"
          aria-label={
            isChosen ? t('calls.stopFeaturing', { name: member.name }) : t('calls.feature', { name: member.name })
          }
          aria-pressed={isChosen}
          onClick={onChoose}
        />
      )}
      <span className="call-tile-name">
        {isChosen ? (
          <span className="call-tile-muted" aria-hidden="true">
            <PinIcon size={12} />
          </span>
        ) : null}
        {member.isMuted ? (
          <span className="call-tile-muted" role="img" aria-label={t('calls.muted')}>
            <MicrophoneOffIcon size={12} />
          </span>
        ) : null}
        <span className="call-tile-name-text">
          {member.name}
          {isSelf ? <span className="muted"> {t('calls.you')}</span> : null}
        </span>
      </span>
    </li>
  )
}

// isDefaultDevice is the entry a browser lists for the system's default
// device, or the empty id a choice of it is kept as.
function isDefaultDevice(deviceId: string): boolean {
  return deviceId === '' || deviceId === 'default'
}

// isDeviceInUse is whether a device listed is the one chosen. The default
// device is listed under an id of its own and chosen as the empty id, and the
// browser reports the id of the default microphone either way.
function isDeviceInUse(device: MediaDeviceInfo, chosenDeviceId: string): boolean {
  return device.deviceId === chosenDeviceId || (isDefaultDevice(chosenDeviceId) && isDefaultDevice(device.deviceId))
}

// useAudioDevices is the microphones and loudspeakers of this device, read
// when it is first used and again whenever one is plugged in or taken away.
function useAudioDevices(session: ChatCallSession) {
  const [devices, setDevices] = useState<{ microphones: MediaDeviceInfo[]; speakers: MediaDeviceInfo[] }>({
    microphones: [],
    speakers: [],
  })
  useEffect(() => {
    let isCancelled = false
    const read = () => {
      session
        .listAudioDevices()
        .then((listed) => {
          if (!isCancelled) setDevices(listed)
        })
        // A list that cannot be read is an empty menu, not a failed call.
        .catch(() => {})
    }
    read()
    const stopWatching = session.watchDeviceChanges(read)
    return () => {
      isCancelled = true
      stopWatching()
    }
  }, [session])
  return devices
}

// AudioMenu is where the sound of the call plays and which microphone it
// uses, as the iPhone app lets a person choose the speaker, a headset or the
// phone. The sound section is left out where the browser cannot play on
// another device. The device in use is ticked.
function AudioMenu({ session, close }: { session: ChatCallSession; close: () => void }) {
  const { t } = useTranslation()
  const toast = useToast()
  const snapshot = useSessionSnapshot(session)
  const { microphones, speakers } = useAudioDevices(session)

  // The names are empty until the browser has allowed a microphone. The
  // default device is then still called what it is, and any other is numbered.
  const nameOf = (device: MediaDeviceInfo, index: number) => {
    if (device.label) return device.label
    return isDefaultDevice(device.deviceId) ? t('calls.systemDefault') : t('calls.deviceNumber', { number: index + 1 })
  }

  // Every browser can be taken back to the default device, even one that does
  // not list it.
  const soundOutputs: MediaDeviceInfo[] = speakers.some((device) => isDefaultDevice(device.deviceId))
    ? speakers
    : [{ deviceId: '', groupId: '', kind: 'audiooutput', label: '' } as MediaDeviceInfo, ...speakers]
  const namedMicrophones = microphones.filter((device) => device.deviceId)

  const chooseMicrophone = (deviceId: string) => {
    close()
    session.setMicrophone(deviceId).catch((caught) => toast.failure(caught, t('calls.microphoneFailed')))
  }
  const chooseSoundOutput = (deviceId: string) => {
    close()
    session.setSoundOutput(deviceId)
  }

  const choices = (
    heading: string,
    devices: MediaDeviceInfo[],
    chosenDeviceId: string,
    onChoose: (deviceId: string) => void,
  ) => (
    <div role="group" aria-label={heading}>
      <div className="menu-header" aria-hidden="true">
        {heading}
      </div>
      {devices.map((device, index) => {
        const isInUse = isDeviceInUse(device, chosenDeviceId)
        return (
          <button
            key={device.deviceId || 'system-default'}
            type="button"
            className={isInUse ? 'selected' : undefined}
            aria-pressed={isInUse}
            onClick={() => onChoose(device.deviceId)}
          >
            <span className="menu-check" aria-hidden="true">
              {isInUse && <CheckIcon size={14} />}
            </span>
            {nameOf(device, index)}
          </button>
        )
      })}
    </div>
  )

  return (
    <>
      {canChooseSoundOutput()
        ? choices(t('calls.soundOutput'), soundOutputs, snapshot.soundOutputDeviceId, chooseSoundOutput)
        : null}
      {canChooseSoundOutput() && namedMicrophones.length > 0 ? (
        <div className="menu-separator" role="separator" />
      ) : null}
      {namedMicrophones.length > 0
        ? choices(t('calls.microphone'), namedMicrophones, snapshot.microphoneDeviceId, chooseMicrophone)
        : null}
    </>
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
  const { calls, setMinimized, leaveCall, askTranscription } = useChatCalls()
  const snapshot = useSessionSnapshot(activeCall.session)
  const call = calls.find((each) => each.id === activeCall.callId)
  const elapsedMS = useElapsed(call?.answeredAt ?? null)
  const ownUserId = session?.userId ?? ''

  // Escape puts the call away to a small window, not an end to it. Unless a
  // menu of the bar is open: its Escape closes it, and this one runs first.
  const bar = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented) return
      if (bar.current?.querySelector('[aria-expanded="true"]')) return
      setMinimized(true)
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
      isMuted: snapshot.isMuted,
      joinedAt: null,
      leftAt: null,
    })
  }

  // Whoever turns transcription on turns it on for everybody; each person is
  // told once, in a notice they close, and the mark in the head stays.
  const asker = call?.members.find((member) => member.userId === call.transcriptionAskedByUserId)
  const isTranscribing = Boolean(call?.transcriptionAskedByUserId)
  const [noticeClosedCallId, setNoticeClosedCallId] = useState('')
  const isNoticeShown = isTranscribing && noticeClosedCallId !== activeCall.callId
  const noticeTitle =
    asker?.userId === ownUserId
      ? t('calls.transcriptionOnBySelf')
      : t('calls.transcriptionOnBy', { name: asker?.name || t('chat.someone') })

  const tellCameraFailure = (caught: unknown) => toast.failure(caught, t('calls.cameraFailed'))

  // Somebody is featured once the call is big enough for the grid's pictures
  // to be small, or once the person chose somebody. Who spoke last before
  // this person is kept, so their own voice does not take the picture away.
  const lastOtherSpeaker = useRef('')
  if (snapshot.speakingUserId && snapshot.speakingUserId !== ownUserId) {
    lastOtherSpeaker.current = snapshot.speakingUserId
  }
  const isFeaturedLayout = people.length >= FEATURED_FROM || snapshot.featuredUserId !== ''
  const featured = isFeaturedLayout
    ? featuredOf(people, ownUserId, snapshot.featuredUserId, snapshot.speakingUserId, lastOtherSpeaker.current)
    : null
  const tileOf = (member: ChatCallMember) => {
    const isSelf = member.userId === ownUserId
    const received = snapshot.participants[member.userId]
    const isChosen = member.userId === snapshot.featuredUserId
    return (
      <CallTile
        key={member.userId}
        member={isSelf ? { ...member, isVideoOn: snapshot.isVideoOn, isMuted: snapshot.isMuted } : member}
        isSelf={isSelf}
        isChosen={isChosen}
        voiceStream={isSelf ? snapshot.localAudioStream : (received?.audioStream ?? null)}
        videoStream={isSelf ? snapshot.localVideoStream : (received?.videoStream ?? null)}
        onChoose={() => activeCall.session.setFeatured(isChosen ? '' : member.userId)}
      />
    )
  }

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

        {isNoticeShown ? (
          <div className="call-notice" role="status" aria-label={noticeTitle}>
            <div className="call-notice-words">
              <strong>{noticeTitle}</strong>
              <span className="muted">{t('calls.transcriptionOnHint')}</span>
            </div>
            <Tooltip label={t('calls.dismiss')}>
              <button
                type="button"
                className="icon-button"
                aria-label={t('calls.dismiss')}
                onClick={() => setNoticeClosedCallId(activeCall.callId)}
              >
                <CloseIcon size={18} />
              </button>
            </Tooltip>
          </div>
        ) : null}

        {featured ? (
          <div className="call-stage">
            <ul className="call-featured">{tileOf(featured)}</ul>
            <ul className="call-strip">{people.filter((member) => member.userId !== featured.userId).map(tileOf)}</ul>
          </div>
        ) : (
          <ul className="call-grid" data-count={Math.min(people.length, 9)}>
            {people.map(tileOf)}
          </ul>
        )}

        <div className="call-bar" role="toolbar" aria-label={t('calls.controls')} ref={bar}>
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
          <Tooltip label={t('calls.audio')}>
            <MenuButton
              className="call-control"
              label={t('calls.audio')}
              icon={<SpeakerIcon size={20} />}
              placement="above"
              popup="dialog"
              render={(close) => <AudioMenu session={activeCall.session} close={close} />}
            />
          </Tooltip>
          {call && !isTranscribing ? (
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
