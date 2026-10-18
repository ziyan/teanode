import { useState } from 'react'

import { graphql } from '../../api'
import { formatMoney } from '../../components/common'
import { FormDialog } from '../../components/dialog'
import { CloseIcon } from '../../components/icons'
import { Select } from '../../components/select'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { Key, useTranslation } from '../../i18n/i18n'
import { FINANCE_ACCOUNTS, FinanceAccount, FinanceStatementImport, amountOf, formatDay, isDecimal } from './financeApi'
import { CurrencyPicker, useReportingCurrency } from './financeCommon'
import {
  IMPORT_TRANSACTIONS,
  PREVIEW_IMPORT_TRANSACTIONS,
  TransactionRowInput,
  TransactionRowPreview,
  TransactionRowsPreview,
} from './financeEntryApi'
import { ExchangeRateHint, blankToUndefined, isDay } from './financeEntryCommon'
import { statementImportSummary } from './statementImportSummary'

// The dialog that adds the transactions of an account that no provider
// reaches from rows typed by hand: the Add transactions button of the
// statement import opens it. It asks for the account as ImportTransactions
// takes it (an account imported before, or a new one with its institution,
// kind, currency and number) and the rows, each a day, a description and a
// signed amount with money out negative. Preview asks the server what
// importing would do, writing nothing: which account the rows go into, the
// rows it does not hold yet, those it does, the days and the money of the
// new ones, and what was checked. Changing anything after a preview asks
// for a new one.

type RowDraft = { key: number; postedOn: string; description: string; amount: string }

const NEW_ACCOUNT = ''

const ACCOUNT_KINDS: { value: string; label: Key }[] = [
  { value: 'bank', label: 'finance.entryAccountKindBank' },
  { value: 'card', label: 'finance.entryAccountKindCard' },
  { value: 'other', label: 'finance.entryAccountKindOther' },
]

function emptyRow(key: number): RowDraft {
  return { key, postedOn: '', description: '', amount: '' }
}

