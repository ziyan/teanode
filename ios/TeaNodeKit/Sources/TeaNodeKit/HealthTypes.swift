import Foundation

// Every kind of Health data the app asks HealthKit for, named by HealthKit's
// identifier without its prefix ("stepCount" is HKQuantityTypeIdentifierStepCount),
// grouped the way the Health app groups them. The server keeps the same table
// (internal/health/types.go); a type the server does not know is accepted and
// kept under "other", so a new iOS type needs no server change. No HealthKit
// import here: the app turns these names into HealthKit's objects.

/// HealthCategory is one of the Health app's groups of types.
public enum HealthCategory: String, CaseIterable, Codable, Hashable, Sendable {
    case activity
    case bodyMeasurements
    case cycleTracking
    case hearing
    case heart
    case mentalWellbeing
    case mobility
    case nutrition
    case respiratory
    case sleep
    case symptoms
    case vitals
    case other

    /// The group's name in English, as the Health app writes it; the screens
    /// put it into the person's language.
    public var categoryName: String {
        switch self {
        case .activity: "Activity"
        case .bodyMeasurements: "Body Measurements"
        case .cycleTracking: "Cycle Tracking"
        case .hearing: "Hearing"
        case .heart: "Heart"
        case .mentalWellbeing: "Mental Wellbeing"
        case .mobility: "Mobility"
        case .nutrition: "Nutrition"
        case .respiratory: "Respiratory"
        case .sleep: "Sleep"
        case .symptoms: "Symptoms"
        case .vitals: "Vitals"
        case .other: "Other"
        }
    }
}

/// HealthValueKind is what a sample of a type holds.
public enum HealthValueKind: String, CaseIterable, Codable, Hashable, Sendable {
    /// A number with a unit over a time.
    case quantity
    /// One of a fixed list of values (HealthKit's raw integer) over a time.
    case category
    case workout
    /// An electrocardiogram result: the classification and the average heart
    /// rate, never the voltages.
    case electrocardiogram
    case audiogram
    case stateOfMind
    /// A fact about the person (date of birth), not a reading.
    case characteristic
}

/// HealthSummaryKind is how the readings of a day add up.
public enum HealthSummaryKind: String, CaseIterable, Codable, Hashable, Sendable {
    /// Added: steps, energy, distance. A quantity's sum comes from HealthKit's
    /// statistics, which merge sources the way the Health app does.
    case sum
    /// Averaged, with the least and the most: heart rate, weight.
    case average
    /// The time covered, sources overlapping counted once: sleep, mindful minutes.
    case duration
    /// Counted: events, symptoms, workouts.
    case count
}

/// HealthTypeInfo is one type: what it is called, which group it is in, what
/// its samples hold, the unit to send, and how its days add up.
public struct HealthTypeInfo: Identifiable, Hashable, Sendable {
    /// HealthKit's identifier without its prefix, or the lowercase name of a
    /// kind that has none ("workout", "electrocardiogram").
    public let healthType: String
    public let healthCategory: HealthCategory
    public let valueKind: HealthValueKind
    /// The HKUnit string a quantity is read in and sent in ("count",
    /// "count/min", "kg", "%"); empty for everything else. HealthKit holds a
    /// percentage as a fraction (0.97 for 97 percent) and the server keeps what
    /// it is sent, so the app sends the fraction as HealthKit gave it.
    public let quantityUnit: String
    /// Nil for a characteristic, which is not summed by day.
    public let summaryKind: HealthSummaryKind?
    /// The name in English.
    public let typeName: String

    public var id: String { healthType }

    public init(
        healthType: String, healthCategory: HealthCategory, valueKind: HealthValueKind, quantityUnit: String, summaryKind: HealthSummaryKind?,
        typeName: String
    ) {
        self.healthType = healthType
        self.healthCategory = healthCategory
        self.valueKind = valueKind
        self.quantityUnit = quantityUnit
        self.summaryKind = summaryKind
        self.typeName = typeName
    }

