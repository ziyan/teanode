import React, { useState } from 'react'

import { beginPasskeyAssertion, finishPasskeyAssertion, login } from '../api'
import { AuthCard, AuthField } from '../components/authCard'
import { KeyIcon } from '../components/icons'
import { cancelled, getAssertion, isPasskeySupported } from '../passkeys'
import { useTranslation } from '../i18n/i18n'

export function LoginPage({ onLoggedIn }: { onLoggedIn: () => void }) {
  const { t } = useTranslation()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  // Offered only where it can work. A passkey button on a browser without
  // WebAuthn is a button that fails after somebody has committed to it.
  const [supported] = useState(isPasskeySupported)

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await login(username, password)
      onLoggedIn()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : t('login.failed'))
    } finally {
      setBusy(false)
    }
  }

  // No username first. Discoverable credentials mean the browser offers
  // whichever passkeys it holds for this site, so this asks the server for a
  // challenge, hands it to the authenticator, and hands back what it signed.
  async function signInWithPasskey() {
    setBusy(true)
    setError(null)
    try {
      const ceremony = await beginPasskeyAssertion()
      const response = await getAssertion(ceremony.options)
      await finishPasskeyAssertion(ceremony.ceremonyId, response)
      onLoggedIn()
    } catch (caught) {
      // Somebody who dismissed the browser's prompt does not need to be told
      // their passkey failed.
      if (!cancelled(caught)) {
        setError(caught instanceof Error ? caught.message : t('login.passkeyFailed'))
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthCard
      // The command on a line of its own. Running it through the middle of a
      // centred sentence gave a line nobody can read and nobody can select,
      // and it wrapped in the middle of the command itself.
      footnote={
        <>
          {t('login.hint')}
          <code className="auth-command">teanode user add &lt;username&gt;</code>
        </>
      }
      onSubmit={submit}
    >
      <AuthField
        label={t('login.username')}
        value={username}
        onChange={(event) => setUsername(event.target.value)}
        autoFocus
        autoComplete="username webauthn"
      />
      <AuthField
        label={t('login.password')}
        type="password"
        value={password}
        onChange={(event) => setPassword(event.target.value)}
        autoComplete="current-password"
      />

      {error && <p className="error">{error}</p>}

      <button className="primary auth-button" type="submit" disabled={busy}>
        {busy ? t('login.signingIn') : t('login.signIn')}
      </button>

      {/* Under the password rather than above it, and quieter than it. A
          passkey is the better way in for whoever has one, and a password is
          what everybody has — so the one that always works stays the one the
          form is built around, and this is offered beside it.

          Always shown where the browser supports it, rather than only when
          the server has passkeys turned on: asking whether it does would mean
          telling an anonymous caller something about this server's
          configuration, and pressing it when it is off says so plainly. */}
      {supported && (
        <>
          <div className="auth-or">
            <span>{t('login.or')}</span>
          </div>
          <button className="auth-button" type="button" disabled={busy} onClick={() => void signInWithPasskey()}>
            <KeyIcon size={16} />
            {t('login.withPasskey')}
          </button>
        </>
      )}
    </AuthCard>
  )
}
