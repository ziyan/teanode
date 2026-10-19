import { useTranslation } from '../i18n/i18n'

// The days of the week as a row of toggle buttons, in the order the reader's
// week starts, each named in their language. A day is a number from 1
// (Monday) to 7 (Sunday), as the server counts them.
//
// Toggle buttons rather than checkboxes: seven short names in one row are
// read at a glance, where seven checkboxes with their labels are a column.
// Used by the calendar's custom repeat and by the morning brief's days, so
// the two pick days the same way.

export type WeekStart = 'sunday' | 'monday'

type NameStyle = 'long' | 'short'

// The name of a day in a language, from the browser's own formatter. 1
// January 2024 was a Monday, so the day number is the date.
export function weekdayName(weekday: number, language: string, style: NameStyle): string {
  return new Intl.DateTimeFormat(language, { weekday: style, timeZone: 'UTC' }).format(
    new Date(Date.UTC(2024, 0, weekday)),
  )
}

// The seven days in the order a week starts on, Monday first or Sunday first.
export function weekOrder(weekStart: WeekStart): number[] {
  return weekStart === 'monday' ? [1, 2, 3, 4, 5, 6, 7] : [7, 1, 2, 3, 4, 5, 6]
}

// Where the reader's week starts, for a place that has no calendar to say:
// what the browser knows about the language, and Sunday where it does not.
export function languageWeekStart(language: string): WeekStart {
  try {
    const locale = new Intl.Locale(language) as Intl.Locale & {
      getWeekInfo?: () => { firstDay: number }
      weekInfo?: { firstDay: number }
    }
    const firstDay = locale.getWeekInfo?.().firstDay ?? locale.weekInfo?.firstDay
    return firstDay === 1 ? 'monday' : 'sunday'
  } catch {
    return 'sunday'
  }
}

export function WeekdayButtons({
  label,
  weekdays,
  weekStart,
  disabled,
  onChange,
}: {
  // What the row of days is for, said to a screen reader as the group's name.
  label: string
  weekdays: number[]
  weekStart: WeekStart
  disabled?: boolean
  onChange: (weekdays: number[]) => void
}) {
  const { language } = useTranslation()
  const toggle = (weekday: number) =>
    onChange(
      weekdays.includes(weekday)
        ? weekdays.filter((chosen) => chosen !== weekday)
        : [...weekdays, weekday].sort((first, second) => first - second),
    )

  return (
    <div className="weekday-buttons" role="group" aria-label={label}>
      {weekOrder(weekStart).map((weekday) => (
        <button
          key={weekday}
          type="button"
          className={weekdays.includes(weekday) ? 'active' : undefined}
          aria-pressed={weekdays.includes(weekday)}
          aria-label={weekdayName(weekday, language, 'long')}
          disabled={disabled}
          onClick={() => toggle(weekday)}
        >
          {weekdayName(weekday, language, 'short')}
        </button>
      ))}
    </div>
  )
}
