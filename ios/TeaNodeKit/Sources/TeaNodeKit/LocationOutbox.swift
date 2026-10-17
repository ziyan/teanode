import Foundation

// What the phone does with where it has been, before and after the server is
// told: which positions are worth keeping, how a visit is named so that its
// arrival and its departure are one, what waits to be sent, and which places
// are watched for the person arriving. None of it needs Core Location, so it
// is tested here; LocationRecorder.swift in the app feeds it.

/// LocationGeometry is the arithmetic on coordinates the recorder needs.
public enum LocationGeometry {
    /// Whether the coordinates are on the earth; the server refuses any that
    /// are not, and one bad point must not hold up a batch.
    public static func isOnEarth(latitudeDegrees: Double, longitudeDegrees: Double) -> Bool {
        latitudeDegrees.isFinite && longitudeDegrees.isFinite && (-90...90).contains(latitudeDegrees) && (-180...180).contains(longitudeDegrees)
    }

    /// The distance along the earth's surface between two coordinates, in
    /// meters (the haversine formula, good to a fraction of a percent).
    public static func distanceMeters(
        fromLatitudeDegrees: Double, fromLongitudeDegrees: Double, toLatitudeDegrees: Double, toLongitudeDegrees: Double
    ) -> Double {
        let earthRadiusMeters = 6_371_000.0
        let fromLatitudeRadians = fromLatitudeDegrees * .pi / 180
        let toLatitudeRadians = toLatitudeDegrees * .pi / 180
        let latitudeChange = (toLatitudeDegrees - fromLatitudeDegrees) * .pi / 180
        let longitudeChange = (toLongitudeDegrees - fromLongitudeDegrees) * .pi / 180
        let half =
            sin(latitudeChange / 2) * sin(latitudeChange / 2)
            + cos(fromLatitudeRadians) * cos(toLatitudeRadians) * sin(longitudeChange / 2) * sin(longitudeChange / 2)
        return 2 * earthRadiusMeters * asin(min(1, sqrt(half)))
    }
}

/// LocationWords are the plain facts about OpenStreetMap's tags that the
/// screens put into the person's language.
public enum LocationWords {
    /// The part of an OpenStreetMap tag after the equals sign, as the kind of
    /// thing: "amenity=cafe" is "cafe", "leisure=playground" is "playground".
    public static func categoryValue(_ osmCategory: String) -> String {
        guard let equals = osmCategory.firstIndex(of: "=") else { return osmCategory }
        return String(osmCategory[osmCategory.index(after: equals)...])
    }
}

/// LocationVisitKey names a visit the way the phone does, so that a report
/// sent twice, or an arrival followed by its departure, is one visit.
public enum LocationVisitKey {
    /// A visit iOS reported. The same visit comes back with its departure,
    /// carrying the same arrival, so the key is made from where and when it
    /// began. A visit whose arrival iOS did not see (nil) is named from when
    /// it ended.
    public static func visit(latitudeDegrees: Double, longitudeDegrees: Double, arrivedAt: Date?, departedAt: Date?) -> String {
        let place = String(format: "%.3f:%.3f", latitudeDegrees, longitudeDegrees)
        if let arrivedAt { return "visit:\(place):a\(Int(arrivedAt.timeIntervalSince1970))" }
        return "visit:\(place):d\(Int((departedAt ?? Date()).timeIntervalSince1970))"
    }

    /// A visit to a place the person named, begun when they came in.
    public static func region(placeId: String, arrivedAt: Date) -> String {
        "region:\(placeId):a\(Int(arrivedAt.timeIntervalSince1970))"
    }
}

/// LocationPointFilter decides which live positions are kept: the phone
/// reports where the person has been, not every reading, so a point is kept
/// when they have moved at least 25 meters from the last one kept, or a
/// minute has passed and they are moving. Nothing is kept while the phone
/// says it is stationary.
public struct LocationPointFilter: Sendable, Equatable {
    public static let distanceMetersLeast = 25.0
    public static let secondsLeast = 60.0
    public static let speedMetersPerSecondLeast = 0.5

    private struct Kept: Sendable, Equatable {
        let latitudeDegrees: Double
        let longitudeDegrees: Double
        let measuredAt: Date
    }

    private var lastKept: Kept?

    public init() {}

    /// Forgets the last point kept, so the next reading is kept: when the app
    /// comes back to the foreground after a while.
    public mutating func reset() { lastKept = nil }

