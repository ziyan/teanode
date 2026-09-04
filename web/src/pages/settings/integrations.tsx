import { useEffect, useState } from 'react'

import { graphql } from '../../api'
import { ErrorMessage, Loading, Tag } from '../../components/common'
import { useQuery } from '../../components/useQuery'
import { Key, useTranslation } from '../../i18n/i18n'
import {
  GeoIPForm,
  IdentityForm,
  ListenForm,
  PasskeyForm,
  ResolverForm,
  SessionForm,
  SmtpForm,
  StorageForm,
} from './serverSettings'

const SETTINGS = `
  {
    GetSettings {
      s3 { enabled bucket region endpoint pathStyle accessKeyId hasSecretAccessKey credentialsFile }
      route53 { enabled zoneId region accessKeyId hasSecretAccessKey credentialsFile }
      antivirus { enabled host port }
      antispam { enabled host port }
      relay { enabled host port security username hasPassword }
      proxy { socks5 }
      certificates { perDomain hosts acmeEnabled acmeEmail acmeDirectoryUrl acmeChallenge certificateFile privateKeyFile }
      smtp { maxMessageSize maxRecipientsIncoming maxRecipientsOutgoing greylistDelay authRateLimit authRateBurst trustedSenders }
      resolver { nameserver checkInterval externalAddressServices }
      session { lifetime }
      passkey { enabled relyingPartyId displayName origins maximumPerUser }
      listen { smtpIncoming smtpOutgoing http https debug }
      identity { name mailServers logLevel dataDirectory }
      storage { directory spoolRetention }
      geoip { enabled databaseFile }
    }
  }`

export const UPDATE = `
  mutation (
    $s3: S3ParametersInput
    $route53: Route53ParametersInput
    $antivirus: ServiceParametersInput
    $antispam: ServiceParametersInput
    $relay: RelayParametersInput
    $proxy: ProxyParametersInput
    $certificates: CertificateParametersInput
    $smtp: SmtpParametersInput
    $resolver: ResolverParametersInput
    $session: SessionParametersInput
    $passkey: PasskeyParametersInput
    $listen: ListenParametersInput
    $identity: IdentityParametersInput
    $storage: StorageParametersInput
    $geoip: GeoIPParametersInput
  ) {
    UpdateSettings(
      s3: $s3
      route53: $route53
      antivirus: $antivirus
      antispam: $antispam
      relay: $relay
      proxy: $proxy
      certificates: $certificates
      smtp: $smtp
      resolver: $resolver
      session: $session
      passkey: $passkey
      listen: $listen
      identity: $identity
      storage: $storage
      geoip: $geoip
    ) {
      s3 { enabled bucket region endpoint pathStyle accessKeyId hasSecretAccessKey credentialsFile }
      route53 { enabled zoneId region accessKeyId hasSecretAccessKey credentialsFile }
      antivirus { enabled host port }
      antispam { enabled host port }
      relay { enabled host port security username hasPassword }
      certificates { perDomain hosts acmeEnabled acmeEmail acmeDirectoryUrl acmeChallenge certificateFile privateKeyFile }
      smtp { maxMessageSize maxRecipientsIncoming maxRecipientsOutgoing greylistDelay authRateLimit authRateBurst trustedSenders }
      resolver { nameserver checkInterval externalAddressServices }
      session { lifetime }
      passkey { enabled relyingPartyId displayName origins maximumPerUser }
      listen { smtpIncoming smtpOutgoing http https debug }
      identity { name mailServers logLevel dataDirectory }
      storage { directory spoolRetention }
      geoip { enabled databaseFile }
    }
  }`

type S3 = {
  enabled: boolean
  bucket: string
  region: string
  endpoint?: string
  pathStyle: boolean
  accessKeyId?: string
  hasSecretAccessKey: boolean
  credentialsFile?: string
}

type Route53 = {
  enabled: boolean
  zoneId: string
  region: string
  accessKeyId?: string
  hasSecretAccessKey: boolean
  credentialsFile?: string
}

