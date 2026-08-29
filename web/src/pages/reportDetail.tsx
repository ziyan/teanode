import { useParams } from 'react-router-dom'

import { Feedback, FeedbackRecord, Report, graphql } from '../api'
import { ErrorMessage, Loading, Tag, formatTime, toneFor } from '../components/common'
import { useQuery } from '../components/useQuery'
import { useBreadcrumbDetail } from '../components/breadcrumb'
import { useTranslation } from '../i18n/i18n'

const REPORT = `
  query ($reportId: String!) {
    GetReport(reportId: $reportId) {
      id domainId beginAt endAt count ip rdns
      fromDomain senderDomain disposition dkimAligned spfAligned
      location { city country }
      feedback {
        organizationName email extraContactInfo reportId begin end errors
        domain dkimAlignment spfAlignment policy subdomainPolicy percent failureOptions
        records {
          sourceIp count disposition dkim spf reasonType reasonComment
          headerFrom envelopeFrom envelopeTo
          dkims { domain selector result humanResult }
          spfs { domain scope result }
        }
      }
    }
  }`

// One aggregate report, opened out. The list answers "is anybody forging me,
// and is it working"; this answers "what exactly did they see", which is the
// question anybody chasing a DMARC failure actually has and which the list
// could never fit in a row.
export function ReportDetailPage() {
  const { t, plural } = useTranslation()
  const { reportId } = useParams()
  const { data, error, loading } = useQuery(
    () => graphql<{ GetReport: Report | null }>(REPORT, { reportId }),
    [reportId],
  )

  const report = data?.GetReport
  useBreadcrumbDetail(report ? (report.fromDomain ?? t('reportDetail.unknownDomain')) : null)

  if (loading && !data) {
    return <Loading />
  }
  if (error) {
    return <ErrorMessage error={error} />
  }
  if (!report) {
    return <p className="muted">{t('common.notFound')}</p>
  }

  const feedback = report.feedback
  const aligned = report.dkimAligned || report.spfAligned

  return (
    <>
      {/* What this report says, in one line, before the mechanics of it. */}
      <div className="verdict">
        <Tag
          value={aligned ? t('reports.oneAligned') : t('reports.neitherAligned')}
          tone={aligned ? 'good' : 'bad'}
        />
        <Tag value={report.disposition || t('common.none')} tone={toneFor(report.disposition)} />
        <span className="muted">
          {plural(
            report.count ?? 0,
            { one: 'reportDetail.summaryOne', other: 'reportDetail.summaryOther' },
            { from: report.fromDomain ?? '—', ip: report.ip ?? '—' },
          )}
        </span>
      </div>

      <div className="card">
        <h3>{t('reportDetail.whoReported')}</h3>
        <table className="detail">
          <tbody>
            <Field label={t('reportDetail.reporter')}>
              {feedback?.organizationName}
              {feedback?.email ? <div className="muted">{feedback.email}</div> : null}
              {feedback?.extraContactInfo ? (
                <div className="muted wrap">{feedback.extraContactInfo}</div>
              ) : null}
            </Field>
            <Field label={t('reportDetail.reportId')} mono>
              {feedback?.reportId}
            </Field>
            <Field label={t('reports.period')}>
              {t('reportDetail.between', {
                from: formatTime(report.beginAt),
                to: formatTime(report.endAt),
              })}
            </Field>
            <Field label={t('reportDetail.source')}>
              <span className="mono">{report.ip}</span>
              {report.rdns ? <div className="muted">{report.rdns.replace(/\.$/, '')}</div> : null}
              {report.location?.country ? (
                <div className="muted">
                  {[report.location.city, report.location.country].filter(Boolean).join(', ')}
                </div>
              ) : null}
            </Field>
          </tbody>
        </table>
      </div>

      {/* The policy the reporter actually saw, which is not necessarily the
          one published now: a report covers a day that has already passed. */}
      {feedback && (
        <div className="card">
          <h3>{t('reportDetail.policySeen')}</h3>
          <table className="detail">
            <tbody>
              <Field label={t('reportDetail.forDomain')} mono>
                {feedback.domain}
              </Field>
              <Field label={t('reportDetail.policy')}>
                {[
                  feedback.policy ? `p=${feedback.policy}` : '',
                  feedback.subdomainPolicy ? `sp=${feedback.subdomainPolicy}` : '',
                  feedback.percent !== undefined && feedback.percent !== 100
                    ? `pct=${feedback.percent}`
                    : '',
                  feedback.failureOptions ? `fo=${feedback.failureOptions}` : '',
                ]
                  .filter(Boolean)
                  .join(' · ')}
              </Field>
              <Field label={t('reportDetail.alignment')}>
                {t('mailDetail.alignmentDkim', { mode: alignment(t, feedback.dkimAlignment) })}
                {', '}
                {t('mailDetail.alignmentSpf', { mode: alignment(t, feedback.spfAlignment) })}
              </Field>
            </tbody>
          </table>
          {feedback.errors?.length ? (
            <p className="error" style={{ marginBottom: 0 }}>
              {feedback.errors.join('; ')}
            </p>
          ) : null}
        </div>
      )}

      <div className="card">
        <h3>{t('reportDetail.whatTheySaw')}</h3>
        {!feedback?.records?.length ? (
          <p className="muted" style={{ margin: 0 }}>
            {t('reportDetail.noRecords')}
          </p>
        ) : (
          feedback.records.map((record, index) => <Record key={index} record={record} />)
        )}
      </div>
    </>
  )
}

