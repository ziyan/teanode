package health

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// THE WORDS OF HEALTH
//
// This file is the one reference for how Health is written in English: the
// command line and the agent's tool call it, and the dashboard (web/src) and
// the iPhone app (ios/) copy it exactly, so that a value reads the same
// wherever it is read. A change to a rule or to a name here is a change to the
// three, and words_test.go pins every rule below.
//
// Numbers
//
//   Separators      thousands are separated with a comma: 8,412. The decimal
//                   mark is a point.
//   Precision       |n| >= 1000: no decimals. 100 <= |n| < 1000: one decimal.
//                   below 100: two decimals. Trailing zeros are dropped, so
//                   150.26 is 150.3, 72.40 is 72.4 and 0.350 is 0.35.
//   Whole numbers   a count of something (steps, flights climbed, pushes,
//                   strokes, falls, puffs, drinks) and every rate in
//                   count/min are rounded to a whole number: 74 bpm.
//   Plain counts    a count that counts nothing in particular (body mass
//                   index, UV index, breathing disturbances, NikeFuel) keeps
//                   one decimal and drops a trailing .0: 22.5, 3.
//   Percent         HealthKit keeps a percentage as a fraction (0.975); it is
//                   written times 100 with the usual precision and no space:
//                   97.5%.
//   Distance        meters below 1000 (850 m); from 1000 kilometers with one
//                   decimal and no trailing .0: 1 km, 5.2 km, 13 km.
//   Temperature     the degree sign touches the number: 36.6°C, 98.6°F.
//   Singular        exactly 1 of a counted thing is singular: 1 step, 1 flight,
//                   1 push, 1 stroke, 1 fall, 1 puff, 1 drink; anything else
//                   (0, 2, 1.5) is plural.
//   Durations       hours and minutes, a space after each number and after
//                   each unit: 7 h 12 min, 7 h, 35 min, 45 s. Seconds are
//                   rounded to the nearest second below a minute and to the
//                   nearest minute from a minute on.
//
// Units, by the unit string the phone sends (an unlisted unit is written as
// sent, after a space)
//
//   count        the noun of the kind (steps, flights, pushes, strokes, falls,
//                puffs, drinks), or nothing for a plain count
//   count/min    bpm, except respiratoryRate: breaths/min, and cyclingCadence:
//                rpm. Heart rate, resting, walking and recovery heart rate
//                are bpm.
//   %            %, touching the number
//   degC, degF   °C, °F, touching the number
//   ml/(kg*min)  mL/kg·min
//   kcal/(kg*hr) kcal/kg·hr
//   dBASPL       dB
//   appleEffortScore  nothing: the bare number (6.5)
//   m            m, or km (see Distance)
//
// Characteristics (facts about the person, one value each), by kind: the
// value the phone sends, then the words
//
//   dateOfBirth          the date as YYYY-MM-DD, then the age in years on that
//                        day in brackets: 1990-04-12 (36 years), 1 year when
//                        it is one. A surface may write the date in its own
//                        locale, and keeps the age. A date in the future or
//                        that is not a date is written as it was sent.
//   biologicalSex        female: Female, male: Male, other: Other
//   bloodType            aPositive: A+, aNegative: A-, bPositive: B+,
//                        bNegative: B-, abPositive: AB+, abNegative: AB-,
//                        oPositive: O+, oNegative: O-
//   fitzpatrickSkinType  I, II, III, IV, V or VI, as sent
//   wheelchairUse        yes: Yes, no: No
//   activityMoveMode     activeEnergy: Active energy, appleMoveTime: Move
//                        minutes
//   any of them          notSet: Not set
//
// A characteristic is named by the name of its kind below (Date of birth,
// Biological sex, Blood type, Skin type, Wheelchair use, Activity move mode).
// The one exception is what the agent's tool tells its model: a date of birth
// there is only the age, named Age (40 years), because the date itself is an
// identifying detail the model's provider does not need. Every other
// characteristic reads the same there.
//
// Workouts are named by their activity type, as the identifier in sentence
// case (running: Running, stairClimbing: Stair climbing,
// highIntensityIntervalTraining: High intensity interval training); no
// activity type, or other, is Other workout.
//
// Sleep stages, by the key of a day's seconds by stage, in this order: asleepDeep:
// Deep, asleepCore: Core, asleepREM: REM, asleepUnspecified: Asleep, awake:
// Awake, inBed: In bed.
//
// Names. A category is named in title case (Body Measurements); a kind in
// sentence case (Body mass index). A kind this table does not list is named
// from its identifier in sentence case ("someNewReading" is "Some new
// reading") and is in Other. The categories, in the order the Health app lists
// them, with their kinds, as: identifier: English name, then the unit the
// phone sends and the unit word.
//
// Categories: activity: Activity; bodyMeasurements: Body Measurements;
// cycleTracking: Cycle Tracking; hearing: Hearing; heart: Heart;
// mentalWellbeing: Mental Wellbeing; mobility: Mobility; nutrition:
// Nutrition; respiratory: Respiratory; sleep: Sleep; symptoms: Symptoms;
// vitals: Vitals; other: Other.
//
// Activity (activity)
//   stepCount: Steps  sent as count, written steps
//   distanceWalkingRunning: Walking + running distance  sent as m, written m
//   distanceCycling: Cycling distance  sent as m, written m
//   distanceWheelchair: Wheelchair distance  sent as m, written m
//   distanceSwimming: Swimming distance  sent as m, written m
//   distanceDownhillSnowSports: Downhill snow sports distance  sent as m, written m
//   distanceCrossCountrySkiing: Cross-country skiing distance  sent as m, written m
//   distancePaddleSports: Paddle sports distance  sent as m, written m
//   distanceRowing: Rowing distance  sent as m, written m
//   distanceSkatingSports: Skating sports distance  sent as m, written m
//   basalEnergyBurned: Resting energy  sent as kcal, written kcal
//   activeEnergyBurned: Active energy  sent as kcal, written kcal
//   flightsClimbed: Flights climbed  sent as count, written flights
//   nikeFuel: NikeFuel  sent as count, written bare
//   appleExerciseTime: Exercise minutes  sent as min, written min
//   appleMoveTime: Move minutes  sent as min, written min
//   appleStandTime: Stand minutes  sent as min, written min
//   pushCount: Wheelchair pushes  sent as count, written pushes
//   swimmingStrokeCount: Swimming strokes  sent as count, written strokes
//   cyclingSpeed: Cycling speed  sent as m/s, written m/s
//   cyclingPower: Cycling power  sent as W, written W
//   cyclingCadence: Cycling cadence  sent as count/min, written rpm
//   cyclingFunctionalThresholdPower: Cycling functional threshold power  sent as W, written W
//   runningSpeed: Running speed  sent as m/s, written m/s
//   runningPower: Running power  sent as W, written W
//   runningStrideLength: Running stride length  sent as m, written m
//   runningVerticalOscillation: Running vertical oscillation  sent as cm, written cm
//   runningGroundContactTime: Running ground contact time  sent as ms, written ms
//   crossCountrySkiingSpeed: Cross-country skiing speed  sent as m/s, written m/s
//   paddleSportsSpeed: Paddle sports speed  sent as m/s, written m/s
//   rowingSpeed: Rowing speed  sent as m/s, written m/s
//   physicalEffort: Physical effort  sent as kcal/(kg*hr), written kcal/kg·hr
//   workoutEffortScore: Workout effort score  sent as appleEffortScore, written bare
//   estimatedWorkoutEffortScore: Estimated workout effort score  sent as appleEffortScore, written bare
//   underwaterDepth: Underwater depth  sent as m, written m
//   waterTemperature: Water temperature  sent as degC, written °C
//   appleStandHour: Stand hours
//   workout: Workouts
//
// Body Measurements (bodyMeasurements)
//   bodyMass: Weight  sent as kg, written kg
//   bodyMassIndex: Body mass index  sent as count, written bare
//   bodyFatPercentage: Body fat percentage  sent as %, written %
//   leanBodyMass: Lean body mass  sent as kg, written kg
//   height: Height  sent as m, written m
//   waistCircumference: Waist circumference  sent as m, written m
//
// Cycle Tracking (cycleTracking)
//   basalBodyTemperature: Basal body temperature  sent as degC, written °C
//   menstrualFlow: Menstrual flow
//   intermenstrualBleeding: Spotting
//   infrequentMenstrualCycles: Infrequent periods
//   irregularMenstrualCycles: Irregular periods
//   persistentIntermenstrualBleeding: Persistent spotting
//   prolongedMenstrualPeriods: Prolonged periods
//   cervicalMucusQuality: Cervical mucus quality
//   ovulationTestResult: Ovulation test result
//   pregnancyTestResult: Pregnancy test result
//   progesteroneTestResult: Progesterone test result
//   pregnancy: Pregnancy
//   lactation: Lactation
//   contraceptive: Contraceptive
//   sexualActivity: Sexual activity
//   bleedingDuringPregnancy: Bleeding during pregnancy
//   bleedingAfterPregnancy: Bleeding after pregnancy
//
// Hearing (hearing)
//   environmentalAudioExposure: Environmental sound levels  sent as dBASPL, written dB
//   headphoneAudioExposure: Headphone audio levels  sent as dBASPL, written dB
//   environmentalSoundReduction: Environmental sound reduction  sent as dBASPL, written dB
//   environmentalAudioExposureEvent: Environmental sound level notifications
//   headphoneAudioExposureEvent: Headphone audio level notifications
//   audiogram: Audiograms
//
// Heart (heart)
//   heartRate: Heart rate  sent as count/min, written bpm
//   restingHeartRate: Resting heart rate  sent as count/min, written bpm
//   walkingHeartRateAverage: Walking heart rate average  sent as count/min, written bpm
//   heartRateVariabilitySDNN: Heart rate variability  sent as ms, written ms
//   heartRateRecoveryOneMinute: Cardio recovery  sent as count/min, written bpm
//   vo2Max: Cardio fitness  sent as ml/(kg*min), written mL/kg·min
//   atrialFibrillationBurden: Atrial fibrillation burden  sent as %, written %
//   peripheralPerfusionIndex: Peripheral perfusion index  sent as %, written %
//   highHeartRateEvent: High heart rate notifications
//   lowHeartRateEvent: Low heart rate notifications
//   irregularHeartRhythmEvent: Irregular rhythm notifications
//   lowCardioFitnessEvent: Low cardio fitness notifications
//   electrocardiogram: Electrocardiograms
//
// Mental Wellbeing (mentalWellbeing)
//   mindfulSession: Mindful minutes
//   stateOfMind: State of mind
//
// Mobility (mobility)
//   walkingSpeed: Walking speed  sent as m/s, written m/s
//   walkingStepLength: Walking step length  sent as m, written m
//   walkingAsymmetryPercentage: Walking asymmetry  sent as %, written %
//   walkingDoubleSupportPercentage: Walking double support  sent as %, written %
//   appleWalkingSteadiness: Walking steadiness  sent as %, written %
//   sixMinuteWalkTestDistance: Six-minute walk distance  sent as m, written m
//   stairAscentSpeed: Stair ascent speed  sent as m/s, written m/s
//   stairDescentSpeed: Stair descent speed  sent as m/s, written m/s
//   numberOfTimesFallen: Falls  sent as count, written falls
//   appleWalkingSteadinessEvent: Walking steadiness notifications
//
// Nutrition (nutrition)
//   dietaryEnergyConsumed: Dietary energy  sent as kcal, written kcal
//   dietaryCarbohydrates: Carbohydrates  sent as g, written g
//   dietaryFiber: Fiber  sent as g, written g
//   dietarySugar: Sugar  sent as g, written g
//   dietaryFatTotal: Total fat  sent as g, written g
//   dietaryFatMonounsaturated: Monounsaturated fat  sent as g, written g
//   dietaryFatPolyunsaturated: Polyunsaturated fat  sent as g, written g
//   dietaryFatSaturated: Saturated fat  sent as g, written g
//   dietaryCholesterol: Cholesterol  sent as mg, written mg
//   dietaryProtein: Protein  sent as g, written g
//   dietaryVitaminA: Vitamin A  sent as mcg, written mcg
//   dietaryThiamin: Thiamin (B1)  sent as mg, written mg
//   dietaryRiboflavin: Riboflavin (B2)  sent as mg, written mg
//   dietaryNiacin: Niacin (B3)  sent as mg, written mg
//   dietaryPantothenicAcid: Pantothenic acid (B5)  sent as mg, written mg
//   dietaryVitaminB6: Vitamin B6  sent as mg, written mg
//   dietaryBiotin: Biotin (B7)  sent as mcg, written mcg
//   dietaryVitaminB12: Vitamin B12  sent as mcg, written mcg
//   dietaryVitaminC: Vitamin C  sent as mg, written mg
//   dietaryVitaminD: Vitamin D  sent as mcg, written mcg
//   dietaryVitaminE: Vitamin E  sent as mg, written mg
//   dietaryVitaminK: Vitamin K  sent as mcg, written mcg
//   dietaryFolate: Folate  sent as mcg, written mcg
//   dietaryCalcium: Calcium  sent as mg, written mg
//   dietaryChloride: Chloride  sent as mg, written mg
//   dietaryIron: Iron  sent as mg, written mg
//   dietaryMagnesium: Magnesium  sent as mg, written mg
//   dietaryPhosphorus: Phosphorus  sent as mg, written mg
//   dietaryPotassium: Potassium  sent as mg, written mg
//   dietarySodium: Sodium  sent as mg, written mg
//   dietaryZinc: Zinc  sent as mg, written mg
//   dietaryChromium: Chromium  sent as mcg, written mcg
//   dietaryCopper: Copper  sent as mg, written mg
//   dietaryIodine: Iodine  sent as mcg, written mcg
//   dietaryManganese: Manganese  sent as mg, written mg
//   dietaryMolybdenum: Molybdenum  sent as mcg, written mcg
//   dietarySelenium: Selenium  sent as mcg, written mcg
//   dietaryWater: Water  sent as mL, written mL
//   dietaryCaffeine: Caffeine  sent as mg, written mg
//
// Respiratory (respiratory)
//   respiratoryRate: Respiratory rate  sent as count/min, written breaths/min
//   oxygenSaturation: Blood oxygen  sent as %, written %
//   forcedVitalCapacity: Forced vital capacity  sent as L, written L
//   forcedExpiratoryVolume1: Forced expiratory volume  sent as L, written L
//   peakExpiratoryFlowRate: Peak expiratory flow rate  sent as L/min, written L/min
//   inhalerUsage: Inhaler usage  sent as count, written puffs
//
// Sleep (sleep)
//   sleepAnalysis: Sleep
//   sleepApneaEvent: Sleep apnea notifications
//   appleSleepingBreathingDisturbances: Breathing disturbances  sent as count, written bare
//
// Symptoms (symptoms)
//   abdominalCramps: Abdominal cramps
//   acne: Acne
//   appetiteChanges: Appetite changes
//   bladderIncontinence: Bladder incontinence
//   bloating: Bloating
//   breastPain: Breast pain
//   chestTightnessOrPain: Chest tightness or pain
//   chills: Chills
//   constipation: Constipation
//   coughing: Coughing
//   diarrhea: Diarrhea
//   dizziness: Dizziness
//   drySkin: Dry skin
//   fainting: Fainting
//   fatigue: Fatigue
//   fever: Fever
//   generalizedBodyAche: Body and muscle ache
//   hairLoss: Hair loss
//   headache: Headache
//   heartburn: Heartburn
//   hotFlashes: Hot flashes
//   lossOfSmell: Loss of smell
//   lossOfTaste: Loss of taste
//   lowerBackPain: Lower back pain
//   memoryLapse: Memory lapse
//   moodChanges: Mood changes
//   nausea: Nausea
//   nightSweats: Night sweats
//   pelvicPain: Pelvic pain
//   rapidPoundingOrFlutteringHeartbeat: Rapid, pounding, or fluttering heartbeat
//   runnyNose: Runny nose
//   shortnessOfBreath: Shortness of breath
//   sinusCongestion: Sinus congestion
//   skippedHeartbeat: Skipped heartbeat
//   sleepChanges: Sleep changes
//   soreThroat: Sore throat
//   vaginalDryness: Vaginal dryness
//   vomiting: Vomiting
//   wheezing: Wheezing
//
// Vitals (vitals)
//   bloodPressureSystolic: Systolic blood pressure  sent as mmHg, written mmHg
//   bloodPressureDiastolic: Diastolic blood pressure  sent as mmHg, written mmHg
//   bloodGlucose: Blood glucose  sent as mg/dL, written mg/dL
//   bodyTemperature: Body temperature  sent as degC, written °C
//   appleSleepingWristTemperature: Sleeping wrist temperature  sent as degC, written °C
//   insulinDelivery: Insulin delivery  sent as IU, written IU
//
// Other (other)
//   uvExposure: UV index  sent as count, written bare
//   timeInDaylight: Time in daylight  sent as min, written min
//   electrodermalActivity: Electrodermal activity  sent as S, written S
//   bloodAlcoholContent: Blood alcohol content  sent as %, written %
//   numberOfAlcoholicBeverages: Alcoholic beverages  sent as count, written drinks
//   handwashingEvent: Handwashing
//   toothbrushingEvent: Toothbrushing
//   dateOfBirth: Date of birth
//   biologicalSex: Biological sex
//   bloodType: Blood type
//   fitzpatrickSkinType: Skin type
//   wheelchairUse: Wheelchair use
//   activityMoveMode: Activity move mode

