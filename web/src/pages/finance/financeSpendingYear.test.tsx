import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, useLocation, useNavigate } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { FinanceSpendingSection, usualBudgetedMonthCount } from './financeSpending'

vi.mock('../../api', () => ({ graphql: vi.fn() }))
vi.mock('../../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) => (values ? `${key} ${JSON.stringify(values)}` : key),
  }),
}))
vi.mock('../../components/toast', () => ({ useToast: () => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }) }))
const execute = vi.mocked(graphql)

// The months the history has money in: March 2028, August 2030 and
// February 2031, so the years run 2028 to 2031.
const historyMonths = [
  { cashFlowMonth: '2028-03', incomeAmount: '0.0000', spendingAmount: '120.0000', netAmount: '-120.0000' },
  { cashFlowMonth: '2030-08', incomeAmount: '2500.0000', spendingAmount: '900.0000', netAmount: '1600.0000' },
  { cashFlowMonth: '2031-02', incomeAmount: '2500.0000', spendingAmount: '1000.0000', netAmount: '1500.0000' },
]

// What each question is answered with: cash flow in a few months and
// nothing budgeted, so the budget panels draw their empty states.
function answer(query: string, variables?: Record<string, string>): unknown {
  if (query.includes('CashFlow(')) {
    const fromMonth = variables?.fromMonth ?? ''
    const toMonth = variables?.toMonth ?? ''
    return {
      CashFlow: {
        fromMonth,
        toMonth,
        reportingCurrencyCode: 'USD',
        cashFlowMonths: historyMonths.filter(
          (month) => month.cashFlowMonth >= fromMonth && month.cashFlowMonth <= toMonth,
        ),
        unconvertedCurrencyCodes: [],
      },
    }
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
  if (query.includes('FinanceSpendingSummary(')) {
    return {
      FinanceSpendingSummary: {
        groupBy: 'spendingCategory',
        spendingSummaryRows: [],
        currencyTotals: [],
        convertedSpendingSummaryRows: [],
        unconvertedCurrencyCodes: [],
      },
    }
  }
  return { SpendingCategories: [] }
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2031-05-14T12:00:00'))
  execute.mockImplementation(
    (query: string, variables?: Record<string, unknown>) =>
      Promise.resolve(answer(query, variables as Record<string, string> | undefined)) as never,
  )
  // jsdom lays nothing out, and a chart draws nothing at a width of zero,
  // so the charts are given a width and the observer a stand-in.
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
    width: 600,
    height: 220,
    top: 0,
    left: 0,
    right: 600,
    bottom: 220,
    x: 0,
    y: 0,
    toJSON: () => ({}),
  })
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

// Where the page is, and a way Back, as the browser's button would go.
function Probe() {
  const location = useLocation()
  const navigate = useNavigate()
  return (
    <>
      <output data-testid="search">{location.search}</output>
      <button type="button" onClick={() => navigate(-1)}>
        back
      </button>
    </>
  )
}

function renderAt(path: string) {
  render(
    <MemoryRouter initialEntries={['/finance/transactions', path]} initialIndex={1}>
      <FinanceSpendingSection />
      <Probe />
    </MemoryRouter>,
  )
}

const search = () => screen.getByTestId('search').textContent
const variablesOf = (operation: string) =>
  execute.mock.calls.filter(([query]) => query.includes(`${operation}(`)).map(([, variables]) => variables)

it('switches to Year, puts the year in the address, and asks for the year', async () => {
  renderAt('/finance/spending')
  fireEvent.click(await screen.findByRole('button', { name: 'finance.year' }))
  await waitFor(() => expect(search()).toBe('?year=2031'))
  await waitFor(() => expect(variablesOf('BudgetStatus')).toContainEqual({ year: '2031' }))
  expect(variablesOf('SavingSummary')).toContainEqual({ year: '2031' })
  // Every year there is, read once for the chart of years and the menus.
  expect(variablesOf('CashFlow')).toContainEqual({ fromMonth: '2012-01', toMonth: '2031-05' })
  // The year in progress is read to today.
  expect(variablesOf('FinanceSpendingSummary')).toContainEqual(
    expect.objectContaining({ from: '2031-01-01', to: '2031-05-14' }),
  )
  expect(screen.getByRole('button', { name: 'finance.year' }).getAttribute('aria-pressed')).toBe('true')
})

