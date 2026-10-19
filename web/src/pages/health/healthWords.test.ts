/// <reference types="node" />
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

import { en } from '../../i18n/en'
import {
  characteristicText,
  durationText,
  humanize,
  quantityText,
  stageName,
  unitWords,
  workoutName,
} from './healthApi'

// The words of Health are written once, in the table at the top of
// internal/health/words.go, and three programs follow it: the command line and
// the agent's tool in Go, the dashboard here, and the iPhone app in Swift.
// These are the cases words_test.go pins, run through the dashboard's
// functions, so that a rule changed on one side shows up as a failing test on
// the other. A case added to words_test.go belongs here too.

const t = (key: keyof typeof en, values?: Record<string, string | number>) =>
  en[key].replace(/\{(\w+)\}/g, (_, name: string) => String(values?.[name]))

describe('quantityText, the cases of TestValueWords', () => {
  const cases: [string, number, string, string][] = [
    ['stepCount', 8412.4, 'count', '8,412 steps'],
    ['stepCount', 1, 'count', '1 step'],
    ['flightsClimbed', 12, 'count', '12 flights'],
    ['bodyMassIndex', 22.456, 'count', '22.5'],
    ['bodyMassIndex', 22.04, 'count', '22'],
    ['uvExposure', 3.26, 'count', '3.3'],
    ['appleSleepingBreathingDisturbances', 12.34, 'count', '12.3'],
    ['nikeFuel', 1234.5, 'count', '1,234.5'],
    ['stepCount', 0, 'count', '0 steps'],
    ['stepCount', 1.4, 'count', '1 step'],
    ['stepCount', 2, 'count', '2 steps'],
    ['flightsClimbed', 1, 'count', '1 flight'],
    ['pushCount', 1, 'count', '1 push'],
    ['pushCount', 2, 'count', '2 pushes'],
    ['swimmingStrokeCount', 1, 'count', '1 stroke'],
    ['numberOfTimesFallen', 1, 'count', '1 fall'],
    ['inhalerUsage', 1, 'count', '1 puff'],
    ['numberOfAlcoholicBeverages', 1, 'count', '1 drink'],
    ['numberOfAlcoholicBeverages', 2, 'count', '2 drinks'],
    ['heartRateRecoveryOneMinute', 23.4, 'count/min', '23 bpm'],
    ['walkingHeartRateAverage', 101.5, 'count/min', '102 bpm'],
    ['bodyTemperature', 98.64, 'degF', '98.64°F'],
    ['bodyMass', 150.26, 'lb', '150.3 lb'],
    ['bodyMass', 1500.4, 'lb', '1,500 lb'],
    ['bodyMass', 0.35, 'kg', '0.35 kg'],
    ['heartRate', 74.3, 'count/min', '74 bpm'],
    ['restingHeartRate', 55.5, 'count/min', '56 bpm'],
    ['respiratoryRate', 15.6, 'count/min', '16 breaths/min'],
    ['cyclingCadence', 88.2, 'count/min', '88 rpm'],
    ['distanceWalkingRunning', 850, 'm', '850 m'],
    ['distanceWalkingRunning', 999.6, 'm', '999.6 m'],
    ['distanceWalkingRunning', 1000, 'm', '1 km'],
    ['distanceWalkingRunning', 5234, 'm', '5.2 km'],
    ['distanceWalkingRunning', 12980, 'm', '13 km'],
    ['distanceWalkingRunning', 1234567, 'm', '1,234.6 km'],
    ['runningStrideLength', 0.93, 'm', '0.93 m'],
    ['bodyTemperature', 36.62, 'degC', '36.62°C'],
    ['vo2Max', 42.3, 'ml/(kg*min)', '42.3 mL/kg·min'],
    ['physicalEffort', 3.4, 'kcal/(kg*hr)', '3.4 kcal/kg·hr'],
    ['activeEnergyBurned', 512.7, 'kcal', '512.7 kcal'],
    ['oxygenSaturation', 0.975, '%', '97.5%'],
    ['bodyMass', 72.456, 'kg', '72.46 kg'],
    ['environmentalAudioExposure', 71.2, 'dBASPL', '71.2 dB'],
    ['workoutEffortScore', 6.5, 'appleEffortScore', '6.5'],
    ['bodyMass', 72, '', '72'],
  ]
  it.each(cases)('%s %s %s is %s', (healthType, measuredValue, quantityUnit, expected) => {
    expect(quantityText(measuredValue, quantityUnit, healthType, 'en', t)).toBe(expected)
  })

  it('writes numbers in the language, with its separators', () => {
    expect(quantityText(8402, 'count', 'stepCount', 'de', t)).toBe('8.402 steps')
  })
})

