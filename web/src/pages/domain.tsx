import { useState } from 'react'
import { useParams } from 'react-router-dom'

import { Domain, graphql } from '../api'
import { ErrorMessage, Loading, Tag } from '../components/common'
import { ConfirmDialog } from '../components/dialog'
import { useQuery } from '../components/useQuery'
import { useBreadcrumbDetail } from '../components/breadcrumb'
import { useTranslation } from '../i18n/i18n'

const DOMAIN = `
  query ($domainId: String!) {
    GetDomain(domainId: $domainId) {
      id domain subdomain comment spamFilterScoreThreshold mailServers mailHosts linkHost linkHostname dkimSelector hasDkimKey
      aliases { id pattern comment kind email webhook disabled mailServer { host port username } }
      credentials { id comment alias disabled }
      records { checkedAt error records { type name expected priority optional found verified purpose } }
    }
  }`

const CREATE_ALIAS = `
  mutation ($domainId: String!, $pattern: String!, $kind: String!, $email: String, $webhook: String) {
    CreateAlias(domainId: $domainId, aliasParameters: { pattern: $pattern, kind: $kind, email: $email, webhook: $webhook }) {
      id
    }
  }`

const DELETE_ALIAS = `mutation ($aliasId: String!) { DeleteAlias(aliasId: $aliasId) }`
const CHECK = `mutation ($domainId: String!) { CheckDomain(domainId: $domainId) { id } }`
const UPDATE_MAIL_SERVERS = `
  mutation ($domainId: String!, $mailServers: [String]) {
    UpdateDomain(domainId: $domainId, domainParameters: { mailServers: $mailServers }) {
      id mailServers mailHosts
    }
  }`
const UPDATE_LINK_HOST = `
  mutation ($domainId: String!, $linkHost: String) {
    UpdateDomain(domainId: $domainId, domainParameters: { linkHost: $linkHost }) {
      id linkHost linkHostname
    }
  }`
const UPDATE_SELECTOR = `
  mutation ($domainId: String!, $selector: String!) {
    UpdateDomain(domainId: $domainId, domainParameters: { dkimSelector: $selector }) {
      id dkimSelector
    }
  }`
const REGENERATE_KEY = `
  mutation ($domainId: String!) {
    RegenerateDomainKey(domainId: $domainId) { id dkimSelector hasDkimKey }
  }`
const CREATE_CREDENTIAL = `
  mutation ($domainId: String!, $comment: String) {
    CreateCredential(domainId: $domainId, credentialParameters: { comment: $comment }) {
      username password host port
    }
  }`
const DELETE_CREDENTIAL = `mutation ($credentialId: String!) { DeleteCredential(credentialId: $credentialId) }`

type NewCredential = { username: string; password: string; host: string; port: string }

