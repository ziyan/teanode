package com.teanode.kit.health

import kotlinx.serialization.Serializable

// Every kind of Health data the app handles, named by HealthKit's identifier without its prefix ("stepCount" is
// HKQuantityTypeIdentifierStepCount), grouped the way the Health app groups them. The server keeps the same table
// (internal/health/types.go) and the iPhone app sends these names; the Android app reads Health Connect and sends the
// same names for the kinds both platforms have, so one person's phones add up on one server. A type the server does not
// know is accepted and kept under "other".

/** HealthCategory is one of the Health app's groups of types. */
@Serializable
enum class HealthCategory(val serverWord: String, val categoryName: String) {
    Activity("activity", "Activity"),
    BodyMeasurements("bodyMeasurements", "Body Measurements"),
    CycleTracking("cycleTracking", "Cycle Tracking"),
    Hearing("hearing", "Hearing"),
    Heart("heart", "Heart"),
    MentalWellbeing("mentalWellbeing", "Mental Wellbeing"),
    Mobility("mobility", "Mobility"),
    Nutrition("nutrition", "Nutrition"),
    Respiratory("respiratory", "Respiratory"),
    Sleep("sleep", "Sleep"),
    Symptoms("symptoms", "Symptoms"),
    Vitals("vitals", "Vitals"),
    Other("other", "Other");

    companion object {
        fun fromServerWord(word: String): HealthCategory? = entries.firstOrNull { category -> category.serverWord == word }
    }
}

/** HealthValueKind is what a sample of a type holds. */
enum class HealthValueKind(val serverWord: String) {
    /** A number with a unit over a time. */
    Quantity("quantity"),

    /** One of a fixed list of values over a time. */
    Category("category"),
    Workout("workout"),

    /** An electrocardiogram result: the classification and the average heart rate, never the voltages. */
    Electrocardiogram("electrocardiogram"),
    Audiogram("audiogram"),
    StateOfMind("stateOfMind"),

    /** A fact about the person (date of birth), not a reading. */
    Characteristic("characteristic");

    companion object {
        fun fromServerWord(word: String): HealthValueKind? = entries.firstOrNull { kind -> kind.serverWord == word }
    }
}

/** HealthSummaryKind is how the readings of a day add up. */
enum class HealthSummaryKind(val serverWord: String) {
    /** Added: steps, energy, distance. */
    Sum("sum"),

    /** Averaged, with the least and the most: heart rate, weight. */
    Average("average"),

    /** The time covered, sources overlapping counted once: sleep, mindful minutes. */
    Duration("duration"),

    /** Counted: events, symptoms, workouts. */
    Count("count");

    companion object {
        fun fromServerWord(word: String): HealthSummaryKind? = entries.firstOrNull { kind -> kind.serverWord == word }
    }
}

/** HealthTypeInfo is one type: what it is called, which group it is in, what its samples hold, the unit to send, and how its days add up. */
data class HealthTypeInfo(
    /** The identifier without its prefix, or the lowercase name of a kind that has none ("workout", "electrocardiogram"). */
    val healthType: String,
    val healthCategory: HealthCategory,
    val valueKind: HealthValueKind,
    /** The unit string a quantity is read in and sent in ("count", "count/min", "kg", "%"); empty for everything else. A percentage is a fraction (0.97 for 97 percent). */
    val quantityUnit: String,
    /** Null for a characteristic, which is not summed by day. */
    val summaryKind: HealthSummaryKind?,
    /** The name in English. */
    val typeName: String,
) {
    val id: String get() = healthType
}

/** HealthTypes is the list of types the app knows. */
object HealthTypes {
    /** The most characteristics the server takes in one call. */
    const val CHARACTERISTIC_COUNT_PER_CALL = 20

    private fun quantity(
        healthType: String, healthCategory: HealthCategory, quantityUnit: String, summaryKind: HealthSummaryKind, typeName: String? = null,
    ): HealthTypeInfo = HealthTypeInfo(healthType, healthCategory, HealthValueKind.Quantity, quantityUnit, summaryKind, typeName ?: words(healthType))

    private fun category(
        healthType: String, healthCategory: HealthCategory, summaryKind: HealthSummaryKind = HealthSummaryKind.Count, typeName: String? = null,
    ): HealthTypeInfo = HealthTypeInfo(healthType, healthCategory, HealthValueKind.Category, "", summaryKind, typeName ?: words(healthType))