    /// HealthKit's identifier for it, in full ("HKQuantityTypeIdentifierStepCount"),
    /// for a type that has one. The sample types of the other kinds are
    /// objects of their own (HKWorkoutType, HKElectrocardiogramType,
    /// HKAudiogramSampleType, HKStateOfMind), so these answer nil.
    public var healthKitIdentifier: String? {
        let prefix: String
        switch valueKind {
        case .quantity: prefix = "HKQuantityTypeIdentifier"
        case .category: prefix = "HKCategoryTypeIdentifier"
        case .characteristic: prefix = "HKCharacteristicTypeIdentifier"
        case .workout, .electrocardiogram, .audiogram, .stateOfMind: return nil
        }
        if let spelled = HealthTypes.healthKitSuffixExceptions[healthType] { return prefix + spelled }
        return prefix + healthType.prefix(1).uppercased() + String(healthType.dropFirst())
    }
}

/// HealthTypes is the list of types the app asks for.
public enum HealthTypes {
    /// The types whose HealthKit name is not their name with the first letter
    /// capitalized.
    static let healthKitSuffixExceptions: [String: String] = ["vo2Max": "VO2Max", "uvExposure": "UVExposure"]

    /// The most characteristics the server takes in one call.
    public static let characteristicCountPerCall = 20

    /// Every type, in the order the Health app lists its groups, in the order
    /// of `HealthCategory.allCases`.
    public static let all: [HealthTypeInfo] = categories.flatMap { $0 }

    public static let byHealthType: [String: HealthTypeInfo] = Dictionary(uniqueKeysWithValues: all.map { ($0.healthType, $0) })

    /// What is known about a type; nil for one this list does not have.
    public static func info(for healthType: String) -> HealthTypeInfo? { byHealthType[healthType] }

    /// A type's name in English: the table's, or for one it does not have,
    /// the identifier read as words ("someNewType" is "Some new type").
    public static func typeName(for healthType: String) -> String { byHealthType[healthType]?.typeName ?? words(from: healthType) }

    public static func types(in healthCategory: HealthCategory) -> [HealthTypeInfo] { all.filter { $0.healthCategory == healthCategory } }

    public static func types(of valueKind: HealthValueKind) -> [HealthTypeInfo] { all.filter { $0.valueKind == valueKind } }

    /// The identifier read as English words in sentence case, as the server
    /// writes it (internal/health/words.go): a capital letter begins a word
    /// unless it continues a run of capitals, and a word is lowercase unless it
    /// is the first or a run of capitals ("heartRateVariabilitySDNN" is "Heart
    /// rate variability SDNN").
    static func words(from identifier: String) -> String {
        let characters = Array(identifier)
        var wordList: [String] = []
        var word = ""
        for (index, character) in characters.enumerated() {
            if index > 0, character.isUppercase {
                let previous = characters[index - 1]
                let next = index + 1 < characters.count ? characters[index + 1] : nil
                if previous.isLowercase || previous.isNumber || (previous.isUppercase && (next?.isLowercase ?? false)) {
                    wordList.append(word)
                    word = ""
                }
            }
            word.append(character)
        }
        wordList.append(word)
        let sentence = wordList.enumerated().map { (index, each) -> String in
            let isAcronym = each.count > 1 && each == each.uppercased()
            if isAcronym { return each }
            if index == 0 { return each.prefix(1).uppercased() + String(each.dropFirst()) }
            return each.lowercased()
        }
        return sentence.joined(separator: " ")
    }

    // MARK: The table

    private static func quantity(
        _ healthType: String, _ healthCategory: HealthCategory, _ quantityUnit: String, _ summaryKind: HealthSummaryKind, _ typeName: String? = nil
    ) -> HealthTypeInfo {
        HealthTypeInfo(
            healthType: healthType, healthCategory: healthCategory, valueKind: .quantity, quantityUnit: quantityUnit, summaryKind: summaryKind,
            typeName: typeName ?? words(from: healthType))
    }

    private static func category(
        _ healthType: String, _ healthCategory: HealthCategory, _ summaryKind: HealthSummaryKind = .count, _ typeName: String? = nil
    ) -> HealthTypeInfo {
        HealthTypeInfo(
            healthType: healthType, healthCategory: healthCategory, valueKind: .category, quantityUnit: "", summaryKind: summaryKind,
            typeName: typeName ?? words(from: healthType))
    }

