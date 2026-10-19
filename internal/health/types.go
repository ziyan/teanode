// Package health knows the kinds of data Apple Health keeps, by the names
// HealthKit gives them: what each is called on screen, which group of the
// Health app it is in, what shape its value has, and how a day of it adds up.
//
// The iPhone app has the same identifiers in TeaNodeKit (HealthTypes.swift) to
// ask HealthKit for; this table is what the server, the dashboard and the
// agent describe them with. A kind this table does not know is still accepted
// and kept (see TypeOf), so a new iOS release needs no server change.
package health

import (
	"strings"
	"unicode"
)

// Category is one of the groups the Health app sorts its data into. A person
// shares their Health by category.
type Category string

// The categories, in the order the Health app lists its groups.
const (
	CategoryActivity         Category = "activity"
	CategoryBodyMeasurements Category = "bodyMeasurements"
	CategoryCycleTracking    Category = "cycleTracking"
	CategoryHearing          Category = "hearing"
	CategoryHeart            Category = "heart"
	CategoryMentalWellbeing  Category = "mentalWellbeing"
	CategoryMobility         Category = "mobility"
	CategoryNutrition        Category = "nutrition"
	CategoryRespiratory      Category = "respiratory"
	CategorySleep            Category = "sleep"
	CategorySymptoms         Category = "symptoms"
	CategoryVitals           Category = "vitals"
	// CategoryOther holds what fits no group, and every kind this table does
	// not know.
	CategoryOther Category = "other"
)

// categories is the list in the order a screen offers it, with the English
// name of each.
var categories = []struct {
	category Category
	name     string
}{
	{CategoryActivity, "Activity"},
	{CategoryBodyMeasurements, "Body Measurements"},
	{CategoryCycleTracking, "Cycle Tracking"},
	{CategoryHearing, "Hearing"},
	{CategoryHeart, "Heart"},
	{CategoryMentalWellbeing, "Mental Wellbeing"},
	{CategoryMobility, "Mobility"},
	{CategoryNutrition, "Nutrition"},
	{CategoryRespiratory, "Respiratory"},
	{CategorySleep, "Sleep"},
	{CategorySymptoms, "Symptoms"},
	{CategoryVitals, "Vitals"},
	{CategoryOther, "Other"},
}

// Categories lists every category in the order the Health app shows them.
func Categories() []Category {
	listed := make([]Category, 0, len(categories))
	for _, entry := range categories {
		listed = append(listed, entry.category)
	}
	return listed
}

// IsKnownCategory reports whether the text is one of the categories.
func IsKnownCategory(text string) bool {
	for _, entry := range categories {
		if string(entry.category) == text {
			return true
		}
	}
	return false
}

// Name is the category's English name, or its identifier for one that is not
// known.
func (self Category) Name() string {
	for _, entry := range categories {
		if entry.category == self {
			return entry.name
		}
	}
	return string(self)
}

// ValueKind is the shape of a reading's value.
type ValueKind string

// The shapes of a value.
const (
	// ValueKindQuantity is a number in a unit: steps, heart rate, weight.
	ValueKindQuantity ValueKind = "quantity"
	// ValueKindCategory is one of a fixed list of states over a time: a stage
	// of sleep, a symptom's severity, a flow.
	ValueKindCategory ValueKind = "category"
	// ValueKindWorkout is one workout, with its totals in the metadata and
	// perhaps a route.
	ValueKindWorkout ValueKind = "workout"
	// ValueKindElectrocardiogram is one electrocardiogram result: its
	// classification and average heart rate, not the voltages.
	ValueKindElectrocardiogram ValueKind = "electrocardiogram"
	// ValueKindAudiogram is one hearing test, its points in the metadata.
	ValueKindAudiogram ValueKind = "audiogram"
	// ValueKindStateOfMind is one State of Mind entry, its labels in the
	// metadata.
	ValueKindStateOfMind ValueKind = "stateOfMind"
	// ValueKindCharacteristic is a fact about the person that has one value
	// and no days: date of birth, blood type.
	ValueKindCharacteristic ValueKind = "characteristic"
)

// SummaryKind is how a day of a kind of reading adds up, which decides what a
// chart of it draws.
type SummaryKind string