    private fun other(
        healthType: String, healthCategory: HealthCategory, valueKind: HealthValueKind, summaryKind: HealthSummaryKind?, typeName: String,
    ): HealthTypeInfo = HealthTypeInfo(healthType, healthCategory, valueKind, "", summaryKind, typeName)

    // The table, in the order the Health app lists its groups.

    private val activityTypes: List<HealthTypeInfo> = listOf(
        quantity("stepCount", HealthCategory.Activity, "count", HealthSummaryKind.Sum, "Steps"),
        quantity("distanceWalkingRunning", HealthCategory.Activity, "m", HealthSummaryKind.Sum, "Walking + running distance"),
        quantity("distanceCycling", HealthCategory.Activity, "m", HealthSummaryKind.Sum, "Cycling distance"),
        quantity("distanceWheelchair", HealthCategory.Activity, "m", HealthSummaryKind.Sum, "Wheelchair distance"),
        quantity("distanceSwimming", HealthCategory.Activity, "m", HealthSummaryKind.Sum, "Swimming distance"),
        quantity("distanceDownhillSnowSports", HealthCategory.Activity, "m", HealthSummaryKind.Sum, "Downhill snow sports distance"),
        quantity("distanceCrossCountrySkiing", HealthCategory.Activity, "m", HealthSummaryKind.Sum, "Cross-country skiing distance"),
        quantity("distancePaddleSports", HealthCategory.Activity, "m", HealthSummaryKind.Sum, "Paddle sports distance"),
        quantity("distanceRowing", HealthCategory.Activity, "m", HealthSummaryKind.Sum, "Rowing distance"),
        quantity("distanceSkatingSports", HealthCategory.Activity, "m", HealthSummaryKind.Sum, "Skating sports distance"),
        quantity("basalEnergyBurned", HealthCategory.Activity, "kcal", HealthSummaryKind.Sum, "Resting energy"),
        quantity("activeEnergyBurned", HealthCategory.Activity, "kcal", HealthSummaryKind.Sum, "Active energy"),
        quantity("flightsClimbed", HealthCategory.Activity, "count", HealthSummaryKind.Sum, "Flights climbed"),
        quantity("nikeFuel", HealthCategory.Activity, "count", HealthSummaryKind.Sum, "NikeFuel"),
        quantity("appleExerciseTime", HealthCategory.Activity, "min", HealthSummaryKind.Sum, "Exercise minutes"),
        quantity("appleMoveTime", HealthCategory.Activity, "min", HealthSummaryKind.Sum, "Move minutes"),
        quantity("appleStandTime", HealthCategory.Activity, "min", HealthSummaryKind.Sum, "Stand minutes"),
        quantity("pushCount", HealthCategory.Activity, "count", HealthSummaryKind.Sum, "Wheelchair pushes"),
        quantity("swimmingStrokeCount", HealthCategory.Activity, "count", HealthSummaryKind.Sum, "Swimming strokes"),
        quantity("cyclingSpeed", HealthCategory.Activity, "m/s", HealthSummaryKind.Average, "Cycling speed"),
        quantity("cyclingPower", HealthCategory.Activity, "W", HealthSummaryKind.Average, "Cycling power"),
        quantity("cyclingCadence", HealthCategory.Activity, "count/min", HealthSummaryKind.Average, "Cycling cadence"),
        quantity("cyclingFunctionalThresholdPower", HealthCategory.Activity, "W", HealthSummaryKind.Average, "Cycling functional threshold power"),
        quantity("runningSpeed", HealthCategory.Activity, "m/s", HealthSummaryKind.Average, "Running speed"),
        quantity("runningPower", HealthCategory.Activity, "W", HealthSummaryKind.Average, "Running power"),
        quantity("runningStrideLength", HealthCategory.Activity, "m", HealthSummaryKind.Average, "Running stride length"),
        quantity("runningVerticalOscillation", HealthCategory.Activity, "cm", HealthSummaryKind.Average, "Running vertical oscillation"),
        quantity("runningGroundContactTime", HealthCategory.Activity, "ms", HealthSummaryKind.Average, "Running ground contact time"),
        quantity("crossCountrySkiingSpeed", HealthCategory.Activity, "m/s", HealthSummaryKind.Average, "Cross-country skiing speed"),
        quantity("paddleSportsSpeed", HealthCategory.Activity, "m/s", HealthSummaryKind.Average, "Paddle sports speed"),
        quantity("rowingSpeed", HealthCategory.Activity, "m/s", HealthSummaryKind.Average, "Rowing speed"),
        quantity("physicalEffort", HealthCategory.Activity, "kcal/(kg*hr)", HealthSummaryKind.Average, "Physical effort"),
        quantity("underwaterDepth", HealthCategory.Activity, "m", HealthSummaryKind.Average, "Underwater depth"),
        quantity("waterTemperature", HealthCategory.Activity, "degC", HealthSummaryKind.Average, "Water temperature"),
        category("appleStandHour", HealthCategory.Activity, HealthSummaryKind.Count, "Stand hours"),
        other("workout", HealthCategory.Activity, HealthValueKind.Workout, HealthSummaryKind.Duration, "Workouts"),
    )


