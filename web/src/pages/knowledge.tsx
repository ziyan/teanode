import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from '../i18n/i18n'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { ConfirmDialog, FormDialog } from '../components/dialog'
import { ErrorMessage, Loading, Tag } from '../components/common'
import {
  ChevronLeftIcon,
  ChevronRightIcon,
  GraphIcon,
  MergeIcon,
  MoveIcon,
  PencilIcon,
  PinIcon,
  PinOffIcon,
  TrashIcon,
  SparkIcon,
} from '../components/icons'
import { Select } from '../components/select'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../components/settingsList'
import { askAgentAbout, graphql } from '../api'
import { useQuery } from '../components/useQuery'
import { useToast } from '../components/toast'
import { useIsDesktop } from '../components/sidebar'
import { useSession } from '../session'
import { useBreadcrumbDetail } from '../components/breadcrumb'
import { Markdown } from '../components/markdown'
import { Tooltip } from '../components/tooltip'
import { GraphExplorer } from '../components/graphExplorer'
// The set of things to tick, the one the access pages tick roles and
// permissions with. Four audiences is the short end of what it is for,
// and it is the only control here that is a list of ticks.
import { CheckList } from './access/common'

// messageOf is what went wrong, in words a person can act on.
function messageOf(caught: unknown): string {
  return caught instanceof Error ? caught.message : String(caught)
}

// What the agent knows, as the person reads and corrects it.
//
// The same graph the agent reads, addressed the same way: a page has a
// path, and a fact on it is cited as path#number. That the two agree is
// the point -- when the agent says "people/alice-chen#3" in a
// conversation, this is where that is.

const PAGE = `query ($path: String!) {
  AgentGraphPage(path: $path) {
    node { id path kind name aliases summary contactId pinned dormant usedAt modifiedAt }
    facts { id number kind text happenedAt inferred evidence { kind id quote } audiences createdAt }
    folded { into fact { id number text } }
    children { id path kind name summary }
    attachments { documentId name contentType channel thread path }
    contact { id name emails organization }
  }
}`

const CHILDREN = `query ($path: String!, $first: Int, $offset: Int) {
  AgentGraphChildren(path: $path, first: $first, offset: $offset) {
    rows { node { id path kind name summary pinned dormant importance } hint children }
    total
  }
}`

const SEARCH = `query ($query: String!, $first: Int) {
  SearchAgentGraph(query: $query, first: $first) {
    nodes { id path kind name summary }
    facts { fact { id number text inferred } path name }
  }
}`

// What a turn asking this question would have been carried from the
// graph. It says nothing to a model and marks nothing used, so it can be
// asked as often as it takes to see why an answer read the way it did.
const RECALL = `query ($question: String!) {
  RecallAgentMemory(question: $question) {
    pages { path facts { number text } }
  }
}`

// What the sources indexed, searched the way the agent's own knowledge
// tool searches it. The graph above is what the agent made of what it
// read; this is what it read.
const DOCUMENT_SEARCH = `query ($query: String!, $first: Int, $sourceId: String) {
  SearchAgentDocuments(query: $query, first: $first, sourceId: $sourceId) {
    passages { documentId externalId title kind author sourceId source happenedAt number text }
    definitions { symbol kind line documentId externalId title }
    meaningful
  }
}`

// One document, a slice at a time. A slice rather than the whole of it
// because a source file or a year of chat is megabytes, and a dialog that
// waits for all of it before drawing any of it is a dialog that hangs.
const DOCUMENT_READ = `query ($documentId: String!, $from: Int) {
  ReadAgentDocument(documentId: $documentId, from: $from) {
    documentId externalId title kind author source happenedAt from text total next
  }
}`

// The places the agent reads, for the box that narrows a search to one of
// them. Their names only: this is a filter, not the card on the Agent page
// that manages them.
const KNOWLEDGE_SOURCES = `query { ListAgentKnowledgeSources { id name } }`

const SAVE_NODE = `mutation ($path: String!, $kind: String, $name: String, $summary: String, $aliases: [String!], $pinned: Boolean) {
  SaveAgentNode(path: $path, kind: $kind, name: $name, summary: $summary, aliases: $aliases, pinned: $pinned) { id path }
}`

const SAVE_FACT = `mutation ($path: String!, $number: Int, $kind: String, $text: String!, $happened: String, $audiences: [String!]) {
  SaveAgentFact(path: $path, number: $number, kind: $kind, text: $text, happened: $happened, audiences: $audiences) { id number }
}`

const HISTORY = `query ($path: String!, $first: Int) {
  ListAgentPageHistory(path: $path, first: $first) {
    revision kind actor summary change before after path reason createdAt
  }
}`

const MOVE_NODE = `mutation ($path: String!, $under: String!) {
  MoveAgentNode(path: $path, under: $under) { id path }
}`

const MERGE_NODES = `mutation ($path: String!, $into: String!) {
  MergeAgentNodes(path: $path, into: $into) { id path }
}`

const MOVE_FACT = `mutation ($path: String!, $number: Int!, $to: String!) {
  MoveAgentFact(path: $path, number: $number, to: $to) { id number }
}`

const LINK_NODES = `mutation ($path: String!, $to: String!, $relation: String!, $note: String) {
  LinkAgentNodes(path: $path, to: $to, relation: $relation, note: $note)
}`

const DELETE_FACT = `mutation ($path: String!, $number: Int!) { DeleteAgentFact(path: $path, number: $number) }`
const DELETE_NODE = `mutation ($path: String!) { DeleteAgentNode(path: $path) }`

// The relations the graph states, the same list and the same order the
// agent's own memory tool offers. Stated rather than free text: a link
// typed as "workson" once is a link nothing ever walks along again.
const RELATIONS = [
  'part_of',
  'works_on',
  'member_of',
  'knows',
  'owns',
  'uses',
  'located_in',
  'related_to',
  'decided_in',
  'about',
]

// The unattended runs a fact can be addressed to, in the order the
// command line's --applies-to names them. The conversation is not among
// them: it always reads a fact on a page it is shown, so offering it as
// something to switch off would be offering something that does not
// happen.
const AUDIENCES = ['triage', 'reply', 'research', 'summaries']

// The same four as rows to tick. Made once rather than per render: the
// list keeps the order it was opened with, and a fresh array every
// render would have it settle that order again on every keystroke.
const AUDIENCE_ITEMS = AUDIENCES.map((audience) => ({ id: audience }))

type Node = {
  id: string
  path: string
  kind: string
  name: string
  aliases?: string[]
  summary: string
  contactId?: string
  pinned?: boolean
  dormant?: boolean
  importance?: number
  usedAt?: string | null
  modifiedAt?: string
}

type Evidence = { kind: string; id: string; quote: string }

type Fact = {
  id: string
  number: number
  kind: string
  text: string
  happenedAt?: string | null
  inferred?: boolean
  evidence: Evidence[]
  audiences: string[]
  createdAt: string
}

type Revision = {
  revision: number
  kind: string
  actor: string
  summary: string
  change: string
  before: string
  after: string
  path: string
  reason: string
  createdAt: string
}

// FoldedFact is a fact the page no longer states, with the number of the
// one that absorbed it.
type FoldedFact = { into: number; fact: { id: string; number: number; text: string } }

// A page as recall carries it: its path, and the facts from it that
// would have gone in front of the model. Not the whole fact -- what is
// being asked is what the model was told.
type RecalledPage = { path: string; facts: { number: number; text: string }[] }

// One passage a document search found: enough of the document to head it
// with, and the words that matched. Uncut -- the passage is the answer, and
// the reader below is for the document around it.
type Passage = {
  documentId: string
  externalId: string
  title: string
  kind: string
  author: string
  sourceId: string
  source: string
  happenedAt?: string | null
  number: number
  text: string
}

// Where an identifier in the words is defined. A name pasted out of a log
// is looked up exactly, which is the answer where a ranked search would
// put twenty vaguely related files in front of it.
type Definition = {
  symbol: string
  kind: string
  line: number
  documentId: string
  externalId: string
  title: string
}

// What a document search found, and how it found it: Meaningful is false
// on a deployment with no embedding model, which finds what the words
// find and misses a paraphrase sharing none of them.
type FoundDocuments = { passages: Passage[]; definitions: Definition[]; meaningful: boolean }

// A slice of one document: where in the text it starts, how long the whole
// document is, and where the read that carries on from it begins -- zero
// where this slice reached the end.
type DocumentExtract = {
  documentId: string
  externalId: string
  title: string
  kind: string
  author: string
  source: string
  happenedAt?: string | null
  from: number
  text: string
  total: number
  next: number
}

// A picture or a file a record came with, cited by a fact on this page:
// what it is, where it was posted, and where its bytes are served from.
// The path is empty for a file whose bytes this server does not hold,
// which is a name with nothing to open behind it.
type Attachment = {
  documentId: string
  name: string
  contentType: string
  channel: string
  thread: string
  path: string
}

type Page = {
  node: Node
  facts: Fact[]
  folded: FoldedFact[]
  children: Node[]
  attachments: Attachment[]
  contact?: { id: string; name: string; emails: string[]; organization: string } | null
}

// PAGE_SIZE is how many pages a folder shows at a time. Fifty is a screen
// and a half; a folder of two thousand projects is "show fifty more",
// not a two-thousand-row scroll.
const PAGE_SIZE = 50

// DOCUMENT_PASSAGES is how many passages one search asks for. Enough that
// the question is usually answered from the list, few enough that the
// dialog is something to scroll rather than something to read.
const DOCUMENT_PASSAGES = 20

