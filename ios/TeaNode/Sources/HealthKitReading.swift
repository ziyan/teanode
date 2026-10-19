import CoreLocation
import Foundation
import HealthKit
import TeaNodeKit

// Everything that touches HealthKit objects lives here, in a class that is not
// tied to the main actor. HealthKit's classes are not Sendable, so they never
// leave this file: what goes out is TeaNodeKit's plain values (the upload
// shapes, a day's summary), which cross to HealthSync freely. The calls are
// the async ones HealthKit offers; the only callback API used is the route
// query, which has no async form here, and the observer query, which is the
// way iOS wakes the app.

/// HealthKitPage is one page of what HealthKit holds for one type since an
/// anchor, turned into what the phone sends.
struct HealthKitPage: Sendable {
    var samples: [HealthSampleInput] = []
    var deletedHealthSampleIds: [String] = []
    /// HealthKit's anchor after this page, archived; nil only when the page
    /// holds nothing.
    var newAnchorData: Data?
    /// How many objects HealthKit returned, added and deleted, including any
    /// the phone could not turn into a sample. A page as large as the limit
    /// asked for means there may be more.
    var itemCount = 0
}

/// HealthObserverStart is the sample types the observers just made watch. It
/// is `@unchecked Sendable` because the types are only handed back to the
/// reader, which uses them to ask for background deliveries.
struct HealthObserverStart: @unchecked Sendable {
    let sampleTypes: [HKSampleType]
}

/// HealthKitObserverHandler is told that HealthKit has something new of a
/// type, and must call the second argument once it has dealt with it: iOS
/// keeps the app running until then.
typealias HealthKitObserverHandler = @Sendable (String, @escaping @Sendable () -> Void) -> Void

/// HealthKitReader reads HealthKit. It is `@unchecked Sendable` because every
/// HealthKit object it holds is touched only inside its own methods: the
/// store, which HealthKit allows to be used from any thread, and the observer
/// queries, which are kept under a lock.
final class HealthKitReader: @unchecked Sendable {
    /// The name the wake for a new workout route is reported under. A route
    /// can be saved a little after its workout, so it has its own observer.
    static let workoutRouteHealthType = "workoutRoute"

    /// Most keys and the longest text taken from a sample's HealthKit
    /// metadata: it is the app that wrote the sample that decides what is in
    /// it.
    private static let metadataKeyCountMost = 40
    private static let metadataTextLengthMost = 500
    /// The most days read in one query: a day of heart rate can be thousands
    /// of samples, and each is held until it has been counted.
    private static let dayCountPerQueryMost = 7

    private let store = HKHealthStore()
    private let lock = NSLock()
    private var observerQueries: [String: HKObserverQuery] = [:]

    // MARK: Permission

    /// Asks for read access to every readable type. Nothing is asked to be
    /// written: the app never writes to Health.
    func requestAuthorization() async throws {
        var readTypes = Set<HKObjectType>()
        for typeInfo in HealthTypes.all {
            if let objectType = Self.objectType(for: typeInfo) { readTypes.insert(objectType) }
        }
        readTypes.insert(HKSeriesType.workoutRoute())
        try await store.requestAuthorization(toShare: Set<HKSampleType>(), read: readTypes)
    }

    /// Whether the error is HealthKit saying the phone is locked, so that its
    /// data cannot be read now.
    static func isLockedFailure(_ error: Error) -> Bool {
        guard let healthError = error as? HKError else { return false }
        return healthError.code == .errorDatabaseInaccessible
    }

    // MARK: Types

    private static func objectType(for typeInfo: HealthTypeInfo) -> HKObjectType? {
        if typeInfo.valueKind == .characteristic {
            guard let identifier = typeInfo.healthKitIdentifier else { return nil }
            return HKCharacteristicType.characteristicType(forIdentifier: HKCharacteristicTypeIdentifier(rawValue: identifier))
        }
        return sampleType(for: typeInfo)
    }

    private static func sampleType(for typeInfo: HealthTypeInfo) -> HKSampleType? {
        switch typeInfo.valueKind {
        case .quantity:
            guard let identifier = typeInfo.healthKitIdentifier else { return nil }
            return HKQuantityType.quantityType(forIdentifier: HKQuantityTypeIdentifier(rawValue: identifier))
        case .category:
            guard let identifier = typeInfo.healthKitIdentifier else { return nil }
            return HKCategoryType.categoryType(forIdentifier: HKCategoryTypeIdentifier(rawValue: identifier))
        case .workout:
            return HKObjectType.workoutType()
        case .electrocardiogram:
            return HKObjectType.electrocardiogramType()
        case .audiogram:
            return HKObjectType.audiogramSampleType()
        case .stateOfMind:
            return HKObjectType.stateOfMindType()
        case .characteristic:
            return nil
        }
    }

