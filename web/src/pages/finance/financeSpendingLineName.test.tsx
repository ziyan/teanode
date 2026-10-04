import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { FinanceSpendingSection } from './financeSpending'

vi.mock('../../api', () => ({ graphql: vi.fn() }))
vi.mock('../../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) => (values ? `${key} ${JSON.stringify(values)}` : key),
  }),
}))
vi.mock('../../components/toast', () => ({ useToast: () => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }) }))
const execute = vi.mocked(graphql)

// A month whose spending is all in the built-in other category, still
// under its built-in name, beside a category the person named themselves.
function answer(query: string): unknown {
  if (query.includes('FinanceSpendingSummary(')) {
    return {
      FinanceSpendingSummary: {
        groupBy: 'spendingCategory',
        spendingSummaryRows: [
          {
            groupKey: 'category-other',
            groupLabel: 'other',
            currencyCode: 'USD',
            moneyOut: '40.0000',
            moneyIn: '0.0000',
            financeTransactionCount: 2,
          },
          {
            groupKey: 'category-hobbies',
            groupLabel: 'Hobbies',
            currencyCode: 'USD',
            moneyOut: '25.0000',
            moneyIn: '0.0000',
            financeTransactionCount: 1,
          },
        ],
        currencyTotals: [],
        convertedSpendingSummaryRows: [],
        unconvertedCurrencyCodes: [],
      },
    }
  }
  if (query.includes('SpendingCategories')) {
    return {
      SpendingCategories: [
        { id: 'category-other', spendingCategoryName: 'other', isIncome: false, isHidden: false, isOther: true },
        { id: 'category-hobbies', spendingCategoryName: 'Hobbies', isIncome: false, isHidden: false },
      ],
    }
  }
  if (query.includes('CashFlow(')) {
    return { CashFlow: { cashFlowMonths: [], unconvertedCurrencyCodes: [] } }
  }
  if (query.includes('SpendingByDay(')) {
    return {
      SpendingByDay: { month: '', compareMonth: '', monthDays: [], compareMonthDays: [], unconvertedCurrencyCodes: [] },
    }
  }
  if (query.includes('BudgetStatus(')) {
    return {
      BudgetStatus: {
        month: '',
        asOf: '',
        dayOfMonth: 14,
        daysInMonth: 31,
        spendingCategories: [],
        incomeCategories: [],
      },
    }
  }
  if (query.includes('SavingSummary(')) {
    return { SavingSummary: { incomeBudgetCount: 0, spendingBudgetCount: 0, unconvertedCurrencyCodes: [] } }
  }
  return {}
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2031-05-14T12:00:00'))
  execute.mockImplementation((query: string) => Promise.resolve(answer(query)) as never)
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  )
})

afterEach(() => {
  cleanup()
  execute.mockReset()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

// A line's key is its group and currency together, so the category it is
// named after is found by its group: the built-in other reads in the
// reader's words, and a person's own name as they wrote it.
it("names the built-in other line in the reader's words", async () => {
  render(
    <MemoryRouter initialEntries={['/finance/spending']}>
      <FinanceSpendingSection />
    </MemoryRouter>,
  )
  expect((await screen.findAllByText('finance.builtInSpendingCategory.other')).length).toBeGreaterThan(0)
  expect(screen.getAllByText('Hobbies').length).toBeGreaterThan(0)
  expect(screen.queryByText('other')).toBeNull()
})
