import { useEffect, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'

import { graphql } from '../../api'
import { ErrorMessage, Loading } from '../../components/common'
import { CloseIcon, PencilIcon, PlusIcon, TrashIcon } from '../../components/icons'
import { Select } from '../../components/select'
import { Tooltip } from '../../components/tooltip'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { useQuery } from '../../components/useQuery'
import { useToast } from '../../components/toast'
import { useTranslation } from '../../i18n/i18n'
import { hasPermission, useSession } from '../../session'
import {
  CheckList,
  DomainSummary,
  GROUP_FIELDS,
  Group,
  Role,
  User,
  listDomains,
  listGroups,
  listRoles,
  listUsers,
} from './common'

const CREATE_GROUP = `
  mutation ($name: String!, $description: String, $idpGroup: String) {
    CreateGroup(name: $name, description: $description, idpGroup: $idpGroup) ${GROUP_FIELDS}
  }`

const UPDATE_GROUP = `
  mutation ($groupId: String!, $name: String, $description: String, $idpGroup: String, $userIds: [String!], $roleIds: [String!], $domainIds: [String!]) {
    UpdateGroup(groupId: $groupId, name: $name, description: $description, idpGroup: $idpGroup, userIds: $userIds, roleIds: $roleIds, domainIds: $domainIds) ${GROUP_FIELDS}
  }`

const DELETE_GROUP = `mutation ($groupId: String!) { DeleteGroup(groupId: $groupId) }`

type GroupDraft = { name: string; description: string; idpGroup: string }

const EMPTY_GROUP: GroupDraft = { name: '', description: '', idpGroup: '' }

// GroupsTab is a group and what is attached to it, side by side: the groups
// to pick from, who is in the one picked, and the roles and domains it
// carries.
//
// All of that used to be one dialog. A dialog is the wrong shape for it —
// everything a group is was behind a button, three lists deep in a box that
// covered the page, and reading who was in a group meant opening it, reading,
// and closing it again. Here it is on the page, and a change is saved as it
// is made rather than held until a Save button at the bottom of a scroll.
export function GroupsTab() {
  const { t } = useTranslation()
  const toast = useToast()
  const session = useSession()
  const managesGroups = hasPermission(session.permissions, 'group:manage')
  const managesUsers = hasPermission(session.permissions, 'user:manage')

  const { data, error, loading, reload } = useQuery(async () => {
    const [groups, users, roles, domains] = await Promise.all([
      listGroups(),
      listUsers().catch(() => [] as User[]),
      managesGroups ? listRoles() : Promise.resolve([] as Role[]),
      listDomains().catch(() => [] as DomainSummary[]),
    ])
    return { groups, users, roles, domains }
  }, [managesGroups])

  // Which group is being read, in the query string: a person's group chip on
  // the users tab links to it, and the link has to arrive at the group.
  // Which row is open is in the path, beside the tab. It was a query
  // parameter written with replace, so choosing one left no history entry:
  // the back button left the page rather than going to the row read before.
  const navigate = useNavigate()
  const asked = useParams().selected ?? ''

  // Links made before the row moved into the path still work, and become the
  // new shape as they arrive.
  const [parameters] = useSearchParams()
  const legacy = parameters.get('group') ?? ''
  useEffect(() => {
    if (legacy) {
      navigate(`/access/groups/${legacy}`, { replace: true })
    }
  }, [legacy, navigate])

  const [addingGroup, setAddingGroup] = useState(false)
  const [editingGroup, setEditingGroup] = useState<Group | null>(null)
  const [groupDraft, setGroupDraft] = useState<GroupDraft>(EMPTY_GROUP)
  const [deletingGroup, setDeletingGroup] = useState<Group | null>(null)
  const [addingMembers, setAddingMembers] = useState(false)
  const [memberDraft, setMemberDraft] = useState<string[]>([])

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

  const groups = data?.groups ?? []
  const users = data?.users ?? []
  const roles = data?.roles ?? []
  const domains = data?.domains ?? []

  // The first group when none is asked for, so the page opens on something to
  // read rather than on an instruction to pick.
  const chosen = groups.find((group) => group.id === asked) ?? groups[0] ?? null
  const choose = (groupId: string) => navigate(groupId ? `/access/groups/${groupId}` : '/access/groups')

  const memberOf = (group: Group) => users.filter((user) => group.userIds.includes(user.id))
  const describePerson = (user: User) => (user.name ? `${user.username} (${user.name})` : user.username)

  // Every change to a group is one call with the lists it changes. Somebody
  // with only user:manage may change who is in a group and nothing else about
  // it; the server refuses more, so send no more.
  const save = (group: Group, changes: Record<string, unknown>) =>
    run(() => graphql(UPDATE_GROUP, { groupId: group.id, ...changes }))

  function open(what: () => void) {
    setProblem(null)
    what()
  }

  return (
    <>
      {loading && !data && <Loading />}
      {error ? <ErrorMessage error={error} /> : null}

      {data && groups.length === 0 ? (
        <SettingsSection
          card
          title={t('access.people.groups')}
          description={t('access.groups.intro')}
          action={
            managesGroups ? (
              <Tooltip label={t('access.groups.new')}>
                <button
                  className="icon-button"
                  type="button"
                  aria-label={t('access.groups.new')}
                  onClick={() =>
                    open(() => {
                      setGroupDraft(EMPTY_GROUP)
                      setAddingGroup(true)
                    })
                  }
                >
                  <PlusIcon size={16} />
                </button>
              </Tooltip>
            ) : undefined
          }
        >
          <SettingsEmpty>{t('access.groups.empty')}</SettingsEmpty>
        </SettingsSection>
      ) : (
        <div className="access-groups">
          {/* Where the list of groups is a column too narrow to be one: the
              same choice as a control rather than as a panel. */}
          <div className="access-picker">
            <Select
              label={t('access.people.groups')}
              value={chosen?.id ?? ''}
              options={groups.map((group) => ({ value: group.id, label: group.name }))}
              onChange={choose}
            />
            {managesGroups && chosen && (
              <>
                <Tooltip label={t('access.groups.edit')}>
                  <button
                    className="icon-button"
                    type="button"
                    aria-label={`${chosen.name}: ${t('access.groups.edit')}`}
                    onClick={() =>
                      open(() => {
                        setGroupDraft({
                          name: chosen.name,
                          description: chosen.description ?? '',
                          idpGroup: chosen.idpGroup ?? '',
                        })
                        setEditingGroup(chosen)
                      })
                    }
                  >
                    <PencilIcon size={16} />
                  </button>
                </Tooltip>
                <Tooltip label={t('common.remove')}>
                  <button
                    className="icon-button danger"
                    type="button"
                    aria-label={`${chosen.name}: ${t('common.remove')}`}
                    onClick={() => open(() => setDeletingGroup(chosen))}
                  >
                    <TrashIcon size={16} />
                  </button>
                </Tooltip>
              </>
            )}
            {managesGroups && (
              <Tooltip label={t('access.groups.new')}>
                <button
                  className="icon-button"
                  type="button"
                  aria-label={t('access.groups.new')}
                  onClick={() =>
                    open(() => {
                      setGroupDraft(EMPTY_GROUP)
                      setAddingGroup(true)
                    })
                  }
                >
                  <PlusIcon size={16} />
                </button>
              </Tooltip>
            )}
          </div>

          <SettingsSection
            card
            title={t('access.people.groups')}
            action={
              managesGroups ? (
                <Tooltip label={t('access.groups.new')}>
                  <button
                    className="icon-button"
                    type="button"
                    aria-label={t('access.groups.new')}
                    onClick={() =>
                      open(() => {
                        setGroupDraft(EMPTY_GROUP)
                        setAddingGroup(true)
                      })
                    }
                  >
                    <PlusIcon size={16} />
                  </button>
                </Tooltip>
              ) : undefined
            }
          >
            {groups.map((group) => (
              <div key={group.id} className={group.id === chosen?.id ? 'access-pick-row chosen' : 'access-pick-row'}>
                <button
                  type="button"
                  className="access-pick-name"
                  aria-current={group.id === chosen?.id}
                  onClick={() => choose(group.id)}
                >
                  {group.name}
                </button>
                {managesGroups && group.id === chosen?.id && (
                  <div className="row-actions">
                    <Tooltip label={t('access.groups.edit')}>
                      <button
                        className="icon-action"
                        type="button"
                        aria-label={`${group.name}: ${t('access.groups.edit')}`}
                        onClick={() =>
                          open(() => {
                            setGroupDraft({
                              name: group.name,
                              description: group.description ?? '',
                              idpGroup: group.idpGroup ?? '',
                            })
                            setEditingGroup(group)
                          })
                        }
                      >
                        <PencilIcon size={16} />
                      </button>
                    </Tooltip>
                    <Tooltip label={t('common.remove')}>
                      <button
                        className="icon-action danger"
                        type="button"
                        aria-label={`${group.name}: ${t('common.remove')}`}
                        onClick={() => open(() => setDeletingGroup(group))}
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
            <SettingsSection
              card
              title={t('access.groups.membersTitle', { count: chosen.userIds.length })}
              description={chosen.description || undefined}
              action={
                managesUsers || managesGroups ? (
                  <Tooltip label={t('access.groups.addMembers')}>
                    <button
                      className="icon-button"
                      type="button"
                      aria-label={t('access.groups.addMembers')}
                      onClick={() =>
                        open(() => {
                          setMemberDraft(chosen.userIds)
                          setAddingMembers(true)
                        })
                      }
                    >
                      <PlusIcon size={16} />
                    </button>
                  </Tooltip>
                ) : undefined
              }
            >
              {memberOf(chosen).length === 0 && <SettingsEmpty>{t('access.people.noMembers')}</SettingsEmpty>}
              {memberOf(chosen).map((user) => (
                <SettingsRow
                  key={user.id}
                  title={user.username}
                  subtitle={
                    <>
                      {user.name || t('common.none')}
                      {user.email ? ` · ${user.email}` : ''}
                    </>
                  }
                  actions={
                    managesUsers || managesGroups ? (
                      <div className="row-actions">
                        <Tooltip label={t('access.groups.removeMember')}>
                          <button
                            className="icon-action"
                            type="button"
                            disabled={busy}
                            aria-label={`${user.username}: ${t('access.groups.removeMember')}`}
                            onClick={() =>
                              void save(chosen, {
                                userIds: chosen.userIds.filter((userId) => userId !== user.id),
                              })
                            }
                          >
                            <CloseIcon size={16} />
                          </button>
                        </Tooltip>
                      </div>
                    ) : undefined
                  }
                />
              ))}
            </SettingsSection>
          )}

          {chosen && managesGroups && (
            <div className="access-attached">
              {/* Ticking writes. What a group holds is two short lists, and a
                  Save button under them would be a second thing to remember
                  for a change that is one click. */}
              <CheckList
                label={t('access.groups.roles')}
                items={roles}
                selected={chosen.roleIds}
                onChange={(roleIds) => void save(chosen, { roleIds })}
                // The name reads as the row; what the role is for is the
                // line under it, the way a permission's key is.
                describe={(role) => (
                  <>
                    {role.name}
                    {role.description && <span>{role.description}</span>}
                  </>
                )}
                empty="access.roles.empty"
              />
              <CheckList
                label={t('access.groups.domains')}
                hint={t('access.groups.domainsHint')}
                items={domains}
                selected={chosen.domainIds}
                onChange={(domainIds) => void save(chosen, { domainIds })}
                describe={(domain) => domain.domain}
                text={(domain) => domain.domain}
                empty="access.groups.noDomainsYet"
              />
              {chosen.idpGroup && (
                <p className="muted access-idp-note">
                  {t('access.groups.idpGroup')}: {chosen.idpGroup}
                </p>
              )}
            </div>
          )}
        </div>
      )}

      {addingMembers && chosen && (
        <FormDialog
          title={t('access.groups.addMembersTitle', { name: chosen.name })}
          submitLabel={t('common.save')}
          busy={busy}
          error={problem}
          onClose={() => setAddingMembers(false)}
          onSubmit={async () => {
            if (await save(chosen, { userIds: memberDraft })) {
              setAddingMembers(false)
            }
          }}
        >
          <CheckList
            label={t('access.groups.users')}
            items={users}
            selected={memberDraft}
            onChange={setMemberDraft}
            describe={describePerson}
            text={(user) => `${user.username} ${user.name ?? ''} ${user.email ?? ''}`}
            empty="access.users.empty"
          />
        </FormDialog>
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
            const variables = {
              name: groupDraft.name.trim(),
              description: groupDraft.description,
              idpGroup: groupDraft.idpGroup.trim(),
            }
            const ok = editingGroup
              ? await run(() => graphql(UPDATE_GROUP, { groupId: editingGroup.id, ...variables }))
              : await run(async () => {
                  const created = await graphql<{ CreateGroup: Group }>(CREATE_GROUP, variables)
                  choose(created.CreateGroup.id)
                })
            if (ok) {
              setAddingGroup(false)
              setEditingGroup(null)
            }
          }}
        >
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
        </FormDialog>
      )}

      {deletingGroup && (
        <ConfirmDialog
          title={t('access.groups.deleteTitle', { name: deletingGroup.name })}
          body={t('access.groups.deleteBody')}
          confirmLabel={t('common.remove')}
          busy={busy}
          error={problem}
          onConfirm={async () => {
            if (await run(() => graphql(DELETE_GROUP, { groupId: deletingGroup.id }))) {
              if (deletingGroup.id === asked) {
                choose('')
              }
              setDeletingGroup(null)
            }
          }}
          onClose={() => setDeletingGroup(null)}
        />
      )}
    </>
  )
}
