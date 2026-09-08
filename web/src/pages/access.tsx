import { Navigate, useNavigate, useParams } from 'react-router-dom'

import { Tabs } from '../components/tabs'
import { Key } from '../i18n/i18n'
import { hasPermission, useSession } from '../session'
import { AuditTab } from './access/audit'
import { GroupsTab } from './access/groups'
import { RolesTab } from './access/roles'
import { UsersTab } from './access/users'

// Who may do what on this server: the accounts, the groups they are in, the
// roles a group holds, and the log of every administrative change.
//
// These were tabs of /server, behind Setup and the integrations, which put
// the two things an operator reaches for most — an account and a group —
// four tabs into a page about TLS and spam. They are their own row in the
// rail now, beside Domains and Server, and /server is what the server is
// rather than who may use it.
type Tab = { id: string; label: Key; permissions: string[] }

const TABS: Tab[] = [
  { id: 'users', label: 'access.users.tab', permissions: ['user:manage', 'group:manage'] },
  { id: 'groups', label: 'access.groups.tab', permissions: ['user:manage', 'group:manage'] },
  { id: 'roles', label: 'server.tabRoles', permissions: ['role:manage', 'group:manage'] },
  { id: 'audit', label: 'server.tabAudit', permissions: ['audit:read'] },
]

// Where the one tab that became two used to be, so a link to it still
// arrives somewhere.
const MOVED: Record<string, string> = { people: 'users' }

export function AccessPage() {
  // In the path rather than in state, so a tab can be linked to, survives a
  // reload and can be reached with the back button.
  const { tab } = useParams()
  const navigate = useNavigate()
  const session = useSession()

  const permitted = TABS.filter((candidate) => candidate.permissions.some((key) => hasPermission(session.permissions, key)))
  if (permitted.length === 0) {
    return <Navigate to="/" replace />
  }
  if (tab && MOVED[tab]) {
    return <Navigate to={`/access/${MOVED[tab]}`} replace />
  }
  if (!permitted.some((candidate) => candidate.id === tab)) {
    return <Navigate to={`/access/${permitted[0].id}`} replace />
  }

  return (
    <>
      <Tabs items={permitted} active={tab} onSelect={(id) => navigate(`/access/${id}`)} />

      {tab === 'users' && <UsersTab />}
      {tab === 'groups' && <GroupsTab />}
      {tab === 'roles' && <RolesTab />}
      {tab === 'audit' && <AuditTab />}
    </>
  )
}