// countNouns are what a "count" is a count of, for the kinds whose unit says
// nothing: 8,412 steps. A kind that is not here (body mass index, UV index) is
// a bare number with one decimal.
var countNouns = map[string][2]string{
	"stepCount":                  {"step", "steps"},
	"flightsClimbed":             {"flight", "flights"},
	"pushCount":                  {"push", "pushes"},
	"swimmingStrokeCount":        {"stroke", "strokes"},
	"numberOfTimesFallen":        {"fall", "falls"},
	"inhalerUsage":               {"puff", "puffs"},
	"numberOfAlcoholicBeverages": {"drink", "drinks"},
}

// rateUnitWords are the kinds measured in "count/min" that are not beats:
// heart rate and the rest are bpm.
var rateUnitWords = map[string]string{
	"cyclingCadence":  "rpm",
	"respiratoryRate": "breaths/min",
}

// unitWords are HealthKit's unit strings as a person writes them. A unit not
// here is written as it is.
var unitWords = map[string]string{
	"degC":             "°C",
	"degF":             "°F",
	"ml/(kg*min)":      "mL/kg·min",
	"kcal/(kg*hr)":     "kcal/kg·hr",
	"dBASPL":           "dB",
	"appleEffortScore": "",
}

// UnitWords is the word that follows a number of this kind, without a space
// when it is a symbol: "bpm", "kg", "°C", "%" or, for a count, its noun
// ("steps") or nothing. A number of one is told apart by ValueWords; this is
// the plural.
func UnitWords(healthType, quantityUnit string) string {
	switch quantityUnit {
	case "":
		return ""
	case "count":
		return countNouns[healthType][1]
	case "count/min":
		if words, isListed := rateUnitWords[healthType]; isListed {
			return words
		}
		return "bpm"
	case "%":
		return "%"
	}
	if words, isListed := unitWords[quantityUnit]; isListed {
		return words
	}
	return quantityUnit
}