    /// Whether to keep this reading, remembering it if so.
    public mutating func shouldKeep(
        latitudeDegrees: Double, longitudeDegrees: Double, speedMetersPerSecond: Double?, measuredAt: Date, isStationary: Bool
    ) -> Bool {
        guard !isStationary else { return false }
        if let lastKept {
            let movedMeters = LocationGeometry.distanceMeters(
                fromLatitudeDegrees: lastKept.latitudeDegrees, fromLongitudeDegrees: lastKept.longitudeDegrees,
                toLatitudeDegrees: latitudeDegrees, toLongitudeDegrees: longitudeDegrees)
            let waitedSeconds = measuredAt.timeIntervalSince(lastKept.measuredAt)
            let isMoving = (speedMetersPerSecond ?? 0) > Self.speedMetersPerSecondLeast
            guard movedMeters >= Self.distanceMetersLeast || (waitedSeconds >= Self.secondsLeast && isMoving) else { return false }
        }
        lastKept = Kept(latitudeDegrees: latitudeDegrees, longitudeDegrees: longitudeDegrees, measuredAt: measuredAt)
        return true
    }
}

/// ReportedPoint is one position waiting to be reported.
public struct ReportedPoint: Codable, Hashable, Sendable {
    /// Its place in the outbox, which tells what a batch took with it.
    public var sequence: Int
    public let latitudeDegrees: Double
    public let longitudeDegrees: Double
    public let accuracyMeters: Double
    public let altitudeMeters: Double?
    public let speedMetersPerSecond: Double?
    public let courseDegrees: Double?
    public let measuredAt: Date

    public init(
        sequence: Int = 0, latitudeDegrees: Double, longitudeDegrees: Double, accuracyMeters: Double, altitudeMeters: Double? = nil,
        speedMetersPerSecond: Double? = nil, courseDegrees: Double? = nil, measuredAt: Date
    ) {
        self.sequence = sequence
        self.latitudeDegrees = latitudeDegrees
        self.longitudeDegrees = longitudeDegrees
        self.accuracyMeters = accuracyMeters
        self.altitudeMeters = altitudeMeters
        self.speedMetersPerSecond = speedMetersPerSecond
        self.courseDegrees = courseDegrees
        self.measuredAt = measuredAt
    }

    var variables: JSONValue {
        var fields: [String: JSONValue] = [
            "latitudeDegrees": .number(latitudeDegrees), "longitudeDegrees": .number(longitudeDegrees), "accuracyMeters": .number(accuracyMeters),
            "measuredAt": .string(CalendarDates.text(measuredAt)),
        ]
        if let altitudeMeters { fields["altitudeMeters"] = .number(altitudeMeters) }
        if let speedMetersPerSecond { fields["speedMetersPerSecond"] = .number(speedMetersPerSecond) }
        if let courseDegrees { fields["courseDegrees"] = .number(courseDegrees) }
        return .object(fields)
    }
}

/// ReportedVisit is one arrival, departure or both waiting to be reported.
public struct ReportedVisit: Codable, Hashable, Sendable {
    public var sequence: Int
    public let clientVisitKey: String
    public let latitudeDegrees: Double
    public let longitudeDegrees: Double
    public let accuracyMeters: Double
    public let arrivedAt: Date?
    public let departedAt: Date?
    /// "visit" from iOS visit monitoring, "region" from entering a place the
    /// person named.
    public let visitSource: String
    /// For "region": the place that was watched.
    public let placeId: String?

    public init(
        sequence: Int = 0, clientVisitKey: String, latitudeDegrees: Double, longitudeDegrees: Double, accuracyMeters: Double, arrivedAt: Date?,
        departedAt: Date?, visitSource: String, placeId: String? = nil
    ) {
        self.sequence = sequence
        self.clientVisitKey = clientVisitKey
        self.latitudeDegrees = latitudeDegrees
        self.longitudeDegrees = longitudeDegrees
        self.accuracyMeters = accuracyMeters
        self.arrivedAt = arrivedAt
        self.departedAt = departedAt
        self.visitSource = visitSource
        self.placeId = placeId
    }

    var variables: JSONValue {
        var fields: [String: JSONValue] = [
            "clientVisitKey": .string(clientVisitKey), "latitudeDegrees": .number(latitudeDegrees), "longitudeDegrees": .number(longitudeDegrees),
            "accuracyMeters": .number(accuracyMeters), "visitSource": .string(visitSource),
        ]
        if let arrivedAt { fields["arrivedAt"] = .string(CalendarDates.text(arrivedAt)) }
        if let departedAt { fields["departedAt"] = .string(CalendarDates.text(departedAt)) }
        if let placeId { fields["placeId"] = .string(placeId) }
        return .object(fields)
    }
}

