import React, { useEffect, useState } from 'react'

import { graphql } from '../api'
import { AuthCard, AuthField } from '../components/authCard'
import { CopyButton } from '../components/settingsList'
import { useTranslation } from '../i18n/i18n'

// The page "teanode auth login" opens.
//
// The client is listening on a loopback port on the reader's own machine and
// has put that port and a nonce in the query. Once the reader presses
// Authorize, this page asks the server for a token — the same mutation the
// tokens page uses — and posts it to the client, nonce included, so that the
// client can tell this page from any other. Nothing passes through the
// clipboard unless the post fails, in which case the whole command to paste
// is shown instead, because a remote desktop or a locked-down browser cannot
// always reach a loopback address.
//
// It is drawn the way the login form is — one card, nothing else on the page
// — because it is the same kind of moment: the reader has been brought here
// by something else to answer one question, and the rail would be a list of
// other places to go.

const CREATE = `
  mutation ($name: String!, $lifetime: String) {
    CreateToken(name: $name, lifetime: $lifetime) {
      secret
      token { id }
    }
  }`

// The same handful of answers the tokens page offers, and the same default.
// The command line may preset one with --lifetime; a value that is not one of
// these is offered as it was given, so the flag is never silently changed.
const LIFETIMES = [
  { value: '720h', label: 'tokens.lifetime30' },
  { value: '2160h', label: 'tokens.lifetime90' },
  { value: '8760h', label: 'tokens.lifetime365' },
  { value: '', label: 'tokens.lifetimeNever' },
] as const

const DEFAULT_LIFETIME = '2160h'

type Phase = 'consent' | 'working' | 'delivered' | 'manual'

export function CommandLinePage({ username }: { username: string }) {
  const { t } = useTranslation()

  const query = new URLSearchParams(window.location.search)
  const port = query.get('port') ?? ''
  const state = query.get('state') ?? ''
  const profile = query.get('name') ?? ''
  const preset = query.has('lifetime') ? (query.get('lifetime') ?? '') : DEFAULT_LIFETIME

  const [phase, setPhase] = useState<Phase>('consent')
  const [lifetime, setLifetime] = useState(preset)
  const [secret, setSecret] = useState('')
  const [error, setError] = useState<string | null>(null)

  // Outside the shell there is nothing else setting the tab's title.
  useEffect(() => {
    document.title = `${t('cli.title')} · ${t('app.name')}`
  }, [t])

  if (!/^\d+$/.test(port) || !state) {
    return (
      <AuthCard purpose={t('cli.title')} onSubmit={(event) => event.preventDefault()}>
        <p className="muted">{t('cli.notOpenedByCommand')}</p>
        <code className="auth-command">teanode auth login --url {window.location.origin}</code>
      </AuthCard>
    )
  }

  const tokenName = profile ? `teanode (${profile})` : 'teanode'
  const lifetimes: { value: string; label: string }[] = LIFETIMES.map((option) => ({
    value: option.value,
    label: t(option.label),
  }))
  if (!lifetimes.some((option) => option.value === preset)) {
    lifetimes.unshift({ value: preset, label: preset })
  }

  async function authorize(event: React.FormEvent) {
    event.preventDefault()
    setError(null)
    setPhase('working')

    let issued = ''
    let tokenId = ''
    try {
      const data = await graphql<{ CreateToken: { secret: string; token: { id: string } } }>(CREATE, {
        name: tokenName,
        lifetime: lifetime || null,
      })
      issued = data.CreateToken.secret
      tokenId = data.CreateToken.token.id
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : t('cli.noToken'))
      setPhase('consent')
      return
    }
    if (!issued) {
      setError(t('cli.noToken'))
      setPhase('consent')
      return
    }
    setSecret(issued)

    // Hand it over. The client checks the nonce before accepting it.
    try {
      const response = await fetch(`http://127.0.0.1:${port}/callback`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ state, token: issued, tokenId, username }),
      })
      setPhase(response.ok ? 'delivered' : 'manual')
    } catch {
      setPhase('manual')
    }
  }

  if (phase === 'delivered') {
    return (
      <AuthCard purpose={t('cli.title')} onSubmit={(event) => event.preventDefault()}>
        <p>{t('cli.delivered')}</p>
      </AuthCard>
    )
  }

  if (phase === 'manual') {
    const command =
      `teanode auth login --url ${window.location.origin}` +
      (profile ? ` --name ${profile}` : '') +
      ` --token ${secret}`
    return (
      <AuthCard purpose={t('cli.manualTitle')} onSubmit={(event) => event.preventDefault()}>
        <p className="muted">{t('cli.manualBody')}</p>
        <code className="auth-command">{command}</code>
        <div className="auth-actions">
          <CopyButton value={command} />
        </div>
      </AuthCard>
    )
  }

  return (
    <AuthCard purpose={t('cli.intro')} onSubmit={(event) => void authorize(event)}>
      <AuthField label={t('cli.signedInAs')} value={username} readOnly />
      <AuthField label={t('cli.tokenLabel')} value={tokenName} readOnly />
      <label className="auth-field">
        <span>{t('tokens.lifetime')}</span>
        <select value={lifetime} onChange={(event) => setLifetime(event.target.value)}>
          {lifetimes.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
      </label>

      {error && <p className="error">{error}</p>}

      <button className="primary auth-button" type="submit" disabled={phase === 'working'}>
        {phase === 'working' ? t('cli.working') : t('cli.authorize')}
      </button>
    </AuthCard>
  )
}
