import { expect, it } from 'vitest'

import type { CashFlowMonth } from './financeApi'
import { yearAfter, yearBefore, yearCashFlowTotals, yearMonths, yearOptions } from './spendingYear'

const month = (cashFlowMonth: string, incomeAmount: string, spendingAmount: string): CashFlowMonth => ({
  cashFlowMonth,
  incomeAmount,
  spendingAmount,
  netAmount: String(Number(incomeAmount) - Number(spendingAmount)),
})

it('lists the twelve months of a year', () => {
  const months = yearMonths('2031')
  expect(months).toHaveLength(12)
  expect(months[0]).toBe('2031-01')
  expect(months[8]).toBe('2031-09')
  expect(months[11]).toBe('2031-12')
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

it('offers this year and the ones before, and keeps a year further back that was chosen', () => {
  expect(yearOptions('2031', '2031', 3)).toEqual(['2031', '2030', '2029'])
  expect(yearOptions('2031', '2020', 3)).toEqual(['2031', '2030', '2029', '2020'])
  expect(yearOptions('2031', '2030', 3)).toEqual(['2031', '2030', '2029'])
  expect(yearOptions('2031', '2031')).toHaveLength(10)
})

it('steps a year either way', () => {
  expect(yearBefore('2031')).toBe('2030')
  expect(yearAfter('2030')).toBe('2031')
})
