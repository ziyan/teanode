import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

// Sharing an album with a person or a circle, keeping a photo someone else
// shared, and the corrections the library offers on the person's own photos:
// the kind and date filters and the date a photo is placed at.

// MARK: Kind and date filters

/// PhotoKindFilter is the kind of photo the library is narrowed to. The raw
/// values are the server's `photoKind`.
public enum PhotoKindFilter: String, CaseIterable, Identifiable, Hashable, Sendable {
    case all = ""
    case picture
    case livePhoto
    case video

    public var id: String { rawValue }
}

/// PhotoNearFilter keeps the photos taken within a distance of a place: the
/// place of one photo, and a kilometer round it unless said otherwise.
public struct PhotoNearFilter: Hashable, Sendable {
    /// How far round a photo "Photos taken near here" looks.
    public static let defaultDistanceKilometers = 1.0

    public let latitudeDegrees: Double
    public let longitudeDegrees: Double
    public let distanceKilometers: Double
    /// The name of the place, for the filter to say what it is near; empty
    /// when the photo has coordinates and no name.
    public let placeName: String

    public init(latitudeDegrees: Double, longitudeDegrees: Double, distanceKilometers: Double = PhotoNearFilter.defaultDistanceKilometers, placeName: String = "") {
        self.latitudeDegrees = latitudeDegrees
        self.longitudeDegrees = longitudeDegrees
        self.distanceKilometers = distanceKilometers
        self.placeName = placeName.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// The filter for the place of a photo; nil for a photo that has none, as
    /// a photo shared without places does.
    public init?(near photo: Photo, distanceKilometers: Double = PhotoNearFilter.defaultDistanceKilometers) {
        guard let latitude = photo.latitude, let longitude = photo.longitude else { return nil }
        self.init(latitudeDegrees: latitude, longitudeDegrees: longitude, distanceKilometers: distanceKilometers, placeName: photo.placeName)
    }
}

/// PhotoFilter narrows a list of the library by kind, by the days the photos
/// were taken on, by how near a place they were taken and by the person in the
/// circles they are of. An empty filter keeps everything; the choices combine.
public struct PhotoFilter: Hashable, Sendable {
    public var photoKindFilter: PhotoKindFilter
    /// The first day to keep, as any moment on that day; nil for no start.
    public var firstDay: Date?
    /// The last day to keep, as any moment on that day; nil for no end.
    public var lastDay: Date?
    /// The place the photos kept were taken near; nil for anywhere.
    public var photoNearFilter: PhotoNearFilter?
    /// The ID of the person in the circles whose photos are kept (the photos of
    /// the contact the caller linked to them); nil for everybody.
    public var accountUserId: String?
    /// The name the chip shows for accountUserId. It is not sent.
    public var accountName: String

    public init(
        photoKindFilter: PhotoKindFilter = .all, firstDay: Date? = nil, lastDay: Date? = nil, photoNearFilter: PhotoNearFilter? = nil,
        accountUserId: String? = nil, accountName: String = ""
    ) {
        self.photoKindFilter = photoKindFilter
        self.firstDay = firstDay
        self.lastDay = lastDay
        self.photoNearFilter = photoNearFilter
        self.accountUserId = accountUserId
        self.accountName = accountName
    }

    /// Whether anything is filtered.
    public var isActive: Bool {
        photoKindFilter != .all || firstDay != nil || lastDay != nil || photoNearFilter != nil || !(accountUserId ?? "").isEmpty
    }

    /// Whether the days are asked for backwards, which no photo is in.
    public var isRangeBackwards: Bool {
        guard let firstDay, let lastDay else { return false }
        return firstDay > lastDay
    }

    /// The variables ListPhotos takes for this filter. A day is the phone's
    /// calendar day, not the day on the clock where the photo was taken: the
    /// range starts when the first day starts and ends where the day after the
    /// last begins, as the dashboard's does, since the server keeps photos
    /// between two moments.
    public func variables(calendar: Calendar = .current) -> [String: JSONValue] {
        var variables: [String: JSONValue] = [:]
        if photoKindFilter != .all { variables["photoKind"] = .string(photoKindFilter.rawValue) }
        if let firstDay { variables["takenAfter"] = .string(Self.text(of: calendar.startOfDay(for: firstDay))) }
        if let lastDay, let next = calendar.date(byAdding: .day, value: 1, to: calendar.startOfDay(for: lastDay)) {
            variables["takenBefore"] = .string(Self.text(of: next))
        }
        if let accountUserId, !accountUserId.isEmpty { variables["accountUserId"] = .string(accountUserId) }
        // The server wants all three of the place or none.
        if let photoNearFilter {
            variables["nearLatitude"] = .number(photoNearFilter.latitudeDegrees)
            variables["nearLongitude"] = .number(photoNearFilter.longitudeDegrees)
            variables["nearDistanceKilometers"] = .number(photoNearFilter.distanceKilometers)
        }
        return variables
    }