    private val bodyMeasurementTypes: List<HealthTypeInfo> = listOf(
        quantity("bodyMass", HealthCategory.BodyMeasurements, "kg", HealthSummaryKind.Average, "Weight"),
        quantity("bodyMassIndex", HealthCategory.BodyMeasurements, "count", HealthSummaryKind.Average, "Body mass index"),
        quantity("bodyFatPercentage", HealthCategory.BodyMeasurements, "%", HealthSummaryKind.Average, "Body fat percentage"),
        quantity("leanBodyMass", HealthCategory.BodyMeasurements, "kg", HealthSummaryKind.Average, "Lean body mass"),
        quantity("height", HealthCategory.BodyMeasurements, "m", HealthSummaryKind.Average, "Height"),
        quantity("waistCircumference", HealthCategory.BodyMeasurements, "m", HealthSummaryKind.Average, "Waist circumference"),
    )


    private val cycleTrackingTypes: List<HealthTypeInfo> = listOf(
        quantity("basalBodyTemperature", HealthCategory.CycleTracking, "degC", HealthSummaryKind.Average, "Basal body temperature"),
        category("menstrualFlow", HealthCategory.CycleTracking, HealthSummaryKind.Count, "Menstrual flow"),
        category("intermenstrualBleeding", HealthCategory.CycleTracking, HealthSummaryKind.Count, "Spotting"),
        category("infrequentMenstrualCycles", HealthCategory.CycleTracking, HealthSummaryKind.Count, "Infrequent periods"),
        category("irregularMenstrualCycles", HealthCategory.CycleTracking, HealthSummaryKind.Count, "Irregular periods"),
        category("persistentIntermenstrualBleeding", HealthCategory.CycleTracking, HealthSummaryKind.Count, "Persistent spotting"),
        category("prolongedMenstrualPeriods", HealthCategory.CycleTracking, HealthSummaryKind.Count, "Prolonged periods"),
        category("cervicalMucusQuality", HealthCategory.CycleTracking, HealthSummaryKind.Count, "Cervical mucus quality"),
        category("ovulationTestResult", HealthCategory.CycleTracking, HealthSummaryKind.Count, "Ovulation test result"),
        category("pregnancyTestResult", HealthCategory.CycleTracking, HealthSummaryKind.Count, "Pregnancy test result"),
        category("progesteroneTestResult", HealthCategory.CycleTracking, HealthSummaryKind.Count, "Progesterone test result"),
        category("pregnancy", HealthCategory.CycleTracking, HealthSummaryKind.Duration, "Pregnancy"),
        category("lactation", HealthCategory.CycleTracking, HealthSummaryKind.Duration, "Lactation"),
        category("contraceptive", HealthCategory.CycleTracking, HealthSummaryKind.Duration, "Contraceptive"),
        category("sexualActivity", HealthCategory.CycleTracking, HealthSummaryKind.Count, "Sexual activity"),
        category("bleedingDuringPregnancy", HealthCategory.CycleTracking, HealthSummaryKind.Count, "Bleeding during pregnancy"),
        category("bleedingAfterPregnancy", HealthCategory.CycleTracking, HealthSummaryKind.Count, "Bleeding after pregnancy"),
    )