// PreviewRows is the rows of a preview as a table: the day, the
// description and the amount of each, as the server would keep them.
function PreviewRows({
  rows,
  currencyCode,
  caption,
}: {
  rows: TransactionRowPreview[]
  currencyCode: string
  caption: string
}) {
  const { t } = useTranslation()
  return (
    <div className="table-surface finance-entry-rows-table">
      <table>
        <caption>{caption}</caption>
        <thead>
          <tr>
            <th scope="col">{t('finance.postedOn')}</th>
            <th scope="col">{t('finance.entryRowDescription')}</th>
            <th scope="col" className="numeric">
              {t('finance.amount')}
            </th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.rowNumber}>
              <td>{formatDay(row.postedOn)}</td>
              <td>
                {row.description}
                {row.hasNearbyStoredTransaction ? (
                  <span className="muted"> · {t('finance.entryRowHasNearbyTransaction')}</span>
                ) : null}
              </td>
              <td className="numeric">{formatMoney(amountOf(row.amount), currencyCode)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

export function TransactionRowsDialog({ onImported, onClose }: { onImported: () => void; onClose: () => void }) {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const { reportingCurrencyCode } = useReportingCurrency()
  const accounts = useQuery(() => graphql<{ FinanceAccounts: FinanceAccount[] }>(FINANCE_ACCOUNTS), [], {
    refresh: false,
  })
  const statementAccounts = (accounts.data?.FinanceAccounts ?? []).filter(
    (account) => account.providerKind === 'statement',
  )
  const [financeAccountId, setFinanceAccountId] = useState(NEW_ACCOUNT)
  const [institutionName, setInstitutionName] = useState('')
  const [accountName, setAccountName] = useState('')
  const [accountNumber, setAccountNumber] = useState('')
  const [isAccountNumberPartial, setIsAccountNumberPartial] = useState(false)
  const [statementAccountKind, setStatementAccountKind] = useState('bank')
  const [typedCurrencyCode, setTypedCurrencyCode] = useState('')
  const [rows, setRows] = useState<RowDraft[]>([emptyRow(1)])
  const [nextRowKey, setNextRowKey] = useState(2)
  const [preview, setPreview] = useState<{ answer: TransactionRowsPreview; signature: string } | null>(null)
  const [isBusy, setIsBusy] = useState(false)
  const currencyCode = typedCurrencyCode || reportingCurrencyCode
  const isExistingAccount = financeAccountId !== NEW_ACCOUNT

  const chooseAccount = (value: string) => {
    setFinanceAccountId(value)
    setAccountNumber('')
    setIsAccountNumberPartial(false)
    const chosen = statementAccounts.find((account) => account.id === value)
    if (!chosen) {
      setInstitutionName('')
      setAccountName('')
      setTypedCurrencyCode('')
      setStatementAccountKind('bank')
      return
    }
    setInstitutionName(chosen.institutionName ?? chosen.accountName)
    setAccountName(chosen.accountName)
    setTypedCurrencyCode(chosen.currencyCode)
    setStatementAccountKind(/credit|card/i.test(chosen.accountKind) ? 'card' : 'bank')
  }

  const transactionRows: TransactionRowInput[] = rows.map((row) => ({
    postedOn: row.postedOn.trim(),
    description: row.description.trim(),
    amount: row.amount.trim(),
  }))
  const variables = {
    financeAccountId: blankToUndefined(financeAccountId),
    isNewAccount: financeAccountId === NEW_ACCOUNT ? true : undefined,
    institutionName: institutionName.trim(),
    accountName: blankToUndefined(accountName),
    accountNumber: blankToUndefined(accountNumber),
    isAccountNumberPartial: blankToUndefined(accountNumber) ? isAccountNumberPartial : undefined,
    statementAccountKind,
    currencyCode: currencyCode.trim().toUpperCase(),
    transactionRows,
  }
  const signature = JSON.stringify(variables)
  const shownPreview = preview && preview.signature === signature ? preview.answer : null

  const isValid =
    institutionName.trim() !== '' &&
    /^[A-Za-z]{3}$/.test(currencyCode.trim()) &&
    rows.length > 0 &&
    rows.every((row) => isDay(row.postedOn) && row.description.trim() !== '' && isDecimal(row.amount))

  const changeRow = (key: number, change: Partial<RowDraft>) =>
    setRows((previous) => previous.map((row) => (row.key === key ? { ...row, ...change } : row)))
  const addRow = () => {
    setRows((previous) => [...previous, emptyRow(nextRowKey)])
    setNextRowKey(nextRowKey + 1)
  }
  const removeRow = (key: number) => setRows((previous) => previous.filter((row) => row.key !== key))

  const submit = async () => {
    setIsBusy(true)
    try {
      if (!shownPreview) {
        const answer = await graphql<{ PreviewImportTransactions: TransactionRowsPreview }>(
          PREVIEW_IMPORT_TRANSACTIONS,
          variables,
        )
        setPreview({ answer: answer.PreviewImportTransactions, signature })
        return
      }
      const answer = await graphql<{ ImportTransactions: FinanceStatementImport }>(IMPORT_TRANSACTIONS, variables)
      toast.done(statementImportSummary(t, answer.ImportTransactions))
      onImported()
      onClose()
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setIsBusy(false)
    }
  }

  const newRowCount = shownPreview?.newTransactionRows.length ?? 0
  const presentRowCount = shownPreview?.presentTransactionRows.length ?? 0

  return (
    <FormDialog
      wide
      title={t('finance.entryRowsTitle')}
      submitLabel={shownPreview ? t('finance.entryImportRows') : t('finance.entryPreview')}
      busy={isBusy}
      canSubmit={isValid && !isBusy}
      onSubmit={() => void submit()}
      onClose={() => {
        if (!isBusy) onClose()
      }}
    >
      <p className="muted">{t('finance.entryRowsHint')}</p>
      <div className="finance-entry-fields">
        <div>
          <span className="finance-entry-label">{t('finance.account')}</span>
          <Select
            block
            value={financeAccountId}
            label={t('finance.account')}
            options={[
              { value: NEW_ACCOUNT, label: t('finance.entryNewAccount') },
              ...statementAccounts.map((account) => ({
                value: account.id,
                label: account.accountMask ? `${account.accountName} ····${account.accountMask}` : account.accountName,
              })),
            ]}
            onChange={chooseAccount}
          />
        </div>
        <label>
          <span>{t('finance.institution')}</span>
          <input
            value={institutionName}
            disabled={isExistingAccount}
            onChange={(event) => setInstitutionName(event.target.value)}
          />
        </label>
        <label>
          <span>{t('finance.entryAccountName')}</span>
          <input
            value={accountName}
            disabled={isExistingAccount}
            onChange={(event) => setAccountName(event.target.value)}
          />
        </label>
        <label>
          <span>{t('finance.entryAccountNumber')}</span>
          <input
            value={accountNumber}
            inputMode="numeric"
            disabled={isExistingAccount}
            onChange={(event) => setAccountNumber(event.target.value)}
          />
        </label>
        <label>
          <span>{t('finance.accountKindLabel')}</span>
          <select
            value={statementAccountKind}
            disabled={isExistingAccount}
            onChange={(event) => setStatementAccountKind(event.target.value)}
          >
            {ACCOUNT_KINDS.map((kind) => (
              <option key={kind.value} value={kind.value}>
                {t(kind.label)}
              </option>
            ))}
          </select>
        </label>
        <div>
          <span className="finance-entry-label">{t('finance.currency')}</span>
          <CurrencyPicker
            value={currencyCode}
            label={t('finance.currency')}
            disabled={isExistingAccount}
            onChange={setTypedCurrencyCode}
          />
          <ExchangeRateHint currencyCode={currencyCode} />
        </div>
      </div>
      {accountNumber.trim() !== '' ? (
        <label className="checkbox">
          <input
            type="checkbox"
            checked={isAccountNumberPartial}
            onChange={(event) => setIsAccountNumberPartial(event.target.checked)}
          />
          {t('finance.entryIsAccountNumberPartial')}
        </label>
      ) : null}
      <fieldset className="finance-entry-lines">
        <legend>{t('finance.entryRows')}</legend>
        <p className="muted field-hint">{t('finance.entryRowsAmountHint')}</p>
        {rows.map((row, index) => (
          <div
            key={row.key}
            className="finance-entry-line"
            role="group"
            aria-label={t('finance.entryRowNumber', { number: index + 1 })}
          >
            <strong className="finance-entry-line-number muted">
              {t('finance.entryRowNumber', { number: index + 1 })}
            </strong>
            <label>
              <span>{t('finance.postedOn')}</span>
              <input
                type="date"
                value={row.postedOn}
                onChange={(event) => changeRow(row.key, { postedOn: event.target.value })}
              />
            </label>
            <label className="finance-entry-wide">
              <span>{t('finance.entryRowDescription')}</span>
              <input
                value={row.description}
                onChange={(event) => changeRow(row.key, { description: event.target.value })}
              />
            </label>
            <label>
              <span>{t('finance.amount')}</span>
              <input
                inputMode="decimal"
                value={row.amount}
                onChange={(event) => changeRow(row.key, { amount: event.target.value })}
              />
            </label>
            <Tooltip label={t('common.remove')}>
              <button
                type="button"
                className="icon-action danger finance-entry-line-remove"
                disabled={rows.length === 1}
                aria-label={`${t('finance.entryRowNumber', { number: index + 1 })}: ${t('common.remove')}`}
                onClick={() => removeRow(row.key)}
              >
                <CloseIcon size={16} />
              </button>
            </Tooltip>
          </div>
        ))}
        <button type="button" onClick={addRow}>
          {t('finance.entryAddRow')}
        </button>
      </fieldset>
      {shownPreview ? (
        <section className="finance-entry-preview" aria-label={t('finance.entryPreviewTitle')}>
          <strong>{t('finance.entryPreviewTitle')}</strong>
          <p>
            {shownPreview.isNewAccount
              ? t('finance.entryRowsNewAccount', { account: shownPreview.accountName })
              : t('finance.entryRowsIntoAccount', { account: shownPreview.accountName })}
          </p>
          <p>
            {plural(
              newRowCount,
              { one: 'finance.entryRowsNewOne', other: 'finance.entryRowsNewOther' },
              { count: String(newRowCount) },
            )}
            {presentRowCount > 0
              ? ' ' +
                plural(
                  presentRowCount,
                  { one: 'finance.entryRowsPresentOne', other: 'finance.entryRowsPresentOther' },
                  { count: String(presentRowCount) },
                )
              : ''}
          </p>
          {newRowCount > 0 && shownPreview.firstPostedOn && shownPreview.lastPostedOn ? (
            <p className="muted">
              {t('finance.entryRowsMoney', {
                from: formatDay(shownPreview.firstPostedOn),
                to: formatDay(shownPreview.lastPostedOn),
                moneyIn: formatMoney(amountOf(shownPreview.moneyInAmount), shownPreview.currencyCode),
                moneyOut: formatMoney(Math.abs(amountOf(shownPreview.moneyOutAmount)), shownPreview.currencyCode),
              })}
            </p>
          ) : null}
          {shownPreview.verificationSummary ? <p className="muted">{shownPreview.verificationSummary}</p> : null}
          {newRowCount > 0 ? (
            <PreviewRows
              rows={shownPreview.newTransactionRows}
              currencyCode={shownPreview.currencyCode}
              caption={t('finance.entryRowsNewCaption')}
            />
          ) : null}
          {presentRowCount > 0 ? (
            <PreviewRows
              rows={shownPreview.presentTransactionRows}
              currencyCode={shownPreview.currencyCode}
              caption={t('finance.entryRowsPresentCaption')}
            />
          ) : null}
        </section>
      ) : null}
    </FormDialog>
  )
}
