import Foundation

// What the phone sends the server about Health, and what it reads back. The
// phone reads HealthKit from a saved anchor (HealthSyncState.swift), works out
// each day's summary (HealthDaySummaries.swift or HealthKit's statistics),
// and sends both with ReportHealth, a batch at a time. The names are the
// server's (internal/api/v1api/apigraph/health.go) and the dashboard's.

// MARK: Reporting

/// HealthJSON turns the upload shapes into the JSON the calls send, and back.
enum HealthJSON {
    static func makeEncoder() -> JSONEncoder {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return encoder
    }

    /// An object written as JSON text, which the server takes as a string
    /// (sampleMetadataJson, categorySecondsJson).
    static func text<Value: Encodable>(_ value: Value) -> String {
        guard let data = try? makeEncoder().encode(value) else { return "{}" }
        return String(decoding: data, as: UTF8.self)
    }

    /// The object in JSON text; empty when the text is empty or not an object.
    static func object<Value: Decodable>(_ text: String?, as type: Value.Type) -> Value? {
        guard let text, !text.isEmpty else { return nil }
        return try? JSONDecoder().decode(type, from: Data(text.utf8))
    }

    /// A value as the JSON the call sends.
    static func value<Value: Encodable>(_ value: Value) -> JSONValue {
        guard let data = try? makeEncoder().encode(value), let decoded = try? JSONDecoder().decode(JSONValue.self, from: data) else { return .null }
        return decoded
    }

    /// The seconds of a view, which the server sends as a JSON object (and
    /// which JSON text of one is read as too).
    static func seconds<Key: CodingKey>(_ container: KeyedDecodingContainer<Key>, _ key: Key) -> [String: Double] {
        if let object = try? container.decodeIfPresent([String: Double].self, forKey: key) { return object }
        if let text = try? container.decodeIfPresent(String.self, forKey: key) { return object(text, as: [String: Double].self) ?? [:] }
        return [:]
    }

    static func date<Key: CodingKey>(_ container: KeyedDecodingContainer<Key>, _ key: Key) throws -> Date {
        let text = try container.decode(String.self, forKey: key)
        guard let date = ServerTime.date(text) else {
            throw DecodingError.dataCorruptedError(forKey: key, in: container, debugDescription: "not an RFC 3339 time")
        }
        return date
    }
}

/// HealthSampleInput is one reading, as the phone sends it. A quantity has a
/// value and a unit, a category sample a raw value, a workout its activity
/// type; anything else about it (the totals of a workout, the classification
/// of an electrocardiogram, the keys HealthKit keeps in a sample's metadata)
/// goes in the metadata, which is sent as JSON text.
public struct HealthSampleInput: Codable, Hashable, Sendable {
    /// HealthKit's UUID for the sample; the server keeps one sample for each,
    /// so a batch sent twice adds nothing.
    public let healthSampleId: String
    public let healthType: String
    public let startedAt: Date
    public let endedAt: Date
    public let quantityValue: Double?
    public let quantityUnit: String
    public let categoryValue: Int?
    public let workoutActivityType: String
    public let sourceName: String
    public let sourceBundleId: String
    public let deviceName: String
    public let sampleMetadata: [String: JSONValue]

    /// The longest healthSampleId the server keeps.
    public static let healthSampleIdLengthMost = 64

    public init(
        healthSampleId: String, healthType: String, startedAt: Date, endedAt: Date, quantityValue: Double? = nil, quantityUnit: String = "",
        categoryValue: Int? = nil, workoutActivityType: String = "", sourceName: String = "", sourceBundleId: String = "", deviceName: String = "",
        sampleMetadata: [String: JSONValue] = [:]
    ) {
        self.healthSampleId = healthSampleId
        self.healthType = healthType
        self.startedAt = startedAt
        self.endedAt = endedAt
        self.quantityValue = quantityValue
        self.quantityUnit = quantityUnit
        self.categoryValue = categoryValue
        self.workoutActivityType = workoutActivityType
        self.sourceName = sourceName
        self.sourceBundleId = sourceBundleId
        self.deviceName = deviceName
        self.sampleMetadata = sampleMetadata
    }

    private enum CodingKeys: String, CodingKey {
        case healthSampleId, healthType, startedAt, endedAt, quantityValue, quantityUnit, categoryValue, workoutActivityType, sourceName
        case sourceBundleId, deviceName, sampleMetadataJson
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        healthSampleId = try container.decode(String.self, forKey: .healthSampleId)
        healthType = try container.decode(String.self, forKey: .healthType)
        startedAt = try HealthJSON.date(container, CodingKeys.startedAt)
        endedAt = try HealthJSON.date(container, CodingKeys.endedAt)
        quantityValue = try container.decodeIfPresent(Double.self, forKey: .quantityValue)
        quantityUnit = try container.decodeIfPresent(String.self, forKey: .quantityUnit) ?? ""
        categoryValue = try container.decodeIfPresent(Int.self, forKey: .categoryValue)
        workoutActivityType = try container.decodeIfPresent(String.self, forKey: .workoutActivityType) ?? ""
        sourceName = try container.decodeIfPresent(String.self, forKey: .sourceName) ?? ""
        sourceBundleId = try container.decodeIfPresent(String.self, forKey: .sourceBundleId) ?? ""
        deviceName = try container.decodeIfPresent(String.self, forKey: .deviceName) ?? ""
        sampleMetadata = HealthJSON.object(try container.decodeIfPresent(String.self, forKey: .sampleMetadataJson), as: [String: JSONValue].self) ?? [:]
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(healthSampleId, forKey: .healthSampleId)
        try container.encode(healthType, forKey: .healthType)
        try container.encode(CalendarDates.text(startedAt), forKey: .startedAt)
        try container.encode(CalendarDates.text(endedAt), forKey: .endedAt)
        try container.encodeIfPresent(quantityValue, forKey: .quantityValue)
        try container.encode(quantityUnit, forKey: .quantityUnit)
        try container.encodeIfPresent(categoryValue, forKey: .categoryValue)
        try container.encode(workoutActivityType, forKey: .workoutActivityType)
        try container.encode(sourceName, forKey: .sourceName)
        try container.encode(sourceBundleId, forKey: .sourceBundleId)
        try container.encode(deviceName, forKey: .deviceName)
        try container.encode(HealthJSON.text(sampleMetadata), forKey: .sampleMetadataJson)
    }

