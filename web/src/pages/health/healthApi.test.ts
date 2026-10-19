import { describe, expect, it } from 'vitest'

import { en } from '../../i18n/en'
import {
  HealthPeriod,
  STEPS_UNIT,
  addDays,
  chartDays,
  displayUnit,
  numbersOf,
  quantityText,
  shownValue,
  summarizedDayCount,
} from './healthApi'

const t = (key: keyof typeof en, values?: Record<string, string | number>) =>
  en[key].replace(/\{(\w+)\}/g, (_, name: string) => String(values?.[name]))

function week(overrides: Partial<HealthPeriod> = {}): HealthPeriod {
  return {
    startDay: '2026-10-05',
    endDay: '2026-10-11',
    sumValue: 21000,
    averageValue: null,
    minimumValue: null,
    maximumValue: null,
    sampleCount: 30,
    dayCount: 3,
    durationSeconds: null,
    categorySeconds: null,
    ...overrides,
  }
}

describe('quantityText unit words', () => {
  const cases: [number, string, string][] = [
    // A heart rate is whole beats per minute.
    [74.3, 'count/min', '74 bpm'],
    // A count has no unit word; steps get theirs through the steps unit.
    [8402.4, 'count', '8,402'],
    [8402.4, STEPS_UNIT, '8,402 steps'],
    // A distance is kilometers with one decimal from 1000 meters.
    [8432, 'm', '8.4 km'],
    [12000, 'm', '12 km'],
    [950, 'm', '950 m'],
    [5.93, 'km', '5.9 km'],
    [36.6, 'degC', '36.6°C'],
    [42.4, 'ml/(kg*min)', '42.4 mL/kg·min'],
    [42.4, 'mL/(kg*min)', '42.4 mL/kg·min'],
    [512.4, 'kcal', '512 kcal'],
    // A percentage is the fraction times a hundred.
    [0.97, '%', '97%'],
    [0.035, '%', '3.5%'],
    // A unit the table does not know is written as it came.
    [3, 'made_up', '3 made_up'],
  ]
  it.each(cases)('%s %s is %s', (value, unit, expected) => {
    expect(quantityText(value, unit, 'en', t)).toBe(expected)
  })

  it('gives only steps the steps word', () => {
    expect(displayUnit('stepCount', 'count')).toBe(STEPS_UNIT)
    expect(displayUnit('flightsClimbed', 'count')).toBe('count')
    expect(displayUnit('heartRate', 'count/min')).toBe('count/min')
  })

  it('writes numbers in the language', () => {
    expect(quantityText(8402, STEPS_UNIT, 'de', t)).toBe('8.402 steps')
  })

  it('shows a percentage as the person reads it', () => {
    expect(shownValue(0.97, '%')).toBeCloseTo(97)
    expect(shownValue(0.97, 'kg')).toBe(0.97)
  })
})

describe('per day figures', () => {
  it('divides by the days with a summary, not the calendar days', () => {
    // A week the watch was worn on three days: 21000 steps is 7000 a day.
    expect(summarizedDayCount(week(), '2026-09-01', '2026-10-18')).toBe(3)
    expect(numbersOf([week()], 'sum', '2026-09-01', '2026-10-18')).toMatchObject({
      total: 21000,
      averagePerDay: 7000,
    })
  })

  it('adds the days of every period', () => {
    const later = week({ startDay: '2026-10-12', endDay: '2026-10-18', sumValue: 40000, dayCount: 4 })
    expect(numbersOf([week(), later], 'sum', '2026-09-01', '2026-10-18').averagePerDay).toBeCloseTo(61000 / 7)
  })

  it('falls back to the days the chart reaches over when the server sent no count', () => {
    expect(summarizedDayCount(week({ dayCount: undefined }), '2026-10-08', '2026-10-18')).toBe(4)
  })
})

describe('chartDays', () => {
  const today = '2026-10-18'

  it('reaches back the range', () => {
    expect(chartDays('30', 'day', today, null)).toMatchObject({ fromDay: addDays(today, -29), isCut: false })
  })

  it('cuts a chart by day to 400 days and says so', () => {
    expect(chartDays('all', 'day', today, '2010-01-01')).toMatchObject({
      fromDay: addDays(today, -399),
      isCut: true,
      mostPeriods: 400,
    })
  })

  it('cuts a chart by week so the server is never asked for more than 400 weeks', () => {
    const cut = chartDays('all', 'week', today, '2010-01-01')
    expect(cut.isCut).toBe(true)
    expect(cut.fromDay).toBe(addDays(today, 1 - 399 * 7))
    // A month is never cut: 400 of them are thirty-three years.
    expect(chartDays('all', 'month', today, '2010-01-01')).toMatchObject({ fromDay: '2010-01-01', isCut: false })
  })
})
