import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react'

import { graphql, subscribe } from '../api'
import { useToast } from '../components/toast'
import { useTranslation } from '../i18n/i18n'
import { useOptionalSession } from '../session'
import { CallAudio, CallMiniWindow, CallScreen } from './callScreen'
import {
  ASK_CHAT_CALL_TRANSCRIPTION,
  CHAT_CALL_EVENTS,
  DECLINE_CHAT_CALL,
  JOIN_CHAT_CALL,
  LIST_GOING_CHAT_CALLS,
  READ_GOING_CHAT_CALL,
  START_CHAT_CALL,
  type ChatCall,
} from './chatCallApi'
import { ChatCallsContext, type ActiveChatCall, type ChatCallsValue, type StartCallOptions } from './chatCallContext'
import { ChatCallSession, type ChatCallProblem } from './chatCallSession'
import { ChatCallStore } from './chatCallStore'
import { RingingCards } from './ringingCards'

// CreateSession makes the connection of this device to a call.
export type CreateSession = (options: {
  callId: string
  isVideoOn: boolean
  onProblem: (problem: ChatCallProblem, caught: unknown) => void
}) => ChatCallSession

// browserCanCall is whether this browser can send a microphone to a peer
// connection at all. A page served over plain HTTP, or an old browser, cannot.
export function browserCanCall(): boolean {
  return typeof RTCPeerConnection !== 'undefined' && Boolean(navigator.mediaDevices?.getUserMedia)
}

const createBrowserSession: CreateSession = ({ callId, isVideoOn, onProblem }) =>
  new ChatCallSession({
    callId,
    isVideoOn,
    graphql,
    subscribe,
    // The media server is ICE-lite on a public address: no STUN or TURN.
    createPeerConnection: () => new RTCPeerConnection({ iceServers: [] }),
    mediaDevices: navigator.mediaDevices,
    onProblem,
  })

