import { useEffect, useMemo } from 'react'
import { Link, Navigate, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'

import { graphql } from '../api'
import { ErrorMessage, Loading } from '../components/common'
import { Select } from '../components/select'
import { TabItem, Tabs } from '../components/tabs'
import { useToast } from '../components/toast'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'
import { FinanceAccountsSection } from './finance/financeAccounts'
import { FinanceBudgetsSection } from './finance/financeBudgets'
import { usePersonZone } from './finance/financeCommon'
import { FinanceNetWorthSection } from './finance/financeNetWorth'
import { useAgentFinancePresence } from './finance/financePresence'
import { FinanceReceiptListSection } from './finance/financeReceiptList'
import { FinanceSavingsTargetsSection } from './finance/financeSavingsTargets'
import { FinanceShared } from './finance/financeShared'
import { LIST_SHARED_FINANCE_ACCOUNTS, SharedFinanceAccount } from './finance/financeShareApi'
import { FinanceSpendingSection } from './finance/financeSpending'
import { FinanceTaxesSection } from './finance/financeTaxes'
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
  { id: 'receipts', label: 'finance.tabReceipts' },
  { id: 'taxes', label: 'finance.tabTaxes' },
  { id: 'accounts', label: 'finance.tabAccounts' },
  { id: 'budgets', label: 'finance.tabBudgets' },
  { id: 'net-worth', label: 'finance.tabNetWorth' },
  { id: 'savings-targets', label: 'finance.tabSavingsTargets' },
]

type Sharer = { userId: string; name: string }

export function FinancePage() {
  const { section } = useParams()
  const navigate = useNavigate()
  const location = useLocation()
  const { t } = useTranslation()
  const toast = useToast()
  const [parameters, setParameters] = useSearchParams()
  const presence = useAgentFinancePresence()
  // The sections' default days and months are the person's, in the zone
  // their agent keeps, so the zone is read before any section is drawn.
  const isZoneRead = usePersonZone()
  // What other people share with this person: who, once each, however many
  // accounts they share. Asked once, for the Showing choice and for the
  // accounts of whoever is chosen.
  const shared = useQuery(
    async () =>
      (await graphql<{ ListSharedFinanceAccounts: SharedFinanceAccount[] | null }>(LIST_SHARED_FINANCE_ACCOUNTS))
        .ListSharedFinanceAccounts ?? [],
    [],
    { refresh: false },
  )
  const sharers = useMemo(() => {
    const found = new Map<string, Sharer>()
    for (const account of shared.data ?? []) {
      if (!found.has(account.ownerUserId)) {
        found.set(account.ownerUserId, {
          userId: account.ownerUserId,
          name: account.ownerName || account.ownerUsername,
        })
      }
    }
    return [...found.values()].sort((first, second) => first.name.localeCompare(second.name))
  }, [shared.data])
  const requestedPerson = parameters.get('person')
  const viewedPerson = sharers.find((sharer) => sharer.userId === requestedPerson) ?? null
  // A link to somebody who is not sharing (or stopped) is taken out of the
  // address once the shares are read, and the person is told.
  const isSharesFailed = shared.error !== null && shared.data === null
  const isPersonGone = requestedPerson !== null && (shared.data !== null || isSharesFailed) && viewedPerson === null
  useEffect(() => {
    if (!isPersonGone) return
    toast.failed(t('finance.personNotSharing'))
    setParameters(
      (previous) => {
        const written = new URLSearchParams(previous)
        written.delete('person')
        written.delete('account')
        written.delete('page')
        return written
      },
      { replace: true },
    )
    // Once per link found stale.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isPersonGone, requestedPerson])
  if (presence.isShown === null) return <Loading />
  if (!presence.isShown) {
    return (
      <div className="card">
        <h3>{t('finance.title')}</h3>
        <p className="muted">{t('finance.notShown')}</p>
        <div className="page-actions">
          <Link className="button" to="/settings/agent">
            {t('finance.openAgentPage')}
          </Link>
        </div>
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
  const choosePerson = (chosenUserId: string) =>
    setParameters((previous) => {
      const written = new URLSearchParams(previous)
      if (chosenUserId) written.set('person', chosenUserId)
      else written.delete('person')
      written.delete('account')
      written.delete('page')
      return written
    })
  const personOptions = [
    { value: '', label: t('finance.you') },
    ...sharers.map((sharer) => ({ value: sharer.userId, label: sharer.name })),
  ]
  const picker =
    sharers.length > 0 ? (
      <div className="finance-toolbar">
        <label>
          <span>{t('finance.person')}</span>
          <Select
            value={viewedPerson?.userId ?? ''}
            label={t('finance.person')}
            options={personOptions}
            onChange={choosePerson}
          />
        </label>
      </div>
    ) : null
  // Somebody's shared accounts are not the person's own sections: they are
  // drawn instead of the tabs, with the picker to go back to one's own.
  if (viewedPerson) {
    return (
      <>
        <ErrorMessage error={shared.error} />
        {picker}
        <FinanceShared
          ownerName={viewedPerson.name}
          accounts={(shared.data ?? []).filter((account) => account.ownerUserId === viewedPerson.userId)}
        />
      </>
    )
  }
  // The same tabs at every width, as the other pages of tabs have: on a
  // phone the strip scrolls sideways inside itself and brings the section
  // being shown into view, and the page stays the width of the screen.
  return (
    <>
      <ErrorMessage error={shared.error} />
      {picker}
      <Tabs items={FINANCE_SECTIONS} active={section} onSelect={openSection} />
      {!presence.hasSources ? (
        <div className="finance-pointer">
          <p className="muted">{t('finance.nothingLinkedPointer')}</p>
          <div className="page-actions">
            <Link className="button" to={FINANCE_SETUP_PATH}>
              {t('finance.openFinanceSetup')}
            </Link>
          </div>
        </div>
      ) : null}
      {section === 'spending' ? <FinanceSpendingSection /> : null}
      {section === 'transactions' ? <FinanceTransactionsSection /> : null}
      {section === 'receipts' ? <FinanceReceiptListSection /> : null}
      {section === 'taxes' ? <FinanceTaxesSection /> : null}
      {section === 'accounts' ? <FinanceAccountsSection /> : null}
      {section === 'budgets' ? <FinanceBudgetsSection /> : null}
      {section === 'net-worth' ? <FinanceNetWorthSection /> : null}
      {section === 'savings-targets' ? <FinanceSavingsTargetsSection /> : null}
    </>
  )
}
