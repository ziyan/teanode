import { useState } from 'react'
import { Link, Navigate, useNavigate, useParams } from 'react-router-dom'

import { Domain, graphql } from '../api'
import { ErrorMessage, Loading } from '../components/common'
import { useQuery } from '../components/useQuery'
import { useBreadcrumbDetail } from '../components/breadcrumb'
import { Key, useTranslation } from '../i18n/i18n'
import { DomainOverviewTab } from './domainOverview'
import { DomainDnsTab } from './domainDns'
import { DomainAliasesTab } from './domainAliases'
import { DomainCredentialsTab } from './domainCredentials'
import { TemplatesTab } from './templates'

// Everything about one domain, in one place with tabs across the top.
//
// It was an overview whose bottom half was four large tiles that only linked
// somewhere else, and a settings page that was one scroll through six
// unrelated subjects. Aliases — the thing an operator of a forwarding server
// opens most often — were two clicks and four screens away, under a heading
// named after the shape of the page rather than the question asked of it.
//
// The order is the order somebody meets them: what is happening, then whether
// the domain is set up right, then who receives mail, then who may send it,
// then what the messages look like.
type Tab = { id: string; label: Key }

const TABS: Tab[] = [
  { id: 'overview', label: 'domain.tabOverview' },
  { id: 'settings', label: 'domain.tabSettings' },
  { id: 'aliases', label: 'domain.tabAliases' },
  { id: 'credentials', label: 'domain.tabCredentials' },
  { id: 'templates', label: 'domain.tabTemplates' },
]

// Everything the settings tabs need, in one round trip. It is the query the
// one long page used to make: three of the tabs want the same domain, and
// fetching it three times would be three loading states and three error lines
// for one failure.
const DOMAIN = `
  query ($domainId: String!) {
    GetDomain(domainId: $domainId) {
      id domain subdomain comment spamFilterScoreThreshold mailServers mailHosts linkHost linkHostname dkimSelector hasDkimKey
      aliases { id pattern comment kind email webhook disabled mailServer { host port username } }
      credentials { id comment alias disabled }
      records { checkedAt error records { type name expected priority optional found verified purpose } }
    }
  }`

// What every tab that changes something is given: what to show, and how to
// change it. run() performs a mutation, reloads the domain and puts any
// failure in the one error line above the tabs.
export type DomainTabProps = {
  domain: Domain
  run: (work: () => Promise<unknown>) => Promise<void>
}

export function DomainTabsPage() {
  const { t } = useTranslation()
  const { domainId, tab } = useParams()
  const navigate = useNavigate()
  const { data, error, loading, reload } = useQuery(
    () => graphql<{ GetDomain: Domain }>(DOMAIN, { domainId }),
    [domainId],
  )
  const [problem, setProblem] = useState<string | null>(null)

  // The domain's name, not the tab's: the tab row below already says which
  // tab this is, and a trail that said it as well was the same word twice.
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

  // A tab nobody has, or none named at all, is the first one. A path somebody
  // typed is not a tab.
  if (!TABS.some((candidate) => candidate.id === tab)) {
    return <Navigate to={`/domains/${domainId}/${TABS[0].id}`} replace />
  }

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

  return (
    <>
      <div className="tabs">
        {TABS.map((candidate) => (
          <button
            key={candidate.id}
            type="button"
            className={tab === candidate.id ? 'active' : ''}
            onClick={() => navigate(`/domains/${domainId}/${candidate.id}`)}
          >
            {t(candidate.label)}
          </button>
        ))}

        {/* Beside the tabs rather than among them. Both leave this page for a
            different one with a filter applied, and a tab that takes you
            somewhere else is a lie about where you are. They were two large
            tiles for exactly the same two links. */}
        <div className="tab-actions">
          <Link className="tab-action" to={`/mail?domain=${encodeURIComponent(domain.domain)}`}>
            {t('domain.viewMail')}
          </Link>
          <Link className="tab-action" to={`/queue?domain=${encodeURIComponent(domain.domain)}`}>
            {t('domain.viewQueue')}
          </Link>
        </div>
      </div>

      {/* One error line for the whole page, above the tab that caused it. */}
      {problem && <p className="error">{problem}</p>}

      {tab === 'overview' && <DomainOverviewTab domain={domain} />}
      {tab === 'settings' && <DomainDnsTab domain={domain} run={run} />}
      {tab === 'aliases' && <DomainAliasesTab domain={domain} run={run} />}
      {tab === 'credentials' && <DomainCredentialsTab domain={domain} run={run} />}
      {tab === 'templates' && <TemplatesTab />}
    </>
  )
}