    private val hearingTypes: List<HealthTypeInfo> = listOf(
        quantity("environmentalAudioExposure", HealthCategory.Hearing, "dBASPL", HealthSummaryKind.Average, "Environmental sound levels"),
        quantity("headphoneAudioExposure", HealthCategory.Hearing, "dBASPL", HealthSummaryKind.Average, "Headphone audio levels"),
        quantity("environmentalSoundReduction", HealthCategory.Hearing, "dBASPL", HealthSummaryKind.Average, "Environmental sound reduction"),
        category("environmentalAudioExposureEvent", HealthCategory.Hearing, HealthSummaryKind.Count, "Environmental sound level notifications"),
        category("headphoneAudioExposureEvent", HealthCategory.Hearing, HealthSummaryKind.Count, "Headphone audio level notifications"),
        other("audiogram", HealthCategory.Hearing, HealthValueKind.Audiogram, HealthSummaryKind.Count, "Audiograms"),
    )


    private val heartTypes: List<HealthTypeInfo> = listOf(
        quantity("heartRate", HealthCategory.Heart, "count/min", HealthSummaryKind.Average, "Heart rate"),
        quantity("restingHeartRate", HealthCategory.Heart, "count/min", HealthSummaryKind.Average, "Resting heart rate"),
        quantity("walkingHeartRateAverage", HealthCategory.Heart, "count/min", HealthSummaryKind.Average, "Walking heart rate average"),
        quantity("heartRateVariabilitySDNN", HealthCategory.Heart, "ms", HealthSummaryKind.Average, "Heart rate variability"),
        quantity("heartRateRecoveryOneMinute", HealthCategory.Heart, "count/min", HealthSummaryKind.Average, "Cardio recovery"),
        quantity("vo2Max", HealthCategory.Heart, "ml/(kg*min)", HealthSummaryKind.Average, "Cardio fitness"),
        quantity("atrialFibrillationBurden", HealthCategory.Heart, "%", HealthSummaryKind.Average, "Atrial fibrillation burden"),
        quantity("peripheralPerfusionIndex", HealthCategory.Heart, "%", HealthSummaryKind.Average, "Peripheral perfusion index"),
        category("highHeartRateEvent", HealthCategory.Heart, HealthSummaryKind.Count, "High heart rate notifications"),
        category("lowHeartRateEvent", HealthCategory.Heart, HealthSummaryKind.Count, "Low heart rate notifications"),
        category("irregularHeartRhythmEvent", HealthCategory.Heart, HealthSummaryKind.Count, "Irregular rhythm notifications"),
        category("lowCardioFitnessEvent", HealthCategory.Heart, HealthSummaryKind.Count, "Low cardio fitness notifications"),
        other("electrocardiogram", HealthCategory.Heart, HealthValueKind.Electrocardiogram, HealthSummaryKind.Count, "Electrocardiograms"),
    )


    private val mentalWellbeingTypes: List<HealthTypeInfo> = listOf(
        category("mindfulSession", HealthCategory.MentalWellbeing, HealthSummaryKind.Duration, "Mindful minutes"),
        other("stateOfMind", HealthCategory.MentalWellbeing, HealthValueKind.StateOfMind, HealthSummaryKind.Count, "State of mind"),
    )


    private val mobilityTypes: List<HealthTypeInfo> = listOf(
        quantity("walkingSpeed", HealthCategory.Mobility, "m/s", HealthSummaryKind.Average, "Walking speed"),
        quantity("walkingStepLength", HealthCategory.Mobility, "m", HealthSummaryKind.Average, "Walking step length"),
        quantity("walkingAsymmetryPercentage", HealthCategory.Mobility, "%", HealthSummaryKind.Average, "Walking asymmetry"),
        quantity("walkingDoubleSupportPercentage", HealthCategory.Mobility, "%", HealthSummaryKind.Average, "Walking double support"),
        quantity("appleWalkingSteadiness", HealthCategory.Mobility, "%", HealthSummaryKind.Average, "Walking steadiness"),
        quantity("sixMinuteWalkTestDistance", HealthCategory.Mobility, "m", HealthSummaryKind.Average, "Six-minute walk distance"),
        quantity("stairAscentSpeed", HealthCategory.Mobility, "m/s", HealthSummaryKind.Average, "Stair ascent speed"),
        quantity("stairDescentSpeed", HealthCategory.Mobility, "m/s", HealthSummaryKind.Average, "Stair descent speed"),
        quantity("numberOfTimesFallen", HealthCategory.Mobility, "count", HealthSummaryKind.Sum, "Falls"),
        category("appleWalkingSteadinessEvent", HealthCategory.Mobility, HealthSummaryKind.Count, "Walking steadiness notifications"),
    )


