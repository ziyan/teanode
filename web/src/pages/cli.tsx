import { useState } from 'react'

import { graphql } from '../api'
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

const CREATE = `
  mutation ($name: String!, $lifetime: String) {
    CreateToken(name: $name, lifetime: $lifetime) {
      secret
      token { id }
    }
  }`

type Phase = 'consent' | 'working' | 'delivered' | 'manual'

export function CommandLinePage({ username }: { username: string }) {
  const { t } = useTranslation()
  const [phase, setPhase] = useState<Phase>('consent')
  const [secret, setSecret] = useState('')
  const [error, setError] = useState<string | null>(null)

  const query = new URLSearchParams(window.location.search)
  const port = query.get('port') ?? ''
  const state = query.get('state') ?? ''
  const profile = query.get('name') ?? ''
  const lifetime = query.get('lifetime') ?? ''

  if (!/^\d+$/.test(port) || !state) {
    return (
      <div className="card">
        <p>{t('cli.notOpenedByCommand')}</p>
        <pre className="secret">teanode auth login --url {window.location.origin}</pre>
      </div>
    )
  }

  const tokenName = profile ? `teanode (${profile})` : 'teanode'

  async function authorize() {
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
      <div className="card">
        <p>{t('cli.delivered')}</p>
      </div>
    )
  }

  if (phase === 'manual') {
    const command =
      `teanode auth login --url ${window.location.origin}` +
      (profile ? ` --name ${profile}` : '') +
      ` --token ${secret}`
    return (
      <div className="card">
        <h3>{t('cli.manualTitle')}</h3>
        <p>{t('cli.manualBody')}</p>
        <pre className="secret">{command}</pre>
        <div className="dialog-actions">
          <CopyButton value={command} />
        </div>
      </div>
    )
  }

  return (
    <div className="card">
      <p>{t('cli.intro', { username })}</p>
      <p>
        {t('cli.tokenName')} <code>{tokenName}</code>.{' '}
        {lifetime ? t('cli.expires', { lifetime }) : t('cli.neverExpires')}
      </p>
      {error && <p className="error">{error}</p>}
      <div className="dialog-actions">
        <button className="primary" type="button" disabled={phase === 'working'} onClick={() => void authorize()}>
          {phase === 'working' ? t('cli.working') : t('cli.authorize')}
        </button>
      </div>
    </div>
  )
}
