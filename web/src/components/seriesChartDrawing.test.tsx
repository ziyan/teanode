import { cleanup, render } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { SeriesChart } from './seriesChart'

// What a chart of stacked columns or of a line with a band draws: the stages
// of a night are piled in one column, and the lowest to highest reading of a
// day is the area around its average.
beforeEach(() => {
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
  vi.unstubAllGlobals()
})

function axisLabels(container: HTMLElement): string[] {
  return [...container.querySelectorAll('.usage-chart-grid + text')].map((each) => each.textContent ?? '')
}

function stackedChart() {
  return render(
    <SeriesChart
      label="Sleep by day"
      keys={['2031-01-01', '2031-01-02']}
      keyLabel={(key) => key}
      format={(value) => String(value)}
      isStacked
      series={[
        { id: 'deep', label: 'Deep', tone: 'deep', shape: 'column', values: [2, 3] },
        { id: 'core', label: 'Core', tone: 'core', shape: 'column', values: [4, 1] },
      ]}
    />,
  )
}

it('draws the scale from the totals of a stack, not from its tallest part', () => {
  const { container } = stackedChart()
  // The nights are 6 and 4 tall; the tallest single part is 4, which would
  // have ended the axis at 4.
  expect(axisLabels(container)).toEqual(['0', '2', '4', '6'])
})

it('draws a stack as one column a key, with only the top rounded', () => {
  const { container } = stackedChart()
  const columns = [...container.querySelectorAll('path.usage-chart-column')].map((each) => each.getAttribute('d') ?? '')
  // Two keys with two stages each, side by side in no key: four paths.
  expect(columns).toHaveLength(4)
  for (const [bottom, top] of [
    [columns[0], columns[1]],
    [columns[2], columns[3]],
  ]) {
    expect(bottom).not.toContain('Q')
    expect(top).toContain('Q')
    // One column width: both stages start at the same left edge.
    expect(bottom.split(',')[0]).toBe(top.split(',')[0])
  }
})

it('leaves a stage out of its column when the key has none of it', () => {
  const { container } = render(
    <SeriesChart
      label="Sleep by day"
      keys={['2031-01-01']}
      keyLabel={(key) => key}
      format={(value) => String(value)}
      isStacked
      series={[
        { id: 'deep', label: 'Deep', tone: 'deep', shape: 'column', values: [null] },
        { id: 'core', label: 'Core', tone: 'core', shape: 'column', values: [3] },
      ]}
    />,
  )
  const columns = container.querySelectorAll('path.usage-chart-column')
  expect(columns).toHaveLength(1)
  // The only stage is the top one, so it is rounded.
  expect(columns[0].getAttribute('d')).toContain('Q')
})

function bandChart() {
  return render(
    <SeriesChart
      label="Heart rate by day"
      keys={['2031-01-01', '2031-01-02', '2031-01-03']}
      keyLabel={(key) => key}
      format={(value) => String(value)}
      series={[{ id: 'average', label: 'Average', tone: 'output', shape: 'line', values: [5, null, 6] }]}
      bands={[{ id: 'range', tone: 'band', lows: [3, null, 4], highs: [8, null, 9] }]}
    />,
  )
}

it('draws a band between the lows and highs and breaks it at a gap', () => {
  const { container } = bandChart()
  const band = container.querySelector('path.series-chart-band')
  expect(band).not.toBeNull()
  const path = band?.getAttribute('d') ?? ''
  // The first and the last day are two pieces, not one area across the gap.
  expect(path.match(/M/g)).toHaveLength(2)
  expect(path.match(/Z/g)).toHaveLength(2)
})

it('reaches the scale up to the highest of a band, above the line', () => {
  const { container } = bandChart()
  // The line peaks at 6 but the band at 9: the axis has to hold the band.
  const labels = axisLabels(container).map(Number)
  expect(Math.max(...labels)).toBeGreaterThanOrEqual(9)
})

it('draws no band where either end is missing', () => {
  const { container } = render(
    <SeriesChart
      label="Heart rate by day"
      keys={['2031-01-01', '2031-01-02']}
      keyLabel={(key) => key}
      format={(value) => String(value)}
      series={[{ id: 'average', label: 'Average', tone: 'output', shape: 'line', values: [5, 6] }]}
      bands={[{ id: 'range', tone: 'band', lows: [3, null], highs: [8, 9] }]}
    />,
  )
  const path = container.querySelector('path.series-chart-band')?.getAttribute('d') ?? ''
  // One day has both ends: a single piece.
  expect(path.match(/M/g)).toHaveLength(1)
})
