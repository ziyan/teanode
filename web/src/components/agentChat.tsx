import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'

import { graphql, type AgentReference } from '../api'
import { useTranslation, type Key, type Values } from '../i18n/i18n'
import { Avatar } from './avatar'
import { Tag } from './common'
import { ConfirmDialog, FormDialog } from './dialog'
import { PlusIcon, SparkIcon, TrashIcon } from './icons'
import { Tooltip } from './tooltip'
import type { AgentMessageReaction } from './agentReactions'

// Chats in the drawer. A chat is a conversation with people in it: it is
// listed with the other conversations, read and followed the way they are,
// and written to with the same box. This file holds what only a chat needs:
// who is in it, how a person's message is told from an agent's, the
// dialogs for starting one and changing who is in it, and the small marks
// (typing, who has read) that a conversation with an agent never had.

export type ChatKind = 'direct' | 'group'
export type ChatMemberRole = 'admin' | 'member'

// ChatMember is one person in a chat, as the list of conversations says.
export interface ChatMember {
  userId: string
  username: string
  name: string
  avatarUrl: string | null
  avatarColorIndex: number
  memberRole: ChatMemberRole
  // The account was deleted: the name is what it was, shown with a note.
  isDeletedAccount: boolean
  // The person is no longer in the chat (left, removed, or no longer
  // shares a circle): what they said stays.
  isRemoved: boolean
  // The newest message this person has read, by id; ids sort by time.
  readThroughMessageId: string | null
  // Their agent remembers this chat.
  isAgentReading: boolean
  // The others in the chat may address their agent there.
  isAgentAskableByOthers: boolean
  // Which agent is theirs and what it is called, and whether it is on. The
  // server tells them for the signed-in person's own and for an agent whose
  // owner let the others ask it; they are empty for any other.
  agentId: string
  agentName: string
  isAgentActive: boolean
}

// ChatSummary is what a conversation of the kind chat carries beside the
// fields every conversation has; null on the others.
export interface ChatSummary {
  chatId: string
  chatKind: ChatKind
  circleId: string | null
  title: string
  isCircleChat: boolean
  members: ChatMember[]
  unreadCount: number
  isMuted: boolean
  mutedUntil: string | null
  isRemoved: boolean
}

// The chat as the queries select it. Interpolated whole, as a selection,
// the way the dashboard's other shared field lists are.
export const CHAT_FIELDS =
  '{ chatId chatKind circleId title isCircleChat unreadCount isMuted mutedUntil isRemoved members { userId username name avatarUrl avatarColorIndex memberRole isDeletedAccount isRemoved readThroughMessageId isAgentReading isAgentAskableByOthers agentId agentName isAgentActive } }'

const LIST_PEOPLE = `query { ListCirclePeople { userId username name avatarUrl avatarColorIndex } }`

const OPEN_DIRECT_CHAT = `mutation ($userId: String!) { OpenDirectChat(userId: $userId) { id } }`
const CREATE_GROUP_CHAT = `
  mutation ($title: String!, $userIds: [String!]!) {
    CreateGroupChat(title: $title, userIds: $userIds) { id }
  }`
export const RENAME_CHAT = `mutation ($conversationId: String!, $title: String!) { RenameChat(conversationId: $conversationId, title: $title) { id } }`
const ADD_CHAT_MEMBERS = `
  mutation ($conversationId: String!, $userIds: [String!]!) {
    AddChatMembers(conversationId: $conversationId, userIds: $userIds) { id }
  }`
const REMOVE_CHAT_MEMBER = `
  mutation ($conversationId: String!, $userId: String!) {
    RemoveChatMember(conversationId: $conversationId, userId: $userId) { id }
  }`
export const LEAVE_CHAT = `mutation ($conversationId: String!) { LeaveChat(conversationId: $conversationId) }`
export const EDIT_CHAT_MESSAGE = `mutation ($messageId: String!, $message: String!) { EditChatMessage(messageId: $messageId, message: $message) { id } }`
export const DELETE_CHAT_MESSAGE = `mutation ($messageId: String!) { DeleteChatMessage(messageId: $messageId) }`
export const MARK_CHAT_READ = `
  mutation ($conversationId: String!, $throughMessageId: String!) {
    MarkChatRead(conversationId: $conversationId, throughMessageId: $throughMessageId)
  }`
export const MUTE_CHAT = `
  mutation ($conversationId: String!, $mutedUntil: DateTime) {
    MuteChat(conversationId: $conversationId, mutedUntil: $mutedUntil) { id }
  }`
export const SET_CHAT_TYPING = `mutation ($conversationId: String!) { SetChatTyping(conversationId: $conversationId) }`
export const SET_CHAT_AGENT_READING = `
  mutation ($conversationId: String!, $isAgentReading: Boolean!) {
    SetChatAgentReading(conversationId: $conversationId, isAgentReading: $isAgentReading) { id chat ${CHAT_FIELDS} }
  }`
export const SET_CHAT_AGENT_ASKABLE = `
  mutation ($conversationId: String!, $isAgentAskableByOthers: Boolean!) {
    SetChatAgentAskableByOthers(conversationId: $conversationId, isAgentAskableByOthers: $isAgentAskableByOthers) { id chat ${CHAT_FIELDS} }
  }`
