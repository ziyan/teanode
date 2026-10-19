import Foundation
import TeaNodeKit

// The words the Health screens say for Health's own things, in the person's
// language: the Health app's groups, the kinds of data the dashboard names, workouts
// and the stages of sleep. Each is written under a key of its own because
// "Health" and "Activity" are already words of the app with another meaning
// (the spending category, the agent's activity). Anything not listed falls
// back to the English name the server sends, the same as the dashboard.

enum HealthWords {
    /// The feature's name: the side menu, the screen and the setting.
    static var title: String { String(localized: "health-title", defaultValue: "Health") }

    /// The choice of day, week or month a chart is drawn by. Its own key: the
    /// word is read in Chinese as a cycle elsewhere in the app, and here as the
    /// unit of the chart, as the dashboard writes it.
    static var periodTitle: String { String(localized: "health-period", defaultValue: "Period") }

    /// The range of a year back. Its own key, as the dashboard writes it.
    static var yearRangeTitle: String { String(localized: "health-range-year", defaultValue: "Year") }

    /// The total of a chart. Its own key, as the dashboard writes it.
    static var totalTitle: String { String(localized: "health-total", defaultValue: "Total") }

    /// What the settings and the empty screen say when the server told of an
    /// erase the phone had not heard of, so Health is off.
    static var erasedOnServerText: String {
        String(
            localized:
                "Health data was erased on your server, so TeaNode stopped copying it. Turn Health on again to copy it again.")
    }

    /// A Health category as the Health app names it.
    static func categoryName(_ healthCategory: HealthCategory) -> String {
        switch healthCategory {
        case .activity: String(localized: "health-category-activity", defaultValue: "Activity")
        case .bodyMeasurements: String(localized: "health-category-bodyMeasurements", defaultValue: "Body Measurements")
        case .cycleTracking: String(localized: "health-category-cycleTracking", defaultValue: "Cycle Tracking")
        case .hearing: String(localized: "health-category-hearing", defaultValue: "Hearing")
        case .heart: String(localized: "health-category-heart", defaultValue: "Heart")
        case .mentalWellbeing: String(localized: "health-category-mentalWellbeing", defaultValue: "Mental Wellbeing")
        case .mobility: String(localized: "health-category-mobility", defaultValue: "Mobility")
        case .nutrition: String(localized: "health-category-nutrition", defaultValue: "Nutrition")
        case .respiratory: String(localized: "health-category-respiratory", defaultValue: "Respiratory")
        case .sleep: String(localized: "health-category-sleep", defaultValue: "Sleep")
        case .symptoms: String(localized: "health-category-symptoms", defaultValue: "Symptoms")
        case .vitals: String(localized: "health-category-vitals", defaultValue: "Vitals")
        case .other: String(localized: "health-category-other", defaultValue: "Other")
        }
    }

    /// A kind of data in the person's language, else the server's English
    /// name, else the identifier read as words.
    static func typeName(healthType: String, serverName: String) -> String {
        if let translatedName = translatedTypeName(healthType) { return translatedName }
        if !serverName.isEmpty { return serverName }
        return HealthTypes.typeName(for: healthType)
    }

