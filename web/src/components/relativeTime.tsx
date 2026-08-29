import { useEffect, useState } from 'react'

import { useTranslation } from '../i18n/i18n'

// "5 minutes ago", with the exact time on hover.
//
// A list is read to answer "is this recent" — a relative label answers that
// at a glance, where a formatted date has to be compared against today's.
// The exact time is what you need once something looks wrong, so it is one
// hover away rather than gone. The tooltip names the zone: a timestamp
// without one is ambiguous the moment it is read anywhere but the machine
// that rendered it.

// The zero time a Go backend writes for a timestamp that was never set.
// Rendering it as "2025 years ago" is worse than rendering nothing.
// hasTime reports whether a timestamp names a real moment. Go's zero time
// arrives as "0001-01-01T00:00:00Z", which is a perfectly truthy string, so
// `value ? ... : ...` gets it wrong everywhere it is written.
export function hasTime(value: string | undefined | null): boolean {
  return parse(value) !== null
}

function parse(value: string | undefined | null): Date | null {
  if (!value) {
    return null
  }
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime()) || parsed.getUTCFullYear() <= 1) {
    return null
  }
  return parsed
}

const UNITS: { unit: Intl.RelativeTimeFormatUnit; seconds: number }[] = [
  { unit: 'year', seconds: 60 * 60 * 24 * 365 },
  { unit: 'month', seconds: 60 * 60 * 24 * 30 },
  { unit: 'week', seconds: 60 * 60 * 24 * 7 },
  { unit: 'day', seconds: 60 * 60 * 24 },
  { unit: 'hour', seconds: 60 * 60 },
  { unit: 'minute', seconds: 60 },
]

// formatRelative uses the platform's own rules, so Chinese and Japanese read
// correctly without a plural table of their own.
export function formatRelative(date: Date, language: string, now = Date.now()): string {
  const elapsed = (date.getTime() - now) / 1000
  const magnitude = Math.abs(elapsed)
  const format = new Intl.RelativeTimeFormat(language, { numeric: 'auto' })

  for (const { unit, seconds } of UNITS) {
    if (magnitude >= seconds) {
      return format.format(Math.round(elapsed / seconds), unit)
    }
  }
  // Under a minute. "now" reads better than "in 3 seconds" for a clock that
  // is a second out.
  return format.format(0, 'second')
}

export function formatAbsolute(date: Date): string {
  return date.toLocaleString(undefined, { timeZoneName: 'short' })
}

// How often the labels re-read the clock. A minute, because that is the
// smallest unit any of them show.
const TICK = 60_000

export function RelativeTime({ value }: { value: string | undefined | null }) {
  const { language } = useTranslation()
  const [, setTick] = useState(0)

  useEffect(() => {
    const timer = window.setInterval(() => setTick((previous) => previous + 1), TICK)
    return () => window.clearInterval(timer)
  }, [])

  const date = parse(value)
  if (!date) {
    return <span className="muted">—</span>
  }

  return (
    <time dateTime={date.toISOString()} title={formatAbsolute(date)}>
      {formatRelative(date, language)}
    </time>
  )
}
