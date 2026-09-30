import { Link, Navigate, useLocation, useNavigate, useParams } from 'react-router-dom'

import { Loading } from '../components/common'
import { TabItem, Tabs } from '../components/tabs'
import { useTranslation } from '../i18n/i18n'
import { FinanceAccountsSection } from './finance/financeAccounts'
import { FinanceBudgetsSection } from './finance/financeBudgets'
import { usePersonZone } from './finance/financeCommon'
import { FinanceNetWorthSection } from './finance/financeNetWorth'
import { useAgentFinancePresence } from './finance/financePresence'
import { FinanceSavingsTargetsSection } from './finance/financeSavingsTargets'
import { FinanceSpendingSection } from './finance/financeSpending'
import { FinanceTransactionsSection } from './finance/financeTransactions'

// Where the Finance page's setup is: linking an institution and choosing
// the reporting currency are the agent page's Finance tab.
export const FINANCE_SETUP_PATH = '/settings/agent/finance'

// The Finance page: everything built on what the linked institutions
// report, in the same operations the command line's teanode finance and
// the agent's finance tool call. A page of its own in the rail, like
// Knowledge, because it is read often and is not a setting; linking and the
// reporting currency stay on the agent page. Its sections are tabs, each a
// place in the address, because a person opens it to answer one question --
// what did I spend, what am I worth, am I on budget -- and six panels in
// one scroll made them hunt for it.
export const FINANCE_SECTIONS: TabItem[] = [
  { id: 'spending', label: 'finance.tabSpending' },
  { id: 'transactions', label: 'finance.tabTransactions' },
  { id: 'accounts', label: 'finance.tabAccounts' },
  { id: 'budgets', label: 'finance.tabBudgets' },
  { id: 'net-worth', label: 'finance.tabNetWorth' },
  { id: 'savings-targets', label: 'finance.tabSavingsTargets' },
]

export function FinancePage() {
  const { section } = useParams()
  const navigate = useNavigate()
  const location = useLocation()
  const { t } = useTranslation()
  const presence = useAgentFinancePresence()
  // The sections' default days and months are the person's, in the zone
  // their agent keeps, so the zone is read before any section is drawn.
  const isZoneRead = usePersonZone()
  if (presence.isShown === null) return <Loading />
  if (!presence.isShown) {
    return (
      <div className="card">
        <h3>{t('finance.title')}</h3>
        <p className="muted">
          {t('finance.notShown')} <Link to="/settings/agent">{t('finance.openAgentPage')}</Link>
        </p>
      </div>
    )
  }
  // /finance on its own, or a section this page does not have, is the
  // first section. Somebody with nothing linked yet still lands here rather
  // than on the agent page: the rail says Finance, and the line under the
  // sections says where linking is.
  if (!FINANCE_SECTIONS.some((candidate) => candidate.id === section)) {
    return <Navigate to={`/finance/${FINANCE_SECTIONS[0].id}${location.search}`} replace />
  }
  if (!isZoneRead) return <Loading />
  const openSection = (id: string) => navigate(`/finance/${id}`)
  // The same tabs at every width, as the other pages of tabs have: on a
  // phone the strip scrolls sideways inside itself and brings the section
  // being shown into view, and the page stays the width of the screen.
  return (
    <>
      <Tabs items={FINANCE_SECTIONS} active={section} onSelect={openSection} />
      {!presence.hasSources ? (
        <p className="muted finance-pointer">
          {t('finance.nothingLinkedPointer')} <Link to={FINANCE_SETUP_PATH}>{t('finance.openFinanceSetup')}</Link>
        </p>
      ) : null}
      {section === 'spending' ? <FinanceSpendingSection /> : null}
      {section === 'transactions' ? <FinanceTransactionsSection /> : null}
      {section === 'accounts' ? <FinanceAccountsSection /> : null}
      {section === 'budgets' ? <FinanceBudgetsSection /> : null}
      {section === 'net-worth' ? <FinanceNetWorthSection /> : null}
      {section === 'savings-targets' ? <FinanceSavingsTargetsSection /> : null}
    </>
  )
}
