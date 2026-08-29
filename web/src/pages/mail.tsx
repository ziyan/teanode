import { useMemo } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { Domain, Mail, MailOpens, graphql } from '../api'
import {
  DomainLink,
  ErrorMessage,
  KindTag,
  Loading,
  Tag,
  formatBytes,
  toneFor,
  useEnumLabel,
} from '../components/common'
import { Column, DataTable, useFilterParams } from '../components/dataTable'
import { RelativeTime } from '../components/relativeTime'
import { useQuery } from '../components/useQuery'
import { Trans, useTranslation } from '../i18n/i18n'

const DOMAINS = `{ ListDomains { id domain } }`

// How many rows the list asks for. Filtering happens over these, so the
// count below the table says when it is showing a window rather than
// everything.
const MAIL_LIMIT = 500

const MAILS = `
  query ($domainId: String!, $pagination: PaginationInput) {
    ListMails(domainId: $domainId, pagination: $pagination) {
      id domainId sender from subject status kind size receivedAt
    }
  }`

// Asked separately, and after the list has arrived, because it is the only
// part of the page that depends on which rows there are — and because a list
// that waited for it would be slower for the majority of deployments, where
// no message carries a picture at all.
const OPENS = `
  query ($mailIds: [String!]!) {
    ListMailOpens(mailIds: $mailIds) {
      mailId trackable opened openedAt openCount
    }
  }`

