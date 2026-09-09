import React, { useCallback, useEffect, useState } from 'react'
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom'

import { Key, useTranslation } from '../i18n/i18n'
import {
  ChevronRightIcon,
  ComposeIcon,
  DomainsIcon,
  GridIcon,
  RefreshIcon,
  KeyIcon,
  LogoutIcon,
  MailIcon,
  PeopleIcon,
  QueueIcon,
  ServerIcon,
  ServiceIcon,
  SettingsIcon,
  SetupIcon,
  ShieldIcon,
  TerminalIcon,
  ListIcon,
  UserIcon,
} from './icons'
import { Logo } from './logo'
import { matchSettingsSurface, surfacesByCategory } from '../pages/settings/nav'
import { useFreshness } from './freshness'
import { hasAnywhere, hasPermission, useSession } from '../session'
import { folderLabel, railRows, useMailboxes } from '../mailboxes'
import { FolderKindIcon } from './folderIcon'
import { MailboxFolder } from '../api'
import { Select } from './select'
import { Tooltip } from './tooltip'

// permission is what a row needs, when it needs one: a domain permission held
// over at least one domain, or a server permission. A row nothing gates is
// for everyone who is signed in.
type Item = { label: Key; to: string; icon: React.ReactNode; anyOf?: string[] }
type Group = { label?: Key; items: Item[] }

// One icon per settings surface that appears in the rail. Here rather than in
// nav.ts, which stays free of React so anything can read it.
const SERVER_ICONS: Record<string, React.ReactNode> = {
  setup: <SetupIcon />,
  integrations: <ServiceIcon />,
  server: <ServerIcon />,
}

const ACCOUNT_ICONS: Record<string, React.ReactNode> = {
  profile: <UserIcon />,
  password: <KeyIcon />,
  passkeys: <ShieldIcon />,
  tokens: <TerminalIcon />,
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
  })),
}