// The chat's people alone, read again when one of them changes what their
// agent does there.
export const READ_CHAT_MEMBERS = `
  query ($conversationId: String) {
    ReadAgentConversation(conversationId: $conversationId, first: 1) { conversation { id chat ${CHAT_FIELDS} } }
  }`

// How often a person's typing is told to the others: at most this often
// while they type, so a long message is a handful of calls and not one per
// key.
export const TYPING_SENT_EVERY = 3_000
// How long a typing line stays after the last word of it: a little longer
// than the interval it is told at, so it does not flicker between two.
export const TYPING_SHOWN_FOR = 5_000
// Muting "until I unmute" is a date no chat outlasts; the server keeps what
// it is given.
export const MUTED_FOR_GOOD = '2999-01-01T00:00:00Z'
// "Mute for a while" is this long.
export const MUTED_FOR_A_WHILE = 8 * 60 * 60 * 1000

// ChatLine is one message of a chat as the drawer draws it: a person's or
// an agent's, with who said it.
export interface ChatLine {
  kind: 'chat'
  key: string
  // Empty for a message just sent that the feed has not yet confirmed.
  messageId?: string
  text: string
  at?: string
  authorKind: 'person' | 'agent'
  authorUserId: string
  authorName: string
  // On an agent's line, its owner's name: most agents keep the name "Agent",
  // and the owner is what tells two of them apart.
  authorOwnerName: string
  authorAvatarUrl: string | null
  authorAvatarColorIndex: number | null
  authorAgentId: string
  // Said by the signed-in person: drawn on the right.
  isMine: boolean
  editedAt: string | null
  isDeleted: boolean
  isPending?: boolean
  // The words of an agent's answer still being written, told to every
  // member as they come; the stored message with the same run replaces it.
  isStreaming?: boolean
  // The run of the turn that wrote an agent's message.
  agentRunId?: string
  attachments?: { id: string; name: string; contentType: string; size: number }[]
  references?: AgentReference[]
  reactions?: AgentMessageReaction[]
}

// ChatStoredMessage is the part of a stored message a chat line reads.
export interface ChatStoredMessage {
  id: string
  createdAt?: string
  role: string
  content: string
  attachments?: ChatLine['attachments'] | null
  references?: AgentReference[] | null
  reactions?: AgentMessageReaction[] | null
  authorKind?: string | null
  authorUserId?: string | null
  authorName?: string | null
  authorOwnerName?: string | null
  authorAvatarUrl?: string | null
  authorAvatarColorIndex?: number | null
  authorAgentId?: string | null
  isMine?: boolean | null
  editedAt?: string | null
  isDeleted?: boolean | null
  // The run of the turn that wrote an agent's message.
  agentRunId?: string | null
  // In a chat, a row of what the signed-in person's own agent did to
  // answer (a call, its result, a note), which only they read, and not
  // something said there.
  isAgentWork?: boolean | null
  toolCalls?: { id: string }[] | null
}

// isChatMessage is whether a stored message of a chat conversation was said
// in the chat, rather than being a row of the member's own agent's work:
// the chat's messages are the ones that say who wrote them.
export function isChatMessage(message: ChatStoredMessage): boolean {
  if (!message.authorKind || message.isAgentWork) return false
  // The call a round made is a row of the agent's own work, with no words.
  if (message.toolCalls && message.toolCalls.length > 0 && !message.content.trim()) return false
  return message.role !== 'tool' && message.role !== 'note' && message.role !== 'compaction'
}

// chatLineOf is the line a stored chat message is drawn as.
export function chatLineOf(message: ChatStoredMessage): ChatLine {
  return {
    kind: 'chat',
    key: message.id,
    messageId: message.id,
    text: message.isDeleted ? '' : message.content,
    at: message.createdAt,
    authorKind: message.authorKind === 'agent' ? 'agent' : 'person',
    authorUserId: message.authorUserId ?? '',
    authorName: message.authorName ?? '',
    authorOwnerName: message.authorOwnerName ?? '',
    authorAvatarUrl: message.authorAvatarUrl ?? null,
    authorAvatarColorIndex: message.authorAvatarColorIndex ?? null,
    authorAgentId: message.authorAgentId ?? '',
    agentRunId: message.agentRunId ?? undefined,
    isMine: Boolean(message.isMine),
    editedAt: message.editedAt ?? null,
    isDeleted: Boolean(message.isDeleted),
    attachments: message.attachments ?? undefined,
    references: message.references ?? undefined,
    reactions: message.reactions ?? undefined,
  }
}

// isOwnWords is whether a line is what the signed-in person wrote
// themselves: drawn on the right. Their agent's words are said as the agent,
// under its name, though the server counts them as theirs.
export function isOwnWords(line: ChatLine): boolean {
  return line.isMine && line.authorKind === 'person'
}

// DEFAULT_AGENT_NAME is what an agent is called until its person names it,
// the server's models.AgentDefaultName.
export const DEFAULT_AGENT_NAME = 'Agent'