describe('unitWords, the cases of TestUnitWords', () => {
  const cases: [string, string, string][] = [
    ['bodyMassIndex', 'count', ''],
    ['heartRate', 'count/min', 'bpm'],
    ['bodyMass', 'kg', 'kg'],
    ['x', 'degC', '°C'],
    ['x', 'degF', '°F'],
    ['respiratoryRate', 'count/min', 'breaths/min'],
    ['cyclingCadence', 'count/min', 'rpm'],
    ['x', 'ml/(kg*min)', 'mL/kg·min'],
    ['x', '', ''],
    ['x', '%', '%'],
    // An unlisted unit is written as it was sent.
    ['x', 'made_up', 'made_up'],
  ]
  it.each(cases)('%s %s is %s', (healthType, quantityUnit, expected) => {
    expect(unitWords(healthType, quantityUnit)).toBe(expected)
  })

  it('leaves the noun of a count to the number, which the language words', () => {
    // Go's UnitWords says "steps" for a count of steps; the dashboard writes
    // the noun with the number (see quantityText), in the person's language.
    expect(unitWords('stepCount', 'count')).toBe('')
    expect(quantityText(2, 'count', 'stepCount', 'en', t)).toBe('2 steps')
  })
})

describe('stageName, the cases of TestStageNamesAreTheDashboardsWords', () => {
  const cases: [string, string][] = [
    ['asleepDeep', 'Deep'],
    ['asleepCore', 'Core'],
    ['asleepREM', 'REM'],
    ['awake', 'Awake'],
    ['inBed', 'In bed'],
    ['asleepUnspecified', 'Asleep'],
    // A key that is not a stage is written as it came.
    ['energyKilocalories', 'energyKilocalories'],
  ]
  it.each(cases)('%s is %s', (key, expected) => {
    expect(stageName(t, key)).toBe(expected)
  })
})

describe('characteristicText, the cases of TestCharacteristicWords', () => {
  const today = new Date(2026, 2, 2, 15)
  const cases: [string, string, string][] = [
    ['dateOfBirth', '1990-03-02', '1990-03-02 (36 years)'],
    ['dateOfBirth', '1990-03-03', '1990-03-03 (35 years)'],
    ['dateOfBirth', '2025-03-02', '2025-03-02 (1 year)'],
    ['dateOfBirth', '2026-03-02', '2026-03-02 (0 years)'],
    ['dateOfBirth', 'notSet', 'Not set'],
    ['dateOfBirth', '2030-01-01', '2030-01-01'],
    ['dateOfBirth', 'not a date', 'not a date'],
    // A date that cannot be one is written as it was sent, not a crash.
    ['dateOfBirth', '', ''],
    ['dateOfBirth', '1990-02-31', '1990-02-31'],
    ['bloodType', 'oNegative', 'O-'],
    ['bloodType', 'abPositive', 'AB+'],
    ['bloodType', 'notSet', 'Not set'],
    ['bloodType', 'aPositive', 'A+'],
    ['bloodType', 'bNegative', 'B-'],
    ['biologicalSex', 'female', 'Female'],
    ['biologicalSex', 'male', 'Male'],
    ['biologicalSex', 'other', 'Other'],
    ['biologicalSex', 'notSet', 'Not set'],
    ['fitzpatrickSkinType', 'I', 'I'],
    ['fitzpatrickSkinType', 'VI', 'VI'],
    ['wheelchairUse', 'yes', 'Yes'],
    ['wheelchairUse', 'no', 'No'],
    ['wheelchairUse', 'notSet', 'Not set'],
    ['activityMoveMode', 'activeEnergy', 'Active energy'],
    ['activityMoveMode', 'appleMoveTime', 'Move minutes'],
    ['somethingNew', 'whatever', 'whatever'],
  ]
  it.each(cases)('%s %s is %s', (healthType, characteristicValue, expected) => {
    expect(characteristicText(t, healthType, characteristicValue, today)).toBe(expected)
  })

  it('names a characteristic by its kind', () => {
    const names: [string, string][] = [
      ['dateOfBirth', 'Date of birth'],
      ['biologicalSex', 'Biological sex'],
      ['bloodType', 'Blood type'],
      ['fitzpatrickSkinType', 'Skin type'],
      ['wheelchairUse', 'Wheelchair use'],
      ['activityMoveMode', 'Activity move mode'],
    ]
    for (const [healthType, expected] of names) {
      expect(t(`health.type.${healthType}` as keyof typeof en)).toBe(expected)
    }
  })

  it('writes the date in the reader language and keeps the age', () => {
    const text = characteristicText(t, 'dateOfBirth', '1990-03-02', today, (day) => `day ${day.getDate()}`)
    expect(text).toBe('day 2 (36 years)')
  })
})

