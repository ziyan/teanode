import { useEffect } from 'react'

import { CopyIconButton, formatMoney, formatTime } from '../../components/common'
import { ConfirmDialog } from '../../components/dialog'
import { Select } from '../../components/select'
import { useTranslation } from '../../i18n/i18n'
import { FinanceAccount, FinanceTransaction, amountOf, formatDay, hasAmount } from './financeApi'
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
// categories and ids, who decided its spending category and its transfer
// mark, when it was added and changed), and the provider's whole record of
// it. The spending category can be changed here as in the row. What the
// provider wrote is shown as text and nothing else: it comes from whoever
// charged the account.
export function FinanceTransactionDialog({
  financeTransaction,
  financeAccount,
  categoryOptions,
  onCategorize,
  onClose,
}: {
  financeTransaction: FinanceTransaction
  financeAccount?: FinanceAccount
  categoryOptions: { value: string; label: string }[]
  onCategorize: (spendingCategoryId: string) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const words = useFinanceWords()
  const metadata = providerMetadataText(financeTransaction.providerMetadata)
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
              'numeric-value',
            )}
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
            {property(
              t('finance.transfer'),
              financeTransaction.isTransfer || financeTransaction.transferMarkedBy
                ? [yesOrNo(financeTransaction.isTransfer), words.transferMarkedBy(financeTransaction.transferMarkedBy)]
                    .filter(Boolean)
                    .join(' · ')
                : yesOrNo(false),
            )}
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
