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
      <div
        className="agent-budget-bar"
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(fraction * 100)}
        aria-label={said}
      >
        <span
          className={`agent-budget-bar-fill ${budgetNearness(fraction, 1)}`}
          style={{ width: `${Math.round(fraction * 100)}%` }}
        />
      </div>
    </div>
  )
}
