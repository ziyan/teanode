import Foundation

// Where the person goes, the places they name and the visits they make, as
// the server keeps them (docs/planning/location-execplan.md). The phone
// reports points and visits (LocationOutbox.swift holds what waits to be
// reported); the screens read them back here.

/// PlaceLabel is what a place is to the person: the fixed list the server
/// checks, which the agent reads for "at home" and "at work".
public enum PlaceLabel: String, CaseIterable, Sendable {
    case home
    case work
    case school
    case family
    case gym

    /// The SF Symbol that stands for it on a map and in a list.
    public var symbolName: String {
        switch self {
        case .home: "house"
        case .work: "briefcase"
        case .school: "graduationcap"
        case .family: "figure.2.and.child.holdinghands"
        case .gym: "dumbbell"
        }
    }
}

/// LocationShareLevel is how much of a location somebody may see: where the
/// person is now, or that and their visits. "owner" is the person's own.
public enum LocationShareLevel: String, CaseIterable, Sendable {
    case current
    case history
    case owner

    /// The levels a location can be shared at, least to most.
    public static var grantable: [LocationShareLevel] { [.current, .history] }
}

/// LocationPoint is one position, as the server keeps it.
public struct LocationPoint: Codable, Hashable, Sendable {
    public let latitudeDegrees: Double
    public let longitudeDegrees: Double
    public let accuracyMeters: Double
    public let altitudeMeters: Double?
    public let speedMetersPerSecond: Double?
    public let courseDegrees: Double?
    /// RFC 3339.
    public let measuredAt: String
    /// "phone" or "browser".
    public let locationSource: String

    public var measuredDate: Date? { ServerTime.date(measuredAt) }

    public init(
        latitudeDegrees: Double, longitudeDegrees: Double, accuracyMeters: Double, altitudeMeters: Double? = nil,
        speedMetersPerSecond: Double? = nil, courseDegrees: Double? = nil, measuredAt: String, locationSource: String = "phone"
    ) {
        self.latitudeDegrees = latitudeDegrees
        self.longitudeDegrees = longitudeDegrees
        self.accuracyMeters = accuracyMeters
        self.altitudeMeters = altitudeMeters
        self.speedMetersPerSecond = speedMetersPerSecond
        self.courseDegrees = courseDegrees
        self.measuredAt = measuredAt
        self.locationSource = locationSource
    }
}

/// LocationPlace is a spot with a name and a radius, belonging to one person.
public struct LocationPlace: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let userId: String
    /// What the person sees: their own name if they gave one, otherwise the
    /// OpenStreetMap name, the street address or the city.
    public let placeName: String
    public let isNamedByPerson: Bool
    /// "" or one of PlaceLabel's words.
    public let placeLabel: String
    public let latitudeDegrees: Double
    public let longitudeDegrees: Double
    public let radiusMeters: Double
    /// "node", "way", "relation" or "".
    public let osmType: String
    public let osmId: Int64?
    public let osmName: String
    /// Such as "amenity=cafe".
    public let osmCategory: String
    public let addressText: String
    public let visitCount: Int
    public let lastVisitAt: String?
    /// Why the name could not be looked up, when it could not.
    public let namingErrorMessage: String
    public let createdAt: String
    public let updatedAt: String

    public var label: PlaceLabel? { PlaceLabel(rawValue: placeLabel) }
    public var lastVisitDate: Date? { ServerTime.date(lastVisitAt) }

    /// Whether the person named or labeled it, which is what puts it first
    /// in a list and what a notification to the people they share with is
    /// made for.
    public var isOfPersonsOwn: Bool { isNamedByPerson || label != nil }

    public init(
        id: String, userId: String = "", placeName: String, isNamedByPerson: Bool = false, placeLabel: String = "", latitudeDegrees: Double,
        longitudeDegrees: Double, radiusMeters: Double = 75, osmType: String = "", osmId: Int64? = nil, osmName: String = "",
        osmCategory: String = "", addressText: String = "", visitCount: Int = 0, lastVisitAt: String? = nil, namingErrorMessage: String = "",
        createdAt: String = "", updatedAt: String = ""
    ) {
        self.id = id
        self.userId = userId
        self.placeName = placeName
        self.isNamedByPerson = isNamedByPerson
        self.placeLabel = placeLabel
        self.latitudeDegrees = latitudeDegrees
        self.longitudeDegrees = longitudeDegrees
        self.radiusMeters = radiusMeters
        self.osmType = osmType
        self.osmId = osmId
        self.osmName = osmName
        self.osmCategory = osmCategory
        self.addressText = addressText
        self.visitCount = visitCount
        self.lastVisitAt = lastVisitAt
        self.namingErrorMessage = namingErrorMessage
        self.createdAt = createdAt
        self.updatedAt = updatedAt
    }
}