    private static func other(
        _ healthType: String, _ healthCategory: HealthCategory, _ valueKind: HealthValueKind, _ summaryKind: HealthSummaryKind?, _ typeName: String
    ) -> HealthTypeInfo {
        HealthTypeInfo(
            healthType: healthType, healthCategory: healthCategory, valueKind: valueKind, quantityUnit: "", summaryKind: summaryKind,
            typeName: typeName)
    }

    private static let categories: [[HealthTypeInfo]] = [
        activityTypes, bodyMeasurementTypes, cycleTrackingTypes, hearingTypes, heartTypes, mentalWellbeingTypes, mobilityTypes, nutritionTypes,
        respiratoryTypes, sleepTypes, symptomTypes, vitalsTypes, otherTypes,
    ]

    private static let activityTypes: [HealthTypeInfo] = [
        quantity("stepCount", .activity, "count", .sum, "Steps"),
        quantity("distanceWalkingRunning", .activity, "m", .sum, "Walking + running distance"),
        quantity("distanceCycling", .activity, "m", .sum, "Cycling distance"),
        quantity("distanceWheelchair", .activity, "m", .sum, "Wheelchair distance"),
        quantity("distanceSwimming", .activity, "m", .sum, "Swimming distance"),
        quantity("distanceDownhillSnowSports", .activity, "m", .sum, "Downhill snow sports distance"),
        quantity("distanceCrossCountrySkiing", .activity, "m", .sum, "Cross-country skiing distance"),
        quantity("distancePaddleSports", .activity, "m", .sum, "Paddle sports distance"),
        quantity("distanceRowing", .activity, "m", .sum, "Rowing distance"),
        quantity("distanceSkatingSports", .activity, "m", .sum, "Skating sports distance"),
        quantity("basalEnergyBurned", .activity, "kcal", .sum, "Resting energy"),
        quantity("activeEnergyBurned", .activity, "kcal", .sum, "Active energy"),
        quantity("flightsClimbed", .activity, "count", .sum, "Flights climbed"),
        quantity("nikeFuel", .activity, "count", .sum, "NikeFuel"),
        quantity("appleExerciseTime", .activity, "min", .sum, "Exercise minutes"),
        quantity("appleMoveTime", .activity, "min", .sum, "Move minutes"),
        quantity("appleStandTime", .activity, "min", .sum, "Stand minutes"),
        quantity("pushCount", .activity, "count", .sum, "Wheelchair pushes"),
        quantity("swimmingStrokeCount", .activity, "count", .sum, "Swimming strokes"),
        quantity("cyclingSpeed", .activity, "m/s", .average, "Cycling speed"),
        quantity("cyclingPower", .activity, "W", .average, "Cycling power"),
        quantity("cyclingCadence", .activity, "count/min", .average, "Cycling cadence"),
        quantity("cyclingFunctionalThresholdPower", .activity, "W", .average, "Cycling functional threshold power"),
        quantity("runningSpeed", .activity, "m/s", .average, "Running speed"),
        quantity("runningPower", .activity, "W", .average, "Running power"),
        quantity("runningStrideLength", .activity, "m", .average, "Running stride length"),
        quantity("runningVerticalOscillation", .activity, "cm", .average, "Running vertical oscillation"),
        quantity("runningGroundContactTime", .activity, "ms", .average, "Running ground contact time"),
        quantity("crossCountrySkiingSpeed", .activity, "m/s", .average, "Cross-country skiing speed"),
        quantity("paddleSportsSpeed", .activity, "m/s", .average, "Paddle sports speed"),
        quantity("rowingSpeed", .activity, "m/s", .average, "Rowing speed"),
        quantity("physicalEffort", .activity, "kcal/(kg*hr)", .average, "Physical effort"),
        quantity("underwaterDepth", .activity, "m", .average, "Underwater depth"),
        quantity("waterTemperature", .activity, "degC", .average, "Water temperature"),
        category("appleStandHour", .activity, .count, "Stand hours"),
        other("workout", .activity, .workout, .duration, "Workouts"),
    ]

