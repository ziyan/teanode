package health_test

import (
	"os"
	"strings"
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
		{"bodyMassIndex", 22.456, "count", "22.5"},
		{"bodyMassIndex", 22.04, "count", "22"},
		{"uvExposure", 3.26, "count", "3.3"},
		{"appleSleepingBreathingDisturbances", 12.34, "count", "12.3"},
		{"nikeFuel", 1234.5, "count", "1,234.5"},
		{"stepCount", 0, "count", "0 steps"},
		{"stepCount", 1.4, "count", "1 step"},
		{"stepCount", 2, "count", "2 steps"},
		{"flightsClimbed", 1, "count", "1 flight"},
		{"pushCount", 1, "count", "1 push"},
		{"swimmingStrokeCount", 1, "count", "1 stroke"},
		{"numberOfTimesFallen", 1, "count", "1 fall"},
		{"inhalerUsage", 1, "count", "1 puff"},
		{"numberOfAlcoholicBeverages", 1, "count", "1 drink"},
		{"numberOfAlcoholicBeverages", 2, "count", "2 drinks"},
		{"heartRateRecoveryOneMinute", 23.4, "count/min", "23 bpm"},
		{"walkingHeartRateAverage", 101.5, "count/min", "102 bpm"},
		{"bodyTemperature", 98.64, "degF", "98.64°F"},
		{"bodyMass", 150.26, "lb", "150.3 lb"},
		{"bodyMass", 1500.4, "lb", "1,500 lb"},
		{"bodyMass", 0.35, "kg", "0.35 kg"},
		{"heartRate", 74.3, "count/min", "74 bpm"},
		{"restingHeartRate", 55.5, "count/min", "56 bpm"},
		{"respiratoryRate", 15.6, "count/min", "16 breaths/min"},
		{"cyclingCadence", 88.2, "count/min", "88 rpm"},
		{"distanceWalkingRunning", 850, "m", "850 m"},
		{"distanceWalkingRunning", 999.6, "m", "999.6 m"},
		{"distanceWalkingRunning", 1000, "m", "1 km"},
		{"distanceWalkingRunning", 5234, "m", "5.2 km"},
		{"distanceWalkingRunning", 12980, "m", "13 km"},
		{"distanceWalkingRunning", 1234567, "m", "1,234.6 km"},
		{"runningStrideLength", 0.93, "m", "0.93 m"},
		{"bodyTemperature", 36.62, "degC", "36.62°C"},
		{"vo2Max", 42.3, "ml/(kg*min)", "42.3 mL/kg·min"},
		{"activeEnergyBurned", 512.7, "kcal", "512.7 kcal"},
		// Percentages: whole numbers for blood oxygen and walking steadiness,
		// one decimal for body fat, walking asymmetry and double support, the
		// usual precision for the rest.
		{"oxygenSaturation", 0.975, "%", "98%"},
		{"oxygenSaturation", 0.9797, "%", "98%"},
		{"oxygenSaturation", 0.94, "%", "94%"},
		{"oxygenSaturation", 1, "%", "100%"},
		{"appleWalkingSteadiness", 0.674, "%", "67%"},
		{"bodyFatPercentage", 0.1837, "%", "18.4%"},
		{"bodyFatPercentage", 0.18, "%", "18%"},
		{"walkingAsymmetryPercentage", 0.0345, "%", "3.5%"},
		{"walkingDoubleSupportPercentage", 0.2849, "%", "28.5%"},
		{"peripheralPerfusionIndex", 0.0235, "%", "2.35%"},
		{"bloodAlcoholContent", 0.0008, "%", "0.08%"},
		// The tie rule: half away from zero on the shortest decimal form.
		{"bodyMassIndex", 22.25, "", "22.25"},
		{"bodyMass", 0.35, "", "0.35"},
		{"x", 0.355, "", "0.36"},
		{"x", 1.005, "", "1.01"},
		{"x", 0.975, "", "0.98"},
		{"x", 150.25, "", "150.3"},
		{"x", 1000.5, "", "1,001"},
		{"x", -150.25, "", "-150.3"},
		{"x", -0.001, "", "0"},
		{"x", -0.004, "kg", "0 kg"},
		{"bodyFatPercentage", 0.1845, "%", "18.5%"},
		{"heartRate", 72.5, "count/min", "73 bpm"},
		{"heartRate", -0.5, "count/min", "-1 bpm"},
		{"stepCount", 2.5, "count", "3 steps"},
		{"stepCount", 0.5, "count", "1 step"},
		{"stepCount", 999999.5, "count", "1,000,000 steps"},
		{"distanceWalkingRunning", 1050, "m", "1.1 km"},
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
		{"x", "degC", "°C"}, {"x", "degF", "°F"}, {"respiratoryRate", "count/min", "breaths/min"}, {"cyclingCadence", "count/min", "rpm"}, {"heartRate", "count/min", "bpm"}, {"x", "ml/(kg*min)", "mL/kg·min"}, {"x", "", ""}, {"x", "%", "%"},
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
		{"dateOfBirth", "1990-03-02", "1990-03-02 (36 years)"},
		{"dateOfBirth", "1990-03-03", "1990-03-03 (35 years)"},
		{"dateOfBirth", "2025-03-02", "2025-03-02 (1 year)"},
		{"dateOfBirth", "2026-03-02", "2026-03-02 (0 years)"},
		{"dateOfBirth", "notSet", "Not set"},
		{"dateOfBirth", "2030-01-01", "2030-01-01"},
		{"dateOfBirth", "not a date", "not a date"},
		{"bloodType", "oNegative", "O-"},
		{"bloodType", "abPositive", "AB+"},
		{"bloodType", "notSet", "Not set"},
		{"bloodType", "aPositive", "A+"},
		{"bloodType", "bNegative", "B-"},
		{"biologicalSex", "female", "Female"},
		{"biologicalSex", "male", "Male"},
		{"biologicalSex", "other", "Other"},
		{"biologicalSex", "notSet", "Not set"},
		{"fitzpatrickSkinType", "I", "I"},
		{"fitzpatrickSkinType", "VI", "VI"},
		{"wheelchairUse", "yes", "Yes"},
		{"wheelchairUse", "no", "No"},
		{"wheelchairUse", "notSet", "Not set"},
		{"activityMoveMode", "activeEnergy", "Active energy"},
		{"activityMoveMode", "appleMoveTime", "Move minutes"},
		{"somethingNew", "whatever", "whatever"},
	} {
		if got := health.CharacteristicWords(testCase.healthType, testCase.value, today); got != testCase.want {
			t.Errorf("CharacteristicWords(%q, %q) = %q, want %q", testCase.healthType, testCase.value, got, testCase.want)
		}
	}
	for healthType, want := range map[string]string{
		"dateOfBirth": "Date of birth", "biologicalSex": "Biological sex", "bloodType": "Blood type",
		"fitzpatrickSkinType": "Skin type", "wheelchairUse": "Wheelchair use", "activityMoveMode": "Activity move mode",
	} {
		if got := health.CharacteristicName(healthType); got != want {
			t.Errorf("CharacteristicName(%q) = %q, want %q", healthType, got, want)
		}
	}
}

