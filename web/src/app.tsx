import { useCallback, useEffect, useState } from 'react'
import { Navigate, Route, Routes, useLocation } from 'react-router-dom'

import { Session, getSession, logout } from './api'
import { LoginPage } from './pages/login'
import { MailPage } from './pages/mail'
import { MailDetailPage } from './pages/mailDetail'
import { MailboxPage } from './pages/mailbox'
import { MailboxSettingsPage } from './pages/mailboxSettings'
import { QueuePage } from './pages/queue'
import { ReportsPage } from './pages/reports'
import { ReportDetailPage } from './pages/reportDetail'
import { DomainsPage } from './pages/domains'
import { DomainTabsPage } from './pages/domainTabs'
import { TemplateEditorPage } from './pages/templateEditor'
import { LayoutEditorPage } from './pages/layoutEditor'
import { ComposePage } from './pages/compose'
import { SetupAccountPage } from './pages/setupAccount'
import { ChangePasswordPage } from './pages/changePasswordPage'
import { CommandLinePage } from './pages/cli'
import { TokensPage } from './pages/settings/tokens'
import { ServerPage } from './pages/server'
import { SessionsPage } from './pages/settings/sessions'
import { ProfilePage } from './pages/settings/profile'
import { PasskeysPage } from './pages/settings/passkeys'
import { SETTINGS_LANDING } from './pages/settings/nav'
import { ThemeToggle } from './components/theme'
import { LanguagePicker, useTranslation } from './i18n/i18n'
import { AccountMenu } from './components/accountMenu'
import { Sidebar, useIsDesktop, useSidebar } from './components/sidebar'
import { MenuIcon } from './components/icons'
import { Breadcrumb, BreadcrumbProvider, PageHeading } from './components/breadcrumb'
import { PasskeyNudge } from './components/passkeyNudge'
import { SessionProvider, hasAnywhere } from './session'
import { MailboxesProvider } from './mailboxes'

