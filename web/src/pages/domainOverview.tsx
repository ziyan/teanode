import { useMemo } from 'react'
import { useParams } from 'react-router-dom'

import { Delivery, Domain, Mail, graphql } from '../api'
import { ErrorMessage, Loading } from '../components/common'
import { RelativeTime } from '../components/relativeTime'
import { useQuery } from '../components/useQuery'
import { useBreadcrumbDetail } from '../components/breadcrumb'
import { DomainsIcon, GridIcon, MailIcon, QueueIcon, TemplateIcon, WarningIcon } from '../components/icons'
import { ResourceTile, Section, StatTile } from '../components/tiles'
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
    GetDomain(domainId: $domainId) {
      id domain subdomain comment dkimSelector hasDkimKey
      aliases { id }
      credentials { id }
      records { checkedAt error records { type name verified } }
    }
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
  GetDomain: Domain
  CountMailsBy: { value: string; count: number }[]
  ListMails: Mail[]
  ListPendingDeliveries: Delivery[]
}

// The domain at a glance: is its DNS right, is mail arriving, and where to go
// to change anything. The detail lives one click away, because the questions
// asked on arrival are these three and not "what is the TXT record".
export function DomainOverviewPage() {
  const { t } = useTranslation()
  const { domainId } = useParams()
  const { data, error, loading } = useQuery(() => graphql<Response>(OVERVIEW, { domainId }), [domainId])

  useBreadcrumbDetail(data?.GetDomain?.domain)

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

  const domain = data?.GetDomain
  if (!domain) {
    return <p className="muted">{t('common.notFound')}</p>
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
      </Section>

      <Section icon={<GridIcon size={15} />} label={t('domainOverview.resources')}>
        <ResourceTile
          icon={<MailIcon size={18} />}
          title={t('domainOverview.mail')}
          detail={t('domainOverview.mailDetail')}
          to={`/mail?domain=${encodeURIComponent(domain.domain)}`}
        />
        <ResourceTile
          icon={<QueueIcon size={18} />}
          title={t('domainOverview.queue')}
          detail={t('domainOverview.queueDetail')}
          to={`/queue?domain=${encodeURIComponent(domain.domain)}`}
        />
        <ResourceTile
          icon={<TemplateIcon size={18} />}
          title={t('templates.title')}
          detail={t('domainOverview.templatesDetail')}
          to={`/domains/${domainId}/templates`}
        />
        <ResourceTile
          icon={<DomainsIcon size={18} />}
          title={t('domainOverview.settings')}
          detail={t('domainOverview.settingsDetail')}
          to={`/domains/${domainId}/settings`}
        />
      </Section>
    </>
  )
}
