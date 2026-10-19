import { expect, it } from 'vitest'

import { dayNumbersFromCronField } from './agentBriefDays'

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
