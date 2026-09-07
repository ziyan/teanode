import { useState } from 'react'

import { graphql } from '../../api'
import { ErrorMessage, Loading } from '../../components/common'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { hasPermission, useSession } from '../../session'
import { CheckList, PermissionDescription, ROLE_FIELDS, Role, listPermissions, listRoles, usePermissionLabel } from './common'

const CREATE = `
  mutation ($name: String!, $description: String, $permissions: [String!]) {
    CreateRole(name: $name, description: $description, permissions: $permissions) ${ROLE_FIELDS}
  }`

const UPDATE = `
  mutation ($roleId: String!, $name: String, $description: String, $permissions: [String!]) {
    UpdateRole(roleId: $roleId, name: $name, description: $description, permissions: $permissions) ${ROLE_FIELDS}
  }`

const DELETE = `mutation ($roleId: String!) { DeleteRole(roleId: $roleId) }`

type Draft = { name: string; description: string; permissions: string[] }

const KINDS: { id: PermissionDescription['kind']; label: 'access.roles.kindServer' | 'access.roles.kindDomain' | 'access.roles.kindAllDomains' }[] = [
  { id: 'server', label: 'access.roles.kindServer' },
  { id: 'domain', label: 'access.roles.kindDomain' },
  { id: 'all-domains', label: 'access.roles.kindAllDomains' },
]

// RolesTab is the vocabulary of permissions, bundled into names. The three
// seeded roles are ordinary rows: renamed, edited or deleted like any other.
export function RolesTab() {
  const { t } = useTranslation()
  const session = useSession()
  const manages = hasPermission(session.permissions, 'role:manage')
  const label = usePermissionLabel()
  const { data, error, loading, reload } = useQuery(
    async () => ({ roles: await listRoles(), permissions: await listPermissions() }),
    [],
  )

  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<Role | null>(null)
  const [draft, setDraft] = useState<Draft>({ name: '', description: '', permissions: [] })
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
      return false
    } finally {
      setBusy(false)
    }
  }

  const roles = data?.roles ?? []
  const permissions = data?.permissions ?? []

  return (
    <>
      <SettingsSection
        description={t('access.roles.intro')}
        action={
          manages ? (
            <button
              className="primary"
              type="button"
              onClick={() => {
                setDraft({ name: '', description: '', permissions: [] })
                setAdding(true)
              }}
            >
              {t('access.roles.new')}
            </button>
          ) : undefined
        }
      >
        {problem && <p className="error">{problem}</p>}
        {loading && !data && <Loading />}
        {error ? <ErrorMessage error={error} /> : null}
        {data && roles.length === 0 && <SettingsEmpty>{t('access.roles.empty')}</SettingsEmpty>}

        {roles.map((role) => (
          <SettingsRow
            key={role.id}
            title={role.name}
            subtitle={
              <>
                {role.description && <div>{role.description}</div>}
                <div>
                  {role.permissions.length === permissions.length && permissions.length > 0
                    ? t('access.roles.everyPermission')
                    : role.permissions.length > 0
                      ? role.permissions.map(label).join(', ')
                      : t('access.roles.noPermissions')}
                </div>
              </>
            }
            actions={
              manages ? (
                <>
                  <button
                    className="link"
                    type="button"
                    onClick={() => {
                      setDraft({ name: role.name, description: role.description ?? '', permissions: role.permissions })
                      setEditing(role)
                    }}
                  >
                    {t('access.roles.edit')}
                  </button>
                  <button className="link danger" type="button" onClick={() => setDeleting(role)}>
                    {t('common.remove')}
                  </button>
                </>
              ) : undefined
            }
          />
        ))}
      </SettingsSection>

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
            const variables = { name: draft.name.trim(), description: draft.description, permissions: draft.permissions }
            const ok = editing
              ? await run(() => graphql(UPDATE, { roleId: editing.id, ...variables }))
              : await run(() => graphql(CREATE, variables))
            if (ok) {
              setAdding(false)
              setEditing(null)
            }
          }}
        >
          <label>
            {t('access.roles.name')}
            <input autoFocus value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} />
          </label>
          <label>
            {t('access.roles.description')}
            <input
              value={draft.description}
              onChange={(event) => setDraft({ ...draft, description: event.target.value })}
            />
          </label>
          {KINDS.map((kind) => (
            <CheckList
              key={kind.id}
              label={t(kind.label)}
              items={permissions.filter((permission) => permission.kind === kind.id).map((permission) => ({ ...permission, id: permission.key }))}
              selected={draft.permissions}
              onChange={(selected) =>
                setDraft({
                  ...draft,
                  permissions: permissions
                    .map((permission) => permission.key)
                    .filter((key) =>
                      permissions.find((permission) => permission.key === key)?.kind === kind.id
                        ? selected.includes(key)
                        : draft.permissions.includes(key),
                    ),
                })
              }
              describe={(permission) => (
                <>
                  {label(permission.key)} <span className="muted mono">{permission.key}</span>
                </>
              )}
            />
          ))}
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
              setDeleting(null)
            }
          }}
          onClose={() => setDeleting(null)}
        />
      )}
    </>
  )
}
