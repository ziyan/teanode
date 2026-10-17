import Foundation

/// CircleKind is what a circle is for: it is how screens describe a circle
/// ("Family", "Colleagues"), where the word circle itself would read oddly.
public enum CircleKind: String, CaseIterable, Sendable {
    case family
    case friends
    case colleagues
}

/// CircleMemberRole is what somebody may do in a circle. An admin manages who
/// is in it; a role never decides what anybody sees of anybody else.
public enum CircleMemberRole: String, Sendable {
    case admin
    case member
}

/// CircleMember is somebody in a circle, as the circle lists them.
public struct CircleMember: Codable, Hashable, Sendable, Identifiable {
    public let userId: String
    /// What they sign in with.
    public let username: String
    /// What to call them, when they have said.
    public let name: String?
    /// Where their picture is, a path to read with the app's token; nil when
    /// they have none.
    public let avatarUrl: String?
    /// Which of the twelve colors their initial is drawn on without a picture.
    public let avatarColorIndex: Int?
    /// admin or member.
    public let memberRole: String
    public let joinedAt: String

    public var id: String { userId }
    public var isAdmin: Bool { memberRole == CircleMemberRole.admin.rawValue }

    /// What a list of people calls them: their name, or the username when they
    /// have none.
    public var displayName: String {
        guard let name, !name.isEmpty else { return username }
        return name
    }

    public init(
        userId: String, username: String, name: String? = nil, avatarUrl: String? = nil, avatarColorIndex: Int? = nil,
        memberRole: String, joinedAt: String
    ) {
        self.userId = userId
        self.username = username
        self.name = name
        self.avatarUrl = avatarUrl
        self.avatarColorIndex = avatarColorIndex
        self.memberRole = memberRole
        self.joinedAt = joinedAt
    }
}

/// CircleInvitation is an invitation to join a circle: to the person it was
/// sent to, with the circle's name and who asked, and to the circle's admins,
/// as one still waiting for an answer.
public struct CircleInvitation: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let circleId: String
    public let circleName: String
    /// family, friends or colleagues.
    public let circleKind: String
    public let invitedByName: String
    public let invitedUsername: String
    /// The role they join with: admin or member.
    public let memberRole: String
    public let createdAt: String
    public let expiresAt: String

    public var isAdminInvitation: Bool { memberRole == CircleMemberRole.admin.rawValue }
    public var knownCircleKind: CircleKind? { CircleKind(rawValue: circleKind) }

    public init(
        id: String, circleId: String, circleName: String, circleKind: String, invitedByName: String, invitedUsername: String,
        memberRole: String, createdAt: String, expiresAt: String
    ) {
        self.id = id
        self.circleId = circleId
        self.circleName = circleName
        self.circleKind = circleKind
        self.invitedByName = invitedByName
        self.invitedUsername = invitedUsername
        self.memberRole = memberRole
        self.createdAt = createdAt
        self.expiresAt = expiresAt
    }
}

/// CircleCalendarOffer is the question asked once, as a person joins a family or
/// a group of colleagues: share their calendar with everyone in it, at a level
/// they choose, or not now. For a family it also asks about location, in the
/// same sheet: share where they are, now and where they have been, only where
/// they are now, or not now. It is asked only as the invitation is accepted and
/// never again, as the dashboard's Circles page does; the calendar screen and
/// Location's Sharing have the same choices whenever they want them. Friends are
/// not asked, since neither is what friends are assumed to share.
public struct CircleCalendarOffer: Hashable, Sendable, Identifiable {
    public let circleId: String
    public let circleName: String
    /// The person's own calendar of events, which is what would be shared; nil
    /// when they have none, and then only a family's location is offered.
    public let calendarId: String?
    /// Whether where the person is can be shared too: a family, to a person
    /// who may use Location.
    public let isLocationOffered: Bool

    public var id: String { circleId }

    /// Whether the calendar is part of the question.
    public var isCalendarOffered: Bool { calendarId != nil }

    /// The levels offered: edit is a choice made on the calendar screen, not
    /// in passing.
    public static let levels: [CalendarShareLevel] = [.busy, .titles, .details]

    /// The location levels offered, the fuller one first. "Not now" is the
    /// absence of a choice.
    public static let locationLevels: [LocationShareLevel] = [.history, .current]