    /// The unit a type is read in, built from HealthKit's own constructors.
    /// `HKUnit(from:)` is not used: it stops the app with an exception on text
    /// it does not understand. A unit not listed here leaves the type unread.
    private static func healthKitUnit(for unitText: String) -> HKUnit? {
        switch unitText {
        case "count": return HKUnit.count()
        case "m": return HKUnit.meter()
        case "cm": return HKUnit.meterUnit(with: .centi)
        case "ms": return HKUnit.secondUnit(with: .milli)
        case "min": return HKUnit.minute()
        case "m/s": return HKUnit.meter().unitDivided(by: HKUnit.second())
        case "W": return HKUnit.watt()
        case "kcal": return HKUnit.kilocalorie()
        case "count/min": return HKUnit.count().unitDivided(by: HKUnit.minute())
        case "kcal/(kg*hr)":
            return HKUnit.kilocalorie().unitDivided(by: HKUnit.gramUnit(with: .kilo).unitMultiplied(by: HKUnit.hour()))
        case "degC": return HKUnit.degreeCelsius()
        case "kg": return HKUnit.gramUnit(with: .kilo)
        case "g": return HKUnit.gram()
        case "mg": return HKUnit.gramUnit(with: .milli)
        case "mcg": return HKUnit.gramUnit(with: .micro)
        case "%": return HKUnit.percent()
        case "dBASPL": return HKUnit.decibelAWeightedSoundPressureLevel()
        case "ml/(kg*min)":
            return HKUnit.literUnit(with: .milli).unitDivided(by: HKUnit.gramUnit(with: .kilo).unitMultiplied(by: HKUnit.minute()))
        case "mmHg": return HKUnit.millimeterOfMercury()
        case "mg/dL": return HKUnit.gramUnit(with: .milli).unitDivided(by: HKUnit.literUnit(with: .deci))
        case "mL": return HKUnit.literUnit(with: .milli)
        case "L": return HKUnit.liter()
        case "L/min": return HKUnit.liter().unitDivided(by: HKUnit.minute())
        case "IU": return HKUnit.internationalUnit()
        case "S": return HKUnit.siemen()
        default: return nil
        }
    }

    // MARK: Reading a page

    /// Reads up to `limit` objects of a type after the anchor (all of them
    /// when there is none). A workout's route is not read here: a page can
    /// hold a thousand workouts, and every route of them held at once is
    /// hundreds of megabytes. HealthSync asks for each (routeInput) as it
    /// sends the workout.
    func readPage(typeInfo: HealthTypeInfo, anchorData: Data?, limit: Int) async throws -> HealthKitPage {
        guard let sampleType = Self.sampleType(for: typeInfo) else { return HealthKitPage() }
        var anchor: HKQueryAnchor?
        if let anchorData {
            // An anchor that cannot be read means reading from the beginning,
            // which the server takes as the same samples.
            anchor = try? NSKeyedUnarchiver.unarchivedObject(ofClass: HKQueryAnchor.self, from: anchorData)
        }
        let descriptor = HKAnchoredObjectQueryDescriptor<HKSample>(
            predicates: [HKSamplePredicate<HKSample>.sample(type: sampleType)], anchor: anchor, limit: limit)
        let anchoredResult = try await descriptor.result(for: store)

        var page = HealthKitPage()
        page.itemCount = anchoredResult.addedSamples.count + anchoredResult.deletedObjects.count
        page.deletedHealthSampleIds = anchoredResult.deletedObjects.map { $0.uuid.uuidString }
        for sample in anchoredResult.addedSamples {
            guard let sampleInput = Self.sampleInput(from: sample, typeInfo: typeInfo) else { continue }
            page.samples.append(sampleInput)
        }
        if page.itemCount > 0 {
            page.newAnchorData = try NSKeyedArchiver.archivedData(withRootObject: anchoredResult.newAnchor, requiringSecureCoding: true)
        }
        return page
    }

    // MARK: Turning a sample into what is sent

    private static func sampleInput(from sample: HKSample, typeInfo: HealthTypeInfo) -> HealthSampleInput? {
        var metadata = plainMetadata(sample.metadata)
        var quantityValue: Double?
        var quantityUnit = ""
        var categoryValue: Int?
        var workoutActivityType = ""

        switch typeInfo.valueKind {
        case .quantity:
            guard let quantitySample = sample as? HKQuantitySample, let readUnit = healthKitUnit(for: typeInfo.quantityUnit),
                quantitySample.quantity.is(compatibleWith: readUnit)
            else { return nil }
            quantityValue = quantitySample.quantity.doubleValue(for: readUnit)
            quantityUnit = typeInfo.quantityUnit
        case .category:
            guard let categorySample = sample as? HKCategorySample else { return nil }
            categoryValue = categorySample.value
        case .workout:
            guard let workout = sample as? HKWorkout else { return nil }
            workoutActivityType = activityTypeName(workout.workoutActivityType)
            metadata["workoutActivityTypeRawValue"] = .number(Double(workout.workoutActivityType.rawValue))
            metadata["durationSeconds"] = .number(workout.duration)
            for (key, value) in workoutTotals(of: workout) { metadata[key] = value }
        case .electrocardiogram:
            guard let electrocardiogram = sample as? HKElectrocardiogram else { return nil }
            metadata["electrocardiogramClassification"] = .string(classificationName(electrocardiogram.classification))
            let beatsPerMinute = HKUnit.count().unitDivided(by: HKUnit.minute())
            if let averageHeartRate = electrocardiogram.averageHeartRate, averageHeartRate.is(compatibleWith: beatsPerMinute) {
                metadata["averageHeartRateBeatsPerMinute"] = .number(averageHeartRate.doubleValue(for: beatsPerMinute))
            }
        case .audiogram:
            guard let audiogram = sample as? HKAudiogramSample else { return nil }
            metadata["audiogramPoints"] = .array(audiogramPoints(of: audiogram))
        case .stateOfMind:
            guard let stateOfMind = sample as? HKStateOfMind else { return nil }
            metadata["stateOfMindKind"] = .string(stateOfMindKindName(stateOfMind.kind))
            metadata["stateOfMindValence"] = .number(stateOfMind.valence)
            metadata["stateOfMindLabels"] = .array(stateOfMind.labels.map { .string(stateOfMindLabelName($0)) })
        case .characteristic:
            return nil
        }

        let source = sample.sourceRevision.source
        return HealthSampleInput(
            healthSampleId: sample.uuid.uuidString, healthType: typeInfo.healthType, startedAt: sample.startDate, endedAt: sample.endDate,
            quantityValue: quantityValue, quantityUnit: quantityUnit, categoryValue: categoryValue, workoutActivityType: workoutActivityType,
            sourceName: source.name, sourceBundleId: source.bundleIdentifier, deviceName: sample.device?.name ?? "", sampleMetadata: metadata)
    }

