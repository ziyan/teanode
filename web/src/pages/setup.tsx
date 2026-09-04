import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

import { Domain, ServerAddresses, graphql } from '../api'
import { ErrorMessage, Loading, Tag } from '../components/common'
import { useQuery } from '../components/useQuery'
import { Trans, useTranslation } from '../i18n/i18n'

const OVERVIEW = `
  {
    ListDomains {
      id domain
      aliases { id }
      records { records { type verified } }
    }
    GetServerAddresses { ipv4 ipv6 error }
    GetOutgoingIdentity {
      address via reverseName forwardAddresses confirmed
      helloName helloAddresses helloMatches unknown
    }
    GetSettings { submission { host port effectiveHost effectivePort } }
  }`

const UPDATE_SUBMISSION = `
  mutation ($submission: SubmissionParametersInput) {
    UpdateSettings(submission: $submission) {
      submission { host port effectiveHost effectivePort }
    }
  }`

type OutgoingIdentity = {
  address?: string
  via: string
  reverseName?: string
  forwardAddresses?: string[]
  confirmed: boolean
  helloName?: string
  helloAddresses?: string[]
  helloMatches: boolean
  unknown?: string
}

type Submission = {
  host?: string | null
  port?: number | null
  effectiveHost: string
  effectivePort: string
}

// Setup walks a new operator from nothing to working mail. Everything it needs
// is already visible elsewhere in the dashboard; the value is in saying what
// order to do it in and what is still missing.
export function SetupPage() {
  const { t } = useTranslation()
  const { data, error, loading, reload } = useQuery(
    () =>
      graphql<{
        ListDomains: Domain[]
        GetServerAddresses: ServerAddresses
        GetOutgoingIdentity: OutgoingIdentity
        GetSettings: { submission: Submission }
      }>(OVERVIEW),
    [],
  )

  if (loading) {
    return <Loading />
  }
  if (error) {
    return <ErrorMessage error={error} />
  }

  const domains = data?.ListDomains ?? []
  const withAliases = domains.filter((domain) => domain.aliases.length > 0)
  const published = domains.filter((domain) => {
    const records = domain.records?.records ?? []
    return records.length > 0 && records.every((record) => record.verified)
  })

  const steps = [
    {
      done: domains.length > 0,
      title: t('setup.step1'),
      detail: <Trans k="setup.step1Detail" nodes={{ link: <Link to="/domains">{t('nav.domains')}</Link> }} />,
    },
    {
      done: withAliases.length > 0,
      title: t('setup.step2'),
      detail: t('setup.step2Detail'),
    },
    {
      done: domains.length > 0 && published.length === domains.length,
      title: t('setup.step3'),
      detail: t('setup.step3Detail'),
    },
    {
      done: false,
      title: t('setup.step4'),
      detail: t('setup.step4Detail'),
    },
  ]

  return (
    <>
      <p className="muted">
        <Trans
          k="setup.intro"
          // The page that lists what a restart is waiting on is the Server
          // page's own, not the services tab beside it — and there is no
          // "Integrations" surface to name any more: it became tabs of
          // /server, which is why this link had outlived both its label and
          // its destination.
          nodes={{ integrations: <Link to="/server/about">{t('server.title')}</Link> }}
        />
      </p>

      <ServerAddressCard addresses={data?.GetServerAddresses} />
      <OutgoingIdentityCard identity={data?.GetOutgoingIdentity} />
      {data?.GetSettings?.submission && (
        <SubmissionCard submission={data.GetSettings.submission} onSaved={reload} />
      )}

      <h3>{t('setup.stepsTitle')}</h3>
      {steps.map((step, index) => (
        <div className="card" key={index}>
          <div className="row">
            <div>
              <h3 style={{ margin: 0 }}>
                {index + 1}. {step.title}
              </h3>
              <p className="muted" style={{ marginBottom: 0 }}>
                {step.detail}
              </p>
            </div>
            <div className="shrink">
              <Tag value={step.done ? t('common.done') : t('common.toDo')} tone={step.done ? 'good' : undefined} />
            </div>
          </div>
        </div>
      ))}
    </>
  )
}

