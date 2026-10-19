package health

import (
	"slices"
	"testing"
)

// Every kind in the table is in one of the categories, has a shape and, when
// it is a quantity, a unit; a kind that adds up has a way to.
func TestEveryTypeInTheTableIsComplete(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for _, each := range Types() {
		if seen[each.Identifier] {
			t.Errorf("%s is listed twice", each.Identifier)
		}
		seen[each.Identifier] = true
		switch {
		case !IsValidIdentifier(each.Identifier):
			t.Errorf("%q is not a valid identifier", each.Identifier)
		case !IsKnownCategory(string(each.Category)):
			t.Errorf("%s is in %q, which is not a category", each.Identifier, each.Category)
		case each.Name == "" || each.ValueKind == "":
			t.Errorf("%s has no name or no value kind: %+v", each.Identifier, each)
		case each.ValueKind == ValueKindQuantity && each.QuantityUnit == "":
			t.Errorf("%s is a quantity with no unit", each.Identifier)
		case each.ValueKind != ValueKindQuantity && each.QuantityUnit != "":
			t.Errorf("%s has a unit and is not a quantity", each.Identifier)
		case each.ValueKind == ValueKindCharacteristic && each.SummaryKind != "":
			t.Errorf("%s is a characteristic and has days", each.Identifier)
		case each.ValueKind != ValueKindCharacteristic && each.SummaryKind == "":
			t.Errorf("%s has days and no way to add them up", each.Identifier)
		case !each.IsKnown:
			t.Errorf("%s is in the table and says it is not known", each.Identifier)
		}
	}
	if len(Types()) != len(knownTypes) {
		t.Errorf("Types lists %d of %d kinds: one is in a category that is not listed", len(Types()), len(knownTypes))
	}
}

// The kinds a person is likely to ask about are described the way the Health
// app describes them.
func TestTheCommonTypesAreDescribed(t *testing.T) {
	t.Parallel()

	for _, want := range []Type{
		{Identifier: "stepCount", Category: CategoryActivity, Name: "Steps", ValueKind: ValueKindQuantity, QuantityUnit: "count", SummaryKind: SummaryKindSum, IsKnown: true},
		{Identifier: "heartRate", Category: CategoryHeart, Name: "Heart rate", ValueKind: ValueKindQuantity, QuantityUnit: "count/min", SummaryKind: SummaryKindAverage, IsKnown: true},
		{Identifier: "bodyMass", Category: CategoryBodyMeasurements, Name: "Weight", ValueKind: ValueKindQuantity, QuantityUnit: "kg", SummaryKind: SummaryKindAverage, IsKnown: true},
		{Identifier: "sleepAnalysis", Category: CategorySleep, Name: "Sleep", ValueKind: ValueKindCategory, SummaryKind: SummaryKindDuration, IsKnown: true},
		{Identifier: "workout", Category: CategoryActivity, Name: "Workouts", ValueKind: ValueKindWorkout, SummaryKind: SummaryKindDuration, IsKnown: true},
		{Identifier: "bloodType", Category: CategoryOther, Name: "Blood type", ValueKind: ValueKindCharacteristic, IsKnown: true},
	} {
		if got := TypeOf(want.Identifier); got != want {
			t.Errorf("TypeOf(%q) = %+v, want %+v", want.Identifier, got, want)
		}
	}
	for identifier, unit := range map[string]string{
		"vo2Max": "ml/(kg*min)", "oxygenSaturation": "%", "bloodGlucose": "mg/dL", "bloodPressureSystolic": "mmHg",
		"heartRateVariabilitySDNN": "ms", "dietaryEnergyConsumed": "kcal", "bodyTemperature": "degC", "distanceWalkingRunning": "m",
	} {
		if got := TypeOf(identifier).QuantityUnit; got != unit {
			t.Errorf("%s is sent in %q, want %q", identifier, got, unit)
		}
	}
}

// A kind the table does not know is kept under other and named from its
// identifier, so a new release of iOS needs no change here.
func TestAnUnknownTypeIsKeptUnderOther(t *testing.T) {
	t.Parallel()

	for identifier, name := range map[string]string{
		"someNewReading":     "Some new reading",
		"heartRateSDNNScore": "Heart rate SDNN score",
		"stepCount2":         "Step count2",
		"x":                  "X",
		"":                   "",
		"AlreadyCapitalized": "Already capitalized",
	} {
		got := TypeOf(identifier)
		if got.IsKnown || got.Category != CategoryOther || got.Name != name || got.Identifier != identifier {
			t.Errorf("TypeOf(%q) = %+v, want an unknown kind in other named %q", identifier, got, name)
		}
		if IsKnownType(identifier) {
			t.Errorf("%q is not in the table", identifier)
		}
	}
	if !IsKnownType("stepCount") {
		t.Error("stepCount is in the table")
	}
}

// The categories are the Health app's, in its order, and nothing else is one.
func TestTheCategoriesAreTheHealthAppsGroups(t *testing.T) {
	t.Parallel()

	want := []Category{"activity", "bodyMeasurements", "cycleTracking", "hearing", "heart", "mentalWellbeing", "mobility",
		"nutrition", "respiratory", "sleep", "symptoms", "vitals", "other"}
	if got := Categories(); !slices.Equal(got, want) {
		t.Errorf("Categories() = %v, want %v", got, want)
	}
	for _, category := range want {
		if !IsKnownCategory(string(category)) || category.Name() == "" || category.Name() == string(category) {
			t.Errorf("%s is a category with the name %q", category, category.Name())
		}
	}
	for _, text := range []string{"", "Activity", "steps", "activity ", "all"} {
		if IsKnownCategory(text) {
			t.Errorf("%q is not a category", text)
		}
	}
	if got := Category("unheardOf").Name(); got != "unheardOf" {
		t.Errorf("a category not known is named by its identifier, got %q", got)
	}
	// Every category has something in it, so an offered choice is never empty.
	for _, category := range want {
		isFound := false
		for _, each := range Types() {
			isFound = isFound || each.Category == category
		}
		if !isFound {
			t.Errorf("no kind is in %s", category)
		}
	}
}

func TestOnlyLettersAndDigitsAreAnIdentifier(t *testing.T) {
	t.Parallel()

	for _, good := range []string{"stepCount", "vo2Max", "a", "x123"} {
		if !IsValidIdentifier(good) {
			t.Errorf("%q is a valid identifier", good)
		}
	}
	long := make([]byte, IdentifierLongest+1)
	for index := range long {
		long[index] = 'a'
	}
	for _, bad := range []string{"", "step count", "step-count", "step_count", "step.count", "stép", "step\ncount", "日本語", string(long)} {
		if IsValidIdentifier(bad) {
			t.Errorf("%q is not a valid identifier", bad)
		}
	}
	if !IsValidIdentifier(string(long[:IdentifierLongest])) {
		t.Error("an identifier of the longest length is valid")
	}
}