// documentGroups gathers the passages under the document each came from,
// keeping the order the search ranked them in: a document's best passage
// decides where it sits, and its other passages follow it rather than
// scattering down the list under a heading repeated four times.
function documentGroups(passages: Passage[]): { document: Passage; passages: Passage[] }[] {
  const groups: { document: Passage; passages: Passage[] }[] = []
  const byDocument = new Map<string, { document: Passage; passages: Passage[] }>()
  for (const passage of passages) {
    const gathered = byDocument.get(passage.documentId)
    if (gathered) {
      gathered.passages.push(passage)
      continue
    }
    const group = { document: passage, passages: [passage] }
    byDocument.set(passage.documentId, group)
    groups.push(group)
  }
  return groups
}

// documentDetail is the line under a document's title: who wrote it, when
// it happened, and which source read it. A document that says none of
// those gets no line rather than a line of separators.
function documentDetail(document: { author: string; source: string; happenedAt?: string | null }): string {
  const when = document.happenedAt ? new Date(document.happenedAt).toLocaleDateString() : ''
  return [document.author, when, document.source].filter((part) => part !== '').join(' · ')
}

// documentName is what a document is called on screen: its title, and the
// identifier its source knows it by where it has no title -- a path in a
// checkout is a name, and an empty heading is not.
function documentName(document: { title: string; externalId: string }): string {
  return document.title || document.externalId
}

// rootOf is the folder a path is filed under: its first segment. The
// roots are whatever the graph has -- the eight it starts with, and any
// a source has been filed under since -- so nothing here is a list.
function rootOf(path: string): string {
  return path.split('/')[0]
}

// parentOf is the page a path is filed under, and empty for a root. A
// root is the shape of the graph rather than a page about anything, so
// there is nowhere above it to move it to.
function parentOf(path: string): string {
  const cut = path.lastIndexOf('/')
  return cut < 0 ? '' : path.slice(0, cut)
}

// depthOf is how far down the graph a path is. It is what decides which
// way the navigator slides: a deeper folder comes in from the right, and
// anything shallower -- or sideways, from a lookup -- comes back.
function depthOf(path: string): number {
  return path === '' ? 0 : path.split('/').length
}

// folderName is the navigator's heading for a folder: the person's own
// name on their page, the node's name where the node it is showing has
// arrived, and the last segment with a capital where it has not. The
// heading is wanted before a query for the folder itself could answer
// it, and a page open in the column beside it only names its own folder
// half the time.
function folderName(node: Node | null, path: string, me: string): string {
  if (path === 'self' && me) return me
  const name = node && node.path === path ? node.name || path : path.split('/').pop() || path
  return name.charAt(0).toUpperCase() + name.slice(1)
}

// nameOf is what one row is called: the person's own name on their own
// page, the node's name where it says more than the path does, and the
// last segment of the path where it does not.
function nameOf(node: Node, me: string): string {
  if (node.path === 'self' && me) return me
  const segment = node.path.split('/').pop() || node.path
  const name = node.name || segment
  return name.charAt(0).toUpperCase() + name.slice(1)
}

// What the agent knows, as the person reads and corrects it.
//
// Two columns: the navigator, and one page. The navigator is walked one
// folder at a time -- what is inside a folder slides in from the right,
// the way back slides it out again -- because the graph is a tree of no
// fixed depth, and a column per level runs out of window at three. On a
// phone the two show one at a time, and the URL says which, so the
// browser's own Back agrees with the way back on the screen.
//
// The lookup box at the top is the primary way in. A graph of thousands
// of pages is not browsed; it is looked up, and the list is for when you
// do not know the name yet.
export function KnowledgePage() {
  const { t } = useTranslation()
  const toast = useToast()
  const desktop = useIsDesktop()
  const me = useSession().name || ''
  // The URL is the graph path: /settings/knowledge/projects/portal is
  // that page, /settings/knowledge/projects is that folder's list, and
  // nothing is the top. Which of the two a path is, is the node's own
  // kind and not the shape of the path: a folder can be nested, and a
  // page can have pages filed under it, so counting slashes gets both
  // wrong.
  const navigate = useNavigate()
  const at = (useParams()['*'] || '').replace(/^\/+|\/+$/g, '')
  // How many columns there is room for, measured on the page itself
  // rather than the window: the sidebar takes a third of a laptop, and a
  // window that fits two columns with it closed does not with it open.
  const [frameElement, setFrameElement] = useState<HTMLDivElement | null>(null)
  const width = useContainerWidth(frameElement)
  const onePane = (width === null ? (desktop ? 2 : 1) : width >= 760 ? 2 : 1) === 1
  const [filter, setFilter] = useState('')
  const search = filter.trim()
  // Whether the recall preview is open. A dialog rather than a third box
  // in the column: the answer is a list of pages with their facts under
  // them, which is more than fits beside a folder list on a phone.
  const [recalling, setRecalling] = useState(false)
  // Whether the search over what was read is open. A dialog for the same
  // reason, and a second one rather than a tab inside the first: recall
  // answers what a turn would carry, this answers what the sources hold,
  // and neither is a step on the way to the other.
  const [searchingDocuments, setSearchingDocuments] = useState(false)
  // The page the navigator is showing the inside of, when the URL alone
  // would have shown the folder it is filed in. A page with children is
  // two things at one address -- something to read and something to walk
  // through -- and the chevron is what says which was meant. Kept
  // against the path it belongs to, so that going back to it comes back
  // to the list and going anywhere else does not.
  const [walkedInto, setWalkedInto] = useState('')

  // Desktop with nothing open shows the person's own page.
  const open = at || (onePane ? '' : 'self')

  // The answer carries the path it answered for. Until the new one
  // arrives the old page is still in hand, and deciding from it whether
  // this path is a folder would put the wrong column up for as long as
  // the query takes.
  const page = useQuery(
    async () => ({
      path: open,
      found: open ? (await graphql<{ AgentGraphPage: Page | null }>(PAGE, { path: open })).AgentGraphPage : null,
    }),
    [open],
    { refresh: false },
  )
  const answered = page.data?.path === open
  const current = answered ? (page.data?.found ?? null) : null
  const node = current?.node ?? null
  // The folder being listed is not always the page in hand -- a page
  // shows its parent's list -- and the header wants the folder's name,
  // not the slug it is filed under.
  const folderPath = at === '' ? '' : node?.kind === 'folder' ? at : parentOf(at)
  const folderNode = useQuery(
    async () =>
      folderPath && folderPath !== node?.path
        ? ((await graphql<{ AgentGraphPage: Page | null }>(PAGE, { path: folderPath })).AgentGraphPage?.node ?? null)
        : null,
    [folderPath, node?.path],
    { refresh: false },
  )
  const isFolder = node?.kind === 'folder'
  // Which folder the navigator is listing, and null while that is still
  // a question: the top, the page in the address if it was walked into,
  // the folder in the address, and otherwise the folder that page is
  // filed in, with the page's own row marked in it.
  const folder = at === '' || walkedInto === at ? at : !answered ? null : isFolder ? at : parentOf(at)
  // A page walked into keeps the one pane for its list; on two columns
  // it is read on the right while its children are walked on the left.
  const showingPage = open !== '' && !isFolder && !(onePane && folder === at)
  const showingDetail = onePane ? showingPage : open !== ''

  const found = useQuery(
    () =>
      search
        ? graphql<{ SearchAgentGraph: { nodes: Node[]; facts: { fact: Fact; path: string; name: string }[] } }>(
            SEARCH,
            { query: search, first: 60 },
          )
        : Promise.resolve(null),
    [search],
    { refresh: false },
  )

  // Going somewhere is one move: the address changes, the lookup is put
  // away, and the navigator is told whether the chevron or the name was
  // pressed -- the two lead to the same address and mean different
  // things once it is open.
  const goTo = useCallback(
    (next: string, into: boolean) => {
      // Only ever set here, never cleared: clearing it in the same
      // breath as the address change landed one render early, and for
      // that render the navigator thought the page in hand had not been
      // walked into, slid back to its parent, and slid forward again
      // when the new address arrived. It is cleared below, once the
      // address has moved on from it.
      if (into) setWalkedInto(next)
      setFilter('')
      navigate('/settings/knowledge' + (next ? '/' + next : ''))
    },
    [navigate],
  )
  useEffect(() => {
    if (walkedInto && walkedInto !== at) setWalkedInto('')
  }, [at, walkedInto])
  const goPage = useCallback((next: string) => goTo(next, false), [goTo])
  // Up is the folder this one is filed in, walked into rather than read:
  // it may be a page with children itself.
  const goUp = useCallback(() => goTo(parentOf(folder ?? ''), true), [goTo, folder])

  const recall = recalling ? (
    <RecallDialog
      onSelect={(next: string) => {
        setRecalling(false)
        goPage(next)
      }}
      onClose={() => setRecalling(false)}
    />
  ) : null

  const documents = searchingDocuments ? <DocumentsDialog onClose={() => setSearchingDocuments(false)} /> : null

  // The toolbar above the list, the one the mail list has: the box that
  // searches the pages by name, beside the ways in that are not about a
  // name. A div rather than a form because this filters as it is typed --
  // there is nothing for Enter to submit.
  //
  // One row, as the mail list has it. The labels are a word each for that
  // reason: as a question and a phrase they were 323 pixels of a 359 pixel
  // toolbar, which left nowhere for the box and forced the two apart. The
  // long wording is still what each dialog is titled, where there is room
  // for it and where a sentence reads better than a noun.
  const lookup = (
    <div className="list-toolbar">
      <input
        type="search"
        value={filter}
        placeholder={t('knowledge.find')}
        aria-label={t('knowledge.find')}
        onChange={(event) => setFilter(event.target.value)}
      />
      {/* The other three questions the column answers, and they are alike:
          not what a page is called, but what a question would carry into a
          turn, what the agent read to get there, and what a page sits
          among. One segmented control because they are one set, the same
          control the mail list narrows itself with. */}
      <div className="segmented" role="group" aria-label={t('knowledge.waysIn')}>
        <button type="button" onClick={() => setRecalling(true)} title={t('knowledge.recall.title')}>
          {t('knowledge.recall.button')}
        </button>
        <button type="button" onClick={() => setSearchingDocuments(true)} title={t('knowledge.documents.title')}>
          {t('knowledge.documents.button')}
        </button>
        <Link to="/settings/knowledge/explore" title={t('knowledge.explore.go')}>
          {t('knowledge.explore.title')}
        </Link>
      </div>
    </div>
  )

  const pageName = current ? nameOf(current.node, me) : null

  // One column at a time, so the breadcrumb is the way back: the folder
  // above the page, the top above the folder. With the columns side by
  // side the page is still Knowledge, and the trail says so.
  // While the page walked into is still being answered, folder is null
  // and the header must not flash "Knowledge": it keeps saying what it
  // said until the answer names the new folder.
  const lastFolderLabel = useRef(t('knowledge.root'))
  const folderLabel =
    folder === null
      ? lastFolderLabel.current
      : folder
        ? folderName(folderNode.data ?? node, folder, me)
        : t('knowledge.root')
  useEffect(() => {
    if (folder !== null) lastFolderLabel.current = folderLabel
  }, [folder, folderLabel])
  useBreadcrumbDetail(
    onePane ? (showingPage ? (folder ? folderLabel : (pageName ?? '…')) : folder ? folderLabel : null) : null,
    onePane && showingPage && folder ? (pageName ?? '…') : null,
  )

  const list = search ? (
    <SearchResults found={found.data?.SearchAgentGraph} loading={found.loading} me={me} onSelect={goPage} />
  ) : (
    <Navigator
      path={folder}
      label={folderLabel}
      selected={showingPage ? open : ''}
      me={me}
      onOpen={goPage}
      onInto={(next: string) => goTo(next, true)}
      onUp={goUp}
    />
  )

  const detail = showingDetail ? (
    <>
      {page.loading && !current ? <Loading /> : null}
      {page.error ? <ErrorMessage error={page.error} /> : null}
      {answered && !current ? (
        <div className="card">
          <h3>{open}</h3>
          <SettingsEmpty>{t('knowledge.noPage')}</SettingsEmpty>
        </div>
      ) : null}
      {current ? (
        <PageView
          page={current}
          onChanged={() => void page.reload()}
          onSelect={goPage}
          onFailed={(caught: unknown) => toast.failed(messageOf(caught))}
          onDone={(said: string) => toast.done(said)}
        />
      ) : null}
    </>
  ) : null

  // The toolbar is inside the panel rather than above it, so it stays put
  // while the rows under it scroll.
  const column = (
    <div className="card knowledge-list">
      {lookup}
      <div className="knowledge-list-rows">{list}</div>
    </div>
  )

  if (onePane) {
    // One column at a time. Which one is in the URL, so Back is Back,
    // and the breadcrumb on the bar is the way up out of the navigator.
    return (
      <div ref={setFrameElement} className="knowledge-phone">
        {showingDetail ? detail : column}
        {recall}
        {documents}
      </div>
    )
  }

  return (
    <div ref={setFrameElement} className="knowledge-columns">
      <div className="knowledge-column knowledge-column-navigator">{column}</div>
      <div className="knowledge-column knowledge-page">{detail}</div>
      {recall}
      {documents}
    </div>
  )
}