type Service = { enabled: boolean; host: string; port: number }

type Relay = {
  enabled: boolean
  host: string
  port: number
  security: string
  username?: string
  hasPassword: boolean
}

type Proxy = { socks5?: string }

type Certificates = {
  perDomain: boolean
  hosts?: string[]
  acmeEnabled: boolean
  acmeEmail?: string
  acmeDirectoryUrl?: string
  acmeChallenge?: string
  certificateFile?: string
  privateKeyFile?: string
}

export type Smtp = {
  maxMessageSize: string
  maxRecipientsIncoming: number
  maxRecipientsOutgoing: number
  greylistDelay: string
  authRateLimit: number
  authRateBurst: number
  trustedSenders?: string[]
}

export type Resolver = { nameserver: string; checkInterval: string; externalAddressServices?: string[] }
export type SessionSettings = { lifetime: string }
export type Passkey = {
  enabled: boolean
  relyingPartyId?: string
  displayName?: string
  origins?: string[]
  maximumPerUser: number
}
export type Listen = { smtpIncoming: string; smtpOutgoing: string; http: string; https: string; debug?: string }
export type Identity = { name: string; mailServers?: string[]; logLevel: string; dataDirectory: string }
export type StorageSettings = { directory: string; spoolRetention: string }
export type GeoIP = { enabled: boolean; databaseFile?: string }

type Settings = {
  s3: S3
  route53: Route53
  antivirus: Service
  antispam: Service
  relay: Relay
  proxy: Proxy
  certificates: Certificates
  smtp: Smtp
  resolver: Resolver
  session: SessionSettings
  passkey: Passkey
  listen: Listen
  identity: Identity
  storage: StorageSettings
  geoip: GeoIP
}

// The six forms, in four groups.
//
// Stacked they were a page you scrolled through looking for the one you came
// for, and the two about outgoing mail sat far apart from each other. Grouped
// by the question each answers: how mail leaves, where messages are kept, how
// certificates are obtained, and what inspects a message on the way in.
export type Section =
  | 'identity'
  | 'mail'
  | 'sending'
  | 'listeners'
  | 'certificates'
  | 'storage'
  | 'resolver'
  | 'scanning'
  | 'sessions'

// The tabs these four are, for the Server page to render along with the rest
// of its own. Here rather than there because this file is what knows which
// forms exist.
export const INTEGRATION_SECTIONS: { id: Section; label: Key }[] = [
  { id: 'identity', label: 'serverSettings.tabIdentity' },
  { id: 'mail', label: 'serverSettings.tabMail' },
  { id: 'sending', label: 'integrations.tabSending' },
  { id: 'listeners', label: 'serverSettings.tabListeners' },
  { id: 'certificates', label: 'serverSettings.tabCertificates' },
  { id: 'storage', label: 'integrations.tabStorage' },
  { id: 'resolver', label: 'serverSettings.tabResolver' },
  { id: 'scanning', label: 'integrations.tabScanning' },
  { id: 'sessions', label: 'serverSettings.tabSessions' },
]

