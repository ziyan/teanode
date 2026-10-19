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
    /// What this person is to the caller, such as parent or spouse; empty
    /// when neither of them has said, and for the caller's own row.
    public let relationshipKind: String
    /// masculine, feminine or neutral: the gender of the word to show for
    /// the relationship. Empty when there is none.
    public let relationshipTermGender: String
    /// The relationship in English, such as father. The app words it from
    /// the kind and the gender in its own language and shows this only for a
    /// kind it does not know.
    public let relationshipWord: String
    /// The caller's own contact that is this person, or empty. Private to the
    /// caller: the member is never told. The member rows offer Photos when it
    /// is set.
    public let linkedContactId: String

    public var id: String { userId }
    public var isAdmin: Bool { memberRole == CircleMemberRole.admin.rawValue }

    /// What a list of people calls them: their name, or the username when they
    /// have none.
    public var displayName: String {
        guard let name, !name.isEmpty else { return username }
        return name
    }

    /// Whether the caller has a contact that is this person.
    public var isLinkedToContact: Bool { !linkedContactId.isEmpty }

    /// Whether they are somebody to the caller by relationship.
    public var hasRelationship: Bool { !relationshipKind.isEmpty }

    /// The kind, or nil when there is none or the app does not know it.
    public var knownRelationshipKind: RelationshipKind? { RelationshipKind(rawValue: relationshipKind) }

    /// The gender of the word for the relationship; neutral when the server
    /// said none.
    public var knownRelationshipTermGender: RelationshipTermGender {
        RelationshipTermGender(rawValue: relationshipTermGender) ?? .neutral
    }

    public init(
        userId: String, username: String, name: String? = nil, avatarUrl: String? = nil, avatarColorIndex: Int? = nil,
        memberRole: String, joinedAt: String, relationshipKind: String = "", relationshipTermGender: String = "",
        relationshipWord: String = "", linkedContactId: String = ""
    ) {
        self.userId = userId
        self.username = username
        self.name = name
        self.avatarUrl = avatarUrl
        self.avatarColorIndex = avatarColorIndex
        self.memberRole = memberRole
        self.joinedAt = joinedAt
        self.relationshipKind = relationshipKind
        self.relationshipTermGender = relationshipTermGender
        self.relationshipWord = relationshipWord
        self.linkedContactId = linkedContactId
    }

    /// The relationship fields are absent from a server that predates them,
    /// and empty when there is no relationship.
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        userId = try container.decode(String.self, forKey: .userId)
        username = try container.decode(String.self, forKey: .username)
        name = try container.decodeIfPresent(String.self, forKey: .name)
        avatarUrl = try container.decodeIfPresent(String.self, forKey: .avatarUrl)
        avatarColorIndex = try container.decodeIfPresent(Int.self, forKey: .avatarColorIndex)
        memberRole = try container.decode(String.self, forKey: .memberRole)
        joinedAt = try container.decode(String.self, forKey: .joinedAt)
        relationshipKind = try container.decodeIfPresent(String.self, forKey: .relationshipKind) ?? ""
        relationshipTermGender = try container.decodeIfPresent(String.self, forKey: .relationshipTermGender) ?? ""
        relationshipWord = try container.decodeIfPresent(String.self, forKey: .relationshipWord) ?? ""
        linkedContactId = try container.decodeIfPresent(String.self, forKey: .linkedContactId) ?? ""
    }
}

/// RelationshipKind is what one person is to another, in words that do not
/// depend on who they are: a parent, a spouse. The server keeps it in these
/// names; the app words each in its own language and in the gender of the
/// person it is said of.
public enum RelationshipKind: String, CaseIterable, Sendable {
    case spouse
    case partner
    case parent
    case child
    case sibling
    case grandparent
    case grandchild
    case auntOrUncle = "aunt_or_uncle"
    case nieceOrNephew = "niece_or_nephew"
    case cousin
    case parentInLaw = "parent_in_law"
    case childInLaw = "child_in_law"
    case siblingInLaw = "sibling_in_law"
    case stepParent = "step_parent"
    case stepChild = "step_child"
    case friend
    case colleague

