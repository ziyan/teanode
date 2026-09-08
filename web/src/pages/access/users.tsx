import { useState } from 'react'
import { Link } from 'react-router-dom'

import { graphql } from '../../api'
import { ErrorMessage, Loading, Tag, formatTime } from '../../components/common'
import { KeyIcon, PencilIcon, TrashIcon } from '../../components/icons'
import { Tooltip } from '../../components/tooltip'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { hasPermission, useSession } from '../../session'
import { Group, USER_FIELDS, User, listGroups, listUsers } from './common'

const CREATE_USER = `
  mutation ($username: String!, $password: String, $name: String, $email: String) {
    CreateUser(username: $username, password: $password, name: $name, email: $email) ${USER_FIELDS}
  }`

const UPDATE_USER = `
  mutation ($userId: String!, $username: String, $name: String, $email: String, $disabled: Boolean) {
    UpdateUser(userId: $userId, username: $username, name: $name, email: $email, disabled: $disabled) ${USER_FIELDS}
  }`

const SET_PASSWORD = `mutation ($userId: String!, $password: String!) { SetUserPassword(userId: $userId, password: $password) { id } }`
const DELETE_USER = `mutation ($userId: String!) { DeleteUser(userId: $userId) }`

type PersonDraft = { username: string; password: string; name: string; email: string }

const EMPTY_PERSON: PersonDraft = { username: '', password: '', name: '', email: '' }

// UsersTab is the accounts: who has one, what they are called, and what it
// can do at the moment — which is what the groups beside each name say.
//
// Membership is not edited here. A group is where roles and domains are
// attached, so it is where who is in it belongs too; the chips are the way
// there rather than a second place to set the same thing from. A person made
// here joins the server's default group, which the server decides.
export function UsersTab() {
  const { t } = useTranslation()
  const session = useSession()
  const managesUsers = hasPermission(session.permissions, 'user:manage')

  const { data, error, loading, reload } = useQuery(async () => {
    const [users, groups] = await Promise.all([listUsers().catch(() => [] as User[]), listGroups()])
    return { users, groups }
  }, [])

  const [addingPerson, setAddingPerson] = useState(false)
  const [editingPerson, setEditingPerson] = useState<User | null>(null)
  const [personDraft, setPersonDraft] = useState<PersonDraft>(EMPTY_PERSON)
  const [passwordFor, setPasswordFor] = useState<User | null>(null)
  const [newPassword, setNewPassword] = useState('')
  const [deletingPerson, setDeletingPerson] = useState<User | null>(null)

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
  const groups: Group[] = data?.groups ?? []
  const groupName = (groupId: string) => groups.find((group) => group.id === groupId)?.name ?? groupId

  // The group a new person joins by default — when it is still there. It can
  // be renamed or deleted like any other, and nothing puts it back, so this
  // page must not promise it either way: without it an account joins no
  // group, which is no permissions at all.
  const members = groups.find((group) => group.name.toLowerCase() === 'members')

  // Opening a dialog starts with a clean slate: they share one error, and one
  // should not open showing another's.
  function open(what: () => void) {
    setProblem(null)
    what()
  }

  return (
    <>
      <ErrorMessage error={problem} />
      {loading && !data && <Loading />}
      {error ? <ErrorMessage error={error} /> : null}

      <SettingsSection
        card
        title={t('access.users.tab')}
        description={members ? t('access.users.intro') : t('access.users.introNoMembers')}
        action={
          managesUsers ? (
            <button
              className="primary"
              type="button"
              onClick={() =>
                open(() => {
                  setPersonDraft(EMPTY_PERSON)
                  setAddingPerson(true)
                })
              }
            >
              {t('access.users.new')}
            </button>
          ) : undefined
        }
      >
        {data && users.length === 0 && (
          <SettingsEmpty>{managesUsers ? t('access.users.empty') : t('access.people.hidden')}</SettingsEmpty>
        )}

        {users.map((user) => (
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
                      // What this person can do is what their groups say, so
                      // each one is the way to the group that says it.
                      <Link key={groupId} className="access-chip" to={`/access/groups?group=${groupId}`}>
                        {groupName(groupId)}
                      </Link>
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
                <div className="row-actions">
                  <Tooltip label={t('access.users.edit')}>
                    <button
                      className="icon-action"
                      type="button"
                      aria-label={`${user.username}: ${t('access.users.edit')}`}
                      onClick={() =>
                        open(() => {
                          setPersonDraft({
                            username: user.username,
                            password: '',
                            name: user.name ?? '',
                            email: user.email ?? '',
                          })
                          setEditingPerson(user)
                        })
                      }
                    >
                      <PencilIcon size={16} />
                    </button>
                  </Tooltip>
                  <Tooltip label={t('access.users.setPassword')}>
                    <button
                      className="icon-action"
                      type="button"
                      aria-label={`${user.username}: ${t('access.users.setPassword')}`}
                      onClick={() =>
                        open(() => {
                          setNewPassword('')
                          setPasswordFor(user)
                        })
                      }
                    >
                      <KeyIcon size={16} />
                    </button>
                  </Tooltip>
                  {user.id !== session.userId && (
                    <Tooltip label={t('common.remove')}>
                      <button
                        className="icon-action danger"
                        type="button"
                        aria-label={`${user.username}: ${t('common.remove')}`}
                        onClick={() => open(() => setDeletingPerson(user))}
                      >
                        <TrashIcon size={16} />
                      </button>
                    </Tooltip>
                  )}
                </div>
              ) : undefined
            }
          />
        ))}
      </SettingsSection>

      {(addingPerson || editingPerson) && (
        <FormDialog
          title={
            editingPerson ? t('access.users.editTitle', { username: editingPerson.username }) : t('access.users.new')
          }
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
                  }),
                )
              : // No groups named, so the server puts them in its default one
                // and says so in the log when there is not one.
                await run(() =>
                  graphql(CREATE_USER, {
                    username: personDraft.username.trim(),
                    password: personDraft.password || null,
                    name: personDraft.name,
                    email: personDraft.email,
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
            <input
              value={personDraft.name}
              onChange={(event) => setPersonDraft({ ...personDraft, name: event.target.value })}
            />
          </label>
          <label>
            {t('access.users.email')}
            <input
              type="email"
              value={personDraft.email}
              onChange={(event) => setPersonDraft({ ...personDraft, email: event.target.value })}
            />
          </label>
          <p className="muted">{t('access.users.groupsElsewhere')}</p>
          {editingPerson && editingPerson.id !== session.userId && (
            <label className="check-list-item">
              <input
                type="checkbox"
                checked={Boolean(editingPerson.disabledAt)}
                onChange={(event) =>
                  void run(() =>
                    graphql(UPDATE_USER, { userId: editingPerson.id, disabled: event.target.checked }),
                  ).then(
                    (done) =>
                      done &&
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
            <input
              autoFocus
              type="password"
              value={newPassword}
              onChange={(event) => setNewPassword(event.target.value)}
            />
          </label>
        </FormDialog>
      )}

      {deletingPerson && (
        <ConfirmDialog
          title={t('access.users.deleteTitle', { username: deletingPerson.username })}
          body={t('access.users.deleteBody')}
          confirmLabel={t('common.remove')}
          busy={busy}
          error={problem}
          onConfirm={async () => {
            if (await run(() => graphql(DELETE_USER, { userId: deletingPerson.id }))) {
              setDeletingPerson(null)
            }
          }}
          onClose={() => setDeletingPerson(null)}
        />
      )}
    </>
  )
}
