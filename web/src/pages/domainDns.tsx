import { useRef, useState } from 'react'
import { useParams } from 'react-router-dom'

import { graphql } from '../api'
import { CopyIconButton, ErrorMessage, Tag } from '../components/common'
import { RelativeTime } from '../components/relativeTime'
import { SenderLogo } from '../components/senderLogo'
import { ConfirmDialog } from '../components/dialog'
import { useTranslation } from '../i18n/i18n'
import { DomainTabProps } from './domainTabs'
import { Tooltip } from '../components/tooltip'

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

// What this domain publishes and how it is identified: the records to put in
// the zone, the names the MX records point at, the host a reader's mail
// program fetches pictures from, and the key that signs outgoing mail.
//
// Four subjects rather than four tabs. Each is a card of a few fields, and a
// tab holding one field is a tab nobody would look in.
// What to publish, as one string: an MX record's value is its preference and
// its host together, and that is what belongs on the screen, in the tooltip
// and on the clipboard alike.
function expectedValue(record: { priority?: number | null; expected?: string | null }): string {
  return record.priority ? `${record.priority} ${record.expected ?? ''}` : (record.expected ?? '')
}

export function DomainDnsTab({ domain, run }: DomainTabProps) {
  const { t } = useTranslation()
  const { domainId } = useParams()

  // Null while the field is untouched, so it shows what is stored rather than
  // a copy taken before the domain had loaded.
  const [selector, setSelector] = useState<string | null>(null)
  const [movingSelector, setMovingSelector] = useState(false)
  const [mailServers, setMailServers] = useState<string | null>(null)
  const [linkHost, setLinkHost] = useState<string | null>(null)
  const [replacing, setReplacing] = useState(false)

  return (
    <>
      <div className="card">
        <h3>{t('domain.dnsTitle')}</h3>
        <p className="muted">{t('domain.dnsIntro')}</p>
        <table className="dns-table">
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
                <td className="mono wrap">{record.name}</td>
                <td className="mono">
                  {/* Clamped to its line, with the whole of it a button away.
                      A DKIM public key is four hundred characters that say
                      nothing to a reader checking by eye, and printing them
                      took four lines of the table to say it. What somebody
                      does with this value is paste it into a zone, which is
                      what the button is for; hovering says the rest. */}
                  <div className="value-row">
                    {/* An MX value is the preference and the host together,
                        so it can be copied into a zone as it stands. */}
                    <Tooltip label={expectedValue(record)}>
                      <span className="value-clamp">{expectedValue(record)}</span>
                    </Tooltip>
                    <CopyIconButton value={expectedValue(record)} />
                  </div>
                  {!record.verified && <div className="muted cell-note">{record.purpose}</div>}
                  {/* What is stopping it, when something is. Said whether or
                      not the record is published: publishing one and waiting
                      to see what happens is how somebody spends a week
                      finding this out. */}
                  {record.blocked && <div className="cell-note warn">{record.blocked}</div>}
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
                      record.verified ? t('domain.ok') : record.optional ? t('domain.optional') : t('domain.change')
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

      <DomainLogoCard domain={domain} domainId={domainId ?? ''} run={run} />

      <div className="card">
        <h3>{t('domain.mailServersTitle')}</h3>
        <p className="muted">{t('domain.mailServersIntro')}</p>
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
        <p className="muted">{t('domain.linkHostIntro')}</p>
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
          <p className="muted field-hint">{t('domain.linkHostHint', { name: domain.linkHostname ?? '' })}</p>
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
          <p className="muted">{t('domain.keyPresent', { selector: domain.dkimSelector ?? '' })}</p>
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
              (selector ?? domain.dkimSelector ?? '') === (domain.dkimSelector ?? '') || (selector ?? '').trim() === ''
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
    </>
  )
}

// The mark this server publishes for the domain, which is what the BIMI
// record above points at.
//
// Hosting it here is the point of the card. A BIMI record names an address
// over HTTPS, and somebody running a mail server and no web server has
// nowhere to put a file — so the record the page offers names one this server
// serves, and uploading is the whole of that step.
//
// The file is checked before it is stored, against the profile a mark has to
// satisfy. A receiver that dislikes a file says nothing to anybody: the mark
// never appears and the sender never learns why. So the message here names
// what is wrong with it.
function DomainLogoCard({
  domain,
  domainId,
  run,
}: {
  domain: DomainTabProps['domain']
  domainId: string
  run: DomainTabProps['run']
}) {
  const { t } = useTranslation()
  const [busy, setBusy] = useState(false)
  const [refused, setRefused] = useState<string | null>(null)
  const picker = useRef<HTMLInputElement>(null)
  const logo = domain.logo

  const upload = async (file: File) => {
    setBusy(true)
    setRefused(null)
    try {
      const form = new FormData()
      form.append('file', file, file.name)
      const answer = await fetch(`/api/v1/domains/${encodeURIComponent(domainId)}/logo`, {
        method: 'POST',
        credentials: 'include',
        body: form,
      })
      const body = await answer.json().catch(() => null)
      if (!answer.ok) {
        setRefused(body?.error ?? t('domain.logoRefused'))
        return
      }
      // The records are computed on a schedule, so the row above would go on
      // saying there is no logo until the next sweep. Ask for a check now, so
      // the value to copy is the one that was just uploaded.
      await run(() => graphql(CHECK, { domainId }))
    } catch (caught) {
      setRefused(caught instanceof Error ? caught.message : t('domain.logoRefused'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="card">
      <h3>{t('domain.logoTitle')}</h3>
      <p className="muted">{t('domain.logoIntro')}</p>

      {logo ? (
        <div className="domain-logo">
          {/* Drawn at both the sizes a reader sees it: beside a row of a
              list, and beside the message itself. */}
          {/* The file this server publishes, drawn from where it publishes
              it — not from the cache of marks fetched from other people's
              domains, which is what a sending domain's name would read. */}
          <SenderLogo name={logo.title || domain.domain} src={logo.url} size={32} />
          <SenderLogo name={logo.title || domain.domain} src={logo.url} size={20} />
          <div className="domain-logo-about">
            <div>{logo.filename}</div>
            <div className="muted">{t('domain.logoTitleIs', { title: logo.title })}</div>
            <div className="muted">
              {t('domain.logoUploaded')} <RelativeTime value={logo.uploadedAt} />
            </div>
          </div>
        </div>
      ) : (
        <p className="muted">{t('domain.logoNone')}</p>
      )}

      <ErrorMessage error={refused} />

      <div className="page-actions">
        <input
          ref={picker}
          type="file"
          accept="image/svg+xml,.svg"
          hidden
          onChange={(event) => {
            const file = event.target.files?.[0]
            event.target.value = ''
            if (file) {
              void upload(file)
            }
          }}
        />
        <button disabled={busy} onClick={() => picker.current?.click()}>
          {logo ? t('domain.logoReplace') : t('domain.logoUpload')}
        </button>
      </div>

      {/* Said before anybody starts, not after. The difference between "this
          works" and "this works everywhere except the two receivers that
          matter most to you" is a certificate costing about a thousand
          dollars a year, and learning that after commissioning artwork is
          learning it too late. */}
      <p className="muted">{t('domain.logoCertificate')}</p>
    </div>
  )
}
