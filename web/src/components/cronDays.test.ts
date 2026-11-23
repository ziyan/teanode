import { expect, it } from 'vitest'

import { cronFieldFromDayNumbers, dayNumbersFromCronField, readDayNumbers } from './cronDays'

it('reads the days of a cron line the way the iPhone does', () => {
  expect(dayNumbersFromCronField('*')).toEqual([1, 2, 3, 4, 5, 6, 7])
  expect(dayNumbersFromCronField('1-5')).toEqual([1, 2, 3, 4, 5])
  expect(dayNumbersFromCronField('5,1,3')).toEqual([1, 3, 5])
  // Cron has Sunday as both 0 and 7; the server counts it as 7.
  expect(dayNumbersFromCronField('0')).toEqual([7])
  expect(dayNumbersFromCronField('0-2')).toEqual([1, 2, 7])
  expect(dayNumbersFromCronField('6,7,0')).toEqual([6, 7])
})

it('takes the weekdays for a field it cannot read', () => {
  for (const field of ['*/2', '5-1', '8', '1,', 'MON', '']) {
    expect(dayNumbersFromCronField(field), field).toEqual([1, 2, 3, 4, 5])
  }
})

it('writes days as a cron field and reads them back', () => {
  expect(cronFieldFromDayNumbers([1, 2, 3, 4, 5])).toBe('1-5')
  expect(cronFieldFromDayNumbers([1, 2, 3, 4, 5, 6, 7])).toBe('*')
  expect(cronFieldFromDayNumbers([6, 7])).toBe('6,0')
  expect(cronFieldFromDayNumbers([5, 1, 3])).toBe('1,3,5')
  expect(readDayNumbers('6,0')).toEqual([6, 7])
  expect(readDayNumbers('*/2')).toBeNull()
})
