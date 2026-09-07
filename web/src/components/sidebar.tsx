import { useCallback, useEffect, useState } from 'react'
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom'

import { Key, useTranslation } from '../i18n/i18n'
import {
  ChevronRightIcon,
  DomainsIcon,
  GridIcon,
  RefreshIcon,
  KeyIcon,
  LogoutIcon,
  MailIcon,
  QueueIcon,
  ServerIcon,
  ServiceIcon,
  SettingsIcon,
  SetupIcon,
  ShieldIcon,
  TerminalIcon,
  UserIcon,
} from './icons'
import { Logo } from './logo'
import { matchSettingsSurface, surfacesByCategory } from '../pages/settings/nav'
import { useFreshness } from './freshness'
import { hasAnywhere, hasPermission, useSession } from '../session'
import { folderLabel, folderRows, useMailboxes } from '../mailboxes'

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
      ...surfacesByCategory('server').map((surface) => ({
        label: surface.label,
        to: surface.path,
        icon: SERVER_ICONS[surface.segment],
        anyOf: ['server:manage', 'user:manage', 'group:manage', 'role:manage', 'audit:read'],
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
            <button
              type="button"
              className="sidebar-refresh"
              title={t('nav.refreshTooltip')}
              aria-label={t('nav.refreshTooltip')}
              onClick={() => window.location.reload()}
            >
              <RefreshIcon size={16} />
            </button>
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
              <div className="sidebar-mailbox">
                {mailboxes.views.length > 1 ? (
                  <select
                    aria-label={t('nav.chooseMailbox')}
                    value={current.mailbox.id}
                    onClick={(event) => event.stopPropagation()}
                    onChange={(event) => {
                      mailboxes.setCurrentId(event.target.value)
                      navigate('/mailbox')
                    }}
                  >
                    {mailboxes.views.map((view) => (
                      <option key={view.mailbox.id} value={view.mailbox.id}>
                        {view.mailbox.name}
                        {view.unread > 0 ? ` (${view.unread})` : ''}
                      </option>
                    ))}
                  </select>
                ) : (
                  <div className="sidebar-mailbox-name sidebar-label" title={current.mailbox.name}>
                    {current.mailbox.name}
                  </div>
                )}
              </div>
              {folderRows(current.folders).map(({ folder, depth }) => {
                const label = folderLabel(t, folder)
                return (
                  <NavLink
                    key={folder.id}
                    to={`/mailbox/${folder.id}`}
                    className={folder.unread > 0 ? 'unread' : undefined}
                    data-depth={Math.min(depth, 3)}
                    title={collapsed ? `${label}${folder.unread > 0 ? ` (${folder.unread})` : ''}` : undefined}
                  >
                    <span className="sidebar-icon">
                      <MailIcon />
                    </span>
                    <span className="sidebar-label">{label}</span>
                    {folder.unread > 0 && (
                      <span className="sidebar-count" aria-label={t('mailbox.unreadCount', { count: folder.unread })}>
                        {folder.unread}
                      </span>
                    )}
                  </NavLink>
                )
              })}
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
                  room to be read is a grey smear above the icons. */}
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
                      <span className="sidebar-dot" title={t('nav.upgradeAvailable')}>
                        <span className="visually-hidden">{t('nav.upgradeAvailable')}</span>
                      </span>
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
              <button
                type="button"
                className="sidebar-collapse"
                title={t('nav.manageTooltip')}
                aria-label={t('nav.manage')}
                onClick={() => navigate(firstManagementRow.to)}
              >
                <span className="sidebar-icon">
                  <GridIcon />
                </span>
                <span className="sidebar-label">{t('nav.manage')}</span>
              </button>
            )}
            {onToggle && (
              <button
                type="button"
                className="sidebar-collapse"
                aria-label={collapsed ? t('nav.expand') : t('nav.collapse')}
                title={collapsed ? t('nav.expand') : t('nav.collapse')}
                aria-expanded={!collapsed}
                onClick={onToggle}
              >
                <span className="sidebar-icon">
                  <ChevronRightIcon size={18} />
                </span>
                <span className="sidebar-label">{t('nav.collapse')}</span>
              </button>
            )}
            {account}
          </div>
        )}
      </aside>
    </>
  )
}