    private val nutritionTypes: List<HealthTypeInfo> = listOf(
        quantity("dietaryEnergyConsumed", HealthCategory.Nutrition, "kcal", HealthSummaryKind.Sum, "Dietary energy"),
        quantity("dietaryCarbohydrates", HealthCategory.Nutrition, "g", HealthSummaryKind.Sum, "Carbohydrates"),
        quantity("dietaryFiber", HealthCategory.Nutrition, "g", HealthSummaryKind.Sum, "Fiber"),
        quantity("dietarySugar", HealthCategory.Nutrition, "g", HealthSummaryKind.Sum, "Sugar"),
        quantity("dietaryFatTotal", HealthCategory.Nutrition, "g", HealthSummaryKind.Sum, "Total fat"),
        quantity("dietaryFatMonounsaturated", HealthCategory.Nutrition, "g", HealthSummaryKind.Sum, "Monounsaturated fat"),
        quantity("dietaryFatPolyunsaturated", HealthCategory.Nutrition, "g", HealthSummaryKind.Sum, "Polyunsaturated fat"),
        quantity("dietaryFatSaturated", HealthCategory.Nutrition, "g", HealthSummaryKind.Sum, "Saturated fat"),
        quantity("dietaryCholesterol", HealthCategory.Nutrition, "mg", HealthSummaryKind.Sum, "Cholesterol"),
        quantity("dietaryProtein", HealthCategory.Nutrition, "g", HealthSummaryKind.Sum, "Protein"),
        quantity("dietaryVitaminA", HealthCategory.Nutrition, "mcg", HealthSummaryKind.Sum, "Vitamin A"),
        quantity("dietaryThiamin", HealthCategory.Nutrition, "mg", HealthSummaryKind.Sum, "Thiamin (B1)"),
        quantity("dietaryRiboflavin", HealthCategory.Nutrition, "mg", HealthSummaryKind.Sum, "Riboflavin (B2)"),
        quantity("dietaryNiacin", HealthCategory.Nutrition, "mg", HealthSummaryKind.Sum, "Niacin (B3)"),
        quantity("dietaryPantothenicAcid", HealthCategory.Nutrition, "mg", HealthSummaryKind.Sum, "Pantothenic acid (B5)"),
        quantity("dietaryVitaminB6", HealthCategory.Nutrition, "mg", HealthSummaryKind.Sum, "Vitamin B6"),
        quantity("dietaryBiotin", HealthCategory.Nutrition, "mcg", HealthSummaryKind.Sum, "Biotin (B7)"),
        quantity("dietaryVitaminB12", HealthCategory.Nutrition, "mcg", HealthSummaryKind.Sum, "Vitamin B12"),
        quantity("dietaryVitaminC", HealthCategory.Nutrition, "mg", HealthSummaryKind.Sum, "Vitamin C"),
        quantity("dietaryVitaminD", HealthCategory.Nutrition, "mcg", HealthSummaryKind.Sum, "Vitamin D"),
        quantity("dietaryVitaminE", HealthCategory.Nutrition, "mg", HealthSummaryKind.Sum, "Vitamin E"),
        quantity("dietaryVitaminK", HealthCategory.Nutrition, "mcg", HealthSummaryKind.Sum, "Vitamin K"),
        quantity("dietaryFolate", HealthCategory.Nutrition, "mcg", HealthSummaryKind.Sum, "Folate"),
        quantity("dietaryCalcium", HealthCategory.Nutrition, "mg", HealthSummaryKind.Sum, "Calcium"),
        quantity("dietaryChloride", HealthCategory.Nutrition, "mg", HealthSummaryKind.Sum, "Chloride"),
        quantity("dietaryIron", HealthCategory.Nutrition, "mg", HealthSummaryKind.Sum, "Iron"),
        quantity("dietaryMagnesium", HealthCategory.Nutrition, "mg", HealthSummaryKind.Sum, "Magnesium"),
        quantity("dietaryPhosphorus", HealthCategory.Nutrition, "mg", HealthSummaryKind.Sum, "Phosphorus"),
        quantity("dietaryPotassium", HealthCategory.Nutrition, "mg", HealthSummaryKind.Sum, "Potassium"),
        quantity("dietarySodium", HealthCategory.Nutrition, "mg", HealthSummaryKind.Sum, "Sodium"),
        quantity("dietaryZinc", HealthCategory.Nutrition, "mg", HealthSummaryKind.Sum, "Zinc"),
        quantity("dietaryChromium", HealthCategory.Nutrition, "mcg", HealthSummaryKind.Sum, "Chromium"),
        quantity("dietaryCopper", HealthCategory.Nutrition, "mg", HealthSummaryKind.Sum, "Copper"),
        quantity("dietaryIodine", HealthCategory.Nutrition, "mcg", HealthSummaryKind.Sum, "Iodine"),
        quantity("dietaryManganese", HealthCategory.Nutrition, "mg", HealthSummaryKind.Sum, "Manganese"),
        quantity("dietaryMolybdenum", HealthCategory.Nutrition, "mcg", HealthSummaryKind.Sum, "Molybdenum"),
        quantity("dietarySelenium", HealthCategory.Nutrition, "mcg", HealthSummaryKind.Sum, "Selenium"),
        quantity("dietaryWater", HealthCategory.Nutrition, "mL", HealthSummaryKind.Sum, "Water"),
        quantity("dietaryCaffeine", HealthCategory.Nutrition, "mg", HealthSummaryKind.Sum, "Caffeine"),
    )