    /// Whether the server would take the sample. It refuses a whole batch for
    /// one that is not, so such a sample is left out of the batch: it has no
    /// identifier, no type, a value that is not a number (JSON cannot hold
    /// one), or ends before it begins.
    public var isAcceptable: Bool {
        !healthSampleId.isEmpty && healthSampleId.utf8.count <= Self.healthSampleIdLengthMost && !healthType.isEmpty && endedAt >= startedAt
            && (quantityValue?.isFinite ?? true)
    }

    var variables: JSONValue { HealthJSON.value(self) }
}

/// HealthDaySummaryInput is one type on one day, worked out on the phone,
/// where sources can be merged the way the Health app merges them.
public struct HealthDaySummaryInput: Codable, Hashable, Sendable {
    public let healthType: String
    /// The phone's calendar day, "YYYY-MM-DD".
    public let summaryDay: String
    public let quantityUnit: String
    public let sumValue: Double?
    public let averageValue: Double?
    public let minimumValue: Double?
    public let maximumValue: Double?
    public let sampleCount: Int
    /// For categories and workouts: the time covered, sources overlapping
    /// counted once.
    public let durationSeconds: Double?
    /// Seconds by sleep stage (and "asleep" for all the asleep stages
    /// together); for workouts, "energyKilocalories" and "distanceMeters".
    public let categorySeconds: [String: Double]

    public init(
        healthType: String, summaryDay: String, quantityUnit: String = "", sumValue: Double? = nil, averageValue: Double? = nil,
        minimumValue: Double? = nil, maximumValue: Double? = nil, sampleCount: Int, durationSeconds: Double? = nil,
        categorySeconds: [String: Double] = [:]
    ) {
        self.healthType = healthType
        self.summaryDay = summaryDay
        self.quantityUnit = quantityUnit
        self.sumValue = sumValue
        self.averageValue = averageValue
        self.minimumValue = minimumValue
        self.maximumValue = maximumValue
        self.sampleCount = sampleCount
        self.durationSeconds = durationSeconds
        self.categorySeconds = categorySeconds
    }

    private enum CodingKeys: String, CodingKey {
        case healthType, summaryDay, quantityUnit, sumValue, averageValue, minimumValue, maximumValue, sampleCount, durationSeconds
        case categorySecondsJson
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        healthType = try container.decode(String.self, forKey: .healthType)
        summaryDay = try container.decode(String.self, forKey: .summaryDay)
        quantityUnit = try container.decodeIfPresent(String.self, forKey: .quantityUnit) ?? ""
        sumValue = try container.decodeIfPresent(Double.self, forKey: .sumValue)
        averageValue = try container.decodeIfPresent(Double.self, forKey: .averageValue)
        minimumValue = try container.decodeIfPresent(Double.self, forKey: .minimumValue)
        maximumValue = try container.decodeIfPresent(Double.self, forKey: .maximumValue)
        sampleCount = try container.decode(Int.self, forKey: .sampleCount)
        durationSeconds = try container.decodeIfPresent(Double.self, forKey: .durationSeconds)
        categorySeconds = HealthJSON.object(try container.decodeIfPresent(String.self, forKey: .categorySecondsJson), as: [String: Double].self) ?? [:]
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(healthType, forKey: .healthType)
        try container.encode(summaryDay, forKey: .summaryDay)
        try container.encode(quantityUnit, forKey: .quantityUnit)
        try container.encodeIfPresent(sumValue, forKey: .sumValue)
        try container.encodeIfPresent(averageValue, forKey: .averageValue)
        try container.encodeIfPresent(minimumValue, forKey: .minimumValue)
        try container.encodeIfPresent(maximumValue, forKey: .maximumValue)
        try container.encode(sampleCount, forKey: .sampleCount)
        try container.encodeIfPresent(durationSeconds, forKey: .durationSeconds)
        try container.encode(HealthJSON.text(categorySeconds), forKey: .categorySecondsJson)
    }

    /// Whether the server would take the summary: a day written as
    /// "YYYY-MM-DD", no number that is not a number, and a type.
    public var isAcceptable: Bool {
        let values = [sumValue, averageValue, minimumValue, maximumValue, durationSeconds].compactMap { $0 } + Array(categorySeconds.values)
        return !healthType.isEmpty && HealthDays.isDayText(summaryDay) && sampleCount >= 0 && values.allSatisfy(\.isFinite)
    }

    var variables: JSONValue { HealthJSON.value(self) }
}

/// HealthRoutePoint is one position of a workout's path.
public struct HealthRoutePoint: Hashable, Sendable {
    public let secondsFromStart: Double
    public let latitudeDegrees: Double
    public let longitudeDegrees: Double
    public let altitudeMeters: Double

    public init(secondsFromStart: Double, latitudeDegrees: Double, longitudeDegrees: Double, altitudeMeters: Double) {
        self.secondsFromStart = secondsFromStart
        self.latitudeDegrees = latitudeDegrees
        self.longitudeDegrees = longitudeDegrees
        self.altitudeMeters = altitudeMeters
    }

    /// From a row as sent: [secondsFromStart, latitudeDegrees, longitudeDegrees, altitudeMeters].
    public init?(row: [Double]) {
        guard row.count >= 3 else { return nil }
        self.init(secondsFromStart: row[0], latitudeDegrees: row[1], longitudeDegrees: row[2], altitudeMeters: row.count > 3 ? row[3] : 0)
    }

    public var row: [Double] { [secondsFromStart, latitudeDegrees, longitudeDegrees, altitudeMeters] }

    /// Whether the coordinates are on the earth and every number is a number.
    public var isAcceptable: Bool {
        secondsFromStart.isFinite && altitudeMeters.isFinite
            && LocationGeometry.isOnEarth(latitudeDegrees: latitudeDegrees, longitudeDegrees: longitudeDegrees)
    }
}