/// LocationBatch is what one ReportLocation call carries: the oldest points
/// and visits in the outbox, at most 500 and 50.
public struct LocationBatch: Sendable, Equatable {
    public let points: [ReportedPoint]
    public let visits: [ReportedVisit]

    public var isEmpty: Bool { points.isEmpty && visits.isEmpty }

    public var variables: [String: JSONValue] {
        ["points": .array(points.map(\.variables)), "visits": .array(visits.map(\.variables))]
    }
}

/// OpenRegionVisit is a visit to a place the person named that has begun and
/// not ended, kept so a departure after the app was closed finds its arrival.
public struct OpenRegionVisit: Codable, Hashable, Sendable {
    public let clientVisitKey: String
    public let latitudeDegrees: Double
    public let longitudeDegrees: Double
    public let arrivedAt: Date

    public init(clientVisitKey: String, latitudeDegrees: Double, longitudeDegrees: Double, arrivedAt: Date) {
        self.clientVisitKey = clientVisitKey
        self.latitudeDegrees = latitudeDegrees
        self.longitudeDegrees = longitudeDegrees
        self.arrivedAt = arrivedAt
    }
}

/// LocationOutbox is everything the phone has noticed that the server has not
/// yet been told. It is kept in a file, so a closed app, a lost connection or
/// a refused sign-in loses nothing; a batch leaves it only after the server
/// answered.
public struct LocationOutbox: Codable, Sendable, Equatable {
    /// The most points kept; the oldest go first. Visits are never dropped.
    public static let pointCountMost = 20_000
    public static let pointCountPerBatch = 500
    public static let visitCountPerBatch = 50

    /// Whose it is. What was noticed while another account was in use is not
    /// reported as the person now signed in.
    public var accountKey: String?
    private var nextSequence = 1
    public private(set) var points: [ReportedPoint] = []
    public private(set) var visits: [ReportedVisit] = []
    /// The visits begun and not ended, by the place they are at.
    public private(set) var openRegionVisits: [String: OpenRegionVisit] = [:]

    public init(accountKey: String? = nil) { self.accountKey = accountKey }

    public var isEmpty: Bool { points.isEmpty && visits.isEmpty }

    /// Queues a position. One that is not on the earth, or that the phone
    /// says it could not measure (a negative accuracy), is dropped: the
    /// server would refuse the whole batch for it.
    public mutating func addPoint(_ point: ReportedPoint) {
        guard LocationGeometry.isOnEarth(latitudeDegrees: point.latitudeDegrees, longitudeDegrees: point.longitudeDegrees),
            point.accuracyMeters >= 0
        else { return }
        var queued = point
        queued.sequence = nextSequence
        nextSequence += 1
        points.append(queued)
        if points.count > Self.pointCountMost { points.removeFirst(points.count - Self.pointCountMost) }
    }

    /// Queues an arrival, a departure or a whole visit.
    /// One with neither an arrival nor a departure (iOS reports a visit with
    /// both dates unknown) is dropped: the server would refuse the whole batch.
    public mutating func addVisit(_ visit: ReportedVisit) {
        guard LocationGeometry.isOnEarth(latitudeDegrees: visit.latitudeDegrees, longitudeDegrees: visit.longitudeDegrees),
            visit.arrivedAt != nil || visit.departedAt != nil
        else { return }
        var queued = visit
        queued.sequence = nextSequence
        nextSequence += 1
        visits.append(queued)
    }

    /// Notes that the person came into a place they named, and queues the
    /// arrival. If they are already counted as there, the departure was not
    /// seen (monitoring was off, or the phone was off), so that visit is
    /// ended at this arrival and a new one begins; leaving it open would
    /// hide every later arrival.
    @discardableResult
    public mutating func arrive(placeId: String, latitudeDegrees: Double, longitudeDegrees: Double, at date: Date) -> Bool {
        depart(placeId: placeId, at: date)
        let key = LocationVisitKey.region(placeId: placeId, arrivedAt: date)
        openRegionVisits[placeId] = OpenRegionVisit(
            clientVisitKey: key, latitudeDegrees: latitudeDegrees, longitudeDegrees: longitudeDegrees, arrivedAt: date)
        addVisit(
            ReportedVisit(
                clientVisitKey: key, latitudeDegrees: latitudeDegrees, longitudeDegrees: longitudeDegrees, accuracyMeters: 0, arrivedAt: date,
                departedAt: nil, visitSource: "region", placeId: placeId))
        return true
    }