    /// The offer for an invitation just accepted, or nil for a circle of
    /// friends, and when there is nothing to offer: the person has no
    /// calendar to share and the location is not offered. The location is
    /// offered for a family, to a person who holds location:use
    /// (`canShareLocation`); one who does not would be refused when sharing.
    public init?(accepting invitation: CircleInvitation, calendarId: String?, canShareLocation: Bool) {
        guard invitation.circleKind != CircleKind.friends.rawValue else { return nil }
        let isLocationOffered = invitation.circleKind == CircleKind.family.rawValue && canShareLocation
        guard calendarId != nil || isLocationOffered else { return nil }
        circleId = invitation.circleId
        circleName = invitation.circleName
        self.calendarId = calendarId
        self.isLocationOffered = isLocationOffered
    }

    /// The calendar a share would be of: the person's own calendar of events,
    /// not their reminders or one shared with them.
    public static func ownCalendar(among calendars: [AgentCalendar]) -> AgentCalendar? {
        calendars.first { $0.isOwn && $0.calendarKind == "events" }
    }
}

/// PeopleCircle is a circle: a set of people on one server who know each
/// other, named and described by its kind. Not called Circle because SwiftUI
/// already has a shape of that name.
public struct PeopleCircle: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let name: String
    /// family, friends or colleagues.
    public let circleKind: String
    public let createdAt: String
    /// The caller's own role in it: admin or member.
    public let memberRole: String
    public let members: [CircleMember]
    /// Who is still being waited for. Empty for somebody who is not an admin.
    public let pendingInvitations: [CircleInvitation]

    public var isAdmin: Bool { memberRole == CircleMemberRole.admin.rawValue }
    public var knownCircleKind: CircleKind? { CircleKind(rawValue: circleKind) }

    public func member(_ userId: String) -> CircleMember? { members.first { $0.userId == userId } }

    public init(
        id: String, name: String, circleKind: String, createdAt: String, memberRole: String, members: [CircleMember],
        pendingInvitations: [CircleInvitation] = []
    ) {
        self.id = id
        self.name = name
        self.circleKind = circleKind
        self.createdAt = createdAt
        self.memberRole = memberRole
        self.members = members
        self.pendingInvitations = pendingInvitations
    }
}

/// CircleDraft is a circle's name and kind as the form holds them. A rename
/// sends the name alone.
public struct CircleDraft: Hashable, Sendable {
    public var name = ""
    public var circleKind = CircleKind.family

    public init() {}

    public init(_ circle: PeopleCircle) {
        name = circle.name
        circleKind = circle.knownCircleKind ?? .family
    }

    public var trimmedName: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }

    /// Whether the form can be sent: a circle needs a name.
    public var canSubmit: Bool { !trimmedName.isEmpty }

    var createVariables: [String: JSONValue] {
        ["name": .string(trimmedName), "circleKind": .string(circleKind.rawValue)]
    }
}

/// CircleInvitationDraft is who to invite and with which role, as the invite
/// form holds them.
public struct CircleInvitationDraft: Hashable, Sendable {
    public var username = ""
    public var isAdmin = false

    public init() {}

    public var trimmedUsername: String { username.trimmingCharacters(in: .whitespacesAndNewlines) }

    public var canSubmit: Bool { !trimmedUsername.isEmpty }

    var memberRole: CircleMemberRole { isAdmin ? .admin : .member }
}

/// CircleAccountDraft is an account an operator makes for somebody while
/// putting them in a circle, as the form holds it: the account is made with
/// CreateUser and put in the circle with AddCircleMember, so there is no
/// invitation for the person to accept.
public struct CircleAccountDraft: Hashable, Sendable {
    public var username = ""
    public var name = ""
    public var password = ""
    public var isAdmin = false

    public init() {}

    public var trimmedUsername: String { username.trimmingCharacters(in: .whitespacesAndNewlines) }

    /// Whether the form can be sent: a username and a password to sign in
    /// with, since nobody has set this account up.
    public var canSubmit: Bool { !trimmedUsername.isEmpty && !password.isEmpty }

    public var memberRole: CircleMemberRole { isAdmin ? .admin : .member }