/// HealthRouteInput is the path of a workout, sent after (or with) the workout.
public struct HealthRouteInput: Codable, Hashable, Sendable {
    /// The workout's healthSampleId.
    public let healthSampleId: String
    /// [secondsFromStart, latitudeDegrees, longitudeDegrees, altitudeMeters] rows.
    public let routePoints: [[Double]]

    /// The most points the server keeps of one route.
    public static let pointCountMost = 20_000

    public init(healthSampleId: String, routePoints: [[Double]]) {
        self.healthSampleId = healthSampleId
        self.routePoints = routePoints
    }

    /// A route from its points, in order. Points not on the earth are left
    /// out; a path of more than 20,000 points is thinned evenly, keeping the
    /// first and the last.
    public init(healthSampleId: String, points: [HealthRoutePoint]) {
        let usable = points.filter(\.isAcceptable)
        var kept = usable
        if usable.count > Self.pointCountMost {
            let step = Double(usable.count - 1) / Double(Self.pointCountMost - 1)
            kept = (0..<Self.pointCountMost).map { usable[Int((Double($0) * step).rounded())] }
        }
        self.init(healthSampleId: healthSampleId, routePoints: kept.map(\.row))
    }

    public var points: [HealthRoutePoint] { routePoints.compactMap { HealthRoutePoint(row: $0) } }

    /// Whether the server would take the route: a workout, at least one
    /// point, no more than the most, and every point a position.
    public var isAcceptable: Bool {
        !healthSampleId.isEmpty && !routePoints.isEmpty && routePoints.count <= Self.pointCountMost
            && routePoints.allSatisfy { HealthRoutePoint(row: $0)?.isAcceptable ?? false }
    }

    var variables: JSONValue { HealthJSON.value(self) }
}

/// HealthCharacteristicInput is a fact about the person: date of birth,
/// biological sex, blood type, skin type, wheelchair use.
public struct HealthCharacteristicInput: Codable, Hashable, Sendable {
    public let healthType: String
    /// "1990-04-12", "female", "aPositive", ...
    public let characteristicValue: String

    public init(healthType: String, characteristicValue: String) {
        self.healthType = healthType
        self.characteristicValue = characteristicValue
    }

    var variables: JSONValue { HealthJSON.value(self) }
}

/// HealthBatch is what one ReportHealth call carries.
public struct HealthBatch: Sendable, Equatable {
    public static let sampleCountMost = 1000
    public static let deletedSampleIdCountMost = 2000
    public static let daySummaryCountMost = 1000
    public static let routeCountMost = 5
    public static let characteristicCountMost = 20

    public let samples: [HealthSampleInput]
    public let deletedHealthSampleIds: [String]
    public let daySummaries: [HealthDaySummaryInput]
    public let routes: [HealthRouteInput]
    public let characteristics: [HealthCharacteristicInput]

    public init(
        samples: [HealthSampleInput] = [], deletedHealthSampleIds: [String] = [], daySummaries: [HealthDaySummaryInput] = [],
        routes: [HealthRouteInput] = [], characteristics: [HealthCharacteristicInput] = []
    ) {
        self.samples = samples
        self.deletedHealthSampleIds = deletedHealthSampleIds
        self.daySummaries = daySummaries
        self.routes = routes
        self.characteristics = characteristics
    }

    public var isEmpty: Bool { entryCount == 0 }

    /// How many samples, deleted identifiers, summaries, routes and
    /// characteristics it carries together.
    public var entryCount: Int { samples.count + deletedHealthSampleIds.count + daySummaries.count + routes.count + characteristics.count }

    /// Whether it is within what one call takes.
    public var isWithinLimits: Bool {
        samples.count <= Self.sampleCountMost && deletedHealthSampleIds.count <= Self.deletedSampleIdCountMost
            && daySummaries.count <= Self.daySummaryCountMost && routes.count <= Self.routeCountMost
            && characteristics.count <= Self.characteristicCountMost
    }

    /// Splits what is waiting into the calls to make: each carries at most
    /// 1000 samples, 2000 deleted identifiers, 1000 day summaries, 5 routes
    /// and 20 characteristics, the oldest first. A route is never sent in a
    /// call before the one that carries its workout (a route for a workout
    /// sent earlier goes in the first call). What the server would refuse for
    /// one entry (see isAcceptable) is left out rather than costing the
    /// batch, and counted.
    public static func requests(
        samples: [HealthSampleInput] = [], deletedHealthSampleIds: [String] = [], daySummaries: [HealthDaySummaryInput] = [],
        routes: [HealthRouteInput] = [], characteristics: [HealthCharacteristicInput] = []
    ) -> (requests: [HealthBatch], rejectedEntryCount: Int) {
        let acceptableSamples = samples.filter(\.isAcceptable)
        let acceptableSummaries = daySummaries.filter(\.isAcceptable)
        let acceptableRoutes = routes.filter(\.isAcceptable)
        let acceptableIds = deletedHealthSampleIds.filter { !$0.isEmpty }
        let rejectedEntryCount =
            (samples.count - acceptableSamples.count) + (daySummaries.count - acceptableSummaries.count)
            + (routes.count - acceptableRoutes.count) + (deletedHealthSampleIds.count - acceptableIds.count)

        func slices<Element>(_ elements: [Element], _ size: Int) -> [[Element]] {
            stride(from: 0, to: elements.count, by: size).map { Array(elements[$0..<min($0 + size, elements.count)]) }
        }
        let sampleSlices = slices(acceptableSamples, sampleCountMost)
        let deletedSlices = slices(acceptableIds, deletedSampleIdCountMost)
        let summarySlices = slices(acceptableSummaries, daySummaryCountMost)
        let characteristicSlices = slices(characteristics, characteristicCountMost)
        let requestCount = max(sampleSlices.count, deletedSlices.count, summarySlices.count, characteristicSlices.count)

        // The call each sample is in, so that its route is not sent before it.
        var callOfSample: [String: Int] = [:]
        for (index, slice) in sampleSlices.enumerated() { for sample in slice { callOfSample[sample.healthSampleId] = index } }
        var routeSlices = [[HealthRouteInput]](repeating: [], count: requestCount)
        for route in acceptableRoutes {
            var index = callOfSample[route.healthSampleId] ?? 0
            while true {
                if index == routeSlices.count { routeSlices.append([]) }
                if routeSlices[index].count < routeCountMost { break }
                index += 1
            }
            routeSlices[index].append(route)
        }

        let total = max(requestCount, routeSlices.count)
        let requests = (0..<total).map { index in
            HealthBatch(
                samples: index < sampleSlices.count ? sampleSlices[index] : [],
                deletedHealthSampleIds: index < deletedSlices.count ? deletedSlices[index] : [],
                daySummaries: index < summarySlices.count ? summarySlices[index] : [],
                routes: index < routeSlices.count ? routeSlices[index] : [],
                characteristics: index < characteristicSlices.count ? characteristicSlices[index] : [])
        }.filter { !$0.isEmpty }
        return (requests, rejectedEntryCount)
    }

