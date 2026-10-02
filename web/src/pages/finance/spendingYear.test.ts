import { expect, it } from 'vitest'

import type { CashFlowMonth } from './financeApi'
import {
  cashFlowYears,
  firstCashFlowMonth,
  firstIncomeMonth,
  historyRange,
  incomeStartMonthWithin,
  monthOptions,
  partialYearStartMonth,
  yearCashFlowTotals,
  yearOptions,
} from './spendingYear'

const month = (cashFlowMonth: string, incomeAmount: string, spendingAmount: string): CashFlowMonth => ({
  cashFlowMonth,
  incomeAmount,
  spendingAmount,
  netAmount: String(Number(incomeAmount) - Number(spendingAmount)),
})

it('reads twenty years of history, this one included, to this month', () => {
  expect(historyRange('2031-05')).toEqual({ fromMonth: '2012-01', toMonth: '2031-05' })
})

// The year is its months added up as the server counted each, so a month
// of refunds below zero lowers the year's spending, and a month of another
// year in the answer is not counted.
it('adds up the year from its months', () => {
  const totals = yearCashFlowTotals(
    [
      month('2030-12', '9000.0000', '9000.0000'),
      month('2031-01', '3000.0000', '1200.5000'),
      month('2031-02', '3000.0000', '-50.0000'),
      month('2031-03', '0.0000', '800.0000'),
      month('2031-04', '0.0000', '0.0000'),
    ],
    '2031',
  )
  expect(totals.incomeAmount).toBeCloseTo(6000)
  expect(totals.spendingAmount).toBeCloseTo(1950.5)
  expect(totals.netAmount).toBeCloseTo(4049.5)
  expect(yearCashFlowTotals([], '2031')).toEqual({ incomeAmount: 0, spendingAmount: 0, netAmount: 0 })
})

// Empty months before the first with money in or out are not years of
// history; a quiet year in between still is one.
it('lists every year from the first with cash flow to this one', () => {
  const months = [
    month('2027-11', '0.0000', '0.0000'),
    month('2028-03', '0.0000', '120.0000'),
    month('2029-06', '0.0000', '0.0000'),
    month('2030-02', '2500.0000', '900.0000'),
    month('2031-01', '2500.0000', '1000.0000'),
  ]
  expect(firstCashFlowMonth(months)).toBe('2028-03')
  const years = cashFlowYears(months, '2031')
  expect(years.map((year) => year.year)).toEqual(['2028', '2029', '2030', '2031'])
  expect(years[0].spendingAmount).toBeCloseTo(120)
  expect(years[1]).toEqual({ year: '2029', incomeAmount: 0, spendingAmount: 0, netAmount: 0 })
  expect(years[3].netAmount).toBeCloseTo(1500)
  expect(cashFlowYears([], '2031').map((year) => year.year)).toEqual(['2031'])
  expect(firstCashFlowMonth([])).toBeNull()
})

it('offers the years with cash flow newest first, and keeps a year further back that was chosen', () => {
  expect(yearOptions(['2029', '2030', '2031'], '2031')).toEqual(['2031', '2030', '2029'])
  expect(yearOptions(['2029', '2030', '2031'], '2020')).toEqual(['2031', '2030', '2029', '2020'])
})

it('offers the months back to the first with cash flow, or a year of them without any', () => {
  expect(monthOptions('2031-02', '2031-05', '2031-05')).toEqual(['2031-05', '2031-04', '2031-03', '2031-02'])
  expect(monthOptions('2031-02', '2031-05', '2030-07')).toEqual(['2031-05', '2031-04', '2031-03', '2031-02', '2030-07'])
  const yearOfMonths = monthOptions(null, '2031-05', '2031-05')
  expect(yearOfMonths).toHaveLength(12)
  expect(yearOfMonths[11]).toBe('2030-06')
})

// Only the year the history starts in, and only when it starts after
// January, is a partial year.
it('finds the month a partial first year starts in', () => {
  expect(partialYearStartMonth('2028', '2028-08')).toBe('2028-08')
  expect(partialYearStartMonth('2028', '2028-01')).toBeNull()
  expect(partialYearStartMonth('2029', '2028-08')).toBeNull()
  expect(partialYearStartMonth('2028', null)).toBeNull()
})

// Income can start later than spending, a card's statements reaching
// further back than the bank account pay goes into; the year it starts in
// says so, unless it starts in January.
it('finds the month income starts in, partway through a year', () => {
  const months = [month('2028-01', '0', '-40'), month('2028-09', '1200', '-50'), month('2029-01', '1300', '-60')]
  expect(firstIncomeMonth(months)).toBe('2028-09')
  expect(incomeStartMonthWithin('2028', '2028-09')).toBe('2028-09')
  expect(incomeStartMonthWithin('2029', '2028-09')).toBeNull()
  expect(incomeStartMonthWithin('2028', '2028-01')).toBeNull()
  expect(incomeStartMonthWithin('2028', null)).toBeNull()
})