export function App() {
  const { t } = useTranslation()
  const [collapsed, toggleSidebar] = useSidebar()
  const desktop = useIsDesktop()
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [session, setSession] = useState<Session | null>(null)
  const location = useLocation()

  const refresh = useCallback(async () => {
    try {
      setSession(await getSession())
    } catch {
      // The server is unreachable. Show the login form rather than a blank
      // page; trying again is the only useful thing a reader can do. Without
      // a server there is nobody to answer a passkey, so none is offered.
      setSession({ authenticated: false, authenticationRequired: true, username: '', passkeysEnabled: false })
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  if (session === null) {
    // Nothing, not a word. Asking the server who you are takes a few
    // milliseconds, and "loading…" that appears and vanishes in that time is
    // a flicker on top of every page load.
    return <div className="content" />
  }

  // The language and appearance controls follow onto the pages that have no
  // shell to hold them, so somebody who cannot read the login form can fix
  // that before logging in.
  const corner = (
    <div className="page-corner">
      <LanguagePicker />
      <ThemeToggle />
    </div>
  )

  // A server with no account has never been set up. Ask the first arrival to
  // create one rather than showing a login form for credentials that do not
  // exist yet.
  if (!session.authenticationRequired) {
    return (
      <div className="auth-page">
        {corner}
        <SetupAccountPage onCreated={refresh} />
      </div>
    )
  }

  if (!session.authenticated) {
    return (
      <div className="auth-page">
        {corner}
        <LoginPage onLoggedIn={refresh} passkeysEnabled={session.passkeysEnabled} />
      </div>
    )
  }

  // The page "teanode auth login" opens is drawn like the login form — one
  // card and nothing else — because the reader was brought here to answer
  // one question, and the rail would be a list of other places to go.
  if (location.pathname === '/cli') {
    return (
      <div className="auth-page">
        {corner}
        <CommandLinePage username={session.username} />
      </div>
    )
  }

  // The rail runs the full height of the window and the bar sits beside it,
  // rather than the bar running across the top of both: the rail is the
  // product's own furniture, and the bar belongs to the page it is above.
  return (
    <SessionProvider value={session}>
    <MailboxesProvider>
    <BreadcrumbProvider>
      <div className="layout">
        <Sidebar
          collapsed={desktop && collapsed}
          onToggle={desktop ? toggleSidebar : undefined}
          open={!desktop && drawerOpen}
          onClose={() => setDrawerOpen(false)}
          account={
            session.username ? (
              <AccountMenu
                username={session.username}
                name={session.name}
                onLogout={async () => {
                  await logout()
                  await refresh()
                }}
              />
            ) : undefined
          }
        />

        <div className="main-column">
          {/* No bar across the top. What was on it was a control to collapse
              the rail, which belongs to the rail, and two menus about the
              reader, which belong with the reader's own name at its foot —
              leaving a 56-pixel band with a rule under it and nothing in it.
              On a phone the rail is an overlay, so the button that opens it
              has nowhere else to be and this comes back. */}
          {!desktop && (
            <header className="topbar">
              <button
                type="button"
                className="icon-button"
                aria-label={t('nav.toggle')}
                title={t('nav.toggle')}
                aria-expanded={drawerOpen}
                onClick={() => setDrawerOpen((previous) => !previous)}
              >
                <MenuIcon />
              </button>
              <div className="topbar-controls">
                {!session.username && <span className="muted">{t('nav.noAuthentication')}</span>}
              </div>
            </header>
          )}

          <main className="content">
            {/* Above everything, and on every page, until it is acted on or
                dismissed: a suggestion is only a suggestion if it can be
                declined. The console has no username and no passkeys, so it
                is never asked. */}
            {session.username && <PasskeyNudge username={session.username} />}
            {/* The way back up, then the name of where you are. Together
                rather than one on the bar and one in the page: they answer
                the same question, and split apart neither of them read as an
                answer. */}
            <Breadcrumb />
            <PageHeading />
            <Routes>
              {/* Home is the mailbox, for anyone who has one. The console
                  and an account without mail:read land on the first
                  management page instead. */}
              <Route path="/" element={<Navigate to={session.userId ? '/mailbox' : '/mail'} replace />} />
              <Route path="/mailbox" element={<MailboxPage />} />
              <Route path="/mailbox/settings" element={<MailboxSettingsPage />} />
              <Route path="/mailbox/settings/:tab" element={<MailboxSettingsPage />} />
              <Route path="/mailbox/:folderId" element={<MailboxPage />} />
              <Route path="/mailbox/:folderId/:itemId" element={<MailboxPage />} />
              {/* The operator's view of every message needs mail:audit;
                  without it this is not a page, and the mailbox is. */}
              <Route
                path="/mail"
                element={
                  hasAnywhere(session.permissions, 'mail:audit') ? <MailPage /> : <Navigate to="/mailbox" replace />
                }
              />
              {/* Before the message route: "compose" is not a message
                  identifier, and the router should never treat it as one. */}
              <Route path="/mail/compose" element={<ComposePage />} />
              <Route path="/mail/:mailId" element={<MailDetailPage />} />
              <Route path="/queue" element={<QueuePage />} />
              <Route path="/reports" element={<ReportsPage />} />
              <Route path="/reports/:reportId" element={<ReportDetailPage />} />

              <Route path="/domains" element={<DomainsPage />} />
              <Route path="/domains/:domainId" element={<Navigate to="overview" replace />} />
              {/* Everything a domain has is a tab of one page, and the two
                  item pages below are not tabs. They are declared first so
                  that "templates/<id>" reaches the editor rather than being
                  read as the name of a tab. */}
              <Route path="/domains/:domainId/templates/:templateId" element={<TemplateEditorPage />} />
              <Route path="/domains/:domainId/layouts/:layoutId" element={<LayoutEditorPage />} />
              <Route path="/domains/:domainId/:tab" element={<DomainTabsPage />} />

              {/* /settings on its own is not a page: the rail and the account
                  menu are the menu, and a page of cards pointing at the same
                  seven places was a page whose only content was that menu. */}
              {/* Everything about this server, under one row in the rail and
                  one path: what it is, what it talks to, and which version it
                  is running. Three rows made somebody choose between them
                  before knowing which one held the thing they wanted. */}
              <Route path="/server" element={<ServerPage />} />
              <Route path="/server/:tab" element={<ServerPage />} />

              {/* What configures the person signed in, which is a place you
                  go into from your own name at the foot of the rail. */}
              <Route path="/settings" element={<Navigate to={SETTINGS_LANDING} replace />} />
              <Route path="/settings/profile" element={<ProfilePage onSaved={refresh} />} />
              <Route
                path="/settings/password"
                element={<ChangePasswordPage username={session.username} />}
              />
              <Route path="/settings/passkeys" element={<PasskeysPage />} />
              <Route path="/settings/tokens" element={<TokensPage />} />
              <Route path="/settings/sessions" element={<SessionsPage onSignedOut={refresh} />} />

              {/* Where these used to live. Somebody's bookmark should not
                  break because the navigation was reorganised. */}
              <Route path="/settings/domains" element={<Navigate to="/domains" replace />} />
              <Route path="/settings/domains/:domainId" element={<RedirectDomain />} />
              <Route path="/setup" element={<Navigate to="/server/setup" replace />} />
              <Route path="/settings/setup" element={<Navigate to="/server/setup" replace />} />
              <Route path="/settings/server" element={<Navigate to="/server/about" replace />} />
              <Route path="/integrations" element={<Navigate to="/server/sending" replace />} />
              <Route path="/settings/integrations" element={<Navigate to="/server/sending" replace />} />
              <Route path="/integrations/:section" element={<RedirectIntegrations />} />
              <Route path="/settings/integrations/:section" element={<RedirectIntegrations />} />

              <Route path="*" element={<p className="muted">{t('common.notFound')}</p>} />
            </Routes>
          </main>
        </div>
      </div>
    </BreadcrumbProvider>
    </MailboxesProvider>
    </SessionProvider>
  )
}

// RedirectDomain carries the identifier across to the new path, so an old link
// to one domain lands on that domain rather than on the list.
function RedirectDomain() {
  const { pathname } = window.location
  return <Navigate to={pathname.replace(/^\/settings/, '')} replace />
}

// An old link to one integration carried the section it was opened at, and
// that section is a tab of the server page now.
function RedirectIntegrations() {
  const section = window.location.pathname.split('/').filter(Boolean).pop()
  return <Navigate to={`/server/${section ?? 'sending'}`} replace />
}

