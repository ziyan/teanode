import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from '../i18n/i18n'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { ConfirmDialog, FormDialog } from '../components/dialog'
import { ErrorMessage, Loading, Tag } from '../components/common'
import {
  ChevronLeftIcon,
  ChevronRightIcon,
  GraphIcon,
  MoveIcon,
  PencilIcon,
  PinIcon,
  PinOffIcon,
  TrashIcon,
  SparkIcon,
} from '../components/icons'
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

const SAVE_NODE = `mutation ($path: String!, $kind: String, $name: String, $summary: String, $pinned: Boolean) {
  SaveAgentNode(path: $path, kind: $kind, name: $name, summary: $summary, pinned: $pinned) { id path }
}`

const SAVE_FACT = `mutation ($path: String!, $number: Int, $kind: String, $text: String!, $happened: String) {
  SaveAgentFact(path: $path, number: $number, kind: $kind, text: $text, happened: $happened) { id number }
}`

const HISTORY = `query ($path: String!, $first: Int) {
  ListAgentPageHistory(path: $path, first: $first) {
    revision kind actor summary change before after path reason createdAt
  }
}`

const MOVE_NODE = `mutation ($path: String!, $under: String!) {
  MoveAgentNode(path: $path, under: $under) { id path }
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

type Page = {
  node: Node
  facts: Fact[]
  folded: FoldedFact[]
  children: Node[]
  contact?: { id: string; name: string; emails: string[]; organization: string } | null
}

// PAGE_SIZE is how many pages a folder shows at a time. Fifty is a screen
// and a half; a folder of two thousand projects is "show fifty more",
// not a two-thousand-row scroll.
const PAGE_SIZE = 50

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
  const frameRef = useRef<HTMLDivElement | null>(null)
  const width = useContainerWidth(frameRef)
  const onePane = (width === null ? (desktop ? 2 : 1) : width >= 760 ? 2 : 1) === 1
  const [filter, setFilter] = useState('')
  const search = filter.trim()
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
      setWalkedInto(into ? next : '')
      setFilter('')
      navigate('/settings/knowledge' + (next ? '/' + next : ''))
    },
    [navigate],
  )
  const goPage = useCallback((next: string) => goTo(next, false), [goTo])
  // Up is the folder this one is filed in, walked into rather than read:
  // it may be a page with children itself.
  const goUp = useCallback(() => goTo(parentOf(folder ?? ''), true), [goTo, folder])

  // The lookup, and beside it the whole graph drawn. They are the two ways
  // in and they answer different questions -- what is this called, and what
  // does this sit among -- so neither one is behind the other.
  const lookup = (
    <div className="knowledge-lookup-row">
      <input
        type="search"
        className="knowledge-lookup"
        value={filter}
        placeholder={t('knowledge.find')}
        aria-label={t('knowledge.find')}
        onChange={(event) => setFilter(event.target.value)}
      />
      {/* An icon beside the box, the way the mailbox lays out its
          toolbar: the words are the title and the label. */}
      <Link
        className="icon-action knowledge-lookup-action"
        to="/settings/knowledge/explore"
        title={t('knowledge.explore.go')}
        aria-label={t('knowledge.explore.go')}
      >
        <GraphIcon size={18} />
      </Link>
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

  if (onePane) {
    // One column at a time. Which one is in the URL, so Back is Back,
    // and the breadcrumb on the bar is the way up out of the navigator.
    return (
      <div ref={frameRef} className="knowledge-phone">
        {showingDetail ? detail : lookup}
        {showingDetail ? null : <div className="card knowledge-list">{list}</div>}
      </div>
    )
  }

  return (
    <div ref={frameRef} className="knowledge-columns">
      <div className="knowledge-column knowledge-column-navigator">
        {lookup}
        <div className="card knowledge-list">{list}</div>
      </div>
      <div className="knowledge-column knowledge-page">{detail}</div>
    </div>
  )
}

// useContainerWidth is how wide an element is, kept up to date as it
// changes, and null before it has been measured.
function useContainerWidth(ref: React.RefObject<HTMLDivElement | null>): number | null {
  const [width, setWidth] = useState<number | null>(null)
  useEffect(() => {
    const element = ref.current
    if (!element) return
    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) setWidth(entry.contentRect.width)
    })
    observer.observe(element)
    setWidth(element.getBoundingClientRect().width)
    return () => observer.disconnect()
  }, [ref])
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
        <div className="knowledge-navigator-frame">
          {panels.map((panel) => (
            <div key={panel.path} className={panel.className}>
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
        <button type="button" className="knowledge-more" disabled={loading} onClick={() => void load(rows.length)}>
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

  return (
    <>
      <div className="card">
        <div className="knowledge-heading">
          <div>
            <h3>{node.path === 'self' && me ? me : node.name || node.path}</h3>
            <p className="muted">
              <code className="tag knowledge-path">{node.path}</code>{' '}
              <Tag value={t(`knowledge.kind.${node.kind}` as 'knowledge.kind.person')} />
              {node.pinned ? <Tag value={t('knowledge.pinned')} tone="good" /> : null}
            </p>
          </div>
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
        </div>
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
      </div>

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
            subtitle={<Provenance fact={fact} />}
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
          <button type="button" className="knowledge-more" onClick={() => setFactsShown((count) => count + PAGE_SIZE)}>
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
function Provenance({ fact }: { fact: Fact }) {
  const { t } = useTranslation()
  const first = fact.evidence[0]
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
    </>
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

  return (
    <FormDialog
      title={t('knowledge.editPage')}
      submitLabel={t('common.save')}
      busy={busy}
      error={error}
      canSubmit={name.trim() !== ''}
      onClose={onClose}
      onSubmit={() => onSubmit({ name, summary })}
    >
      <label>
        <span>{t('knowledge.name')}</span>
        <input value={name} onChange={(event) => setName(event.target.value)} />
      </label>
      <label>
        <span>{t('knowledge.summary')}</span>
        <textarea rows={8} value={summary} onChange={(event) => setSummary(event.target.value)} />
      </label>
      <p className="muted">{t('knowledge.summaryHint')}</p>
    </FormDialog>
  )
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

  return (
    <FormDialog
      title={fact ? t('knowledge.editFact') : t('knowledge.addFact')}
      submitLabel={fact ? t('common.save') : t('knowledge.addFact')}
      busy={busy}
      error={error}
      canSubmit={text.trim() !== ''}
      onClose={onClose}
      onSubmit={() => onSubmit({ text, kind, happened })}
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
    </FormDialog>
  )
}