    private static let bodyMeasurementTypes: [HealthTypeInfo] = [
        quantity("bodyMass", .bodyMeasurements, "kg", .average, "Weight"),
        quantity("bodyMassIndex", .bodyMeasurements, "count", .average, "Body mass index"),
        quantity("bodyFatPercentage", .bodyMeasurements, "%", .average, "Body fat percentage"),
        quantity("leanBodyMass", .bodyMeasurements, "kg", .average, "Lean body mass"),
        quantity("height", .bodyMeasurements, "m", .average, "Height"),
        quantity("waistCircumference", .bodyMeasurements, "m", .average, "Waist circumference"),
    ]

    private static let cycleTrackingTypes: [HealthTypeInfo] = [
        quantity("basalBodyTemperature", .cycleTracking, "degC", .average, "Basal body temperature"),
        category("menstrualFlow", .cycleTracking, .count, "Menstrual flow"),
        category("intermenstrualBleeding", .cycleTracking, .count, "Spotting"),
        category("infrequentMenstrualCycles", .cycleTracking, .count, "Infrequent periods"),
        category("irregularMenstrualCycles", .cycleTracking, .count, "Irregular periods"),
        category("persistentIntermenstrualBleeding", .cycleTracking, .count, "Persistent spotting"),
        category("prolongedMenstrualPeriods", .cycleTracking, .count, "Prolonged periods"),
        category("cervicalMucusQuality", .cycleTracking, .count, "Cervical mucus quality"),
        category("ovulationTestResult", .cycleTracking, .count, "Ovulation test result"),
        category("pregnancyTestResult", .cycleTracking, .count, "Pregnancy test result"),
        category("progesteroneTestResult", .cycleTracking, .count, "Progesterone test result"),
        category("pregnancy", .cycleTracking, .duration, "Pregnancy"),
        category("lactation", .cycleTracking, .duration, "Lactation"),
        category("contraceptive", .cycleTracking, .duration, "Contraceptive"),
        category("sexualActivity", .cycleTracking, .count, "Sexual activity"),
        category("bleedingDuringPregnancy", .cycleTracking, .count, "Bleeding during pregnancy"),
        category("bleedingAfterPregnancy", .cycleTracking, .count, "Bleeding after pregnancy"),
    ]

    private static let hearingTypes: [HealthTypeInfo] = [
        quantity("environmentalAudioExposure", .hearing, "dBASPL", .average, "Environmental sound levels"),
        quantity("headphoneAudioExposure", .hearing, "dBASPL", .average, "Headphone audio levels"),
        quantity("environmentalSoundReduction", .hearing, "dBASPL", .average, "Environmental sound reduction"),
        category("environmentalAudioExposureEvent", .hearing, .count, "Environmental sound level notifications"),
        category("headphoneAudioExposureEvent", .hearing, .count, "Headphone audio level notifications"),
        other("audiogram", .hearing, .audiogram, .count, "Audiograms"),
    ]

    private static let heartTypes: [HealthTypeInfo] = [
        quantity("heartRate", .heart, "count/min", .average, "Heart rate"),
        quantity("restingHeartRate", .heart, "count/min", .average, "Resting heart rate"),
        quantity("walkingHeartRateAverage", .heart, "count/min", .average, "Walking heart rate average"),
        quantity("heartRateVariabilitySDNN", .heart, "ms", .average, "Heart rate variability"),
        quantity("heartRateRecoveryOneMinute", .heart, "count/min", .average, "Cardio recovery"),
        quantity("vo2Max", .heart, "ml/(kg*min)", .average, "Cardio fitness"),
        quantity("atrialFibrillationBurden", .heart, "%", .average, "Atrial fibrillation burden"),
        quantity("peripheralPerfusionIndex", .heart, "%", .average, "Peripheral perfusion index"),
        category("highHeartRateEvent", .heart, .count, "High heart rate notifications"),
        category("lowHeartRateEvent", .heart, .count, "Low heart rate notifications"),
        category("irregularHeartRhythmEvent", .heart, .count, "Irregular rhythm notifications"),
        category("lowCardioFitnessEvent", .heart, .count, "Low cardio fitness notifications"),
        other("electrocardiogram", .heart, .electrocardiogram, .count, "Electrocardiograms"),
    ]

