import { useMemo } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { Delivery, Domain, graphql } from '../api'
import { DomainLink, ErrorMessage, KindTag, Loading, Tag, toneFor, useEnumLabel } from '../components/common'
import { Column, DataTable, useFilterParams } from '../components/dataTable'
import { RelativeTime } from '../components/relativeTime'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'

const DOMAINS = `{ ListDomains { id domain } }`

const PENDING = `
  query {
    ListPendingDeliveries(domainId: "", pagination: { first: 500 }) {
      id mailId domainId recipient kind status attempts error retryAt
    }
  }`

const RETRY = `
  mutation ($deliveryId: String!) {
    RetryDelivery(deliveryId: $deliveryId) { id status attempts }
  }`

// The queue answers one question: is anything stuck, and why. Delivered and
// dropped deliveries are not here; they are on the message they belong to.
export function QueuePage() {
  const { t, plural } = useTranslation()
  const label = useEnumLabel()

  // The domain overview links here already narrowed to one domain.
  const [search] = useSearchParams()
  const requestedFilters = useFilterParams(search)

  const { data, error, loading, reload } = useQuery(
    () => graphql<{ ListPendingDeliveries: Delivery[] }>(PENDING),
    [],
  )
  const domainQuery = useQuery(() => graphql<{ ListDomains: Domain[] }>(DOMAINS), [])
  const domains = domainQuery.data?.ListDomains ?? []
  const domainNames = useMemo(
    () => new Map(domains.map((domain) => [domain.id, domain.domain])),
    [domains],
  )

  const columns = useMemo<Column<Delivery>[]>(
    () => [
      {
        key: 'domain',
        header: t('mail.domain'),
        width: '12rem',
        optional: true,
        filter: 'select',
        // From the configuration rather than from the rows, so a domain with
        // nothing queued is still offered — and its absence is the answer.
        options: domains.map((domain) => ({ value: domain.domain, label: domain.domain })),
        value: (delivery) => domainNames.get(delivery.domainId ?? ''),
        sort: (first, second) =>
          (domainNames.get(first.domainId ?? '') ?? '').localeCompare(
            domainNames.get(second.domainId ?? '') ?? '',
          ),
        render: (delivery) => <DomainLink domainId={delivery.domainId} names={domainNames} />,
      },
      {
        key: 'recipient',
        header: t('mailDetail.recipient'),
        width: '28%',
        filter: 'text',
        truncate: true,
        value: (delivery) => delivery.recipient,
        sort: (first, second) => (first.recipient ?? '').localeCompare(second.recipient ?? ''),
        render: (delivery) =>
          delivery.mailId ? (
            <Link to={`/mail/${delivery.mailId}`}>{delivery.recipient}</Link>
          ) : (
            delivery.recipient
          ),
      },
      {
        key: 'kind',
        header: t('mail.kind'),
        width: '8rem',
        optional: true,
        filter: 'select',
        value: (delivery) => label.kind(delivery.kind),
        sort: (first, second) => label.kind(first.kind).localeCompare(label.kind(second.kind)),
        render: (delivery) => <KindTag value={delivery.kind} />,
      },
      {
        key: 'status',
        header: t('mail.status'),
        width: '9rem',
        filter: 'select',
        value: (delivery) => label.status(delivery.status),
        sort: (first, second) => label.status(first.status).localeCompare(label.status(second.status)),
        render: (delivery) => (
          <Tag value={label.status(delivery.status) || t('common.none')} tone={toneFor(delivery.status)} />
        ),
      },
      {
        key: 'attempts',
        header: t('mailDetail.attempts'),
        width: '6rem',
        value: (delivery) => String(delivery.attempts ?? 0),
        sort: (first, second) => (first.attempts ?? 0) - (second.attempts ?? 0),
      },
      {
        key: 'retry',
        header: t('queue.nextAttempt'),
        width: '9rem',
        optional: true,
        value: (delivery) => delivery.retryAt,
        sort: (first, second) => (first.retryAt ?? '').localeCompare(second.retryAt ?? ''),
        render: (delivery) => <RelativeTime value={delivery.retryAt} />,
      },
      {
        key: 'error',
        header: t('queue.lastError'),
        filter: 'text',
        truncate: true,
        value: (delivery) => delivery.error,
        render: (delivery) => <span className="muted">{delivery.error}</span>,
      },
      {
        key: 'actions',
        header: '',
        width: '7rem',
        render: (delivery) => (
          <button
            onClick={async () => {
              await graphql(RETRY, { deliveryId: delivery.id })
              await reload()
            }}
          >
            {t('queue.retryNow')}
          </button>
        ),
      },
    ],
    [t, reload, domains, domainNames],
  )

  if (loading && !data) {
    return <Loading />
  }
  if (error) {
    return <ErrorMessage error={error} />
  }

  return (
    <DataTable
      columns={columns}
      rows={data?.ListPendingDeliveries ?? []}
      rowKey={(delivery) => delivery.id}
      rowLink={(delivery) => (delivery.mailId ? `/mail/${delivery.mailId}` : undefined)}
      loading={loading}
      emptyMessage={t('queue.empty')}
      initialFilters={requestedFilters}
      countLabel={(count, filtering) =>
        filtering
          ? plural(
              count,
              { one: 'queue.countFilteredOne', other: 'queue.countFilteredOther' },
              { total: data?.ListPendingDeliveries.length ?? 0 },
            )
          : plural(count, { one: 'queue.countOne', other: 'queue.countOther' })
      }
    />
  )
}