    /// The keys HealthKit keeps in a sample's metadata whose values are plain
    /// text, numbers or yes and no. Dates, quantities and the rest are left
    /// out.
    private static func plainMetadata(_ metadata: [String: Any]?) -> [String: JSONValue] {
        guard let metadata else { return [:] }
        var plain: [String: JSONValue] = [:]
        for key in metadata.keys.sorted().prefix(metadataKeyCountMost) {
            let value = metadata[key]
            if let text = value as? String {
                plain[key] = .string(String(text.prefix(metadataTextLengthMost)))
            } else if let number = value as? NSNumber {
                if CFGetTypeID(number) == CFBooleanGetTypeID() {
                    plain[key] = .bool(number.boolValue)
                } else if number.doubleValue.isFinite {
                    plain[key] = .number(number.doubleValue)
                }
            }
        }
        return plain
    }

    /// The energy and distance a workout carries, under the names the day
    /// summaries read them by.
    private static func workoutTotals(of workout: HKWorkout) -> [String: JSONValue] {
        var totals: [String: JSONValue] = [:]
        let kilocalories = HKUnit.kilocalorie()
        let meters = HKUnit.meter()
        if let energy = workout.statistics(for: HKQuantityType(.activeEnergyBurned))?.sumQuantity(), energy.is(compatibleWith: kilocalories) {
            totals[HealthWorkoutMetadata.totalEnergyBurnedKilocalories] = .number(energy.doubleValue(for: kilocalories))
        }
        // A workout counts its distance under the type of its sport.
        for distanceType in Self.distanceTypes() {
            if let distance = workout.statistics(for: distanceType)?.sumQuantity(), distance.is(compatibleWith: meters) {
                totals[HealthWorkoutMetadata.totalDistanceMeters] = .number(distance.doubleValue(for: meters))
                break
            }
        }
        // A workout written by an older app has its totals on the workout
        // itself and no statistics. These two properties are deprecated, and
        // still there.
        if totals[HealthWorkoutMetadata.totalEnergyBurnedKilocalories] == nil, let energy = workout.totalEnergyBurned,
            energy.is(compatibleWith: kilocalories)
        {
            totals[HealthWorkoutMetadata.totalEnergyBurnedKilocalories] = .number(energy.doubleValue(for: kilocalories))
        }
        if totals[HealthWorkoutMetadata.totalDistanceMeters] == nil, let distance = workout.totalDistance, distance.is(compatibleWith: meters) {
            totals[HealthWorkoutMetadata.totalDistanceMeters] = .number(distance.doubleValue(for: meters))
        }
        return totals
    }

    /// The distance types a workout may count its distance under, looked up
    /// by HealthKit's name for each so that a type the system does not have
    /// is left out rather than stopping the build. A function and not a
    /// stored property: the types are not Sendable.
    private static func distanceTypes() -> [HKQuantityType] {
        let names = [
            "DistanceWalkingRunning", "DistanceCycling", "DistanceSwimming", "DistanceWheelchair", "DistanceDownhillSnowSports",
            "DistanceCrossCountrySkiing", "DistancePaddleSports", "DistanceRowing", "DistanceSkatingSports",
        ]
        return names.compactMap { name in
            HKQuantityType.quantityType(forIdentifier: HKQuantityTypeIdentifier(rawValue: "HKQuantityTypeIdentifier" + name))
        }
    }

    private static func audiogramPoints(of audiogram: HKAudiogramSample) -> [JSONValue] {
        let decibels = HKUnit.decibelHearingLevel()
        var points: [JSONValue] = []
        for point in audiogram.sensitivityPoints {
            var row: [String: JSONValue] = ["frequencyHertz": .number(point.frequency.doubleValue(for: HKUnit.hertz()))]
            if let left = point.leftEarSensitivity { row["leftEarDecibels"] = .number(left.doubleValue(for: decibels)) }
            if let right = point.rightEarSensitivity { row["rightEarDecibels"] = .number(right.doubleValue(for: decibels)) }
            points.append(.object(row))
        }
        return points
    }