// IntegrationsSection edits one group of the optional services: how outgoing
// mail leaves, the object store, the DNS solver, or the two scanners.
//
// All of them are read once when the process starts, so saving here changes
// what is stored and nothing else until a restart. The page says so rather
// than leaving the operator to discover it, and points at the one place that
// can do something about it.
//
// One section at a time, chosen by whoever renders this: these are four tabs
// of the Server page now rather than a page of their own, because what a
// server sends mail through and where it keeps messages are the same subject
// as which version it is running.
export function IntegrationsSection({ section }: { section: Section }) {
  const { t } = useTranslation()
  const { data, error, loading, reload } = useQuery(
    () => graphql<{ GetSettings: Settings }>(SETTINGS),
    [],
  )

  if (loading && !data) {
    return <Loading />
  }
  if (error && !data) {
    return <ErrorMessage error={error} />
  }
  const settings = data?.GetSettings
  if (!settings) {
    return <ErrorMessage error={new Error(t('integrations.unavailable'))} />
  }

  return (
    <>

      {section === 'sending' && (
        <>
          <RelayForm settings={settings.relay} onSaved={reload} />
          <ProxyForm settings={settings.proxy} onSaved={reload} />
        </>
      )}
      {section === 'storage' && (
        <>
          <StorageForm settings={settings.storage} onSaved={reload} />
          <ObjectStoreForm settings={settings.s3} onSaved={reload} />
          <GeoIPForm settings={settings.geoip} onSaved={reload} />
        </>
      )}
      {section === 'certificates' && (
        <>
          <CertificateForm settings={settings.certificates} onSaved={reload} />
          <Route53Form settings={settings.route53} onSaved={reload} />
        </>
      )}
      {section === 'identity' && <IdentityForm settings={settings.identity} onSaved={reload} />}
      {section === 'mail' && <SmtpForm settings={settings.smtp} onSaved={reload} />}
      {section === 'listeners' && <ListenForm settings={settings.listen} onSaved={reload} />}
      {section === 'resolver' && <ResolverForm settings={settings.resolver} onSaved={reload} />}
      {section === 'sessions' && (
        <>
          <SessionForm settings={settings.session} onSaved={reload} />
          <PasskeyForm settings={settings.passkey} onSaved={reload} />
        </>
      )}
      {section === 'scanning' && (
        <>
          <ServiceForm
            title={t('integrations.antivirus')}
            description={t('integrations.antivirusDescription')}
            settings={settings.antivirus}
            field="antivirus"
            onSaved={reload}
          />
          <ServiceForm
            title={t('integrations.antispam')}
            description={t('integrations.antispamDescription')}
            settings={settings.antispam}
            field="antispam"
            onSaved={reload}
          />
        </>
      )}
    </>
  )
}

// useSaver holds the busy, error and saved state every form here needs, so
// that four forms do not each grow their own copy of it.
export function useSaver(onSaved: () => Promise<unknown> | unknown) {
  const { t } = useTranslation()
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)

  async function save(variables: Record<string, unknown>) {
    setBusy(true)
    setProblem(null)
    setSaved(false)
    try {
      await graphql(UPDATE, variables)
      setSaved(true)
      await onSaved()
    } catch (caught) {
      setProblem(caught instanceof Error ? caught.message : t('integrations.failed'))
    } finally {
      setBusy(false)
    }
  }

  return { busy, problem, saved, save }
}

// SecretField is an input for a value the API will not read back.
//
// It shows whether one is set rather than pretending to show it, and an empty
// box means "leave it alone" — so a form can be saved after changing the
// bucket without re-entering the key. Clearing one is deliberate and separate.
//
// Whether a key is stored belongs beside the label, not in the placeholder. A
// placeholder is a hint about what to type; it disappears the moment somebody
// types, so a status written there is a status that vanishes exactly when the
// reader is acting on it — and "set — leave empty to keep it" was trying to be
// a status and an instruction at once, with the way to remove it hanging
// underneath as a bare underlined link.
function SecretField({
  label,
  present,
  value,
  onChange,
  onClear,
  cleared,
}: {
  label: string
  present: boolean
  value: string
  onChange: (value: string) => void
  onClear: () => void
  cleared: boolean
}) {
  const { t } = useTranslation()
  const stored = present && !cleared

  return (
    <label className="secret-field">
      <span className="secret-label">
        {label}
        {stored && <Tag value={t('integrations.secretStored')} />}
        {cleared && <Tag value={t('integrations.secretClearing')} tone="warn" />}
        {stored && value === '' && (
          <button type="button" className="link danger secret-clear" onClick={onClear}>
            {t('integrations.clearSecret')}
          </button>
        )}
      </span>
      <input
        type="password"
        value={value}
        placeholder={stored ? t('integrations.secretKeep') : t('integrations.secretEnter')}
        onChange={(event) => onChange(event.target.value)}
        autoComplete="off"
      />
    </label>
  )
}

