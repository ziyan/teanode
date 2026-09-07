import { useState } from 'react'

import { graphql } from '../../api'
import { ErrorMessage, Loading, Tag, formatTime } from '../../components/common'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { hasPermission, useSession } from '../../session'
import {
  CheckList,
  DomainSummary,
  GROUP_FIELDS,
  Group,
  Role,
  USER_FIELDS,
  User,
  listDomains,
  listGroups,
  listRoles,
  listUsers,
} from './common'

const CREATE_USER = `
  mutation ($username: String!, $password: String, $name: String, $email: String, $groupIds: [String!]) {
    CreateUser(username: $username, password: $password, name: $name, email: $email, groupIds: $groupIds) ${USER_FIELDS}
  }`

const UPDATE_USER = `
  mutation ($userId: String!, $username: String, $name: String, $email: String, $disabled: Boolean, $groupIds: [String!]) {
    UpdateUser(userId: $userId, username: $username, name: $name, email: $email, disabled: $disabled, groupIds: $groupIds) ${USER_FIELDS}
  }`

const SET_PASSWORD = `mutation ($userId: String!, $password: String!) { SetUserPassword(userId: $userId, password: $password) { id } }`
const DELETE_USER = `mutation ($userId: String!) { DeleteUser(userId: $userId) }`

const CREATE_GROUP = `
  mutation ($name: String!, $description: String, $idpGroup: String, $userIds: [String!], $roleIds: [String!], $domainIds: [String!]) {
    CreateGroup(name: $name, description: $description, idpGroup: $idpGroup, userIds: $userIds, roleIds: $roleIds, domainIds: $domainIds) ${GROUP_FIELDS}
  }`

const UPDATE_GROUP = `
  mutation ($groupId: String!, $name: String, $description: String, $idpGroup: String, $userIds: [String!], $roleIds: [String!], $domainIds: [String!]) {
    UpdateGroup(groupId: $groupId, name: $name, description: $description, idpGroup: $idpGroup, userIds: $userIds, roleIds: $roleIds, domainIds: $domainIds) ${GROUP_FIELDS}
  }`

const DELETE_GROUP = `mutation ($groupId: String!) { DeleteGroup(groupId: $groupId) }`

type PersonDraft = { username: string; password: string; name: string; email: string; groupIds: string[] }
type GroupDraft = { name: string; description: string; idpGroup: string; userIds: string[]; roleIds: string[]; domainIds: string[] }

const EMPTY_GROUP: GroupDraft = { name: '', description: '', idpGroup: '', userIds: [], roleIds: [], domainIds: [] }

