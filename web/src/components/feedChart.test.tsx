import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'

import { TranslationProvider } from '../i18n/i18n'
import { chartBlocksOf, FeedChartBlocks, valueText } from './feedChart'

afterEach(() => cleanup())

const seriesSent = {
  chartKind: 'series',
  chartTitle: 'Daily steps',
  unit: 'steps',
  provenance: 'Read from the watch at 09:40',
  xLabels: ['Mon', 'Tue', 'Wed', 'Thu'],
  series: [
    { seriesName: 'This week', seriesRole: 'measured', values: [4000, 6500, 5200, null] },
    { seriesName: 'Last week', seriesRole: 'reference', values: [3000, 3200, 3100, 3300] },
    { seriesName: 'Goal pace', seriesRole: 'projection', values: [null, null, 5200, 7000] },
  ],
  markers: [{ markerAt: 'Tue', markerLabel: 'Long walk' }],
  bars: [],
}

const barsSent = {
  chartKind: 'bars',
  chartTitle: 'Spending by shop',
  unit: 'USD',
  provenance: 'Read from the bank at 09:40 EST',
  xLabels: [],
  series: [],
  markers: [],
  bars: [
    { barLabel: 'Corner grocer', barValue: 120.5 },
    { barLabel: 'Bike repair', barValue: 60 },
  ],
}

function draw(sent: unknown[]) {
  return render(
    <TranslationProvider>
      <FeedChartBlocks blocks={chartBlocksOf(sent)} />
    </TranslationProvider>,
  )
}

it('draws reference solid, projection dashed and measured in its own color, with a marker rule', () => {
  const { container } = draw([seriesSent])
  expect(container.querySelector('.feed-chart-line.is-reference path')).toBeTruthy()
  expect(container.querySelector('.feed-chart-line.is-projection path')).toBeTruthy()
  expect(container.querySelector('.feed-chart-line.is-measured path')).toBeTruthy()
  // The marker is a labeled vertical rule.
  const marker = container.querySelector('.feed-chart-marker')
  expect(marker?.querySelector('line')?.getAttribute('x1')).toBe(marker?.querySelector('line')?.getAttribute('x2'))
  expect(within(marker as HTMLElement).getByText('Long walk')).toBeTruthy()
  // A missing value leaves a gap: the measured line is one segment of three points.
  expect(container.querySelectorAll('.feed-chart-line.is-measured path')).toHaveLength(1)
  expect(screen.getByText('Daily steps')).toBeTruthy()
  expect(screen.getByText('Read from the watch at 09:40').className).toContain('muted')
})

it('shows each value with its unit when a point is touched, and hides it after', () => {
  draw([seriesSent])
  const column = screen.getByRole('img', { name: /^Tue: This week 6,500 steps/ })
  fireEvent.pointerDown(column)
  const tooltip = screen.getByRole('status')
  expect(within(tooltip).getByText('Tue')).toBeTruthy()
  expect(tooltip.textContent).toContain('This week: 6,500 steps')
  expect(tooltip.textContent).toContain('Last week: 3,200 steps')
  expect(tooltip.textContent).toContain('Goal pace: no value')
  fireEvent.blur(column)
  expect(screen.queryByRole('status')).toBeNull()
})

it('draws horizontal bars largest first with the value and unit at the end', () => {
  const { container } = draw([barsSent])
  const rows = container.querySelectorAll('.feed-chart-bars li')
  expect(rows).toHaveLength(2)
  expect(rows[0].textContent).toContain('Corner grocer')
  expect(within(rows[0] as HTMLElement).getByText('120.5 USD')).toBeTruthy()
  const widths = [...container.querySelectorAll('.feed-chart-bar-fill')].map((bar) => Number(bar.getAttribute('width')))
  expect(widths[0]).toBe(100)
  expect(widths[1]).toBeCloseTo((60 / 120.5) * 100)
  expect(screen.getByText('Read from the bank at 09:40 EST')).toBeTruthy()
})

it('draws a series with an unknown role as measured, and drops what cannot be drawn', () => {
  const blocks = chartBlocksOf([
    { ...seriesSent, series: [{ seriesName: 'Odd', seriesRole: 'forecast-ish', values: [1, 2, 3, 4] }] },
    { ...seriesSent, series: [{ seriesName: 'Short', seriesRole: 'measured', values: [1, 2] }] },
    { ...seriesSent, markers: [{ markerAt: 'Fri', markerLabel: 'Not on the axis' }] },
    { chartKind: 'bars', bars: [{ barLabel: 'Negative', barValue: -2 }] },
    'nonsense',
  ])
  expect(blocks).toHaveLength(2)
  expect(blocks[0].series[0].seriesRole).toBe('measured')
  expect(blocks[1].markers).toHaveLength(0)
})

it('says a value with its unit', () => {
  expect(valueText(1234.567, 'USD')).toBe('1,234.57 USD')
  expect(valueText(5, '')).toBe('5')
})