    private val respiratoryTypes: List<HealthTypeInfo> = listOf(
        quantity("respiratoryRate", HealthCategory.Respiratory, "count/min", HealthSummaryKind.Average, "Respiratory rate"),
        quantity("oxygenSaturation", HealthCategory.Respiratory, "%", HealthSummaryKind.Average, "Blood oxygen"),
        quantity("forcedVitalCapacity", HealthCategory.Respiratory, "L", HealthSummaryKind.Average, "Forced vital capacity"),
        quantity("forcedExpiratoryVolume1", HealthCategory.Respiratory, "L", HealthSummaryKind.Average, "Forced expiratory volume"),
        quantity("peakExpiratoryFlowRate", HealthCategory.Respiratory, "L/min", HealthSummaryKind.Average, "Peak expiratory flow rate"),
        quantity("inhalerUsage", HealthCategory.Respiratory, "count", HealthSummaryKind.Sum, "Inhaler usage"),
    )


    private val sleepTypes: List<HealthTypeInfo> = listOf(
        category("sleepAnalysis", HealthCategory.Sleep, HealthSummaryKind.Duration, "Sleep"),
        category("sleepApneaEvent", HealthCategory.Sleep, HealthSummaryKind.Count, "Sleep apnea notifications"),
        quantity("appleSleepingBreathingDisturbances", HealthCategory.Sleep, "count", HealthSummaryKind.Average, "Breathing disturbances"),
    )


