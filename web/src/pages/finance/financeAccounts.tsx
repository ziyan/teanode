import { useState } from 'react'

import { graphql } from '../../api'
import { ErrorMessage, Loading, Tag, formatTime } from '../../components/common'
import { SettingsEmpty, SettingsSection } from '../../components/settingsList'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { FINANCE_ACCOUNTS, FinanceAccount } from './financeApi'
import { Money, accountLabel, useFinanceWords } from './financeCommon'
import { FinanceCreditUsageSection } from './financeCreditUsage'
import { FinanceStatementImportSection } from './financeStatementImport'

// The finance accounts every finance source reports, with their balances
// in their own currency and, where there is an exchange rate, in the
// reporting currency. A table: the balances line up down the page, which is
// the point of looking at them together, and on a phone it scrolls sideways
// rather than falling apart into cards. Above it, the credit cards' usage,
// which is read from these same balances; under it, the statement import,
// for the accounts no provider reaches, after which both are read again.
export function FinanceAccountsSection() {
  const { t } = useTranslation()
  const words = useFinanceWords()
  const { data, error, loading, reload } = useQuery(
    () => graphql<{ FinanceAccounts: FinanceAccount[] }>(FINANCE_ACCOUNTS),
    [],
  )
  const accounts = data?.FinanceAccounts ?? []
  const [importCount, setImportCount] = useState(0)

  return (
    <>
      <FinanceCreditUsageSection refreshKey={importCount} />
      <SettingsSection card title={t('finance.accountsTitle')} description={t('finance.accountsHint')}>
        <ErrorMessage error={error} />
        {loading && !data ? <Loading /> : null}
        {data && accounts.length === 0 ? <SettingsEmpty>{t('finance.noAccounts')}</SettingsEmpty> : null}
        {accounts.length > 0 ? (
          <div className="table-wrap">
            <table className="numbers-table finance-table">
              <thead>
                <tr>
                  <th>{t('finance.account')}</th>
                  <th>{t('finance.institution')}</th>
                  <th>{t('finance.accountKindLabel')}</th>
                  <th className="numeric">{t('finance.currentBalance')}</th>
                  <th className="numeric">{t('finance.availableBalance')}</th>
                  <th className="numeric">{t('finance.inReportingCurrency')}</th>
                  <th>{t('finance.balanceAt')}</th>
                </tr>
              </thead>
              <tbody>
                {accounts.map((account) => (
                  <tr key={account.id}>
                    <td>
                      {accountLabel(account)}
                      {account.providerKind === 'statement' ? (
                        <>
                          {' '}
                          <Tag value={t('finance.fromStatements')} />
                        </>
                      ) : null}
                    </td>
                    <td>{account.institutionName || '—'}</td>
                    <td>{words.accountKind(account.accountKind)}</td>
                    <td className="numeric">
                      <Money amount={account.currentBalance} currency={account.currencyCode} />
                    </td>
                    <td className="numeric">
                      <Money amount={account.availableBalance} currency={account.currencyCode} />
                    </td>
                    <td className="numeric">
                      {account.reportingCurrencyCode && account.reportingCurrencyCode !== account.currencyCode ? (
                        <Money amount={account.convertedCurrentBalance} currency={account.reportingCurrencyCode} />
                      ) : (
                        <span className="muted">—</span>
                      )}
                    </td>
                    <td className="muted">{account.balanceAt ? formatTime(account.balanceAt) : '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </SettingsSection>
      <FinanceStatementImportSection
        onImported={() => {
          setImportCount((count) => count + 1)
          void reload()
        }}
      />
    </>
  )
}