    /// Notes that the person went out of a place they named, and queues the
    /// departure under the key of the arrival. Does nothing if no arrival was
    /// seen.
    @discardableResult
    public mutating func depart(placeId: String, at date: Date) -> Bool {
        guard let open = openRegionVisits.removeValue(forKey: placeId) else { return false }
        addVisit(
            ReportedVisit(
                clientVisitKey: open.clientVisitKey, latitudeDegrees: open.latitudeDegrees, longitudeDegrees: open.longitudeDegrees,
                accuracyMeters: 0, arrivedAt: nil, departedAt: date, visitSource: "region", placeId: placeId))
        return true
    }

    /// Forgets the visits begun, for a place that is no longer watched.
    public mutating func forgetOpenRegionVisit(placeId: String) { openRegionVisits[placeId] = nil }

    /// Ends every visit begun, as of a date: when monitoring stops, nothing
    /// will say the person left.
    public mutating func departEverywhere(at date: Date) {
        for placeId in openRegionVisits.keys.sorted() { depart(placeId: placeId, at: date) }
    }

    /// Throws away what waits to be sent in a range (with neither end, all of
    /// it), keeping the visits begun: the person is still where they are. For
    /// after the history is erased on the server, so that nothing erased is
    /// sent back. A visit is in the range by when it began, or when it ended
    /// if the beginning was not seen.
    public mutating func discardUnsent(from: Date? = nil, until: Date? = nil) {
        func isInRange(_ date: Date?) -> Bool {
            guard let date else { return from == nil && until == nil }
            if let from, date < from { return false }
            if let until, date >= until { return false }
            return true
        }
        points.removeAll { isInRange($0.measuredAt) }
        visits.removeAll { isInRange($0.arrivedAt ?? $0.departedAt) }
    }

    /// The oldest points and visits, as many as one call takes.
    public func nextBatch() -> LocationBatch {
        LocationBatch(points: Array(points.prefix(Self.pointCountPerBatch)), visits: Array(visits.prefix(Self.visitCountPerBatch)))
    }

    /// Takes out what a batch carried, once the server has answered. Entries
    /// are known by their place in the queue, so ones added meanwhile stay,
    /// and ones dropped meanwhile for being the oldest do not take newer ones
    /// with them.
    public mutating func remove(_ batch: LocationBatch) {
        if let last = batch.points.last?.sequence { points.removeAll { $0.sequence <= last } }
        if let last = batch.visits.last?.sequence { visits.removeAll { $0.sequence <= last } }
    }

    /// The name of the file an account's outbox is kept in: one file for each
    /// account, so that switching accounts neither sends one person's
    /// positions as another nor loses what has not been sent. The key is
    /// percent-encoded, so no key can name another file or a directory.
    public static func fileName(accountKey: String) -> String {
        let safe = accountKey.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? ""
        return "outbox-\(safe).json"
    }

    static func makeEncoder() -> JSONEncoder {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        encoder.outputFormatting = [.sortedKeys]
        return encoder
    }

    static func makeDecoder() -> JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }

    /// The file's contents.
    public func encoded() throws -> Data { try Self.makeEncoder().encode(self) }

    /// An outbox read from a file's contents.
    public static func decoded(from data: Data) throws -> LocationOutbox { try makeDecoder().decode(LocationOutbox.self, from: data) }
}

/// LocationReportFailure is what the phone does about a batch the server did
/// not take.
public enum LocationReportFailure: Equatable, Sendable {
    /// The call did not get through, or the sign-in needs renewing: keep the
    /// batch and try again later.
    case retryLater
    /// The server named no phone of the person's with the identifier given:
    /// send the batch again without it.
    case phoneNotFound
    /// The server will not take anything from this person (not allowed to
    /// use Location): retrying cannot change the answer. Carries the server's
    /// reason.
    case refused(String)
    /// The server called this batch invalid: sending it again cannot change
    /// the answer, but later batches may be fine, so it is dropped and
    /// recording goes on. Carries the server's reason.
    case batchInvalid(String)

    public static func classify(_ error: Error) -> LocationReportFailure {
        guard let known = error as? TeaNodeError, case .refused(let reason) = known else { return .retryLater }
        if known.isNotFound { return .phoneNotFound }
        if reason.contains("permission denied") { return .refused(reason) }
        if reason.contains("invalid arguments") { return .batchInvalid(reason) }
        return .retryLater
    }
}

