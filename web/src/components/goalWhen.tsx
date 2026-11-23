import { MailboxRuleCondition } from '../api'
import { Key, useTranslation } from '../i18n/i18n'
import { cronFieldFromDayNumbers, readDayNumbers } from './cronDays'
import { TrashIcon } from './icons'
import {
  GOAL_MAIL_FIELDS,
  MailConditionsEditor,
  cleanMailConditions,
  describeMailWhen,
  isMailConditionComplete,
  newMailConditions,
} from './mailConditions'
import { Select } from './select'
import { Tooltip } from './tooltip'
import { WeekdayButtons, languageWeekStart, weekdayName } from './weekdayButtons'

// When a goal runs, in words and as a form. A goal's times are each a cron
// line in the person's zone ("30 7 * * 1-5" is weekdays at 7:30) or one
// moment ("@at 2026-09-12 09:00"); with none it keeps its own pace. It can
// also take a turn when mail arrives. The goals list and a goal's page say
// it in words; starting a goal and editing its When use the same form.

type Translate = (key: Key, values?: Record<string, string | number>) => string

const CRON_LINE = /^(\d{1,2}) (\d{1,2}) \* \* (\S+)$/
const MOMENT_LINE = /^@at (\d{4}-\d{2}-\d{2}) (\d{1,2}:\d{2})$/

function clockTime(hour: string | number, minute: string | number): string {
  return `${Number(hour)}:${String(minute).padStart(2, '0')}`
}

// describeGoalTime is one of a goal's times in words: "Weekdays at 7:30",
// "Every Monday at 9:00", "On 11 Dec at 9:00". A line it cannot put in
// words is shown as it is.
export function describeGoalTime(goalTime: string, t: Translate, language: string): string {
  const moment = MOMENT_LINE.exec(goalTime.trim())
  if (moment) {
    const [year, month, day] = moment[1].split('-').map(Number)
    const date = new Intl.DateTimeFormat(language, { day: 'numeric', month: 'short', timeZone: 'UTC' }).format(
      new Date(Date.UTC(year, month - 1, day)),
    )
    const [hour, minute] = moment[2].split(':')
    return t('goals.whenMoment', { date, time: clockTime(hour, minute) })
  }
  const cron = CRON_LINE.exec(goalTime.trim())
  const days = cron ? readDayNumbers(cron[3]) : null
  if (!cron || !days || days.length === 0 || Number(cron[1]) > 59 || Number(cron[2]) > 23) return goalTime
  const time = clockTime(cron[2], cron[1])
  const key = days.join(',')
  if (key === '1,2,3,4,5,6,7') return t('goals.whenEveryDay', { time })
  if (key === '1,2,3,4,5') return t('goals.whenWeekdays', { time })
  if (key === '6,7') return t('goals.whenWeekends', { time })
  if (days.length === 1) return t('goals.whenOneDay', { day: weekdayName(days[0], language, 'long'), time })
  return t('goals.whenSomeDays', { days: days.map((day) => weekdayName(day, language, 'short')).join(', '), time })
}

// describeGoalWhen is when a goal runs, for a list: its times and the mail it
// waits for, in words, joined; an empty string for a goal at its own pace.
export function describeGoalWhen(
  goal: { goalTimes: string[]; goalMailConditions: MailboxRuleCondition[] },
  t: Translate,
  language: string,
): string {
  const parts = goal.goalTimes.map((goalTime) => describeGoalTime(goalTime, t, language))
  if (goal.goalMailConditions.length > 0) parts.push(describeMailWhen(goal.goalMailConditions, t))
  try {
    return new Intl.ListFormat(language, { style: 'long', type: 'conjunction' }).format(parts)
  } catch {
    return parts.join(', ')
  }
}

export type WhenMode = 'ownPace' | 'setTimes' | 'mail'

// One of the times being edited: days and a time of day, a single moment, or
// a cron line written out for the ones the first two cannot say.
export interface TimeDraft {
  kind: 'repeat' | 'moment' | 'cron'
  days: number[]
  time: string
  date: string
  cron: string
}

export interface WhenDraft {
  whenMode: WhenMode
  timeDrafts: TimeDraft[]
  mailConditions: MailboxRuleCondition[]
  // The mail conditions a goal that has both times and mail came with,
  // kept as they were while the form shows its times.
  keptMailConditions: MailboxRuleCondition[]
  isNotifiedWhenPosted: boolean
}

function newTimeDraft(): TimeDraft {
  return { kind: 'repeat', days: [1, 2, 3, 4, 5], time: '07:30', date: '', cron: '' }
}