    private val symptomTypes: List<HealthTypeInfo> = listOf(
        category("abdominalCramps", HealthCategory.Symptoms, HealthSummaryKind.Count, "Abdominal cramps"),
        category("acne", HealthCategory.Symptoms, HealthSummaryKind.Count, "Acne"),
        category("appetiteChanges", HealthCategory.Symptoms, HealthSummaryKind.Count, "Appetite changes"),
        category("bladderIncontinence", HealthCategory.Symptoms, HealthSummaryKind.Count, "Bladder incontinence"),
        category("bloating", HealthCategory.Symptoms, HealthSummaryKind.Count, "Bloating"),
        category("breastPain", HealthCategory.Symptoms, HealthSummaryKind.Count, "Breast pain"),
        category("chestTightnessOrPain", HealthCategory.Symptoms, HealthSummaryKind.Count, "Chest tightness or pain"),
        category("chills", HealthCategory.Symptoms, HealthSummaryKind.Count, "Chills"),
        category("constipation", HealthCategory.Symptoms, HealthSummaryKind.Count, "Constipation"),
        category("coughing", HealthCategory.Symptoms, HealthSummaryKind.Count, "Coughing"),
        category("diarrhea", HealthCategory.Symptoms, HealthSummaryKind.Count, "Diarrhea"),
        category("dizziness", HealthCategory.Symptoms, HealthSummaryKind.Count, "Dizziness"),
        category("drySkin", HealthCategory.Symptoms, HealthSummaryKind.Count, "Dry skin"),
        category("fainting", HealthCategory.Symptoms, HealthSummaryKind.Count, "Fainting"),
        category("fatigue", HealthCategory.Symptoms, HealthSummaryKind.Count, "Fatigue"),
        category("fever", HealthCategory.Symptoms, HealthSummaryKind.Count, "Fever"),
        category("generalizedBodyAche", HealthCategory.Symptoms, HealthSummaryKind.Count, "Body and muscle ache"),
        category("hairLoss", HealthCategory.Symptoms, HealthSummaryKind.Count, "Hair loss"),
        category("headache", HealthCategory.Symptoms, HealthSummaryKind.Count, "Headache"),
        category("heartburn", HealthCategory.Symptoms, HealthSummaryKind.Count, "Heartburn"),
        category("hotFlashes", HealthCategory.Symptoms, HealthSummaryKind.Count, "Hot flashes"),
        category("lossOfSmell", HealthCategory.Symptoms, HealthSummaryKind.Count, "Loss of smell"),
        category("lossOfTaste", HealthCategory.Symptoms, HealthSummaryKind.Count, "Loss of taste"),
        category("lowerBackPain", HealthCategory.Symptoms, HealthSummaryKind.Count, "Lower back pain"),
        category("memoryLapse", HealthCategory.Symptoms, HealthSummaryKind.Count, "Memory lapse"),
        category("moodChanges", HealthCategory.Symptoms, HealthSummaryKind.Count, "Mood changes"),
        category("nausea", HealthCategory.Symptoms, HealthSummaryKind.Count, "Nausea"),
        category("nightSweats", HealthCategory.Symptoms, HealthSummaryKind.Count, "Night sweats"),
        category("pelvicPain", HealthCategory.Symptoms, HealthSummaryKind.Count, "Pelvic pain"),
        category("rapidPoundingOrFlutteringHeartbeat", HealthCategory.Symptoms, HealthSummaryKind.Count, "Rapid, pounding, or fluttering heartbeat"),
        category("runnyNose", HealthCategory.Symptoms, HealthSummaryKind.Count, "Runny nose"),
        category("shortnessOfBreath", HealthCategory.Symptoms, HealthSummaryKind.Count, "Shortness of breath"),
        category("sinusCongestion", HealthCategory.Symptoms, HealthSummaryKind.Count, "Sinus congestion"),
        category("skippedHeartbeat", HealthCategory.Symptoms, HealthSummaryKind.Count, "Skipped heartbeat"),
        category("sleepChanges", HealthCategory.Symptoms, HealthSummaryKind.Count, "Sleep changes"),
        category("soreThroat", HealthCategory.Symptoms, HealthSummaryKind.Count, "Sore throat"),
        category("vaginalDryness", HealthCategory.Symptoms, HealthSummaryKind.Count, "Vaginal dryness"),
        category("vomiting", HealthCategory.Symptoms, HealthSummaryKind.Count, "Vomiting"),
        category("wheezing", HealthCategory.Symptoms, HealthSummaryKind.Count, "Wheezing"),
    )


    private val vitalsTypes: List<HealthTypeInfo> = listOf(
        quantity("bloodPressureSystolic", HealthCategory.Vitals, "mmHg", HealthSummaryKind.Average, "Systolic blood pressure"),
        quantity("bloodPressureDiastolic", HealthCategory.Vitals, "mmHg", HealthSummaryKind.Average, "Diastolic blood pressure"),
        quantity("bloodGlucose", HealthCategory.Vitals, "mg/dL", HealthSummaryKind.Average, "Blood glucose"),
        quantity("bodyTemperature", HealthCategory.Vitals, "degC", HealthSummaryKind.Average, "Body temperature"),
        quantity("appleSleepingWristTemperature", HealthCategory.Vitals, "degC", HealthSummaryKind.Average, "Sleeping wrist temperature"),
        quantity("insulinDelivery", HealthCategory.Vitals, "IU", HealthSummaryKind.Sum, "Insulin delivery"),
    )


