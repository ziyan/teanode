import { createContext, useContext, useEffect, useMemo } from 'react'

import type { ChatCall } from './chatCallApi'
import type { ChatCallSession } from './chatCallSession'
import { goingCallOf } from './chatCallStore'

// ActiveChatCall is the call this tab is on: at most one at a time.
export interface ActiveChatCall {
  callId: string
  conversationId: string
  chatTitle: string
  session: ChatCallSession
  // Shrunk to a small window, so the dashboard can be used on the call.
  isMinimized: boolean
}

// ChatCallsValue is what the calls of the dashboard offer the rest of it.
// Each action says its own failure in a toast, so a caller only starts it.
export interface ChatCallsValue {
  // The browser can make calls, and the shell mounts them. False where
  // nothing provides them (the drawer framed by the extension).
  isAvailable: boolean
  // Every call this tab has been told of, going or over.
  calls: ChatCall[]
  activeCall: ActiveChatCall | null
  // A call is being started or joined, so a second press does nothing.
  isBusy: boolean
  startCall: (conversationId: string) => Promise<void>
  joinCall: (callId: string, isVideoOn: boolean) => Promise<void>
  declineCall: (callId: string) => Promise<void>
  leaveCall: () => Promise<void>
  setMinimized: (isMinimized: boolean) => void
  askTranscription: () => Promise<void>
  // Reads the call going in a chat again, as a chat that opens does.
  readGoingCall: (conversationId: string) => Promise<void>
}

const nothing = async () => {}

const UNAVAILABLE: ChatCallsValue = {
  isAvailable: false,
  calls: [],
  activeCall: null,
  isBusy: false,
  startCall: nothing,
  joinCall: nothing,
  declineCall: nothing,
  leaveCall: nothing,
  setMinimized: () => {},
  askTranscription: nothing,
  readGoingCall: nothing,
}

export const ChatCallsContext = createContext<ChatCallsValue>(UNAVAILABLE)

// useChatCalls is the calls of the dashboard. Where there are none (a drawer
// framed by the extension) it says calls are not available, and the rest do
// nothing.
export function useChatCalls(): ChatCallsValue {
  return useContext(ChatCallsContext)
}

// useGoingChatCall is the call going in a chat, for the banner at its top: read
// when the chat opens, and kept current by the events after.
export function useGoingChatCall(conversationId: string): ChatCall | null {
  const { isAvailable, calls, readGoingCall } = useChatCalls()
  useEffect(() => {
    if (isAvailable && conversationId) void readGoingCall(conversationId)
  }, [isAvailable, conversationId, readGoingCall])
  return useMemo(() => goingCallOf(calls, conversationId), [calls, conversationId])
}
