import Foundation

// How the person got from one place to the next: the trips between visits, the
// legs a trip is made of, each with a way of travel, and the distance traveled
// by each way over days, weeks, months and years. The server works the ways
// out (from the motion activity the phone reports, else from speed) and keeps
// the person's corrections; the screens read them back here.

/// TravelMode is a way of travel.
public enum TravelMode: String, CaseIterable, Identifiable, Sendable {
    case walk
    case run
    case bike
    case car
    case bus
    case train
    case boat
    case plane

    public var id: String { rawValue }

    /// The SF Symbol that stands for it in a list.
    public var symbolName: String {
        switch self {
        case .walk: "figure.walk"
        case .run: "figure.run"
        case .bike: "bicycle"
        case .car: "car.fill"
        case .bus: "bus.fill"
        case .train: "tram.fill"
        case .boat: "ferry.fill"
        case .plane: "airplane"
        }
    }

    /// The symbol for a way the server named; a generic one for a way this
    /// version does not know.
    public static func symbolName(forTravelMode travelMode: String) -> String {
        TravelMode(rawValue: travelMode)?.symbolName ?? "arrow.triangle.turn.up.right.diamond"
    }
}

/// TripLeg is a part of a trip with one way of travel.
public struct TripLeg: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    /// RFC 3339.
    public let startedAt: String
    public let endedAt: String
    public let durationSeconds: Double
    public let distanceMeters: Double
    /// One of `TravelMode`'s values, as the person corrected it if they did.
    public let travelMode: String
    /// The way the server worked out, whatever the person corrected.
    public let detectedTravelMode: String
    public let isTravelModeCorrected: Bool
    /// "motion" or "speed".
    public let detectionSource: String

    public var startedDate: Date? { ServerTime.date(startedAt) }
    public var endedDate: Date? { ServerTime.date(endedAt) }

    /// The way of travel, when this version knows it.
    public var mode: TravelMode? { TravelMode(rawValue: travelMode) }

    public init(
        id: String, startedAt: String, endedAt: String, durationSeconds: Double, distanceMeters: Double, travelMode: String,
        detectedTravelMode: String? = nil, isTravelModeCorrected: Bool = false, detectionSource: String = "motion"
    ) {
        self.id = id
        self.startedAt = startedAt
        self.endedAt = endedAt
        self.durationSeconds = durationSeconds
        self.distanceMeters = distanceMeters
        self.travelMode = travelMode
        self.detectedTravelMode = detectedTravelMode ?? travelMode
        self.isTravelModeCorrected = isTravelModeCorrected
        self.detectionSource = detectionSource
    }
}

/// Trip is the travel between two visits.
public struct Trip: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    /// RFC 3339.
    public let startedAt: String
    public let endedAt: String
    public let durationSeconds: Double
    public let distanceMeters: Double
    /// The places of the visits either side; nil when one is gone.
    public let fromPlace: LocationPlace?
    public let toPlace: LocationPlace?
    public let legs: [TripLeg]

    public var startedDate: Date? { ServerTime.date(startedAt) }
    public var endedDate: Date? { ServerTime.date(endedAt) }

    public init(
        id: String, startedAt: String, endedAt: String, durationSeconds: Double, distanceMeters: Double, fromPlace: LocationPlace? = nil,
        toPlace: LocationPlace? = nil, legs: [TripLeg]
    ) {
        self.id = id
        self.startedAt = startedAt
        self.endedAt = endedAt
        self.durationSeconds = durationSeconds
        self.distanceMeters = distanceMeters
        self.fromPlace = fromPlace
        self.toPlace = toPlace
        self.legs = legs
    }
}

public struct TripPage: Codable, Hashable, Sendable {
    public let trips: [Trip]
    /// Where the next page starts; nil on the last.
    public let nextOffset: Int?

    public init(trips: [Trip], nextOffset: Int? = nil) {
        self.trips = trips
        self.nextOffset = nextOffset
    }
}