// agentSpeakerName is how an agent is named in a chat: "<name> · <owner>'s
// agent", and just "<owner>'s agent" while it still has the default name,
// which would only repeat the word. The same rule names it above its
// messages and in the line that says it is thinking, so that two people's
// agents are never both "Agent". An agent whose owner is not known is called
// by its own name.
export function agentSpeakerName(
  agentName: string,
  ownerName: string,
  t: (key: Key, values?: Values) => string,
): string {
  const name = agentName.trim()
  const owner = ownerName.trim()
  if (!owner) return name
  const owned = t('chat.agentOf', { owner })
  return !name || name === DEFAULT_AGENT_NAME ? owned : `${name} · ${owned}`
}

// authorKeyOf names who wrote a line, so that a run of one author's lines
// can be drawn under one name.
export function authorKeyOf(line: ChatLine): string {
  return line.authorKind === 'agent' ? `agent:${line.authorAgentId}` : `person:${line.authorUserId || line.authorName}`
}

// MOST_MINUTES_IN_A_RUN is how long a pause ends a run: the same person,
// an hour later, is saying something new and gets their name again.
const MOST_MINUTES_IN_A_RUN = 10

// localDayOf is the calendar day a time falls on in the reader's zone, the same
// day the drawer's dividers are drawn between.
function localDayOf(at: string): string {
  const date = new Date(at)
  if (Number.isNaN(date.getTime())) return ''
  return `${date.getFullYear()}-${date.getMonth()}-${date.getDate()}`
}

// beginsRun is whether a line begins a run: the first of its author's
// lines in a row. A line after a deleted one, or a long pause, or a
// different day, begins one as well.
export function beginsRun(line: ChatLine, previous: { kind: string } | undefined): boolean {
  if (!previous || previous.kind !== 'chat') return true
  const before = previous as ChatLine
  if (authorKeyOf(before) !== authorKeyOf(line)) return true
  if (!before.at || !line.at) return false
  // The drawer draws a divider where the day changes ("Today"), and the line
  // under it is the first of what follows, so it shows the name and the picture
  // again however little time passed across midnight.
  if (localDayOf(before.at) !== localDayOf(line.at)) return true
  const minutes = (new Date(line.at).getTime() - new Date(before.at).getTime()) / 60_000
  return Number.isNaN(minutes) || minutes > MOST_MINUTES_IN_A_RUN || minutes < 0
}

// linesWithSaid adds a message that was just said. A line for it already
// there (read with the conversation a moment before the event arrived) is
// kept as it is; the line a person's own message was drawn as while it
// was being sent is the one it replaces.
export function linesWithSaid<Line extends { kind: string; key: string }>(
  lines: Line[],
  message: ChatStoredMessage,
): Line[] {
  if (lines.some((line) => line.kind === 'chat' && (line as unknown as ChatLine).messageId === message.id)) {
    return lines
  }
  const said = chatLineOf(message) as unknown as Line
  // An agent's answer that was shown while it was being written is
  // replaced where it stood, not added beside the message that is the same
  // words kept.
  if (message.authorKind === 'agent' && message.agentRunId) {
    const streamingAt = lines.findIndex(
      (line) =>
        line.kind === 'chat' &&
        (line as unknown as ChatLine).isStreaming &&
        line.key === streamingKeyOf(message.agentRunId as string),
    )
    if (streamingAt >= 0) {
      const next = [...lines]
      next[streamingAt] = said
      return next
    }
  }
  if (message.isMine && message.authorKind !== 'agent') {
    const pendingAt = lines.findIndex(
      (line) =>
        line.kind === 'chat' &&
        (line as unknown as ChatLine).isPending &&
        (line as unknown as ChatLine).text.trim() === message.content.trim(),
    )
    if (pendingAt >= 0) {
      const next = [...lines]
      next[pendingAt] = said
      return next
    }
  }
  return [...lines, said]
}

// streamingKeyOf is the key of the line an agent's answer is drawn as while
// it is being written: one for each run, since a run answers in its rounds
// one after another and the stored message replaces each before the next.
export function streamingKeyOf(runId: string): string {
  return `streaming-${runId}`
}

// linesWithStreamedText adds words of an agent's answer that are still being
// written, to the line for its run or a new one.
export function linesWithStreamedText<Line extends { kind: string; key: string }>(
  lines: Line[],
  event: { runId: string; text?: string; name?: string; authorOwnerName?: string; authorAgentId?: string; at?: string },
): Line[] {
  const key = streamingKeyOf(event.runId)
  const words = event.text ?? ''
  const at = lines.findIndex((line) => line.kind === 'chat' && line.key === key)
  if (at >= 0) {
    const next = [...lines]
    const line = next[at] as unknown as ChatLine
    next[at] = { ...line, text: line.text + words } as unknown as Line
    return next
  }
  const started: ChatLine = {
    kind: 'chat',
    key,
    text: words,
    at: event.at,
    authorKind: 'agent',
    authorUserId: '',
    authorName: event.name ?? '',
    authorOwnerName: event.authorOwnerName ?? '',
    authorAvatarUrl: null,
    authorAvatarColorIndex: null,
    authorAgentId: event.authorAgentId ?? '',
    isMine: false,
    editedAt: null,
    isDeleted: false,
    isStreaming: true,
    agentRunId: event.runId,
  }
  return [...lines, started as unknown as Line]
}

