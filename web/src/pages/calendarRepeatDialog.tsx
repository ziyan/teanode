import { useState } from 'react'

import { FormDialog } from '../components/dialog'
import { WeekdayButtons, weekdayName, type WeekStart } from '../components/weekdayButtons'
import { useTranslation, type Key } from '../i18n/i18n'
import {
  defaultParts,
  describeRepeat,
  editedRepeat,
  isSameRepeat,
  MONTHLY_ORDINALS,
  ordinalName,
  ordinalOf,
  parseRepeat,
  WEEKDAY_NUMBERS,
  weekdayOf,
  type RepeatContext,
  type RepeatFrequency,
  type RepeatParts,
} from './calendarRepeat'

const FREQUENCIES: RepeatFrequency[] = ['daily', 'weekly', 'monthly', 'yearly']

// The names a catalog carries for each frequency, written out so that a
// missing one is a type error rather than a blank.
const FREQUENCY_KEYS: Record<RepeatFrequency, Key> = {
  daily: 'calendar.repeatFrequencyDaily',
  weekly: 'calendar.repeatFrequencyWeekly',
  monthly: 'calendar.repeatFrequencyMonthly',
  yearly: 'calendar.repeatFrequencyYearly',
}

const UNIT_KEYS: Record<RepeatFrequency, { one: Key; other: Key }> = {
  daily: { one: 'calendar.repeatUnitDailyOne', other: 'calendar.repeatUnitDailyOther' },
  weekly: { one: 'calendar.repeatUnitWeeklyOne', other: 'calendar.repeatUnitWeeklyOther' },
  monthly: { one: 'calendar.repeatUnitMonthlyOne', other: 'calendar.repeatUnitMonthlyOther' },
  yearly: { one: 'calendar.repeatUnitYearlyOne', other: 'calendar.repeatUnitYearlyOther' },
}

// A whole number the person types. While the box is being edited it may hold
// text that is not a number yet (empty, after a backspace), which is shown as
// typed and not committed; leaving the box shows the committed number again.
function WholeNumberInput({
  value,
  minimum,
  maximum,
  label,
  onChange,
}: {
  value: number
  minimum: number
  maximum: number
  label: string
  onChange: (value: number) => void
}) {
  const [typed, setTyped] = useState<string | null>(null)
  return (
    <input
      type="number"
      inputMode="numeric"
      min={minimum}
      max={maximum}
      aria-label={label}
      value={typed ?? String(value)}
      onChange={(event) => {
        setTyped(event.target.value)
        const number = Number(event.target.value)
        if (Number.isInteger(number) && number >= minimum && number <= maximum) onChange(number)
      }}
      onBlur={() => setTyped(null)}
    />
  )
}

// A year after a date: the day a repeat that is told to end on a date starts
// out ending on.
function yearAfter(date: string): string {
  const year = Number(date.slice(0, 4))
  return Number.isNaN(year) ? date : `${year + 1}${date.slice(4)}`
}

