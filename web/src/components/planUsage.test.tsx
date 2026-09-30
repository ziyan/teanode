import { afterEach, describe, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { PlanUsageBars } from './planUsage'
import { TranslationProvider } from '../i18n/i18n'

afterEach(cleanup)

const renderBars = (usage: Parameters<typeof PlanUsageBars>[0]['usage']) =>
  render(
    <TranslationProvider>
      <PlanUsageBars usage={usage} />
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

  it('draws nothing for a plan that reports no window', () => {
    const { container } = renderBars({ planName: 'plus', observedAt: '2026-01-05T12:00:00Z', windows: [] })
    expect(container.textContent).toBe('')
  })
})
