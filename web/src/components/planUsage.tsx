import { budgetNearness, formatClock, formatTime } from './common'
import { useTranslation } from '../i18n/i18n'

// PlanUsage is what a provider paid for by subscription last said of its
// allowance, as the server reports it: the plan's name, and each window the
// allowance is spent over, shortest first.
export interface PlanUsage {
  planName: string
  observedAt: string
  windows: PlanWindow[]
}

export interface PlanWindow {
  usedPercent: number
  windowMinutes: number
  resetsAt?: string | null
}

// PlanUsageBars draws each window as a bar of what is used, in the colour of
// how near the end it is, with what is left and when it starts over beside
// it. The numbers are from the plan's last answer to this server, which the
// line under the bars dates, since nothing asks the plan between turns. A
// window whose reset has passed since is drawn as the whole allowance, muted,
// until the next answer says how much of the new one is used.
export function PlanUsageBars({ usage, now = Date.now() }: { usage: PlanUsage; now?: number }) {
  const { t } = useTranslation()
  if (usage.windows.length === 0) return null
  return (
    <div className="plan-usage">
      {usage.windows.map((window, index) => {
        const hasReset = window.resetsAt ? new Date(window.resetsAt).getTime() <= now : false
        const used = hasReset ? 0 : Math.max(0, Math.min(100, window.usedPercent))
        const said = t('agentSettings.planWindowLeft', {
          window: windowName(window.windowMinutes, t),
          left: String(100 - used),
        })
        return (
          <div className={hasReset ? 'agent-budget muted' : 'agent-budget'} key={index}>
            <div className="agent-budget-said">
              <span>{said}</span>
              {window.resetsAt ? (
                <span className="muted">
                  {t(hasReset ? 'agentSettings.planResetSince' : 'agentSettings.planResets', {
                    at: formatReset(window.resetsAt),
                  })}
                </span>
              ) : null}
            </div>
            <div
              className="agent-budget-bar"
              role="progressbar"
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={used}
              aria-label={said}
            >
              <span className={`agent-budget-bar-fill ${budgetNearness(used, 100)}`} style={{ width: `${used}%` }} />
            </div>
          </div>
        )
      })}
      <span className="muted plan-usage-observed">
        {t('agentSettings.planObserved', { at: formatObserved(usage.observedAt, now) })}
      </span>
    </div>
  )
}

// formatObserved is when the plan last answered: the time alone on the day
// it is read, and the date with it on any other, since a reading from days
// ago is not the allowance left now.
function formatObserved(value: string, now: number): string {
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return value
  return parsed.toDateString() === new Date(now).toDateString() ? formatClock(value) : formatTime(value)
}

// windowName is a window by its length: the ones the plans use by name, any
// other by its hours or days.
function windowName(minutes: number, t: ReturnType<typeof useTranslation>['t']): string {
  if (minutes === 7 * 24 * 60) return t('agentSettings.planWindowWeek')
  if (minutes === 24 * 60) return t('agentSettings.planWindowDay')
  if (minutes % (24 * 60) === 0) return t('agentSettings.planWindowDays', { count: String(minutes / (24 * 60)) })
  return t('agentSettings.planWindowHours', { count: String(Math.round((minutes / 60) * 10) / 10) })
}

// formatReset is the moment a window starts over: a weekday and a time, with
// the zone, since a weekly reset is days away and the reader needs the day.
function formatReset(value: string): string {
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return value
  return parsed.toLocaleString(undefined, {
    weekday: 'short',
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
    timeZoneName: 'short',
  })
}