function SaveRow({
  busy,
  saved,
  problem,
}: {
  busy: boolean
  saved: boolean
  problem: string | null
}) {
  const { t } = useTranslation()
  return (
    <>
      {problem && <p className="error">{problem}</p>}
      {saved && <p className="notice good">{t('integrations.savedNeedsRestart')}</p>}
      <button className="primary" type="submit" disabled={busy}>
        {busy ? t('integrations.saving') : t('common.save')}
      </button>
    </>
  )
}

// PRESETS are the providers whose SMTP endpoint people actually point this
// at, with the host, port and security each documents.
//
// Not a provider integration: the relay speaks SMTP to all of them, so this is
// three fields filled in rather than three code paths. Postmark has no 465 and
// Resend and SES have no 2525, which is why the ports differ.
const PRESETS: { label: string; host: string; port: number; security: string; username?: string }[] = [
  { label: 'Gmail', host: 'smtp.gmail.com', port: 587, security: 'starttls' },
  { label: 'Amazon SES', host: 'email-smtp.us-east-1.amazonaws.com', port: 587, security: 'starttls' },
  { label: 'Postmark', host: 'smtp.postmarkapp.com', port: 587, security: 'starttls' },
  { label: 'Resend', host: 'smtp.resend.com', port: 465, security: 'tls', username: 'resend' },
]

function RelayForm({ settings, onSaved }: { settings: Relay; onSaved: () => void }) {
  const { t } = useTranslation()
  const { busy, problem, saved, save } = useSaver(onSaved)

  const [enabled, setEnabled] = useState(settings.enabled)
  const [host, setHost] = useState(settings.host)
  const [port, setPort] = useState(String(settings.port || 587))
  const [security, setSecurity] = useState(settings.security || 'starttls')
  const [username, setUsername] = useState(settings.username ?? '')
  const [password, setPassword] = useState('')
  const [clearPassword, setClearPassword] = useState(false)

  useEffect(() => {
    setEnabled(settings.enabled)
    setHost(settings.host)
    setPort(String(settings.port || 587))
    setSecurity(settings.security || 'starttls')
    setUsername(settings.username ?? '')
    setPassword('')
    setClearPassword(false)
  }, [settings])

  return (
    <form
      className="card"
      onSubmit={(event) => {
        event.preventDefault()
        void save({
          relay: {
            enabled,
            host,
            port: Number(port) || 0,
            security,
            username,
            ...(clearPassword ? { password: '' } : password ? { password } : {}),
          },
        })
      }}
    >
      <h3>{t('integrations.relay')}</h3>
      <p className="muted" style={{ marginTop: 0 }}>
        {t('integrations.relayDescription')}
      </p>

      <label>
        <input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />{' '}
        {t('integrations.relayEnabled')}
      </label>

      <label>
        <span>{t('integrations.relayPreset')}</span>
        <select
          value=""
          onChange={(event) => {
            const preset = PRESETS.find((candidate) => candidate.label === event.target.value)
            if (!preset) {
              return
            }
            setHost(preset.host)
            setPort(String(preset.port))
            setSecurity(preset.security)
            if (preset.username) {
              setUsername(preset.username)
            }
          }}
        >
          <option value="">{t('integrations.relayPresetChoose')}</option>
          {PRESETS.map((preset) => (
            <option key={preset.label} value={preset.label}>
              {preset.label}
            </option>
          ))}
        </select>
        <span className="muted">{t('integrations.relayPresetHelp')}</span>
      </label>

      <div className="row">
        <label>
          <span>{t('integrations.host')}</span>
          <input value={host} onChange={(event) => setHost(event.target.value)} placeholder="smtp.example.net" />
        </label>
        <label className="shrink">
          <span>{t('integrations.port')}</span>
          <input
            value={port}
            inputMode="numeric"
            onChange={(event) => setPort(event.target.value.replace(/[^0-9]/g, ''))}
          />
        </label>
        <label className="shrink">
          <span>{t('integrations.relaySecurity')}</span>
          <select value={security} onChange={(event) => setSecurity(event.target.value)}>
            <option value="starttls">{t('integrations.relayStartTls')}</option>
            <option value="tls">{t('integrations.relayTls')}</option>
            <option value="none">{t('integrations.relayNone')}</option>
          </select>
        </label>
      </div>

      <div className="row">
        <label>
          <span>{t('integrations.relayUsername')}</span>
          <input value={username} onChange={(event) => setUsername(event.target.value)} autoComplete="off" />
        </label>
        <SecretField
          label={t('integrations.relayPassword')}
          present={settings.hasPassword}
          value={password}
          onChange={setPassword}
          onClear={() => setClearPassword(true)}
          cleared={clearPassword}
        />
      </div>

      <SaveRow busy={busy} saved={saved} problem={problem} />
    </form>
  )
}

