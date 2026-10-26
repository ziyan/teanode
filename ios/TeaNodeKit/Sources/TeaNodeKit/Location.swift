import Foundation

// Where the person goes, the places they name and the visits they make, as
// the server keeps them. The phone reports points (one reported position
// each) and visits (LocationOutbox.swift holds what waits to be reported);
// the screens read them back here.

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

/// LocationPlace is a place with a name and a radius, belonging to one person.
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
    /// Whether the person typed the address, so the map no longer names the
    /// place.
    public let isAddressTyped: Bool
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
        osmCategory: String = "", addressText: String = "", isAddressTyped: Bool = false, visitCount: Int = 0, lastVisitAt: String? = nil,
        namingErrorMessage: String = "", createdAt: String = "", updatedAt: String = ""
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
        self.isAddressTyped = isAddressTyped
        self.visitCount = visitCount
        self.lastVisitAt = lastVisitAt
        self.namingErrorMessage = namingErrorMessage
        self.createdAt = createdAt
        self.updatedAt = updatedAt
    }
}

/// PlaceVisit is one visit to a place: from arriving to leaving.
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
    /// How many of the person's own photos were taken during the visit; nil
    /// for another person's visit, without the Photos service, or when the
    /// arrival was not seen.
    public let photoCount: Int?
    /// The first few of them, earliest first; nil when `photoCount` is.
    public let photos: [Photo]?
    /// RFC 3339: when the photos counted end (the departure, the person's
    /// latest position for a visit they left without one being seen, or the
    /// time of the answer); nil when `photoCount` is.
    public let photosTakenBefore: String?

    public var arrivedDate: Date? { ServerTime.date(arrivedAt) }
    public var departedDate: Date? { ServerTime.date(departedAt) }
    public var photosTakenBeforeDate: Date? { ServerTime.date(photosTakenBefore) }

    /// Whether the person is still there, as far as the server knows.
    public var isOpen: Bool { departedAt == nil }

    /// When it is placed on a day: the arrival, or the departure when the
    /// arrival was not seen.
    public var anchorDate: Date? { arrivedDate ?? departedDate }

    /// How long the visit was, when both ends are known.
    public var visitDuration: Duration? {
        if let durationSeconds { return .seconds(durationSeconds) }
        guard let arrivedDate, let departedDate else { return nil }
        return .seconds(max(0, departedDate.timeIntervalSince(arrivedDate)))
    }

    public init(
        id: String, place: LocationPlace, arrivedAt: String?, departedAt: String?, durationSeconds: Double? = nil, latitudeDegrees: Double,
        longitudeDegrees: Double, accuracyMeters: Double = 0, visitSource: String = "visit", photoCount: Int? = nil, photos: [Photo]? = nil,
        photosTakenBefore: String? = nil
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
        self.photoCount = photoCount
        self.photos = photos
        self.photosTakenBefore = photosTakenBefore
    }
}

/// PersonLocation is where one person is: the latest point and the visit in
/// progress. The person's own, or somebody sharing with them.
public struct PersonLocation: Codable, Identifiable, Hashable, Sendable {
    public let userId: String
    public let username: String
    public let name: String?
    public let avatarUrl: String?
    public let avatarColorIndex: Int?
    /// "owner" for the person's own, else current or history.
    public let shareLevel: String
    public let latestPoint: LocationPoint?
    public let openVisit: PlaceVisit?
    /// The pause in effect, when the person has paused. For somebody else
    /// the server then sends no `latestPoint` and no `openVisit`.
    public let pause: LocationPause?
    /// The caller's mute of this person's arrived and left notices; nil when
    /// not muted (the server sends nil for a mute that has ended, and always
    /// for the caller's own location).
    public let notificationMute: LocationNotificationMute?

    public var id: String { userId }
    public var level: LocationShareLevel { LocationShareLevel(rawValue: shareLevel) ?? .current }
    public var displayName: String {
        guard let name, !name.isEmpty else { return username }
        return name
    }

    public init(
        userId: String, username: String, name: String? = nil, avatarUrl: String? = nil, avatarColorIndex: Int? = nil,
        shareLevel: String = "owner", latestPoint: LocationPoint? = nil, openVisit: PlaceVisit? = nil, pause: LocationPause? = nil,
        notificationMute: LocationNotificationMute? = nil
    ) {
        self.userId = userId
        self.username = username
        self.name = name
        self.avatarUrl = avatarUrl
        self.avatarColorIndex = avatarColorIndex
        self.shareLevel = shareLevel
        self.latestPoint = latestPoint
        self.openVisit = openVisit
        self.pause = pause
        self.notificationMute = notificationMute
    }