    /// The account as the people page makes one, so that both send the same
    /// CreateUser variables.
    public var accountDraft: AccessUserDraft {
        var draft = AccessUserDraft()
        draft.username = trimmedUsername
        draft.password = password
        draft.name = name.trimmingCharacters(in: .whitespacesAndNewlines)
        return draft
    }
}

extension Documents {
    /// What every read of an invitation answers with.
    static let circleInvitationFields = """
        id circleId circleName circleKind invitedByName invitedUsername memberRole createdAt expiresAt
        """
    /// What every read of a circle answers with. Written out in full rather
    /// than built from the invitation fields, since the server's test of the
    /// app's documents fills in one level of shared fields.
    static let circleFields = """
        id name circleKind createdAt memberRole
        members { userId username name avatarUrl avatarColorIndex memberRole joinedAt }
        pendingInvitations { id circleId circleName circleKind invitedByName invitedUsername memberRole createdAt expiresAt }
        """
    public static let circles = """
        query { ListCircles { \(circleFields) } }
        """
    public static let circle = """
        query ($circleId: String!) { ReadCircle(circleId: $circleId) { \(circleFields) } }
        """
    public static let circleInvitations = """
        query { ListCircleInvitations { \(circleInvitationFields) } }
        """
    public static let createCircle = """
        mutation ($name: String!, $circleKind: String!) {
          CreateCircle(name: $name, circleKind: $circleKind) { id }
        }
        """
    public static let renameCircle = """
        mutation ($circleId: String!, $name: String!) { RenameCircle(circleId: $circleId, name: $name) { id } }
        """
    public static let deleteCircle = """
        mutation ($circleId: String!) { DeleteCircle(circleId: $circleId) }
        """
    public static let inviteToCircle = """
        mutation ($circleId: String!, $username: String!, $memberRole: String!) {
          InviteToCircle(circleId: $circleId, username: $username, memberRole: $memberRole)
        }
        """
    public static let addCircleMember = """
        mutation ($circleId: String!, $username: String!, $memberRole: String!) {
          AddCircleMember(circleId: $circleId, username: $username, memberRole: $memberRole) { id }
        }
        """
    public static let answerCircleInvitation = """
        mutation ($invitationId: String!, $isAccepted: Boolean!) {
          AnswerCircleInvitation(invitationId: $invitationId, isAccepted: $isAccepted) { id }
        }
        """
    public static let withdrawCircleInvitation = """
        mutation ($invitationId: String!) { WithdrawCircleInvitation(invitationId: $invitationId) }
        """
    public static let setCircleMemberRole = """
        mutation ($circleId: String!, $userId: String!, $memberRole: String!) {
          SetCircleMemberRole(circleId: $circleId, userId: $userId, memberRole: $memberRole) { id }
        }
        """
    public static let removeCircleMember = """
        mutation ($circleId: String!, $userId: String!) { RemoveCircleMember(circleId: $circleId, userId: $userId) { id } }
        """
    public static let leaveCircle = """
        mutation ($circleId: String!) { LeaveCircle(circleId: $circleId) }
        """
}

extension Connection {
    /// The circles the person is in, with their members and, for an admin,
    /// the invitations still waiting.
    public func circles() async throws -> [PeopleCircle] {
        struct Answer: Decodable, Sendable { let ListCircles: [PeopleCircle] }
        return try await execute(Documents.circles, as: Answer.self).ListCircles
    }

    /// One circle. A circle the person is not in is answered as not found.
    public func circle(_ circleId: String) async throws -> PeopleCircle {
        struct Answer: Decodable, Sendable { let ReadCircle: PeopleCircle }
        return try await execute(Documents.circle, variables: ["circleId": .string(circleId)], as: Answer.self).ReadCircle
    }

    /// The invitations to join a circle that are waiting for the person's
    /// answer.
    public func circleInvitations() async throws -> [CircleInvitation] {
        struct Answer: Decodable, Sendable { let ListCircleInvitations: [CircleInvitation] }
        return try await execute(Documents.circleInvitations, as: Answer.self).ListCircleInvitations
    }