/// PlaceVisit is one stay at a place.
public struct PlaceVisit: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let place: LocationPlace
    /// RFC 3339; nil when the phone did not see the arrival.
    public let arrivedAt: String?
    /// RFC 3339; nil while the person is still there.
    public let departedAt: String?
    /// Set when both times are.
    public let durationSeconds: Double?
    public let latitudeDegrees: Double
    public let longitudeDegrees: Double
    public let accuracyMeters: Double
    /// "visit", "region", "check_in" or "browser".
    public let visitSource: String

    public var arrivedDate: Date? { ServerTime.date(arrivedAt) }
    public var departedDate: Date? { ServerTime.date(departedAt) }

    /// Whether the person is still there, as far as the server knows.
    public var isOpen: Bool { departedAt == nil }

    /// When it is placed on a day: the arrival, or the departure when the
    /// arrival was not seen.
    public var anchorDate: Date? { arrivedDate ?? departedDate }

    /// How long the stay was, when both ends are known.
    public var stayDuration: Duration? {
        if let durationSeconds { return .seconds(durationSeconds) }
        guard let arrivedDate, let departedDate else { return nil }
        return .seconds(max(0, departedDate.timeIntervalSince(arrivedDate)))
    }

    public init(
        id: String, place: LocationPlace, arrivedAt: String?, departedAt: String?, durationSeconds: Double? = nil, latitudeDegrees: Double,
        longitudeDegrees: Double, accuracyMeters: Double = 0, visitSource: String = "visit"
    ) {
        self.id = id
        self.place = place
        self.arrivedAt = arrivedAt
        self.departedAt = departedAt
        self.durationSeconds = durationSeconds
        self.latitudeDegrees = latitudeDegrees
        self.longitudeDegrees = longitudeDegrees
        self.accuracyMeters = accuracyMeters
        self.visitSource = visitSource
    }
}

/// PersonLocation is where one person is: the latest point and the visit in
/// progress. The person's own, or from milestone 4 somebody sharing with
/// them.
public struct PersonLocation: Codable, Identifiable, Hashable, Sendable {
    public let userId: String
    public let username: String
    public let name: String?
    public let avatarUrl: String?
    public let avatarColorIndex: Int?
    /// "owner" for the person's own, else current or history.
    public let locationShareLevel: String
    public let latestPoint: LocationPoint?
    public let openVisit: PlaceVisit?

    public var id: String { userId }
    public var level: LocationShareLevel { LocationShareLevel(rawValue: locationShareLevel) ?? .current }
    public var displayName: String {
        guard let name, !name.isEmpty else { return username }
        return name
    }

    public init(
        userId: String, username: String, name: String? = nil, avatarUrl: String? = nil, avatarColorIndex: Int? = nil,
        locationShareLevel: String = "owner", latestPoint: LocationPoint? = nil, openVisit: PlaceVisit? = nil
    ) {
        self.userId = userId
        self.username = username
        self.name = name
        self.avatarUrl = avatarUrl
        self.avatarColorIndex = avatarColorIndex
        self.locationShareLevel = locationShareLevel
        self.latestPoint = latestPoint
        self.openVisit = openVisit
    }
}

