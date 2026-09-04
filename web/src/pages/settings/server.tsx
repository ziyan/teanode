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
      current latest available notes url checkedAt error checkError applicable reason automatic enabled window upgrading
    }
  }`

const APPLY = `
  mutation ($version: String) {
    ApplyUpgrade(version: $version) {
      current latest available applicable reason upgrading
    }
  }`

type UpgradeStatus = {
  current: string
  latest?: string
  available: boolean
  notes?: string
  url?: string
  checkedAt?: string
  error?: string
  checkError?: string
  applicable: boolean
  reason?: string
  automatic: boolean
  enabled: boolean
  window?: string
  upgrading: boolean
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

// UPGRADE_TIMEOUT_MS bounds the wait for the download and the swap, which
// happen before the restart the wait above covers. Generous, because it is a
// forty-five megabyte download over whatever link the server has.
const UPGRADE_TIMEOUT_MS = 15 * 60_000

// CHECK_TIMEOUT_MS bounds the wait for a release check, which has a
// thirty-second timeout of its own on the server.
const CHECK_TIMEOUT_MS = 40_000

// ServerAboutPage is what this instance is, which version it is running, and
// the two controls that act on the process rather than on the configuration:
// upgrade, and restart.
//
// Separate from the other settings because its subject is not shared. Every
// other setting is in the database and the same everywhere; this is the one
// process you happen to be talking to, and restarting it is not something the
// other instances feel.
export function ServerAboutPage() {
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

  // The check runs in the background — the request that asks for one cannot
  // wait for somebody else's endpoint while holding a database transaction —
  // so this waits for an answer newer than the one on screen. Reading straight
  // back, which is what it did, showed the answer from before the check and
  // made the button look broken.
  async function checkForUpgrade() {
    const before = upgrade.data?.GetUpgrade?.checkedAt
    const beforeError = upgrade.data?.GetUpgrade?.checkError
    setProblem(null)
    setChecking(true)
    try {
      await graphql<{ GetUpgrade: UpgradeStatus }>(UPGRADE, { check: true })

      const deadline = Date.now() + CHECK_TIMEOUT_MS
      for (;;) {
        await new Promise((resolve) => setTimeout(resolve, 1000))
        const status = (await graphql<{ GetUpgrade: UpgradeStatus }>(UPGRADE, { check: false })).GetUpgrade
        // checkError, not error: the second is the last upgrade's failure,
        // which stays set until another upgrade starts — watching it meant
        // breaking on the first poll and showing the answer from before the
        // check, which is what this loop exists to avoid.
        if (status.checkedAt !== before || status.checkError !== beforeError) {
          break
        }
        if (Date.now() > deadline) {
          break
        }
      }
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
  async function applyUpgrade(version?: string) {
    setProblem(null)
    setCameBack(false)
    setUpgrading(true)
    try {
      await graphql(APPLY, { version })
    } catch (failure) {
      if (!isLostConnection(failure)) {
        setProblem(String(failure))
        setUpgrading(false)
        return
      }
    }

    // The server answers as soon as the upgrade starts and stays up for the
    // whole download, which is minutes. Waiting for it to answer again — what
    // restarting does — declared success a second and a half in, re-enabled
    // the buttons and said it had come back, and then the real restart broke
    // the page underneath somebody who had been told it was finished.
    //
    // So: wait for it to stop saying it is upgrading. Either it goes away,
    // which is the restart and is handled by the wait below, or it comes back
    // with an error to show.
    const deadline = Date.now() + UPGRADE_TIMEOUT_MS
    let lost = 0
    // Whether the server has said, in so many words, that it is upgrading.
    // Until it has, a run of failed requests is much more likely to be
    // something between here and it than the restart: the restart cannot
    // happen before the download does.
    let confirmed = false
    for (;;) {
      // Checked at the top, because it is the only way out of this loop for an
      // upgrade that never finishes — and a "continue" below skipped it, so
      // an expired session turned a fifteen-minute bound into a page that
      // polled for ever.
      if (Date.now() > deadline) {
        setUpgrading(false)
        setProblem(t('upgrade.tookTooLong'))
        return
      }
      await new Promise((resolve) => setTimeout(resolve, 2000))
      let status: UpgradeStatus | undefined
      try {
        status = (await graphql<{ GetUpgrade: UpgradeStatus }>(UPGRADE, { check: false })).GetUpgrade
      } catch (caught) {
        // Gone means gone. Anything the server managed to say — a session
        // that expired, a request whose transaction failed — is not the
        // restart, and breaking out here left the page waiting ninety
        // seconds for a server that was still downloading, then telling
        // somebody it had not come back.
        //
        // And a run of them, not one: a single dropped request is a proxy
        // hiccup as often as it is a server going away, and reading one as
        // the restart declared the upgrade finished while it was still
        // downloading. Two was not enough either — a reverse proxy reloading
        // mid-download produces two 502s in a row as easily as a restart
        // does — so the run has to be longer, and longer still while the
        // server has never once said it was upgrading.
        if (isLostConnection(caught)) {
          lost += 1
          if (lost >= (confirmed ? 3 : 5)) {
            break
          }
        } else {
          lost = 0
        }
        continue
      }
      lost = 0
      if (status?.upgrading) {
        confirmed = true
      }
      if (status && !status.upgrading) {
        // Back, and not upgrading: it failed before it got as far as
        // restarting, and the reason is on the status.
        setUpgrading(false)
        await upgrade.reload()
        if (status.error) {
          setProblem(status.error)
        }
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

// isLostConnection separates a connection that dropped from an answer.
//
// The wrong way round was tried first: look for the phrases a refusal uses and
// swallow everything else. Then a session that had expired mid-confirm threw
// "not logged in", matched neither phrase, and the page sat waiting ninety
// seconds for a server that was never restarting.
//
// So: an answer is anything the server managed to say, and only a request that
// never got one is treated as the restart having beaten the reply.
function isLostConnection(failure: unknown): boolean {
  if (failure instanceof TypeError) {
    return true
  }
  if (!(failure instanceof Error)) {
    return false
  }
  // A gateway's answer for a server that is not there. Behind a reverse proxy
  // — the ordinary way this dashboard is exposed — a restart produces 502 or
  // 503 rather than a connection that fails, and reading those as "the server
  // answered" left the page waiting the full fifteen minutes for an upgrade
  // that had already finished.
  return /fetch|network/i.test(failure.message) || /the server returned 50[234]/.test(failure.message)
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
  onApply: (version?: string) => void
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
            <>
              <Tag value={status.latest ?? ''} tone="good" />{' '}
              {status.url && (
                <a href={status.url} target="_blank" rel="noopener noreferrer nofollow">
                  {t('upgrade.releasePage')}
                </a>
              )}
            </>
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
          {status.checkError && <div className="error">{status.checkError}</div>}
        </dd>
      </dl>

      {/* The notes as they were written, in a box that scrolls: a release
          somebody is deciding about is a release whose changelog they should
          be able to read here rather than in another tab. */}
      {/* What changed, as the release said it, so that deciding whether to
          upgrade is a thing somebody can do here rather than in another tab. */}
      {status.available && status.notes && (
        <>
          <h4 className="upgrade-notes-heading">{t('upgrade.whatChanged', { version: status.latest ?? '' })}</h4>
          <pre className="message-text upgrade-notes">{status.notes}</pre>
        </>
      )}

      {status.error && <p className="error">{t('upgrade.failed', { reason: status.error })}</p>}

      {!status.enabled && <p className="notice">{t('upgrade.checkingOff')}</p>}

      {status.automatic && (
        <p className="notice">
          {status.window
            ? t('upgrade.automaticOnWindow', { window: status.window })
            : t('upgrade.automaticOn')}
        </p>
      )}

      {!status.applicable && status.reason && (
        <p className="notice" style={{ marginBottom: 0 }}>
          {t('upgrade.cannot', { reason: status.reason })}
        </p>
      )}

      <div className="page-actions" style={{ marginTop: 12 }}>
        <span />
        <span>
          <button onClick={onCheck} disabled={checking || upgrading || !status.enabled}>
            {checking ? t('upgrade.checking') : t('upgrade.checkNow')}
          </button>{' '}
          {status.available && status.applicable && !confirming && (
            <button
              className="primary"
              onClick={() => setConfirming(true)}
              disabled={upgrading || status.upgrading}
            >
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
              // The version this card is showing, so the upgrade installs what
              // the sentence above the button said rather than whatever is
              // newest by the time it runs.
              onApply(status.latest)
            }}
          >
            {t('upgrade.confirmUpgrade')}
          </button>{' '}
          <button onClick={() => setConfirming(false)} disabled={upgrading}>
            {t('common.cancel')}
          </button>
        </>
      )}

      {(upgrading || status.upgrading) && <p className="muted">{t('upgrade.waiting')}</p>}
    </div>
  )
}
