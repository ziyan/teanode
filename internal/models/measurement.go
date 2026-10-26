package models

import "strings"

// The measurement systems a person can read numbers in. The server keeps
// every measurement in metric units whatever the choice; the choice is only
// how a temperature, a distance or a weight is written for that person.
const (
	// MeasurementSystemAutomatic reads the system from the person's language
	// and region.
	MeasurementSystemAutomatic = ""

	// MeasurementSystemMetric is degrees Celsius, kilometers and kilograms.
	MeasurementSystemMetric = "metric"

	// MeasurementSystemUS is degrees Fahrenheit, miles and pounds.
	MeasurementSystemUS = "us"
)

// IsMeasurementSystem says whether a value is one the database accepts for
// an account's measurement system.
func IsMeasurementSystem(value string) bool {
	switch value {
	case MeasurementSystemAutomatic, MeasurementSystemMetric, MeasurementSystemUS:
		return true
	}
	return false
}

// usRegions are the regions whose people read Fahrenheit and miles. The
// dashboard keeps the same list for the browser's region.
var usRegions = map[string]bool{"US": true, "LR": true, "MM": true}

// UsesUSUnits says whether numbers are written in degrees Fahrenheit and
// miles for a person: as they chose, or when they did not, as the region of
// the locale says. A locale with no region ("en", "ja") says nothing about
// where the person is, so it reads as metric; only a locale that names a
// region ("en-US", "en_US.UTF-8") can read as US.
func UsesUSUnits(measurementSystem string, locale string) bool {
	switch measurementSystem {
	case MeasurementSystemMetric:
		return false
	case MeasurementSystemUS:
		return true
	}
	locale, _, _ = strings.Cut(locale, ".")
	locale, _, _ = strings.Cut(locale, "@")
	subtags := strings.FieldsFunc(locale, func(character rune) bool { return character == '-' || character == '_' })
	for _, subtag := range subtags[min(1, len(subtags)):] {
		if len(subtag) == 2 && usRegions[strings.ToUpper(subtag)] {
			return true
		}
	}
	return false
}

// UsesUSUnits says whether numbers are written in US units for this person.
// It reads the language they chose, else the one their browser last said.
func (self *User) UsesUSUnits() bool {
	if self == nil {
		return false
	}
	locale := self.Locale
	if locale == "" {
		locale = self.LocaleSeen
	}
	return UsesUSUnits(self.MeasurementSystem, locale)
}

// FahrenheitFromCelsius converts a temperature.
func FahrenheitFromCelsius(celsius float64) float64 {
	return celsius*9/5 + 32
}

// MilesFromMeters converts a distance.
func MilesFromMeters(meters float64) float64 {
	return meters / 1609.344
}
