import { useEffect, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'

import { graphql } from '../../api'
import { ErrorMessage, Loading } from '../../components/common'
import { PencilIcon, PlusIcon, TrashIcon } from '../../components/icons'
import { Select } from '../../components/select'
import { Tooltip } from '../../components/tooltip'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { SettingsEmpty, SettingsSection } from '../../components/settingsList'
import { useQuery } from '../../components/useQuery'
import { useToast } from '../../components/toast'
import { useTranslation } from '../../i18n/i18n'
import { hasPermission, useSession } from '../../session'
import {
  CheckList,
  PermissionDescription,
  ROLE_FIELDS,
  Role,
  listPermissions,
  listRoles,
  usePermissionLabel,
} from './common'

// permissions is required and a new role holds none: they are granted on the
// page this opens, once the role exists. Leaving the argument out made every
// creation fail before it reached the server.
const CREATE = `
  mutation ($name: String!, $description: String, $permissions: [String!]!) {
    CreateRole(name: $name, description: $description, permissions: $permissions) ${ROLE_FIELDS}
  }`

const UPDATE = `
  mutation ($roleId: String!, $name: String, $description: String, $permissions: [String!]) {
    UpdateRole(roleId: $roleId, name: $name, description: $description, permissions: $permissions) ${ROLE_FIELDS}
  }`

const DELETE = `mutation ($roleId: String!) { DeleteRole(roleId: $roleId) }`

type Draft = { name: string; description: string }

const EMPTY: Draft = { name: '', description: '' }

const KINDS: {
  id: PermissionDescription['kind']
  label: 'access.roles.kindServer' | 'access.roles.kindDomain' | 'access.roles.kindAllDomains'
}[] = [
  { id: 'server', label: 'access.roles.kindServer' },
  { id: 'domain', label: 'access.roles.kindDomain' },
  { id: 'all-domains', label: 'access.roles.kindAllDomains' },
]

// RolesTab is the vocabulary of permissions, bundled into names. The three
// seeded roles are ordinary rows: renamed, edited or deleted like any other.
//
// The roles are the list, and the one being read is beside it: what it is for,
// and the sixty-odd permissions it may hold, ticked on the page. They were in
// a dialog, which for a list that long meant scrolling a box that covered the
// page to find the one permission you came to change. Only the name and the
// description are still a dialog, because they are a form.
export function RolesTab() {
  const { t } = useTranslation()
  const toast = useToast()
  const session = useSession()
  const manages = hasPermission(session.permissions, 'role:manage')
  const label = usePermissionLabel()
  const { data, error, loading, reload } = useQuery(
    async () => ({ roles: await listRoles(), permissions: await listPermissions() }),
    [],
  )

  // Which role is being read, in the query string, so it can be linked to and
  // survives a reload.
  // Which row is open is in the path, beside the tab. It was a query
  // parameter written with replace, so choosing one left no history entry:
  // the back button left the page rather than going to the row read before.
  const navigate = useNavigate()
  const asked = useParams().selected ?? ''

  // Links made before the row moved into the path still work, and become the
  // new shape as they arrive.
  const [parameters] = useSearchParams()
  const legacy = parameters.get('role') ?? ''
  useEffect(() => {
    if (legacy) {
      navigate(`/access/roles/${legacy}`, { replace: true })
    }
  }, [legacy, navigate])

  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<Role | null>(null)
  const [draft, setDraft] = useState<Draft>(EMPTY)
  const [deleting, setDeleting] = useState<Role | null>(null)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  async function run(work: () => Promise<unknown>) {
    setBusy(true)
    setProblem(null)
    try {
      await work()
      await reload()
      return true
    } catch (caught) {
      setProblem(caught instanceof Error ? caught.message : t('domain.failed'))
      toast.failure(caught, t('domain.failed'))
      return false
    } finally {
      setBusy(false)
    }
  }

  const roles = data?.roles ?? []
  const permissions = data?.permissions ?? []

  const chosen = roles.find((role) => role.id === asked) ?? roles[0] ?? null
  const choose = (roleId: string) => navigate(roleId ? `/access/roles/${roleId}` : '/access/roles')

  function startAdding() {
    setProblem(null)
    setDraft(EMPTY)
    setAdding(true)
  }

  function startEditing(role: Role) {
    setProblem(null)
    setDraft({ name: role.name, description: role.description ?? '' })
    setEditing(role)
  }

  // Ticking writes. A role is a set of permissions and nothing else, so the
  // set is the thing being edited; holding it behind a Save button made
  // changing one permission a three-step operation.
  const setPermissions = (role: Role, kind: PermissionDescription['kind'], selected: string[]) =>
    run(() =>
      graphql(UPDATE, {
        roleId: role.id,
        permissions: permissions
          .map((permission) => permission.key)
          .filter((key) =>
            permissions.find((permission) => permission.key === key)?.kind === kind
              ? selected.includes(key)
              : role.permissions.includes(key),
          ),
      }),
    )

  const newRoleButton = manages ? (
    <Tooltip label={t('access.roles.new')}>
      <button className="icon-button" type="button" aria-label={t('access.roles.new')} onClick={startAdding}>
        <PlusIcon size={16} />
      </button>
    </Tooltip>
  ) : undefined

  return (
    <>
      {loading && !data && <Loading />}
      {error ? <ErrorMessage error={error} /> : null}

      {data && roles.length === 0 ? (
        <SettingsSection card description={t('access.roles.intro')} action={newRoleButton}>
          <SettingsEmpty>{t('access.roles.empty')}</SettingsEmpty>
        </SettingsSection>
      ) : (
        <div className="access-roles">
          {/* Where the list is a column too narrow to be one: the same choice
              as a control rather than as a panel. */}
          <div className="access-picker">
            <Select
              label={t('server.tabRoles')}
              value={chosen?.id ?? ''}
              options={roles.map((role) => ({ value: role.id, label: role.name }))}
              onChange={choose}
            />
            {manages && chosen && (
              <>
                <Tooltip label={t('access.roles.edit')}>
                  <button
                    className="icon-button"
                    type="button"
                    aria-label={`${chosen.name}: ${t('access.roles.edit')}`}
                    onClick={() => startEditing(chosen)}
                  >
                    <PencilIcon size={16} />
                  </button>
                </Tooltip>
                <Tooltip label={t('common.remove')}>
                  <button
                    className="icon-button danger"
                    type="button"
                    aria-label={`${chosen.name}: ${t('common.remove')}`}
                    onClick={() => setDeleting(chosen)}
                  >
                    <TrashIcon size={16} />
                  </button>
                </Tooltip>
              </>
            )}
            {manages && (
              <Tooltip label={t('access.roles.new')}>
                <button className="icon-button" type="button" aria-label={t('access.roles.new')} onClick={startAdding}>
                  <PlusIcon size={16} />
                </button>
              </Tooltip>
            )}
          </div>

          <SettingsSection card title={t('server.tabRoles')} action={newRoleButton}>
            {roles.map((role) => (
              <div key={role.id} className={role.id === chosen?.id ? 'access-pick-row chosen' : 'access-pick-row'}>
                <button
                  type="button"
                  className="access-pick-name"
                  aria-current={role.id === chosen?.id}
                  onClick={() => choose(role.id)}
                >
                  {role.name}
                </button>
                {manages && role.id === chosen?.id && (
                  <div className="row-actions">
                    <Tooltip label={t('access.roles.edit')}>
                      <button
                        className="icon-action"
                        type="button"
                        aria-label={`${role.name}: ${t('access.roles.edit')}`}
                        onClick={() => startEditing(role)}
                      >
                        <PencilIcon size={16} />
                      </button>
                    </Tooltip>
                    <Tooltip label={t('common.remove')}>
                      <button
                        className="icon-action danger"
                        type="button"
                        aria-label={`${role.name}: ${t('common.remove')}`}
                        onClick={() => setDeleting(role)}
                      >
                        <TrashIcon size={16} />
                      </button>
                    </Tooltip>
                  </div>
                )}
              </div>
            ))}
          </SettingsSection>

          {chosen && (
            <div className="access-attached">
              <SettingsSection card title={t('access.roles.description')}>
                <p className={chosen.description ? undefined : 'muted'}>
                  {chosen.description || t('access.roles.noDescription')}
                </p>
              </SettingsSection>
              {KINDS.map((kind) => (
                <CheckList
                  key={kind.id}
                  label={t(kind.label)}
                  items={permissions
                    .filter((permission) => permission.kind === kind.id)
                    .map((permission) => ({ ...permission, id: permission.key }))}
                  selected={chosen.permissions}
                  onChange={(selected) => manages && void setPermissions(chosen, kind.id, selected)}
                  describe={(permission) => (
                    <>
                      {label(permission.key)}
                      <code className="mono">{permission.key}</code>
                    </>
                  )}
                />
              ))}
            </div>
          )}
        </div>
      )}

      {(adding || editing) && (
        <FormDialog
          title={editing ? t('access.roles.editTitle', { name: editing.name }) : t('access.roles.new')}
          submitLabel={editing ? t('common.save') : t('common.create')}
          busy={busy}
          error={problem}
          canSubmit={draft.name.trim() !== ''}
          onClose={() => {
            setAdding(false)
            setEditing(null)
          }}
          onSubmit={async () => {
            const variables = { name: draft.name.trim(), description: draft.description }
            const ok = editing
              ? await run(() => graphql(UPDATE, { roleId: editing.id, ...variables }))
              : await run(async () => {
                  const created = await graphql<{ CreateRole: Role }>(CREATE, { ...variables, permissions: [] })
                  choose(created.CreateRole.id)
                })
            if (ok) {
              setAdding(false)
              setEditing(null)
            }
          }}
        >
          <label>
            {t('access.roles.name')}
            <input
              autoFocus
              value={draft.name}
              onChange={(event) => setDraft({ ...draft, name: event.target.value })}
            />
          </label>
          <label>
            {t('access.roles.description')}
            <input
              value={draft.description}
              onChange={(event) => setDraft({ ...draft, description: event.target.value })}
            />
          </label>
        </FormDialog>
      )}

      {deleting && (
        <ConfirmDialog
          title={t('access.roles.deleteTitle', { name: deleting.name })}
          body={t('access.roles.deleteBody')}
          confirmLabel={t('common.remove')}
          busy={busy}
          onConfirm={async () => {
            if (await run(() => graphql(DELETE, { roleId: deleting.id }))) {
              if (deleting.id === asked) {
                choose('')
              }
              setDeleting(null)
            }
          }}
          onClose={() => setDeleting(null)}
        />
      )}
    </>
  )
}