    private static let mentalWellbeingTypes: [HealthTypeInfo] = [
        category("mindfulSession", .mentalWellbeing, .duration, "Mindful minutes"),
        other("stateOfMind", .mentalWellbeing, .stateOfMind, .count, "State of mind"),
    ]

    private static let mobilityTypes: [HealthTypeInfo] = [
        quantity("walkingSpeed", .mobility, "m/s", .average, "Walking speed"),
        quantity("walkingStepLength", .mobility, "m", .average, "Walking step length"),
        quantity("walkingAsymmetryPercentage", .mobility, "%", .average, "Walking asymmetry"),
        quantity("walkingDoubleSupportPercentage", .mobility, "%", .average, "Walking double support"),
        quantity("appleWalkingSteadiness", .mobility, "%", .average, "Walking steadiness"),
        quantity("sixMinuteWalkTestDistance", .mobility, "m", .average, "Six-minute walk distance"),
        quantity("stairAscentSpeed", .mobility, "m/s", .average, "Stair ascent speed"),
        quantity("stairDescentSpeed", .mobility, "m/s", .average, "Stair descent speed"),
        quantity("numberOfTimesFallen", .mobility, "count", .sum, "Falls"),
        category("appleWalkingSteadinessEvent", .mobility, .count, "Walking steadiness notifications"),
    ]

    private static let nutritionTypes: [HealthTypeInfo] = [
        quantity("dietaryEnergyConsumed", .nutrition, "kcal", .sum, "Dietary energy"),
        quantity("dietaryCarbohydrates", .nutrition, "g", .sum, "Carbohydrates"),
        quantity("dietaryFiber", .nutrition, "g", .sum, "Fiber"),
        quantity("dietarySugar", .nutrition, "g", .sum, "Sugar"),
        quantity("dietaryFatTotal", .nutrition, "g", .sum, "Total fat"),
        quantity("dietaryFatMonounsaturated", .nutrition, "g", .sum, "Monounsaturated fat"),
        quantity("dietaryFatPolyunsaturated", .nutrition, "g", .sum, "Polyunsaturated fat"),
        quantity("dietaryFatSaturated", .nutrition, "g", .sum, "Saturated fat"),
        quantity("dietaryCholesterol", .nutrition, "mg", .sum, "Cholesterol"),
        quantity("dietaryProtein", .nutrition, "g", .sum, "Protein"),
        quantity("dietaryVitaminA", .nutrition, "mcg", .sum, "Vitamin A"),
        quantity("dietaryThiamin", .nutrition, "mg", .sum, "Thiamin (B1)"),
        quantity("dietaryRiboflavin", .nutrition, "mg", .sum, "Riboflavin (B2)"),
        quantity("dietaryNiacin", .nutrition, "mg", .sum, "Niacin (B3)"),
        quantity("dietaryPantothenicAcid", .nutrition, "mg", .sum, "Pantothenic acid (B5)"),
        quantity("dietaryVitaminB6", .nutrition, "mg", .sum, "Vitamin B6"),
        quantity("dietaryBiotin", .nutrition, "mcg", .sum, "Biotin (B7)"),
        quantity("dietaryVitaminB12", .nutrition, "mcg", .sum, "Vitamin B12"),
        quantity("dietaryVitaminC", .nutrition, "mg", .sum, "Vitamin C"),
        quantity("dietaryVitaminD", .nutrition, "mcg", .sum, "Vitamin D"),
        quantity("dietaryVitaminE", .nutrition, "mg", .sum, "Vitamin E"),
        quantity("dietaryVitaminK", .nutrition, "mcg", .sum, "Vitamin K"),
        quantity("dietaryFolate", .nutrition, "mcg", .sum, "Folate"),
        quantity("dietaryCalcium", .nutrition, "mg", .sum, "Calcium"),
        quantity("dietaryChloride", .nutrition, "mg", .sum, "Chloride"),
        quantity("dietaryIron", .nutrition, "mg", .sum, "Iron"),
        quantity("dietaryMagnesium", .nutrition, "mg", .sum, "Magnesium"),
        quantity("dietaryPhosphorus", .nutrition, "mg", .sum, "Phosphorus"),
        quantity("dietaryPotassium", .nutrition, "mg", .sum, "Potassium"),
        quantity("dietarySodium", .nutrition, "mg", .sum, "Sodium"),
        quantity("dietaryZinc", .nutrition, "mg", .sum, "Zinc"),
        quantity("dietaryChromium", .nutrition, "mcg", .sum, "Chromium"),
        quantity("dietaryCopper", .nutrition, "mg", .sum, "Copper"),
        quantity("dietaryIodine", .nutrition, "mcg", .sum, "Iodine"),
        quantity("dietaryManganese", .nutrition, "mg", .sum, "Manganese"),
        quantity("dietaryMolybdenum", .nutrition, "mcg", .sum, "Molybdenum"),
        quantity("dietarySelenium", .nutrition, "mcg", .sum, "Selenium"),
        quantity("dietaryWater", .nutrition, "mL", .sum, "Water"),
        quantity("dietaryCaffeine", .nutrition, "mg", .sum, "Caffeine"),
    ]

