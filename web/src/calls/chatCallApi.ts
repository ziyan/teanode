// The GraphQL operations of a call in a chat, and the shapes they answer with.
// docs/subsystems/chat-calls.md tells what each is for; the field names are the
// json tags of internal/api/v1api/apigraph/chat_call.go.

// ChatCallMember is one person's part in a call.
export interface ChatCallMember {
  userId: string
  name: string
  avatarUrl: string | null
  avatarColorIndex: number
  // ringing, answered, declined, missed, or none for whoever started it.
  ringState: string
  isOn: boolean
  isVideoOn: boolean
  // Their microphone is off, shown beside their name.
  isMuted: boolean
  joinedAt: string | null
  leftAt: string | null
}

// ChatCall is a call as the signed-in person sees it.
export interface ChatCall {
  id: string
  // The person's own conversation for the chat, the chat's name as they see
  // it, and whether it is a group.
  conversationId: string
  chatTitle: string
  isGroup: boolean
  startedByUserId: string | null
  startedByName: string
  startedAt: string
  answeredAt: string | null
  endedAt: string | null
  // ended, missed, declined or failed; empty while it goes.
  endReason: string
  isGoing: boolean
  durationMs: number
  // The person's part: ringing, answered, declined, missed, or none for
  // whoever started it; empty when they had none.
  askerRingState: string
  isAskerOn: boolean
  transcriptionAskedByUserId: string | null
  // waiting, done or failed; empty before the call ends or when nobody asked.
  transcriptState: string
  summaryMessageId: string | null
  members: ChatCallMember[]
}

// ChatCallTranscriptSegment is one line of a transcribed call under its
// speaker, timed from the start of the call.
export interface ChatCallTranscriptSegment {
  id: string
  callId: string
  userId?: string
  speakerName: string
  startMs: number
  endMs: number
  text: string
}

// ChatCallDetail is a call's transcript, read from the recording the call
// became, and that recording when the reader may open it: their own, or one
// shared with them.
export interface ChatCallDetail {
  transcript: ChatCallTranscriptSegment[]
  recordingId: string | null
  isRecordingOwned: boolean
}

// ChatCallSummary is the part of a call a message's line in the chat carries:
// enough to say how the call went, and to be replaced by the call itself as
// ChatCallEvents tells of it.
export interface ChatCallSummary {
  id: string
  startedAt: string
  answeredAt?: string | null
  endedAt?: string | null
  endReason?: string | null
  transcriptState?: string | null
  members?: { userId: string; ringState: string }[] | null
}

// CHAT_CALL_SUMMARY_FIELDS is what a message selects of the call it is the
// line of.
export const CHAT_CALL_SUMMARY_FIELDS =
  'callId call { id endReason answeredAt endedAt startedAt transcriptState members { userId ringState } }'

const CALL_FIELDS = `
  id conversationId chatTitle isGroup startedByUserId startedByName startedAt answeredAt endedAt endReason isGoing
  durationMs askerRingState isAskerOn transcriptionAskedByUserId transcriptState summaryMessageId
  members { userId name avatarUrl avatarColorIndex ringState isOn isVideoOn isMuted joinedAt leftAt }`

export const START_CHAT_CALL = `
  mutation ($conversationId: String!, $isVideoOn: Boolean, $userIds: [String!]) {
    StartChatCall(conversationId: $conversationId, isVideoOn: $isVideoOn, userIds: $userIds) { ${CALL_FIELDS} }
  }`
export const JOIN_CHAT_CALL = `
  mutation ($callId: String!) {
    JoinChatCall(callId: $callId) { ${CALL_FIELDS} }
  }`
export const DECLINE_CHAT_CALL = `
  mutation ($callId: String!) {
    DeclineChatCall(callId: $callId) { ${CALL_FIELDS} }
  }`
export const LEAVE_CHAT_CALL = `
  mutation ($callId: String!) {
    LeaveChatCall(callId: $callId) { ${CALL_FIELDS} }
  }`