// linesWithEdit changes the words of a line that was edited.
export function linesWithEdit<Line extends { kind: string; key: string }>(
  lines: Line[],
  messageId: string,
  text: string,
  editedAt: string | null,
): Line[] {
  return lines.map((line) =>
    line.kind === 'chat' && (line as unknown as ChatLine).messageId === messageId
      ? ({ ...line, text, editedAt: editedAt ?? new Date().toISOString() } as Line)
      : line,
  )
}

// isStreamingKey says a key is a streaming line's, which a deleted event names
// when the words it was drawn from will not be stored as a message: the turn
// was stopped, it failed, or what it said came to nothing.
export function isStreamingKey(key: string): boolean {
  return key.startsWith('streaming-')
}

// linesWithoutStreaming takes the line drawn from streamed words off the
// screen. Nothing marks it deleted: no message was ever stored for it.
export function linesWithoutStreaming<Line extends { kind: string; key: string }>(lines: Line[], key: string): Line[] {
  return lines.filter((line) => !(line.kind === 'chat' && line.key === key))
}

// linesWithDeleted turns a line that was deleted into the mark that it was.
export function linesWithDeleted<Line extends { kind: string; key: string }>(lines: Line[], messageId: string): Line[] {
  return lines.map((line) =>
    line.kind === 'chat' && (line as unknown as ChatLine).messageId === messageId
      ? ({ ...line, text: '', isDeleted: true, attachments: undefined, reactions: undefined } as Line)
      : line,
  )
}

// otherMembersOf is the people to show a chat by: everybody but the
// signed-in person, and but those who are no longer in a group. A direct
// chat keeps showing the other person after they have left a circle: it
// is who the chat is with.
export function otherMembersOf(chat: ChatSummary, ownUserId: string): ChatMember[] {
  return chat.members.filter(
    (member) => member.userId !== ownUserId && (chat.chatKind === 'direct' || !member.isRemoved),
  )
}

// memberNameOf is how a person is named: their name, or their username
// when they have none, and a note when the account is gone.
export function memberNameOf(member: Pick<ChatMember, 'name' | 'username' | 'isDeletedAccount'>, deleted: string) {
  const name = member.name?.trim() || member.username
  return member.isDeletedAccount ? `${name} ${deleted}` : name
}

// chatTitleOf is what a chat is called: its title if it has one, else the
// other people in it by name, which is all a direct chat is.
export function chatTitleOf(chat: ChatSummary, ownUserId: string, deleted: string, fallback: string): string {
  if (chat.title.trim()) return chat.title.trim()
  const names = otherMembersOf(chat, ownUserId).map((member) => memberNameOf(member, deleted))
  return names.join(', ') || fallback
}

// totalUnread is how many messages wait across the chats that are not
// muted: the number on the drawer's button. A muted chat still shows its
// own count in the list; it does not ask for attention from outside.
export function totalUnread(conversations: { chat?: Pick<ChatSummary, 'unreadCount' | 'isMuted'> | null }[]): number {
  return conversations.reduce(
    (sum, conversation) => sum + (conversation.chat && !conversation.chat.isMuted ? conversation.chat.unreadCount : 0),
    0,
  )
}

// readersByMessage says which members to draw under which message: each
// other member under the newest message by somebody else that they have
// read, never under one of their own. In a direct chat that is under the
// signed-in person's own newest message the other has read. A member who
// has read nothing of what is loaded by anybody else is under none.
export function readersByMessage(lines: { kind: string }[], members: ChatMember[], ownUserId: string) {
  const readers = new Map<string, ChatMember[]>()
  const chatLines = (lines as ChatLine[]).filter((line) => line.kind === 'chat' && line.messageId && !line.isPending)
  for (const member of members) {
    if (member.userId === ownUserId || member.isRemoved || !member.readThroughMessageId) continue
    // Ids are ULIDs, so they sort by time and a string comparison finds
    // the newest message at or before the one the member read through.
    let found: ChatLine | undefined
    for (const line of chatLines) {
      if ((line.messageId as string) > member.readThroughMessageId) break
      if (line.authorKind === 'person' && line.authorUserId === member.userId) continue
      found = line
    }
    if (!found?.messageId) continue
    readers.set(found.messageId, [...(readers.get(found.messageId) ?? []), member])
  }
  return readers
}

// withMemberRead moves one member's read mark forward.
export function withMemberRead(chat: ChatSummary, userId: string, messageId: string): ChatSummary {
  return {
    ...chat,
    members: chat.members.map((member) =>
      member.userId === userId && (!member.readThroughMessageId || member.readThroughMessageId < messageId)
        ? { ...member, readThroughMessageId: messageId }
        : member,
    ),
  }
}

