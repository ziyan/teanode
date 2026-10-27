import { isPathAvailable } from '../services'
import type { ServiceCapabilities } from '../api'
import React, { useCallback, useEffect, useState } from 'react'
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom'

import { Key, useTranslation } from '../i18n/i18n'
import {
  CalendarIcon,
  PictureIcon,
  ChevronRightIcon,
  CodeIcon,
  ComposeIcon,
  DomainsIcon,
  GridIcon,
  HeartIcon,
  KeyIcon,
  LinkIcon,
  ListIcon,
  LocationIcon,
  CartIcon,
  LockIcon,
  LogoutIcon,
  MailIcon,
  RecordIcon,
  PeopleIcon,
  QueueIcon,
  RefreshIcon,
  ServerIcon,
  ServiceIcon,
  SetupIcon,
  SettingsIcon,
  ShareIcon,
  ShieldIcon,
  SmartphoneIcon,
  SparkIcon,
  ChatIcon,
  JournalIcon,
  TerminalIcon,
  UserIcon,
  WalletIcon,
} from './icons'
import { Logo } from './logo'
import { useAgentFinancePresence } from '../pages/finance/financePresence'
import { useHerdrPresence } from './herdrPresence'
import { useAgentIdentity } from '../agentPreferences'
import { PAGE_TABS, shownTabs, tabAddress } from '../pageTabs'
import { matchSettingsSurface, SETTINGS_LANDING, surfacesByCategory } from '../pages/settings/nav'
import { useFreshness } from './freshness'
import { Permissions as ApiPermissions } from '../api'
import { hasAnywhere, hasPermission, useSession } from '../session'
import { folderLabel, railRows, useMailboxes } from '../mailboxes'
import { FolderKindIcon } from './folderIcon'
import { MailboxFolder } from '../api'
import { Select } from './select'
import { SidebarSearch } from './sidebarSearch'
import { Tooltip } from './tooltip'

// permission is what a row needs, when it needs one: a domain permission held
// over at least one domain, or a server permission. A row nothing gates is
// for everyone who is signed in.
type Item = {
  label: Key
  to: string
  icon: React.ReactNode
  anyOf?: string[]
  shownWhen?: 'finance' | 'mailbox' | 'coding'
  rail?: 'mailbox'
}
type Group = { label?: Key; items: Item[] }

// One icon per settings surface that appears in the rail. Here rather than in
// nav.ts, which stays free of React so anything can read it.
const SERVER_ICONS: Record<string, React.ReactNode> = {
  setup: <SetupIcon />,
  integrations: <ServiceIcon />,
  server: <ServerIcon />,
}

// Every page of the account's settings has one; a test checks it.
export const ACCOUNT_ICONS: Record<string, React.ReactNode> = {
  preference: <UserIcon />,
  agent: <SparkIcon />,
  mailbox: <MailIcon />,
  knowledge: <GridIcon />,
  finance: <WalletIcon />,
  recordings: <RecordIcon />,
  coding: <CodeIcon />,
  password: <KeyIcon />,
  passkeys: <ShieldIcon />,
  tokens: <TerminalIcon />,
  devices: <SmartphoneIcon />,
  apps: <LinkIcon />,
  sessions: <LogoutIcon />,
}

// The rail while you are in your own settings. Four short pages, and they are
// reached from each other rather than only from the menu they were opened
// from — which is what makes this a place rather than a detour.
const ACCOUNT_GROUP: Group = {
  label: 'settings.category.account',
  items: surfacesByCategory('account').map((surface) => ({
    label: surface.label,
    to: surface.path,
    icon: ACCOUNT_ICONS[surface.segment],
    shownWhen: surface.shownWhen,
    rail: surface.rail,
  })),
}

// The account's pages whose row is in the mailbox's rail instead, after the
// calendar: Knowledge and Finance are read the way the mail is, not set once.
const MAILBOX_RAIL_ITEMS: Item[] = ACCOUNT_GROUP.items.filter((item) => item.rail === 'mailbox')

// The rail. Two groups: what is arriving and what is stuck, then what it
// arrives for and how this server is set up. They are separate questions,
// asked at different times, and the space between the groups says so; a
// label over each was two more words in a rail that is already a short list.
//
// The server's own settings are rows here rather than pages behind a Settings
// row, because they are configuration of the thing the rail is about, and one
// more click to reach a page is one more click every time. What configures the
// person signed in is not here: it hangs off their name at the foot, which is
// where people look for it.
// Where "Manage" goes: the first management row this person may open, or
// nothing when they may open none. Exported because the account menu offers
// the way in, and should not have to know how the management side is
// arranged to do it.
export function firstManagementPath(
  permissions: ApiPermissions | undefined | null,
  services?: ServiceCapabilities,
): string | undefined {
  const allowed = (item: Item) =>
    !item.anyOf || item.anyOf.some((key) => hasPermission(permissions, key) || hasAnywhere(permissions, key))
  return GROUPS.flatMap((group) => group.items).find((item) => allowed(item) && isPathAvailable(item.to, services))?.to
}

