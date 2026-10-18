import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import {
  AGENT_ASK_EVENT,
  AGENT_NEW_EVENT,
  AGENT_OPEN_EVENT,
  AgentAskDetail,
  AgentNewDetail,
  AgentOpenDetail,
  AgentReference,
  AgentViewing,
  VIEWING_EVENT,
  agentViewing,
  announceMailChanged,
  graphql,
  isNotFound,
  sendToAgentConversation,
  subscribe,
  authorization,
  framedDrawer,
  sharedAttachment,
} from '../api'
import { uploadFiles } from '../upload'
import { suggestedRepliesOf, withoutPartialMarker } from '../suggestions'
import { useAgentConversation } from '../hooks/useAgentConversation'
import { useAgentPresence } from '../hooks/useAgentPresence'
import { useFitToContent } from '../hooks/useFitToContent'
import { budgetNearness, formatClock, formatCount, formatMoney, formatTime } from './common'
import { useResolvedTheme } from './theme'
import { Tooltip } from './tooltip'
import { Markdown } from './markdown'
import { hasSharedCitation, linkSharedCitations, useShowPage } from './dashboardPath'
import { IdeaSuggestions } from './ideaRow'
import { RelativeTime } from './relativeTime'
import { VoiceSession } from '../voice/voiceSession'
import type { InterruptedAnswer } from '../voice/voiceAnswer'
import { VoiceMeter } from '../voice/voiceMeter'
import {
  ArchiveIcon,
  ArrowDownIcon,
  ArrowUpIcon,
  CheckIcon,
  ChevronUpIcon,
  ChevronDownIcon,
  CloseIcon,
  ComputerIcon,
  GlobeIcon,
  InboxIcon,
  ListIcon,
  LogoutIcon,
  MicrophoneIcon,
  MicrophoneOffIcon,
  PhoneHangUpIcon,
  PhoneIcon,
  PaperclipIcon,
  PencilIcon,
  PeopleIcon,
  BellOffIcon,
  MoreIcon,
  StarIcon,
  PlusIcon,
  RefreshIcon,
  SparkIcon,
  CalendarIcon,
  TargetIcon,
  TerminalIcon,
  TrashIcon,
  WarningIcon,
  ExternalIcon,
  PhotoSaveIcon,
} from './icons'
import { BackgroundCommand, BackgroundPanel, useBackgroundCommands } from './backgroundCommands'
import { HerdrQuestionAnswer, HerdrQuestionCard, codingAgentName, paneNameOf, type HerdrSession } from './herdrSessions'
import { SkillShareCallWaitingNote } from './skillShare'
import { Budget, BudgetBar } from './budgetBar'
import { CodeBlock } from './codeBlock'
import { ConfirmDialog } from './dialog'
import { ZoomablePicture } from './lightbox'
import { isReplyReference, ReferenceChips, ReplyQuote } from './agentReferenceChips'
import { isAskedOfThisDrawer, newDrawerId, readBrowserLocation } from './browserLocation'
import { SwipeableLine } from './swipeableLine'
import {
  personReaction,
  ReactionBadges,
  ReactionPicker,
  withReaction,
  type AgentMessageReaction,
} from './agentReactions'
import { AGENT_TURNED_EVENT, announceAgentAvailable, useAgentPreferences } from '../agentPreferences'
import { useToast } from './toast'
import { savePhotoFromAttachment } from '../photos/api'
import { hasPermission, useSession } from '../session'
import { useTranslation, type Key, type Values } from '../i18n/i18n'
import {
  CHAT_FIELDS,
  ChatAvatars,
  ChatLineFrame,
  ChatMembersDialog,
  ChatPeopleDialog,
  DEFAULT_AGENT_NAME,
  DELETE_CHAT_MESSAGE,
  EDIT_CHAT_MESSAGE,
  LEAVE_CHAT,
  MARK_CHAT_READ,
  MUTED_FOR_A_WHILE,
  MUTED_FOR_GOOD,
  MUTE_CHAT,
  MentionList,
  READ_CHAT_MEMBERS,
  RENAME_CHAT,
  RenameChatDialog,
  SET_CHAT_TYPING,
  TYPING_SENT_EVERY,
  TYPING_SHOWN_FOR,
  TypingLine,
  UnreadCount,
  addressableAgentsOf,
  addressedIn,
  agentSpeakerName,
  agentsMatching,
  chatLineOf,
  chatTitleOf,
  isChatMessage,
  isOwnWords,
  linesWithEdit,
  linesWithSaid,
  linesWithStreamedText,
  linesWithDeleted,
  linesWithoutStreaming,
  isStreamingKey,
  mentionAt,
  readersByMessage,
  beginsRun,
  totalUnread,
  withMemberRead,
  withMention,
  type AddressableAgent,
  type ChatLine,
  type ChatMember,
  type ChatStoredMessage,
  type ChatSummary,
  type Mention,
} from './agentChat'

// DEVICES_EVERY is how often the drawer asks what is attached while it is
// open. Attaching and detaching happen outside this page, so there is
// nothing to be told by; often enough to feel immediate, rarely enough to
// be three small reads a minute.
const DEVICES_EVERY = 10_000

// UNREAD_EVERY is how often the drawer asks which chats have something new
// in them. A chat's own events reach the drawer only while it is the one on
// screen; the rest is learned by asking, often enough for the number on the
// button to be the news and rarely enough to be a small read a quarter of a
// minute.
const UNREAD_EVERY = 15_000

// isAgentOffRefusal is whether an error says only that the agent is off. A
// person whose agent is off is still among people, and the drawer is for
// them: it never tells them so in a toast.
export function isAgentOffRefusal(error: unknown): boolean {
  const message = error instanceof Error ? error.message : String(error ?? '')
  return message.trim() === 'agent: unavailable'
}

// The events only a chat has.
const CHAT_EVENTS = new Set(['said', 'edited', 'deleted', 'read', 'typing', 'members'])

// CHAT_CONFIRMED_WITHIN is how long a message just sent in a chat is given
// to come back as the chat's own before the chat is read again.
const CHAT_CONFIRMED_WITHIN = 3_000

// hasChats is whether there is anything for a drawer to open on without an
// agent: a chat already, or a person to start one with.
async function hasChats(): Promise<boolean> {
  try {
    const listed = await graphql<{ ListAgentConversations: { kind: string }[] }>(CHAT_UNREAD)
    if (listed.ListAgentConversations.some((conversation) => conversation.kind === 'chat')) return true
  } catch {
    // Nothing to read is nothing to open on; the people may still be there.
  }
  try {
    const people = await graphql<{ ListCirclePeople: { userId: string }[] }>('query { ListCirclePeople { userId } }')
    return (people.ListCirclePeople ?? []).length > 0
  } catch {
    return false
  }
}

// The drawer: the person talking to their agent from any page, in the one
// continuous conversation or a named one, with what they have open told to
// the agent so "this" means it. The conversation's turns stream back over
// the websocket, whichever surface started them — this drawer, a phone, a
// terminal, a chat app: the words as they come, the tools as they run,
// and a card when the agent needs the person's word before it does
// something it cannot undo. A
// second turn sent while one runs queues behind it on the server, so the
// person never waits to type; files come with a turn, and a thread can be
// pointed at from the reader.

// Where a conversation's goal stands, as the server writes it. Empty is
// the fourth answer and the commonest one: there is no goal.
type GoalState = 'working' | 'waiting' | 'met' | 'dropped'

export interface Conversation {
  id: string
  kind: 'main' | 'named' | 'run' | 'goal' | 'chat'
  // Who is in it, for a chat; null on every other conversation.
  chat?: ChatSummary | null
  // For a run, what made it: a dream, a triage, a call over MCP.
  jobKind?: string
  title: string
  summary?: string
  lastAt: string
  archivedAt?: string | null
  // The standing instruction this conversation carries, if any: what the
  // agent keeps working toward across turns of its own. goalNote is its
  // last word on where it is, and goalNextAt when it looks again.
  goal?: string
  goalState?: GoalState | ''
  goalNote?: string
  goalNextAt?: string | null
  goalSetAt?: string | null
}

// One item of the task list a conversation carries. The agent writes the
// list with its todo tool as it works through something in several steps
// and reads it back every round; the person ticks, adds to and takes from
// the same list.
interface Todo {
  id: string
  text: string
  doneAt?: string | null
}

// The marker a turn of the agent's own begins with, which is
// models.GoalCheckInMarker on the server. A user message starting with it
// is the agent checking in against the goal, not the person, and the
// transcript draws it as a line rather than as their bubble.
const GOAL_CHECK_IN_MARKER = '[goal check-in]'

// The marker a turn begins with when a command the agent left running in
// the background has ended, which is models.BackgroundCommandMarker on the
// server. Like a check-in it is the agent's own turn in the person's
// shape, and is drawn the same quiet way.
const BACKGROUND_COMMAND_MARKER = '[background command]'

// The marker a turn begins with when a survey or a subagent the agent left
// running in the background has finished, which is
// models.BackgroundWorkMarker on the server. Drawn the same quiet way.
const BACKGROUND_WORK_MARKER = '[background work]'

// The marker a schedule's turn begins with when it answers in a
// conversation, which is models.ScheduleMarker on the server.
const SCHEDULE_MARKER = '[schedule]'

// The marker a turn begins with when the agent starts a conversation on
// its own, to introduce itself, check what it remembers or offer an idea,
// which is models.SpeakFirstMarker on the server.
const SPEAK_FIRST_MARKER = '[speaking first]'

// The surface such a turn is taken on, "speak_first:idea" and the like. A
// turn that begins with it in the main conversation opens the drawer.
const SPEAK_FIRST_SURFACE = 'speak_first:'

// The marker an alert is written under: the agent telling the person,
// unasked, what their mail showed, which is models.AlertMarker on the
// server. Its surface opens the drawer as speaking first does.
const ALERT_MARKER = '[alert]'
const ALERT_SURFACE = 'alert'

// The marker a goal kept in the background is written under in the main
// conversation when it comes to need the person, followed by the goal's
// id, which is models.GoalNeedsYouMarker on the server. Its surface opens
// the drawer as an alert's does.
const GOAL_NEEDS_YOU_MARKER = '[goal needs you]'
const GOAL_NEEDS_YOU_SURFACE = 'goal_needs_you'

// The marker a question from one of the person's herdr coding sessions is
// written under in the main conversation, followed by the computer, the
// pane and the question's fingerprint, which is models.HerdrQuestionMarker
// on the server. Its surface opens the drawer as an alert's does, and its
// line carries the question's options for the person to answer.
const HERDR_QUESTION_MARKER = '[herdr question]'
const HERDR_QUESTION_SURFACE = 'herdr_question'

// The kind of the note written when a herdr question went, which is
// models.NoteHerdrAnswered on the server; its detail is the computer, the
// pane and the fingerprint, as the question's marker line names them.
const HERDR_ANSWERED_NOTE = 'herdr_answered'

// The kind of the note written beside a herdr question when it is asked,
// which is models.NoteHerdrAsked: the session and the question whole, so
// the question is drawn as its card from the transcript alone.
const HERDR_ASKED_NOTE = 'herdr_asked'

// A call of a skill the person shared that waits for their approval, drawn as a
// card by SkillShareCallWaitingNote from the call the note names.
const SKILL_SHARE_CALL_WAITING_NOTE = 'skill_share_call_waiting'

// The marker a turn begins with when a herdr coding session the agent
// watched has finished, which is models.HerdrSessionMarker on the server.
const HERDR_SESSION_MARKER = '[herdr session]'

// The marker a turn begins with when the server restarted in the middle
// of a turn that answered the person and the agent carries on, which is
// models.RestartResumeMarker on the server.
const RESTART_RESUME_MARKER = '[resuming after a restart]'

// What a question card answers when the person would rather talk than
// pick, which is askuser.ChatAboutIt on the server: the same in every
// language, so the tool can tell it from an answer.
const CHAT_ABOUT_IT = '[chat about it]'

// How long a question card takes what is typed in the box as its answer.
const CARD_FRESH_MS = 60 * 60 * 1000

// The most of a message a reply quotes, as the server keeps it: enough to
// say which part of a long answer is meant.
const REPLY_QUOTE_CHARACTERS = 1000

// Which kind of turn of the agent's own a user message opens, if it opens
// one at all.
type CheckInOrigin =
  | 'goal'
  | 'goalNeedsYou'
  | 'background'
  | 'backgroundWork'
  | 'schedule'
  | 'speakFirst'
  | 'alert'
  | 'herdrQuestion'
  | 'herdrSession'
  | 'restartResume'
  | 'approved'
  | 'declined'

// The markers a turn begins with when the person answers a card after the
// turn that raised it had ended, which are agent.AnsweringMarker,
// ApprovedMarker and DeclinedMarker on the server. An answer is drawn as
// the person's own words under the question; an approval as a line.
const ANSWERING_MARKER = '[answering]'
const APPROVED_MARKER = '[approved]'
const DECLINED_MARKER = '[declined]'

function checkInOriginOf(text: string): CheckInOrigin | null {
  if (text.startsWith(GOAL_CHECK_IN_MARKER)) return 'goal'
  if (text.startsWith(GOAL_NEEDS_YOU_MARKER)) return 'goalNeedsYou'
  if (text.startsWith(BACKGROUND_COMMAND_MARKER)) return 'background'
  if (text.startsWith(BACKGROUND_WORK_MARKER)) return 'backgroundWork'
  if (text.startsWith(SCHEDULE_MARKER)) return 'schedule'
  if (text.startsWith(SPEAK_FIRST_MARKER)) return 'speakFirst'
  if (text.startsWith(ALERT_MARKER)) return 'alert'
  if (text.startsWith(HERDR_QUESTION_MARKER)) return 'herdrQuestion'
  if (text.startsWith(HERDR_SESSION_MARKER)) return 'herdrSession'
  if (text.startsWith(RESTART_RESUME_MARKER)) return 'restartResume'
  if (text.startsWith(APPROVED_MARKER)) return 'approved'
  if (text.startsWith(DECLINED_MARKER)) return 'declined'
  return null
}

// answeringOf reads a late answer to a question: the question it answers
// and the answer itself, or null for any other message.
function answeringOf(text: string): { question: string; answer: string } | null {
  if (!text.startsWith(ANSWERING_MARKER)) return null
  const rest = text.slice(ANSWERING_MARKER.length).trim()
  const split = rest.indexOf('\n\n')
  if (split < 0) return { question: '', answer: rest }
  return { question: rest.slice(0, split).trim(), answer: rest.slice(split + 2).trim() }
}

interface Artifact {
  artifact_id: string
  title: string
  kind: 'html' | 'svg' | 'markdown'
  url: string
}

// artifactOf reads what the artifact tool answered, if this is its line.
function artifactOf(line: { tool: string; result?: string }): Artifact | null {
  if (line.tool !== 'artifact' || !line.result) return null
  try {
    const parsed = JSON.parse(unfenced(line.result)) as Partial<Artifact>
    if (parsed.artifact_id && parsed.url && parsed.title) return parsed as Artifact
  } catch {
    // Not an artifact after all: an error, most likely.
  }
  return null
}

// unfenced is a tool's answer with the fence around it taken off.
//
// What a tool fetched from outside is wrapped before it reaches the model,
// so that a page or a message cannot pass itself off as an instruction. It
// is still the tool's own JSON inside, and a reader that wants to draw what
// is in it has to get past the fence first -- which is why a picture a
// skill fetched was filed, named in the answer, and drawn nowhere.
function unfenced(result: string): string {
  const text = result.trim()
  if (!text.startsWith(UNTRUSTED_OPEN) || !text.endsWith(UNTRUSTED_CLOSE)) return result
  return text.slice(UNTRUSTED_OPEN.length, text.length - UNTRUSTED_CLOSE.length).trim()
}

const UNTRUSTED_OPEN = '<untrusted-data>'
const UNTRUSTED_CLOSE = '</untrusted-data>'

// A file the agent handed over, as share_file answered.
interface SharedFile {
  attachment_id: string
  name: string
  content_type: string
  size: number
  url: string
  caption?: string
  // The photo this is a preview of, where the photo tool showed it. The
  // card links to that photo in Photos.
  photo_id?: string
}

// sharedFilesOf reads the files a tool line handed the person.
//
// Two tools answer with them and they are not the same shape: share_file
// was asked for one file and answers with it flat, while a skill whose step
// fetched pictures or a clip answers with a list of them under files. Both
// end up drawn the same way, because from the reader's side they are the
// same thing -- something the agent put in the conversation for them.
export function sharedFilesOf(line: { tool: string; result?: string }): SharedFile[] {
  if (!line.result) return []
  try {
    const parsed = JSON.parse(unfenced(line.result)) as Partial<SharedFile> & { files?: Partial<SharedFile>[] }
    if (drawable(parsed)) return [photoLinked(line.tool, parsed)]
    // A file a skill fetched but could not keep has no address to draw it
    // from, and is left out rather than drawn as a broken one.
    if (Array.isArray(parsed.files)) return parsed.files.filter(drawable).map((file) => photoLinked(line.tool, file))
  } catch {
    // An error, most likely.
  }
  return []
}

// photoLinked keeps the photo_id of a file only where the photo tool
// handed it over and it is shaped like an id of ours. Any other tool's
// answer is somebody else's text, and the card turns the id into a link
// inside this app, so it is not taken from there.
function photoLinked(tool: string, file: SharedFile): SharedFile {
  const { photo_id: photoId, ...rest } = file
  if (tool === 'photo' && typeof photoId === 'string' && PHOTO_ID_PATTERN.test(photoId)) {
    return { ...rest, photo_id: photoId }
  }
  return rest
}

const PHOTO_ID_PATTERN = /^[A-Za-z0-9_-]{1,64}$/

// drawable says whether this is a file of the conversation, addressed the
// way this server addresses one.
//
// Checking the address, not the tool that named it: most of what a tool
// answers with came from outside -- a page, a service, a house -- and a
// file's address is drawn into the page as the source of an image. A
// service that put a url of its own in its answer could otherwise have the
// dashboard fetch it, which is somebody else's server learning when the
// person read their conversation, and from where. So an address is drawn
// only when it is one of this server's own attachment addresses and names
// the same file the entry does.
function drawable(file: Partial<SharedFile> | null | undefined): file is SharedFile {
  if (!file?.attachment_id || !file.url || !file.name) return false
  // The exact address this server builds, and nothing that merely contains
  // it: https://somewhere-else/api/v1/agent/attachments/x ends with the
  // right path and is not this server.
  return file.url === ATTACHMENT_PATH + file.attachment_id
}

const ATTACHMENT_PATH = '/api/v1/agent/attachments/'

// Today's spend against the day's budget, in tokens and in money. A
// limit of zero is no limit of that kind; where both are set, whichever
// runs out first stops the day, and the ring shows that one.

// budgetShown is the budget the ring draws: the one nearer its end where
// both are set, and null where neither is.
function budgetShown(budget: Budget): { used: string; limit: string; fraction: number; money: boolean } | null {
  const tokens = budget.limit > 0 ? budget.used / budget.limit : -1
  const money = budget.costLimit > 0 ? budget.cost / budget.costLimit : -1
  if (tokens < 0 && money < 0) return null
  if (money >= tokens) {
    return {
      used: formatMoney(budget.cost, budget.currency),
      limit: formatMoney(budget.costLimit, budget.currency),
      fraction: money,
      money: true,
    }
  }
  return { used: formatCount(budget.used), limit: formatCount(budget.limit), fraction: tokens, money: false }
}

interface Attachment {
  id: string
  name: string
  contentType: string
  size: number
  // The file went with the account that held it; the message it came with
  // is kept.
  isRemoved?: boolean | null
}

// CitedFile is one picture or file the fact behind a citation was read
// from: what it is, where it was posted, and where its bytes are served
// from. The path is empty for a file this server no longer holds the
// bytes of, which is a name with nothing to open behind it.
interface CitedFile {
  documentId: string
  name: string
  contentType: string
  channel: string
  path: string
}

interface Usage {
  promptTokens: number
  completionTokens: number
  cost?: number
}

interface StoredMessage extends ChatStoredMessage {
  id: string
  createdAt?: string
  role: string
  content: string
  name?: string
  toolCallId?: string
  toolCalls?: { id: string; name: string; arguments: string }[]
  usage?: Usage | null
  attachments?: Attachment[] | null
  references?: AgentReference[] | null
  reactions?: AgentMessageReaction[] | null
}

interface RunEvent {
  kind:
    | 'asked'
    | 'text'
    | 'message'
    | 'tool_call'
    | 'tool_result'
    | 'confirmation'
    | 'question'
    | 'note'
    | 'titled'
    | 'navigate'
    | 'locate'
    | 'reacted'
    | 'done'
    | 'error'
    // A chat's own: a message said, edited or deleted, a member's read mark
    // moved, and a member typing.
    | 'said'
    | 'edited'
    | 'deleted'
    | 'read'
    | 'typing'
    // A member changed what their agent does in the chat.
    | 'members'
  runId: string
  sequence: number
  at?: string
  text?: string
  tool?: string
  callId?: string
  arguments?: string
  risk?: string
  note?: string
  noteKind?: string
  noteDetail?: string
  error?: string
  // On a locate event, the drawer that is to answer it.
  drawerId?: string
  // On a reacted event, the message, the emoji now on it from that
  // reactor (empty when it was taken away), and who: person or agent.
  messageId?: string
  emoji?: string
  reactorKind?: string
  // In a chat: who reacted, who read or is typing, and the message said or
  // edited, whole.
  reactorUserId?: string | null
  reactorName?: string | null
  userId?: string
  name?: string
  // On a typing event of an agent, which agent is thinking; on a text event
  // in a chat, the agent whose answer is being written. Its owner's name is
  // what tells it from another person's agent of the same name.
  authorAgentId?: string
  authorOwnerName?: string
  message?: StoredMessage | null
  // The message was too long to travel whole, and the chat is read again.
  isMessageCut?: boolean
}

// A line of the transcript as the drawer draws it.
type Line =
  // inReplyTo is the question a late answer answers.
  | {
      kind: 'user'
      key: string
      text: string
      at?: string
      attachments?: Attachment[]
      references?: AgentReference[]
      inReplyTo?: string
      // The stored message's id, once the transcript is read: what a reply
      // to it names. A line just sent or still arriving has none yet.
      messageId?: string
      reactions?: AgentMessageReaction[]
    }
  | {
      kind: 'assistant'
      key: string
      text: string
      at?: string
      streaming?: boolean
      usage?: Usage | null
      messageId?: string
      reactions?: AgentMessageReaction[]
    }
  | {
      kind: 'tool'
      key: string
      tool: string
      note: string
      done: boolean
      arguments?: string
      result?: string
      at?: string
    }
  | {
      kind: 'confirmation'
      key: string
      runId: string
      callId: string
      tool: string
      summary: string
      risk: string
      resolved?: 'approved' | 'declined'
    }
  | {
      kind: 'question'
      key: string
      runId: string
      callId: string
      question: string
      choices: string[]
      answered?: string
      // When the card was raised: typing in the box answers only a card
      // that is fresh, never one ignored days ago.
      raisedAt?: string
    }
  // detail is what a compaction's line opens to: the note itself.
  // isPending marks the line of a compaction still being written.
  // noteKind is the note's kind where it has one, which decides whether it
  // is a working note shown only on request.
  | { kind: 'note'; key: string; text: string; at?: string; detail?: string; isPending?: boolean; noteKind?: string }
  | { kind: 'error'; key: string; text: string }
  // A turn the agent started on its own: against the goal, or because a
  // background command ended. Its words are framing for the model and were
  // never the person's, so only the hour is drawn.
  | { kind: 'checkin'; key: string; at?: string; text: string; origin: CheckInOrigin }
  // A message in a chat: a person's or an agent's, with who said it.
  | ChatLine

const AGENT = `
  query {
    ReadAgent { agent { id enabled name } allowed { enabled ask } }
  }`

// READ_VOICE says whether the drawer offers the microphone.
const READ_VOICE = `query { ReadAgentVoice { isVoiceAvailable sampleRate } }`

const TAB = `
  query {
    ReadAgentTab { attached title url }
    ReadAgentComputers { computers { name system description } }
    ReadAgent { budget { used limit resetsAt cost costLimit currency } timezone }
  }`

// Also read by the agent's page of conversations, which lists the same
// conversations the picker does.
export const CONVERSATIONS = `
  query ($archived: Boolean, $query: String) {
    ListAgentConversations(archived: $archived, query: $query) {
      id kind title summary lastAt archivedAt goal goalState goalNote goalNextAt goalSetAt
      chat ${CHAT_FIELDS}
    }
  }`

// What the drawer's button counts while it is shut: only the chats, and of
// those only what is unread.
const CHAT_UNREAD = `
  query {
    ListAgentConversations(archived: false) { id kind chat { unreadCount isMuted } }
  }`

// What waits for the person in each conversation: the questions and
// approvals not yet answered, across all of them, so the list can say which
// conversation needs them without opening each.
const WAITING = `
  query {
    ReadAgentWaiting(first: 50) { waitingCount interactions { conversationId } }
  }`

// The agent's task list alone, read again after each change its todo tool
// makes, so the person watches the steps move while a turn is running.
const CONVERSATION_TODOS = `
  query ($conversationId: String) {
    ReadAgentConversation(conversationId: $conversationId, first: 1) { todos { id text doneAt } }
  }`

const CONVERSATION = `
  query ($conversationId: String, $first: Int, $offset: Int) {
    ReadAgentConversation(conversationId: $conversationId, first: $first, offset: $offset) {
      conversation { id kind jobKind title summary lastAt archivedAt goal goalState goalNote goalNextAt goalSetAt chat ${CHAT_FIELDS} }
      actingAs
      messages {
        id createdAt role content name toolCallId toolCalls { id name arguments }
        usage { promptTokens completionTokens cost }
        attachments { id name contentType size isRemoved }
        references { itemId threadId subject from path name recordingId recordingTitle photoId photoTitle financeTransactionId postedOn amount currencyCode merchantName description agentMessageId quotedRole quotedText }
        reactions { emoji reactorKind reactedAt reactorUserId reactorName }
        authorKind authorUserId authorName authorOwnerName authorAvatarUrl authorAvatarColorIndex authorAgentId agentRunId isMine isAgentWork editedAt isDeleted
      }
      total
      todos { id text doneAt }
    }
    ListAgentInteractions(conversationId: $conversationId) { runId callId interactionKind toolName interactionText interactionChoices toolRisk createdAt }
  }`

// The files behind the citations an answer made. An assistant's line
// carries text and nothing else -- only a person's own message may carry
// a file -- so a screenshot an answer was read out of can only be reached
// through what the answer already says: the agent cites what it used, as
// "work/mcx#3", and this resolves those citations to the fact and from
// there to the file. Nothing the model does has to change.
const CITED = `
  query ($citations: [String!]!) {
    AgentCitedAttachments(citations: $citations) {
      citation
      files { documentId name contentType channel path }
    }
  }`

// Who shared the pages shared with the person, which is how a citation of one
// ("people/a-grandparent#3 (shared by Ada)", a display name) is linked to its
// owner's username.
const SHARED_OWNERS = `query { ListSharedAgentPages { ownerUsername ownerName } }`

const ASK = `
  mutation ($conversationId: String, $message: String!, $viewing: ViewingInput, $surface: String, $interruptedAnswer: InterruptedAnswerInput, $attachmentIds: [String!], $references: [AgentReferenceInput!], $drawerId: String, $addressedAgentIds: [String!]) {
    AskAgent(conversationId: $conversationId, message: $message, viewing: $viewing, surface: $surface, interruptedAnswer: $interruptedAnswer, attachmentIds: $attachmentIds, references: $references, drawerId: $drawerId, addressedAgentIds: $addressedAgentIds) { runId conversationId }
  }`

const FEED = `
  subscription ($conversationId: String!) {
    AgentConversationEvents(conversationId: $conversationId) {
      kind runId sequence at text tool callId arguments risk note noteKind noteDetail error drawerId messageId emoji reactorKind reactorUserId reactorName userId name authorAgentId authorOwnerName isMessageCut
      message {
        id createdAt role content
        attachments { id name contentType size isRemoved }
        references { agentMessageId quotedRole quotedText }
        reactions { emoji reactorKind reactedAt reactorUserId reactorName }
        authorKind authorUserId authorName authorOwnerName authorAvatarUrl authorAvatarColorIndex authorAgentId agentRunId isMine isAgentWork editedAt isDeleted
      }
    }
  }`

const ANSWER = `
  mutation ($runId: String!, $callId: String!, $answer: String!) {
    AnswerAgentQuestion(runId: $runId, callId: $callId, answer: $answer)
  }`

// The browser's answer to a turn that asked where the person is.
const ANSWER_LOCATION = `
  mutation ($runId: String!, $callId: String!, $latitudeDegrees: Float, $longitudeDegrees: Float, $accuracyMeters: Float, $measuredAt: String, $errorMessage: String) {
    AnswerAgentLocation(runId: $runId, callId: $callId, latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, accuracyMeters: $accuracyMeters, measuredAt: $measuredAt, errorMessage: $errorMessage)
  }`