// ChatAvatars are a chat's people: the other person's picture for a direct
// chat, two overlapped for a group.
export function ChatAvatars({
  chat,
  ownUserId,
  deleted,
  size = 24,
}: {
  chat: ChatSummary
  ownUserId: string
  deleted: string
  size?: number
}) {
  const others = otherMembersOf(chat, ownUserId)
  const shown = chat.chatKind === 'direct' ? others.slice(0, 1) : others.slice(0, 2)
  return (
    <span className="avatar-stack chat-avatars" aria-hidden="true">
      {shown.length === 0 ? (
        <Avatar name={chat.title || '?'} size={size} />
      ) : (
        shown.map((member) => (
          <Avatar
            key={member.userId}
            name={memberNameOf(member, deleted)}
            avatarUrl={member.avatarUrl}
            colorIndex={member.avatarColorIndex}
            size={size}
          />
        ))
      )}
    </span>
  )
}

// UnreadCount is the count on a chat that has messages waiting.
export function UnreadCount({ count }: { count: number }) {
  if (count <= 0) return null
  return <span className="chat-unread">{count > 99 ? '99+' : count}</span>
}

// typingWords says who is typing, from the names, in the reader's words.
export function typingWords(names: string[], t: (key: Key, values?: Values) => string): string {
  if (names.length === 0) return ''
  if (names.length === 1) return t('chat.typingOne', { name: names[0] })
  if (names.length === 2) return t('chat.typingTwo', { first: names[0], second: names[1] })
  return t('chat.typingMany')
}

// thinkingWords says which agents are working on an answer, from their
// names: an agent is thinking where a person is typing.
export function thinkingWords(names: string[], t: (key: Key, values?: Values) => string): string {
  if (names.length === 0) return ''
  if (names.length === 1) return t('chat.thinkingOne', { name: names[0] })
  if (names.length === 2) return t('chat.thinkingTwo', { first: names[0], second: names[1] })
  return t('chat.thinkingMany')
}

// TypingLine is the line under the messages saying who is typing, and which
// agent is thinking.
export function TypingLine({ names, agentNames = [] }: { names: string[]; agentNames?: string[] }) {
  const { t } = useTranslation()
  if (names.length === 0 && agentNames.length === 0) return null
  const dots = (
    <span className="agent-dots" aria-hidden="true">
      <i />
      <i />
      <i />
    </span>
  )
  return (
    <>
      {names.length > 0 && (
        <div className="agent-chat-typing muted" role="status">
          {dots}
          {typingWords(names, t)}
        </div>
      )}
      {agentNames.length > 0 && (
        <div className="agent-chat-typing muted" role="status">
          {dots}
          {thinkingWords(agentNames, t)}
        </div>
      )}
    </>
  )
}

// An agent that a message can be addressed to, as the composer's @ offers
// it: the person's own, and the agents of members who let the others ask.
export interface AddressableAgent {
  agentId: string
  name: string
  // The signed-in person's own agent; the others are named by whose they are.
  isOwn: boolean
  ownerName: string
}

// addressableAgentsOf are the agents an @ in this chat offers: the person's
// own when it is on, and an agent whose owner let the others ask it and that
// is on. An agent that is off cannot answer, so none is offered; one addressed
// anyway (by a reply to its message) is told so by the server.
export function addressableAgentsOf(chat: ChatSummary, ownUserId: string, deleted: string): AddressableAgent[] {
  const agents: AddressableAgent[] = []
  for (const member of chat.members) {
    if (member.isRemoved || !member.agentId || !member.agentName.trim() || !member.isAgentActive) continue
    const isOwn = member.userId === ownUserId
    if (!isOwn && !member.isAgentAskableByOthers) continue
    agents.push({
      agentId: member.agentId,
      name: member.agentName.trim(),
      isOwn,
      ownerName: memberNameOf(member, deleted),
    })
  }
  // The person's own first.
  return agents.sort((left, right) => Number(right.isOwn) - Number(left.isOwn))
}

// Mention is an @ being typed: where it starts and the letters after it.
export interface Mention {
  start: number
  query: string
}

// mentionAt is the @ the caret is at the end of, if any: an @ that starts
// the box or follows a space, and only letters, digits and the like between
// it and the caret. Anything else is just an @ in a sentence.
export function mentionAt(text: string, caret: number): Mention | null {
  const before = text.slice(0, caret)
  const at = before.lastIndexOf('@')
  if (at < 0) return null
  if (at > 0 && !/\s/.test(before[at - 1])) return null
  const query = before.slice(at + 1)
  if (/\s/.test(query)) return null
  return { start: at, query }
}

// agentsMatching are the agents whose name starts with what was typed after
// the @, in any case; every agent for a bare @.
export function agentsMatching(agents: AddressableAgent[], query: string): AddressableAgent[] {
  const wanted = query.toLowerCase()
  return agents.filter((agent) => agent.name.toLowerCase().replace(/\s+/g, '').startsWith(wanted))
}

// withMention puts the chosen agent's name where the @ was typed, and says
// where the caret goes: after the name and a space.
export function withMention(
  text: string,
  mention: Mention,
  caret: number,
  agent: AddressableAgent,
): { text: string; caret: number } {
  const name = `@${agent.name} `
  return { text: text.slice(0, mention.start) + name + text.slice(caret), caret: mention.start + name.length }
}