    private static let respiratoryTypes: [HealthTypeInfo] = [
        quantity("respiratoryRate", .respiratory, "count/min", .average, "Respiratory rate"),
        quantity("oxygenSaturation", .respiratory, "%", .average, "Blood oxygen"),
        quantity("forcedVitalCapacity", .respiratory, "L", .average, "Forced vital capacity"),
        quantity("forcedExpiratoryVolume1", .respiratory, "L", .average, "Forced expiratory volume"),
        quantity("peakExpiratoryFlowRate", .respiratory, "L/min", .average, "Peak expiratory flow rate"),
        quantity("inhalerUsage", .respiratory, "count", .sum, "Inhaler usage"),
    ]

    private static let sleepTypes: [HealthTypeInfo] = [
        category("sleepAnalysis", .sleep, .duration, "Sleep"),
        category("sleepApneaEvent", .sleep, .count, "Sleep apnea notifications"),
        quantity("appleSleepingBreathingDisturbances", .sleep, "count", .average, "Breathing disturbances"),
    ]

    private static let symptomTypes: [HealthTypeInfo] = [
        category("abdominalCramps", .symptoms, .count, "Abdominal cramps"),
        category("acne", .symptoms, .count, "Acne"),
        category("appetiteChanges", .symptoms, .count, "Appetite changes"),
        category("bladderIncontinence", .symptoms, .count, "Bladder incontinence"),
        category("bloating", .symptoms, .count, "Bloating"),
        category("breastPain", .symptoms, .count, "Breast pain"),
        category("chestTightnessOrPain", .symptoms, .count, "Chest tightness or pain"),
        category("chills", .symptoms, .count, "Chills"),
        category("constipation", .symptoms, .count, "Constipation"),
        category("coughing", .symptoms, .count, "Coughing"),
        category("diarrhea", .symptoms, .count, "Diarrhea"),
        category("dizziness", .symptoms, .count, "Dizziness"),
        category("drySkin", .symptoms, .count, "Dry skin"),
        category("fainting", .symptoms, .count, "Fainting"),
        category("fatigue", .symptoms, .count, "Fatigue"),
        category("fever", .symptoms, .count, "Fever"),
        category("generalizedBodyAche", .symptoms, .count, "Body and muscle ache"),
        category("hairLoss", .symptoms, .count, "Hair loss"),
        category("headache", .symptoms, .count, "Headache"),
        category("heartburn", .symptoms, .count, "Heartburn"),
        category("hotFlashes", .symptoms, .count, "Hot flashes"),
        category("lossOfSmell", .symptoms, .count, "Loss of smell"),
        category("lossOfTaste", .symptoms, .count, "Loss of taste"),
        category("lowerBackPain", .symptoms, .count, "Lower back pain"),
        category("memoryLapse", .symptoms, .count, "Memory lapse"),
        category("moodChanges", .symptoms, .count, "Mood changes"),
        category("nausea", .symptoms, .count, "Nausea"),
        category("nightSweats", .symptoms, .count, "Night sweats"),
        category("pelvicPain", .symptoms, .count, "Pelvic pain"),
        category("rapidPoundingOrFlutteringHeartbeat", .symptoms, .count, "Rapid, pounding, or fluttering heartbeat"),
        category("runnyNose", .symptoms, .count, "Runny nose"),
        category("shortnessOfBreath", .symptoms, .count, "Shortness of breath"),
        category("sinusCongestion", .symptoms, .count, "Sinus congestion"),
        category("skippedHeartbeat", .symptoms, .count, "Skipped heartbeat"),
        category("sleepChanges", .symptoms, .count, "Sleep changes"),
        category("soreThroat", .symptoms, .count, "Sore throat"),
        category("vaginalDryness", .symptoms, .count, "Vaginal dryness"),
        category("vomiting", .symptoms, .count, "Vomiting"),
        category("wheezing", .symptoms, .count, "Wheezing"),
    ]

