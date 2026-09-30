import { Navigate, useLocation, useNavigate, useParams } from 'react-router-dom'

import { graphql } from '../api'
import { TabItem, Tabs } from '../components/tabs'
import { useQuery } from '../components/useQuery'
import { FINANCE_PRESENCE, FinanceProvider } from './finance/financeApi'
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
  const { data, error, loading } = useQuery(
    () =>
      isAgentOn
        ? graphql<{ FinanceProviders: FinanceProvider[]; FinanceSources: { id: string }[] }>(FINANCE_PRESENCE)
        : Promise.resolve({ FinanceProviders: [], FinanceSources: [] }),
    [isAgentOn],
    { refresh: false },
  )
  if (error) return { isShown: false, hasSources: false }
  if (loading && !data) return { isShown: null, hasSources: false }
  const hasSources = (data?.FinanceSources.length ?? 0) > 0
  return { isShown: (data?.FinanceProviders.length ?? 0) > 0 || hasSources, hasSources }
}

export function FinanceTab({ hasSources }: { hasSources: boolean }) {
  const { section } = useParams()
  const navigate = useNavigate()
  const location = useLocation()
  // Nothing linked yet, the only useful section is where linking happens.
  if (!FINANCE_SECTIONS.some((candidate) => candidate.id === section)) {
    const landing = hasSources ? FINANCE_SECTIONS[0].id : 'sources'
    return <Navigate to={`/settings/agent/finance/${landing}${location.search}`} replace />
  }
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
