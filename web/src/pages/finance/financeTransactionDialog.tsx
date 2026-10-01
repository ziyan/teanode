import { useEffect } from 'react'

import { graphql } from '../../api'
import { CopyIconButton, formatMoney, formatTime } from '../../components/common'
import { ConfirmDialog } from '../../components/dialog'
import { Select } from '../../components/select'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import {
  FINANCE_TRANSACTIONS,
  FinanceAccount,
  FinanceTransaction,
  FinanceTransactionPage,
  amountOf,
  formatDay,
  hasAmount,
} from './financeApi'
import { accountLabel, useFinanceWords } from './financeCommon'

// providerMetadataText is the provider's object for a transaction laid out
// to be read: indented JSON, or nothing when the provider sent none.
export function providerMetadataText(providerMetadata: unknown): string {
  if (providerMetadata === undefined || providerMetadata === null || providerMetadata === '') return ''
  if (typeof providerMetadata === 'string') {
    try {
      return JSON.stringify(JSON.parse(providerMetadata), null, 2)
    } catch {
      return providerMetadata
    }
  }
  return JSON.stringify(providerMetadata, null, 2)
}

// FinanceTransactionDialog is everything known about one finance
// transaction: what the list shows, what it leaves out (the provider's
// categories and ids, who decided its spending category, when it was added
// and changed), and the provider's whole record of it. The spending
// category can be changed here as in the row; a transfer is the transfer
// category, and the line under it says what made it one (paired with the
// other side, the provider, a rule or the person). What the provider wrote
// is shown as text and nothing else: it comes from whoever charged the
// account.
//
// A mirrored copy says which copy is counted, by account and day, and
// opens it; Count this one is the person's word that it is a charge of
// its own. The counted copy names its duplicates the same way.
export function FinanceTransactionDialog({
  financeTransaction,
  financeAccount,
  financeAccounts,
  categoryOptions,
  isCounting,
  onCategorize,
  onCount,
  onUndoCount,
  onOpenTransaction,
  onClose,
}: {
  financeTransaction: FinanceTransaction
  financeAccount?: FinanceAccount
  financeAccounts: FinanceAccount[]
  categoryOptions: { value: string; label: string }[]
  isCounting?: boolean
  onCategorize: (spendingCategoryId: string) => void
  onCount: () => void
  onUndoCount: () => void
  onOpenTransaction: (financeTransaction: FinanceTransaction) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const words = useFinanceWords()
  const metadata = providerMetadataText(financeTransaction.providerMetadata)
  const duplicateOfTransactionId = financeTransaction.duplicateOfTransactionId ?? ''
  // The counted copy of a duplicate, or the duplicates of a counted copy.
  // There is no query for one transaction, but a copy and its counted copy
  // posted on the same day, so that day's transactions hold it.
  const related = useQuery(
    async () => {
      if (duplicateOfTransactionId) {
        const answer = await graphql<{ FinanceTransactions: FinanceTransactionPage }>(FINANCE_TRANSACTIONS, {
          from: financeTransaction.postedOn,
          to: financeTransaction.postedOn,
          limit: 200,
        })
        return {
          countedCopy: answer.FinanceTransactions.financeTransactions.find((copy) => copy.id === duplicateOfTransactionId),
          duplicates: [] as FinanceTransaction[],
        }
      }
      const answer = await graphql<{ FinanceTransactions: FinanceTransactionPage }>(FINANCE_TRANSACTIONS, {
        duplicateOfTransactionId: financeTransaction.id,
        limit: 200,
      })
      return { countedCopy: undefined, duplicates: answer.FinanceTransactions.financeTransactions }
    },
    [financeTransaction.id, duplicateOfTransactionId],
    { refresh: false },
  )
  const countedCopy = related.data?.countedCopy
  const duplicates = related.data?.duplicates ?? []
  const copyLabel = (copy: FinanceTransaction) => {
    const account = financeAccounts.find((candidate) => candidate.id === copy.financeAccountId)
    return t('finance.copyOnAccount', {
      account: account ? accountLabel(account) : t('finance.deletedFinanceAccount'),
      day: formatDay(copy.postedOn),
    })
  }
  // Into the dialog when it opens, so the keyboard that opened it from its
  // row is in it: on the close button, out of the way of the fields.
  useEffect(() => {
    document
      .querySelector('.finance-transaction-details')
      ?.closest('.dialog')
      ?.querySelector<HTMLElement>('.dialog-actions button')
      ?.focus()
  }, [])
  const percent = new Intl.NumberFormat(undefined, { style: 'percent', maximumFractionDigits: 0 })
  const yesOrNo = (value: boolean) => (value ? t('common.yes') : t('common.no'))
  const categorizedBy = [
    financeTransaction.spendingCategoryId ? words.categorizedBy(financeTransaction.categorizedBy) : '',
    hasAmount(financeTransaction.categorizationConfidence)
      ? t('finance.confidence', {
          percent: percent.format(amountOf(financeTransaction.categorizationConfidence)),
        })
      : '',
  ]
    .filter(Boolean)
    .join(' · ')

  // One row of the list, left out when there is nothing to say.
  const property = (label: string, value: React.ReactNode, className?: string) =>
    value === undefined || value === null || value === '' ? null : (
      <>
        <dt>{label}</dt>
        <dd className={className}>{value}</dd>
      </>
    )

  return (
    <ConfirmDialog
      wide
      title={financeTransaction.merchantName || financeTransaction.description || t('finance.transactionDetails')}
      onClose={onClose}
      body={
        <div className="finance-transaction-details">
          <dl className="properties">
            {property(t('finance.postedOn'), formatDay(financeTransaction.postedOn))}
            {property(
              t('finance.transactedAt'),
              financeTransaction.transactedAt ? formatTime(financeTransaction.transactedAt) : '',
            )}
            {property(
              t('finance.amount'),
              `${formatMoney(amountOf(financeTransaction.amount), financeTransaction.currencyCode)} ${financeTransaction.currencyCode}`,
              duplicateOfTransactionId ? 'numeric-value finance-duplicate-amount' : 'numeric-value',
            )}
            {duplicateOfTransactionId ? (
              <>
                <dt>{t('finance.duplicateOf')}</dt>
                <dd>
                  {countedCopy ? (
                    <button /* link-button: goes to the counted copy, inline in a sentence */
                      type="button"
                      className="link"
                      onClick={() => onOpenTransaction(countedCopy)}
                    >
                      {copyLabel(countedCopy)}
                    </button>
                  ) : (
                    <span className="muted">{t('finance.countedCopy')}</span>
                  )}
                  <span className="muted finance-detail-note">{t('finance.duplicateHint')}</span>
                  <div className="finance-detail-actions">
                    <button type="button" disabled={isCounting} onClick={onCount}>
                      {t('finance.countThisOne')}
                    </button>
                  </div>
                </dd>
              </>
            ) : null}
            {financeTransaction.duplicateDecidedBy === 'person' ? (
              <>
                <dt>{t('finance.counting')}</dt>
                <dd>
                  {t('finance.countedByPerson')}
                  <div className="finance-detail-actions">
                    <button type="button" disabled={isCounting} onClick={onUndoCount}>
                      {t('finance.letDetectionDecide')}
                    </button>
                  </div>
                </dd>
              </>
            ) : null}
            {duplicates.length > 0 ? (
              <>
                <dt>{t('finance.duplicates')}</dt>
                <dd>
                  <ul className="finance-duplicate-list">
                    {duplicates.map((copy) => (
                      <li key={copy.id}>
                        <button /* link-button: goes to a duplicate, inline in a list of them */
                          type="button"
                          className="link"
                          onClick={() => onOpenTransaction(copy)}
                        >
                          {copyLabel(copy)}
                        </button>
                      </li>
                    ))}
                  </ul>
                  <span className="muted finance-detail-note">{t('finance.duplicatesHint')}</span>
                </dd>
              </>
            ) : null}
            {property(t('finance.merchant'), financeTransaction.merchantName)}
            {property(t('finance.fullDescription'), financeTransaction.description)}
            {property(
              t('finance.account'),
              financeAccount ? accountLabel(financeAccount) : t('finance.deletedFinanceAccount'),
            )}
            {property(t('finance.institution'), financeAccount?.institutionName ?? '')}
            <dt>{t('finance.spendingCategory')}</dt>
            <dd>
              <Select
                block
                value={financeTransaction.spendingCategoryId ?? ''}
                label={t('finance.spendingCategory')}
                options={[{ value: '', label: t('finance.uncategorized') }, ...categoryOptions]}
                onChange={onCategorize}
              />
              {categorizedBy ? <span className="muted finance-detail-note">{categorizedBy}</span> : null}
            </dd>
            {property(t('finance.providerCategoryPrimary'), financeTransaction.providerCategoryPrimary, 'mono')}
            {property(t('finance.providerCategoryDetailed'), financeTransaction.providerCategoryDetailed, 'mono')}
            {property(t('finance.pending'), yesOrNo(financeTransaction.isPending))}
            {property(t('finance.replacedPending'), financeTransaction.pendingProviderTransactionId, 'mono')}
            {property(t('finance.providerTransactionId'), financeTransaction.providerTransactionId, 'mono')}
            {property(t('finance.createdAt'), formatTime(financeTransaction.createdAt))}
            {property(t('finance.modifiedAt'), formatTime(financeTransaction.modifiedAt))}
          </dl>
          <div className="finance-metadata-head">
            <strong>{t('finance.providerMetadata')}</strong>
            {metadata ? (
              <CopyIconButton value={metadata} label={t('finance.copyProviderMetadata')} />
            ) : null}
          </div>
          <p className="muted field-hint">
            {metadata ? t('finance.providerMetadataHint') : t('finance.noProviderMetadata')}
          </p>
          {metadata ? <pre className="finance-metadata">{metadata}</pre> : null}
        </div>
      }
    />
  )
}