    private static func classificationName(_ classification: HKElectrocardiogram.Classification) -> String {
        switch classification {
        case .notSet: return "notSet"
        case .sinusRhythm: return "sinusRhythm"
        case .atrialFibrillation: return "atrialFibrillation"
        case .inconclusiveLowHeartRate: return "inconclusiveLowHeartRate"
        case .inconclusiveHighHeartRate: return "inconclusiveHighHeartRate"
        case .inconclusivePoorReading: return "inconclusivePoorReading"
        case .inconclusiveOther: return "inconclusiveOther"
        default: return "unrecognized"
        }
    }

    private static func stateOfMindKindName(_ kind: HKStateOfMind.Kind) -> String {
        switch kind {
        case .momentaryEmotion: return "momentaryEmotion"
        case .dailyMood: return "dailyMood"
        default: return "kind\(kind.rawValue)"
        }
    }

    /// A label is named by HealthKit's number for it: the list of labels
    /// grows with each system, and a number is what stays the same.
    private static func stateOfMindLabelName(_ label: HKStateOfMind.Label) -> String {
        "label\(label.rawValue)"
    }

    /// A workout's sport by name, for the sports that have been in HealthKit
    /// for years. Any other is "other", and its number is in the sample's
    /// metadata too (workoutActivityTypeRawValue). Both kinds of dance are
    /// "dance".
    private static func activityTypeName(_ activityType: HKWorkoutActivityType) -> String {
        switch activityType {
        case .americanFootball: return "americanFootball"
        case .archery: return "archery"
        case .australianFootball: return "australianFootball"
        case .badminton: return "badminton"
        case .barre: return "barre"
        case .baseball: return "baseball"
        case .basketball: return "basketball"
        case .bowling: return "bowling"
        case .boxing: return "boxing"
        case .climbing: return "climbing"
        case .cooldown: return "cooldown"
        case .coreTraining: return "coreTraining"
        case .cricket: return "cricket"
        case .crossCountrySkiing: return "crossCountrySkiing"
        case .crossTraining: return "crossTraining"
        case .curling: return "curling"
        case .cycling: return "cycling"
        case .discSports: return "discSports"
        case .downhillSkiing: return "downhillSkiing"
        case .elliptical: return "elliptical"
        case .equestrianSports: return "equestrianSports"
        case .fencing: return "fencing"
        case .fishing: return "fishing"
        case .fitnessGaming: return "fitnessGaming"
        case .flexibility: return "flexibility"
        case .functionalStrengthTraining: return "functionalStrengthTraining"
        case .golf: return "golf"
        case .gymnastics: return "gymnastics"
        case .handCycling: return "handCycling"
        case .handball: return "handball"
        case .highIntensityIntervalTraining: return "highIntensityIntervalTraining"
        case .hiking: return "hiking"
        case .hockey: return "hockey"
        case .hunting: return "hunting"
        case .jumpRope: return "jumpRope"
        case .kickboxing: return "kickboxing"
        case .lacrosse: return "lacrosse"
        case .martialArts: return "martialArts"
        case .mindAndBody: return "mindAndBody"
        case .mixedCardio: return "mixedCardio"
        case .paddleSports: return "paddleSports"
        case .play: return "play"
        case .preparationAndRecovery: return "preparationAndRecovery"
        case .pilates: return "pilates"
        case .racquetball: return "racquetball"
        case .rowing: return "rowing"
        case .rugby: return "rugby"
        case .running: return "running"
        case .sailing: return "sailing"
        case .skatingSports: return "skatingSports"
        case .snowSports: return "snowSports"
        case .snowboarding: return "snowboarding"
        case .soccer: return "soccer"
        case .softball: return "softball"
        case .squash: return "squash"
        case .stairClimbing: return "stairClimbing"
        case .stairs: return "stairs"
        case .stepTraining: return "stepTraining"
        case .surfingSports: return "surfingSports"
        case .swimming: return "swimming"
        case .tableTennis: return "tableTennis"
        case .taiChi: return "taiChi"
        case .tennis: return "tennis"
        case .trackAndField: return "trackAndField"
        case .traditionalStrengthTraining: return "traditionalStrengthTraining"
        case .volleyball: return "volleyball"
        case .walking: return "walking"
        case .waterFitness: return "waterFitness"
        case .waterPolo: return "waterPolo"
        case .waterSports: return "waterSports"
        case .wheelchairRunPace: return "wheelchairRunPace"
        case .wheelchairWalkPace: return "wheelchairWalkPace"
        case .wrestling: return "wrestling"
        case .yoga: return "yoga"
        case .cardioDance, .socialDance: return "dance"
        default: return "other"
        }
    }

    // MARK: Routes

