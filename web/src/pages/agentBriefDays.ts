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
  if (field === '*') return [1, 2, 3, 4, 5, 6, 7]
  const days = new Set<number>()
  for (const piece of field.split(',')) {
    const ends = piece.split('-')
    if (ends.length > 2 || ends.some((end) => !/^\d$/.test(end))) return WEEKDAYS
    const low = Number(ends[0])
    const high = Number(ends[ends.length - 1])
    if (low > high || high > 7) return WEEKDAYS
    for (let day = low; day <= high; day++) days.add(day === 0 ? 7 : day)
  }
  return [...days].sort((first, second) => first - second)
}