// ShownValue is a value as a person reads it. HealthKit keeps a percentage as
// a fraction (0.97 for 97%), and so does the server.
func ShownValue(quantityValue float64, quantityUnit string) float64 {
	if quantityUnit == "%" {
		return quantityValue * 100
	}
	return quantityValue
}

// NumberWords writes a reading the way a person says it: thousands
// separated, and only as many decimals as the size of the number is worth
// (8,412, 72.4, 0.35).
func NumberWords(quantityValue float64) string {
	decimals := 2
	switch magnitude := math.Abs(quantityValue); {
	case magnitude >= 1000:
		decimals = 0
	case magnitude >= 100:
		decimals = 1
	}
	return numberWithDecimals(quantityValue, decimals, true)
}

// numberWithDecimals writes the number with up to this many decimals and
// thousands separated; trailing zeros are dropped when isTrimmed.
func numberWithDecimals(quantityValue float64, decimals int, isTrimmed bool) string {
	text := strconv.FormatFloat(quantityValue, 'f', decimals, 64)
	if decimals > 0 && isTrimmed {
		text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	}
	sign := ""
	if strings.HasPrefix(text, "-") {
		sign, text = "-", text[1:]
	}
	whole, fraction, hasFraction := strings.Cut(text, ".")
	var grouped strings.Builder
	for index, digit := range whole {
		if index > 0 && (len(whole)-index)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteRune(digit)
	}
	if hasFraction {
		return sign + grouped.String() + "." + fraction
	}
	return sign + grouped.String()
}

