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
  withToken,
} from '../api'
import { uploadFiles } from '../upload'
import { budgetNearness, formatClock, formatCount, formatMoney, formatTime } from './common'
import { useResolvedTheme } from './theme'
import { Tooltip } from './tooltip'
import { Markdown } from './markdown'
import { ArrowDownIcon, ArrowUpIcon, ChevronDownIcon, GlobeIcon, PaperclipIcon, PencilIcon, ServerIcon, StarIcon, PlusIcon, SparkIcon, TrashIcon, ExternalIcon } from './icons'
import { CodeBlock } from './codeBlock'
import { ConfirmDialog } from './dialog'
import { announceAgentAvailable, useAgentPreferences } from '../agentPreferences'
import { useToast } from './toast'
import { useTranslation } from '../i18n/i18n'

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

interface Conversation {
  id: string
  kind: 'main' | 'named' | 'run'
  title: string
  summary?: string
  lastAt: string
  archivedAt?: string | null
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
    const parsed = JSON.parse(line.result) as Partial<Artifact>
    if (parsed.artifact_id && parsed.url && parsed.title) return parsed as Artifact
  } catch {
    // Not an artifact after all: an error, most likely.
  }
  return null
}

// A file the agent handed over, as share_file answered.
interface SharedFile {
  attachment_id: string
  name: string
  content_type: string
  size: number
  url: string
  caption?: string
}

// sharedFileOf reads what share_file answered, if this is its line.
function sharedFileOf(line: { tool: string; result?: string }): SharedFile | null {
  if (line.tool !== 'share_file' || !line.result) return null
  try {
    const parsed = JSON.parse(line.result) as Partial<SharedFile>
    if (parsed.attachment_id && parsed.url && parsed.name) return parsed as SharedFile
  } catch {
    // An error, most likely.
  }
  return null
}

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
    return { used: formatMoney(budget.cost, budget.currency), limit: formatMoney(budget.costLimit, budget.currency), fraction: money, money: true }
  }
  return { used: formatCount(budget.used), limit: formatCount(budget.limit), fraction: tokens, money: false }
}