// PeopleTab is the accounts and the groups on one page, because they are one
// subject: a person may do something because a group they are in holds a
// role that says so. They were two tabs, and answering "why can this person
// do this" meant reading one, remembering it, and reading the other.
//
// The groups are the column on the right, and choosing one narrows the
// people on the left to its members, so the same page answers "who is in
// this group" and "what is this person in". Membership is edited from either
// side: a person's dialog lists the groups, a group's dialog lists the
// people, and both write the same thing.
export function PeopleTab() {
  const { t } = useTranslation()
  const session = useSession()
  const managesGroups = hasPermission(session.permissions, 'group:manage')
  const managesUsers = hasPermission(session.permissions, 'user:manage')

  const { data, error, loading, reload } = useQuery(async () => {
    const [users, groups, roles, domains] = await Promise.all([
      listUsers().catch(() => [] as User[]),
      listGroups(),
      managesGroups ? listRoles() : Promise.resolve([] as Role[]),
      listDomains().catch(() => [] as DomainSummary[]),
    ])
    return { users, groups, roles, domains }
  }, [managesGroups])

  // Which group the people list is narrowed to; empty is everyone.
  const [chosenGroupId, setChosenGroupId] = useState('')

  const [addingPerson, setAddingPerson] = useState(false)
  const [editingPerson, setEditingPerson] = useState<User | null>(null)
  const [personDraft, setPersonDraft] = useState<PersonDraft>({ username: '', password: '', name: '', email: '', groupIds: [] })
  const [passwordFor, setPasswordFor] = useState<User | null>(null)
  const [newPassword, setNewPassword] = useState('')
  const [deletingPerson, setDeletingPerson] = useState<User | null>(null)

  const [addingGroup, setAddingGroup] = useState(false)
  const [editingGroup, setEditingGroup] = useState<Group | null>(null)
  const [groupDraft, setGroupDraft] = useState<GroupDraft>(EMPTY_GROUP)
  const [deletingGroup, setDeletingGroup] = useState<Group | null>(null)

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

  const users = data?.users ?? []
  const groups = data?.groups ?? []
  const roles = data?.roles ?? []
  const domains = data?.domains ?? []
  const groupName = (groupId: string) => groups.find((group) => group.id === groupId)?.name ?? groupId
  const roleName = (roleId: string) => roles.find((role) => role.id === roleId)?.name ?? roleId
  const domainName = (domainId: string) => domains.find((domain) => domain.id === domainId)?.domain ?? domainId

  const chosen = groups.find((group) => group.id === chosenGroupId) ?? null
  const shown = chosen ? users.filter((user) => user.groupIds.includes(chosen.id)) : users

  function startAddingPerson() {
    // A person made while a group is chosen joins it; otherwise they join
    // Members, so they can read the mailbox they are about to be given.
    const fallback = groups.find((group) => group.name.toLowerCase() === 'members')
    const joins = chosen ?? fallback
    setPersonDraft({ username: '', password: '', name: '', email: '', groupIds: joins ? [joins.id] : [] })
    setAddingPerson(true)
  }

  function startEditingPerson(user: User) {
    setPersonDraft({ username: user.username, password: '', name: user.name ?? '', email: user.email ?? '', groupIds: user.groupIds })
    setEditingPerson(user)
  }

  function startEditingGroup(group: Group) {
    setGroupDraft({
      name: group.name,
      description: group.description ?? '',
      idpGroup: group.idpGroup ?? '',
      userIds: group.userIds,
      roleIds: group.roleIds,
      domainIds: group.domainIds,
    })
    setEditingGroup(group)
  }

  return (
    <>
      {problem && <p className="error">{problem}</p>}
      {loading && !data && <Loading />}
      {error ? <ErrorMessage error={error} /> : null}

      <div className="access-columns">
        <SettingsSection
          title={chosen ? t('access.people.inGroup', { name: chosen.name }) : t('access.people.everyone')}
          description={t('access.users.intro')}
          action={
            managesUsers ? (
              <button className="primary" type="button" onClick={startAddingPerson}>
                {t('access.users.new')}
              </button>
            ) : undefined
          }
        >
          {chosen && (
            <p className="muted access-filter">
              {t('access.people.narrowed', { name: chosen.name, count: shown.length })}{' '}
              <button className="link" type="button" onClick={() => setChosenGroupId('')}>
                {t('access.people.showEveryone')}
              </button>
            </p>
          )}
          {data && shown.length === 0 && (
            <SettingsEmpty>{chosen ? t('access.people.noMembers') : t('access.users.empty')}</SettingsEmpty>
          )}

          {shown.map((user) => (
            <SettingsRow
              key={user.id}
              title={
                <>
                  {user.username}
                  {user.id === session.userId ? <span className="muted"> · {t('access.users.you')}</span> : null}
                </>
              }
              badge={
                user.disabledAt ? (
                  <Tag value={t('access.users.disabled')} tone="bad" />
                ) : !user.hasPassword ? (
                  <Tag value={t('access.users.noPassword')} tone="warn" />
                ) : undefined
              }
              subtitle={
                <>
                  <div>
                    {user.name || t('common.none')}
                    {user.email ? ` · ${user.email}` : ''}
                  </div>
                  <div className="access-chips">
                    {user.groupIds.length > 0 ? (
                      user.groupIds.map((groupId) => (
                        // A person's group is a way into that group: it
                        // narrows the list to everybody else in it.
                        <button
                          key={groupId}
                          type="button"
                          className={groupId === chosenGroupId ? 'access-chip chosen' : 'access-chip'}
                          onClick={() => setChosenGroupId(groupId === chosenGroupId ? '' : groupId)}
                        >
                          {groupName(groupId)}
                        </button>
                      ))
                    ) : (
                      <span className="muted">{t('access.users.noGroups')}</span>
                    )}
                  </div>
                  <div className="muted">{t('access.users.created', { time: formatTime(user.createdAt) })}</div>
                </>
              }
              actions={
                managesUsers ? (
                  <>
                    <button className="link" type="button" onClick={() => startEditingPerson(user)}>
                      {t('access.users.edit')}
                    </button>
                    <button
                      className="link"
                      type="button"
                      onClick={() => {
                        setNewPassword('')
                        setPasswordFor(user)
                      }}
                    >
                      {t('access.users.setPassword')}
                    </button>
                    {user.id !== session.userId && (
                      <button className="link danger" type="button" onClick={() => setDeletingPerson(user)}>
                        {t('common.remove')}
                      </button>
                    )}
                  </>
                ) : undefined
              }
            />
          ))}
        </SettingsSection>

        <SettingsSection
          title={t('access.people.groups')}
          description={t('access.groups.intro')}
          action={
            managesGroups ? (
              <button
                className="primary"
                type="button"
                onClick={() => {
                  setGroupDraft(EMPTY_GROUP)
                  setAddingGroup(true)
                }}
              >
                {t('access.groups.new')}
              </button>
            ) : undefined
          }
        >
          {data && groups.length === 0 && <SettingsEmpty>{t('access.groups.empty')}</SettingsEmpty>}

          {groups.map((group) => (
            <SettingsRow
              key={group.id}
              title={
                <button
                  type="button"
                  className={group.id === chosenGroupId ? 'access-group-name chosen' : 'access-group-name'}
                  onClick={() => setChosenGroupId(group.id === chosenGroupId ? '' : group.id)}
                >
                  {group.name}
                </button>
              }
              subtitle={
                <>
                  {group.description && <div>{group.description}</div>}
                  <div>
                    {t('access.groups.members', { count: group.userIds.length })}
                    {' · '}
                    {group.roleIds.length > 0 ? group.roleIds.map(roleName).join(', ') : t('access.groups.noRoles')}
                    {' · '}
                    {group.domainIds.length > 0 ? group.domainIds.map(domainName).join(', ') : t('access.groups.noDomains')}
                    {group.idpGroup ? ` · ${t('access.groups.idpGroup')}: ${group.idpGroup}` : ''}
                  </div>
                </>
              }
              actions={
                <>
                  <button className="link" type="button" onClick={() => startEditingGroup(group)}>
                    {t('access.groups.edit')}
                  </button>
                  {managesGroups && (
                    <button className="link danger" type="button" onClick={() => setDeletingGroup(group)}>
                      {t('common.remove')}
                    </button>
                  )}
                </>
              }
            />
          ))}
        </SettingsSection>
      </div>

      {(addingPerson || editingPerson) && (
        <FormDialog
          title={editingPerson ? t('access.users.editTitle', { username: editingPerson.username }) : t('access.users.new')}
          submitLabel={editingPerson ? t('common.save') : t('common.create')}
          busy={busy}
          error={problem}
          canSubmit={personDraft.username.trim() !== ''}
          onClose={() => {
            setAddingPerson(false)
            setEditingPerson(null)
          }}
          onSubmit={async () => {
            const ok = editingPerson
              ? await run(() =>
                  graphql(UPDATE_USER, {
                    userId: editingPerson.id,
                    username: personDraft.username.trim(),
                    name: personDraft.name,
                    email: personDraft.email,
                    groupIds: personDraft.groupIds,
                  }),
                )
              : await run(() =>
                  graphql(CREATE_USER, {
                    username: personDraft.username.trim(),
                    password: personDraft.password || null,
                    name: personDraft.name,
                    email: personDraft.email,
                    groupIds: personDraft.groupIds,
                  }),
                )
            if (ok) {
              setAddingPerson(false)
              setEditingPerson(null)
            }
          }}
        >
          <label>
            {t('access.users.username')}
            <input
              autoFocus
              value={personDraft.username}
              onChange={(event) => setPersonDraft({ ...personDraft, username: event.target.value })}
            />
          </label>
          {!editingPerson && (
            <label>
              {t('access.users.password')}
              <input
                type="password"
                value={personDraft.password}
                onChange={(event) => setPersonDraft({ ...personDraft, password: event.target.value })}
              />
              <span className="muted">{t('access.users.passwordHint')}</span>
            </label>
          )}
          <label>
            {t('access.users.name')}
            <input value={personDraft.name} onChange={(event) => setPersonDraft({ ...personDraft, name: event.target.value })} />
          </label>
          <label>
            {t('access.users.email')}
            <input
              type="email"
              value={personDraft.email}
              onChange={(event) => setPersonDraft({ ...personDraft, email: event.target.value })}
            />
          </label>
          <CheckList
            label={t('access.users.groups')}
            items={groups}
            selected={personDraft.groupIds}
            onChange={(groupIds) => setPersonDraft({ ...personDraft, groupIds })}
            describe={(group) => group.name}
            empty="access.groups.empty"
          />
          {editingPerson && editingPerson.id !== session.userId && (
            <label className="check-list-item">
              <input
                type="checkbox"
                checked={Boolean(editingPerson.disabledAt)}
                onChange={(event) =>
                  void run(() => graphql(UPDATE_USER, { userId: editingPerson.id, disabled: event.target.checked })).then(() =>
                    setEditingPerson({
                      ...editingPerson,
                      disabledAt: event.target.checked ? new Date().toISOString() : null,
                    }),
                  )
                }
              />
              <span>{t('access.users.disabledLabel')}</span>
            </label>
          )}
        </FormDialog>
      )}

      {passwordFor && (
        <FormDialog
          title={t('access.users.setPasswordTitle', { username: passwordFor.username })}
          submitLabel={t('common.save')}
          busy={busy}
          error={problem}
          canSubmit={newPassword.length > 0}
          onClose={() => setPasswordFor(null)}
          onSubmit={async () => {
            if (await run(() => graphql(SET_PASSWORD, { userId: passwordFor.id, password: newPassword }))) {
              setPasswordFor(null)
            }
          }}
        >
          <label>
            {t('access.users.newPassword')}
            <input autoFocus type="password" value={newPassword} onChange={(event) => setNewPassword(event.target.value)} />
          </label>
        </FormDialog>
      )}

      {deletingPerson && (
        <ConfirmDialog
          title={t('access.users.deleteTitle', { username: deletingPerson.username })}
          body={t('access.users.deleteBody')}
          confirmLabel={t('common.remove')}
          busy={busy}
          onConfirm={async () => {
            if (await run(() => graphql(DELETE_USER, { userId: deletingPerson.id }))) {
              setDeletingPerson(null)
            }
          }}
          onClose={() => setDeletingPerson(null)}
        />
      )}

      {(addingGroup || editingGroup) && (
        <FormDialog
          title={editingGroup ? t('access.groups.editTitle', { name: editingGroup.name }) : t('access.groups.new')}
          submitLabel={editingGroup ? t('common.save') : t('common.create')}
          busy={busy}
          error={problem}
          canSubmit={groupDraft.name.trim() !== ''}
          onClose={() => {
            setAddingGroup(false)
            setEditingGroup(null)
          }}
          onSubmit={async () => {
            // Somebody with only user:manage may change who is in a group and
            // nothing else about it; the server refuses more, so send no more.
            const variables = managesGroups
              ? {
                  name: groupDraft.name.trim(),
                  description: groupDraft.description,
                  idpGroup: groupDraft.idpGroup.trim(),
                  userIds: groupDraft.userIds,
                  roleIds: groupDraft.roleIds,
                  domainIds: groupDraft.domainIds,
                }
              : { userIds: groupDraft.userIds }
            const ok = editingGroup
              ? await run(() => graphql(UPDATE_GROUP, { groupId: editingGroup.id, ...variables }))
              : await run(() => graphql(CREATE_GROUP, variables))
            if (ok) {
              setAddingGroup(false)
              setEditingGroup(null)
            }
          }}
        >
          {managesGroups && (
            <>
              <label>
                {t('access.groups.name')}
                <input
                  autoFocus
                  value={groupDraft.name}
                  onChange={(event) => setGroupDraft({ ...groupDraft, name: event.target.value })}
                />
              </label>
              <label>
                {t('access.groups.description')}
                <input
                  value={groupDraft.description}
                  onChange={(event) => setGroupDraft({ ...groupDraft, description: event.target.value })}
                />
              </label>
              <label>
                {t('access.groups.idpGroup')}
                <input
                  value={groupDraft.idpGroup}
                  onChange={(event) => setGroupDraft({ ...groupDraft, idpGroup: event.target.value })}
                />
                <span className="muted">{t('access.groups.idpGroupHint')}</span>
              </label>
            </>
          )}
          <CheckList
            label={t('access.groups.users')}
            items={users}
            selected={groupDraft.userIds}
            onChange={(userIds) => setGroupDraft({ ...groupDraft, userIds })}
            describe={(user) => (user.name ? `${user.username} (${user.name})` : user.username)}
            empty="access.users.empty"
          />
          {managesGroups && (
            <>
              <CheckList
                label={t('access.groups.roles')}
                items={roles}
                selected={groupDraft.roleIds}
                onChange={(roleIds) => setGroupDraft({ ...groupDraft, roleIds })}
                describe={(role) => role.name}
                empty="access.roles.empty"
              />
              <CheckList
                label={t('access.groups.domains')}
                hint={t('access.groups.domainsHint')}
                items={domains}
                selected={groupDraft.domainIds}
                onChange={(domainIds) => setGroupDraft({ ...groupDraft, domainIds })}
                describe={(domain) => domain.domain}
                empty="access.groups.noDomainsYet"
              />
            </>
          )}
        </FormDialog>
      )}

      {deletingGroup && (
        <ConfirmDialog
          title={t('access.groups.deleteTitle', { name: deletingGroup.name })}
          body={t('access.groups.deleteBody')}
          confirmLabel={t('common.remove')}
          busy={busy}
          onConfirm={async () => {
            if (await run(() => graphql(DELETE_GROUP, { groupId: deletingGroup.id }))) {
              setDeletingGroup(null)
            }
          }}
          onClose={() => setDeletingGroup(null)}
        />
      )}
    </>
  )
}
