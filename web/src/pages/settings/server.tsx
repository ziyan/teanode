import { useState } from 'react'

import { graphql } from '../../api'
import { ErrorMessage, Loading, Tag, formatTime } from '../../components/common'
import { useQuery } from '../../components/useQuery'
import { RelativeTime } from '../../components/relativeTime'
import { useTranslation } from '../../i18n/i18n'

const STATUS = `
  {
    GetServerStatus {
      instance version commit startedAt uptimeSeconds
      pendingRestart supervision restarting
    }
  }`

const RESTART = `mutation { RestartServer { started instance supervision } }`

type ServerStatus = {
  instance: string
  version: string
  commit: string
  startedAt: string
  uptimeSeconds: number
  pendingRestart: string[]
  supervision: 'container' | 'systemd' | 'unknown'
  restarting: boolean
}

// RESTART_TIMEOUT_MS bounds the wait afterwards. A server that has not
// answered in this long is not coming back on its own, and saying so is more
// use than a spinner that never stops.
const RESTART_TIMEOUT_MS = 90_000

// ServerPage is what this instance is, and the one control that acts on the
// process rather than on the configuration.
//
// Separate from the other settings because its subject is not shared. Every
// other setting is in the database and the same everywhere; this is the one
// process you happen to be talking to, and restarting it is not something the
// other instances feel.
export function ServerPage() {
  const { t } = useTranslation()
  const { data, error, loading, reload } = useQuery(
    () => graphql<{ GetServerStatus: ServerStatus }>(STATUS),
    [],
  )

  const [confirming, setConfirming] = useState(false)
  const [restarting, setRestarting] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  const [cameBack, setCameBack] = useState(false)

  if (loading && !data) {
    return <Loading />
  }
  if (error && !data) {
    return <ErrorMessage error={error} />
  }

  const status = data?.GetServerStatus
  if (!status) {
    return <ErrorMessage error={new Error(t('server.unavailable'))} />
  }
  const pending = status.pendingRestart ?? []

  async function restart() {
    setProblem(null)
    setConfirming(false)
    setRestarting(true)
    setCameBack(false)

    try {
      await graphql(RESTART)
    } catch (caught) {
      // The server answers and then shuts down, so a connection that closes
      // mid-reply is a success that looks like a failure. Whether it worked is
      // decided by the wait below, not by this.
      void caught
    }

    const deadline = Date.now() + RESTART_TIMEOUT_MS
    for (;;) {
      await new Promise((resolve) => setTimeout(resolve, 1500))
      try {
        await graphql(STATUS)
        setRestarting(false)
        setCameBack(true)
        await reload()
        return
      } catch (caught) {
        void caught
      }
      if (Date.now() > deadline) {
        setRestarting(false)
        setProblem(t('server.didNotComeBack'))
        return
      }
    }
  }

  return (
    <>
      {problem && <p className="error">{problem}</p>}
      {cameBack && <p className="notice good">{t('server.cameBack')}</p>}

      <div className="card">
        <h3>{t('server.thisInstance')}</h3>
        <dl className="properties">
          <dt>{t('server.instance')}</dt>
          <dd>
            <span className="mono">{status.instance}</span>
          </dd>

          <dt>{t('server.version')}</dt>
          <dd>
            {status.version} <span className="muted mono">{status.commit.slice(0, 12)}</span>
          </dd>

          <dt>{t('server.started')}</dt>
          <dd>
            <RelativeTime value={status.startedAt} />{' '}
            <span className="muted">{formatTime(status.startedAt)}</span>
          </dd>

          <dt>{t('server.supervision')}</dt>
          <dd>
            <Tag
              value={t(`server.supervision.${status.supervision}`)}
              tone={status.supervision === 'unknown' ? 'warn' : 'good'}
            />
          </dd>
        </dl>
      </div>

      <div className="card">
        <h3>{t('server.restart')}</h3>

        {pending.length > 0 ? (
          <p className="notice">{t('server.pendingRestart', { settings: pending.join(', ') })}</p>
        ) : (
          <p className="muted" style={{ marginTop: 0 }}>
            {t('server.nothingPending')}
          </p>
        )}

        <p className="muted">{t('server.restartExplained')}</p>

        {status.supervision === 'unknown' && (
          <p className="notice bad">{t('server.unsupervisedWarning')}</p>
        )}

        {confirming ? (
          <>
            <p>
              <strong>{t('server.confirmQuestion', { instance: status.instance })}</strong>
            </p>
            <button className="destructive" onClick={restart} disabled={restarting}>
              {t('server.confirmRestart')}
            </button>{' '}
            <button onClick={() => setConfirming(false)} disabled={restarting}>
              {t('common.cancel')}
            </button>
          </>
        ) : (
          <button onClick={() => setConfirming(true)} disabled={restarting}>
            {restarting ? t('server.restarting') : t('server.restartNow')}
          </button>
        )}

        {restarting && <p className="muted">{t('server.waiting')}</p>}
      </div>
    </>
  )
}
