import { Navigate, useNavigate, useParams } from 'react-router-dom'

import { Tabs } from '../components/tabs'
import { Key } from '../i18n/i18n'
import { hasPermission, useSession } from '../session'
import { INTEGRATION_SECTIONS, IntegrationsSection, Section } from './settings/integrations'
import { ServerAboutPage } from './settings/server'
import { SetupPage } from './setup'

// Everything about this server, in one place with tabs across the top.
//
// It was three rows in the rail — Setup, Integrations, Server — and they are
// one subject: what this server is, what it talks to, and which version it is
// running. Three rows made somebody choose between them before knowing which
// one held the thing they wanted, and two of the three were named after the
// shape of the page rather than the question it answers.
//
// The order is the order somebody meets them. Setup first, because it is what
// a new server needs and the page that says what is still missing. Then the
// services, in the order mail moves through them. About last: it is the page
// you go to when something is already running.
type Tab = { id: string; label: Key }

const SERVER_TABS: Tab[] = [
  { id: 'setup', label: 'server.tabSetup' },
  ...INTEGRATION_SECTIONS,
  { id: 'about', label: 'server.tabAbout' },
]

// Who may do what moved out of here to /access, its own row in the rail:
// the accounts and the groups were four tabs deep in a page about TLS and
// spam, and they are what an operator reaches for most. Links to the old
// paths still work, below.
const ACCESS_TABS = ['users', 'groups', 'roles', 'audit']

export function ServerPage() {
  // In the path rather than in state, so a tab can be linked to, survives a
  // reload and can be reached with the back button. A tab that only exists in
  // memory is a place you cannot send somebody.
  const { tab } = useParams()
  const navigate = useNavigate()
  const session = useSession()

  // A link to where the access tabs used to be goes where they are.
  if (tab && ACCESS_TABS.includes(tab)) {
    return <Navigate to={`/access/${tab}`} replace />
  }

  const TABS: Tab[] = hasPermission(session.permissions, 'server:manage') ? SERVER_TABS : []
  if (TABS.length === 0) {
    return <Navigate to="/access" replace />
  }

  // The certificates tab was called "dns" while the only DNS on this page was
  // the challenge solver. There is a resolver tab now, and two tabs whose
  // names both mean DNS is a page nobody can navigate.
  if (tab === 'dns') {
    return <Navigate to="/server/certificates" replace />
  }

  // A tab nobody has, or none named at all, is the first one. A path somebody
  // typed is not a tab.
  if (!TABS.some((candidate) => candidate.id === tab)) {
    return <Navigate to={`/server/${TABS[0].id}`} replace />
  }

  return (
    <>
      <Tabs items={TABS} active={tab} onSelect={(id) => navigate(`/server/${id}`)} />

      {tab === 'setup' && <SetupPage />}
      {tab === 'about' && <ServerAboutPage />}
      {INTEGRATION_SECTIONS.some((candidate) => candidate.id === tab) && (
        <IntegrationsSection section={tab as Section} />
      )}
    </>
  )
}