interface Attachment {
  id: string
  name: string
  contentType: string
  size: number
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
  kind: 'asked' | 'text' | 'message' | 'tool_call' | 'tool_result' | 'confirmation' | 'question' | 'note' | 'done' | 'error'
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
  | { kind: 'tool'; key: string; tool: string; note: string; done: boolean; arguments?: string; result?: string }
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
  | { kind: 'note'; key: string; text: string }
  | { kind: 'error'; key: string; text: string }

const AGENT = `
  query {
    ReadAgent { agent { id enabled name } allowed { enabled ask } }
  }`

const TAB = `
  query {
    ReadAgentTab { attached title url }
    ReadAgentComputers { computers { name } }
    ReadAgent { budget { used limit resetsAt cost costLimit currency } }
  }`

const CONVERSATIONS = `
  query ($archived: Boolean, $query: String) {
    ListAgentConversations(archived: $archived, query: $query) { id kind title summary lastAt archivedAt }
  }`

const CONVERSATION = `
  query ($conversationId: String, $first: Int) {
    ReadAgentConversation(conversationId: $conversationId, first: $first) {
      conversation { id kind title summary lastAt archivedAt }
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

const START = `
  mutation ($title: String) {
    StartAgentConversation(title: $title) { id kind title summary lastAt archivedAt }
  }`

const UPDATE = `
  mutation ($conversationId: String!, $title: String) {
    UpdateAgentConversation(conversationId: $conversationId, title: $title) { id }
  }`

const DELETE = `
  mutation ($conversationId: String!) {
    DeleteAgentConversation(conversationId: $conversationId)
  }`

const MAKE_MAIN = `
  mutation ($conversationId: String) {
    SetAgentMainConversation(conversationId: $conversationId) { id kind title summary lastAt archivedAt }
  }`

// The tools after which what the mailbox shows may have changed.
const MAIL_TOOLS = new Set([
  'mail_act',
  'mail_draft',
  'mail_send',
  'folder_manage',
  'rule_add',
  'rule_update',
  'rule_remove',
  'rule_apply',
  'mailbox_settings',
  'reply_queue',
])

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

function draftKey(conversationId: string): string {
  return `teanode.agent.draft.${conversationId || 'main'}`
}

function isImage(contentType: string): boolean {
  return /^image\/(png|jpeg|gif|webp)/i.test(contentType)
}

function attachmentHref(attachment: Attachment): string {
  return withToken(`/api/v1/agent/attachments/${encodeURIComponent(attachment.id)}`)
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
  for (const message of messages) {
    switch (message.role) {
      case 'user':
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
        if (message.content.trim()) {
          lines.push({ kind: 'assistant', key: message.id, text: message.content, at: message.createdAt, usage: message.usage })
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
          })
        }
        break
      case 'compaction':
        lines.push({ kind: 'note', key: message.id, text: '' })
        break
      case 'note':
        lines.push({ kind: 'note', key: message.id, text: message.content === 'stopped' ? t('agentDrawer.stopped') : message.content })
        break
      default:
        break
    }
  }
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
          <a key={attachment.id} href={attachmentHref(attachment)} target="_blank" rel="noreferrer" title={attachment.name}>
            <img src={attachmentHref(attachment)} alt={attachment.name} className="agent-attachment-image" />
          </a>
        ) : (
          <a key={attachment.id} href={attachmentHref(attachment)} className="agent-attachment-chip" download={attachment.name}>
            <PaperclipIcon size={12} /> {attachment.name} <span className="muted">{formatBytes(attachment.size)}</span>
          </a>
        ),
      )}
    </div>
  )
}

function ReferenceChips({ references, onRemove }: { references: AgentReference[]; onRemove?: (index: number) => void }) {
  const { t } = useTranslation()
  return (
    <div className="agent-references">
      {references.map((reference, index) => (
        <span key={`${reference.itemId ?? ''}-${index}`} className="agent-reference-chip" title={reference.from ?? ''}>
          <SparkIcon size={11} /> {reference.subject || reference.itemId || reference.threadId}
          {onRemove && (
            <button type="button" className="link" aria-label={t('agentDrawer.remove')} onClick={() => onRemove(index)}>
              ×
            </button>
          )}
        </span>
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
  const [markdown, setMarkdown] = useState<string | null>(null)
  useEffect(() => {
    if (artifact.kind !== 'markdown') return
    let cancelled = false
    fetch(withToken(artifact.url), { credentials: 'same-origin' })
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
        <a
          className="icon-button agent-artifact-open"
          href={withToken(artifact.url)}
          target="_blank"
          rel="noreferrer"
          aria-label={t('agentDrawer.openArtifact')}
          title={t('agentDrawer.openArtifact')}
        >
          <ExternalIcon size={14} />
        </a>
      </div>
      {artifact.kind === 'markdown' ? (
        <div className="agent-artifact-body">{markdown === null ? <span className="muted">…</span> : <Markdown text={markdown} />}</div>
      ) : (
        <iframe
          ref={frame}
          className="agent-artifact-frame"
          title={artifact.title}
          src={`${withToken(artifact.url)}#theme=${shownTheme}`}
          sandbox="allow-scripts"
          style={height === null ? undefined : { height }}
        />
      )}
    </div>
  )
}

// FileCard is a file the agent handed over, under the tool line: a
// picture shown, a video or a sound playing, anything else to open.
function FileCard({ file }: { file: SharedFile }) {
  const { t } = useTranslation()
  const href = withToken(file.url)
  const type = file.content_type.toLowerCase()
  const media = isImage(type) ? (
    <img src={href} alt={file.name} className="agent-file-media" />
  ) : type.startsWith('video/') ? (
    <video src={href} controls preload="metadata" className="agent-file-media" />
  ) : type.startsWith('audio/') ? (
    <audio src={href} controls preload="metadata" className="agent-file-audio" />
  ) : null
  return (
    <div className="agent-artifact agent-file">
      <div className="agent-artifact-head">
        <PaperclipIcon size={11} />
        <span className="agent-artifact-title">{file.name}</span>
        <span className="muted">{formatBytes(file.size)}</span>
        <a
          className="icon-button agent-artifact-open"
          href={href}
          target="_blank"
          rel="noreferrer"
          aria-label={t('agentDrawer.openArtifact')}
          title={t('agentDrawer.openArtifact')}
        >
          <ExternalIcon size={14} />
        </a>
      </div>
      {media}
      {file.caption ? <div className="agent-file-caption">{file.caption}</div> : null}
    </div>
  )
}

