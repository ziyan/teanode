import { useState } from 'react'

import { graphql } from '../../api'
import { ErrorMessage, Loading, Tag, formatTime } from '../../components/common'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { PencilIcon, TrashIcon } from '../../components/icons'
import { SettingsEmpty, SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import {
  DELETE_STATEMENT_ACCOUNT,
  FINANCE_ACCOUNTS,
  FinanceAccount,
  RENAME_STATEMENT_ACCOUNT,
  StatementAccountDeleted,
} from './financeApi'
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
// An account from statements can be renamed, given the last digits of its
// number when its statements show a word or nothing there, and deleted
// from its row; a provider's cannot, since its next sync would bring it
// back as it was.
export function FinanceAccountsSection() {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const words = useFinanceWords()
  const { data, error, loading, reload } = useQuery(
    () => graphql<{ FinanceAccounts: FinanceAccount[] }>(FINANCE_ACCOUNTS),
    [],
  )
  const accounts = data?.FinanceAccounts ?? []
  const [importCount, setImportCount] = useState(0)
  const [renaming, setRenaming] = useState<FinanceAccount | null>(null)
  const [accountName, setAccountName] = useState('')
  const [accountMask, setAccountMask] = useState('')
  const [deleting, setDeleting] = useState<FinanceAccount | null>(null)
  const [busy, setBusy] = useState(false)
  const hasStatementAccount = accounts.some((account) => account.providerKind === 'statement')

  // What a rename or a delete changes is read again everywhere it shows:
  // the table, and the credit usage above it.
  const changed = async () => {
    setImportCount((count) => count + 1)
    await reload()
  }

  // Only what was changed is sent: the number shown may be the
  // statement's, and sending it back unchanged would make it the person's,
  // kept over every later statement. An emptied number takes theirs back.
  const isNameChanged = renaming !== null && accountName.trim() !== '' && accountName.trim() !== renaming.accountName
  const isMaskChanged = renaming !== null && accountMask.trim() !== (renaming.accountMask ?? '')
  const rename = async () => {
    if (!renaming) return
    setBusy(true)
    try {
      const variables: Record<string, string> = { financeAccountId: renaming.id }
      if (isNameChanged) variables.accountName = accountName.trim()
      if (isMaskChanged) variables.accountMask = accountMask.trim()
      await graphql(RENAME_STATEMENT_ACCOUNT, variables)
      // The toast says each thing that changed, so a number that did not
      // take is not hidden behind a rename that did.
      const newName = accountName.trim()
      const newMask = accountMask.trim()
      if (isNameChanged && isMaskChanged) {
        toast.done(
          newMask !== ''
            ? t('finance.accountRenamedWithMask', { name: newName, accountMask: newMask })
            : t('finance.accountRenamedMaskCleared', { name: newName }),
        )
      } else if (isNameChanged) {
        toast.done(t('finance.accountRenamed', { name: newName }))
      } else if (newMask !== '') {
        toast.done(t('finance.accountMaskSaved', { accountMask: newMask }))
      } else {
        toast.done(t('finance.accountMaskCleared'))
      }
      setRenaming(null)
      await changed()
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setBusy(false)
    }
  }

  const remove = async () => {
    if (!deleting) return
    setBusy(true)
    try {
      const answer = await graphql<{ DeleteStatementAccount: StatementAccountDeleted }>(DELETE_STATEMENT_ACCOUNT, {
        financeAccountId: deleting.id,
      })
      const count = answer.DeleteStatementAccount.deletedTransactionCount
      toast.done(
        plural(
          count,
          { one: 'finance.accountDeletedOne', other: 'finance.accountDeletedOther' },
          { name: accountLabel(deleting), count: String(count) },
        ),
      )
      setDeleting(null)
      await changed()
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setBusy(false)
    }
  }

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
                  {hasStatementAccount ? <th></th> : null}
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
                    {hasStatementAccount ? (
                      <td className="shrink">
                        {account.providerKind === 'statement' ? (
                          <div className="row-actions">
                            <Tooltip label={t('common.rename')}>
                              <button
                                type="button"
                                className="icon-action"
                                aria-label={`${accountLabel(account)}: ${t('common.rename')}`}
                                disabled={busy}
                                onClick={() => {
                                  setAccountName(account.accountName)
                                  setAccountMask(account.accountMask ?? '')
                                  setRenaming(account)
                                }}
                              >
                                <PencilIcon size={16} />
                              </button>
                            </Tooltip>
                            <Tooltip label={t('common.delete')}>
                              <button
                                type="button"
                                className="icon-action danger"
                                aria-label={`${accountLabel(account)}: ${t('common.delete')}`}
                                disabled={busy}
                                onClick={() => setDeleting(account)}
                              >
                                <TrashIcon size={16} />
                              </button>
                            </Tooltip>
                          </div>
                        ) : null}
                      </td>
                    ) : null}
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
      {renaming ? (
        <FormDialog
          title={t('finance.renameAccountTitle')}
          submitLabel={t('common.save')}
          busy={busy}
          canSubmit={accountName.trim() !== '' && (isNameChanged || isMaskChanged)}
          onClose={() => setRenaming(null)}
          onSubmit={() => void rename()}
        >
          <label>
            <span>{t('finance.accountNameLabel')}</span>
            <input
              value={accountName}
              maxLength={200}
              autoFocus
              onChange={(event) => setAccountName(event.target.value)}
            />
          </label>
          <p className="muted">{t('finance.accountNameHint')}</p>
          <label>
            <span>{t('finance.accountMaskLabel')}</span>
            <input
              value={accountMask}
              inputMode="numeric"
              maxLength={8}
              onChange={(event) => setAccountMask(event.target.value)}
            />
          </label>
          <p className="muted">{t('finance.accountMaskHint')}</p>
        </FormDialog>
      ) : null}
      {deleting ? (
        <ConfirmDialog
          title={t('finance.deleteAccountTitle', { name: accountLabel(deleting) })}
          body={<p className="muted">{t('finance.deleteAccountBody')}</p>}
          confirmLabel={t('finance.deleteAccountConfirm')}
          busy={busy}
          onClose={() => setDeleting(null)}
          onConfirm={() => void remove()}
        />
      ) : null}
    </>
  )
}
