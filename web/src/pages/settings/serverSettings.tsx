import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

import { useTranslation } from '../../i18n/i18n'
import {
  GeoIP,
  Identity,
  Listen,
  Passkey,
  Resolver,
  SessionSettings,
  Smtp,
  StorageSettings,
  useSaver,
} from './integrations'

// The server's own settings, as forms.
//
// The forms in integrations.tsx configure the services this server talks to.
// These configure the server: what it accepts, how it resolves names, how long
// a sign-in lasts, what it listens on, what it calls itself. Every one of them
// was reachable only by exporting the configuration to YAML, editing it, and
// importing it back.
//
// Which of them need a restart is not decided here. The server watches its own
// configuration and reports what a restart would pick up, on the About tab; a
// form that needs one says so beside its button so that the sentence is where
// the decision is made, and links to the page that can do something about it.

// A list typed as one field. Comma separated is how the configuration file
// writes these and how the mail server names field on a domain already asks
// for them, so it is what an operator has already learnt here.
function toList(value: string): string[] {
  return value
    .split(',')
    .map((entry) => entry.trim())
    .filter((entry) => entry !== '')
}

function fromList(value: string[] | undefined): string {
  return (value ?? []).join(', ')
}

// RestartNote is what a form says when saving it changes what is stored and
// nothing else until the process restarts.
function RestartNote() {
  const { t } = useTranslation()
  return (
    <p className="notice">
      {t('serverSettings.needsRestart')} <Link to="/server/about">{t('serverSettings.restartHere')}</Link>
    </p>
  )
}