    /// Whether the caller muted this person's notices and the mute has not
    /// ended at an instant.
    public func isNotificationMuted(at date: Date = Date()) -> Bool {
        notificationMute?.isInEffect(at: date) ?? false
    }
}

/// LocationNotificationMute is the caller's mute of the arrived and left
/// notices of one person sharing with them. It is the caller's own: the
/// person muted is not told.
public struct LocationNotificationMute: Codable, Hashable, Sendable {
    /// RFC 3339; nil mutes until the caller unmutes.
    public let mutedUntil: String?

    public var mutedUntilDate: Date? { ServerTime.date(mutedUntil) }

    /// Whether the mute goes on until the caller unmutes.
    public var isOpenEnded: Bool { mutedUntil == nil }

    public init(mutedUntil: String? = nil) {
        self.mutedUntil = mutedUntil
    }

    public init(mutedUntilDate: Date?) {
        self.init(mutedUntil: mutedUntilDate.map(CalendarDates.text))
    }

    /// Whether it is in effect at an instant: no end, or an end not yet reached.
    public func isInEffect(at date: Date) -> Bool {
        guard !isOpenEnded else { return true }
        guard let mutedUntilDate else { return false }
        return date < mutedUntilDate
    }
}

/// FoundPlace is a named place near a position, from OpenStreetMap, which a
/// check-in can be made at.
public struct FoundPlace: Codable, Hashable, Sendable, Identifiable {
    /// The map name; for the person's own place that they named, their name.
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
    /// "" or one of PlaceLabel's words; set only on the person's own place.
    public let placeLabel: String
    /// Whether placeName is the person's own name for their own place.
    public let isNamedByPerson: Bool

    /// OpenStreetMap's own identity for it.
    public var id: String { "\(osmType)/\(osmId)" }

    public var label: PlaceLabel? { PlaceLabel(rawValue: placeLabel) }

    /// The kind of place it is, from the OpenStreetMap tag ("amenity=cafe" is
    /// "cafe"); empty when there is none. The screen puts it in words.
    public var categoryValue: String { LocationWords.categoryValue(osmCategory) }

    public init(
        placeName: String, osmType: String, osmId: Int64, osmCategory: String = "", addressText: String = "", latitudeDegrees: Double,
        longitudeDegrees: Double, distanceMeters: Double, placeId: String? = nil, placeLabel: String = "", isNamedByPerson: Bool = false
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
        self.placeLabel = placeLabel
        self.isNamedByPerson = isNamedByPerson
    }

    /// The two fields an older server does not send decode to their defaults.
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        placeName = try container.decode(String.self, forKey: .placeName)
        osmType = try container.decode(String.self, forKey: .osmType)
        osmId = try container.decode(Int64.self, forKey: .osmId)
        osmCategory = try container.decode(String.self, forKey: .osmCategory)
        addressText = try container.decode(String.self, forKey: .addressText)
        latitudeDegrees = try container.decode(Double.self, forKey: .latitudeDegrees)
        longitudeDegrees = try container.decode(Double.self, forKey: .longitudeDegrees)
        distanceMeters = try container.decode(Double.self, forKey: .distanceMeters)
        placeId = try container.decodeIfPresent(String.self, forKey: .placeId)
        placeLabel = try container.decodeIfPresent(String.self, forKey: .placeLabel) ?? ""
        isNamedByPerson = try container.decodeIfPresent(Bool.self, forKey: .isNamedByPerson) ?? false
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
    /// The pause in effect after the report, so the phone learns of one set
    /// elsewhere.
    public let pause: LocationPause?
    /// How many positions and visits the server dropped because they fell in
    /// a pause.
    public let pausedPointCount: Int
    public let pausedVisitCount: Int

    public init(
        newPointCount: Int, newVisitCount: Int, updatedVisitCount: Int, pause: LocationPause? = nil, pausedPointCount: Int = 0,
        pausedVisitCount: Int = 0
    ) {
        self.newPointCount = newPointCount
        self.newVisitCount = newVisitCount
        self.updatedVisitCount = updatedVisitCount
        self.pause = pause
        self.pausedPointCount = pausedPointCount
        self.pausedVisitCount = pausedVisitCount
    }

