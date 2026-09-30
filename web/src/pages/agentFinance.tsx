import { Link } from 'react-router-dom'

import { Loading } from '../components/common'
import { useTranslation } from '../i18n/i18n'
import { usePersonZone } from './finance/financeCommon'
import { FinanceSettingsSection } from './finance/financeSettings'
import { FinanceSourcesSection } from './finance/financeSources'

// The agent page's Finance tab: how finance is set up, not what it says.
// The institutions a person linked, with linking, repairing, importing,
// syncing, switching and deleting them, then the reporting currency and the
// converter, stacked. What they report is read on the Finance page, a place
// of its own in the rail: a person reads their spending often and sets up a
// bank once, and the two rows of tabs this tab used to stack were one too
// many on a phone.
export function FinanceTab() {
  const { t } = useTranslation()
  // The converter's default day is the person's, in the zone their agent
  // keeps, so the zone is read before the panels are drawn.
  const isZoneRead = usePersonZone()
  if (!isZoneRead) return <Loading />
  return (
    <>
      <p className="muted finance-pointer">
        {t('finance.setupPointer')} <Link to="/finance">{t('finance.openFinancePage')}</Link>
      </p>
      <FinanceSourcesSection />
      <FinanceSettingsSection />
    </>
  )
}