/// FoundPlace is a named thing near a point, from OpenStreetMap, which a
/// check-in can be made at.
public struct FoundPlace: Codable, Hashable, Sendable, Identifiable {
    public let placeName: String
    public let osmType: String
    public let osmId: Int64
    public let osmCategory: String
    public let addressText: String
    public let latitudeDegrees: Double
    public let longitudeDegrees: Double
    public let distanceMeters: Double
    /// The person's own place there, if they have one.
    public let placeId: String?

    /// OpenStreetMap's own identity for it.
    public var id: String { "\(osmType)/\(osmId)" }

    /// The kind of thing it is, from the OpenStreetMap tag ("amenity=cafe" is
    /// "cafe"); empty when there is none. The screen puts it in words.
    public var categoryValue: String { LocationWords.categoryValue(osmCategory) }

    public init(
        placeName: String, osmType: String, osmId: Int64, osmCategory: String = "", addressText: String = "", latitudeDegrees: Double,
        longitudeDegrees: Double, distanceMeters: Double, placeId: String? = nil
    ) {
        self.placeName = placeName
        self.osmType = osmType
        self.osmId = osmId
        self.osmCategory = osmCategory
        self.addressText = addressText
        self.latitudeDegrees = latitudeDegrees
        self.longitudeDegrees = longitudeDegrees
        self.distanceMeters = distanceMeters
        self.placeId = placeId
    }
}

public struct PlaceVisitPage: Codable, Hashable, Sendable {
    public let placeVisits: [PlaceVisit]
    /// Where the next page starts; nil on the last.
    public let nextOffset: Int?
}

public struct PlacePage: Codable, Hashable, Sendable {
    public let places: [LocationPlace]
    public let nextOffset: Int?
}

public struct FoundPlacePage: Codable, Hashable, Sendable {
    public let foundPlaces: [FoundPlace]
    public let nextOffset: Int?
}

/// ReportLocationResult is what the server took of a batch.
public struct ReportLocationResult: Codable, Hashable, Sendable {
    public let newPointCount: Int
    public let newVisitCount: Int
    public let updatedVisitCount: Int
}

/// ForgetLocationHistoryResult is what an erase removed.
public struct ForgetLocationHistoryResult: Codable, Hashable, Sendable {
    public let pointCount: Int
    public let visitCount: Int
    public let placeCount: Int
}

/// LocationShare is one share of the person's location, with one person or
/// every member of one circle (milestone 4).
public struct LocationShare: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let granteeUserId: String?
    public let granteeUsername: String?
    public let granteeName: String?
    public let granteeCircleId: String?
    public let granteeCircleName: String?
    /// current or history.
    public let shareLevel: String
    public let createdAt: String

    public var level: LocationShareLevel? { LocationShareLevel(rawValue: shareLevel) }
    public var isWithCircle: Bool { granteeCircleId != nil }

    /// Who it is with: the person's name, the username when they have no
    /// name, or the circle's name.
    public var granteeDisplayName: String {
        if isWithCircle { return granteeCircleName ?? "" }
        if let granteeName, !granteeName.isEmpty { return granteeName }
        return granteeUsername ?? ""
    }
}

/// PlaceChange is what SavePlace is told: only the fields given change. With
/// no place identifier it makes a place at the coordinates.
public struct PlaceChange: Sendable, Equatable {
    public var placeId: String?
    public var placeName: String?
    /// "" takes the label off.
    public var placeLabel: String?
    public var latitudeDegrees: Double?
    public var longitudeDegrees: Double?
    public var radiusMeters: Double?

    public init(
        placeId: String? = nil, placeName: String? = nil, placeLabel: String? = nil, latitudeDegrees: Double? = nil,
        longitudeDegrees: Double? = nil, radiusMeters: Double? = nil
    ) {
        self.placeId = placeId
        self.placeName = placeName
        self.placeLabel = placeLabel
        self.latitudeDegrees = latitudeDegrees
        self.longitudeDegrees = longitudeDegrees
        self.radiusMeters = radiusMeters
    }