    /// A workout's path: every route saved with it, joined, in order.
    private func routePoints(of workout: HKWorkout) async throws -> [HealthRoutePoint] {
        let routeDescriptor = HKSampleQueryDescriptor<HKSample>(
            predicates: [HKSamplePredicate<HKSample>.sample(type: HKSeriesType.workoutRoute(), predicate: HKQuery.predicateForObjects(from: workout))],
            sortDescriptors: [], limit: nil)
        let routeSamples = try await routeDescriptor.result(for: store)
        var points: [HealthRoutePoint] = []
        for routeSample in routeSamples {
            guard let route = routeSample as? HKWorkoutRoute else { continue }
            points += try await routePoints(of: route, workoutStart: workout.startDate)
        }
        return points.sorted { $0.secondsFromStart < $1.secondsFromStart }
    }

    /// One route's locations. The query hands them over in pieces and says
    /// when it is done; it has no async form, so a continuation waits for it.
    private func routePoints(of route: HKWorkoutRoute, workoutStart: Date) async throws -> [HealthRoutePoint] {
        let collector = RoutePointCollector(workoutStart: workoutStart)
        return try await withCheckedThrowingContinuation { continuation in
            let query = HKWorkoutRouteQuery(route: route) { _, locations, isDone, error in
                if let error {
                    if collector.finish() != nil { continuation.resume(throwing: error) }
                    return
                }
                if let locations { collector.add(locations) }
                if isDone, let points = collector.finish() { continuation.resume(returning: points) }
            }
            store.execute(query)
        }
    }

    /// The workouts of the last days, whose route may have been saved after
    /// the workout was read.
    func recentWorkoutHealthSampleIds(since date: Date) async throws -> [String] {
        let predicate = HKQuery.predicateForSamples(withStart: date, end: nil, options: [])
        let workoutDescriptor = HKSampleQueryDescriptor<HKWorkout>(
            predicates: [HKSamplePredicate<HKWorkout>.workout(predicate)], sortDescriptors: [], limit: nil)
        let workouts = try await workoutDescriptor.result(for: store)
        return workouts.map { $0.uuid.uuidString }
    }

    /// The route of one workout, read when it is about to be sent; nil if the
    /// workout has none (yet).
    func routeInput(forWorkoutHealthSampleId healthSampleId: String) async throws -> HealthRouteInput? {
        guard let workoutUUID = UUID(uuidString: healthSampleId) else { return nil }
        let workoutDescriptor = HKSampleQueryDescriptor<HKWorkout>(
            predicates: [HKSamplePredicate<HKWorkout>.workout(HKQuery.predicateForObject(with: workoutUUID))], sortDescriptors: [], limit: 1)
        guard let workout = try await workoutDescriptor.result(for: store).first else { return nil }
        let points = try await routePoints(of: workout)
        guard !points.isEmpty else { return nil }
        return HealthRouteInput(healthSampleId: healthSampleId, points: points)
    }

    // MARK: Day summaries

    /// The summaries of a type for days ("YYYY-MM-DD"). A quantity's comes from
    /// HealthKit's statistics, which merge the sources the way the Health app
    /// does; everything else from the samples of the day, by TeaNodeKit. A day
    /// with nothing on it comes back as an empty summary, which tells the
    /// server the day is empty now.
    func daySummaries(typeInfo: HealthTypeInfo, days: [String], calendar: Calendar) async throws -> [HealthDaySummaryInput] {
        var summaries: [HealthDaySummaryInput] = []
        for run in Self.runs(of: days, calendar: calendar) {
            switch typeInfo.valueKind {
            case .quantity:
                summaries += try await quantitySummaries(typeInfo: typeInfo, days: run.days, interval: run.interval, calendar: calendar)
            case .category:
                summaries += try await categoryDaySummaries(typeInfo: typeInfo, days: run.days, interval: run.interval, calendar: calendar)
            case .workout:
                summaries += try await workoutDaySummaries(days: run.days, interval: run.interval, calendar: calendar)
            case .electrocardiogram, .audiogram, .stateOfMind:
                summaries += try await countedDaySummaries(typeInfo: typeInfo, days: run.days, interval: run.interval, calendar: calendar)
            case .characteristic:
                break
            }
        }
        let summarizedDays = Set(summaries.map(\.summaryDay))
        for day in days where !summarizedDays.contains(day) {
            summaries.append(
                HealthDaySummaryInput(healthType: typeInfo.healthType, summaryDay: day, quantityUnit: typeInfo.quantityUnit, sampleCount: 0))
        }
        return summaries
    }

    /// Days grouped into runs of days that follow each other, each no longer
    /// than a query should read, with the time they cover.
    private static func runs(of days: [String], calendar: Calendar) -> [(days: [String], interval: DateInterval)] {
        var runs: [(days: [String], interval: DateInterval)] = []
        var runDays: [String] = []
        var runStart: Date?
        var runEnd: Date?
        for day in Set(days).sorted() {
            guard let interval = HealthDays.interval(ofDay: day, calendar: calendar) else { continue }
            if let runEndDate = runEnd, runDays.count < dayCountPerQueryMost, abs(interval.start.timeIntervalSince(runEndDate)) < 1 {
                runDays.append(day)
                runEnd = interval.end
            } else {
                if let start = runStart, let end = runEnd { runs.append((runDays, DateInterval(start: start, end: end))) }
                runDays = [day]
                runStart = interval.start
                runEnd = interval.end
            }
        }
        if let start = runStart, let end = runEnd { runs.append((runDays, DateInterval(start: start, end: end))) }
        return runs
    }