const RESOLVE = `
  mutation ($runId: String!, $callId: String!, $approve: Boolean!) {
    ResolveAgentConfirmation(runId: $runId, callId: $callId, approve: $approve)
  }`

const STOP = `
  mutation ($runId: String!) {
    StopAgentRun(runId: $runId)
  }`

// The person's reaction on a message; an empty emoji takes it away.
const REACT = `
  mutation ($messageId: String!, $emoji: String!) {
    ReactToAgentMessage(messageId: $messageId, emoji: $emoji)
  }`

// A new conversation starts with nothing but itself: it is named after its
// first turn, and a goal, if it wants one, is set from the drawer's head.
const START = `
  mutation {
    StartAgentConversation { id kind title summary lastAt archivedAt }
  }`

// A variable left out is a field left alone: renaming sends no goal, and
// setting a goal sends no title. An empty goal is not nothing — it is the
// person saying there is no goal any more.
const UPDATE = `
  mutation ($conversationId: String!, $title: String, $archived: Boolean) {
    UpdateAgentConversation(conversationId: $conversationId, title: $title, archived: $archived) { id }
  }`

const DELETE = `
  mutation ($conversationId: String!) {
    DeleteAgentConversation(conversationId: $conversationId)
  }`

const MAKE_MAIN = `
  mutation ($conversationId: String) {
    SetAgentMainConversation(conversationId: $conversationId) { id kind title summary lastAt archivedAt }
  }`

// The tools after which what the mailbox shows may have changed. The rules
// and the folders are one tool each now, whatever action they were asked
// for: a list is a read and refreshing after one costs nothing.
const MAIL_TOOLS = new Set(['mail_act', 'mail_draft', 'mail_send', 'folder', 'rule', 'mailbox_settings', 'reply_queue'])

const OPEN_KEY = 'teanode.agent.drawer'
const CONVERSATION_KEY = 'teanode.agent.conversation'

function remembered(key: string): string {
  try {
    return localStorage.getItem(key) ?? ''
  } catch {
    return ''
  }
}

// openDrawerOnNextLoad has the drawer open when the page next loads, for a
// page that is about to reload into a conversation.
export function openDrawerOnNextLoad() {
  remember(OPEN_KEY, '1')
}

function remember(key: string, value: string) {
  try {
    localStorage.setItem(key, value)
  } catch {
    // A browser that keeps nothing forgets, which is fine.
  }
}

// PLACEMENT_KEY holds where a person moved the box on a wide window and how
// big they made it, as {left, top, width, height} in CSS pixels. Absent, the
// box sits in its corner at the size the stylesheet gives it.
const PLACEMENT_KEY = 'teanode.agentChatBox'
// The width at which the box stops floating and takes the screen; the
// stylesheet's media query for the drawer says the same number.
const PHONE_QUERY = '(max-width: 720px)'
const MINIMUM_WIDTH = 320
const MINIMUM_HEIGHT = 360
// How close to the window's edge the box may go, so that the edge it is
// grabbed by is never outside the window.
const VIEWPORT_MARGIN = 8

type Placement = { left: number; top: number; width: number; height: number }

// The edges a resize grip moves: one for a side, two for a corner.
type ResizeEdges = { isTop: boolean; isLeft: boolean; isBottom: boolean; isRight: boolean }

const RESIZE_GRIPS: { name: string; edges: ResizeEdges }[] = [
  { name: 'top', edges: { isTop: true, isLeft: false, isBottom: false, isRight: false } },
  { name: 'left', edges: { isTop: false, isLeft: true, isBottom: false, isRight: false } },
  { name: 'bottom', edges: { isTop: false, isLeft: false, isBottom: true, isRight: false } },
  { name: 'right', edges: { isTop: false, isLeft: false, isBottom: false, isRight: true } },
  { name: 'top-left', edges: { isTop: true, isLeft: true, isBottom: false, isRight: false } },
  { name: 'top-right', edges: { isTop: true, isLeft: false, isBottom: false, isRight: true } },
  { name: 'bottom-left', edges: { isTop: false, isLeft: true, isBottom: true, isRight: false } },
  { name: 'bottom-right', edges: { isTop: false, isLeft: false, isBottom: true, isRight: true } },
]

// A move or a resize in progress: which pointer, where it went down, and
// where the box was then. Every step is measured from the start, so a
// step the browser dropped costs nothing.
type PlacementGesture = {
  pointerId: number
  startX: number
  startY: number
  startPlacement: Placement
  edges: ResizeEdges | null
  hasMoved: boolean
}

// How far a pointer goes before a press on the bar becomes a move: a click
// that wobbles by a pixel is still a click, and pins nothing.
const GESTURE_THRESHOLD = 3

function isPhoneWidth(): boolean {
  return window.matchMedia(PHONE_QUERY).matches
}

// clampPlacement keeps the box inside the window: no smaller than the
// minimum where the window has room for it, no larger than the window, and
// moved rather than cut when the window shrinks under it.
function clampPlacement(placement: Placement): Placement {
  const availableWidth = Math.max(0, window.innerWidth - 2 * VIEWPORT_MARGIN)
  const availableHeight = Math.max(0, window.innerHeight - 2 * VIEWPORT_MARGIN)
  const width = Math.min(Math.max(placement.width, Math.min(MINIMUM_WIDTH, availableWidth)), availableWidth)
  const height = Math.min(Math.max(placement.height, Math.min(MINIMUM_HEIGHT, availableHeight)), availableHeight)
  const left = Math.min(Math.max(placement.left, VIEWPORT_MARGIN), VIEWPORT_MARGIN + availableWidth - width)
  const top = Math.min(Math.max(placement.top, VIEWPORT_MARGIN), VIEWPORT_MARGIN + availableHeight - height)
  return { left, top, width, height }
}

// fullScreenPlacement is the whole window, less the margin every placement
// keeps from its edges.
function fullScreenPlacement(): Placement {
  return clampPlacement({ left: 0, top: 0, width: window.innerWidth, height: window.innerHeight })
}

function isSamePlacement(first: Placement, second: Placement): boolean {
  return (
    first.left === second.left &&
    first.top === second.top &&
    first.width === second.width &&
    first.height === second.height
  )
}

// resizedPlacement moves the edges a grip holds by how far the pointer
// went, holding the opposite edge still: a box shrunk from its left keeps
// its right edge where it was, including when it reaches its minimum.
function resizedPlacement(start: Placement, edges: ResizeEdges, deltaX: number, deltaY: number): Placement {
  let { left, top, width, height } = start
  const right = start.left + start.width
  const bottom = start.top + start.height
  if (edges.isLeft) {
    left = Math.min(Math.max(start.left + deltaX, VIEWPORT_MARGIN), right - MINIMUM_WIDTH)
    width = right - left
  }
  if (edges.isRight) {
    width = Math.min(Math.max(start.width + deltaX, MINIMUM_WIDTH), window.innerWidth - VIEWPORT_MARGIN - left)
  }
  if (edges.isTop) {
    top = Math.min(Math.max(start.top + deltaY, VIEWPORT_MARGIN), bottom - MINIMUM_HEIGHT)
    height = bottom - top
  }
  if (edges.isBottom) {
    height = Math.min(Math.max(start.height + deltaY, MINIMUM_HEIGHT), window.innerHeight - VIEWPORT_MARGIN - top)
  }
  return clampPlacement({ left, top, width, height })
}

function rememberedPlacement(): Placement | null {
  try {
    const stored = JSON.parse(localStorage.getItem(PLACEMENT_KEY) ?? 'null') as Partial<Placement> | null
    if (
      stored &&
      [stored.left, stored.top, stored.width, stored.height].every(
        (dimension) => typeof dimension === 'number' && Number.isFinite(dimension),
      )
    ) {
      return clampPlacement(stored as Placement)
    }
  } catch {
    // Unreadable or unparsable, the box sits in its corner.
  }
  return null
}

function rememberPlacement(placement: Placement | null) {
  try {
    if (placement) {
      localStorage.setItem(PLACEMENT_KEY, JSON.stringify(placement))
    } else {
      localStorage.removeItem(PLACEMENT_KEY)
    }
  } catch {
    // A browser that keeps nothing puts the box back in its corner next time.
  }
}

// isGestureExempt says whether a pointer went down on something in the
// header that is pressed rather than grabbed: the buttons, a link, and the
// list the picker opens. The title, which opens the list, is not: it runs
// most of the header's width, and exempting it left almost nothing to take
// hold of. A press on it that moves is a move; one that does not is still
// a click.
function isGestureExempt(target: EventTarget | null): boolean {
  if (!(target instanceof Element) || target.closest('[role="menu"]')) {
    return target instanceof Element
  }
  const pressed = target.closest('button, a, input, select, textarea')
  return pressed !== null && !pressed.classList.contains('agent-drawer-conversation')
}

// usePlacement lets a person move the floating box by its header and resize
// it by its edges on a wide window, and remembers where they left it. The
// steps of a gesture are written straight to the element's custom
// properties rather than through state, because the drawer is a large tree
// and a render per pointer move would lag behind the pointer; state, and
// the stored copy, are brought up to date when the pointer comes up.
function usePlacement(isEnabled: boolean) {
  const [placement, setPlacement] = useState<Placement | null>(() => (isEnabled ? rememberedPlacement() : null))
  const [isRepositioning, setIsRepositioning] = useState(false)
  const boxElement = useRef<HTMLElement | null>(null)
  const gesture = useRef<PlacementGesture | null>(null)
  const livePlacement = useRef<Placement | null>(placement)

  // A window made smaller brings the box back inside it. What is stored
  // stays as the person left it, so a window made large again gets it back
  // on the next load.
  useEffect(() => {
    if (!isEnabled) return
    const onResize = () => {
      const previous = livePlacement.current
      if (!previous || gesture.current) return
      const clamped = clampPlacement(previous)
      if (isSamePlacement(clamped, previous)) return
      livePlacement.current = clamped
      setPlacement(clamped)
    }
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [isEnabled])

  const writePlacement = (next: Placement) => {
    livePlacement.current = next
    const element = boxElement.current
    if (!element) return
    element.style.setProperty('--agent-drawer-left', `${next.left}px`)
    element.style.setProperty('--agent-drawer-top', `${next.top}px`)
    element.style.setProperty('--agent-drawer-width', `${next.width}px`)
    element.style.setProperty('--agent-drawer-height', `${next.height}px`)
  }

  const begin = (pointerEvent: React.PointerEvent<HTMLElement>, edges: ResizeEdges | null) => {
    if (!isEnabled || pointerEvent.button !== 0 || isPhoneWidth() || !boxElement.current) return
    const bounds = boxElement.current.getBoundingClientRect()
    const startPlacement = clampPlacement({
      left: bounds.left,
      top: bounds.top,
      width: bounds.width,
      height: bounds.height,
    })
    gesture.current = {
      pointerId: pointerEvent.pointerId,
      startX: pointerEvent.clientX,
      startY: pointerEvent.clientY,
      startPlacement,
      edges,
      hasMoved: false,
    }
    // A grip is a few pixels wide and has no click to keep, so it holds
    // the pointer from the press; the header waits until it is a move.
    if (edges) {
      if (!pointerEvent.currentTarget.hasPointerCapture(pointerEvent.pointerId)) {
        pointerEvent.currentTarget.setPointerCapture(pointerEvent.pointerId)
      }
    }
    // Held down, a pointer would otherwise select the title it drags across.
    pointerEvent.preventDefault()
  }

  const onPointerMove = (pointerEvent: React.PointerEvent<HTMLElement>) => {
    const current = gesture.current
    if (!current || current.pointerId !== pointerEvent.pointerId) return
    const deltaX = pointerEvent.clientX - current.startX
    const deltaY = pointerEvent.clientY - current.startY
    if (!current.hasMoved) {
      if (Math.abs(deltaX) < GESTURE_THRESHOLD && Math.abs(deltaY) < GESTURE_THRESHOLD) return
      current.hasMoved = true
      // Captured only once it is a move: captured from the press, the
      // click that follows a still press would go to the header rather
      // than to the title under it, and the list would never open.
      pointerEvent.currentTarget.setPointerCapture(pointerEvent.pointerId)
      // From here on the box is placed by its custom properties rather than
      // docked by the stylesheet; they start where it already is.
      writePlacement(current.startPlacement)
      setPlacement(current.startPlacement)
      setIsRepositioning(true)
    }
    const next = current.edges
      ? resizedPlacement(current.startPlacement, current.edges, deltaX, deltaY)
      : clampPlacement({
          ...current.startPlacement,
          left: current.startPlacement.left + deltaX,
          top: current.startPlacement.top + deltaY,
        })
    writePlacement(next)
  }

  const onPointerEnd = (pointerEvent: React.PointerEvent<HTMLElement>) => {
    const current = gesture.current
    if (!current || current.pointerId !== pointerEvent.pointerId) return
    gesture.current = null
    if (pointerEvent.currentTarget.hasPointerCapture(pointerEvent.pointerId)) {
      pointerEvent.currentTarget.releasePointerCapture(pointerEvent.pointerId)
    }
    if (!current.hasMoved) return
    setIsRepositioning(false)
    const finished = livePlacement.current
    if (!finished) return
    setPlacement(finished)
    rememberPlacement(finished)
  }

  // reset puts the box back in its corner at its first size, and forgets
  // where it was.
  const reset = () => {
    livePlacement.current = null
    setPlacement(null)
    rememberPlacement(null)
  }

  // toggleFullScreen fills the window with the box, or, when it already
  // fills it, puts it back in its corner.
  const toggleFullScreen = () => {
    const full = fullScreenPlacement()
    const current = livePlacement.current
    if (current && isSamePlacement(current, full)) {
      reset()
      return
    }
    writePlacement(full)
    setPlacement(full)
    rememberPlacement(full)
  }

  const headProps = {
    onPointerDown: (pointerEvent: React.PointerEvent<HTMLElement>) => {
      if (isGestureExempt(pointerEvent.target)) return
      begin(pointerEvent, null)
    },
    onPointerMove,
    onPointerUp: onPointerEnd,
    onPointerCancel: onPointerEnd,
    // A double click on the bar fills the window with the box, and a
    // second puts it back in its corner at its first size.
    onDoubleClick: (mouseEvent: React.MouseEvent<HTMLElement>) => {
      if (!isEnabled || isPhoneWidth() || isGestureExempt(mouseEvent.target)) return
      toggleFullScreen()
    },
  }

  const gripProps = (edges: ResizeEdges) => ({
    onPointerDown: (pointerEvent: React.PointerEvent<HTMLElement>) => begin(pointerEvent, edges),
    onPointerMove,
    onPointerUp: onPointerEnd,
    onPointerCancel: onPointerEnd,
  })

  const style = placement
    ? ({
        '--agent-drawer-left': `${placement.left}px`,
        '--agent-drawer-top': `${placement.top}px`,
        '--agent-drawer-width': `${placement.width}px`,
        '--agent-drawer-height': `${placement.height}px`,
      } as React.CSSProperties)
    : undefined

  return { placement, isRepositioning, boxElement, style, headProps, gripProps, reset }
}

function formatBytes(size: number): string {
  if (size >= 1 << 20) return `${(size / (1 << 20)).toFixed(1)} MB`
  if (size >= 1 << 10) return `${Math.round(size / (1 << 10))} kB`
  return `${size} B`
}

// dayOf is the calendar day a time falls on, in the person's zone, for
// the dividers between days.
function dayOf(at: string | undefined): string {
  if (!at) return ''
  const date = new Date(at)
  if (Number.isNaN(date.getTime())) return ''
  return `${date.getFullYear()}-${date.getMonth()}-${date.getDate()}`
}

function dayLabel(at: string, today: string, yesterday: string): string {
  const date = new Date(at)
  const now = new Date()
  const day = dayOf(at)
  if (day === dayOf(now.toISOString())) return today
  const before = new Date(now)
  before.setDate(now.getDate() - 1)
  if (day === dayOf(before.toISOString())) return yesterday
  return date.toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' })
}

// clockTime is the hour and minute a moment falls on, in the reader's own
// zone. Used where the day is already known -- the dividers say it, and a
// goal's next look is always today or tomorrow -- so the date would be
// noise on a line that is meant to be read past.
function clockTime(at?: string): string {
  if (!at) return ''
  const moment = new Date(at)
  if (Number.isNaN(moment.getTime())) return ''
  return moment.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })
}

// What goes in the box when a page asks for a draft: after what is already
// written there, on a line of its own.
function withAskedDraft(kept: string, asked: string): string {
  if (asked === '') return kept
  return kept.trim() === '' ? asked : `${kept.trimEnd()}\n${asked}`
}

function draftKey(conversationId: string): string {
  return `teanode.agent.draft.${conversationId || 'main'}`
}

function isImage(contentType: string): boolean {
  return /^image\/(png|jpeg|gif|webp)/i.test(contentType)
}