/// TravelTotal is how far the person traveled one way in one period.
public struct TravelTotal: Codable, Hashable, Sendable {
    /// The first day of the period, as YYYY-MM-DD.
    public let periodStartDay: String
    public let travelMode: String
    public let distanceMeters: Double
    public let durationSeconds: Double
    public let legCount: Int

    public init(periodStartDay: String, travelMode: String, distanceMeters: Double, durationSeconds: Double, legCount: Int) {
        self.periodStartDay = periodStartDay
        self.travelMode = travelMode
        self.distanceMeters = distanceMeters
        self.durationSeconds = durationSeconds
        self.legCount = legCount
    }
}

/// TravelPeriod is one period of the Travel screen: the distance by each way.
public struct TravelPeriod: Identifiable, Hashable, Sendable {
    /// The first day of the period, as YYYY-MM-DD.
    public let periodStartDay: String
    /// The ways traveled, farthest first.
    public let totals: [TravelTotal]

    public var id: String { periodStartDay }

    public var distanceMeters: Double { totals.reduce(0) { $0 + $1.distanceMeters } }
    public var durationSeconds: Double { totals.reduce(0) { $0 + $1.durationSeconds } }

    /// Midnight of the first day, in the calendar's zone.
    public func startDate(calendar: Calendar = .current) -> Date? {
        let parts = periodStartDay.split(separator: "-").compactMap { Int($0) }
        guard parts.count == 3 else { return nil }
        return calendar.date(from: DateComponents(year: parts[0], month: parts[1], day: parts[2]))
    }
}

/// TravelPeriodKind is the length of the periods the totals are added up in.
public enum TravelPeriodKind: String, CaseIterable, Identifiable, Sendable {
    case day
    case week
    case month
    case year

    public var id: String { rawValue }

    /// How many periods one page of the Travel screen covers.
    public var periodCountPerPage: Int {
        switch self {
        case .day: 14
        case .week: 12
        case .month: 12
        case .year: 5
        }
    }

    var calendarComponent: Calendar.Component {
        switch self {
        case .day: .day
        case .week: .weekOfYear
        case .month: .month
        case .year: .year
        }
    }

    /// The range a page covers: `periodCountPerPage` whole periods ending
    /// with the one that holds `now`, moved back by `pageBack` pages. Weeks
    /// begin on Monday, as the server counts them.
    public func pageRange(pageBack: Int, now: Date = Date(), calendar: Calendar = .current) -> (from: Date, until: Date) {
        var mondayCalendar = calendar
        mondayCalendar.firstWeekday = 2
        mondayCalendar.minimumDaysInFirstWeek = 4
        let current = mondayCalendar.dateInterval(of: calendarComponent, for: now)
        let currentStart = current?.start ?? mondayCalendar.startOfDay(for: now)
        let currentEnd = current?.end ?? currentStart.addingTimeInterval(86_400)
        let count = periodCountPerPage
        let until = mondayCalendar.date(byAdding: calendarComponent, value: -count * pageBack, to: currentEnd) ?? currentEnd
        let from = mondayCalendar.date(byAdding: calendarComponent, value: -count, to: until) ?? currentStart
        return (from, until)
    }
}

public enum TravelPeriods {
    /// The totals gathered by period, newest period first, the ways in each
    /// from the farthest. A way with no distance is left out.
    public static func group(_ totals: [TravelTotal]) -> [TravelPeriod] {
        var byDay: [String: [TravelTotal]] = [:]
        for total in totals where total.distanceMeters > 0 { byDay[total.periodStartDay, default: []].append(total) }
        return byDay.keys.sorted(by: >).map { day in
            TravelPeriod(
                periodStartDay: day,
                totals: byDay[day, default: []].sorted { ($0.distanceMeters, $1.travelMode) > ($1.distanceMeters, $0.travelMode) })
        }
    }

    /// The trips that lie between visits, placed on a day by when they
    /// began, newest first.
    public static func trips(_ trips: [Trip], from: Date, until: Date) -> [Trip] {
        trips.filter { trip in
            guard let startedDate = trip.startedDate else { return false }
            return startedDate >= from && startedDate < until
        }
        .sorted { ($0.startedDate ?? .distantPast) > ($1.startedDate ?? .distantPast) }
    }
}

