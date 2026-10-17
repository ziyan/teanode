import { useRef, useState } from 'react'

import { graphql } from '../../api'
import { Avatar } from '../../components/avatar'
import { pictureToJpegBase64 } from '../../components/avatarPicture'
import { ErrorMessage, Loading } from '../../components/common'
import { useQuery } from '../../components/useQuery'
import { useToast } from '../../components/toast'
import { ConfirmDialog } from '../../components/dialog'
import { useTranslation } from '../../i18n/i18n'
import { Select } from '../../components/select'

const CURRENT_USER = `{ GetCurrentUser { id username name email avatarUrl avatarColorIndex timezone timezoneMode } }`

// An account is named by its identifier, and "username" is the name to sign in
// with from here on. This page used to send the old username as the identity
// and the new one as "newUsername", which the schema stopped taking: saving
// the page failed with "unknown argument" and nothing was changed.
const UPDATE = `
  mutation ($userId: String!, $username: String, $name: String, $email: String, $timezone: String, $timezoneMode: String) {
    UpdateUser(userId: $userId, username: $username, name: $name, email: $email, timezone: $timezone, timezoneMode: $timezoneMode) {
      id username name email avatarUrl avatarColorIndex timezone timezoneMode
    }
  }`

// The picture is its own pair of requests rather than part of saving the form:
// choosing one is done the moment it is chosen, like a switch, and should not
// wait behind a Save button for the name beside it.
const SET_AVATAR = `
  mutation ($content: String!) {
    SetUserAvatar(content: $content) { id avatarUrl avatarColorIndex }
  }`

const REMOVE_AVATAR = `
  mutation {
    RemoveUserAvatar { id avatarUrl avatarColorIndex }
  }`

