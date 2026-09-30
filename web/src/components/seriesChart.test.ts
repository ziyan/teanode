import { expect, it } from 'vitest'

import { axisWidthFor, chartScale } from './seriesChart'

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
