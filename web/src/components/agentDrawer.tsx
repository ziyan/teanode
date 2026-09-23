import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'
import {
  AGENT_ASK_EVENT,
  AGENT_OPEN_EVENT,
  AgentAskDetail,
  AgentOpenDetail,
  AgentReference,
  AgentViewing,
  VIEWING_EVENT,
  agentViewing,
  announceMailChanged,
  graphql,
  subscribe,
  authorization,
  framedDrawer,
  sharedAttachment,
} from '../api'
import { uploadFiles } from '../upload'
import { useAgentConversation } from '../hooks/useAgentConversation'
import { budgetNearness, formatClock, formatCount, formatMoney, formatTime } from './common'
import { useResolvedTheme } from './theme'
import { Tooltip } from './tooltip'
import { Markdown } from './markdown'
import { RelativeTime } from './relativeTime'
import {
  ArchiveIcon,
  ArrowDownIcon,
  ArrowUpIcon,
  ChevronDownIcon,
  ComputerIcon,
  GlobeIcon,
  InboxIcon,
  PaperclipIcon,
  PencilIcon,
  StarIcon,
  PlusIcon,
  SparkIcon,
  TargetIcon,
  TerminalIcon,
  TrashIcon,
  ExternalIcon,
} from './icons'
import {
  BackgroundCommand,
  BackgroundCommandRows,
  BackgroundOutputDialog,
  useBackgroundCommands,
} from './backgroundCommands'
import { CodeBlock } from './codeBlock'
import { ConfirmDialog, FormDialog } from './dialog'
import { ZoomablePicture } from './lightbox'
import { announceAgentAvailable, useAgentPreferences } from '../agentPreferences'
import { useToast } from './toast'
import { useTranslation } from '../i18n/i18n'

// DEVICES_EVERY is how often the drawer asks what is attached while it is
// open. Attaching and detaching happen outside this page, so there is
// nothing to be told by; often enough to feel immediate, rarely enough to
// be three small reads a minute.
const DEVICES_EVERY = 10_000

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
type GoalState = 'working' | 'waiting' | 'met'

interface Conversation {
  id: string
  kind: 'main' | 'named' | 'run'
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

// Which kind of turn of the agent's own a user message opens, if it opens
// one at all.
type CheckInOrigin = 'goal' | 'background'

function checkInOriginOf(text: string): CheckInOrigin | null {
  if (text.startsWith(GOAL_CHECK_IN_MARKER)) return 'goal'
  if (text.startsWith(BACKGROUND_COMMAND_MARKER)) return 'background'
  return null
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
}

// sharedFilesOf reads the files a tool line handed the person.
//
// Two tools answer with them and they are not the same shape: share_file
// was asked for one file and answers with it flat, while a skill whose step
// fetched pictures or a clip answers with a list of them under files. Both
// end up drawn the same way, because from the reader's side they are the
// same thing -- something the agent put in the conversation for them.
function sharedFilesOf(line: { tool: string; result?: string }): SharedFile[] {
  if (!line.result) return []
  try {
    const parsed = JSON.parse(unfenced(line.result)) as Partial<SharedFile> & { files?: Partial<SharedFile>[] }
    if (drawable(parsed)) return [parsed]
    // A file a skill fetched but could not keep has no address to draw it
    // from, and is left out rather than drawn as a broken one.
    if (Array.isArray(parsed.files)) return parsed.files.filter(drawable)
  } catch {
    // An error, most likely.
  }
  return []
}

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
interface Budget {
  used: number
  limit: number
  resetsAt: string
  cost: number
  costLimit: number
  currency: string
}

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

interface StoredMessage {
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
}

interface RunEvent {
  kind:
    'asked' | 'text' | 'message' | 'tool_call' | 'tool_result' | 'confirmation' | 'question' | 'note' | 'done' | 'error'
  runId: string
  sequence: number
  at?: string
  text?: string
  tool?: string
  callId?: string
  arguments?: string
  risk?: string
  note?: string
  error?: string
}

// A line of the transcript as the drawer draws it.
type Line =
  | { kind: 'user'; key: string; text: string; at?: string; attachments?: Attachment[]; references?: AgentReference[] }
  | { kind: 'assistant'; key: string; text: string; at?: string; streaming?: boolean; usage?: Usage | null }
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
    }
  | { kind: 'note'; key: string; text: string; at?: string }
  | { kind: 'error'; key: string; text: string }
  // A turn the agent started on its own: against the goal, or because a
  // background command ended. Its words are framing for the model and were
  // never the person's, so only the hour is drawn.
  | { kind: 'checkin'; key: string; at?: string; text: string; origin: CheckInOrigin }

const AGENT = `
  query {
    ReadAgent { agent { id enabled name } allowed { enabled ask } }
  }`

const TAB = `
  query {
    ReadAgentTab { attached title url }
    ReadAgentComputers { computers { name } }
    ReadAgent { budget { used limit resetsAt cost costLimit currency } timezone }
  }`

const CONVERSATIONS = `
  query ($archived: Boolean, $query: String) {
    ListAgentConversations(archived: $archived, query: $query) {
      id kind title summary lastAt archivedAt goal goalState goalNote goalNextAt goalSetAt
    }
  }`