// addressedIn is the agents a message is for: those chosen from the @ whose
// name is still in the words, so that taking the name out takes the agent
// out, and the agent of a message replied to. An agent's name typed by hand
// addresses nothing.
export function addressedIn(text: string, chosen: AddressableAgent[], repliedToAgentId?: string): string[] {
  const addressed: string[] = []
  for (const agent of chosen) {
    const name = `@${agent.name}`
    const at = text.indexOf(name)
    if (at < 0) continue
    const after = text[at + name.length]
    if (after !== undefined && /[\p{L}\p{N}_]/u.test(after)) continue
    if (!addressed.includes(agent.agentId)) addressed.push(agent.agentId)
  }
  if (repliedToAgentId && !addressed.includes(repliedToAgentId)) addressed.push(repliedToAgentId)
  return addressed
}

// MentionList is the agents offered under an @, one to press.
export function MentionList({
  agents,
  activeIndex,
  onChoose,
  onHover,
}: {
  agents: AddressableAgent[]
  activeIndex: number
  onChoose: (agent: AddressableAgent) => void
  onHover: (index: number) => void
}) {
  const { t } = useTranslation()
  return (
    <ul className="agent-mention-list" role="listbox" aria-label={t('chat.mentionAgents')}>
      {agents.map((agent, index) => (
        <li
          key={agent.agentId}
          role="option"
          aria-selected={index === activeIndex}
          className={index === activeIndex ? 'active' : ''}
          onMouseDown={(event) => {
            // The box keeps its focus and its caret.
            event.preventDefault()
            onChoose(agent)
          }}
          onMouseEnter={() => onHover(index)}
        >
          <SparkIcon size={14} />
          <span className="agent-mention-name">{agent.name}</span>
          <span className="muted">{agent.isOwn ? t('chat.mentionYours') : agent.ownerName}</span>
        </li>
      ))}
    </ul>
  )
}

// ReadMarks are the small pictures under a message of the people who have read
// as far as it.
export function ReadMarks({ readers, deleted }: { readers: ChatMember[]; deleted: string }) {
  const { t } = useTranslation()
  if (readers.length === 0) return null
  const names = readers.map((member) => memberNameOf(member, deleted)).join(', ')
  return (
    <Tooltip label={t('chat.readBy', { names })}>
      <span className="agent-chat-reads" role="img" aria-label={t('chat.readBy', { names })}>
        {readers.slice(0, 4).map((member) => (
          <Avatar
            key={member.userId}
            name={memberNameOf(member, deleted)}
            avatarUrl={member.avatarUrl}
            colorIndex={member.avatarColorIndex}
            size={14}
          />
        ))}
        {readers.length > 4 ? <span className="muted">+{readers.length - 4}</span> : null}
      </span>
    </Tooltip>
  )
}

// ChatLineFrame puts a message where it belongs: the person's own on the
// right, and everyone else's on the left with their picture and, above the
// first message of a run, their name. The bubble itself is the children.
export function ChatLineFrame({
  line,
  isRunStart,
  readers,
  children,
}: {
  line: ChatLine
  isRunStart: boolean
  readers: ChatMember[]
  children: React.ReactNode
}) {
  const { t } = useTranslation()
  const deleted = t('chat.deletedAccount')
  const marks = <ReadMarks readers={readers} deleted={deleted} />
  if (isOwnWords(line)) {
    return (
      <div className="agent-chat-turn mine">
        {children}
        {marks}
      </div>
    )
  }
  // A person whose account is gone has no id left on their messages.
  const name =
    line.authorKind === 'agent'
      ? agentSpeakerName(line.authorName, line.authorOwnerName, t)
      : !line.authorUserId && line.authorName
        ? `${line.authorName} ${deleted}`
        : line.authorName
  return (
    <div className={['agent-chat-turn', isRunStart ? 'starts-run' : ''].filter(Boolean).join(' ')}>
      <span className="agent-chat-avatar">
        {isRunStart ? (
          <Avatar
            name={name || '?'}
            avatarUrl={line.authorAvatarUrl}
            colorIndex={line.authorAvatarColorIndex}
            size={28}
          />
        ) : null}
      </span>
      <div className="agent-chat-body">
        {isRunStart ? (
          <div className="agent-chat-author">
            <span>{name}</span>
            {line.authorKind === 'agent' ? <SparkIcon size={11} /> : null}
          </div>
        ) : null}
        {children}
        {marks}
      </div>
    </div>
  )
}

// A person the chat can be offered to.
interface Person {
  userId: string
  username: string
  name: string
  avatarUrl: string | null
  avatarColorIndex: number
}

// usePeople reads everybody the person shares a circle with: the only
// people a chat can be started with or a person added from.
function usePeople(): { people: Person[] | null; problem: string | null } {
  const [people, setPeople] = useState<Person[] | null>(null)
  const [problem, setProblem] = useState<string | null>(null)
  useEffect(() => {
    let isCancelled = false
    graphql<{ ListCirclePeople: Person[] }>(LIST_PEOPLE)
      .then((response) => {
        if (!isCancelled) setPeople(response.ListCirclePeople ?? [])
      })
      .catch((caught) => {
        if (isCancelled) return
        setPeople([])
        setProblem(caught instanceof Error ? caught.message : String(caught))
      })
    return () => {
      isCancelled = true
    }
  }, [])
  return { people, problem }
}