// Changing the kind is a step in history, so Back returns to the month;
// stepping the year replaces the address, as choosing a month does.
it('goes Back from a year to the month, and steps a year without a history entry', async () => {
  renderAt('/finance/spending?month=2030-08')
  fireEvent.click(await screen.findByRole('button', { name: 'finance.year' }))
  await waitFor(() => expect(search()).toBe('?year=2030'))
  fireEvent.click(screen.getByRole('button', { name: 'finance.previousYear' }))
  await waitFor(() => expect(search()).toBe('?year=2029'))
  await waitFor(() =>
    expect(variablesOf('FinanceSpendingSummary')).toContainEqual(
      expect.objectContaining({ from: '2029-01-01', to: '2029-12-31' }),
    ),
  )
  fireEvent.click(screen.getByRole('button', { name: 'back' }))
  await waitFor(() => expect(search()).toBe('?month=2030-08'))
  expect(screen.getByRole('button', { name: 'finance.month' }).getAttribute('aria-pressed')).toBe('true')
})

it('opens a linked year, and goes from it to its latest month', async () => {
  renderAt('/finance/spending?year=2029')
  const next = await screen.findByRole('button', { name: 'finance.nextYear' })
  expect(next.hasAttribute('disabled')).toBe(false)
  fireEvent.click(screen.getByRole('button', { name: 'finance.month' }))
  await waitFor(() => expect(search()).toBe('?month=2029-12'))
})

it('does not step past this year', async () => {
  renderAt('/finance/spending?year=2031')
  const next = await screen.findByRole('button', { name: 'finance.nextYear' })
  expect(next.hasAttribute('disabled')).toBe(true)
})

// The chart of years runs from the first year with money in or out to this
// one, this one marked as so far, and choosing a year group shows it.
it('draws a group a year and chooses the year clicked', async () => {
  renderAt('/finance/spending?year=2031')
  const groups = await screen.findAllByRole('button', { name: /^(2028|2029|2030|finance\.yearSoFar)/ })
  expect(groups.map((group) => group.getAttribute('aria-label')?.split(': ')[0])).toEqual([
    '2028',
    '2029',
    '2030',
    'finance.yearSoFar {"year":"2031"}',
  ])
  expect(groups[3].getAttribute('aria-pressed')).toBe('true')
  fireEvent.click(groups[2])
  await waitFor(() => expect(search()).toBe('?year=2030'))
  await waitFor(() => expect(variablesOf('BudgetStatus')).toContainEqual({ year: '2030' }))
})

// Month or Year and the period sit in the one row above the panels in
// both modes, never inside a card.
it('keeps Month or Year in the same row above the panels in both modes', async () => {
  renderAt('/finance/spending?month=2031-02')
  const monthKind = await screen.findByRole('button', { name: 'finance.month' })
  expect(monthKind.closest('.finance-period-bar')).not.toBeNull()
  expect(monthKind.closest('.card')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'finance.year' }))
  await waitFor(() => expect(search()).toBe('?year=2031'))
  const yearKind = screen.getByRole('button', { name: 'finance.year' })
  expect(yearKind.closest('.finance-period-bar')).not.toBeNull()
  expect(yearKind.closest('.card')).toBeNull()
  // A month steps back to the one before, as far as the first with money.
  fireEvent.click(screen.getByRole('button', { name: 'finance.month' }))
  await waitFor(() => expect(search()).toBe(''))
  fireEvent.click(screen.getByRole('button', { name: 'finance.previousMonth' }))
  await waitFor(() => expect(search()).toBe('?month=2031-04'))
})

// The months most budgets covered are said once; a row says its own only
// when it differs.
it('finds how many months most budgets were in force', () => {
  const rows = (...counts: number[]) => counts.map((budgetedMonthCount) => ({ budgetedMonthCount }))
  expect(usualBudgetedMonthCount(rows(4, 4, 12))).toBe(4)
  expect(usualBudgetedMonthCount(rows(12, 12, 3))).toBe(12)
  expect(usualBudgetedMonthCount(rows(4, 6))).toBe(6)
  expect(usualBudgetedMonthCount([])).toBe(12)
})
