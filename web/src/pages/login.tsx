import React, { useState } from 'react'

import { beginPasskeyAssertion, finishPasskeyAssertion, login } from '../api'
import { AuthCard, AuthField } from '../components/authCard'
import { KeyIcon } from '../components/icons'
import { cancelled, getAssertion, isPasskeySupported } from '../passkeys'
import { Key, useTranslation } from '../i18n/i18n'

// What a failed single sign-on came back with, as a code the server chose:
// the address bar is not a place to carry a sentence anyone could write.
const SSO_MESSAGES: Record<string, Key> = {
  refused: 'login.sso.refused',
  state: 'login.sso.state',
  verify: 'login.sso.verify',
  noaccount: 'login.sso.noAccount',
  disabled: 'login.sso.disabled',
  failed: 'login.sso.failed',
}

export function LoginPage({
  onLoggedIn,
  passkeysEnabled,
  ssoProviders = [],
}: {
  onLoggedIn: () => void
  passkeysEnabled: boolean
  ssoProviders?: { id: string; name: string }[]
}) {
  const { t } = useTranslation()
  // A single sign-on that failed comes back here with what went wrong in
  // the address bar, since the page it failed on was the provider's.
  const [ssoCode] = useState(() => new URLSearchParams(window.location.search).get('sso'))
  const ssoMessage = ssoCode ? t(SSO_MESSAGES[ssoCode] ?? 'login.sso.failed') : null
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  // Offered only where it can work: a browser with WebAuthn, on a server
  // that has passkeys turned on. A button that fails after somebody has
  // committed to it is worse than no button.
  const [supported] = useState(isPasskeySupported)
  const offerPasskey = supported && passkeysEnabled

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
    <AuthCard onSubmit={submit}>
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
      {!error && ssoMessage && <p className="error">{ssoMessage}</p>}

      <button className="primary auth-button" type="submit" disabled={busy}>
        {busy ? t('login.signingIn') : t('login.signIn')}
      </button>

      {/* Under the password rather than above it, and quieter than it. A
          passkey is the better way in for whoever has one, and a password is
          what everybody has — so the one that always works stays the one the
          form is built around, and this is offered beside it.

          Only when the server has passkeys turned on, which the session
          answer says. It used to be shown wherever the browser could, so as
          not to tell an anonymous caller anything about the configuration;
          but pressing it told them the same thing, after they had chosen it
          and been refused. */}
      {offerPasskey && (
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
      {ssoProviders.length > 0 && (
        <div className="sso-buttons">
          {ssoProviders.map((provider) => (
            <a key={provider.id} className="button" href={`/api/v1/sso/${encodeURIComponent(provider.id)}/start`}>
              {t('login.withProvider', { name: provider.name })}
            </a>
          ))}
        </div>
      )}
    </AuthCard>
  )
}
