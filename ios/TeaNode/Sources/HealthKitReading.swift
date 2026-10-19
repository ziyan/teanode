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
    var routes: [HealthRouteInput] = []
    /// HealthKit's anchor after this page, archived; nil only when the page
    /// holds nothing.
    var newAnchorData: Data?
    /// How many objects HealthKit returned, added and deleted, including any
    /// the phone could not turn into a sample. A page as large as the limit
    /// asked for means there may be more.
    var itemCount = 0
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
    /// of samples.
    private static let dayCountPerQueryMost = 14

    private let store = HKHealthStore()
    private let lock = NSLock()
    private var observerQueries: [String: HKObserverQuery] = [:]

    // MARK: Permission

    /// Asks for read access to every readable type. Nothing is asked to be
    /// written: the app never writes to Health.
    func requestAuthorization() async throws {
        var readTypes = Set<HKObjectType>()
        for info in HealthTypes.all {
            if let objectType = Self.objectType(for: info) { readTypes.insert(objectType) }
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

    private static func objectType(for info: HealthTypeInfo) -> HKObjectType? {
        if info.valueKind == .characteristic {
            guard let identifier = info.healthKitIdentifier else { return nil }
            return HKCharacteristicType.characteristicType(forIdentifier: HKCharacteristicTypeIdentifier(rawValue: identifier))
        }
        return sampleType(for: info)
    }

    private static func sampleType(for info: HealthTypeInfo) -> HKSampleType? {
        switch info.valueKind {
        case .quantity:
            guard let identifier = info.healthKitIdentifier else { return nil }
            return HKQuantityType.quantityType(forIdentifier: HKQuantityTypeIdentifier(rawValue: identifier))
        case .category:
            guard let identifier = info.healthKitIdentifier else { return nil }
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
    private static func unit(for unitText: String) -> HKUnit? {
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
        case "appleEffortScore": return HKUnit.appleEffortScore()
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
    /// when there is none), and for workouts their routes.
    func readPage(info: HealthTypeInfo, anchorData: Data?, limit: Int) async throws -> HealthKitPage {
        guard let sampleType = Self.sampleType(for: info) else { return HealthKitPage() }
        var anchor: HKQueryAnchor?
        if let anchorData {
            // An anchor that cannot be read means reading from the beginning,
            // which the server takes as the same samples.
            anchor = try? NSKeyedUnarchiver.unarchivedObject(ofClass: HKQueryAnchor.self, from: anchorData)
        }
        let descriptor = HKAnchoredObjectQueryDescriptor<HKSample>(
            predicates: [HKSamplePredicate<HKSample>.sample(type: sampleType)], anchor: anchor, limit: limit)
        let result = try await descriptor.result(for: store)

        var page = HealthKitPage()
        page.itemCount = result.addedSamples.count + result.deletedObjects.count
        page.deletedHealthSampleIds = result.deletedObjects.map { $0.uuid.uuidString }
        var workouts: [HKWorkout] = []
        for sample in result.addedSamples {
            guard let input = Self.sampleInput(from: sample, info: info) else { continue }
            page.samples.append(input)
            if let workout = sample as? HKWorkout { workouts.append(workout) }
        }
        for workout in workouts {
            // A route that cannot be read now is found again by the sweep of
            // recent workouts, so it does not fail the page.
            guard let points = try? await routePoints(of: workout), !points.isEmpty else { continue }
            page.routes.append(HealthRouteInput(healthSampleId: workout.uuid.uuidString, points: points))
        }
        if page.itemCount > 0 {
            page.newAnchorData = try NSKeyedArchiver.archivedData(withRootObject: result.newAnchor, requiringSecureCoding: true)
        }
        return page
    }

    // MARK: Turning a sample into what is sent

    private static func sampleInput(from sample: HKSample, info: HealthTypeInfo) -> HealthSampleInput? {
        var metadata = plainMetadata(sample.metadata)
        var quantityValue: Double?
        var quantityUnit = ""
        var categoryValue: Int?
        var workoutActivityType = ""

        switch info.valueKind {
        case .quantity:
            guard let quantitySample = sample as? HKQuantitySample, let unit = unit(for: info.quantityUnit),
                quantitySample.quantity.is(compatibleWith: unit)
            else { return nil }
            quantityValue = quantitySample.quantity.doubleValue(for: unit)
            quantityUnit = info.quantityUnit
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
            healthSampleId: sample.uuid.uuidString, healthType: info.healthType, startedAt: sample.startDate, endedAt: sample.endDate,
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
        let energyType = HKQuantityType(.activeEnergyBurned)
        if let energy = workout.statistics(for: energyType)?.sumQuantity(), energy.is(compatibleWith: HKUnit.kilocalorie()) {
            totals[HealthWorkoutMetadata.totalEnergyBurnedKilocalories] = .number(energy.doubleValue(for: HKUnit.kilocalorie()))
        }
        // A workout counts its distance under the type of its sport.
        let distanceTypes: [HKQuantityType] = [
            HKQuantityType(.distanceWalkingRunning), HKQuantityType(.distanceCycling), HKQuantityType(.distanceSwimming),
            HKQuantityType(.distanceWheelchair), HKQuantityType(.distanceDownhillSnowSports), HKQuantityType(.distanceCrossCountrySkiing),
            HKQuantityType(.distancePaddleSports), HKQuantityType(.distanceRowing), HKQuantityType(.distanceSkatingSports),
        ]
        for distanceType in distanceTypes {
            if let distance = workout.statistics(for: distanceType)?.sumQuantity(), distance.is(compatibleWith: HKUnit.meter()) {
                totals[HealthWorkoutMetadata.totalDistanceMeters] = .number(distance.doubleValue(for: HKUnit.meter()))
                break
            }
        }
        return totals
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
        default: return "unrecognized"
        }
    }

    private static func stateOfMindLabelName(_ label: HKStateOfMind.Label) -> String {
        switch label {
        case .amazed: return "amazed"
        case .amused: return "amused"
        case .angry: return "angry"
        case .annoyed: return "annoyed"
        case .anxious: return "anxious"
        case .ashamed: return "ashamed"
        case .brave: return "brave"
        case .calm: return "calm"
        case .confident: return "confident"
        case .content: return "content"
        case .disappointed: return "disappointed"
        case .discouraged: return "discouraged"
        case .disgusted: return "disgusted"
        case .drained: return "drained"
        case .embarrassed: return "embarrassed"
        case .excited: return "excited"
        case .frustrated: return "frustrated"
        case .grateful: return "grateful"
        case .guilty: return "guilty"
        case .happy: return "happy"
        case .hopeful: return "hopeful"
        case .hopeless: return "hopeless"
        case .indifferent: return "indifferent"
        case .irritated: return "irritated"
        case .jealous: return "jealous"
        case .joyful: return "joyful"
        case .lonely: return "lonely"
        case .overwhelmed: return "overwhelmed"
        case .passionate: return "passionate"
        case .peaceful: return "peaceful"
        case .proud: return "proud"
        case .relieved: return "relieved"
        case .sad: return "sad"
        case .satisfied: return "satisfied"
        case .scared: return "scared"
        case .stressed: return "stressed"
        case .surprised: return "surprised"
        case .worried: return "worried"
        default: return "label\(label.rawValue)"
        }
    }

    /// A workout's sport by name. A sport this list does not have is "other";
    /// the raw value is in the sample's metadata too.
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
        case .cardioDance: return "cardioDance"
        case .climbing: return "climbing"
        case .cooldown: return "cooldown"
        case .coreTraining: return "coreTraining"
        case .cricket: return "cricket"
        case .crossCountrySkiing: return "crossCountrySkiing"
        case .crossTraining: return "crossTraining"
        case .curling: return "curling"
        case .cycling: return "cycling"
        case .dance: return "dance"
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
        case .pickleball: return "pickleball"
        case .pilates: return "pilates"
        case .play: return "play"
        case .preparationAndRecovery: return "preparationAndRecovery"
        case .racquetball: return "racquetball"
        case .rowing: return "rowing"
        case .rugby: return "rugby"
        case .running: return "running"
        case .sailing: return "sailing"
        case .skatingSports: return "skatingSports"
        case .snowSports: return "snowSports"
        case .snowboarding: return "snowboarding"
        case .soccer: return "soccer"
        case .socialDance: return "socialDance"
        case .softball: return "softball"
        case .squash: return "squash"
        case .stairClimbing: return "stairClimbing"
        case .stairs: return "stairs"
        case .stepTraining: return "stepTraining"
        case .surfingSports: return "surfingSports"
        case .swimBikeRun: return "swimBikeRun"
        case .swimming: return "swimming"
        case .tableTennis: return "tableTennis"
        case .taiChi: return "taiChi"
        case .tennis: return "tennis"
        case .trackAndField: return "trackAndField"
        case .traditionalStrengthTraining: return "traditionalStrengthTraining"
        case .transition: return "transition"
        case .underwaterDiving: return "underwaterDiving"
        case .volleyball: return "volleyball"
        case .walking: return "walking"
        case .waterFitness: return "waterFitness"
        case .waterPolo: return "waterPolo"
        case .waterSports: return "waterSports"
        case .wheelchairRunPace: return "wheelchairRunPace"
        case .wheelchairWalkPace: return "wheelchairWalkPace"
        case .wrestling: return "wrestling"
        case .yoga: return "yoga"
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

    /// Workouts of the last days whose route has not been sent: a route is
    /// often saved a little after its workout, so the page that carried the
    /// workout did not have it.
    func recentWorkoutRoutes(since date: Date, excluding sentHealthSampleIds: Set<String>) async throws -> [HealthRouteInput] {
        let predicate = HKQuery.predicateForSamples(withStart: date, end: nil, options: [])
        let workoutDescriptor = HKSampleQueryDescriptor<HKWorkout>(
            predicates: [HKSamplePredicate<HKWorkout>.workout(predicate)], sortDescriptors: [], limit: nil)
        let workouts = try await workoutDescriptor.result(for: store)
        var routes: [HealthRouteInput] = []
        for workout in workouts where !sentHealthSampleIds.contains(workout.uuid.uuidString) {
            let points = try await routePoints(of: workout)
            if !points.isEmpty { routes.append(HealthRouteInput(healthSampleId: workout.uuid.uuidString, points: points)) }
        }
        return routes
    }

    // MARK: Day summaries

    /// The summaries of a type for days ("YYYY-MM-DD"). A quantity's comes from
    /// HealthKit's statistics, which merge the sources the way the Health app
    /// does; everything else from the samples of the day, by TeaNodeKit. A day
    /// with nothing on it comes back as an empty summary, which tells the
    /// server the day is empty now.
    func daySummaries(info: HealthTypeInfo, days: [String], calendar: Calendar) async throws -> [HealthDaySummaryInput] {
        var summaries: [HealthDaySummaryInput] = []
        for run in Self.runs(of: days, calendar: calendar) {
            switch info.valueKind {
            case .quantity:
                summaries += try await quantitySummaries(info: info, days: run.days, interval: run.interval, calendar: calendar)
            case .category:
                summaries += try await categoryDaySummaries(info: info, days: run.days, interval: run.interval, calendar: calendar)
            case .workout:
                summaries += try await workoutDaySummaries(days: run.days, interval: run.interval, calendar: calendar)
            case .electrocardiogram, .audiogram, .stateOfMind:
                summaries += try await countedDaySummaries(info: info, days: run.days, interval: run.interval, calendar: calendar)
            case .characteristic:
                break
            }
        }
        let summarizedDays = Set(summaries.map(\.summaryDay))
        for day in days where !summarizedDays.contains(day) {
            summaries.append(HealthDaySummaryInput(healthType: info.healthType, summaryDay: day, quantityUnit: info.quantityUnit, sampleCount: 0))
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
            if let end = runEnd, runDays.count < dayCountPerQueryMost, abs(interval.start.timeIntervalSince(end)) < 1 {
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
        info: HealthTypeInfo, days: [String], interval: DateInterval, calendar: Calendar
    ) async throws -> [HealthDaySummaryInput] {
        guard let quantityType = Self.sampleType(for: info) as? HKQuantityType, let unit = Self.unit(for: info.quantityUnit) else { return [] }
        let predicate = HKQuery.predicateForSamples(withStart: interval.start, end: interval.end, options: .strictStartDate)
        // What HealthKit allows depends on how the type adds up: asking for
        // an average of steps stops the app with an exception.
        let isCumulative = quantityType.aggregationStyle == .cumulative
        let options: HKStatisticsOptions = isCumulative ? [.cumulativeSum] : [.discreteAverage, .discreteMin, .discreteMax]
        let statisticsDescriptor = HKStatisticsCollectionQueryDescriptor(
            predicate: HKSamplePredicate<HKQuantitySample>.quantitySample(type: quantityType, predicate: predicate), options: options,
            anchorDate: interval.start, intervalComponents: DateComponents(day: 1))
        let collection = try await statisticsDescriptor.result(for: store)

        // HealthKit does not count the samples behind a statistic, and the
        // server weights a month's average by them.
        let sampleDescriptor = HKSampleQueryDescriptor<HKQuantitySample>(
            predicates: [HKSamplePredicate<HKQuantitySample>.quantitySample(type: quantityType, predicate: predicate)], sortDescriptors: [],
            limit: nil)
        let samples = try await sampleDescriptor.result(for: store)
        var sampleCounts: [String: Int] = [:]
        for sample in samples { sampleCounts[HealthDays.text(of: sample.startDate, calendar: calendar), default: 0] += 1 }

        var summaries: [HealthDaySummaryInput] = []
        for statistics in collection.statistics() {
            let day = HealthDays.text(of: statistics.startDate, calendar: calendar)
            guard days.contains(day) else { continue }
            let measured = HealthQuantityStatistics(
                sumValue: Self.number(statistics.sumQuantity(), unit: unit), averageValue: Self.number(statistics.averageQuantity(), unit: unit),
                minimumValue: Self.number(statistics.minimumQuantity(), unit: unit),
                maximumValue: Self.number(statistics.maximumQuantity(), unit: unit), sampleCount: sampleCounts[day] ?? 0)
            summaries.append(
                HealthDaySummaries.quantitySummary(healthType: info.healthType, summaryDay: day, statistics: measured, quantityUnit: info.quantityUnit))
        }
        return summaries
    }

    private static func number(_ quantity: HKQuantity?, unit: HKUnit) -> Double? {
        guard let quantity, quantity.is(compatibleWith: unit) else { return nil }
        let value = quantity.doubleValue(for: unit)
        return value.isFinite ? value : nil
    }

    private func categoryDaySummaries(
        info: HealthTypeInfo, days: [String], interval: DateInterval, calendar: Calendar
    ) async throws -> [HealthDaySummaryInput] {
        guard let categoryType = Self.sampleType(for: info) as? HKCategoryType else { return [] }
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
        return HealthDaySummaries.categorySummaries(healthType: info.healthType, samples: categorySamples, calendar: calendar, days: Set(days))
    }

    private func workoutDaySummaries(days: [String], interval: DateInterval, calendar: Calendar) async throws -> [HealthDaySummaryInput] {
        let predicate = HKQuery.predicateForSamples(withStart: interval.start, end: interval.end, options: .strictStartDate)
        let descriptor = HKSampleQueryDescriptor<HKWorkout>(
            predicates: [HKSamplePredicate<HKWorkout>.workout(predicate)], sortDescriptors: [], limit: nil)
        let workouts = try await descriptor.result(for: store)
        // The same conversion as for sending, so the summary and the samples
        // agree on a workout's totals.
        let workoutInfo = HealthTypes.info(for: "workout")
        var workoutSamples: [HealthWorkoutSample] = []
        if let workoutInfo {
            for workout in workouts {
                if let input = Self.sampleInput(from: workout, info: workoutInfo), let workoutSample = HealthWorkoutSample(input) {
                    workoutSamples.append(workoutSample)
                }
            }
        }
        return HealthDaySummaries.workoutSummaries(workoutSamples, calendar: calendar, days: Set(days))
    }

    /// Electrocardiograms, audiograms and State of Mind entries are counted by
    /// the day they began.
    private func countedDaySummaries(
        info: HealthTypeInfo, days: [String], interval: DateInterval, calendar: Calendar
    ) async throws -> [HealthDaySummaryInput] {
        guard let sampleType = Self.sampleType(for: info) else { return [] }
        let predicate = HKQuery.predicateForSamples(withStart: interval.start, end: interval.end, options: .strictStartDate)
        let descriptor = HKSampleQueryDescriptor<HKSample>(
            predicates: [HKSamplePredicate<HKSample>.sample(type: sampleType, predicate: predicate)], sortDescriptors: [], limit: nil)
        let samples = try await descriptor.result(for: store)
        let categorySamples = samples.map {
            HealthCategorySample(healthSampleId: $0.uuid.uuidString, startedAt: $0.startDate, endedAt: $0.endDate, categoryValue: 0)
        }
        return HealthDaySummaries.countSummaries(healthType: info.healthType, samples: categorySamples, calendar: calendar, days: Set(days))
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
            add("dateOfBirth", String(format: "%04d-%02d-%02d", year, month, day))
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

    /// Starts one observer for each readable type, and asks iOS to wake the
    /// app about each at most hourly. Called as the app launches, whether the
    /// person opened it or iOS relaunched it for new data: iOS delivers the
    /// event to an observer that exists then.
    func startObserving(handler: @escaping HealthKitObserverHandler) async {
        var targets: [(healthType: String, sampleType: HKSampleType)] = []
        for info in HealthTypes.all where info.valueKind != .characteristic {
            if let sampleType = Self.sampleType(for: info) { targets.append((info.healthType, sampleType)) }
        }
        targets.append((Self.workoutRouteHealthType, HKSeriesType.workoutRoute()))
        for target in targets {
            let healthType = target.healthType
            guard !isObserving(healthType) else { continue }
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
            remember(query, for: healthType)
            store.execute(query)
            // The person may not have allowed this type, which fails here and
            // is no reason to stop the others.
            try? await store.enableBackgroundDelivery(for: target.sampleType, frequency: .hourly)
        }
    }

    /// Stops the observers and asks iOS to stop waking the app.
    func stopObserving() async {
        for query in forgetObserverQueries() { store.stop(query) }
        try? await store.disableAllBackgroundDelivery()
    }

    private func isObserving(_ healthType: String) -> Bool {
        lock.lock()
        defer { lock.unlock() }
        return observerQueries[healthType] != nil
    }

    private func remember(_ query: HKObserverQuery, for healthType: String) {
        lock.lock()
        defer { lock.unlock() }
        observerQueries[healthType] = query
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