    /// The kinds of data the dashboard names, in the same words as it does.
    private static func translatedTypeName(_ healthType: String) -> String? {
        switch healthType {
        case "stepCount": return String(localized: "health-type-stepCount", defaultValue: "Steps")
        case "distanceWalkingRunning": return String(localized: "health-type-distanceWalkingRunning", defaultValue: "Walking + running distance")
        case "distanceCycling": return String(localized: "health-type-distanceCycling", defaultValue: "Cycling distance")
        case "distanceSwimming": return String(localized: "health-type-distanceSwimming", defaultValue: "Swimming distance")
        case "basalEnergyBurned": return String(localized: "health-type-basalEnergyBurned", defaultValue: "Resting energy")
        case "activeEnergyBurned": return String(localized: "health-type-activeEnergyBurned", defaultValue: "Active energy")
        case "flightsClimbed": return String(localized: "health-type-flightsClimbed", defaultValue: "Flights climbed")
        case "appleExerciseTime": return String(localized: "health-type-appleExerciseTime", defaultValue: "Exercise minutes")
        case "appleMoveTime": return String(localized: "health-type-appleMoveTime", defaultValue: "Move minutes")
        case "appleStandTime": return String(localized: "health-type-appleStandTime", defaultValue: "Stand minutes")
        case "appleStandHour": return String(localized: "health-type-appleStandHour", defaultValue: "Stand hours")
        case "workout": return String(localized: "health-type-workout", defaultValue: "Workouts")
        case "heartRate": return String(localized: "health-type-heartRate", defaultValue: "Heart rate")
        case "restingHeartRate": return String(localized: "health-type-restingHeartRate", defaultValue: "Resting heart rate")
        case "walkingHeartRateAverage": return String(localized: "health-type-walkingHeartRateAverage", defaultValue: "Walking heart rate average")
        case "heartRateVariabilitySDNN": return String(localized: "health-type-heartRateVariabilitySDNN", defaultValue: "Heart rate variability")
        case "heartRateRecoveryOneMinute": return String(localized: "health-type-heartRateRecoveryOneMinute", defaultValue: "Cardio recovery")
        case "vo2Max": return String(localized: "health-type-vo2Max", defaultValue: "Cardio fitness")
        case "electrocardiogram": return String(localized: "health-type-electrocardiogram", defaultValue: "Electrocardiograms")
        case "bodyMass": return String(localized: "health-type-bodyMass", defaultValue: "Weight")
        case "bodyMassIndex": return String(localized: "health-type-bodyMassIndex", defaultValue: "Body mass index")
        case "bodyFatPercentage": return String(localized: "health-type-bodyFatPercentage", defaultValue: "Body fat percentage")
        case "leanBodyMass": return String(localized: "health-type-leanBodyMass", defaultValue: "Lean body mass")
        case "height": return String(localized: "health-type-height", defaultValue: "Height")
        case "waistCircumference": return String(localized: "health-type-waistCircumference", defaultValue: "Waist circumference")
        case "sleepAnalysis": return String(localized: "health-type-sleepAnalysis", defaultValue: "Sleep")
        case "mindfulSession": return String(localized: "health-type-mindfulSession", defaultValue: "Mindful minutes")
        case "stateOfMind": return String(localized: "health-type-stateOfMind", defaultValue: "State of mind")
        case "respiratoryRate": return String(localized: "health-type-respiratoryRate", defaultValue: "Respiratory rate")
        case "oxygenSaturation": return String(localized: "health-type-oxygenSaturation", defaultValue: "Blood oxygen")
        case "bodyTemperature": return String(localized: "health-type-bodyTemperature", defaultValue: "Body temperature")
        case "bloodPressureSystolic": return String(localized: "health-type-bloodPressureSystolic", defaultValue: "Systolic blood pressure")
        case "bloodPressureDiastolic": return String(localized: "health-type-bloodPressureDiastolic", defaultValue: "Diastolic blood pressure")
        case "bloodGlucose": return String(localized: "health-type-bloodGlucose", defaultValue: "Blood glucose")
        case "appleSleepingWristTemperature": return String(localized: "health-type-appleSleepingWristTemperature", defaultValue: "Sleeping wrist temperature")
        case "walkingSpeed": return String(localized: "health-type-walkingSpeed", defaultValue: "Walking speed")
        case "walkingStepLength": return String(localized: "health-type-walkingStepLength", defaultValue: "Walking step length")
        case "walkingAsymmetryPercentage": return String(localized: "health-type-walkingAsymmetryPercentage", defaultValue: "Walking asymmetry")
        case "walkingDoubleSupportPercentage": return String(localized: "health-type-walkingDoubleSupportPercentage", defaultValue: "Walking double support")
        case "appleWalkingSteadiness": return String(localized: "health-type-appleWalkingSteadiness", defaultValue: "Walking steadiness")
        case "stairAscentSpeed": return String(localized: "health-type-stairAscentSpeed", defaultValue: "Stair ascent speed")
        case "stairDescentSpeed": return String(localized: "health-type-stairDescentSpeed", defaultValue: "Stair descent speed")
        case "sixMinuteWalkTestDistance": return String(localized: "health-type-sixMinuteWalkTestDistance", defaultValue: "Six-minute walk distance")
        case "environmentalAudioExposure": return String(localized: "health-type-environmentalAudioExposure", defaultValue: "Environmental sound levels")
        case "headphoneAudioExposure": return String(localized: "health-type-headphoneAudioExposure", defaultValue: "Headphone audio levels")
        case "dietaryEnergyConsumed": return String(localized: "health-type-dietaryEnergyConsumed", defaultValue: "Dietary energy")
        case "dietaryProtein": return String(localized: "health-type-dietaryProtein", defaultValue: "Protein")
        case "dietaryCarbohydrates": return String(localized: "health-type-dietaryCarbohydrates", defaultValue: "Carbohydrates")
        case "dietaryFatTotal": return String(localized: "health-type-dietaryFatTotal", defaultValue: "Total fat")
        case "dietarySugar": return String(localized: "health-type-dietarySugar", defaultValue: "Sugar")
        case "dietaryFiber": return String(localized: "health-type-dietaryFiber", defaultValue: "Fiber")
        case "dietarySodium": return String(localized: "health-type-dietarySodium", defaultValue: "Sodium")
        case "dietaryCaffeine": return String(localized: "health-type-dietaryCaffeine", defaultValue: "Caffeine")
        case "dietaryWater": return String(localized: "health-type-dietaryWater", defaultValue: "Water")
        case "timeInDaylight": return String(localized: "health-type-timeInDaylight", defaultValue: "Time in daylight")
        case "uvExposure": return String(localized: "health-type-uvExposure", defaultValue: "UV index")
        case "runningSpeed": return String(localized: "health-type-runningSpeed", defaultValue: "Running speed")
        case "numberOfTimesFallen": return String(localized: "health-type-numberOfTimesFallen", defaultValue: "Falls")
        case "handwashingEvent": return String(localized: "health-type-handwashingEvent", defaultValue: "Handwashing")
        case "toothbrushingEvent": return String(localized: "health-type-toothbrushingEvent", defaultValue: "Toothbrushing")
        case "distanceWheelchair": return String(localized: "health-type-distanceWheelchair", defaultValue: "Wheelchair distance")
        case "pushCount": return String(localized: "health-type-pushCount", defaultValue: "Wheelchair pushes")
        case "swimmingStrokeCount": return String(localized: "health-type-swimmingStrokeCount", defaultValue: "Swimming strokes")
        case "physicalEffort": return String(localized: "health-type-physicalEffort", defaultValue: "Physical effort")
        case "cyclingSpeed": return String(localized: "health-type-cyclingSpeed", defaultValue: "Cycling speed")
        case "cyclingPower": return String(localized: "health-type-cyclingPower", defaultValue: "Cycling power")
        case "cyclingCadence": return String(localized: "health-type-cyclingCadence", defaultValue: "Cycling cadence")
        case "runningPower": return String(localized: "health-type-runningPower", defaultValue: "Running power")
        case "runningStrideLength": return String(localized: "health-type-runningStrideLength", defaultValue: "Running stride length")
        case "runningGroundContactTime": return String(localized: "health-type-runningGroundContactTime", defaultValue: "Running ground contact time")
        case "runningVerticalOscillation": return String(localized: "health-type-runningVerticalOscillation", defaultValue: "Running vertical oscillation")
        case "atrialFibrillationBurden": return String(localized: "health-type-atrialFibrillationBurden", defaultValue: "Atrial fibrillation burden")
        case "highHeartRateEvent": return String(localized: "health-type-highHeartRateEvent", defaultValue: "High heart rate notifications")
        case "lowHeartRateEvent": return String(localized: "health-type-lowHeartRateEvent", defaultValue: "Low heart rate notifications")
        case "irregularHeartRhythmEvent": return String(localized: "health-type-irregularHeartRhythmEvent", defaultValue: "Irregular rhythm notifications")
        case "basalBodyTemperature": return String(localized: "health-type-basalBodyTemperature", defaultValue: "Basal body temperature")
        case "bloodAlcoholContent": return String(localized: "health-type-bloodAlcoholContent", defaultValue: "Blood alcohol content")
        case "forcedVitalCapacity": return String(localized: "health-type-forcedVitalCapacity", defaultValue: "Forced vital capacity")
        case "forcedExpiratoryVolume1": return String(localized: "health-type-forcedExpiratoryVolume1", defaultValue: "Forced expiratory volume")
        case "peakExpiratoryFlowRate": return String(localized: "health-type-peakExpiratoryFlowRate", defaultValue: "Peak expiratory flow rate")
        case "inhalerUsage": return String(localized: "health-type-inhalerUsage", defaultValue: "Inhaler usage")
        case "environmentalSoundReduction": return String(localized: "health-type-environmentalSoundReduction", defaultValue: "Environmental sound reduction")
        case "audiogram": return String(localized: "health-type-audiogram", defaultValue: "Audiograms")
        case "dietaryCholesterol": return String(localized: "health-type-dietaryCholesterol", defaultValue: "Cholesterol")
        case "menstrualFlow": return String(localized: "health-type-menstrualFlow", defaultValue: "Menstrual flow")
        case "intermenstrualBleeding": return String(localized: "health-type-intermenstrualBleeding", defaultValue: "Spotting")
        case "ovulationTestResult": return String(localized: "health-type-ovulationTestResult", defaultValue: "Ovulation test result")
        case "sexualActivity": return String(localized: "health-type-sexualActivity", defaultValue: "Sexual activity")
        case "cervicalMucusQuality": return String(localized: "health-type-cervicalMucusQuality", defaultValue: "Cervical mucus quality")
        case "contraceptive": return String(localized: "health-type-contraceptive", defaultValue: "Contraceptive")
        case "insulinDelivery": return String(localized: "health-type-insulinDelivery", defaultValue: "Insulin delivery")
        case "dateOfBirth": return String(localized: "health-type-dateOfBirth", defaultValue: "Date of birth")
        case "biologicalSex": return String(localized: "health-type-biologicalSex", defaultValue: "Biological sex")
        case "bloodType": return String(localized: "health-type-bloodType", defaultValue: "Blood type")
        case "fitzpatrickSkinType": return String(localized: "health-type-fitzpatrickSkinType", defaultValue: "Skin type")
        case "wheelchairUse": return String(localized: "health-type-wheelchairUse", defaultValue: "Wheelchair use")
        case "activityMoveMode": return String(localized: "health-type-activityMoveMode", defaultValue: "Activity move mode")
        default:
            return nil
        }
    }