    var variables: [String: JSONValue] {
        var variables: [String: JSONValue] = [:]
        if let placeId { variables["placeId"] = .string(placeId) }
        if let placeName { variables["placeName"] = .string(placeName.trimmingCharacters(in: .whitespacesAndNewlines)) }
        if let placeLabel { variables["placeLabel"] = .string(placeLabel) }
        if let latitudeDegrees { variables["latitudeDegrees"] = .number(latitudeDegrees) }
        if let longitudeDegrees { variables["longitudeDegrees"] = .number(longitudeDegrees) }
        if let radiusMeters { variables["radiusMeters"] = .number(radiusMeters) }
        return variables
    }
}

/// CheckInTarget is where a check-in is made: a place of the person's own,
/// or a thing found nearby.
public enum CheckInTarget: Sendable, Equatable {
    case place(String)
    case found(FoundPlace)

    var variables: [String: JSONValue] {
        switch self {
        case .place(let placeId):
            return ["placeId": .string(placeId)]
        case .found(let found):
            return [
                "latitudeDegrees": .number(found.latitudeDegrees), "longitudeDegrees": .number(found.longitudeDegrees),
                "placeName": .string(found.placeName), "osmType": .string(found.osmType), "osmId": .number(Double(found.osmId)),
                "osmCategory": .string(found.osmCategory), "addressText": .string(found.addressText),
            ]
        }
    }
}

extension Documents {
    // Each shared list of fields is written out in full, nested ones too: the
    // server's test of the app's documents fills in one level of them.
    static let locationPointFields = """
        latitudeDegrees longitudeDegrees accuracyMeters altitudeMeters speedMetersPerSecond courseDegrees measuredAt locationSource
        """
    static let locationPlaceFields = """
        id userId placeName isNamedByPerson placeLabel latitudeDegrees longitudeDegrees radiusMeters osmType osmId osmName osmCategory addressText
        visitCount lastVisitAt namingErrorMessage createdAt updatedAt
        """
    static let placeVisitFields = """
        id arrivedAt departedAt durationSeconds latitudeDegrees longitudeDegrees accuracyMeters visitSource
        place {
          id userId placeName isNamedByPerson placeLabel latitudeDegrees longitudeDegrees radiusMeters osmType osmId osmName osmCategory addressText
          visitCount lastVisitAt namingErrorMessage createdAt updatedAt
        }
        """
    static let personLocationFields = """
        userId username name avatarUrl avatarColorIndex locationShareLevel
        latestPoint { latitudeDegrees longitudeDegrees accuracyMeters altitudeMeters speedMetersPerSecond courseDegrees measuredAt locationSource }
        openVisit {
          id arrivedAt departedAt durationSeconds latitudeDegrees longitudeDegrees accuracyMeters visitSource
          place {
            id userId placeName isNamedByPerson placeLabel latitudeDegrees longitudeDegrees radiusMeters osmType osmId osmName osmCategory addressText
            visitCount lastVisitAt namingErrorMessage createdAt updatedAt
          }
        }
        """
    static let foundPlaceFields = """
        placeName osmType osmId osmCategory addressText latitudeDegrees longitudeDegrees distanceMeters placeId
        """

