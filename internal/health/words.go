package health

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// The words a number is written with, the same on every surface that is not a
// screen of its own (the command line and the agent's tool), and the table
// the dashboard and the iPhone app follow so that a value reads the same
// wherever it is read.

// countNouns are what a "count" is a count of, for the kinds whose unit says
// nothing: 8,412 steps. A kind that is not here (body mass index, UV index) is
// a bare number.
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
func ShownValue(value float64, quantityUnit string) float64 {
	if quantityUnit == "%" {
		return value * 100
	}
	return value
}

// NumberWords writes a reading the way a person says it: thousands
// separated, and only as many decimals as the size of the number is worth
// (8,412, 72.4, 0.35).
func NumberWords(value float64) string {
	decimals := 2
	switch magnitude := math.Abs(value); {
	case magnitude >= 1000:
		decimals = 0
	case magnitude >= 100:
		decimals = 1
	}
	return numberWithDecimals(value, decimals, true)
}

// numberWithDecimals writes the number with up to this many decimals and
// thousands separated; trailing zeros are dropped when isTrimmed.
func numberWithDecimals(value float64, decimals int, isTrimmed bool) string {
	text := strconv.FormatFloat(value, 'f', decimals, 64)
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
// is in kilometers with one decimal.
func ValueWords(healthType string, value float64, quantityUnit string) string {
	switch {
	case quantityUnit == "m" && math.Abs(value) >= 1000:
		return numberWithDecimals(value/1000, 1, false) + " km"
	case quantityUnit == "%":
		return NumberWords(ShownValue(value, quantityUnit)) + "%"
	case isWholeNumberKind(healthType, quantityUnit):
		number := numberWithDecimals(math.Round(value), 0, false)
		if quantityUnit == "count" && math.Abs(value) == 1 {
			return number + " " + countNouns[healthType][0]
		}
		return withUnit(number, UnitWords(healthType, quantityUnit))
	case quantityUnit == "":
		return NumberWords(value)
	}
	return withUnit(NumberWords(value), UnitWords(healthType, quantityUnit))
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

// DurationWords writes a time covered in hours and minutes, as the Health app
// does: 7h 12m, 35m, 45s.
func DurationWords(seconds float64) string {
	totalSeconds := int(math.Round(seconds))
	if totalSeconds < 60 {
		return fmt.Sprintf("%ds", totalSeconds)
	}
	minutes := int(math.Round(seconds / 60))
	hours, minutes := minutes/60, minutes%60
	switch {
	case hours == 0:
		return fmt.Sprintf("%dm", minutes)
	case minutes == 0:
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh %dm", hours, minutes)
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

// bloodTypeWords are the blood types as people write them.
var bloodTypeWords = map[string]string{
	"aPositive": "A+", "aNegative": "A-", "bPositive": "B+", "bNegative": "B-",
	"abPositive": "AB+", "abNegative": "AB-", "oPositive": "O+", "oNegative": "O-",
}

// CharacteristicWords is a characteristic as it is told to somebody: a date of
// birth as an age in years on the given day, a blood type as O+, anything else
// as the phone sent it. A date of birth that is not a date, or in the future,
// is written as it was sent.
func CharacteristicWords(healthType, characteristicValue string, today time.Time) string {
	switch healthType {
	case "dateOfBirth":
		born, err := ParseDay(characteristicValue)
		if err != nil || born.After(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)) {
			return characteristicValue
		}
		years := today.Year() - born.Year()
		if today.Month() < born.Month() || (today.Month() == born.Month() && today.Day() < born.Day()) {
			years--
		}
		if years == 1 {
			return "1 year"
		}
		return fmt.Sprintf("%d years", years)
	case "bloodType":
		if words, isListed := bloodTypeWords[characteristicValue]; isListed {
			return words
		}
	}
	return characteristicValue
}

// CharacteristicName is what a characteristic is called when it is told: a
// date of birth is told as an age.
func CharacteristicName(healthType string) string {
	if healthType == "dateOfBirth" {
		return "Age"
	}
	return TypeOf(healthType).Name
}
