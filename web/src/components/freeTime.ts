// Working out when people are all free from when each is busy. The same sum
// the command line and the agent make (calendar.FreeByDay on the server side
// of them), so that the time one offers is the time the others offer: day by
// day, between an earliest and a latest hour, with everyone's busy times
// merged, so what is left is when all of them could meet.

export type BusyPeriod = { startsAt: string; endsAt: string }

export type FreeStretch = { day: Date; startsAt: Date; endsAt: Date }

// mergedBusy is the busy periods that touch a window, clipped to it, in order,
// with overlapping or touching ones made one.
function mergedBusy(busy: BusyPeriod[], opens: number, closes: number): Array<[number, number]> {
  const clipped: Array<[number, number]> = []
  for (const period of busy) {
    const starts = Math.max(Date.parse(period.startsAt), opens)
    const ends = Math.min(Date.parse(period.endsAt), closes)
    if (Number.isFinite(starts) && Number.isFinite(ends) && starts < ends) clipped.push([starts, ends])
  }
  clipped.sort((first, second) => first[0] - second[0])
  const merged: Array<[number, number]> = []
  for (const each of clipped) {
    const last = merged[merged.length - 1]
    if (last && each[0] <= last[1]) last[1] = Math.max(last[1], each[1])
    else merged.push([each[0], each[1]])
  }
  return merged
}

// atMinutes is the moment that many minutes into a day, in the browser's zone.
function atMinutes(day: Date, minutes: number): Date {
  return new Date(day.getFullYear(), day.getMonth(), day.getDate(), 0, minutes)
}

// freeByDay is the stretches nobody in `busy` is busy, for each day from
// `first` up to but not including `dayAfterLast`, between the earliest and
// the latest minute of the day.
export function freeByDay(
  busy: BusyPeriod[],
  first: Date,
  dayAfterLast: Date,
  earliestMinutes: number,
  latestMinutes: number,
): FreeStretch[] {
  const free: FreeStretch[] = []
  for (
    let day = new Date(first.getFullYear(), first.getMonth(), first.getDate());
    day < dayAfterLast;
    day = new Date(day.getFullYear(), day.getMonth(), day.getDate() + 1)
  ) {
    const opens = atMinutes(day, earliestMinutes)
    const closes = atMinutes(day, latestMinutes)
    let cursor = opens.getTime()
    for (const [starts, ends] of mergedBusy(busy, cursor, closes.getTime())) {
      if (starts > cursor) free.push({ day, startsAt: new Date(cursor), endsAt: new Date(starts) })
      cursor = Math.max(cursor, ends)
    }
    if (cursor < closes.getTime()) free.push({ day, startsAt: new Date(cursor), endsAt: closes })
  }
  return free
}

// minutesOf reads a time of day typed as HH:MM, or -1 for anything else.
export function minutesOf(text: string): number {
  const match = /^(\d{1,2}):(\d{2})$/.exec(text.trim())
  if (!match) return -1
  const hours = Number(match[1])
  const minutes = Number(match[2])
  return hours > 24 || minutes > 59 || (hours === 24 && minutes > 0) ? -1 : hours * 60 + minutes
}