    public static let location = """
        query ($userId: String) { GetLocation(userId: $userId) { \(personLocationFields) } }
        """
    public static let locationVisits = """
        query ($userId: String, $from: DateTime, $until: DateTime, $placeId: String, $placeLabel: String, $offset: Int, $limitCount: Int) {
          ListLocationVisits(userId: $userId, from: $from, until: $until, placeId: $placeId, placeLabel: $placeLabel, offset: $offset, limitCount: $limitCount) {
            placeVisits { \(placeVisitFields) }
            nextOffset
          }
        }
        """
    public static let locationPlaces = """
        query ($userId: String, $placeLabel: String, $query: String, $offset: Int, $limitCount: Int) {
          ListPlaces(userId: $userId, placeLabel: $placeLabel, query: $query, offset: $offset, limitCount: $limitCount) {
            places { \(locationPlaceFields) }
            nextOffset
          }
        }
        """
    public static let nearbyPlaces = """
        query ($latitudeDegrees: Float!, $longitudeDegrees: Float!, $radiusMeters: Float) {
          ListNearbyPlaces(latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, radiusMeters: $radiusMeters) { \(foundPlaceFields) }
        }
        """
    public static let searchPlaces = """
        query ($query: String!, $latitudeDegrees: Float, $longitudeDegrees: Float, $placeId: String, $radiusMeters: Float, $offset: Int, $limitCount: Int) {
          SearchPlaces(query: $query, latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, placeId: $placeId, radiusMeters: $radiusMeters, offset: $offset, limitCount: $limitCount) {
            foundPlaces { \(foundPlaceFields) }
            nextOffset
          }
        }
        """
    public static let locationPoints = """
        query ($userId: String, $from: DateTime!, $until: DateTime!) {
          ListLocationPoints(userId: $userId, from: $from, until: $until) { \(locationPointFields) }
        }
        """
    public static let reportLocation = """
        mutation ($agentPhoneId: String, $points: [ReportedPointInput!]!, $visits: [ReportedVisitInput!]!) {
          ReportLocation(agentPhoneId: $agentPhoneId, points: $points, visits: $visits) { newPointCount newVisitCount updatedVisitCount }
        }
        """
    public static let savePlace = """
        mutation ($placeId: String, $placeName: String, $placeLabel: String, $latitudeDegrees: Float, $longitudeDegrees: Float, $radiusMeters: Float) {
          SavePlace(placeId: $placeId, placeName: $placeName, placeLabel: $placeLabel, latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, radiusMeters: $radiusMeters) {
            \(locationPlaceFields)
          }
        }
        """
    public static let forgetPlace = """
        mutation ($placeId: String!) { ForgetPlace(placeId: $placeId) }
        """
    public static let checkIn = """
        mutation ($placeId: String, $latitudeDegrees: Float, $longitudeDegrees: Float, $placeName: String, $osmType: String, $osmId: Int64, $osmCategory: String, $addressText: String) {
          CheckIn(placeId: $placeId, latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, placeName: $placeName, osmType: $osmType, osmId: $osmId, osmCategory: $osmCategory, addressText: $addressText) {
            \(placeVisitFields)
          }
        }
        """
    public static let forgetLocationHistory = """
        mutation ($from: DateTime, $until: DateTime) {
          ForgetLocationHistory(from: $from, until: $until) { pointCount visitCount placeCount }
        }
        """

    // Milestone 4: sharing where the person is with a circle or a person, and
    // seeing the people who share with them. Written here so the documents are
    // checked against the server's schema from the start.
    public static let sharedLocations = """
        query { ListSharedLocations { \(personLocationFields) } }
        """
    public static let locationShares = """
        query {
          ListLocationShares { id granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName shareLevel createdAt }
        }
        """
    public static let shareLocation = """
        mutation ($granteeUserId: String, $granteeCircleId: String, $shareLevel: String!) {
          ShareLocation(granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, shareLevel: $shareLevel) {
            id granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName shareLevel createdAt
          }
        }
        """
    public static let unshareLocation = """
        mutation ($shareId: String!) { UnshareLocation(shareId: $shareId) }
        """
    public static let locationEvents = """
        subscription { LocationEvents { userId } }
        """

    /// The location documents, for the server's schema check.
    public static let locationDocuments: [String: String] = [
        "location": location, "locationVisits": locationVisits, "locationPlaces": locationPlaces, "nearbyPlaces": nearbyPlaces,
        "searchPlaces": searchPlaces, "locationPoints": locationPoints, "reportLocation": reportLocation, "savePlace": savePlace,
        "forgetPlace": forgetPlace, "checkIn": checkIn, "forgetLocationHistory": forgetLocationHistory,
        "sharedLocations": sharedLocations, "locationShares": locationShares, "shareLocation": shareLocation,
        "unshareLocation": unshareLocation, "locationEvents": locationEvents,
    ]
}

extension Connection {
    /// Where the person is: the latest point and the visit in progress.
    public func location(userId: String? = nil) async throws -> PersonLocation {
        struct Answer: Decodable, Sendable { let GetLocation: PersonLocation }
        var variables: [String: JSONValue] = [:]
        if let userId { variables["userId"] = .string(userId) }
        return try await execute(Documents.location, variables: variables, as: Answer.self).GetLocation
    }

