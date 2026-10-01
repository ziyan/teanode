import { budgetNearness, formatClock, formatCount, formatMoney } from './common'
import { useTranslation } from '../i18n/i18n'

// Budget is the day's use of the agent against its limits, as the server
// reports it: tokens, and what they cost, each with its limit (zero for
// none), and when the day starts again.
export interface Budget {
  used: number
  limit: number
  resetsAt: string
  cost: number
  costLimit: number
  currency: string
}

// BudgetBar is the day against its budget: what has gone of it as a bar
// in the colour of how near the end it is, the numbers beside it, and
// when the day starts again. A budget can be set in tokens or in money;
// where both are, the one nearer its end is the one drawn, since that is
// the one that will stop the day.
export function BudgetBar({ budget, zone }: { budget: Budget | null; zone: string }) {
  const { t } = useTranslation()
  if (!budget) return null
  const tokens = budget.limit > 0 ? budget.used / budget.limit : -1
  const money = budget.costLimit > 0 ? budget.cost / budget.costLimit : -1
  if (tokens < 0 && money < 0) {
    return (
      <p className="muted">
        {t('agent.budgetNone')}{' '}
        {t('agent.budgetMoney', { used: formatMoney(budget.cost, budget.currency), limit: t('agent.unlimited') })}
      </p>
    )
  }
  const byMoney = money >= tokens
  const fraction = Math.max(0, Math.min(1, byMoney ? money : tokens))
  const said = byMoney
    ? t('agent.budgetMoney', {
        used: formatMoney(budget.cost, budget.currency),
        limit: formatMoney(budget.costLimit, budget.currency),
      })
    : t('agent.budgetTokens', { used: formatCount(budget.used), limit: formatCount(budget.limit) })
  return (
    <div className="agent-budget">
      <div className="agent-budget-said">
        <span>
          {said}
          {/* What it came to, whichever way the budget is counted: a
              number of tokens is not something anybody can act on. */}
          {!byMoney && budget.cost > 0 ? (
            <span className="muted"> · {formatMoney(budget.cost, budget.currency)}</span>
          ) : null}
        </span>
        <span className="muted">{t('agent.budgetResets', { at: formatClock(budget.resetsAt, zone) })}</span>
      </div>
      <MeterBar fraction={fraction} tone={budgetNearness(fraction, 1)} label={said} />
    </div>
  )
}

// MeterBand is where a meter's fill and forecast band sit, as shares of
// the bar from 0 to 1: the fill up to fillShare, the band from there to
// bandEndShare (equal to fillShare when there is no band), and whether the
// forecast passes the end of the bar while the fill has not yet.
export interface MeterBand {
  fillShare: number
  bandEndShare: number
  isHeadingOver: boolean
}

// meterBand is the fill and the forecast band of a meter. A forecast
// behind the fill (refunds, or a projection that is only the income
// expected) draws no band: the bar already says more than it.
export function meterBand(fraction: number, forecast?: number | null): MeterBand {
  const fillShare = clampShare(fraction)
  if (forecast === undefined || forecast === null || !Number.isFinite(forecast)) {
    return { fillShare, bandEndShare: fillShare, isHeadingOver: false }
  }
  return {
    fillShare,
    bandEndShare: Math.max(fillShare, clampShare(forecast)),
    isHeadingOver: forecast > 1 && fraction < 1,
  }
}

function clampShare(share: number): number {
  return Number.isFinite(share) ? Math.max(0, Math.min(1, share)) : 0
}

// MeterBar is the bar itself: how much of something has gone, in the
// colour the caller judged it, and, when the caller knows where it is
// heading (a spending category's projected month end against its
// budget), a faint band from the fill to there. A band whose forecast
// passes the end of the bar reaches the end, and with overTone it is drawn
// in that tone with a solid end, so a month heading over its budget says
// so before it gets there. forecastLabel is added to what a screen reader
// is told. The day's budget above and the Finance tab's budgets and
// savings targets draw the same bar.
export function MeterBar({
  fraction,
  tone,
  label,
  forecast,
  forecastLabel,
  overTone,
}: {
  fraction: number
  tone: 'good' | 'warn' | 'bad'
  label: string
  forecast?: number | null
  forecastLabel?: string
  overTone?: 'good' | 'warn' | 'bad'
}) {
  const band = meterBand(fraction, forecast)
  const isOverShown = band.isHeadingOver && overTone !== undefined
  const bandTone = isOverShown ? overTone : tone
  const fillPercent = Math.round(band.fillShare * 100)
  const bandEndPercent = Math.round(band.bandEndShare * 100)
  return (
    <div
      className="agent-budget-bar"
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={fillPercent}
      aria-label={forecastLabel ? `${label}. ${forecastLabel}` : label}
    >
      {bandEndPercent > fillPercent ? (
        <span
          className={`agent-budget-bar-band ${bandTone}${isOverShown ? ' over' : ''}`}
          style={{ left: `calc(${fillPercent}% - 3px)`, width: `calc(${bandEndPercent - fillPercent}% + 3px)` }}
        />
      ) : null}
      <span className={`agent-budget-bar-fill ${tone}`} style={{ width: `${fillPercent}%` }} />
    </div>
  )
}
