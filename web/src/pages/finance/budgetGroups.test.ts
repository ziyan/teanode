import { describe, expect, it } from 'vitest'

import {
  budgetAmountSince,
  formatSigned,
  groupedCategoryOptions,
  reachTone,
  savingMeter,
  spendingForecastParts,
  splitByIncome,
} from './budgetGroups'
import type { Budget, SavingSummary, SpendingCategory } from './financeApi'

function category(id: string, isIncome: boolean): SpendingCategory {
  return { id, spendingCategoryName: id, isIncome, isHidden: false } as SpendingCategory
}

const categories = [category('dining', false), category('salary', true), category('travel', false)]

function summary(overrides: Partial<SavingSummary>): SavingSummary {
  return {
    month: '2026-06',
    asOf: '2026-06-15',
    dayOfMonth: 15,
    daysInMonth: 30,
    year: '',
    monthsElapsedCount: 0,
    dayOfYear: 0,
    daysInYear: 0,
    budgetedMonths: ['2026-06'],
    budgetedMonthCount: 1,
    budgetedMonthsElapsedCount: 1,
    reportingCurrencyCode: 'USD',
    incomeBudgetCount: 1,
    spendingBudgetCount: 2,
    expectedIncomeAmount: '4000.0000',
    expectedSpendingAmount: '3000.0000',
    expectedSavingAmount: '1000.0000',
    incomeAmount: '2000.0000',
    spendingAmount: '1500.0000',
    savingAmount: '500.0000',
    projectedIncomeAmount: '4000.0000',
    projectedSpendingAmount: '3200.0000',
    projectedSavingAmount: '800.0000',
    savingDifferenceAmount: '-200.0000',
    savingPace: 'on_track',
    unconvertedCurrencyCodes: [],
    ...overrides,
  }
}

describe('splitByIncome', () => {
  it('puts budgets on income categories apart, keeping the order', () => {
    const rows = [{ spendingCategoryId: 'travel' }, { spendingCategoryId: 'salary' }, { spendingCategoryId: 'dining' }]
    const { spending, income } = splitByIncome(rows, categories)
    expect(spending.map((row) => row.spendingCategoryId)).toEqual(['travel', 'dining'])
    expect(income.map((row) => row.spendingCategoryId)).toEqual(['salary'])
  })

  // A budget on a spending category since deleted stays where it was.
  it('keeps a row whose category is gone with the spending', () => {
    const { spending, income } = splitByIncome([{ spendingCategoryId: 'gone' }], categories)
    expect(spending).toHaveLength(1)
    expect(income).toHaveLength(0)
  })
})

describe('groupedCategoryOptions', () => {
  it('lists the spending categories first and the income ones after, each named by group', () => {
    const options = [
      { value: 'dining', label: 'Dining' },
      { value: 'salary', label: 'Salary' },
      { value: 'travel', label: 'Travel' },
    ]
    expect(groupedCategoryOptions(options, categories, { spending: 'Spending', income: 'Income' })).toEqual([
      { value: 'dining', label: 'Dining', group: 'Spending' },
      { value: 'travel', label: 'Travel', group: 'Spending' },
      { value: 'salary', label: 'Salary', group: 'Income' },
    ])
  })
})

describe('reachTone', () => {
  // Falling short is what is worth a look, for income and for saving.
  it('colors behind and nothing else', () => {
    expect(reachTone('behind')).toBe('warn')
    expect(reachTone('on_track')).toBe('good')
    expect(reachTone('ahead')).toBe('good')
  })
})

describe('savingMeter', () => {
  it('is the saving so far against the expected saving, with the projection as the forecast', () => {
    expect(savingMeter(summary({}), false)).toEqual({ fraction: 0.5, forecast: 0.8 })
  })

  it('has no forecast once the month is over', () => {
    expect(savingMeter(summary({}), true)).toEqual({ fraction: 0.5, forecast: null })
  })

  // A month spending more than came in has saved nothing, not less than
  // nothing, as far as a bar can say.
  it('does not draw below zero', () => {
    expect(savingMeter(summary({ savingAmount: '-300.0000', projectedSavingAmount: '-100.0000' }), false)).toEqual({
      fraction: 0,
      forecast: 0,
    })
  })

  it('is nothing when the budgets expect nothing saved', () => {
    expect(savingMeter(summary({ expectedSavingAmount: '0.0000' }), false)).toBeNull()
    expect(savingMeter(summary({ expectedSavingAmount: '-500.0000' }), false)).toBeNull()
  })
})

describe('spendingForecastParts', () => {
  it('splits the projection into spent, repeat charges still to come and the rest at this pace', () => {
    expect(
      spendingForecastParts({
        spendingAmount: '120.0000',
        fixedChargesDueAmount: '45.0000',
        projectedAmount: '405.0000',
      }),
    ).toEqual({ spentAmount: 120, repeatChargesAmount: 45, atPaceAmount: 240 })
  })

  // The first of the month with nothing spent: the projection is only the
  // repeat charges, and nothing is carried on at a pace of nothing.
  it('has nothing at pace when nothing was spent', () => {
    expect(
      spendingForecastParts({ spendingAmount: '0.0000', fixedChargesDueAmount: '60.0000', projectedAmount: '60.0000' }),
    ).toEqual({ spentAmount: 0, repeatChargesAmount: 60, atPaceAmount: 0 })
  })

  it('never says a negative amount at pace', () => {
    expect(
      spendingForecastParts({ spendingAmount: '-20.0000', fixedChargesDueAmount: '30.0000', projectedAmount: '0.0000' })
        .atPaceAmount,
    ).toBe(0)
  })
})

describe('formatSigned', () => {
  it('puts a plus in front of more and leaves less and nothing as they are', () => {
    expect(formatSigned(200, 'USD')).toMatch(/^\+/)
    expect(formatSigned(-200, 'USD')).not.toMatch(/^\+/)
    expect(formatSigned(0, 'USD')).not.toMatch(/^\+/)
  })
})

describe('budgetAmountSince', () => {
  const row = (id: string, spendingCategoryId: string, monthlyAmount: string, effectiveFrom: string): Budget => ({
    id,
    spendingCategoryId,
    monthlyAmount,
    currencyCode: 'USD',
    effectiveFrom,
  })
  const budgets = [
    row('a', 'dining', '400.0000', '2026-01-01'),
    row('b', 'dining', '400', '2026-09-01'),
    row('c', 'travel', '200.0000', '2026-03-01'),
    row('d', 'travel', '250.0000', '2026-06-01'),
  ]

  it('names the month an unbroken run of the same amount began', () => {
    expect(budgetAmountSince(budgets[1], budgets)).toBe('2026-01-01')
  })

  it('stops at an earlier row with another amount', () => {
    expect(budgetAmountSince(budgets[3], budgets)).toBe('2026-06-01')
  })

  it('looks only at the same spending category', () => {
    expect(budgetAmountSince(budgets[2], budgets)).toBe('2026-03-01')
  })
})