// PeopleList is the people to choose from, one row each: one may be chosen,
// or as many as wanted.
function PeopleList({
  people,
  chosen,
  isSingle,
  onChange,
}: {
  people: Person[]
  chosen: string[]
  isSingle: boolean
  onChange: (chosen: string[]) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="chat-people" role={isSingle ? 'radiogroup' : 'group'} aria-label={t('chat.people')}>
      {people.map((person) => {
        const name = person.name?.trim() || person.username
        const isChosen = chosen.includes(person.userId)
        return (
          <label key={person.userId} className="chat-person checkbox">
            <input
              type={isSingle ? 'radio' : 'checkbox'}
              name="chat-person"
              checked={isChosen}
              onChange={() =>
                onChange(
                  isSingle
                    ? [person.userId]
                    : isChosen
                      ? chosen.filter((userId) => userId !== person.userId)
                      : [...chosen, person.userId],
                )
              }
            />
            <Avatar name={name} avatarUrl={person.avatarUrl} colorIndex={person.avatarColorIndex} size={24} />
            <span>{name}</span>
            <span className="muted">{person.username}</span>
          </label>
        )
      })}
    </div>
  )
}

// ChatPeopleDialog is the one dialog for choosing people: to message one,
// to start a group with several and a name, or to add to a group that is
// there. Those who are in it already are left out of the list.
export function ChatPeopleDialog({
  purpose,
  conversationId,
  leftOut = [],
  onClose,
  onDone,
}: {
  purpose: 'direct' | 'group' | 'add'
  conversationId?: string
  leftOut?: string[]
  onClose: () => void
  // The conversation to show now: the one just opened, or made.
  onDone: (conversationId: string | null) => void
}) {
  const { t } = useTranslation()
  const { people, problem } = usePeople()
  const [chosen, setChosen] = useState<string[]>([])
  const [title, setTitle] = useState('')
  const [isBusy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const choices = useMemo(() => (people ?? []).filter((person) => !leftOut.includes(person.userId)), [people, leftOut])
  const dialogTitle =
    purpose === 'direct' ? t('chat.messageSomeone') : purpose === 'group' ? t('chat.newGroup') : t('chat.addPeople')
  const submitLabel =
    purpose === 'direct' ? t('chat.message') : purpose === 'group' ? t('common.create') : t('chat.add')
  const canSubmit = chosen.length > 0 && (purpose !== 'group' || title.trim() !== '')

  const submit = async () => {
    setBusy(true)
    setError(null)
    try {
      if (purpose === 'direct') {
        const response = await graphql<{ OpenDirectChat: { id: string } }>(OPEN_DIRECT_CHAT, { userId: chosen[0] })
        onDone(response.OpenDirectChat.id)
      } else if (purpose === 'group') {
        const response = await graphql<{ CreateGroupChat: { id: string } }>(CREATE_GROUP_CHAT, {
          title: title.trim(),
          userIds: chosen,
        })
        onDone(response.CreateGroupChat.id)
      } else {
        await graphql(ADD_CHAT_MEMBERS, { conversationId, userIds: chosen })
        onDone(null)
      }
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : String(caught))
      setBusy(false)
    }
  }

  // Nobody to choose from at all: a person in no circle, who has met the
  // entry before meeting what makes it work. Say who can be messaged and
  // where that is set up, rather than showing an empty list. Adding to a
  // group is different: there the people are only all in it already.
  if (purpose !== 'add' && people !== null && people.length === 0 && !problem) {
    return (
      <ConfirmDialog
        title={dialogTitle}
        body={
          <>
            <p className="muted">{t('chat.noPeople')}</p>
            <p>
              <Link to="/circles" onClick={onClose}>
                {t('chat.noPeopleOpenCircles')}
              </Link>
            </p>
          </>
        }
        onClose={onClose}
      />
    )
  }

  return (
    <FormDialog
      title={dialogTitle}
      submitLabel={submitLabel}
      busy={isBusy}
      error={error ?? problem}
      canSubmit={canSubmit}
      onClose={onClose}
      onSubmit={() => void submit()}
    >
      {purpose === 'group' && (
        <label>
          {t('chat.groupName')}
          <input autoFocus value={title} maxLength={100} onChange={(event) => setTitle(event.target.value)} />
        </label>
      )}
      {people === null ? (
        <p className="muted">{t('common.loading')}</p>
      ) : choices.length === 0 ? (
        // For a failed read the error above says it.
        purpose === 'add' && !problem ? (
          <p className="muted">{t('chat.everyoneIsIn')}</p>
        ) : null
      ) : (
        <PeopleList people={choices} chosen={chosen} isSingle={purpose === 'direct'} onChange={setChosen} />
      )}
    </FormDialog>
  )
}