    private func quantitySummaries(
        typeInfo: HealthTypeInfo, days: [String], interval: DateInterval, calendar: Calendar
    ) async throws -> [HealthDaySummaryInput] {
        guard let quantityType = Self.sampleType(for: typeInfo) as? HKQuantityType, let readUnit = Self.healthKitUnit(for: typeInfo.quantityUnit)
        else { return [] }
        // What HealthKit allows depends on how the type adds up, and an option
        // it rejects stops the app with an exception that cannot be caught: a
        // sum for what accumulates, an average with the least and the most
        // for what is read as a level. A type read as an equivalent continuous
        // level (the sound levels) has no average of its own to ask for, so
        // its day is worked out from its readings, and no statistics query is
        // made for it at all.
        let options: HKStatisticsOptions
        switch quantityType.aggregationStyle {
        case .cumulative:
            options = [.cumulativeSum]
        case .discreteEquivalentContinuousLevel:
            return try await readingSummaries(
                typeInfo: typeInfo, quantityType: quantityType, readUnit: readUnit, days: days, interval: interval, calendar: calendar)
        default:
            options = [.discreteAverage, .discreteMin, .discreteMax]
        }
        // No option: a reading that begins before midnight and ends after it
        // is split between the days, as the Health app counts it.
        let predicate = HKQuery.predicateForSamples(withStart: interval.start, end: interval.end, options: [])
        let statisticsDescriptor = HKStatisticsCollectionQueryDescriptor(
            predicate: HKSamplePredicate<HKQuantitySample>.quantitySample(type: quantityType, predicate: predicate), options: options,
            anchorDate: interval.start, intervalComponents: DateComponents(day: 1))
        let collection = try await statisticsDescriptor.result(for: store)
        let sampleCounts = try await sampleCountsByDay(quantityType: quantityType, days: days, calendar: calendar)

        var summaries: [HealthDaySummaryInput] = []
        for statistics in collection.statistics() {
            let day = HealthDays.text(of: statistics.startDate, calendar: calendar)
            guard days.contains(day) else { continue }
            let measured = HealthQuantityStatistics(
                sumValue: Self.number(statistics.sumQuantity(), unit: readUnit), averageValue: Self.number(statistics.averageQuantity(), unit: readUnit),
                minimumValue: Self.number(statistics.minimumQuantity(), unit: readUnit),
                maximumValue: Self.number(statistics.maximumQuantity(), unit: readUnit), sampleCount: sampleCounts[day] ?? 0)
            summaries.append(
                HealthDaySummaries.quantitySummary(
                    healthType: typeInfo.healthType, summaryDay: day, statistics: measured, quantityUnit: typeInfo.quantityUnit))
        }
        return summaries
    }

    /// The days of a quantity worked out from its readings, one day at a time
    /// so that no more than a day of them is held.
    private func readingSummaries(
        typeInfo: HealthTypeInfo, quantityType: HKQuantityType, readUnit: HKUnit, days: [String], interval: DateInterval, calendar: Calendar
    ) async throws -> [HealthDaySummaryInput] {
        var summaries: [HealthDaySummaryInput] = []
        for day in days {
            guard let dayInterval = HealthDays.interval(ofDay: day, calendar: calendar), interval.intersection(with: dayInterval) != nil else {
                continue
            }
            let samples = try await quantitySamples(quantityType: quantityType, interval: dayInterval)
            let readings = samples.compactMap { sample -> HealthReading? in
                guard let value = Self.number(sample.quantity, unit: readUnit) else { return nil }
                return HealthReading(startedAt: sample.startDate, value: value)
            }
            let statisticsByDay = HealthDaySummaries.readingStatistics(readings, calendar: calendar, days: [day])
            guard let statistics = statisticsByDay[day] else { continue }
            summaries.append(
                HealthDaySummaries.quantitySummary(
                    healthType: typeInfo.healthType, summaryDay: day, statistics: statistics, quantityUnit: typeInfo.quantityUnit))
        }
        return summaries
    }

    /// The readings of a quantity that begin in the interval.
    private func quantitySamples(quantityType: HKQuantityType, interval: DateInterval) async throws -> [HKQuantitySample] {
        let predicate = HKQuery.predicateForSamples(withStart: interval.start, end: interval.end, options: .strictStartDate)
        let sampleDescriptor = HKSampleQueryDescriptor<HKQuantitySample>(
            predicates: [HKSamplePredicate<HKQuantitySample>.quantitySample(type: quantityType, predicate: predicate)], sortDescriptors: [],
            limit: nil)
        return try await sampleDescriptor.result(for: store)
    }

    /// How many readings begin on each of the days. HealthKit does not count
    /// the readings behind a statistic, and the server weights a month's
    /// average by them. They are counted one day at a time and let go, so no
    /// more than a day of them is held.
    private func sampleCountsByDay(quantityType: HKQuantityType, days: [String], calendar: Calendar) async throws -> [String: Int] {
        var sampleCounts: [String: Int] = [:]
        for day in days {
            guard let dayInterval = HealthDays.interval(ofDay: day, calendar: calendar) else { continue }
            sampleCounts[day] = try await quantitySamples(quantityType: quantityType, interval: dayInterval).count
        }
        return sampleCounts
    }

