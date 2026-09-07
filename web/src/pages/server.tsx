import { Navigate, useNavigate, useParams } from 'react-router-dom'

import { Tabs } from '../components/tabs'
import { Key } from '../i18n/i18n'
import { hasPermission, useSession } from '../session'
import { AuditTab } from './access/audit'
import { GroupsTab } from './access/groups'
import { RolesTab } from './access/roles'
import { UsersTab } from './access/users'
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

// Who may do what, after what the server is. Each tab is there only for a
// caller who may open it; the server refuses the rest anyway.
const ACCESS_TABS: (Tab & { permissions: string[] })[] = [
  { id: 'users', label: 'server.tabUsers', permissions: ['user:manage'] },
  { id: 'groups', label: 'server.tabGroups', permissions: ['group:manage', 'user:manage'] },
  { id: 'roles', label: 'server.tabRoles', permissions: ['role:manage', 'group:manage'] },
  { id: 'audit', label: 'server.tabAudit', permissions: ['audit:read'] },
]

export function ServerPage() {
  // In the path rather than in state, so a tab can be linked to, survives a
  // reload and can be reached with the back button. A tab that only exists in
  // memory is a place you cannot send somebody.
  const { tab } = useParams()
  const navigate = useNavigate()
  const session = useSession()

  const TABS: Tab[] = [
    ...(hasPermission(session.permissions, 'server:manage') ? SERVER_TABS : []),
    ...ACCESS_TABS.filter((candidate) => candidate.permissions.some((key) => hasPermission(session.permissions, key))),
  ]
  if (TABS.length === 0) {
    return <Navigate to="/" replace />
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
      {tab === 'users' && <UsersTab />}
      {tab === 'groups' && <GroupsTab />}
      {tab === 'roles' && <RolesTab />}
      {tab === 'audit' && <AuditTab />}
      {INTEGRATION_SECTIONS.some((candidate) => candidate.id === tab) && (
        <IntegrationsSection section={tab as Section} />
      )}
    </>
  )
}