// RenameChatDialog changes a group's name.
export function RenameChatDialog({
  title,
  onClose,
  onRename,
}: {
  title: string
  onClose: () => void
  onRename: (title: string) => Promise<void>
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState(title)
  const [isBusy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  return (
    <FormDialog
      title={t('chat.rename')}
      submitLabel={t('common.save')}
      busy={isBusy}
      error={error}
      canSubmit={draft.trim() !== '' && draft.trim() !== title}
      onClose={onClose}
      onSubmit={async () => {
        setBusy(true)
        setError(null)
        try {
          await onRename(draft.trim())
        } catch (caught) {
          setError(caught instanceof Error ? caught.message : String(caught))
          setBusy(false)
        }
      }}
    >
      <label>
        {t('chat.groupName')}
        <input autoFocus value={draft} maxLength={100} onChange={(event) => setDraft(event.target.value)} />
      </label>
    </FormDialog>
  )
}

// ChatMembersDialog lists who is in a chat. A group's admins can add people
// and take them out; a circle's chat follows the circle, so there it is a
// list.
export function ChatMembersDialog({
  chat,
  conversationId,
  ownUserId,
  title,
  onClose,
  onAdd,
  onChanged,
}: {
  chat: ChatSummary
  conversationId: string
  ownUserId: string
  title: string
  onClose: () => void
  onAdd: () => void
  onChanged: () => void
}) {
  const { t } = useTranslation()
  const deleted = t('chat.deletedAccount')
  const [removing, setRemoving] = useState<ChatMember | null>(null)
  const [isBusy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const isAdmin = chat.members.some((member) => member.userId === ownUserId && member.memberRole === 'admin')
  const canChange = chat.chatKind === 'group' && !chat.isCircleChat && isAdmin
  const present = chat.members.filter((member) => !member.isRemoved)
  const own = present.find((member) => member.userId === ownUserId)
  // Changed for the signed-in person's own membership: what their agent
  // does in this chat. Each switch is the person's alone.
  const switchAgent = async (document: string, variables: Record<string, unknown>) => {
    setBusy(true)
    setError(null)
    try {
      await graphql(document, { conversationId, ...variables })
      onChanged()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : String(caught))
    } finally {
      setBusy(false)
    }
  }

  if (removing) {
    const name = memberNameOf(removing, deleted)
    return (
      <ConfirmDialog
        title={t('chat.removeMemberTitle', { name })}
        body={t('chat.removeMemberBody', { name, title })}
        confirmLabel={t('chat.removeMember')}
        busy={isBusy}
        error={error}
        onClose={() => setRemoving(null)}
        onConfirm={async () => {
          setBusy(true)
          setError(null)
          try {
            await graphql(REMOVE_CHAT_MEMBER, { conversationId, userId: removing.userId })
            setRemoving(null)
            onChanged()
          } catch (caught) {
            setError(caught instanceof Error ? caught.message : String(caught))
          } finally {
            setBusy(false)
          }
        }}
      />
    )
  }

  return (
    <ConfirmDialog
      title={t('chat.members')}
      body={
        <>
          <ul className="chat-members">
            {present.map((member) => {
              const name = memberNameOf(member, deleted)
              return (
                <li key={member.userId || name}>
                  <Avatar name={name} avatarUrl={member.avatarUrl} colorIndex={member.avatarColorIndex} size={28} />
                  <span className="chat-member-name">{name}</span>
                  {member.memberRole === 'admin' && chat.chatKind === 'group' ? <Tag value={t('chat.admin')} /> : null}
                  {member.isAgentReading ? <Tag value={t('chat.agentRemembersTag')} /> : null}
                  {member.isAgentAskableByOthers ? <Tag value={t('chat.agentAskableTag')} /> : null}
                  {canChange && member.userId !== ownUserId ? (
                    <Tooltip label={t('chat.removeMember')}>
                      <button
                        type="button"
                        className="icon-action danger"
                        aria-label={`${name}: ${t('chat.removeMember')}`}
                        onClick={() => setRemoving(member)}
                      >
                        <TrashIcon size={16} />
                      </button>
                    </Tooltip>
                  ) : null}
                </li>
              )
            })}
          </ul>
          {own && !chat.isRemoved ? (
            <fieldset className="chat-agent-switches" disabled={isBusy}>
              <legend>{t('chat.yourAgent')}</legend>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={own.isAgentReading}
                  onChange={(event) =>
                    void switchAgent(SET_CHAT_AGENT_READING, { isAgentReading: event.target.checked })
                  }
                />
                <span>{t('chat.agentRemembers')}</span>
              </label>
              <p className="muted">{t('chat.agentRemembersHelp')}</p>
              <label className="checkbox">
                <input
                  type="checkbox"
                  checked={own.isAgentAskableByOthers}
                  onChange={(event) =>
                    void switchAgent(SET_CHAT_AGENT_ASKABLE, { isAgentAskableByOthers: event.target.checked })
                  }
                />
                <span>{t('chat.agentAskable')}</span>
              </label>
              <p className="muted">{t('chat.agentAskableHelp')}</p>
              {error ? (
                <p className="form-error" role="alert">
                  {error}
                </p>
              ) : null}
            </fieldset>
          ) : null}
        </>
      }
      onClose={onClose}
      otherAction={
        canChange ? (
          <button type="button" className="with-icon" onClick={onAdd}>
            <PlusIcon size={14} />
            {t('chat.addPeople')}
          </button>
        ) : undefined
      }
    />
  )
}