// SmtpForm holds the limits every message meets. All of them are read per
// message, so a change applies to the next one and there is nothing to
// restart.
export function SmtpForm({ settings, onSaved }: { settings: Smtp; onSaved: () => void }) {
  const { t } = useTranslation()
  const { busy, problem, saved, save } = useSaver(onSaved)

  const [maxMessageSize, setMaxMessageSize] = useState(settings.maxMessageSize)
  const [incoming, setIncoming] = useState(String(settings.maxRecipientsIncoming))
  const [outgoing, setOutgoing] = useState(String(settings.maxRecipientsOutgoing))
  const [greylistDelay, setGreylistDelay] = useState(settings.greylistDelay)
  const [rate, setRate] = useState(String(settings.authRateLimit))
  const [burst, setBurst] = useState(String(settings.authRateBurst))
  const [trusted, setTrusted] = useState(fromList(settings.trustedSenders))

  useEffect(() => {
    setMaxMessageSize(settings.maxMessageSize)
    setIncoming(String(settings.maxRecipientsIncoming))
    setOutgoing(String(settings.maxRecipientsOutgoing))
    setGreylistDelay(settings.greylistDelay)
    setRate(String(settings.authRateLimit))
    setBurst(String(settings.authRateBurst))
    setTrusted(fromList(settings.trustedSenders))
  }, [settings])

  return (
    <form
      className="card"
      onSubmit={(event) => {
        event.preventDefault()
        void save({
          smtp: {
            maxMessageSize,
            maxRecipientsIncoming: Number(incoming),
            maxRecipientsOutgoing: Number(outgoing),
            greylistDelay,
            authRateLimit: Number(rate),
            authRateBurst: Number(burst),
            trustedSenders: toList(trusted),
          },
        })
      }}
    >
      <h3>{t('serverSettings.smtpTitle')}</h3>
      <p className="muted" style={{ marginTop: 0 }}>
        {t('serverSettings.smtpIntro')}
      </p>

      <div className="form-narrow">
        <label>
          <span>{t('serverSettings.maxMessageSize')}</span>
          <input className="mono" value={maxMessageSize} onChange={(event) => setMaxMessageSize(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.maxMessageSizeHint')}</p>

        <label>
          <span>{t('serverSettings.maxRecipientsIncoming')}</span>
          <input type="number" min={1} value={incoming} onChange={(event) => setIncoming(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.maxRecipientsIncomingHint')}</p>

        <label>
          <span>{t('serverSettings.maxRecipientsOutgoing')}</span>
          <input type="number" min={1} value={outgoing} onChange={(event) => setOutgoing(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.maxRecipientsOutgoingHint')}</p>

        <label>
          <span>{t('serverSettings.greylistDelay')}</span>
          <input className="mono" value={greylistDelay} onChange={(event) => setGreylistDelay(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.greylistDelayHint')}</p>

        <label>
          <span>{t('serverSettings.authRateLimit')}</span>
          <input type="number" min={1} value={rate} onChange={(event) => setRate(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.authRateLimitHint')}</p>

        <label>
          <span>{t('serverSettings.authRateBurst')}</span>
          <input type="number" min={1} value={burst} onChange={(event) => setBurst(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.authRateBurstHint')}</p>

        <label>
          <span>{t('serverSettings.trustedSenders')}</span>
          <input className="mono" value={trusted} onChange={(event) => setTrusted(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.trustedSendersHint')}</p>
      </div>

      {problem && <p className="error">{problem}</p>}
      <div className="page-actions">
        <button className="primary" type="submit" disabled={busy}>
          {t('common.save')}
        </button>
        {saved && <span className="muted">{t('serverSettings.savedLive')}</span>}
      </div>
    </form>
  )
}

// ResolverForm is how this server asks DNS questions when it checks a domain.
export function ResolverForm({ settings, onSaved }: { settings: Resolver; onSaved: () => void }) {
  const { t } = useTranslation()
  const { busy, problem, saved, save } = useSaver(onSaved)

  const [nameserver, setNameserver] = useState(settings.nameserver)
  const [checkInterval, setCheckInterval] = useState(settings.checkInterval)
  const [services, setServices] = useState(fromList(settings.externalAddressServices))

  useEffect(() => {
    setNameserver(settings.nameserver)
    setCheckInterval(settings.checkInterval)
    setServices(fromList(settings.externalAddressServices))
  }, [settings])

  return (
    <form
      className="card"
      onSubmit={(event) => {
        event.preventDefault()
        void save({ resolver: { nameserver, checkInterval, externalAddressServices: toList(services) } })
      }}
    >
      <h3>{t('serverSettings.resolverTitle')}</h3>
      <p className="muted" style={{ marginTop: 0 }}>
        {t('serverSettings.resolverIntro')}
      </p>

      <div className="form-narrow">
        <label>
          <span>{t('serverSettings.nameserver')}</span>
          <input className="mono" value={nameserver} onChange={(event) => setNameserver(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.nameserverHint')}</p>

        <label>
          <span>{t('serverSettings.checkInterval')}</span>
          <input className="mono" value={checkInterval} onChange={(event) => setCheckInterval(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.checkIntervalHint')}</p>

        <label>
          <span>{t('serverSettings.externalAddressServices')}</span>
          <input className="mono" value={services} onChange={(event) => setServices(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.externalAddressServicesHint')}</p>
      </div>

      {problem && <p className="error">{problem}</p>}
      <div className="page-actions">
        <button className="primary" type="submit" disabled={busy}>
          {t('common.save')}
        </button>
        {saved && <span className="muted">{t('serverSettings.savedLive')}</span>}
      </div>
    </form>
  )
}

// SessionForm is how long somebody stays signed in.
export function SessionForm({ settings, onSaved }: { settings: SessionSettings; onSaved: () => void }) {
  const { t } = useTranslation()
  const { busy, problem, saved, save } = useSaver(onSaved)
  const [lifetime, setLifetime] = useState(settings.lifetime)

  useEffect(() => {
    setLifetime(settings.lifetime)
  }, [settings])

  return (
    <form
      className="card"
      onSubmit={(event) => {
        event.preventDefault()
        void save({ session: { lifetime } })
      }}
    >
      <h3>{t('serverSettings.sessionTitle')}</h3>
      <p className="muted" style={{ marginTop: 0 }}>
        {t('serverSettings.sessionIntro')}
      </p>

      <div className="form-narrow">
        <label>
          <span>{t('serverSettings.sessionLifetime')}</span>
          <input className="mono" value={lifetime} onChange={(event) => setLifetime(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.sessionLifetimeHint')}</p>
      </div>

      {problem && <p className="error">{problem}</p>}
      <div className="page-actions">
        <button className="primary" type="submit" disabled={busy || lifetime === settings.lifetime}>
          {t('common.save')}
        </button>
        {saved && <span className="muted">{t('serverSettings.savedLive')}</span>}
      </div>
    </form>
  )
}

// PasskeyForm is how this server presents itself to an authenticator.
export function PasskeyForm({ settings, onSaved }: { settings: Passkey; onSaved: () => void }) {
  const { t } = useTranslation()
  const { busy, problem, saved, save } = useSaver(onSaved)

  const [enabled, setEnabled] = useState(settings.enabled)
  const [relyingPartyId, setRelyingPartyId] = useState(settings.relyingPartyId ?? '')
  const [displayName, setDisplayName] = useState(settings.displayName ?? '')
  const [origins, setOrigins] = useState(fromList(settings.origins))
  const [maximum, setMaximum] = useState(String(settings.maximumPerUser))

  useEffect(() => {
    setEnabled(settings.enabled)
    setRelyingPartyId(settings.relyingPartyId ?? '')
    setDisplayName(settings.displayName ?? '')
    setOrigins(fromList(settings.origins))
    setMaximum(String(settings.maximumPerUser))
  }, [settings])

  return (
    <form
      className="card"
      onSubmit={(event) => {
        event.preventDefault()
        void save({
          passkey: {
            enabled,
            relyingPartyId,
            displayName,
            origins: toList(origins),
            maximumPerUser: Number(maximum),
          },
        })
      }}
    >
      <h3>{t('serverSettings.passkeyTitle')}</h3>
      <p className="muted" style={{ marginTop: 0 }}>
        {t('serverSettings.passkeyIntro')}
      </p>

      <label>
        <input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />{' '}
        {t('serverSettings.passkeyEnabled')}
      </label>

      <div className="form-narrow">
        <label>
          <span>{t('serverSettings.relyingPartyId')}</span>
          <input className="mono" value={relyingPartyId} onChange={(event) => setRelyingPartyId(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.relyingPartyIdHint')}</p>

        <label>
          <span>{t('serverSettings.passkeyDisplayName')}</span>
          <input value={displayName} onChange={(event) => setDisplayName(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.passkeyDisplayNameHint')}</p>

        <label>
          <span>{t('serverSettings.passkeyOrigins')}</span>
          <input className="mono" value={origins} onChange={(event) => setOrigins(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.passkeyOriginsHint')}</p>

        <label>
          <span>{t('serverSettings.passkeyMaximum')}</span>
          <input type="number" min={0} value={maximum} onChange={(event) => setMaximum(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.passkeyMaximumHint')}</p>
      </div>

      {problem && <p className="error">{problem}</p>}
      <div className="page-actions">
        <button className="primary" type="submit" disabled={busy}>
          {t('common.save')}
        </button>
        {saved && <span className="muted">{t('serverSettings.savedLive')}</span>}
      </div>
    </form>
  )
}

// ListenForm is the addresses this server binds.
//
// The one form on this page that asks before it saves. An operator who points
// HTTPS somewhere nothing can reach loses the page they did it on, and the way
// back is a shell on the host and "teanode-server config". Naming the address
// in the question is the point of asking: it is the value, read back, at the
// moment before it is stored.
export function ListenForm({ settings, onSaved }: { settings: Listen; onSaved: () => void }) {
  const { t } = useTranslation()
  const { busy, problem, saved, save } = useSaver(onSaved)

  const [smtpIncoming, setSmtpIncoming] = useState(settings.smtpIncoming)
  const [smtpOutgoing, setSmtpOutgoing] = useState(settings.smtpOutgoing)
  const [http, setHttp] = useState(settings.http)
  const [https, setHttps] = useState(settings.https)
  const [debug, setDebug] = useState(settings.debug ?? '')
  const [confirming, setConfirming] = useState(false)

  useEffect(() => {
    setSmtpIncoming(settings.smtpIncoming)
    setSmtpOutgoing(settings.smtpOutgoing)
    setHttp(settings.http)
    setHttps(settings.https)
    setDebug(settings.debug ?? '')
    setConfirming(false)
  }, [settings])

  const changed =
    smtpIncoming !== settings.smtpIncoming ||
    smtpOutgoing !== settings.smtpOutgoing ||
    http !== settings.http ||
    https !== settings.https ||
    debug !== (settings.debug ?? '')

  return (
    <form
      className="card"
      onSubmit={(event) => {
        event.preventDefault()
        setConfirming(true)
      }}
    >
      <h3>{t('serverSettings.listenTitle')}</h3>
      <p className="muted" style={{ marginTop: 0 }}>
        {t('serverSettings.listenIntro')}
      </p>

      <div className="form-narrow">
        <label>
          <span>{t('serverSettings.listenSmtpIncoming')}</span>
          <input className="mono" value={smtpIncoming} onChange={(event) => setSmtpIncoming(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.listenSmtpIncomingHint')}</p>

        <label>
          <span>{t('serverSettings.listenSmtpOutgoing')}</span>
          <input className="mono" value={smtpOutgoing} onChange={(event) => setSmtpOutgoing(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.listenSmtpOutgoingHint')}</p>

        <label>
          <span>{t('serverSettings.listenHttp')}</span>
          <input className="mono" value={http} onChange={(event) => setHttp(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.listenHttpHint')}</p>

        <label>
          <span>{t('serverSettings.listenHttps')}</span>
          <input className="mono" value={https} onChange={(event) => setHttps(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.listenHttpsHint')}</p>

        <label>
          <span>{t('serverSettings.listenDebug')}</span>
          <input className="mono" value={debug} onChange={(event) => setDebug(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.listenDebugHint')}</p>
      </div>

      <RestartNote />

      {problem && <p className="error">{problem}</p>}

      {confirming ? (
        <>
          <p style={{ marginBottom: 8 }}>
            <strong>{t('serverSettings.listenConfirmQuestion', { https })}</strong>
          </p>
          <p className="muted">{t('serverSettings.listenConfirmExplained')}</p>
          <button
            className="primary"
            type="button"
            disabled={busy}
            onClick={() => {
              setConfirming(false)
              void save({ listen: { smtpIncoming, smtpOutgoing, http, https, debug } })
            }}
          >
            {t('serverSettings.listenConfirmSave')}
          </button>{' '}
          <button type="button" onClick={() => setConfirming(false)} disabled={busy}>
            {t('common.cancel')}
          </button>
        </>
      ) : (
        <div className="page-actions">
          <button className="primary" type="submit" disabled={busy || !changed}>
            {t('common.save')}
          </button>
          {saved && <span className="muted">{t('serverSettings.savedNeedsRestart')}</span>}
        </div>
      )}
    </form>
  )
}

// IdentityForm is what this server calls itself. dataDirectory is shown and
// not editable: a server restarted against a different directory does not find
// what is in the old one, and moving it is done with the server stopped.
export function IdentityForm({ settings, onSaved }: { settings: Identity; onSaved: () => void }) {
  const { t } = useTranslation()
  const { busy, problem, saved, save } = useSaver(onSaved)

  const [name, setName] = useState(settings.name)
  const [mailServers, setMailServers] = useState(fromList(settings.mailServers))
  const [logLevel, setLogLevel] = useState(settings.logLevel)

  useEffect(() => {
    setName(settings.name)
    setMailServers(fromList(settings.mailServers))
    setLogLevel(settings.logLevel)
  }, [settings])

  return (
    <form
      className="card"
      onSubmit={(event) => {
        event.preventDefault()
        void save({ identity: { name, mailServers: toList(mailServers), logLevel } })
      }}
    >
      <h3>{t('serverSettings.identityTitle')}</h3>
      <p className="muted" style={{ marginTop: 0 }}>
        {t('serverSettings.identityIntro')}
      </p>

      <div className="form-narrow">
        <label>
          <span>{t('serverSettings.serverName')}</span>
          <input className="mono" value={name} onChange={(event) => setName(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.serverNameHint')}</p>

        <label>
          <span>{t('serverSettings.serverMailServers')}</span>
          <input className="mono" value={mailServers} onChange={(event) => setMailServers(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.serverMailServersHint')}</p>

        <label>
          <span>{t('serverSettings.logLevel')}</span>
          {/* Upper case, which is how the configuration stores them and how
              validation reads them back — a lower-case list left the field
              matching nothing and showing the first entry, so opening the page
              and saving anything else would have quietly set the log level to
              debug. CRITICAL is offered because validation accepts it, though
              a mail server that only logs those is one nobody is watching. */}
          <select value={logLevel.toUpperCase()} onChange={(event) => setLogLevel(event.target.value)}>
            <option value="DEBUG">DEBUG</option>
            <option value="INFO">INFO</option>
            <option value="NOTICE">NOTICE</option>
            <option value="WARNING">WARNING</option>
            <option value="ERROR">ERROR</option>
            <option value="CRITICAL">CRITICAL</option>
          </select>
        </label>
        <p className="muted field-hint">{t('serverSettings.logLevelHint')}</p>
      </div>

      <dl className="properties">
        <dt>{t('serverSettings.dataDirectory')}</dt>
        <dd className="mono">{settings.dataDirectory}</dd>
      </dl>
      <p className="muted field-hint">{t('serverSettings.dataDirectoryHint')}</p>

      {problem && <p className="error">{problem}</p>}
      <div className="page-actions">
        <button className="primary" type="submit" disabled={busy}>
          {t('common.save')}
        </button>
        {saved && <span className="muted">{t('serverSettings.savedLive')}</span>}
      </div>
    </form>
  )
}

// StorageForm is where mail is kept on this machine, and how long a failing
// delivery is kept trying.
export function StorageForm({ settings, onSaved }: { settings: StorageSettings; onSaved: () => void }) {
  const { t } = useTranslation()
  const { busy, problem, saved, save } = useSaver(onSaved)

  const [directory, setDirectory] = useState(settings.directory)
  const [spoolRetention, setSpoolRetention] = useState(settings.spoolRetention)

  useEffect(() => {
    setDirectory(settings.directory)
    setSpoolRetention(settings.spoolRetention)
  }, [settings])

  return (
    <form
      className="card"
      onSubmit={(event) => {
        event.preventDefault()
        void save({ storage: { directory, spoolRetention } })
      }}
    >
      <h3>{t('serverSettings.storageTitle')}</h3>
      <p className="muted" style={{ marginTop: 0 }}>
        {t('serverSettings.storageIntro')}
      </p>

      <div className="form-narrow">
        <label>
          <span>{t('serverSettings.storageDirectory')}</span>
          <input className="mono" value={directory} onChange={(event) => setDirectory(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.storageDirectoryHint')}</p>

        <label>
          <span>{t('serverSettings.spoolRetention')}</span>
          <input className="mono" value={spoolRetention} onChange={(event) => setSpoolRetention(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.spoolRetentionHint')}</p>
      </div>

      <RestartNote />

      {problem && <p className="error">{problem}</p>}
      <div className="page-actions">
        <button className="primary" type="submit" disabled={busy}>
          {t('common.save')}
        </button>
        {saved && <span className="muted">{t('serverSettings.savedNeedsRestart')}</span>}
      </div>
    </form>
  )
}

// GeoIPForm is the optional database that says where a sender connected from.
export function GeoIPForm({ settings, onSaved }: { settings: GeoIP; onSaved: () => void }) {
  const { t } = useTranslation()
  const { busy, problem, saved, save } = useSaver(onSaved)

  const [enabled, setEnabled] = useState(settings.enabled)
  const [databaseFile, setDatabaseFile] = useState(settings.databaseFile ?? '')

  useEffect(() => {
    setEnabled(settings.enabled)
    setDatabaseFile(settings.databaseFile ?? '')
  }, [settings])

  return (
    <form
      className="card"
      onSubmit={(event) => {
        event.preventDefault()
        void save({ geoip: { enabled, databaseFile } })
      }}
    >
      <h3>{t('serverSettings.geoipTitle')}</h3>
      <p className="muted" style={{ marginTop: 0 }}>
        {t('serverSettings.geoipIntro')}
      </p>

      <label>
        <input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />{' '}
        {t('serverSettings.geoipEnabled')}
      </label>

      <div className="form-narrow">
        <label>
          <span>{t('serverSettings.geoipDatabaseFile')}</span>
          <input className="mono" value={databaseFile} onChange={(event) => setDatabaseFile(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.geoipDatabaseFileHint')}</p>
      </div>

      <RestartNote />

      {problem && <p className="error">{problem}</p>}
      <div className="page-actions">
        <button className="primary" type="submit" disabled={busy}>
          {t('common.save')}
        </button>
        {saved && <span className="muted">{t('serverSettings.savedNeedsRestart')}</span>}
      </div>
    </form>
  )
}