type User = {
  id: string
  username: string
  name?: string
  email?: string
  avatarUrl?: string | null
  avatarColorIndex?: number
  timezone?: string
  timezoneMode?: string
}

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
  const [editedTimezone, setEditedTimezone] = useState<string | null>(null)
  const [editedMode, setEditedMode] = useState<string | null>(null)
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const [confirmingRename, setConfirmingRename] = useState(false)
  const [pictureBusy, setPictureBusy] = useState(false)
  const picker = useRef<HTMLInputElement>(null)

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
    return <p className="muted">{t('profile.noAccount')}</p>
  }

  const name = editedName ?? user.name ?? ''
  const username = editedUsername ?? user.username
  const email = editedEmail ?? user.email ?? ''
  const timezone = editedTimezone ?? user.timezone ?? ''
  const timezoneMode = editedMode ?? user.timezoneMode ?? 'auto'
  const browserZone = (() => {
    try {
      return Intl.DateTimeFormat().resolvedOptions().timeZone
    } catch {
      return ''
    }
  })()

  const renaming = username.trim() !== user.username
  const changed =
    renaming ||
    name.trim() !== (user.name ?? '') ||
    email.trim() !== (user.email ?? '') ||
    timezone.trim() !== (user.timezone ?? '') ||
    timezoneMode !== (user.timezoneMode || 'auto')
  const ready = changed && username.trim().length > 0 && !busy

  async function save() {
    setBusy(true)
    try {
      await graphql(UPDATE, {
        userId: user!.id,
        name: name.trim(),
        email: email.trim(),
        username: username.trim(),
        timezone: timezoneMode === 'fixed' ? timezone.trim() : '',
        timezoneMode,
      })
      setEditedName(null)
      setEditedUsername(null)
      setEditedEmail(null)
      setEditedTimezone(null)
      setEditedMode(null)
      toast.done(t('profile.saved'))
      await reload()
      // Always, not only on a rename. The rail greets you by your name, so
      // changing it and watching the rail keep the old one is the change
      // appearing not to have worked — which is what it did.
      onSaved()
    } catch (caught) {
      toast.failure(caught, t('profile.failed'))
    } finally {
      setBusy(false)
    }
  }

  async function choosePicture(file: File) {
    setPictureBusy(true)
    try {
      let content: string
      try {
        content = await pictureToJpegBase64(file)
      } catch {
        // The browser could not open it: not a picture, or a format it does
        // not read. Said as that rather than as a failed upload.
        toast.failure(null, t('profile.pictureUnreadable'))
        return
      }
      await graphql(SET_AVATAR, { content })
      toast.done(t('profile.pictureSaved'))
      await reload()
      // The rail shows the same picture, and is told like it is for a name.
      onSaved()
    } catch (caught) {
      toast.failure(caught, t('profile.pictureFailed'))
    } finally {
      setPictureBusy(false)
    }
  }

  async function removePicture() {
    setPictureBusy(true)
    try {
      await graphql(REMOVE_AVATAR)
      toast.done(t('profile.pictureRemoved'))
      await reload()
      onSaved()
    } catch (caught) {
      toast.failure(caught, t('profile.pictureRemoveFailed'))
    } finally {
      setPictureBusy(false)
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
        className="card"
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
        <p className="muted">{t('settings.profile.description')}</p>

        <div className="form-narrow">
          {/* Not a label: it holds two buttons, and a label would make
              the whole row open the file chooser. */}
          <div className="profile-picture">
            <Avatar
              name={name.trim() || user.username}
              avatarUrl={user.avatarUrl}
              colorIndex={user.avatarColorIndex}
              size={64}
            />
            <div className="profile-picture-body">
              <strong>{t('profile.picture')}</strong>
              <div className="profile-picture-actions">
                <button type="button" disabled={pictureBusy} onClick={() => picker.current?.click()}>
                  {t('profile.pictureChoose')}
                </button>
                {user.avatarUrl ? (
                  <button type="button" disabled={pictureBusy} onClick={() => void removePicture()}>
                    {t('profile.pictureRemove')}
                  </button>
                ) : null}
              </div>
            </div>
            <input
              ref={picker}
              type="file"
              accept="image/*"
              hidden
              onChange={(event) => {
                const file = event.target.files?.[0]
                event.target.value = ''
                if (file) void choosePicture(file)
              }}
            />
          </div>
          <p className="muted field-hint">{t('profile.pictureHint')}</p>

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
          <p className="muted field-hint">{t('profile.nameHint')}</p>

          <label>
            <span>{t('profile.username')}</span>
            <input
              value={username}
              maxLength={64}
              autoComplete="username"
              onChange={(event) => {
                setEditedUsername(event.target.value)
              }}
            />
          </label>
          <p className="muted field-hint">{t('profile.usernameHint')}</p>

          <label>
            <span>{t('profile.email')}</span>
            <input
              type="email"
              value={email}
              placeholder={t('profile.emailPlaceholder')}
              onChange={(event) => {
                setEditedEmail(event.target.value)
              }}
            />
          </label>
          <p className="muted field-hint">{t('profile.emailHint')}</p>

          <label>
            <span>{t('profile.timezone')}</span>
            <Select
              block
              value={timezoneMode}
              label={t('profile.timezone')}
              options={[
                { value: 'auto', label: t('profile.timezoneAuto', { zone: user.timezone || browserZone || '—' }) },
                { value: 'fixed', label: t('profile.timezoneFixed') },
              ]}
              onChange={(value) => {
                setEditedMode(value)
                if (value === 'fixed' && !timezone) {
                  setEditedTimezone(user.timezone || browserZone)
                }
              }}
            />
          </label>
          {timezoneMode === 'fixed' ? (
            <input
              className="field-standalone"
              aria-label={t('profile.timezone')}
              value={timezone}
              placeholder="Europe/Berlin"
              onChange={(event) => {
                setEditedTimezone(event.target.value)
              }}
            />
          ) : null}
          <p className="muted field-hint">{t('profile.timezoneHint')}</p>
        </div>

        <button className="primary" type="submit" disabled={!ready}>
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