function padTime(hour: string, minute: string): string {
  return `${hour.padStart(2, '0')}:${minute.padStart(2, '0')}`
}

function timeDraftFrom(goalTime: string): TimeDraft {
  const moment = MOMENT_LINE.exec(goalTime.trim())
  if (moment) {
    const [hour, minute] = moment[2].split(':')
    return { ...newTimeDraft(), kind: 'moment', date: moment[1], time: padTime(hour, minute) }
  }
  const cron = CRON_LINE.exec(goalTime.trim())
  const days = cron ? readDayNumbers(cron[3]) : null
  if (cron && days && days.length > 0 && Number(cron[1]) <= 59 && Number(cron[2]) <= 23) {
    return { ...newTimeDraft(), kind: 'repeat', days, time: padTime(cron[2], cron[1]) }
  }
  return { ...newTimeDraft(), kind: 'cron', cron: goalTime }
}

// whenDraftFrom is the form's start: empty for a new goal, a goal's own
// times and mail when it is edited.
export function whenDraftFrom(goal?: {
  goalTimes: string[]
  goalMailConditions: MailboxRuleCondition[]
  isGoalNotifiedWhenPosted: boolean
}): WhenDraft {
  const goalTimes = goal?.goalTimes ?? []
  const mailConditions = goal?.goalMailConditions ?? []
  const whenMode: WhenMode = goalTimes.length > 0 ? 'setTimes' : mailConditions.length > 0 ? 'mail' : 'ownPace'
  return {
    whenMode,
    timeDrafts: goalTimes.length > 0 ? goalTimes.map(timeDraftFrom) : [newTimeDraft()],
    mailConditions:
      mailConditions.length > 0
        ? mailConditions.map((condition) => ({
            field: condition.field,
            header: condition.header ?? '',
            operator: condition.operator,
            value: condition.value ?? '',
          }))
        : newMailConditions(),
    keptMailConditions: goalTimes.length > 0 ? mailConditions : [],
    isNotifiedWhenPosted: goal?.isGoalNotifiedWhenPosted ?? false,
  }
}

function goalTimeFrom(timeDraft: TimeDraft): string {
  if (timeDraft.kind === 'cron') return timeDraft.cron.trim()
  const [hour, minute] = (timeDraft.time || '00:00').split(':').map(Number)
  if (timeDraft.kind === 'moment') return `@at ${timeDraft.date} ${padTime(String(hour), String(minute))}`
  return `${minute} ${hour} * * ${cronFieldFromDayNumbers(timeDraft.days)}`
}

function isTimeDraftComplete(timeDraft: TimeDraft): boolean {
  if (timeDraft.kind === 'cron') return timeDraft.cron.trim() !== ''
  if (timeDraft.time === '') return false
  if (timeDraft.kind === 'moment') return timeDraft.date !== ''
  return timeDraft.days.length > 0
}

// isWhenDraftComplete says whether the form can be saved.
export function isWhenDraftComplete(draft: WhenDraft): boolean {
  if (draft.whenMode === 'setTimes') return draft.timeDrafts.length > 0 && draft.timeDrafts.every(isTimeDraftComplete)
  if (draft.whenMode === 'mail') return draft.mailConditions.every(isMailConditionComplete)
  return true
}

// whenVariables is what a draft says to the server: the goal's times, the
// mail it waits for, and the notice, under the names the operations take.
export function whenVariables(draft: WhenDraft): {
  goalTimes: string[]
  goalMailConditions: MailboxRuleCondition[]
  isGoalNotifiedWhenPosted: boolean
} {
  if (draft.whenMode === 'setTimes') {
    return {
      goalTimes: draft.timeDrafts.map(goalTimeFrom),
      goalMailConditions: draft.keptMailConditions,
      isGoalNotifiedWhenPosted: draft.isNotifiedWhenPosted,
    }
  }
  if (draft.whenMode === 'mail') {
    return {
      goalTimes: [],
      goalMailConditions: cleanMailConditions(draft.mailConditions),
      isGoalNotifiedWhenPosted: draft.isNotifiedWhenPosted,
    }
  }
  return { goalTimes: [], goalMailConditions: [], isGoalNotifiedWhenPosted: draft.isNotifiedWhenPosted }
}

const MODES: { whenMode: WhenMode; label: Key }[] = [
  { whenMode: 'ownPace', label: 'goals.whenOwnPace' },
  { whenMode: 'setTimes', label: 'goals.whenSetTimes' },
  { whenMode: 'mail', label: 'goals.whenMailArrives' },
]