// The rail. Two groups, each under its own label: what is arriving and what is
// stuck, then what it arrives for and how this server is set up. They are
// separate questions, asked at different times.
//
// The server's own settings are rows here rather than pages behind a Settings
// row, because they are configuration of the thing the rail is about, and one
// more click to reach a page is one more click every time. What configures the
// person signed in is not here: it hangs off their name at the foot, which is
// where people look for it.
const GROUPS: Group[] = [
  {
    label: 'nav.groupMail',
    items: [
      { label: 'nav.mail', to: '/mail', icon: <MailIcon />, anyOf: ['mail:audit'] },
      { label: 'nav.queue', to: '/queue', icon: <QueueIcon />, anyOf: ['queue:manage'] },
      { label: 'nav.reports', to: '/reports', icon: <ShieldIcon />, anyOf: ['report:read'] },
    ],
  },
  {
    label: 'nav.groupConfiguration',
    items: [
      { label: 'nav.domains', to: '/domains', icon: <DomainsIcon />, anyOf: ['domain:manage'] },
      {
        label: 'nav.access',
        to: '/access',
        icon: <PeopleIcon />,
        anyOf: ['user:manage', 'group:manage', 'role:manage', 'audit:read'],
      },
      ...surfacesByCategory('server').map((surface) => ({
        label: surface.label,
        to: surface.path,
        icon: SERVER_ICONS[surface.segment],
        anyOf: ['server:manage'],
      })),
    ],
  },
]

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
  const inAccount = surface?.category === 'account'

  // The mailbox is where the page opens and where most people stay. The
  // management pages — every message, the queue, reports, domains, the
  // server — are a mode entered from the foot of the rail, in parallel with
  // the account settings, and left by the row at the top of it. Somebody
  // with nothing to manage never sees that mode at all.
  const inMailbox = location.pathname === '/mailbox' || location.pathname.startsWith('/mailbox/')
  const inManagement = !inAccount && !inMailbox
  const navigate = useNavigate()
  const mailboxes = useMailboxes()

  // Only the rows the caller may open. What is hidden here is refused by the
  // server anyway; hiding it is the courtesy of not offering a door that
  // does not open. A group with no rows left is not drawn at all.
  const session = useSession()
  const permitted = (item: Item) =>
    !item.anyOf ||
    item.anyOf.some((key) => hasPermission(session.permissions, key) || hasAnywhere(session.permissions, key))
  const groups = (inAccount ? [ACCOUNT_GROUP] : inMailbox ? [] : GROUPS)
    .map((group) => ({ ...group, items: group.items.filter(permitted) }))
    .filter((group) => group.items.length > 0)

  // Where "Manage" goes: the first management row this person may open.
  const firstManagementRow = GROUPS.flatMap((group) => group.items).find(permitted)
  // Somebody whose only permission is over their own mailbox has no
  // management side, and a person with no mailbox at all (the console, or a
  // group with no mail:read) has no mailbox side.
  const hasMailbox = Boolean(session.userId) && (!mailboxes.loaded || mailboxes.views.length > 0)
  const current = mailboxes.current

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
          <Link className="sidebar-brand" to="/" onClick={onClose}>
            <Logo size={22} />
            <span className="sidebar-label">{t('app.name')}</span>
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
                      className={folder.unread > 0 ? 'unread' : undefined}
                      data-depth={Math.min(depth, 3)}
                      title={collapsed ? `${label}${folder.unread > 0 ? ` (${folder.unread})` : ''}` : undefined}
                    >
                      <span className="sidebar-icon">
                        <FolderKindIcon kind={folder.kind} />
                      </span>
                      <span className="sidebar-label">{label}</span>
                      {folder.unread > 0 && (
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
                    <NavLink to="/mailbox/starred" title={collapsed ? t('mailbox.folder.starred') : undefined}>
                      <span className="sidebar-icon">
                        <FolderKindIcon kind="starred" />
                      </span>
                      <span className="sidebar-label">{t('mailbox.folder.starred')}</span>
                    </NavLink>
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
              <NavLink to="/mailbox/contacts" title={collapsed ? t('nav.contacts') : undefined}>
                <span className="sidebar-icon">
                  <UserIcon />
                </span>
                <span className="sidebar-label">{t('nav.contacts')}</span>
              </NavLink>
              <NavLink to="/mailbox/settings" title={collapsed ? t('nav.mailboxSettings') : undefined}>
                <span className="sidebar-icon">
                  <SettingsIcon />
                </span>
                <span className="sidebar-label">{t('nav.mailboxSettings')}</span>
              </NavLink>
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
                const marked = upgradeAvailable && item.to === '/server'
                return (
                  <NavLink key={item.to} to={item.to} title={collapsed ? label : undefined}>
                    <span className="sidebar-icon">{item.icon}</span>
                    {/* Rendered even when collapsed, and hidden with CSS: a
                        screen reader still needs the name of the link, and the
                        label should not have to be re-read when the rail
                        reopens. */}
                    <span className="sidebar-label">{label}</span>
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
        {(onToggle || account) && (
          <div className="sidebar-account">
            {/* Into management mode. Beside the account menu because the two
                are the same kind of thing: a mode about something other than
                the mailbox, entered on purpose and left by the row at the
                top. Only for somebody who has anything to manage. */}
            {!inManagement && firstManagementRow && (
              <Tooltip label={t('nav.manageTooltip')}>
                <button
                  type="button"
                  className="sidebar-collapse"
                  aria-label={t('nav.manage')}
                  onClick={() => navigate(firstManagementRow.to)}
                >
                  <span className="sidebar-icon">
                    <GridIcon />
                  </span>
                  <span className="sidebar-label">{t('nav.manage')}</span>
                </button>
              </Tooltip>
            )}
            {onToggle && (
              <Tooltip label={collapsed ? t('nav.expand') : t('nav.collapse')}>
                <button
                  type="button"
                  className="sidebar-collapse"
                  aria-label={collapsed ? t('nav.expand') : t('nav.collapse')}
                  aria-expanded={!collapsed}
                  onClick={onToggle}
                >
                  <span className="sidebar-icon">
                    <ChevronRightIcon size={18} />
                  </span>
                  <span className="sidebar-label">{t('nav.collapse')}</span>
                </button>
              </Tooltip>
            )}
            {account}
          </div>
        )}
      </aside>
    </>
  )
}