function ObjectStoreForm({ settings, onSaved }: { settings: S3; onSaved: () => void }) {
  const { t } = useTranslation()
  const { busy, problem, saved, save } = useSaver(onSaved)

  const [enabled, setEnabled] = useState(settings.enabled)
  const [bucket, setBucket] = useState(settings.bucket)
  const [region, setRegion] = useState(settings.region)
  const [endpoint, setEndpoint] = useState(settings.endpoint ?? '')
  const [pathStyle, setPathStyle] = useState(settings.pathStyle)
  const [accessKeyId, setAccessKeyId] = useState(settings.accessKeyId ?? '')
  const [secret, setSecret] = useState('')
  const [clearSecret, setClearSecret] = useState(false)

  // Re-seeded when the saved settings change, so a save that the server
  // adjusted — trimming a value, say — shows what was actually stored.
  useEffect(() => {
    setEnabled(settings.enabled)
    setBucket(settings.bucket)
    setRegion(settings.region)
    setEndpoint(settings.endpoint ?? '')
    setPathStyle(settings.pathStyle)
    setAccessKeyId(settings.accessKeyId ?? '')
    setSecret('')
    setClearSecret(false)
  }, [settings])

  return (
    <form
      className="card"
      onSubmit={(event) => {
        event.preventDefault()
        void save({
          s3: {
            enabled,
            bucket,
            region,
            endpoint,
            pathStyle,
            accessKeyId,
            ...(clearSecret ? { secretAccessKey: '' } : secret ? { secretAccessKey: secret } : {}),
          },
        })
      }}
    >
      <h3>{t('integrations.objectStore')}</h3>
      <p className="muted" style={{ marginTop: 0 }}>
        {t('integrations.objectStoreDescription')}
      </p>

      <label>
        <input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />{' '}
        {t('integrations.enabled')}
      </label>

      <div className="row">
        <label>
          <span>{t('integrations.bucket')}</span>
          <input value={bucket} onChange={(event) => setBucket(event.target.value)} />
        </label>
        <label>
          <span>{t('integrations.region')}</span>
          <input value={region} onChange={(event) => setRegion(event.target.value)} />
        </label>
      </div>

      <label>
        <span>{t('integrations.endpoint')}</span>
        <input
          value={endpoint}
          placeholder={t('integrations.endpointPlaceholder')}
          onChange={(event) => setEndpoint(event.target.value)}
        />
        <span className="muted">{t('integrations.endpointHelp')}</span>
      </label>

      <label>
        <input
          type="checkbox"
          checked={pathStyle}
          onChange={(event) => setPathStyle(event.target.checked)}
        />{' '}
        {t('integrations.pathStyle')}
      </label>

      <div className="row">
        <label>
          <span>{t('integrations.accessKeyId')}</span>
          <input value={accessKeyId} onChange={(event) => setAccessKeyId(event.target.value)} />
        </label>
        <SecretField
          label={t('integrations.secretAccessKey')}
          present={settings.hasSecretAccessKey}
          value={secret}
          onChange={setSecret}
          onClear={() => setClearSecret(true)}
          cleared={clearSecret}
        />
      </div>
      <CredentialsNote
        file={settings.credentialsFile}
        hasKeys={Boolean(settings.accessKeyId) || settings.hasSecretAccessKey}
      />

      <SaveRow busy={busy} saved={saved} problem={problem} />
    </form>
  )
}