// isWholeNumberKind says whether a kind is read in whole numbers: heart rate
// and the other rates (a fraction of a beat means nothing) and what is counted
// (steps).
func isWholeNumberKind(healthType, quantityUnit string) bool {
	if quantityUnit == "count/min" {
		return true
	}
	_, isCounted := countNouns[healthType]
	return quantityUnit == "count" && isCounted
}

// ValueWords is a reading with its unit, as a person says it: 8,412 steps,
// 72.4 kg, 74 bpm, 97%, 36.6°C, 5.2 km. A distance in meters of 1000 or more
// is in kilometers with one decimal, and a whole number of kilometers has
// none (13 km). A count that counts nothing in particular (body mass index,
// UV index) keeps one decimal (22.5), and is not rounded to a whole number.
func ValueWords(healthType string, quantityValue float64, quantityUnit string) string {
	switch {
	case quantityUnit == "m" && math.Abs(quantityValue) >= 1000:
		return numberWithDecimals(quantityValue/1000, 1, true) + " km"
	case quantityUnit == "%":
		return NumberWords(ShownValue(quantityValue, quantityUnit)) + "%"
	case isWholeNumberKind(healthType, quantityUnit):
		rounded := math.Round(quantityValue)
		number := numberWithDecimals(rounded, 0, false)
		if quantityUnit == "count" && math.Abs(rounded) == 1 {
			return number + " " + countNouns[healthType][0]
		}
		return withUnit(number, UnitWords(healthType, quantityUnit))
	case quantityUnit == "count":
		return numberWithDecimals(quantityValue, 1, true)
	case quantityUnit == "":
		return NumberWords(quantityValue)
	}
	return withUnit(NumberWords(quantityValue), UnitWords(healthType, quantityUnit))
}

