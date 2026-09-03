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

const UPGRADE = `
  query ($check: Boolean) {
    GetUpgrade(check: $check) {
      current latest available notes checkedAt error applicable reason automatic
    }
  }`

const APPLY = `mutation { ApplyUpgrade { current latest available applicable reason } }`

type UpgradeStatus = {
  current: string
  latest?: string
  available: boolean
  notes?: string
  checkedAt?: string
  error?: string
  applicable: boolean
  reason?: string
  automatic: boolean
}

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

  // Asked separately from the status, and allowed to fail on its own: a
  // server that cannot reach the release list is a server with an out-of-date
  // answer to one question, not a broken page.
  const upgrade = useQuery(() => graphql<{ GetUpgrade: UpgradeStatus }>(UPGRADE, { check: false }), [])
  const [checking, setChecking] = useState(false)
  const [upgrading, setUpgrading] = useState(false)

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

    await waitForServer()
  }

  // The server goes away and is looked for again. Shared by the restart button
  // and the upgrade button, because an upgrade ends in a restart and the two
  // waits were the same wait.
  async function waitForServer() {
    const deadline = Date.now() + RESTART_TIMEOUT_MS
    for (;;) {
      await new Promise((resolve) => setTimeout(resolve, 1500))
      try {
        await graphql(STATUS)
        setRestarting(false)
        setCameBack(true)
        await reload()
        await upgrade.reload()
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

  async function checkForUpgrade() {
    setProblem(null)
    setChecking(true)
    try {
      await graphql<{ GetUpgrade: UpgradeStatus }>(UPGRADE, { check: true })
      await upgrade.reload()
    } catch (failure) {
      setProblem(String(failure))
    } finally {
      setChecking(false)
    }
  }

  // The server replies and then restarts, so a connection that closes
  // mid-reply is a success that looks like a failure — the same trap the
  // restart button already knew about. Whether it worked is decided by the
  // wait, not by this: showing an error beside a server that is coming back
  // new invites somebody to press the button again.
  //
  // A refusal is different: it answers before anything is downloaded, and it
  // is the reply the reader needs.
  async function applyUpgrade() {
    setProblem(null)
    setUpgrading(true)
    try {
      await graphql(APPLY)
    } catch (failure) {
      if (isRefusal(failure)) {
        setProblem(String(failure))
        setUpgrading(false)
        return
      }
    }
    setRestarting(true)
    await waitForServer()
    setUpgrading(false)
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

      {/* What has been released since this build, and — where it is possible
          — the button. Above restarting rather than below it because an
          upgrade is the reason most people arrive at this page, and because
          an upgrade is a restart with a download in front of it. */}
      <UpgradeCard
        status={upgrade.data?.GetUpgrade}
        loading={upgrade.loading && !upgrade.data}
        checking={checking}
        upgrading={upgrading}
        onCheck={checkForUpgrade}
        onApply={applyUpgrade}
      />

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

// isRefusal separates an answer from a lost connection.
//
// The server says why it will not upgrade — a container, no supervisor, a
// checksum that did not match — and every one of those arrives as a complete
// reply. A connection that dropped has no message worth showing, and showing
// it would be showing a failure for an upgrade that is happening.
function isRefusal(failure: unknown): boolean {
  return failure instanceof Error && /upgrade:|invalid arguments/.test(failure.message)
}

// UpgradeCard says what is running, what is available, and either offers the
// button or says why there is none.
//
// The reason matters more than the button. Most deployments of this are
// containers, where the answer is that the image is the thing to replace —
// somebody who is told that goes and does it, and somebody shown a greyed-out
// control goes looking for a bug.
function UpgradeCard({
  status,
  loading,
  checking,
  upgrading,
  onCheck,
  onApply,
}: {
  status?: UpgradeStatus
  loading: boolean
  checking: boolean
  upgrading: boolean
  onCheck: () => void
  onApply: () => void
}) {
  const { t } = useTranslation()
  const [confirming, setConfirming] = useState(false)

  if (loading || !status) {
    return null
  }

  return (
    <div className="card">
      <h3>{t('upgrade.title')}</h3>

      <dl className="properties">
        <dt>{t('upgrade.running')}</dt>
        <dd>{status.current}</dd>

        <dt>{t('upgrade.available')}</dt>
        <dd>
          {status.available ? (
            <Tag value={status.latest ?? ''} tone="good" />
          ) : status.latest ? (
            <span className="muted">{t('upgrade.upToDate', { version: status.latest })}</span>
          ) : (
            <span className="muted">{t('upgrade.notCheckedYet')}</span>
          )}
        </dd>

        <dt>{t('upgrade.checked')}</dt>
        <dd>
          {status.checkedAt ? (
            <RelativeTime value={status.checkedAt} />
          ) : (
            <span className="muted">{t('common.none')}</span>
          )}
          {status.error && <div className="error">{status.error}</div>}
        </dd>
      </dl>

      {/* The notes as they were written, in a box that scrolls: a release
          somebody is deciding about is a release whose changelog they should
          be able to read here rather than in another tab. */}
      {status.available && status.notes && (
        <pre className="message-text upgrade-notes">{status.notes}</pre>
      )}

      {status.automatic && <p className="notice">{t('upgrade.automaticOn')}</p>}

      {!status.applicable && status.reason && (
        <p className="notice" style={{ marginBottom: 0 }}>
          {t('upgrade.cannot', { reason: status.reason })}
        </p>
      )}

      <div className="page-actions" style={{ marginTop: 12 }}>
        <span />
        <span>
          <button onClick={onCheck} disabled={checking || upgrading}>
            {checking ? t('upgrade.checking') : t('upgrade.checkNow')}
          </button>{' '}
          {status.available && status.applicable && !confirming && (
            <button className="primary" onClick={() => setConfirming(true)} disabled={upgrading}>
              {upgrading ? t('upgrade.upgrading') : t('upgrade.upgradeTo', { version: status.latest ?? '' })}
            </button>
          )}
        </span>
      </div>

      {confirming && (
        <>
          <p style={{ marginBottom: 8 }}>
            <strong>{t('upgrade.confirmQuestion', { version: status.latest ?? '' })}</strong>
          </p>
          <p className="muted">{t('upgrade.confirmExplained')}</p>
          <button
            className="primary"
            disabled={upgrading}
            onClick={() => {
              setConfirming(false)
              onApply()
            }}
          >
            {t('upgrade.confirmUpgrade')}
          </button>{' '}
          <button onClick={() => setConfirming(false)} disabled={upgrading}>
            {t('common.cancel')}
          </button>
        </>
      )}

      {upgrading && <p className="muted">{t('upgrade.waiting')}</p>}
    </div>
  )
}