// BudgetRing is the day's tokens as a ring in the drawer's head: how much
// of the budget has gone, coloured by how near the end of it the day is,
// with the numbers and the hour it resets on hover, and the agent's own
// page a click away. Nothing is drawn where there is no limit to be near.
function BudgetRing({ budget, framed, onLeaving }: { budget: Budget; framed: boolean; onLeaving: () => void }) {
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
  const spent = shown.money ? '' : ` ${t('agentDrawer.budgetSpent', { spent: formatMoney(budget.cost, budget.currency) })}`
  const label = `${t('agentDrawer.budget', { used: shown.used, limit: shown.limit, percent: String(percent) })}${spent} ${t(
    'agentDrawer.budgetResets',
    { at: formatClock(budget.resetsAt) },
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
        <a className="agent-drawer-budget" href={`${window.location.origin}/settings/agent`} target="_blank" rel="noreferrer" aria-label={label}>
          {ring}
        </a>
      ) : (
        <Link className="agent-drawer-budget" to="/settings/agent" aria-label={label} onClick={onLeaving}>
          {ring}
        </Link>
      )}
    </Tooltip>
  )
}

// standalone is the drawer as a page of its own, framed by the browser
// extension into another site: always open, filling its frame, and its
// close mark telling the framing page to hide it.
export function AgentDrawer({ standalone = false }: { standalone?: boolean } = {}) {
  const { t } = useTranslation()
  const toast = useToast()
  const location = useLocation()
  const [available, setAvailable] = useState(false)
  const [agentName, setAgentName] = useState('')
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
  const [conversationId, setConversationId] = useState(() => remembered(CONVERSATION_KEY))
  // The conversation as loaded, which the list does not always hold: a
  // run's transcript is opened from the agent page and is not in it.
  const [loaded, setLoaded] = useState<Conversation | null>(null)
  const [lines, setLines] = useState<Line[]>([])
  const [todos, setTodos] = useState<{ id: string; text: string; doneAt?: string | null }[]>([])
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
  const [{ showTools, showUsage }] = useAgentPreferences()
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set())
  // The bubbles whose time is shown: a tap on a phone, where there is no
  // pointer to hover with.
  const [timed, setTimed] = useState<Set<string>>(() => new Set())
  const [tab, setTab] = useState<{ attached: boolean; title?: string; url?: string } | null>(null)
  const [computers, setComputers] = useState<string[]>([])
  // The day's tokens against the budget, read with the rest and so kept
  // current as turns start and finish.
  const [budget, setBudget] = useState<Budget | null>(null)
  // How many turns this drawer has sent and not yet been handed the run
  // of: the feed's "asked" for one of those is the drawer's own words,
  // already on the page.
  const sending = useRef(0)
  // The transcript being read, so that the feed's first start waits for
  // it rather than reading it again.
  const loading = useRef<Promise<void> | null>(null)
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

  const loadConversations = useCallback(async () => {
    const response = await graphql<{ ListAgentConversations: Conversation[] }>(CONVERSATIONS, { archived: false })
    setConversations(response.ListAgentConversations)
    return response.ListAgentConversations
  }, [])

  const loadConversation = useCallback(async (id: string) => {
    const response = await graphql<{
      ReadAgentConversation: {
        conversation: Conversation
        messages: StoredMessage[]
        todos: { id: string; text: string; doneAt?: string | null }[]
      }
    }>(CONVERSATION, {
      conversationId: id || undefined,
      first: 100,
    })
    setConversationId(response.ReadAgentConversation.conversation.id)
    setLoaded(response.ReadAgentConversation.conversation)
    remember(CONVERSATION_KEY, response.ReadAgentConversation.conversation.id)
    setLines(linesOf(response.ReadAgentConversation.messages, t))
    setTodos(response.ReadAgentConversation.todos ?? [])
    setDraft(remembered(draftKey(response.ReadAgentConversation.conversation.id)))
    draftLoadedFor.current = response.ReadAgentConversation.conversation.id
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // readConversation is loadConversation with the promise kept, so the
  // feed's first start can wait for it. Reading is not by itself a
  // reason to follow the end again: a turn finishing and a socket coming
  // back both read, and a person who scrolled up to read stays where
  // they are through either. Opening a conversation is a deliberate act,
  // and says so.
  const readConversation = useCallback(
    (id: string, deliberate = false) => {
      if (deliberate) {
        sticking.current = true
        setAtBottom(true)
      }
      const reading = loadConversation(id).finally(() => {
        if (loading.current === reading) loading.current = null
      })
      loading.current = reading
      return reading
    },
    [loadConversation],
  )

  useEffect(() => {
    if (!open || !available) return
    void loadConversations().catch((caught) => toast.failed(caught instanceof Error ? caught.message : String(caught)))
    void readConversation(conversationId, true).catch((caught) =>
      toast.failed(caught instanceof Error ? caught.message : String(caught)),
    )
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, available])

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
      // The id first, so that opening the drawer loads this conversation
      // and not the one it remembers.
      setConversationId(detail.conversationId)
      remember(CONVERSATION_KEY, detail.conversationId)
      setOpen(true)
      remember(OPEN_KEY, '1')
      void switchTo(detail.conversationId)
    }
    window.addEventListener(AGENT_OPEN_EVENT, listener)
    return () => window.removeEventListener(AGENT_OPEN_EVENT, listener)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [available])

  // Whether the person's own tab is attached, asked when the drawer opens
  // and again before each turn.
  useEffect(() => {
    if (!open || !available) return
    graphql<{
      ReadAgentTab: { attached: boolean; title?: string; url?: string }
      ReadAgentComputers: { computers: { name: string }[] }
      ReadAgent: { budget: Budget | null }
    }>(TAB)
      .then((response) => {
        setTab(response.ReadAgentTab)
        setComputers(response.ReadAgentComputers.computers.map((computer) => computer.name))
        setBudget(response.ReadAgent.budget)
      })
      .catch(() => {
        setTab(null)
        setComputers([])
        setBudget(null)
      })
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
        previous.some((reference) => reference.itemId === detail.reference.itemId) ? previous : [...previous, detail.reference],
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
    if (event.kind === 'tool_result' && event.tool && MAIL_TOOLS.has(event.tool) && !(event.text ?? '').startsWith('{"error"')) {
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
    if ((!message && files.length === 0) || uploading) return
    setDraft('')
    remember(draftKey(conversationId), '')
    setPending([])
    setReferences([])
    if (input.current) input.current.style.height = 'auto'
    // Saying something is meaning to see it: wherever the transcript was
    // being read, it goes back to its end for the turn that follows.
    sticking.current = true
    setAtBottom(true)
    const key = `user-${Date.now()}`
    setLines((previous) => [
      ...previous,
      {
        kind: 'user',
        key,
        text: message,
        at: new Date().toISOString(),
        references: pointed.length > 0 ? pointed : undefined,
        attachments: files.map((file, index) => ({ id: `pending-${index}`, name: file.name, contentType: file.type, size: file.size })),
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
          previous.map((line) => (line.key === key && line.kind === 'user' ? { ...line, attachments: uploaded } : line)),
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
      if (!conversationId) {
        const conversation = response.AskAgent.conversationId
        setConversationId(conversation)
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
    await readConversation(id, true)
  }

  const startNew = async () => {
    try {
      const response = await graphql<{ StartAgentConversation: Conversation }>(START, {})
      await loadConversations()
      await switchTo(response.StartAgentConversation.id)
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

  const toggleTimed = (key: string) => {
    setTimed((previous) => {
      const next = new Set(previous)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
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
      case 'user':
        return (
          <div
  key={line.key}
  className={['agent-line user', timed.has(line.key) ? 'timed' : ''].filter(Boolean).join(' ')}
  title={line.at ? formatTime(line.at) : undefined}
  onClick={() => toggleTimed(line.key)}
          >
  {line.references && line.references.length > 0 && <ReferenceChips references={line.references} />}
  {line.text}
  {line.attachments && line.attachments.length > 0 && <AttachmentChips attachments={line.attachments} />}
  {line.at && <div className="agent-line-time">{formatTime(line.at)}</div>}
          </div>
        )
      case 'assistant':
        return (
          <div
  key={line.key}
  className={['agent-line assistant', line.streaming ? 'streaming' : '', timed.has(line.key) ? 'timed' : '']
    .filter(Boolean)
    .join(' ')}
  title={line.at ? formatTime(line.at) : undefined}
  onClick={() => toggleTimed(line.key)}
          >
  <Markdown text={line.text} onLeaving={leaving} />
  {line.at && <div className="agent-line-time">{formatTime(line.at)}</div>}
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
        )
      case 'tool': {
        const artifact = artifactOf(line)
        const shared = sharedFileOf(line)
        if (!showTools) {
          if (artifact) return <ArtifactCard key={line.key} artifact={artifact} />
          return shared ? <FileCard key={line.key} file={shared} /> : null
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
  {shared ? <FileCard file={shared} /> : null}
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

  return (
    <>
      {!open && !standalone && (
        <button
          type="button"
          className="agent-drawer-toggle"
          aria-label={t('agentDrawer.open')}
          title={t('agentDrawer.open')}
          onClick={toggle}
        >
          <SparkIcon size={20} />
        </button>
      )}
      {open && (
        <aside
          className={['agent-drawer', dragging ? 'dragging' : '', standalone ? 'standalone' : ''].filter(Boolean).join(' ')}
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
          <div className="agent-drawer-head">
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
            {/* What of the person's own is attached, as a mark with the
                details on hover: the transcript is for the conversation. */}
            {tab?.attached && (
              <Tooltip label={t('agentDrawer.tabAttached', { title: tab.title || tab.url || '' })}>
                <span className="agent-drawer-device" aria-label={t('agentDrawer.tabAttached', { title: tab.title || tab.url || '' })}>
                  <GlobeIcon size={14} />
                </span>
              </Tooltip>
            )}
            {computers.length > 0 && (
              <Tooltip label={computers.length === 1 ? t('agentDrawer.computerAttached', { name: computers[0] }) : t('agentDrawer.computersAttached', { names: computers.join(', ') })}>
                <span className="agent-drawer-device" aria-label={computers.length === 1 ? t('agentDrawer.computerAttached', { name: computers[0] }) : t('agentDrawer.computersAttached', { names: computers.join(', ') })}>
                  <ServerIcon size={14} />
                </span>
              </Tooltip>
            )}
            {budget && (
              <BudgetRing
                budget={budget}
                framed={standalone}
                onLeaving={leaving}
              />
            )}
            <button
              type="button"
              className="icon-button"
              aria-label={t('agentDrawer.close')}
              title={t('agentDrawer.close')}
              onClick={toggle}
            >
              ×
            </button>
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
                {found === null && (
                  <>
                    <button type="button" className="agent-drawer-list-row new" role="menuitem" onClick={() => void startNew()}>
                      <PlusIcon size={14} />
                      <span className="agent-drawer-list-title">{t('agentDrawer.new')}</span>
                    </button>
                  </>
                )}
                {found !== null && found.length === 0 && (
                  <div className="agent-drawer-list-row muted">
                    <span className="agent-drawer-list-title">{t('agentDrawer.nothingFound')}</span>
                  </div>
                )}
                {[...(found ?? conversations)]
                  // The main conversation first, whatever was said last,
                  // and a rule under it: it is the one the drawer opens to.
                  .sort((first, second) => Number(second.kind === 'main') - Number(first.kind === 'main'))
                  .map((conversation) => (
                  <div
                    key={conversation.id}
                    className={[
                      'agent-drawer-list-row',
                      conversation.id === conversationId ? 'active' : '',
                      conversation.kind === 'main' ? 'main' : '',
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
                      <button
                        type="button"
                        className="agent-drawer-list-title"
                        role="menuitem"
                        title={conversation.summary || undefined}
                        onClick={() => void switchTo(conversation.id)}
                      >
                        <span className="agent-drawer-list-name">
                          {conversation.kind === 'main' ? (
                            <>
                              <StarIcon size={12} /> {t('agentDrawer.main')}
                            </>
                          ) : (
                            conversation.title || t('agentDrawer.untitled')
                          )}
                        </span>
                        {conversation.summary ? (
                          <span className="agent-drawer-list-summary muted">{conversation.summary}</span>
                        ) : null}
                      </button>
                    )}
                    {conversation.kind !== 'main' && renaming?.id !== conversation.id && (
                      <span className="agent-drawer-list-actions">
                        <button
                          type="button"
                          className="icon-button"
                          aria-label={t('agentDrawer.makeMain')}
                          title={t('agentDrawer.makeMain')}
                          onClick={() => void makeMain(conversation.id)}
                        >
                          <StarIcon size={14} />
                        </button>
                        <button
                          type="button"
                          className="icon-button"
                          aria-label={t('agentDrawer.rename')}
                          title={t('agentDrawer.rename')}
                          onClick={() => setRenaming({ id: conversation.id, title: conversation.title })}
                        >
                          <PencilIcon size={14} />
                        </button>
                        <button
                          type="button"
                          className="icon-button danger"
                          aria-label={t('agentDrawer.delete')}
                          title={t('agentDrawer.delete')}
                          onClick={() => setDeleting(conversation)}
                        >
                          <TrashIcon size={14} />
                        </button>
                      </span>
                    )}
                  </div>
                ))}
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
            {running && !(lines[lines.length - 1]?.kind === 'assistant' && (lines[lines.length - 1] as { streaming?: boolean }).streaming) && (
              <div className="agent-line thinking" aria-label={t('agentDrawer.thinking')} title={t('agentDrawer.thinking')}>
                <span className="agent-dots" aria-hidden="true">
                  <i />
                  <i />
                  <i />
                </span>
              </div>
            )}
          </div>
          {!atBottom && lines.length > 0 && (
            <button
              type="button"
              className="icon-button agent-drawer-jump"
              aria-label={t('agentDrawer.jumpToEnd')}
              title={t('agentDrawer.jumpToEnd')}
              onClick={() => {
                const element = transcript.current
                if (element) element.scrollTop = element.scrollHeight
                sticking.current = true
                setAtBottom(true)
              }}
            >
              <ArrowDownIcon size={16} />
            </button>
          )}
          {todos.length > 0 && (
            <ul className="agent-drawer-todo">
              {todos.map((todo) => (
                <li key={todo.id} className={todo.doneAt ? 'done' : ''}>
                  {todo.doneAt ? '☑' : '☐'} {todo.text}
                </li>
              ))}
            </ul>
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
          {isRun ? (
            <div className="agent-drawer-readonly muted">
              {t('agentDrawer.runTranscript')}{' '}
              <button type="button" className="agent-artifact-action" onClick={() => void switchTo('')}>
                {t('agentDrawer.backToConversation')}
              </button>
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
            <button
              type="button"
              className="icon-button"
              aria-label={t('agentDrawer.attach')}
              title={t('agentDrawer.attach')}
              onClick={() => filePicker.current?.click()}
            >
              <PaperclipIcon size={16} />
            </button>
            <textarea
              ref={input}
              rows={1}
              value={draft}
              placeholder={uploading ? t('agentDrawer.uploading') : t('agentDrawer.placeholder')}
              aria-label={t('agentDrawer.placeholder')}
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
              <button
                type="button"
                className="icon-button agent-stop"
                aria-label={t('agentDrawer.stop')}
                title={t('agentDrawer.stop')}
                onClick={() => void stop()}
              >
                ■
              </button>
            )}
            <button
              type="submit"
              className="icon-button agent-send"
              aria-label={t('agentDrawer.send')}
              title={t('agentDrawer.send')}
              disabled={!canSend}
            >
              <ArrowUpIcon size={16} />
            </button>
          </form>
          <p className="agent-drawer-note muted">{t('agentDrawer.mistakes')}</p>
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
    </>
  )
}
