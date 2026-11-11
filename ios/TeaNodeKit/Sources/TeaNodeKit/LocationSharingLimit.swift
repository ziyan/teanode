import Foundation

// What limits how much of other people's location the person sees: their own
// phone's location permission, the recording switch and a pause. The server
// applies the rule; this file reads its answer and decides what to tell the
// person about it.

/// LocationPhonePermission is the phone's location permission as the server
/// takes it: Always, While Using, or off (denied, restricted, or not yet
/// chosen).
public enum LocationPhonePermission: String, CaseIterable, Sendable {
    case always
    case whileUsing = "while_using"
    case off
}

/// LocationPermissionReport is what the phone tells the server about its own
/// permission and recording switch. Equatable so the phone sends it only when
/// it changed.
public struct LocationPermissionReport: Equatable, Sendable {
    public let phonePermission: LocationPhonePermission
    public let isRecordingOn: Bool

    public init(phonePermission: LocationPhonePermission, isRecordingOn: Bool) {
        self.phonePermission = phonePermission
        self.isRecordingOn = isRecordingOn
    }
}

/// LocationSharingLimit is the most of anybody's location the person may see,
/// and why it is less than everything.
public struct LocationSharingLimit: Codable, Hashable, Sendable {
    /// "history", "current", or "" for nothing.
    public let shareLevelLimit: String
    /// "", "paused", "recording_off", "phone_location_off" or "phone_while_using".
    public let sharingLimitReason: String
    /// "always", "while_using" or "off"; nil when the app never reported.
    public let phonePermission: String?
    public let isRecordingOn: Bool?
    /// RFC 3339; nil when the app never reported.
    public let reportedAt: String?

    public init(
        shareLevelLimit: String, sharingLimitReason: String, phonePermission: String? = nil, isRecordingOn: Bool? = nil,
        reportedAt: String? = nil
    ) {
        self.shareLevelLimit = shareLevelLimit
        self.sharingLimitReason = sharingLimitReason
        self.phonePermission = phonePermission
        self.isRecordingOn = isRecordingOn
        self.reportedAt = reportedAt
    }
}

/// LocationReducedPerson is somebody the person sees less of because of what
/// the person shares back with them.
public struct LocationReducedPerson: Equatable, Sendable {
    public let userId: String
    public let displayName: String
    /// Whether the person sees nothing of them (the person shares nothing
    /// with them), else only where they are and not their visits.
    public let isSeeingNothing: Bool

    public init(userId: String, displayName: String, isSeeingNothing: Bool) {
        self.userId = userId
        self.displayName = displayName
        self.isSeeingNothing = isSeeingNothing
    }
}

/// LocationSharingBanner is the one notice the top of the people list shows.
public enum LocationSharingBanner: Equatable, Sendable {
    /// The person's recording is paused.
    case paused
    /// The recording switch is off.
    case recordingOff
    /// Location is off for the app.
    case phoneLocationOff
    /// Location is While Using: the person sees where people are, not visits.
    case phoneWhileUsing
    /// No limit of the person's own, but they share less back than others
    /// share with them.
    case sharesLess(people: [LocationReducedPerson])
}

/// LocationSharingNotice decides which banner applies. The server's rule: what
/// the person sees of somebody is the lowest of what that person's shares give
/// them, what the person shares back, and the person's own limit.
public enum LocationSharingNotice {
    /// The most people named in the banner before "and N more".
    public static let mostNamedPeople = 3

    /// Levels in order: "" is nothing, current is where they are, history adds
    /// their visits. Any other level ("owner") is above all.
    public static func rank(of shareLevel: String) -> Int {
        switch shareLevel {
        case "": 0
        case "current": 1
        case "history": 2
        default: 3
        }
    }

    /// The people the person sees less of because of what they share back:
    /// those whose shown level is below the lower of what they grant and the
    /// person's own limit. A person whose granted level the server did not
    /// say is left out.
    public static func reducedPeople(limit: LocationSharingLimit?, people: [PersonLocation]) -> [LocationReducedPerson] {
        let limitRank = limit.map { rank(of: $0.shareLevelLimit) } ?? rank(of: "history")
        var reduced: [LocationReducedPerson] = []
        for person in people {
            guard let grantedShareLevel = person.grantedShareLevel else { continue }
            let possibleRank = min(rank(of: grantedShareLevel), limitRank)
            guard rank(of: person.shareLevel) < possibleRank else { continue }
            reduced.append(
                LocationReducedPerson(userId: person.userId, displayName: person.displayName, isSeeingNothing: person.shareLevel.isEmpty))
        }
        return reduced
    }

    /// The banner that applies, or nil for none. A limit of the person's own
    /// comes first; the people reduced by what they share back are on their
    /// rows besides.
    public static func banner(limit: LocationSharingLimit?, people: [PersonLocation]) -> LocationSharingBanner? {
        switch limit?.sharingLimitReason {
        case "paused": return .paused
        case "recording_off": return .recordingOff
        case "phone_location_off": return .phoneLocationOff
        case "phone_while_using": return .phoneWhileUsing
        default: break
        }
        let reduced = reducedPeople(limit: limit, people: people)
        if reduced.isEmpty { return nil }
        return .sharesLess(people: reduced)
    }
}