// The ways a day adds up.
const (
	// SummaryKindSum is for what is counted or burned over the day: steps,
	// energy, distance.
	SummaryKindSum SummaryKind = "sum"
	// SummaryKindAverage is for what is measured at moments: heart rate,
	// weight. A day keeps the average with the lowest and the highest.
	SummaryKindAverage SummaryKind = "average"
	// SummaryKindDuration is for what is the time something lasted: sleep,
	// mindful minutes. Sources that overlap are counted once.
	SummaryKindDuration SummaryKind = "duration"
	// SummaryKindCount is for events: a symptom, a high heart rate
	// notification, an electrocardiogram.
	SummaryKindCount SummaryKind = "count"
)

// Type is one kind of data.
type Type struct {
	// Identifier is HealthKit's identifier without its prefix: stepCount,
	// heartRate, sleepAnalysis, workout.
	Identifier string

	// Category is the group of the Health app it is shown in.
	Category Category

	// Name is the English name in sentence case (the table at the top of
	// words.go lists the rules), the same on every surface.
	Name string

	ValueKind ValueKind

	// QuantityUnit is the HKUnit string the phone sends a quantity in
	// ("count/min", "kg", "ml/(kg*min)"), empty for a kind that has none.
	QuantityUnit string

	// SummaryKind is how a day of it adds up; empty for a characteristic,
	// which has no days.
	SummaryKind SummaryKind

	// IsKnown is false for a kind the table does not list. Its category is
	// other and its name is made from its identifier; its value kind and
	// summary kind are guesses, and the day summaries the phone sent say what
	// is filled in.
	IsKnown bool
}

func quantity(identifier string, category Category, name, quantityUnit string, summaryKind SummaryKind) Type {
	return Type{Identifier: identifier, Category: category, Name: name, ValueKind: ValueKindQuantity,
		QuantityUnit: quantityUnit, SummaryKind: summaryKind, IsKnown: true}
}

func categorical(identifier string, category Category, name string, summaryKind SummaryKind) Type {
	return Type{Identifier: identifier, Category: category, Name: name, ValueKind: ValueKindCategory,
		SummaryKind: summaryKind, IsKnown: true}
}

func characteristic(identifier, name string) Type {
	return Type{Identifier: identifier, Category: CategoryOther, Name: name, ValueKind: ValueKindCharacteristic, IsKnown: true}
}

// symptom is a category type of the Symptoms group; each is an event with a
// severity, and a day counts them.
func symptom(identifier, name string) Type {
	return categorical(identifier, CategorySymptoms, name, SummaryKindCount)
}

// dietary is a nutrient, which a day adds up.
func dietary(identifier, name, quantityUnit string) Type {
	return quantity(identifier, CategoryNutrition, name, quantityUnit, SummaryKindSum)
}

