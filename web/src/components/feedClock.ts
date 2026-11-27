// The time of day on a feed card, said in the zone it was written in when that
// is not the viewer's own. A card for a trip, or a camera at another house,
// writes its times with that place's offset; read in the viewer's zone they
// would look right and be wrong, so such a time names the zone it is in.

export interface FeedClockOptions {
  // Show the seconds, as a camera's time does.
  isSecondShown?: boolean
  // Two-digit hour and minute ("06:41") rather than the locale's short form.
  isHourPadded?: boolean
  // The hour alone ("3 PM"), for a row of hourly forecasts.
  isHourOnly?: boolean
}

export interface FeedClock {
  clockText: string
  // "UTC+9" for a time written in a zone other than the viewer's; empty when
  // it is the viewer's own zone or the time carries no offset.
  zoneName: string
}

// writtenOffsetMinutes is the offset an RFC 3339 time was written with, in
// minutes east of UTC, or null when it names none. A trailing Z is the way a
// machine writes a moment, not a place, so it names none either: the time is
// read in the viewer's zone, as it always was.
export function writtenOffsetMinutes(written: string): number | null {
  const match = /(Z|[+-]\d{2}(?::?\d{2})?)$/i.exec(written.trim())
  if (!match) return null
  if (match[1].toUpperCase() === 'Z') return null
  const sign = match[1][0] === '-' ? -1 : 1
  const digits = match[1].slice(1).replace(':', '')
  const hours = Number(digits.slice(0, 2))
  const minutes = Number(digits.slice(2) || '0')
  return sign * (hours * 60 + minutes)
}

// zoneNameOf is "UTC", "UTC+9" or "UTC+5:30".
export function zoneNameOf(offsetMinutes: number): string {
  if (offsetMinutes === 0) return 'UTC'
  const sign = offsetMinutes < 0 ? '-' : '+'
  const absolute = Math.abs(offsetMinutes)
  const hours = Math.floor(absolute / 60)
  const minutes = absolute % 60
  return `UTC${sign}${hours}${minutes ? `:${String(minutes).padStart(2, '0')}` : ''}`
}

// feedClockOf is the time of day of a written moment. The viewer's offset at
// that moment is read from the browser unless the caller gives it (a test does).
export function feedClockOf(
  written: string,
  options: FeedClockOptions = {},
  viewerOffsetMinutes?: number,
): FeedClock | null {
  const moment = new Date(written)
  if (Number.isNaN(moment.getTime())) return null
  const format: Intl.DateTimeFormatOptions = {
    hour: options.isHourPadded ? '2-digit' : 'numeric',
    ...(options.isHourOnly ? {} : { minute: '2-digit' }),
    ...(options.isSecondShown ? { second: '2-digit' } : {}),
  }
  const writtenOffset = writtenOffsetMinutes(written)
  const viewerOffset = viewerOffsetMinutes ?? -moment.getTimezoneOffset()
  if (writtenOffset === null) {
    return { clockText: moment.toLocaleTimeString(undefined, format), zoneName: '' }
  }
  if (writtenOffset === viewerOffset) {
    // The viewer's own zone, read at the offset given rather than the
    // machine's, which a caller that passes one need not be in.
    const atViewer = new Date(moment.getTime() + viewerOffset * 60_000)
    return { clockText: atViewer.toLocaleTimeString(undefined, { ...format, timeZone: 'UTC' }), zoneName: '' }
  }
  // The wall clock of the written zone: shift the moment by its offset and
  // read the result as UTC.
  const shifted = new Date(moment.getTime() + writtenOffset * 60_000)
  return {
    clockText: shifted.toLocaleTimeString(undefined, { ...format, timeZone: 'UTC' }),
    zoneName: zoneNameOf(writtenOffset),
  }
}

// feedClockTextOf is the time with its zone named when it is not the viewer's.
export function feedClockTextOf(written: string, options: FeedClockOptions = {}, viewerOffsetMinutes?: number): string {
  const clock = feedClockOf(written, options, viewerOffsetMinutes)
  if (!clock) return ''
  return clock.zoneName ? `${clock.clockText} ${clock.zoneName}` : clock.clockText
}
