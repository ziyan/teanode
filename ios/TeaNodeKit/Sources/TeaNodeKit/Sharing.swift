import Foundation

// Everything the person shares and everything shared with them, in one list,
// across calendar, location, Health, memory pages, photo albums, recordings,
// finance accounts, skills and passwords: the same operations the dashboard's
// Sharing page, the command line and the circle tool use. Stopping a share is
// the kind's own operation; what is new here is declining a share that was made
// to the person by name.

/// SharingKind is what a share is of. The server names them with these words.
public enum SharingKind: String, CaseIterable, Sendable {
    case calendar
    case location
    case health
    case memoryPage = "memory_page"
    case photoAlbum = "photo_album"
    case recording
    case finance
    case skill
    case passwords
}

/// SharingMade is one share the person made: of what, with whom, at what level.
/// A share of passwords has no id of its own and is counted, never named,
/// because an item's title is sealed.
public struct SharingMade: Decodable, Hashable, Sendable, Identifiable {
    public let shareKind: String
    /// Empty for passwords.
    public let shareId: String
    /// The calendar's, album's, recording's or finance account's identifier,
    /// the page's path or the skill's name; empty for location, Health and
    /// passwords.
    public let subjectId: String
    /// A recording's title or a finance account's name, among the rest.
    public let subjectName: String
    /// The last digits of a finance account's number; empty for every other kind.
    public let subjectMask: String
    /// True for a share of a memory page that no longer exists, which the owner
    /// can still stop; its subjectName is then the path. Absent from an older server.
    public let isSubjectDeleted: Bool?
    /// The level of a calendar, location, album, recording or finance share
    /// (busy, titles, details, edit; current, history; view, contribute; summary,
    /// transcript; balance, spending, transactions), empty for the rest.
    public let shareLevel: String
    /// What else it gives: the Health categories, the skill's tools, quotes for
    /// a page and location for an album.
    public let shareScopes: [String]
    public let passwordItemCount: Int
    public let granteeUserId: String?
    public let granteeUsername: String
    public let granteeName: String
    public let granteeCircleId: String?
    public let granteeCircleName: String
    public let createdAt: String

    public var id: String { shareKind + "|" + (shareId.isEmpty ? (granteeUserId ?? granteeCircleId ?? "") : shareId) }

    /// The kind, or nil for one the app does not know.
    public var knownKind: SharingKind? { SharingKind(rawValue: shareKind) }

    /// Whether it is shared with a circle rather than a person.
    public var isCircle: Bool { granteeCircleId != nil }

    /// Who it is shared with: the circle's name, or the person's name, or their
    /// username when they have no name.
    public var shownGrantee: String {
        if isCircle { return granteeCircleName }
        return granteeName.isEmpty ? granteeUsername : granteeName
    }

    /// Whether a button can stop it: every kind but passwords, which are
    /// stopped item by item on the Passwords screen.
    public var canStop: Bool {
        guard let knownKind else { return false }
        return knownKind != .passwords
    }

    /// Whether the page that was shared is gone.
    public var isPageGone: Bool { isSubjectDeleted == true }

    /// When it was made, or nil when the server's text cannot be read.
    public var since: Date? { ServerTime.date(createdAt) }

    public init(
        shareKind: String, shareId: String, subjectId: String = "", subjectName: String = "", subjectMask: String = "",
        isSubjectDeleted: Bool? = nil, shareLevel: String = "",
        shareScopes: [String] = [], passwordItemCount: Int = 0, granteeUserId: String? = nil, granteeUsername: String = "",
        granteeName: String = "", granteeCircleId: String? = nil, granteeCircleName: String = "", createdAt: String = ""
    ) {
        self.shareKind = shareKind
        self.shareId = shareId
        self.subjectId = subjectId
        self.subjectName = subjectName
        self.subjectMask = subjectMask
        self.isSubjectDeleted = isSubjectDeleted
        self.shareLevel = shareLevel
        self.shareScopes = shareScopes
        self.passwordItemCount = passwordItemCount
        self.granteeUserId = granteeUserId
        self.granteeUsername = granteeUsername
        self.granteeName = granteeName
        self.granteeCircleId = granteeCircleId
        self.granteeCircleName = granteeCircleName
        self.createdAt = createdAt
    }
}

/// SharingReceived is one share made to the person. A share through a circle
/// names the circle and ends when the person leaves it; one made by name can be
/// declined, except passwords, which their owner removes.
public struct SharingReceived: Decodable, Hashable, Sendable, Identifiable {
    public let shareKind: String
    public let shareId: String
    public let subjectId: String
    public let subjectName: String
    public let subjectMask: String
    public let shareLevel: String
    public let shareScopes: [String]
    public let passwordItemCount: Int
    public let ownerUserId: String
    public let ownerUsername: String
    public let ownerName: String
    public let granteeCircleId: String?
    public let granteeCircleName: String
    /// Whether it was made to the person by name.
    public let isDirect: Bool
    /// Whether DeclineShare ends it.
    public let canDecline: Bool
    public let createdAt: String