    /// What the other person is to the first: a parent's child.
    public var opposite: RelationshipKind {
        switch self {
        case .spouse: .spouse
        case .partner: .partner
        case .parent: .child
        case .child: .parent
        case .sibling: .sibling
        case .grandparent: .grandchild
        case .grandchild: .grandparent
        case .auntOrUncle: .nieceOrNephew
        case .nieceOrNephew: .auntOrUncle
        case .cousin: .cousin
        case .parentInLaw: .childInLaw
        case .childInLaw: .parentInLaw
        case .siblingInLaw: .siblingInLaw
        case .stepParent: .stepChild
        case .stepChild: .stepParent
        case .friend: .friend
        case .colleague: .colleague
        }
    }

    /// Whether the kind has a word for each gender. A cousin, a friend and a
    /// colleague are the same word whoever they are, so nobody is asked how
    /// they are described for them.
    public var hasGenderedWords: Bool {
        switch self {
        case .cousin, .friend, .colleague: false
        default: true
        }
    }
}

/// RelationshipTermGender is the gender of a word for a relationship: "wife"
/// is feminine, "husband" masculine, "spouse" neutral. It belongs to the
/// word somebody chose, not to a person.
public enum RelationshipTermGender: String, CaseIterable, Sendable {
    case masculine
    case feminine
    case neutral
}

/// PersonPronoun is how a person says they are described. It genders the
/// words for their side of a relationship ("son" rather than "child") and how
/// their agent speaks of them. Not set reads as they, and is sent as empty.
public enum PersonPronoun: String, CaseIterable, Sendable {
    case notSet = ""
    case he
    case she
    case they

    /// The three a person can choose, without not set.
    public static let choices: [PersonPronoun] = [.he, .she, .they]

    /// The gender of the words for somebody described this way: neutral until
    /// they say, and for they.
    public var termGender: RelationshipTermGender {
        switch self {
        case .he: .masculine
        case .she: .feminine
        case .they, .notSet: .neutral
        }
    }
}

/// RelationshipTerm is one word a person can pick for somebody: its kind and
/// the gender of the word. "wife" is a spouse, said in a feminine word.
public struct RelationshipTerm: Codable, Hashable, Sendable, Identifiable {
    /// What is sent to set it: wife, father, aunt_or_uncle.
    public let term: String
    public let relationshipKind: String
    public let termGender: String
    /// The word in English, for a kind the app does not know.
    public let word: String

    public var id: String { term }
    public var knownRelationshipKind: RelationshipKind? { RelationshipKind(rawValue: relationshipKind) }
    public var knownTermGender: RelationshipTermGender { RelationshipTermGender(rawValue: termGender) ?? .neutral }

    public init(term: String, relationshipKind: String, termGender: String, word: String) {
        self.term = term
        self.relationshipKind = relationshipKind
        self.termGender = termGender
        self.word = word
    }
}

/// PersonRelationshipDraft is what the Relationship sheet holds for one other
/// member: the term chosen (empty for none), and, when the person has not said
/// how they are described and the other side has gendered words, the pronoun
/// they pick for themselves.
public struct PersonRelationshipDraft: Hashable, Sendable {
    public let memberUserId: String
    /// Every term the server offers, in the order it offers them.
    public let terms: [RelationshipTerm]
    /// The term the member has now, or empty for none.
    public let originalTerm: String
    /// How the person described themselves before the sheet opened.
    public let originalPronoun: PersonPronoun
    /// The term chosen; empty removes the relationship.
    public var selectedTerm: String
    public var pronoun: PersonPronoun

    public init(member: CircleMember, terms: [RelationshipTerm], ownPronoun: PersonPronoun) {
        memberUserId = member.userId
        self.terms = terms
        originalTerm = Self.term(for: member, among: terms)
        selectedTerm = originalTerm
        originalPronoun = ownPronoun
        pronoun = ownPronoun
    }

    /// The term that says what the member is now. The member's gender follows
    /// how they are described, which may differ from the word the person
    /// chose, so a kind with no word of that gender falls back to the
    /// neutral one.
    public static func term(for member: CircleMember, among terms: [RelationshipTerm]) -> String {
        guard member.hasRelationship else { return "" }
        let termsOfKind = terms.filter { $0.relationshipKind == member.relationshipKind }
        if let exact = termsOfKind.first(where: { $0.termGender == member.relationshipTermGender }) { return exact.term }
        if let neutral = termsOfKind.first(where: { $0.termGender == RelationshipTermGender.neutral.rawValue }) { return neutral.term }
        return termsOfKind.first?.term ?? ""
    }

