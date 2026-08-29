import { useState } from 'react'

import { createFirstAccount } from '../api'
import { AuthCard, AuthField } from '../components/authCard'
import { useTranslation } from '../i18n/i18n'

// SetupAccountPage is what a freshly installed server shows: it has no account
// yet, so rather than asking for credentials that do not exist, it asks the
// first person to arrive to choose their own.
export function SetupAccountPage({ onCreated }: { onCreated: () => void }) {
  const { t } = useTranslation()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const mismatch = confirmation.length > 0 && password !== confirmation
  const ready = username.length > 0 && password.length > 0 && password === confirmation

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await createFirstAccount(username, password)
      onCreated()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : t('setupAccount.failed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthCard purpose={t('setupAccount.intro')} footnote={t('setupAccount.warning')} onSubmit={submit}>
      <AuthField
        label={t('login.username')}
        value={username}
        onChange={(event) => setUsername(event.target.value)}
        autoFocus
        autoComplete="username"
      />
      <AuthField
        label={t('login.password')}
        type="password"
        value={password}
        onChange={(event) => setPassword(event.target.value)}
        autoComplete="new-password"
      />
      <AuthField
        label={t('setupAccount.passwordAgain')}
        type="password"
        value={confirmation}
        onChange={(event) => setConfirmation(event.target.value)}
        autoComplete="new-password"
        hint={mismatch ? <span className="error auth-hint">{t('common.passwordsDoNotMatch')}</span> : undefined}
      />

      {error && <p className="error">{error}</p>}

      <button className="primary auth-button" type="submit" disabled={busy || !ready}>
        {busy ? t('setupAccount.creating') : t('setupAccount.create')}
      </button>
    </AuthCard>
  )
}
