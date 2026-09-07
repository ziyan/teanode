import { useState } from 'react'

import { graphql } from '../../api'
import { ErrorMessage, Loading } from '../../components/common'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { hasPermission, useSession } from '../../session'
import { CheckList, DomainSummary, GROUP_FIELDS, Group, Role, User, listDomains, listGroups, listRoles, listUsers } from './common'

const CREATE = `
  mutation ($name: String!, $description: String, $idpGroup: String, $userIds: [String], $roleIds: [String], $domainIds: [String]) {
    CreateGroup(name: $name, description: $description, idpGroup: $idpGroup, userIds: $userIds, roleIds: $roleIds, domainIds: $domainIds) ${GROUP_FIELDS}
  }`

const UPDATE = `
  mutation ($groupId: String!, $name: String, $description: String, $idpGroup: String, $userIds: [String], $roleIds: [String], $domainIds: [String]) {
    UpdateGroup(groupId: $groupId, name: $name, description: $description, idpGroup: $idpGroup, userIds: $userIds, roleIds: $roleIds, domainIds: $domainIds) ${GROUP_FIELDS}
  }`

const DELETE = `mutation ($groupId: String!) { DeleteGroup(groupId: $groupId) }`

type Draft = {
  name: string
  description: string
  idpGroup: string
  userIds: string[]
  roleIds: string[]
  domainIds: string[]
}

const EMPTY: Draft = { name: '', description: '', idpGroup: '', userIds: [], roleIds: [], domainIds: [] }

// GroupsTab is the page that matters: a group's users, its roles and its
// domains on one screen, so "why can this person do this" is answered by
// the groups they are in.
export function GroupsTab() {
  const { t } = useTranslation()
  const session = useSession()
  const manages = hasPermission(session.permissions, 'group:manage')
  const { data, error, loading, reload } = useQuery(async () => {
    const [groups, users, roles, domains] = await Promise.all([
      listGroups(),
      listUsers().catch(() => [] as User[]),
      manages ? listRoles() : Promise.resolve([] as Role[]),
      listDomains().catch(() => [] as DomainSummary[]),
    ])
    return { groups, users, roles, domains }
  }, [manages])

  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<Group | null>(null)
  const [draft, setDraft] = useState<Draft>(EMPTY)
  const [deleting, setDeleting] = useState<Group | null>(null)
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

  const groups = data?.groups ?? []
  const users = data?.users ?? []
  const roles = data?.roles ?? []
  const domains = data?.domains ?? []
  const roleName = (roleId: string) => roles.find((role) => role.id === roleId)?.name ?? roleId
  const domainName = (domainId: string) => domains.find((domain) => domain.id === domainId)?.domain ?? domainId

  return (
    <>
      <SettingsSection
        description={t('access.groups.intro')}
        action={
          manages ? (
            <button
              className="primary"
              type="button"
              onClick={() => {
                setDraft(EMPTY)
                setAdding(true)
              }}
            >
              {t('access.groups.new')}
            </button>
          ) : undefined
        }
      >
        {problem && <p className="error">{problem}</p>}
        {loading && !data && <Loading />}
        {error ? <ErrorMessage error={error} /> : null}
        {data && groups.length === 0 && <SettingsEmpty>{t('access.groups.empty')}</SettingsEmpty>}

        {groups.map((group) => (
          <SettingsRow
            key={group.id}
            title={group.name}
            subtitle={
              <>
                {group.description && <div>{group.description}</div>}
                <div>
                  {t('access.groups.members', { count: group.userIds.length })}
                  {' · '}
                  {group.roleIds.length > 0 ? group.roleIds.map(roleName).join(', ') : t('access.groups.noRoles')}
                  {' · '}
                  {group.domainIds.length > 0
                    ? group.domainIds.map(domainName).join(', ')
                    : t('access.groups.noDomains')}
                  {group.idpGroup ? ` · ${t('access.groups.idpGroup')}: ${group.idpGroup}` : ''}
                </div>
              </>
            }
            actions={
              <>
                <button
                  className="link"
                  type="button"
                  onClick={() => {
                    setDraft({
                      name: group.name,
                      description: group.description ?? '',
                      idpGroup: group.idpGroup ?? '',
                      userIds: group.userIds,
                      roleIds: group.roleIds,
                      domainIds: group.domainIds,
                    })
                    setEditing(group)
                  }}
                >
                  {t('access.groups.edit')}
                </button>
                {manages && (
                  <button className="link danger" type="button" onClick={() => setDeleting(group)}>
                    {t('common.remove')}
                  </button>
                )}
              </>
            }
          />
        ))}
      </SettingsSection>

      {(adding || editing) && (
        <FormDialog
          title={editing ? t('access.groups.editTitle', { name: editing.name }) : t('access.groups.new')}
          submitLabel={editing ? t('common.save') : t('common.create')}
          busy={busy}
          error={problem}
          canSubmit={draft.name.trim() !== ''}
          onClose={() => {
            setAdding(false)
            setEditing(null)
          }}
          onSubmit={async () => {
            // Somebody with only user:manage may change who is in a group
            // and nothing else about it; the server refuses more, so send
            // no more.
            const variables = manages
              ? {
                  name: draft.name.trim(),
                  description: draft.description,
                  idpGroup: draft.idpGroup.trim(),
                  userIds: draft.userIds,
                  roleIds: draft.roleIds,
                  domainIds: draft.domainIds,
                }
              : { userIds: draft.userIds }
            const ok = editing
              ? await run(() => graphql(UPDATE, { groupId: editing.id, ...variables }))
              : await run(() => graphql(CREATE, variables))
            if (ok) {
              setAdding(false)
              setEditing(null)
            }
          }}
        >
          {manages && (
            <>
              <label>
                {t('access.groups.name')}
                <input autoFocus value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} />
              </label>
              <label>
                {t('access.groups.description')}
                <input
                  value={draft.description}
                  onChange={(event) => setDraft({ ...draft, description: event.target.value })}
                />
              </label>
              <label>
                {t('access.groups.idpGroup')}
                <input value={draft.idpGroup} onChange={(event) => setDraft({ ...draft, idpGroup: event.target.value })} />
                <span className="muted">{t('access.groups.idpGroupHint')}</span>
              </label>
            </>
          )}
          <CheckList
            label={t('access.groups.users')}
            items={users}
            selected={draft.userIds}
            onChange={(userIds) => setDraft({ ...draft, userIds })}
            describe={(user) => (user.name ? `${user.username} (${user.name})` : user.username)}
            empty="access.users.empty"
          />
          {manages && (
            <>
              <CheckList
                label={t('access.groups.roles')}
                items={roles}
                selected={draft.roleIds}
                onChange={(roleIds) => setDraft({ ...draft, roleIds })}
                describe={(role) => role.name}
                empty="access.roles.empty"
              />
              <CheckList
                label={t('access.groups.domains')}
                hint={t('access.groups.domainsHint')}
                items={domains}
                selected={draft.domainIds}
                onChange={(domainIds) => setDraft({ ...draft, domainIds })}
                describe={(domain) => domain.domain}
                empty="access.groups.noDomainsYet"
              />
            </>
          )}
        </FormDialog>
      )}

      {deleting && (
        <ConfirmDialog
          title={t('access.groups.deleteTitle', { name: deleting.name })}
          body={t('access.groups.deleteBody')}
          confirmLabel={t('common.remove')}
          busy={busy}
          onConfirm={async () => {
            if (await run(() => graphql(DELETE, { groupId: deleting.id }))) {
              setDeleting(null)
            }
          }}
          onClose={() => setDeleting(null)}
        />
      )}
    </>
  )
}
