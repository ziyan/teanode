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
	{CategoryOther, "Other Data"},
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

	// Name is the English name, as the Health app writes it.
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
	quantity("distanceWalkingRunning", CategoryActivity, "Walking + Running Distance", "m", SummaryKindSum),
	quantity("distanceCycling", CategoryActivity, "Cycling Distance", "m", SummaryKindSum),
	quantity("distanceWheelchair", CategoryActivity, "Wheelchair Distance", "m", SummaryKindSum),
	quantity("distanceSwimming", CategoryActivity, "Swimming Distance", "m", SummaryKindSum),
	quantity("distanceDownhillSnowSports", CategoryActivity, "Downhill Snow Sports Distance", "m", SummaryKindSum),
	quantity("distanceCrossCountrySkiing", CategoryActivity, "Cross-Country Skiing Distance", "m", SummaryKindSum),
	quantity("distancePaddleSports", CategoryActivity, "Paddle Sports Distance", "m", SummaryKindSum),
	quantity("distanceRowing", CategoryActivity, "Rowing Distance", "m", SummaryKindSum),
	quantity("distanceSkatingSports", CategoryActivity, "Skating Sports Distance", "m", SummaryKindSum),
	quantity("basalEnergyBurned", CategoryActivity, "Resting Energy", "kcal", SummaryKindSum),
	quantity("activeEnergyBurned", CategoryActivity, "Active Energy", "kcal", SummaryKindSum),
	quantity("flightsClimbed", CategoryActivity, "Flights Climbed", "count", SummaryKindSum),
	quantity("nikeFuel", CategoryActivity, "NikeFuel", "count", SummaryKindSum),
	quantity("appleExerciseTime", CategoryActivity, "Exercise Minutes", "min", SummaryKindSum),
	quantity("appleMoveTime", CategoryActivity, "Move Minutes", "min", SummaryKindSum),
	quantity("appleStandTime", CategoryActivity, "Stand Minutes", "min", SummaryKindSum),
	quantity("pushCount", CategoryActivity, "Wheelchair Pushes", "count", SummaryKindSum),
	quantity("swimmingStrokeCount", CategoryActivity, "Swimming Strokes", "count", SummaryKindSum),
	quantity("cyclingSpeed", CategoryActivity, "Cycling Speed", "m/s", SummaryKindAverage),
	quantity("cyclingPower", CategoryActivity, "Cycling Power", "W", SummaryKindAverage),
	quantity("cyclingCadence", CategoryActivity, "Cycling Cadence", "count/min", SummaryKindAverage),
	quantity("cyclingFunctionalThresholdPower", CategoryActivity, "Cycling Functional Threshold Power", "W", SummaryKindAverage),
	quantity("runningSpeed", CategoryActivity, "Running Speed", "m/s", SummaryKindAverage),
	quantity("runningPower", CategoryActivity, "Running Power", "W", SummaryKindAverage),
	quantity("runningStrideLength", CategoryActivity, "Running Stride Length", "m", SummaryKindAverage),
	quantity("runningVerticalOscillation", CategoryActivity, "Running Vertical Oscillation", "cm", SummaryKindAverage),
	quantity("runningGroundContactTime", CategoryActivity, "Running Ground Contact Time", "ms", SummaryKindAverage),
	quantity("crossCountrySkiingSpeed", CategoryActivity, "Cross-Country Skiing Speed", "m/s", SummaryKindAverage),
	quantity("paddleSportsSpeed", CategoryActivity, "Paddle Sports Speed", "m/s", SummaryKindAverage),
	quantity("rowingSpeed", CategoryActivity, "Rowing Speed", "m/s", SummaryKindAverage),
	quantity("physicalEffort", CategoryActivity, "Physical Effort", "kcal/(kg*hr)", SummaryKindAverage),
	quantity("workoutEffortScore", CategoryActivity, "Workout Effort Score", "appleEffortScore", SummaryKindAverage),
	quantity("estimatedWorkoutEffortScore", CategoryActivity, "Estimated Workout Effort Score", "appleEffortScore", SummaryKindAverage),
	quantity("underwaterDepth", CategoryActivity, "Underwater Depth", "m", SummaryKindAverage),
	quantity("waterTemperature", CategoryActivity, "Water Temperature", "degC", SummaryKindAverage),
	categorical("appleStandHour", CategoryActivity, "Stand Hours", SummaryKindCount),
	{Identifier: "workout", Category: CategoryActivity, Name: "Workouts", ValueKind: ValueKindWorkout, SummaryKind: SummaryKindDuration, IsKnown: true},

	// Body measurements.
	quantity("bodyMass", CategoryBodyMeasurements, "Weight", "kg", SummaryKindAverage),
	quantity("bodyMassIndex", CategoryBodyMeasurements, "Body Mass Index", "count", SummaryKindAverage),
	quantity("bodyFatPercentage", CategoryBodyMeasurements, "Body Fat Percentage", "%", SummaryKindAverage),
	quantity("leanBodyMass", CategoryBodyMeasurements, "Lean Body Mass", "kg", SummaryKindAverage),
	quantity("height", CategoryBodyMeasurements, "Height", "m", SummaryKindAverage),
	quantity("waistCircumference", CategoryBodyMeasurements, "Waist Circumference", "m", SummaryKindAverage),

	// Cycle tracking.
	quantity("basalBodyTemperature", CategoryCycleTracking, "Basal Body Temperature", "degC", SummaryKindAverage),
	categorical("menstrualFlow", CategoryCycleTracking, "Menstrual Flow", SummaryKindCount),
	categorical("intermenstrualBleeding", CategoryCycleTracking, "Spotting", SummaryKindCount),
	categorical("infrequentMenstrualCycles", CategoryCycleTracking, "Infrequent Periods", SummaryKindCount),
	categorical("irregularMenstrualCycles", CategoryCycleTracking, "Irregular Periods", SummaryKindCount),
	categorical("persistentIntermenstrualBleeding", CategoryCycleTracking, "Persistent Spotting", SummaryKindCount),
	categorical("prolongedMenstrualPeriods", CategoryCycleTracking, "Prolonged Periods", SummaryKindCount),
	categorical("cervicalMucusQuality", CategoryCycleTracking, "Cervical Mucus Quality", SummaryKindCount),
	categorical("ovulationTestResult", CategoryCycleTracking, "Ovulation Test Result", SummaryKindCount),
	categorical("pregnancyTestResult", CategoryCycleTracking, "Pregnancy Test Result", SummaryKindCount),
	categorical("progesteroneTestResult", CategoryCycleTracking, "Progesterone Test Result", SummaryKindCount),
	categorical("pregnancy", CategoryCycleTracking, "Pregnancy", SummaryKindDuration),
	categorical("lactation", CategoryCycleTracking, "Lactation", SummaryKindDuration),
	categorical("contraceptive", CategoryCycleTracking, "Contraceptive", SummaryKindDuration),
	categorical("sexualActivity", CategoryCycleTracking, "Sexual Activity", SummaryKindCount),
	categorical("bleedingDuringPregnancy", CategoryCycleTracking, "Bleeding During Pregnancy", SummaryKindCount),
	categorical("bleedingAfterPregnancy", CategoryCycleTracking, "Bleeding After Pregnancy", SummaryKindCount),

	// Hearing.
	quantity("environmentalAudioExposure", CategoryHearing, "Environmental Sound Levels", "dBASPL", SummaryKindAverage),
	quantity("headphoneAudioExposure", CategoryHearing, "Headphone Audio Levels", "dBASPL", SummaryKindAverage),
	quantity("environmentalSoundReduction", CategoryHearing, "Environmental Sound Reduction", "dBASPL", SummaryKindAverage),
	categorical("environmentalAudioExposureEvent", CategoryHearing, "Environmental Sound Level Notifications", SummaryKindCount),
	categorical("headphoneAudioExposureEvent", CategoryHearing, "Headphone Audio Level Notifications", SummaryKindCount),
	{Identifier: "audiogram", Category: CategoryHearing, Name: "Audiograms", ValueKind: ValueKindAudiogram, SummaryKind: SummaryKindCount, IsKnown: true},

	// Heart.
	quantity("heartRate", CategoryHeart, "Heart Rate", "count/min", SummaryKindAverage),
	quantity("restingHeartRate", CategoryHeart, "Resting Heart Rate", "count/min", SummaryKindAverage),
	quantity("walkingHeartRateAverage", CategoryHeart, "Walking Heart Rate Average", "count/min", SummaryKindAverage),
	quantity("heartRateVariabilitySDNN", CategoryHeart, "Heart Rate Variability", "ms", SummaryKindAverage),
	quantity("heartRateRecoveryOneMinute", CategoryHeart, "Cardio Recovery", "count/min", SummaryKindAverage),
	quantity("vo2Max", CategoryHeart, "Cardio Fitness", "ml/(kg*min)", SummaryKindAverage),
	quantity("atrialFibrillationBurden", CategoryHeart, "Atrial Fibrillation Burden", "%", SummaryKindAverage),
	quantity("peripheralPerfusionIndex", CategoryHeart, "Peripheral Perfusion Index", "%", SummaryKindAverage),
	categorical("highHeartRateEvent", CategoryHeart, "High Heart Rate Notifications", SummaryKindCount),
	categorical("lowHeartRateEvent", CategoryHeart, "Low Heart Rate Notifications", SummaryKindCount),
	categorical("irregularHeartRhythmEvent", CategoryHeart, "Irregular Rhythm Notifications", SummaryKindCount),
	categorical("lowCardioFitnessEvent", CategoryHeart, "Low Cardio Fitness Notifications", SummaryKindCount),
	{Identifier: "electrocardiogram", Category: CategoryHeart, Name: "Electrocardiograms", ValueKind: ValueKindElectrocardiogram, SummaryKind: SummaryKindCount, IsKnown: true},

	// Mental wellbeing.
	categorical("mindfulSession", CategoryMentalWellbeing, "Mindful Minutes", SummaryKindDuration),
	{Identifier: "stateOfMind", Category: CategoryMentalWellbeing, Name: "State of Mind", ValueKind: ValueKindStateOfMind, SummaryKind: SummaryKindCount, IsKnown: true},

	// Mobility.
	quantity("walkingSpeed", CategoryMobility, "Walking Speed", "m/s", SummaryKindAverage),
	quantity("walkingStepLength", CategoryMobility, "Walking Step Length", "m", SummaryKindAverage),
	quantity("walkingAsymmetryPercentage", CategoryMobility, "Walking Asymmetry", "%", SummaryKindAverage),
	quantity("walkingDoubleSupportPercentage", CategoryMobility, "Walking Double Support", "%", SummaryKindAverage),
	quantity("appleWalkingSteadiness", CategoryMobility, "Walking Steadiness", "%", SummaryKindAverage),
	quantity("sixMinuteWalkTestDistance", CategoryMobility, "Six-Minute Walk Distance", "m", SummaryKindAverage),
	quantity("stairAscentSpeed", CategoryMobility, "Stair Ascent Speed", "m/s", SummaryKindAverage),
	quantity("stairDescentSpeed", CategoryMobility, "Stair Descent Speed", "m/s", SummaryKindAverage),
	quantity("numberOfTimesFallen", CategoryMobility, "Falls", "count", SummaryKindSum),
	categorical("appleWalkingSteadinessEvent", CategoryMobility, "Walking Steadiness Notifications", SummaryKindCount),

	// Nutrition.
	dietary("dietaryEnergyConsumed", "Dietary Energy", "kcal"),
	dietary("dietaryCarbohydrates", "Carbohydrates", "g"),
	dietary("dietaryFiber", "Fiber", "g"),
	dietary("dietarySugar", "Sugar", "g"),
	dietary("dietaryFatTotal", "Total Fat", "g"),
	dietary("dietaryFatMonounsaturated", "Monounsaturated Fat", "g"),
	dietary("dietaryFatPolyunsaturated", "Polyunsaturated Fat", "g"),
	dietary("dietaryFatSaturated", "Saturated Fat", "g"),
	dietary("dietaryCholesterol", "Cholesterol", "mg"),
	dietary("dietaryProtein", "Protein", "g"),
	dietary("dietaryVitaminA", "Vitamin A", "mcg"),
	dietary("dietaryThiamin", "Thiamin (B1)", "mg"),
	dietary("dietaryRiboflavin", "Riboflavin (B2)", "mg"),
	dietary("dietaryNiacin", "Niacin (B3)", "mg"),
	dietary("dietaryPantothenicAcid", "Pantothenic Acid (B5)", "mg"),
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
	quantity("respiratoryRate", CategoryRespiratory, "Respiratory Rate", "count/min", SummaryKindAverage),
	quantity("oxygenSaturation", CategoryRespiratory, "Blood Oxygen", "%", SummaryKindAverage),
	quantity("forcedVitalCapacity", CategoryRespiratory, "Forced Vital Capacity", "L", SummaryKindAverage),
	quantity("forcedExpiratoryVolume1", CategoryRespiratory, "Forced Expiratory Volume", "L", SummaryKindAverage),
	quantity("peakExpiratoryFlowRate", CategoryRespiratory, "Peak Expiratory Flow Rate", "L/min", SummaryKindAverage),
	quantity("inhalerUsage", CategoryRespiratory, "Inhaler Usage", "count", SummaryKindSum),

	// Sleep.
	categorical("sleepAnalysis", CategorySleep, "Sleep", SummaryKindDuration),
	categorical("sleepApneaEvent", CategorySleep, "Sleep Apnea Notifications", SummaryKindCount),
	quantity("appleSleepingBreathingDisturbances", CategorySleep, "Breathing Disturbances", "count", SummaryKindAverage),

	// Symptoms.
	symptom("abdominalCramps", "Abdominal Cramps"),
	symptom("acne", "Acne"),
	symptom("appetiteChanges", "Appetite Changes"),
	symptom("bladderIncontinence", "Bladder Incontinence"),
	symptom("bloating", "Bloating"),
	symptom("breastPain", "Breast Pain"),
	symptom("chestTightnessOrPain", "Chest Tightness or Pain"),
	symptom("chills", "Chills"),
	symptom("constipation", "Constipation"),
	symptom("coughing", "Coughing"),
	symptom("diarrhea", "Diarrhea"),
	symptom("dizziness", "Dizziness"),
	symptom("drySkin", "Dry Skin"),
	symptom("fainting", "Fainting"),
	symptom("fatigue", "Fatigue"),
	symptom("fever", "Fever"),
	symptom("generalizedBodyAche", "Body and Muscle Ache"),
	symptom("hairLoss", "Hair Loss"),
	symptom("headache", "Headache"),
	symptom("heartburn", "Heartburn"),
	symptom("hotFlashes", "Hot Flashes"),
	symptom("lossOfSmell", "Loss of Smell"),
	symptom("lossOfTaste", "Loss of Taste"),
	symptom("lowerBackPain", "Lower Back Pain"),
	symptom("memoryLapse", "Memory Lapse"),
	symptom("moodChanges", "Mood Changes"),
	symptom("nausea", "Nausea"),
	symptom("nightSweats", "Night Sweats"),
	symptom("pelvicPain", "Pelvic Pain"),
	symptom("rapidPoundingOrFlutteringHeartbeat", "Rapid, Pounding, or Fluttering Heartbeat"),
	symptom("runnyNose", "Runny Nose"),
	symptom("shortnessOfBreath", "Shortness of Breath"),
	symptom("sinusCongestion", "Sinus Congestion"),
	symptom("skippedHeartbeat", "Skipped Heartbeat"),
	symptom("sleepChanges", "Sleep Changes"),
	symptom("soreThroat", "Sore Throat"),
	symptom("vaginalDryness", "Vaginal Dryness"),
	symptom("vomiting", "Vomiting"),
	symptom("wheezing", "Wheezing"),

	// Vitals.
	quantity("bloodPressureSystolic", CategoryVitals, "Systolic Blood Pressure", "mmHg", SummaryKindAverage),
	quantity("bloodPressureDiastolic", CategoryVitals, "Diastolic Blood Pressure", "mmHg", SummaryKindAverage),
	quantity("bloodGlucose", CategoryVitals, "Blood Glucose", "mg/dL", SummaryKindAverage),
	quantity("bodyTemperature", CategoryVitals, "Body Temperature", "degC", SummaryKindAverage),
	quantity("appleSleepingWristTemperature", CategoryVitals, "Sleeping Wrist Temperature", "degC", SummaryKindAverage),
	quantity("insulinDelivery", CategoryVitals, "Insulin Delivery", "IU", SummaryKindSum),

	// Other data.
	quantity("uvExposure", CategoryOther, "UV Index", "count", SummaryKindAverage),
	quantity("timeInDaylight", CategoryOther, "Time in Daylight", "min", SummaryKindSum),
	quantity("electrodermalActivity", CategoryOther, "Electrodermal Activity", "S", SummaryKindAverage),
	quantity("bloodAlcoholContent", CategoryOther, "Blood Alcohol Content", "%", SummaryKindAverage),
	quantity("numberOfAlcoholicBeverages", CategoryOther, "Alcoholic Beverages", "count", SummaryKindSum),
	categorical("handwashingEvent", CategoryOther, "Handwashing", SummaryKindCount),
	categorical("toothbrushingEvent", CategoryOther, "Toothbrushing", SummaryKindCount),

	// What HealthKit calls characteristics: the person, not a reading.
	characteristic("dateOfBirth", "Date of Birth"),
	characteristic("biologicalSex", "Biological Sex"),
	characteristic("bloodType", "Blood Type"),
	characteristic("fitzpatrickSkinType", "Skin Type"),
	characteristic("wheelchairUse", "Wheelchair Use"),
	characteristic("activityMoveMode", "Activity Move Mode"),
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
// is "Some New Reading").
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

// nameFromIdentifier writes an identifier as words: a capital letter starts a
// word unless it continues a run of capitals, so "heartRateVariabilitySDNN"
// is "Heart Rate Variability SDNN".
func nameFromIdentifier(identifier string) string {
	letters := []rune(identifier)
	var name strings.Builder
	for index, letter := range letters {
		if index == 0 {
			name.WriteRune(unicode.ToUpper(letter))
			continue
		}
		isWordStart := unicode.IsUpper(letter) && (unicode.IsLower(letters[index-1]) || unicode.IsDigit(letters[index-1]) ||
			(unicode.IsUpper(letters[index-1]) && index+1 < len(letters) && unicode.IsLower(letters[index+1])))
		if isWordStart {
			name.WriteRune(' ')
		}
		name.WriteRune(letter)
	}
	return name.String()
}