// RepeatDialog is the detailed editor behind Custom in the event's Repeat
// list: how often, on which days, and when it stops, with the sentence it
// makes under it. It hands back the rule, or the rule it was given when the
// person changed nothing, so that a rule set elsewhere survives being looked
// at.
export function RepeatDialog({
  rule,
  context,
  weekStart,
  onSave,
  onClose,
}: {
  rule: string
  context: RepeatContext
  weekStart: WeekStart
  onSave: (rule: string) => void
  onClose: () => void
}) {
  const translation = useTranslation()
  const { t, plural, language } = translation
  // What the editor opens on: the rule's parts, or a weekly repeat when the
  // rule is empty or is text it cannot read. Read once, since the dialog is
  // made for one rule and one start.
  const [initial] = useState<RepeatParts>(() => parseRepeat(rule, context) ?? defaultParts('weekly', context))
  const [isReadable] = useState(() => rule.trim() === '' || parseRepeat(rule, context) !== null)
  const [parts, setParts] = useState<RepeatParts>(initial)
  const startWeekday = weekdayOf(context.startDate) || 1

  const change = (changes: Partial<RepeatParts>) => setParts({ ...parts, ...changes })
  const monthly = parts.monthly
  const end = parts.end
  const canSubmit =
    (parts.frequency !== 'weekly' || parts.weekdays.length > 0) &&
    (end.kind !== 'date' || (end.endDate !== '' && end.endDate >= context.startDate))

  return (
    <FormDialog
      title={t('calendar.repeatCustomTitle')}
      submitLabel={t('common.done')}
      canSubmit={canSubmit}
      onClose={onClose}
      onSubmit={() => onSave(editedRepeat(rule, initial, parts, context))}
    >
      <label>
        {t('calendar.repeatFrequency')}
        <select
          value={parts.frequency}
          onChange={(event) => change({ frequency: event.target.value as RepeatFrequency })}
        >
          {FREQUENCIES.map((frequency) => (
            <option key={frequency} value={frequency}>
              {t(FREQUENCY_KEYS[frequency])}
            </option>
          ))}
        </select>
      </label>
      <label>
        {t('calendar.repeatInterval')}
        <span className="repeat-interval">
          <WholeNumberInput
            value={parts.intervalCount}
            minimum={1}
            maximum={999}
            label={t('calendar.repeatInterval')}
            onChange={(intervalCount) => change({ intervalCount })}
          />
          <span>{plural(parts.intervalCount, UNIT_KEYS[parts.frequency])}</span>
        </span>
      </label>
      {parts.frequency === 'weekly' && (
        <>
          <div className="field-label">{t('calendar.repeatOnDays')}</div>
          <WeekdayButtons
            label={t('calendar.repeatOnDays')}
            weekdays={parts.weekdays}
            weekStart={weekStart}
            onChange={(weekdays) => change({ weekdays })}
          />
          {parts.weekdays.length === 0 && <p className="muted field-hint">{t('calendar.repeatNoDays')}</p>}
        </>
      )}
      {parts.frequency === 'monthly' && (
        <>
          <label>
            {t('calendar.repeatMonthlyOn')}
            <select
              value={monthly.kind}
              onChange={(event) =>
                change({
                  monthly:
                    event.target.value === 'ordinalWeekday'
                      ? { kind: 'ordinalWeekday', ordinal: ordinalOf(context.startDate), weekday: startWeekday }
                      : defaultParts('monthly', context).monthly,
                })
              }
            >
              <option value="dayOfMonth">{t('calendar.repeatMonthDay')}</option>
              <option value="ordinalWeekday">{t('calendar.repeatMonthWeekday')}</option>
            </select>
          </label>
          {monthly.kind === 'dayOfMonth' ? (
            <label>
              {t('calendar.repeatDayNumber')}
              <WholeNumberInput
                value={monthly.dayOfMonth}
                minimum={1}
                maximum={31}
                label={t('calendar.repeatDayNumber')}
                onChange={(dayOfMonth) => change({ monthly: { kind: 'dayOfMonth', dayOfMonth } })}
              />
            </label>
          ) : (
            <div className="field-row">
              <label>
                {t('calendar.repeatOrdinalLabel')}
                <select
                  value={monthly.ordinal}
                  onChange={(event) => change({ monthly: { ...monthly, ordinal: Number(event.target.value) } })}
                >
                  {MONTHLY_ORDINALS.map((ordinal) => (
                    <option key={ordinal} value={ordinal}>
                      {ordinalName(ordinal, t)}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                {t('calendar.repeatWeekdayLabel')}
                <select
                  value={monthly.weekday}
                  onChange={(event) => change({ monthly: { ...monthly, weekday: Number(event.target.value) } })}
                >
                  {WEEKDAY_NUMBERS.map((weekday) => (
                    <option key={weekday} value={weekday}>
                      {weekdayName(weekday, language, 'long')}
                    </option>
                  ))}
                </select>
              </label>
            </div>
          )}
        </>
      )}
      <label>
        {t('calendar.repeatEnds')}
        <select
          value={end.kind}
          onChange={(event) => {
            const kind = event.target.value
            change({
              end:
                kind === 'date'
                  ? { kind: 'date', endDate: yearAfter(context.startDate) }
                  : kind === 'count'
                    ? { kind: 'count', occurrenceCount: 10 }
                    : { kind: 'never' },
            })
          }}
        >
          <option value="never">{t('calendar.repeatEndNever')}</option>
          <option value="date">{t('calendar.repeatEndDate')}</option>
          <option value="count">{t('calendar.repeatEndCount')}</option>
        </select>
      </label>
      {end.kind === 'date' && (
        <label>
          {t('calendar.repeatEndDateLabel')}
          <input
            type="date"
            min={context.startDate}
            value={end.endDate}
            onChange={(event) => change({ end: { kind: 'date', endDate: event.target.value } })}
          />
        </label>
      )}
      {end.kind === 'count' && (
        <label>
          {t('calendar.repeatEndCount')}
          <span className="repeat-interval">
            <WholeNumberInput
              value={end.occurrenceCount}
              minimum={1}
              maximum={999}
              label={t('calendar.repeatEndCount')}
              onChange={(occurrenceCount) => change({ end: { kind: 'count', occurrenceCount } })}
            />
            <span>
              {plural(end.occurrenceCount, {
                one: 'calendar.repeatEndCountOne',
                other: 'calendar.repeatEndCountOther',
              })}
            </span>
          </span>
        </label>
      )}
      {canSubmit && <p className="muted">{describeRepeat(parts, translation)}</p>}
      {(!isReadable || initial.hasUnshownParts) && isSameRepeat(parts, initial) && (
        <p className="muted field-hint">{t('calendar.repeatKept')}</p>
      )}
    </FormDialog>
  )
}