    /// The two smaller batches to send when the server calls this one invalid
    /// without saying which entry is wrong. If it holds more than one kind of
    /// entry, the first kinds (samples, then deleted identifiers, summaries,
    /// routes, characteristics) are apart from the rest, so a route still
    /// comes after its workout; else the first half and the rest of the one
    /// kind it holds. Nil for a single entry, which is the one the server
    /// refuses.
    public func split() -> (first: HealthBatch, second: HealthBatch)? {
        guard entryCount > 1 else { return nil }
        let kinds = [
            samples.count, deletedHealthSampleIds.count, daySummaries.count, routes.count, characteristics.count,
        ]
        let present = kinds.indices.filter { kinds[$0] > 0 }
        if present.count > 1 {
            let firstKinds = Set(present.prefix(present.count / 2))
            func part(_ keep: (Int) -> Bool) -> HealthBatch {
                HealthBatch(
                    samples: keep(0) ? samples : [], deletedHealthSampleIds: keep(1) ? deletedHealthSampleIds : [],
                    daySummaries: keep(2) ? daySummaries : [], routes: keep(3) ? routes : [], characteristics: keep(4) ? characteristics : [])
            }
            return (part { firstKinds.contains($0) }, part { !firstKinds.contains($0) })
        }
        func halves<Element>(_ elements: [Element]) -> ([Element], [Element]) {
            let middle = elements.count / 2
            return (Array(elements[..<middle]), Array(elements[middle...]))
        }
        switch present[0] {
        case 0:
            let (first, second) = halves(samples)
            return (HealthBatch(samples: first), HealthBatch(samples: second))
        case 1:
            let (first, second) = halves(deletedHealthSampleIds)
            return (HealthBatch(deletedHealthSampleIds: first), HealthBatch(deletedHealthSampleIds: second))
        case 2:
            let (first, second) = halves(daySummaries)
            return (HealthBatch(daySummaries: first), HealthBatch(daySummaries: second))
        case 3:
            let (first, second) = halves(routes)
            return (HealthBatch(routes: first), HealthBatch(routes: second))
        default:
            let (first, second) = halves(characteristics)
            return (HealthBatch(characteristics: first), HealthBatch(characteristics: second))
        }
    }

    /// The GraphQL variables of the call (the phone's identifier is added by
    /// Connection.reportHealth).
    public var variables: [String: JSONValue] {
        [
            "samples": .array(samples.map(\.variables)), "deletedHealthSampleIds": .array(deletedHealthSampleIds.map { .string($0) }),
            "daySummaries": .array(daySummaries.map(\.variables)), "routes": .array(routes.map(\.variables)),
            "characteristics": .array(characteristics.map(\.variables)),
        ]
    }
}

/// HealthDeletedSample is a sample the server removed because HealthKit
/// deleted it, with the time it covered. The phone sent only its identifier,
/// so this is how it learns which days to work out again.
public struct HealthDeletedSample: Codable, Hashable, Sendable {
    public let healthType: String
    /// RFC 3339.
    public let startedAt: String
    public let endedAt: String

    public var startedDate: Date? { ServerTime.date(startedAt) }
    public var endedDate: Date? { ServerTime.date(endedAt) }

    public init(healthType: String, startedAt: String, endedAt: String) {
        self.healthType = healthType
        self.startedAt = startedAt
        self.endedAt = endedAt
    }
}

/// ReportHealthResult is what the server took of a batch.
public struct ReportHealthResult: Codable, Hashable, Sendable {
    public let newSampleCount: Int
    public let deletedSampleCount: Int
    public let updatedDayCount: Int
    public let deletedSamples: [HealthDeletedSample]
    /// When the person last erased their Health data on the server, RFC 3339;
    /// nil if never. When this is later than when Health was turned on in the
    /// app, the app stops (HealthSyncState.applyServerErase).
    public let healthErasedAt: String?

    public var healthErasedDate: Date? { ServerTime.date(healthErasedAt) }

    public init(
        newSampleCount: Int, deletedSampleCount: Int, updatedDayCount: Int, deletedSamples: [HealthDeletedSample] = [], healthErasedAt: String? = nil
    ) {
        self.newSampleCount = newSampleCount
        self.deletedSampleCount = deletedSampleCount
        self.updatedDayCount = updatedDayCount
        self.deletedSamples = deletedSamples
        self.healthErasedAt = healthErasedAt
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        newSampleCount = try container.decodeIfPresent(Int.self, forKey: .newSampleCount) ?? 0
        deletedSampleCount = try container.decodeIfPresent(Int.self, forKey: .deletedSampleCount) ?? 0
        updatedDayCount = try container.decodeIfPresent(Int.self, forKey: .updatedDayCount) ?? 0
        deletedSamples = try container.decodeIfPresent([HealthDeletedSample].self, forKey: .deletedSamples) ?? []
        healthErasedAt = try container.decodeIfPresent(String.self, forKey: .healthErasedAt)
    }

    /// The answers of two calls as one: the counts added, the deleted samples
    /// joined, and the later erase time.
    public func merged(with other: ReportHealthResult) -> ReportHealthResult {
        let later: String?
        switch (healthErasedDate, other.healthErasedDate) {
        case (let mine?, let theirs?): later = theirs > mine ? other.healthErasedAt : healthErasedAt
        case (nil, _?): later = other.healthErasedAt
        default: later = healthErasedAt
        }
        return ReportHealthResult(
            newSampleCount: newSampleCount + other.newSampleCount, deletedSampleCount: deletedSampleCount + other.deletedSampleCount,
            updatedDayCount: updatedDayCount + other.updatedDayCount, deletedSamples: deletedSamples + other.deletedSamples, healthErasedAt: later)
    }

