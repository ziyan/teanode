import { useState } from 'react'

import { graphql } from '../../api'
import { ErrorMessage, Loading, Tag } from '../../components/common'
import { useQuery } from '../../components/useQuery'
import { RelativeTime } from '../../components/relativeTime'
import { TrashIcon } from '../../components/icons'
import { useTranslation } from '../../i18n/i18n'

const SESSIONS = `
  query ($includeRevoked: Boolean) {
    ListSessions(includeRevoked: $includeRevoked) {
      id current created expires lastUsed ip userAgent revoked
    }
  }`

const REVOKE = `mutation ($sessionId: String!) { RevokeSession(sessionId: $sessionId) }`
const REVOKE_ALL = `mutation { RevokeAllSessions { authenticated } }`

type Session = {
  id: string
  current: boolean
  created: string
  expires?: string | null
  lastUsed?: string | null
  ip?: string | null
  userAgent?: string | null
  revoked?: string | null
}

// SessionsPage lists the browsers signed in to this account.
//
// There used to be nothing to list: a session was a signed cookie and the
// server kept none of them, so the only thing this page could offer was to
// invalidate the signing key and sign everybody out. Each is a row now, so one
// can be ended without touching the others.
export function SessionsPage({ onSignedOut }: { onSignedOut: () => void }) {
  const { t } = useTranslation()
  const [includeRevoked, setIncludeRevoked] = useState(false)
  const { data, error, loading, reload } = useQuery(
    () => graphql<{ ListSessions: Session[] }>(SESSIONS, { includeRevoked }),
    [includeRevoked],
  )

  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  async function run(work: () => Promise<unknown>) {
    setBusy(true)
    setProblem(null)
    try {
      await work()
      await reload()
    } catch (caught) {
      setProblem(caught instanceof Error ? caught.message : t('sessions.failed'))
    } finally {
      setBusy(false)
    }
  }

  if (loading && !data) {
    return <Loading />
  }
  if (error && !data) {
    return <ErrorMessage error={error} />
  }

  const sessions = data?.ListSessions ?? []
  const current = sessions.filter((session) => session.current)
  const others = sessions.filter((session) => !session.current)

  return (
    <>
      <p className="muted">{t('sessions.intro')}</p>
      {problem && <p className="error">{problem}</p>}

      {current.length > 0 && (
        <div className="card">
          <h3>{t('sessions.thisBrowser')}</h3>
          {current.map((session) => (
            <SessionRow key={session.id} session={session} busy={busy} onRevoke={null} />
          ))}
        </div>
      )}

      <div className="card">
        <h3>{t('sessions.otherBrowsers')}</h3>
        {others.length === 0 ? (
          <p className="muted" style={{ marginBottom: 0 }}>
            {t('sessions.noOthers')}
          </p>
        ) : (
          others.map((session) => (
            <SessionRow
              key={session.id}
              session={session}
              busy={busy}
              onRevoke={
                session.revoked
                  ? null
                  : () => run(() => graphql(REVOKE, { sessionId: session.id }))
              }
            />
          ))
        )}
      </div>

      <div className="card">
        <label>
          <input
            type="checkbox"
            checked={includeRevoked}
            onChange={(event) => setIncludeRevoked(event.target.checked)}
          />{' '}
          {t('sessions.showRevoked')}
        </label>

        <p className="muted">{t('sessions.revokeAllExplained')}</p>
        <button
          className="destructive"
          disabled={busy}
          onClick={async () => {
            if (!window.confirm(t('sessions.revokeConfirm'))) {
              return
            }
            setBusy(true)
            setProblem(null)
            try {
              await graphql(REVOKE_ALL)
              // This browser's session was one of them, so the shell has to
              // notice it is signed out.
              onSignedOut()
            } catch (caught) {
              setProblem(caught instanceof Error ? caught.message : t('sessions.failed'))
            } finally {
              setBusy(false)
            }
          }}
        >
          {t('sessions.revokeAll')}
        </button>
      </div>
    </>
  )
}

function SessionRow({
  session,
  busy,
  onRevoke,
}: {
  session: Session
  busy: boolean
  onRevoke: (() => void) | null
}) {
  const { t } = useTranslation()

  return (
    <div className="settings-row">
      <div className="settings-row-detail">
        <strong title={session.userAgent ?? undefined}>
          {describeAgent(session.userAgent) || t('sessions.unknownBrowser')}
        </strong>
        <span className="muted">
          {[
            session.ip,
            session.lastUsed ? (
              <>
                {t('sessions.lastUsed')} <RelativeTime value={session.lastUsed} />
              </>
            ) : (
              t('sessions.neverUsed')
            ),
          ]
            .filter(Boolean)
            .map((part, index) => (
              <span key={index}>
                {index > 0 && ' · '}
                {part}
              </span>
            ))}
        </span>
      </div>

      {session.revoked ? (
        <Tag value={t('sessions.revoked')} tone="bad" />
      ) : session.current ? (
        <Tag value={t('sessions.current')} tone="good" />
      ) : null}

      {onRevoke && (
        <button
          className="icon-button"
          aria-label={t('sessions.revokeOne')}
          title={t('sessions.revokeOne')}
          disabled={busy}
          onClick={onRevoke}
        >
          <TrashIcon />
        </button>
      )}
    </div>
  )
}

// describeAgent shortens a user agent to the part a person recognises.
//
// Not a parser: the string is whatever the client sent, and guessing wrongly
// is worse than showing less. The full value is on the title attribute.
function describeAgent(userAgent?: string | null): string {
  if (!userAgent) {
    return ''
  }
  const browser = /(Firefox|Edg|OPR|Chrome|Safari)\/[\d.]+/.exec(userAgent)
  const platform = /\(([^)]+)\)/.exec(userAgent)
  const names: Record<string, string> = { Edg: 'Edge', OPR: 'Opera' }

  if (!browser) {
    return userAgent.length > 60 ? `${userAgent.slice(0, 60)}…` : userAgent
  }
  const name = names[browser[1]] ?? browser[1]
  return platform ? `${name} · ${platform[1].split(';')[0].trim()}` : name
}