// GoalWhenEditor is the form for when a goal runs: at its own pace, at set
// times (several can be added and removed), or when mail arrives.
export function GoalWhenEditor({
  draft,
  onChange,
  isDisabled,
}: {
  draft: WhenDraft
  onChange: (draft: WhenDraft) => void
  isDisabled?: boolean
}) {
  const { t, language } = useTranslation()
  const changeTime = (index: number, change: Partial<TimeDraft>) =>
    onChange({
      ...draft,
      timeDrafts: draft.timeDrafts.map((timeDraft, at) => (at === index ? { ...timeDraft, ...change } : timeDraft)),
    })
  return (
    <>
      <div className="field-label">{t('goals.when')}</div>
      <div className="segmented" role="group" aria-label={t('goals.when')}>
        {MODES.map((mode) => (
          <button
            key={mode.whenMode}
            type="button"
            className={draft.whenMode === mode.whenMode ? 'active' : undefined}
            aria-pressed={draft.whenMode === mode.whenMode}
            disabled={isDisabled}
            onClick={() => onChange({ ...draft, whenMode: mode.whenMode })}
          >
            {t(mode.label)}
          </button>
        ))}
      </div>
      {draft.whenMode === 'ownPace' ? <p className="muted">{t('goals.whenOwnPaceHint')}</p> : null}
      {draft.whenMode === 'setTimes' ? (
        <>
          {draft.timeDrafts.map((timeDraft, index) => (
            <div className="goal-time" key={index}>
              <Select
                label={t('goals.timeKind')}
                value={timeDraft.kind}
                options={[
                  { value: 'repeat', label: t('goals.timeRepeat') },
                  { value: 'moment', label: t('goals.timeMoment') },
                  { value: 'cron', label: t('goals.timeCron') },
                ]}
                onChange={(value) => changeTime(index, { kind: value as TimeDraft['kind'] })}
              />
              {timeDraft.kind === 'repeat' ? (
                <WeekdayButtons
                  label={t('goals.timeDays')}
                  weekdays={timeDraft.days}
                  weekStart={languageWeekStart(language)}
                  disabled={isDisabled}
                  onChange={(days) => changeTime(index, { days })}
                />
              ) : null}
              {timeDraft.kind === 'moment' ? (
                <input
                  type="date"
                  aria-label={t('goals.timeDate')}
                  value={timeDraft.date}
                  disabled={isDisabled}
                  onChange={(event) => changeTime(index, { date: event.target.value })}
                />
              ) : null}
              {timeDraft.kind === 'cron' ? (
                <input
                  aria-label={t('goals.timeCronLine')}
                  placeholder="30 7 * * 1-5"
                  value={timeDraft.cron}
                  disabled={isDisabled}
                  onChange={(event) => changeTime(index, { cron: event.target.value })}
                />
              ) : (
                <input
                  type="time"
                  aria-label={t('goals.timeOfDay')}
                  value={timeDraft.time}
                  disabled={isDisabled}
                  onChange={(event) => changeTime(index, { time: event.target.value })}
                />
              )}
              <Tooltip label={t('common.remove')}>
                <button
                  type="button"
                  className="icon-action danger"
                  aria-label={t('common.remove')}
                  disabled={isDisabled || draft.timeDrafts.length === 1}
                  onClick={() => onChange({ ...draft, timeDrafts: draft.timeDrafts.filter((_, at) => at !== index) })}
                >
                  <TrashIcon size={16} />
                </button>
              </Tooltip>
            </div>
          ))}
          <div>
            <button
              type="button"
              disabled={isDisabled}
              onClick={() => onChange({ ...draft, timeDrafts: [...draft.timeDrafts, newTimeDraft()] })}
            >
              {t('goals.addTime')}
            </button>
          </div>
          <p className="muted">{t('goals.timeHint')}</p>
        </>
      ) : null}
      {draft.whenMode === 'mail' ? (
        <>
          <div className="field-label">{t('mailboxSettings.conditions')}</div>
          <MailConditionsEditor
            conditions={draft.mailConditions}
            onChange={(mailConditions) => onChange({ ...draft, mailConditions })}
            fields={GOAL_MAIL_FIELDS}
          />
          <p className="muted">{t('goals.mailHint')}</p>
        </>
      ) : null}
      {draft.whenMode === 'setTimes' ? (
        <label className="checkbox">
          <input
            type="checkbox"
            checked={draft.isNotifiedWhenPosted}
            disabled={isDisabled}
            onChange={(event) => onChange({ ...draft, isNotifiedWhenPosted: event.target.checked })}
          />
          {t('goals.notifyWhenPosted')}
        </label>
      ) : null}
    </>
  )
}