    /// A fact about the person as the table writes it: a date of birth as
    /// "1990-04-12 (36 years)", a sex, a blood type, wheelchair
    /// use or how Activity is measured in words, a skin type as the numeral
    /// HealthKit sends, and "Not set" for a fact that was never set. Anything
    /// else is written as it was sent.
    static func characteristicText(healthType: String, characteristicValue: String, today: Date = Date()) -> String {
        if characteristicValue == HealthValueWords.notSetValue { return String(localized: "Not set") }
        switch healthType {
        case "dateOfBirth":
            guard let age = HealthValueWords.ageInYears(dateOfBirth: characteristicValue, today: today, calendar: Calendar.current) else {
                return characteristicValue
            }
            let ageText = age == 1 ? String(localized: "health-age-single", defaultValue: "1 year") : String(localized: "health-age-plural", defaultValue: "\(age) years")
            // The date as the table writes it, the same on every surface.
            return characteristicValue + " (" + ageText + ")"
        case "biologicalSex":
            switch characteristicValue {
            case "female": return String(localized: "Female")
            case "male": return String(localized: "Male")
            case "other": return String(localized: "Other")
            default: return characteristicValue
            }
        case "bloodType":
            return HealthValueWords.bloodTypeText(characteristicValue)
        case "wheelchairUse":
            switch characteristicValue {
            case "yes": return String(localized: "Yes")
            case "no": return String(localized: "No")
            default: return characteristicValue
            }
        case "activityMoveMode":
            switch characteristicValue {
            case "activeEnergy": return typeName(healthType: "activeEnergyBurned", serverName: "Active energy")
            case "appleMoveTime": return typeName(healthType: "appleMoveTime", serverName: "Move minutes")
            default: return characteristicValue
            }
        default:
            return characteristicValue
        }
    }