// ServerAddressCard shows what the outside world sees this server as. It is
// the one value an operator cannot look up anywhere else, and every DNS record
// they are about to create depends on it.
// OutgoingIdentityCard is the other half of "will my mail be accepted", and
// the half nothing reported until now.
//
// The records on a domain's page say how mail reaches this server. These three
// facts say what a receiver sees when this server reaches out, and a large
// receiver decides on them before it reads a single header. They are worth a
// card of their own because the failure is invisible from here: mail is
// accepted, queued, and refused at the far end, and the only trace is a line
// in the delivery queue.
function OutgoingIdentityCard({ identity }: { identity?: OutgoingIdentity }) {
  const { t } = useTranslation()
  if (!identity) {
    return null
  }

  // Nothing to check, and saying so beats reporting a failure that is not one.
  // A relay sends the mail from its own address, under its own name.
  if (identity.unknown) {
    return (
      <div className="card">
        <h3>{t('setup.outgoingTitle')}</h3>
        <p className="muted" style={{ marginBottom: 0 }}>
          {identity.unknown}
        </p>
      </div>
    )
  }

  // The address is a fact rather than a check, so it carries no verdict. A
  // column where one row always says the same thing is a column the eye stops
  // reading, and the two rows below it are the ones that can be wrong.
  const rows: { label: string; value: React.ReactNode; ok?: boolean; advice?: string }[] = [
    {
      label: t('setup.outgoingAddress'),
      value: <span className="mono">{identity.address}</span>,
    },
    {
      label: t('setup.outgoingReverse'),
      value: identity.reverseName ? (
        <span className="mono">{identity.reverseName}</span>
      ) : (
        <span className="muted">{t('setup.outgoingNoReverse')}</span>
      ),
      ok: identity.confirmed,
      advice: identity.confirmed
        ? undefined
        : identity.reverseName
          ? t('setup.outgoingReverseMismatch', {
              name: identity.reverseName,
              resolves: (identity.forwardAddresses ?? []).join(', ') || t('setup.outgoingNothing'),
              address: identity.address ?? '',
            })
          : t('setup.outgoingReverseMissing', { address: identity.address ?? '' }),
    },
    {
      label: t('setup.outgoingHello'),
      value: <span className="mono">{identity.helloName}</span>,
      ok: identity.helloMatches,
      advice: identity.helloMatches
        ? undefined
        : t('setup.outgoingHelloMismatch', {
            name: identity.helloName ?? '',
            resolves: (identity.helloAddresses ?? []).join(', ') || t('setup.outgoingNothing'),
            address: identity.address ?? '',
          }),
    },
  ]

  return (
    <div className="card">
      <h3>{t('setup.outgoingTitle')}</h3>
      <p className="muted" style={{ marginTop: 0 }}>
        {identity.via === 'proxy' ? t('setup.outgoingIntroProxy') : t('setup.outgoingIntro')}
      </p>
      <table>
        <tbody>
          {rows.map((row) => (
            <tr key={row.label}>
              <td className="shrink">{row.label}</td>
              <td>
                {row.value}
                {row.advice && <div className="muted cell-note">{row.advice}</div>}
              </td>
              <td className="shrink">
                {row.ok !== undefined && (
                  <Tag value={row.ok ? t('domain.ok') : t('domain.change')} tone={row.ok ? 'good' : 'warn'} />
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function ServerAddressCard({ addresses }: { addresses?: ServerAddresses }) {
  const { t } = useTranslation()
  if (!addresses) {
    return null
  }

  if (!addresses.ipv4 && !addresses.ipv6) {
    return (
      <div className="card">
        <h3>{t('setup.addressTitle')}</h3>
        <p className="error" style={{ marginBottom: 0 }}>
          {addresses.error ?? t('setup.addressUnknown')} {t('setup.addressUnknownAdvice')}
        </p>
      </div>
    )
  }

  return (
    <div className="card">
      <h3>{t('setup.addressTitle')}</h3>
      <p className="muted" style={{ marginTop: 0 }}>
        {t('setup.addressIntro')}
      </p>
      <table>
        <tbody>
          {addresses.ipv4 && (
            <tr>
              <td className="shrink">A</td>
              <td className="mono">{addresses.ipv4}</td>
            </tr>
          )}
          {addresses.ipv6 && (
            <tr>
              <td className="shrink">AAAA</td>
              <td className="mono">{addresses.ipv6}</td>
            </tr>
          )}
        </tbody>
      </table>
      {!addresses.ipv6 && (
        <p className="muted" style={{ marginBottom: 0 }}>
          {t('setup.noIPv6')}
        </p>
      )}
    </div>
  )
}

// SubmissionCard is the address a mail client is told to use.
//
// Normally it follows the server: its own name, and the port it listens on.
// That is wrong exactly when something forwards a different port to it — a
// container publishing 10587, a firewall taking 587 — and then the setup
// instructions hand somebody a number nothing answers on, which is a
// frustrating half hour for whoever is setting up their phone.
function SubmissionCard({ submission, onSaved }: { submission: Submission; onSaved: () => void }) {
  const { t } = useTranslation()
  const [host, setHost] = useState(submission.host ?? '')
  const [port, setPort] = useState(submission.port ? String(submission.port) : '')
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  useEffect(() => {
    setHost(submission.host ?? '')
    setPort(submission.port ? String(submission.port) : '')
    setSaved(false)
  }, [submission])

  return (
    <form
      className="card"
      onSubmit={async (event) => {
        event.preventDefault()
        setBusy(true)
        setProblem(null)
        setSaved(false)
        try {
          await graphql(UPDATE_SUBMISSION, { submission: { host, port: Number(port) || 0 } })
          setSaved(true)
          onSaved()
        } catch (caught) {
          setProblem(caught instanceof Error ? caught.message : t('setup.submissionFailed'))
        } finally {
          setBusy(false)
        }
      }}
    >
      <h3>{t('setup.submissionTitle')}</h3>
      <p className="muted" style={{ marginTop: 0 }}>
        {t('setup.submissionIntro')}
      </p>

      <p>
        <span className="mono">
          {submission.effectiveHost} : {submission.effectivePort}
        </span>{' '}
        <span className="muted">{t('setup.submissionStartTls')}</span>
      </p>

      <div className="row">
        <label>
          <span>{t('setup.submissionHost')}</span>
          <input
            value={host}
            placeholder={submission.effectiveHost}
            onChange={(event) => setHost(event.target.value)}
          />
        </label>
        <label className="shrink">
          <span>{t('setup.submissionPort')}</span>
          <input
            value={port}
            inputMode="numeric"
            placeholder={submission.effectivePort}
            onChange={(event) => setPort(event.target.value.replace(/[^0-9]/g, ''))}
          />
        </label>
      </div>
      <p className="muted">{t('setup.submissionHelp')}</p>

      {problem && <p className="error">{problem}</p>}
      {saved && <p className="notice good">{t('setup.submissionSaved')}</p>}
      <button className="primary" type="submit" disabled={busy}>
        {busy ? t('integrations.saving') : t('common.save')}
      </button>
    </form>
  )
}