describe('workoutName, the cases of TestWorkoutName', () => {
  const cases: [string, string][] = [
    ['running', 'Running'],
    ['stairClimbing', 'Stair climbing'],
    ['highIntensityIntervalTraining', 'High intensity interval training'],
    ['traditionalStrengthTraining', 'Traditional strength training'],
    ['other', 'Other workout'],
    ['', 'Other workout'],
    // Not in the dashboard's list: read from the identifier, as Go does.
    ['wheelchairRunPace', 'Wheelchair run pace'],
  ]
  it.each(cases)('%s is %s', (activityType, expected) => {
    expect(workoutName(t, activityType)).toBe(expected)
  })

  it('names every listed workout as the identifier read as words, but other', () => {
    for (const key of Object.keys(en).filter((each) => each.startsWith('health.workout.'))) {
      const identifier = key.slice('health.workout.'.length)
      if (identifier === 'other') continue
      expect(en[key as keyof typeof en]).toBe(humanize(identifier))
    }
  })
})

describe('durationText, the cases of TestDurationWords', () => {
  const cases: [number, string][] = [
    [0, '0 s'],
    [45, '45 s'],
    [59.4, '59 s'],
    [60, '1 min'],
    [2100, '35 min'],
    [3600, '1 h'],
    [25920, '7 h 12 min'],
    [25950, '7 h 13 min'],
    [3599, '1 h'],
    [637800, '177 h 10 min'],
  ]
  it.each(cases)('%s seconds is %s', (seconds, expected) => {
    expect(durationText(seconds, t)).toBe(expected)
  })
})

describe('humanize, nameFromIdentifier in types.go', () => {
  it('reads an identifier as words in sentence case, keeping an acronym', () => {
    expect(humanize('someNewReading')).toBe('Some new reading')
    expect(humanize('someREMReading')).toBe('Some REM reading')
    expect(humanize('vo2Max')).toBe('Vo2 max')
    expect(humanize('')).toBe('')
  })
})

// The names are copied from the table at the top of words.go, which a Go test
// keeps equal to the types in types.go; so here they are compared with the
// table itself.
describe('the English names against the table in words.go', () => {
  const source = readFileSync(resolve(__dirname, '../../../../internal/health/words.go'), 'utf8')

  const tableNames = new Map<string, string>()
  for (const line of source.split('\n')) {
    const match = /^\/\/ {3}(\w+): (.+?)(?: {2}sent as .*)?$/.exec(line)
    if (match) tableNames.set(match[1], match[2])
  }

  const categoryNames = new Map<string, string>()
  const categoryBlock = /\/\/ Categories: ([\s\S]*?)\n\/\/\n/.exec(source)
  for (const match of (categoryBlock?.[1] ?? '').replace(/\n\/\/ /g, ' ').matchAll(/(\w+): ([^;.]+)[;.]/g)) {
    categoryNames.set(match[1], match[2].trim())
  }

  it('reads the table', () => {
    expect(tableNames.get('stepCount')).toBe('Steps')
    expect(tableNames.size).toBeGreaterThan(150)
    expect(categoryNames.get('bodyMeasurements')).toBe('Body Measurements')
    expect(categoryNames.size).toBe(13)
  })

  it('names every kind the dashboard translates as the table does', () => {
    const typeKeys = Object.keys(en).filter((key) => key.startsWith('health.type.'))
    expect(typeKeys.length).toBeGreaterThan(90)
    for (const key of typeKeys) {
      const identifier = key.slice('health.type.'.length)
      expect(en[key as keyof typeof en], identifier).toBe(tableNames.get(identifier))
    }
  })

  it('names every category as the table does', () => {
    for (const [identifier, name] of categoryNames) {
      expect(en[`health.category.${identifier}` as keyof typeof en], identifier).toBe(name)
    }
  })
})
