import { useState } from 'react'

import { changePassword } from '../api'
import { useTranslation } from '../i18n/i18n'

// The current password is asked for even though the caller is already signed
// in, because a session can be an unattended browser.
export function ChangePassword({ username }: { username: string }) {
  const { t } = useTranslation()
  const [current, setCurrent] = useState('')
  const [replacement, setReplacement] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [done, setDone] = useState(false)

  const mismatch = confirmation.length > 0 && replacement !== confirmation
  const ready = current.length > 0 && replacement.length > 0 && replacement === confirmation

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    setDone(false)
    try {
      await changePassword(current, replacement)
      setCurrent('')
      setReplacement('')
      setConfirmation('')
      setDone(true)
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : t('password.failed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="card" onSubmit={submit}>
      <h3>{t('password.title')}</h3>
      <p className="muted" style={{ marginTop: 0 }}>
        {t('password.signedInAs', { username })}
      </p>

      <label>
        <span>{t('password.current')}</span>
        <input
          type="password"
          value={current}
          onChange={(event) => setCurrent(event.target.value)}
          autoComplete="current-password"
        />
      </label>
      <label>
        <span>{t('password.new')}</span>
        <input
          type="password"
          value={replacement}
          onChange={(event) => setReplacement(event.target.value)}
          autoComplete="new-password"
        />
      </label>
      <label>
        <span>{t('password.newAgain')}</span>
        <input
          type="password"
          value={confirmation}
          onChange={(event) => setConfirmation(event.target.value)}
          autoComplete="new-password"
        />
        {mismatch && <span className="error">{t('common.passwordsDoNotMatch')}</span>}
      </label>

      {error && <p className="error">{error}</p>}
      {done && <p style={{ color: 'var(--good)' }}>{t('password.changed')}</p>}

      <button className="primary" type="submit" disabled={busy || !ready}>
        {busy ? t('password.changing') : t('password.change')}
      </button>
    </form>
  )
}
