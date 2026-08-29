import { useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'

import { Domain, Report, graphql } from '../api'
import { DomainLink, ErrorMessage, Loading, Tag, toneFor } from '../components/common'
import { Column, DataTable, useFilterParams } from '../components/dataTable'
import { RelativeTime } from '../components/relativeTime'
import { useQuery } from '../components/useQuery'
import { Trans, useTranslation } from '../i18n/i18n'

const DOMAINS = `{ ListDomains { id domain } }`

const REPORTS = `
  query ($pagination: PaginationInput) {
    ListReports(domainId: "", pagination: $pagination) {
      id domainId beginAt endAt count ip rdns
      fromDomain senderDomain disposition dkimAligned spfAligned
    }
  }`

const REPORT_LIMIT = 500

// Aggregate reports are other people telling you what they did with mail that
// claimed to be from your domains. Almost all of it is somebody forging you:
// mail you actually sent aligns, and mail that aligns is not interesting.
//
// So the page is built around the question worth asking — who is sending as me,
// and is anyone believing them — rather than around the shape of the XML.
export function ReportsPage() {
  const { t, plural } = useTranslation()

  const [search] = useSearchParams()
  const requestedFilters = useFilterParams(search)

  const reports = useQuery(
    () => graphql<{ ListReports: Report[] }>(REPORTS, { pagination: { first: REPORT_LIMIT } }),
    [],
  )
  const domainQuery = useQuery(() => graphql<{ ListDomains: Domain[] }>(DOMAINS), [])
  const domains = domainQuery.data?.ListDomains ?? []
  const domainNames = useMemo(
    () => new Map(domains.map((domain) => [domain.id, domain.domain])),
    [domains],
  )

  const columns = useMemo<Column<Report>[]>(
    () => [
      {
        key: 'domain',
        header: t('reports.domain'),
        width: '9rem',
        filter: 'select',
        options: domains.map((domain) => ({ value: domain.domain, label: domain.domain })),
        value: (report) => domainNames.get(report.domainId ?? ''),
        sort: (first, second) =>
          (domainNames.get(first.domainId ?? '') ?? '').localeCompare(
            domainNames.get(second.domainId ?? '') ?? '',
          ),
        render: (report) => <DomainLink domainId={report.domainId} names={domainNames} />,
      },
      {
        key: 'from',
        header: t('reports.claimedFrom'),
        width: '11rem',
        filter: 'text',
        truncate: true,
        value: (report) => report.fromDomain ?? '',
        sort: (first, second) => (first.fromDomain ?? '').localeCompare(second.fromDomain ?? ''),
      },
      {
        key: 'source',
        header: t('reports.sentBy'),
        // Not truncated: the address and the name it resolves to are together
        // the whole identification of whoever sent this, and half a hostname
        // identifies nobody.
        filter: 'text',
        value: (report) => `${report.ip ?? ''} ${report.rdns ?? ''}`,
        sort: (first, second) => (first.ip ?? '').localeCompare(second.ip ?? ''),
        // The address is what identifies the sender; the name it claims is
        // context, and often absent.
        render: (report) => (
          <>
            <div className="mono">{report.ip}</div>
            {report.rdns && (
              <div className="muted wrap">{report.rdns.replace(/\.$/, '')}</div>
            )}
          </>
        ),
      },
      {
        key: 'authentication',
        header: t('reports.authentication'),
        width: '11rem',
        filter: 'select',
        // Aligned on either count is a pass under DMARC, so the three states
        // are what a reader needs, not two booleans to combine themselves.
        value: (report) =>
          report.dkimAligned && report.spfAligned
            ? t('reports.bothAligned')
            : report.dkimAligned || report.spfAligned
              ? t('reports.oneAligned')
              : t('reports.neitherAligned'),
        render: (report) => {
          const passed = report.dkimAligned || report.spfAligned
          return (
            <Tag
              value={
                report.dkimAligned && report.spfAligned
                  ? t('reports.bothAligned')
                  : passed
                    ? t('reports.oneAligned')
                    : t('reports.neitherAligned')
              }
              tone={passed ? 'good' : 'bad'}
            />
          )
        },
      },
      {
        key: 'disposition',
        header: t('reports.disposition'),
        width: '8rem',
        filter: 'select',
        value: (report) => report.disposition ?? '',
        render: (report) => (
          <Tag value={report.disposition ?? t('common.none')} tone={toneFor(report.disposition)} />
        ),
      },
      {
        key: 'count',
        header: t('reports.messages'),
        width: '7rem',
        value: (report) => String(report.count ?? 0),
        sort: (first, second) => (first.count ?? 0) - (second.count ?? 0),
      },
      {
        key: 'period',
        header: t('reports.period'),
        width: '10rem',
        optional: true,
        value: (report) => report.beginAt,
        sort: (first, second) => (first.beginAt ?? '').localeCompare(second.beginAt ?? ''),
        render: (report) => <RelativeTime value={report.endAt ?? report.beginAt} />,
      },
    ],
    [t, domains, domainNames],
  )

  if (domainQuery.loading || (reports.loading && !reports.data)) {
    return <Loading />
  }
  if (domainQuery.error) {
    return <ErrorMessage error={domainQuery.error} />
  }

  const rows = reports.data?.ListReports ?? []

  return (
    <>
      {reports.error && <ErrorMessage error={reports.error} />}
      {rows.length === 0 ? (
        <div className="table-surface" style={{ padding: '2rem' }}>
          <p className="muted" style={{ margin: 0, maxWidth: '54ch' }}>
            <Trans k="reports.emptyExplained" nodes={{}} />
          </p>
        </div>
      ) : (
        <DataTable
          columns={columns}
          rows={rows}
          rowKey={(report) => report.id}
          rowLink={(report) => `/reports/${report.id}`}
          loading={reports.loading}
          emptyMessage={t('reports.empty')}
          initialFilters={requestedFilters}
          countLabel={(count, filtering) =>
            filtering
              ? plural(
                  count,
                  { one: 'reports.countFilteredOne', other: 'reports.countFilteredOther' },
                  { total: rows.length },
                )
              : plural(count, { one: 'reports.countOne', other: 'reports.countOther' })
          }
        />
      )}
    </>
  )
}