// withUnit joins a number and its unit: a symbol touches the number, a word
// does not.
func withUnit(number, unit string) string {
	switch {
	case unit == "":
		return number
	case strings.HasPrefix(unit, "°"):
		return number + unit
	}
	return number + " " + unit
}

// DurationWords writes a time covered in hours and minutes: 7 h 12 min,
// 35 min, 45 s.
func DurationWords(seconds float64) string {
	totalSeconds := int(math.Round(seconds))
	if totalSeconds < 60 {
		return fmt.Sprintf("%d s", totalSeconds)
	}
	minutes := int(math.Round(seconds / 60))
	hours, minutes := minutes/60, minutes%60
	switch {
	case hours == 0:
		return fmt.Sprintf("%d min", minutes)
	case minutes == 0:
		return fmt.Sprintf("%d h", hours)
	}
	return fmt.Sprintf("%d h %d min", hours, minutes)
}

// SleepStageKeys are the values of HealthKit's sleep analysis by raw number,
// as the phone names them in a day's seconds by stage.
var SleepStageKeys = map[int]string{0: "inBed", 1: "asleepUnspecified", 2: "awake", 3: "asleepCore", 4: "asleepDeep", 5: "asleepREM"}

// SleepAsleepKey is the key of the night's total time asleep in a day's
// seconds by stage: the union of the asleep stages, which is not their sum
// when two sources overlap.
const SleepAsleepKey = "asleep"

