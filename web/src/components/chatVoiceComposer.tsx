import { useCallback, useEffect, useRef, useState } from 'react'

import { graphql, type AgentReference } from '../api'
import { useTranslation } from '../i18n/i18n'
import { uploadFiles } from '../upload'
import {
  COUNTDOWN_VOICE_MESSAGE_MS,
  LONGEST_VOICE_MESSAGE_MS,
  SHORTEST_VOICE_MESSAGE_MS,
  VoiceMessageRecorder,
  clockOf,
} from '../voice/voiceMessageRecorder'
import { VoiceMeter } from '../voice/voiceMeter'
import { KeyboardIcon } from './icons'
import { useToast } from './toast'
import { Tooltip } from './tooltip'

// The composer of a people chat has two modes, like a messaging app's: the
// keyboard, with the text box, and voice, where the box is replaced by one
// wide button that records while it is held. Release sends the recording
// as a voice message, sliding up and releasing throws it away.

// CHAT_COMPOSER_MODE_KEY is where the mode is kept in this browser, for
// every chat. Agent conversations always use the keyboard.
export const CHAT_COMPOSER_MODE_KEY = 'teanode.chatComposerMode'

export type ChatComposerMode = 'keyboard' | 'voice'

function storedMode(): ChatComposerMode {
  try {
    return localStorage.getItem(CHAT_COMPOSER_MODE_KEY) === 'voice' ? 'voice' : 'keyboard'
  } catch {
    return 'keyboard'
  }
}

// useChatComposerMode is the mode the composer is in, kept across reloads.
export function useChatComposerMode(): [ChatComposerMode, (mode: ChatComposerMode) => void] {
  const [mode, setModeState] = useState<ChatComposerMode>(storedMode)
  const setMode = useCallback((next: ChatComposerMode) => {
    setModeState(next)
    try {
      localStorage.setItem(CHAT_COMPOSER_MODE_KEY, next)
    } catch {
      // A browser that keeps nothing forgets the mode, which is fine.
    }
  }, [])
  return [mode, setMode]
}

// SLIDE_TO_CANCEL_PX is how far up from where the press began the finger or
// pointer must go for a release to cancel.
export const SLIDE_TO_CANCEL_PX = 60

const UPLOAD_VOICE_MESSAGE = '/api/v1/agent/attachments?isVoiceMessage=true'

const ASK_VOICE_MESSAGE = `
  mutation ($conversationId: String, $message: String!, $voiceMessageAttachmentId: String, $surface: String, $references: [AgentReferenceInput!], $drawerId: String) {
    AskAgent(conversationId: $conversationId, message: $message, voiceMessageAttachmentId: $voiceMessageAttachmentId, surface: $surface, references: $references, drawerId: $drawerId) { runId conversationId }
  }`

type Phase = 'idle' | 'starting' | 'recording' | 'sending'

// isHoldKey is a key that holds the button down when it is focused.
function isHoldKey(key: string): boolean {
  return key === ' ' || key === 'Enter'
}