    public var id: String { shareKind + "|" + (shareId.isEmpty ? ownerUserId + "|" + (granteeCircleId ?? "") : shareId) }

    public var knownKind: SharingKind? { SharingKind(rawValue: shareKind) }

    /// Who shared it: their name, or their username when they have none.
    public var shownOwner: String { ownerName.isEmpty ? ownerUsername : ownerName }

    public var since: Date? { ServerTime.date(createdAt) }

    public init(
        shareKind: String, shareId: String, subjectId: String = "", subjectName: String = "", subjectMask: String = "", shareLevel: String = "",
        shareScopes: [String] = [], passwordItemCount: Int = 0, ownerUserId: String, ownerUsername: String, ownerName: String = "",
        granteeCircleId: String? = nil, granteeCircleName: String = "", isDirect: Bool, canDecline: Bool, createdAt: String = ""
    ) {
        self.shareKind = shareKind
        self.shareId = shareId
        self.subjectId = subjectId
        self.subjectName = subjectName
        self.subjectMask = subjectMask
        self.shareLevel = shareLevel
        self.shareScopes = shareScopes
        self.passwordItemCount = passwordItemCount
        self.ownerUserId = ownerUserId
        self.ownerUsername = ownerUsername
        self.ownerName = ownerName
        self.granteeCircleId = granteeCircleId
        self.granteeCircleName = granteeCircleName
        self.isDirect = isDirect
        self.canDecline = canDecline
        self.createdAt = createdAt
    }
}

/// SharingList is what the person shares and what is shared with them.
public struct SharingList: Decodable, Hashable, Sendable {
    public let sharesMade: [SharingMade]
    public let sharesReceived: [SharingReceived]

    public init(sharesMade: [SharingMade], sharesReceived: [SharingReceived]) {
        self.sharesMade = sharesMade
        self.sharesReceived = sharesReceived
    }

    public var isEmpty: Bool { sharesMade.isEmpty && sharesReceived.isEmpty }
}

extension Documents {
    public static let sharing = """
        query {
          ListSharing {
            sharesMade { shareKind shareId subjectId subjectName subjectMask isSubjectDeleted shareLevel shareScopes passwordItemCount granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName createdAt }
            sharesReceived { shareKind shareId subjectId subjectName subjectMask shareLevel shareScopes passwordItemCount ownerUserId ownerUsername ownerName granteeCircleId granteeCircleName isDirect canDecline createdAt }
          }
        }
        """
    public static let declineShare = """
        mutation ($shareKind: String!, $shareId: String!) { DeclineShare(shareKind: $shareKind, shareId: $shareId) }
        """
}

extension Connection {
    /// What the person shares and what is shared with them.
    public func sharing() async throws -> SharingList {
        struct Answer: Decodable, Sendable { let ListSharing: SharingList }
        return try await execute(Documents.sharing, as: Answer.self).ListSharing
    }

    /// Takes a share back with the kind's own operation. A share of passwords
    /// is stopped item by item on the Passwords screen, so it is refused here.
    public func stopSharing(_ share: SharingMade) async throws {
        guard let kind = share.knownKind else { throw SharingError.unknownKind(share.shareKind) }
        switch kind {
        case .calendar: try await unshareCalendar(shareId: share.shareId)
        case .location: try await unshareLocation(shareId: share.shareId)
        case .health:
            try await setHealthShare(granteeUserId: share.granteeUserId, granteeCircleId: share.granteeCircleId, healthCategories: [])
        case .memoryPage: try await unshareKnowledgePage(shareId: share.shareId)
        case .photoAlbum: try await removePhotoAlbumShare(share.shareId)
        case .recording: try await unshareRecording(shareId: share.shareId)
        case .finance: try await unshareFinanceAccount(shareId: share.shareId)
        case .skill: try await unshareSkill(shareId: share.shareId)
        case .passwords: throw SharingError.passwordsAreStoppedOnTheirOwnScreen
        }
    }

    /// Stops receiving a share that was made to the person by name. The owner
    /// is not told.
    public func declineShare(_ share: SharingReceived) async throws {
        struct Answer: Decodable, Sendable { let DeclineShare: Bool? }
        _ = try await execute(
            Documents.declineShare, variables: ["shareKind": .string(share.shareKind), "shareId": .string(share.shareId)], as: Answer.self)
    }
}

/// SharingError is why a share could not be stopped from the list.
public enum SharingError: Error, Equatable, Sendable {
    case unknownKind(String)
    case passwordsAreStoppedOnTheirOwnScreen
}
