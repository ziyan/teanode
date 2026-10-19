import { describe, expect, it } from 'vitest'

import { en } from '../../i18n/en'
import { HealthPeriod, addDays, chartDays, numbersOf, quantityText, shownValue, summarizedDayCount } from './healthApi'

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

describe('quantityText', () => {
  // The cases of words_test.go are in healthWords.test.ts; these are the ones
  // the dashboard's charts depend on.
  const cases: [number, string, string, string][] = [
    // A heart rate is whole beats per minute.
    [74.3, 'count/min', 'heartRate', '74 bpm'],
    // A body mass index keeps its decimal; it is a count of nothing.
    [22.4, 'count', 'bodyMassIndex', '22.4'],
    [7.4, 'count', 'uvExposure', '7.4'],
    // A distance is kilometers with one decimal from 1000 meters.
    [8432, 'm', 'distanceWalkingRunning', '8.4 km'],
    [12000, 'm', 'distanceWalkingRunning', '12 km'],
    [950, 'm', 'distanceWalkingRunning', '950 m'],
    // A percentage is the fraction times a hundred.
    [0.97, '%', 'oxygenSaturation', '97%'],
    [0.035, '%', 'atrialFibrillationBurden', '3.5%'],
    // A unit the table does not know is written as it came.
    [3, 'made_up', 'x', '3 made_up'],
  ]
  it.each(cases)('%s %s of %s is %s', (measuredValue, quantityUnit, healthType, expected) => {
    expect(quantityText(measuredValue, quantityUnit, healthType, 'en', t)).toBe(expected)
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