// Where the AWS credentials are coming from, when not from the two fields
// above.
//
// Empty key fields read as "nothing is configured", and on a deployment using
// a shared credentials file — or an instance role — that is wrong and alarming:
// the solver works, and filling the fields in would be the mistake. The API
// has always returned the file's path; nothing showed it.
function CredentialsNote({ file, hasKeys }: { file?: string; hasKeys: boolean }) {
  const { t } = useTranslation()

  if (hasKeys) {
    return null
  }
  return (
    <p className="muted field-hint">
      {file ? (
        <>
          {t('integrations.credentialsFromFile')} <span className="mono">{file}</span>
        </>
      ) : (
        t('integrations.credentialsFromEnvironment')
      )}
    </p>
  )
}

// CertificateForm is the one switch that decides whether a sender connecting
// to a domain is handed a certificate in that domain's name, or the server's.
function CertificateForm({ settings, onSaved }: { settings: Certificates; onSaved: () => void }) {
  const { t } = useTranslation()
  const { busy, problem, saved, save } = useSaver(onSaved)
  const [perDomain, setPerDomain] = useState(settings.perDomain)
  const [hosts, setHosts] = useState((settings.hosts ?? []).join(', '))
  const [acmeEnabled, setAcmeEnabled] = useState(settings.acmeEnabled)
  const [acmeEmail, setAcmeEmail] = useState(settings.acmeEmail ?? '')
  const [acmeDirectoryUrl, setAcmeDirectoryUrl] = useState(settings.acmeDirectoryUrl ?? '')
  const [acmeChallenge, setAcmeChallenge] = useState(settings.acmeChallenge ?? 'http-01')
  const [certificateFile, setCertificateFile] = useState(settings.certificateFile ?? '')
  const [privateKeyFile, setPrivateKeyFile] = useState(settings.privateKeyFile ?? '')

  useEffect(() => {
    setPerDomain(settings.perDomain)
    setHosts((settings.hosts ?? []).join(', '))
    setAcmeEnabled(settings.acmeEnabled)
    setAcmeEmail(settings.acmeEmail ?? '')
    setAcmeDirectoryUrl(settings.acmeDirectoryUrl ?? '')
    setAcmeChallenge(settings.acmeChallenge ?? 'http-01')
    setCertificateFile(settings.certificateFile ?? '')
    setPrivateKeyFile(settings.privateKeyFile ?? '')
  }, [settings])

  return (
    <form
      className="card"
      onSubmit={(event) => {
        event.preventDefault()
        void save({
          certificates: {
            perDomain,
            hosts: hosts
              .split(',')
              .map((entry) => entry.trim())
              .filter((entry) => entry !== ''),
            acmeEnabled,
            acmeEmail,
            acmeDirectoryUrl,
            acmeChallenge,
            certificateFile,
            privateKeyFile,
          },
        })
      }}
    >
      <h3>{t('integrations.certificatesTitle')}</h3>
      <p className="muted" style={{ marginTop: 0 }}>
        {t('integrations.certificatesIntro', { hosts: (settings.hosts ?? []).join(', ') })}
      </p>

      <div className="form-narrow">
        <label>
          <span>{t('serverSettings.certificateHosts')}</span>
          <input className="mono" value={hosts} onChange={(event) => setHosts(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.certificateHostsHint')}</p>
      </div>

      <label>
        <input type="checkbox" checked={perDomain} onChange={(event) => setPerDomain(event.target.checked)} />{' '}
        {t('integrations.certificatesPerDomain')}
      </label>
      <p className="muted field-hint">{t('integrations.certificatesPerDomainHint')}</p>

      <label>
        <input type="checkbox" checked={acmeEnabled} onChange={(event) => setAcmeEnabled(event.target.checked)} />{' '}
        {t('serverSettings.acmeEnabled')}
      </label>
      <p className="muted field-hint">{t('serverSettings.acmeEnabledHint')}</p>

      <div className="form-narrow">
        <label>
          <span>{t('serverSettings.acmeEmail')}</span>
          <input value={acmeEmail} onChange={(event) => setAcmeEmail(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.acmeEmailHint')}</p>

        <label>
          <span>{t('serverSettings.acmeChallenge')}</span>
          <select value={acmeChallenge} onChange={(event) => setAcmeChallenge(event.target.value)}>
            <option value="http-01">http-01</option>
            <option value="dns-01">dns-01</option>
          </select>
        </label>
        <p className="muted field-hint">{t('serverSettings.acmeChallengeHint')}</p>

        <label>
          <span>{t('serverSettings.acmeDirectoryUrl')}</span>
          <input
            className="mono"
            value={acmeDirectoryUrl}
            onChange={(event) => setAcmeDirectoryUrl(event.target.value)}
          />
        </label>
        <p className="muted field-hint">{t('serverSettings.acmeDirectoryUrlHint')}</p>

        <label>
          <span>{t('serverSettings.certificateFile')}</span>
          <input
            className="mono"
            value={certificateFile}
            onChange={(event) => setCertificateFile(event.target.value)}
          />
        </label>
        <p className="muted field-hint">{t('serverSettings.certificateFileHint')}</p>

        <label>
          <span>{t('serverSettings.privateKeyFile')}</span>
          <input className="mono" value={privateKeyFile} onChange={(event) => setPrivateKeyFile(event.target.value)} />
        </label>
        <p className="muted field-hint">{t('serverSettings.privateKeyFileHint')}</p>
      </div>

      {problem && <p className="error">{problem}</p>}
      <div className="page-actions">
        <button className="primary" type="submit" disabled={busy}>
          {t('common.save')}
        </button>
        {saved && <span className="muted">{t('integrations.savedNeedsRestart')}</span>}
      </div>
    </form>
  )
}

function Route53Form({ settings, onSaved }: { settings: Route53; onSaved: () => void }) {
  const { t } = useTranslation()
  const { busy, problem, saved, save } = useSaver(onSaved)

  const [enabled, setEnabled] = useState(settings.enabled)
  const [zoneId, setZoneId] = useState(settings.zoneId)
  const [region, setRegion] = useState(settings.region)
  const [accessKeyId, setAccessKeyId] = useState(settings.accessKeyId ?? '')
  const [secret, setSecret] = useState('')
  const [clearSecret, setClearSecret] = useState(false)

  useEffect(() => {
    setEnabled(settings.enabled)
    setZoneId(settings.zoneId)
    setRegion(settings.region)
    setAccessKeyId(settings.accessKeyId ?? '')
    setSecret('')
    setClearSecret(false)
  }, [settings])

  return (
    <form
      className="card"
      onSubmit={(event) => {
        event.preventDefault()
        void save({
          route53: {
            enabled,
            zoneId,
            region,
            accessKeyId,
            ...(clearSecret ? { secretAccessKey: '' } : secret ? { secretAccessKey: secret } : {}),
          },
        })
      }}
    >
      <h3>{t('integrations.route53')}</h3>
      <p className="muted" style={{ marginTop: 0 }}>
        {t('integrations.route53Description')}
      </p>

      <label>
        <input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />{' '}
        {t('integrations.enabled')}
      </label>

      <div className="row">
        <label>
          <span>{t('integrations.zoneId')}</span>
          <input value={zoneId} onChange={(event) => setZoneId(event.target.value)} />
        </label>
        <label>
          <span>{t('integrations.region')}</span>
          <input value={region} onChange={(event) => setRegion(event.target.value)} />
        </label>
      </div>

      <div className="row">
        <label>
          <span>{t('integrations.accessKeyId')}</span>
          <input value={accessKeyId} onChange={(event) => setAccessKeyId(event.target.value)} />
        </label>
        <SecretField
          label={t('integrations.secretAccessKey')}
          present={settings.hasSecretAccessKey}
          value={secret}
          onChange={setSecret}
          onClear={() => setClearSecret(true)}
          cleared={clearSecret}
        />
      </div>
      <CredentialsNote
        file={settings.credentialsFile}
        hasKeys={Boolean(settings.accessKeyId) || settings.hasSecretAccessKey}
      />

      <SaveRow busy={busy} saved={saved} problem={problem} />
    </form>
  )
}

// Where outgoing mail goes out through, when not out of this machine.
//
// It was configurable in the file and nowhere else, which meant the one
// deployment using it had a setting the dashboard could not show — and a
// setting nothing shows is one nobody remembers is there.
//
// The relay above answers the same question a different way: that hands the
// message to somebody else's mail server, and this carries this server's own
// SMTP conversation out through another address. Either is an answer to a
// blocked port 25; neither needs the other.
function ProxyForm({ settings, onSaved }: { settings: Proxy; onSaved: () => void }) {
  const { t } = useTranslation()
  const { busy, problem, saved, save } = useSaver(onSaved)

  const [socks5, setSocks5] = useState(settings.socks5 ?? '')

  useEffect(() => {
    setSocks5(settings.socks5 ?? '')
  }, [settings])

  return (
    <form
      className="card"
      onSubmit={(event) => {
        event.preventDefault()
        void save({ proxy: { socks5: socks5.trim() } })
      }}
    >
      <h3>{t('integrations.proxy')}</h3>
      <p className="muted" style={{ marginTop: 0 }}>
        {t('integrations.proxyDescription')}
      </p>

      <label>
        <span>{t('integrations.proxySocks5')}</span>
        <input
          value={socks5}
          placeholder="127.0.0.1:1080"
          onChange={(event) => setSocks5(event.target.value)}
        />
      </label>
      <p className="muted field-hint">{t('integrations.proxyHelp')}</p>

      <SaveRow busy={busy} saved={saved} problem={problem} />
    </form>
  )
}

function ServiceForm({
  title,
  description,
  settings,
  field,
  onSaved,
}: {
  title: string
  description: string
  settings: Service
  field: 'antivirus' | 'antispam'
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const { busy, problem, saved, save } = useSaver(onSaved)

  const [enabled, setEnabled] = useState(settings.enabled)
  const [host, setHost] = useState(settings.host)
  const [port, setPort] = useState(String(settings.port))

  useEffect(() => {
    setEnabled(settings.enabled)
    setHost(settings.host)
    setPort(String(settings.port))
  }, [settings])

  return (
    <form
      className="card"
      onSubmit={(event) => {
        event.preventDefault()
        void save({ [field]: { enabled, host, port: Number(port) || 0 } })
      }}
    >
      <h3>{title}</h3>
      <p className="muted" style={{ marginTop: 0 }}>
        {description}
      </p>

      <label>
        <input type="checkbox" checked={enabled} onChange={(event) => setEnabled(event.target.checked)} />{' '}
        {t('integrations.enabled')}
      </label>

      <div className="row">
        <label>
          <span>{t('integrations.host')}</span>
          <input value={host} onChange={(event) => setHost(event.target.value)} />
        </label>
        <label className="shrink">
          <span>{t('integrations.port')}</span>
          <input
            value={port}
            inputMode="numeric"
            onChange={(event) => setPort(event.target.value.replace(/[^0-9]/g, ''))}
          />
        </label>
      </div>

      <SaveRow busy={busy} saved={saved} problem={problem} />
    </form>
  )
}