    /// Visits, newest first, a page at a time. `offset` is the previous
    /// page's `nextOffset`.
    public func locationVisits(
        userId: String? = nil, from: Date? = nil, until: Date? = nil, placeId: String? = nil, placeLabel: String? = nil, offset: Int = 0,
        limitCount: Int? = nil
    ) async throws -> PlaceVisitPage {
        struct Answer: Decodable, Sendable { let ListLocationVisits: PlaceVisitPage }
        var variables: [String: JSONValue] = [:]
        if let userId { variables["userId"] = .string(userId) }
        if let from { variables["from"] = .string(CalendarDates.text(from)) }
        if let until { variables["until"] = .string(CalendarDates.text(until)) }
        if let placeId { variables["placeId"] = .string(placeId) }
        if let placeLabel { variables["placeLabel"] = .string(placeLabel) }
        if offset > 0 { variables["offset"] = .number(Double(offset)) }
        if let limitCount { variables["limitCount"] = .number(Double(limitCount)) }
        return try await execute(Documents.locationVisits, variables: variables, as: Answer.self).ListLocationVisits
    }

    /// The person's places, a page at a time.
    public func locationPlaces(userId: String? = nil, placeLabel: String? = nil, query: String? = nil, offset: Int = 0, limitCount: Int? = nil)
        async throws -> PlacePage
    {
        struct Answer: Decodable, Sendable { let ListPlaces: PlacePage }
        var variables: [String: JSONValue] = [:]
        if let userId { variables["userId"] = .string(userId) }
        if let placeLabel { variables["placeLabel"] = .string(placeLabel) }
        if let query, !query.isEmpty { variables["query"] = .string(query) }
        if offset > 0 { variables["offset"] = .number(Double(offset)) }
        if let limitCount { variables["limitCount"] = .number(Double(limitCount)) }
        return try await execute(Documents.locationPlaces, variables: variables, as: Answer.self).ListPlaces
    }

    /// Named things near a point, nearest first, for a check-in.
    public func nearbyPlaces(latitudeDegrees: Double, longitudeDegrees: Double, radiusMeters: Double? = nil) async throws -> [FoundPlace] {
        struct Answer: Decodable, Sendable { let ListNearbyPlaces: [FoundPlace] }
        var variables: [String: JSONValue] = ["latitudeDegrees": .number(latitudeDegrees), "longitudeDegrees": .number(longitudeDegrees)]
        if let radiusMeters { variables["radiusMeters"] = .number(radiusMeters) }
        return try await execute(Documents.nearbyPlaces, variables: variables, as: Answer.self).ListNearbyPlaces
    }

    /// Places matching words near a point, from OpenStreetMap. With no point
    /// and no place, near where the person is now.
    public func searchPlaces(
        query: String, latitudeDegrees: Double? = nil, longitudeDegrees: Double? = nil, placeId: String? = nil, radiusMeters: Double? = nil,
        offset: Int = 0, limitCount: Int? = nil
    ) async throws -> FoundPlacePage {
        struct Answer: Decodable, Sendable { let SearchPlaces: FoundPlacePage }
        var variables: [String: JSONValue] = ["query": .string(query)]
        if let latitudeDegrees { variables["latitudeDegrees"] = .number(latitudeDegrees) }
        if let longitudeDegrees { variables["longitudeDegrees"] = .number(longitudeDegrees) }
        if let placeId { variables["placeId"] = .string(placeId) }
        if let radiusMeters { variables["radiusMeters"] = .number(radiusMeters) }
        if offset > 0 { variables["offset"] = .number(Double(offset)) }
        if let limitCount { variables["limitCount"] = .number(Double(limitCount)) }
        return try await execute(Documents.searchPlaces, variables: variables, as: Answer.self).SearchPlaces
    }