    /// Makes a circle with the person as its admin, and answers its id.
    public func createCircle(_ draft: CircleDraft) async throws -> String {
        struct Answer: Decodable, Sendable {
            struct Created: Decodable, Sendable { let id: String }
            let CreateCircle: Created
        }
        return try await execute(Documents.createCircle, variables: draft.createVariables, as: Answer.self).CreateCircle.id
    }

    /// Renames a circle. Admins only.
    public func renameCircle(_ circleId: String, name: String) async throws {
        struct Answer: Decodable, Sendable { let RenameCircle: JSONValue? }
        _ = try await execute(
            Documents.renameCircle, variables: ["circleId": .string(circleId), "name": .string(name.trimmingCharacters(in: .whitespacesAndNewlines))],
            as: Answer.self)
    }

    /// Deletes a circle for everyone in it. Admins only; nobody's account
    /// goes with it.
    public func deleteCircle(_ circleId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteCircle: JSONValue? }
        _ = try await execute(Documents.deleteCircle, variables: ["circleId": .string(circleId)], as: Answer.self)
    }

    /// Invites a username to a circle. Admins only. The server answers the
    /// same whether or not the account exists or is already in the circle, so
    /// a screen must not say the person was found.
    public func inviteToCircle(_ circleId: String, draft: CircleInvitationDraft) async throws {
        struct Answer: Decodable, Sendable { let InviteToCircle: JSONValue? }
        _ = try await execute(
            Documents.inviteToCircle,
            variables: [
                "circleId": .string(circleId), "username": .string(draft.trimmedUsername), "memberRole": .string(draft.memberRole.rawValue),
            ], as: Answer.self)
    }

    /// Puts an account in a circle at once, with no invitation to accept.
    /// Needs the permission to manage users; the circle need not be one the
    /// person is in. Asking again for an account already in it changes
    /// nothing.
    public func addCircleMember(_ circleId: String, username: String, memberRole: CircleMemberRole) async throws {
        struct Answer: Decodable, Sendable { let AddCircleMember: JSONValue? }
        _ = try await execute(
            Documents.addCircleMember,
            variables: ["circleId": .string(circleId), "username": .string(username), "memberRole": .string(memberRole.rawValue)],
            as: Answer.self)
    }

    /// Joins the circle an invitation is to, or turns it down.
    public func answerCircleInvitation(_ invitationId: String, isAccepted: Bool) async throws {
        struct Answer: Decodable, Sendable { let AnswerCircleInvitation: JSONValue? }
        _ = try await execute(
            Documents.answerCircleInvitation, variables: ["invitationId": .string(invitationId), "isAccepted": .bool(isAccepted)], as: Answer.self)
    }

    /// Takes back an invitation that has not been answered. Admins only.
    public func withdrawCircleInvitation(_ invitationId: String) async throws {
        struct Answer: Decodable, Sendable { let WithdrawCircleInvitation: JSONValue? }
        _ = try await execute(Documents.withdrawCircleInvitation, variables: ["invitationId": .string(invitationId)], as: Answer.self)
    }

    /// Makes somebody an admin or a member. Admins only; the last admin
    /// cannot be made a member while others remain.
    public func setCircleMemberRole(_ circleId: String, userId: String, memberRole: CircleMemberRole) async throws {
        struct Answer: Decodable, Sendable { let SetCircleMemberRole: JSONValue? }
        _ = try await execute(
            Documents.setCircleMemberRole,
            variables: ["circleId": .string(circleId), "userId": .string(userId), "memberRole": .string(memberRole.rawValue)], as: Answer.self)
    }

    /// Takes somebody out of a circle. Admins only; they can be invited again.
    public func removeCircleMember(_ circleId: String, userId: String) async throws {
        struct Answer: Decodable, Sendable { let RemoveCircleMember: JSONValue? }
        _ = try await execute(
            Documents.removeCircleMember, variables: ["circleId": .string(circleId), "userId": .string(userId)], as: Answer.self)
    }

    /// Leaves a circle. The last admin cannot leave while others remain, and
    /// a circle whose last member leaves is deleted.
    public func leaveCircle(_ circleId: String) async throws {
        struct Answer: Decodable, Sendable { let LeaveCircle: JSONValue? }
        _ = try await execute(Documents.leaveCircle, variables: ["circleId": .string(circleId)], as: Answer.self)
    }
}