export function MailPage() {
  const { t, plural } = useTranslation()
  const label = useEnumLabel()

  // A link can arrive already saying what it wants to see — the domain
  // overview sends you here narrowed to a domain, or to one status within it.
  const [search] = useSearchParams()
  const requestedFilters = useFilterParams(search)

  const domainQuery = useQuery(() => graphql<{ ListDomains: Domain[] }>(DOMAINS), [])
  const mails = useQuery(
    () => graphql<{ ListMails: Mail[] }>(MAILS, { domainId: '', pagination: { first: MAIL_LIMIT } }),
    [],
  )

  const rows = mails.data?.ListMails
  const opensQuery = useQuery(
    () =>
      rows && rows.length > 0
        ? graphql<{ ListMailOpens: MailOpens[] }>(OPENS, { mailIds: rows.map((mail) => mail.id) })
        : Promise.resolve({ ListMailOpens: [] }),
    [rows],
  )
  const opens = useMemo(
    () => new Map((opensQuery.data?.ListMailOpens ?? []).map((entry) => [entry.mailId ?? '', entry])),
    [opensQuery.data],
  )

  const domains = domainQuery.data?.ListDomains ?? []
  const domainNames = useMemo(
    () => new Map(domains.map((domain) => [domain.id, domain.domain])),
    [domains],
  )

  const columns = useMemo<Column<Mail>[]>(
    () => [
      {
        key: 'domain',
        header: t('mail.domain'),
        width: '10rem',
        filter: 'select',
        // From the configuration rather than from the rows: a domain with no
        // mail yet is still a sensible thing to ask about.
        options: domains.map((domain) => ({ value: domain.domain, label: domain.domain })),
        value: (mail) => domainNames.get(mail.domainId ?? ''),
        sort: (first, second) =>
          (domainNames.get(first.domainId ?? '') ?? '').localeCompare(domainNames.get(second.domainId ?? '') ?? ''),
        render: (mail) => <DomainLink domainId={mail.domainId} names={domainNames} />,
      },
      {
        key: 'from',
        header: t('mail.from'),
        width: '26%',
        filter: 'text',
        truncate: true,
        value: (mail) => mail.from || mail.sender || '',
        sort: (first, second) =>
          (first.from || first.sender || '').localeCompare(second.from || second.sender || ''),
      },
      {
        key: 'subject',
        header: t('mail.subject'),
        width: '40%',
        filter: 'text',
        truncate: true,
        value: (mail) => mail.subject ?? '',
        sort: (first, second) => (first.subject ?? '').localeCompare(second.subject ?? ''),
        render: (mail) => (
          <Link to={`/mail/${mail.id}`}>
            {mail.subject || <span className="muted">{t('mail.noSubject')}</span>}
          </Link>
        ),
      },
      {
        key: 'kind',
        header: t('mail.kind'),
        width: '8rem',
        optional: true,
        filter: 'select',
        value: (mail) => label.kind(mail.kind),
        sort: (first, second) => label.kind(first.kind).localeCompare(label.kind(second.kind)),
        render: (mail) => <KindTag value={mail.kind} />,
      },
      {
        key: 'status',
        header: t('mail.status'),
        width: '9rem',
        filter: 'select',
        value: (mail) => label.status(mail.status),
        sort: (first, second) => label.status(first.status).localeCompare(label.status(second.status)),
        render: (mail) => (
          <Tag value={label.status(mail.status) || t('common.none')} tone={toneFor(mail.status)} />
        ),
      },
      {
        key: 'size',
        header: t('mail.size'),
        width: '8rem',
        optional: true,
        value: (mail) => String(mail.size ?? 0),
        // Numerically. Sorting "1 KB" against "205 KB" as text puts 205
        // before 1, which is the classic way a size column lies.
        sort: (first, second) => (first.size ?? 0) - (second.size ?? 0),
        render: (mail) => formatBytes(mail.size),
      },
      {
        // Whether a picture in a sent message has been fetched. Blank for
        // everything that carries none — most of the list — because "not
        // fetched" beside a message with nothing to fetch is a statement
        // about nothing, and a column of it would be read as a column of
        // unread mail.
        key: 'opens',
        header: t('mail.opens'),
        width: '8rem',
        optional: true,
        filter: 'select',
        value: (mail) => {
          const entry = opens.get(mail.id)
          if (!entry?.trackable) {
            return ''
          }
          return entry.opened ? t('mail.opened') : t('mailDetail.notOpened')
        },
        sort: (first, second) => (opens.get(first.id)?.openCount ?? -1) - (opens.get(second.id)?.openCount ?? -1),
        render: (mail) => {
          const entry = opens.get(mail.id)
          if (!entry?.trackable) {
            return null
          }
          if (!entry.opened) {
            return <span className="muted">{t('mailDetail.notOpened')}</span>
          }
          return (
            <>
              <Tag value={t('mail.opened')} tone="good" />
              {(entry.openCount ?? 0) > 1 && (
                <span className="muted"> {t('mail.openedTimes', { count: entry.openCount ?? 0 })}</span>
              )}
            </>
          )
        },
      },
      {
        key: 'received',
        header: t('mail.received'),
        width: '11rem',
        value: (mail) => mail.receivedAt,
        sort: (first, second) => (first.receivedAt ?? '').localeCompare(second.receivedAt ?? ''),
        render: (mail) => <RelativeTime value={mail.receivedAt} />,
      },
    ],
    [t, domains, domainNames, opens],
  )

  if (domainQuery.loading || (mails.loading && !mails.data)) {
    return <Loading />
  }
  if (domainQuery.error) {
    return <ErrorMessage error={domainQuery.error} />
  }
  if (domains.length === 0) {
    return (
      <p className="muted">
        <Trans k="mail.noDomains" nodes={{ link: <Link to="/domains">{t('nav.domains')}</Link> }} />
      </p>
    )
  }

  return (
    <>
      {mails.error && <ErrorMessage error={mails.error} />}
      {/* Writing a message is the one thing to do here that is not reading
          the list, so it is the one action above it. */}
      <div className="page-actions">
        <span />
        <Link className="button primary" to="/mail/compose">
          {t('compose.new')}
        </Link>
      </div>
      <DataTable
        columns={columns}
        rows={mails.data?.ListMails ?? []}
        rowKey={(mail) => mail.id}
        rowLink={(mail) => `/mail/${mail.id}`}
        loading={mails.loading}
        emptyMessage={t('mail.empty')}
        initialFilters={requestedFilters}
        countLabel={(count, filtering) => {
          const total = mails.data?.ListMails.length ?? 0
          if (filtering) {
            return plural(count, { one: 'mail.countFilteredOne', other: 'mail.countFilteredOther' }, { total })
          }
          // The list is the most recent N, not everything the server holds,
          // and saying "500 messages" when there are fifty thousand would be
          // a plain untruth.
          if (total >= MAIL_LIMIT) {
            return t('mail.countCapped', { count: total })
          }
          return plural(count, { one: 'mail.countOne', other: 'mail.countOther' })
        }}
      />
    </>
  )
}
