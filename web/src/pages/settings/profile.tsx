import { useState } from 'react'

import { graphql } from '../../api'
import { ErrorMessage, Loading } from '../../components/common'
import { useQuery } from '../../components/useQuery'
import { useToast } from '../../components/toast'
import { ConfirmDialog } from '../../components/dialog'
import { useTranslation } from '../../i18n/i18n'

const CURRENT_USER = `{ GetCurrentUser { id username name email } }`

// An account is named by its identifier, and "username" is the name to sign in
// with from here on. This page used to send the old username as the identity
// and the new one as "newUsername", which the schema stopped taking: saving
// the page failed with "unknown argument" and nothing was changed.
const UPDATE = `
  mutation ($userId: String!, $username: String, $name: String, $email: String) {
    UpdateUser(userId: $userId, username: $username, name: $name, email: $email) {
      id username name email
    }
  }`

type User = { id: string; username: string; name?: string; email?: string }

// The account itself: what to call you, what you sign in with, and where
// notifications go. Where /settings lands, because it is the one page here
// that is about the person rather than about a mechanism.
export function ProfilePage({ onSaved }: { onSaved: () => void }) {
  const { t } = useTranslation()
  const { data, error, loading, reload } = useQuery(() => graphql<{ GetCurrentUser: User | null }>(CURRENT_USER), [])

  // Null until somebody types. The account is what each field shows until
  // then, so a value arriving from the server fills the field without an
  // effect writing state, and without overwriting what somebody is part way
  // through typing.
  const [editedName, setEditedName] = useState<string | null>(null)
  const [editedUsername, setEditedUsername] = useState<string | null>(null)
  const [editedEmail, setEditedEmail] = useState<string | null>(null)
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  const [confirmingRename, setConfirmingRename] = useState(false)

  if (loading && !data) {
    return <Loading />
  }
  if (error) {
    return <ErrorMessage error={error} />
  }

  const user = data?.GetCurrentUser
  if (!user) {
    // A server with no accounts, reached over a socket that needs none. There
    // is no profile to edit, and saying so beats an empty form.
    return <p className='muted'>{t('profile.noAccount')}</p>
  }

  const name = editedName ?? user.name ?? ''
  const username = editedUsername ?? user.username
  const email = editedEmail ?? user.email ?? ''

  const renaming = username.trim() !== user.username
  const changed = renaming || name.trim() !== (user.name ?? '') || email.trim() !== (user.email ?? '')
  const ready = changed && username.trim().length > 0 && !busy

  async function save() {
    setBusy(true)
    setProblem(null)
    try {
      await graphql(UPDATE, {
        userId: user!.id,
        name: name.trim(),
        email: email.trim(),
        username: username.trim(),
      })
      setEditedName(null)
      setEditedUsername(null)
      setEditedEmail(null)
      toast.done(t('profile.saved'))
      await reload()
      // Always, not only on a rename. The rail greets you by your name, so
      // changing it and watching the rail keep the old one is the change
      // appearing not to have worked — which is what it did.
      onSaved()
    } catch (caught) {
      setProblem(caught instanceof Error ? caught.message : t('profile.failed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      {/* The same shape as every other settings form: a card the width of
          the column, a heading, and the fields capped inside it. This one
          used to put the width cap on the card itself, which left it a
          narrow box alone at the left of the page — the one settings page
          that did not look like the others. */}
      <form
        className='card'
        onSubmit={(event) => {
          event.preventDefault()
          if (!ready) {
            return
          }
          // Renaming is the one change here that alters how you sign in, and
          // somebody who typed in the wrong field should be asked rather than
          // locked out of the name they knew.
          if (renaming) {
            setConfirmingRename(true)
            return
          }
          void save()
        }}
      >
        <h3>{t('profile.title')}</h3>
        <p className='muted'>{t('settings.profile.description')}</p>

        <div className='form-narrow'>
          <label>
            <span>{t('profile.name')}</span>
            <input
              value={name}
              maxLength={64}
              placeholder={t('profile.namePlaceholder')}
              onChange={(event) => {
                setEditedName(event.target.value)
                          }}
            />
          </label>
          <p className='muted field-hint'>{t('profile.nameHint')}</p>

          <label>
            <span>{t('profile.username')}</span>
            <input
              value={username}
              maxLength={64}
              autoComplete='username'
              onChange={(event) => {
                setEditedUsername(event.target.value)
                          }}
            />
          </label>
          <p className='muted field-hint'>{t('profile.usernameHint')}</p>

          <label>
            <span>{t('profile.email')}</span>
            <input
              type='email'
              value={email}
              placeholder={t('profile.emailPlaceholder')}
              onChange={(event) => {
                setEditedEmail(event.target.value)
                          }}
            />
          </label>
          <p className='muted field-hint'>{t('profile.emailHint')}</p>
        </div>

        {problem && <p className='error'>{problem}</p>}
        <button className='primary' type='submit' disabled={!ready}>
          {t('common.save')}
        </button>
      </form>

      {confirmingRename && (
        <ConfirmDialog
          title={t('profile.renameTitle')}
          body={t('profile.renameBody', { from: user.username, to: username.trim() })}
          confirmLabel={t('profile.rename')}
          destructive={false}
          busy={busy}
          onConfirm={() => {
            setConfirmingRename(false)
            void save()
          }}
          onClose={() => setConfirmingRename(false)}
        />
      )}
    </>
  )
}