// One row of the report: a batch of messages from one address, what the
// receiver decided, and why. The list page shows the first of these and calls
// it the report, which is right until a report carries more than one.
function Record({ record }: { record: FeedbackRecord }) {
  const { t, plural } = useTranslation()
  const passed = record.dkim === 'pass' || record.spf === 'pass'

  return (
    <div className="delivery">
      <div className="delivery-head">
        <Tag
          value={plural(record.count ?? 0, {
            one: 'reportDetail.messagesOne',
            other: 'reportDetail.messagesOther',
          })}
          tone={passed ? 'good' : 'bad'}
        />
        <span className="delivery-recipient mono">{record.sourceIp}</span>
        <Tag value={record.disposition || 'none'} tone={toneFor(record.disposition)} />
      </div>

      <table className="detail">
        <tbody>
          <Field label={t('reportDetail.evaluated')}>
            {[
              record.dkim ? `DKIM ${record.dkim}` : '',
              record.spf ? `SPF ${record.spf}` : '',
            ]
              .filter(Boolean)
              .join(' · ')}
          </Field>
          {/* Why the receiver did something other than the policy said. A
              forwarder and a mailing list both break alignment legitimately,
              and this is the only place that distinction is recorded. */}
          <Field label={t('reportDetail.override')}>
            {record.reasonType
              ? `${record.reasonType}${record.reasonComment ? ` — ${record.reasonComment}` : ''}`
              : undefined}
          </Field>
          <Field label={t('reportDetail.headerFrom')} mono>
            {record.headerFrom}
          </Field>
          <Field label={t('reportDetail.envelopeFrom')} mono>
            {record.envelopeFrom}
          </Field>
          <Field label={t('reportDetail.envelopeTo')} mono>
            {record.envelopeTo}
          </Field>
          {(record.dkims ?? []).map((dkim, index) => (
            <Field key={`dkim-${index}`} label={`DKIM ${dkim.result ?? ''}`}>
              <span className="mono">
                {dkim.selector && dkim.domain
                  ? `${dkim.selector}._domainkey.${dkim.domain}`
                  : dkim.domain}
              </span>
              {dkim.humanResult ? <div className="muted wrap">{dkim.humanResult}</div> : null}
            </Field>
          ))}
          {(record.spfs ?? []).map((spf, index) => (
            <Field key={`spf-${index}`} label={`SPF ${spf.result ?? ''}`} mono>
              {[spf.domain, spf.scope ? `(${spf.scope})` : ''].filter(Boolean).join(' ')}
            </Field>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function Field({ label, mono, children }: { label: string; mono?: boolean; children?: React.ReactNode }) {
  if (children === undefined || children === null || children === '' || children === false) {
    return null
  }
  return (
    <tr>
      <td className="shrink muted">{label}</td>
      <td className={mono ? 'mono wrap' : 'wrap'}>{children}</td>
    </tr>
  )
}

function alignment(t: (key: 'mailDetail.alignmentStrict' | 'mailDetail.alignmentRelaxed') => string, mode?: string): string {
  return mode === 's' ? t('mailDetail.alignmentStrict') : t('mailDetail.alignmentRelaxed')
}

export type { Feedback }