var knownTypes = []Type{
	// Activity.
	quantity("stepCount", CategoryActivity, "Steps", "count", SummaryKindSum),
	quantity("distanceWalkingRunning", CategoryActivity, "Walking + running distance", "m", SummaryKindSum),
	quantity("distanceCycling", CategoryActivity, "Cycling distance", "m", SummaryKindSum),
	quantity("distanceWheelchair", CategoryActivity, "Wheelchair distance", "m", SummaryKindSum),
	quantity("distanceSwimming", CategoryActivity, "Swimming distance", "m", SummaryKindSum),
	quantity("distanceDownhillSnowSports", CategoryActivity, "Downhill snow sports distance", "m", SummaryKindSum),
	quantity("distanceCrossCountrySkiing", CategoryActivity, "Cross-country skiing distance", "m", SummaryKindSum),
	quantity("distancePaddleSports", CategoryActivity, "Paddle sports distance", "m", SummaryKindSum),
	quantity("distanceRowing", CategoryActivity, "Rowing distance", "m", SummaryKindSum),
	quantity("distanceSkatingSports", CategoryActivity, "Skating sports distance", "m", SummaryKindSum),
	quantity("basalEnergyBurned", CategoryActivity, "Resting energy", "kcal", SummaryKindSum),
	quantity("activeEnergyBurned", CategoryActivity, "Active energy", "kcal", SummaryKindSum),
	quantity("flightsClimbed", CategoryActivity, "Flights climbed", "count", SummaryKindSum),
	quantity("nikeFuel", CategoryActivity, "NikeFuel", "count", SummaryKindSum),
	quantity("appleExerciseTime", CategoryActivity, "Exercise minutes", "min", SummaryKindSum),
	quantity("appleMoveTime", CategoryActivity, "Move minutes", "min", SummaryKindSum),
	quantity("appleStandTime", CategoryActivity, "Stand minutes", "min", SummaryKindSum),
	quantity("pushCount", CategoryActivity, "Wheelchair pushes", "count", SummaryKindSum),
	quantity("swimmingStrokeCount", CategoryActivity, "Swimming strokes", "count", SummaryKindSum),
	quantity("cyclingSpeed", CategoryActivity, "Cycling speed", "m/s", SummaryKindAverage),
	quantity("cyclingPower", CategoryActivity, "Cycling power", "W", SummaryKindAverage),
	quantity("cyclingCadence", CategoryActivity, "Cycling cadence", "count/min", SummaryKindAverage),
	quantity("cyclingFunctionalThresholdPower", CategoryActivity, "Cycling functional threshold power", "W", SummaryKindAverage),
	quantity("runningSpeed", CategoryActivity, "Running speed", "m/s", SummaryKindAverage),
	quantity("runningPower", CategoryActivity, "Running power", "W", SummaryKindAverage),
	quantity("runningStrideLength", CategoryActivity, "Running stride length", "m", SummaryKindAverage),
	quantity("runningVerticalOscillation", CategoryActivity, "Running vertical oscillation", "cm", SummaryKindAverage),
	quantity("runningGroundContactTime", CategoryActivity, "Running ground contact time", "ms", SummaryKindAverage),
	quantity("crossCountrySkiingSpeed", CategoryActivity, "Cross-country skiing speed", "m/s", SummaryKindAverage),
	quantity("paddleSportsSpeed", CategoryActivity, "Paddle sports speed", "m/s", SummaryKindAverage),
	quantity("rowingSpeed", CategoryActivity, "Rowing speed", "m/s", SummaryKindAverage),
	quantity("physicalEffort", CategoryActivity, "Physical effort", "kcal/(kg*hr)", SummaryKindAverage),
	quantity("workoutEffortScore", CategoryActivity, "Workout effort score", "appleEffortScore", SummaryKindAverage),
	quantity("estimatedWorkoutEffortScore", CategoryActivity, "Estimated workout effort score", "appleEffortScore", SummaryKindAverage),
	quantity("underwaterDepth", CategoryActivity, "Underwater depth", "m", SummaryKindAverage),
	quantity("waterTemperature", CategoryActivity, "Water temperature", "degC", SummaryKindAverage),
	categorical("appleStandHour", CategoryActivity, "Stand hours", SummaryKindCount),
	{Identifier: "workout", Category: CategoryActivity, Name: "Workouts", ValueKind: ValueKindWorkout, SummaryKind: SummaryKindDuration, IsKnown: true},

	// Body measurements.
	quantity("bodyMass", CategoryBodyMeasurements, "Weight", "kg", SummaryKindAverage),
	quantity("bodyMassIndex", CategoryBodyMeasurements, "Body mass index", "count", SummaryKindAverage),
	quantity("bodyFatPercentage", CategoryBodyMeasurements, "Body fat percentage", "%", SummaryKindAverage),
	quantity("leanBodyMass", CategoryBodyMeasurements, "Lean body mass", "kg", SummaryKindAverage),
	quantity("height", CategoryBodyMeasurements, "Height", "m", SummaryKindAverage),
	quantity("waistCircumference", CategoryBodyMeasurements, "Waist circumference", "m", SummaryKindAverage),

	// Cycle tracking.
	quantity("basalBodyTemperature", CategoryCycleTracking, "Basal body temperature", "degC", SummaryKindAverage),
	categorical("menstrualFlow", CategoryCycleTracking, "Menstrual flow", SummaryKindCount),
	categorical("intermenstrualBleeding", CategoryCycleTracking, "Spotting", SummaryKindCount),
	categorical("infrequentMenstrualCycles", CategoryCycleTracking, "Infrequent periods", SummaryKindCount),
	categorical("irregularMenstrualCycles", CategoryCycleTracking, "Irregular periods", SummaryKindCount),
	categorical("persistentIntermenstrualBleeding", CategoryCycleTracking, "Persistent spotting", SummaryKindCount),
	categorical("prolongedMenstrualPeriods", CategoryCycleTracking, "Prolonged periods", SummaryKindCount),
	categorical("cervicalMucusQuality", CategoryCycleTracking, "Cervical mucus quality", SummaryKindCount),
	categorical("ovulationTestResult", CategoryCycleTracking, "Ovulation test result", SummaryKindCount),
	categorical("pregnancyTestResult", CategoryCycleTracking, "Pregnancy test result", SummaryKindCount),
	categorical("progesteroneTestResult", CategoryCycleTracking, "Progesterone test result", SummaryKindCount),
	categorical("pregnancy", CategoryCycleTracking, "Pregnancy", SummaryKindDuration),
	categorical("lactation", CategoryCycleTracking, "Lactation", SummaryKindDuration),
	categorical("contraceptive", CategoryCycleTracking, "Contraceptive", SummaryKindDuration),
	categorical("sexualActivity", CategoryCycleTracking, "Sexual activity", SummaryKindCount),
	categorical("bleedingDuringPregnancy", CategoryCycleTracking, "Bleeding during pregnancy", SummaryKindCount),
	categorical("bleedingAfterPregnancy", CategoryCycleTracking, "Bleeding after pregnancy", SummaryKindCount),

	// Hearing.
	quantity("environmentalAudioExposure", CategoryHearing, "Environmental sound levels", "dBASPL", SummaryKindAverage),
	quantity("headphoneAudioExposure", CategoryHearing, "Headphone audio levels", "dBASPL", SummaryKindAverage),
	quantity("environmentalSoundReduction", CategoryHearing, "Environmental sound reduction", "dBASPL", SummaryKindAverage),
	categorical("environmentalAudioExposureEvent", CategoryHearing, "Environmental sound level notifications", SummaryKindCount),
	categorical("headphoneAudioExposureEvent", CategoryHearing, "Headphone audio level notifications", SummaryKindCount),
	{Identifier: "audiogram", Category: CategoryHearing, Name: "Audiograms", ValueKind: ValueKindAudiogram, SummaryKind: SummaryKindCount, IsKnown: true},

	// Heart.
	quantity("heartRate", CategoryHeart, "Heart rate", "count/min", SummaryKindAverage),
	quantity("restingHeartRate", CategoryHeart, "Resting heart rate", "count/min", SummaryKindAverage),
	quantity("walkingHeartRateAverage", CategoryHeart, "Walking heart rate average", "count/min", SummaryKindAverage),
	quantity("heartRateVariabilitySDNN", CategoryHeart, "Heart rate variability", "ms", SummaryKindAverage),
	quantity("heartRateRecoveryOneMinute", CategoryHeart, "Cardio recovery", "count/min", SummaryKindAverage),
	quantity("vo2Max", CategoryHeart, "Cardio fitness", "ml/(kg*min)", SummaryKindAverage),
	quantity("atrialFibrillationBurden", CategoryHeart, "Atrial fibrillation burden", "%", SummaryKindAverage),
	quantity("peripheralPerfusionIndex", CategoryHeart, "Peripheral perfusion index", "%", SummaryKindAverage),
	categorical("highHeartRateEvent", CategoryHeart, "High heart rate notifications", SummaryKindCount),
	categorical("lowHeartRateEvent", CategoryHeart, "Low heart rate notifications", SummaryKindCount),
	categorical("irregularHeartRhythmEvent", CategoryHeart, "Irregular rhythm notifications", SummaryKindCount),
	categorical("lowCardioFitnessEvent", CategoryHeart, "Low cardio fitness notifications", SummaryKindCount),
	{Identifier: "electrocardiogram", Category: CategoryHeart, Name: "Electrocardiograms", ValueKind: ValueKindElectrocardiogram, SummaryKind: SummaryKindCount, IsKnown: true},

	// Mental wellbeing.
	categorical("mindfulSession", CategoryMentalWellbeing, "Mindful minutes", SummaryKindDuration),
	{Identifier: "stateOfMind", Category: CategoryMentalWellbeing, Name: "State of mind", ValueKind: ValueKindStateOfMind, SummaryKind: SummaryKindCount, IsKnown: true},

	// Mobility.
	quantity("walkingSpeed", CategoryMobility, "Walking speed", "m/s", SummaryKindAverage),
	quantity("walkingStepLength", CategoryMobility, "Walking step length", "m", SummaryKindAverage),
	quantity("walkingAsymmetryPercentage", CategoryMobility, "Walking asymmetry", "%", SummaryKindAverage),
	quantity("walkingDoubleSupportPercentage", CategoryMobility, "Walking double support", "%", SummaryKindAverage),
	quantity("appleWalkingSteadiness", CategoryMobility, "Walking steadiness", "%", SummaryKindAverage),
	quantity("sixMinuteWalkTestDistance", CategoryMobility, "Six-minute walk distance", "m", SummaryKindAverage),
	quantity("stairAscentSpeed", CategoryMobility, "Stair ascent speed", "m/s", SummaryKindAverage),
	quantity("stairDescentSpeed", CategoryMobility, "Stair descent speed", "m/s", SummaryKindAverage),
	quantity("numberOfTimesFallen", CategoryMobility, "Falls", "count", SummaryKindSum),
	categorical("appleWalkingSteadinessEvent", CategoryMobility, "Walking steadiness notifications", SummaryKindCount),

	// Nutrition.
	dietary("dietaryEnergyConsumed", "Dietary energy", "kcal"),
	dietary("dietaryCarbohydrates", "Carbohydrates", "g"),
	dietary("dietaryFiber", "Fiber", "g"),
	dietary("dietarySugar", "Sugar", "g"),
	dietary("dietaryFatTotal", "Total fat", "g"),
	dietary("dietaryFatMonounsaturated", "Monounsaturated fat", "g"),
	dietary("dietaryFatPolyunsaturated", "Polyunsaturated fat", "g"),
	dietary("dietaryFatSaturated", "Saturated fat", "g"),
	dietary("dietaryCholesterol", "Cholesterol", "mg"),
	dietary("dietaryProtein", "Protein", "g"),
	dietary("dietaryVitaminA", "Vitamin A", "mcg"),
	dietary("dietaryThiamin", "Thiamin (B1)", "mg"),
	dietary("dietaryRiboflavin", "Riboflavin (B2)", "mg"),
	dietary("dietaryNiacin", "Niacin (B3)", "mg"),
	dietary("dietaryPantothenicAcid", "Pantothenic acid (B5)", "mg"),
	dietary("dietaryVitaminB6", "Vitamin B6", "mg"),
	dietary("dietaryBiotin", "Biotin (B7)", "mcg"),
	dietary("dietaryVitaminB12", "Vitamin B12", "mcg"),
	dietary("dietaryVitaminC", "Vitamin C", "mg"),
	dietary("dietaryVitaminD", "Vitamin D", "mcg"),
	dietary("dietaryVitaminE", "Vitamin E", "mg"),
	dietary("dietaryVitaminK", "Vitamin K", "mcg"),
	dietary("dietaryFolate", "Folate", "mcg"),
	dietary("dietaryCalcium", "Calcium", "mg"),
	dietary("dietaryChloride", "Chloride", "mg"),
	dietary("dietaryIron", "Iron", "mg"),
	dietary("dietaryMagnesium", "Magnesium", "mg"),
	dietary("dietaryPhosphorus", "Phosphorus", "mg"),
	dietary("dietaryPotassium", "Potassium", "mg"),
	dietary("dietarySodium", "Sodium", "mg"),
	dietary("dietaryZinc", "Zinc", "mg"),
	dietary("dietaryChromium", "Chromium", "mcg"),
	dietary("dietaryCopper", "Copper", "mg"),
	dietary("dietaryIodine", "Iodine", "mcg"),
	dietary("dietaryManganese", "Manganese", "mg"),
	dietary("dietaryMolybdenum", "Molybdenum", "mcg"),
	dietary("dietarySelenium", "Selenium", "mcg"),
	dietary("dietaryWater", "Water", "mL"),
	dietary("dietaryCaffeine", "Caffeine", "mg"),

	// Respiratory.
	quantity("respiratoryRate", CategoryRespiratory, "Respiratory rate", "count/min", SummaryKindAverage),
	quantity("oxygenSaturation", CategoryRespiratory, "Blood oxygen", "%", SummaryKindAverage),
	quantity("forcedVitalCapacity", CategoryRespiratory, "Forced vital capacity", "L", SummaryKindAverage),
	quantity("forcedExpiratoryVolume1", CategoryRespiratory, "Forced expiratory volume", "L", SummaryKindAverage),
	quantity("peakExpiratoryFlowRate", CategoryRespiratory, "Peak expiratory flow rate", "L/min", SummaryKindAverage),
	quantity("inhalerUsage", CategoryRespiratory, "Inhaler usage", "count", SummaryKindSum),

	// Sleep.
	categorical("sleepAnalysis", CategorySleep, "Sleep", SummaryKindDuration),
	categorical("sleepApneaEvent", CategorySleep, "Sleep apnea notifications", SummaryKindCount),
	quantity("appleSleepingBreathingDisturbances", CategorySleep, "Breathing disturbances", "count", SummaryKindAverage),

	// Symptoms.
	symptom("abdominalCramps", "Abdominal cramps"),
	symptom("acne", "Acne"),
	symptom("appetiteChanges", "Appetite changes"),
	symptom("bladderIncontinence", "Bladder incontinence"),
	symptom("bloating", "Bloating"),
	symptom("breastPain", "Breast pain"),
	symptom("chestTightnessOrPain", "Chest tightness or pain"),
	symptom("chills", "Chills"),
	symptom("constipation", "Constipation"),
	symptom("coughing", "Coughing"),
	symptom("diarrhea", "Diarrhea"),
	symptom("dizziness", "Dizziness"),
	symptom("drySkin", "Dry skin"),
	symptom("fainting", "Fainting"),
	symptom("fatigue", "Fatigue"),
	symptom("fever", "Fever"),
	symptom("generalizedBodyAche", "Body and muscle ache"),
	symptom("hairLoss", "Hair loss"),
	symptom("headache", "Headache"),
	symptom("heartburn", "Heartburn"),
	symptom("hotFlashes", "Hot flashes"),
	symptom("lossOfSmell", "Loss of smell"),
	symptom("lossOfTaste", "Loss of taste"),
	symptom("lowerBackPain", "Lower back pain"),
	symptom("memoryLapse", "Memory lapse"),
	symptom("moodChanges", "Mood changes"),
	symptom("nausea", "Nausea"),
	symptom("nightSweats", "Night sweats"),
	symptom("pelvicPain", "Pelvic pain"),
	symptom("rapidPoundingOrFlutteringHeartbeat", "Rapid, pounding, or fluttering heartbeat"),
	symptom("runnyNose", "Runny nose"),
	symptom("shortnessOfBreath", "Shortness of breath"),
	symptom("sinusCongestion", "Sinus congestion"),
	symptom("skippedHeartbeat", "Skipped heartbeat"),
	symptom("sleepChanges", "Sleep changes"),
	symptom("soreThroat", "Sore throat"),
	symptom("vaginalDryness", "Vaginal dryness"),
	symptom("vomiting", "Vomiting"),
	symptom("wheezing", "Wheezing"),

	// Vitals.
	quantity("bloodPressureSystolic", CategoryVitals, "Systolic blood pressure", "mmHg", SummaryKindAverage),
	quantity("bloodPressureDiastolic", CategoryVitals, "Diastolic blood pressure", "mmHg", SummaryKindAverage),
	quantity("bloodGlucose", CategoryVitals, "Blood glucose", "mg/dL", SummaryKindAverage),
	quantity("bodyTemperature", CategoryVitals, "Body temperature", "degC", SummaryKindAverage),
	quantity("appleSleepingWristTemperature", CategoryVitals, "Sleeping wrist temperature", "degC", SummaryKindAverage),
	quantity("insulinDelivery", CategoryVitals, "Insulin delivery", "IU", SummaryKindSum),

	// The Other category.
	quantity("uvExposure", CategoryOther, "UV index", "count", SummaryKindAverage),
	quantity("timeInDaylight", CategoryOther, "Time in daylight", "min", SummaryKindSum),
	quantity("electrodermalActivity", CategoryOther, "Electrodermal activity", "S", SummaryKindAverage),
	quantity("bloodAlcoholContent", CategoryOther, "Blood alcohol content", "%", SummaryKindAverage),
	quantity("numberOfAlcoholicBeverages", CategoryOther, "Alcoholic beverages", "count", SummaryKindSum),
	categorical("handwashingEvent", CategoryOther, "Handwashing", SummaryKindCount),
	categorical("toothbrushingEvent", CategoryOther, "Toothbrushing", SummaryKindCount),

	// What HealthKit calls characteristics: the person, not a reading.
	characteristic("dateOfBirth", "Date of birth"),
	characteristic("biologicalSex", "Biological sex"),
	characteristic("bloodType", "Blood type"),
	characteristic("fitzpatrickSkinType", "Skin type"),
	characteristic("wheelchairUse", "Wheelchair use"),
	characteristic("activityMoveMode", "Activity move mode"),
}

