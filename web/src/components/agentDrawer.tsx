import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useLocation } from 'react-router-dom'
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
} from '../api'
import { uploadFiles } from '../upload'
import { formatCount, formatTime } from './common'
import { Markdown } from './markdown'
import { ArrowDownIcon, ArrowUpIcon, ChevronDownIcon, PaperclipIcon, PencilIcon, PlusIcon, SparkIcon, TrashIcon } from './icons'
import { CodeBlock } from './codeBlock'
import { ConfirmDialog } from './dialog'
import { announceAgentAvailable, useAgentPreferences } from '../agentPreferences'
import { useToast } from './toast'
import { useTranslation } from '../i18n/i18n'

// The drawer: the person talking to their agent from any page, in the one
// continuous conversation or a named one, with what they have open told to
// the agent so "this" means it. A turn streams back over the websocket:
// the words as they come, the tools as they run, and a card when the agent
// needs the person's word before it does something it cannot undo. A
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
  if ((line.tool !== 'artifact' && line.tool !== 'chart') || !line.result) return null
  try {
    const parsed = JSON.parse(line.result) as Partial<Artifact>
    if (parsed.artifact_id && parsed.url && parsed.title) return parsed as Artifact
  } catch {
    // Not an artifact after all: an error, most likely.
  }
  return null
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
  kind: 'text' | 'message' | 'tool_call' | 'tool_result' | 'confirmation' | 'question' | 'note' | 'done' | 'error'
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
        usage { promptTokens completionTokens }
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

