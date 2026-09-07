import { useState } from 'react'

import { graphql } from '../../api'
import { ErrorMessage, Loading, Tag, formatTime } from '../../components/common'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { useSession } from '../../session'
import { CheckList, Group, USER_FIELDS, User, listGroups, listUsers } from './common'

const CREATE = `
  mutation ($username: String!, $password: String, $name: String, $email: String, $groupIds: [String!]) {
    CreateUser(username: $username, password: $password, name: $name, email: $email, groupIds: $groupIds) ${USER_FIELDS}
  }`

const UPDATE = `
  mutation ($userId: String!, $username: String, $name: String, $email: String, $disabled: Boolean, $groupIds: [String!]) {
    UpdateUser(userId: $userId, username: $username, name: $name, email: $email, disabled: $disabled, groupIds: $groupIds) ${USER_FIELDS}
  }`

const SET_PASSWORD = `mutation ($userId: String!, $password: String!) { SetUserPassword(userId: $userId, password: $password) { id } }`
const DELETE = `mutation ($userId: String!) { DeleteUser(userId: $userId) }`

type Draft = {
  username: string
  password: string
  name: string
  email: string
  groupIds: string[]
}

// UsersTab is everyone with an account, and the way to give somebody one.
// A person made here is put into Members unless told otherwise, so that
// they can read the mailbox they are about to be given.
export function UsersTab() {
  const { t } = useTranslation()
  const session = useSession()
  const { data, error, loading, reload } = useQuery(
    async () => ({ users: await listUsers(), groups: await listGroups() }),
    [],
  )

  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<User | null>(null)
  const [draft, setDraft] = useState<Draft>({ username: '', password: '', name: '', email: '', groupIds: [] })
  const [passwordFor, setPasswordFor] = useState<User | null>(null)
  const [newPassword, setNewPassword] = useState('')
  const [deleting, setDeleting] = useState<User | null>(null)
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

  function startAdding() {
    const members = groups.find((group) => group.name.toLowerCase() === 'members')
    setDraft({ username: '', password: '', name: '', email: '', groupIds: members ? [members.id] : [] })
    setAdding(true)
  }

  function startEditing(user: User) {
    setDraft({ username: user.username, password: '', name: user.name ?? '', email: user.email ?? '', groupIds: user.groupIds })
    setEditing(user)
  }

  return (
    <>
      <SettingsSection
        description={t('access.users.intro')}
        action={
          <button className="primary" type="button" onClick={startAdding}>
            {t('access.users.new')}
          </button>
        }
      >
        {problem && <p className="error">{problem}</p>}
        {loading && !data && <Loading />}
        {error ? <ErrorMessage error={error} /> : null}
        {data && users.length === 0 && <SettingsEmpty>{t('access.users.empty')}</SettingsEmpty>}

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
                <div>
                  {user.groupIds.length > 0
                    ? user.groupIds.map(groupName).join(', ')
                    : t('access.users.noGroups')}
                  {' · '}
                  {t('access.users.created', { time: formatTime(user.createdAt) })}
                </div>
              </>
            }
            actions={
              <>
                <button className="link" type="button" onClick={() => startEditing(user)}>
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
                  <button className="link danger" type="button" onClick={() => setDeleting(user)}>
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
          title={editing ? t('access.users.editTitle', { username: editing.username }) : t('access.users.new')}
          submitLabel={editing ? t('common.save') : t('common.create')}
          busy={busy}
          error={problem}
          canSubmit={draft.username.trim() !== ''}
          onClose={() => {
            setAdding(false)
            setEditing(null)
          }}
          onSubmit={async () => {
            const ok = editing
              ? await run(() =>
                  graphql(UPDATE, {
                    userId: editing.id,
                    username: draft.username.trim(),
                    name: draft.name,
                    email: draft.email,
                    groupIds: draft.groupIds,
                  }),
                )
              : await run(() =>
                  graphql(CREATE, {
                    username: draft.username.trim(),
                    password: draft.password || null,
                    name: draft.name,
                    email: draft.email,
                    groupIds: draft.groupIds,
                  }),
                )
            if (ok) {
              setAdding(false)
              setEditing(null)
            }
          }}
        >
          <label>
            {t('access.users.username')}
            <input
              autoFocus
              value={draft.username}
              onChange={(event) => setDraft({ ...draft, username: event.target.value })}
            />
          </label>
          {!editing && (
            <label>
              {t('access.users.password')}
              <input
                type="password"
                value={draft.password}
                onChange={(event) => setDraft({ ...draft, password: event.target.value })}
              />
              <span className="muted">{t('access.users.passwordHint')}</span>
            </label>
          )}
          <label>
            {t('access.users.name')}
            <input value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} />
          </label>
          <label>
            {t('access.users.email')}
            <input
              type="email"
              value={draft.email}
              onChange={(event) => setDraft({ ...draft, email: event.target.value })}
            />
          </label>
          <CheckList
            label={t('access.users.groups')}
            items={groups}
            selected={draft.groupIds}
            onChange={(groupIds) => setDraft({ ...draft, groupIds })}
            describe={(group) => group.name}
            empty="access.groups.empty"
          />
          {editing && editing.id !== session.userId && (
            <label className="check-list-item">
              <input
                type="checkbox"
                checked={Boolean(editing.disabledAt)}
                onChange={(event) =>
                  void run(() => graphql(UPDATE, { userId: editing.id, disabled: event.target.checked })).then(
                    () => setEditing({ ...editing, disabledAt: event.target.checked ? new Date().toISOString() : null }),
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

      {deleting && (
        <ConfirmDialog
          title={t('access.users.deleteTitle', { username: deleting.username })}
          body={t('access.users.deleteBody')}
          confirmLabel={t('common.remove')}
          busy={busy}
          onConfirm={async () => {
            if (await run(() => graphql(DELETE, { userId: deleting.id }))) {
              setDeleting(null)
            }
          }}
          onClose={() => setDeleting(null)}
        />
      )}
    </>
  )
}