    private static func text(of date: Date) -> String {
        ISO8601DateFormatter().string(from: date)
    }
}

// MARK: Changing the date of a photo

/// PhotoDateChange writes the date a person chose for a photo the way the
/// server wants it.
public enum PhotoDateChange {
    /// The moment as RFC 3339 with the offset the photo is shown at, such as
    /// "2026-10-15T14:42:00-04:00". The photo's own offset when it has one
    /// (so that a trip abroad keeps its clock), else the phone's at that
    /// moment. Zero is written "+00:00", never "Z", which would say the place
    /// is on UTC.
    public static func takenAtText(for date: Date, photo: Photo, phoneTimeZone: TimeZone = .current) -> String {
        let offsetSeconds = photo.takenAtOffsetMinutes.map { $0 * 60 } ?? phoneTimeZone.secondsFromGMT(for: date)
        return takenAtText(for: date, offsetSeconds: offsetSeconds)
    }

    public static func takenAtText(for date: Date, offsetSeconds: Int) -> String {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(secondsFromGMT: offsetSeconds) ?? .gmt
        let parts = calendar.dateComponents([.year, .month, .day, .hour, .minute, .second], from: date)
        let sign = offsetSeconds < 0 ? "-" : "+"
        let absolute = abs(offsetSeconds)
        return String(
            format: "%04ld-%02ld-%02ldT%02ld:%02ld:%02ld%@%02ld:%02ld", parts.year ?? 0, parts.month ?? 0, parts.day ?? 0, parts.hour ?? 0, parts.minute ?? 0,
            parts.second ?? 0, sign, absolute / 3600, (absolute % 3600) / 60)
    }

    /// The zone a photo's date is edited in: where it was taken when the
    /// photo says, else the phone's.
    public static func timeZone(of photo: Photo, phoneTimeZone: TimeZone = .current) -> TimeZone {
        PhotoTimeline.moment(of: photo, phoneTimeZone: phoneTimeZone)?.timeZone ?? phoneTimeZone
    }
}

// MARK: Sharing an album

/// PhotoShareLevel is what a share lets the people it reaches do. The raw
/// values are the server's.
public enum PhotoShareLevel: String, CaseIterable, Identifiable, Hashable, Sendable {
    /// See the photos and save them.
    case view
    /// Also add photos of their own and take out the ones they added.
    case contribute

    public var id: String { rawValue }
}

/// PhotoAlbumShare is one share of an album: with a person or with a circle.
public struct PhotoAlbumShare: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let photoAlbumId: String
    /// Who it is shared with: a person or a circle, never both.
    public let granteeUserId: String?
    public let granteeUsername: String
    public let granteeName: String
    public let granteeAvatarUrl: String?
    public let granteeAvatarColorIndex: Int
    public let granteeCircleId: String?
    public let granteeCircleName: String
    /// "view" or "contribute".
    public let shareLevel: String
    /// Whether the people reached see where the photos were taken and receive
    /// the original files.
    public let isLocationIncluded: Bool
    public let createdAt: String

    public init(
        id: String, photoAlbumId: String, granteeUserId: String? = nil, granteeUsername: String = "", granteeName: String = "", granteeAvatarUrl: String? = nil,
        granteeAvatarColorIndex: Int = 0, granteeCircleId: String? = nil, granteeCircleName: String = "", shareLevel: String = "view",
        isLocationIncluded: Bool = false, createdAt: String = ""
    ) {
        self.id = id
        self.photoAlbumId = photoAlbumId
        self.granteeUserId = granteeUserId
        self.granteeUsername = granteeUsername
        self.granteeName = granteeName
        self.granteeAvatarUrl = granteeAvatarUrl
        self.granteeAvatarColorIndex = granteeAvatarColorIndex
        self.granteeCircleId = granteeCircleId
        self.granteeCircleName = granteeCircleName
        self.shareLevel = shareLevel
        self.isLocationIncluded = isLocationIncluded
        self.createdAt = createdAt
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        photoAlbumId = try container.decode(String.self, forKey: .photoAlbumId)
        granteeUserId = try container.decodeIfPresent(String.self, forKey: .granteeUserId)
        granteeUsername = try container.decodeIfPresent(String.self, forKey: .granteeUsername) ?? ""
        granteeName = try container.decodeIfPresent(String.self, forKey: .granteeName) ?? ""
        granteeAvatarUrl = try container.decodeIfPresent(String.self, forKey: .granteeAvatarUrl)
        granteeAvatarColorIndex = try container.decodeIfPresent(Int.self, forKey: .granteeAvatarColorIndex) ?? 0
        granteeCircleId = try container.decodeIfPresent(String.self, forKey: .granteeCircleId)
        granteeCircleName = try container.decodeIfPresent(String.self, forKey: .granteeCircleName) ?? ""
        shareLevel = try container.decode(String.self, forKey: .shareLevel)
        isLocationIncluded = try container.decodeIfPresent(Bool.self, forKey: .isLocationIncluded) ?? false
        createdAt = try container.decodeIfPresent(String.self, forKey: .createdAt) ?? ""
    }