    /// The three fields a server that cannot pause does not send decode to
    /// their defaults.
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        newPointCount = try container.decode(Int.self, forKey: .newPointCount)
        newVisitCount = try container.decode(Int.self, forKey: .newVisitCount)
        updatedVisitCount = try container.decode(Int.self, forKey: .updatedVisitCount)
        pause = try container.decodeIfPresent(LocationPause.self, forKey: .pause)
        pausedPointCount = try container.decodeIfPresent(Int.self, forKey: .pausedPointCount) ?? 0
        pausedVisitCount = try container.decodeIfPresent(Int.self, forKey: .pausedVisitCount) ?? 0
    }
}

/// EraseLocationHistoryResult is what an erase removed.
public struct EraseLocationHistoryResult: Codable, Hashable, Sendable {
    public let pointCount: Int
    public let visitCount: Int
    public let placeCount: Int
}

/// LocationShare is one share of the person's location, with one person or
/// every member of one circle.
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

/// LocationEvent is the server telling a viewer that a person they can see
/// (themselves included) has a new position or visit.
public struct LocationEvent: Codable, Hashable, Sendable {
    public let userId: String
}

/// LocationSetup is how far the person has got with setting Location up the
/// first time: allowing it, labeling the place they live as home, and, for
/// each family circle they are in, sharing where they are with it. It is
/// worked out from what the server and the phone say and never stored; only
/// the person's "Not now" is kept.
public struct LocationSetup: Equatable, Sendable {
    /// Whether the phone may record while the app is closed (Always).
    public let isPermissionDone: Bool
    /// Whether a place labeled home exists.
    public let isHomeDone: Bool
    /// The family circles to offer sharing with, every one the person is in.
    public let familyCircles: [PeopleCircle]
    /// The identifiers of the family circles the person's location is
    /// already shared with.
    public let sharedFamilyCircleIds: Set<String>

    public init(isPermissionDone: Bool, homePlaces: [LocationPlace], circles: [PeopleCircle], shares: [LocationShare]) {
        self.isPermissionDone = isPermissionDone
        isHomeDone = homePlaces.contains { $0.label == .home }
        let families = circles.filter { $0.knownCircleKind == .family }
        familyCircles = families
        let sharedCircleIds = Set(shares.compactMap(\.granteeCircleId))
        sharedFamilyCircleIds = Set(families.map(\.id)).intersection(sharedCircleIds)
    }

    /// Whether the person's location is already shared with this circle.
    public func isShared(with circle: PeopleCircle) -> Bool { sharedFamilyCircleIds.contains(circle.id) }

    /// Whether the share rows are part of the setup: the person is in a
    /// family circle.
    public var isShareOffered: Bool { !familyCircles.isEmpty }

    /// Whether the location is shared with every family circle.
    public var isShareDone: Bool { familyCircles.allSatisfy { sharedFamilyCircleIds.contains($0.id) } }

    /// Whether every row that applies is done.
    public var isFinished: Bool { isPermissionDone && isHomeDone && isShareDone }

    /// Whether the setup is shown in place of the lists: the server has no
    /// position for the person yet, they have not said "Not now", and
    /// something is left to do.
    public func isShown(isDismissed: Bool, hasLatestPoint: Bool) -> Bool {
        !isDismissed && !hasLatestPoint && !isFinished
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
    /// The address the person chose, sent with the position it belongs to:
    /// the server moves the place there and keeps this text as its address.
    public var addressText: String?

    public init(
        placeId: String? = nil, placeName: String? = nil, placeLabel: String? = nil, latitudeDegrees: Double? = nil,
        longitudeDegrees: Double? = nil, radiusMeters: Double? = nil, addressText: String? = nil
    ) {
        self.placeId = placeId
        self.placeName = placeName
        self.placeLabel = placeLabel
        self.latitudeDegrees = latitudeDegrees
        self.longitudeDegrees = longitudeDegrees
        self.radiusMeters = radiusMeters
        self.addressText = addressText
    }

    var variables: [String: JSONValue] {
        var variables: [String: JSONValue] = [:]
        if let placeId { variables["placeId"] = .string(placeId) }
        if let placeName { variables["placeName"] = .string(placeName.trimmingCharacters(in: .whitespacesAndNewlines)) }
        if let placeLabel { variables["placeLabel"] = .string(placeLabel) }
        if let latitudeDegrees { variables["latitudeDegrees"] = .number(latitudeDegrees) }
        if let longitudeDegrees { variables["longitudeDegrees"] = .number(longitudeDegrees) }
        if let radiusMeters { variables["radiusMeters"] = .number(radiusMeters) }
        if let addressText { variables["addressText"] = .string(addressText.trimmingCharacters(in: .whitespacesAndNewlines)) }
        return variables
    }
}

/// CheckInTarget is where a check-in is made: a place of the person's own,
/// a place found nearby, or just a position, where the server uses the
/// person's own place there or makes a new one and names it afterwards.
/// A position may carry how precise the device says it is, in meters; the
/// server stores it on the visit. Negative or not a number means unknown and
/// is not sent.
public enum CheckInTarget: Sendable, Equatable {
    case place(String)
    case found(FoundPlace)
    case position(latitudeDegrees: Double, longitudeDegrees: Double, accuracyMeters: Double? = nil)