    public static let nothing = ReportHealthResult(newSampleCount: 0, deletedSampleCount: 0, updatedDayCount: 0)
}

/// HealthSendOutcome is what sending a batch, with any splitting, came to.
public struct HealthSendOutcome: Equatable, Sendable {
    /// What the server answered over all the calls, added up.
    public var result: ReportHealthResult
    /// How many entries the server refused on their own (a batch of one) and
    /// that were therefore dropped.
    public var droppedEntryCount: Int
    /// The server's reasons for those, in the order met.
    public var droppedReasons: [String]
}

/// HealthBatchSender sends a batch the way the location recorder does.
public enum HealthBatchSender {
    /// Sends a batch. If the server calls it invalid without saying which
    /// entry is wrong, it is sent again in two smaller batches, down to the
    /// single entry the server refuses on its own, which is dropped: one bad
    /// entry must not take the rest sent beside it. Any other failure is
    /// thrown at once, and the caller keeps the whole batch, since the server
    /// takes a batch sent twice as the same samples.
    public static func sendSplittingInvalid(
        _ batch: HealthBatch, using send: (HealthBatch) async throws -> ReportHealthResult
    ) async throws -> HealthSendOutcome {
        do {
            return HealthSendOutcome(result: try await send(batch), droppedEntryCount: 0, droppedReasons: [])
        } catch {
            guard case .batchInvalid(let reason) = HealthReportFailure.classify(error) else { throw error }
            guard let halves = batch.split() else {
                return HealthSendOutcome(result: .nothing, droppedEntryCount: batch.entryCount, droppedReasons: [reason])
            }
            let first = try await sendSplittingInvalid(halves.first, using: send)
            let second = try await sendSplittingInvalid(halves.second, using: send)
            return HealthSendOutcome(
                result: first.result.merged(with: second.result), droppedEntryCount: first.droppedEntryCount + second.droppedEntryCount,
                droppedReasons: first.droppedReasons + second.droppedReasons)
        }
    }
}

/// HealthReportFailure is what the phone does about a batch the server did
/// not take.
public enum HealthReportFailure: Equatable, Sendable {
    /// The call did not get through, or the sign-in needs renewing: keep the
    /// batch and try again later.
    case retryLater
    /// The server named no phone of the person's with the identifier given:
    /// send the batch again without it.
    case phoneNotFound
    /// The server will not take anything from this person (not allowed to
    /// use Health): retrying cannot change the answer. Carries the server's
    /// reason.
    case refused(String)
    /// The server called this batch invalid: sending it again cannot change
    /// the answer, but smaller batches may be fine. Carries the server's
    /// reason.
    case batchInvalid(String)

    public static func classify(_ error: Error) -> HealthReportFailure {
        guard let known = error as? TeaNodeError, case .refused(let reason) = known else { return .retryLater }
        if known.isNotFound { return .phoneNotFound }
        if reason.contains("permission denied") { return .refused(reason) }
        if reason.contains("invalid arguments") { return .batchInvalid(reason) }
        return .retryLater
    }
}

/// HealthRetryGate keeps the phone from asking a server that just failed once
/// for every change HealthKit reports: after a failure, events wait this long
/// before they try again. Leaving the app, coming back to it, and signing in
/// are tried at once.
public struct HealthRetryGate: Sendable, Equatable {
    public static let waitSeconds = 30.0

    private var lastFailedAt: Date?

    public init() {}

    /// Whether an event may start a request now.
    public func canTry(at date: Date) -> Bool {
        guard let lastFailedAt else { return true }
        return date.timeIntervalSince(lastFailedAt) >= Self.waitSeconds
    }

    public mutating func didFail(at date: Date) { lastFailedAt = date }

    public mutating func didSucceed() { lastFailedAt = nil }
}

// MARK: Reading

/// HealthPeriod is how long each row of a summary covers.
public enum HealthPeriod: String, CaseIterable, Codable, Hashable, Sendable {
    case day
    case week
    case month
}

/// HealthPeriodView is one type over one period (a day, a week starting on
/// Monday, or a month), as the server adds up the day summaries.
public struct HealthPeriodView: Codable, Hashable, Sendable {
    /// "YYYY-MM-DD".
    public let startDay: String
    public let endDay: String
    public let sumValue: Double?
    public let averageValue: Double?
    public let minimumValue: Double?
    public let maximumValue: Double?
    public let sampleCount: Int
    public let durationSeconds: Double?
    /// Seconds by sleep stage, and "asleep"; see HealthDaySummaryInput.
    public let categorySeconds: [String: Double]

    private enum CodingKeys: String, CodingKey {
        case startDay, endDay, sumValue, averageValue, minimumValue, maximumValue, sampleCount, durationSeconds, categorySeconds
    }

    public init(
        startDay: String, endDay: String, sumValue: Double? = nil, averageValue: Double? = nil, minimumValue: Double? = nil,
        maximumValue: Double? = nil, sampleCount: Int, durationSeconds: Double? = nil, categorySeconds: [String: Double] = [:]
    ) {
        self.startDay = startDay
        self.endDay = endDay
        self.sumValue = sumValue
        self.averageValue = averageValue
        self.minimumValue = minimumValue
        self.maximumValue = maximumValue
        self.sampleCount = sampleCount
        self.durationSeconds = durationSeconds
        self.categorySeconds = categorySeconds
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        startDay = try container.decode(String.self, forKey: .startDay)
        endDay = try container.decode(String.self, forKey: .endDay)
        sumValue = try container.decodeIfPresent(Double.self, forKey: .sumValue)
        averageValue = try container.decodeIfPresent(Double.self, forKey: .averageValue)
        minimumValue = try container.decodeIfPresent(Double.self, forKey: .minimumValue)
        maximumValue = try container.decodeIfPresent(Double.self, forKey: .maximumValue)
        sampleCount = try container.decodeIfPresent(Int.self, forKey: .sampleCount) ?? 0
        durationSeconds = try container.decodeIfPresent(Double.self, forKey: .durationSeconds)
        categorySeconds = HealthJSON.seconds(container, .categorySeconds)
    }
}

