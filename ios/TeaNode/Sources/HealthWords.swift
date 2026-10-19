import Foundation
import TeaNodeKit

// The words the Health screens say for Health's own things, in the person's
// language: the Health app's groups, the most common kinds of data, workouts
// and the stages of sleep. Each is written under a key of its own because
// "Health" and "Activity" are already words of the app with another meaning
// (the spending category, the agent's activity). Anything not listed falls
// back to the English name the server sends, the same as the dashboard.

enum HealthWords {
    /// The feature's name: the side menu, the screen and the setting.
    static var title: String { String(localized: "health-title", defaultValue: "Health") }

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

    /// The most common sixty kinds of data, translated.
    private static func translatedTypeName(_ healthType: String) -> String? {
        switch healthType {
        case "stepCount": return String(localized: "health-type-stepCount", defaultValue: "Steps")
        case "distanceWalkingRunning": return String(localized: "health-type-distanceWalkingRunning", defaultValue: "Walking and running distance")
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
        case "heartRateRecoveryOneMinute": return String(localized: "health-type-heartRateRecoveryOneMinute", defaultValue: "Heart rate recovery")
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
        case "appleSleepingWristTemperature": return String(localized: "health-type-appleSleepingWristTemperature", defaultValue: "Wrist temperature")
        case "walkingSpeed": return String(localized: "health-type-walkingSpeed", defaultValue: "Walking speed")
        case "walkingStepLength": return String(localized: "health-type-walkingStepLength", defaultValue: "Walking step length")
        case "walkingAsymmetryPercentage": return String(localized: "health-type-walkingAsymmetryPercentage", defaultValue: "Walking asymmetry")
        case "walkingDoubleSupportPercentage": return String(localized: "health-type-walkingDoubleSupportPercentage", defaultValue: "Double support time")
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
        default:
            return nil
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