const CONVERSATION = `
  query ($conversationId: String, $first: Int, $offset: Int) {
    ReadAgentConversation(conversationId: $conversationId, first: $first, offset: $offset) {
      conversation { id kind title summary lastAt archivedAt goal goalState goalNote goalNextAt goalSetAt }
      actingAs
      goalTurnsToday
      messages {
        id createdAt role content name toolCallId toolCalls { id name arguments }
        usage { promptTokens completionTokens cost }
        attachments { id name contentType size }
        references { itemId threadId subject from }
      }
      total
      todos { id text doneAt }
    }
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

const ASK = `
  mutation ($conversationId: String, $message: String!, $viewing: ViewingInput, $surface: String, $attachmentIds: [String!], $references: [AgentReferenceInput!]) {
    AskAgent(conversationId: $conversationId, message: $message, viewing: $viewing, surface: $surface, attachmentIds: $attachmentIds, references: $references) { runId conversationId }
  }`

const FEED = `
  subscription ($conversationId: String!) {
    AgentConversationEvents(conversationId: $conversationId) { kind runId sequence at text tool callId arguments risk note error }
  }`

const ANSWER = `
  mutation ($runId: String!, $callId: String!, $answer: String!) {
    AnswerAgentQuestion(runId: $runId, callId: $callId, answer: $answer)
  }`

const RESOLVE = `
  mutation ($runId: String!, $callId: String!, $approve: Boolean!) {
    ResolveAgentConfirmation(runId: $runId, callId: $callId, approve: $approve)
  }`

const STOP = `
  mutation ($runId: String!) {
    StopAgentRun(runId: $runId)
  }`

// A goal given here starts the conversation already working toward it, so
// the first turn runs with it rather than being told a moment later.
const START = `
  mutation ($title: String, $goal: String) {
    StartAgentConversation(title: $title, goal: $goal) { id kind title summary lastAt archivedAt }
  }`

// A variable left out is a field left alone: renaming sends no goal, and
// setting a goal sends no title. An empty goal is not nothing — it is the
// person saying there is no goal any more.
const UPDATE = `
  mutation ($conversationId: String!, $title: String, $goal: String, $archived: Boolean) {
    UpdateAgentConversation(conversationId: $conversationId, title: $title, goal: $goal, archived: $archived) { id }
  }`

const DELETE = `
  mutation ($conversationId: String!) {
    DeleteAgentConversation(conversationId: $conversationId)
  }`

const MAKE_MAIN = `
  mutation ($conversationId: String) {
    SetAgentMainConversation(conversationId: $conversationId) { id kind title summary lastAt archivedAt }
  }`

// The task list, written from this end as well as by the agent's own todo
// tool. Each of the three answers with the item as it stands afterwards,
// which is what the drawer puts in its list: a tick is one line of the
// conversation changing, and reading the whole transcript back to learn it
// would both cost a page of messages and race the ticks the agent makes
// while a turn is running.
const ADD_TODO = `
  mutation ($conversationId: String!, $text: String!) {
    AddAgentTodo(conversationId: $conversationId, text: $text) { id text doneAt }
  }`

const SET_TODO = `
  mutation ($conversationId: String!, $todoId: String!, $done: Boolean) {
    SetAgentTodo(conversationId: $conversationId, todoId: $todoId, done: $done) { id text doneAt }
  }`

const REMOVE_TODO = `
  mutation ($conversationId: String!, $todoId: String!) {
    RemoveAgentTodo(conversationId: $conversationId, todoId: $todoId)
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

  const headProps = {
    onPointerDown: (pointerEvent: React.PointerEvent<HTMLElement>) => {
      if (isGestureExempt(pointerEvent.target)) return
      begin(pointerEvent, null)
    },
    onPointerMove,
    onPointerUp: onPointerEnd,
    onPointerCancel: onPointerEnd,
    // A double click on the bar puts the box back in its corner at its
    // first size, and forgets where it was.
    onDoubleClick: (mouseEvent: React.MouseEvent<HTMLElement>) => {
      if (!isEnabled || isPhoneWidth() || isGestureExempt(mouseEvent.target)) return
      reset()
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
const CITATION = /(?:^|[^\w#/-])([a-z0-9][a-z0-9-]*(?:\/[a-z0-9][a-z0-9-]*)*#\d{1,6})(?![\w-])/g

// citationsIn is the references one answer makes, in the order it makes
// them and without repeats.
function citationsIn(text: string): string[] {
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
function linesOf(messages: StoredMessage[], t: (key: 'agentDrawer.stopped') => string): Line[] {
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
        lines.push({
          kind: 'user',
          key: message.id,
          text: message.content,
          at: message.createdAt,
          attachments: message.attachments ?? undefined,
          references: message.references ?? undefined,
        })
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
        lines.push({ kind: 'note', key: message.id, text: '' })
        break
      case 'note':
        // Timed, so that the day divider counts a note that opens a day
        // -- "Goal set" is the first line of a fresh conversation -- and
        // does not land under it.
        lines.push({
          kind: 'note',
          key: message.id,
          text: message.content === 'stopped' ? t('agentDrawer.stopped') : message.content,
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

// QuestionCard is the agent asking, with the choices it offered and a line
// for anything else.
function QuestionCard({
  line,
  onAnswer,
}: {
  line: Extract<Line, { kind: 'question' }>
  onAnswer: (text: string) => void
}) {
  const { t } = useTranslation()
  const [text, setText] = useState('')
  return (
    <div className="agent-line confirmation">
      <p>{line.question}</p>
      {line.answered ? (
        <p className="muted">{line.answered}</p>
      ) : (
        <>
          {line.choices.length > 0 && (
            <div className="row wrap">
              {line.choices.map((choice) => (
                <button key={choice} type="button" onClick={() => onAnswer(choice)}>
                  {choice}
                </button>
              ))}
            </div>
          )}
          <form
            className="row"
            onSubmit={(event) => {
              event.preventDefault()
              onAnswer(text)
            }}
          >
            <input
              value={text}
              placeholder={t('agentDrawer.answer')}
              aria-label={t('agentDrawer.answer')}
              onChange={(event) => setText(event.target.value)}
            />
            <button type="submit" className="primary" disabled={!text.trim()}>
              {t('agentDrawer.send')}
            </button>
          </form>
        </>
      )}
    </div>
  )
}

// AttachmentChips are the files under a turn: a picture shown small, the
// rest named, each a link to the file itself.
function AttachmentChips({ attachments }: { attachments: Attachment[] }) {
  return (
    <div className="agent-attachments">
      {attachments.map((attachment) =>
        attachment.id.startsWith('pending-') ? (
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

function ReferenceChips({
  references,
  onRemove,
}: {
  references: AgentReference[]
  onRemove?: (index: number) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="agent-references">
      {references.map((reference, index) => (
        <Tooltip
          key={`${reference.itemId ?? reference.path ?? ''}-${index}`}
          label={reference.from ?? reference.path ?? ''}
        >
          <span className="agent-reference-chip">
            <SparkIcon size={11} />{' '}
            {reference.subject || reference.name || reference.path || reference.itemId || reference.threadId}
            {onRemove && (
              <button
                type="button"
                className="link"
                aria-label={t('agentDrawer.remove')}
                onClick={() => onRemove(index)}
              >
                ×
              </button>
            )}
          </span>
        </Tooltip>
      ))}
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
function FileCard({ file }: { file: SharedFile }) {
  const { t } = useTranslation()
  const signed = useFileHref(file.url)
  // Nothing is drawn until there is an address to draw it from.
  const href = signed ?? undefined
  const type = file.content_type.toLowerCase()
  const media = !signed ? null : isImage(type) ? (
    <img src={signed} alt={file.name} className="agent-file-media" />
  ) : type.startsWith('video/') ? (
    <video src={signed} controls preload="metadata" className="agent-file-media" />
  ) : type.startsWith('audio/') ? (
    <audio src={signed} controls preload="metadata" className="agent-file-audio" />
  ) : null
  return (
    <div className="agent-artifact agent-file">
      <div className="agent-artifact-head">
        <PaperclipIcon size={11} />
        <span className="agent-artifact-title">{file.name}</span>
        <span className="muted">{formatBytes(file.size)}</span>
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

// DeviceButton is something of the person's that is attached, a browser
// tab or a computer: a button the size of the others in the bar, with the
// details on hover, that opens where attached things are listed.
function DeviceButton({
  label,
  framed,
  onLeaving,
  children,
}: {
  label: string
  framed: boolean
  onLeaving: () => void
  children: React.ReactNode
}) {
  const where = '/settings/agent/connections'
  return (
    <Tooltip label={label}>
      {framed ? (
        <a
          className="icon-button agent-drawer-device"
          href={`${window.location.origin}${where}`}
          target="_blank"
          rel="noreferrer"
          aria-label={label}
        >
          {children}
        </a>
      ) : (
        <Link className="icon-button agent-drawer-device" to={where} aria-label={label} onClick={onLeaving}>
          {children}
        </Link>
      )}
    </Tooltip>
  )
}

// BudgetRing is the day's tokens as a ring in the drawer's head: how much
// of the budget has gone, coloured by how near the end of it the day is,
// with the numbers and the hour it resets on hover, and the agent's own
// page a click away. Nothing is drawn where there is no limit to be near.
function BudgetRing({
  budget,
  zone,
  framed,
  onLeaving,
}: {
  budget: Budget
  zone: string
  framed: boolean
  onLeaving: () => void
}) {
  const { t } = useTranslation()
  const shown = budgetShown(budget)
  if (!shown) return null
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
    <Tooltip label={label}>
      {framed ? (
        // Framed into another site, the drawer sends the person to the
        // dashboard itself rather than drawing a settings page in here.
        <a
          className="icon-button agent-drawer-budget"
          href={`${window.location.origin}/settings/agent`}
          target="_blank"
          rel="noreferrer"
          aria-label={label}
        >
          {ring}
        </a>
      ) : (
        <Link className="icon-button agent-drawer-budget" to="/settings/agent" aria-label={label} onClick={onLeaving}>
          {ring}
        </Link>
      )}
    </Tooltip>
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
// CheckInLine is one turn of the agent's own, toward the goal or on
// hearing that a background command ended, as a line rather than a
// bubble; pressing it shows the words the turn was given, because a
// person watching the agent wants to know what it was told as much as
// what it did.
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
        {origin === 'background' ? <TerminalIcon size={12} /> : <TargetIcon size={12} />}
        {origin === 'background' ? t('agentDrawer.backgroundEnded') : t('agentDrawer.goal.checkIn')}
        {at ? ` · ${clockTime(at)}` : ''}
      </button>
      {open ? <pre className="agent-checkin-prompt">{text}</pre> : null}
    </div>
  )
}

function GoalChip({ conversation, onOpen }: { conversation: Conversation; onOpen: () => void }) {
  const { t } = useTranslation()
  // One icon, whatever the state: the head has the conversation's name,
  // the attached marks and the budget ring on it, and there is no room
  // for words. With a goal the icon takes the state's colour and the
  // tooltip says the state and the goal; the dialog behind it says the
  // rest.
  const goal = conversation.goal?.trim() ?? ''
  const state = goal ? goalStateOf(conversation) : ''
  const label = goal ? `${t(goalStateKey(state as GoalState))} · ${goal}` : t('agentDrawer.goal.set')
  return (
    <Tooltip label={label}>
      <button type="button" className={`icon-button agent-drawer-goal ${state}`} aria-label={label} onClick={onOpen}>
        <TargetIcon size={16} />
      </button>
    </Tooltip>
  )
}

// BackgroundMark is the head's mark for the commands this conversation
// left running in the background: a terminal, and how many still run.
// It stays while ended ones are listed, so how one ended can be read.
function BackgroundMark({ commands, onOpen }: { commands: BackgroundCommand[]; onOpen: () => void }) {
  const { t } = useTranslation()
  const runningCount = commands.filter((command) => command.isRunning).length
  const label =
    runningCount > 0 ? t('agentDrawer.backgroundRunning', { count: runningCount }) : t('agentDrawer.backgroundCommands')
  return (
    <Tooltip label={label}>
      <button
        type="button"
        className={
          runningCount > 0 ? 'icon-button agent-drawer-background running' : 'icon-button agent-drawer-background'
        }
        aria-label={label}
        onClick={onOpen}
      >
        <TerminalIcon size={14} />
        {runningCount > 0 ? <span>{runningCount}</span> : null}
      </button>
    </Tooltip>
  )
}

async function readConversationSnapshot(conversationId: string, signal: AbortSignal) {
  const response = await graphql<{
    ReadAgentConversation: {
      conversation: Conversation
      actingAs?: string | null
      goalTurnsToday?: number
      messages: StoredMessage[]
      total?: number
      todos: Todo[]
    }
  }>(CONVERSATION, { conversationId: conversationId || undefined, first: 100 }, signal)
  return response.ReadAgentConversation
}

// standalone is the drawer as a page of its own, framed by the browser
// extension into another site: always open, filling its frame, and its
// close mark telling the framing page to hide it.
export function AgentDrawer({ standalone = false }: { standalone?: boolean } = {}) {
  const { t } = useTranslation()
  const toast = useToast()
  const location = useLocation()
  // Framed by the extension, the panel is the frame and is moved with it.
  const chatBox = usePlacement(!standalone)
  const [available, setAvailable] = useState(false)
  const [agentName, setAgentName] = useState('')
  // What the box says before anybody types: the agent's own name where it
  // has one, because "your agent" is what a stranger calls it. Until the
  // name has arrived it says the general thing rather than flickering.
  const askPlaceholder = agentName.trim()
    ? t('agentDrawer.placeholderNamed', { name: agentName.trim() })
    : t('agentDrawer.placeholder')
  const [open, setOpen] = useState(() => standalone || remembered(OPEN_KEY) === '1')
  const [conversations, setConversations] = useState<Conversation[]>([])
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
  // The messages behind the lines, oldest first, and how many the
  // conversation holds: a drawer opens on the newest hundred, and the
  // difference is what "earlier messages" fetches.
  const messages = useRef<StoredMessage[]>([])
  const [total, setTotal] = useState(0)
  const [loadingEarlier, setLoadingEarlier] = useState(false)
  const [todos, setTodos] = useState<Todo[]>([])
  // What is being typed into the foot of the task list, and the items with
  // a write of the person's in flight -- a second tick on a box already on
  // its way to the server would ask for the opposite of what it shows.
  const [todoDraft, setTodoDraft] = useState('')
  const [todosBusy, setTodosBusy] = useState<string[]>([])
  const [addingTodo, setAddingTodo] = useState(false)
  // Whether the person has written to this conversation's list themselves.
  // The list is drawn only when there is one, so taking the last item off
  // would otherwise take away the box that puts one back.
  const [todosTouched, setTodosTouched] = useState(false)
  // When the person last wrote to the list. The agent ticks items off
  // through its tool while a turn runs, and a read of the conversation is
  // how those arrive -- so a read that was already outstanding when the
  // person ticked one is older than what they did, and does not get to
  // answer for the list. The next read settles it.
  const todoWrittenAt = useRef(0)
  const todosLoadedFor = useRef('')
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
  // The goal being typed, or null while the dialog is shut. An empty
  // string is a dialog open on a conversation that has no goal yet.
  const [goalDraft, setGoalDraft] = useState<string | null>(null)
  const [goalBusy, setGoalBusy] = useState(false)
  const [goalTurnsToday, setGoalTurnsToday] = useState(0)
  // The goal a conversation about to be started is given, or null while
  // the dialog is shut; an empty string starts one with no goal.
  const [startingGoal, setStartingGoal] = useState<string | null>(null)
  const [startingBusy, setStartingBusy] = useState(false)
  // The conversations that have been put away, and whether the picker is
  // showing them. Read only when the section is opened: most of the time
  // nobody looks, and the list is the one the drawer opens to.
  const [archived, setArchived] = useState<Conversation[]>([])
  const [showingArchived, setShowingArchived] = useState(false)
  // Whether the bar saying what the agent is waiting for is still up. It
  // comes down when the person sends, because the answer is on its way,
  // and the next read puts it back if the agent is still waiting.
  const [showingGoalNote, setShowingGoalNote] = useState(true)
  const [{ showTools, showUsage }] = useAgentPreferences()
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set())
  // The bubbles whose time is shown: a tap on a phone, where there is no
  // pointer to hover with.
  const [tab, setTab] = useState<{ attached: boolean; title?: string; url?: string } | null>(null)
  const [computers, setComputers] = useState<string[]>([])
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
  const filePicker = useRef<HTMLInputElement>(null)

  // Whether there is an agent to talk to at all. Asked once; the button
  // stays hidden otherwise, which is most servers.
  useEffect(() => {
    let cancelled = false
    graphql<{
      ReadAgent: { agent?: { enabled: boolean; name: string } | null; allowed: { enabled: boolean; ask: boolean } }
    }>(AGENT)
      .then((response) => {
        if (cancelled) return
        const view = response.ReadAgent
        const usable = Boolean(view.agent?.enabled && view.allowed.enabled && view.allowed.ask)
        setAvailable(usable)
        announceAgentAvailable(usable)
        setAgentName(view.agent?.name ?? '')
      })
      .catch(() => {
        if (!cancelled) setAvailable(false)
      })
    return () => {
      cancelled = true
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

  const loadConversations = useCallback(async () => {
    const response = await graphql<{ ListAgentConversations: Conversation[] }>(CONVERSATIONS, { archived: false })
    setConversations(response.ListAgentConversations)
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
    (snapshot: Awaited<ReturnType<typeof readConversationSnapshot>>, askedAt: number) => {
      setLoaded(snapshot.conversation)
      setActingAs(snapshot.actingAs ?? null)
      setGoalTurnsToday(snapshot.goalTurnsToday ?? 0)
      remember(CONVERSATION_KEY, snapshot.conversation.id)
      messages.current = snapshot.messages
      setTotal(snapshot.total ?? snapshot.messages.length)
      setLines(linesOf(snapshot.messages, t))
      setShowingGoalNote(true)
      // This read is how a tick the agent made during its turn reaches the
      // list; a tick of the person's own is already there, set from the
      // answer their own mutation gave, and is the newer of the two. A
      // conversation being opened is another list entirely, and takes what
      // the server says whatever was written to the one before it.
      const sameList = todosLoadedFor.current === snapshot.conversation.id
      if (!sameList) {
        todosLoadedFor.current = snapshot.conversation.id
        setTodoDraft('')
        setTodosTouched(false)
      }
      if (!sameList || todoWrittenAt.current < askedAt) {
        setTodos(snapshot.todos ?? [])
      }
      if (draftLoadedFor.current !== snapshot.conversation.id) {
        setDraft(remembered(draftKey(snapshot.conversation.id)))
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

  // What this conversation left running on the person's computers, kept up
  // while the drawer is open. The main conversation is named by an empty
  // id until it has been read, and the list wants its real one.
  const backgroundConversationId = conversationId || loaded?.id || ''
  const { commands: backgroundCommands, reload: reloadBackground } = useBackgroundCommands(
    backgroundConversationId,
    open && available && backgroundConversationId !== '',
  )
  const [isShowingBackground, setIsShowingBackground] = useState(false)
  const [backgroundOutput, setBackgroundOutput] = useState<BackgroundCommand | null>(null)

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
    if (!open || !available) return
    void loadConversations().catch((caught) => toast.failed(caught instanceof Error ? caught.message : String(caught)))
    void readConversation(conversationRef.current, true).catch((caught) =>
      toast.failed(caught instanceof Error ? caught.message : String(caught)),
    )
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, available])

  useEffect(() => {
    if (!open) adoptConversation(conversationId)
  }, [open, conversationId, adoptConversation])

  // The conversation's events while the drawer is open — every turn,
  // wherever it was started: here, a phone, a terminal, a chat app. The
  // turns in flight are replayed once the transcript has been read, and
  // a socket that drops comes back on its own, reading the transcript
  // again first so that the replay lands on a fresh one.
  useEffect(() => {
    if (!open || !available || !conversationId) return
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
        if (error && !stopped) toast.failed(error.message)
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
  }, [open, available, conversationId])

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

  // Escape toggles the drawer from anywhere on the page — unless a dialog
  // or a list is open, which Escape closes first.
  useEffect(() => {
    if (!available || standalone) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented) return
      if (document.querySelector('.dialog-scrim, .select-list, .agent-drawer-list')) return
      event.preventDefault()
      toggle()
      if (!open) setTimeout(() => input.current?.focus(), 50)
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [available, open])

  // Another page asking for a conversation to be opened here: a run's
  // transcript from the agent page.
  useEffect(() => {
    const listener = (event: Event) => {
      const detail = (event as CustomEvent<AgentOpenDetail>).detail
      if (!available || !detail?.conversationId) return
      detail.handled = true
      setOpen(true)
      remember(OPEN_KEY, '1')
      void switchTo(detail.conversationId)
    }
    window.addEventListener(AGENT_OPEN_EVENT, listener)
    return () => window.removeEventListener(AGENT_OPEN_EVENT, listener)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [available])

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
        ReadAgentComputers: { computers: { name: string }[] }
        ReadAgent: { budget: Budget | null; timezone: string }
      }>(TAB)
        .then((response) => {
          if (stopped) return
          setTab(response.ReadAgentTab)
          setComputers(response.ReadAgentComputers.computers.map((computer) => computer.name))
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
            (reference.path && reference.path === detail.reference.path),
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
        case 'settings':
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

  const applyEvent = (event: RunEvent) => {
    // What an event does beyond the transcript happens here, once: the
    // updater below may run twice under StrictMode.
    if (
      event.kind === 'tool_result' &&
      event.tool &&
      MAIL_TOOLS.has(event.tool) &&
      !(event.text ?? '').startsWith('{"error"')
    ) {
      announceMailChanged()
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
          return next
        }
        case 'confirmation':
          next.push({
            kind: 'confirmation',
            key: `${event.runId}-${event.callId}`,
            runId: event.runId,
            callId: event.callId ?? '',
            tool: event.tool ?? '',
            summary: event.note ?? '',
            risk: event.risk ?? '',
          })
          return next
        case 'question':
          next.push({
            kind: 'question',
            key: `${event.runId}-${event.callId}`,
            runId: event.runId,
            callId: event.callId ?? '',
            question: event.note ?? '',
            choices: (event.text ?? '').split('\n').filter(Boolean),
          })
          return next
        case 'note': {
          // A queued turn says so once, and the line goes when it starts.
          const queuedKey = `${event.runId}-queued`
          if (event.note === 'queued behind the turn before it') {
            next.push({ kind: 'note', key: queuedKey, text: t('agentDrawer.queued') })
            return next
          }
          const withoutQueued = next.filter((line) => line.key !== queuedKey)
          withoutQueued.push({
            kind: 'note',
            key: `${event.runId}-${event.sequence}`,
            text: event.note === 'stopped' ? t('agentDrawer.stopped') : (event.note ?? ''),
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
    if (sending.current > 0 && event.note === surface()) return
    // A turn of the agent's own, arriving live: the line, not the bubble,
    // and nothing here to have said it twice.
    const origin = checkInOriginOf(event.text ?? '')
    if (origin) {
      setLines((previous) => [
        ...previous,
        { kind: 'checkin', key: `${event.runId}-asked`, at: event.at, text: event.text ?? '', origin },
      ])
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
      return [...kept, { kind: 'user', key: `${event.runId}-asked`, text: event.text ?? '', at: event.at }]
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

  const send = async () => {
    const message = draft.trim()
    const files = pending
    const pointed = references
    if ((!message && files.length === 0) || uploading || loading.current) return
    const sendingConversationId = conversationRef.current
    setDraft('')
    remember(draftKey(conversationId), '')
    setPending([])
    setReferences([])
    if (input.current) input.current.style.height = 'auto'
    // Saying something is meaning to see it: wherever the transcript was
    // being read, it goes back to its end for the turn that follows.
    sticking.current = true
    setAtBottom(true)
    // What the agent was waiting for has been answered, as far as this
    // person is concerned; the bar asking for it has served.
    setShowingGoalNote(false)
    const key = `user-${Date.now()}`
    setLines((previous) => [
      ...previous,
      {
        kind: 'user',
        key,
        text: message,
        at: new Date().toISOString(),
        references: pointed.length > 0 ? pointed : undefined,
        attachments: files.map((file, index) => ({
          id: `pending-${index}`,
          name: file.name,
          contentType: file.type,
          size: file.size,
        })),
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
            line.key === key && line.kind === 'user' ? { ...line, attachments: uploaded } : line,
          ),
        )
      }
      sending.current += 1
      let response: { AskAgent: { runId: string; conversationId: string } }
      try {
        response = await graphql<{ AskAgent: { runId: string; conversationId: string } }>(ASK, {
          conversationId: conversationId || undefined,
          message: message || (files.length > 0 ? t('agentDrawer.filesOnly') : ''),
          viewing,
          surface: surface(),
          attachmentIds: attachmentIds.length > 0 ? attachmentIds : undefined,
          references: pointed.length > 0 ? pointed : undefined,
        })
      } finally {
        sending.current -= 1
      }
      if (conversationRef.current !== sendingConversationId) return
      if (!conversationId) {
        const conversation = response.AskAgent.conversationId
        adoptConversation(conversation)
        remember(CONVERSATION_KEY, conversation)
      }
      follow(response.AskAgent.runId)
    } catch (caught) {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
    }
  }

  const resolve = async (line: Extract<Line, { kind: 'confirmation' }>, approve: boolean) => {
    try {
      await graphql(RESOLVE, { runId: line.runId, callId: line.callId, approve })
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

  const answer = async (line: Extract<Line, { kind: 'question' }>, text: string) => {
    if (!text.trim()) return
    try {
      await graphql(ANSWER, { runId: line.runId, callId: line.callId, answer: text.trim() })
      setLines((previous) =>
        previous.map((candidate) =>
          candidate.key === line.key && candidate.kind === 'question'
            ? { ...candidate, answered: text.trim() }
            : candidate,
        ),
      )
    } catch (caught) {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
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
      toast.failed(caught instanceof Error ? caught.message : String(caught))
    }
  }

  // A new conversation may be given what it is for before a word is said,
  // so its first turn already works toward it; left empty it is an
  // ordinary conversation, which is most of them.
  const startNew = async (goal: string) => {
    setStartingBusy(true)
    try {
      const response = await graphql<{ StartAgentConversation: Conversation }>(START, {
        goal: goal.trim() || undefined,
      })
      setStartingGoal(null)
      await loadConversations()
      await switchTo(response.StartAgentConversation.id)
      if (goal.trim()) {
        toast.done(t('agentDrawer.goal.saved'))
      }
    } catch (caught) {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
    } finally {
      setStartingBusy(false)
    }
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
        await switchTo(remaining[0]?.id ?? '')
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
            <span className="agent-drawer-list-name">
              {/* A conversation working toward something is
              marked before its name, in the colour of
              where it stands: the accent while it works,
              the warning colour while it waits for the
              person, muted once it is met. */}
              {conversation.goal ? (
                <TargetIcon size={12} className={`agent-drawer-list-goal ${goalStateOf(conversation)}`} />
              ) : null}
              {conversation.kind === 'main' ? (
                <>
                  <StarIcon size={12} /> {t('agentDrawer.main')}
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
              <RelativeTime value={conversation.lastAt} />
            </span>
          </button>
        </Tooltip>
      )}
      {conversation.kind !== 'main' && renaming?.id !== conversation.id && (
        <span className="agent-drawer-list-actions">
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
          {/* Not on the main conversation: the server refuses to archive
              it, and an action that is always refused is worse than none. */}
          <Tooltip label={conversation.archivedAt ? t('agentDrawer.unarchive') : t('agentDrawer.archive')}>
            <button
              type="button"
              className="icon-button"
              aria-label={`${conversation.title || t('agentDrawer.untitled')}: ${
                conversation.archivedAt ? t('agentDrawer.unarchive') : t('agentDrawer.archive')
              }`}
              onClick={() => void archiveConversation(conversation, !conversation.archivedAt)}
            >
              {conversation.archivedAt ? <InboxIcon size={14} /> : <ArchiveIcon size={14} />}
            </button>
          </Tooltip>
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

  // Save and Clear in the goal dialog are the same write: the sentence the
  // person typed, or the empty string, which the server reads as "there is
  // no goal any more" and which also stops the turn under way. The dialog
  // keeps what was typed when the write fails, so nothing is retyped.
  const saveGoal = async (goal: string) => {
    if (!conversationId) return
    setGoalBusy(true)
    try {
      await graphql(UPDATE, { conversationId, goal })
      setGoalDraft(null)
      await loadConversations()
      await readConversation(conversationId)
      toast.done(goal ? t('agentDrawer.goal.saved') : t('agentDrawer.goal.cleared'))
    } catch (caught) {
      toast.failure(caught, t('agentDrawer.goal.clearFailed'))
    } finally {
      setGoalBusy(false)
    }
  }

  // The person's own three writes to the task list. Each takes the item
  // the server hands back and puts that one item in the list, so the
  // transcript is left where it is and nothing else on screen moves. The
  // hour of the write is kept so that a read already on its way cannot
  // undo it; see todoWrittenAt.
  const setTodoDone = async (todo: Todo, done: boolean) => {
    if (!conversationId || todosBusy.includes(todo.id)) return
    todoWrittenAt.current = Date.now()
    setTodosBusy((previous) => [...previous, todo.id])
    try {
      const response = await graphql<{ SetAgentTodo: Todo }>(SET_TODO, {
        conversationId,
        todoId: todo.id,
        done,
      })
      todoWrittenAt.current = Date.now()
      setTodos((previous) =>
        previous.map((candidate) => (candidate.id === todo.id ? response.SetAgentTodo : candidate)),
      )
    } catch (caught) {
      toast.failure(caught, t('agentDrawer.todoFailed'))
    } finally {
      setTodosBusy((previous) => previous.filter((candidate) => candidate !== todo.id))
    }
  }

  const addTodo = async () => {
    const text = todoDraft.trim()
    if (!conversationId || !text || addingTodo) return
    todoWrittenAt.current = Date.now()
    setAddingTodo(true)
    try {
      const response = await graphql<{ AddAgentTodo: Todo }>(ADD_TODO, { conversationId, text })
      todoWrittenAt.current = Date.now()
      const added = response.AddAgentTodo
      setTodos((previous) => [...previous.filter((candidate) => candidate.id !== added.id), added])
      setTodoDraft('')
      setTodosTouched(true)
      toast.done(t('agentDrawer.todoAdded'))
    } catch (caught) {
      toast.failure(caught, t('agentDrawer.todoFailed'))
    } finally {
      setAddingTodo(false)
    }
  }

  // No question asked before it goes: the item is one line the person
  // wrote, and typing it again costs less than a dialog.
  const removeTodo = async (todo: Todo) => {
    if (!conversationId || todosBusy.includes(todo.id)) return
    todoWrittenAt.current = Date.now()
    setTodosBusy((previous) => [...previous, todo.id])
    try {
      await graphql<{ RemoveAgentTodo: boolean }>(REMOVE_TODO, { conversationId, todoId: todo.id })
      todoWrittenAt.current = Date.now()
      setTodos((previous) => previous.filter((candidate) => candidate.id !== todo.id))
      setTodosTouched(true)
      toast.done(t('agentDrawer.todoRemoved'))
    } catch (caught) {
      toast.failure(caught, t('agentDrawer.todoFailed'))
    } finally {
      setTodosBusy((previous) => previous.filter((candidate) => candidate !== todo.id))
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
      setLines(linesOf(messages.current, t))
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

  // drawLine is one line of the transcript as the drawer draws it.
  const drawLine = (line: Line) => {
    switch (line.kind) {
      // When a message was said is a tooltip over the bubble, never a
      // line inside it: a line that appears on hover changes the bubble's
      // size under the pointer, and the conversation should read as a
      // conversation, not as a log.
      case 'user':
        return (
          <Tooltip key={line.key} label={line.at ? formatTime(line.at) : ''}>
            <div className="agent-line user">
              {line.references && line.references.length > 0 && <ReferenceChips references={line.references} />}
              {line.text}
              {line.attachments && line.attachments.length > 0 && <AttachmentChips attachments={line.attachments} />}
            </div>
          </Tooltip>
        )
      case 'assistant':
        return (
          <Tooltip key={line.key} label={line.at ? formatTime(line.at) : ''}>
            <div className={['agent-line assistant', line.streaming ? 'streaming' : ''].filter(Boolean).join(' ')}>
              <Markdown text={line.text} onLeaving={leaving} />
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
            </div>
          </Tooltip>
        )
      case 'tool': {
        const artifact = artifactOf(line)
        const shared = sharedFilesOf(line)
        if (!showTools) {
          if (artifact) return <ArtifactCard key={line.key} artifact={artifact} />
          return shared.length > 0 ? (
            <Fragment key={line.key}>
              {shared.map((file) => (
                <FileCard key={file.attachment_id} file={file} />
              ))}
            </Fragment>
          ) : null
        }
        return (
          <div
            key={line.key}
            className={['agent-line tool', line.done ? 'done' : '', expanded.has(line.key) ? 'open' : '']
              .filter(Boolean)
              .join(' ')}
          >
            <button type="button" className="agent-tool-toggle" onClick={() => toggleExpanded(line.key)}>
              {line.done ? '✓' : '…'} {line.tool}
              {line.note ? <span className="muted"> · {line.note}</span> : null}
            </button>
            {expanded.has(line.key) && (
              <div className="agent-tool-detail">
                {line.arguments && <CodeBlock text={line.arguments} tidy />}
                {line.result && <CodeBlock text={line.result} tidy />}
              </div>
            )}
            {artifact ? <ArtifactCard artifact={artifact} /> : null}
            {shared.map((file) => (
              <FileCard key={file.attachment_id} file={file} />
            ))}
          </div>
        )
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
        return <QuestionCard key={line.key} line={line} onAnswer={(text) => void answer(line, text)} />
      case 'checkin':
        return <CheckInLine key={line.key} at={line.at} text={line.text} origin={line.origin} />
      case 'note':
        return (
          <div key={line.key} className="agent-line note muted">
            {line.text || t('agentDrawer.compacted')}
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

  if (!available) {
    return null
  }
  const current = conversations.find((conversation) => conversation.id === conversationId) ?? loaded
  const title = current
    ? current.kind === 'main'
      ? t('agentDrawer.main')
      : current.title || t('agentDrawer.untitled')
    : t('agentDrawer.main')
  // A run's transcript can be talked into: the person reading what the
  // agent did on its own asks about it right there, with the run as the
  // history. A line above the box says what they are looking at.
  const isRun = current?.kind === 'run'
  const running = runs.length > 0
  const canSend = (draft.trim().length > 0 || pending.length > 0) && !uploading
  // What the agent said it needs, while it is still waiting for it and
  // the person has not yet written back.
  const waitingNote = showingGoalNote && current?.goalState === 'waiting' ? (current.goalNote ?? '').trim() : ''

  return (
    <>
      {!open && !standalone && (
        <Tooltip label={t('agentDrawer.open')}>
          <button type="button" className="agent-drawer-toggle" aria-label={t('agentDrawer.open')} onClick={toggle}>
            <SparkIcon size={20} />
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
              onClick={() => setShowingList((previous) => !previous)}
            >
              <SparkIcon size={14} />
              <span className="agent-drawer-title">{title}</span>
              <ChevronDownIcon size={14} className="chevron" />
            </button>
            {/* What this conversation is working toward, set and cleared
                here. A run has no goal: nobody talks it into one, and it
                is over by the time it is read. */}
            {current && conversationId && !isRun && (
              <GoalChip conversation={current} onOpen={() => setGoalDraft(current.goal ?? '')} />
            )}
            {/* What of the person's own is attached, as a mark with the
                details on hover: the transcript is for the conversation. */}
            {tab?.attached && (
              <DeviceButton
                label={t('agentDrawer.tabAttached', { title: tab.title || tab.url || '' })}
                framed={standalone}
                onLeaving={leaving}
              >
                <GlobeIcon size={14} />
              </DeviceButton>
            )}
            {computers.length > 0 && (
              <DeviceButton
                label={
                  computers.length === 1
                    ? t('agentDrawer.computerAttached', { name: computers[0] })
                    : t('agentDrawer.computersAttached', { names: computers.join(', ') })
                }
                framed={standalone}
                onLeaving={leaving}
              >
                <ComputerIcon size={14} />
              </DeviceButton>
            )}
            {/* What this conversation left running on a computer, while
                there is any: the count still running, and the list and
                their output behind it. */}
            {backgroundCommands.length > 0 && (
              <BackgroundMark commands={backgroundCommands} onOpen={() => setIsShowingBackground(true)} />
            )}
            {budget && <BudgetRing budget={budget} zone={agentZone} framed={standalone} onLeaving={leaving} />}
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
                {found === null && (
                  <>
                    <button
                      type="button"
                      className="agent-drawer-list-row new"
                      role="menuitem"
                      onClick={() => {
                        setShowingList(false)
                        setStartingGoal('')
                      }}
                    >
                      <PlusIcon size={14} />
                      <span className="agent-drawer-list-title">{t('agentDrawer.new')}</span>
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
            {lines.length === 0 && <p className="muted agent-drawer-empty">{t('agentDrawer.empty')}</p>}
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
              const drawn = drawLine(line)
              return divider ? (
                <Fragment key={`day-${line.key}`}>
                  {divider}
                  {drawn}
                </Fragment>
              ) : (
                drawn
              )
            })}
            {running &&
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
          {/* The task list, in the place it has always been: above what
              the person is about to type, under the transcript. On a
              conversation it is theirs to change as well as the agent's;
              a run's transcript is over and its list only says what
              happened, which is also what the server answers, since the
              todo mutations refuse a run. */}
          {(todos.length > 0 || (!isRun && todosTouched)) && (
            <div className="agent-drawer-todo">
              <ul>
                {todos.map((todo) => {
                  const done = Boolean(todo.doneAt)
                  const busy = todosBusy.includes(todo.id)
                  return (
                    <li key={todo.id} className={done ? 'done' : ''}>
                      {isRun ? (
                        <span>
                          {done ? '☑' : '☐'} {todo.text}
                        </span>
                      ) : (
                        <>
                          <label className="checkbox">
                            <input
                              type="checkbox"
                              checked={done}
                              disabled={busy}
                              onChange={() => void setTodoDone(todo, !done)}
                            />
                            <span>{todo.text}</span>
                          </label>
                          <Tooltip label={t('agentDrawer.todoRemove')}>
                            <button
                              type="button"
                              className="icon-action danger"
                              disabled={busy}
                              aria-label={`${todo.text}: ${t('agentDrawer.todoRemove')}`}
                              onClick={() => void removeTodo(todo)}
                            >
                              <TrashIcon size={12} />
                            </button>
                          </Tooltip>
                        </>
                      )}
                    </li>
                  )
                })}
              </ul>
              {!isRun && (
                <form
                  className="agent-drawer-todo-add"
                  onSubmit={(event) => {
                    event.preventDefault()
                    void addTodo()
                  }}
                >
                  <input
                    value={todoDraft}
                    onChange={(event) => setTodoDraft(event.target.value)}
                    placeholder={t('agentDrawer.todoPlaceholder')}
                    aria-label={t('agentDrawer.todoAdd')}
                    disabled={addingTodo}
                  />
                  <Tooltip label={t('agentDrawer.todoAdd')}>
                    <button
                      type="submit"
                      className="icon-action"
                      disabled={addingTodo || todoDraft.trim().length === 0}
                      aria-label={t('agentDrawer.todoAdd')}
                    >
                      <PlusIcon size={14} />
                    </button>
                  </Tooltip>
                </form>
              )}
            </div>
          )}
          {(references.length > 0 || pending.length > 0) && (
            <div className="agent-drawer-pending">
              {references.length > 0 && (
                <ReferenceChips
                  references={references}
                  onRemove={(index) => setReferences((previous) => previous.filter((_, at) => at !== index))}
                />
              )}
              {pending.length > 0 && (
                <div className="agent-attachments">
                  {pending.map((file, index) => (
                    <span key={`${file.name}-${index}`} className="agent-attachment-chip">
                      <PaperclipIcon size={12} /> {file.name} <span className="muted">{formatBytes(file.size)}</span>
                      <button
                        type="button"
                        className="link"
                        aria-label={t('agentDrawer.remove')}
                        onClick={() => setPending((previous) => previous.filter((_, at) => at !== index))}
                      >
                        ×
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
            <textarea
              ref={input}
              rows={1}
              value={draft}
              placeholder={uploading ? t('agentDrawer.uploading') : askPlaceholder}
              aria-label={askPlaceholder}
              onChange={(event) => {
                setDraft(event.target.value)
                // One line until there is more; then as tall as the words,
                // up to the cap the stylesheet sets.
                event.target.style.height = 'auto'
                event.target.style.height = `${Math.max(36, event.target.scrollHeight)}px`
              }}
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
          <p className="agent-drawer-note muted">{t('agentDrawer.mistakes')}</p>
          {dragging && <div className="agent-drawer-drop">{t('agentDrawer.dropHere')}</div>}
        </aside>
      )}
      {/* A new conversation, with what it is for if there is one. The same
          field as the goal dialog, asked before the first word rather than
          after it, so the first turn already works toward it. */}
      {startingGoal !== null ? (
        <FormDialog
          title={t('agentDrawer.new')}
          submitLabel={t('agentDrawer.start')}
          busy={startingBusy}
          onClose={() => setStartingGoal(null)}
          onSubmit={() => void startNew(startingGoal)}
        >
          <p className="muted">{t('agentDrawer.goal.hint')}</p>
          <label>
            <span>{t('agentDrawer.newGoal')}</span>
            <textarea rows={3} value={startingGoal} onChange={(event) => setStartingGoal(event.target.value)} />
          </label>
        </FormDialog>
      ) : null}
      {goalDraft !== null && current ? (
        <FormDialog
          title={t('agentDrawer.goal.title')}
          submitLabel={t('common.save')}
          busy={goalBusy}
          canSubmit={goalDraft.trim() !== '' && goalDraft.trim() !== (current.goal ?? '')}
          otherAction={
            current.goal ? (
              <button type="button" className="danger" disabled={goalBusy} onClick={() => void saveGoal('')}>
                {t('agentDrawer.goal.clear')}
              </button>
            ) : undefined
          }
          onClose={() => setGoalDraft(null)}
          onSubmit={() => void saveGoal(goalDraft.trim())}
        >
          <p className="muted">{t('agentDrawer.goal.hint')}</p>
          <label>
            <span>{t('agentDrawer.goal.label')}</span>
            <textarea rows={3} value={goalDraft} onChange={(event) => setGoalDraft(event.target.value)} />
          </label>
          {/* Where the goal stands, as the row says it: the state, since
              when, when the agent looks again, how many turns it has
              taken today, and its last word. Read, not edited. */}
          {current.goal ? (
            <dl className="agent-drawer-goal-status">
              <dt>{t('agentDrawer.goal.state')}</dt>
              <dd>{t(goalStateKey(goalStateOf(current)))}</dd>
              {current.goalSetAt ? (
                <>
                  <dt>{t('agentDrawer.goal.since')}</dt>
                  <dd>{formatTime(current.goalSetAt)}</dd>
                </>
              ) : null}
              {goalStateOf(current) === 'working' && current.goalNextAt ? (
                <>
                  <dt>{t('agentDrawer.goal.next')}</dt>
                  <dd>{formatTime(current.goalNextAt)}</dd>
                </>
              ) : null}
              <dt>{t('agentDrawer.goal.turnsToday')}</dt>
              <dd>{goalTurnsToday}</dd>
              {current.goalNote ? (
                <>
                  <dt>{t('agentDrawer.goal.lastNote')}</dt>
                  <dd>{current.goalNote}</dd>
                </>
              ) : null}
            </dl>
          ) : null}
        </FormDialog>
      ) : null}
      {/* One dialog at a time: the output takes the list's place while it
          is open, and closing it brings the list back. */}
      {backgroundOutput ? (
        <BackgroundOutputDialog
          command={backgroundOutput}
          onChanged={() => void reloadBackground(true)}
          onClose={() => setBackgroundOutput(null)}
        />
      ) : isShowingBackground ? (
        <ConfirmDialog
          title={t('agentDrawer.backgroundCommands')}
          wide
          body={
            <>
              <p className="muted">{t('agentDrawer.backgroundCommandsHint')}</p>
              {backgroundCommands.length === 0 ? (
                <p className="muted">{t('backgroundCommands.none')}</p>
              ) : (
                <BackgroundCommandRows
                  commands={backgroundCommands}
                  onOutput={setBackgroundOutput}
                  onChanged={() => void reloadBackground(true)}
                />
              )}
            </>
          }
          onClose={() => setIsShowingBackground(false)}
        />
      ) : null}
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
    </>
  )
}
