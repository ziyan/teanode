import { describe, expect, it } from 'vitest'

import { freeByDay, minutesOf } from './freeTime'

// Two people's busy times on one day, as the server sends them: ada 10:00 to
// 11:00 and ben 10:30 to 12:00 and 15:00 to 16:00, in a 09:00 to 17:00 day.
// The command line's free-together test (internal/cmd/calendar_share_test.go) expects the same
// stretches of the same busy times.
function at(hours: number, minutes = 0): string {
  return new Date(2030, 0, 2, hours, minutes).toISOString()
}

describe('freeByDay', () => {
  it('leaves what none of them is busy in, with overlaps made one', () => {
    const busy = [
      { startsAt: at(10), endsAt: at(11) },
      { startsAt: at(10, 30), endsAt: at(12) },
      { startsAt: at(15), endsAt: at(16) },
    ]
    const free = freeByDay(busy, new Date(2030, 0, 2), new Date(2030, 0, 3), 9 * 60, 17 * 60)
    expect(free.map((each) => [each.startsAt.getHours(), each.endsAt.getHours()])).toEqual([
      [9, 10],
      [12, 15],
      [16, 17],
    ])
  })

  it('offers a whole day to people with nothing booked, and each day on its own', () => {
    const free = freeByDay([], new Date(2030, 0, 2, 14), new Date(2030, 0, 4), 9 * 60, 17 * 60)
    expect(free).toHaveLength(2)
    expect(free[0].startsAt.getDate()).toBe(2)
    expect(free[1].startsAt.getDate()).toBe(3)
    expect(free[1].endsAt.getHours()).toBe(17)
  })

  it('clips a period that starts before the day and offers nothing when the day is taken', () => {
    const free = freeByDay(
      [{ startsAt: new Date(2030, 0, 1, 22).toISOString(), endsAt: new Date(2030, 0, 2, 10).toISOString() }],
      new Date(2030, 0, 2),
      new Date(2030, 0, 3),
      9 * 60,
      17 * 60,
    )
    expect(free).toHaveLength(1)
    expect(free[0].startsAt.getHours()).toBe(10)
    const taken = freeByDay(
      [{ startsAt: at(8), endsAt: at(18) }],
      new Date(2030, 0, 2),
      new Date(2030, 0, 3),
      9 * 60,
      17 * 60,
    )
    expect(taken).toEqual([])
  })
})

describe('minutesOf', () => {
  it('reads a time of day and refuses what is not one', () => {
    expect(minutesOf('09:30')).toBe(570)
    expect(minutesOf('9:05')).toBe(545)
    expect(minutesOf('24:00')).toBe(1440)
    for (const typed of ['', '9', '25:00', '10:60', 'noon']) expect(minutesOf(typed)).toBe(-1)
  })
})