    /// A workout's activity type ("running") in the person's language; one
    /// not listed is the identifier read as words.
    static func workoutName(_ workoutActivityType: String) -> String {
        switch workoutActivityType {
        case "running": return String(localized: "health-workout-running", defaultValue: "Running")
        case "walking": return String(localized: "health-workout-walking", defaultValue: "Walking")
        case "cycling": return String(localized: "health-workout-cycling", defaultValue: "Cycling")
        case "swimming": return String(localized: "health-workout-swimming", defaultValue: "Swimming")
        case "hiking": return String(localized: "health-workout-hiking", defaultValue: "Hiking")
        case "yoga": return String(localized: "health-workout-yoga", defaultValue: "Yoga")
        case "pilates": return String(localized: "health-workout-pilates", defaultValue: "Pilates")
        case "dance": return String(localized: "health-workout-dance", defaultValue: "Dance")
        case "rowing": return String(localized: "health-workout-rowing", defaultValue: "Rowing")
        case "elliptical": return String(localized: "health-workout-elliptical", defaultValue: "Elliptical")
        case "stairClimbing": return String(localized: "health-workout-stairClimbing", defaultValue: "Stair climbing")
        case "coreTraining": return String(localized: "health-workout-coreTraining", defaultValue: "Core training")
        case "functionalStrengthTraining": return String(localized: "health-workout-functionalStrengthTraining", defaultValue: "Functional strength training")
        case "traditionalStrengthTraining": return String(localized: "health-workout-traditionalStrengthTraining", defaultValue: "Traditional strength training")
        case "highIntensityIntervalTraining": return String(localized: "health-workout-highIntensityIntervalTraining", defaultValue: "High intensity interval training")
        case "other": return String(localized: "health-workout-other", defaultValue: "Other workout")
        default:
            return workoutActivityType.isEmpty ? workoutName("other") : readAsWords(workoutActivityType)
        }
    }