// Durations are written the same on every surface: hours and minutes with a
// space after each number and its unit.
// What the agent's model is told of a date of birth is the age and not the
// date.
func TestAgentCharacteristicWords(t *testing.T) {
	today := time.Date(2026, 3, 2, 15, 0, 0, 0, time.UTC)
	for _, testCase := range []struct{ healthType, value, want string }{
		{"dateOfBirth", "1990-03-02", "36 years"},
		{"dateOfBirth", "2025-03-02", "1 year"},
		{"dateOfBirth", "2030-01-01", "2030-01-01"},
		{"dateOfBirth", "notSet", "Not set"},
		{"bloodType", "oNegative", "O-"},
	} {
		if got := health.AgentCharacteristicWords(testCase.healthType, testCase.value, today); got != testCase.want {
			t.Errorf("AgentCharacteristicWords(%q, %q) = %q, want %q", testCase.healthType, testCase.value, got, testCase.want)
		}
	}
	if health.AgentCharacteristicName("dateOfBirth") != "Age" || health.AgentCharacteristicName("bloodType") != "Blood type" {
		t.Error("a date of birth is told to the agent as an age")
	}
}

func TestWorkoutName(t *testing.T) {
	for activityType, want := range map[string]string{
		"running": "Running", "stairClimbing": "Stair climbing", "highIntensityIntervalTraining": "High intensity interval training",
		"traditionalStrengthTraining": "Traditional strength training", "other": "Other workout", "": "Other workout",
	} {
		if got := health.WorkoutName(activityType); got != want {
			t.Errorf("WorkoutName(%q) = %q, want %q", activityType, got, want)
		}
	}
}

func TestDurationWords(t *testing.T) {
	for seconds, want := range map[float64]string{
		0: "0 s", 45: "45 s", 59.4: "59 s", 60: "1 min", 2100: "35 min", 3600: "1 h", 25920: "7 h 12 min",
		25950: "7 h 13 min", 3599: "1 h", 637800: "177 h 10 min",
	} {
		if got := health.DurationWords(seconds); got != want {
			t.Errorf("DurationWords(%v) = %q, want %q", seconds, got, want)
		}
	}
}

// Every kind is written with one English name in sentence case, and every
// category with a title case name, and the table at the top of words.go lists
// them all, so that the dashboard and the iPhone app have one place to copy
// from.
func TestTheTableAtTheTopOfWordsGoIsTheTable(t *testing.T) {
	source, err := os.ReadFile("words.go")
	if err != nil {
		t.Fatalf("ReadFile: %s", err)
	}
	text := string(source)
	for _, category := range health.Categories() {
		if !strings.Contains(text, "// "+category.Name()+" ("+string(category)+")\n") {
			t.Errorf("the table has no heading for %s (%s)", category.Name(), category)
		}
	}
	for _, each := range health.Types() {
		line := "//   " + each.Identifier + ": " + each.Name
		if !strings.Contains(text, line+"\n") && !strings.Contains(text, line+"  sent as ") {
			t.Errorf("the table does not list %s as %q", each.Identifier, each.Name)
		}
		if each.QuantityUnit != "" && !strings.Contains(text, line+"  sent as "+each.QuantityUnit+", written ") {
			t.Errorf("the table does not give the unit %s of %s", each.QuantityUnit, each.Identifier)
		}
		if first := []rune(each.Name)[0]; first < 'A' || first > 'Z' {
			t.Errorf("%s: the name %q starts with a capital letter", each.Identifier, each.Name)
		}
	}
	if health.CategoryOther.Name() != "Other" {
		t.Errorf("the last category is Other, not %q", health.CategoryOther.Name())
	}
}