    public var isWithCircle: Bool { granteeCircleId != nil }
    public var knownShareLevel: PhotoShareLevel { PhotoShareLevel(rawValue: shareLevel) ?? .view }

    /// What a list calls who it is shared with: the circle's name, or the
    /// person's name, or their username when they have no name.
    public var granteeDisplayName: String {
        if isWithCircle { return granteeCircleName }
        return granteeName.isEmpty ? granteeUsername : granteeName
    }
}

/// PhotoShareTarget is who an album is about to be shared with: one person or
/// one circle.
public enum PhotoShareTarget: Hashable, Sendable {
    case person(userId: String)
    case circle(circleId: String)
}

/// PhotoShareDraft is a new share as the sheet holds it.
public struct PhotoShareDraft: Hashable, Sendable {
    public var target: PhotoShareTarget?
    public var photoShareLevel = PhotoShareLevel.view
    public var isLocationIncluded = false

    public init() {}

    /// Whether the share can be sent: somebody is chosen.
    public var canSubmit: Bool { target != nil }

    func variables(photoAlbumId: String) -> [String: JSONValue] {
        var variables: [String: JSONValue] = [
            "photoAlbumId": .string(photoAlbumId), "shareLevel": .string(photoShareLevel.rawValue), "isLocationIncluded": .bool(isLocationIncluded),
        ]
        switch target {
        case .person(let userId)?: variables["granteeUserId"] = .string(userId)
        case .circle(let circleId)?: variables["granteeCircleId"] = .string(circleId)
        case nil: break
        }
        return variables
    }
}

extension Documents {
    static let photoAlbumShareFields = """
        id photoAlbumId granteeUserId granteeUsername granteeName granteeAvatarUrl granteeAvatarColorIndex granteeCircleId granteeCircleName
        shareLevel isLocationIncluded createdAt
        """
    public static let listPhotoAlbumShares = """
        query ($photoAlbumId: String!) { ListPhotoAlbumShares(photoAlbumId: $photoAlbumId) { \(photoAlbumShareFields) } }
        """
    public static let sharePhotoAlbum = """
        mutation ($photoAlbumId: String!, $granteeUserId: String, $granteeCircleId: String, $shareLevel: String!, $isLocationIncluded: Boolean) {
          SharePhotoAlbum(photoAlbumId: $photoAlbumId, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, shareLevel: $shareLevel, isLocationIncluded: $isLocationIncluded) { \(photoAlbumShareFields) }
        }
        """
    public static let updatePhotoAlbumShare = """
        mutation ($photoAlbumShareId: String!, $shareLevel: String!, $isLocationIncluded: Boolean) {
          UpdatePhotoAlbumShare(photoAlbumShareId: $photoAlbumShareId, shareLevel: $shareLevel, isLocationIncluded: $isLocationIncluded) { \(photoAlbumShareFields) }
        }
        """
    public static let removePhotoAlbumShare = """
        mutation ($photoAlbumShareId: String!) { RemovePhotoAlbumShare(photoAlbumShareId: $photoAlbumShareId) }
        """
    public static let savePhotoToLibrary = """
        mutation ($photoId: String!) { SavePhotoToLibrary(photoId: $photoId) { \(photoFields) } }
        """
    public static let savePhotoFromAttachment = """
        mutation ($agentAttachmentId: String!) { SavePhotoFromAttachment(agentAttachmentId: $agentAttachmentId) { \(photoFields) } }
        """
}

extension Connection {
    /// The shares of an album, which only its managers may read.
    public func photoAlbumShares(_ photoAlbumId: String) async throws -> [PhotoAlbumShare] {
        struct Answer: Decodable, Sendable { let ListPhotoAlbumShares: [PhotoAlbumShare] }
        return try await execute(Documents.listPhotoAlbumShares, variables: ["photoAlbumId": .string(photoAlbumId)], as: Answer.self).ListPhotoAlbumShares
    }