    private static let vitalsTypes: [HealthTypeInfo] = [
        quantity("bloodPressureSystolic", .vitals, "mmHg", .average, "Systolic blood pressure"),
        quantity("bloodPressureDiastolic", .vitals, "mmHg", .average, "Diastolic blood pressure"),
        quantity("bloodGlucose", .vitals, "mg/dL", .average, "Blood glucose"),
        quantity("bodyTemperature", .vitals, "degC", .average, "Body temperature"),
        quantity("appleSleepingWristTemperature", .vitals, "degC", .average, "Sleeping wrist temperature"),
        quantity("insulinDelivery", .vitals, "IU", .sum, "Insulin delivery"),
    ]

    private static let otherTypes: [HealthTypeInfo] = [
        quantity("uvExposure", .other, "count", .average, "UV index"),
        quantity("timeInDaylight", .other, "min", .sum, "Time in daylight"),
        quantity("electrodermalActivity", .other, "S", .average, "Electrodermal activity"),
        quantity("bloodAlcoholContent", .other, "%", .average, "Blood alcohol content"),
        quantity("numberOfAlcoholicBeverages", .other, "count", .sum, "Alcoholic beverages"),
        category("handwashingEvent", .other, .count, "Handwashing"),
        category("toothbrushingEvent", .other, .count, "Toothbrushing"),
        other("dateOfBirth", .other, .characteristic, nil, "Date of birth"),
        other("biologicalSex", .other, .characteristic, nil, "Biological sex"),
        other("bloodType", .other, .characteristic, nil, "Blood type"),
        other("fitzpatrickSkinType", .other, .characteristic, nil, "Skin type"),
        other("wheelchairUse", .other, .characteristic, nil, "Wheelchair use"),
        other("activityMoveMode", .other, .characteristic, nil, "Activity move mode"),
    ]
}

/// HealthSleepStage is a value of a sleep sample, by HealthKit's raw integer
/// (HKCategoryValueSleepAnalysis).
public enum HealthSleepStage: Int, CaseIterable, Codable, Hashable, Sendable {
    case inBed = 0
    case asleepUnspecified = 1
    case awake = 2
    case asleepCore = 3
    case asleepDeep = 4
    case asleepREM = 5

    /// The key under which the stage's seconds are sent.
    public var stageName: String {
        switch self {
        case .inBed: "inBed"
        case .asleepUnspecified: "asleepUnspecified"
        case .awake: "awake"
        case .asleepCore: "asleepCore"
        case .asleepDeep: "asleepDeep"
        case .asleepREM: "asleepREM"
        }
    }

    /// Whether the person was asleep, not merely in bed or awake in it.
    public var isAsleep: Bool {
        switch self {
        case .asleepUnspecified, .asleepCore, .asleepDeep, .asleepREM: true
        case .inBed, .awake: false
        }
    }

    /// The key under which the seconds asleep in any stage, sources
    /// overlapping counted once, are sent.
    public static let asleepKey = "asleep"
}