// A reference to one thing the agent knows, as it writes one into an
// answer: a page's path and the fact's number after a hash --
// "work/mcx#3", "people/alice-chen#12", "self#4". A path is slugs joined
// by slashes, so the pattern is that and nothing else.
//
// The character before is consumed and refused where it is part of a word
// or an address, so that the fragment on the end of a link and the
// "issue#3" in a sentence are not read as citations. Anything that gets
// through and means nothing resolves to nothing, which costs one lookup.
//
// Not one followed by " (shared by ": that is a fact of a page somebody else
// shared, and asking for it among the person's own pages could show the file
// of a page of theirs at the same path.
const CITATION = /(?:^|[^\w#/-])([a-z0-9][a-z0-9-]*(?:\/[a-z0-9][a-z0-9-]*)*#\d{1,6})(?![\w-])(?! \(shared by )/g

// citationsIn is the references one answer makes, in the order it makes
// them and without repeats.
export function citationsIn(text: string): string[] {
  const found: string[] = []
  for (const match of text.matchAll(CITATION)) {
    if (!found.includes(match[1])) found.push(match[1])
  }
  return found
}

// citedIn is the files one answer's citations resolved to, in the order
// the answer cites them, with each file named once however many of its
// citations point at it.
function citedIn(text: string, resolved: Record<string, CitedFile[]>): CitedFile[] {
  const files: CitedFile[] = []
  // Nothing resolved, nothing to look for. This is drawn on every render
  // of every answer, a transcript is redrawn on every token of a turn in
  // flight, and most conversations cite nothing with a file behind it.
  if (Object.keys(resolved).length === 0) return files
  for (const citation of citationsIn(text)) {
    for (const file of resolved[citation] ?? []) {
      if (!files.some((already) => already.documentId === file.documentId)) files.push(file)
    }
  }
  return files
}

function attachmentHref(attachment: Attachment): string {
  return `/api/v1/agent/attachments/${encodeURIComponent(attachment.id)}`
}

// idOfAttachment reads the file's id out of one of these addresses, which
// is what the server signs for.
function idOfAttachment(url: string): string {
  const match = /\/agent\/attachments\/([A-Za-z0-9_-]+)/.exec(url)
  return match ? match[1] : ''
}

// useFileHref is the address to put on an img, a video or an iframe. On
// the dashboard it is the plain one and the session cookie carries it.
// Framed into another site there is no cookie, so the server signs an
// address for that one file; until it answers there is nothing to draw.
function useFileHref(url: string): string | null {
  const [signed, setSigned] = useState<string | null>(framedDrawer ? null : url)
  useEffect(() => {
    if (!framedDrawer) {
      setSigned(url)
      return
    }
    const attachmentId = idOfAttachment(url)
    if (!attachmentId) {
      setSigned(url)
      return
    }
    let cancelled = false
    sharedAttachment(attachmentId)
      .then((address) => {
        if (!cancelled) setSigned(address)
      })
      .catch(() => {
        if (!cancelled) setSigned(null)
      })
    return () => {
      cancelled = true
    }
  }, [url])
  return signed
}

// openAttachment opens a file the drawer cannot address directly. Framed
// into another site the plain address has nothing to authenticate it, so
// the link is followed only once the server has signed one.
function openAttachment(event: React.MouseEvent<HTMLAnchorElement>) {
  if (!framedDrawer) return
  const attachmentId = idOfAttachment(event.currentTarget.getAttribute('href') ?? '')
  if (!attachmentId) return
  event.preventDefault()
  void sharedAttachment(attachmentId).then((address) => {
    window.open(address, '_blank', 'noopener')
  })
}

// AttachedPicture is a picture somebody handed the agent, shown where they
// sent it. Its address is signed when the drawer is framed elsewhere, so
// there is a moment before there is anything to show -- and the lightbox is
// given that same signed address, because it is the only one that loads
// from inside another site's page.
//
// Nothing here says where it came from: a file handed to the agent came
// from the person reading this, in the turn it is drawn under.
function AttachedPicture({ attachment }: { attachment: Attachment }) {
  const href = useFileHref(attachmentHref(attachment))
  if (!href) return null
  return (
    <ZoomablePicture
      source={href}
      name={attachment.name}
      imageClassName="agent-attachment-image"
      openTitle={attachment.name}
    />
  )
}

// linesOf turns stored messages into what the drawer draws: the person's
// turns with what came with them, the agent's answers with what they cost,
// and a line per tool it used, with what the tool answered.
// The kinds of note the server words by kind, and the words for each. A
// note of a kind carries only its detail -- the goal, the reason -- and is
// worded here in the person's language; a note without one is the
// server's prose, shown as written.
const NOTE_WORDS: Record<string, { plain: Key; withDetail?: Key }> = {
  queued: { plain: 'agentDrawer.note.queued' },
  stopped: { plain: 'agentDrawer.note.stopped', withDetail: 'agentDrawer.note.stoppedBecause' },
  failed: { plain: 'agentDrawer.note.failed', withDetail: 'agentDrawer.note.failedBecause' },
  call_unreadable: { plain: 'agentDrawer.note.callUnreadable' },
  repeated_failure: { plain: 'agentDrawer.note.repeatedFailure' },
  round_limit: { plain: 'agentDrawer.note.roundLimit' },
  compacting: { plain: 'agentDrawer.note.compacting' },
  compacted: { plain: 'agentDrawer.note.compacted' },
  depth: { plain: 'agentDrawer.note.depth', withDetail: 'agentDrawer.note.depthBecause' },
  goal_set: { plain: 'agentDrawer.note.goalSet', withDetail: 'agentDrawer.note.goalSet' },
  goal_set_again: { plain: 'agentDrawer.note.goalSetAgain', withDetail: 'agentDrawer.note.goalSetAgain' },
  goal_changed: { plain: 'agentDrawer.note.goalChanged', withDetail: 'agentDrawer.note.goalChanged' },
  goal_cleared: { plain: 'agentDrawer.note.goalCleared', withDetail: 'agentDrawer.note.goalCleared' },
  goal_met: { plain: 'agentDrawer.note.goalMet', withDetail: 'agentDrawer.note.goalMet' },
  goal_stalled: { plain: 'agentDrawer.note.goalStalled', withDetail: 'agentDrawer.note.goalStalled' },
  circle_invited: { plain: 'agentDrawer.note.circleInvited' },
  chat_agent_owner_only: {
    plain: 'agentDrawer.note.chatAgentOwnerOnly',
    withDetail: 'agentDrawer.note.chatAgentOwnerOnlyNamed',
  },
  chat_agent_off: { plain: 'agentDrawer.note.chatAgentOff', withDetail: 'agentDrawer.note.chatAgentOffNamed' },
  chat_agent_absent: { plain: 'agentDrawer.note.chatAgentAbsent' },
}

// LEGACY_NOTES is the English each kind was written in before notes had
// kinds, and the kind it was.
const LEGACY_NOTES: [string, string][] = [
  ['stopped', 'stopped'],
  ['looking into this carefully', 'depth'],
  ['a tool call the server could not read; asked again', 'call_unreadable'],
  ['failed', 'failed'],
  ['Goal set again', 'goal_set_again'],
  ['Goal set', 'goal_set'],
  ['Goal changed', 'goal_changed'],
  ['Goal cleared', 'goal_cleared'],
  ['Goal met', 'goal_met'],
]

// resolvedNoteKind is a note's kind, read back from its English words for
// one written before notes had kinds.
function resolvedNoteKind(noteKind: string, prose: string): string {
  if (NOTE_WORDS[noteKind]) return noteKind
  const legacy = LEGACY_NOTES.find(([english]) => prose === english || prose.startsWith(english + ': '))
  return legacy ? legacy[1] : ''
}

// WORKING_NOTES are the notes about how the agent went about a turn rather
// than what happened in the conversation: shown with the working notes
// switch, for somebody looking at what it did.
const WORKING_NOTES = new Set(['depth', 'call_unreadable'])

// noteWords is a note as the drawer shows it. A compaction's detail is the
// note it wrote, which its line opens to rather than repeats.
function noteWords(
  t: (key: Key, values?: Values) => string,
  noteKind: string,
  noteDetail: string,
  prose: string,
): string {
  if (!NOTE_WORDS[noteKind]) {
    // Written before notes had kinds, in English: read the kind back from
    // the words it was written with, so an old transcript is worded too.
    const legacy = LEGACY_NOTES.find(([english]) => prose === english || prose.startsWith(english + ': '))
    if (!legacy) return prose
    ;[, noteKind] = legacy
    noteDetail = prose.slice(legacy[0].length + 2)
  }
  const words = NOTE_WORDS[noteKind]
  if (noteKind === 'circle_invited') {
    // Two names cannot share one plain string, so the server sends both as
    // JSON; one it cannot read is shown as the server's own English.
    try {
      const invitation = JSON.parse(noteDetail) as { invitedByName?: string; circleName?: string }
      if (invitation.circleName) {
        return t(words.plain, { invitedByName: invitation.invitedByName ?? '', circleName: invitation.circleName })
      }
    } catch {
      // Not JSON: fall through to the prose.
    }
    return prose
  }
  if (noteKind === 'chat_agent_owner_only') {
    // Whose agent it is, so that the default name "Agent" is not read as a
    // person; a note written before it was told says only the agent's name.
    try {
      const owned = JSON.parse(noteDetail) as { agentName?: string; ownerName?: string }
      if (owned.ownerName) {
        const agentName = (owned.agentName ?? '').trim()
        return agentName && agentName !== DEFAULT_AGENT_NAME
          ? t('agentDrawer.note.chatAgentOwnerOnlyOwnedNamed', { owner: owned.ownerName, agent: agentName })
          : t('agentDrawer.note.chatAgentOwnerOnlyOwned', { owner: owned.ownerName })
      }
    } catch {
      // The agent's name alone: the words below.
    }
  }
  if (noteDetail && words.withDetail) {
    return t(words.withDetail, { detail: noteDetail })
  }
  return t(words.plain, { detail: noteDetail })
}

// answerOf is what a question card was answered with, read from the
// ask_user call's result; the result itself when it is not that shape.
function answerOf(result: string | undefined): string {
  try {
    const parsed = JSON.parse(result ?? '') as { answer?: unknown }
    if (typeof parsed.answer === 'string' && parsed.answer) return parsed.answer
  } catch {
    // Not JSON: the words are the answer.
  }
  return result || '…'
}

// isDeclined says whether a confirmation's call came back declined.
function isDeclined(result: string | undefined): boolean {
  try {
    return (JSON.parse(result ?? '') as { declined?: unknown }).declined === true
  } catch {
    return false
  }
}

function linesOf(messages: StoredMessage[], t: (key: Key, values?: Values) => string, isChat = false): Line[] {
  const lines: Line[] = []
  const results = new Map<string, string>()
  for (const message of messages) {
    if (message.role === 'tool' && message.toolCallId) {
      results.set(message.toolCallId, message.content)
    }
  }
  // What a turn cost is what every round of it cost: the rounds that
  // only called tools have usage and no words, and the answer at the end
  // used to show its own round alone, which for a turn of six rounds
  // was a sixth of the truth. Added up from the person's message on, and
  // shown once, on the turn's last answer.
  const running: { turn: Usage | null; lastAnswer: number } = { turn: null, lastAnswer: -1 }
  const closeTurn = () => {
    if (running.lastAnswer >= 0 && running.turn) {
      const answer = lines[running.lastAnswer]
      if (answer.kind === 'assistant') {
        lines[running.lastAnswer] = { ...answer, usage: running.turn }
      }
    }
    running.turn = null
    running.lastAnswer = -1
  }
  for (const message of messages) {
    // What was said in a chat is drawn as what a person said there, whoever
    // it was; the rows of the member's own agent's work in it are drawn as
    // anywhere else.
    if (isChat && isChatMessage(message)) {
      lines.push(chatLineOf(message))
      continue
    }
    switch (message.role) {
      case 'user':
        closeTurn()
        // A check-in is a user message only because that is the shape a
        // turn starts in. Nobody typed it, so none of it is shown.
        {
          const origin = checkInOriginOf(message.content)
          if (origin) {
            lines.push({ kind: 'checkin', key: message.id, at: message.createdAt, text: message.content, origin })
            break
          }
        }
        {
          const answering = answeringOf(message.content)
          lines.push({
            kind: 'user',
            key: message.id,
            text: answering ? answering.answer : message.content,
            inReplyTo: answering?.question || undefined,
            at: message.createdAt,
            attachments: message.attachments ?? undefined,
            references: message.references ?? undefined,
            messageId: message.id,
            reactions: message.reactions ?? undefined,
          })
        }
        break
      case 'assistant':
        if (message.usage) {
          running.turn = {
            promptTokens: (running.turn?.promptTokens ?? 0) + message.usage.promptTokens,
            completionTokens: (running.turn?.completionTokens ?? 0) + message.usage.completionTokens,
            cost: (running.turn?.cost ?? 0) + (message.usage.cost ?? 0),
          }
        }
        if (message.content.trim()) {
          lines.push({
            kind: 'assistant',
            key: message.id,
            text: message.content,
            at: message.createdAt,
            messageId: message.id,
            reactions: message.reactions ?? undefined,
          })
          running.lastAnswer = lines.length - 1
        }
        for (const call of message.toolCalls ?? []) {
          lines.push({
            kind: 'tool',
            key: `${message.id}-${call.id}`,
            tool: call.name,
            note: '',
            done: true,
            arguments: call.arguments,
            result: results.get(call.id),
            // Timed like every other line, so the day divider does not
            // land under the tool calls that open a day.
            at: message.createdAt,
          })
        }
        break
      case 'compaction':
        lines.push({
          kind: 'note',
          key: message.id,
          text: t('agentDrawer.note.compacted'),
          detail: message.content,
          at: message.createdAt,
        })
        break
      case 'note':
        // That a coding session's question went: drawn by nobody, read by
        // the question's line, which then starts collapsed. Not timed, so
        // it opens no day of its own.
        if (message.name === HERDR_ANSWERED_NOTE || message.name === HERDR_ASKED_NOTE) {
          lines.push({ kind: 'note', key: message.id, text: '', noteKind: message.name, detail: message.content })
          break
        }
        if (message.name === SKILL_SHARE_CALL_WAITING_NOTE) {
          lines.push({ kind: 'note', key: message.id, text: '', noteKind: message.name, detail: message.content })
          break
        }
        // Timed, so that the day divider counts a note that opens a day
        // -- "Goal set" is the first line of a fresh conversation -- and
        // does not land under it.
        lines.push({
          kind: 'note',
          key: message.id,
          text: noteWords(t, message.name ?? '', message.content, message.content),
          noteKind: resolvedNoteKind(message.name ?? '', message.content),
          at: message.createdAt,
        })
        break
      default:
        break
    }
  }
  closeTurn()
  return lines
}

// TodoLine is one of the agent's steps on a single line. A step too long
// for the line is cut with an ellipsis; its tooltip holds the whole of it,
// and a tap or Enter opens it in place, for a phone that has no hover.
function TodoLine({ todo }: { todo: Todo }) {
  const { t } = useTranslation()
  const text = useRef<HTMLSpanElement>(null)
  const [isCut, setIsCut] = useState(false)
  const [isOpen, setIsOpen] = useState(false)
  const isDone = Boolean(todo.doneAt)
  useEffect(() => {
    const element = text.current
    if (!element) return
    const measure = () => setIsCut(element.scrollWidth > element.clientWidth)
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [todo.text])
  const canOpen = isCut || isOpen
  return (
    <li className={[isDone ? 'done' : '', isOpen ? 'open' : ''].filter(Boolean).join(' ')}>
      <span className="agent-drawer-todo-mark">
        {isDone ? <CheckIcon size={12} /> : null}
        <span className="visually-hidden">{isDone ? t('agentDrawer.todoDone') : t('agentDrawer.todoOpen')}</span>
      </span>
      <Tooltip label={isCut && !isOpen ? todo.text : ''}>
        <span
          ref={text}
          className="agent-drawer-todo-text"
          role={canOpen ? 'button' : undefined}
          tabIndex={canOpen ? 0 : undefined}
          aria-expanded={canOpen ? isOpen : undefined}
          onClick={canOpen ? () => setIsOpen((previous) => !previous) : undefined}
          onKeyDown={
            canOpen
              ? (event) => {
                  if (event.key === 'Enter' || event.key === ' ') {
                    event.preventDefault()
                    setIsOpen((previous) => !previous)
                  }
                }
              : undefined
          }
        >
          {todo.text}
        </span>
      </Tooltip>
    </li>
  )
}

// QuestionCard is the agent asking: its choices to pick from, then Submit,
// or "Chat about it", which closes the card and hands the conversation
// back for an answer the choices do not hold. Anything else is typed in
// the chat box below, which answers the card while it is fresh; a second
// box on the card only made two places to type.
function QuestionCard({
  line,
  onAnswer,
  onChat,
}: {
  line: Extract<Line, { kind: 'question' }>
  onAnswer: (text: string) => void
  onChat: () => void
}) {
  const { t } = useTranslation()
  const [chosen, setChosen] = useState('')
  return (
    <div className="agent-line confirmation">
      <p>{line.question}</p>
      {line.answered ? (
        <p className="muted">{line.answered}</p>
      ) : (
        <>
          {line.choices.length > 0 ? (
            <div className="agent-question-choices" role="radiogroup" aria-label={line.question}>
              {line.choices.map((choice) => (
                <button
                  key={choice}
                  type="button"
                  role="radio"
                  aria-checked={chosen === choice}
                  className={chosen === choice ? 'chosen' : undefined}
                  onClick={() => setChosen(choice)}
                >
                  {choice}
                </button>
              ))}
            </div>
          ) : null}
          <div className="agent-question-actions">
            <button type="button" onClick={onChat}>
              {t('agentDrawer.chatAboutIt')}
            </button>
            {line.choices.length > 0 ? (
              <button type="button" className="primary" disabled={!chosen} onClick={() => onAnswer(chosen)}>
                {t('agentDrawer.submit')}
              </button>
            ) : null}
          </div>
        </>
      )}
    </div>
  )
}

// AttachmentChips are the files under a turn: a picture shown small, the
// rest named, each a link to the file itself.
function AttachmentChips({ attachments }: { attachments: Attachment[] }) {
  const { t } = useTranslation()
  return (
    <div className="agent-attachments">
      {attachments.map((attachment) =>
        attachment.isRemoved ? (
          <span key={attachment.id} className="agent-attachment-chip muted">
            <PaperclipIcon size={12} /> {t('chat.fileRemoved')}
          </span>
        ) : attachment.id.startsWith('pending-') ? (
          <span key={attachment.id} className="agent-attachment-chip">
            <PaperclipIcon size={12} /> {attachment.name} <span className="muted">{formatBytes(attachment.size)}</span>
          </span>
        ) : isImage(attachment.contentType) ? (
          <AttachedPicture key={attachment.id} attachment={attachment} />
        ) : (
          <a
            key={attachment.id}
            href={attachmentHref(attachment)}
            className="agent-attachment-chip"
            download={attachment.name}
            onClick={openAttachment}
          >
            <PaperclipIcon size={12} /> {attachment.name} <span className="muted">{formatBytes(attachment.size)}</span>
          </a>
        ),
      )}
    </div>
  )
}

// ArtifactCard is what the agent made, shown under the line that made it:
// a page or a drawing in a frame of its own with no way out of it, a
// document as text. It opens larger, and in a tab of its own.
function ArtifactCard({ artifact }: { artifact: Artifact }) {
  // The page is told which theme the drawer is in, since its sandbox cannot
  // see the dashboard's choice.
  const shownTheme = useResolvedTheme()
  // The page says how tall it is, and the frame follows, within reason.
  const frame = useRef<HTMLIFrameElement>(null)
  const [height, setHeight] = useState<number | null>(null)
  useEffect(() => {
    const listen = (event: MessageEvent) => {
      if (!frame.current || event.source !== frame.current.contentWindow) return
      const told = (event.data as { teanodeArtifact?: { height?: unknown } } | null)?.teanodeArtifact?.height
      if (typeof told === 'number' && Number.isFinite(told)) setHeight(Math.min(640, Math.max(120, Math.ceil(told))))
    }
    window.addEventListener('message', listen)
    return () => window.removeEventListener('message', listen)
  }, [])
  const { t } = useTranslation()
  const framed = useFileHref(artifact.url)
  const [markdown, setMarkdown] = useState<string | null>(null)
  useEffect(() => {
    if (artifact.kind !== 'markdown') return
    let cancelled = false
    fetch(artifact.url, { credentials: 'same-origin', headers: authorization() })
      .then((response) => (response.ok ? response.text() : Promise.reject(new Error(response.statusText))))
      .then((text) => {
        if (!cancelled) setMarkdown(text)
      })
      .catch(() => {
        if (!cancelled) setMarkdown('')
      })
    return () => {
      cancelled = true
    }
  }, [artifact.kind, artifact.url])
  return (
    <div className="agent-artifact">
      <div className="agent-artifact-head">
        <SparkIcon size={11} />
        <span className="agent-artifact-title">{artifact.title}</span>
        <Tooltip label={t('agentDrawer.openArtifact')}>
          <a
            className="icon-button agent-artifact-open"
            href={framed ?? undefined}
            target="_blank"
            rel="noreferrer"
            aria-label={t('agentDrawer.openArtifact')}
          >
            <ExternalIcon size={14} />
          </a>
        </Tooltip>
      </div>
      {artifact.kind === 'markdown' ? (
        <div className="agent-artifact-body">
          {markdown === null ? <span className="muted">…</span> : <Markdown text={markdown} />}
        </div>
      ) : framed ? (
        <iframe
          ref={frame}
          className="agent-artifact-frame"
          title={artifact.title}
          src={`${framed}#theme=${shownTheme}`}
          sandbox="allow-scripts"
          style={height === null ? undefined : { height }}
        />
      ) : null}
    </div>
  )
}

// FileCard is a file the agent handed over, under the tool line: a
// picture shown, a video or a sound playing, anything else to open.
export function FileCard({ file }: { file: SharedFile }) {
  const { t } = useTranslation()
  const toast = useToast()
  const navigate = useNavigate()
  const session = useSession()
  const signed = useFileHref(file.url)
  // The photo the file was kept as, once the person kept it here.
  const [savedPhotoId, setSavedPhotoId] = useState<string | null>(null)
  const [isSaving, setIsSaving] = useState(false)
  // Nothing is drawn until there is an address to draw it from.
  const href = signed ?? undefined
  const type = file.content_type.toLowerCase()
  const photoAddress = file.photo_id || savedPhotoId ? `/photos/${file.photo_id || savedPhotoId}` : null
  // A picture or a video can be kept in Photos, unless it is a photo already
  // or Photos is off for this person.
  const canSaveToPhotos =
    !photoAddress &&
    (isImage(type) || type.startsWith('video/')) &&
    session.services?.isPhotosEnabled === true &&
    hasPermission(session.permissions, 'photos:use')

  async function saveToPhotos() {
    setIsSaving(true)
    try {
      const saved = await savePhotoFromAttachment(file.attachment_id)
      setSavedPhotoId(saved.id)
      toast.done(t('agentDrawer.savedToPhotos'), {
        label: t('agentDrawer.openPhoto'),
        run: () => navigate(`/photos/${encodeURIComponent(saved.id)}`),
      })
    } catch (caught) {
      toast.failure(caught, t('agentDrawer.saveToPhotosFailed'))
    } finally {
      setIsSaving(false)
    }
  }
  const picture = signed ? <img src={signed} alt={file.name} className="agent-file-media" /> : null
  const media = !signed ? null : isImage(type) ? (
    photoAddress ? (
      <Link to={photoAddress} aria-label={t('agentDrawer.openPhoto')}>
        {picture}
      </Link>
    ) : (
      picture
    )
  ) : type.startsWith('video/') ? (
    <video src={signed} controls preload="metadata" className="agent-file-media" />
  ) : type.startsWith('audio/') ? (
    <audio src={signed} controls preload="metadata" className="agent-file-audio" />
  ) : null
  return (
    <div className="agent-artifact agent-file">
      <div className="agent-artifact-head">
        <PaperclipIcon size={11} />
        {photoAddress ? (
          <Link className="agent-artifact-title" to={photoAddress}>
            {file.name}
          </Link>
        ) : (
          <span className="agent-artifact-title">{file.name}</span>
        )}
        <span className="muted">{formatBytes(file.size)}</span>
        {canSaveToPhotos && (
          <Tooltip label={t('agentDrawer.saveToPhotos')}>
            <button
              type="button"
              className="icon-button agent-artifact-open"
              aria-label={t('agentDrawer.saveToPhotos')}
              disabled={isSaving}
              onClick={() => void saveToPhotos()}
            >
              <PhotoSaveIcon size={14} />
            </button>
          </Tooltip>
        )}
        <Tooltip label={t('agentDrawer.openArtifact')}>
          <a
            className="icon-button agent-artifact-open"
            href={href}
            target="_blank"
            rel="noreferrer"
            aria-label={t('agentDrawer.openArtifact')}
          >
            <ExternalIcon size={14} />
          </a>
        </Tooltip>
      </div>
      {media}
      {file.caption ? <div className="agent-file-caption">{file.caption}</div> : null}
    </div>
  )
}

// CitedEvidence is what an answer was read from, under the answer: the
// picture itself, or the file's name where it is not a picture, with
// where it was posted.
//
// The agent is not taught to attach anything. It cites what it used, as
// it always has, and this follows the citation back to the file -- so a
// person sees the screenshot the answer rests on rather than being asked
// to take the sentence on trust.
function CitedEvidence({ files }: { files: CitedFile[] }) {
  const shown = files.filter((file) => file.path !== '')
  if (shown.length === 0) return null
  return (
    <div className="agent-cited">
      {shown.map((file) => (
        <CitedPicture key={file.documentId} file={file} />
      ))}
    </div>
  )
}

// CitedPicture is one of those files: the picture, which opens into the
// lightbox, or the name where it is not one.
//
// Framed into another site the picture is a name too, and no lightbox. The
// drawer is on the dashboard's own origin there, but a third-party cookie
// is not sent with it, and an indexed file has no signed address the way a
// file of the conversation has -- so drawing an img would draw a broken
// one, in the lightbox as much as in the transcript. A link opened in a tab
// of its own is a top-level request, which carries the session and works
// from either place.
function CitedPicture({ file }: { file: CitedFile }) {
  const { t } = useTranslation()
  // Where it was posted, which is the channel.
  //
  // The thread was in this line too, and a thread here is an identifier
  // the service made -- "b17nohesk3b7xn95nhoypatzby" -- which is not
  // anywhere anybody has been. Every one of them on the archive this was
  // written against is twenty-six characters of nothing, under a picture,
  // in the line that is supposed to say where the picture came from. The
  // channel beside it is the part that answers that.
  const where = file.channel.trim()
  const said = where === '' ? t('agentDrawer.citedFile') : t('agentDrawer.citedFrom', { where })
  return (
    <span className="agent-cited-file">
      {isImage(file.contentType) && !framedDrawer ? (
        <ZoomablePicture
          source={file.path}
          name={file.name}
          where={where}
          imageClassName="agent-cited-image"
          openTitle={t('agentDrawer.citedOpen')}
        />
      ) : (
        <a className="link" href={file.path} target="_blank" rel="noreferrer">
          {file.name}
        </a>
      )}
      <span className="muted">{said}</span>
    </span>
  )
}

// BudgetRing is the day's tokens as a ring in the drawer's head: how much
// of the budget has gone, coloured by how near the end of it the day is,
// with the numbers and the hour it resets on hover, and the usage dropped
// down under the head on a press. With no limit to be near it is the
// ring's empty track: the dropdown is still where the person chooses what
// the conversation shows of the agent's work.
function BudgetRing({
  budget,
  zone,
  isOpen,
  onToggle,
}: {
  budget: Budget | null
  zone: string
  isOpen: boolean
  onToggle: () => void
}) {
  const { t } = useTranslation()
  const shown = budget ? budgetShown(budget) : null
  if (!budget || !shown) {
    return (
      <HeadMark label={t('agentDrawer.usageTitle')} className="agent-drawer-budget" isOpen={isOpen} onToggle={onToggle}>
        <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
          <circle className="agent-budget-track" cx="8" cy="8" r={6} />
        </svg>
      </HeadMark>
    )
  }
  const fraction = Math.max(0, Math.min(1, shown.fraction))
  const percent = Math.round(fraction * 100)
  const nearness = budgetNearness(fraction, 1)
  const radius = 6
  const round = 2 * Math.PI * radius
  // Said in whichever the budget is counted in, and what it came to in
  // money when that is not the same thing.
  const spent = shown.money
    ? ''
    : ` ${t('agentDrawer.budgetSpent', { spent: formatMoney(budget.cost, budget.currency) })}`
  const label = `${t('agentDrawer.budget', { used: shown.used, limit: shown.limit, percent: String(percent) })}${spent} ${t(
    'agentDrawer.budgetResets',
    { at: formatClock(budget.resetsAt, zone) },
  )}`
  const ring = (
    <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
      <circle className="agent-budget-track" cx="8" cy="8" r={radius} />
      <circle
        className={`agent-budget-fill ${nearness}`}
        cx="8"
        cy="8"
        r={radius}
        strokeDasharray={`${round * fraction} ${round}`}
        transform="rotate(-90 8 8)"
      />
    </svg>
  )
  return (
    <HeadMark label={label} className="agent-drawer-budget" isOpen={isOpen} onToggle={onToggle}>
      {ring}
    </HeadMark>
  )
}

// The head's dropdowns.
//
// Every mark in the drawer's head -- the goal, what is attached, the day's
// budget, what is running in the background -- opens the same way the
// list of conversations does: a list dropped down under the head, over the
// conversation, closed by anywhere outside it or Escape. What is behind a
// mark is on the settings pages, reached from the sidebar as ever. Not a dialog over
// the page, because the drawer is where the person is looking; and not a
// panel over the whole box, because the head is what the box is dragged by
// and its edges what it is resized by.

// HeadMenuName is which of them is open.
type HeadMenuName = 'computers' | 'tab' | 'usage' | 'background' | 'chat'

// HeadMenu is the frame: a small title, and what is under it.
function HeadMenu({
  title,
  className,
  onClose,
  children,
}: {
  title: string
  className?: string
  onClose: () => void
  children: React.ReactNode
}) {
  return (
    <section
      className={['agent-drawer-list', 'head-menu', className].filter(Boolean).join(' ')}
      aria-label={title}
      onKeyDown={(event) => {
        if (event.key === 'Escape') {
          event.stopPropagation()
          onClose()
        }
      }}
    >
      <header className="head-menu-title">
        <strong>{title}</strong>
      </header>
      {children}
    </section>
  )
}

// HeadMark is a mark in the head that opens its dropdown.
function HeadMark({
  label,
  className,
  isOpen,
  onToggle,
  children,
}: {
  label: string
  className?: string
  isOpen: boolean
  onToggle: () => void
  children: React.ReactNode
}) {
  return (
    <Tooltip label={label}>
      <button
        type="button"
        className={['icon-button', className].filter(Boolean).join(' ')}
        aria-label={label}
        aria-expanded={isOpen}
        onClick={onToggle}
      >
        {children}
      </button>
    </Tooltip>
  )
}

// AttachedComputer is one of the person's computers as the drawer lists it.
interface AttachedComputer {
  name: string
  system?: string
  description?: string
}

// ComputersMenu lists what is attached, a line each: the name, and the
// person's own words about it, which is what tells two apart.
function ComputersMenu({ computers, onClose }: { computers: AttachedComputer[]; onClose: () => void }) {
  const { t } = useTranslation()
  return (
    <HeadMenu title={t('agentDrawer.computersTitle')} onClose={onClose}>
      {computers.map((computer) => (
        <div key={computer.name} className="head-menu-row" title={computer.description || computer.name}>
          <ComputerIcon size={14} />
          <span className="head-menu-row-name">{computer.name}</span>
          <span className="head-menu-row-detail muted">
            {[computer.description, computer.system].filter(Boolean).join(' · ')}
          </span>
        </div>
      ))}
    </HeadMenu>
  )
}

// TabMenu is the attached tab, a line: its title, and the site it is on.
function TabMenu({ tab, onClose }: { tab: { title?: string; url?: string }; onClose: () => void }) {
  const { t } = useTranslation()
  const site = siteOf(tab.url)
  return (
    <HeadMenu title={t('agentDrawer.tabTitle')} onClose={onClose}>
      <div className="head-menu-row" title={tab.url}>
        <GlobeIcon size={14} />
        <span className="head-menu-row-name">{tab.title || site}</span>
        <span className="head-menu-row-detail muted">{site}</span>
      </div>
    </HeadMenu>
  )
}

// siteOf is the host a tab is on, or the address as it came when it is
// not one that can be read.
function siteOf(url: string | undefined): string {
  if (!url) return ''
  try {
    return new URL(url).host
  } catch {
    return url
  }
}

// UsageMenu is the day's budget, drawn as the agent's page draws it: how
// much has gone, as words and as a bar coloured by how near the end it
// is, what it came to, and when it starts again.
// And what the conversation shows of the agent's work -- the tool calls,
// and what each answer used -- switched where the usage is, rather than in
// the settings, far from the conversation they change.
function UsageMenu({ budget, zone, onClose }: { budget: Budget | null; zone: string; onClose: () => void }) {
  const { t } = useTranslation()
  const [preferences, setPreferences] = useAgentPreferences()
  return (
    <HeadMenu title={t('agentDrawer.usageTitle')} onClose={onClose}>
      {budget && budgetShown(budget) ? (
        <div className="head-menu-usage">
          <BudgetBar budget={budget} zone={zone} />
        </div>
      ) : null}
      <div className="head-menu-switches">
        <label className="checkbox">
          <input
            type="checkbox"
            checked={preferences.showTools}
            onChange={(event) => setPreferences({ showTools: event.target.checked })}
          />
          {t('agentDrawer.showTools')}
        </label>
        <label className="checkbox">
          <input
            type="checkbox"
            checked={preferences.showUsage}
            onChange={(event) => setPreferences({ showUsage: event.target.checked })}
          />
          {t('agentDrawer.showUsage')}
        </label>
        <label className="checkbox">
          <input
            type="checkbox"
            checked={preferences.showWorkingNotes}
            onChange={(event) => setPreferences({ showWorkingNotes: event.target.checked })}
          />
          {t('agentDrawer.showWorkingNotes')}
        </label>
      </div>
    </HeadMenu>
  )
}

// ChatMenu is what can be done about the chat on screen: who is in it, its
// name if it is a group, its sound, and leaving it. A circle's chat follows
// the circle, so it is neither renamed nor left from here.
function ChatMenu({
  chat,
  onMembers,
  onRename,
  onMute,
  onLeave,
  onClose,
}: {
  chat: ChatSummary
  onMembers: () => void
  onRename: () => void
  onMute: (mutedUntil: string | null) => void
  onLeave: () => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const isForGood = chat.mutedUntil !== null && chat.mutedUntil >= '2999'
  return (
    <HeadMenu title={t('chat.menu')} className="chat-menu" onClose={onClose}>
      <button type="button" className="head-menu-row head-menu-action" role="menuitem" onClick={onMembers}>
        <PeopleIcon size={14} />
        <span className="head-menu-row-name">{t('chat.members')}</span>
      </button>
      {chat.chatKind === 'group' && !chat.isCircleChat ? (
        <button type="button" className="head-menu-row head-menu-action" role="menuitem" onClick={onRename}>
          <PencilIcon size={14} />
          <span className="head-menu-row-name">{t('chat.rename')}</span>
        </button>
      ) : null}
      {chat.isMuted ? (
        <button type="button" className="head-menu-row head-menu-action" role="menuitem" onClick={() => onMute(null)}>
          <BellOffIcon size={14} />
          <span className="head-menu-row-name">{t('chat.unmute')}</span>
          {chat.mutedUntil && !isForGood ? (
            <span className="head-menu-row-detail muted">
              {t('chat.mutedUntil', { time: formatTime(chat.mutedUntil) })}
            </span>
          ) : null}
        </button>
      ) : (
        <>
          <button
            type="button"
            className="head-menu-row head-menu-action"
            role="menuitem"
            onClick={() => onMute(new Date(Date.now() + MUTED_FOR_A_WHILE).toISOString())}
          >
            <BellOffIcon size={14} />
            <span className="head-menu-row-name">{t('chat.muteForAWhile')}</span>
          </button>
          <button
            type="button"
            className="head-menu-row head-menu-action"
            role="menuitem"
            onClick={() => onMute(MUTED_FOR_GOOD)}
          >
            <BellOffIcon size={14} />
            <span className="head-menu-row-name">{t('chat.muteForGood')}</span>
          </button>
        </>
      )}
      {chat.chatKind === 'group' && !chat.isCircleChat ? (
        <button type="button" className="head-menu-row head-menu-action danger" role="menuitem" onClick={onLeave}>
          <LogoutIcon size={14} />
          <span className="head-menu-row-name">{t('chat.leave')}</span>
        </button>
      ) : null}
    </HeadMenu>
  )
}

// goalStateOf is the state to draw a conversation's goal in. A goal with
// no state is one the server has not written a state for yet, and it is
// working: that is what setting one does.
function goalStateOf(conversation: Conversation): GoalState {
  return conversation.goalState || 'working'
}

// goalStateKey is what a state is called, as a key of the catalogue.
function goalStateKey(state: GoalState): `agentDrawer.goal.${GoalState}` {
  return `agentDrawer.goal.${state}`
}

// The goal control in the drawer's head: a target to press when the
// conversation is working toward nothing, and a chip carrying the state
// when it has a goal. Either one opens the same dialog.
//
// Narrow, the chip is the mark alone in the state's colour and the state
// word moves into the tooltip: there is no room beside a title on a phone
// for "waiting for you · next look 10:42", and the mark's colour already
// says which of the three it is to anyone who has seen it once.
// What each kind of turn of the agent's own is called, and drawn with.
const CHECK_IN_LABEL = {
  goal: 'agentDrawer.goal.checkIn',
  goalNeedsYou: 'agentDrawer.goalNeedsYouTurn',
  background: 'agentDrawer.backgroundEnded',
  backgroundWork: 'agentDrawer.backgroundWorkEnded',
  schedule: 'agentDrawer.scheduleTurn',
  speakFirst: 'agentDrawer.speakFirstTurn',
  alert: 'agentDrawer.alertTurn',
  herdrQuestion: 'agentDrawer.herdrQuestionTurn',
  herdrSession: 'agentDrawer.herdrSessionTurn',
  restartResume: 'agentDrawer.restartResumeTurn',
  approved: 'agentDrawer.approvedLater',
  declined: 'agentDrawer.declinedLater',
} as const

function CheckInIcon({ origin }: { origin: CheckInOrigin }) {
  if (origin === 'background' || origin === 'herdrSession') return <TerminalIcon size={12} />
  if (origin === 'herdrQuestion') return <WarningIcon size={12} />
  if (origin === 'backgroundWork') return <ListIcon size={12} />
  if (origin === 'schedule') return <CalendarIcon size={12} />
  if (origin === 'speakFirst') return <SparkIcon size={12} />
  if (origin === 'restartResume') return <RefreshIcon size={12} />
  if (origin === 'alert') return <WarningIcon size={12} />
  if (origin === 'approved') return <CheckIcon size={12} />
  if (origin === 'declined') return <CloseIcon size={12} />
  return <TargetIcon size={12} />
}

// goalIdOf is the goal a "[goal needs you]" line names: the id right after
// the marker.
function goalIdOf(text: string): string {
  return text.slice(GOAL_NEEDS_YOU_MARKER.length).trim().split(/\s+/)[0] ?? ''
}

// herdrQuestionOf is the question a "[herdr question]" line names: the
// computer, which may hold spaces, then the pane and the fingerprint.
function herdrQuestionOf(text: string): { computer: string; paneId: string; questionFingerprint: string } | null {
  const words = (text.slice(HERDR_QUESTION_MARKER.length).split('\n')[0] ?? '').trim().split(/\s+/)
  if (words.length < 3) return null
  return {
    computer: words.slice(0, -2).join(' '),
    paneId: words[words.length - 2],
    questionFingerprint: words[words.length - 1],
  }
}

// herdrSummaryOf is a herdr question as the agent said it: the question's
// first line, and who asked it.
function herdrSummaryOf(text: string): { asked: string; who: string } {
  const lines = text.split('\n')
  const who = (lines[0] ?? '')
    .replace(/\*\*/g, '')
    .replace(/( \(.*\))? asks[^:]*:\s*$/, '')
    .trim()
  // The line that asks, where the form opens with something else (a path,
  // a heading); the first line otherwise.
  const quoted = lines.filter((each) => each.startsWith('> ')).map((each) => each.slice(2).trim())
  const asking = quoted.find((each) => each.includes('?')) ?? quoted[0] ?? ''
  const asked = asking.length > 160 ? `${asking.slice(0, 157)}…` : asking
  return { asked: asked || who, who: asked ? who : '' }
}

// questionFromSaid is a herdr question read back from the agent's words,
// for one asked before its question was kept whole beside it: the quoted
// lines are the question, the numbered ones its options.
function questionFromSaid(
  text: string,
  question: { computer: string; paneId: string; questionFingerprint: string },
): HerdrSession | undefined {
  const lines = text.split('\n')
  const options = lines
    .map((line) => /^(\d+)\. (.*)$/.exec(line.trim()))
    .filter((match): match is RegExpExecArray => match !== null)
    .map((match) => {
      const [label, ...rest] = match[2].split(': ')
      const optionLabel = label.trim()
      const herdrOptionKind: 'choice' | 'freeText' | 'chat' = optionLabel.startsWith('Chat about')
        ? 'chat'
        : optionLabel.startsWith('Type something') || optionLabel.startsWith('Tell Claude what to change')
          ? 'freeText'
          : 'choice'
      return { optionNumber: Number(match[1]), optionLabel, optionDescription: rest.join(': ').trim(), herdrOptionKind }
    })
  if (options.length === 0) return undefined
  const head = (lines[0] ?? '').replace(/\*\*/g, '')
  const codingAgentKind = head.startsWith('Codex') ? 'codex' : 'claude'
  return {
    computer: question.computer,
    paneId: question.paneId,
    paneName: '',
    codingAgentKind,
    codingSessionId: '',
    herdrSessionState: 'idle',
    herdrAgentStatus: '',
    paneTitle: '',
    workingDirectory: '',
    transcriptPath: '',
    isWatched: false,
    question: {
      questionFingerprint: question.questionFingerprint,
      herdrQuestionKind: 'question',
      questionText: lines
        .filter((line) => line.startsWith('> '))
        .map((line) => line.slice(2))
        .join('\n'),
      isMultipleChoice: text.includes('You may choose several.'),
      isFromTranscript: false,
      options,
    },
  }
}

// HERDR_QUESTION_STALE_MS is how old a question with no word of its answer
// is taken to be answered without asking its computer: a coding session
// does not wait a day on one question.
const HERDR_QUESTION_STALE_MS = 24 * 60 * 60 * 1000

// herdrNoteOf reads a herdr question's note: JSON, or, in a note written
// before notes carried more, the computer, the pane and the fingerprint.
interface HerdrNote {
  computer: string
  paneId: string
  questionFingerprint: string
  herdrSession?: HerdrSession
  answerText?: string
}

function herdrNoteOf(detail: string | undefined): HerdrNote | null {
  if (!detail) return null
  try {
    return JSON.parse(detail) as HerdrNote
  } catch {
    const words = detail.trim().split(/\s+/)
    if (words.length < 3) return null
    return {
      computer: words.slice(0, -2).join(' '),
      paneId: words[words.length - 2],
      questionFingerprint: words[words.length - 1],
    }
  }
}

// askingLineOf is the line of a question that asks, where a form opens with
// something else (a path, a heading), cut to a line's length.
function askingLineOf(questionText: string): string {
  const lines = questionText
    .split('\n')
    .map((each) => each.trim())
    .filter(Boolean)
  const asking = lines.find((each) => each.includes('?')) ?? lines[0] ?? ''
  return asking.length > 160 ? `${asking.slice(0, 157)}…` : asking
}

// HerdrQuestionLine is a coding session's question, in one box: a head that
// says what it asks, what it was answered with once it was, and who asks;
// under it, the options. While it waits they can be pressed. Once it is
// answered, at the keyboard or here, the box folds to its head, which opens
// it again to the same options, frozen, with the chosen one marked. What it
// asked and how it went are read from the notes written beside it, so it is
// drawn as it stands the moment the transcript is, with no question asked
// of the computer for one that went.
function HerdrQuestionLine({
  drawn,
  text,
  at,
  question,
  asked,
  answered,
}: {
  drawn: React.ReactNode
  text: string
  at?: string
  question: { computer: string; paneId: string; questionFingerprint: string }
  asked?: HerdrSession
  answered: HerdrNote | null
}) {
  const { t } = useTranslation()
  const isStale = !!at && Date.now() - new Date(at).getTime() > HERDR_QUESTION_STALE_MS
  const [foldReason, setFoldReason] = useState<'answered' | 'unreachable' | null>(
    answered || isStale ? 'answered' : null,
  )
  const [answerText, setAnswerText] = useState(answered?.answerText ?? '')
  const [isOpen, setOpen] = useState(false)
  useEffect(() => {
    if (!answered) return
    setFoldReason('answered')
    if (answered.answerText) setAnswerText(answered.answerText)
  }, [answered])
  const summary = herdrSummaryOf(text)
  // Kept whole beside it, or read back from the agent's words for a
  // question asked before it was.
  const shown = asked?.question ? asked : questionFromSaid(text, question)
  const title = asked?.question ? askingLineOf(asked.question.questionText) : summary.asked
  const who = asked
    ? [codingAgentName(asked.codingAgentKind), paneNameOf(asked), question.computer].join(' · ')
    : summary.who
  // A question of more than a line (an approval's command, a plan) shows
  // the whole of it over its options; the head says the rest.
  const isWhole =
    !!asked?.question && asked.question.questionText.split('\n').filter((each: string) => each.trim()).length > 1
  const isFolded = foldReason !== null
  const head = (
    <>
      {isFolded ? <CheckIcon size={14} /> : <WarningIcon size={14} />}
      <span className="herdr-question-head-text">
        <span className="herdr-question-head-asked">
          {foldReason === 'unreachable' ? `${t('herdr.unreachable')} · ` : null}
          {title}
          {answerText ? <span className="herdr-question-head-answer"> → {answerText}</span> : null}
        </span>
        {who ? <span className="herdr-question-head-who">{who}</span> : null}
      </span>
      {isFolded ? isOpen ? <ChevronUpIcon size={14} /> : <ChevronDownIcon size={14} /> : null}
    </>
  )
  return (
    <div className="agent-line herdr-question-line">
      <div className={['herdr-question-box', isFolded ? 'folded' : 'waiting', isOpen ? 'open' : ''].join(' ')}>
        {isFolded ? (
          <button
            type="button"
            className="herdr-question-head"
            aria-expanded={isOpen}
            onClick={() => setOpen((before) => !before)}
          >
            {head}
          </button>
        ) : (
          <div className="herdr-question-head">{head}</div>
        )}
        {!isFolded ? (
          <div className="herdr-question-body">
            <HerdrQuestionCard
              {...question}
              initial={asked}
              isHeaderShown={false}
              isQuestionShown={isWhole}
              onAnswered={(answeredWith) => {
                if (answeredWith) setAnswerText(answeredWith)
                setFoldReason('answered')
              }}
              onUnreachable={() => setFoldReason('unreachable')}
            />
          </div>
        ) : null}
        {isFolded && isOpen ? (
          <div className="herdr-question-body">
            {shown?.question ? (
              <HerdrQuestionAnswer
                session={shown}
                question={shown.question}
                isQuestionShown={isWhole}
                answeredWith={answerText}
              />
            ) : (
              drawn
            )}
          </div>
        ) : null}
      </div>
    </div>
  )
}

// CheckInLine is one turn of the agent's own -- toward the goal, on
// hearing that a background command ended, or at a schedule's time -- as a
// line rather than a bubble; pressing it shows the words the turn was
// given, because a person watching the agent wants to know what it was told
// as much as what it did.
function CheckInLine({ at, text, origin }: { at?: string; text: string; origin: CheckInOrigin }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  return (
    <div className="agent-line checkin muted">
      <button
        type="button"
        className="agent-checkin-toggle"
        aria-expanded={open}
        onClick={() => setOpen((before) => !before)}
      >
        <CheckInIcon origin={origin} />
        {t(CHECK_IN_LABEL[origin])}
        {at ? ` · ${clockTime(at)}` : ''}
      </button>
      {origin === 'goalNeedsYou' && goalIdOf(text) ? (
        <Link className="button-quiet agent-checkin-goal" to={`/settings/agent/goals?goal=${goalIdOf(text)}`}>
          {t('agentDrawer.openGoal')}
        </Link>
      ) : null}
      {open ? <pre className="agent-checkin-prompt">{text}</pre> : null}
    </div>
  )
}

function GoalChip({ conversation }: { conversation: Conversation }) {
  const { t } = useTranslation()
  // One icon in the state's colour; the tooltip says the state and the
  // goal, and the goal's page says the rest.
  const state = goalStateOf(conversation)
  const label = `${t(goalStateKey(state))} · ${conversation.title || conversation.goal}`
  return (
    <Tooltip label={label}>
      <Link
        className={`icon-button agent-drawer-goal ${state}`}
        aria-label={label}
        to={`/settings/agent/goals?goal=${conversation.id}`}
      >
        <TargetIcon size={16} />
      </Link>
    </Tooltip>
  )
}

// BackgroundMark is the head's mark for the commands this conversation
// has running in the background: a terminal whose cursor blinks. It is
// there only while one runs; how one ended is said in the conversation.
function BackgroundMark({
  commands,
  isOpen,
  onToggle,
}: {
  commands: BackgroundCommand[]
  isOpen: boolean
  onToggle: () => void
}) {
  const { t } = useTranslation()
  return (
    <HeadMark
      label={t('agentDrawer.backgroundRunning', { count: commands.length })}
      className="agent-drawer-background running"
      isOpen={isOpen}
      onToggle={onToggle}
    >
      <TerminalIcon size={14} />
    </HeadMark>
  )
}

// A question or an approval the person has not answered, kept by the
// server beyond the turn that raised it, so that it is still a card to
// press after a reload, on another device, or an hour later.
interface OpenInteraction {
  runId: string
  callId: string
  interactionKind: 'question' | 'approval'
  toolName: string
  interactionText: string
  interactionChoices: string[]
  toolRisk: string
  createdAt: string
}

// readConversationSnapshot reads a conversation, or the main one when the
// conversation asked for is not there. The drawer reopens the conversation
// this browser last showed, and that one may belong to another account:
// an operator who switched to someone else's account, or signed in as
// someone else, asked for their own conversation as that person and was
// told "api: not found" twice. A conversation deleted elsewhere is the
// same case. The main one is always there.
export async function readConversationSnapshot(conversationId: string, signal: AbortSignal) {
  try {
    return await readConversationOnce(conversationId, signal)
  } catch (caught) {
    if (conversationId && isNotFound(caught) && !signal.aborted) {
      return await readConversationOnce('', signal)
    }
    throw caught
  }
}

async function readConversationOnce(conversationId: string, signal: AbortSignal) {
  const response = await graphql<{
    ReadAgentConversation: {
      conversation: Conversation
      actingAs?: string | null
      messages: StoredMessage[]
      total?: number
      todos: Todo[]
    }
    ListAgentInteractions: OpenInteraction[] | null
  }>(CONVERSATION, { conversationId: conversationId || undefined, first: 100 }, signal)
  return { ...response.ReadAgentConversation, interactions: response.ListAgentInteractions ?? [] }
}

// cardsOf draws the open cards after the transcript, keyed as the live
// events key them, so a card that is both still waiting in a running turn
// and read back is drawn once. A card's key is its call's with "-card"
// after it: the tool line for the same call has the call's own, and a
// card that shared it was taken for that line and never drawn.
// toolWords is a tool's name as words, as the iPhone shows it: "mail_draft" is
// "Mail draft", and a skill's or a server's tool is named by its own last part.
export function toolWords(tool: string): string {
  const words = (tool.split('__').pop() ?? tool).replace(/[_-]/g, ' ')
  return words.charAt(0).toUpperCase() + words.slice(1)
}

function cardsOf(interactions: OpenInteraction[]): Line[] {
  return interactions.map((interaction): Line => {
    const key = `${interaction.runId}-${interaction.callId}-card`
    if (interaction.interactionKind === 'question') {
      return {
        kind: 'question',
        key,
        runId: interaction.runId,
        callId: interaction.callId,
        question: interaction.interactionText,
        choices: interaction.interactionChoices,
        raisedAt: interaction.createdAt,
      }
    }
    return {
      kind: 'confirmation',
      key,
      runId: interaction.runId,
      callId: interaction.callId,
      tool: interaction.toolName,
      summary: interaction.interactionText,
      risk: interaction.toolRisk,
    }
  })
}

// standalone is the drawer as a page of its own, framed by the browser
// extension into another site: always open, filling its frame, and its
// close mark telling the framing page to hide it.
export function AgentDrawer({ standalone = false }: { standalone?: boolean } = {}) {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const location = useLocation()
  const navigate = useNavigate()
  const session = useSession()
  const ownUserId = session.userId ?? ''
  const ownName = session.name || session.username
  // Framed by the extension, the panel is the frame and is moved with it.
  const chatBox = usePlacement(!standalone)
  const [available, setAvailable] = useState(false)
  // Whether there are chats to open without an agent, and so a drawer to
  // open them in. A person whose agent is off is still among people.
  const [canChat, setCanChat] = useState(false)
  const isReachable = available || canChat
  // How many messages wait in the chats that are not muted: the number on
  // the button.
  const [unreadChatCount, setUnreadChatCount] = useState(0)
  // Who is typing in the chat on screen, by person, until when.
  // An agent that is thinking is told apart from a person typing.
  const [typing, setTyping] = useState<
    Record<string, { name: string; ownerName?: string; until: number; isAgent?: boolean }>
  >({})
  // The dialog open over the drawer for a chat, if any.
  const [chatDialog, setChatDialog] = useState<'direct' | 'group' | 'add' | 'members' | 'rename' | 'leave' | null>(null)
  // The message being rewritten in place, and the one asked about deleting.
  const [editing, setEditing] = useState<{ messageId: string; text: string } | null>(null)
  const [deletingMessage, setDeletingMessage] = useState<ChatLine | null>(null)
  const lastTypingTold = useRef(0)
  // The @ being typed in the box, which of the agents it offers is lit, and
  // the agents chosen from it for the message being written.
  const [mention, setMention] = useState<Mention | null>(null)
  const [mentionIndex, setMentionIndex] = useState(0)
  const [chosenAgents, setChosenAgents] = useState<AddressableAgent[]>([])
  // The newest message told to the server as read, by conversation, so that
  // a chat open is told once for each message and not on every render.
  const readThrough = useRef<Record<string, string>>({})
  const [agentName, setAgentName] = useState('')
  // What the box says before anybody types: the agent's own name where it
  // has one, because "your agent" is what a stranger calls it. Until the
  // name has arrived it says the general thing rather than flickering.
  const askPlaceholder = agentName.trim()
    ? t('agentDrawer.placeholderNamed', { name: agentName.trim() })
    : t('agentDrawer.placeholder')
  const [open, setOpen] = useState(() => standalone || remembered(OPEN_KEY) === '1')
  const [conversations, setConversations] = useState<Conversation[]>([])
  const [waitingByConversation, setWaitingByConversation] = useState<Record<string, number>>({})
  // Words typed into the picker find conversations by title, summary or
  // what was said; the list shows the matches while there are any.
  const [search, setSearch] = useState('')
  const [found, setFound] = useState<Conversation[] | null>(null)
  const [deleting, setDeleting] = useState<Conversation | null>(null)
  // Whether the transcript is scrolled to its end. New words keep it
  // there; a person who scrolled up to read is left where they are.
  const [atBottom, setAtBottom] = useState(true)
  const [initialConversationId] = useState(() => remembered(CONVERSATION_KEY))
  // The conversation as loaded, which the list does not always hold: a
  // run's transcript is opened from the agent page and is not in it.
  const [loaded, setLoaded] = useState<Conversation | null>(null)
  // With the agent off: whether the chats have been looked through yet. The
  // offer to turn the agent on is for a person who has no chat, and is not
  // drawn while that is still being found out.
  const [hasSearchedChats, setHasSearchedChats] = useState(false)
  // Whose conversation this is, when it is not the person's own: an
  // operator reading another person's agent speaks to it as them, and
  // must be told so every time the drawer is open on it.
  const [actingAs, setActingAs] = useState<string | null>(null)
  const [lines, setLines] = useState<Line[]>([])
  // The files behind the citations the agent's answers have made, by
  // citation, and which citations have already been asked about. Asked
  // once each: a transcript is read many times over and the answer does
  // not change, and a citation that resolves to nothing is still a
  // citation that resolves to nothing.
  const [citedFiles, setCitedFiles] = useState<Record<string, CitedFile[]>>({})
  const citationsAsked = useRef<Set<string>>(new Set())
  // Display name (lower case) to username, for the owners of shared pages; a
  // name two people share is not in it, because the link could open either.
  const [sharedOwners, setSharedOwners] = useState<Record<string, string>>({})
  const sharedOwnersAsked = useRef(false)
  // The messages behind the lines, oldest first, and how many the
  // conversation holds: a drawer opens on the newest hundred, and the
  // difference is what "earlier messages" fetches.
  const messages = useRef<StoredMessage[]>([])
  const [total, setTotal] = useState(0)
  const [loadingEarlier, setLoadingEarlier] = useState(false)
  // The agent's own task list for this conversation: its steps and how far
  // it has got, which the person reads and does not change.
  const [todos, setTodos] = useState<Todo[]>([])
  // The message whose picker of reactions is open, by its id; one at a time.
  const [pickingReactionFor, setPickingReactionFor] = useState<string | null>(null)
  const closeReactionPicker = useCallback(() => setPickingReactionFor(null), [])
  // A second tap on the same message, or its React button again, closes it.
  const toggleReactionPicker = (messageId: string | null) =>
    setPickingReactionFor((previous) => (previous === messageId ? null : messageId))
  const [draft, setDraft] = useState('')
  const [pending, setPending] = useState<File[]>([])
  const [references, setReferences] = useState<AgentReference[]>([])
  const [uploading, setUploading] = useState(false)
  const [dragging, setDragging] = useState(false)
  // The runs in flight for this conversation, oldest first: the first is
  // the one running, the rest wait behind it on the server.
  const [runs, setRuns] = useState<string[]>([])
  const [showingList, setShowingList] = useState(false)
  const [renaming, setRenaming] = useState<{ id: string; title: string } | null>(null)
  // Whether a new conversation is being started, so a second press does
  // not start a second one.
  const isStarting = useRef(false)
  // The conversations that have been put away, and whether the picker is
  // showing them. Read only when the section is opened: most of the time
  // nobody looks, and the list is the one the drawer opens to.
  const [archived, setArchived] = useState<Conversation[]>([])
  const [showingArchived, setShowingArchived] = useState(false)
  // Whether the bar saying what the agent is waiting for is still up. It
  // comes down when the person sends, because the answer is on its way,
  // and the next read puts it back if the agent is still waiting.
  const [showingGoalNote, setShowingGoalNote] = useState(true)
  const [{ showTools, showUsage, showWorkingNotes }] = useAgentPreferences()
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set())
  // The bubbles whose time is shown: a tap on a phone, where there is no
  // pointer to hover with.
  const [tab, setTab] = useState<{ attached: boolean; title?: string; url?: string } | null>(null)
  const [computers, setComputers] = useState<AttachedComputer[]>([])
  // The day's tokens against the budget, read with the rest and so kept
  // current as turns start and finish.
  const [budget, setBudget] = useState<Budget | null>(null)
  // The zone the agent counts its day in, which is the zone the budget
  // starts again in and not always the zone the reader is sitting in.
  const [agentZone, setAgentZone] = useState('')
  // How many turns this drawer has sent and not yet been handed the run
  // of: the feed's "asked" for one of those is the drawer's own words,
  // already on the page.
  const sending = useRef(0)
  const transcript = useRef<HTMLDivElement>(null)
  // The lines as of the latest render, for a timer that outlives it.
  const latestLines = useRef<Line[]>([])
  latestLines.current = lines
  // The transcript as state as well as a ref, so what watches it is set
  // up when the element appears rather than when the drawer is opened.
  // A drawer remembered open has no element on its first render — the
  // agent has not answered whether there is one to talk to — and an
  // effect keyed on "open" would then never run again.
  const [transcriptElement, setTranscriptElement] = useState<HTMLDivElement | null>(null)
  // Whether the transcript is to follow its end, as the watchers below
  // read it. Not the same thing as atBottom, which says where it is now
  // and draws the button back to the end: this says where it belongs,
  // and only a deliberate act changes it — a scroll the person made, a
  // conversation opened, something said, the button pressed.
  const sticking = useRef(true)
  // When the person last moved the transcript with their own hands: a
  // wheel, a finger, a key, the scrollbar. A transcript that leaves its
  // end without one of those did not leave it on purpose — a picture
  // grew above, a phone's keyboard closed under it — and is put back.
  const movedAt = useRef(0)
  const input = useRef<HTMLTextAreaElement>(null)
  const draftLoadedFor = useRef('')
  // A draft another page asked for before the conversation it belongs in was
  // read: put in the box, after what is kept, once it has been.
  const askedDraft = useRef('')
  // The conversation whose draft is sent as soon as it is open and its
  // draft read back: an idea or a goal started from a page, or from the
  // suggestions here.
  const sendWhenOpen = useRef('')
  const filePicker = useRef<HTMLInputElement>(null)
  useFitToContent(input, draft, open)

  // Whether there is an agent to talk to at all. Asked when the page loads,
  // and again when a page says the person turned theirs on or off; the
  // button stays hidden otherwise, which is most servers.
  useEffect(() => {
    let cancelled = false
    const check = () => {
      graphql<{
        ReadAgent: { agent?: { enabled: boolean; name: string } | null; allowed: { enabled: boolean; ask: boolean } }
      }>(AGENT)
        .then((response) => {
          if (cancelled) return
          const view = response.ReadAgent
          const usable = Boolean(view.agent?.enabled && view.allowed.enabled && view.allowed.ask)
          setAvailable(usable)
          announceAgentAvailable(usable, view.agent?.name?.trim() ?? '')
          setAgentName(view.agent?.name ?? '')
          if (!usable) void hasChats().then((has) => !cancelled && setCanChat(has))
        })
        .catch(() => {
          if (cancelled) return
          setAvailable(false)
          void hasChats().then((has) => !cancelled && setCanChat(has))
        })
    }
    check()
    window.addEventListener(AGENT_TURNED_EVENT, check)
    return () => {
      cancelled = true
      window.removeEventListener(AGENT_TURNED_EVENT, check)
    }
  }, [])

  // The files behind whatever the agent's answers have cited, fetched as
  // the transcript settles. A line still streaming is left alone: its
  // last citation is half-written, and "work/mcx#3" on the way to
  // "work/mcx#31" would be asked about and then wrong.
  useEffect(() => {
    // Not while an answer is still arriving. This runs on every token, and
    // the walk below is over the whole transcript; the last line is the
    // only one that ever streams, so waiting for it costs one look.
    const last = lines[lines.length - 1]
    if (last && last.kind === 'assistant' && last.streaming) return
    const wanted: string[] = []
    for (const line of lines) {
      if (line.kind !== 'assistant' || line.streaming) continue
      for (const citation of citationsIn(line.text)) {
        if (citationsAsked.current.has(citation) || wanted.includes(citation)) continue
        wanted.push(citation)
      }
    }
    if (wanted.length === 0) return
    for (const citation of wanted) citationsAsked.current.add(citation)
    let cancelled = false
    graphql<{ AgentCitedAttachments: { citation: string; files: CitedFile[] }[] }>(CITED, { citations: wanted })
      .then((response) => {
        if (cancelled) return
        const found: Record<string, CitedFile[]> = {}
        for (const row of response.AgentCitedAttachments ?? []) found[row.citation] = row.files
        if (Object.keys(found).length > 0) setCitedFiles((previous) => ({ ...previous, ...found }))
      })
      .catch(() => {
        // Evidence nobody can resolve is not worth a failure in the
        // transcript. The answer still reads; it is only the picture
        // under it that is missing.
      })
    return () => {
      cancelled = true
    }
  }, [lines])

  // Who the shared pages cited in the answers belong to, asked once, the first
  // time an answer cites one.
  useEffect(() => {
    if (sharedOwnersAsked.current) return
    if (!lines.some((line) => line.kind === 'assistant' && !line.streaming && hasSharedCitation(line.text))) return
    sharedOwnersAsked.current = true
    graphql<{ ListSharedAgentPages: { ownerUsername: string; ownerName: string }[] }>(SHARED_OWNERS)
      .then((response) => {
        const found: Record<string, string> = {}
        const shared = new Set<string>()
        for (const root of response.ListSharedAgentPages ?? []) {
          const name = root.ownerName.trim().toLowerCase()
          if (!name || shared.has(name)) continue
          if (found[name] !== undefined && found[name] !== root.ownerUsername) {
            delete found[name]
            shared.add(name)
            continue
          }
          found[name] = root.ownerUsername
        }
        setSharedOwners(found)
      })
      .catch(() => {
        // The words still read; they are only not a link.
      })
  }, [lines])

  const loadConversations = useCallback(async () => {
    const response = await graphql<{ ListAgentConversations: Conversation[] }>(CONVERSATIONS, { archived: false })
    setConversations(response.ListAgentConversations)
    setUnreadChatCount(totalUnread(response.ListAgentConversations))
    graphql<{ ReadAgentWaiting: { interactions: { conversationId: string }[] } }>(WAITING)
      .then((waiting) => {
        const counted: Record<string, number> = {}
        for (const interaction of waiting.ReadAgentWaiting.interactions) {
          counted[interaction.conversationId] = (counted[interaction.conversationId] ?? 0) + 1
        }
        setWaitingByConversation(counted)
      })
      .catch(() => {
        // The list reads without it; only the marks are missing.
      })
    return response.ListAgentConversations
  }, [])

  // The archived ones, asked for on their own. The main conversation comes
  // back in both lists — it is never archived — and is left out here so
  // that the section holds only what was put away.
  const loadArchived = useCallback(async () => {
    const response = await graphql<{ ListAgentConversations: Conversation[] }>(CONVERSATIONS, { archived: true })
    const put = response.ListAgentConversations.filter((conversation) => conversation.kind !== 'main')
    setArchived(put)
    return put
  }, [])

  const applyConversationSnapshot = useCallback(
    (snapshot: Awaited<ReturnType<typeof readConversationSnapshot>>) => {
      setLoaded(snapshot.conversation)
      setActingAs(snapshot.actingAs ?? null)
      setTyping({})
      setEditing(null)
      remember(CONVERSATION_KEY, snapshot.conversation.id)
      messages.current = snapshot.messages
      setTotal(snapshot.total ?? snapshot.messages.length)
      setLines([
        ...linesOf(snapshot.messages, t, snapshot.conversation.kind === 'chat'),
        ...cardsOf(snapshot.interactions),
      ])
      setShowingGoalNote(true)
      // This read is how a step the agent finished during its turn
      // reaches the list.
      setTodos(snapshot.todos ?? [])
      if (draftLoadedFor.current !== snapshot.conversation.id) {
        const kept = remembered(draftKey(snapshot.conversation.id))
        setDraft(withAskedDraft(kept, askedDraft.current))
        askedDraft.current = ''
        draftLoadedFor.current = snapshot.conversation.id
      }
    },
    [t],
  )

  const {
    conversationId,
    selectedConversation: conversationRef,
    currentRead: loading,
    isLoading: isReadingConversation,
    readConversation: requestConversation,
    adoptConversation,
  } = useAgentConversation(initialConversationId, readConversationSnapshot, applyConversationSnapshot)

  // A reply names a message of the conversation it was swiped in: moving
  // to another drops it, or the next turn there would answer a message it
  // does not have.
  useEffect(() => {
    setReferences((previous) =>
      previous.some(isReplyReference) ? previous.filter((reference) => !isReplyReference(reference)) : previous,
    )
  }, [conversationId])

  // What this conversation left running on the person's computers, kept up
  // while the drawer is open. The main conversation is named by an empty
  // id until it has been read, and the list wants its real one.
  const backgroundConversationId = conversationId || loaded?.id || ''
  // Where a picture in an answer is fetched from: the server, which fetches
  // only an address a tool showed the agent in this conversation. Framed
  // into another site the drawer has no cookie to send with a picture, so
  // there it stays a link.
  const pictureSource = useCallback(
    (address: string) =>
      framedDrawer || !backgroundConversationId
        ? null
        : `/api/v1/agent/conversations/${encodeURIComponent(backgroundConversationId)}/picture?url=${encodeURIComponent(address)}`,
    [backgroundConversationId],
  )
  const { commands: runningCommands, reload: reloadBackground } = useBackgroundCommands(
    backgroundConversationId,
    open && available && backgroundConversationId !== '' && loaded?.kind !== 'chat',
  )
  // Which of the head's dropdowns is open, if any. One at a time, and none
  // while the list of conversations is.
  const [headMenu, setHeadMenu] = useState<HeadMenuName | null>(null)
  const toggleHeadMenu = (name: HeadMenuName) => {
    setShowingList(false)
    setHeadMenu((before) => (before === name ? null : name))
  }

  const readConversation = useCallback(
    (conversationId: string, isSelection = false) => {
      if (isSelection) {
        sticking.current = true
        setAtBottom(true)
      }
      return requestConversation(conversationId, isSelection)
    },
    [requestConversation],
  )

  useEffect(() => {
    if (!open || !isReachable) return
    if (!available) {
      setHasSearchedChats(false)
      void openChatsOnly()
        .catch((caught) => {
          if (!isAgentOffRefusal(caught)) toast.failed(caught instanceof Error ? caught.message : String(caught))
        })
        .finally(() => setHasSearchedChats(true))
      return
    }
    void loadConversations().catch((caught) => toast.failed(caught instanceof Error ? caught.message : String(caught)))
    void readConversation(conversationRef.current, true).catch((caught) =>
      toast.failed(caught instanceof Error ? caught.message : String(caught)),
    )
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, available, isReachable])

  // Which chats have something new in them, asked while the drawer is shut
  // for the number on its button and while it is open for the counts in its
  // list. Not while the tab is hidden: nobody is looking at a number.
  useEffect(() => {
    if (!isReachable) return
    let stopped = false
    const read = () => {
      if (stopped || document.hidden) return
      if (open) {
        void loadConversations().catch(() => undefined)
        return
      }
      graphql<{ ListAgentConversations: { chat?: { unreadCount: number; isMuted: boolean } | null }[] }>(CHAT_UNREAD)
        .then((response) => {
          if (!stopped) setUnreadChatCount(totalUnread(response.ListAgentConversations))
        })
        .catch(() => undefined)
    }
    // The first look is a moment after the page loads, not with it: the
    // conversations are read by the open drawer already, and a button that
    // cannot have a number yet is not worth a request of its own.
    const first = window.setTimeout(read, open ? UNREAD_EVERY : 0)
    const every = window.setInterval(read, UNREAD_EVERY)
    window.addEventListener('focus', read)
    document.addEventListener('visibilitychange', read)
    return () => {
      stopped = true
      window.clearTimeout(first)
      window.clearInterval(every)
      window.removeEventListener('focus', read)
      document.removeEventListener('visibilitychange', read)
    }
  }, [isReachable, open, loadConversations])

  // A chat on screen is read once the drawer is in front of the person and
  // its newest message has arrived: told to the server, which moves the
  // read mark the others see and clears the count in the list.
  const newestChatMessageId = useMemo(() => {
    for (let at = lines.length - 1; at >= 0; at--) {
      const line = lines[at]
      if (line.kind === 'chat' && line.messageId && !line.isPending) return line.messageId
    }
    return ''
  }, [lines])
  useEffect(() => {
    if (!open || loaded?.kind !== 'chat' || loaded.id !== conversationId || !newestChatMessageId) return
    const markRead = () => {
      if (document.visibilityState !== 'visible') return
      if ((readThrough.current[conversationId] ?? '') >= newestChatMessageId) return
      readThrough.current[conversationId] = newestChatMessageId
      graphql(MARK_CHAT_READ, { conversationId, throughMessageId: newestChatMessageId })
        .then(() => loadConversations())
        .catch(() => {
          delete readThrough.current[conversationId]
        })
    }
    markRead()
    document.addEventListener('visibilitychange', markRead)
    window.addEventListener('focus', markRead)
    return () => {
      document.removeEventListener('visibilitychange', markRead)
      window.removeEventListener('focus', markRead)
    }
  }, [open, loaded?.kind, loaded?.id, conversationId, newestChatMessageId, loadConversations])

  // A line saying someone is typing goes by itself a few seconds after the
  // last word of it.
  useEffect(() => {
    if (Object.keys(typing).length === 0) return
    const timer = window.setInterval(() => {
      setTyping((previous) => {
        const now = Date.now()
        const remaining = Object.fromEntries(Object.entries(previous).filter(([, person]) => person.until > now))
        return Object.keys(remaining).length === Object.keys(previous).length ? previous : remaining
      })
    }, 1000)
    return () => window.clearInterval(timer)
  }, [typing])

  useEffect(() => {
    if (!open) adoptConversation(conversationId)
  }, [open, conversationId, adoptConversation])

  // An agent chosen for a message is not carried to another conversation.
  useEffect(() => {
    setChosenAgents([])
    setMention(null)
  }, [conversationId])

  // The conversation's events while the drawer is open — every turn,
  // wherever it was started: here, a phone, a terminal, a chat app. The
  // turns in flight are replayed once the transcript has been read, and
  // a socket that drops comes back on its own, reading the transcript
  // again first so that the replay lands on a fresh one.
  useEffect(() => {
    // Only a conversation read already: the one remembered from before may
    // not be there, and following it would fail before the reading had
    // fallen back to the main one.
    if (!open || !isReachable || !conversationId || loaded?.id !== conversationId) return
    // With the agent off the only conversations are the person's chats; the
    // agent's own are not followed, and are not on screen to follow.
    if (!available && loaded?.kind !== 'chat') return
    const followed = conversationId
    let stopped = false
    const stop = subscribe<{ AgentConversationEvents: RunEvent }>(
      FEED,
      { conversationId: followed },
      (data) => {
        // Only while this is still the conversation on screen. The socket
        // guards this too; this guards the gap React leaves between the
        // effect being torn down and the next one settling, and it keeps
        // the `done` branch below from reading a transcript nobody is
        // looking at over the one they are.
        if (stopped || followed !== conversationRef.current) {
          return
        }
        const event = data.AgentConversationEvents
        // The words of an agent's answer in a chat come with the agent they
        // are the answer of; the run's own words, in any other conversation,
        // do not.
        if (CHAT_EVENTS.has(event.kind) || (event.kind === 'text' && event.authorAgentId)) {
          applyChatEvent(event)
          return
        }
        // In voice mode the answers are spoken as they are written.
        voiceSession.current?.follow(event)
        if (event.kind === 'asked') {
          asked(event)
          return
        }
        if (event.kind !== 'note' || event.note !== 'queued behind the turn before it') {
          // The first event of a turn that is running: its queued line
          // has served.
          setLines((previous) => previous.filter((line) => line.key !== `${event.runId}-queued`))
        }
        applyEvent(event)
        if (event.kind === 'done') {
          setRuns((previous) => previous.filter((candidate) => candidate !== event.runId))
          void loadConversations().catch(() => undefined)
          void readConversation(followed).catch(() => undefined)
        }
      },
      (error) => {
        if (!error || stopped) return
        toast.failed(error.message)
      },
      {
        beforeStart: async (reconnecting) => {
          // A socket that opens after the conversation moved on reads
          // nothing: this runs before the subscription's own check, so
          // without it a reconnection could put the transcript of the
          // conversation somebody left back on the screen.
          if (stopped || followed !== conversationRef.current) {
            return
          }
          if (!reconnecting && loading.current) {
            await loading.current
            return
          }
          setRuns([])
          await readConversation(followed)
        },
      },
    )
    return () => {
      stopped = true
      stop()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, isReachable, available, conversationId, loaded?.id])

  // The list is read again each time it is opened: a conversation started
  // from a terminal since is there too.
  useEffect(() => {
    if (!showingList) {
      setSearch('')
      setFound(null)
      setShowingArchived(false)
      return
    }
    void loadConversations().catch(() => undefined)
  }, [showingList, loadConversations])

  // The search, a moment after the typing stops.
  useEffect(() => {
    const words = search.trim()
    if (!words) {
      setFound(null)
      return
    }
    const timer = setTimeout(() => {
      graphql<{ ListAgentConversations: Conversation[] }>(CONVERSATIONS, { query: words })
        .then((response) => setFound(response.ListAgentConversations))
        .catch(() => setFound([]))
    }, 250)
    return () => clearTimeout(timer)
  }, [search])

  // The draft is the conversation's: kept while the person is away and
  // back when they return. Kept only once this conversation's draft has
  // been read back, or the empty box of a fresh page would overwrite what
  // was typed before the refresh.
  useEffect(() => {
    if (draftLoadedFor.current !== conversationId) return
    remember(draftKey(conversationId), draft)
  }, [draft, conversationId])

  // Escape closes a dropdown -- the conversations, or one of the head's --
  // wherever the focus is, not only from inside it: the one that opens a
  // dropdown is a button in the head, and the focus stays there. A
  // command's output in the background dropdown takes its Escape first,
  // going back to the list, and a field that uses Escape marks it used.
  useEffect(() => {
    if (!headMenu && !showingList) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented) return
      if (document.querySelector('.dialog-scrim, .select-list')) return
      event.preventDefault()
      setHeadMenu(null)
      setShowingList(false)
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [headMenu, showingList])

  // Escape toggles the drawer from anywhere on the page — unless a dialog,
  // a menu or a list is open, which Escape closes first.
  useEffect(() => {
    if (!isReachable || standalone) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented) return
      if (
        document.querySelector(
          '.dialog-scrim, .select-list, .agent-drawer-list, .menu[role="menu"], .menu[role="dialog"]',
        )
      )
        return
      event.preventDefault()
      toggle()
      if (!open) setTimeout(() => input.current?.focus(), 50)
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isReachable, open])

  // Another page asking for a conversation to be opened here: a run's
  // transcript from the agent page, or an idea or a goal started, whose
  // first message is written in as the conversation's draft so that it is
  // in the box when the conversation is drawn, and sent from there when
  // asked, by the same send the button uses.
  useEffect(() => {
    const listener = (event: Event) => {
      const detail = (event as CustomEvent<AgentOpenDetail>).detail
      if (!isReachable || !detail) return
      detail.handled = true
      // Asked to open with no conversation named: open on the one it shows,
      // with the draft, if there is one, put in its box after what is there.
      if (!detail.conversationId) {
        const asked = detail.draft
        if (asked) {
          // Read already: into the box at once. Not yet: when it is read.
          if (draftLoadedFor.current !== '') setDraft((previous) => withAskedDraft(previous, asked))
          else askedDraft.current = asked
        }
        setOpen(true)
        remember(OPEN_KEY, '1')
        setTimeout(() => input.current?.focus(), 50)
        return
      }
      if (detail.draft) {
        remember(draftKey(detail.conversationId), detail.draft)
        draftLoadedFor.current = ''
      }
      sendWhenOpen.current = detail.draft && detail.shouldSendDraft ? detail.conversationId : ''
      setOpen(true)
      remember(OPEN_KEY, '1')
      void switchTo(detail.conversationId)
      if (detail.draft) setTimeout(() => input.current?.focus(), 50)
    }
    window.addEventListener(AGENT_OPEN_EVENT, listener)
    return () => window.removeEventListener(AGENT_OPEN_EVENT, listener)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isReachable])

  // Another page asking for a new conversation: the agent's page of
  // conversations, whose button does what the picker's new chat does.
  useEffect(() => {
    const listener = (event: Event) => {
      const detail = (event as CustomEvent<AgentNewDetail>).detail
      if (!available || !detail) return
      detail.handled = true
      setOpen(true)
      remember(OPEN_KEY, '1')
      void startNew(detail.onStarted)
    }
    window.addEventListener(AGENT_NEW_EVENT, listener)
    return () => window.removeEventListener(AGENT_NEW_EVENT, listener)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [available])

  // Whether this tab is in front of the person, told to the server so that
  // the agent starts a conversation on its own only with somebody who is
  // there, and a chat is sent to their phone only when they are not looking
  // at that conversation, which is why the one on show is named: the drawer
  // closed, or still reading another conversation, names none. Not from the
  // extension's frame: the page behind it reports already. With the agent
  // off a person who chats is still reported.
  const presenceConversationId = open && loaded && (!conversationId || loaded.id === conversationId) ? loaded.id : ''
  useAgentPresence(isReachable && !standalone, presenceConversationId)

  // A turn the agent starts on its own in the main conversation -- its
  // introduction, a memory check, an idea, an alert -- opens the drawer on
  // it: a message nobody sees might as well not have been written. Followed
  // whenever the drawer is not already showing the main conversation,
  // which is when the drawer's own subscription would not hear it.
  const isShowingMain = open && loaded?.kind === 'main'
  useEffect(() => {
    if (!available || standalone || isShowingMain) return
    let stopped = false
    const stop = subscribe<{ AgentConversationEvents: RunEvent }>(
      FEED,
      { conversationId: '' },
      (data) => {
        const event = data.AgentConversationEvents
        const note = event.note ?? ''
        if (
          stopped ||
          event.kind !== 'asked' ||
          !(
            note.startsWith(SPEAK_FIRST_SURFACE) ||
            note === ALERT_SURFACE ||
            note === GOAL_NEEDS_YOU_SURFACE ||
            note === HERDR_QUESTION_SURFACE
          )
        )
          return
        void loadConversations()
          .then((listed) => {
            const main = listed.find((conversation) => conversation.kind === 'main')
            if (stopped || !main) return
            setOpen(true)
            remember(OPEN_KEY, '1')
            void switchTo(main.id)
          })
          .catch(() => undefined)
      },
      () => undefined,
    )
    return () => {
      stopped = true
      stop()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [available, standalone, isShowingMain])

  // What of the person's own is attached, and what today has cost. Asked
  // when the drawer opens and before each turn -- and then kept up, because
  // attaching a tab happens in the extension and detaching it happens in
  // another window: waiting for the next turn left the mark showing a tab
  // that had gone.
  useEffect(() => {
    if (!open || !available) return
    let stopped = false
    const read = () => {
      if (stopped || document.hidden) return
      graphql<{
        ReadAgentTab: { attached: boolean; title?: string; url?: string }
        ReadAgentComputers: { computers: AttachedComputer[] }
        ReadAgent: { budget: Budget | null; timezone: string }
      }>(TAB)
        .then((response) => {
          if (stopped) return
          setTab(response.ReadAgentTab)
          setComputers(response.ReadAgentComputers.computers)
          setBudget(response.ReadAgent.budget)
          setAgentZone(response.ReadAgent.timezone)
        })
        .catch(() => {
          if (stopped) return
          setTab(null)
          setComputers([])
          setBudget(null)
        })
    }
    read()
    const every = window.setInterval(read, DEVICES_EVERY)
    // A person who attached a tab in another window comes back to this one,
    // and should not wait out the interval to see it.
    window.addEventListener('focus', read)
    document.addEventListener('visibilitychange', read)
    return () => {
      stopped = true
      window.clearInterval(every)
      window.removeEventListener('focus', read)
      document.removeEventListener('visibilitychange', read)
    }
  }, [open, available, runs.length])

  // A line arriving, when the transcript is following its end.
  useEffect(() => {
    const element = transcript.current
    if (element && sticking.current) {
      element.scrollTop = element.scrollHeight
    }
  }, [lines])

  // A person at the end stays at the end while the transcript grows
  // under them. Most of what makes it grow is not a line arriving: a
  // picture that finished loading, a framed page saying how tall it is,
  // a document fetched and drawn, the box below growing with the words.
  // None of those is a scroll and none of them changes the lines, so
  // none of them is noticed unless it is watched for — which is why a
  // conversation would open at its end and then sit part way up it a
  // moment later.
  //
  // One thing is left alone: a tool line opened to look inside it. That
  // growth is the person's own doing and reading it is why they opened
  // it, so the end is not chased for a moment afterwards.
  useEffect(() => {
    const element = transcriptElement
    if (!element) return
    let frame = 0
    let opened = 0
    const pin = () => {
      if (!sticking.current || Date.now() - opened < 500) return
      // On the next frame, once what grew has been laid out, so the
      // height is the one the person is about to see.
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(() => {
        element.scrollTop = element.scrollHeight
      })
    }
    const noteOpened = (event: Event) => {
      const target = event.target
      if (target instanceof Element && target.closest('.agent-tool-toggle')) {
        // Opening a tool line is reading it, and reading is not
        // following: the words that arrive next would otherwise take the
        // person to the end a moment after they looked inside. Scrolling
        // back to the end starts the following again.
        opened = Date.now()
        sticking.current = false
      }
    }
    const noteMoved = () => {
      movedAt.current = Date.now()
    }
    const observers: { disconnect: () => void }[] = []
    if (typeof ResizeObserver !== 'undefined') {
      const sizes = new ResizeObserver(pin)
      sizes.observe(element)
      observers.push(sizes)
    }
    if (typeof MutationObserver !== 'undefined') {
      const changes = new MutationObserver(pin)
      changes.observe(element, {
        childList: true,
        subtree: true,
        characterData: true,
        attributes: true,
        attributeFilter: ['style', 'src', 'height', 'width'],
      })
      observers.push(changes)
    }
    // A picture, a video or a framed page finishing: load does not
    // bubble, so it is caught on the way down.
    element.addEventListener('load', pin, true)
    element.addEventListener('loadedmetadata', pin, true)
    element.addEventListener('click', noteOpened, true)
    for (const gesture of ['wheel', 'touchmove', 'keydown', 'pointerdown'] as const) {
      element.addEventListener(gesture, noteMoved, { capture: true, passive: true })
    }
    return () => {
      cancelAnimationFrame(frame)
      element.removeEventListener('load', pin, true)
      element.removeEventListener('loadedmetadata', pin, true)
      element.removeEventListener('click', noteOpened, true)
      for (const gesture of ['wheel', 'touchmove', 'keydown', 'pointerdown'] as const) {
        element.removeEventListener(gesture, noteMoved, true)
      }
      for (const observer of observers) {
        observer.disconnect()
      }
    }
  }, [transcriptElement])

  // A page pointing the agent at a thread: the drawer opens with a chip
  // for it, and the next turn carries it.
  useEffect(() => {
    const listener = (event: Event) => {
      const detail = (event as CustomEvent<AgentAskDetail>).detail
      if (!available || !detail) return
      detail.handled = true
      setReferences((previous) =>
        previous.some(
          (reference) =>
            (reference.itemId && reference.itemId === detail.reference.itemId) ||
            (reference.path && reference.path === detail.reference.path) ||
            (reference.financeTransactionId &&
              reference.financeTransactionId === detail.reference.financeTransactionId) ||
            (reference.recordingId && reference.recordingId === detail.reference.recordingId) ||
            (reference.photoId && reference.photoId === detail.reference.photoId),
        )
          ? previous
          : [...previous, detail.reference],
      )
      setOpen(true)
      remember(OPEN_KEY, '1')
      setTimeout(() => input.current?.focus(), 50)
    }
    window.addEventListener(AGENT_ASK_EVENT, listener)
    return () => window.removeEventListener(AGENT_ASK_EVENT, listener)
  }, [available])

  // What the person has open: what the page said, when it said, else what
  // the address says — the reader, a folder, the subscriptions, a page.
  // Told to the agent with every turn.
  const [told, setTold] = useState<AgentViewing | null>(agentViewing())
  useEffect(() => {
    const listener = () => setTold(agentViewing())
    window.addEventListener(VIEWING_EVENT, listener)
    return () => window.removeEventListener(VIEWING_EVENT, listener)
  }, [])
  const viewing = useMemo<AgentViewing>(() => {
    if (told) return told
    const parts = location.pathname.split('/').filter(Boolean)
    if (parts[0] === 'mailbox') {
      switch (parts[1]) {
        case 'compose':
          return { page: 'compose' }
        case 'subscriptions':
          return parts[2] ? { page: 'subscriptions', listKey: decodeURIComponent(parts[2]) } : { page: 'subscriptions' }
        case 'contacts':
        case 'priority':
          return { page: parts[1] }
        case 'starred':
          return parts[2] ? { itemId: parts[2], page: 'reader' } : { page: 'starred' }
        default:
          if (parts[1] && parts[2]) return { folderId: parts[1], itemId: parts[2], page: 'reader' }
          if (parts[1]) return { folderId: parts[1], page: 'folder' }
      }
    }
    return { page: location.pathname }
  }, [location.pathname, told])

  // What the last answer offered to reply with: only when it is the last
  // thing in the conversation, nothing is running, and no card is waiting.
  const suggestedReplies = useMemo(() => {
    if (runs.length > 0) return []
    const last = [...lines].reverse().find((line) => line.kind !== 'note')
    if (last?.kind !== 'assistant' || last.streaming) return []
    return suggestedRepliesOf(last.text).suggestions
  }, [lines, runs])

  // leaving is what a link out of the drawer does on the way: on a phone
  // the drawer is the whole screen, so the page it goes to would be
  // behind it, and going somewhere is leaving here.
  const leaving = () => {
    if (window.innerWidth <= 720) close()
  }

  // close puts the drawer away, wherever it is drawn.
  const close = () => {
    if (standalone) {
      window.parent.postMessage({ teanode: 'close' }, '*')
      return
    }
    setOpen(false)
    remember(OPEN_KEY, '0')
  }

  const toggle = () => {
    if (standalone) {
      window.parent.postMessage({ teanode: 'close' }, '*')
      return
    }
    setOpen((previous) => {
      remember(OPEN_KEY, previous ? '0' : '1')
      return !previous
    })
  }

  // Framed by the extension, the drawer tells the panel around it which
  // theme it wears, so the panel's bar wears the same.
  const drawerTheme = useResolvedTheme()
  useEffect(() => {
    if (standalone) window.parent.postMessage({ teanode: 'theme', theme: drawerTheme }, '*')
  }, [standalone, drawerTheme])

  // The browser extension, on this dashboard's own pages, opens this
  // drawer rather than a copy of it: it raises this event from its
  // content script.
  useEffect(() => {
    if (standalone) return
    const onAgentEvent = (event: Event) => {
      if ((event as CustomEvent<string>).detail === 'toggle') toggle()
    }
    window.addEventListener('teanode:agent', onAgentEvent)
    return () => window.removeEventListener('teanode:agent', onAgentEvent)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [standalone])

  // The conversation on screen now, for an answer that arrives after the
  // person has moved to another one.
  const shownConversationId = useRef(conversationId)
  shownConversationId.current = conversationId
  // open_page moves the dashboard to a page of it. Framed into another
  // site the drawer has no dashboard around it to move, and the tool is not
  // offered there.
  const showPage = useShowPage(() => leaving())
  // The location tool asks the browser the person is chatting in where it
  // is: the drawer that sent the turn, named by the id it sends turns
  // with, or for a turn no drawer sent the one they sent from last. The
  // browser asks the person the first time; what it says, or why it
  // cannot, goes back to the turn. A call is answered once, though the
  // feed may replay it.
  const drawerId = useRef(newDrawerId())
  const lastSent = useRef({ conversationId: '', at: 0 })
  const answeredLocationCalls = useRef(new Set<string>())
  const answerLocation = (event: RunEvent) => {
    const callId = event.callId
    if (!callId || answeredLocationCalls.current.has(callId)) return
    const isAsked = isAskedOfThisDrawer({
      askedDrawerId: event.drawerId,
      drawerId: drawerId.current,
      lastSentAt: lastSent.current.conversationId === conversationRef.current ? lastSent.current.at : 0,
      now: Date.now(),
      isVisible: document.visibilityState === 'visible',
    })
    if (!isAsked) return
    answeredLocationCalls.current.add(callId)
    void readBrowserLocation()
      .then((answer) =>
        graphql<{ AnswerAgentLocation: boolean }>(ANSWER_LOCATION, { runId: event.runId, callId, ...answer }),
      )
      .catch(() => undefined)
  }
  // applyReaction puts one reactor's emoji on a message, or takes it off
  // when the emoji is empty: in the messages kept for paging back, and on
  // the line drawn from it.
  const applyReaction = (
    messageId: string,
    reactorKind: string,
    emoji: string,
    reactedAt?: string,
    reactor?: { userId?: string | null; name?: string | null },
  ) => {
    messages.current = messages.current.map((message) =>
      message.id === messageId
        ? {
            ...message,
            reactions: withReaction(message.reactions ?? undefined, reactorKind, emoji, reactedAt, reactor),
          }
        : message,
    )
    setLines((previous) =>
      previous.map((line) =>
        (line.kind === 'user' || line.kind === 'assistant' || line.kind === 'chat') && line.messageId === messageId
          ? { ...line, reactions: withReaction(line.reactions, reactorKind, emoji, reactedAt, reactor) }
          : line,
      ),
    )
  }
  // patchStored changes a message kept for paging back, so that reading
  // earlier messages does not bring back what was edited or deleted.
  const patchStored = (messageId: string, patch: Partial<StoredMessage>) => {
    messages.current = messages.current.map((message) =>
      message.id === messageId ? { ...message, ...patch } : message,
    )
  }
  // applyChatEvent is what a chat's own events do: a message said, edited
  // or deleted, a member's read mark moved, a member typing. An event that
  // cannot be drawn as it came -- a message it did not carry -- is made up
  // for by reading the chat again.
  const applyChatEvent = (event: RunEvent) => {
    const message = event.message
    switch (event.kind) {
      case 'said': {
        if (!message || event.isMessageCut) {
          void readConversation(conversationRef.current).catch(() => undefined)
          return
        }
        if (!messages.current.some((stored) => stored.id === message.id)) {
          messages.current = [...messages.current, message]
          setTotal((previous) => previous + 1)
        }
        setLines((previous) => linesWithSaid(previous, message))
        // Whoever said it has finished writing: a person's typing line goes,
        // and so does an agent's "is thinking" once it has answered.
        const finished = message.authorKind === 'agent' ? message.authorAgentId : message.authorUserId
        if (finished) {
          setTyping((previous) => {
            if (!(finished in previous)) return previous
            const remaining = { ...previous }
            delete remaining[finished]
            return remaining
          })
        }
        // The list follows: this chat is now the newest, and the count of
        // another that was said in is only known by asking.
        void loadConversations().catch(() => undefined)
        return
      }
      case 'edited':
        if (!message || event.isMessageCut) {
          void readConversation(conversationRef.current).catch(() => undefined)
          return
        }
        patchStored(message.id, { content: message.content, editedAt: message.editedAt })
        setLines((previous) => linesWithEdit(previous, message.id, message.content, message.editedAt ?? null))
        return
      case 'deleted':
        if (!event.messageId) return
        // The words of a turn that was stopped or failed, drawn while they
        // came: no message was stored, so the line goes.
        if (isStreamingKey(event.messageId)) {
          const streamingKey = event.messageId
          setLines((previous) => linesWithoutStreaming(previous, streamingKey))
          return
        }
        patchStored(event.messageId, { content: '', isDeleted: true })
        setLines((previous) => linesWithDeleted(previous, event.messageId as string))
        return
      case 'read': {
        const readerId = event.userId
        const readMessageId = event.messageId
        if (!readerId || !readMessageId) return
        setLoaded((previous) =>
          previous?.chat ? { ...previous, chat: withMemberRead(previous.chat, readerId, readMessageId) } : previous,
        )
        return
      }
      case 'typing': {
        // An agent working on an answer is told by its agent and thought of
        // as thinking; a person by theirs, and typing.
        const typist = event.authorAgentId || event.userId
        if (!typist || (!event.authorAgentId && typist === ownUserId)) return
        setTyping((previous) => ({
          ...previous,
          [typist]: {
            name: event.name ?? '',
            ownerName: event.authorOwnerName ?? '',
            until: Date.now() + TYPING_SHOWN_FOR,
            isAgent: Boolean(event.authorAgentId),
          },
        }))
        return
      }
      case 'text':
        // The words of an agent's answer as they are written, told to
        // every member: drawn as a message that the stored one replaces.
        if (!event.authorAgentId) return
        {
          // The agent's color is its owner's, told by the members.
          const owner = loaded?.chat?.members.find(
            (member) => member.agentId && member.agentId === event.authorAgentId,
          )
          setLines((previous) =>
            linesWithStreamedText(previous, { ...event, authorAvatarColorIndex: owner?.avatarColorIndex }),
          )
        }
        return
      case 'members': {
        // Somebody was added, taken out or left, or changed what their
        // agent does here (who can be asked, who remembers the chat): the
        // members are read again, and the list, which shows who a chat is
        // with.
        void loadConversations().catch(() => undefined)
        const memberOf = conversationRef.current
        void graphql<{ ReadAgentConversation: { conversation: { id: string; chat: ChatSummary | null } } }>(
          READ_CHAT_MEMBERS,
          { conversationId: memberOf },
        )
          .then((response) => {
            const read = response.ReadAgentConversation.conversation
            if (memberOf !== conversationRef.current || !read.chat) return
            setLoaded((previous) => (previous?.id === read.id ? { ...previous, chat: read.chat } : previous))
          })
          .catch(() => undefined)
        return
      }
      default:
        return
    }
  }
  const applyEvent = (event: RunEvent) => {
    // What an event does beyond the transcript happens here, once: the
    // updater below may run twice under StrictMode.
    //
    // A new conversation is titled after its first turn has ended: the
    // head and the list take the title as soon as it is written.
    if (event.kind === 'titled') {
      void loadConversations()
      return
    }
    if (event.kind === 'navigate') {
      if (!standalone) showPage(event)
      return
    }
    if (event.kind === 'locate') {
      answerLocation(event)
      return
    }
    // A reaction changed, here or anywhere else the conversation is open:
    // the badge on the message follows it. A message not drawn yet with
    // its id, one still arriving, has it when the transcript is read.
    if (event.kind === 'reacted') {
      if (event.messageId && event.reactorKind) {
        applyReaction(event.messageId, event.reactorKind, event.emoji ?? '', event.at, {
          userId: event.reactorUserId,
          name: event.reactorName,
        })
      }
      return
    }
    //
    // A shell call may have left something running, or stopped it: the
    // head's mark says so now rather than at the next poll.
    if (event.kind === 'tool_result' && event.tool === 'shell') {
      void reloadBackground(true)
    }
    if (
      event.kind === 'tool_result' &&
      event.tool &&
      MAIL_TOOLS.has(event.tool) &&
      !(event.text ?? '').startsWith('{"error"')
    ) {
      announceMailChanged()
    }
    if (
      event.kind === 'tool_result' &&
      event.tool === 'todo' &&
      conversationId &&
      !(event.text ?? '').startsWith('{"error"')
    ) {
      const readFor = conversationId
      void graphql<{ ReadAgentConversation: { todos: Todo[] } }>(CONVERSATION_TODOS, { conversationId: readFor })
        .then((answer) => {
          if (readFor === shownConversationId.current) setTodos(answer.ReadAgentConversation.todos ?? [])
        })
        .catch(() => undefined)
    }
    if (event.kind === 'error') {
      toast.failed(event.error ?? t('agentDrawer.failed'))
    }
    setLines((previous) => {
      const next = [...previous]
      const last = next[next.length - 1]
      switch (event.kind) {
        case 'text':
          if (last && last.kind === 'assistant' && last.streaming) {
            next[next.length - 1] = { ...last, text: last.text + (event.text ?? '') }
          } else {
            next.push({
              kind: 'assistant',
              key: `${event.runId}-${event.sequence}`,
              text: event.text ?? '',
              at: event.at,
              streaming: true,
            })
          }
          return next
        case 'message':
          // In a chat the answer is the chat's message, which arrives as its
          // own event and replaces the words drawn as they were written.
          if (loaded?.kind === 'chat') return next
          if (last && last.kind === 'assistant' && last.streaming) {
            next[next.length - 1] = { ...last, text: event.text ?? last.text, streaming: false }
          } else if (event.text?.trim()) {
            next.push({ kind: 'assistant', key: `${event.runId}-${event.sequence}`, text: event.text, at: event.at })
          }
          return next
        case 'tool_call':
          next.push({
            kind: 'tool',
            key: `${event.runId}-${event.callId}`,
            tool: event.tool ?? '',
            note: '',
            done: false,
            arguments: event.arguments,
            at: event.at,
          })
          return next
        case 'tool_result': {
          const index = next.findIndex((line) => line.kind === 'tool' && line.key === `${event.runId}-${event.callId}`)
          if (index >= 0) {
            const line = next[index]
            if (line.kind === 'tool') next[index] = { ...line, done: true, note: event.note ?? '', result: event.text }
          }
          // A card's call has an answer: the card is settled, whoever
          // settled it. The events of a recent run are replayed when the
          // drawer loads, and the card they raise would otherwise come back
          // open after it was answered, here or on another device.
          const cardIndex = next.findIndex((line) => line.key === `${event.runId}-${event.callId}-card`)
          if (cardIndex >= 0) {
            const card = next[cardIndex]
            if (card.kind === 'question' && !card.answered) {
              next[cardIndex] = { ...card, answered: answerOf(event.text) }
            } else if (card.kind === 'confirmation' && !card.resolved) {
              next[cardIndex] = { ...card, resolved: isDeclined(event.text) ? 'declined' : 'approved' }
            }
          }
          return next
        }
        case 'confirmation':
          if (next.some((line) => line.key === `${event.runId}-${event.callId}-card`)) return next
          next.push({
            kind: 'confirmation',
            key: `${event.runId}-${event.callId}-card`,
            runId: event.runId,
            callId: event.callId ?? '',
            tool: event.tool ?? '',
            summary: event.note ?? '',
            risk: event.risk ?? '',
          })
          return next
        case 'question':
          // Read back with the conversation already, as a kept card.
          if (next.some((line) => line.key === `${event.runId}-${event.callId}-card`)) return next
          next.push({
            kind: 'question',
            key: `${event.runId}-${event.callId}-card`,
            runId: event.runId,
            callId: event.callId ?? '',
            question: event.note ?? '',
            choices: (event.text ?? '').split('\n').filter(Boolean),
            raisedAt: event.at,
          })
          return next
        case 'note': {
          // A queued turn says so once, and the line goes when it starts.
          const queuedKey = `${event.runId}-queued`
          if (event.noteKind === 'queued' || event.note === 'queued behind the turn before it') {
            next.push({ kind: 'note', key: queuedKey, text: t('agentDrawer.note.queued') })
            return next
          }
          // A compaction says so while the note is written, and the line
          // becomes the finished one, which opens to the note.
          const compactingKey = `${event.runId}-compacting`
          const withoutQueued = next.filter((line) => line.key !== queuedKey && line.key !== compactingKey)
          if (event.noteKind === 'compacting') {
            withoutQueued.push({
              kind: 'note',
              key: compactingKey,
              text: t('agentDrawer.note.compacting'),
              isPending: true,
            })
            return withoutQueued
          }
          // A coding session's question that went comes outside any run:
          // drawn by nobody, read by the question's line, which folds with
          // what it was answered with. The same line the transcript reads
          // back, since its kind is not one NOTE_WORDS has words for.
          if (event.noteKind === HERDR_ANSWERED_NOTE) {
            withoutQueued.push({
              kind: 'note',
              key: `herdr-answered-${event.at}`,
              text: '',
              noteKind: HERDR_ANSWERED_NOTE,
              detail: event.noteDetail,
            })
            return withoutQueued
          }
          if (event.noteKind === SKILL_SHARE_CALL_WAITING_NOTE) {
            withoutQueued.push({
              kind: 'note',
              key: `skill-share-call-waiting-${event.noteDetail}`,
              text: '',
              noteKind: SKILL_SHARE_CALL_WAITING_NOTE,
              detail: event.noteDetail,
            })
            return withoutQueued
          }
          withoutQueued.push({
            kind: 'note',
            key: `${event.runId}-${event.sequence}`,
            text: noteWords(t, event.noteKind ?? '', event.noteDetail ?? '', event.note ?? ''),
            noteKind: resolvedNoteKind(event.noteKind ?? '', event.note ?? ''),
            detail: event.noteKind === 'compacted' ? event.noteDetail : undefined,
          })
          return withoutQueued
        }
        case 'error':
          return next
        default:
          return next
      }
    })
  }

  // A turn begins: the feed says what was said and where from. The
  // drawer's own words are on the page already. Anyone else's — a phone,
  // a chat app, a terminal — go on it now; and a turn replayed after the
  // transcript was read is already there from its words on, which the
  // events draw again.
  const asked = (event: RunEvent) => {
    setRuns((previous) => (previous.includes(event.runId) ? previous : [...previous, event.runId]))
    // In a chat what was said is a message of the chat already, drawn from
    // its own event; the turn it started is only to be followed.
    if (event.note === 'chat') return
    if (sending.current > 0 && (event.note === surface() || event.note === 'voice')) return
    // A turn of the agent's own, arriving live: the line, not the bubble,
    // and nothing here to have said it twice.
    const origin = checkInOriginOf(event.text ?? '')
    if (origin) {
      // Read already with the transcript -- an approval given after a turn
      // ended reads the conversation again as the new turn starts -- it is
      // the same line, and is drawn once, as a person's words are below.
      // Only near the end: a weekly schedule says the same words every week,
      // and last week's line is not this one.
      setLines((previous) => {
        const tail = Math.max(0, previous.length - 8)
        const kept = previous.filter(
          (line, index) =>
            line.key !== `${event.runId}-asked` &&
            !(index >= tail && line.kind === 'checkin' && line.text === (event.text ?? '')),
        )
        return [...kept, { kind: 'checkin', key: `${event.runId}-asked`, at: event.at, text: event.text ?? '', origin }]
      })
      // A background command has just ended: its row says so now rather
      // than at the next poll.
      if (origin === 'background') void reloadBackground(true)
      return
    }
    setLines((previous) => {
      let kept = previous
      for (let index = previous.length - 1; index >= 0; index--) {
        const line = previous[index]
        if (line.kind !== 'user') continue
        if (line.text === (event.text ?? '')) kept = previous.slice(0, index)
        break
      }
      const answering = answeringOf(event.text ?? '')
      return [
        ...kept,
        {
          kind: 'user',
          key: `${event.runId}-asked`,
          text: answering ? answering.answer : (event.text ?? ''),
          inReplyTo: answering?.question || undefined,
          at: event.at,
        },
      ]
    })
  }

  // The run a turn sent from here got: followed through the feed like
  // any other, and noted so that the stop button knows it.
  const follow = (id: string) => {
    setRuns((previous) => (previous.includes(id) ? previous : [...previous, id]))
  }

  const surface = () => (standalone ? 'extension' : window.innerWidth < 720 ? 'phone' : 'drawer')

  const addFiles = (files: FileList | File[] | null | undefined) => {
    if (!files) return
    const added = Array.from(files).filter((file) => file.size > 0 || file.name)
    if (added.length === 0) return
    setPending((previous) => [...previous, ...added])
  }

  // send sends what is in the box, or a suggested reply clicked above it,
  // which leaves the box, its files and its references as they were.
  // send sends what was typed, or the words given: a suggested reply, or
  // what the person said aloud, which goes as the voice surface.
  const send = async (
    suggestedReply?: string,
    options?: { isSpoken?: boolean; interruptedAnswer?: InterruptedAnswer },
  ) => {
    const isSuggested = suggestedReply !== undefined
    const message = (isSuggested ? suggestedReply : draft).trim()
    const files = isSuggested ? [] : pending
    const pointed = isSuggested ? [] : references
    if ((!message && files.length === 0) || uploading || loading.current) return
    // A question card waiting for an answer takes what is typed here as
    // its answer. Sent as a turn of its own, it queued behind the turn
    // that was waiting for it, and neither moved until the card timed out.
    // Only a card that is fresh: raised by a turn still running, or in
    // the last hour. One ignored for days would otherwise answer itself
    // with whatever was typed next. And only if it was taken: what a card
    // could not take goes out as the message it was.
    const waiting = [...lines].reverse().find((line) => line.kind === 'question' && !line.answered)
    const isFresh =
      waiting?.kind === 'question' &&
      (runs.includes(waiting.runId) ||
        (waiting.raisedAt !== undefined && Date.now() - new Date(waiting.raisedAt).getTime() < CARD_FRESH_MS))
    if (waiting && waiting.kind === 'question' && isFresh && message && files.length === 0 && pointed.length === 0) {
      if (await answer(waiting, message, { isQuiet: true })) {
        setDraft('')
        remember(draftKey(conversationId), '')
        return
      }
    }
    const sendingConversationId = conversationRef.current
    // In a chat, which agents the message is for: those chosen from the @
    // whose names are still in it, and the agent of a message it replies to.
    const repliedTo = pointed.find(isReplyReference)
    const repliedToLine = repliedTo
      ? (latestLines.current.find((line) => line.kind === 'chat' && line.messageId === repliedTo.agentMessageId) as
          ChatLine | undefined)
      : undefined
    const addressedAgentIds =
      loaded?.kind === 'chat' && !isSuggested
        ? addressedIn(
            message,
            chosenAgents,
            repliedToLine?.authorKind === 'agent' ? repliedToLine.authorAgentId : undefined,
          )
        : []
    if (!isSuggested) {
      setDraft('')
      remember(draftKey(conversationId), '')
      setPending([])
      setReferences([])
      setChosenAgents([])
      setMention(null)
    }
    // Saying something is meaning to see it: wherever the transcript was
    // being read, it goes back to its end for the turn that follows.
    sticking.current = true
    setAtBottom(true)
    // What the agent was waiting for has been answered, as far as this
    // person is concerned; the bar asking for it has served.
    setShowingGoalNote(false)
    const key = `user-${Date.now()}`
    const pendingAttachments = files.map((file, index) => ({
      id: `pending-${index}`,
      name: file.name,
      contentType: file.type,
      size: file.size,
    }))
    // In a chat the line is the person's own message as everybody will
    // see it, until the chat says it was said.
    const isInChat = loaded?.kind === 'chat'
    setLines((previous) => [
      ...previous,
      isInChat
        ? {
            kind: 'chat',
            key,
            text: message,
            at: new Date().toISOString(),
            authorKind: 'person',
            authorUserId: ownUserId,
            authorName: ownName,
            authorOwnerName: '',
            authorAvatarUrl: session.avatarUrl ?? null,
            authorAvatarColorIndex: session.avatarColorIndex ?? null,
            authorAgentId: '',
            isMine: true,
            editedAt: null,
            isDeleted: false,
            isPending: true,
            references: pointed.length > 0 ? pointed : undefined,
            attachments: pendingAttachments,
          }
        : {
            kind: 'user',
            key,
            text: message,
            at: new Date().toISOString(),
            references: pointed.length > 0 ? pointed : undefined,
            attachments: pendingAttachments,
          },
    ])
    try {
      let attachmentIds: string[] = []
      let uploaded: Attachment[] = []
      if (files.length > 0) {
        setUploading(true)
        try {
          const result = (await uploadFiles('POST', '/api/v1/agent/attachments', files, () => undefined).promise) as {
            attachments: Attachment[]
          }
          uploaded = result.attachments
          attachmentIds = uploaded.map((attachment) => attachment.id)
        } finally {
          setUploading(false)
        }
        setLines((previous) =>
          previous.map((line) =>
            line.key === key && (line.kind === 'user' || line.kind === 'chat')
              ? { ...line, attachments: uploaded }
              : line,
          ),
        )
      }
      // What is said aloud into a running turn is answered aloud too.
      if (options?.isSpoken) voiceSession.current?.adopt(runs)
      sending.current += 1
      let response: { AskAgent: { runId: string; conversationId: string } }
      try {
        response = await graphql<{ AskAgent: { runId: string; conversationId: string } }>(ASK, {
          conversationId: conversationId || undefined,
          message: message || (files.length > 0 ? t('agentDrawer.filesOnly') : ''),
          viewing,
          surface: options?.isSpoken ? 'voice' : surface(),
          interruptedAnswer: options?.interruptedAnswer,
          attachmentIds: attachmentIds.length > 0 ? attachmentIds : undefined,
          references: pointed.length > 0 ? pointed : undefined,
          drawerId: drawerId.current,
          addressedAgentIds: addressedAgentIds.length > 0 ? addressedAgentIds : undefined,
        })
      } finally {
        sending.current -= 1
      }
      lastSent.current = { conversationId: response.AskAgent.conversationId, at: Date.now() }
      if (conversationRef.current !== sendingConversationId) return
      if (!conversationId) {
        const conversation = response.AskAgent.conversationId
        adoptConversation(conversation)
        remember(CONVERSATION_KEY, conversation)
      }
      // A message in a chat comes back as an event. Should it not, the chat
      // is read again so that nothing sent looks unsent for long. It has a
      // run to follow only when it asked the person's own agent.
      if (isInChat) {
        window.setTimeout(() => {
          if (
            conversationRef.current === sendingConversationId &&
            latestLines.current.some((line) => line.key === key)
          ) {
            void readConversation(sendingConversationId).catch(() => undefined)
          }
        }, CHAT_CONFIRMED_WITHIN)
      }
      if (!response.AskAgent.runId) return
      follow(response.AskAgent.runId)
    } catch (caught) {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
      // What was not sent goes back in the box, so nothing is lost and a
      // second press tries again; the line that said it was sent goes.
      if (!isSuggested && conversationRef.current === sendingConversationId) {
        setLines((previous) => previous.filter((line) => line.key !== key))
        setDraft((current) => (current.trim() === '' ? message : current))
        setPending((current) => [...files, ...current])
        setReferences((current) => [...pointed, ...current])
      }
    }
  }

  // Voice: the microphone button, when the server offers it and this is the
  // dashboard's own drawer (the extension's page has a microphone of its
  // own to ask for). Each utterance the server finishes hearing is sent as
  // if typed; the caption shows the one being heard.
  const [voiceSampleRate, setVoiceSampleRate] = useState(0)
  const [voiceState, setVoiceState] = useState<'off' | 'starting' | 'listening'>('off')
  const [voiceCaption, setVoiceCaption] = useState('')
  const [isVoiceHearing, setVoiceHearing] = useState(false)
  const [isVoiceSpeaking, setVoiceSpeaking] = useState(false)
  // Whether the agent hears the person: muted for a word with somebody
  // else, for this call only.
  const [isMicrophoneMuted, setMicrophoneMuted] = useState(false)
  const voiceSession = useRef<VoiceSession | null>(null)
  const voiceLevel = useCallback(() => voiceSession.current?.level() ?? 0, [])
  const sendLatest = useRef(send)
  sendLatest.current = send
  useEffect(() => {
    if (standalone || framedDrawer) return
    let cancelled = false
    graphql<{ ReadAgentVoice: { isVoiceAvailable: boolean; sampleRate: number } }>(READ_VOICE)
      .then((response) => {
        if (!cancelled && response.ReadAgentVoice.isVoiceAvailable)
          setVoiceSampleRate(response.ReadAgentVoice.sampleRate)
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [standalone])
  const toggleVoice = () => {
    if (voiceSession.current) {
      voiceSession.current.stop()
      return
    }
    setVoiceState('starting')
    const session = new VoiceSession(
      {
        onListening: () => setVoiceState('listening'),
        onHearing: setVoiceHearing,
        onCaption: setVoiceCaption,
        onTranscript: (transcriptText, interruptedAnswer) =>
          void sendLatest.current(transcriptText, { isSpoken: true, interruptedAnswer }),
        onSpeaking: setVoiceSpeaking,
        onProblem: (problemText) => toast.failed(problemText),
        onEnded: () => {
          if (voiceSession.current === session) voiceSession.current = null
          setVoiceState('off')
          setVoiceCaption('')
          setMicrophoneMuted(false)
        },
      },
      {
        microphoneRefused: t('agentDrawer.voiceMicrophoneRefused'),
        notHeard: t('agentDrawer.voiceNotHeard'),
        connectionLost: t('agentDrawer.voiceConnectionLost'),
        confirmationNeeded: t('agentDrawer.voiceConfirmationNeeded'),
        answerNotSpoken: t('agentDrawer.voiceAnswerNotSpoken'),
      },
    )
    voiceSession.current = session
    void session.start(voiceSampleRate).catch((caught) => {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
      session.stop()
    })
  }
  const toggleMicrophoneMuted = () => {
    const isMuted = !isMicrophoneMuted
    setMicrophoneMuted(isMuted)
    voiceSession.current?.setMicrophoneMuted(isMuted)
  }
  // The call ends when the person moves to another conversation, but not
  // when a new one is given its id by the call's own first turn.
  const callConversationId = useRef(conversationId)
  useEffect(() => {
    const previous = callConversationId.current
    callConversationId.current = conversationId
    if (previous && previous !== conversationId) voiceSession.current?.stop()
  }, [conversationId])
  // And when the drawer closes, which hides the way to hang up: a
  // microphone left listening behind a closed drawer is not acceptable.
  useEffect(() => {
    if (!open) voiceSession.current?.stop()
  }, [open])
  useEffect(() => () => voiceSession.current?.stop(), [])

  // A draft asked to be sent is sent once its conversation is the one open
  // and its draft has been read back into the box: the same send as the
  // button, so the turn streams in and a failure keeps the words.
  useEffect(() => {
    const waiting = sendWhenOpen.current
    if (!waiting || isReadingConversation) return
    if (conversationId !== waiting) {
      // The read failed, or the person went elsewhere first: the draft
      // stays kept with its conversation, unsent.
      sendWhenOpen.current = ''
      return
    }
    if (draftLoadedFor.current !== waiting || draft.trim() === '') return
    sendWhenOpen.current = ''
    void send()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [conversationId, draft, isReadingConversation])

  const resolve = async (line: Extract<Line, { kind: 'confirmation' }>, approve: boolean) => {
    try {
      const response = await graphql<{ ResolveAgentConfirmation: boolean }>(RESOLVE, {
        runId: line.runId,
        callId: line.callId,
        approve,
      })
      if (!response.ResolveAgentConfirmation) {
        setLines((previous) => previous.filter((candidate) => candidate.key !== line.key))
        toast.failed(t('agentDrawer.questionGone'))
        return
      }
      setLines((previous) =>
        previous.map((candidate) =>
          candidate.key === line.key && candidate.kind === 'confirmation'
            ? { ...candidate, resolved: approve ? 'approved' : 'declined' }
            : candidate,
        ),
      )
    } catch (caught) {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
    }
  }

  // answer gives a card its answer and says whether it was taken. A card
  // nothing waits on any more -- answered on another device, or its wait
  // over with no kept copy -- is taken off the screen rather than left to
  // swallow what is typed next.
  const answer = async (
    line: Extract<Line, { kind: 'question' }>,
    text: string,
    { isQuiet = false }: { isQuiet?: boolean } = {},
  ): Promise<boolean> => {
    if (!text.trim()) return false
    const dropCard = () => setLines((previous) => previous.filter((candidate) => candidate.key !== line.key))
    try {
      const response = await graphql<{ AnswerAgentQuestion: boolean }>(ANSWER, {
        runId: line.runId,
        callId: line.callId,
        answer: text.trim(),
      })
      if (!response.AnswerAgentQuestion) {
        dropCard()
        if (!isQuiet) toast.failed(t('agentDrawer.questionGone'))
        return false
      }
      const shown = text.trim() === CHAT_ABOUT_IT ? t('agentDrawer.chattingInstead') : text.trim()
      setLines((previous) =>
        previous.map((candidate) =>
          candidate.key === line.key && candidate.kind === 'question' ? { ...candidate, answered: shown } : candidate,
        ),
      )
      return true
    } catch (caught) {
      dropCard()
      if (!isQuiet) toast.failed(caught instanceof Error ? caught.message : String(caught))
      return false
    }
  }

  // Stop ends the turn in flight; the ones queued behind it run next, and
  // another press stops the next.
  const stop = async () => {
    const running = runs[0]
    if (!running) return
    try {
      await graphql(STOP, { runId: running })
    } catch (caught) {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
    }
  }

  const switchTo = async (id: string) => {
    setShowingList(false)
    setRuns([])
    try {
      await readConversation(id, true)
    } catch (caught) {
      // A link kept somewhere else -- an idea's history, a goal, a run --
      // can outlive the conversation it names.
      if (isNotFound(caught)) toast.failed(t('agentDrawer.conversationDeleted'))
      else toast.failed(caught instanceof Error ? caught.message : String(caught))
    }
  }

  // A new conversation opens straight away, empty, with the box ready for
  // its first words. It used to ask for a goal first, which put a question
  // most people skipped between them and the conversation they asked for.
  const startNew = async (onStarted?: (conversationId: string) => void) => {
    if (isStarting.current) return
    isStarting.current = true
    try {
      const response = await graphql<{ StartAgentConversation: Conversation }>(START)
      await loadConversations()
      await switchTo(response.StartAgentConversation.id)
      onStarted?.(response.StartAgentConversation.id)
      setTimeout(() => input.current?.focus(), 50)
    } catch (caught) {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
    } finally {
      isStarting.current = false
    }
  }

  // showOffer empties the drawer of any conversation, for an agent that is
  // off: where the main conversation would be, the offer to turn it on.
  const showOffer = () => {
    setShowingList(false)
    adoptConversation('')
    setLoaded(null)
    setLines([])
    messages.current = []
    setTotal(0)
    remember(CONVERSATION_KEY, '')
  }

  // With the agent off the drawer opens on the chat the person had open,
  // else the newest, and with no chat on the offer to turn the agent on.
  const openChatsOnly = async () => {
    const listed = await loadConversations()
    const chats = listed.filter((conversation) => conversation.kind === 'chat')
    const wanted =
      chats.find((chat) => chat.id === conversationRef.current) ??
      [...chats].sort((first, second) => (second.lastAt ?? '').localeCompare(first.lastAt ?? ''))[0]
    if (!wanted) {
      showOffer()
      return
    }
    await readConversation(wanted.id, true)
  }

  // A chat just opened or made is shown, with the box ready.
  const openedChat = async (id: string) => {
    setChatDialog(null)
    try {
      await loadConversations()
      await switchTo(id)
      setTimeout(() => input.current?.focus(), 50)
    } catch (caught) {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
    }
  }

  // Of the chat on screen: its name, its sound, whether the person is in
  // it, and what is said in it. Each says what happened and leaves the list
  // and the head as the server now has them.
  const renameChat = async (title: string) => {
    await graphql(RENAME_CHAT, { conversationId, title })
    setChatDialog(null)
    setLoaded((previous) => (previous?.chat ? { ...previous, title, chat: { ...previous.chat, title } } : previous))
    await loadConversations()
  }
  const muteChat = async (mutedUntil: string | null) => {
    setHeadMenu(null)
    try {
      await graphql(MUTE_CHAT, { conversationId, mutedUntil })
      toast.done(mutedUntil ? t('chat.mutedDone') : t('chat.unmutedDone'))
      setLoaded((previous) =>
        previous?.chat
          ? { ...previous, chat: { ...previous.chat, isMuted: Boolean(mutedUntil), mutedUntil } }
          : previous,
      )
      await loadConversations()
    } catch (caught) {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
    }
  }
  const leaveChat = async () => {
    try {
      await graphql(LEAVE_CHAT, { conversationId })
      setChatDialog(null)
      toast.done(t('chat.left'))
      const remaining = (await loadConversations()).filter((conversation) => conversation.id !== conversationId)
      const next = available ? remaining.find((conversation) => conversation.kind === 'main') : remaining[0]
      if (next) await switchTo(next.id)
      else showOffer()
    } catch (caught) {
      setChatDialog(null)
      toast.failed(caught instanceof Error ? caught.message : String(caught))
    }
  }
  const saveEdit = async () => {
    if (!editing) return
    const { messageId, text } = editing
    const words = text.trim()
    setEditing(null)
    if (!words) return
    try {
      await graphql(EDIT_CHAT_MESSAGE, { messageId, message: words })
      const editedAt = new Date().toISOString()
      patchStored(messageId, { content: words, editedAt })
      setLines((previous) => linesWithEdit(previous, messageId, words, editedAt))
    } catch (caught) {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
    }
  }
  const deleteMessage = async (line: ChatLine) => {
    setDeletingMessage(null)
    const messageId = line.messageId
    if (!messageId) return
    try {
      await graphql(DELETE_CHAT_MESSAGE, { messageId })
      patchStored(messageId, { content: '', isDeleted: true })
      setLines((previous) => linesWithDeleted(previous, messageId))
    } catch (caught) {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
    }
  }
  // tellTyping lets the others know words are being written, at most every
  // few seconds however fast the keys.
  const tellTyping = (words: string) => {
    if (loaded?.kind !== 'chat' || !words.trim()) return
    const now = Date.now()
    if (now - lastTypingTold.current < TYPING_SENT_EVERY) return
    lastTypingTold.current = now
    void graphql(SET_CHAT_TYPING, { conversationId }).catch(() => undefined)
  }

  // Putting a conversation away takes it out of the picker and leaves
  // everything in it; the server refuses to archive the main one, which is
  // why the action is not offered there.
  const archiveConversation = async (conversation: Conversation, put: boolean) => {
    try {
      await graphql(UPDATE, { conversationId: conversation.id, archived: put })
      const remaining = await loadConversations()
      if (showingArchived) {
        await loadArchived()
      }
      toast.done(put ? t('agentDrawer.archivedDone') : t('agentDrawer.unarchivedDone'))
      if (put && conversation.id === conversationId) {
        // With the agent off there is no main conversation to fall back to:
        // the next chat, or the offer to turn the agent on.
        if (remaining[0] || available) await switchTo(remaining[0]?.id ?? '')
        else showOffer()
      }
    } catch (caught) {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
    }
  }

  // One conversation in the picker: the same row wherever it is listed —
  // among the live ones, among the archived, or among what a search found
  // — with the actions reading from the conversation itself, so an
  // archived one found by a search offers to be put back and not put away
  // again.
  const conversationRow = (conversation: Conversation) => (
    <div
      key={conversation.id}
      className={[
        'agent-drawer-list-row',
        conversation.id === conversationId ? 'active' : '',
        conversation.kind === 'main' ? 'main' : '',
        conversation.archivedAt ? 'archived' : '',
        conversation.kind === 'chat' && (conversation.chat?.unreadCount ?? 0) > 0 ? 'unread' : '',
      ]
        .filter(Boolean)
        .join(' ')}
    >
      {renaming?.id === conversation.id ? (
        <form
          className="agent-drawer-rename"
          onSubmit={(event) => {
            event.preventDefault()
            void rename()
          }}
        >
          <input
            autoFocus
            value={renaming.title}
            aria-label={t('agentDrawer.rename')}
            onChange={(event) => setRenaming({ id: conversation.id, title: event.target.value })}
            onBlur={() => void rename()}
            onKeyDown={(event) => {
              if (event.key === 'Escape') {
                event.preventDefault()
                setRenaming(null)
              }
            }}
          />
        </form>
      ) : (
        <Tooltip label={conversation.summary || ''}>
          <button
            type="button"
            className="agent-drawer-list-title"
            role="menuitem"
            onClick={() => void switchTo(conversation.id)}
          >
            <span className={['agent-drawer-list-name', conversation.chat ? 'chat' : ''].filter(Boolean).join(' ')}>
              {conversation.kind === 'main' ? (
                <>
                  <StarIcon size={12} /> {t('agentDrawer.main')}
                </>
              ) : conversation.chat ? (
                <>
                  <ChatAvatars
                    chat={conversation.chat}
                    ownUserId={ownUserId}
                    deleted={t('chat.deletedAccount')}
                    size={20}
                  />
                  <span className="chat-title">
                    {chatTitleOf(
                      conversation.chat,
                      ownUserId,
                      t('chat.deletedAccount'),
                      conversation.title || t('agentDrawer.untitled'),
                    )}
                  </span>
                  {conversation.chat.isMuted ? (
                    <span className="chat-muted" title={t('chat.muted')}>
                      <BellOffIcon size={12} />
                    </span>
                  ) : null}
                  <UnreadCount count={conversation.chat.unreadCount} />
                </>
              ) : (
                conversation.title || t('agentDrawer.untitled')
              )}
            </span>
            {/* When the conversation was last spoken in,
            which is what tells one of these apart from the
            next; a goal's note belongs in the dialog, not
            here in place of the time. The summary is the
            row's tooltip. */}
            <span className="agent-drawer-list-summary muted">
              {waitingByConversation[conversation.id] ? (
                <span className="agent-drawer-list-waiting">{t('agentDrawer.waitingForYou')} · </span>
              ) : null}
              <RelativeTime value={conversation.lastAt} />
            </span>
          </button>
        </Tooltip>
      )}
      {conversation.kind !== 'main' && renaming?.id !== conversation.id && (
        <span className="agent-drawer-list-actions">
          {/* A chat is named by its people and left rather than deleted, so
              of the actions only the one that puts it away is its own. */}
          {conversation.kind !== 'chat' && (
            <>
              <Tooltip label={t('agentDrawer.makeMain')}>
                <button
                  type="button"
                  className="icon-button"
                  aria-label={t('agentDrawer.makeMain')}
                  onClick={() => void makeMain(conversation.id)}
                >
                  <StarIcon size={14} />
                </button>
              </Tooltip>
              <Tooltip label={t('agentDrawer.rename')}>
                <button
                  type="button"
                  className="icon-button"
                  aria-label={t('agentDrawer.rename')}
                  onClick={() => setRenaming({ id: conversation.id, title: conversation.title })}
                >
                  <PencilIcon size={14} />
                </button>
              </Tooltip>
            </>
          )}
          {/* Not on the main conversation: the server refuses to archive
              it, and an action that is always refused is worse than none. */}
          <Tooltip label={conversation.archivedAt ? t('agentDrawer.unarchive') : t('agentDrawer.archive')}>
            <button
              type="button"
              className="icon-button"
              aria-label={`${
                conversation.chat
                  ? chatTitleOf(
                      conversation.chat,
                      ownUserId,
                      t('chat.deletedAccount'),
                      conversation.title || t('agentDrawer.untitled'),
                    )
                  : conversation.title || t('agentDrawer.untitled')
              }: ${conversation.archivedAt ? t('agentDrawer.unarchive') : t('agentDrawer.archive')}`}
              onClick={() => void archiveConversation(conversation, !conversation.archivedAt)}
            >
              {conversation.archivedAt ? <InboxIcon size={14} /> : <ArchiveIcon size={14} />}
            </button>
          </Tooltip>
          {conversation.kind !== 'chat' && (
            <Tooltip label={t('agentDrawer.delete')}>
              <button
                type="button"
                className="icon-button danger"
                aria-label={t('agentDrawer.delete')}
                onClick={() => setDeleting(conversation)}
              >
                <TrashIcon size={14} />
              </button>
            </Tooltip>
          )}
        </span>
      )}
    </div>
  )

  // The archived section is read when it is opened rather than with the
  // list: most of the time nobody asks for it.
  const toggleArchived = async () => {
    if (showingArchived) {
      setShowingArchived(false)
      return
    }
    setShowingArchived(true)
    try {
      await loadArchived()
    } catch (caught) {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
    }
  }

  // makeMain promotes a named conversation, or starts a fresh main one;
  // the main conversation until now stays, named.
  const makeMain = async (id: string) => {
    try {
      const response = await graphql<{ SetAgentMainConversation: Conversation }>(MAKE_MAIN, {
        conversationId: id || undefined,
      })
      await loadConversations()
      await switchTo(response.SetAgentMainConversation.id)
      toast.done(t('agentDrawer.madeMain'))
    } catch (caught) {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
    }
  }

  const remove = async (conversation: Conversation) => {
    try {
      await graphql(DELETE, { conversationId: conversation.id })
      setDeleting(null)
      const remaining = await loadConversations()
      if (conversation.id === conversationId) {
        await switchTo(remaining[0]?.id ?? '')
      }
    } catch (caught) {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
    }
  }

  const rename = async () => {
    if (!renaming) return
    const title = renaming.title.trim()
    setRenaming(null)
    if (!title) return
    try {
      await graphql(UPDATE, { conversationId: renaming.id, title })
      await loadConversations()
    } catch (caught) {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
    }
  }

  // The hundred before the oldest loaded, put in front of what is shown,
  // with the transcript held where the person was reading: the new
  // lines add height above, so the scroll moves down by exactly that.
  const loadEarlier = async () => {
    if (!conversationId || loadingEarlier) return
    setLoadingEarlier(true)
    const element = transcript.current
    const heightBefore = element?.scrollHeight ?? 0
    const loadedMessages = messages.current
    try {
      const response = await graphql<{
        ReadAgentConversation: { messages: StoredMessage[]; total?: number }
      }>(CONVERSATION, { conversationId, first: 100, offset: messages.current.length })
      if (conversationRef.current !== conversationId || messages.current !== loadedMessages) return
      const earlier = response.ReadAgentConversation.messages
      messages.current = [...earlier, ...messages.current]
      setTotal(response.ReadAgentConversation.total ?? messages.current.length)
      sticking.current = false
      // The cards still waiting are kept: they are not in the messages.
      setLines((previous) => [
        ...linesOf(messages.current, t, loaded?.kind === 'chat'),
        ...previous.filter(
          (line) => (line.kind === 'question' && !line.answered) || (line.kind === 'confirmation' && !line.resolved),
        ),
      ])
      requestAnimationFrame(() => {
        if (element && conversationRef.current === conversationId) {
          element.scrollTop += element.scrollHeight - heightBefore
        }
      })
    } catch (caught) {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
    } finally {
      setLoadingEarlier(false)
    }
  }

  const toggleExpanded = (key: string) => {
    setExpanded((previous) => {
      const next = new Set(previous)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  // A call a program made over MCP is filed as a run of one tool call, and
  // that call is all there is to read: what it was asked and what came
  // back, or why it failed. Hidden with the other tool calls, the run said
  // only that the call was made, so here it is always shown, and open.
  const isCallRecord = loaded?.id === conversationId && loaded?.jobKind === 'mcp'
  const isToolOpen = (key: string) => expanded.has(key) !== isCallRecord

  // Swiping a message to the left, or its Reply button, makes the next
  // turn an answer to it: the quote shows above the box, and the turn
  // carries it, so the agent knows what "that" is. One at a time.
  const shownText = (line: Line) =>
    line.kind === 'assistant'
      ? suggestedRepliesOf(withoutPartialMarker(line.text)).displayText.trim()
      : line.kind === 'user' || line.kind === 'chat'
        ? line.text.trim()
        : ''
  const replyTo = (line: Line) => {
    if (line.kind !== 'user' && line.kind !== 'assistant' && line.kind !== 'chat') return
    // An answer still being written has no message to reply to yet.
    if (line.kind === 'chat' && !line.messageId) return
    const quotedText = shownText(line).slice(0, REPLY_QUOTE_CHARACTERS)
    if (!quotedText) return
    // In a chat a person's own words are the user's, and everybody else's
    // are what an answer is in a conversation with an agent.
    const quotedRole = line.kind === 'chat' ? (isOwnWords(line) ? 'user' : 'assistant') : line.kind
    const reply: AgentReference = { agentMessageId: line.messageId, quotedRole, quotedText }
    setReferences((previous) => [...previous.filter((reference) => !isReplyReference(reference)), reply])
    setTimeout(() => input.current?.focus(), 50)
  }
  // Swiping a message to the right, or its Copy button, copies what it
  // says: an answer as the Markdown it was written in.
  // The line itself shows it took, with a tick, and what it does when the
  // browser refuses (SwipeableLine); a copy that cannot be made is a toast.
  const copyLine = async (line: Line): Promise<boolean> => {
    const text = shownText(line)
    if (!text || !navigator.clipboard) return false
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      return false
    }
  }
  const sayCopyFailed = () => toast.failed(t('common.copyFailed'))

  // reactTo puts the person's emoji on a message, or takes it away when
  // the emoji is empty. The badge changes once the server has it; the
  // event that follows says the same and changes nothing more.
  const reactTo = async (messageId: string, emoji: string) => {
    setPickingReactionFor(null)
    try {
      await graphql<{ ReactToAgentMessage: boolean }>(REACT, { messageId, emoji })
      applyReaction(messageId, 'person', emoji, new Date().toISOString(), { userId: ownUserId, name: ownName })
    } catch (caught) {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
    }
  }
  // reactionsOn is what a message's line draws under it: the badges, and
  // the picker while it is open on that message.
  const reactionsOn = (line: Line & { messageId?: string; reactions?: AgentMessageReaction[] }) => {
    const messageId = line.messageId
    if (!messageId) return null
    return (
      <>
        <ReactionBadges reactions={line.reactions} ownUserId={ownUserId} onRemove={() => void reactTo(messageId, '')} />
        {pickingReactionFor === messageId && (
          <ReactionPicker
            current={personReaction(line.reactions, ownUserId)}
            onPick={(emoji) => void reactTo(messageId, emoji)}
            onClose={closeReactionPicker}
          />
        )}
      </>
    )
  }
  const replying = references.find(isReplyReference)
  const chipped = references.filter((reference) => !isReplyReference(reference))
  // Who has read as far as which message, to be drawn under it.
  const chatMembers = loaded?.chat?.members
  const readersOf = useMemo(
    () => (chatMembers ? readersByMessage(lines, chatMembers, ownUserId) : new Map<string, ChatMember[]>()),
    [lines, chatMembers, ownUserId],
  )
  // A group's admins may delete what anybody wrote in it.
  const isChatAdmin = Boolean(
    loaded?.chat?.chatKind === 'group' &&
    loaded.chat.members.some((member) => member.userId === ownUserId && member.memberRole === 'admin'),
  )

  // toolLineElement is one tool call as a line that opens to what was asked
  // and what came back, with what the call made under it when asked to.
  const toolLineElement = (line: Extract<Line, { kind: 'tool' }>, withCards: boolean) => {
    const artifact = artifactOf(line)
    const shared = sharedFilesOf(line)
    return (
      <div
        key={line.key}
        className={['agent-line tool', line.done ? 'done' : '', isToolOpen(line.key) ? 'open' : '']
          .filter(Boolean)
          .join(' ')}
      >
        <button type="button" className="agent-tool-toggle" onClick={() => toggleExpanded(line.key)}>
          {line.done ? '✓' : '…'} {toolWords(line.tool)}
          {line.note ? <span className="muted"> · {line.note}</span> : null}
        </button>
        {isToolOpen(line.key) && (
          <div className="agent-tool-detail">
            {line.arguments && <CodeBlock text={line.arguments} tidy />}
            {line.result && <CodeBlock text={line.result} tidy />}
          </div>
        )}
        {withCards && artifact ? <ArtifactCard artifact={artifact} /> : null}
        {withCards &&
          shared.map((file) => (
            <FileCard key={file.attachment_id} file={file} />
          ))}
      </div>
    )
  }

  // toolGroupElement is what the person's own agent did to answer them in a
  // chat, as one folded row above the answer: whatever the setting for tool
  // calls says, since a chat has no other way to turn them on, and for that
  // person only, since the rows are in their own conversation of the chat and
  // nobody else's. What the calls made is under the row and not inside the
  // fold, as everywhere else.
  const toolGroupElement = (tools: Extract<Line, { kind: 'tool' }>[]) => {
    const groupKey = `work-${tools[0].key}`
    const isOpen = expanded.has(groupKey)
    const names = tools
      .map((tool) => toolWords(tool.tool))
      .filter((name, at, all) => all.indexOf(name) === at)
      .join(', ')
    return (
      <div key={groupKey} className={['agent-line tool work', tools.every((tool) => tool.done) ? 'done' : '', isOpen ? 'open' : ''].filter(Boolean).join(' ')}>
        <button type="button" className="agent-tool-toggle" aria-expanded={isOpen} onClick={() => toggleExpanded(groupKey)}>
          {tools.every((tool) => tool.done) ? '✓' : '…'} {t('chat.whatItDid')}
          <span className="muted"> · {names}</span>
        </button>
        {isOpen && <div className="agent-tool-detail agent-work-detail">{tools.map((tool) => toolLineElement(tool, false))}</div>}
        {tools.map((tool) => {
          const artifact = artifactOf(tool)
          const shared = sharedFilesOf(tool)
          return (
            <Fragment key={tool.key}>
              {artifact ? <ArtifactCard artifact={artifact} /> : null}
              {shared.map((file) => (
                <FileCard key={file.attachment_id} file={file} />
              ))}
            </Fragment>
          )
        })}
      </div>
    )
  }

  // drawLine is one line of the transcript as the drawer draws it.
  const drawLine = (line: Line, previous?: Line) => {
    switch (line.kind) {
      case 'chat': {
        const isRunStart = beginsRun(line, previous)
        const readers = line.messageId ? (readersOf.get(line.messageId) ?? []) : []
        // A deleted message is a mark that it was, not a gap.
        if (line.isDeleted) {
          return (
            <ChatLineFrame key={line.key} line={line} isRunStart={isRunStart} readers={readers}>
              <div className="agent-line chat-deleted muted">{t('chat.deleted')}</div>
            </ChatLineFrame>
          )
        }
        const isEditing = Boolean(line.messageId) && editing?.messageId === line.messageId
        const repliedTo = line.references?.find(isReplyReference)
        const canEdit = isOwnWords(line) && Boolean(line.messageId)
        const canDelete = Boolean(line.messageId) && (canEdit || isChatAdmin)
        return (
          <ChatLineFrame key={line.key} line={line} isRunStart={isRunStart} readers={readers}>
            {/* The time is in the toolbar, where it is read when the pointer
                is on the bubble: a tooltip over the same bubble opened on top
                of the toolbar and of the reactions picker. */}
            <SwipeableLine
              actionsLabel={line.at ? formatTime(line.at) : undefined}
              className={[
                isOwnWords(line)
                  ? 'agent-line user'
                  : line.authorKind === 'agent'
                    ? 'agent-line assistant chat-person chat-agent'
                    : 'agent-line assistant chat-person',
                line.isPending ? 'pending' : '',
              ]
                .filter(Boolean)
                .join(' ')}
              replyLabel={t('agentDrawer.reply')}
              copyLabel={t('common.copy')}
              onReply={() => replyTo(line)}
              onCopy={() => copyLine(line)}
              onCopyFailed={sayCopyFailed}
              reactLabel={t('agentDrawer.react')}
              onReact={line.messageId ? () => toggleReactionPicker(line.messageId ?? null) : undefined}
              extraActions={
                <>
                  {canEdit ? (
                    <button
                      type="button"
                      className="icon-action"
                      title={t('chat.editMessageFrom', { name: line.authorName })}
                      aria-label={t('chat.editMessageFrom', { name: line.authorName })}
                      onClick={() => setEditing({ messageId: line.messageId as string, text: line.text })}
                    >
                      <PencilIcon size={14} />
                    </button>
                  ) : null}
                  {canDelete ? (
                    <button
                      type="button"
                      className="icon-action danger"
                      title={t('chat.deleteMessageFrom', { name: line.authorName })}
                      aria-label={t('chat.deleteMessageFrom', { name: line.authorName })}
                      onClick={() => setDeletingMessage(line)}
                    >
                      <TrashIcon size={14} />
                    </button>
                  ) : null}
                </>
              }
            >
              {repliedTo?.quotedText ? <div className="agent-in-reply-to">{repliedTo.quotedText}</div> : null}
              {isEditing && editing ? (
                <div className="agent-chat-edit">
                  <textarea
                    autoFocus
                    rows={2}
                    value={editing.text}
                    aria-label={t('chat.editMessageFrom', { name: line.authorName })}
                    onChange={(event) => setEditing({ messageId: editing.messageId, text: event.target.value })}
                    onKeyDown={(event) => {
                      if (event.nativeEvent.isComposing || event.keyCode === 229) return
                      if (event.key === 'Escape') {
                        event.preventDefault()
                        setEditing(null)
                      } else if (event.key === 'Enter' && !event.shiftKey) {
                        event.preventDefault()
                        void saveEdit()
                      }
                    }}
                  />
                  <div className="row">
                    <button type="button" className="primary" onClick={() => void saveEdit()}>
                      {t('common.save')}
                    </button>
                    <button type="button" onClick={() => setEditing(null)}>
                      {t('common.cancel')}
                    </button>
                  </div>
                </div>
              ) : line.authorKind === 'agent' ? (
                <Markdown text={line.text} onLeaving={leaving} pictureSource={pictureSource} />
              ) : (
                line.text
              )}
              {line.attachments && line.attachments.length > 0 && <AttachmentChips attachments={line.attachments} />}
              {line.editedAt && !isEditing ? <span className="agent-chat-edited">{t('chat.edited')}</span> : null}
              {reactionsOn(line)}
            </SwipeableLine>
          </ChatLineFrame>
        )
      }
      // When a message was said is a tooltip over the bubble, never a
      // line inside it: a line that appears on hover changes the bubble's
      // size under the pointer, and the conversation should read as a
      // conversation, not as a log.
      case 'user': {
        const repliedTo = line.references?.find(isReplyReference)
        return (
          <Tooltip key={line.key} label={line.at ? formatTime(line.at) : ''}>
            <SwipeableLine
              className="agent-line user"
              replyLabel={t('agentDrawer.reply')}
              copyLabel={t('common.copy')}
              onReply={() => replyTo(line)}
              onCopy={() => copyLine(line)}
              onCopyFailed={sayCopyFailed}
            >
              {line.inReplyTo ? <div className="agent-in-reply-to">{line.inReplyTo}</div> : null}
              {repliedTo?.quotedText ? <div className="agent-in-reply-to">{repliedTo.quotedText}</div> : null}
              {line.references && line.references.length > 0 && <ReferenceChips references={line.references} />}
              {line.text}
              {line.attachments && line.attachments.length > 0 && <AttachmentChips attachments={line.attachments} />}
              {reactionsOn(line)}
            </SwipeableLine>
          </Tooltip>
        )
      }
      case 'assistant':
        return (
          <Tooltip key={line.key} label={line.at ? formatTime(line.at) : ''}>
            <SwipeableLine
              className={['agent-line assistant', line.streaming ? 'streaming' : ''].filter(Boolean).join(' ')}
              replyLabel={t('agentDrawer.reply')}
              copyLabel={t('common.copy')}
              onReply={() => replyTo(line)}
              onCopy={() => copyLine(line)}
              onCopyFailed={sayCopyFailed}
              reactLabel={t('agentDrawer.react')}
              onReact={line.messageId ? () => toggleReactionPicker(line.messageId ?? null) : undefined}
            >
              <Markdown
                text={
                  line.streaming
                    ? withoutPartialMarker(line.text)
                    : linkSharedCitations(
                        suggestedRepliesOf(line.text).displayText,
                        (ownerName) => sharedOwners[ownerName.toLowerCase()],
                      )
                }
                onLeaving={leaving}
                pictureSource={pictureSource}
              />
              <CitedEvidence files={citedIn(line.text, citedFiles)} />
              {showUsage && line.usage && (
                <div className="agent-usage muted">
                  {t('agentDrawer.tokens', {
                    in: formatCount(line.usage.promptTokens),
                    out: formatCount(line.usage.completionTokens),
                  })}
                  {line.usage.cost ? ` · ${formatMoney(line.usage.cost, budget?.currency)}` : ''}
                </div>
              )}
              {reactionsOn(line)}
            </SwipeableLine>
          </Tooltip>
        )
      case 'tool': {
        const artifact = artifactOf(line)
        const shared = sharedFilesOf(line)
        if (!showTools && !isCallRecord) {
          if (artifact) return <ArtifactCard key={line.key} artifact={artifact} />
          return shared.length > 0 ? (
            <Fragment key={line.key}>
              {shared.map((file) => (
                <FileCard key={file.attachment_id} file={file} />
              ))}
            </Fragment>
          ) : null
        }
        return toolLineElement(line, true)
      }
      case 'confirmation':
        return (
          <div key={line.key} className={['agent-line confirmation', line.risk].filter(Boolean).join(' ')}>
            <p>{line.summary}</p>
            {line.resolved ? (
              <p className="muted">
                {line.resolved === 'approved' ? t('agentDrawer.approved') : t('agentDrawer.declined')}
              </p>
            ) : (
              <div className="row">
                <button
                  type="button"
                  className={line.risk === 'destructive' ? 'danger' : 'primary'}
                  onClick={() => void resolve(line, true)}
                >
                  {t('agentDrawer.approve')}
                </button>
                <button type="button" onClick={() => void resolve(line, false)}>
                  {t('agentDrawer.decline')}
                </button>
              </div>
            )}
          </div>
        )
      case 'question':
        return (
          <QuestionCard
            key={line.key}
            line={line}
            onAnswer={(text) => void answer(line, text)}
            onChat={() => {
              void answer(line, CHAT_ABOUT_IT)
              setTimeout(() => input.current?.focus(), 50)
            }}
          />
        )
      case 'checkin':
        // A turn nobody typed, and an approval given after a turn ended:
        // how the agent came to speak, which its answer shows anyway.
        // A goal's call for the person is the exception: its line carries
        // the way to the goal, so it is shown whatever the setting.
        if (!showWorkingNotes && line.origin !== 'goalNeedsYou') return null
        return <CheckInLine key={line.key} at={line.at} text={line.text} origin={line.origin} />
      case 'note':
        if (line.noteKind === HERDR_ANSWERED_NOTE || line.noteKind === HERDR_ASKED_NOTE) return null
        if (line.noteKind === SKILL_SHARE_CALL_WAITING_NOTE)
          return <SkillShareCallWaitingNote key={line.key} detail={line.detail ?? ''} />
        if (!showWorkingNotes && line.noteKind && WORKING_NOTES.has(line.noteKind)) return null
        if (line.detail) {
          return (
            <div
              key={line.key}
              className={['agent-line note', expanded.has(line.key) ? 'open' : ''].filter(Boolean).join(' ')}
            >
              <button
                type="button"
                className="agent-tool-toggle"
                aria-expanded={expanded.has(line.key)}
                onClick={() => toggleExpanded(line.key)}
              >
                {line.text}
              </button>
              {expanded.has(line.key) && (
                <div className="agent-tool-detail agent-note-detail">
                  <Markdown text={line.detail} onLeaving={leaving} />
                </div>
              )}
            </div>
          )
        }
        return (
          <div
            key={line.key}
            className={['agent-line note muted', line.isPending ? 'pending' : ''].filter(Boolean).join(' ')}
            role={line.isPending ? 'status' : undefined}
          >
            {line.text}
          </div>
        )
      case 'error':
        return (
          <div key={line.key} className="agent-line error">
            {line.text}
          </div>
        )
      default:
        return null
    }
  }

  // A dropdown whose mark has gone goes with it -- the tab detached, the
  // last command ended, the limit taken away -- and none outlives the
  // conversation it was opened in. Left, it stayed open with nothing to
  // press to close it, and the backdrop under it took the next click.
  const isHeadMenuMarked =
    headMenu === 'tab'
      ? Boolean(tab?.attached)
      : headMenu === 'computers'
        ? computers.length > 0
        : headMenu === 'usage'
          ? Boolean(budget && budgetShown(budget))
          : headMenu === 'background'
            ? runningCommands.length > 0
            : true
  useEffect(() => {
    if (headMenu && !isHeadMenuMarked) setHeadMenu(null)
  }, [headMenu, isHeadMenuMarked])
  useEffect(() => {
    setHeadMenu(null)
  }, [conversationId])
  if (!isReachable) {
    return null
  }
  const current = conversations.find((conversation) => conversation.id === conversationId) ?? loaded
  // The chat on screen, as the conversation as loaded has it: the list is
  // read less often than a chat's events arrive.
  const chat = loaded?.id === conversationId && loaded.kind === 'chat' ? (loaded.chat ?? null) : null
  const isInChat = chat !== null
  const deletedNote = t('chat.deletedAccount')
  // The agents an @ offers here, those the typed letters match, and the
  // box's @ made into a choice of one of them.
  const addressable = chat && !chat.isRemoved ? addressableAgentsOf(chat, ownUserId, deletedNote) : []
  const mentionAgents = mention ? agentsMatching(addressable, mention.query) : []
  const watchMention = (words: string, caret: number | null) => {
    const typed = isInChat && addressable.length > 0 ? mentionAt(words, caret ?? words.length) : null
    setMention(typed)
    setMentionIndex(0)
  }
  const chooseMention = (agent: AddressableAgent) => {
    if (!mention) return
    const caret = input.current?.selectionStart ?? draft.length
    const next = withMention(draft, mention, caret, agent)
    setDraft(next.text)
    remember(draftKey(conversationId), next.text)
    setChosenAgents((previous) =>
      previous.some((chosen) => chosen.agentId === agent.agentId) ? previous : [...previous, agent],
    )
    setMention(null)
    window.setTimeout(() => {
      input.current?.focus()
      input.current?.setSelectionRange(next.caret, next.caret)
    }, 0)
  }
  const isFindingChat = !available && !loaded && !hasSearchedChats
  const title = current
    ? current.kind === 'main'
      ? t('agentDrawer.main')
      : (chat ?? current.chat)
        ? chatTitleOf(
            (chat ?? current.chat) as ChatSummary,
            ownUserId,
            deletedNote,
            current.title || t('agentDrawer.untitled'),
          )
        : current.title || t('agentDrawer.untitled')
    : isFindingChat
      ? t('common.loading')
      : t('agentDrawer.main')
  // The agent is off and nothing is open: where the main conversation would
  // be, the offer to turn it on, once the chats have been looked through and
  // there is none to open.
  const isOfferShown = !available && !loaded && hasSearchedChats
  const typingNow = Object.values(typing).filter((typist) => typist.until > Date.now() && typist.name)
  const typingNames = typingNow.filter((typist) => !typist.isAgent).map((typist) => typist.name)
  // An agent is named with its owner, as above its messages.
  const thinkingNames = typingNow
    .filter((typist) => typist.isAgent)
    .map((typist) => agentSpeakerName(typist.name, typist.ownerName ?? '', t))
  // A run's transcript can be talked into: the person reading what the
  // agent did on its own asks about it right there, with the run as the
  // history. A line above the box says what they are looking at.
  const isRun = current?.kind === 'run'
  const running = runs.length > 0
  // A list with every step done is put away once the turn ends: it has
  // nothing left to track and was taking the room above the box.
  const showTodos = todos.length > 0 && (running || todos.some((todo) => !todo.doneAt))
  const canSend = (draft.trim().length > 0 || pending.length > 0) && !uploading
  // Where an agent can be asked, the box says how.
  const chatPlaceholder =
    addressable.length > 0 ? t('chat.placeholderWithAgents') : t('chat.placeholder')
  // What the agent said it needs, while it is still waiting for it and
  // the person has not yet written back.
  const waitingNote = showingGoalNote && current?.goalState === 'waiting' ? (current.goalNote ?? '').trim() : ''

  return (
    <>
      {!open && !standalone && (
        <Tooltip label={available ? t('agentDrawer.open') : t('chat.open')}>
          <button
            type="button"
            className="agent-drawer-toggle"
            aria-label={
              unreadChatCount > 0
                ? `${available ? t('agentDrawer.open') : t('chat.open')}: ${plural(
                    unreadChatCount,
                    { one: 'chat.unreadOne', other: 'chat.unread' },
                    { count: unreadChatCount },
                  )}`
                : available
                  ? t('agentDrawer.open')
                  : t('chat.open')
            }
            onClick={toggle}
          >
            {available ? <SparkIcon size={20} /> : <PeopleIcon size={20} />}
            {unreadChatCount > 0 ? (
              <span className="agent-drawer-badge" aria-hidden="true">
                {unreadChatCount > 99 ? '99+' : unreadChatCount}
              </span>
            ) : null}
          </button>
        </Tooltip>
      )}
      {open && (
        <aside
          ref={chatBox.boxElement}
          className={[
            'agent-drawer',
            dragging ? 'dragging' : '',
            standalone ? 'standalone' : '',
            chatBox.placement ? 'placed' : '',
            chatBox.isRepositioning ? 'repositioning' : '',
          ]
            .filter(Boolean)
            .join(' ')}
          style={chatBox.style}
          aria-label={agentName || t('agent.title')}
          onDragOver={(event) => {
            if (event.dataTransfer.types.includes('Files')) {
              event.preventDefault()
              setDragging(true)
            }
          }}
          onDragLeave={() => setDragging(false)}
          onDrop={(event) => {
            if (event.dataTransfer.files.length > 0) {
              event.preventDefault()
              addFiles(event.dataTransfer.files)
            }
            setDragging(false)
          }}
        >
          {/* Grips for resizing on a wide window, for a pointer only: the
              stylesheet hides them where the box takes the screen. */}
          {!standalone &&
            RESIZE_GRIPS.map((grip) => (
              <div
                key={grip.name}
                className={`agent-drawer-grip ${grip.name}`}
                aria-hidden="true"
                {...chatBox.gripProps(grip.edges)}
              />
            ))}
          <div
            className={['agent-drawer-head', standalone ? '' : 'movable'].filter(Boolean).join(' ')}
            {...chatBox.headProps}
          >
            <button
              type="button"
              className="agent-drawer-conversation"
              aria-expanded={showingList}
              onClick={() => {
                setHeadMenu(null)
                setShowingList((previous) => !previous)
              }}
            >
              {chat ? (
                <ChatAvatars chat={chat} ownUserId={ownUserId} deleted={deletedNote} size={20} />
              ) : (
                <SparkIcon size={14} />
              )}
              <span className="agent-drawer-title">{title}</span>
              {chat?.isMuted ? (
                <span className="chat-muted" role="img" aria-label={t('chat.muted')} title={t('chat.muted')}>
                  <BellOffIcon size={12} />
                </span>
              ) : null}
              <ChevronDownIcon size={14} className="chevron" />
            </button>
            {/* In a goal's own conversation, where the goal stands, and the
                way to its page. Goals are started by asking the agent or on
                the Goals tab, never set on a chat. */}
            {current && current.kind === 'goal' && current.goal ? <GoalChip conversation={current} /> : null}
            {/* A chat's menu: who is in it, its name, its sound, leaving. */}
            {chat && (
              <HeadMark label={t('chat.menu')} isOpen={headMenu === 'chat'} onToggle={() => toggleHeadMenu('chat')}>
                <MoreIcon size={14} />
              </HeadMark>
            )}
            {/* What of the person's own is attached, as a mark with the
                details on hover and a line each under it on a press: the
                transcript is for the conversation. */}
            {tab?.attached && (
              <HeadMark
                label={t('agentDrawer.tabAttached', { title: tab.title || tab.url || '' })}
                className="agent-drawer-device"
                isOpen={headMenu === 'tab'}
                onToggle={() => toggleHeadMenu('tab')}
              >
                <GlobeIcon size={14} />
              </HeadMark>
            )}
            {computers.length > 0 && (
              <HeadMark
                label={
                  computers.length === 1
                    ? t('agentDrawer.computerAttached', { name: computers[0].name })
                    : t('agentDrawer.computersAttached', {
                        names: computers.map((computer) => computer.name).join(', '),
                      })
                }
                className="agent-drawer-device"
                isOpen={headMenu === 'computers'}
                onToggle={() => toggleHeadMenu('computers')}
              >
                <ComputerIcon size={14} />
              </HeadMark>
            )}
            {/* What this conversation has running on a computer, while
                anything is: how one ended is said in the conversation. */}
            {runningCommands.length > 0 && (
              <BackgroundMark
                commands={runningCommands}
                isOpen={headMenu === 'background'}
                onToggle={() => toggleHeadMenu('background')}
              />
            )}
            {/* The ring is the agent's budget: it means nothing in a
                conversation of people, or with the agent off. */}
            {available && !isInChat && (
              <BudgetRing
                budget={budget}
                zone={agentZone}
                isOpen={headMenu === 'usage'}
                onToggle={() => toggleHeadMenu('usage')}
              />
            )}
            {/* Framed by the extension, the panel around this has a bar
                of its own with the close on it; two of them, one under
                the other, is one too many. */}
            {!standalone && (
              <Tooltip label={t('agentDrawer.close')}>
                <button type="button" className="icon-button" aria-label={t('agentDrawer.close')} onClick={toggle}>
                  ×
                </button>
              </Tooltip>
            )}
          </div>
          {/* What this chat left running, dropped down from the head the
              way the conversations are, so the head stays where the box is
              dragged from and its edges where it is resized. */}
          {headMenu && <div className="agent-drawer-backdrop" onClick={() => setHeadMenu(null)} />}
          {headMenu === 'background' && (
            <BackgroundPanel
              commands={runningCommands}
              onChanged={() => void reloadBackground(true)}
              onClose={() => setHeadMenu(null)}
            />
          )}
          {headMenu === 'chat' && chat && (
            <ChatMenu
              chat={chat}
              onMembers={() => {
                setHeadMenu(null)
                setChatDialog('members')
              }}
              onRename={() => {
                setHeadMenu(null)
                setChatDialog('rename')
              }}
              onMute={(mutedUntil) => void muteChat(mutedUntil)}
              onLeave={() => {
                setHeadMenu(null)
                setChatDialog('leave')
              }}
              onClose={() => setHeadMenu(null)}
            />
          )}
          {headMenu === 'computers' && <ComputersMenu computers={computers} onClose={() => setHeadMenu(null)} />}
          {headMenu === 'tab' && tab?.attached && <TabMenu tab={tab} onClose={() => setHeadMenu(null)} />}
          {headMenu === 'usage' && available && !isInChat && (
            <UsageMenu budget={budget} zone={agentZone} onClose={() => setHeadMenu(null)} />
          )}
          {showingList && (
            <>
              {/* Anywhere outside the list closes it. */}
              <div className="agent-drawer-backdrop" onClick={() => setShowingList(false)} />
              <div className="agent-drawer-list" role="menu">
                <input
                  className="agent-drawer-search"
                  type="search"
                  value={search}
                  placeholder={t('agentDrawer.search')}
                  aria-label={t('agentDrawer.search')}
                  onChange={(event) => setSearch(event.target.value)}
                />
                {found !== null && found.length === 0 && (
                  <div className="agent-drawer-list-row muted">
                    <span className="agent-drawer-list-title">{t('agentDrawer.nothingFound')}</span>
                  </div>
                )}
                {/* The main chat first, whatever was said last, with the way
                    to start a side chat under it: it is the one the drawer
                    opens to, and a side chat is started from beside it. The
                    rest by when they were last spoken in, because that is
                    how somebody looks for one. */}
                {(found ?? conversations)
                  .filter((conversation) => conversation.kind === 'main')
                  .map((conversation) => conversationRow(conversation))}
                {/* With the agent off the main conversation is the offer to
                    turn it on, kept in its place at the top. */}
                {!available && found === null && (
                  <div className={['agent-drawer-list-row', 'main', isOfferShown ? 'active' : ''].join(' ')}>
                    <button type="button" className="agent-drawer-list-title" role="menuitem" onClick={showOffer}>
                      <span className="agent-drawer-list-name">
                        <StarIcon size={12} /> {t('agentDrawer.main')}
                      </span>
                      <span className="agent-drawer-list-summary muted">{t('chat.agentOff')}</span>
                    </button>
                  </div>
                )}
                {found === null && (
                  <>
                    {available && (
                      <button
                        type="button"
                        className="agent-drawer-list-row new stacked"
                        role="menuitem"
                        onClick={() => {
                          setShowingList(false)
                          void startNew()
                        }}
                      >
                        <PlusIcon size={14} />
                        <span className="agent-drawer-list-title">{t('agentDrawer.new')}</span>
                      </button>
                    )}
                    <button
                      type="button"
                      className="agent-drawer-list-row new stacked"
                      role="menuitem"
                      onClick={() => {
                        setShowingList(false)
                        setChatDialog('direct')
                      }}
                    >
                      <PeopleIcon size={14} />
                      <span className="agent-drawer-list-title">{t('chat.messageSomeone')}</span>
                    </button>
                    <button
                      type="button"
                      className="agent-drawer-list-row new"
                      role="menuitem"
                      onClick={() => {
                        setShowingList(false)
                        setChatDialog('group')
                      }}
                    >
                      <PeopleIcon size={14} />
                      <span className="agent-drawer-list-title">{t('chat.newGroup')}</span>
                    </button>
                  </>
                )}
                {[...(found ?? conversations)]
                  .filter((conversation) => conversation.kind !== 'main')
                  .sort((first, second) => (second.lastAt ?? '').localeCompare(first.lastAt ?? ''))
                  .map((conversation) => conversationRow(conversation))}
                {/* What has been put away, under everything else and shut
                    until it is asked for. */}
                {found === null && (
                  <button
                    type="button"
                    className="agent-drawer-list-row more"
                    role="menuitem"
                    onClick={() => void toggleArchived()}
                  >
                    <ArchiveIcon size={14} />
                    <span className="agent-drawer-list-title">
                      {showingArchived ? t('agentDrawer.hideArchived') : t('agentDrawer.showArchived')}
                    </span>
                  </button>
                )}
                {found === null && showingArchived && (
                  <>
                    <div className="agent-drawer-list-heading muted">{t('agentDrawer.archivedSection')}</div>
                    {archived.length === 0 ? (
                      <div className="agent-drawer-list-row muted">
                        <span className="agent-drawer-list-title">{t('agentDrawer.noneArchived')}</span>
                      </div>
                    ) : (
                      [...archived]
                        .sort((first, second) => (second.lastAt ?? '').localeCompare(first.lastAt ?? ''))
                        .map((conversation) => conversationRow(conversation))
                    )}
                  </>
                )}
              </div>
            </>
          )}
          <div
            className="agent-drawer-transcript"
            ref={(node) => {
              transcript.current = node
              setTranscriptElement(node)
            }}
            onScroll={(event) => {
              const element = event.currentTarget
              const ended = element.scrollHeight - element.scrollTop - element.clientHeight < 40
              setAtBottom(ended)
              if (ended) {
                sticking.current = true
              } else if (Date.now() - movedAt.current < 1500) {
                // Theirs, and still theirs while it carries on: a flick
                // on a phone goes on scrolling after the finger is up,
                // and a scrollbar is dragged without a wheel or a key.
                movedAt.current = Date.now()
                sticking.current = false
              }
            }}
          >
            {isOfferShown && (
              <div className="agent-drawer-offer">
                <p className="muted agent-drawer-empty">{t('agentConversations.unavailable')}</p>
                <button
                  type="button"
                  className="primary"
                  onClick={() => {
                    leaving()
                    navigate('/settings/agent')
                  }}
                >
                  {t('agentConversations.turnOnAction')}
                </button>
              </div>
            )}
            {isFindingChat && <p className="muted agent-drawer-empty">{t('common.loading')}</p>}
            {!isOfferShown && !isFindingChat && isInChat && lines.length === 0 && (
              <p className="muted agent-drawer-empty">{t('chat.empty')}</p>
            )}
            {!isOfferShown && !isFindingChat && !isInChat && lines.length === 0 && (
              <>
                <p className="muted agent-drawer-empty">{t('agentDrawer.empty')}</p>
                {draft.trim() === '' ? (
                  <IdeaSuggestions
                    key={conversationId}
                    conversationId={conversationId}
                    onStarted={(startedIn, openingRequest) => {
                      if (startedIn !== conversationId) {
                        sendToAgentConversation(startedIn, openingRequest)
                        return
                      }
                      remember(draftKey(startedIn), openingRequest)
                      sendWhenOpen.current = startedIn
                      setDraft(openingRequest)
                    }}
                  />
                ) : null}
              </>
            )}
            {total > messages.current.length && (
              <button
                type="button"
                className="agent-drawer-earlier muted"
                onClick={() => void loadEarlier()}
                disabled={loadingEarlier}
              >
                {t('agentDrawer.earlier', { count: total - messages.current.length })}
              </button>
            )}
            {lines.map((line, index) => {
              // A divider where the day changes.
              const at = 'at' in line ? line.at : undefined
              let previousAt: string | undefined
              for (let back = index - 1; back >= 0; back--) {
                const earlier = lines[back]
                if ('at' in earlier && earlier.at) {
                  previousAt = earlier.at
                  break
                }
              }
              const divider =
                at && dayOf(at) !== dayOf(previousAt) ? (
                  <div key={`${line.key}-day`} className="agent-day muted">
                    {dayLabel(at, t('agentDrawer.today'), t('agentDrawer.yesterday'))}
                  </div>
                ) : null
              // In a chat the calls the person's own agent made to answer are
              // one row, drawn at the first of them.
              if (isInChat && line.kind === 'tool' && !isCallRecord) {
                if (lines[index - 1]?.kind === 'tool') return null
                const tools: Extract<Line, { kind: 'tool' }>[] = []
                for (let ahead = index; ahead < lines.length; ahead++) {
                  const following = lines[ahead]
                  if (following.kind !== 'tool') break
                  tools.push(following)
                }
                const work = toolGroupElement(tools)
                return divider ? (
                  <Fragment key={`day-${line.key}`}>
                    {divider}
                    {work}
                  </Fragment>
                ) : (
                  work
                )
              }
              let drawn = drawLine(line, lines[index - 1])
              // A coding session's question, as the agent said it, takes
              // its options under it while it still waits. Which question
              // is named by the line it was written under, which is kept
              // in the list even where working notes are not shown.
              const asked = line.kind === 'assistant' && index > 0 ? lines[index - 1] : undefined
              const herdrQuestion =
                asked?.kind === 'checkin' && asked.origin === 'herdrQuestion' ? herdrQuestionOf(asked.text) : null
              if (herdrQuestion && line.kind === 'assistant') {
                // Its notes, read from the lines after it: what it asked,
                // and whether and how it was answered.
                const isThis = (note: HerdrNote | null): note is HerdrNote =>
                  !!note &&
                  note.computer === herdrQuestion.computer &&
                  note.paneId === herdrQuestion.paneId &&
                  note.questionFingerprint === herdrQuestion.questionFingerprint
                const notesOf = (noteKind: string) =>
                  lines
                    .slice(index + 1)
                    .map((later) =>
                      later.kind === 'note' && later.noteKind === noteKind ? herdrNoteOf(later.detail) : null,
                    )
                    .filter(isThis)
                const askedNote = notesOf(HERDR_ASKED_NOTE)[0]
                const answeredNote = notesOf(HERDR_ANSWERED_NOTE)[0] ?? null
                drawn = (
                  <HerdrQuestionLine
                    key={`${line.key}-herdr`}
                    drawn={drawn}
                    text={line.text}
                    at={line.at}
                    question={herdrQuestion}
                    asked={askedNote?.herdrSession}
                    answered={answeredNote}
                  />
                )
              }
              return divider ? (
                <Fragment key={`day-${line.key}`}>
                  {divider}
                  {drawn}
                </Fragment>
              ) : (
                drawn
              )
            })}
            {/* The replies the last answer offered, under it, to send with
                a click while nothing is running. In the transcript rather
                than on the box: they belong to that answer, and scroll away
                with it. */}
            {suggestedReplies.length > 0 ? (
              <div className="agent-suggested-replies" role="group" aria-label={t('agentDrawer.suggestedReplies')}>
                {suggestedReplies.map((reply) => (
                  <button key={reply} type="button" onClick={() => void send(reply)}>
                    {reply}
                  </button>
                ))}
              </div>
            ) : null}
            {isInChat ? (
              <TypingLine
                names={typingNames}
                // The person's own agent is thinking in the same words as
                // everybody else's: its own events are not the chat's, so
                // it is added here, once.
                agentNames={running ? [...thinkingNames, agentSpeakerName(agentName, ownName, t)] : thinkingNames}
              />
            ) : null}
            {running &&
              !isInChat &&
              !(
                lines[lines.length - 1]?.kind === 'assistant' &&
                (lines[lines.length - 1] as { streaming?: boolean }).streaming
              ) && (
                <Tooltip label={t('agentDrawer.thinking')}>
                  <div className="agent-line thinking" aria-label={t('agentDrawer.thinking')}>
                    <span className="agent-dots" aria-hidden="true">
                      <i />
                      <i />
                      <i />
                    </span>
                  </div>
                </Tooltip>
              )}
          </div>
          {!atBottom && lines.length > 0 && (
            <Tooltip label={t('agentDrawer.jumpToEnd')}>
              <button
                type="button"
                className="icon-button agent-drawer-jump"
                aria-label={t('agentDrawer.jumpToEnd')}
                onClick={() => {
                  const element = transcript.current
                  if (element) element.scrollTop = element.scrollHeight
                  sticking.current = true
                  setAtBottom(true)
                }}
              >
                <ArrowDownIcon size={16} />
              </button>
            </Tooltip>
          )}
          {/* The agent's task list, above what the person is about to
              type: its steps and how far it has got, one line each, the
              whole of a long one in its tooltip. The agent keeps it with
              its todo tool; the person reads it. */}
          {showTodos && (
            <div className="agent-drawer-todo">
              <ul aria-label={t('agentDrawer.todoTitle')}>
                {todos.map((todo) => (
                  <TodoLine key={todo.id} todo={todo} />
                ))}
              </ul>
            </div>
          )}
          {(references.length > 0 || pending.length > 0) && (
            <div className="agent-drawer-pending">
              {replying && (
                <ReplyQuote
                  reference={replying}
                  title={
                    replying.quotedRole === 'user'
                      ? t('agentDrawer.replyingToYourself')
                      : t('agentDrawer.replyingTo', {
                          name: isInChat
                            ? (
                                lines.find(
                                  (line) => line.kind === 'chat' && line.messageId === replying.agentMessageId,
                                ) as ChatLine | undefined
                              )?.authorName || t('chat.someone')
                            : agentName.trim() || t('agent.title'),
                        })
                  }
                  onRemove={() =>
                    setReferences((previous) => previous.filter((reference) => !isReplyReference(reference)))
                  }
                />
              )}
              {chipped.length > 0 && (
                <ReferenceChips
                  references={chipped}
                  onRemove={(removed) =>
                    setReferences((previous) => previous.filter((reference) => reference !== removed))
                  }
                />
              )}
              {pending.length > 0 && (
                <div className="agent-attachments">
                  {pending.map((file, index) => (
                    <span key={`${file.name}-${index}`} className="agent-attachment-chip">
                      <PaperclipIcon size={12} /> {file.name} <span className="muted">{formatBytes(file.size)}</span>
                      <button
                        type="button"
                        className="icon-action"
                        title={t('agentDrawer.remove')}
                        aria-label={`${file.name}: ${t('agentDrawer.remove')}`}
                        onClick={() => setPending((previous) => previous.filter((_, at) => at !== index))}
                      >
                        <CloseIcon size={12} />
                      </button>
                    </span>
                  ))}
                </div>
              )}
            </div>
          )}
          {actingAs ? (
            <div className="agent-drawer-readonly notice">{t('agentDrawer.actingAs', { name: actingAs })}</div>
          ) : null}
          {isRun ? (
            <div className="agent-drawer-readonly muted">
              {t('agentDrawer.runTranscript')}{' '}
              <button type="button" className="agent-artifact-action" onClick={() => void switchTo('')}>
                {t('agentDrawer.backToConversation')}
              </button>
            </div>
          ) : null}
          {/* What the agent needs before it can go on, said where the
              person is about to type rather than somewhere up the
              transcript they would have to scroll back to. */}
          {waitingNote ? (
            <div className="agent-drawer-goal-waiting">
              <TargetIcon size={12} />
              <span>{waitingNote}</span>
            </div>
          ) : null}
          {/* Voice mode: the box gives way to what is being heard, until the
              person ends it; the conversation above stays where it is. */}
          {isOfferShown || isFindingChat ? null : voiceState !== 'off' ? (
            <div className="agent-drawer-voice-mode" role="group" aria-label={t('agentDrawer.voiceMode')}>
              {/* While an answer is read aloud the meter cuts it short at a
                  tap: what a voice cannot do where the phone hears itself. */}
              <button
                type="button"
                className="agent-drawer-voice-meter"
                aria-label={t('agentDrawer.voiceCutAnswer')}
                title={isVoiceSpeaking ? t('agentDrawer.voiceCutAnswer') : undefined}
                disabled={!isVoiceSpeaking}
                onClick={() => voiceSession.current?.cutAnswer()}
              >
                <VoiceMeter
                  level={voiceLevel}
                  isHearing={isVoiceHearing}
                  isSpeaking={isVoiceSpeaking}
                  isAnswering={running}
                />
              </button>
              <p
                className={['agent-drawer-voice-caption', voiceCaption ? '' : 'muted'].filter(Boolean).join(' ')}
                aria-live="polite"
              >
                {voiceState === 'starting'
                  ? t('agentDrawer.voiceStarting')
                  : voiceCaption ||
                    (isVoiceHearing
                      ? t('agentDrawer.voiceHearing')
                      : isVoiceSpeaking
                        ? t('agentDrawer.voiceSpeaking')
                        : running
                          ? t('agentDrawer.voiceWorking')
                          : isMicrophoneMuted
                            ? ''
                            : t('agentDrawer.voiceListening'))}
              </p>
              {running && (
                <Tooltip label={t('agentDrawer.stop')}>
                  <button
                    type="button"
                    className="icon-button agent-stop"
                    aria-label={t('agentDrawer.stop')}
                    onClick={() => void stop()}
                  >
                    ■
                  </button>
                </Tooltip>
              )}
              <Tooltip label={isMicrophoneMuted ? t('agentDrawer.voiceUnmute') : t('agentDrawer.voiceMute')}>
                <button
                  type="button"
                  className="icon-button agent-voice-mute"
                  aria-label={t('agentDrawer.voiceMute')}
                  aria-pressed={isMicrophoneMuted}
                  onClick={toggleMicrophoneMuted}
                >
                  {isMicrophoneMuted ? <MicrophoneOffIcon size={16} /> : <MicrophoneIcon size={16} />}
                </button>
              </Tooltip>
              <Tooltip label={t('agentDrawer.voiceEnd')}>
                <button
                  type="button"
                  className="icon-button agent-drawer-voice-end"
                  aria-label={t('agentDrawer.voiceEnd')}
                  onClick={toggleVoice}
                >
                  <PhoneHangUpIcon size={18} />
                </button>
              </Tooltip>
            </div>
          ) : (
            <form
              className="agent-drawer-input"
              onSubmit={(event) => {
                event.preventDefault()
                void send()
              }}
            >
              <input
                ref={filePicker}
                type="file"
                multiple
                hidden
                onChange={(event) => {
                  addFiles(event.target.files)
                  event.target.value = ''
                }}
              />
              <Tooltip label={t('agentDrawer.attach')}>
                <button
                  type="button"
                  className="icon-button"
                  aria-label={t('agentDrawer.attach')}
                  onClick={() => filePicker.current?.click()}
                >
                  <PaperclipIcon size={16} />
                </button>
              </Tooltip>
              {mention && mentionAgents.length > 0 && (
                <MentionList
                  agents={mentionAgents}
                  activeIndex={Math.min(mentionIndex, mentionAgents.length - 1)}
                  onChoose={chooseMention}
                  onHover={setMentionIndex}
                />
              )}
              <textarea
                ref={input}
                rows={1}
                value={draft}
                placeholder={uploading ? t('agentDrawer.uploading') : isInChat ? chatPlaceholder : askPlaceholder}
                aria-label={isInChat ? chatPlaceholder : askPlaceholder}
                onChange={(event) => {
                  setDraft(event.target.value)
                  tellTyping(event.target.value)
                  watchMention(event.target.value, event.target.selectionStart)
                }}
                onClick={(event) => watchMention(event.currentTarget.value, event.currentTarget.selectionStart)}
                onBlur={() => setMention(null)}
                onPaste={(event) => {
                  const files = Array.from(event.clipboardData.files ?? [])
                  if (files.length > 0) {
                    event.preventDefault()
                    addFiles(files)
                  }
                }}
                onKeyDown={(event) => {
                  // Enter while an input method is composing picks a
                  // character, not a message to send.
                  if (event.nativeEvent.isComposing || event.keyCode === 229) return
                  // While an @ is offering agents, the arrows pick one, Enter
                  // and Tab choose it and Escape puts the offer away.
                  if (mention && mentionAgents.length > 0) {
                    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
                      event.preventDefault()
                      const step = event.key === 'ArrowDown' ? 1 : -1
                      setMentionIndex((mentionIndex + step + mentionAgents.length) % mentionAgents.length)
                      return
                    }
                    if (event.key === 'Enter' || event.key === 'Tab') {
                      event.preventDefault()
                      chooseMention(mentionAgents[Math.min(mentionIndex, mentionAgents.length - 1)])
                      return
                    }
                    if (event.key === 'Escape') {
                      event.preventDefault()
                      setMention(null)
                      return
                    }
                  }
                  if (event.key === 'Enter' && !event.shiftKey) {
                    event.preventDefault()
                    void send()
                  }
                }}
              />
              {running && (
                <Tooltip label={t('agentDrawer.stop')}>
                  <button
                    type="button"
                    className="icon-button agent-stop"
                    aria-label={t('agentDrawer.stop')}
                    onClick={() => void stop()}
                  >
                    ■
                  </button>
                </Tooltip>
              )}
              {voiceSampleRate > 0 && !isInChat && (
                <Tooltip label={voiceState === 'off' ? t('agentDrawer.voiceStart') : t('agentDrawer.voiceStop')}>
                  <button
                    type="button"
                    className={['icon-button', 'agent-voice', voiceState !== 'off' ? 'is-listening' : '']
                      .filter(Boolean)
                      .join(' ')}
                    aria-label={voiceState === 'off' ? t('agentDrawer.voiceStart') : t('agentDrawer.voiceStop')}
                    aria-pressed={voiceState !== 'off'}
                    disabled={isRun || Boolean(actingAs)}
                    onClick={toggleVoice}
                  >
                    <PhoneIcon size={16} />
                  </button>
                </Tooltip>
              )}
              <Tooltip label={t('agentDrawer.send')}>
                <button
                  type="submit"
                  className="icon-button agent-send"
                  aria-label={t('agentDrawer.send')}
                  disabled={!canSend || isReadingConversation}
                >
                  <ArrowUpIcon size={16} />
                </button>
              </Tooltip>
            </form>
          )}
          {dragging && <div className="agent-drawer-drop">{t('agentDrawer.dropHere')}</div>}
        </aside>
      )}
      {deleting ? (
        <ConfirmDialog
          title={t('agentDrawer.delete')}
          body={t('agentDrawer.deleteConfirm', { title: deleting.title || t('agentDrawer.untitled') })}
          confirmLabel={t('agentDrawer.delete')}
          destructive
          onClose={() => setDeleting(null)}
          onConfirm={() => void remove(deleting)}
        />
      ) : null}
      {chatDialog === 'direct' || chatDialog === 'group' ? (
        <ChatPeopleDialog
          purpose={chatDialog}
          onClose={() => setChatDialog(null)}
          onDone={(opened) => void (opened ? openedChat(opened) : setChatDialog(null))}
        />
      ) : null}
      {chatDialog === 'add' && chat ? (
        <ChatPeopleDialog
          purpose="add"
          conversationId={conversationId}
          leftOut={chat.members.filter((member) => !member.isRemoved).map((member) => member.userId)}
          onClose={() => setChatDialog(null)}
          onDone={() => {
            setChatDialog(null)
            void readConversation(conversationId).catch(() => undefined)
          }}
        />
      ) : null}
      {chatDialog === 'members' && chat ? (
        <ChatMembersDialog
          chat={chat}
          conversationId={conversationId}
          ownUserId={ownUserId}
          title={title}
          onClose={() => setChatDialog(null)}
          onAdd={() => setChatDialog('add')}
          onChanged={() => void readConversation(conversationId).catch(() => undefined)}
        />
      ) : null}
      {chatDialog === 'rename' && chat ? (
        <RenameChatDialog title={title} onClose={() => setChatDialog(null)} onRename={renameChat} />
      ) : null}
      {chatDialog === 'leave' && chat ? (
        <ConfirmDialog
          title={t('chat.leaveTitle', { title })}
          body={t('chat.leaveBody')}
          confirmLabel={t('chat.leave')}
          destructive
          onClose={() => setChatDialog(null)}
          onConfirm={() => void leaveChat()}
        />
      ) : null}
      {deletingMessage ? (
        <ConfirmDialog
          title={t('chat.deleteTitle')}
          body={t('chat.deleteBody')}
          confirmLabel={t('common.delete')}
          destructive
          onClose={() => setDeletingMessage(null)}
          onConfirm={() => void deleteMessage(deletingMessage)}
        />
      ) : null}
    </>
  )
}
