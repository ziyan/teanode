import { expect, it } from 'vitest'

import { axisWidthFor, chartScale, isolatedIndexes, labeledIndexes } from './seriesChart'

// A month of cash flow that went below zero: the scale reaches under the
// lowest value, so the line is drawn whole, and zero is a gridline.
it('reaches below zero in round steps that pass through zero', () => {
  const scale = chartScale([6200, 4100, -1800, 7300])
  expect(scale.floor).toBeLessThanOrEqual(-1800)
  expect(scale.ceiling).toBeGreaterThanOrEqual(7300)
  expect(scale.grid).toContain(0)
  const steps = scale.grid.slice(1).map((value, index) => value - scale.grid[index])
  expect(new Set(steps).size).toBe(1)
  expect(steps[0]).toBe(5000)
})

it('starts at zero when nothing is below it', () => {
  const scale = chartScale([120, 480, 950])
  expect(scale.floor).toBe(0)
  expect(scale.grid[0]).toBe(0)
  expect(scale.ceiling).toBeGreaterThanOrEqual(950)
})

// The axis is as wide as its widest label, within bounds.
it('sizes the axis to its widest label', () => {
  expect(axisWidthFor(['$0', '$10k'])).toBeLessThan(axisWidthFor(['$0', '$1,250,000.00']))
  expect(axisWidthFor(['0'])).toBeGreaterThanOrEqual(28)
})

// A value alone, with nothing either side, is drawn as a dot: a line
// through one point shows nothing.
it('finds the values a line cannot show', () => {
  expect(isolatedIndexes([5])).toEqual([0])
  expect(isolatedIndexes([null, 5, null, 3, 4])).toEqual([1])
  expect(isolatedIndexes([1, 2, 3])).toEqual([])
  expect(isolatedIndexes([])).toEqual([])
})

// A refund on the first day of a month dips a few dollars below a line that
// climbs to thousands: no gridline below zero for that. A real negative
// range still gets one.
it('does not reach below zero for a negligible dip', () => {
  const tiny = chartScale([-58, -58, 900, 4600])
  expect(tiny.floor).toBe(0)
  expect(tiny.grid[0]).toBe(0)
  const real = chartScale([-400, 900, 4600])
  expect(real.floor).toBeLessThan(0)
  expect(real.grid).toContain(0)
  expect(chartScale([-50, -20]).floor).toBeLessThanOrEqual(-50)
})

// Every third label fits; the chosen key is labelled whatever its place,
// and the regular labels too close to it are left out.
it('always labels the chosen key and clears its neighbours', () => {
  expect([...labeledIndexes(12, 3, -1)].sort((left, right) => left - right)).toEqual([0, 3, 6, 9])
  expect([...labeledIndexes(12, 3, 7)].sort((left, right) => left - right)).toEqual([0, 3, 7])
  expect([...labeledIndexes(12, 3, 11)].sort((left, right) => left - right)).toEqual([0, 3, 6, 11])
  expect([...labeledIndexes(12, 3, 6)].sort((left, right) => left - right)).toEqual([0, 3, 6, 9])
  expect([...labeledIndexes(5, 1, 2)].sort((left, right) => left - right)).toEqual([0, 1, 2, 3, 4])
})