// stageNames are the words for the keys of a day's seconds by category, as
// the dashboard writes them.
var stageNames = map[string]string{
	"asleepDeep":        "Deep",
	"asleepCore":        "Core",
	"asleepREM":         "REM",
	"asleepUnspecified": "Asleep",
	"awake":             "Awake",
	"inBed":             "In bed",
}

// StageName is the word for one key of a day's seconds by category: Deep,
// Core, REM, Awake. A key that is not a stage is written as it is.
func StageName(key string) string {
	if name, isListed := stageNames[key]; isListed {
		return name
	}
	return key
}

// StageOrder is the order stages are written in: from the deepest up.
var StageOrder = []string{"asleepDeep", "asleepCore", "asleepREM", "asleepUnspecified", "awake", "inBed"}

// WorkoutName is the name of a workout's activity type: running is Running,
// traditionalStrengthTraining is Traditional strength training, and a workout
// with no activity type is Other workout.
func WorkoutName(activityType string) string {
	if activityType == "" || activityType == "other" {
		return "Other workout"
	}
	return nameFromIdentifier(activityType)
}

// SleepAnalysisType is the identifier of the kind that holds sleep.
const SleepAnalysisType = "sleepAnalysis"

// characteristicWords are the words of the characteristics whose value is one
// of a fixed list, by kind and then by the value the phone sends. A value that
// is not here is written as it was sent; "notSet" is "Not set" for every kind.
var characteristicWords = map[string]map[string]string{
	"biologicalSex": {"female": "Female", "male": "Male", "other": "Other"},
	"bloodType": {
		"aPositive": "A+", "aNegative": "A-", "bPositive": "B+", "bNegative": "B-",
		"abPositive": "AB+", "abNegative": "AB-", "oPositive": "O+", "oNegative": "O-",
	},
	"wheelchairUse":    {"yes": "Yes", "no": "No"},
	"activityMoveMode": {"activeEnergy": "Active energy", "appleMoveTime": "Move minutes"},
}