/// HealthDaySummaryView is one type on one day, as the server keeps it: the
/// summary the phone sent.
public struct HealthDaySummaryView: Codable, Hashable, Sendable {
    /// "YYYY-MM-DD", the phone's calendar day.
    public let summaryDay: String
    public let quantityUnit: String
    public let sumValue: Double?
    public let averageValue: Double?
    public let minimumValue: Double?
    public let maximumValue: Double?
    public let sampleCount: Int
    public let durationSeconds: Double?
    /// Seconds by sleep stage, and "asleep"; see HealthDaySummaryInput.
    public let categorySeconds: [String: Double]

    private enum CodingKeys: String, CodingKey {
        case summaryDay, quantityUnit, sumValue, averageValue, minimumValue, maximumValue, sampleCount, durationSeconds, categorySeconds
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        summaryDay = try container.decode(String.self, forKey: .summaryDay)
        quantityUnit = try container.decodeIfPresent(String.self, forKey: .quantityUnit) ?? ""
        sumValue = try container.decodeIfPresent(Double.self, forKey: .sumValue)
        averageValue = try container.decodeIfPresent(Double.self, forKey: .averageValue)
        minimumValue = try container.decodeIfPresent(Double.self, forKey: .minimumValue)
        maximumValue = try container.decodeIfPresent(Double.self, forKey: .maximumValue)
        sampleCount = try container.decodeIfPresent(Int.self, forKey: .sampleCount) ?? 0
        durationSeconds = try container.decodeIfPresent(Double.self, forKey: .durationSeconds)
        categorySeconds = HealthJSON.seconds(container, .categorySeconds)
    }
}

/// HealthTypeView is one type the person (or somebody sharing with them) has
/// data for.
public struct HealthTypeView: Codable, Identifiable, Hashable, Sendable {
    public let healthType: String
    public let healthCategory: String
    /// The name in English; the screens put the types they know into the
    /// person's language and fall back to this.
    public let typeName: String
    public let valueKind: String
    public let quantityUnit: String
    public let summaryKind: String?
    /// "YYYY-MM-DD".
    public let firstDay: String?
    public let lastDay: String?
    public let sampleCount: Int
    public let latestDaySummary: HealthDaySummaryView?

    public var id: String { healthType }

    /// The group, if it is one the app knows.
    public var healthCategoryValue: HealthCategory { HealthCategory(rawValue: healthCategory) ?? .other }
    public var valueKindValue: HealthValueKind? { HealthValueKind(rawValue: valueKind) }
    public var summaryKindValue: HealthSummaryKind? { summaryKind.flatMap { HealthSummaryKind(rawValue: $0) } }
}

/// HealthSummaryView is one type over a range, a row for each period.
public struct HealthSummaryView: Codable, Hashable, Sendable {
    public let healthType: String
    public let quantityUnit: String
    public let summaryKind: String?
    public let periods: [HealthPeriodView]

    public var summaryKindValue: HealthSummaryKind? { summaryKind.flatMap { HealthSummaryKind(rawValue: $0) } }
}

/// HealthSampleView is one reading, as the server keeps it. Only the person's
/// own; a share never reaches samples.
public struct HealthSampleView: Codable, Identifiable, Hashable, Sendable {
    public let healthSampleId: String
    public let healthType: String
    /// RFC 3339.
    public let startedAt: String
    public let endedAt: String
    public let quantityValue: Double?
    public let quantityUnit: String
    public let categoryValue: Int?
    public let workoutActivityType: String
    public let sourceName: String
    public let sourceBundleId: String
    public let deviceName: String
    public let sampleMetadata: [String: JSONValue]
    public let receivedAt: String
    public let hasRoute: Bool

    public var id: String { healthSampleId }
    public var startedDate: Date? { ServerTime.date(startedAt) }
    public var endedDate: Date? { ServerTime.date(endedAt) }

    private enum CodingKeys: String, CodingKey {
        case healthSampleId, healthType, startedAt, endedAt, quantityValue, quantityUnit, categoryValue, workoutActivityType, sourceName
        case sourceBundleId, deviceName, sampleMetadataJson, receivedAt, hasRoute
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        healthSampleId = try container.decode(String.self, forKey: .healthSampleId)
        healthType = try container.decode(String.self, forKey: .healthType)
        startedAt = try container.decode(String.self, forKey: .startedAt)
        endedAt = try container.decode(String.self, forKey: .endedAt)
        quantityValue = try container.decodeIfPresent(Double.self, forKey: .quantityValue)
        quantityUnit = try container.decodeIfPresent(String.self, forKey: .quantityUnit) ?? ""
        categoryValue = try container.decodeIfPresent(Int.self, forKey: .categoryValue)
        workoutActivityType = try container.decodeIfPresent(String.self, forKey: .workoutActivityType) ?? ""
        sourceName = try container.decodeIfPresent(String.self, forKey: .sourceName) ?? ""
        sourceBundleId = try container.decodeIfPresent(String.self, forKey: .sourceBundleId) ?? ""
        deviceName = try container.decodeIfPresent(String.self, forKey: .deviceName) ?? ""
        sampleMetadata = HealthJSON.object(try container.decodeIfPresent(String.self, forKey: .sampleMetadataJson), as: [String: JSONValue].self) ?? [:]
        receivedAt = try container.decodeIfPresent(String.self, forKey: .receivedAt) ?? ""
        hasRoute = try container.decodeIfPresent(Bool.self, forKey: .hasRoute) ?? false
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(healthSampleId, forKey: .healthSampleId)
        try container.encode(healthType, forKey: .healthType)
        try container.encode(startedAt, forKey: .startedAt)
        try container.encode(endedAt, forKey: .endedAt)
        try container.encodeIfPresent(quantityValue, forKey: .quantityValue)
        try container.encode(quantityUnit, forKey: .quantityUnit)
        try container.encodeIfPresent(categoryValue, forKey: .categoryValue)
        try container.encode(workoutActivityType, forKey: .workoutActivityType)
        try container.encode(sourceName, forKey: .sourceName)
        try container.encode(sourceBundleId, forKey: .sourceBundleId)
        try container.encode(deviceName, forKey: .deviceName)
        try container.encode(HealthJSON.text(sampleMetadata), forKey: .sampleMetadataJson)
        try container.encode(receivedAt, forKey: .receivedAt)
        try container.encode(hasRoute, forKey: .hasRoute)
    }
}