const EVENTS = `
  subscription ($runId: String!) {
    AgentRunEvents(runId: $runId) { kind runId sequence at text tool callId arguments risk note error }
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
  return `/api/v1/agent/attachments/${encodeURIComponent(attachment.id)}`
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
        isImage(attachment.contentType) ? (
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
  const { t } = useTranslation()
  const [large, setLarge] = useState(false)
  const [markdown, setMarkdown] = useState<string | null>(null)
  useEffect(() => {
    if (artifact.kind !== 'markdown') return
    let cancelled = false
    fetch(artifact.url, { credentials: 'same-origin' })
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
    <div className={['agent-artifact', large ? 'large' : ''].filter(Boolean).join(' ')}>
      <div className="agent-artifact-head">
        <SparkIcon size={12} />
        <span className="agent-artifact-title">{artifact.title}</span>
        <button type="button" className="agent-artifact-action" onClick={() => setLarge((previous) => !previous)}>
          {large ? t('agentDrawer.smaller') : t('agentDrawer.larger')}
        </button>
        <a className="button agent-artifact-action" href={artifact.url} target="_blank" rel="noreferrer">
          {t('agentDrawer.openArtifact')}
        </a>
      </div>
      {artifact.kind === 'markdown' ? (
        <div className="agent-artifact-body">{markdown === null ? <span className="muted">…</span> : <Markdown text={markdown} />}</div>
      ) : (
        <iframe className="agent-artifact-frame" title={artifact.title} src={artifact.url} sandbox="allow-scripts" />
      )}
    </div>
  )
}

export function AgentDrawer() {
  const { t } = useTranslation()
  const toast = useToast()
  const location = useLocation()
  const [available, setAvailable] = useState(false)
  const [agentName, setAgentName] = useState('')
  const [open, setOpen] = useState(() => remembered(OPEN_KEY) === '1')
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
  const streams = useRef(new Map<string, () => void>())
  const transcript = useRef<HTMLDivElement>(null)
  const input = useRef<HTMLTextAreaElement>(null)
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
    setAtBottom(true)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    if (!open || !available) return
    void loadConversations().catch((caught) => toast.failed(caught instanceof Error ? caught.message : String(caught)))
    void loadConversation(conversationId).catch((caught) =>
      toast.failed(caught instanceof Error ? caught.message : String(caught)),
    )
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, available])

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
  // back when they return.
  useEffect(() => {
    remember(draftKey(conversationId), draft)
  }, [draft, conversationId])

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
    graphql<{ ReadAgentTab: { attached: boolean; title?: string; url?: string } }>(TAB)
      .then((response) => setTab(response.ReadAgentTab))
      .catch(() => setTab(null))
  }, [open, available, runs.length])

  useEffect(() => {
    const element = transcript.current
    if (element && atBottom) {
      element.scrollTop = element.scrollHeight
    }
  }, [lines, atBottom])

  // The transcript shrinks when the box under it grows with the words;
  // a person at the end stays at the end through it.
  useEffect(() => {
    const element = transcript.current
    if (!element || typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(() => {
      if (atBottom) element.scrollTop = element.scrollHeight
    })
    observer.observe(element)
    return () => observer.disconnect()
  }, [atBottom, open])

  useEffect(
    () => () => {
      for (const stop of streams.current.values()) stop()
      streams.current.clear()
    },
    [],
  )

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

  const toggle = () => {
    setOpen((previous) => {
      remember(OPEN_KEY, previous ? '0' : '1')
      return !previous
    })
  }

  const applyEvent = (event: RunEvent) => {
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
          if (event.tool && MAIL_TOOLS.has(event.tool) && !(event.text ?? '').startsWith('{"error"')) {
            announceMailChanged()
          }
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
          toast.failed(event.error ?? t('agentDrawer.failed'))
          return next
        default:
          return next
      }
    })
  }

  const follow = (id: string, conversation: string) => {
    setRuns((previous) => [...previous, id])
    const stop = subscribe<{ AgentRunEvents: RunEvent }>(
      EVENTS,
      { runId: id },
      (data) => {
        const event = data.AgentRunEvents
        if (event.kind !== 'note' || event.note !== 'queued behind the turn before it') {
          // The first event of a turn that is running: its queued line
          // has served.
          setLines((previous) => previous.filter((line) => line.key !== `${id}-queued`))
        }
        applyEvent(event)
        if (event.kind === 'done') {
          streams.current.get(id)?.()
          streams.current.delete(id)
          setRuns((previous) => previous.filter((candidate) => candidate !== id))
          void loadConversations()
          void loadConversation(conversation).catch(() => undefined)
        }
      },
      (error) => {
        if (error) toast.failed(error.message)
        streams.current.delete(id)
        setRuns((previous) => previous.filter((candidate) => candidate !== id))
      },
    )
    streams.current.set(id, stop)
  }

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
      const response = await graphql<{ AskAgent: { runId: string; conversationId: string } }>(ASK, {
        conversationId: conversationId || undefined,
        message: message || (files.length > 0 ? t('agentDrawer.filesOnly') : ''),
        viewing,
        surface: window.innerWidth < 720 ? 'phone' : 'drawer',
        attachmentIds: attachmentIds.length > 0 ? attachmentIds : undefined,
        references: pointed.length > 0 ? pointed : undefined,
      })
      let conversation = conversationId
      if (!conversationId) {
        conversation = response.AskAgent.conversationId
        setConversationId(conversation)
        remember(CONVERSATION_KEY, conversation)
      }
      follow(response.AskAgent.runId, conversation)
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
    for (const stopStream of streams.current.values()) stopStream()
    streams.current.clear()
    setRuns([])
    await loadConversation(id)
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
  <Markdown text={line.text} />
  {line.at && <div className="agent-line-time">{formatTime(line.at)}</div>}
  {showUsage && line.usage && (
    <div className="agent-usage muted">
      {t('agentDrawer.tokens', {
        in: formatCount(line.usage.promptTokens),
        out: formatCount(line.usage.completionTokens),
      })}
    </div>
  )}
          </div>
        )
      case 'tool': {
        const artifact = artifactOf(line)
        if (!showTools) {
          return artifact ? <ArtifactCard key={line.key} artifact={artifact} /> : null
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
      {!open && (
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
          className={['agent-drawer', dragging ? 'dragging' : ''].filter(Boolean).join(' ')}
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
                  <button type="button" className="agent-drawer-list-row new" role="menuitem" onClick={() => void startNew()}>
                    <PlusIcon size={14} />
                    <span className="agent-drawer-list-title">{t('agentDrawer.new')}</span>
                  </button>
                )}
                {found !== null && found.length === 0 && (
                  <div className="agent-drawer-list-row muted">
                    <span className="agent-drawer-list-title">{t('agentDrawer.nothingFound')}</span>
                  </div>
                )}
                {(found ?? conversations).map((conversation) => (
                  <div
                    key={conversation.id}
                    className={['agent-drawer-list-row', conversation.id === conversationId ? 'active' : '']
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
                            if (event.key === 'Escape') setRenaming(null)
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
                          {conversation.kind === 'main'
                            ? t('agentDrawer.main')
                            : conversation.title || t('agentDrawer.untitled')}
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
          {tab?.attached && (
            <div className="agent-drawer-tab muted" title={tab.url}>
              {t('agentDrawer.tabAttached', { title: tab.title || tab.url || '' })}
            </div>
          )}
          <div
            className="agent-drawer-transcript"
            ref={transcript}
            onScroll={(event) => {
              const element = event.currentTarget
              setAtBottom(element.scrollHeight - element.scrollTop - element.clientHeight < 40)
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
                <>
                  {divider}
                  {drawn}
                </>
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
