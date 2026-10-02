import { useState } from 'react'

import { endImpersonation } from '../api'
import { useTranslation } from '../i18n/i18n'
import { formatClock } from './common'
import { useToast } from './toast'

// The strip across the top of every page while an operator is signed in as
// somebody else: whose account this is, who is really looking, when it ends
// by itself, and the way back. In the warning color rather than the
// suggestion's, because it is not an offer: everything on the page below
// belongs to somebody else, and everything done on it is done as them.
export function ImpersonationBanner({
  username,
  impersonatorUsername,
  endsAt,
}: {
  username: string
  impersonatorUsername: string
  endsAt?: string
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [returning, setReturning] = useState(false)

  return (
    <div className="impersonation" role="status">
      <div className="impersonation-text">
        <strong>{t('impersonation.title', { username })}</strong>
        <span>
          {endsAt
            ? t('impersonation.bodyUntil', { operator: impersonatorUsername, time: formatClock(endsAt) })
            : t('impersonation.body', { operator: impersonatorUsername })}
        </span>
      </div>
      <button
        type="button"
        className="button"
        disabled={returning}
        onClick={async () => {
          setReturning(true)
          try {
            await endImpersonation()
            // Everything on the page was the person's: start again as the
            // operator rather than leave any of it on screen.
            window.location.assign('/')
          } catch (caught) {
            setReturning(false)
            toast.failure(caught, t('impersonation.returnFailed'))
          }
        }}
      >
        {t('impersonation.return')}
      </button>
    </div>
  )
}
