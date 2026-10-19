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
        case .other: "Other Data"
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
    /// the identifier read as words ("someNewType" is "Some New Type").
    public static func typeName(for healthType: String) -> String { byHealthType[healthType]?.typeName ?? words(from: healthType) }

    public static func types(in healthCategory: HealthCategory) -> [HealthTypeInfo] { all.filter { $0.healthCategory == healthCategory } }

    public static func types(of valueKind: HealthValueKind) -> [HealthTypeInfo] { all.filter { $0.valueKind == valueKind } }

    /// The identifier read as English words: a space before each capital that
    /// begins a word, runs of capitals kept together ("SDNN").
    static func words(from identifier: String) -> String {
        let characters = Array(identifier)
        var result = ""
        for (index, character) in characters.enumerated() {
            if index > 0, character.isUppercase {
                let previous = characters[index - 1]
                let next = index + 1 < characters.count ? characters[index + 1] : nil
                if previous.isLowercase || previous.isNumber || (previous.isUppercase && (next?.isLowercase ?? false)) { result.append(" ") }
            }
            result.append(index == 0 ? Character(character.uppercased()) : character)
        }
        return result
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
        quantity("distanceWalkingRunning", .activity, "m", .sum, "Walking + Running Distance"),
        quantity("distanceCycling", .activity, "m", .sum, "Cycling Distance"),
        quantity("distanceWheelchair", .activity, "m", .sum, "Wheelchair Distance"),
        quantity("distanceSwimming", .activity, "m", .sum, "Swimming Distance"),
        quantity("distanceDownhillSnowSports", .activity, "m", .sum, "Downhill Snow Sports Distance"),
        quantity("distanceCrossCountrySkiing", .activity, "m", .sum, "Cross-Country Skiing Distance"),
        quantity("distancePaddleSports", .activity, "m", .sum, "Paddle Sports Distance"),
        quantity("distanceRowing", .activity, "m", .sum, "Rowing Distance"),
        quantity("distanceSkatingSports", .activity, "m", .sum, "Skating Sports Distance"),
        quantity("basalEnergyBurned", .activity, "kcal", .sum, "Resting Energy"),
        quantity("activeEnergyBurned", .activity, "kcal", .sum, "Active Energy"),
        quantity("flightsClimbed", .activity, "count", .sum, "Flights Climbed"),
        quantity("nikeFuel", .activity, "count", .sum, "NikeFuel"),
        quantity("appleExerciseTime", .activity, "min", .sum, "Exercise Minutes"),
        quantity("appleMoveTime", .activity, "min", .sum, "Move Minutes"),
        quantity("appleStandTime", .activity, "min", .sum, "Stand Minutes"),
        quantity("pushCount", .activity, "count", .sum, "Wheelchair Pushes"),
        quantity("swimmingStrokeCount", .activity, "count", .sum, "Swimming Strokes"),
        quantity("cyclingSpeed", .activity, "m/s", .average, "Cycling Speed"),
        quantity("cyclingPower", .activity, "W", .average, "Cycling Power"),
        quantity("cyclingCadence", .activity, "count/min", .average, "Cycling Cadence"),
        quantity("cyclingFunctionalThresholdPower", .activity, "W", .average, "Cycling Functional Threshold Power"),
        quantity("runningSpeed", .activity, "m/s", .average, "Running Speed"),
        quantity("runningPower", .activity, "W", .average, "Running Power"),
        quantity("runningStrideLength", .activity, "m", .average, "Running Stride Length"),
        quantity("runningVerticalOscillation", .activity, "cm", .average, "Running Vertical Oscillation"),
        quantity("runningGroundContactTime", .activity, "ms", .average, "Running Ground Contact Time"),
        quantity("crossCountrySkiingSpeed", .activity, "m/s", .average, "Cross-Country Skiing Speed"),
        quantity("paddleSportsSpeed", .activity, "m/s", .average, "Paddle Sports Speed"),
        quantity("rowingSpeed", .activity, "m/s", .average, "Rowing Speed"),
        quantity("physicalEffort", .activity, "kcal/(kg*hr)", .average, "Physical Effort"),
        quantity("underwaterDepth", .activity, "m", .average, "Underwater Depth"),
        quantity("waterTemperature", .activity, "degC", .average, "Water Temperature"),
        category("appleStandHour", .activity, .count, "Stand Hours"),
        other("workout", .activity, .workout, .duration, "Workouts"),
    ]

    private static let bodyMeasurementTypes: [HealthTypeInfo] = [
        quantity("bodyMass", .bodyMeasurements, "kg", .average, "Weight"),
        quantity("bodyMassIndex", .bodyMeasurements, "count", .average, "Body Mass Index"),
        quantity("bodyFatPercentage", .bodyMeasurements, "%", .average, "Body Fat Percentage"),
        quantity("leanBodyMass", .bodyMeasurements, "kg", .average, "Lean Body Mass"),
        quantity("height", .bodyMeasurements, "m", .average, "Height"),
        quantity("waistCircumference", .bodyMeasurements, "m", .average, "Waist Circumference"),
    ]

    private static let cycleTrackingTypes: [HealthTypeInfo] = [
        quantity("basalBodyTemperature", .cycleTracking, "degC", .average, "Basal Body Temperature"),
        category("menstrualFlow", .cycleTracking, .count, "Menstrual Flow"),
        category("intermenstrualBleeding", .cycleTracking, .count, "Spotting"),
        category("infrequentMenstrualCycles", .cycleTracking, .count, "Infrequent Periods"),
        category("irregularMenstrualCycles", .cycleTracking, .count, "Irregular Periods"),
        category("persistentIntermenstrualBleeding", .cycleTracking, .count, "Persistent Spotting"),
        category("prolongedMenstrualPeriods", .cycleTracking, .count, "Prolonged Periods"),
        category("cervicalMucusQuality", .cycleTracking, .count, "Cervical Mucus Quality"),
        category("ovulationTestResult", .cycleTracking, .count, "Ovulation Test Result"),
        category("pregnancyTestResult", .cycleTracking, .count, "Pregnancy Test Result"),
        category("progesteroneTestResult", .cycleTracking, .count, "Progesterone Test Result"),
        category("pregnancy", .cycleTracking, .duration, "Pregnancy"),
        category("lactation", .cycleTracking, .duration, "Lactation"),
        category("contraceptive", .cycleTracking, .duration, "Contraceptive"),
        category("sexualActivity", .cycleTracking, .count, "Sexual Activity"),
        category("bleedingDuringPregnancy", .cycleTracking, .count, "Bleeding During Pregnancy"),
        category("bleedingAfterPregnancy", .cycleTracking, .count, "Bleeding After Pregnancy"),
    ]

    private static let hearingTypes: [HealthTypeInfo] = [
        quantity("environmentalAudioExposure", .hearing, "dBASPL", .average, "Environmental Sound Levels"),
        quantity("headphoneAudioExposure", .hearing, "dBASPL", .average, "Headphone Audio Levels"),
        quantity("environmentalSoundReduction", .hearing, "dBASPL", .average, "Environmental Sound Reduction"),
        category("environmentalAudioExposureEvent", .hearing, .count, "Environmental Sound Level Notifications"),
        category("headphoneAudioExposureEvent", .hearing, .count, "Headphone Audio Level Notifications"),
        other("audiogram", .hearing, .audiogram, .count, "Audiograms"),
    ]

    private static let heartTypes: [HealthTypeInfo] = [
        quantity("heartRate", .heart, "count/min", .average, "Heart Rate"),
        quantity("restingHeartRate", .heart, "count/min", .average, "Resting Heart Rate"),
        quantity("walkingHeartRateAverage", .heart, "count/min", .average, "Walking Heart Rate Average"),
        quantity("heartRateVariabilitySDNN", .heart, "ms", .average, "Heart Rate Variability"),
        quantity("heartRateRecoveryOneMinute", .heart, "count/min", .average, "Cardio Recovery"),
        quantity("vo2Max", .heart, "ml/(kg*min)", .average, "Cardio Fitness"),
        quantity("atrialFibrillationBurden", .heart, "%", .average, "Atrial Fibrillation Burden"),
        quantity("peripheralPerfusionIndex", .heart, "%", .average, "Peripheral Perfusion Index"),
        category("highHeartRateEvent", .heart, .count, "High Heart Rate Notifications"),
        category("lowHeartRateEvent", .heart, .count, "Low Heart Rate Notifications"),
        category("irregularHeartRhythmEvent", .heart, .count, "Irregular Rhythm Notifications"),
        category("lowCardioFitnessEvent", .heart, .count, "Low Cardio Fitness Notifications"),
        other("electrocardiogram", .heart, .electrocardiogram, .count, "Electrocardiograms"),
    ]

    private static let mentalWellbeingTypes: [HealthTypeInfo] = [
        category("mindfulSession", .mentalWellbeing, .duration, "Mindful Minutes"),
        other("stateOfMind", .mentalWellbeing, .stateOfMind, .count, "State of Mind"),
    ]

    private static let mobilityTypes: [HealthTypeInfo] = [
        quantity("walkingSpeed", .mobility, "m/s", .average, "Walking Speed"),
        quantity("walkingStepLength", .mobility, "m", .average, "Walking Step Length"),
        quantity("walkingAsymmetryPercentage", .mobility, "%", .average, "Walking Asymmetry"),
        quantity("walkingDoubleSupportPercentage", .mobility, "%", .average, "Walking Double Support"),
        quantity("appleWalkingSteadiness", .mobility, "%", .average, "Walking Steadiness"),
        quantity("sixMinuteWalkTestDistance", .mobility, "m", .average, "Six-Minute Walk Distance"),
        quantity("stairAscentSpeed", .mobility, "m/s", .average, "Stair Ascent Speed"),
        quantity("stairDescentSpeed", .mobility, "m/s", .average, "Stair Descent Speed"),
        quantity("numberOfTimesFallen", .mobility, "count", .sum, "Falls"),
        category("appleWalkingSteadinessEvent", .mobility, .count, "Walking Steadiness Notifications"),
    ]

    private static let nutritionTypes: [HealthTypeInfo] = [
        quantity("dietaryEnergyConsumed", .nutrition, "kcal", .sum, "Dietary Energy"),
        quantity("dietaryCarbohydrates", .nutrition, "g", .sum, "Carbohydrates"),
        quantity("dietaryFiber", .nutrition, "g", .sum, "Fiber"),
        quantity("dietarySugar", .nutrition, "g", .sum, "Sugar"),
        quantity("dietaryFatTotal", .nutrition, "g", .sum, "Total Fat"),
        quantity("dietaryFatMonounsaturated", .nutrition, "g", .sum, "Monounsaturated Fat"),
        quantity("dietaryFatPolyunsaturated", .nutrition, "g", .sum, "Polyunsaturated Fat"),
        quantity("dietaryFatSaturated", .nutrition, "g", .sum, "Saturated Fat"),
        quantity("dietaryCholesterol", .nutrition, "mg", .sum, "Cholesterol"),
        quantity("dietaryProtein", .nutrition, "g", .sum, "Protein"),
        quantity("dietaryVitaminA", .nutrition, "mcg", .sum, "Vitamin A"),
        quantity("dietaryThiamin", .nutrition, "mg", .sum, "Thiamin (B1)"),
        quantity("dietaryRiboflavin", .nutrition, "mg", .sum, "Riboflavin (B2)"),
        quantity("dietaryNiacin", .nutrition, "mg", .sum, "Niacin (B3)"),
        quantity("dietaryPantothenicAcid", .nutrition, "mg", .sum, "Pantothenic Acid (B5)"),
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
        quantity("respiratoryRate", .respiratory, "count/min", .average, "Respiratory Rate"),
        quantity("oxygenSaturation", .respiratory, "%", .average, "Blood Oxygen"),
        quantity("forcedVitalCapacity", .respiratory, "L", .average, "Forced Vital Capacity"),
        quantity("forcedExpiratoryVolume1", .respiratory, "L", .average, "Forced Expiratory Volume"),
        quantity("peakExpiratoryFlowRate", .respiratory, "L/min", .average, "Peak Expiratory Flow Rate"),
        quantity("inhalerUsage", .respiratory, "count", .sum, "Inhaler Usage"),
    ]

    private static let sleepTypes: [HealthTypeInfo] = [
        category("sleepAnalysis", .sleep, .duration, "Sleep"),
        category("sleepApneaEvent", .sleep, .count, "Sleep Apnea Notifications"),
        quantity("appleSleepingBreathingDisturbances", .sleep, "count", .average, "Breathing Disturbances"),
    ]

    private static let symptomTypes: [HealthTypeInfo] = [
        category("abdominalCramps", .symptoms, .count, "Abdominal Cramps"),
        category("acne", .symptoms, .count, "Acne"),
        category("appetiteChanges", .symptoms, .count, "Appetite Changes"),
        category("bladderIncontinence", .symptoms, .count, "Bladder Incontinence"),
        category("bloating", .symptoms, .count, "Bloating"),
        category("breastPain", .symptoms, .count, "Breast Pain"),
        category("chestTightnessOrPain", .symptoms, .count, "Chest Tightness or Pain"),
        category("chills", .symptoms, .count, "Chills"),
        category("constipation", .symptoms, .count, "Constipation"),
        category("coughing", .symptoms, .count, "Coughing"),
        category("diarrhea", .symptoms, .count, "Diarrhea"),
        category("dizziness", .symptoms, .count, "Dizziness"),
        category("drySkin", .symptoms, .count, "Dry Skin"),
        category("fainting", .symptoms, .count, "Fainting"),
        category("fatigue", .symptoms, .count, "Fatigue"),
        category("fever", .symptoms, .count, "Fever"),
        category("generalizedBodyAche", .symptoms, .count, "Body and Muscle Ache"),
        category("hairLoss", .symptoms, .count, "Hair Loss"),
        category("headache", .symptoms, .count, "Headache"),
        category("heartburn", .symptoms, .count, "Heartburn"),
        category("hotFlashes", .symptoms, .count, "Hot Flashes"),
        category("lossOfSmell", .symptoms, .count, "Loss of Smell"),
        category("lossOfTaste", .symptoms, .count, "Loss of Taste"),
        category("lowerBackPain", .symptoms, .count, "Lower Back Pain"),
        category("memoryLapse", .symptoms, .count, "Memory Lapse"),
        category("moodChanges", .symptoms, .count, "Mood Changes"),
        category("nausea", .symptoms, .count, "Nausea"),
        category("nightSweats", .symptoms, .count, "Night Sweats"),
        category("pelvicPain", .symptoms, .count, "Pelvic Pain"),
        category("rapidPoundingOrFlutteringHeartbeat", .symptoms, .count, "Rapid, Pounding, or Fluttering Heartbeat"),
        category("runnyNose", .symptoms, .count, "Runny Nose"),
        category("shortnessOfBreath", .symptoms, .count, "Shortness of Breath"),
        category("sinusCongestion", .symptoms, .count, "Sinus Congestion"),
        category("skippedHeartbeat", .symptoms, .count, "Skipped Heartbeat"),
        category("sleepChanges", .symptoms, .count, "Sleep Changes"),
        category("soreThroat", .symptoms, .count, "Sore Throat"),
        category("vaginalDryness", .symptoms, .count, "Vaginal Dryness"),
        category("vomiting", .symptoms, .count, "Vomiting"),
        category("wheezing", .symptoms, .count, "Wheezing"),
    ]

    private static let vitalsTypes: [HealthTypeInfo] = [
        quantity("bloodPressureSystolic", .vitals, "mmHg", .average, "Systolic Blood Pressure"),
        quantity("bloodPressureDiastolic", .vitals, "mmHg", .average, "Diastolic Blood Pressure"),
        quantity("bloodGlucose", .vitals, "mg/dL", .average, "Blood Glucose"),
        quantity("bodyTemperature", .vitals, "degC", .average, "Body Temperature"),
        quantity("appleSleepingWristTemperature", .vitals, "degC", .average, "Sleeping Wrist Temperature"),
        quantity("insulinDelivery", .vitals, "IU", .sum, "Insulin Delivery"),
    ]

    private static let otherTypes: [HealthTypeInfo] = [
        quantity("uvExposure", .other, "count", .average, "UV Index"),
        quantity("timeInDaylight", .other, "min", .sum, "Time in Daylight"),
        quantity("electrodermalActivity", .other, "S", .average, "Electrodermal Activity"),
        quantity("bloodAlcoholContent", .other, "%", .average, "Blood Alcohol Content"),
        quantity("numberOfAlcoholicBeverages", .other, "count", .sum, "Alcoholic Beverages"),
        category("handwashingEvent", .other, .count, "Handwashing"),
        category("toothbrushingEvent", .other, .count, "Toothbrushing"),
        other("dateOfBirth", .other, .characteristic, nil, "Date of Birth"),
        other("biologicalSex", .other, .characteristic, nil, "Biological Sex"),
        other("bloodType", .other, .characteristic, nil, "Blood Type"),
        other("fitzpatrickSkinType", .other, .characteristic, nil, "Skin Type"),
        other("wheelchairUse", .other, .characteristic, nil, "Wheelchair Use"),
        other("activityMoveMode", .other, .characteristic, nil, "Activity Move Mode"),
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