    private static func number(_ quantity: HKQuantity?, unit: HKUnit) -> Double? {
        guard let quantity, quantity.is(compatibleWith: unit) else { return nil }
        let value = quantity.doubleValue(for: unit)
        return value.isFinite ? value : nil
    }

    private func categoryDaySummaries(
        typeInfo: HealthTypeInfo, days: [String], interval: DateInterval, calendar: Calendar
    ) async throws -> [HealthDaySummaryInput] {
        guard let categoryType = Self.sampleType(for: typeInfo) as? HKCategoryType else { return [] }
        // Every sample that overlaps the days, not only those that begin in
        // them: a night's sleep begins the evening before.
        let predicate = HKQuery.predicateForSamples(withStart: interval.start, end: interval.end, options: [])
        let descriptor = HKSampleQueryDescriptor<HKCategorySample>(
            predicates: [HKSamplePredicate<HKCategorySample>.categorySample(type: categoryType, predicate: predicate)], sortDescriptors: [],
            limit: nil)
        let samples = try await descriptor.result(for: store)
        let categorySamples = samples.map {
            HealthCategorySample(healthSampleId: $0.uuid.uuidString, startedAt: $0.startDate, endedAt: $0.endDate, categoryValue: $0.value)
        }
        return HealthDaySummaries.categorySummaries(healthType: typeInfo.healthType, samples: categorySamples, calendar: calendar, days: Set(days))
    }

    private func workoutDaySummaries(days: [String], interval: DateInterval, calendar: Calendar) async throws -> [HealthDaySummaryInput] {
        let predicate = HKQuery.predicateForSamples(withStart: interval.start, end: interval.end, options: .strictStartDate)
        let descriptor = HKSampleQueryDescriptor<HKWorkout>(
            predicates: [HKSamplePredicate<HKWorkout>.workout(predicate)], sortDescriptors: [], limit: nil)
        let workouts = try await descriptor.result(for: store)
        // The same conversion as for sending, so the summary and the samples
        // agree on a workout's totals.
        let workoutTypeInfo = HealthTypes.info(for: "workout")
        var workoutSamples: [HealthWorkoutSample] = []
        if let workoutTypeInfo {
            for workout in workouts {
                if let input = Self.sampleInput(from: workout, typeInfo: workoutTypeInfo), let workoutSample = HealthWorkoutSample(input) {
                    workoutSamples.append(workoutSample)
                }
            }
        }
        return HealthDaySummaries.workoutSummaries(workoutSamples, calendar: calendar, days: Set(days))
    }

    /// Electrocardiograms, audiograms and State of Mind entries are counted by
    /// the day they began.
    private func countedDaySummaries(
        typeInfo: HealthTypeInfo, days: [String], interval: DateInterval, calendar: Calendar
    ) async throws -> [HealthDaySummaryInput] {
        guard let sampleType = Self.sampleType(for: typeInfo) else { return [] }
        let predicate = HKQuery.predicateForSamples(withStart: interval.start, end: interval.end, options: .strictStartDate)
        let descriptor = HKSampleQueryDescriptor<HKSample>(
            predicates: [HKSamplePredicate<HKSample>.sample(type: sampleType, predicate: predicate)], sortDescriptors: [], limit: nil)
        let samples = try await descriptor.result(for: store)
        let categorySamples = samples.map {
            HealthCategorySample(healthSampleId: $0.uuid.uuidString, startedAt: $0.startDate, endedAt: $0.endDate, categoryValue: 0)
        }
        return HealthDaySummaries.countSummaries(healthType: typeInfo.healthType, samples: categorySamples, calendar: calendar, days: Set(days))
    }

    // MARK: Characteristics

    /// The facts about the person that are set in Health. One that is not set,
    /// or that the person did not allow, is left out.
    func readCharacteristics() async -> [HealthCharacteristicInput] {
        var characteristics: [HealthCharacteristicInput] = []
        func add(_ healthType: String, _ value: String?) {
            guard let value, !value.isEmpty else { return }
            characteristics.append(HealthCharacteristicInput(healthType: healthType, characteristicValue: value))
        }
        if let components = try? store.dateOfBirthComponents(), let year = components.year, let month = components.month, let day = components.day {
            add("dateOfBirth", String(format: "%04ld-%02ld-%02ld", year, month, day))
        }
        if let sex = try? store.biologicalSex().biologicalSex {
            switch sex {
            case .female: add("biologicalSex", "female")
            case .male: add("biologicalSex", "male")
            case .other: add("biologicalSex", "other")
            default: break
            }
        }
        if let bloodType = try? store.bloodType().bloodType {
            switch bloodType {
            case .aPositive: add("bloodType", "aPositive")
            case .aNegative: add("bloodType", "aNegative")
            case .bPositive: add("bloodType", "bPositive")
            case .bNegative: add("bloodType", "bNegative")
            case .abPositive: add("bloodType", "abPositive")
            case .abNegative: add("bloodType", "abNegative")
            case .oPositive: add("bloodType", "oPositive")
            case .oNegative: add("bloodType", "oNegative")
            default: break
            }
        }
        if let skinType = try? store.fitzpatrickSkinType().skinType {
            let romanNumerals = ["I", "II", "III", "IV", "V", "VI"]
            // HealthKit numbers the types from 1; 0 is "not set".
            if skinType.rawValue >= 1, skinType.rawValue <= romanNumerals.count { add("fitzpatrickSkinType", romanNumerals[skinType.rawValue - 1]) }
        }
        if let wheelchairUse = try? store.wheelchairUse().wheelchairUse {
            switch wheelchairUse {
            case .yes: add("wheelchairUse", "yes")
            case .no: add("wheelchairUse", "no")
            default: break
            }
        }
        if let moveMode = try? store.activityMoveMode().activityMoveMode {
            switch moveMode {
            case .activeEnergy: add("activityMoveMode", "activeEnergy")
            case .appleMoveTime: add("activityMoveMode", "appleMoveTime")
            default: break
            }
        }
        return characteristics
    }