    /// Shares an album with a person or a circle, or changes the level they
    /// have.
    public func sharePhotoAlbum(_ photoAlbumId: String, draft: PhotoShareDraft) async throws -> PhotoAlbumShare {
        struct Answer: Decodable, Sendable { let SharePhotoAlbum: PhotoAlbumShare }
        return try await execute(Documents.sharePhotoAlbum, variables: draft.variables(photoAlbumId: photoAlbumId), as: Answer.self).SharePhotoAlbum
    }

    /// Changes the level of one share and whether it includes where the
    /// photos were taken.
    public func updatePhotoAlbumShare(_ photoAlbumShareId: String, photoShareLevel: PhotoShareLevel, isLocationIncluded: Bool) async throws -> PhotoAlbumShare {
        struct Answer: Decodable, Sendable { let UpdatePhotoAlbumShare: PhotoAlbumShare }
        return try await execute(
            Documents.updatePhotoAlbumShare,
            variables: [
                "photoAlbumShareId": .string(photoAlbumShareId), "shareLevel": .string(photoShareLevel.rawValue), "isLocationIncluded": .bool(isLocationIncluded),
            ], as: Answer.self
        ).UpdatePhotoAlbumShare
    }

    /// Takes a share back. The photos the people reached added stay theirs.
    public func removePhotoAlbumShare(_ photoAlbumShareId: String) async throws {
        struct Answer: Decodable, Sendable { let RemovePhotoAlbumShare: Bool }
        _ = try await execute(Documents.removePhotoAlbumShare, variables: ["photoAlbumShareId": .string(photoAlbumShareId)], as: Answer.self)
    }

    /// Keeps a photo of a shared album as a photo of the person's own, and
    /// returns the copy. Saving the same photo twice finds the copy there.
    public func savePhotoToLibrary(_ photoId: String) async throws -> Photo {
        struct Answer: Decodable, Sendable { let SavePhotoToLibrary: Photo }
        return try await execute(Documents.savePhotoToLibrary, variables: ["photoId": .string(photoId)], as: Answer.self).SavePhotoToLibrary
    }

    /// Keeps a picture or video of a conversation as a photo in the person's
    /// library, and returns it.
    public func savePhotoFromAttachment(_ agentAttachmentId: String) async throws -> Photo {
        struct Answer: Decodable, Sendable { let SavePhotoFromAttachment: Photo }
        return try await execute(
            Documents.savePhotoFromAttachment, variables: ["agentAttachmentId": .string(agentAttachmentId)], as: Answer.self
        ).SavePhotoFromAttachment
    }
}

// MARK: Files a download leaves behind

/// PhotoTemporaryFiles removes what downloads leave in the temporary folder.
/// A download is a folder named by a UUID holding one file; whoever asked for it
/// removes it when done, but a share sheet reads its file after the code that
/// made it has returned, and an app that is killed removes nothing.
public enum PhotoTemporaryFiles {
    /// How long a share sheet is given to copy a file before its folder goes.
    public static let shareGraceSeconds = 600

    /// Removes the folder a download is in after a while.
    public static func removeLater(_ download: PhotoDownload, afterSeconds seconds: Int = shareGraceSeconds) {
        let folder = download.fileURL.deletingLastPathComponent()
        // Never the temporary folder itself, should a file have been placed
        // directly in it.
        guard folder.standardizedFileURL != FileManager.default.temporaryDirectory.standardizedFileURL else {
            Task.detached {
                try? await Task.sleep(for: .seconds(seconds))
                try? FileManager.default.removeItem(at: download.fileURL)
            }
            return
        }
        Task.detached {
            try? await Task.sleep(for: .seconds(seconds))
            try? FileManager.default.removeItem(at: folder)
        }
    }

    /// Removes the folders and files named by a UUID that are older than the
    /// age, in the temporary folder. Run once as the app starts, when nothing
    /// of this launch is using them. Returns how many went.
    @discardableResult
    public static func sweep(olderThanSeconds age: TimeInterval = 3600, in directory: URL = FileManager.default.temporaryDirectory, now: Date = Date()) -> Int {
        let manager = FileManager.default
        guard let entries = try? manager.contentsOfDirectory(at: directory, includingPropertiesForKeys: [.contentModificationDateKey], options: []) else { return 0 }
        var removedCount = 0
        for entry in entries {
            let name = entry.deletingPathExtension().lastPathComponent
            guard UUID(uuidString: name) != nil,
                let modified = try? entry.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate,
                now.timeIntervalSince(modified) > age
            else { continue }
            if (try? manager.removeItem(at: entry)) != nil { removedCount += 1 }
        }
        return removedCount
    }
}