/// HealthSamplePage is a page of readings.
public struct HealthSamplePage: Codable, Hashable, Sendable {
    public let samples: [HealthSampleView]
    public let totalCount: Int
    /// Where the next page begins; nil on the last.
    public let nextOffset: Int?
}

/// HealthRouteView is the path of a workout.
public struct HealthRouteView: Codable, Hashable, Sendable {
    public let healthSampleId: String
    /// [secondsFromStart, latitudeDegrees, longitudeDegrees, altitudeMeters] rows.
    public let routePoints: [[Double]]

    public var points: [HealthRoutePoint] { routePoints.compactMap { HealthRoutePoint(row: $0) } }
}

/// HealthCharacteristicView is one fact about the person, as kept.
public struct HealthCharacteristicView: Codable, Hashable, Sendable {
    public let healthType: String
    public let characteristicValue: String
}

/// HealthSettingsView is the person's Health settings and what the server holds.
public struct HealthSettingsView: Codable, Hashable, Sendable {
    /// Whether the agent may read Health data. Off until the person turns it on.
    public let canAgentReadHealth: Bool
    /// When the person last erased their Health data, RFC 3339; nil if never.
    public let healthErasedAt: String?
    public let sampleCount: Int
    public let lastReceivedAt: String?
    /// The provider the agent's reading goes to, named in the switch's words.
    public let modelProviderName: String
    public let characteristics: [HealthCharacteristicView]

    public var healthErasedDate: Date? { ServerTime.date(healthErasedAt) }
    public var lastReceivedDate: Date? { ServerTime.date(lastReceivedAt) }
}

/// EraseHealthDataResult is what an erase removed.
public struct EraseHealthDataResult: Codable, Hashable, Sendable {
    public let erasedSampleCount: Int
}

/// HealthShareView is one share of Health data by category: a person sharing
/// with the caller, or the caller sharing with a person or a circle.
public struct HealthShareView: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let ownerUserId: String
    public let ownerUsername: String
    public let ownerName: String?
    public let granteeUserId: String?
    public let granteeUsername: String?
    public let granteeName: String?
    public let granteeCircleId: String?
    public let granteeCircleName: String?
    public let healthCategories: [String]
    public let createdAt: String

    /// The categories that are ones the app knows, in the Health app's order.
    public var sharedCategories: [HealthCategory] {
        HealthCategory.allCases.filter { healthCategories.contains($0.rawValue) }
    }
}

// MARK: Documents

extension Documents {
    // Each shared list of fields is written out in full, nested ones too: the
    // server's test of the app's documents fills in one level of them.
    static let healthPeriodFields = """
        startDay endDay sumValue averageValue minimumValue maximumValue sampleCount durationSeconds categorySeconds
        """
    static let healthTypeFields = """
        healthType healthCategory typeName valueKind quantityUnit summaryKind firstDay lastDay sampleCount
        latestDaySummary { summaryDay quantityUnit sumValue averageValue minimumValue maximumValue sampleCount durationSeconds categorySeconds }
        """
    static let healthSampleFields = """
        healthSampleId healthType startedAt endedAt quantityValue quantityUnit categoryValue workoutActivityType sourceName sourceBundleId deviceName
        sampleMetadataJson receivedAt hasRoute
        """
    static let healthSettingsFields = """
        canAgentReadHealth healthErasedAt sampleCount lastReceivedAt modelProviderName characteristics { healthType characteristicValue }
        """
    static let healthShareFields = """
        id ownerUserId ownerUsername ownerName granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName healthCategories createdAt
        """

    public static let reportHealth = """
        mutation ($agentPhoneId: String, $samples: [HealthSampleInput!]!, $deletedHealthSampleIds: [String!]!, $daySummaries: [HealthDaySummaryInput!]!, $routes: [HealthRouteInput!]!, $characteristics: [HealthCharacteristicInput!]!) {
          ReportHealth(agentPhoneId: $agentPhoneId, samples: $samples, deletedHealthSampleIds: $deletedHealthSampleIds, daySummaries: $daySummaries, routes: $routes, characteristics: $characteristics) {
            newSampleCount deletedSampleCount updatedDayCount deletedSamples { healthType startedAt endedAt } healthErasedAt
          }
        }
        """
    public static let healthTypes = """
        query ($ownerUserId: String) { ListHealthTypes(ownerUserId: $ownerUserId) { \(healthTypeFields) } }
        """
    public static let healthSummary = """
        query ($healthType: String!, $fromDay: String!, $toDay: String!, $period: String!, $ownerUserId: String) {
          ReadHealthSummary(healthType: $healthType, fromDay: $fromDay, toDay: $toDay, period: $period, ownerUserId: $ownerUserId) {
            healthType quantityUnit summaryKind periods { \(healthPeriodFields) }
          }
        }
        """
    public static let healthSamples = """
        query ($healthType: String!, $from: DateTime, $to: DateTime, $limitCount: Int, $offset: Int) {
          ListHealthSamples(healthType: $healthType, from: $from, to: $to, limitCount: $limitCount, offset: $offset) {
            samples { \(healthSampleFields) } totalCount nextOffset
          }
        }
        """
    public static let healthRoute = """
        query ($healthSampleId: String!) { ReadHealthRoute(healthSampleId: $healthSampleId) { healthSampleId routePoints } }
        """
    public static let healthSettings = """
        query { ReadHealthSettings { \(healthSettingsFields) } }
        """
    public static let updateHealthSettings = """
        mutation ($canAgentReadHealth: Boolean!) { UpdateHealthSettings(canAgentReadHealth: $canAgentReadHealth) { \(healthSettingsFields) } }
        """
    public static let eraseHealthData = """
        mutation ($healthType: String) { EraseHealthData(healthType: $healthType) { erasedSampleCount } }
        """