const GROUPS: Group[] = [
  {
    items: [
      { label: 'nav.mail', to: '/manage/mail', icon: <MailIcon />, anyOf: ['mail:audit'] },
      { label: 'nav.queue', to: '/manage/queue', icon: <QueueIcon />, anyOf: ['queue:manage'] },
      { label: 'nav.reports', to: '/manage/reports', icon: <ShieldIcon />, anyOf: ['report:read'] },
    ],
  },
  {
    items: [
      { label: 'nav.domains', to: '/manage/domains', icon: <DomainsIcon />, anyOf: ['domain:manage'] },
      {
        label: 'nav.access',
        to: '/manage/access',
        icon: <PeopleIcon />,
        anyOf: ['user:manage', 'group:manage', 'role:manage', 'audit:read'],
      },
      ...surfacesByCategory('server').map((surface) => ({
        label: surface.label,
        to: surface.path,
        icon: SERVER_ICONS[surface.segment],
        anyOf: ['server:manage'],
      })),
      { label: 'nav.agentAdmin', to: '/manage/agent', icon: <SparkIcon />, anyOf: ['agent:audit', 'agent:act'] },
    ],
  },
]

// What decides whether a row is shown: the person's permissions and the
// server's services, whether they have a mailbox, and the two answers the rail
// has to ask for (finance and coding).
type RailContext = {
  permissions: ApiPermissions | undefined | null
  services?: ServiceCapabilities
  hasMailbox: boolean
  isFinanceShown: boolean
  hasCoding: boolean
}

// isItemShown is the rule the rail draws a row by, kept apart from the
// component so the search box offers exactly the pages the rail shows.
function isItemShown(item: Item, context: RailContext): boolean {
  const { permissions, services } = context
  return (
    isPathAvailable(item.to, services) &&
    (item.to !== '/recordings' || hasPermission(permissions, 'agent:use')) &&
    (item.shownWhen !== 'finance' || context.isFinanceShown) &&
    (item.shownWhen !== 'coding' || (context.hasCoding && hasPermission(permissions, 'agent:use'))) &&
    (item.shownWhen !== 'mailbox' || context.hasMailbox) &&
    (!item.anyOf || item.anyOf.some((key) => hasPermission(permissions, key) || hasAnywhere(permissions, key)))
  )
}

// A tab of a page the search box can jump into, and where it is.
export type NavigableTab = { label: Key; to: string }

// A page the search box can jump to, with the tabs of it the person has.
export type NavigablePage = { label: Key; to: string; icon: React.ReactNode; tabs: NavigableTab[] }

