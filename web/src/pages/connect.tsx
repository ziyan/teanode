import React, { useCallback, useEffect, useState } from 'react'

import { graphql } from '../api'
import { AuthCard } from '../components/authCard'
import { ErrorMessage } from '../components/common'
import { useTranslation } from '../i18n/i18n'

// The page a device sends somebody to when it wants to be signed in: the
// TeaNode phone app shows a code, such as KQ7M-4TXD, and a link here. The
// person types the code (or follows the link, which carries it), sees which
// app and device are asking, and approves or turns it away. The device has
// been asking the server all along and collects its sign-in once approved.
//
// Drawn as one card, like the authorize page, for the same reason: the reader
// was brought here to answer one question. Somebody not signed in meets the
// login form at this address first.

const READ = `
  query ($userCode: String!) {
    ReadOAuthDevicePairing(userCode: $userCode) {
      userCode
      clientName
      deviceName
      username
      isFullAccess
    }
  }`

const DECIDE = `
  mutation ($userCode: String!, $isApproved: Boolean!) {
    DecideOAuthDevicePairing(userCode: $userCode, isApproved: $isApproved) {
      isApproved
    }
  }`

type Pairing = {
  userCode: string
  clientName: string
  deviceName: string
  username: string
  isFullAccess: boolean
}

type Phase = 'typing' | 'reading' | 'asking' | 'working' | 'approved' | 'denied'

export function ConnectPage() {
  const { t } = useTranslation()
  const [typedCode, setTypedCode] = useState(() => new URLSearchParams(window.location.search).get('code') ?? '')
  const [phase, setPhase] = useState<Phase>('typing')
  const [pairing, setPairing] = useState<Pairing | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    document.title = `${t('connect.title')} · ${t('app.name')}`
  }, [t])

  const read = useCallback(
    async (userCode: string) => {
      setError(null)
      setPhase('reading')
      try {
        const data = await graphql<{ ReadOAuthDevicePairing: Pairing }>(READ, { userCode })
        setPairing(data.ReadOAuthDevicePairing)
        setPhase('asking')
      } catch (caught) {
        setError(caught instanceof Error ? caught.message : t('connect.failed'))
        setPhase('typing')
      }
    },
    [t],
  )

  // A link from the device carries its code, so the reader only has to
  // check it rather than type it.
  useEffect(() => {
    const linked = new URLSearchParams(window.location.search).get('code')
    if (linked) void read(linked)
  }, [read])

  const decide = useCallback(
    async (isApproved: boolean) => {
      if (!pairing) return
      setError(null)
      setPhase('working')
      try {
        await graphql(DECIDE, { userCode: pairing.userCode, isApproved })
        setPhase(isApproved ? 'approved' : 'denied')
      } catch (caught) {
        setError(caught instanceof Error ? caught.message : t('connect.failed'))
        setPhase('asking')
      }
    },
    [pairing, t],
  )

  if (phase === 'approved' || phase === 'denied') {
    return (
      <AuthCard purpose={t('connect.title')} onSubmit={(event) => event.preventDefault()}>
        <p className="muted">{phase === 'approved' ? t('connect.approved') : t('connect.denied')}</p>
      </AuthCard>
    )
  }

  if (!pairing || phase === 'typing' || phase === 'reading') {
    return (
      <AuthCard
        purpose={t('connect.title')}
        onSubmit={(event: React.FormEvent) => {
          event.preventDefault()
          void read(typedCode)
        }}
      >
        <p className="authorize-intro">{t('connect.intro')}</p>
        <label className="auth-field">
          <span>{t('connect.codeLabel')}</span>
          <input
            className="connect-code"
            value={typedCode}
            onChange={(event) => setTypedCode(event.target.value)}
            placeholder="KQ7M-4TXD"
            autoComplete="off"
            autoCapitalize="characters"
            spellCheck={false}
            autoFocus
          />
        </label>
        <ErrorMessage error={error} />
        <div className="authorize-actions">
          <button
            className="primary auth-button"
            type="submit"
            disabled={phase === 'reading' || typedCode.trim() === ''}
          >
            {phase === 'reading' ? t('connect.reading') : t('connect.continue')}
          </button>
        </div>
      </AuthCard>
    )
  }

  return (
    <AuthCard
      purpose={t('connect.title')}
      onSubmit={(event: React.FormEvent) => {
        event.preventDefault()
        void decide(true)
      }}
    >
      <p className="authorize-intro">{t('connect.checkCode')}</p>

      <dl className="authorize-facts">
        <dt>{t('connect.code')}</dt>
        <dd>
          <code className="connect-code-shown">{pairing.userCode}</code>
        </dd>

        <dt>{t('authorize.program')}</dt>
        <dd>
          <span className="authorize-name">{pairing.clientName || t('authorize.unnamed')}</span>
          {/* Anybody may register under any name, and a device calls itself
              what it likes; the code is the part the reader can check. */}
          <span className="authorize-claimed muted">{t('authorize.claimedName')}</span>
        </dd>

        {pairing.deviceName && (
          <>
            <dt>{t('connect.device')}</dt>
            <dd>
              <span className="authorize-name">{pairing.deviceName}</span>
              <span className="authorize-claimed muted">{t('connect.claimedDeviceName')}</span>
            </dd>
          </>
        )}

        <dt>{t('authorize.actsAs')}</dt>
        <dd>{pairing.username}</dd>
      </dl>

      <p className="authorize-grant muted">
        {pairing.isFullAccess ? t('authorize.grantFullAccess') : t('authorize.grant')}
      </p>

      <ErrorMessage error={error} />

      <div className="authorize-actions">
        <button className="primary auth-button" type="submit" disabled={phase === 'working'}>
          {phase === 'working' ? t('authorize.working') : t('connect.approve')}
        </button>
        <button className="auth-button" type="button" onClick={() => void decide(false)} disabled={phase === 'working'}>
          {t('connect.deny')}
        </button>
      </div>
    </AuthCard>
  )
}