    /// A stage of sleep.
    static func stageName(_ stage: HealthSleepStage) -> String {
        switch stage {
        case .asleepDeep: String(localized: "health-stage-asleepDeep", defaultValue: "Deep")
        case .asleepCore: String(localized: "health-stage-asleepCore", defaultValue: "Core")
        case .asleepREM: String(localized: "health-stage-asleepREM", defaultValue: "REM")
        case .asleepUnspecified: String(localized: "health-stage-asleepUnspecified", defaultValue: "Asleep")
        case .awake: String(localized: "health-stage-awake", defaultValue: "Awake")
        case .inBed: String(localized: "health-stage-inBed", defaultValue: "In bed")
        }
    }

    /// The stage a sleep sample's raw value stands for, if it is one.
    static func stage(ofCategoryValue categoryValue: Int?) -> HealthSleepStage? {
        guard let categoryValue else { return nil }
        return HealthSleepStage(rawValue: categoryValue)
    }

    /// "traditionalStrengthTraining" as "Traditional strength training".
    static func readAsWords(_ identifier: String) -> String {
        var words = ""
        for character in identifier {
            if character.isUppercase, !words.isEmpty {
                words.append(" ")
                words.append(contentsOf: character.lowercased())
            } else {
                words.append(character)
            }
        }
        guard let first = words.first else { return words }
        return first.uppercased() + words.dropFirst()
    }
}