    /// The kind of the term chosen, or nil for none.
    public var selectedKind: RelationshipKind? {
        terms.first { $0.term == selectedTerm }?.knownRelationshipKind
    }

    /// What the member will see the person as: the opposite of the kind chosen.
    public var oppositeKind: RelationshipKind? { selectedKind?.opposite }

    /// The gender of the word the member will see for the person, which is
    /// how the person is described.
    public var oppositeTermGender: RelationshipTermGender { pronoun.termGender }

    /// Whether another term was picked. The term shown at first follows how
    /// the member is described, which may not be the word the person chose
    /// ("mother" where they said "parent"), so it is saved again only when the
    /// person picked another one.
    public var isTermChanged: Bool { selectedTerm != originalTerm }

    public var isRemoving: Bool { isTermChanged && selectedTerm.isEmpty }

    /// Whether the sheet asks how the person should be described: they have
    /// not said, and the word the member will see has genders.
    public var isPronounAsked: Bool {
        originalPronoun == .notSet && (oppositeKind?.hasGenderedWords ?? false)
    }

    /// The pronoun to set before the relationship, or nil when none is.
    public var pronounToSet: PersonPronoun? {
        isPronounAsked && pronoun != .notSet ? pronoun : nil
    }

    public var hasChanges: Bool { isTermChanged || pronounToSet != nil }

    public var canSave: Bool { hasChanges }
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
        members { userId username name avatarUrl avatarColorIndex memberRole joinedAt relationshipKind relationshipTermGender relationshipWord linkedContactId }
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
    public static let relationshipTerms = """
        query { ListRelationshipTerms { term relationshipKind termGender word } }
        """
    public static let setPersonRelationship = """
        mutation ($userId: String!, $relationshipTerm: String!) {
          SetPersonRelationship(userId: $userId, relationshipTerm: $relationshipTerm) {
            userId username name avatarUrl avatarColorIndex memberRole joinedAt relationshipKind relationshipTermGender relationshipWord linkedContactId
          }
        }
        """
    public static let removePersonRelationship = """
        mutation ($userId: String!) { RemovePersonRelationship(userId: $userId) }
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

    /// Every word a relationship can be said with, in the order to offer them.
    public func relationshipTerms() async throws -> [RelationshipTerm] {
        struct Answer: Decodable, Sendable { let ListRelationshipTerms: [RelationshipTerm] }
        return try await execute(Documents.relationshipTerms, as: Answer.self).ListRelationshipTerms
    }

    /// Says what somebody in a circle with the person is to them, by one of
    /// the terms the server offers. They see the opposite at once and are
    /// told. Answers the member as the person now sees them.
    public func setPersonRelationship(_ userId: String, term: String) async throws -> CircleMember {
        struct Answer: Decodable, Sendable { let SetPersonRelationship: CircleMember }
        return try await execute(
            Documents.setPersonRelationship, variables: ["userId": .string(userId), "relationshipTerm": .string(term)], as: Answer.self
        ).SetPersonRelationship
    }

    /// Takes away what the person and somebody are to each other, whichever of
    /// them said it. The other is told. True when there was one.
    public func removePersonRelationship(_ userId: String) async throws -> Bool {
        struct Answer: Decodable, Sendable { let RemovePersonRelationship: Bool }
        return try await execute(Documents.removePersonRelationship, variables: ["userId": .string(userId)], as: Answer.self)
            .RemovePersonRelationship
    }

    /// Saves the Relationship sheet: how the person is described first when
    /// the sheet asked, then the relationship, or its removal.
    public func saveRelationship(_ draft: PersonRelationshipDraft, ownUserId: String) async throws {
        if let pronoun = draft.pronounToSet {
            _ = try await setAccountPronoun(ownUserId, pronoun: pronoun)
        }
        guard draft.isTermChanged else { return }
        if draft.isRemoving {
            _ = try await removePersonRelationship(draft.memberUserId)
        } else {
            _ = try await setPersonRelationship(draft.memberUserId, term: draft.selectedTerm)
        }
    }
}