extension Documents {
    public static let trips = """
        query ($userId: String, $from: DateTime!, $until: DateTime!, $offset: Int, $limitCount: Int) {
          ListTrips(userId: $userId, from: $from, until: $until, offset: $offset, limitCount: $limitCount) {
            trips {
              id startedAt endedAt durationSeconds distanceMeters
              fromPlace {
                id userId placeName isNamedByPerson placeLabel latitudeDegrees longitudeDegrees radiusMeters osmType osmId osmName osmCategory addressText isAddressTyped
                visitCount lastVisitAt namingErrorMessage createdAt updatedAt
              }
              toPlace {
                id userId placeName isNamedByPerson placeLabel latitudeDegrees longitudeDegrees radiusMeters osmType osmId osmName osmCategory addressText isAddressTyped
                visitCount lastVisitAt namingErrorMessage createdAt updatedAt
              }
              legs { id startedAt endedAt durationSeconds distanceMeters travelMode detectedTravelMode isTravelModeCorrected detectionSource }
            }
            nextOffset
          }
        }
        """
    public static let setTripLegTravelMode = """
        mutation ($tripLegId: String!, $travelMode: String) {
          SetTripLegTravelMode(tripLegId: $tripLegId, travelMode: $travelMode) {
            id startedAt endedAt durationSeconds distanceMeters travelMode detectedTravelMode isTravelModeCorrected detectionSource
          }
        }
        """
    public static let travelTotals = """
        query ($userId: String, $period: String!, $from: DateTime!, $until: DateTime!) {
          ListTravelTotals(userId: $userId, period: $period, from: $from, until: $until) {
            periodStartDay travelMode distanceMeters durationSeconds legCount
          }
        }
        """

    /// The travel documents, for the server's schema check.
    public static let travelDocuments: [String: String] = [
        "trips": trips, "setTripLegTravelMode": setTripLegTravelMode, "travelTotals": travelTotals,
    ]
}

extension Connection {
    /// The trips that began in a range, newest first, a page at a time. The
    /// person's own, or somebody sharing their history with the caller.
    public func trips(userId: String? = nil, from: Date, until: Date, offset: Int = 0, limitCount: Int? = nil) async throws -> TripPage {
        struct Answer: Decodable, Sendable { let ListTrips: TripPage }
        var variables: [String: JSONValue] = ["from": .string(CalendarDates.text(from)), "until": .string(CalendarDates.text(until))]
        if let userId { variables["userId"] = .string(userId) }
        if offset > 0 { variables["offset"] = .number(Double(offset)) }
        if let limitCount { variables["limitCount"] = .number(Double(limitCount)) }
        return try await execute(Documents.trips, variables: variables, as: Answer.self).ListTrips
    }

    /// Corrects the way of travel of one of the person's own legs; with none,
    /// takes the correction back so the way the server detected counts again.
    @discardableResult
    public func setTripLegTravelMode(tripLegId: String, travelMode: TravelMode?) async throws -> TripLeg {
        struct Answer: Decodable, Sendable { let SetTripLegTravelMode: TripLeg }
        let variables: [String: JSONValue] = ["tripLegId": .string(tripLegId), "travelMode": travelMode.map { .string($0.rawValue) } ?? .null]
        return try await execute(Documents.setTripLegTravelMode, variables: variables, as: Answer.self).SetTripLegTravelMode
    }

    /// How far was traveled by each way in each period of a range: the
    /// person's own, or somebody sharing their history with the caller.
    public func travelTotals(userId: String? = nil, period: TravelPeriodKind, from: Date, until: Date) async throws -> [TravelTotal] {
        struct Answer: Decodable, Sendable { let ListTravelTotals: [TravelTotal] }
        var variables: [String: JSONValue] = [
            "period": .string(period.rawValue), "from": .string(CalendarDates.text(from)), "until": .string(CalendarDates.text(until)),
        ]
        if let userId { variables["userId"] = .string(userId) }
        return try await execute(Documents.travelTotals, variables: variables, as: Answer.self).ListTravelTotals
    }
}