// ChatVoiceComposer is the composer in voice mode: the way back to the
// keyboard and the button that is held to talk. It records, uploads and
// asks; the chat itself draws the message when the server says it was said.
export function ChatVoiceComposer({
  conversationId,
  references,
  surface,
  drawerId,
  isDisabled = false,
  onSent,
  onKeyboard,
}: {
  conversationId: string
  // The message being replied to, if one is: the voice message answers it.
  references: AgentReference[]
  surface: string
  drawerId: string
  isDisabled?: boolean
  onSent: (conversationId: string) => void
  onKeyboard: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [phase, setPhaseState] = useState<Phase>('idle')
  const phaseNow = useRef<Phase>('idle')
  const [elapsedMS, setElapsedMS] = useState(0)
  const [isCancelArmed, setCancelArmed] = useState(false)
  const isCancelArmedNow = useRef(false)
  const recorder = useRef<VoiceMessageRecorder | null>(null)
  // What began the recording, and whether it ended before the microphone
  // was open: the press then decides what happens when it arrives.
  const holdBy = useRef<'pointer' | 'keyboard' | null>(null)
  const endedStart = useRef<'released' | 'canceled' | null>(null)
  const pressedAtY = useRef(0)
  const latest = useRef({ references, surface, drawerId, onSent })
  latest.current = { references, surface, drawerId, onSent }

  const setPhase = (next: Phase) => {
    phaseNow.current = next
    setPhaseState(next)
  }
  const setArmed = (isArmed: boolean) => {
    isCancelArmedNow.current = isArmed
    setCancelArmed(isArmed)
  }
  const level = useCallback(() => recorder.current?.level() ?? 0, [])

  // send uploads a recording and asks for it to be said in the chat. A
  // recording that could not go stays in memory for a second try.
  const send = async (file: File, sendingConversationId: string) => {
    try {
      const uploaded = (await uploadFiles('POST', UPLOAD_VOICE_MESSAGE, [file], () => undefined).promise) as {
        attachments?: { id: string }[]
      } | null
      const voiceMessageAttachmentId = uploaded?.attachments?.[0]?.id
      if (!voiceMessageAttachmentId) throw new Error(t('chat.voiceMessageNotSent'))
      const { references: replying, surface: surfaceNow, drawerId: drawerNow } = latest.current
      await graphql(ASK_VOICE_MESSAGE, {
        conversationId: sendingConversationId,
        message: '',
        voiceMessageAttachmentId,
        surface: surfaceNow,
        references: replying.length > 0 ? replying : undefined,
        drawerId: drawerNow,
      })
      latest.current.onSent(sendingConversationId)
    } catch (caught) {
      toast.failed(caught instanceof Error && caught.message ? caught.message : t('chat.voiceMessageNotSent'), {
        label: t('common.tryAgain'),
        run: () => {
          setPhase('sending')
          return send(file, sendingConversationId)
        },
      })
    } finally {
      setPhase('idle')
    }
  }

  const finish = async () => {
    if (phaseNow.current === 'starting') {
      endedStart.current = 'released'
      return
    }
    const active = recorder.current
    if (phaseNow.current !== 'recording' || !active) return
    setPhase('sending')
    recorder.current = null
    const recorded = await active.finish()
    if (!recorded || recorded.durationMS < SHORTEST_VOICE_MESSAGE_MS) {
      setPhase('idle')
      if (recorded) toast.done(t('chat.holdToRecordHint'))
      return
    }
    await send(recorded.file, conversationId)
  }

  const cancel = useCallback(() => {
    if (phaseNow.current === 'starting') {
      endedStart.current = 'canceled'
      return
    }
    if (phaseNow.current !== 'recording') return
    recorder.current?.cancel()
    recorder.current = null
    phaseNow.current = 'idle'
    setPhaseState('idle')
  }, [])

  const begin = async (by: 'pointer' | 'keyboard') => {
    if (phaseNow.current !== 'idle' || isDisabled) return
    const started = new VoiceMessageRecorder()
    recorder.current = started
    holdBy.current = by
    endedStart.current = null
    setElapsedMS(0)
    setArmed(false)
    setPhase('starting')
    let outcome: Awaited<ReturnType<VoiceMessageRecorder['start']>>
    try {
      outcome = await started.start()
    } catch (caught) {
      recorder.current = null
      setPhase('idle')
      const detail = caught instanceof Error && caught.message ? ` ${caught.message}` : ''
      toast.failed(`${t('agentDrawer.voiceMicrophoneRefused')}${detail}`)
      return
    }
    if (outcome === 'asked') {
      // The browser asked for the microphone in this press. Nothing was
      // recorded; the next press does.
      recorder.current = null
      setPhase('idle')
      toast.done(t('chat.holdAgain'))
      return
    }
    if (outcome === 'canceled' || endedStart.current) {
      started.cancel()
      recorder.current = null
      setPhase('idle')
      if (endedStart.current === 'released') toast.done(t('chat.holdToRecordHint'))
      return
    }
    setPhase('recording')
  }

  // The time shown, and the end of a recording that has gone on as long as
  // one may.
  useEffect(() => {
    if (phase !== 'recording') return
    const timer = window.setInterval(() => {
      const elapsed = recorder.current?.elapsedMS() ?? 0
      setElapsedMS(elapsed)
      if (elapsed >= LONGEST_VOICE_MESSAGE_MS) void finish()
    }, 100)
    return () => window.clearInterval(timer)
    // finish reads only refs and the conversation it was made for.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [phase, conversationId])

  // Escape, losing the window and leaving the tab throw the recording away.
  useEffect(() => {
    if (phase !== 'recording') return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') cancel()
    }
    const onHidden = () => {
      if (document.visibilityState === 'hidden') cancel()
    }
    window.addEventListener('keydown', onKeyDown)
    window.addEventListener('blur', cancel)
    document.addEventListener('visibilitychange', onHidden)
    return () => {
      window.removeEventListener('keydown', onKeyDown)
      window.removeEventListener('blur', cancel)
      document.removeEventListener('visibilitychange', onHidden)
    }
  }, [phase, cancel])

  // Another chat, or the keyboard, or no chat at all, ends a recording.
  useEffect(() => cancel, [conversationId, cancel])

  const release = () => {
    if (isCancelArmedNow.current) cancel()
    else void finish()
  }
  const isRecording = phase === 'recording'
  const remainingMS = LONGEST_VOICE_MESSAGE_MS - elapsedMS
  const isEnding = elapsedMS >= COUNTDOWN_VOICE_MESSAGE_MS

  return (
    <div className="agent-drawer-input chat-voice-composer">
      {isRecording && (
        <div
          className={['chat-voice-strip', isCancelArmed ? 'is-canceling' : ''].filter(Boolean).join(' ')}
          role="status"
        >
          <VoiceMeter level={level} isHearing={!isCancelArmed} isSpeaking={false} isAnswering={false} />
          <span className={['chat-voice-time', isEnding ? 'is-ending' : ''].filter(Boolean).join(' ')}>
            {clockOf(isEnding ? remainingMS : elapsedMS)}
          </span>
          <span className="chat-voice-hint">
            {isCancelArmed ? t('chat.releaseToCancel') : t('chat.slideUpToCancel')}
          </span>
        </div>
      )}
      <Tooltip label={t('chat.useKeyboard')}>
        <button
          type="button"
          className="icon-button"
          aria-label={t('chat.useKeyboard')}
          disabled={phase === 'sending'}
          onClick={onKeyboard}
        >
          <KeyboardIcon size={18} />
        </button>
      </Tooltip>
      <button
        type="button"
        className={['chat-hold-to-talk', isRecording ? 'is-recording' : '', isCancelArmed ? 'is-canceling' : '']
          .filter(Boolean)
          .join(' ')}
        disabled={isDisabled || phase === 'sending'}
        aria-busy={phase === 'sending'}
        aria-keyshortcuts="Space Enter"
        onPointerDown={(event) => {
          if (event.pointerType === 'mouse' && event.button !== 0) return
          event.currentTarget.setPointerCapture?.(event.pointerId)
          pressedAtY.current = event.clientY
          void begin('pointer')
        }}
        onPointerMove={(event) => {
          if (holdBy.current !== 'pointer' || phaseNow.current === 'idle' || phaseNow.current === 'sending') return
          setArmed(pressedAtY.current - event.clientY > SLIDE_TO_CANCEL_PX)
        }}
        onPointerUp={() => {
          if (holdBy.current === 'pointer') release()
        }}
        onPointerCancel={() => {
          if (holdBy.current === 'pointer') cancel()
        }}
        onKeyDown={(event) => {
          if (isHoldKey(event.key)) {
            event.preventDefault()
            if (!event.repeat) void begin('keyboard')
          } else if (event.key === 'Escape') {
            cancel()
          }
        }}
        onKeyUp={(event) => {
          if (!isHoldKey(event.key)) return
          event.preventDefault()
          if (holdBy.current === 'keyboard') release()
        }}
        onBlur={cancel}
        onContextMenu={(event) => event.preventDefault()}
      >
        {t('chat.holdToTalk')}
      </button>
    </div>
  )
}