    var variables: [String: JSONValue] {
        switch self {
        case .place(let placeId):
            return ["placeId": .string(placeId)]
        case .position(let latitudeDegrees, let longitudeDegrees, let accuracyMeters):
            var variables: [String: JSONValue] = ["latitudeDegrees": .number(latitudeDegrees), "longitudeDegrees": .number(longitudeDegrees)]
            if let accuracyMeters, accuracyMeters.isFinite, accuracyMeters >= 0 { variables["accuracyMeters"] = .number(accuracyMeters) }
            return variables
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
        id userId placeName isNamedByPerson placeLabel latitudeDegrees longitudeDegrees radiusMeters osmType osmId osmName osmCategory addressText isAddressTyped
        visitCount lastVisitAt namingErrorMessage createdAt updatedAt
        """
    static let placeVisitFields = """
        id arrivedAt departedAt durationSeconds latitudeDegrees longitudeDegrees accuracyMeters visitSource
        photoCount photosTakenBefore
        photos {
          id createdAt photoKind takenAt takenAtOffsetMinutes widthPixels heightPixels durationMS latitude longitude placeName cameraMake cameraModel
          originalFilename isFavorite isScreenshot photoProcessingState trashedAt smallPreviewUrl largePreviewUrl videoPreviewUrl
          photoFileRoles processingErrorMessage isOwnedByCaller ownerName
        }
        place {
          id userId placeName isNamedByPerson placeLabel latitudeDegrees longitudeDegrees radiusMeters osmType osmId osmName osmCategory addressText isAddressTyped
          visitCount lastVisitAt namingErrorMessage createdAt updatedAt
        }
        """
    static let locationPauseFields = """
        id startedAt endedAt
        """
    static let personLocationFields = """
        userId username name avatarUrl avatarColorIndex shareLevel
        pause { id startedAt endedAt }
        notificationMute { mutedUntil }
        latestPoint { latitudeDegrees longitudeDegrees accuracyMeters altitudeMeters speedMetersPerSecond courseDegrees measuredAt locationSource }
        openVisit {
          id arrivedAt departedAt durationSeconds latitudeDegrees longitudeDegrees accuracyMeters visitSource
          place {
            id userId placeName isNamedByPerson placeLabel latitudeDegrees longitudeDegrees radiusMeters osmType osmId osmName osmCategory addressText isAddressTyped
            visitCount lastVisitAt namingErrorMessage createdAt updatedAt
          }
        }
        """
    static let foundPlaceFields = """
        placeName osmType osmId osmCategory addressText latitudeDegrees longitudeDegrees distanceMeters placeId placeLabel isNamedByPerson
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
    public static let searchAddresses = """
        query ($query: String!, $latitudeDegrees: Float, $longitudeDegrees: Float, $placeId: String, $limitCount: Int) {
          SearchAddresses(query: $query, latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, placeId: $placeId, limitCount: $limitCount) { \(foundPlaceFields) }
        }
        """
    public static let locationPoints = """
        query ($userId: String, $from: DateTime!, $until: DateTime!) {
          ListLocationPoints(userId: $userId, from: $from, until: $until) { \(locationPointFields) }
        }
        """
    public static let reportLocation = """
        mutation ($agentPhoneId: String, $points: [ReportedPointInput!]!, $visits: [ReportedVisitInput!]!, $activities: [ReportedActivityInput!]!) {
          ReportLocation(agentPhoneId: $agentPhoneId, points: $points, visits: $visits, activities: $activities) {
            newPointCount newVisitCount updatedVisitCount pausedPointCount pausedVisitCount
            pause { id startedAt endedAt }
          }
        }
        """
    /// The same report for a batch with no motion activity: a server that
    /// has no `activities` argument takes it, and only a batch that carries
    /// activity needs the server to know the argument.
    public static let reportLocationWithoutActivities = """
        mutation ($agentPhoneId: String, $points: [ReportedPointInput!]!, $visits: [ReportedVisitInput!]!) {
          ReportLocation(agentPhoneId: $agentPhoneId, points: $points, visits: $visits) {
            newPointCount newVisitCount updatedVisitCount pausedPointCount pausedVisitCount
            pause { id startedAt endedAt }
          }
        }
        """
    public static let locationPauses = """
        query ($userId: String, $from: DateTime!, $until: DateTime!) {
          ListLocationPauses(userId: $userId, from: $from, until: $until) { \(locationPauseFields) }
        }
        """
    public static let pauseLocation = """
        mutation ($endedAt: DateTime) { PauseLocation(endedAt: $endedAt) { \(locationPauseFields) } }
        """
    public static let resumeLocation = """
        mutation { ResumeLocation }
        """
    public static let savePlace = """
        mutation ($placeId: String, $placeName: String, $placeLabel: String, $latitudeDegrees: Float, $longitudeDegrees: Float, $radiusMeters: Float, $addressText: String) {
          SavePlace(placeId: $placeId, placeName: $placeName, placeLabel: $placeLabel, latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, radiusMeters: $radiusMeters, addressText: $addressText) {
            \(locationPlaceFields)
          }
        }
        """
    public static let forgetPlace = """
        mutation ($placeId: String!) { ForgetPlace(placeId: $placeId) }
        """
    public static let checkIn = """
        mutation ($placeId: String, $latitudeDegrees: Float, $longitudeDegrees: Float, $accuracyMeters: Float, $placeName: String, $osmType: String, $osmId: Int64, $osmCategory: String, $addressText: String) {
          CheckIn(placeId: $placeId, latitudeDegrees: $latitudeDegrees, longitudeDegrees: $longitudeDegrees, accuracyMeters: $accuracyMeters, placeName: $placeName, osmType: $osmType, osmId: $osmId, osmCategory: $osmCategory, addressText: $addressText) {
            \(placeVisitFields)
          }
        }
        """
    public static let muteLocationNotifications = """
        mutation ($userId: String!, $mutedUntil: DateTime) {
          MuteLocationNotifications(userId: $userId, mutedUntil: $mutedUntil) { mutedUntil }
        }
        """
    public static let unmuteLocationNotifications = """
        mutation ($userId: String!) { UnmuteLocationNotifications(userId: $userId) }
        """
    public static let eraseLocationHistory = """
        mutation ($from: DateTime, $until: DateTime) {
          EraseLocationHistory(from: $from, until: $until) { pointCount visitCount placeCount }
        }
        """

    // Sharing where the person is with a circle or a person, and seeing the
    // people who share with them.
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
        "searchPlaces": searchPlaces, "searchAddresses": searchAddresses, "locationPoints": locationPoints, "reportLocation": reportLocation,
        "reportLocationWithoutActivities": reportLocationWithoutActivities, "savePlace": savePlace,
        "forgetPlace": forgetPlace, "checkIn": checkIn, "eraseLocationHistory": eraseLocationHistory, "locationPauses": locationPauses,
        "pauseLocation": pauseLocation, "resumeLocation": resumeLocation,
        "muteLocationNotifications": muteLocationNotifications, "unmuteLocationNotifications": unmuteLocationNotifications,
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

    /// Named places near a position, nearest first, for a check-in. With the
    /// place provider off the server answers the person's own places there.
    public func nearbyPlaces(latitudeDegrees: Double, longitudeDegrees: Double, radiusMeters: Double? = nil) async throws -> [FoundPlace] {
        struct Answer: Decodable, Sendable { let ListNearbyPlaces: [FoundPlace] }
        var variables: [String: JSONValue] = ["latitudeDegrees": .number(latitudeDegrees), "longitudeDegrees": .number(longitudeDegrees)]
        if let radiusMeters { variables["radiusMeters"] = .number(radiusMeters) }
        return try await execute(Documents.nearbyPlaces, variables: variables, as: Answer.self).ListNearbyPlaces
    }

    /// Places matching words near a position, from OpenStreetMap. With no
    /// position and no place, near where the person is now.
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

    /// Addresses matching words, from the place provider and anywhere in the
    /// world, nearest first to the given position, else to the place, else
    /// to the person's latest position. `distanceMeters` is from that
    /// center; with no center at all it is 0 and the best match is first.
    public func searchAddresses(
        query: String, latitudeDegrees: Double? = nil, longitudeDegrees: Double? = nil, placeId: String? = nil, limitCount: Int? = nil
    ) async throws -> [FoundPlace] {
        struct Answer: Decodable, Sendable { let SearchAddresses: [FoundPlace] }
        var variables: [String: JSONValue] = ["query": .string(query)]
        if let latitudeDegrees { variables["latitudeDegrees"] = .number(latitudeDegrees) }
        if let longitudeDegrees { variables["longitudeDegrees"] = .number(longitudeDegrees) }
        if let placeId { variables["placeId"] = .string(placeId) }
        if let limitCount { variables["limitCount"] = .number(Double(limitCount)) }
        return try await execute(Documents.searchAddresses, variables: variables, as: Answer.self).SearchAddresses
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
        let document = batch.activities.isEmpty ? Documents.reportLocationWithoutActivities : Documents.reportLocation
        return try await execute(document, variables: variables, as: Answer.self).ReportLocation
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

    /// Erases positions and visits in a range (with neither end, all of
    /// them), and the places that leaves with no visit that the person did
    /// not name or label. Places they named or labeled stay.
    @discardableResult
    public func eraseLocationHistory(from: Date? = nil, until: Date? = nil) async throws -> EraseLocationHistoryResult {
        struct Answer: Decodable, Sendable { let EraseLocationHistory: EraseLocationHistoryResult }
        var variables: [String: JSONValue] = [:]
        if let from { variables["from"] = .string(CalendarDates.text(from)) }
        if let until { variables["until"] = .string(CalendarDates.text(until)) }
        return try await execute(Documents.eraseLocationHistory, variables: variables, as: Answer.self).EraseLocationHistory
    }

    // MARK: Pausing

    /// The pauses that overlap a range, newest first: the person's own, or
    /// somebody sharing their history with the caller.
    public func locationPauses(userId: String? = nil, from: Date, until: Date) async throws -> [LocationPause] {
        struct Answer: Decodable, Sendable { let ListLocationPauses: [LocationPause] }
        var variables: [String: JSONValue] = ["from": .string(CalendarDates.text(from)), "until": .string(CalendarDates.text(until))]
        if let userId { variables["userId"] = .string(userId) }
        return try await execute(Documents.locationPauses, variables: variables, as: Answer.self).ListLocationPauses
    }

    /// Pauses recording now, until the end given or (with none) until the
    /// person resumes. With a pause already in effect, changes its end.
    @discardableResult
    public func pauseLocation(until endedAt: Date? = nil) async throws -> LocationPause {
        struct Answer: Decodable, Sendable { let PauseLocation: LocationPause }
        var variables: [String: JSONValue] = [:]
        if let endedAt { variables["endedAt"] = .string(CalendarDates.text(endedAt)) }
        return try await execute(Documents.pauseLocation, variables: variables, as: Answer.self).PauseLocation
    }

    /// Ends the pause in effect now. True when there was one.
    @discardableResult
    public func resumeLocation() async throws -> Bool {
        struct Answer: Decodable, Sendable { let ResumeLocation: Bool? }
        return try await execute(Documents.resumeLocation, as: Answer.self).ResumeLocation ?? false
    }

    // MARK: Muting notifications

    /// Mutes the arrived and left notices of a person sharing with the caller,
    /// until the end given or (with none) until the caller unmutes. Muting
    /// again changes the end.
    @discardableResult
    public func muteLocationNotifications(userId: String, until mutedUntil: Date? = nil) async throws -> LocationNotificationMute? {
        struct Answer: Decodable, Sendable { let MuteLocationNotifications: LocationNotificationMute? }
        var variables: [String: JSONValue] = ["userId": .string(userId)]
        if let mutedUntil { variables["mutedUntil"] = .string(CalendarDates.text(mutedUntil)) }
        return try await execute(Documents.muteLocationNotifications, variables: variables, as: Answer.self).MuteLocationNotifications
    }

    /// Ends the mute in effect. True when there was one.
    @discardableResult
    public func unmuteLocationNotifications(userId: String) async throws -> Bool {
        struct Answer: Decodable, Sendable { let UnmuteLocationNotifications: Bool? }
        let variables: [String: JSONValue] = ["userId": .string(userId)]
        return try await execute(Documents.unmuteLocationNotifications, variables: variables, as: Answer.self).UnmuteLocationNotifications ?? false
    }

    // MARK: Sharing

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