    // Sharing Health by category with a circle or a person, and seeing the
    // people who share with the caller.
    public static let healthShares = """
        query { ListHealthShares { \(healthShareFields) } }
        """
    public static let setHealthShare = """
        mutation ($granteeUserId: String, $granteeCircleId: String, $healthCategories: [String!]!) {
          SetHealthShare(granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, healthCategories: $healthCategories) { \(healthShareFields) }
        }
        """

    /// The Health documents, for the server's schema check.
    public static let healthDocuments: [String: String] = [
        "reportHealth": reportHealth, "healthTypes": healthTypes, "healthSummary": healthSummary, "healthSamples": healthSamples,
        "healthRoute": healthRoute, "healthSettings": healthSettings, "updateHealthSettings": updateHealthSettings,
        "eraseHealthData": eraseHealthData, "healthShares": healthShares, "setHealthShare": setHealthShare,
    ]
}

extension Connection {
    /// Hands the server a batch. Safe to send twice: the server knows a sample
    /// by HealthKit's identifier and replaces a day's summary.
    public func reportHealth(_ batch: HealthBatch, agentPhoneId: String? = nil) async throws -> ReportHealthResult {
        struct Answer: Decodable, Sendable { let ReportHealth: ReportHealthResult }
        var variables = batch.variables
        if let agentPhoneId { variables["agentPhoneId"] = .string(agentPhoneId) }
        return try await execute(Documents.reportHealth, variables: variables, as: Answer.self).ReportHealth
    }

    /// The types the person has data for, or somebody sharing with them has
    /// (only the ones in the categories shared).
    public func healthTypes(ownerUserId: String? = nil) async throws -> [HealthTypeView] {
        struct Answer: Decodable, Sendable { let ListHealthTypes: [HealthTypeView] }
        var variables: [String: JSONValue] = [:]
        if let ownerUserId { variables["ownerUserId"] = .string(ownerUserId) }
        return try await execute(Documents.healthTypes, variables: variables, as: Answer.self).ListHealthTypes
    }

    /// One type over a range of days ("YYYY-MM-DD"), a row for each period.
    public func healthSummary(
        healthType: String, fromDay: String, toDay: String, period: HealthPeriod = .day, ownerUserId: String? = nil
    ) async throws -> HealthSummaryView {
        struct Answer: Decodable, Sendable { let ReadHealthSummary: HealthSummaryView }
        var variables: [String: JSONValue] = [
            "healthType": .string(healthType), "fromDay": .string(fromDay), "toDay": .string(toDay), "period": .string(period.rawValue),
        ]
        if let ownerUserId { variables["ownerUserId"] = .string(ownerUserId) }
        return try await execute(Documents.healthSummary, variables: variables, as: Answer.self).ReadHealthSummary
    }

    /// A page of the person's own readings of a type, newest first.
    public func healthSamples(healthType: String, from: Date? = nil, to: Date? = nil, limitCount: Int? = nil, offset: Int = 0) async throws
        -> HealthSamplePage
    {
        struct Answer: Decodable, Sendable { let ListHealthSamples: HealthSamplePage }
        var variables: [String: JSONValue] = ["healthType": .string(healthType), "offset": .number(Double(offset))]
        if let from { variables["from"] = .string(CalendarDates.text(from)) }
        if let to { variables["to"] = .string(CalendarDates.text(to)) }
        if let limitCount { variables["limitCount"] = .number(Double(limitCount)) }
        return try await execute(Documents.healthSamples, variables: variables, as: Answer.self).ListHealthSamples
    }

    /// The path of one of the person's own workouts.
    public func healthRoute(healthSampleId: String) async throws -> HealthRouteView {
        struct Answer: Decodable, Sendable { let ReadHealthRoute: HealthRouteView }
        return try await execute(Documents.healthRoute, variables: ["healthSampleId": .string(healthSampleId)], as: Answer.self).ReadHealthRoute
    }

    public func healthSettings() async throws -> HealthSettingsView {
        struct Answer: Decodable, Sendable { let ReadHealthSettings: HealthSettingsView }
        return try await execute(Documents.healthSettings, as: Answer.self).ReadHealthSettings
    }

    /// Lets the agent read Health data, or stops it.
    @discardableResult
    public func updateHealthSettings(canAgentReadHealth: Bool) async throws -> HealthSettingsView {
        struct Answer: Decodable, Sendable { let UpdateHealthSettings: HealthSettingsView }
        return try await execute(
            Documents.updateHealthSettings, variables: ["canAgentReadHealth": .bool(canAgentReadHealth)], as: Answer.self
        ).UpdateHealthSettings
    }

    /// Erases one type's samples, summaries and routes, or with no type
    /// everything Health holds for the person (and the phone then stops).
    @discardableResult
    public func eraseHealthData(healthType: String? = nil) async throws -> EraseHealthDataResult {
        struct Answer: Decodable, Sendable { let EraseHealthData: EraseHealthDataResult }
        var variables: [String: JSONValue] = [:]
        if let healthType { variables["healthType"] = .string(healthType) }
        return try await execute(Documents.eraseHealthData, variables: variables, as: Answer.self).EraseHealthData
    }

    // MARK: Sharing

    /// The caller's own shares, and the ones made with them.
    public func healthShares() async throws -> [HealthShareView] {
        struct Answer: Decodable, Sendable { let ListHealthShares: [HealthShareView] }
        return try await execute(Documents.healthShares, as: Answer.self).ListHealthShares
    }

    /// Shares the categories with a person or a circle (one of the two); an
    /// empty list takes the share back, and then the answer is nil.
    @discardableResult
    public func setHealthShare(granteeUserId: String?, granteeCircleId: String?, healthCategories: [HealthCategory]) async throws -> HealthShareView? {
        struct Answer: Decodable, Sendable { let SetHealthShare: HealthShareView? }
        return try await execute(
            Documents.setHealthShare,
            variables: [
                "granteeUserId": granteeUserId.map { .string($0) } ?? .null, "granteeCircleId": granteeCircleId.map { .string($0) } ?? .null,
                "healthCategories": .array(healthCategories.map { .string($0.rawValue) }),
            ], as: Answer.self
        ).SetHealthShare
    }
}