    private val otherTypes: List<HealthTypeInfo> = listOf(
        quantity("uvExposure", HealthCategory.Other, "count", HealthSummaryKind.Average, "UV index"),
        quantity("timeInDaylight", HealthCategory.Other, "min", HealthSummaryKind.Sum, "Time in daylight"),
        quantity("electrodermalActivity", HealthCategory.Other, "S", HealthSummaryKind.Average, "Electrodermal activity"),
        quantity("bloodAlcoholContent", HealthCategory.Other, "%", HealthSummaryKind.Average, "Blood alcohol content"),
        quantity("numberOfAlcoholicBeverages", HealthCategory.Other, "count", HealthSummaryKind.Sum, "Alcoholic beverages"),
        category("handwashingEvent", HealthCategory.Other, HealthSummaryKind.Count, "Handwashing"),
        category("toothbrushingEvent", HealthCategory.Other, HealthSummaryKind.Count, "Toothbrushing"),
        other("dateOfBirth", HealthCategory.Other, HealthValueKind.Characteristic, null, "Date of birth"),
        other("biologicalSex", HealthCategory.Other, HealthValueKind.Characteristic, null, "Biological sex"),
        other("bloodType", HealthCategory.Other, HealthValueKind.Characteristic, null, "Blood type"),
        other("fitzpatrickSkinType", HealthCategory.Other, HealthValueKind.Characteristic, null, "Skin type"),
        other("wheelchairUse", HealthCategory.Other, HealthValueKind.Characteristic, null, "Wheelchair use"),
        other("activityMoveMode", HealthCategory.Other, HealthValueKind.Characteristic, null, "Activity move mode"),
    )

    /** Every type, in the order the Health app lists its groups, in the order of [HealthCategory.entries]. */
    val all: List<HealthTypeInfo> = listOf(
        activityTypes, bodyMeasurementTypes, cycleTrackingTypes, hearingTypes, heartTypes, mentalWellbeingTypes, mobilityTypes, nutritionTypes,
        respiratoryTypes, sleepTypes, symptomTypes, vitalsTypes, otherTypes,
    ).flatten()

    val byHealthType: Map<String, HealthTypeInfo> = all.associateBy { info -> info.healthType }

    /** What is known about a type; null for one this list does not have. */
    fun info(healthType: String): HealthTypeInfo? = byHealthType[healthType]

    /** A type's name in English: the table's, or for one it does not have, the identifier read as words ("someNewType" is "Some new type"). */
    fun typeName(healthType: String): String = byHealthType[healthType]?.typeName ?: words(healthType)

    fun types(inCategory: HealthCategory): List<HealthTypeInfo> = all.filter { info -> info.healthCategory == inCategory }

    fun types(ofValueKind: HealthValueKind): List<HealthTypeInfo> = all.filter { info -> info.valueKind == ofValueKind }

    /**
     * The identifier read as English words in sentence case, as the server writes it (internal/health/words.go): a
     * capital letter begins a word unless it continues a run of capitals, and a word is lowercase unless it is the
     * first or a run of capitals ("heartRateVariabilitySDNN" is "Heart rate variability SDNN").
     */
    internal fun words(identifier: String): String {
        val characters = identifier.toList()
        val wordList = mutableListOf<String>()
        var word = StringBuilder()
        for ((index, character) in characters.withIndex()) {
            if (index > 0 && character.isUpperCase()) {
                val previous = characters[index - 1]
                val next = characters.getOrNull(index + 1)
                if (previous.isLowerCase() || previous.isDigit() || (previous.isUpperCase() && (next?.isLowerCase() ?: false))) {
                    wordList.add(word.toString())
                    word = StringBuilder()
                }
            }
            word.append(character)
        }
        wordList.add(word.toString())
        return wordList.withIndex().joinToString(" ") { indexed ->
            val each = indexed.value
            val isAcronym = each.length > 1 && each == each.uppercase()
            when {
                isAcronym -> each
                indexed.index == 0 -> each.take(1).uppercase() + each.drop(1)
                else -> each.lowercase()
            }
        }
    }
}

/** HealthSleepStage is a value of a sleep sample, by the raw integer the iPhone app sends (HKCategoryValueSleepAnalysis). */
enum class HealthSleepStage(val rawValue: Int, val stageName: String, val isAsleep: Boolean) {
    InBed(0, "inBed", false),
    AsleepUnspecified(1, "asleepUnspecified", true),
    Awake(2, "awake", false),
    AsleepCore(3, "asleepCore", true),
    AsleepDeep(4, "asleepDeep", true),
    AsleepREM(5, "asleepREM", true);

    companion object {
        /** The key under which the seconds asleep in any stage, sources overlapping counted once, are sent. */
        const val ASLEEP_KEY = "asleep"

        fun fromRawValue(rawValue: Int): HealthSleepStage? = entries.firstOrNull { stage -> stage.rawValue == rawValue }
    }
}