// navigablePages is every page the person could open from the rail, the account
// menu and the management side, in the order they are drawn there: the
// feature pages first, then their own settings, then the management pages.
// The account's and the management lists are the rail's own (ACCOUNT_GROUP,
// MAILBOX_RAIL_ITEMS, GROUPS) through the same isItemShown. The feature rows
// are written out in Sidebar's markup, in two shapes (the mailbox's rail and
// the rail of somebody with no mailbox), so they are repeated here with
// those conditions; a row added to either shape is added here too.
export function navigablePages(
  context: RailContext & { hasMailboxRail: boolean; isAgentAvailable: boolean },
): NavigablePage[] {
  const { permissions, services, hasMailboxRail } = context
  const pages: NavigablePage[] = []
  const tabsOf = (to: string): NavigableTab[] => {
    const page = PAGE_TABS.find((candidate) => candidate.path === to)
    if (!page) return []
    return shownTabs(page.tabs, { hasPermission: (key) => hasPermission(permissions, key), services }).map((tab) => ({
      label: tab.label,
      to: tabAddress(page, tab.id),
    }))
  }
  const add = (isShown: boolean, label: Key, to: string, icon: React.ReactNode, hasTabs = true) => {
    if (isShown) pages.push({ label, to, icon, tabs: hasTabs ? tabsOf(to) : [] })
  }
  const hasJournal = hasPermission(permissions, 'journal:use')
  const hasAgentUse = hasPermission(permissions, 'agent:use')
  if (hasMailboxRail) {
    add(context.isAgentAvailable, 'nav.chats', '/chat', <ChatIcon />)
    add(hasJournal, 'journal.title', '/journal', <JournalIcon />)
    add(true, 'nav.mailbox', '/mailbox', <MailIcon />)
    add(true, 'mailbox.folder.starred', '/mailbox/starred', <FolderKindIcon kind="starred" />)
    add(true, 'mailbox.newMessage', '/mailbox/compose', <ComposeIcon />)
    add(true, 'nav.subscriptions', '/mailbox/subscriptions', <ListIcon />)
    add(services?.isContactsEnabled !== false, 'nav.contacts', '/contacts', <UserIcon />)
    add(
      services?.isCalendarEnabled !== false || services?.isRemindersEnabled === true,
      'nav.calendar',
      '/calendar',
      <CalendarIcon />,
    )
    add(
      services?.isPhotosEnabled === true && hasPermission(permissions, 'photos:use'),
      'nav.photos',
      '/photos',
      <PictureIcon />,
    )
    add(hasPermission(permissions, 'location:use'), 'location.title', '/location', <LocationIcon />)
    add(hasPermission(permissions, 'health:use'), 'health.title', '/health', <HeartIcon />)
    add(hasPermission(permissions, 'passwords:use'), 'passwords.title', '/passwords', <LockIcon />)
    add(hasPermission(permissions, 'shopping:use'), 'shopping.title', '/shopping', <CartIcon />)
  } else {
    const hasAssistant = services?.isAssistantEnabled === true && hasAgentUse
    add(hasAssistant, 'nav.chats', '/chat', <ChatIcon />)
    add(hasJournal, 'journal.title', '/journal', <JournalIcon />)
    add(hasAssistant, 'knowledge.title', '/knowledge', <GridIcon />)
    add(hasAgentUse, 'recordings.title', '/recordings', <RecordIcon />)
    add(
      services?.isCalendarEnabled === true && hasPermission(permissions, 'calendar:use'),
      'nav.calendar',
      '/calendar',
      <CalendarIcon />,
    )
    add(
      services?.isRemindersEnabled === true &&
        hasPermission(permissions, 'calendar:use') &&
        services.isCalendarEnabled !== true,
      'services.remindersLabel',
      '/reminders',
      <CalendarIcon />,
    )
    add(hasPermission(permissions, 'location:use'), 'location.title', '/location', <LocationIcon />)
    add(hasPermission(permissions, 'health:use'), 'health.title', '/health', <HeartIcon />)
    add(hasPermission(permissions, 'passwords:use'), 'passwords.title', '/passwords', <LockIcon />)
    add(hasPermission(permissions, 'shopping:use'), 'shopping.title', '/shopping', <CartIcon />)
    add(
      services?.isContactsEnabled === true && hasPermission(permissions, 'contacts:use'),
      'nav.contacts',
      '/contacts',
      <UserIcon />,
    )
    add(context.hasCoding && hasAgentUse, 'nav.coding', '/coding', <CodeIcon />)
    add(
      services?.isPhotosEnabled === true && hasPermission(permissions, 'photos:use'),
      'nav.photos',
      '/photos',
      <PictureIcon />,
    )
    add(context.isFinanceShown, 'finance.title', '/finance', <WalletIcon />)
  }
  // The rows of the mailbox's rail that are the account's pages (Knowledge,
  // Recordings, Coding, Finance), and then the rest of the account's own.
  for (const item of [...MAILBOX_RAIL_ITEMS, ...ACCOUNT_GROUP.items]) {
    if (isItemShown(item, context)) add(true, item.label, item.to, item.icon)
  }
  // The account menu's own rows.
  add(true, 'nav.settings', SETTINGS_LANDING, <SettingsIcon />)
  const canUseCircles = hasPermission(permissions, 'circle:use')
  add(canUseCircles, 'circles.title', '/circles', <PeopleIcon />)
  add(canUseCircles, 'sharing.title', '/sharing', <ShareIcon />)
  const managePath = firstManagementPath(permissions, services)
  // Manage is a door to the first management page, whose tabs are offered with
  // that page's own row.
  add(managePath !== undefined, 'nav.manage', managePath ?? '', <GridIcon />, false)
  for (const item of GROUPS.flatMap((group) => group.items)) {
    if (isItemShown(item, context)) add(true, item.label, item.to, item.icon)
  }
  // A page the rail lists in two places is offered once.
  const seen = new Set<string>()
  return pages.filter((page) => {
    const identity = `${page.to} ${page.label}`
    if (seen.has(identity)) return false
    seen.add(identity)
    return true
  })
}

const STORAGE_KEY = 'teanode.sidebar.collapsed'

// The single mobile boundary. Everything that switches between the two
// layouts asks this, so there is one number rather than one per component.
const DESKTOP = '(min-width: 861px)'

