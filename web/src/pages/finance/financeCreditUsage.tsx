import { useState } from 'react'

import { graphql } from '../../api'
import { ErrorMessage, formatMoney } from '../../components/common'
import { SettingsSection } from '../../components/settingsList'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { creditUsageSlices, usageTone } from './creditUsage'
import { CREDIT_USAGE, CreditUsage, amountOf } from './financeApi'
import { Money, UnconvertedNote, accountLabel } from './financeCommon'
import { SpendingRing, ringSliceClass } from './spendingRing'

// What the credit cards owe against their credit limits: a ring of what
// each card owes and of the credit still available, the share used in its
// middle, and beside it the same in figures. Under it a table of the cards,
// each with its slice's swatch, a table rather than cards on a phone for the
// same reason the accounts are. Nothing at all for a person with no credit
// card, who has nothing to read here.
export function FinanceCreditUsageSection({ refreshKey }: { refreshKey: number }) {
  const { t, plural } = useTranslation()
  const { data, error } = useQuery(() => graphql<{ CreditUsage: CreditUsage }>(CREDIT_USAGE), [refreshKey])
  const [highlightedKey, setHighlightedKey] = useState<string | null>(null)
  const usage = data?.CreditUsage
  if (!usage || usage.creditCards.length === 0) return <ErrorMessage error={error} />

  const currency = usage.reportingCurrencyCode ?? ''
  const percent = new Intl.NumberFormat(undefined, { style: 'percent', maximumFractionDigits: 0 })
  // A share that rounds to nothing but is not nothing says so, as the
  // shares of net worth do: a card with a few dollars on it is not at 0%.
  const shareWords = (share: number) =>
    share > 0 && share < 0.005 ? `<${percent.format(0.01)}` : percent.format(share)
  const slices = creditUsageSlices(usage, t('finance.creditAvailable'), accountLabel)
  const sliceIndexes = new Map(slices.map((slice, index) => [slice.key, index]))
  const isDerivedShown = usage.creditCards.some((card) => card.creditLimitSource === 'derived')
  const hasShare = usage.usageShare !== undefined && usage.usageShare !== null
  const usageShareText = shareWords(usage.usageShare ?? 0)
  const usageShareTone = usageTone(usage.usageShare ?? 0)

  return (
    <SettingsSection card title={t('finance.creditUsageTitle')} description={t('finance.creditUsageHint')}>
      <ErrorMessage error={error} />
      {hasShare ? (
        <div className="finance-worth-summary">
          {slices.length > 0 ? (
            <SpendingRing
              slices={slices}
              currency={currency}
              label={t('finance.creditUsageRingLabel', { share: usageShareText })}
              totalLabel={t('finance.creditUsed')}
              totalText={usageShareText}
              totalTone={usageShareTone}
              highlightedKey={highlightedKey}
              isSliceShareSpoken={false}
            />
          ) : null}
          <dl className="finance-worth-line">
            <dt>{t('finance.creditOwed')}</dt>
            <dd>{formatMoney(amountOf(usage.totalOwedAmount), currency)}</dd>
            <dt>{t('finance.creditAvailable')}</dt>
            <dd>
              {formatMoney(
                Math.max(amountOf(usage.totalCreditLimitAmount) - amountOf(usage.totalOwedAmount), 0),
                currency,
              )}
            </dd>
            <dt>{t('finance.creditLimit')}</dt>
            <dd>
              <strong>{formatMoney(amountOf(usage.totalCreditLimitAmount), currency)}</strong>
            </dd>
            <dt>{t('finance.creditUsedShare')}</dt>
            <dd>
              <span className={`finance-credit-share ${usageShareTone}`}>{usageShareText}</span>
            </dd>
          </dl>
        </div>
      ) : null}
      <UnconvertedNote currencyCodes={usage.unconvertedCurrencyCodes} />
      <div className="table-wrap">
        <table className="numbers-table finance-table finance-credit-table">
          <thead>
            <tr>
              <th>{t('finance.creditCard')}</th>
              <th className="numeric">{t('finance.creditOwed')}</th>
              <th className="numeric">{t('finance.creditLimit')}</th>
              <th className="numeric">{t('finance.creditUsedShare')}</th>
            </tr>
          </thead>
          <tbody>
            {usage.creditCards.map((card) => {
              const index = sliceIndexes.get(card.financeAccountId)
              const share = card.usageShare
              return (
                <tr
                  key={card.financeAccountId}
                  onPointerEnter={index !== undefined ? () => setHighlightedKey(card.financeAccountId) : undefined}
                  onPointerLeave={index !== undefined ? () => setHighlightedKey(null) : undefined}
                >
                  <td>
                    {index !== undefined ? (
                      <i
                        className={`spending-ring-swatch ${ringSliceClass(slices[index], index)}`}
                        aria-hidden="true"
                      />
                    ) : null}
                    <span className="finance-asset-name">{accountLabel(card)}</span>
                    {card.institutionName ? (
                      <span className="muted finance-credit-institution">{card.institutionName}</span>
                    ) : null}
                  </td>
                  <td className="numeric">
                    <Money amount={card.owedAmount} currency={card.currencyCode} />
                  </td>
                  <td className="numeric">
                    {card.creditLimitSource === 'unknown' ? (
                      <span className="muted">{t('finance.creditLimitUnknown')}</span>
                    ) : (
                      <>
                        <Money amount={card.creditLimitAmount} currency={card.currencyCode} />
                        {card.creditLimitSource === 'derived' ? (
                          <span className="muted finance-credit-mark">*</span>
                        ) : null}
                      </>
                    )}
                  </td>
                  <td className="numeric">
                    {share !== undefined && share !== null ? (
                      <span className={`finance-credit-share ${usageTone(share)}`}>{shareWords(share)}</span>
                    ) : (
                      <span className="muted">—</span>
                    )}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
      {isDerivedShown ? <p className="muted field-hint">{t('finance.creditLimitDerivedNote')}</p> : null}
      {usage.leftOutCardCount > 0 ? (
        <p className="muted field-hint">
          {plural(
            usage.leftOutCardCount,
            { one: 'finance.creditLeftOutOne', other: 'finance.creditLeftOutOther' },
            { count: String(usage.leftOutCardCount), amount: formatMoney(amountOf(usage.leftOutOwedAmount), currency) },
          )}
        </p>
      ) : null}
    </SettingsSection>
  )
}
