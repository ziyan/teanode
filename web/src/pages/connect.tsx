import { useCallback, useEffect, useState } from 'react'

import { graphql } from '../api'
import { AuthCard } from '../components/authCard'
import { ErrorMessage, formatClock } from '../components/common'
import { useTranslation } from '../i18n/i18n'

// The page that connects the TeaNode phone app: a one-time QR code, made for
// whoever is signed in here, that the phone's camera scans. The camera opens
// the app (or the App Store), and the app exchanges the code for a sign-in as
// that person.
//
// The code goes from this screen to the person's own phone and nowhere else.
// That is why the phone scans rather than the person approving something the
// phone shows: a code shown for approval can be anybody's, sent with a link to
// this server, and approving it would sign the stranger in.
//
// Drawn as one card, like the authorize page, because it is one thing to do.
// Somebody not signed in meets the login form at this address first.

const CREATE = `
  mutation {
    CreateOAuthConnectCode {
      connectLink
      qrCodeSvg
      expiresAt
    }
  }`

type ConnectCode = {
  connectLink: string
  qrCodeSvg: string
  expiresAt: string
}

export function ConnectPage() {
  const { t } = useTranslation()
  const [code, setCode] = useState<ConnectCode | null>(null)
  const [isMaking, setIsMaking] = useState(false)
  const [isExpired, setIsExpired] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    document.title = `${t('connect.title')} · ${t('app.name')}`
  }, [t])

  const make = useCallback(async () => {
    setError(null)
    setIsMaking(true)
    try {
      const data = await graphql<{ CreateOAuthConnectCode: ConnectCode }>(CREATE)
      setCode(data.CreateOAuthConnectCode)
      setIsExpired(false)
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : t('connect.failed'))
    } finally {
      setIsMaking(false)
    }
  }, [t])

  useEffect(() => {
    void make()
  }, [make])

  // A code past its time is hidden rather than left on the screen to be
  // scanned and refused.
  useEffect(() => {
    if (!code) return
    const left = new Date(code.expiresAt).getTime() - Date.now()
    const timer = window.setTimeout(() => setIsExpired(true), Math.max(left, 0))
    return () => window.clearTimeout(timer)
  }, [code])

  return (
    <AuthCard purpose={t('connect.title')} onSubmit={(event) => event.preventDefault()}>
      <p className="authorize-intro">{t('connect.intro')}</p>

      {code && !isExpired && (
        <>
          <img
            className="connect-qr"
            src={`data:image/svg+xml;utf8,${encodeURIComponent(code.qrCodeSvg)}`}
            alt={t('connect.qrAlt')}
          />
          <p className="muted">{t('connect.expires', { time: formatClock(code.expiresAt) })}</p>
          <p className="authorize-grant muted">{t('connect.warning')}</p>
          {/* On the phone itself there is nothing to scan: the link opens the
              app directly. */}
          <a className="connect-here" href={code.connectLink}>
            {t('connect.openHere')}
          </a>
        </>
      )}
      {isExpired && <p className="muted">{t('connect.expired')}</p>}

      <ErrorMessage error={error} />

      {(isExpired || error) && (
        <div className="authorize-actions">
          <button className="primary auth-button" type="button" onClick={() => void make()} disabled={isMaking}>
            {isMaking ? t('connect.making') : t('connect.newCode')}
          </button>
        </div>
      )}
    </AuthCard>
  )
}