    /// The points of a stretch of at most two days, for drawing the path.
    public func locationPoints(userId: String? = nil, from: Date, until: Date) async throws -> [LocationPoint] {
        struct Answer: Decodable, Sendable { let ListLocationPoints: [LocationPoint] }
        var variables: [String: JSONValue] = ["from": .string(CalendarDates.text(from)), "until": .string(CalendarDates.text(until))]
        if let userId { variables["userId"] = .string(userId) }
        return try await execute(Documents.locationPoints, variables: variables, as: Answer.self).ListLocationPoints
    }

    /// Hands the server a batch. Safe to send twice: the server knows a point
    /// by when it was measured and a visit by its key.
    public func reportLocation(_ batch: LocationBatch, agentPhoneId: String? = nil) async throws -> ReportLocationResult {
        struct Answer: Decodable, Sendable { let ReportLocation: ReportLocationResult }
        var variables = batch.variables
        if let agentPhoneId { variables["agentPhoneId"] = .string(agentPhoneId) }
        return try await execute(Documents.reportLocation, variables: variables, as: Answer.self).ReportLocation
    }

    /// Names, labels, moves or resizes a place; with no identifier in the
    /// change, makes one at its coordinates.
    @discardableResult
    public func savePlace(_ change: PlaceChange) async throws -> LocationPlace {
        struct Answer: Decodable, Sendable { let SavePlace: LocationPlace }
        return try await execute(Documents.savePlace, variables: change.variables, as: Answer.self).SavePlace
    }

    /// Deletes a place and its visits.
    public func forgetPlace(placeId: String) async throws {
        struct Answer: Decodable, Sendable { let ForgetPlace: Bool? }
        _ = try await execute(Documents.forgetPlace, variables: ["placeId": .string(placeId)], as: Answer.self)
    }

    /// Records that the person is at a place now.
    @discardableResult
    public func checkIn(_ target: CheckInTarget) async throws -> PlaceVisit {
        struct Answer: Decodable, Sendable { let CheckIn: PlaceVisit }
        return try await execute(Documents.checkIn, variables: target.variables, as: Answer.self).CheckIn
    }

    /// Deletes points and visits in a range; with neither end, all of them.
    @discardableResult
    public func forgetLocationHistory(from: Date? = nil, until: Date? = nil) async throws -> ForgetLocationHistoryResult {
        struct Answer: Decodable, Sendable { let ForgetLocationHistory: ForgetLocationHistoryResult }
        var variables: [String: JSONValue] = [:]
        if let from { variables["from"] = .string(CalendarDates.text(from)) }
        if let until { variables["until"] = .string(CalendarDates.text(until)) }
        return try await execute(Documents.forgetLocationHistory, variables: variables, as: Answer.self).ForgetLocationHistory
    }

    // MARK: Milestone 4: sharing

    /// Every person sharing their location with the caller.
    public func sharedLocations() async throws -> [PersonLocation] {
        struct Answer: Decodable, Sendable { let ListSharedLocations: [PersonLocation] }
        return try await execute(Documents.sharedLocations, as: Answer.self).ListSharedLocations
    }

    /// The shares of the person's own location.
    public func locationShares() async throws -> [LocationShare] {
        struct Answer: Decodable, Sendable { let ListLocationShares: [LocationShare] }
        return try await execute(Documents.locationShares, as: Answer.self).ListLocationShares
    }

    /// Shares the person's location with a person or a circle (one of the
    /// two), or changes the level of a share already made.
    @discardableResult
    public func shareLocation(granteeUserId: String?, granteeCircleId: String?, level: LocationShareLevel) async throws -> LocationShare {
        struct Answer: Decodable, Sendable { let ShareLocation: LocationShare }
        return try await execute(
            Documents.shareLocation,
            variables: [
                "granteeUserId": granteeUserId.map { .string($0) } ?? .null, "granteeCircleId": granteeCircleId.map { .string($0) } ?? .null,
                "shareLevel": .string(level.rawValue),
            ], as: Answer.self
        ).ShareLocation
    }

    /// Takes a share back.
    public func unshareLocation(shareId: String) async throws {
        struct Answer: Decodable, Sendable { let UnshareLocation: Bool? }
        _ = try await execute(Documents.unshareLocation, variables: ["shareId": .string(shareId)], as: Answer.self)
    }
}
