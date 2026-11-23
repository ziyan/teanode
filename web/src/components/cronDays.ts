// The days the morning brief comes on, read from the weekday field of its
// cron line, as numbers from 1 (Monday) to 7 (Sunday) in order. The server and
// the agent read the same numbers, and the iPhone app reads the field the same
// way (ScheduleWeekday.dayNumbers in TeaNodeKit).
//
// A field is "*", or numbers, ranges and lists of them, with 0 and 7 both
// Sunday as cron has them. A field it cannot read (a step such as "*/2", or
// day names) is the weekdays, which is what a brief starts as.
const WEEKDAYS = [1, 2, 3, 4, 5]

export function dayNumbersFromCronField(field: string): number[] {
  return readDayNumbers(field) ?? WEEKDAYS
}

// readDayNumbers is the same reading, or null for a field it cannot read, so
// that a caller that has to tell "weekdays" from "something else" can.
export function readDayNumbers(field: string): number[] | null {
  if (field === '*') return [1, 2, 3, 4, 5, 6, 7]
  const days = new Set<number>()
  for (const piece of field.split(',')) {
    const ends = piece.split('-')
    if (ends.length > 2 || ends.some((end) => !/^\d$/.test(end))) return null
    const low = Number(ends[0])
    const high = Number(ends[ends.length - 1])
    if (low > high || high > 7) return null
    for (let day = low; day <= high; day++) days.add(day === 0 ? 7 : day)
  }
  return [...days].sort((first, second) => first - second)
}

// cronFieldFromDayNumbers writes days (1 Monday to 7 Sunday) as the weekday
// field of a cron line: "*" for every day, a range for a run of three or
// more days from Monday to Saturday, Sunday as 0.
export function cronFieldFromDayNumbers(days: number[]): string {
  const sorted = [...new Set(days)].sort((first, second) => first - second)
  if (sorted.length === 7) return '*'
  const pieces: string[] = []
  let at = 0
  while (at < sorted.length) {
    let end = at
    while (end + 1 < sorted.length && sorted[end + 1] === sorted[end] + 1 && sorted[end + 1] < 7) end++
    if (end - at >= 2) pieces.push(`${sorted[at]}-${sorted[end]}`)
    else for (let index = at; index <= end; index++) pieces.push(String(sorted[index] % 7))
    at = end + 1
  }
  // Sunday is written 0, which sorts first in cron but last in a week.
  return pieces.join(',')
}
