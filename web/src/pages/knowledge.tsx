import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from '../i18n/i18n'
import { useNavigate, useParams } from 'react-router-dom'

import { ConfirmDialog, FormDialog } from '../components/dialog'
import { ErrorMessage, Loading, Tag } from '../components/common'
import { ChevronRightIcon, PencilIcon, PinIcon, PinOffIcon, TrashIcon } from '../components/icons'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../components/settingsList'
import { graphql } from '../api'
import { useQuery } from '../components/useQuery'
import { useToast } from '../components/toast'
import { useIsDesktop } from '../components/sidebar'
import { useSession } from '../session'
import { useBreadcrumbDetail } from '../components/breadcrumb'
import { Markdown } from '../components/markdown'
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
    children { id path kind name summary }
    contact { id name emails organization }
  }
}`

const CHILDREN = `query ($path: String!, $first: Int, $offset: Int) {
  AgentGraphChildren(path: $path, first: $first, offset: $offset) {
    rows { node { id path kind name summary pinned dormant importance } hint }
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

const DELETE_FACT = `mutation ($path: String!, $number: Int!) { DeleteAgentFact(path: $path, number: $number) }`
const DELETE_NODE = `mutation ($path: String!) { DeleteAgentNode(path: $path) }`

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

type Page = {
  node: Node
  facts: Fact[]
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

// folderName is a root's name as a heading: the person's own name on
// their page, the server's name for the ones it made, and a capital on
// the ones a source made in lower case.
function folderName(node: Node, me: string): string {
  if (node.path === 'self' && me) return me
  const name = node.name || node.path
  return name.charAt(0).toUpperCase() + name.slice(1)
}

// What the agent knows, as the person reads and corrects it.
//
// Three columns, the way a file browser shows a hierarchy: the folders,
// the pages in one folder, and one page. The columns are what the
// research on this kind of data says works -- the whole path stays in
// view and a deep tree is walked sideways rather than by drilling -- and
// on a phone they show one at a time, each with a way back, so the URL
// says where you are and the browser's own Back agrees.
//
// The lookup box at the top is the primary way in. A graph of thousands
// of pages is not browsed; it is looked up, and the list is for when you
// do not know the name yet.
export function KnowledgePage() {
  const { t } = useTranslation()
  const toast = useToast()
  const desktop = useIsDesktop()
  const me = useSession().name || ''
  // The URL is the graph path: /settings/knowledge/work/portal is the
  // page, /settings/knowledge/projects is the folder's list, and nothing
  // is the top. A root that is a folder opens its list; any other path
  // opens the page.
  const navigate = useNavigate()
  const at = (useParams()['*'] || '').replace(/^\/+|\/+$/g, '')
  const isFolderPath = at !== '' && !at.includes('/') && at !== 'self'
  const path = isFolderPath ? '' : at
  const folder = isFolderPath ? at : path ? rootOf(path) : null
  const go = useCallback((next: string) => navigate('/settings/knowledge' + (next ? '/' + next : '')), [navigate])
  // How many columns there is room for, measured on the page itself
  // rather than the window: the sidebar takes a third of a laptop, and a
  // window that fits three columns with it closed does not with it open.
  // Below two columns' worth it is one at a time, the way a phone is.
  const frameRef = useRef<HTMLDivElement | null>(null)
  const width = useContainerWidth(frameRef)
  const columns = width === null ? (desktop ? 2 : 1) : width >= 1040 ? 3 : width >= 760 ? 2 : 1
  const wide = columns === 3
  const [filter, setFilter] = useState('')
  const search = filter.trim()

  // Desktop with nothing open shows the person's own page.
  const open = path || (columns > 1 && !folder ? 'self' : '')

  const page = useQuery(
    () => (open ? graphql<{ AgentGraphPage: Page | null }>(PAGE, { path: open }) : Promise.resolve(null)),
    [open],
    { refresh: false },
  )
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

  const goFolder = useCallback(
    (next: Node) => {
      go(next.path)
      setFilter('')
    },
    [go],
  )
  const goPage = useCallback(
    (next: string) => {
      go(next)
      setFilter('')
    },
    [go],
  )

  const lookup = (
    <input
      type="search"
      className="knowledge-lookup"
      value={filter}
      placeholder={t('knowledge.find')}
      aria-label={t('knowledge.find')}
      onChange={(event) => setFilter(event.target.value)}
    />
  )

  const roots = useQuery(
    () =>
      graphql<{ AgentGraphChildren: { rows: { node: Node; hint: string }[]; total: number } }>(CHILDREN, {
        path: '',
        first: 100,
      }),
    [],
    { refresh: false },
  )
  const rootNodes = roots.data?.AgentGraphChildren.rows.map((row) => row.node) ?? []
  const folderNode = rootNodes.find((node) => node.path === folder)
  const folderLabel = folderNode ? folderName(folderNode, me) : folder || ''
  const pageName = page.data?.AgentGraphPage?.node.name ?? null

  // One column at a time, so the breadcrumb is the way back: the folder
  // above the page, the top above the folder. With the columns side by
  // side the page is still Knowledge, and the trail says so.
  const onePane = columns === 1
  useBreadcrumbDetail(
    onePane && folder ? (folder === 'self' ? me || pageName : folderLabel) : null,
    onePane && path && folder !== 'self' ? (pageName ?? '…') : null,
  )

  const folders = <Folders roots={rootNodes} selected={folder} onSelect={goFolder} me={me} />
  const pages = search ? (
    <SearchResults found={found.data?.SearchAgentGraph} loading={found.loading} onSelect={goPage} />
  ) : folder && folder !== 'self' ? (
    <FolderPages folder={folder} label={folderLabel} selected={open} onSelect={goPage} />
  ) : null

  const detail = open ? (
    <>
      {page.loading && !page.data ? <Loading /> : null}
      {page.error ? <ErrorMessage error={page.error} /> : null}
      {page.data && !page.data.AgentGraphPage ? (
        <div className="card">
          <h3>{open}</h3>
          <SettingsEmpty>{t('knowledge.noPage')}</SettingsEmpty>
        </div>
      ) : null}
      {page.data?.AgentGraphPage ? (
        <PageView
          page={page.data.AgentGraphPage}
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
    // and the breadcrumb on the bar is the way up.
    return (
      <div ref={frameRef} className="knowledge-phone">
        {path ? detail : null}
        {!path && folder ? lookup : null}
        {!path && folder ? <div className="card knowledge-list">{pages}</div> : null}
        {!path && !folder ? lookup : null}
        {!path && !folder ? <div className="card knowledge-list">{search ? pages : folders}</div> : null}
      </div>
    )
  }

  // Three columns where there is room for three; otherwise the folders
  // fold into a row of chips above the list and it is two.
  return (
    <div ref={frameRef} className={wide ? 'knowledge-columns' : 'knowledge-columns knowledge-columns-two'}>
      {wide ? (
        <div className="knowledge-column knowledge-column-folders">
          {lookup}
          <div className="card knowledge-list">{folders}</div>
        </div>
      ) : null}
      <div className="knowledge-column knowledge-column-pages">
        {wide ? null : lookup}
        {wide ? null : <FolderChips roots={rootNodes} selected={folder} onSelect={goFolder} me={me} />}
        <div className="card knowledge-list">{pages ?? <SettingsEmpty>{t('knowledge.pickFolder')}</SettingsEmpty>}</div>
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

// FolderChips is the folders as one row, for when there is no room for
// them as a column.
function FolderChips({
  roots,
  selected,
  onSelect,
  me,
}: {
  roots: Node[]
  selected: string | null
  onSelect: (root: Node) => void
  me: string
}) {
  return (
    <div className="knowledge-chips">
      {roots.map((root) => (
        <button
          key={root.id}
          type="button"
          className={root.path === selected ? 'knowledge-chip selected' : 'knowledge-chip'}
          onClick={() => onSelect(root)}
        >
          {folderName(root, me)}
        </button>
      ))}
    </div>
  )
}

// Folders is the top of the graph: each root with how much is under it.
function Folders({
  roots,
  selected,
  onSelect,
  me,
}: {
  roots: Node[]
  selected: string | null
  onSelect: (root: Node) => void
  me: string
}) {
  const counts = useQuery(
    async () => {
      const entries = await Promise.all(
        roots
          .filter((root) => root.kind === 'folder')
          .map(async (root) => {
            const result = await graphql<{ AgentGraphChildren: { total: number } }>(CHILDREN, {
              path: root.path,
              first: 1,
            })
            return [root.path, result.AgentGraphChildren.total] as const
          }),
      )
      return Object.fromEntries(entries) as Record<string, number>
    },
    [roots.map((root) => root.path).join(',')],
    { refresh: false },
  )
  if (roots.length === 0) return <Loading />
  return (
    <ul className="knowledge-rows">
      {roots.map((root) => (
        <li key={root.id}>
          <button
            type="button"
            className={root.path === selected ? 'knowledge-row selected' : 'knowledge-row'}
            onClick={() => onSelect(root)}
          >
            <span className="knowledge-row-name">{folderName(root, me)}</span>
            {root.kind === 'folder' && counts.data ? (
              <span className="knowledge-row-count">{counts.data[root.path] ?? 0}</span>
            ) : null}
            <ChevronRightIcon size={14} />
          </button>
        </li>
      ))}
    </ul>
  )
}

// FolderPages is what is under one folder, fifty at a time, most important
// first, each with enough of a hint to tell it from its neighbours.
function FolderPages({
  folder,
  label,
  selected,
  onSelect,
}: {
  folder: string
  label: string
  selected: string
  onSelect: (path: string) => void
}) {
  const { t } = useTranslation()
  const [rows, setRows] = useState<{ node: Node; hint: string }[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [problem, setProblem] = useState<unknown>(null)

  const load = useCallback(
    async (offset: number) => {
      setLoading(true)
      try {
        const result = await graphql<{ AgentGraphChildren: { rows: { node: Node; hint: string }[]; total: number } }>(
          CHILDREN,
          { path: folder, first: PAGE_SIZE, offset },
        )
        setRows((before) =>
          offset === 0 ? result.AgentGraphChildren.rows : [...before, ...result.AgentGraphChildren.rows],
        )
        setTotal(result.AgentGraphChildren.total)
      } catch (caught) {
        setProblem(caught)
      } finally {
        setLoading(false)
      }
    },
    [folder],
  )
  useEffect(() => {
    setRows([])
    setTotal(0)
    void load(0)
  }, [load])

  if (problem) return <ErrorMessage error={problem} />
  if (!loading && rows.length === 0) return <SettingsEmpty>{t('knowledge.emptyFolder')}</SettingsEmpty>
  return (
    <>
      <p className="knowledge-list-heading">
        {label}
        <span className="knowledge-row-count">{total}</span>
      </p>
      <ul className="knowledge-rows">
        {rows.map((row) => (
          <li key={row.node.id}>
            <PageRow node={row.node} hint={row.hint} selected={row.node.path === selected} onSelect={onSelect} />
          </li>
        ))}
      </ul>
      {rows.length < total ? (
        <button type="button" className="knowledge-more" disabled={loading} onClick={() => void load(rows.length)}>
          {t('knowledge.showMore', { count: Math.min(PAGE_SIZE, total - rows.length) })}
        </button>
      ) : null}
      {loading && rows.length === 0 ? <Loading /> : null}
    </>
  )
}

// PageRow is one page in a list: its name, and one line to tell it from
// the row above it -- the opening where there is one, the kind where
// there is not. A list of bare names is the thing a person cannot use.
function PageRow({
  node,
  hint,
  selected,
  onSelect,
}: {
  node: Node
  hint?: string
  selected: boolean
  onSelect: (path: string) => void
}) {
  const { t } = useTranslation()
  const segment = node.path.split('/').pop() || node.path
  const name = node.name && node.name.toLowerCase() !== segment.toLowerCase() ? node.name : segment
  const line = hint || node.summary
  const shown = line ? cut(line, 90) : t(`knowledge.kind.${node.kind}` as 'knowledge.kind.person')
  return (
    <button
      type="button"
      className={selected ? 'knowledge-row selected' : 'knowledge-row'}
      onClick={() => onSelect(node.path)}
    >
      <span className="knowledge-row-text">
        <span className="knowledge-row-name">
          {name}
          {node.pinned ? <PinIcon size={12} /> : null}
        </span>
        <span className="knowledge-row-hint">{shown}</span>
      </span>
      <ChevronRightIcon size={14} />
    </button>
  )
}

// SearchResults is what the lookup found, pages then facts, grouped by
// the folder each is filed under so a hit in Projects reads as one.
function SearchResults({
  found,
  loading,
  onSelect,
}: {
  found?: { nodes: Node[]; facts: { fact: Fact; path: string; name: string }[] } | null
  loading: boolean
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
                <PageRow node={node} selected={false} onSelect={onSelect} />
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
  const node = page.node
  const [editing, setEditing] = useState(false)
  const [adding, setAdding] = useState<Fact | null | undefined>(undefined)
  const [removing, setRemoving] = useState<Fact | null>(null)
  const [removingPage, setRemovingPage] = useState(false)
  const [busy, setBusy] = useState(false)
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

  return (
    <>
      <div className="card">
        <div className="knowledge-heading">
          <div>
            <h3>{node.name || node.path}</h3>
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
        {page.facts.map((fact) => (
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
      </SettingsSection>

      <SettingsSection card title={t('knowledge.connections')} description={t('knowledge.connectionsHint')}>
        <GraphExplorer path={node.path} onOpen={onSelect} />
      </SettingsSection>

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
  return (
    <>
      {first?.quote ? <span className="knowledge-quote">&ldquo;{first.quote}&rdquo;</span> : null}
      <span className="muted">
        {first ? t(`knowledge.from.${first.kind}` as 'knowledge.from.conversation') : t('knowledge.fromNowhere')}
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
