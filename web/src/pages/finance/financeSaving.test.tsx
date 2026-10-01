import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { SavingSummaryPanel } from './financeSaving'
import type { SavingSummary } from './financeApi'

vi.mock('../../api', () => ({ graphql: vi.fn() }))
vi.mock('../../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) => (values ? `${key} ${JSON.stringify(values)}` : key),
  }),
}))
vi.mock('../../components/toast', () => ({ useToast: () => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }) }))
const execute = vi.mocked(graphql)

afterEach(() => {
  cleanup()
  execute.mockReset()
})

// A year as of a day in October, whose figures the test chooses.
function yearSummary(overrides: Partial<SavingSummary>): SavingSummary {
  return {
    month: '',
    asOf: '2031-10-15',
    dayOfMonth: 15,
    daysInMonth: 31,
    year: '2031',
    monthsElapsedCount: 10,
    dayOfYear: 288,
    daysInYear: 365,
    budgetedMonths: [],
    budgetedMonthCount: 0,
    budgetedMonthsElapsedCount: 0,
    reportingCurrencyCode: 'USD',
    incomeBudgetCount: 0,
    spendingBudgetCount: 0,
    expectedIncomeAmount: '0.0000',
    expectedSpendingAmount: '0.0000',
    expectedSavingAmount: '0.0000',
    incomeAmount: '0.0000',
    spendingAmount: '0.0000',
    savingAmount: '0.0000',
    projectedIncomeAmount: '0.0000',
    projectedSpendingAmount: '0.0000',
    projectedSavingAmount: '0.0000',
    savingDifferenceAmount: '0.0000',
    savingPace: 'on_track',
    unconvertedCurrencyCodes: [],
    ...overrides,
  }
}

function drawYear(summary: SavingSummary) {
  execute.mockResolvedValue({ SavingSummary: summary } as never)
  render(<SavingSummaryPanel month="2031-10" year="2031" />)
}

// Budgets from September: the panel says the saving is over September to
// December, in its description and on the bar's label, rather than over
// the year from January.
it('names the months with budgets that a year counts', async () => {
  drawYear(
    yearSummary({
      budgetedMonths: ['2031-09', '2031-10', '2031-11', '2031-12'],
      budgetedMonthCount: 4,
      budgetedMonthsElapsedCount: 2,
      incomeBudgetCount: 1,
      spendingBudgetCount: 2,
      expectedIncomeAmount: '12000.0000',
      expectedSpendingAmount: '1600.0000',
      expectedSavingAmount: '10400.0000',
      incomeAmount: '6000.0000',
      spendingAmount: '600.0000',
      savingAmount: '5400.0000',
    }),
  )
  const covered = { count: 4, from: 'Sep', to: 'Dec' }
  expect(
    await screen.findByText(`finance.savingYearMonthsHint ${JSON.stringify({ year: '2031', ...covered })}`),
  ).toBeTruthy()
  expect(screen.getByText(`finance.savedSoFarInMonths ${JSON.stringify({ from: 'Sep', to: 'Dec' })}`)).toBeTruthy()
  expect(screen.queryByText(/finance\.savingYearHint /)).toBeNull()
})

// A year whose budgets covered all twelve months reads as the year did.
it('says the whole year when every month had budgets', async () => {
  drawYear(
    yearSummary({
      budgetedMonths: Array.from({ length: 12 }, (_, index) => `2031-${String(index + 1).padStart(2, '0')}`),
      budgetedMonthCount: 12,
      budgetedMonthsElapsedCount: 10,
      incomeBudgetCount: 1,
      spendingBudgetCount: 1,
      expectedSavingAmount: '20000.0000',
    }),
  )
  expect(await screen.findByText(/^finance\.savingYearHint /)).toBeTruthy()
  expect(screen.getByText('finance.savedSoFar')).toBeTruthy()
})

// No budget in any month: the year's income, spending and what was left,
// with no expected figure and no pace.
it('shows a year with no budgets as what came in and went out', async () => {
  drawYear(yearSummary({ incomeAmount: '41000.0000', spendingAmount: '30500.0000', savingAmount: '10500.0000' }))
  expect(await screen.findByText(`finance.noSavingBudgetsYear ${JSON.stringify({ year: '2031' })}`)).toBeTruthy()
  expect(screen.getByText('finance.leftOver')).toBeTruthy()
  expect(screen.queryByText('finance.budgeted')).toBeNull()
  expect(screen.queryByText('finance.savingPace.on_track')).toBeNull()
})
