import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'

import { Domain, graphql } from '../api'
import { ErrorMessage, Loading, Tag } from '../components/common'
import { Column, DataTable } from '../components/dataTable'
import { useQuery } from '../components/useQuery'
import { FormDialog } from '../components/dialog'
import { useTranslation } from '../i18n/i18n'

const DOMAINS = `
  {
    ListDomains {
      id domain subdomain comment
      aliases { id }
      credentials { id }
      records { records { type verified } }
    }
  }`

const CREATE = `
  mutation ($domain: String!) {
    CreateDomain(domainParameters: { domain: $domain }) { id domain }
  }`

// A table, like the mail and the queue and the reports. It was a grid of
// cards, which reads as a set of choices; a list of domains with their state
// is the same shape as every other list here, and a reader who has learned to
// sort and filter one has learned all of them.
export function DomainsPage() {
  const { t, plural } = useTranslation()
  const { data, error, loading, reload } = useQuery(() => graphql<{ ListDomains: Domain[] }>(DOMAINS), [])
  const [adding, setAdding] = useState(false)
  const [domain, setDomain] = useState('')
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  async function create() {
    setBusy(true)
    setProblem(null)
    try {
      await graphql(CREATE, { domain })
      setDomain('')
      setAdding(false)
      await reload()
    } catch (caught) {
      setProblem(caught instanceof Error ? caught.message : t('domains.addFailed'))
    } finally {
      setBusy(false)
    }
  }

  const columns = useMemo<Column<Domain>[]>(
    () => [
      {
        key: 'domain',
        header: t('reports.domain'),
        filter: 'text',
        value: (entry) => entry.domain,
        sort: (first, second) => first.domain.localeCompare(second.domain),
        render: (entry) => (
          <>
            <Link to={`/domains/${entry.id}`}>{entry.domain}</Link>
            {entry.comment && <div className="muted">{entry.comment}</div>}
          </>
        ),
      },
      {
        key: 'dns',
        header: t('domains.dns'),
        width: '12rem',
        filter: 'select',
        value: (entry) => describeRecords(entry, t),
        render: (entry) => {
          const records = entry.records?.records ?? []
          const missing = records.filter((record) => !record.verified).length
          if (records.length === 0) {
            return <Tag value={t('domains.notChecked')} />
          }
          return missing === 0 ? (
            <Tag value={t('domains.allPublished')} tone="good" />
          ) : (
            <Tag value={t('domains.missing', { count: missing })} tone="warn" />
          )
        },
      },
      {
        key: 'aliases',
        header: t('domain.aliasesTitle'),
        width: '8rem',
        value: (entry) => String(entry.aliases.length),
        sort: (first, second) => first.aliases.length - second.aliases.length,
        // Filtered to the domain, so the number and the list behind it agree.
        render: (entry) => (
          <Link to={`/domains/${entry.id}`}>
            {plural(entry.aliases.length, { one: 'domains.aliasOne', other: 'domains.aliasOther' })}
          </Link>
        ),
      },
      {
        key: 'credentials',
        header: t('domain.credentialsTitle'),
        width: '9rem',
        optional: true,
        value: (entry) => String(entry.credentials.length),
        sort: (first, second) => first.credentials.length - second.credentials.length,
        render: (entry) => (
          <Link to={`/domains/${entry.id}`}>
            {plural(entry.credentials.length, {
              one: 'domains.credentialOne',
              other: 'domains.credentialOther',
            })}
          </Link>
        ),
      },
      {
        key: 'mail',
        header: t('nav.mail'),
        width: '7rem',
        optional: true,
        // Straight to the mail this domain received, which is the question
        // most often asked next.
        render: (entry) => <Link to={`/mail?domain=${encodeURIComponent(entry.domain)}`}>{t('domains.viewMail')}</Link>,
      },
    ],
    [t, plural],
  )

  const domains = data?.ListDomains ?? []

  return (
    <>
      {/* Behind a button rather than always on screen. Adding a domain is
          done a handful of times and then never again; the list is what
          somebody came here to read. */}
      <div className="page-actions">
        <span />
        <button className="primary" type="button" onClick={() => setAdding(true)}>
          {t('domains.newDomain')}
        </button>
      </div>

      {adding && (
        <FormDialog
          title={t('domains.newDomain')}
          submitLabel={t('domains.addButton')}
          busy={busy}
          error={problem}
          canSubmit={domain.length > 0}
          onClose={() => {
            setAdding(false)
            setProblem(null)
          }}
          onSubmit={() => void create()}
        >
          <label>
            <span>{t('domains.add')}</span>
            <input
              value={domain}
              onChange={(event) => setDomain(event.target.value)}
              placeholder="example.com"
            />
          </label>
        </FormDialog>
      )}

      {loading && !data && <Loading />}
      {error ? <ErrorMessage error={error} /> : null}
      {data && domains.length === 0 && !adding && <p className="muted">{t('domains.empty')}</p>}
      {domains.length > 0 && (
        <DataTable
          columns={columns}
          rows={domains}
          rowKey={(entry) => entry.id}
          rowLink={(entry) => `/domains/${entry.id}`}
          loading={loading}
          emptyMessage={t('domains.empty')}
          countLabel={(count) => plural(count, { one: 'domains.countOne', other: 'domains.countOther' })}
        />
      )}
    </>
  )
}

// The value the filter and the sort see: the same three states the tag shows,
// so narrowing to "not published" is narrowing to what the colour says.
function describeRecords(entry: Domain, t: ReturnType<typeof useTranslation>['t']): string {
  const records = entry.records?.records ?? []
  if (records.length === 0) {
    return t('domains.notChecked')
  }
  return records.every((record) => record.verified) ? t('domains.allPublished') : t('domains.notPublished')
}