// CharacteristicWords is a characteristic as it is told to somebody: a date of
// birth as the date with the age in years on the given day (1990-04-12 (36
// years)), a sex, blood type, wheelchair use or move mode in its words (Female,
// O+, Yes), a skin type as the Roman numeral the phone sends (I to VI), and
// "Not set" for a value HealthKit has not been given. Anything else is written
// as the phone sent it, and so is a date of birth that is not a date or is in
// the future.
func CharacteristicWords(healthType, characteristicValue string, today time.Time) string {
	if characteristicValue == "notSet" {
		return "Not set"
	}
	if healthType == "dateOfBirth" {
		born, err := ParseDay(characteristicValue)
		if err != nil || born.After(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)) {
			return characteristicValue
		}
		years := today.Year() - born.Year()
		if today.Month() < born.Month() || (today.Month() == born.Month() && today.Day() < born.Day()) {
			years--
		}
		if years == 1 {
			return characteristicValue + " (1 year)"
		}
		return fmt.Sprintf("%s (%d years)", characteristicValue, years)
	}
	if words, isListed := characteristicWords[healthType][characteristicValue]; isListed {
		return words
	}
	return characteristicValue
}

// AgentCharacteristicWords is CharacteristicWords for what the agent's model
// is told: a date of birth is only the age in years on the given day (40
// years), and nothing else changes.
func AgentCharacteristicWords(healthType, characteristicValue string, today time.Time) string {
	words := CharacteristicWords(healthType, characteristicValue, today)
	if healthType == "dateOfBirth" {
		if _, brackets, isFound := strings.Cut(words, " ("); isFound {
			return strings.TrimSuffix(brackets, ")")
		}
	}
	return words
}

// AgentCharacteristicName is CharacteristicName for what the agent's model is
// told: a date of birth is told as an age.
func AgentCharacteristicName(healthType string) string {
	if healthType == "dateOfBirth" {
		return "Age"
	}
	return CharacteristicName(healthType)
}

// CharacteristicName is what a characteristic is called: the English name of
// its kind (Date of birth, Biological sex, Blood type).
func CharacteristicName(healthType string) string {
	return TypeOf(healthType).Name
}
