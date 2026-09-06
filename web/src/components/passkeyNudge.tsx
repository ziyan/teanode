import { useEffect, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'

import { PasskeyPolicy, graphql } from '../api'
import { useTranslation } from '../i18n/i18n'
import { isPasskeySupported } from '../passkeys'
import { KeyIcon } from './icons'

// A quiet suggestion to add a passkey, for somebody who has none.
//
// Passkeys are the better way in — nothing to type, nothing to phish — but
// the page that offers them is three clicks away under the account menu, and
// nobody goes looking for a setting they do not know exists. So a server that
// offers them says so once, at the top of the page, until the reader either
// adds one or says not now.
//
// It is shown only when it can be acted on: the server has passkeys turned
// on, this browser can register one, and this account has none yet. On the
// passkeys page itself the page is the nudge, so it stays out of the way.

const PASSKEYS = `
  {
    GetPasskeyPolicy { enabled }
    ListPasskeys { id }
  }`

type Response = { GetPasskeyPolicy: PasskeyPolicy; ListPasskeys: { id: string }[] }

// Where a dismissal is remembered. Per account rather than per browser,
// because two people sharing a laptop are two decisions; and in local
// storage rather than on the server, because it is this browser's opinion
// about this browser's nagging, and a server-side flag would silence every
// other device too.
const STORAGE_KEY = 'teanode.passkeyNudge.dismissed'

const PASSKEYS_PATH = '/settings/passkeys'

// The accounts that have said not now, tolerating a browser that refuses
// local storage: private browsing does, and then the nudge simply comes back
// next time, which is the worst outcome there is.
function dismissedAccounts(): string[] {
  try {
    const stored = window.localStorage.getItem(STORAGE_KEY)
    const parsed: unknown = stored ? JSON.parse(stored) : []
    return Array.isArray(parsed) ? parsed.filter((value): value is string => typeof value === 'string') : []
  } catch {
    return []
  }
}

function rememberDismissal(username: string) {
  try {
    const accounts = dismissedAccounts()
    if (!accounts.includes(username)) {
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify([...accounts, username]))
    }
  } catch {
    // Not being able to remember is not a reason to keep the nudge on screen
    // for this visit.
  }
}

export function PasskeyNudge({ username }: { username: string }) {
  const { t } = useTranslation()
  const location = useLocation()
  const [dismissed, setDismissed] = useState(() => dismissedAccounts().includes(username))
  const [wanted, setWanted] = useState(false)

  const onPasskeysPage = location.pathname === PASSKEYS_PATH

  // Asked once, and again when the reader comes back from the passkeys page,
  // which is the one place the answer changes. Not on every navigation: a
  // question to the server per page for one line of text would be the kind
  // of chatter the rail was rebuilt to avoid.
  useEffect(() => {
    if (dismissed || onPasskeysPage || !isPasskeySupported()) {
      return
    }
    let cancelled = false
    graphql<Response>(PASSKEYS).then(
      (answer) => {
        if (!cancelled) {
          setWanted(answer.GetPasskeyPolicy.enabled && answer.ListPasskeys.length === 0)
        }
      },
      () => {
        // An older server without the query, or one restarting. No nudge is
        // the right answer to either.
        if (!cancelled) {
          setWanted(false)
        }
      },
    )
    return () => {
      cancelled = true
    }
  }, [dismissed, onPasskeysPage, username])

  if (dismissed || onPasskeysPage || !wanted) {
    return null
  }

  return (
    <div className="nudge" role="status">
      <span className="nudge-icon">
        <KeyIcon size={18} />
      </span>
      <div className="nudge-text">
        <strong>{t('passkeyNudge.title')}</strong>
        <span className="muted">{t('passkeyNudge.body')}</span>
      </div>
      <div className="nudge-actions">
        <Link className="button" to={PASSKEYS_PATH}>
          {t('passkeyNudge.add')}
        </Link>
        <button
          type="button"
          className="link"
          onClick={() => {
            rememberDismissal(username)
            setDismissed(true)
          }}
        >
          {t('passkeyNudge.dismiss')}
        </button>
      </div>
    </div>
  )
}
