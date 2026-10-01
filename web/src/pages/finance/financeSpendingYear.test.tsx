import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, useLocation, useNavigate } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { monthLabel } from './financeApi'
import {
  BudgetedMonths,
  FinanceSpendingSection,
  budgetStatusYearHint,
  budgetedMonthsNote,
  usualBudgetedMonths,
} from './financeSpending'
import { monthStartLabel, yearEndLabel } from './spendingYear'

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

// The year's spending budget rows a test answers BudgetStatus with: none
// unless a test sets some.
let spendingBudgetRows: unknown[] = []

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
        spendingCategories: spendingBudgetRows,
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
  spendingBudgetRows = []
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
// one, this one marked as so far and the first, which starts in March,
// marked with its month, and choosing a year group shows it.
it('draws a group a year and chooses the year clicked', async () => {
  renderAt('/finance/spending?year=2031')
  const groups = await screen.findAllByRole('button', {
    name: /^(finance\.yearFrom|2029|2030|finance\.yearIncomeFrom|finance\.yearSoFar)/,
  })
  const labels = groups.map((group) => group.getAttribute('aria-label')?.split(': ')[0])
  expect(labels[0]).toBe(`finance.yearFrom {"year":"2028","month":"${monthLabel('2028-03')}"}`)
  expect(labels[1]).toBe('2029')
  // Income first comes in partway through 2030 in this history, which the
  // year's label says rather than leaving it to read as a loss year.
  expect(labels[2]).toMatch(/^(2030|finance\.yearIncomeFrom \{"year":"2030")/)
  expect(labels[3]).toBe('finance.yearSoFar {"year":"2031"}')
  expect(labels).toHaveLength(4)
  expect(groups[3].getAttribute('aria-pressed')).toBe('true')
  fireEvent.click(groups[2])
  await waitFor(() => expect(search()).toBe('?year=2030'))
  await waitFor(() => expect(variablesOf('BudgetStatus')).toContainEqual({ year: '2030' }))
})

// A first year whose history starts in March is not a whole year: chosen,
// its totals say they run from the first of March to the end of December.
it('says a partial first year runs from its first month', async () => {
  renderAt('/finance/spending?year=2028')
  const range = `finance.yearRange {"from":"${monthStartLabel('2028-03')}","to":"${yearEndLabel('2028')}"}`
  expect(await screen.findByText(range)).toBeTruthy()
  expect(
    screen.getByText(`finance.spentInRange {"from":"${monthStartLabel('2028-03')}","to":"${yearEndLabel('2028')}"}`),
  ).toBeTruthy()
  // A whole year past says neither.
  cleanup()
  renderAt('/finance/spending?year=2030')
  expect(await screen.findByText('finance.spentInYear {"year":"2030"}')).toBeTruthy()
  expect(screen.queryByText(/finance\.yearRange/)).toBeNull()
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

const budgetedMonths = (budgetedMonthCount: number, firstBudgetedMonth: string, lastBudgetedMonth: string) => ({
  budgetedMonthCount,
  firstBudgetedMonth,
  lastBudgetedMonth,
})
const translate = ((key: string, values?: Record<string, unknown>) =>
  values ? `${key} ${JSON.stringify(values)}` : key) as Parameters<typeof budgetStatusYearHint>[0]

// The months most budgets covered are named once; a row names its own
// only when they differ.
it('finds the months most budgets were in force', () => {
  const fromSeptember = budgetedMonths(4, '2030-09', '2030-12')
  const all: BudgetedMonths = budgetedMonths(12, '2030-01', '2030-12')
  expect(usualBudgetedMonths([fromSeptember, fromSeptember, all])).toEqual(fromSeptember)
  expect(usualBudgetedMonths([all, all, fromSeptember]).budgetedMonthCount).toBe(12)
  // Four months from September and four from March are not the same months.
  const fromMarch = budgetedMonths(4, '2030-03', '2030-06')
  expect(usualBudgetedMonths([fromSeptember, fromMarch, fromMarch])).toEqual(fromMarch)
  // Of two as common, the more months.
  expect(usualBudgetedMonths([fromSeptember, budgetedMonths(6, '2030-07', '2030-12')]).budgetedMonthCount).toBe(6)
  expect(usualBudgetedMonths([]).budgetedMonthCount).toBe(12)
})

it('names the months most budgets counted in the description, and a row its own when they differ', () => {
  const fromSeptember = budgetedMonths(4, '2030-09', '2030-12')
  const september = monthLabel('2030-09')
  const december = monthLabel('2030-12')
  expect(budgetStatusYearHint(translate, '2030', true, fromSeptember)).toBe(
    `finance.budgetStatusYearMonthsHintPast {"year":"2030","count":4,"from":"${september}","to":"${december}"}`,
  )
  expect(budgetStatusYearHint(translate, '2031', false, budgetedMonths(1, '2031-05', '2031-05'))).toBe(
    `finance.budgetStatusYearMonthHint {"year":"2031","month":"${monthLabel('2031-05')}"}`,
  )
  // A whole year is counted from the first of January.
  expect(budgetStatusYearHint(translate, '2030', true, budgetedMonths(12, '2030-01', '2030-12'))).toBe(
    'finance.budgetStatusYearHintPast {"year":"2030"}',
  )
  expect(budgetedMonthsNote(translate, fromSeptember, fromSeptember)).toBeNull()
  expect(budgetedMonthsNote(translate, budgetedMonths(2, '2030-11', '2030-12'), fromSeptember)).toBe(
    `finance.budgetedMonths {"count":2,"from":"${monthLabel('2030-11')}","to":"${december}"}`,
  )
  expect(budgetedMonthsNote(translate, budgetedMonths(1, '2030-10', '2030-10'), fromSeptember)).toBe(
    `finance.budgetedOneMonth {"month":"${monthLabel('2030-10')}"}`,
  )
  expect(budgetedMonthsNote(translate, budgetedMonths(12, '2030-01', '2030-12'), fromSeptember)).toBe(
    'finance.budgetedAllMonths',
  )
})

// The year's budgets panel says over which months it counts, the ones
// most of its budgets were in force, not from the first of January.
it('describes the year of budgets by the months they counted', async () => {
  const row = (spendingCategoryId: string, budgetedMonthCount: number, firstBudgetedMonth: string) => ({
    spendingCategoryId,
    spendingCategoryName: `Invented ${spendingCategoryId}`,
    budgetAmount: '400.0000',
    currencyCode: 'USD',
    budgetToDateAmount: '400.0000',
    budgetedMonthCount,
    firstBudgetedMonth,
    lastBudgetedMonth: '2030-12',
    spendingAmount: '300.0000',
    spendingBySameDayLastMonthAmount: '0.0000',
    fixedChargesDueAmount: '0.0000',
    expectedRepeatCharges: [],
    projectedAmount: '300.0000',
    budgetPace: 'under',
    unconvertedSpending: [],
  })
  spendingBudgetRows = [
    row('category-one', 4, '2030-09'),
    row('category-two', 4, '2030-09'),
    row('category-three', 2, '2030-11'),
  ]
  renderAt('/finance/spending?year=2030')
  const september = monthLabel('2030-09')
  const december = monthLabel('2030-12')
  expect(
    await screen.findByText(
      `finance.budgetStatusYearMonthsHintPast {"year":"2030","count":4,"from":"${september}","to":"${december}"}`,
    ),
  ).toBeTruthy()
  expect(screen.getAllByText(/^finance\.budgetedMonths /).map((note) => note.textContent)).toEqual([
    `finance.budgetedMonths {"count":2,"from":"${monthLabel('2030-11')}","to":"${december}"}`,
  ])
})