// useContainerWidth is how wide an element is, kept up to date as it
// changes, and null before it has been measured.
//
// The element is state rather than a ref: the one-pane and two-column
// layouts are different elements, and an observer left on the first
// stopped reporting the moment the page switched, so a window widened
// after that never went back to two columns. A callback ref sets the
// state, and the observer follows whichever element is there.
function useContainerWidth(element: HTMLDivElement | null): number | null {
  const [width, setWidth] = useState<number | null>(null)
  useEffect(() => {
    if (!element) return
    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) setWidth(entry.contentRect.width)
    })
    observer.observe(element)
    setWidth(element.getBoundingClientRect().width)
    return () => observer.disconnect()
  }, [element])
  return width
}

// SLIDE_MILLISECONDS is how long a folder takes to leave, and matches
// the animations in the stylesheet: long enough to be read as a
// direction, short enough that walking four folders deep is not a wait.
const SLIDE_MILLISECONDS = 200

// A row of a folder: the page, the line that tells it from the row above
// it, and how much is filed inside it -- null where that could not be
// asked, which is not the same as nothing.
type Row = { node: Node; hint: string; children: number | null }

// Navigator is the hierarchy, one folder at a time.
//
// Two lists live in the frame while one is replacing the other: the one
// being left slides out and the one being entered slides in, and the
// frame drops the old one when the animation is over. They are keyed by
// path so that the list on its way out keeps the rows it had already
// fetched rather than remounting and flashing "loading" as it goes.
function Navigator({
  path,
  label,
  selected,
  me,
  onOpen,
  onInto,
  onUp,
}: {
  path: string | null
  label: string
  selected: string
  me: string
  onOpen: (path: string) => void
  onInto: (path: string) => void
  onUp: () => void
}) {
  const { t } = useTranslation()
  const [shown, setShown] = useState(path)
  const [leaving, setLeaving] = useState<{ path: string; deeper: boolean } | null>(null)
  // The frame's height is animated by hand while a folder slides in. The
  // folder coming in mounts empty and fills as its rows arrive, and a
  // frame sized by either list alone either cut the tall one to the short
  // one's height or collapsed to a loading line and sprang back. Held at
  // the taller of the two while the slide runs, then eased to the height
  // of what came in.
  const frame = useRef<HTMLDivElement>(null)
  const entering = useRef<HTMLDivElement>(null)
  const settle = useRef<number | null>(null)
  // The frame's height as it stood before the slide: once both lists are
  // lifted out of the flow the frame measures as nothing, so it has to
  // be remembered from the render before, not read when the slide starts.
  const standing = useRef(0)
  useLayoutEffect(() => {
    if (!leaving && frame.current) {
      standing.current = frame.current.offsetHeight
    }
  })
  useEffect(() => {
    const box = frame.current
    if (!box) return
    if (!leaving) {
      // The slide is over: ease to the new list's height, then let the
      // frame size itself again so a list that grows later is not clipped.
      const target = entering.current?.offsetHeight
      if (target !== undefined && box.style.height) {
        box.style.height = `${target}px`
        settle.current = window.setTimeout(() => {
          box.style.height = ''
          box.style.transition = ''
        }, SLIDE_MILLISECONDS)
        return () => {
          if (settle.current !== null) window.clearTimeout(settle.current)
        }
      }
      return
    }
    if (settle.current !== null) window.clearTimeout(settle.current)
    const from = standing.current
    box.style.transition = `height ${SLIDE_MILLISECONDS}ms ease-out`
    box.style.height = `${from}px`
    const watched = entering.current
    if (!watched) return
    const observer = new ResizeObserver(() => {
      box.style.height = `${Math.max(from, watched.offsetHeight)}px`
    })
    observer.observe(watched)
    return () => observer.disconnect()
  }, [leaving])

  useEffect(() => {
    if (path === null || path === shown) return
    // The first folder is not a change of folder: a deep link is where
    // the person already is, and sliding it in from the right would be
    // saying they had just walked there.
    const still = shown === null || window.matchMedia('(prefers-reduced-motion: reduce)').matches
    setLeaving(still ? null : { path: shown, deeper: depthOf(path) > depthOf(shown) })
    setShown(path)
  }, [path, shown])

  useEffect(() => {
    if (!leaving) return
    const timer = window.setTimeout(() => setLeaving(null), SLIDE_MILLISECONDS)
    return () => window.clearTimeout(timer)
  }, [leaving])

  const way = leaving?.deeper ? 'deeper' : 'back'
  const panels = leaving
    ? [
        { path: leaving.path, className: `knowledge-navigator-panel leaving ${way}` },
        { path: shown ?? '', className: `knowledge-navigator-panel entering ${way}` },
      ]
    : [{ path: shown ?? '', className: 'knowledge-navigator-panel' }]

  return (
    <div className="knowledge-navigator">
      <div className="knowledge-navigator-header">
        {shown ? (
          <button
            type="button"
            className="icon-action"
            title={t('knowledge.back')}
            aria-label={t('knowledge.back')}
            onClick={onUp}
          >
            <ChevronLeftIcon size={16} />
          </button>
        ) : null}
        <span className="knowledge-navigator-name">{label}</span>
      </div>
      {shown === null ? (
        <Loading />
      ) : (
        <div className="knowledge-navigator-frame" ref={frame}>
          {panels.map((panel) => (
            <div key={panel.path} className={panel.className} ref={panel.path === (shown ?? '') ? entering : undefined}>
              <NavigatorList path={panel.path} selected={selected} me={me} onOpen={onOpen} onInto={onInto} />
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

// NavigatorList is what is filed inside one folder, fifty at a time,
// each row with enough of a hint to tell it from its neighbours.
function NavigatorList({
  path,
  selected,
  me,
  onOpen,
  onInto,
}: {
  path: string
  selected: string
  me: string
  onOpen: (path: string) => void
  onInto: (path: string) => void
}) {
  const { t } = useTranslation()
  const [rows, setRows] = useState<Row[]>([])
  const [total, setTotal] = useState(0)
  // Loading before anything has been asked for, because the first render
  // happens before the effect that asks: "nothing here yet" for one
  // frame said the folder was empty, which is the one thing it was not.
  const [loading, setLoading] = useState(true)
  const [problem, setProblem] = useState<unknown>(null)

  const load = useCallback(
    async (offset: number) => {
      setLoading(true)
      try {
        const result = await graphql<{
          AgentGraphChildren: { rows: { node: Node; hint: string; children: number }[]; total: number }
        }>(CHILDREN, { path, first: PAGE_SIZE, offset })
        const batch = result.AgentGraphChildren.rows
        // Ordered within the batch it arrived in rather than across the
        // whole list: a folder that turned up in the second fifty
        // jumping over pages somebody has already read past is worse
        // than its being where the server put it.
        const fresh = ordered(batch.map((row) => ({ ...row, children: row.children })))
        setRows((before) => (offset === 0 ? fresh : [...before, ...fresh]))
        setTotal(result.AgentGraphChildren.total)
      } catch (caught) {
        setProblem(caught)
      } finally {
        setLoading(false)
      }
    },
    [path],
  )
  useEffect(() => {
    setRows([])
    setTotal(0)
    setProblem(null)
    void load(0)
  }, [load])
  // The open page's row is in the list even when it sorts past the
  // first fifty: a page opened from a link is otherwise selected in a
  // list that does not show it.
  useEffect(() => {
    if (loading || !selected || rows.length === 0 || rows.length >= total) return
    if (rows.some((row) => row.node.path === selected)) return
    if (rows.length >= PAGE_SIZE * 10) return
    void load(rows.length)
  }, [loading, selected, rows, total, load])

  if (problem) return <ErrorMessage error={problem} />
  if (loading && rows.length === 0) return <Loading />
  if (rows.length === 0) return <SettingsEmpty>{t('knowledge.emptyFolder')}</SettingsEmpty>
  return (
    <>
      <ul className="knowledge-rows">
        {rows.map((row) => (
          <li key={row.node.id}>
            <NavigatorRow row={row} selected={row.node.path === selected} me={me} onOpen={onOpen} onInto={onInto} />
          </li>
        ))}
      </ul>
      {rows.length < total ? (
        <button type="button" className="link" disabled={loading} onClick={() => void load(rows.length)}>
          {t('knowledge.showMore', { count: Math.min(PAGE_SIZE, total - rows.length) })}
        </button>
      ) : null}
    </>
  )
}

// NavigatorRow is one row of a folder. The name and the line under it
// open the page; the chevron beside them walks into what is filed under
// it. Two presses because they are two places: a page with children is
// both something to read and something to walk through, and a row that
// did only one of them made the other unreachable.
function NavigatorRow({
  row,
  selected,
  me,
  onOpen,
  onInto,
}: {
  row: Row
  selected: boolean
  me: string
  onOpen: (path: string) => void
  onInto: (path: string) => void
}) {
  const { t } = useTranslation()
  const name = nameOf(row.node, me)
  return (
    <div className={selected ? 'knowledge-row selected' : 'knowledge-row'}>
      {/* A folder is a place rather than a page to read, so its name and
          its chevron are the same door. They lead to the same address
          either way; saying so here is what stops the press waiting for
          a query to come back and say which it was. */}
      <button
        type="button"
        className="knowledge-row-open"
        onClick={() => (row.node.kind === 'folder' ? onInto(row.node.path) : onOpen(row.node.path))}
      >
        <RowText node={row.node} name={name} hint={row.hint} />
      </button>
      {row.node.kind === 'folder' && row.children !== null ? (
        <span className="knowledge-row-count">{row.children}</span>
      ) : null}
      {opensInto(row) ? (
        <button
          type="button"
          className="knowledge-row-into"
          title={t('knowledge.into', { name })}
          aria-label={t('knowledge.into', { name })}
          onClick={() => onInto(row.node.path)}
        >
          <ChevronRightIcon size={14} />
        </button>
      ) : null}
    </div>
  )
}

// opensInto is whether a row can be walked into. A folder always can,
// empty or not, because that is what a folder is for; anything else can
// when something is filed under it, which a page may have now.
function opensInto(row: Row): boolean {
  return row.node.kind === 'folder' || (row.children ?? 0) > 0
}

// ordered is the order the navigator shows a folder's rows in: the
// person's own page and anything pinned stay where the server put them,
// then what opens into something, then the pages that do not. Two
// subfolders buried alphabetically among forty pages is a folder whose
// shape nobody finds. Sorting is stable, so within each of the three the
// server's own order -- pinned, then by name -- survives.
function ordered(rows: Row[]): Row[] {
  return [...rows].sort((left, right) => rankOf(left) - rankOf(right))
}

function rankOf(row: Row): number {
  if (row.node.kind === 'self' || row.node.pinned) return 0
  return opensInto(row) ? 1 : 2
}

// RowText is a row's two lines: its name, and the one line that tells it
// from the row above -- the opening where there is one, the kind where
// there is not. A list of bare names is the thing a person cannot use.
function RowText({ node, name, hint }: { node: Node; name: string; hint?: string }) {
  const { t } = useTranslation()
  const line = hint || node.summary
  const shown = line ? cut(line, 90) : t(`knowledge.kind.${node.kind}` as 'knowledge.kind.person')
  return (
    <span className="knowledge-row-text">
      <span className="knowledge-row-name">
        {name}
        {node.pinned ? <PinIcon size={12} /> : null}
      </span>
      <span className="knowledge-row-hint">{shown}</span>
    </span>
  )
}

// PageRow is one page as the lookup lists it: the whole row opens it,
// because a result is somewhere to go rather than somewhere to walk
// through.
function PageRow({
  node,
  hint,
  me,
  onSelect,
}: {
  node: Node
  hint?: string
  me: string
  onSelect: (path: string) => void
}) {
  return (
    <button type="button" className="knowledge-row" onClick={() => onSelect(node.path)}>
      <RowText node={node} name={nameOf(node, me)} hint={hint} />
      <ChevronRightIcon size={14} />
    </button>
  )
}

// SearchResults is what the lookup found, pages then facts, grouped by
// the folder each is filed under so a hit in Projects reads as one.
function SearchResults({
  found,
  loading,
  me,
  onSelect,
}: {
  found?: { nodes: Node[]; facts: { fact: Fact; path: string; name: string }[] } | null
  loading: boolean
  me: string
  onSelect: (path: string) => void
}) {
  const { t } = useTranslation()
  if (loading && !found) return <Loading />
  if (!found || (found.nodes.length === 0 && found.facts.length === 0)) {
    return <SettingsEmpty>{t('knowledge.nothingFound')}</SettingsEmpty>
  }
  const groups = new Map<string, Node[]>()
  for (const node of found.nodes) {
    const root = rootOf(node.path)
    groups.set(root, [...(groups.get(root) ?? []), node])
  }
  return (
    <>
      {[...groups.entries()].map(([root, nodes]) => (
        <div key={root}>
          <p className="knowledge-list-heading">{root.charAt(0).toUpperCase() + root.slice(1)}</p>
          <ul className="knowledge-rows">
            {nodes.map((node) => (
              <li key={node.id}>
                <PageRow node={node} me={me} onSelect={onSelect} />
              </li>
            ))}
          </ul>
        </div>
      ))}
      {found.facts.length > 0 ? (
        <>
          <p className="knowledge-list-heading">{t('knowledge.facts')}</p>
          <ul className="knowledge-rows">
            {found.facts.map((row) => (
              <li key={row.fact.id}>
                <button type="button" className="knowledge-row" onClick={() => onSelect(row.path)}>
                  <span className="knowledge-row-text">
                    <span className="knowledge-row-name">{row.name || row.path}</span>
                    <span className="knowledge-row-hint">
                      #{row.fact.number} {cut(row.fact.text, 110)}
                    </span>
                  </span>
                  <ChevronRightIcon size={14} />
                </button>
              </li>
            ))}
          </ul>
        </>
      ) : null}
    </>
  )
}

// RecallDialog is what a question would carry into a turn.
//
// The lookup beside it answers "is this written down"; this answers "will
// it be read". They are different questions: a page can be on the graph
// and still never reach a turn about it, and the only way to see that
// before this was to ask the agent and read what it cited. Nothing is
// said to a model here and nothing is marked used, so it can be asked
// over and over while a page is being corrected.
function RecallDialog({ onSelect, onClose }: { onSelect: (path: string) => void; onClose: () => void }) {
  const { t } = useTranslation()
  const [question, setQuestion] = useState('')
  // The question the pages in hand answer, so that typing on after an
  // answer puts the old one away rather than leaving it under a question
  // it no longer belongs to.
  const [asked, setAsked] = useState<string | null>(null)
  const [pages, setPages] = useState<RecalledPage[]>([])
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  const wanted = question.trim()

  const ask = () => {
    if (wanted === '' || busy) return
    setBusy(true)
    setProblem(null)
    void (async () => {
      try {
        const answer = await graphql<{ RecallAgentMemory: { pages: RecalledPage[] } }>(RECALL, { question: wanted })
        setPages(answer.RecallAgentMemory.pages)
        setAsked(wanted)
      } catch (caught) {
        setProblem(messageOf(caught))
        setAsked(null)
      } finally {
        setBusy(false)
      }
    })()
  }

  return (
    <FormDialog
      title={t('knowledge.recall.title')}
      submitLabel={t('knowledge.recall.ask')}
      busy={busy}
      error={problem}
      canSubmit={wanted !== ''}
      // Nothing is changed by asking, so the way out is Close: Cancel
      // would name something that is not being cancelled.
      closeLabel={t('common.close')}
      onClose={onClose}
      onSubmit={ask}
    >
      <p className="muted">{t('knowledge.recall.hint')}</p>
      <label>
        <span>{t('knowledge.recall.question')}</span>
        <input
          value={question}
          placeholder={t('knowledge.recall.placeholder')}
          onChange={(event) => setQuestion(event.target.value)}
        />
      </label>
      {asked !== null && asked === wanted ? (
        pages.length === 0 ? (
          // Carrying nothing is an ordinary answer -- a question about
          // something it has never been told -- so it is said quietly
          // here rather than raised as a failure.
          <p className="muted">{t('knowledge.recall.nothing')}</p>
        ) : (
          <ul className="recall-pages">
            {pages.map((page) => (
              <li key={page.path}>
                <button type="button" className="link" onClick={() => onSelect(page.path)}>
                  {page.path}
                </button>
                <ul className="recall-facts">
                  {page.facts.map((fact) => (
                    <li key={fact.number}>
                      <span className="mono">#{fact.number}</span> {fact.text}
                    </li>
                  ))}
                </ul>
              </li>
            ))}
          </ul>
        )
      ) : null}
    </FormDialog>
  )
}

// DocumentsDialog is what the sources read, searched and then read back.
//
// The graph beside it is what the agent made of its reading -- pages,
// facts, links -- and this is the reading itself: the commit message, the
// chat post, the file. A person checking a fact wants the sentence it came
// from, and before this the only way to that sentence was to ask the agent
// and pay for the turn.
//
// Two things in one dialog, because they are one errand. The results are
// the passages that matched, under the document each came from; Read opens
// that document from its beginning in the same dialog, a slice at a time,
// and the way back is the way back to the results rather than out.
function DocumentsDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')
  const [sourceId, setSourceId] = useState('')
  // The search the passages in hand answer -- the words and the source --
  // so that typing on, or narrowing to another source, puts the old answer
  // away rather than leaving it under a question it no longer belongs to.
  const [asked, setAsked] = useState<{ words: string; sourceId: string } | null>(null)
  const [found, setFound] = useState<FoundDocuments | null>(null)
  // The document being read, and null while the results are what is shown.
  const [reading, setReading] = useState<DocumentExtract | null>(null)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  const wanted = query.trim()

  const sources = useQuery(
    () => graphql<{ ListAgentKnowledgeSources: { id: string; name: string }[] }>(KNOWLEDGE_SOURCES, {}),
    [],
    { refresh: false },
  )
  const sourceOptions = [
    { value: '', label: t('knowledge.documents.anySource') },
    ...(sources.data?.ListAgentKnowledgeSources ?? []).map((source) => ({ value: source.id, label: source.name })),
  ]

  const search = () => {
    if (wanted === '' || busy) return
    setBusy(true)
    setProblem(null)
    void (async () => {
      try {
        // The source is left out of the variables rather than sent empty:
        // any source is the absence of a filter, not a filter on nothing.
        const variables: Record<string, unknown> = { query: wanted, first: DOCUMENT_PASSAGES }
        if (sourceId !== '') variables.sourceId = sourceId
        const answer = await graphql<{ SearchAgentDocuments: FoundDocuments }>(DOCUMENT_SEARCH, variables)
        setFound(answer.SearchAgentDocuments)
        setAsked({ words: wanted, sourceId })
      } catch (caught) {
        setProblem(messageOf(caught))
        setAsked(null)
      } finally {
        setBusy(false)
      }
    })()
  }

  // Opening a document at its beginning rather than at the passage that was
  // found: the passage is already on screen, and what a person opens the
  // document for is what is around it.
  const read = (documentId: string) => {
    if (busy) return
    setBusy(true)
    setProblem(null)
    void (async () => {
      try {
        const answer = await graphql<{ ReadAgentDocument: DocumentExtract | null }>(DOCUMENT_READ, {
          documentId,
          from: 0,
        })
        if (answer.ReadAgentDocument === null) {
          setProblem(t('knowledge.documents.gone'))
          return
        }
        setReading(answer.ReadAgentDocument)
      } catch (caught) {
        setProblem(messageOf(caught))
      } finally {
        setBusy(false)
      }
    })()
  }

  // Reading on appends the following slice to what is already on screen, so
  // that the way back up a long document is the scroll a reader already has
  // rather than a button that pages away from what they just read.
  const readOn = () => {
    if (reading === null || reading.next <= 0 || busy) return
    setBusy(true)
    setProblem(null)
    const documentId = reading.documentId
    const from = reading.next
    void (async () => {
      try {
        const answer = await graphql<{ ReadAgentDocument: DocumentExtract | null }>(DOCUMENT_READ, {
          documentId,
          from,
        })
        const slice = answer.ReadAgentDocument
        if (slice === null) {
          setProblem(t('knowledge.documents.gone'))
          return
        }
        setReading((previous) =>
          previous === null ? slice : { ...slice, from: previous.from, text: previous.text + slice.text },
        )
      } catch (caught) {
        setProblem(messageOf(caught))
      } finally {
        setBusy(false)
      }
    })()
  }

  // Coming back to the top of the dialog when the reader opens. The scrim
  // is what scrolls, so a document opened from the foot of a long list of
  // results would otherwise start halfway down its own text. A callback ref
  // fires when the reader is put up and not when a slice is appended to it,
  // which is exactly the difference wanted: Read starts at the top, Read on
  // stays where the reader is.
  const toTheTop = useCallback((element: HTMLDivElement | null) => {
    element?.closest('.dialog-scrim')?.scrollTo({ top: 0 })
  }, [])

  // What is shown is only ever the answer to what is typed. An answer to
  // an older question is kept -- it costs nothing and comes back if the
  // words come back -- and not drawn under the new one.
  const showing = asked !== null && asked.words === wanted && asked.sourceId === sourceId ? found : null

  const results = (
    <>
      <p className="muted">{t('knowledge.documents.hint')}</p>
      <label>
        <span>{t('knowledge.documents.query')}</span>
        <input
          value={query}
          placeholder={t('knowledge.documents.placeholder')}
          onChange={(event) => setQuery(event.target.value)}
        />
      </label>
      <label>
        <span>{t('knowledge.documents.source')}</span>
        <Select
          block
          value={sourceId}
          label={t('knowledge.documents.source')}
          options={sourceOptions}
          onChange={setSourceId}
        />
      </label>
      {showing === null ? null : showing.passages.length === 0 && showing.definitions.length === 0 ? (
        // Finding nothing is an ordinary answer -- a question about
        // something no source has read -- so it is said quietly here
        // rather than raised as a failure.
        <p className="muted">{t('knowledge.documents.nothing')}</p>
      ) : (
        <>
          {showing.definitions.length > 0 ? (
            <>
              <p className="muted document-section">{t('knowledge.documents.definitions')}</p>
              <ul className="document-definitions">
                {showing.definitions.map((definition) => (
                  <li key={`${definition.documentId}:${definition.symbol}:${definition.line}`}>
                    <span className="mono">{definition.symbol}</span> · {definition.kind} ·{' '}
                    {documentName({ title: definition.title, externalId: definition.externalId })}
                  </li>
                ))}
              </ul>
            </>
          ) : null}
          <ul className="document-hits">
            {documentGroups(showing.passages).map((group) => {
              const name = documentName(group.document)
              const detail = documentDetail(group.document)
              return (
                <li key={group.document.documentId}>
                  <div className="document-hit-heading">
                    <span className="document-hit-name">{name}</span>
                    {/* One action on the heading, so it is a word rather
                        than an icon. The label names the document: "Read"
                        on its own is the same word six times over to
                        anybody who cannot see which heading it is under. */}
                    <button
                      type="button"
                      className="link"
                      aria-label={t('knowledge.documents.readOne', { title: name })}
                      onClick={() => read(group.document.documentId)}
                    >
                      {t('knowledge.documents.read')}
                    </button>
                  </div>
                  {detail === '' ? null : <p className="muted document-hit-detail">{detail}</p>}
                  {group.passages.map((passage) => (
                    <p key={passage.number} className="document-hit-text">
                      {passage.text}
                    </p>
                  ))}
                </li>
              )
            })}
          </ul>
          {showing.meaningful ? null : <p className="muted document-section">{t('knowledge.documents.wordsOnly')}</p>}
        </>
      )}
    </>
  )

  const detail = reading === null ? '' : documentDetail(reading)
  const reader =
    reading === null ? null : (
      <div ref={toTheTop} className="document-read">
        {detail === '' ? null : <p className="muted document-hit-detail">{detail}</p>}
        {reading.total === 0 ? (
          <p className="muted">{t('knowledge.documents.blank')}</p>
        ) : (
          <div className="document-read-text">{reading.text}</div>
        )}
        {reading.next > 0 ? (
          <p className="muted document-section">
            {t('knowledge.documents.left', { count: reading.total - reading.next })}
          </p>
        ) : null}
      </div>
    )

  return (
    <FormDialog
      title={reading === null ? t('knowledge.documents.title') : documentName(reading)}
      submitLabel={reading === null ? t('knowledge.documents.search') : t('knowledge.documents.readOn')}
      busy={busy}
      // The sources are only the filter's list, so a failure to read them
      // says so here and leaves the search itself alone.
      error={problem ?? (sources.error ? messageOf(sources.error) : null)}
      canSubmit={reading === null ? wanted !== '' : reading.next > 0}
      // Nothing is changed by searching or by reading, so the way out is
      // Close: Cancel would name something that is not being cancelled.
      closeLabel={t('common.close')}
      otherAction={
        reading === null ? undefined : (
          <button type="button" className="link" onClick={() => setReading(null)}>
            {t('knowledge.documents.back')}
          </button>
        )
      }
      onClose={onClose}
      onSubmit={reading === null ? search : readOn}
    >
      {reading === null ? results : reader}
    </FormDialog>
  )
}

function PageView({
  page,
  onChanged,
  onSelect,
  onFailed,
  onDone,
}: {
  page: Page
  onChanged: () => void
  onSelect: (path: string) => void
  onFailed: (caught: unknown) => void
  onDone: (said: string) => void
}) {
  const { t } = useTranslation()
  const [factsShown, setFactsShown] = useState(PAGE_SIZE)
  const me = useSession().name || ''
  const node = page.node
  const [editing, setEditing] = useState(false)
  const [adding, setAdding] = useState<Fact | null | undefined>(undefined)
  const [removing, setRemoving] = useState<Fact | null>(null)
  const [removingPage, setRemovingPage] = useState(false)
  const [moving, setMoving] = useState(false)
  // The fact whose page is being changed, and the page a merge has been
  // asked for. A merge is asked in two steps -- where to, and then
  // whether -- because naming the destination is a typing mistake away
  // from folding this page into the wrong one, and the fold cannot be
  // taken back.
  const [movingFact, setMovingFact] = useState<Fact | null>(null)
  const [merging, setMerging] = useState(false)
  const [mergingInto, setMergingInto] = useState('')
  // Bumped whenever a link is made from here, which is how the drawing
  // below is told to fetch this page's neighbourhood again. A key would
  // redraw it from scratch and throw away the walk somebody is in the
  // middle of; this only refetches.
  const [linked, setLinked] = useState(0)
  const [busy, setBusy] = useState(false)
  const [linking, setLinking] = useState(false)
  const [problem, setProblem] = useState('')

  async function run(document: string, variables: Record<string, unknown>, said: string) {
    setBusy(true)
    setProblem('')
    try {
      await graphql(document, variables)
      onDone(said)
      onChanged()
      return true
    } catch (caught) {
      setProblem(messageOf(caught))
      onFailed(caught)
      return false
    } finally {
      setBusy(false)
    }
  }

  // Moving is not a save: the page answers to a new path afterwards, so
  // the column has to follow it there. Reloading the one it was at would
  // show the empty page that is no longer anything.
  async function move(under: string) {
    setBusy(true)
    setProblem('')
    try {
      const result = await graphql<{ MoveAgentNode: { id: string; path: string } | null }>(MOVE_NODE, {
        path: node.path,
        under,
      })
      onDone(t('knowledge.moved'))
      setMoving(false)
      const now = result.MoveAgentNode?.path
      if (now && now !== node.path) {
        onSelect(now)
      } else {
        onChanged()
      }
    } catch (caught) {
      setProblem(messageOf(caught))
      onFailed(caught)
    } finally {
      setBusy(false)
    }
  }

  // Merging is not a save either: this page is gone when it is over, and
  // what is left of it is on the page it was folded into, which is where
  // the column goes.
  async function merge(into: string) {
    setBusy(true)
    setProblem('')
    try {
      const result = await graphql<{ MergeAgentNodes: { id: string; path: string } | null }>(MERGE_NODES, {
        path: node.path,
        into,
      })
      onDone(t('knowledge.merged'))
      setMergingInto('')
      onSelect(result.MergeAgentNodes?.path || into)
    } catch (caught) {
      setProblem(messageOf(caught))
      onFailed(caught)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <SettingsSection
        card
        title={node.path === 'self' && me ? me : node.name || node.path}
        description={
          <>
            <code className="tag knowledge-path">{node.path}</code>{' '}
            <Tag value={t(`knowledge.kind.${node.kind}` as 'knowledge.kind.person')} />
            {node.pinned ? <Tag value={t('knowledge.pinned')} tone="good" /> : null}
          </>
        }
        action={
          <div className="row-actions">
            <button
              type="button"
              className="icon-action"
              title={t('knowledge.editPage')}
              aria-label={`${node.path}: ${t('knowledge.editPage')}`}
              onClick={() => setEditing(true)}
            >
              <PencilIcon size={16} />
            </button>
            {/* A root is where things are filed rather than a page about
                anything, so there is nowhere above it to move it to. */}
            {parentOf(node.path) ? (
              <button
                type="button"
                className="icon-action"
                title={t('knowledge.movePage')}
                aria-label={`${node.path}: ${t('knowledge.movePage')}`}
                onClick={() => setMoving(true)}
              >
                <MoveIcon size={16} />
              </button>
            ) : null}
            {/* Merging is refused on a root for the same reason moving
                one is: a root is where things are filed rather than a
                page about anything. */}
            {parentOf(node.path) ? (
              <button
                type="button"
                className="icon-action"
                title={t('knowledge.mergePage')}
                aria-label={`${node.path}: ${t('knowledge.mergePage')}`}
                onClick={() => setMerging(true)}
              >
                <MergeIcon size={16} />
              </button>
            ) : null}
            <button
              type="button"
              className={node.pinned ? 'icon-action pinned' : 'icon-action'}
              title={node.pinned ? t('knowledge.unpin') : t('knowledge.pin')}
              aria-label={`${node.path}: ${node.pinned ? t('knowledge.unpin') : t('knowledge.pin')}`}
              onClick={() =>
                void run(
                  SAVE_NODE,
                  { path: node.path, summary: node.summary, pinned: !node.pinned },
                  node.pinned ? t('knowledge.unpinned') : t('knowledge.pinnedIt'),
                )
              }
            >
              {node.pinned ? <PinOffIcon size={16} /> : <PinIcon size={16} />}
            </button>
            {/* The agent, pointed at this page, the way the reader points
                it at a thread: the drawer opens with a chip for it, and
                the person asks it to dig deeper, or to change what the
                page says and links to. */}
            <button
              type="button"
              className="icon-action"
              title={t('knowledge.askAgent')}
              aria-label={`${node.path}: ${t('knowledge.askAgent')}`}
              onClick={() => {
                if (!askAgentAbout({ path: node.path, name: nameOf(node, me) }))
                  window.location.assign('/settings/agent')
              }}
            >
              <SparkIcon size={16} />
            </button>
            <button
              type="button"
              className="icon-action danger"
              title={t('knowledge.forgetPage')}
              aria-label={`${node.path}: ${t('knowledge.forgetPage')}`}
              onClick={() => setRemovingPage(true)}
            >
              <TrashIcon size={16} />
            </button>
          </div>
        }
      >
        {/* The other names the page answers to, said the way the command
            line says them, because a page found under a name that is not
            its heading is otherwise a mystery. */}
        {node.aliases && node.aliases.length > 0 ? (
          <p className="muted">{t('knowledge.alsoCalled', { names: node.aliases.join(', ') })}</p>
        ) : null}
        {page.contact ? (
          <p className="muted">
            {t('knowledge.contact', {
              name: page.contact.name,
              detail: page.contact.emails[0] || page.contact.organization || '',
            })}
          </p>
        ) : null}
        {node.summary ? (
          <div className="knowledge-summary">
            <Markdown text={node.summary} />
          </div>
        ) : (
          <SettingsEmpty>{t('knowledge.noSummary')}</SettingsEmpty>
        )}
      </SettingsSection>

      <SettingsSection
        card
        title={t('knowledge.facts')}
        description={t('knowledge.factsHint')}
        action={
          <button type="button" className="primary" onClick={() => setAdding(null)}>
            {t('knowledge.addFact')}
          </button>
        }
      >
        {page.facts.length === 0 ? <SettingsEmpty>{t('knowledge.noFacts')}</SettingsEmpty> : null}
        {page.facts.slice(0, factsShown).map((fact) => (
          <SettingsRow
            key={fact.id}
            title={`#${fact.number} ${fact.text}`}
            badge={
              <>
                {fact.kind !== 'fact' ? (
                  <Tag value={t(`knowledge.factKind.${fact.kind}` as 'knowledge.factKind.preference')} />
                ) : null}
                {fact.inferred ? <Tag value={t('knowledge.inferred')} tone="warn" /> : null}
              </>
            }
            subtitle={<Provenance fact={fact} attachments={page.attachments} />}
            actions={
              <div className="row-actions">
                <button
                  type="button"
                  className="icon-action"
                  title={t('knowledge.editFact')}
                  aria-label={`#${fact.number}: ${t('knowledge.editFact')}`}
                  onClick={() => setAdding(fact)}
                >
                  <PencilIcon size={16} />
                </button>
                <button
                  type="button"
                  className="icon-action"
                  title={t('knowledge.moveFact')}
                  aria-label={`#${fact.number}: ${t('knowledge.moveFact')}`}
                  onClick={() => setMovingFact(fact)}
                >
                  <MoveIcon size={16} />
                </button>
                <button
                  type="button"
                  className="icon-action danger"
                  title={t('knowledge.strike')}
                  aria-label={`#${fact.number}: ${t('knowledge.strike')}`}
                  onClick={() => setRemoving(fact)}
                >
                  <TrashIcon size={16} />
                </button>
              </div>
            }
          />
        ))}
        {page.facts.length > factsShown ? (
          <button type="button" className="link" onClick={() => setFactsShown((count) => count + PAGE_SIZE)}>
            {t('knowledge.showMore', { count: Math.min(PAGE_SIZE, page.facts.length - factsShown) })}
          </button>
        ) : null}
        {/* What the page used to say and no longer states. Shown because
            a fold is the agent's own judgement about two sentences, and a
            judgement nobody can see is one nobody can disagree with. */}
        {page.folded.length > 0 ? (
          <>
            <p className="knowledge-list-heading">{t('knowledge.foldedFacts')}</p>
            <ul className="knowledge-rows">
              {page.folded.map((row) => (
                <li key={row.fact.id} className="knowledge-folded">
                  <span>
                    #{row.fact.number} {row.fact.text}
                  </span>
                  <span className="knowledge-folded-into">{t('knowledge.foldedInto', { number: row.into })}</span>
                </li>
              ))}
            </ul>
          </>
        ) : null}
      </SettingsSection>

      {/* The drawing beside the card is this page's neighbourhood; the one
          behind the action is whatever is reached from it, with the room to
          walk there. It opens on this page rather than at the roots. */}
      <SettingsSection
        card
        title={t('knowledge.connections')}
        description={t('knowledge.connectionsHint')}
        action={
          <>
            <Tooltip label={t('knowledge.explore.from')}>
              <Link
                className="icon-button"
                aria-label={t('knowledge.explore.from')}
                to={`/settings/knowledge/explore?from=${encodeURIComponent(node.path)}`}
              >
                <GraphIcon />
              </Link>
            </Tooltip>
            {/* Behind a button: the agent draws nearly every link, and a
                form of three fields standing open under every page was
                a page of controls nobody used. */}
            <button type="button" onClick={() => setLinking(true)}>
              {t('knowledge.linkPage')}
            </button>
          </>
        }
      >
        <GraphExplorer path={node.path} onOpen={onSelect} version={linked} />
      </SettingsSection>
      {linking ? (
        <LinkDialog
          path={node.path}
          busy={busy}
          onClose={() => setLinking(false)}
          onLink={async (to, relation, note) => {
            const made = await run(LINK_NODES, { path: node.path, to, relation, note }, t('knowledge.linked'))
            if (made) {
              setLinked((before) => before + 1)
              setLinking(false)
            }
            return made
          }}
        />
      ) : null}

      {page.children.length > 0 ? (
        <SettingsSection card title={t('knowledge.under')} description={t('knowledge.underHint')}>
          {page.children.map((child) => (
            <SettingsRow
              key={child.id}
              title={
                <button type="button" className="link" onClick={() => onSelect(child.path)}>
                  {child.name || child.path}
                </button>
              }
              subtitle={child.summary}
            />
          ))}
        </SettingsSection>
      ) : null}

      <History path={node.path} />

      {editing ? (
        <EditPageDialog
          node={node}
          busy={busy}
          error={problem}
          onClose={() => setEditing(false)}
          onSubmit={async (fields) => {
            if (await run(SAVE_NODE, { path: node.path, ...fields }, t('knowledge.pageSaved'))) setEditing(false)
          }}
        />
      ) : null}

      {moving ? (
        <MovePageDialog
          node={node}
          busy={busy}
          error={problem}
          onClose={() => setMoving(false)}
          onSubmit={(under) => void move(under)}
        />
      ) : null}

      {merging ? (
        <MergePageDialog
          node={node}
          busy={busy}
          error={problem}
          onClose={() => setMerging(false)}
          onSubmit={(into) => {
            setMerging(false)
            setMergingInto(into)
          }}
        />
      ) : null}

      {mergingInto ? (
        <ConfirmDialog
          title={t('knowledge.mergePage')}
          body={t('knowledge.mergeBody', {
            path: node.path,
            into: mergingInto,
            facts: page.facts.length,
            pages: page.children.length,
          })}
          confirmLabel={t('knowledge.merge')}
          busy={busy}
          error={problem}
          onClose={() => setMergingInto('')}
          onConfirm={() => void merge(mergingInto)}
        />
      ) : null}

      {movingFact ? (
        <MoveFactDialog
          fact={movingFact}
          path={node.path}
          busy={busy}
          error={problem}
          onClose={() => setMovingFact(null)}
          onSubmit={async (to) => {
            if (await run(MOVE_FACT, { path: node.path, number: movingFact.number, to }, t('knowledge.factMoved')))
              setMovingFact(null)
          }}
        />
      ) : null}

      {adding !== undefined ? (
        <EditFactDialog
          fact={adding}
          busy={busy}
          error={problem}
          onClose={() => setAdding(undefined)}
          onSubmit={async (fields) => {
            const variables: Record<string, unknown> = { path: node.path, ...fields }
            if (adding) variables.number = adding.number
            if (await run(SAVE_FACT, variables, t('knowledge.factSaved'))) setAdding(undefined)
          }}
        />
      ) : null}

      {removing ? (
        <ConfirmDialog
          title={t('knowledge.strike')}
          body={t('knowledge.strikeBody', { text: removing.text })}
          confirmLabel={t('knowledge.strike')}
          busy={busy}
          onClose={() => setRemoving(null)}
          onConfirm={async () => {
            await run(DELETE_FACT, { path: node.path, number: removing.number }, t('knowledge.struck'))
            setRemoving(null)
          }}
        />
      ) : null}

      {removingPage ? (
        <ConfirmDialog
          title={t('knowledge.forgetPage')}
          body={t('knowledge.forgetPageBody', {
            path: node.path,
            facts: page.facts.length,
            pages: page.children.length,
          })}
          confirmLabel={t('knowledge.forgetPage')}
          busy={busy}
          onClose={() => setRemovingPage(false)}
          onConfirm={async () => {
            if (await run(DELETE_NODE, { path: node.path }, t('knowledge.forgotten'))) {
              setRemovingPage(false)
              onSelect('self')
            }
          }}
        />
      ) : null}
    </>
  )
}

// History is how a page got to say what it says.
//
// Behind a press rather than always open, because it is a query per page
// and most visits do not want it. But it is here rather than in a tool
// nobody knows about, because a page written by a nightly run and a page
// the person wrote themselves look identical until somebody looks: this
// is where "who said that?" is answered.
function History({ path }: { path: string }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const history = useQuery(
    () => (open ? graphql<{ ListAgentPageHistory: Revision[] }>(HISTORY, { path, first: 50 }) : Promise.resolve(null)),
    [path, open],
    { refresh: false },
  )
  const revisions = history.data?.ListAgentPageHistory ?? []

  return (
    <SettingsSection
      card
      title={t('knowledge.history')}
      description={t('knowledge.historyHint')}
      action={
        open ? null : (
          <button type="button" onClick={() => setOpen(true)}>
            {t('knowledge.showHistory')}
          </button>
        )
      }
    >
      {history.error ? <ErrorMessage error={history.error} /> : null}
      {open && history.loading && !history.data ? <Loading /> : null}
      {open && history.data && revisions.length === 0 ? (
        <SettingsEmpty>{t('knowledge.noHistory')}</SettingsEmpty>
      ) : null}
      {revisions.map((revision) => (
        <SettingsRow
          key={revision.revision}
          title={revision.change}
          badge={<Tag value={t(`knowledge.actor.${revision.actor}` as 'knowledge.actor.person')} />}
          subtitle={
            <>
              <span className="muted">{new Date(revision.createdAt).toLocaleString()}</span>
              {revision.before ? (
                <>
                  <br />
                  <span className="knowledge-quote">{t('knowledge.historyWas', { text: cut(revision.before) })}</span>
                </>
              ) : null}
            </>
          }
        />
      ))}
    </SettingsSection>
  )
}

// cut keeps an old revision readable in a row: enough to recognize it,
// not so much that the list becomes the old page.
function cut(text: string, length = 200): string {
  return text.length > length ? text.slice(0, length) + '…' : text
}

// Provenance says where a fact came from, which is what makes a page
// worth trusting: a sentence with a quote behind it can be checked.
//
// Where what it came from is a picture or a file, the thing itself is
// shown under the quote. A fact read out of a screenshot is worth little
// to somebody who cannot see the screenshot, and the name of a file in an
// archive of fifty thousand says nothing on its own.
function Provenance({ fact, attachments }: { fact: Fact; attachments?: Attachment[] }) {
  const { t } = useTranslation()
  const first = fact.evidence[0]
  const cited = attachmentsCitedBy(fact, attachments)
  // A citation with no quote on an inferred fact is the write-time check
  // having found the words somewhere other than the message they were
  // said to come from. Saying so is the point: an empty quote looks like
  // a fact nobody bothered to source, and this one was checked.
  const quoteNotFound = Boolean(first) && !first.quote && fact.inferred
  return (
    <>
      {first?.quote ? <span className="knowledge-quote">&ldquo;{first.quote}&rdquo;</span> : null}
      <span className="muted">
        {first ? t(`knowledge.from.${first.kind}` as 'knowledge.from.conversation') : t('knowledge.fromNowhere')}
        {quoteNotFound ? `, ${t('knowledge.quoteNotFound')}` : ''}
        {fact.happenedAt ? ` · ${new Date(fact.happenedAt).toLocaleDateString()}` : ''}
      </span>
      {cited.map((attachment) => (
        <AttachmentEvidence key={attachment.documentId} attachment={attachment} />
      ))}
    </>
  )
}

// attachmentsCitedBy is the files one fact's evidence names, in the order
// the fact cites them. The evidence carries an identifier, which a filing
// run may have written in brackets, and the page carries the files.
function attachmentsCitedBy(fact: Fact, attachments?: Attachment[]): Attachment[] {
  if (!attachments || attachments.length === 0) return []
  const cited: Attachment[] = []
  for (const evidence of fact.evidence) {
    const id = evidence.id.trim().replace(/^\[|\]$/g, '')
    if (id === '') continue
    const found = attachments.find((attachment) => attachment.documentId === id)
    if (found && !cited.includes(found)) cited.push(found)
  }
  return cited
}

// AttachmentEvidence is the file itself under the fact it stands behind:
// the picture, at a size that leaves the page a page and opens to full
// size in a tab of its own, or the file's name to save where it is not a
// picture. Under it, where it was posted, in the muted line the rest of a
// fact's source is written in.
function AttachmentEvidence({ attachment }: { attachment: Attachment }) {
  const { t } = useTranslation()
  const where = [attachment.thread, attachment.channel].filter((part) => part.trim() !== '').join(' · ')
  const picture = attachment.contentType.toLowerCase().startsWith('image/')
  return (
    <span className="knowledge-attachment">
      {attachment.path === '' ? (
        <span className="muted">{t('knowledge.attachmentMissing', { name: attachment.name })}</span>
      ) : picture ? (
        <a href={attachment.path} target="_blank" rel="noreferrer" title={t('knowledge.attachmentOpen')}>
          {/* Loaded at once rather than lazily. A lazy picture is only
              fetched when its box comes into view, and this box has no
              size until the picture is in it: width and height are auto
              under a max, so before the bytes arrive the element is three
              pixels square, never intersects anything, and the picture is
              never asked for. The result was a page of facts with a blank
              where every screenshot should be. A page holds a bounded
              number of facts, each with at most one picture drawn no
              larger than 320 by 200, so there is little here to defer. */}
          <img className="knowledge-attachment-image" src={attachment.path} alt={attachment.name} />
        </a>
      ) : (
        <a className="link" href={attachment.path} download={attachment.name}>
          {attachment.name}
        </a>
      )}
      <span className="muted">
        {where === '' ? t('knowledge.attachmentFile') : t('knowledge.attachmentFrom', { where })}
      </span>
    </span>
  )
}

function EditPageDialog({
  node,
  busy,
  error,
  onClose,
  onSubmit,
}: {
  node: Node
  busy: boolean
  error: string
  onClose: () => void
  onSubmit: (fields: Record<string, unknown>) => void
}) {
  const { t } = useTranslation()
  const [name, setName] = useState(node.name)
  const [summary, setSummary] = useState(node.summary)
  const started = node.aliases ?? []
  const [aliases, setAliases] = useState(started.join(', '))
  const named = namesOf(aliases)

  return (
    <FormDialog
      title={t('knowledge.editPage')}
      submitLabel={t('common.save')}
      busy={busy}
      error={error}
      canSubmit={name.trim() !== ''}
      onClose={onClose}
      onSubmit={() => {
        const fields: Record<string, unknown> = { name, summary }
        // Only when they were changed: the server replaces the page's
        // other names with whatever arrives, so sending the list back
        // unasked would make every save a rewrite of it -- and a save
        // from a page whose aliases a dream had just written would undo
        // that.
        if (named.join(',') !== started.join(',')) fields.aliases = named
        onSubmit(fields)
      }}
    >
      <label>
        <span>{t('knowledge.name')}</span>
        <input value={name} onChange={(event) => setName(event.target.value)} />
      </label>
      <label>
        <span>{t('knowledge.aliases')}</span>
        <input value={aliases} placeholder="alice, ac" onChange={(event) => setAliases(event.target.value)} />
      </label>
      <p className="muted">{t('knowledge.aliasesHint')}</p>
      <label>
        <span>{t('knowledge.summary')}</span>
        <textarea rows={8} value={summary} onChange={(event) => setSummary(event.target.value)} />
      </label>
      <p className="muted">{t('knowledge.summaryHint')}</p>
    </FormDialog>
  )
}

// namesOf is a comma-separated line read as a list: trimmed, and with
// the empties a trailing comma leaves dropped.
function namesOf(typed: string): string[] {
  return typed
    .split(',')
    .map((name) => name.trim())
    .filter((name) => name !== '')
}

// MovePageDialog asks where a page belongs.
//
// The field is the folder or page it is filed under, prefilled with the
// one it is under now, because most moves are a page in the wrong
// project rather than a page with no home at all.
function MovePageDialog({
  node,
  busy,
  error,
  onClose,
  onSubmit,
}: {
  node: Node
  busy: boolean
  error: string
  onClose: () => void
  onSubmit: (under: string) => void
}) {
  const { t } = useTranslation()
  const [under, setUnder] = useState(parentOf(node.path))

  return (
    <FormDialog
      title={t('knowledge.movePage')}
      submitLabel={t('knowledge.movePage')}
      busy={busy}
      error={error}
      canSubmit={under.trim() !== '' && under.trim() !== parentOf(node.path)}
      onClose={onClose}
      onSubmit={() => onSubmit(under.trim())}
    >
      <label>
        <span>{t('knowledge.moveUnder')}</span>
        <input value={under} placeholder="projects" onChange={(event) => setUnder(event.target.value)} />
      </label>
      <p className="muted">{t('knowledge.moveUnderHint', { path: node.path })}</p>
    </FormDialog>
  )
}

// MergePageDialog asks which page this one becomes part of.
//
// The same field the move dialog uses, and empty rather than prefilled:
// a merge has no obvious destination the way a move has the folder it is
// in already, and a prefilled one on a form whose submit removes this
// page is an accident waiting for a stray Enter.
function MergePageDialog({
  node,
  busy,
  error,
  onClose,
  onSubmit,
}: {
  node: Node
  busy: boolean
  error: string
  onClose: () => void
  onSubmit: (into: string) => void
}) {
  const { t } = useTranslation()
  const [into, setInto] = useState('')
  const other = into.trim()

  return (
    <FormDialog
      title={t('knowledge.mergePage')}
      submitLabel={t('knowledge.mergeNext')}
      busy={busy}
      error={error}
      canSubmit={other !== '' && other !== node.path}
      onClose={onClose}
      onSubmit={() => onSubmit(other)}
    >
      <label>
        <span>{t('knowledge.mergeInto')}</span>
        <input
          autoFocus
          value={into}
          placeholder="people/alice-chen"
          onChange={(event) => setInto(event.target.value)}
        />
      </label>
      <p className="muted">{t('knowledge.mergeIntoHint', { path: node.path })}</p>
    </FormDialog>
  )
}

// MoveFactDialog asks which page a sentence belongs on.
//
// The page has to exist: the server refuses to make one on the way,
// because a mistyped path would file the sentence somewhere nobody
// reads.
function MoveFactDialog({
  fact,
  path,
  busy,
  error,
  onClose,
  onSubmit,
}: {
  fact: Fact
  path: string
  busy: boolean
  error: string
  onClose: () => void
  onSubmit: (to: string) => void
}) {
  const { t } = useTranslation()
  const [to, setTo] = useState('')
  const other = to.trim()

  return (
    <FormDialog
      title={t('knowledge.moveFact')}
      submitLabel={t('common.move')}
      busy={busy}
      error={error}
      canSubmit={other !== '' && other !== path}
      onClose={onClose}
      onSubmit={() => onSubmit(other)}
    >
      <p className="knowledge-quote">{cut(fact.text, 200)}</p>
      <label>
        <span>{t('knowledge.moveFactTo')}</span>
        <input autoFocus value={to} placeholder="projects/portal" onChange={(event) => setTo(event.target.value)} />
      </label>
      <p className="muted">{t('knowledge.moveFactToHint')}</p>
    </FormDialog>
  )
}

// LinkDialog joins this page to another one, in a dialog: a link is a
// claim the person makes on purpose, and rare, since the agent draws
// nearly all of them.
function LinkDialog({
  path,
  busy,
  onClose,
  onLink,
}: {
  path: string
  busy: boolean
  onClose: () => void
  onLink: (to: string, relation: string, note: string) => Promise<boolean>
}) {
  const { t } = useTranslation()
  const [to, setTo] = useState('')
  // The general one to start from. Anything more specific is a claim the
  // person is making, and should be chosen rather than defaulted into.
  const [relation, setRelation] = useState('related_to')
  const [note, setNote] = useState('')
  const other = to.trim()

  return (
    <FormDialog
      title={t('knowledge.linkPage')}
      submitLabel={t('knowledge.link')}
      busy={busy}
      canSubmit={other !== '' && other !== path}
      onClose={onClose}
      onSubmit={() => {
        void onLink(other, relation, note.trim())
      }}
    >
      <label>
        <span>{t('knowledge.linkTo')}</span>
        <input autoFocus value={to} placeholder="people/alice-chen" onChange={(event) => setTo(event.target.value)} />
      </label>
      <label>
        <span>{t('knowledge.linkRelation')}</span>
        <select value={relation} onChange={(event) => setRelation(event.target.value)}>
          {RELATIONS.map((value) => (
            <option key={value} value={value}>
              {t(`knowledge.relation.${value}` as 'knowledge.relation.works_on')}
            </option>
          ))}
        </select>
      </label>
      <label>
        <span>{t('knowledge.linkNote')}</span>
        <input value={note} onChange={(event) => setNote(event.target.value)} />
      </label>
    </FormDialog>
  )
}

function EditFactDialog({
  fact,
  busy,
  error,
  onClose,
  onSubmit,
}: {
  fact: Fact | null
  busy: boolean
  error: string
  onClose: () => void
  onSubmit: (fields: Record<string, unknown>) => void
}) {
  const { t } = useTranslation()
  const [text, setText] = useState(fact?.text || '')
  const [kind, setKind] = useState(fact?.kind || 'fact')
  const [happened, setHappened] = useState(fact?.happenedAt ? fact.happenedAt.slice(0, 10) : '')
  // In the order AUDIENCES names them, on both sides, so that comparing
  // what was ticked with what the fact arrived with is a comparison of
  // lists rather than of sets.
  const started = AUDIENCES.filter((audience) => (fact?.audiences ?? []).includes(audience))
  const [audiences, setAudiences] = useState<string[]>(started)

  return (
    <FormDialog
      title={fact ? t('knowledge.editFact') : t('knowledge.addFact')}
      submitLabel={fact ? t('common.save') : t('knowledge.addFact')}
      busy={busy}
      error={error}
      canSubmit={text.trim() !== ''}
      onClose={onClose}
      onSubmit={() => {
        const fields: Record<string, unknown> = { text, kind, happened }
        // Only when they were changed. The server keeps the audiences an
        // edit says nothing about, and sending them back untouched would
        // throw away what the agent addressed the fact to the moment
        // somebody fixed a typo in it.
        if (audiences.join(',') !== started.join(',')) fields.audiences = audiences
        onSubmit(fields)
      }}
    >
      <label>
        <span>{t('knowledge.factText')}</span>
        <textarea rows={3} value={text} onChange={(event) => setText(event.target.value)} />
      </label>
      <label>
        <span>{t('knowledge.factKindLabel')}</span>
        <select value={kind} onChange={(event) => setKind(event.target.value)}>
          {['fact', 'preference', 'decision', 'event', 'howto'].map((value) => (
            <option key={value} value={value}>
              {t(`knowledge.factKind.${value}` as 'knowledge.factKind.fact')}
            </option>
          ))}
        </select>
      </label>
      <label>
        <span>{t('knowledge.happened')}</span>
        <input
          type="text"
          value={happened}
          placeholder="2023-06"
          onChange={(event) => setHappened(event.target.value)}
        />
      </label>
      <p className="muted">{t('knowledge.happenedHint')}</p>
      <CheckList
        label={t('knowledge.factAudiences')}
        hint={t('knowledge.factAudiencesHint')}
        items={AUDIENCE_ITEMS}
        selected={audiences}
        // Back into the order AUDIENCES names them, whatever order they
        // were ticked in, so that what is sent reads the same way twice
        // and comparing it with what the fact arrived with is a
        // comparison of lists.
        onChange={(chosen) => setAudiences(AUDIENCES.filter((audience) => chosen.includes(audience)))}
        describe={(item) => t(`agent.audience.${item.id}` as 'agent.audience.triage')}
      />
    </FormDialog>
  )
}
