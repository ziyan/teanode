// Correcting the date of a photo: RFC 3339 written at the offset the photo is
// shown at. The pieces are pure so that the dialog and its test share them.

// The offset as RFC 3339 writes it: +09:00 or -03:30, and +00:00 for UTC.
export function formatOffset(offsetMinutes: number): string {
  const sign = offsetMinutes < 0 ? '-' : '+'
  const absolute = Math.abs(offsetMinutes)
  const hours = String(Math.floor(absolute / 60)).padStart(2, '0')
  const minutes = String(absolute % 60).padStart(2, '0')
  return `${sign}${hours}:${minutes}`
}

// The moment as the wall clock shows it at the offset, as a date and time
// field holds it: 2030-07-14T09:05.
export function localFieldValue(instant: string, offsetMinutes: number): string {
  const moment = new Date(instant)
  if (Number.isNaN(moment.getTime())) return ''
  return new Date(moment.getTime() + offsetMinutes * 60_000).toISOString().slice(0, 16)
}

// The offset of the browser's clock at a moment, in minutes east of UTC.
export function browserOffsetMinutes(moment: Date): number {
  return -moment.getTimezoneOffset()
}

// The date and time field's value written as RFC 3339 at an offset, or null
// when the field holds no date. Seconds are written as zero: the field asks
// for minutes.
export function takenAtFromField(fieldValue: string, offsetMinutes: number): string | null {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2})?$/.test(fieldValue)) return null
  const written = fieldValue.length === 16 ? `${fieldValue}:00` : fieldValue
  const text = `${written}${formatOffset(offsetMinutes)}`
  return Number.isNaN(new Date(text).getTime()) ? null : text
}