export function DomainPage() {
  const { t } = useTranslation()
  const { domainId } = useParams()
  const { data, error, loading, reload } = useQuery(() => graphql<{ GetDomain: Domain }>(DOMAIN, { domainId }), [domainId])

  const [pattern, setPattern] = useState('')
  const [kind, setKind] = useState('email')
  const [destination, setDestination] = useState('')
  const [comment, setComment] = useState('')
  const [created, setCreated] = useState<NewCredential | null>(null)
  const [replacing, setReplacing] = useState(false)
  // Null while the field is untouched, so it shows what is stored rather than
  // a copy taken before the domain had loaded.
  const [selector, setSelector] = useState<string | null>(null)
  const [movingSelector, setMovingSelector] = useState(false)
  // Null while untouched, so the field shows what is stored rather than a copy
  // taken before the domain had loaded.
  const [mailServers, setMailServers] = useState<string | null>(null)
  const [linkHost, setLinkHost] = useState<string | null>(null)
  const [problem, setProblem] = useState<string | null>(null)

  useBreadcrumbDetail(data?.GetDomain?.domain)

  async function run(work: () => Promise<unknown>) {
    setProblem(null)
    try {
      await work()
      await reload()
    } catch (caught) {
      setProblem(caught instanceof Error ? caught.message : t('domain.failed'))
    }
  }

  if (loading) {
    return <Loading />
  }
  if (error) {
    return <ErrorMessage error={error} />
  }
  const domain = data?.GetDomain
  if (!domain) {
    return <p className="muted">{t('common.notFound')}</p>
  }

  return (
    <>
      {/* No heading of its own. The page heading above the content already
          says where this is, and saying the domain again immediately below it
          read as the same word twice. */}
      {problem && <p className="error">{problem}</p>}

      <div className="card">
        <h3>{t('domain.dnsTitle')}</h3>
        <p className="muted" style={{ marginTop: 0 }}>
          {t('domain.dnsIntro')}
        </p>
        <table>
          <thead>
            <tr>
              <th>{t('domain.type')}</th>
              <th>{t('domain.name')}</th>
              <th>{t('domain.requiredValue')}</th>
              <th>{t('domain.publishedNow')}</th>
              <th>{t('domain.state')}</th>
            </tr>
          </thead>
          <tbody>
            {(domain.records?.records ?? []).map((record, index) => (
              <tr key={index}>
                <td className="shrink">{record.type}</td>
                <td className="mono wrap record-name">{record.name}</td>
                <td className="mono wrap">
                  {/* An MX value is the preference and the host together, so
                      it can be copied into a zone as it stands. */}
                  {record.priority ? `${record.priority} ${record.expected}` : record.expected}
                  {!record.verified && <div className="muted cell-note">{record.purpose}</div>}
                </td>
                <td className="mono wrap">
                  {record.verified ? (
                    <span className="muted">{t('domain.matches')}</span>
                  ) : record.found && record.found.length > 0 ? (
                    // Showing what is actually published is the difference
                    // between "something is wrong" and knowing what to edit.
                    record.found.map((value, position) => <div key={position}>{value}</div>)
                  ) : (
                    <span className="muted">{t('domain.nothingPublished')}</span>
                  )}
                </td>
                <td className="shrink">
                  <Tag
                    value={
                      record.verified
                        ? t('domain.ok')
                        : record.optional
                          ? t('domain.optional')
                          : t('domain.change')
                    }
                    tone={record.verified ? 'good' : record.optional ? undefined : 'warn'}
                  />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        <p style={{ marginBottom: 0 }}>
          <button onClick={() => void run(() => graphql(CHECK, { domainId }))}>{t('domain.checkAgain')}</button>
        </p>
      </div>

      <div className="card">
        <h3>{t('domain.mailServersTitle')}</h3>
        <p className="muted" style={{ marginTop: 0 }}>
          {t('domain.mailServersIntro')}
        </p>
        {/* A short host name in a field as wide as a table of DNS records
            reads as a mistake. The cards here are full width because they
            hold those tables; the fields inside them are not. */}
        <div className="form-narrow">
          <label>
            <span>{t('domain.mailServersLabel')}</span>
            <input
              className="mono"
              value={mailServers ?? (domain.mailServers ?? []).join(', ')}
              placeholder={(domain.mailHosts ?? []).join(', ')}
              spellCheck={false}
              autoComplete="off"
              onChange={(event) => setMailServers(event.target.value)}
            />
          </label>
          <p className="muted field-hint">
            {t('domain.mailServersHint', { names: (domain.mailHosts ?? []).join(', ') })}
          </p>
        </div>
        <div className="page-actions">
          <button
            disabled={mailServers === null || mailServers === (domain.mailServers ?? []).join(', ')}
            onClick={() => {
              const chosen = (mailServers ?? '')
                .split(',')
                .map((name) => name.trim())
                .filter((name) => name !== '')
              void run(async () => {
                await graphql(UPDATE_MAIL_SERVERS, { domainId, mailServers: chosen })
                // What to publish has changed, so what the page says about DNS
                // is out of date until it is checked again.
                await graphql(CHECK, { domainId })
                setMailServers(null)
              })
            }}
          >
            {t('domain.mailServersSave')}
          </button>
        </div>
      </div>

      {/* Where a picture in a message is fetched from. It looks like a
          detail of DNS and is not: get it wrong and the mail is fine while
          every picture in it is broken, which is a failure nobody sees from
          here — it happens in the reader's mail program. */}
      <div className="card">
        <h3>{t('domain.linkHostTitle')}</h3>
        <p className="muted" style={{ marginTop: 0 }}>
          {t('domain.linkHostIntro')}
        </p>
        <div className="form-narrow">
          <label>
            <span>{t('domain.linkHostLabel')}</span>
            <input
              className="mono"
              value={linkHost ?? domain.linkHost ?? ''}
              placeholder={domain.linkHostname ?? ''}
              spellCheck={false}
              autoComplete="off"
              onChange={(event) => setLinkHost(event.target.value)}
            />
          </label>
          <p className="muted field-hint">
            {t('domain.linkHostHint', { name: domain.linkHostname ?? '' })}
          </p>
        </div>
        <div className="page-actions">
          <button
            disabled={linkHost === null || linkHost.trim() === (domain.linkHost ?? '')}
            onClick={() => {
              void run(async () => {
                await graphql(UPDATE_LINK_HOST, { domainId, linkHost: (linkHost ?? '').trim() })
                setLinkHost(null)
              })
            }}
          >
            {t('domain.linkHostSave')}
          </button>
        </div>
      </div>

      <div className="card">
        <h3>{t('domain.keyTitle')}</h3>
        {domain.hasDkimKey ? (
          <p className="muted" style={{ marginTop: 0 }}>
            {t('domain.keyPresent', { selector: domain.dkimSelector ?? '' })}
          </p>
        ) : (
          <p className="error" style={{ marginTop: 0 }}>
            {t('domain.keyMissing')}
          </p>
        )}

        {/* The selector is a DNS label, and now that every domain publishes
            its own key at its own name it is this domain's to choose: a
            second one is how a key is rotated without a gap, by publishing
            the new record before anything signs under it. */}
        <div className="form-narrow">
          <label>
            <span>{t('domain.selectorLabel')}</span>
            <input
              className="mono"
              value={selector ?? domain.dkimSelector ?? ''}
              maxLength={63}
              spellCheck={false}
              autoComplete="off"
              onChange={(event) => setSelector(event.target.value)}
            />
          </label>
          <p className="muted field-hint">
            {t('domain.selectorHint', {
              name: `${(selector ?? domain.dkimSelector ?? '').trim()}._domainkey.${domain.domain}`,
            })}
          </p>
        </div>

        <div className="page-actions">
          <button
            disabled={
              (selector ?? domain.dkimSelector ?? '') === (domain.dkimSelector ?? '') ||
              (selector ?? '').trim() === ''
            }
            onClick={() => setMovingSelector(true)}
          >
            {t('domain.selectorSave')}
          </button>
          <button
            onClick={() => {
              if (domain.hasDkimKey) {
                setReplacing(true)
                return
              }
              void run(() => graphql(REGENERATE_KEY, { domainId }))
            }}
          >
            {domain.hasDkimKey ? t('domain.keyReplace') : t('domain.keyGenerate')}
          </button>
        </div>
      </div>

      {movingSelector && (
        <ConfirmDialog
          title={t('domain.selectorMoveTitle')}
          body={t('domain.selectorConfirm', {
            name: `${(selector ?? '').trim()}._domainkey.${domain.domain}`,
          })}
          confirmLabel={t('domain.selectorSave')}
          onConfirm={() => {
            setMovingSelector(false)
            const chosen = (selector ?? '').trim()
            void run(async () => {
              await graphql(UPDATE_SELECTOR, { domainId, selector: chosen })
              // The record to publish has moved, so what the page shows about
              // DNS is out of date until it is checked again.
              await graphql(CHECK, { domainId })
              setSelector(null)
            })
          }}
          onClose={() => setMovingSelector(false)}
        />
      )}

      {replacing && (
        <ConfirmDialog
          title={t('domain.keyReplaceTitle')}
          body={t('domain.keyConfirm')}
          confirmLabel={t('domain.keyReplace')}
          onConfirm={() => {
            setReplacing(false)
            void run(() => graphql(REGENERATE_KEY, { domainId }))
          }}
          onClose={() => setReplacing(false)}
        />
      )}

      <div className="card">
        <h3>{t('domain.aliasesTitle')}</h3>
        <p className="muted" style={{ marginTop: 0 }}>
          {t('domain.aliasesIntro')}
        </p>
        <table>
          <thead>
            <tr>
              <th>{t('domain.pattern')}</th>
              <th>{t('domain.goesTo')}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {domain.aliases.map((alias) => (
              <tr key={alias.id}>
                <td className="mono">
                  {alias.pattern || <span className="muted">{t('domain.catchAll')}</span>}
                  {alias.disabled && ' '}
                  {alias.disabled && <Tag value={t('domain.disabled')} />}
                </td>
                <td>
                  {alias.kind === 'email' && alias.email}
                  {alias.kind === 'webhook' && <span className="mono">{alias.webhook}</span>}
                  {alias.kind === 'mailServer' && alias.mailServer && (
                    <span className="mono">
                      {alias.mailServer.host}:{alias.mailServer.port}
                    </span>
                  )}
                  {alias.kind === 'null' && <span className="muted">{t('domain.discarded')}</span>}
                </td>
                <td className="shrink">
                  <button onClick={() => void run(() => graphql(DELETE_ALIAS, { aliasId: alias.id }))}>
                    {t('common.remove')}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>

        <form
          className="row"
          style={{ marginTop: 12 }}
          onSubmit={(event) => {
            event.preventDefault()
            void run(async () => {
              await graphql(CREATE_ALIAS, {
                domainId,
                pattern,
                kind,
                email: kind === 'email' ? destination : null,
                webhook: kind === 'webhook' ? destination : null,
              })
              setPattern('')
              setDestination('')
            })
          }}
        >
          <label style={{ margin: 0 }}>
            <span>{t('domain.pattern')}</span>
            {/* Left blank it is a catch-all, which is not guessable from an
                empty box, so the placeholder says so. */}
            <input
              value={pattern}
              onChange={(event) => setPattern(event.target.value)}
              placeholder={t('domain.patternPlaceholder')}
            />
          </label>
          <label style={{ margin: 0, maxWidth: 140 }}>
            <span>{t('domain.kind')}</span>
            <select value={kind} onChange={(event) => setKind(event.target.value)}>
              <option value="email">{t('domain.kindEmail')}</option>
              <option value="webhook">{t('domain.kindWebhook')}</option>
              <option value="null">{t('domain.kindDiscard')}</option>
            </select>
          </label>
          {kind !== 'null' && (
            <label style={{ margin: 0 }}>
              <span>{kind === 'email' ? t('domain.forwardTo') : t('domain.postTo')}</span>
              <input value={destination} onChange={(event) => setDestination(event.target.value)} />
            </label>
          )}
          <div className="shrink">
            <button className="primary" type="submit">
              {t('domain.addAlias')}
            </button>
          </div>
        </form>
      </div>

      <div className="card">
        <h3>{t('domain.credentialsTitle')}</h3>
        <p className="muted" style={{ marginTop: 0 }}>
          {t('domain.credentialsIntro', { domain: domain.domain })}
        </p>
        {created && (
          <div className="banner">
            <strong>{t('domain.credentialSaveNow')}</strong>
            <table>
              <tbody>
                <tr>
                  <td className="shrink">{t('domain.credentialServer')}</td>
                  <td className="mono">
                    {t('domain.credentialServerValue', { host: created.host, port: created.port })}
                  </td>
                </tr>
                <tr>
                  <td className="shrink">{t('domain.credentialUsername')}</td>
                  <td className="mono">{created.username}</td>
                </tr>
                <tr>
                  <td className="shrink">{t('domain.credentialPassword')}</td>
                  <td className="mono">{created.password}</td>
                </tr>
              </tbody>
            </table>
            <button className="link" onClick={() => setCreated(null)}>
              {t('common.done')}
            </button>
          </div>
        )}
        <table>
          <tbody>
            {domain.credentials.map((credential) => (
              <tr key={credential.id}>
                <td>{credential.comment || <span className="muted">{t('domain.credentialNoNote')}</span>}</td>
                <td className="mono muted">{credential.id}</td>
                <td className="shrink">
                  <button onClick={() => void run(() => graphql(DELETE_CREDENTIAL, { credentialId: credential.id }))}>
                    {t('common.remove')}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        <form
          className="row"
          style={{ marginTop: 12 }}
          onSubmit={(event) => {
            event.preventDefault()
            void run(async () => {
              const result = await graphql<{ CreateCredential: NewCredential }>(CREATE_CREDENTIAL, {
                domainId,
                comment,
              })
              setCreated(result.CreateCredential)
              setComment('')
            })
          }}
        >
          <label style={{ margin: 0 }}>
            <span>{t('domain.credentialNote')}</span>
            <input value={comment} onChange={(event) => setComment(event.target.value)} placeholder="laptop" />
          </label>
          <div className="shrink">
            <button className="primary" type="submit">
              {t('domain.createCredential')}
            </button>
          </div>
        </form>
      </div>
    </>
  )
}
