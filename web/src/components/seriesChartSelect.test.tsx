import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { SeriesChart } from './seriesChart'

// jsdom lays nothing out, and the chart draws nothing at a width of zero,
// so the holder is given a width and the observer a stand-in.
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

const keys = ['2031-01', '2031-02', '2031-03']

function drawChart(selectedKey: string | null, onSelectKey?: (key: string) => void) {
  return render(
    <SeriesChart
      label="Spending by month"
      keys={keys}
      keyLabel={(key) => key}
      format={(value) => String(value)}
      series={[{ id: 'spending', label: 'Spending', tone: 'output', shape: 'column', values: [120, 80, 45] }]}
      selectedKey={selectedKey}
      onSelectKey={onSelectKey}
    />,
  )
}

it('draws no buttons when nothing can be chosen', () => {
  drawChart(null)
  expect(screen.queryAllByRole('button')).toHaveLength(0)
  expect(screen.getByRole('img')).toBeTruthy()
})

it('chooses a key when its slot is pressed, and marks the chosen one', () => {
  const onSelectKey = vi.fn()
  drawChart('2031-02', onSelectKey)
  const slots = screen.getAllByRole('button')
  expect(slots).toHaveLength(3)
  expect(slots.map((slot) => slot.getAttribute('aria-pressed'))).toEqual(['false', 'true', 'false'])
  expect(slots[0].getAttribute('aria-label')).toBe('2031-01: Spending 120')
  fireEvent.click(slots[2])
  expect(onSelectKey).toHaveBeenCalledWith('2031-03')
})

// One tab stop, on the chosen slot; the arrows move along from there.
it('is one tab stop that the arrow keys move along', () => {
  const onSelectKey = vi.fn()
  drawChart('2031-02', onSelectKey)
  const slots = screen.getAllByRole('button')
  expect(slots.map((slot) => slot.getAttribute('tabindex'))).toEqual(['-1', '0', '-1'])
  fireEvent.keyDown(slots[1], { key: 'ArrowLeft' })
  expect(onSelectKey).toHaveBeenLastCalledWith('2031-01')
  fireEvent.keyDown(slots[1], { key: 'ArrowRight' })
  expect(onSelectKey).toHaveBeenLastCalledWith('2031-03')
  fireEvent.keyDown(slots[2], { key: 'ArrowRight' })
  expect(onSelectKey).toHaveBeenLastCalledWith('2031-03')
  fireEvent.keyDown(slots[0], { key: 'End' })
  expect(onSelectKey).toHaveBeenLastCalledWith('2031-03')
  fireEvent.keyDown(slots[1], { key: 'Enter' })
  expect(onSelectKey).toHaveBeenLastCalledWith('2031-02')
})

// Cash flow: two columns a key side by side and a line that goes below
// zero, still chosen by a slot.
it('draws two columns a key and a line below zero, and still chooses a key', () => {
  const onSelectKey = vi.fn()
  const { container } = render(
    <SeriesChart
      label="Cash flow by month"
      keys={keys}
      keyLabel={(key) => key}
      format={(value) => String(value)}
      series={[
        { id: 'income', label: 'Income', tone: 'output', shape: 'column', values: [500, 400, 300] },
        { id: 'spending', label: 'Spending', tone: 'cached', shape: 'column', values: [300, 450, 350] },
        { id: 'left', label: 'Left over', tone: 'input', shape: 'line', values: [200, -50, -50] },
      ]}
      selectedKey="2031-02"
      onSelectKey={onSelectKey}
    />,
  )
  expect(container.querySelectorAll('.usage-chart-column.output')).toHaveLength(3)
  expect(container.querySelectorAll('.usage-chart-column.cached')).toHaveLength(3)
  expect(container.querySelector('.series-chart-zero')).toBeTruthy()
  expect(container.querySelector('.series-chart-line.input')?.getAttribute('d')).toMatch(/^M.* L.* L/)
  const slots = screen.getAllByRole('button')
  expect(slots[1].getAttribute('aria-label')).toBe('2031-02: Income 400, Spending 450, Left over -50')
  fireEvent.click(slots[0])
  expect(onSelectKey).toHaveBeenCalledWith('2031-01')
})

// pointAt moves a pointer over the drawing, measured as wide as its holder.
// jsdom has no PointerEvent, and the stand-in testing-library falls back to
// drops the pointer's position.
function pointAt(element: Element, clientX: number) {
  vi.spyOn(SVGElement.prototype, 'getBoundingClientRect').mockReturnValue(HTMLElement.prototype.getBoundingClientRect())
  act(() => {
    element.dispatchEvent(new MouseEvent('pointermove', { clientX, bubbles: true }))
  })
}

// The tooltip carries a line under the series for a key that has one, and
// none for a key that has not.
it('adds the note under the series in the tooltip', () => {
  render(
    <SeriesChart
      label="Spending by day"
      keys={keys}
      keyLabel={(key) => key}
      format={(value) => String(value)}
      series={[
        { id: 'month', label: 'This month', tone: 'output', shape: 'column', values: [10, 30, 70] },
        { id: 'compare', label: 'Month before', tone: 'cached', shape: 'line', values: [20, 25, null] },
      ]}
      tooltipNote={(index) => (index === 1 ? { label: 'Difference', text: '5 more' } : null)}
    />,
  )
  const drawing = screen.getByRole('img')
  // Three slots across 600 pixels less the axis: the middle of the drawing
  // is the middle slot, and its right edge the last.
  pointAt(drawing, 330)
  const tooltip = screen.getByRole('status')
  expect(tooltip.textContent).toContain('Difference')
  expect(tooltip.textContent).toContain('5 more')
  pointAt(drawing, 595)
  expect(screen.getByRole('status').textContent).not.toContain('Difference')
})
