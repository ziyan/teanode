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

// MeterBar is the bar itself: how much of something has gone, in the
// colour the caller judged it, and where it is heading when the caller
// knows (a spending category's projected month end against its budget).
// The day's budget above and the Finance tab's budgets and savings
// targets draw the same bar.
export function MeterBar({
  fraction,
  tone,
  label,
  marker,
}: {
  fraction: number
  tone: 'good' | 'warn' | 'bad'
  label: string
  marker?: number | null
}) {
  const shown = Math.max(0, Math.min(1, fraction))
  return (
    <div
      className="agent-budget-bar"
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(shown * 100)}
      aria-label={label}
    >
      <span className={`agent-budget-bar-fill ${tone}`} style={{ width: `${Math.round(shown * 100)}%` }} />
      {marker !== undefined && marker !== null ? (
        <span
          className="agent-budget-bar-marker"
          style={{ left: `${Math.round(Math.max(0, Math.min(1, marker)) * 100)}%` }}
        />
      ) : null}
    </div>
  )
}
