import { Navigate, useNavigate, useParams } from 'react-router-dom'

import { TabItem, Tabs } from '../components/tabs'
import { useFreshness } from '../components/freshness'
import { useTranslation } from '../i18n/i18n'
import { INTEGRATION_TABS, SERVER_TABS, shownTabs } from '../pageTabs'
import { hasPermission, useSession } from '../session'
import { IntegrationsSection, Section } from './settings/integrations'
import { ServerAboutPage } from './settings/server'
import { SetupPage } from './serviceSetup'

// Everything about this server, in one place with tabs across the top.
//
// It was three rows in the rail — Setup, Integrations, Server — and they are
// one subject: what this server is, what it talks to, and which version it is
// running. Three rows made somebody choose between them before knowing which
// one held the thing they wanted, and two of the three were named after the
// shape of the page rather than the question it answers.
//
// About first: it is what this server is — the version it runs, and the
// upgrade waiting for it, which is what the dot in the rail is pointing at.
// Then Setup, which says what is still missing, and then the services in the
// order mail moves through them.
//
// The tabs are in pageTabs.ts, so the search box offers the same ones.

export function ServerPage() {
  // In the path rather than in state, so a tab can be linked to, survives a
  // reload and can be reached with the back button. A tab that only exists in
  // memory is a place you cannot send somebody.
  const { tab } = useParams()
  const navigate = useNavigate()
  const session = useSession()
  const { t } = useTranslation()
  const { upgradeAvailable } = useFreshness()

  // The rail's dot says "there is something on the Server page"; this one
  // says which tab it is on.
  const TABS: TabItem[] = hasPermission(session.permissions, 'server:manage')
    ? shownTabs(SERVER_TABS, {
        hasPermission: (key) => hasPermission(session.permissions, key),
        services: session.services,
      }).map((candidate) =>
        candidate.id === 'about' && upgradeAvailable
          ? { ...candidate, marked: true, markedLabel: t('nav.upgradeAvailable') }
          : candidate,
      )
    : []
  if (TABS.length === 0) {
    return <Navigate to="/manage/access" replace />
  }

  // A tab nobody has, or none named at all, is the first one. A path somebody
  // typed is not a tab.
  if (!TABS.some((candidate) => candidate.id === tab)) {
    return <Navigate to={`/manage/server/${TABS[0].id}`} replace />
  }

  return (
    <>
      <Tabs items={TABS} active={tab} onSelect={(id) => navigate(`/manage/server/${id}`)} />

      {tab === 'setup' && <SetupPage />}
      {tab === 'about' && <ServerAboutPage />}
      {INTEGRATION_TABS.some((candidate) => candidate.id === tab) &&
        hasPermission(session.permissions, 'server:manage') && <IntegrationsSection section={tab as Section} />}
    </>
  )
}
