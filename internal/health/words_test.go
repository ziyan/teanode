package health_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/health"
)

// The words of a value are the same on every surface, so the table is pinned
// here: counts say what they count, rates are beats, meters become kilometers,
// and the units HealthKit spells for a machine are written for a person.
func TestValueWords(t *testing.T) {
	for _, testCase := range []struct {
		healthType string
		value      float64
		unit       string
		want       string
	}{
		{"stepCount", 8412.4, "count", "8,412 steps"},
		{"stepCount", 1, "count", "1 step"},
		{"flightsClimbed", 12, "count", "12 flights"},
		{"bodyMassIndex", 22.456, "count", "22.46"},
		{"heartRate", 74.3, "count/min", "74 bpm"},
		{"restingHeartRate", 55.5, "count/min", "56 bpm"},
		{"respiratoryRate", 15.6, "count/min", "16 breaths/min"},
		{"cyclingCadence", 88.2, "count/min", "88 rpm"},
		{"distanceWalkingRunning", 850, "m", "850 m"},
		{"distanceWalkingRunning", 1000, "m", "1.0 km"},
		{"distanceWalkingRunning", 5234, "m", "5.2 km"},
		{"distanceWalkingRunning", 12980, "m", "13.0 km"},
		{"runningStrideLength", 0.93, "m", "0.93 m"},
		{"bodyTemperature", 36.62, "degC", "36.62°C"},
		{"vo2Max", 42.3, "ml/(kg*min)", "42.3 mL/kg·min"},
		{"activeEnergyBurned", 512.7, "kcal", "512.7 kcal"},
		{"oxygenSaturation", 0.975, "%", "97.5%"},
		{"bodyMass", 72.456, "kg", "72.46 kg"},
		{"environmentalAudioExposure", 71.2, "dBASPL", "71.2 dB"},
		{"workoutEffortScore", 6.5, "appleEffortScore", "6.5"},
		{"bodyMass", 72, "", "72"},
	} {
		if got := health.ValueWords(testCase.healthType, testCase.value, testCase.unit); got != testCase.want {
			t.Errorf("ValueWords(%q, %v, %q) = %q, want %q", testCase.healthType, testCase.value, testCase.unit, got, testCase.want)
		}
	}
}

func TestUnitWords(t *testing.T) {
	for _, testCase := range []struct{ healthType, unit, want string }{
		{"stepCount", "count", "steps"}, {"bodyMassIndex", "count", ""}, {"heartRate", "count/min", "bpm"}, {"bodyMass", "kg", "kg"},
		{"x", "degC", "°C"}, {"x", "ml/(kg*min)", "mL/kg·min"}, {"x", "", ""}, {"x", "%", "%"},
	} {
		if got := health.UnitWords(testCase.healthType, testCase.unit); got != testCase.want {
			t.Errorf("UnitWords(%q, %q) = %q, want %q", testCase.healthType, testCase.unit, got, testCase.want)
		}
	}
}

func TestStageNamesAreTheDashboardsWords(t *testing.T) {
	for key, want := range map[string]string{"asleepDeep": "Deep", "asleepCore": "Core", "asleepREM": "REM", "awake": "Awake", "inBed": "In bed", "energyKilocalories": "energyKilocalories"} {
		if got := health.StageName(key); got != want {
			t.Errorf("StageName(%q) = %q, want %q", key, got, want)
		}
	}
	if health.SleepStageKeys[3] != "asleepCore" || health.SleepStageKeys[5] != "asleepREM" {
		t.Error("the raw values of HealthKit's sleep analysis")
	}
}

func TestCharacteristicWords(t *testing.T) {
	today := time.Date(2026, 3, 2, 15, 0, 0, 0, time.UTC)
	for _, testCase := range []struct{ healthType, value, want string }{
		{"dateOfBirth", "1990-03-02", "36 years"},
		{"dateOfBirth", "1990-03-03", "35 years"},
		{"dateOfBirth", "2025-03-02", "1 year"},
		{"dateOfBirth", "2030-01-01", "2030-01-01"},
		{"dateOfBirth", "not a date", "not a date"},
		{"bloodType", "oNegative", "O-"},
		{"bloodType", "abPositive", "AB+"},
		{"biologicalSex", "female", "female"},
	} {
		if got := health.CharacteristicWords(testCase.healthType, testCase.value, today); got != testCase.want {
			t.Errorf("CharacteristicWords(%q, %q) = %q, want %q", testCase.healthType, testCase.value, got, testCase.want)
		}
	}
	if health.CharacteristicName("dateOfBirth") != "Age" || health.CharacteristicName("bloodType") != "Blood Type" {
		t.Error("a date of birth is told as an age")
	}
}
