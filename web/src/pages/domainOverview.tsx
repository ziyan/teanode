import { useMemo } from 'react'
import { useParams } from 'react-router-dom'

import { Delivery, Domain, Mail, graphql } from '../api'
import { ErrorMessage, Loading } from '../components/common'
import { RelativeTime } from '../components/relativeTime'
import { useQuery } from '../components/useQuery'
import { MailIcon, WarningIcon } from '../components/icons'
import { Section, StatTile } from '../components/tiles'
import { Trans, useTranslation } from '../i18n/i18n'

// Everything the overview needs, in one round trip.
//
// The newest message comes back through the aggregation pipeline: sort by
// arrival, descending, take one. Asking the database for the newest row is a
// different thing from fetching five hundred and looking at the first, and on
// a domain with fifty thousand messages it is the difference between a page
// that loads and one that does not.
const OVERVIEW = `
  query ($domainId: String!) {
    CountMailsBy(domainId: $domainId, field: "status") {
      value
      count
    }
    ListMails(
      domainId: $domainId
      aggregations: [{ sort: [{ field: "receivedAt", direction: "descending" }] }]
      pagination: { first: 1 }
    ) {
      id
      receivedAt
    }
    ListPendingDeliveries(domainId: $domainId, pagination: { first: 500 }) {
      id
      status
      retryAt
    }
  }`

type Response = {
  CountMailsBy: { value: string; count: number }[]
  ListMails: Mail[]
  ListPendingDeliveries: Delivery[]
}

// The domain at a glance: is its DNS right, and is mail arriving. DNS first,
// because it is the question that decides whether the others mean anything —
// a domain whose MX record is wrong has no mail to count, and a row of zeroes
// explains itself only once you have looked at the record.
//
// The
// questions asked on arrival are those two and not "what is the TXT record",
// which is why every tile here is a number and none of them is a link to a
// page dressed up as a tile — the tab row above is what those were.
export function DomainOverviewTab({ domain }: { domain: Domain }) {
  const { t } = useTranslation()
  const { domainId } = useParams()
  const { data, error, loading } = useQuery(() => graphql<Response>(OVERVIEW, { domainId }), [domainId])

  const counts = useMemo(() => {
    const byStatus = new Map((data?.CountMailsBy ?? []).map((facet) => [facet.value, facet.count]))
    const total = [...byStatus.values()].reduce((sum, count) => sum + count, 0)
    return { byStatus, total }
  }, [data])

  if (loading && !data) {
    return <Loading />
  }
  if (error) {
    return <ErrorMessage error={error} />
  }

  const records = domain.records?.records ?? []
  const missing = records.filter((record) => !record.verified).length
  const newest = data?.ListMails?.[0]
  const pending = data?.ListPendingDeliveries ?? []
  // A delivery that has already been put off to a later attempt is one that
  // failed; one still on its first pass is simply in flight.
  const failing = pending.filter((delivery) => delivery.status === 'failed' || delivery.retryAt).length

  return (
    <>
      <Section icon={<MailIcon size={15} />} label={t('domainOverview.overview')}>
        <StatTile
          label={t('domainOverview.dns')}
          // Nothing to count until the first check has run, and a dash at
          // this size reads as a rule rather than as an absence — so the
          // detail line carries it instead.
          value={records.length === 0 ? <span className="tile-unknown">?</span> : missing === 0 ? records.length : missing}
          unit={
            records.length === 0
              ? undefined
              : missing === 0
                ? t('domainOverview.allPublished')
                : t('domainOverview.needChanging')
          }
          icon={missing > 0 ? <WarningIcon size={18} /> : undefined}
          detail={
            domain.records?.checkedAt ? (
              <Trans
                k="domainOverview.checked"
                nodes={{ time: <RelativeTime value={domain.records.checkedAt} /> }}
              />
            ) : (
              t('domainOverview.dnsNever')
            )
          }
          to={`/domains/${domainId}/settings`}
        />

        <StatTile
          label={t('domainOverview.messages')}
          value={counts.total}
          detail={
            newest?.receivedAt ? (
              <Trans
                k="domainOverview.lastReceived"
                nodes={{ time: <RelativeTime value={newest.receivedAt} /> }}
              />
            ) : (
              t('domainOverview.nothingReceived')
            )
          }
          to={`/mail?domain=${encodeURIComponent(domain.domain)}`}
        />

        <StatTile
          label={t('domainOverview.accepted')}
          value={counts.byStatus.get('accepted') ?? 0}
          to={`/mail?domain=${encodeURIComponent(domain.domain)}&status=accepted`}
          detail={t('domainOverview.deliveredOrForwarded')}
        />

        <StatTile
          label={t('domainOverview.rejected')}
          value={counts.byStatus.get('rejected') ?? 0}
          to={`/mail?domain=${encodeURIComponent(domain.domain)}&status=rejected`}
          detail={t('domainOverview.refused')}
        />

        <StatTile
          label={t('domainOverview.queued')}
          value={pending.length}
          // Queued is only worth noticing once something has failed at least
          // once; a delivery on its first attempt is the system working.
          icon={failing > 0 ? <WarningIcon size={18} /> : undefined}
          detail={
            pending.length === 0
              ? t('domainOverview.queueEmpty')
              : failing > 0
                ? t('domainOverview.queueFailing', { count: failing })
                : t('domainOverview.queueWaiting')
          }
          to={`/queue?domain=${encodeURIComponent(domain.domain)}`}
        />

      </Section>
    </>
  )
}