/// LocationRetryGate keeps the phone from asking a server that just failed
/// once for every position noticed: after a failure, events wait this long
/// before they try again. Leaving the app, coming back to it, and signing in
/// are tried at once.
public struct LocationRetryGate: Sendable, Equatable {
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

/// WatchedPlace is a place the phone asks iOS to tell it about the person
/// entering and leaving.
public struct WatchedPlace: Hashable, Sendable {
    public let placeId: String
    public let latitudeDegrees: Double
    public let longitudeDegrees: Double
    public let radiusMeters: Double

    public init(placeId: String, latitudeDegrees: Double, longitudeDegrees: Double, radiusMeters: Double) {
        self.placeId = placeId
        self.latitudeDegrees = latitudeDegrees
        self.longitudeDegrees = longitudeDegrees
        self.radiusMeters = radiusMeters
    }
}

/// LocationPlaceWatch chooses which places are watched. iOS allows twenty
/// regions at a time; the person's labeled and named places are the ones
/// that matter to them, so those nearest to where they last were are watched,
/// each at least 100 meters wide, since a smaller circle is not reliably
/// noticed.
public enum LocationPlaceWatch {
    public static let countMost = 20
    public static let radiusMetersLeast = 100.0

    public static func choose(from places: [LocationPlace], nearLatitudeDegrees: Double?, nearLongitudeDegrees: Double?) -> [WatchedPlace] {
        let candidates = places.filter {
            $0.isOfPersonsOwn && LocationGeometry.isOnEarth(latitudeDegrees: $0.latitudeDegrees, longitudeDegrees: $0.longitudeDegrees)
        }
        let ordered: [LocationPlace]
        if let nearLatitudeDegrees, let nearLongitudeDegrees {
            ordered = candidates.sorted { first, second in
                distance(first, nearLatitudeDegrees, nearLongitudeDegrees) < distance(second, nearLatitudeDegrees, nearLongitudeDegrees)
            }
        } else {
            // With no position yet, the labeled ones first: home and work.
            ordered = candidates.sorted { ($0.label != nil ? 0 : 1, $0.id) < ($1.label != nil ? 0 : 1, $1.id) }
        }
        return ordered.prefix(countMost).map {
            WatchedPlace(
                placeId: $0.id, latitudeDegrees: $0.latitudeDegrees, longitudeDegrees: $0.longitudeDegrees,
                radiusMeters: max($0.radiusMeters, radiusMetersLeast))
        }
    }

    private static func distance(_ place: LocationPlace, _ latitudeDegrees: Double, _ longitudeDegrees: Double) -> Double {
        LocationGeometry.distanceMeters(
            fromLatitudeDegrees: latitudeDegrees, fromLongitudeDegrees: longitudeDegrees, toLatitudeDegrees: place.latitudeDegrees,
            toLongitudeDegrees: place.longitudeDegrees)
    }
}

/// LocationVisitDay is the visits that began on one day, newest first.
public struct LocationVisitDay: Identifiable, Hashable, Sendable {
    /// Midnight of the day, in the zone it was grouped in.
    public let startOfDay: Date
    public let visits: [PlaceVisit]

    public var id: Date { startOfDay }
}

public enum LocationDays {
    /// Groups visits by the day they began (or ended, when the arrival was
    /// not seen), newest day first and newest visit first within it. A visit
    /// with no time at all is left out: it cannot be placed.
    public static func group(_ visits: [PlaceVisit], calendar: Calendar = .current) -> [LocationVisitDay] {
        var byDay: [Date: [PlaceVisit]] = [:]
        for visit in visits {
            guard let anchor = visit.anchorDate else { continue }
            byDay[calendar.startOfDay(for: anchor), default: []].append(visit)
        }
        return byDay.keys.sorted(by: >).map { day in
            LocationVisitDay(startOfDay: day, visits: byDay[day, default: []].sorted { ($0.anchorDate ?? .distantPast) > ($1.anchorDate ?? .distantPast) })
        }
    }

    /// The span of one day, from its midnight to the next.
    public static func window(of day: Date, calendar: Calendar = .current) -> (from: Date, until: Date) {
        let start = calendar.startOfDay(for: day)
        return (start, calendar.date(byAdding: .day, value: 1, to: start) ?? start.addingTimeInterval(86_400))
    }
}
