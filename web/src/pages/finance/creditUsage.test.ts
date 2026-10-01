import { describe, expect, it } from 'vitest'

import { AVAILABLE_SLICE_KEY, creditUsageSlices, isCountedCard, usageTone } from './creditUsage'
import { CreditCardUsage, CreditUsage } from './financeApi'

describe('usageTone', () => {
  it('is good under 30%, a warning from 30% to 50%, and bad above', () => {
    expect(usageTone(0)).toBe('good')
    expect(usageTone(0.2949)).toBe('good')
    expect(usageTone(0.3)).toBe('warn')
    expect(usageTone(0.5)).toBe('warn')
    expect(usageTone(0.5051)).toBe('bad')
    expect(usageTone(1.2)).toBe('bad')
  })

  // The tone is that of the share as it is shown, to the whole percent.
  it('judges a share as it is rounded to be shown', () => {
    expect(usageTone(0.2996)).toBe('warn')
    expect(usageTone(0.5001)).toBe('warn')
    expect(usageTone(0.5049)).toBe('warn')
  })
})

function card(overrides: Partial<CreditCardUsage>): CreditCardUsage {
  return {
    financeAccountId: 'card',
    accountName: 'Example Card',
    currencyCode: 'USD',
    owedAmount: '100.0000',
    creditLimitAmount: '1000.0000',
    creditLimitSource: 'derived',
    usageShare: 0.1,
    convertedOwedAmount: '100.0000',
    convertedCreditLimitAmount: '1000.0000',
    ...overrides,
  }
}

function usage(creditCards: CreditCardUsage[], totalOwedAmount: string, totalCreditLimitAmount: string): CreditUsage {
  return {
    reportingCurrencyCode: 'USD',
    unconvertedCurrencyCodes: [],
    totalOwedAmount,
    totalCreditLimitAmount,
    usageShare: Number(totalOwedAmount) / Number(totalCreditLimitAmount),
    leftOutCardCount: 0,
    leftOutOwedAmount: '0.0000',
    creditCards,
  }
}

describe('isCountedCard', () => {
  it('counts a card only with a share and both amounts converted', () => {
    expect(isCountedCard(card({}))).toBe(true)
    expect(isCountedCard(card({ usageShare: null, creditLimitAmount: null, creditLimitSource: 'unknown' }))).toBe(false)
    expect(isCountedCard(card({ convertedOwedAmount: null, convertedCreditLimitAmount: null }))).toBe(false)
  })
})

describe('creditUsageSlices', () => {
  const labelOf = (counted: CreditCardUsage) => counted.accountName

  it('draws a slice per card owed, then the credit still available', () => {
    const slices = creditUsageSlices(
      usage(
        [
          card({ financeAccountId: 'first', accountName: 'Card A', convertedOwedAmount: '600.0000' }),
          card({ financeAccountId: 'second', accountName: 'Card B', convertedOwedAmount: '250.0000' }),
        ],
        '850.0000',
        '2000.0000',
      ),
      'Available',
      labelOf,
    )
    expect(slices.map((slice) => [slice.key, slice.label, slice.amount, slice.isOther])).toEqual([
      ['first', 'Card A', 600, false],
      ['second', 'Card B', 250, false],
      [AVAILABLE_SLICE_KEY, 'Available', 1150, true],
    ])
  })

  it('leaves out a card that owes nothing and one that is not counted', () => {
    const slices = creditUsageSlices(
      usage(
        [
          card({ financeAccountId: 'paid', convertedOwedAmount: '0.0000', usageShare: 0 }),
          card({ financeAccountId: 'unknown', usageShare: null, convertedCreditLimitAmount: null }),
          card({ financeAccountId: 'owing', convertedOwedAmount: '100.0000' }),
        ],
        '100.0000',
        '2000.0000',
      ),
      'Available',
      labelOf,
    )
    expect(slices.map((slice) => slice.key)).toEqual(['owing', AVAILABLE_SLICE_KEY])
  })

  it('has nothing available past the limit', () => {
    const slices = creditUsageSlices(
      usage([card({ convertedOwedAmount: '1100.0000', usageShare: 1.1 })], '1100.0000', '1000.0000'),
      'Available',
      labelOf,
    )
    expect(slices.map((slice) => slice.key)).toEqual(['card'])
  })
})