    // MARK: Waking the app

    /// Starts one observer for each readable type. Called as the app launches,
    /// whether the person opened it or iOS relaunched it for new data: iOS
    /// delivers the event to an observer that exists then, so every observer is
    /// made and started in this step, before anything is awaited. Returns the
    /// sample types it made an observer for, for `enableBackgroundDelivery`.
    func startObservers(handler: @escaping HealthKitObserverHandler) -> HealthObserverStart {
        var targets: [(healthType: String, sampleType: HKSampleType)] = []
        for typeInfo in HealthTypes.all where typeInfo.valueKind != .characteristic {
            if let sampleType = Self.sampleType(for: typeInfo) { targets.append((typeInfo.healthType, sampleType)) }
        }
        targets.append((Self.workoutRouteHealthType, HKSeriesType.workoutRoute()))
        return HealthObserverStart(sampleTypes: startObservers(for: targets, handler: handler))
    }

    /// Makes and executes an observer for each target that has none. The
    /// check and the remembering are one step under the lock, so two calls at
    /// once (the launch, and the account being known) make one observer for
    /// each type between them. Returns the sample types it made one for.
    private func startObservers(for targets: [(healthType: String, sampleType: HKSampleType)], handler: @escaping HealthKitObserverHandler)
        -> [HKSampleType]
    {
        var made: [(sampleType: HKSampleType, query: HKObserverQuery)] = []
        lock.lock()
        for target in targets where observerQueries[target.healthType] == nil {
            let healthType = target.healthType
            let query = HKObserverQuery(sampleType: target.sampleType, predicate: nil) { _, completionHandler, error in
                // iOS hands the completion handler over to be called once, on
                // whichever thread finishes the work.
                nonisolated(unsafe) let finish = completionHandler
                guard error == nil else {
                    finish()
                    return
                }
                handler(healthType) { finish() }
            }
            observerQueries[healthType] = query
            made.append((target.sampleType, query))
        }
        lock.unlock()
        for observer in made { store.execute(observer.query) }
        return made.map(\.sampleType)
    }

    /// Asks iOS to wake the app about each type at most hourly. The person
    /// may not have allowed a type, which fails for that type and is no
    /// reason to stop the others.
    func enableBackgroundDelivery(_ started: HealthObserverStart) async {
        nonisolated(unsafe) let types = started.sampleTypes
        await withTaskGroup(of: Void.self) { group in
            for index in types.indices {
                group.addTask {
                    _ = try? await self.store.enableBackgroundDelivery(for: types[index], frequency: .hourly)
                }
            }
        }
    }

    /// Stops the observers and asks iOS to stop waking the app.
    func stopObserving() async {
        for query in forgetObserverQueries() { store.stop(query) }
        try? await store.disableAllBackgroundDelivery()
    }

    private func forgetObserverQueries() -> [HKObserverQuery] {
        lock.lock()
        defer { lock.unlock() }
        let queries = Array(observerQueries.values)
        observerQueries = [:]
        return queries
    }
}

/// RoutePointCollector gathers the locations a route query hands over in
/// pieces, on a queue of HealthKit's, and says once when it is finished.
private final class RoutePointCollector: @unchecked Sendable {
    private let workoutStart: Date
    private let lock = NSLock()
    private var points: [HealthRoutePoint] = []
    private var isFinished = false

    init(workoutStart: Date) { self.workoutStart = workoutStart }

    func add(_ locations: [CLLocation]) {
        lock.lock()
        defer { lock.unlock() }
        for location in locations where location.horizontalAccuracy >= 0 {
            points.append(
                HealthRoutePoint(
                    secondsFromStart: location.timestamp.timeIntervalSince(workoutStart), latitudeDegrees: location.coordinate.latitude,
                    longitudeDegrees: location.coordinate.longitude,
                    // Core Location writes "could not tell" as a negative accuracy.
                    altitudeMeters: location.verticalAccuracy >= 0 ? location.altitude : 0))
        }
    }

    /// The points the first time it is called, nil afterwards, so that the
    /// continuation is resumed once.
    func finish() -> [HealthRoutePoint]? {
        lock.lock()
        defer { lock.unlock() }
        guard !isFinished else { return nil }
        isFinished = true
        return points
    }
}
