import { Navigate, useNavigate, useParams } from 'react-router-dom'

import { Tabs } from '../components/tabs'
import { ACCESS_TABS, shownTabs } from '../pageTabs'
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
//
// The tabs are in pageTabs.ts, with the permission each wants, so the search
// box offers the same ones.

export function AccessPage() {
  // In the path rather than in state, so a tab can be linked to, survives a
  // reload and can be reached with the back button.
  const { tab } = useParams()
  const navigate = useNavigate()
  const session = useSession()

  const permitted = shownTabs(ACCESS_TABS, { hasPermission: (key) => hasPermission(session.permissions, key) })
  if (permitted.length === 0) {
    return <Navigate to="/" replace />
  }
  if (!permitted.some((candidate) => candidate.id === tab)) {
    return <Navigate to={`/manage/access/${permitted[0].id}`} replace />
  }

  return (
    <>
      <Tabs items={permitted} active={tab} onSelect={(id) => navigate(`/manage/access/${id}`)} />

      {tab === 'users' && <UsersTab />}
      {tab === 'groups' && <GroupsTab />}
      {tab === 'roles' && <RolesTab />}
      {tab === 'audit' && <AuditTab />}
    </>
  )
}
