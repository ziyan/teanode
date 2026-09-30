import { Navigate, useLocation, useNavigate, useParams } from 'react-router-dom'

import { graphql } from '../api'
import { Loading } from '../components/common'
import { TabItem, Tabs } from '../components/tabs'
import { useQuery } from '../components/useQuery'
import { FINANCE_PRESENCE, FinanceProvider, PERSON_ZONE, PersonZoneAnswer, setPersonZone } from './finance/financeApi'
import { FinanceAccountsSection } from './finance/financeAccounts'
import { FinanceBudgetsSection } from './finance/financeBudgets'
import { FinanceNetWorthSection } from './finance/financeNetWorth'
import { FinanceSavingsTargetsSection } from './finance/financeSavingsTargets'
import { FinanceSettingsSection } from './finance/financeSettings'
import { FinanceSourcesSection } from './finance/financeSources'
import { FinanceSpendingSection } from './finance/financeSpending'
import { FinanceTransactionsSection } from './finance/financeTransactions'

// The Finance tab: the institutions a person linked and everything built on
// what they report, in the same operations the command line's teanode
// finance and the agent's finance tool call. Its sections are tabs of
// their own, each a place in the address, because a person opens it to
// answer one question -- what did I spend, what am I worth, am I on
// budget -- and eight panels in one scroll made them hunt for it.
export const FINANCE_SECTIONS: TabItem[] = [
  { id: 'spending', label: 'finance.tabSpending' },
  { id: 'transactions', label: 'finance.tabTransactions' },
  { id: 'accounts', label: 'finance.tabAccounts' },
  { id: 'budgets', label: 'finance.tabBudgets' },
  { id: 'net-worth', label: 'finance.tabNetWorth' },
  { id: 'savings-targets', label: 'finance.tabSavingsTargets' },
  { id: 'sources', label: 'finance.tabSources' },
  { id: 'settings', label: 'finance.tabSettings' },
]

// useFinancePresence says whether the Finance tab is shown: when the
// operator offers a provider to link through, or when the person already
// has finance sources, which stay theirs to see and delete after a
// provider stops being offered. Null while it is being asked. A server
// without the finance operations answers with an error, and then there is
// no tab.
export function useFinancePresence(isAgentOn: boolean): { isShown: boolean | null; hasSources: boolean } {
  const { data, error } = useQuery(
    async () =>
      isAgentOn
        ? {
            ...(await graphql<{ FinanceProviders: FinanceProvider[]; FinanceSources: { id: string }[] }>(
              FINANCE_PRESENCE,
            )),
            isAskedForAgent: true,
          }
        : { FinanceProviders: [], FinanceSources: [], isAskedForAgent: false },
    [isAgentOn],
    { refresh: false },
  )
  // Undecided until the question has been answered for the agent that is
  // on. The query's data outlives a change of its inputs, and on the render
  // after the agent loaded it still held the empty answer given while the
  // agent was loading, with nothing marked as loading yet; read as "no
  // finance", it sent a link to the Finance tab to the first tab.
  if (error && isAgentOn) return { isShown: false, hasSources: false }
  if (!isAgentOn || !data?.isAskedForAgent) return { isShown: null, hasSources: false }
  const hasSources = (data?.FinanceSources.length ?? 0) > 0
  return { isShown: (data?.FinanceProviders.length ?? 0) > 0 || hasSources, hasSources }
}

export function FinanceTab({ hasSources }: { hasSources: boolean }) {
  const { section } = useParams()
  const navigate = useNavigate()
  const location = useLocation()
  // The sections' default days and months are the person's, in the zone
  // their agent keeps, so the zone is read before any section is drawn. A
  // failure to read it leaves the browser's zone rather than no tab.
  const zone = useQuery(() => graphql<PersonZoneAnswer>(PERSON_ZONE), [], { refresh: false })
  // Nothing linked yet, the only useful section is where linking happens.
  if (!FINANCE_SECTIONS.some((candidate) => candidate.id === section)) {
    const landing = hasSources ? FINANCE_SECTIONS[0].id : 'sources'
    return <Navigate to={`/settings/agent/finance/${landing}${location.search}`} replace />
  }
  if (!zone.data && !zone.error) return <Loading />
  setPersonZone(zone.data?.ReadAgent?.timezone ?? '')
  return (
    <>
      <Tabs items={FINANCE_SECTIONS} active={section} onSelect={(id) => navigate(`/settings/agent/finance/${id}`)} />
      {section === 'sources' ? <FinanceSourcesSection /> : null}
      {section === 'accounts' ? <FinanceAccountsSection /> : null}
      {section === 'transactions' ? <FinanceTransactionsSection /> : null}
      {section === 'spending' ? <FinanceSpendingSection /> : null}
      {section === 'budgets' ? <FinanceBudgetsSection /> : null}
      {section === 'net-worth' ? <FinanceNetWorthSection /> : null}
      {section === 'savings-targets' ? <FinanceSavingsTargetsSection /> : null}
      {section === 'settings' ? <FinanceSettingsSection /> : null}
    </>
  )
}