var typesByIdentifier = func() map[string]Type {
	indexed := make(map[string]Type, len(knownTypes))
	for _, each := range knownTypes {
		if _, isDuplicate := indexed[each.Identifier]; isDuplicate {
			panic("health: the type " + each.Identifier + " is listed twice")
		}
		indexed[each.Identifier] = each
	}
	return indexed
}()

// Types lists every kind the table knows, in the order of its categories and,
// within one, the order a screen lists them in.
func Types() []Type {
	listed := make([]Type, 0, len(knownTypes))
	for _, category := range Categories() {
		for _, each := range knownTypes {
			if each.Category == category {
				listed = append(listed, each)
			}
		}
	}
	return listed
}

// TypeOf describes the kind with this identifier. A kind the table does not
// know is described anyway, so that a new release of iOS needs no change here:
// it is in the other category and named from its identifier ("someNewReading"
// is "Some new reading").
func TypeOf(identifier string) Type {
	if known, isKnown := typesByIdentifier[identifier]; isKnown {
		return known
	}
	return Type{
		Identifier: identifier, Category: CategoryOther, Name: nameFromIdentifier(identifier),
		ValueKind: ValueKindQuantity, SummaryKind: SummaryKindAverage,
	}
}

// IsKnownType reports whether the table lists this identifier.
func IsKnownType(identifier string) bool {
	_, isKnown := typesByIdentifier[identifier]
	return isKnown
}