export function useIsDesktop(): boolean {
  const [desktop, setDesktop] = useState(() => window.matchMedia(DESKTOP).matches)
  useEffect(() => {
    const query = window.matchMedia(DESKTOP)
    const onChange = (event: MediaQueryListEvent) => setDesktop(event.matches)
    query.addEventListener('change', onChange)
    return () => query.removeEventListener('change', onChange)
  }, [])
  return desktop
}

export function useSidebar(): [boolean, () => void] {
  const [collapsed, setCollapsed] = useState(() => window.localStorage.getItem(STORAGE_KEY) === 'true')

  const toggleSidebar = useCallback(() => {
    setCollapsed((previous) => {
      const next = !previous
      window.localStorage.setItem(STORAGE_KEY, String(next))
      return next
    })
  }, [])

  return [collapsed, toggleSidebar]
}

export function Sidebar({
  collapsed,
  onToggle,
  open,
  onOpen,
  onClose,
  account,
}: {
  collapsed: boolean
  // Narrows the rail to its icons. On the rail rather than on a bar above the
  // page: it is the rail's own width it changes, and the bar it used to sit on
  // had nothing else left in it. Absent on a phone, where the rail is an
  // overlay that is opened and closed rather than narrowed.
  onToggle?: () => void
  open: boolean
  // Opens the overlay a phone has for a rail, for the search key.
  onOpen?: () => void
  onClose: () => void
  account?: React.ReactNode
}) {
  const { t } = useTranslation()
  const location = useLocation()

  // Two things the rail says about the server without being asked: that a
  // release is waiting, and that this page is older than the server serving
  // it.
  const { staleBundle, upgradeAvailable } = useFreshness()

  // Your own settings are a mode, not a page. They are four short pages about
  // one account, and swapping the rail for them is what makes them reachable
  // from each other. The server's settings are not a mode: they are rows in
  // the ordinary rail, because they are configuration of the thing the rail
  // is already about.
  const surface = matchSettingsSurface(location.pathname)
  const navigate = useNavigate()
  const mailboxes = useMailboxes()
  const session = useSession()
  const agent = useAgentIdentity()

  // Somebody whose only permission is over their own mailbox has no
  // management side, and a person with no mailbox at all (the console, or a
  // group with no mail:read) has no mailbox side.
  const hasMailbox = Boolean(session.userId) && (!mailboxes.loaded || mailboxes.views.length > 0)
  const current = mailboxes.current

  // The mailbox is where the page opens and where most people stay, and the
  // agent's conversations, Knowledge and Finance are rows of its rail, so
  // they keep it. Without a mailbox there is no such rail, and Knowledge and
  // Finance are rows of the account's instead. The management pages, all
  // under /manage, are a mode entered from the foot of the rail, in
  // parallel with the account settings, and left by the row at the top of
  // it. Somebody with nothing to manage never sees that mode at all.
  const inMailbox =
    [
      '/chat',
      '/journal',
      '/knowledge',
      '/recordings',
      '/coding',
      '/finance',
      '/calendar',
      '/contacts',
      '/reminders',
      '/photos',
      '/location',
      '/health',
      '/passwords',
      '/shopping',
      '/circles',
      '/browser',
      '/people',
      '/search',
    ].some((path) => location.pathname === path || location.pathname.startsWith(path + '/')) ||
    location.pathname === '/mailbox' ||
    location.pathname.startsWith('/mailbox/') ||
    (hasMailbox && (location.pathname === '/chat' || surface?.rail === 'mailbox'))
  const inAccount = surface?.category === 'account' && !inMailbox
  const inManagement = !inAccount && !inMailbox

  // Only the rows the caller may open. What is hidden here is refused by the
  // server anyway; hiding it is the courtesy of not offering a door that
  // does not open. A group with no rows left is not drawn at all.
  // Finance is a row only for somebody who has finance, or whom somebody
  // shares finance with. Asked whenever somebody is signed in, whichever rail
  // is shown, because the search box offers the page from the management rail
  // too.
  const finance = useAgentFinancePresence(Boolean(session.userId))
  // Coding is a row only where a computer reports herdr, asked under the
  // same condition.
  const coding = useHerdrPresence(Boolean(session.userId) && hasPermission(session.permissions, 'agent:use'))
  const hasCoding = coding.isPresent
  // The Coding row counts the sessions that ask something, as the mail rows
  // count what is unread: a number there is a question waiting.
  const codingCount = (to: string) =>
    to === '/coding' && coding.askingCount > 0 ? (
      <span className="sidebar-count" aria-label={t('herdr.askingCount', { count: coding.askingCount })}>
        {coding.askingCount}
      </span>
    ) : null
  const railContext: RailContext = {
    permissions: session.permissions,
    services: session.services,
    hasMailbox,
    isFinanceShown: finance.isShown === true || finance.isShared,
    hasCoding: hasCoding === true,
  }
  const permitted = (item: Item) => isItemShown(item, railContext)
  // What the search box offers: the pages the rail, the account menu and the
  // management side would show this person.
  const pages = navigablePages({
    ...railContext,
    hasMailboxRail: Boolean(current),
    isAgentAvailable: agent.isAvailable,
  })
  const accountGroup: Group = {
    ...ACCOUNT_GROUP,
    items: ACCOUNT_GROUP.items.filter((item) => !(hasMailbox && item.rail === 'mailbox')),
  }
  const groups = (inAccount ? [accountGroup] : inMailbox ? [] : GROUPS)
    .map((group) => ({ ...group, items: group.items.filter(permitted) }))
    .filter((group) => group.items.length > 0)
  // The agent's row at the top of the mailbox's rail is called what the
  // agent is called, and is there only when there is an agent to talk to.
  // The agent's part holds the chats with people and groups too, so it is named Chats.
  const agentLabel = t('nav.chats')

  return (
    <>
      {/* On a phone the rail is an overlay, and the scrim behind it is how it
          is dismissed. Tapping the page you can see is the gesture people
          already try. */}
      {open && <div className="scrim" onClick={onClose} aria-hidden="true" />}
      <aside className={['sidebar', collapsed ? 'collapsed' : '', open ? 'open' : ''].filter(Boolean).join(' ')}>
        {/* The product belongs at the top of its own navigation, not on the bar
            across the page: the bar says where you are, and the rail says what
            this is. Collapsed, the mark stays and the word goes. */}
        <div className="sidebar-top">
          {/* The mark alone. The word beside it said what the mark already
              says, twice, at the top of every page. */}
          <Link className="sidebar-brand" to="/" onClick={onClose} aria-label={t('app.name')}>
            <Logo size={22} />
          </Link>

          {/* The server has been upgraded under this page, so what is loaded
              in the browser is the old dashboard talking to the new server.
              Usually harmless and occasionally a field that no longer exists,
              which is why this asks rather than reloading underneath somebody
              in the middle of writing a message. */}
          {staleBundle && (
            <Tooltip label={t('nav.refreshTooltip')}>
              <button
                type="button"
                className="sidebar-refresh"
                aria-label={t('nav.refreshTooltip')}
                onClick={() => window.location.reload()}
              >
                <RefreshIcon size={16} />
              </button>
            </Tooltip>
          )}
        </div>

        {/* Across everything the person has, above the rail's own rows. Only
            for somebody the search can be for: the console has no chats,
            mail or photos of its own to look through. */}
        {session.userId && (
          <SidebarSearch
            pages={pages}
            folders={current?.folders ?? []}
            isHidden={collapsed || (!onToggle && !open)}
            onReveal={() => (collapsed ? onToggle?.() : onOpen?.())}
            onSubmitted={onClose}
          />
        )}

        <nav onClick={onClose}>
          {/* The way back out. First, and on its own, because it is the one
              row that changes what the rail is showing rather than where in
              it you are. */}
          {(inAccount || inManagement) && hasMailbox && (
            <div className="sidebar-group">
              <Link className="sidebar-back" to="/mailbox" title={collapsed ? t('nav.backToMailbox') : undefined}>
                <span className="sidebar-icon flip">
                  <ChevronRightIcon size={18} />
                </span>
                <span className="sidebar-label">{t('nav.backToMailbox')}</span>
              </Link>
            </div>
          )}

          {/* The mailbox: which one, when there are several, then its
              folders with what is unread in each. The tree is here rather
              than in the page because it is navigation, and the rail is
              where navigation lives. */}
          {inMailbox && current && (
            <div className="sidebar-group">
              {/* The agent, first and by its name: its conversations, as a
                  page of their own. Above the mailbox's folders because it
                  reads every mailbox, not one. */}
              {agent.isAvailable && (
                <NavLink to="/chat" end title={collapsed ? agentLabel : undefined}>
                  <span className="sidebar-icon">
                    <ChatIcon />
                  </span>
                  <span className="sidebar-label">{agentLabel}</span>
                </NavLink>
              )}
              {hasPermission(session.permissions, 'journal:use') && (
                <NavLink to="/journal" title={collapsed ? t('journal.title') : undefined}>
                  <span className="sidebar-icon">
                    <JournalIcon />
                  </span>
                  <span className="sidebar-label">{t('journal.title')}</span>
                </NavLink>
              )}
              {(agent.isAvailable || hasPermission(session.permissions, 'journal:use')) && (
                <div className="sidebar-divider" />
              )}
              {/* Only when there is a choice to make. One mailbox named at
                  the top of its own rail is a heading that says nothing: the
                  rows under it are that mailbox's folders and there is
                  nothing else they could be. */}
              {/* The dashboard's own dropdown rather than the browser's: the
                  rail is dark, and a native select opens its list in the
                  operating system's colors — a white rectangle over a dark
                  column. */}
              {mailboxes.views.length > 1 && (
                <div className="sidebar-mailbox" onClick={(event) => event.stopPropagation()}>
                  <Select
                    label={t('nav.chooseMailbox')}
                    value={current.mailbox.id}
                    options={mailboxes.views.map((view) => ({
                      value: view.mailbox.id,
                      label: view.unread > 0 ? `${view.mailbox.name} (${view.unread})` : view.mailbox.name,
                    }))}
                    onChange={(mailboxId) => {
                      mailboxes.setCurrentId(mailboxId)
                      navigate('/mailbox')
                    }}
                  />
                </div>
              )}
              {(() => {
                const { inbox, pinned, rest } = railRows(current.folders)
                // A row of the tree, or of the pinned area at the top. The
                // rail is for going places; pinning is done on the Folders
                // tab, where a folder is also renamed and removed. A control
                // that only appears under the pointer, on a row whose whole
                // job is to be clicked, was a second thing to aim at on every
                // row and a thing a phone could not reach at all.
                const folderRow = (folder: MailboxFolder, depth: number, key: string) => {
                  const label = folderLabel(t, folder)
                  return (
                    <NavLink
                      key={key}
                      to={`/mailbox/${folder.id}`}
                      className={folder.unread > 0 && folder.kind !== 'archive' ? 'unread' : undefined}
                      data-depth={Math.min(depth, 3)}
                      title={collapsed ? `${label}${folder.unread > 0 ? ` (${folder.unread})` : ''}` : undefined}
                    >
                      <span className="sidebar-icon">
                        <FolderKindIcon kind={folder.kind} />
                      </span>
                      <span className="sidebar-label">{label}</span>
                      {/* The Archive is where read-later goes: a count there is
                          a nag, not news. */}
                      {folder.unread > 0 && folder.kind !== 'archive' && (
                        <span className="sidebar-count" aria-label={t('mailbox.unreadCount', { count: folder.unread })}>
                          {folder.unread}
                        </span>
                      )}
                    </NavLink>
                  )
                }
                return (
                  <>
                    {/* Writing one, above reading them. Not a folder — there
                        is nothing to count and nothing to open — so it is
                        drawn as what it is: the one thing here that makes
                        something rather than showing something. */}
                    <NavLink
                      className="sidebar-compose"
                      to="/mailbox/compose"
                      title={collapsed ? t('mailbox.newMessage') : undefined}
                    >
                      <span className="sidebar-icon">
                        <ComposeIcon />
                      </span>
                      <span className="sidebar-label">{t('mailbox.newMessage')}</span>
                    </NavLink>
                    {inbox.map(({ folder, depth }) => folderRow(folder, depth, folder.id))}
                    <NavLink
                      to="/mailbox/starred"
                      className={current.starredUnread > 0 ? 'unread' : undefined}
                      title={collapsed ? t('mailbox.folder.starred') : undefined}
                    >
                      <span className="sidebar-icon">
                        <FolderKindIcon kind="starred" />
                      </span>
                      <span className="sidebar-label">{t('mailbox.folder.starred')}</span>
                      {current.starredUnread > 0 && (
                        <span
                          className="sidebar-count"
                          aria-label={t('mailbox.unreadCount', { count: current.starredUnread })}
                        >
                          {current.starredUnread}
                        </span>
                      )}
                    </NavLink>
                    {/* What the agent said matters today. Only where the
                        agent sorts this mailbox; a view that is always empty
                        would be a question with no answer. */}
                    {current.mailbox.agent?.granted && current.mailbox.agent.triage?.enabled && (
                      <NavLink
                        to="/mailbox/priority"
                        className={current.priorityUnread > 0 ? 'unread' : undefined}
                        title={collapsed ? t('mailbox.folder.priority') : undefined}
                      >
                        <span className="sidebar-icon">
                          <FolderKindIcon kind="priority" />
                        </span>
                        <span className="sidebar-label">{t('mailbox.folder.priority')}</span>
                        {current.priorityUnread > 0 && (
                          <span
                            className="sidebar-count"
                            aria-label={t('mailbox.unreadCount', { count: current.priorityUnread })}
                          >
                            {current.priorityUnread}
                          </span>
                        )}
                      </NavLink>
                    )}
                    {pinned.map(({ folder, depth }) => folderRow(folder, depth, `pinned-${folder.id}`))}
                    {/* What is always at the top — the inbox, what is
                        starred, and whatever has been pinned up there — ends
                        here, and the mailbox's own folders start. The rule is
                        drawn whether or not anything is pinned, so the rail
                        does not change shape the first time somebody pins
                        something. */}
                    <div className="sidebar-divider" />
                    {rest.map(({ folder, depth }) => folderRow(folder, depth, folder.id))}
                  </>
                )
              })()}
              {/* The folders end here; what is below is about the mailbox
                  rather than in it. */}
              <div className="sidebar-divider" />
              <NavLink to="/mailbox/subscriptions" title={collapsed ? t('nav.subscriptions') : undefined}>
                <span className="sidebar-icon">
                  <ListIcon />
                </span>
                <span className="sidebar-label">{t('nav.subscriptions')}</span>
              </NavLink>
              {session.services?.isContactsEnabled !== false && (
                <NavLink to="/contacts" title={collapsed ? t('nav.contacts') : undefined}>
                  <span className="sidebar-icon">
                    <UserIcon />
                  </span>
                  <span className="sidebar-label">{t('nav.contacts')}</span>
                </NavLink>
              )}
              {(session.services?.isCalendarEnabled !== false || session.services?.isRemindersEnabled) && (
                <NavLink to="/calendar" title={collapsed ? t('nav.calendar') : undefined}>
                  <span className="sidebar-icon">
                    <CalendarIcon />
                  </span>
                  <span className="sidebar-label">{t('nav.calendar')}</span>
                </NavLink>
              )}
              {session.services?.isPhotosEnabled === true && hasPermission(session.permissions, 'photos:use') && (
                <NavLink to="/photos" title={collapsed ? t('nav.photos') : undefined}>
                  <span className="sidebar-icon">
                    <PictureIcon />
                  </span>
                  <span className="sidebar-label">{t('nav.photos')}</span>
                </NavLink>
              )}
              {hasPermission(session.permissions, 'location:use') && (
                <NavLink to="/location" title={collapsed ? t('location.title') : undefined}>
                  <span className="sidebar-icon">
                    <LocationIcon />
                  </span>
                  <span className="sidebar-label">{t('location.title')}</span>
                </NavLink>
              )}
              {hasPermission(session.permissions, 'health:use') && (
                <NavLink to="/health" title={collapsed ? t('health.title') : undefined}>
                  <span className="sidebar-icon">
                    <HeartIcon />
                  </span>
                  <span className="sidebar-label">{t('health.title')}</span>
                </NavLink>
              )}
              {hasPermission(session.permissions, 'passwords:use') && (
                <NavLink to="/passwords" title={collapsed ? t('passwords.title') : undefined}>
                  <span className="sidebar-icon">
                    <LockIcon />
                  </span>
                  <span className="sidebar-label">{t('passwords.title')}</span>
                </NavLink>
              )}
              {hasPermission(session.permissions, 'shopping:use') && (
                <NavLink to="/shopping" title={collapsed ? t('shopping.title') : undefined}>
                  <span className="sidebar-icon">
                    <CartIcon />
                  </span>
                  <span className="sidebar-label">{t('shopping.title')}</span>
                </NavLink>
              )}
              {/* What the agent knows and what the person's money did, under
                  the calendar: pages read often, not settings. The mailbox's
                  own settings are with the rest of the person's, in the
                  account's rail. */}
              {MAILBOX_RAIL_ITEMS.filter(permitted).map((item) => {
                const label = t(item.label)
                return (
                  <NavLink key={item.to} to={item.to} title={collapsed ? label : undefined}>
                    <span className="sidebar-icon">{item.icon}</span>
                    <span className="sidebar-label">{label}</span>
                    {codingCount(item.to)}
                  </NavLink>
                )
              })}
            </div>
          )}

          {!current && !inManagement && (
            <div className="sidebar-group">
              {session.services?.isAssistantEnabled && hasPermission(session.permissions, 'agent:use') && (
                <NavLink to="/chat">
                  <ChatIcon />
                  <span className="sidebar-label">{t('nav.chats')}</span>
                </NavLink>
              )}
              {hasPermission(session.permissions, 'journal:use') && (
                <NavLink to="/journal">
                  <JournalIcon />
                  <span className="sidebar-label">{t('journal.title')}</span>
                </NavLink>
              )}
              {session.services?.isAssistantEnabled && hasPermission(session.permissions, 'agent:use') && (
                <NavLink to="/knowledge">
                  <GridIcon />
                  <span className="sidebar-label">{t('knowledge.title')}</span>
                </NavLink>
              )}
              {hasPermission(session.permissions, 'agent:use') && (
                <NavLink to="/recordings">
                  <RecordIcon />
                  <span className="sidebar-label">{t('recordings.title')}</span>
                </NavLink>
              )}
              {session.services?.isCalendarEnabled && hasPermission(session.permissions, 'calendar:use') && (
                <NavLink to="/calendar">
                  <CalendarIcon />
                  <span className="sidebar-label">{t('nav.calendar')}</span>
                </NavLink>
              )}
              {session.services?.isRemindersEnabled &&
                hasPermission(session.permissions, 'calendar:use') &&
                !session.services.isCalendarEnabled && (
                  <NavLink to="/reminders">
                    <CalendarIcon />
                    <span className="sidebar-label">{t('services.remindersLabel')}</span>
                  </NavLink>
                )}
              {hasPermission(session.permissions, 'location:use') && (
                <NavLink to="/location">
                  <LocationIcon />
                  <span className="sidebar-label">{t('location.title')}</span>
                </NavLink>
              )}
              {hasPermission(session.permissions, 'health:use') && (
                <NavLink to="/health">
                  <HeartIcon />
                  <span className="sidebar-label">{t('health.title')}</span>
                </NavLink>
              )}
              {hasPermission(session.permissions, 'passwords:use') && (
                <NavLink to="/passwords">
                  <LockIcon />
                  <span className="sidebar-label">{t('passwords.title')}</span>
                </NavLink>
              )}
              {hasPermission(session.permissions, 'shopping:use') && (
                <NavLink to="/shopping">
                  <CartIcon />
                  <span className="sidebar-label">{t('shopping.title')}</span>
                </NavLink>
              )}
              {session.services?.isContactsEnabled && hasPermission(session.permissions, 'contacts:use') && (
                <NavLink to="/contacts">
                  <UserIcon />
                  <span className="sidebar-label">{t('nav.contacts')}</span>
                </NavLink>
              )}
              {hasCoding === true && hasPermission(session.permissions, 'agent:use') && (
                <NavLink to="/coding">
                  <CodeIcon />
                  <span className="sidebar-label">{t('nav.coding')}</span>
                  {codingCount('/coding')}
                </NavLink>
              )}
              {session.services?.isPhotosEnabled === true && hasPermission(session.permissions, 'photos:use') && (
                <NavLink to="/photos">
                  <PictureIcon />
                  <span className="sidebar-label">{t('nav.photos')}</span>
                </NavLink>
              )}
              {(finance.isShown || finance.isShared) && (
                <NavLink to="/finance">
                  <WalletIcon />
                  <span className="sidebar-label">{t('finance.title')}</span>
                </NavLink>
              )}
            </div>
          )}

          {groups.map((group, index) => (
            <div className="sidebar-group" key={index}>
              {/* Hidden when the rail is collapsed to icons: a label with no
                  room to be read is a gray smear above the icons. */}
              {group.label && <div className="sidebar-group-label sidebar-label">{t(group.label)}</div>}
              {group.items.map((item) => {
                const label = t(item.label)
                // The one row that has something waiting on it. A dot rather
                // than a number or a word: it says "look here" and nothing
                // else, which is all a rail should say.
                const marked = upgradeAvailable && item.to === '/manage/server'
                return (
                  <NavLink key={item.to} to={item.to} title={collapsed ? label : undefined}>
                    <span className="sidebar-icon">{item.icon}</span>
                    {/* Rendered even when collapsed, and hidden with CSS: a
                        screen reader still needs the name of the link, and the
                        label should not have to be re-read when the rail
                        reopens. */}
                    <span className="sidebar-label">{label}</span>
                    {codingCount(item.to)}
                    {marked && (
                      <Tooltip label={t('nav.upgradeAvailable')}>
                        <span className="sidebar-dot">
                          <span className="visually-hidden">{t('nav.upgradeAvailable')}</span>
                        </span>
                      </Tooltip>
                    )}
                  </NavLink>
                )
              })}
            </div>
          ))}
        </nav>

        {/* Narrowing the rail, and who you are, at the foot. The control is
            here rather than beside the mark at the top because narrow is a
            state you have to be able to leave: at the top there was no room
            for it once the rail had narrowed, and a control that disappears
            when you use it is a trap. */}
        {(onToggle || account) && <div className="sidebar-account">{account}</div>}
      </aside>
    </>
  )
}