export const JOIN_CHAT_CALL_MEDIA = `
  mutation ($callId: String!, $offerSdp: String!) {
    JoinChatCallMedia(callId: $callId, offerSdp: $offerSdp) { answerSdp }
  }`
export const ANSWER_CHAT_CALL_MEDIA = `
  mutation ($callId: String!, $answerSdp: String!) {
    AnswerChatCallMedia(callId: $callId, answerSdp: $answerSdp)
  }`
export const SET_CHAT_CALL_VIDEO = `
  mutation ($callId: String!, $isVideoOn: Boolean!) {
    SetChatCallVideo(callId: $callId, isVideoOn: $isVideoOn) { ${CALL_FIELDS} }
  }`
export const SET_CHAT_CALL_MUTED = `
  mutation ($callId: String!, $isMuted: Boolean!) {
    SetChatCallMuted(callId: $callId, isMuted: $isMuted) { ${CALL_FIELDS} }
  }`
export const ASK_CHAT_CALL_TRANSCRIPTION = `
  mutation ($callId: String!) {
    AskChatCallTranscription(callId: $callId) { ${CALL_FIELDS} }
  }`

export const LIST_GOING_CHAT_CALLS = `
  query {
    ListGoingChatCalls { ${CALL_FIELDS} }
  }`
export const READ_GOING_CHAT_CALL = `
  query ($conversationId: String!) {
    ReadGoingChatCall(conversationId: $conversationId) { ${CALL_FIELDS} }
  }`
export const LIST_CHAT_CALLS = `
  query ($conversationId: String!, $first: Int, $before: DateTime) {
    ListChatCalls(conversationId: $conversationId, first: $first, before: $before) { ${CALL_FIELDS} }
  }`
export const READ_CHAT_CALL = `
  query ($callId: String!) {
    ReadChatCall(callId: $callId) {
      call { ${CALL_FIELDS} }
      transcript { id callId userId speakerName startMs endMs text }
      recordingId isRecordingOwned
    }
  }`

// Every change to a call in a chat the person is in, as they see it now.
export const CHAT_CALL_EVENTS = `
  subscription {
    ChatCallEvents { ${CALL_FIELDS} }
  }`
// What the media server tells the device on a call: offers to answer.
export const CHAT_CALL_SIGNALS = `
  subscription ($callId: String!) {
    ChatCallSignals(callId: $callId) { offerSdp }
  }`

// isRinging is whether a call is ringing the person now.
export function isRinging(call: ChatCall): boolean {
  return call.isGoing && call.askerRingState === 'ringing'
}

// formatCallLength is a duration as a clock: 4:12, or 1:04:12 past an hour.
// Written here rather than reused from the recordings, which the dashboard
// fetches on their own.
export function formatCallLength(durationMS: number): string {
  const totalSeconds = Math.max(0, Math.round(durationMS / 1000))
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  const secondsText = String(seconds).padStart(2, '0')
  return hours > 0 ? `${hours}:${String(minutes).padStart(2, '0')}:${secondsText}` : `${minutes}:${secondsText}`
}

// ChatCallLineState is how a call stands in the chat's transcript.
export type ChatCallLineState =
  | { kind: 'going' }
  | { kind: 'ended'; durationMS: number }
  | { kind: 'missed' }
  | { kind: 'declined' }
  | { kind: 'failed' }

// chatCallLineState reads how a call went from the call, as the line of it
// in a chat is drawn: going until it has an end, then by why it ended. A call
// that ended after being answered says how long it went, from the first
// answer to the end.
export function chatCallLineState(call: ChatCallSummary): ChatCallLineState {
  if (!call.endedAt) return { kind: 'going' }
  switch (call.endReason) {
    case 'missed':
      return { kind: 'missed' }
    case 'declined':
      return { kind: 'declined' }
    case 'failed':
      return { kind: 'failed' }
    default: {
      const answered = call.answeredAt ? new Date(call.answeredAt).getTime() : NaN
      const ended = new Date(call.endedAt).getTime()
      return { kind: 'ended', durationMS: Number.isNaN(answered) || Number.isNaN(ended) ? 0 : ended - answered }
    }
  }
}
