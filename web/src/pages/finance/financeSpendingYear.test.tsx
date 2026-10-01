import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, useLocation, useNavigate } from 'react-router-dom'
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

// What each question is answered with: nothing spent and nothing budgeted,
// so every panel draws its empty state and the period picker stands alone.
function answer(query: string): unknown {
  if (query.includes('CashFlow(')) {
    return { CashFlow: { fromMonth: '', toMonth: '', cashFlowMonths: [], unconvertedCurrencyCodes: [] } }
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
  execute.mockImplementation((query: string) => Promise.resolve(answer(query)) as never)
})

afterEach(() => {
  cleanup()
  execute.mockReset()
  vi.useRealTimers()
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
  expect(variablesOf('CashFlow')).toContainEqual({ fromMonth: '2031-01', toMonth: '2031-12' })
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
