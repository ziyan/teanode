import { afterEach, describe, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { PlanUsageBars } from './planUsage'
import { TranslationProvider } from '../i18n/i18n'

afterEach(cleanup)

// A moment an hour after the readings below, on the same day.
const NOW = Date.parse('2026-01-05T13:00:00Z')

const renderBars = (usage: Parameters<typeof PlanUsageBars>[0]['usage'], now = NOW) =>
  render(
    <TranslationProvider>
      <PlanUsageBars usage={usage} now={now} />
    </TranslationProvider>,
  )

describe('PlanUsageBars', () => {
  it('draws each window as a bar of what is used, and says what is left and when it resets', () => {
    renderBars({
      planName: 'plus',
      observedAt: '2026-01-05T12:00:00Z',
      windows: [
        { usedPercent: 20, windowMinutes: 300, resetsAt: null },
        { usedPercent: 69, windowMinutes: 10080, resetsAt: '2026-01-08T18:00:00Z' },
      ],
    })
    const bars = screen.getAllByRole('progressbar')
    expect(bars).toHaveLength(2)
    expect(bars[0].getAttribute('aria-valuenow')).toBe('20')
    expect(bars[1].getAttribute('aria-valuenow')).toBe('69')
    expect(screen.getByText('5-hour: 80% left')).toBeTruthy()
    expect(screen.getByText('Weekly: 31% left')).toBeTruthy()
    // Only the window that says when it resets says so.
    expect(screen.getAllByText(/^resets /)).toHaveLength(1)
    // Near the end of the allowance, the bar says so in its colour.
    expect(bars[1].querySelector('.agent-budget-bar-fill')?.className).toContain('warn')
  })

  it('draws a window whose reset has passed as the whole allowance, and says it has reset', () => {
    renderBars({
      planName: 'plus',
      observedAt: '2026-01-05T12:00:00Z',
      windows: [
        { usedPercent: 90, windowMinutes: 300, resetsAt: '2026-01-05T12:30:00Z' },
        { usedPercent: 90, windowMinutes: 300, resetsAt: '2026-01-05T14:00:00Z' },
      ],
    })
    const bars = screen.getAllByRole('progressbar')
    expect(bars[0].getAttribute('aria-valuenow')).toBe('0')
    expect(bars[1].getAttribute('aria-valuenow')).toBe('90')
    expect(screen.getByText('5-hour: 100% left')).toBeTruthy()
    expect(screen.getAllByText(/^reset since this reading, /)).toHaveLength(1)
    expect(bars[0].closest('.agent-budget')?.className).toContain('muted')
  })

  it('dates a reading that is not from today', () => {
    const usage = {
      planName: 'plus',
      observedAt: '2026-01-05T12:00:00Z',
      windows: [{ usedPercent: 20, windowMinutes: 300, resetsAt: null }],
    }
    // A minute later, which is the same day in any zone the test runs in.
    const { container, unmount } = renderBars(usage, Date.parse('2026-01-05T12:01:00Z'))
    const today = container.querySelector('.plan-usage-observed')?.textContent ?? ''
    expect(today).not.toContain('2026')
    unmount()
    const later = renderBars(usage, Date.parse('2026-01-08T13:00:00Z'))
    expect(later.container.querySelector('.plan-usage-observed')?.textContent).toContain('2026')
  })

  it('draws nothing for a plan that reports no window', () => {
    const { container } = renderBars({ planName: 'plus', observedAt: '2026-01-05T12:00:00Z', windows: [] })
    expect(container.textContent).toBe('')
  })
})