// IdentifierLongest is the most characters a type's identifier may have.
const IdentifierLongest = 96

// IsValidIdentifier reports whether the text can be a type's identifier: one
// to IdentifierLongest letters and digits, as HealthKit's are. The check keeps
// what a phone sends out of places an identifier is used as a name.
func IsValidIdentifier(identifier string) bool {
	if identifier == "" || len(identifier) > IdentifierLongest {
		return false
	}
	for _, character := range identifier {
		if character > unicode.MaxASCII || (!unicode.IsLetter(character) && !unicode.IsDigit(character)) {
			return false
		}
	}
	return true
}

// nameFromIdentifier writes an identifier as words in sentence case: a capital
// letter starts a word unless it continues a run of capitals, and a word is
// lowercase unless it is a run of capitals or the first, so
// "heartRateVariabilitySDNN" is "Heart rate variability SDNN".
func nameFromIdentifier(identifier string) string {
	letters := []rune(identifier)
	if len(letters) == 0 {
		return ""
	}
	words := []string{}
	var word strings.Builder
	for index, letter := range letters {
		isWordStart := index > 0 && unicode.IsUpper(letter) && (unicode.IsLower(letters[index-1]) || unicode.IsDigit(letters[index-1]) ||
			(unicode.IsUpper(letters[index-1]) && index+1 < len(letters) && unicode.IsLower(letters[index+1])))
		if isWordStart {
			words = append(words, word.String())
			word.Reset()
		}
		word.WriteRune(letter)
	}
	words = append(words, word.String())
	for index, each := range words {
		isAcronym := len([]rune(each)) > 1 && each == strings.ToUpper(each)
		switch {
		case isAcronym:
		case index == 0:
			words[index] = string(unicode.ToUpper([]rune(each)[0])) + string([]rune(each)[1:])
		default:
			words[index] = strings.ToLower(each)
		}
	}
	return strings.Join(words, " ")
}