// ChatCallProvider holds the calls of the whole dashboard: it follows
// ChatCallEvents once for every tab of the page, rings the person on any page
// when a call comes in, and holds the call this tab is on, drawn over
// whatever page is open.
export function ChatCallProvider({
  children,
  isSupported = browserCanCall(),
  createSession = createBrowserSession,
}: {
  children: React.ReactNode
  isSupported?: boolean
  createSession?: CreateSession
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const session = useOptionalSession()
  const isSignedIn = Boolean(session?.userId)
  const [store] = useState(() => new ChatCallStore())
  const calls = useSyncExternalStore(store.subscribe, store.getSnapshot)
  const [activeCall, setActiveCall] = useState<ActiveChatCall | null>(null)
  const [isBusy, setBusy] = useState(false)
  const activeCallRef = useRef<ActiveChatCall | null>(null)
  activeCallRef.current = activeCall
  const isBusyRef = useRef(false)

  // What a person is told of every call, wherever they are: reading the calls
  // going first, on load and after every reconnect, so that what was missed
  // while the socket was down is not.
  useEffect(() => {
    if (!isSupported || !isSignedIn) return
    return subscribe<{ ChatCallEvents: ChatCall }>(
      CHAT_CALL_EVENTS,
      {},
      (data) => store.apply(data.ChatCallEvents),
      undefined,
      {
        beforeStart: async () => {
          const markedAt = store.writeMark()
          const listed = await graphql<{ ListGoingChatCalls: ChatCall[] }>(LIST_GOING_CHAT_CALLS)
          store.settleGoing(listed.ListGoingChatCalls, markedAt)
        },
      },
    )
  }, [isSupported, isSignedIn, store])

  // A call the server ended (the last of the others left, or it was missed)
  // takes this tab off it.
  useEffect(() => {
    if (!activeCall) return
    const call = calls.find((each) => each.id === activeCall.callId)
    if (call && !call.isGoing) {
      activeCall.session.close()
      setActiveCall(null)
    }
  }, [calls, activeCall])

  // leaveActiveCall takes this tab off the call it is on, and the server too.
  // The screen goes at once: waiting for the server to answer would leave a
  // call on screen that the person has already ended.
  const leaveActiveCall = useCallback(async () => {
    const current = activeCallRef.current
    if (!current) return
    activeCallRef.current = null
    setActiveCall(null)
    try {
      await current.session.leave()
    } catch (caught) {
      toast.failure(caught, t('calls.leaveFailed'))
    }
  }, [t, toast])

  // connect puts this tab on a call the person has joined with the server.
  const connect = useCallback(
    async (call: ChatCall, isVideoOn: boolean) => {
      let hasSaidMediaProblem = false
      const next = createSession({
        callId: call.id,
        isVideoOn,
        onProblem: (problem, caught) => {
          if (problem === 'camera') {
            toast.failure(caught, t('calls.cameraFailed'))
          } else if (!hasSaidMediaProblem) {
            hasSaidMediaProblem = true
            toast.failure(caught, t('calls.mediaFailed'))
          }
        },
      })
      const connected: ActiveChatCall = {
        callId: call.id,
        conversationId: call.conversationId,
        chatTitle: call.chatTitle,
        session: next,
        isMinimized: false,
      }
      activeCallRef.current = connected
      setActiveCall(connected)
      try {
        await next.start()
      } catch (caught) {
        if (activeCallRef.current === connected) {
          activeCallRef.current = null
          setActiveCall(null)
        }
        // The person is on the call as far as the server knows.
        await next.leave().catch(() => {})
        toast.failure(caught, t('calls.connectFailed'))
      }
    },
    [createSession, t, toast],
  )

  // joinWith starts, answers or joins a call with the server, then connects.
  // Only one is under way at a time, and answering one leaves the call this
  // tab is on.
  const joinWith = useCallback(
    async (join: () => Promise<ChatCall>, isVideoOn: boolean, failure: string) => {
      if (isBusyRef.current) return
      isBusyRef.current = true
      setBusy(true)
      try {
        await leaveActiveCall()
        let call = await join()
        store.apply(call)
        // A call going in the chat already is answered by joining it.
        if (!call.isAskerOn) {
          call = (await graphql<{ JoinChatCall: ChatCall }>(JOIN_CHAT_CALL, { callId: call.id })).JoinChatCall
          store.apply(call)
        }
        await connect(call, isVideoOn)
      } catch (caught) {
        toast.failure(caught, failure)
      } finally {
        isBusyRef.current = false
        setBusy(false)
      }
    },
    [connect, leaveActiveCall, store, toast],
  )

  const startCall = useCallback(
    (conversationId: string, options: StartCallOptions = {}) => {
      const isVideoOn = options.isVideoOn ?? false
      return joinWith(
        async () =>
          (
            await graphql<{ StartChatCall: ChatCall }>(START_CHAT_CALL, {
              conversationId,
              isVideoOn,
              userIds: options.userIds ?? null,
            })
          ).StartChatCall,
        isVideoOn,
        t('calls.startFailed'),
      )
    },
    [joinWith, t],
  )

  const joinCall = useCallback(
    (callId: string, isVideoOn: boolean) =>
      joinWith(
        async () => (await graphql<{ JoinChatCall: ChatCall }>(JOIN_CHAT_CALL, { callId })).JoinChatCall,
        isVideoOn,
        t('calls.joinFailed'),
      ),
    [joinWith, t],
  )

  const declineCall = useCallback(
    async (callId: string) => {
      try {
        store.apply((await graphql<{ DeclineChatCall: ChatCall }>(DECLINE_CHAT_CALL, { callId })).DeclineChatCall)
      } catch (caught) {
        toast.failure(caught, t('calls.declineFailed'))
      }
    },
    [store, t, toast],
  )

  const setMinimized = useCallback((isMinimized: boolean) => {
    setActiveCall((current) => (current ? { ...current, isMinimized } : current))
  }, [])

  const askTranscription = useCallback(async () => {
    const current = activeCallRef.current
    if (!current) return
    try {
      store.apply(
        (await graphql<{ AskChatCallTranscription: ChatCall }>(ASK_CHAT_CALL_TRANSCRIPTION, { callId: current.callId }))
          .AskChatCallTranscription,
      )
    } catch (caught) {
      toast.failure(caught, t('calls.transcriptionFailed'))
    }
  }, [store, t, toast])

  const readGoingCall = useCallback(
    async (conversationId: string) => {
      const markedAt = store.writeMark()
      try {
        const read = await graphql<{ ReadGoingChatCall: ChatCall | null }>(READ_GOING_CHAT_CALL, { conversationId })
        store.settleConversation(conversationId, read.ReadGoingChatCall, markedAt)
      } catch {
        // The banner is a courtesy; the events keep it current when they come.
      }
    },
    [store],
  )

  const value = useMemo<ChatCallsValue>(
    () => ({
      isAvailable: isSupported,
      calls,
      activeCall,
      isBusy,
      startCall,
      joinCall,
      declineCall,
      leaveCall: leaveActiveCall,
      setMinimized,
      askTranscription,
      readGoingCall,
    }),
    [
      isSupported,
      calls,
      activeCall,
      isBusy,
      startCall,
      joinCall,
      declineCall,
      leaveActiveCall,
      setMinimized,
      askTranscription,
      readGoingCall,
    ],
  )

  return (
    <ChatCallsContext.Provider value={value}>
      {children}
      {isSupported && isSignedIn ? <RingingCards /> : null}
      {activeCall ? (
        <>
          <CallAudio />
          {activeCall.isMinimized ? <CallMiniWindow /> : <CallScreen />}
        </>
      ) : null}
    </ChatCallsContext.Provider>
  )
}
