import Foundation

// The calendar, its reminders and the address book, as the dashboard's
// Calendar and Contacts pages read and change them (web/src/pages/calendar.tsx,
// web/src/components/reminders.tsx, web/src/pages/addressBook.tsx).

/// AgentCalendar is one of the person's calendars: the events one, or the
/// reminders one.
public struct AgentCalendar: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let name: String
    /// What it is for; nil when it has none written.
    public let description: String?
    /// What its events are painted, as "#rrggbb"; nil when none was chosen.
    public let colour: String?
    public let timezone: String?
    /// "sunday" or "monday".
    public let weekStart: String?
    /// "events" or "reminders".
    public let calendarKind: String?
    /// What the person may do with it: owner for their own, busy, titles,
    /// details or edit for one shared with them, and edit for a circle's.
    /// Nil from a server that does not know about sharing, which is their own.
    public let shareLevel: String?
    /// Whose it is, for one shared with them; nil for a circle's, which
    /// belongs to the circle.
    public let ownerUserId: String?
    public let ownerName: String?
    /// Where the owner's picture is, a path read with the app's token, and
    /// which of the twelve colors their initial is drawn on without one.
    public let ownerAvatarUrl: String?
    public let ownerAvatarColorIndex: Int?
    public let circleId: String?
    public let circleName: String?
    public let isCircleCalendar: Bool?
    /// Which of eight colors it is painted in when it has none of its own,
    /// the same one the dashboard uses.
    public let colorIndex: Int?

    /// Whether it is the person's own calendar.
    public var isOwn: Bool { shareLevel == nil || shareLevel == CalendarShareLevel.owner.rawValue }

    /// How much of it the person may do. A server that does not say means
    /// their own; a level this app does not know is the least, because
    /// guessing more would offer to change what the server refuses.
    public var level: CalendarShareLevel {
        guard let shareLevel else { return .owner }
        return CalendarShareLevel(rawValue: shareLevel) ?? .busy
    }

    /// Whether its events may be added to, changed and removed from here.
    public var canWrite: Bool { level.canWrite }

    /// Whether it is listed with events to read, rather than only the times
    /// its owner is busy.
    public var canRead: Bool { level.canRead }

    /// What a list calls it: its own name for the person's and for a
    /// circle's, and whose it is for another person's, since everybody's is
    /// called "Calendar". The possessive is the screen's to write.
    public var isNamedForOwner: Bool { !isOwn && isCircleCalendar != true && !(ownerName ?? "").isEmpty }

    /// Whose it is, to be said beside an event of it when several calendars
    /// are on one screen: the owner's name for one shared with the person, the
    /// circle's name for a circle's, and nil for their own.
    public var whoseName: String? {
        if isOwn { return nil }
        let name = isCircleCalendar == true ? (circleName ?? self.name) : (ownerName ?? "")
        return name.isEmpty ? nil : name
    }

    public init(
        id: String, name: String, description: String? = nil, colour: String? = nil, timezone: String? = nil, weekStart: String? = nil,
        calendarKind: String? = nil, shareLevel: String? = nil, ownerUserId: String? = nil, ownerName: String? = nil,
        ownerAvatarUrl: String? = nil, ownerAvatarColorIndex: Int? = nil, circleId: String? = nil, circleName: String? = nil,
        isCircleCalendar: Bool? = nil, colorIndex: Int? = nil
    ) {
        self.id = id
        self.name = name
        self.description = description
        self.colour = colour
        self.timezone = timezone
        self.weekStart = weekStart
        self.calendarKind = calendarKind
        self.shareLevel = shareLevel
        self.ownerUserId = ownerUserId
        self.ownerName = ownerName
        self.ownerAvatarUrl = ownerAvatarUrl
        self.ownerAvatarColorIndex = ownerAvatarColorIndex
        self.circleId = circleId
        self.circleName = circleName
        self.isCircleCalendar = isCircleCalendar
        self.colorIndex = colorIndex
    }
}

/// CalendarShareLevel is how much of a calendar somebody may see, and whether
/// they may change it; the dashboard, the command line and the agent use the
/// same four words (docs/decisions/20261016-a-calendar-is-shared-at-a-level.md).
public enum CalendarShareLevel: String, CaseIterable, Sendable {
    /// When the owner is busy, and nothing else.
    case busy
    /// The time, title and place of each event.
    case titles
    /// Everything in each event, guests included.
    case details
    /// Details, and the reader may add, change and remove events.
    case edit
    /// The person's own calendar; nobody is given this.
    case owner

    /// The levels a calendar can be shared at, least to most.
    public static var grantable: [CalendarShareLevel] { [.busy, .titles, .details, .edit] }

    /// Whether events are listed, rather than only busy time.
    public var canRead: Bool { self != .busy }

    /// Whether events may be added, changed and removed.
    public var canWrite: Bool { self == .edit || self == .owner }
}

/// CalendarShare is one share of the person's calendar, with one person or
/// every member of one circle.
public struct CalendarShare: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let calendarId: String
    public let granteeUserId: String?
    public let granteeUsername: String?
    public let granteeName: String?
    public let granteeCircleId: String?
    public let granteeCircleName: String?
    /// busy, titles, details or edit.
    public let shareLevel: String

    public var level: CalendarShareLevel? { CalendarShareLevel(rawValue: shareLevel) }

    /// Whether it is with a circle rather than a person.
    public var isWithCircle: Bool { granteeCircleId != nil }

    /// Who it is with: the person's name, or the username when they have no
    /// name, or the circle's name.
    public var granteeDisplayName: String {
        if isWithCircle { return granteeCircleName ?? "" }
        if let granteeName, !granteeName.isEmpty { return granteeName }
        return granteeUsername ?? ""
    }
}

/// CalendarSharePerson is somebody the person shares a circle with, and so
/// may share their calendar with.
public struct CalendarSharePerson: Codable, Identifiable, Hashable, Sendable {
    public let userId: String
    public let username: String
    public let name: String?
    public let avatarUrl: String?
    public let avatarColorIndex: Int?

    public var id: String { userId }
    public var displayName: String {
        guard let name, !name.isEmpty else { return username }
        return name
    }

    /// How a picker names them: the name and, in brackets, the username, so two
    /// people with one name can be told apart.
    public var pickerLabel: String { PersonWords.pickerLabel(name: name, username: username) }
}

/// PersonWords are the words for a person that more than one screen shares.
public enum PersonWords {
    /// "Ada Example (ada)": the name and the username; the username alone for a
    /// person with no name, or whose name is their username.
    public static func pickerLabel(name: String?, username: String) -> String {
        let trimmed = name?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return trimmed.isEmpty || trimmed == username ? username : "\(trimmed) (\(username))"
    }
}

/// ShareTarget is who a calendar or a page is shared with, as one choice: a
/// person, or a whole circle. The server takes one of the two.
public enum ShareTarget: Hashable, Sendable {
    case person(String)
    case circle(String)

    public var granteeUserId: String? {
        if case .person(let userId) = self { return userId }
        return nil
    }

    public var granteeCircleId: String? {
        if case .circle(let circleId) = self { return circleId }
        return nil
    }
}

/// CalendarShareCircle is a circle the person is in, which their calendar can
/// be shared with whole.
public struct CalendarShareCircle: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let name: String
    /// Who is in it, so a picker can say how many people a circle is and two
    /// circles with one name can be told apart.
    public let members: [Member]

    public var memberCount: Int { members.count }

    public struct Member: Codable, Hashable, Sendable {
        public let userId: String
    }
}

/// CalendarEvent is one occurrence of an event in a window.
public struct CalendarEvent: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let calendarId: String
    public let summary: String?
    public let location: String?
    /// RFC 3339: the occurrence's times.
    public let startsAt: String
    public let endsAt: String
    public let allDay: Bool
    public let recurring: Bool
    /// Whether this is one time of a repeating event.
    public let occurrence: Bool?
    public let status: String?
    /// Whether all the person is told of it is that the owner is busy: a
    /// calendar shared at busy, or an event its owner marked private. Its
    /// title is empty and the screen says "Busy" in the person's language.
    public let isBusyOnly: Bool?
    /// Whether the owner marked it private, which the server only says of
    /// the person's own events: everyone the calendar is shared with sees
    /// only that they are busy. Absent from an older server.
    public let isPrivate: Bool?

    /// A stable identity for one occurrence of a repeating event. The
    /// calendar is part of it: a busy block has no identifier of its own.
    public var occurrenceId: String { "\(calendarId)-\(id)-\(startsAt)" }

    /// Whether there is nothing in it to open.
    public var isBusy: Bool { isBusyOnly == true }

    /// Whether to draw the lock beside it.
    public var isMarkedPrivate: Bool { isPrivate == true }

    /// Whether it was called off; the server writes the iCalendar status,
    /// "CANCELLED".
    public var isCancelled: Bool { status?.uppercased() == "CANCELLED" }
}

/// CalendarEventDetail is one event whole, read before it is changed: the
/// dashboard sends every field back, so nothing is changed by being unread.
public struct CalendarEventDetail: Codable, Hashable, Sendable {
    public struct Attendee: Codable, Hashable, Sendable {
        public let address: String
        public let name: String?
        /// NEEDS-ACTION, ACCEPTED, DECLINED or TENTATIVE.
        public let participation: String?
        public let role: String?
    }

    public let id: String
    public let summary: String?
    public let location: String?
    public let description: String?
    public let startsAt: String
    public let endsAt: String
    public let allDay: Bool
    public let recurring: Bool
    /// The RRULE without its name, such as FREQ=WEEKLY;BYDAY=MO.
    public let recurrence: String?
    public let status: String?
    public let timezone: String?
    /// Whether the owner marked it private; absent from an older server.
    public let isPrivate: Bool?
    public let organizer: String?
    public let attendees: [Attendee]?
}

/// AgentReminder is one reminder, from the reminders calendar.
public struct AgentReminder: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let title: String
    public let notes: String?
    public let dueAt: String?
    /// Due on a day rather than at a time: the day is read from UTC parts.
    public let isDueDate: Bool?
    public let isDone: Bool
    public let doneAt: String?
    public let priority: Int?
    public let isRepeating: Bool?
}

/// CalendarDates are the calendar's rules for writing and reading times.
public enum CalendarDates {
    static var utc: Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        return calendar
    }

    /// The day an all-day event or a dated reminder is on. It is stored as
    /// midnight UTC of that day, so it is read from UTC parts: read in local
    /// time, anyone west of Greenwich would see the day before.
    public static func day(ofAllDay text: String?) -> DateComponents? {
        guard let date = ServerTime.date(text) else { return nil }
        return utc.dateComponents([.year, .month, .day], from: date)
    }

    /// The UTC midnight a day is written as.
    public static func allDayText(_ day: DateComponents) -> String {
        String(format: "%04d-%02d-%02dT00:00:00Z", day.year ?? 1970, day.month ?? 1, day.day ?? 1)
    }

    /// The day after, which an all-day event's end is: the end is exclusive.
    public static func dayAfter(_ day: DateComponents) -> DateComponents {
        let date = utc.date(from: day) ?? Date()
        let next = utc.date(byAdding: .day, value: 1, to: date) ?? date
        return utc.dateComponents([.year, .month, .day], from: next)
    }

    /// The last day an all-day event covers: the day before its exclusive
    /// end, never before its start.
    public static func lastDay(startsAt: String, endsAt: String) -> DateComponents? {
        guard let start = day(ofAllDay: startsAt), let end = day(ofAllDay: endsAt),
            let startDate = utc.date(from: start), let endDate = utc.date(from: end)
        else { return day(ofAllDay: startsAt) }
        let last = utc.date(byAdding: .day, value: -1, to: endDate) ?? endDate
        return utc.dateComponents([.year, .month, .day], from: max(startDate, last))
    }

    /// A time as the server takes it: RFC 3339 in UTC.
    public static func text(_ date: Date) -> String {
        date.formatted(.iso8601)
    }

    /// The day an event appears on in this time zone: an all-day event's
    /// own day, a timed one's local start.
    public static func localDay(of event: CalendarEvent, in calendar: Calendar) -> DateComponents? {
        if event.allDay { return day(ofAllDay: event.startsAt) }
        guard let date = ServerTime.date(event.startsAt) else { return nil }
        return calendar.dateComponents([.year, .month, .day], from: date)
    }
}

/// AddressBook is the person's address book.
public struct AddressBook: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let name: String
    public let contacts: Int?
}

/// ContactSummary is a contact as the list shows it.
public struct ContactSummary: Codable, Identifiable, Hashable, Sendable {
    public struct Written: Codable, Hashable, Sendable { public let written: String? }
    public let id: String
    public let name: String?
    public let organization: String?
    public let emails: [String]?
    public let phones: [String]?
    public let hasPhoto: Bool?
    public let addresses: [Written]?
    /// The account in the person's circles this contact is, when they said so.
    public var accountUserId: String? = nil

    /// What the list calls them: their name, or else their first address.
    public var shownName: String {
        if let name, !name.isEmpty { return name }
        return emails?.first ?? ""
    }

    public var photoPath: String { "/api/v1/contacts/\(id)/photo" }
}

/// ContactAddress is one postal address of a contact.
public struct ContactAddress: Codable, Hashable, Sendable {
    public var street: String?
    public var locality: String?
    public var region: String?
    public var postalCode: String?
    public var country: String?

    public init(street: String? = nil, locality: String? = nil, region: String? = nil, postalCode: String? = nil, country: String? = nil) {
        self.street = street
        self.locality = locality
        self.region = region
        self.postalCode = postalCode
        self.country = country
    }
}

/// ContactDetail is a contact whole, read before it is changed: every field
/// is sent back, since an empty one clears what is there.
public struct ContactDetail: Codable, Hashable, Sendable {
    public let id: String
    /// The address book it is in; nil where it was not asked for.
    public let addressBookId: String?
    public let name: String?
    public let organization: String?
    public let emails: [String]?
    public let phones: [String]?
    public let note: String?
    public let hasPhoto: Bool?
    public let addresses: [ContactAddress]?
    /// The account on this server the contact is, when the person said so and
    /// still shares a circle with it; nil otherwise. Private to the person.
    public let accountUserId: String?
    public let account: ChatPerson?

    /// Whether the contact is linked to somebody in the person's circles.
    public var isLinkedToAccount: Bool { !(accountUserId ?? "").isEmpty && account != nil }
}

/// ContactAccountLink is what linking a contact to an account (or clearing
/// the link) answers: the contact and the account it is now.
public struct ContactAccountLink: Codable, Hashable, Sendable {
    public let id: String
    public let accountUserId: String?
    public let account: ChatPerson?
}

/// ContactAccountSuggestion is an unlinked contact that looks like somebody
/// in the person's circles, offered for the person to confirm. Nothing is
/// linked until they do, and the server does not say which address matched.
public struct ContactAccountSuggestion: Codable, Identifiable, Hashable, Sendable {
    /// The address book the contact is in, which linking takes with the contact.
    public let addressBookId: String
    public let contact: ContactSummary
    public let account: ChatPerson

    public var id: String { contact.id }

    public init(addressBookId: String, contact: ContactSummary, account: ChatPerson) {
        self.addressBookId = addressBookId
        self.contact = contact
        self.account = account
    }
}

extension Documents {
    public static let calendars = """
        query {
          ListCalendars {
            id name description colour timezone weekStart calendarKind
            shareLevel ownerUserId ownerName ownerAvatarUrl ownerAvatarColorIndex circleId circleName isCircleCalendar colorIndex
          }
        }
        """
    public static let calendarEvents = """
        query ($calendarId: String!, $from: String!, $until: String!) {
          ListCalendarEvents(calendarId: $calendarId, from: $from, until: $until) {
            id calendarId summary location startsAt endsAt allDay recurring occurrence status isBusyOnly isPrivate
          }
        }
        """
    public static let calendarEvent = """
        query ($calendarId: String!, $id: String!) {
          GetCalendarEvent(calendarId: $calendarId, id: $id) {
            id summary location description startsAt endsAt allDay
            recurring recurrence status timezone isPrivate organizer attendees { address name participation role }
          }
        }
        """
    public static let saveCalendarEvent = """
        mutation ($requestId: String!, $calendarId: String!, $id: String, $summary: String, $location: String,
          $description: String, $startsAt: String, $endsAt: String, $allDay: Boolean,
          $timezone: String, $recurrence: String, $status: String, $isPrivate: Boolean, $attendees: [String!]) {
          SaveCalendarEvent(requestId: $requestId, calendarId: $calendarId, id: $id, summary: $summary, location: $location,
            description: $description, startsAt: $startsAt, endsAt: $endsAt, allDay: $allDay,
            timezone: $timezone, recurrence: $recurrence, status: $status, isPrivate: $isPrivate, attendees: $attendees) { id }
        }
        """
    public static let deleteCalendarEvent = """
        mutation ($requestId: String!, $calendarId: String!, $id: String!) {
          DeleteCalendarEvent(requestId: $requestId, calendarId: $calendarId, id: $id)
        }
        """
    public static let calendarRequest = """
        query ($requestId: String!) { GetCalendarRequest(requestId: $requestId) { requestId isMissing } }
        """
    public static let cancelCalendarRequest = """
        mutation ($requestId: String!) { CancelCalendarRequest(requestId: $requestId) { requestId isMissing } }
        """
    public static let calendarShares = """
        query ($calendarId: String!) {
          ListCalendarShares(calendarId: $calendarId) {
            id calendarId granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName shareLevel
          }
        }
        """
    public static let shareCalendar = """
        mutation ($calendarId: String!, $granteeUserId: String, $granteeCircleId: String, $shareLevel: String!) {
          ShareCalendar(calendarId: $calendarId, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, shareLevel: $shareLevel) { id }
        }
        """
    public static let unshareCalendar = """
        mutation ($shareId: String!) { UnshareCalendar(shareId: $shareId) }
        """
    public static let calendarSharePeople = """
        query { ListCirclePeople { userId username name avatarUrl avatarColorIndex } }
        """
    public static let calendarShareCircles = """
        query { ListCircles { id name members { userId } } }
        """
    public static let saveCalendar = """
        mutation ($id: String!, $name: String, $description: String, $colour: String, $timezone: String, $weekStart: String) {
          SaveCalendar(id: $id, name: $name, description: $description, colour: $colour, timezone: $timezone, weekStart: $weekStart) { id name }
        }
        """
    public static let createCircleCalendar = """
        mutation ($circleId: String!) { CreateCircleCalendar(circleId: $circleId) { id } }
        """
    public static let deleteCircleCalendar = """
        mutation ($circleId: String!) { DeleteCircleCalendar(circleId: $circleId) }
        """
    public static let reminders = """
        query ($isDone: Boolean) { ListReminders(isDone: $isDone) { id title notes dueAt isDueDate isDone doneAt priority isRepeating } }
        """
    public static let saveReminder = """
        mutation ($reminderId: String, $title: String, $notes: String, $dueAt: String, $dueOn: String, $isDueCleared: Boolean) {
          SaveReminder(reminderId: $reminderId, title: $title, notes: $notes, dueAt: $dueAt, dueOn: $dueOn, isDueCleared: $isDueCleared) { id }
        }
        """
    public static let setReminderDone = """
        mutation ($reminderId: String!, $isDone: Boolean!) { SetReminderDone(reminderId: $reminderId, isDone: $isDone) { id } }
        """
    public static let deleteReminder = """
        mutation ($reminderId: String!) { DeleteReminder(reminderId: $reminderId) }
        """
    public static let addressBooks = """
        query { ListAddressBooks { id name contacts } }
        """
    public static let contacts = """
        query ($addressBookId: String!, $query: String, $first: Int) {
          ListContacts(addressBookId: $addressBookId, query: $query, first: $first) {
            id name organization emails phones hasPhoto addresses { written } accountUserId
          }
        }
        """
    public static let contact = """
        query ($id: String!) {
          GetContact(id: $id) {
            id name organization emails phones note hasPhoto
            addresses { street locality region postalCode country }
            accountUserId account { userId username name avatarUrl avatarColorIndex }
          }
        }
        """
    public static let saveContact = """
        mutation ($addressBookId: String!, $id: String, $name: String, $organization: String,
                  $emails: [String!], $phones: [String!], $note: String, $addresses: [AddressInput!]) {
          SaveContact(addressBookId: $addressBookId, id: $id, name: $name, organization: $organization,
                      emails: $emails, phones: $phones, note: $note, addresses: $addresses) { id name }
        }
        """
    public static let deleteContact = """
        mutation ($id: String!) { DeleteContact(id: $id) }
        """
    public static let removeContactPicture = """
        mutation ($id: String!) { RemoveContactPicture(id: $id) { id hasPhoto } }
        """
    public static let myContact = """
        query { AgentGraphPage(path: "self") { contact { id } } }
        """
    public static let setMyContact = """
        mutation ($contactId: String) { SetMyContact(contactId: $contactId) }
        """
    public static let setContactAccount = """
        mutation ($addressBookId: String!, $contactId: String!, $accountUserId: String) {
          SetContactAccount(addressBookId: $addressBookId, contactId: $contactId, accountUserId: $accountUserId) {
            id accountUserId account { userId username name avatarUrl avatarColorIndex }
          }
        }
        """
    public static let contactAccountSuggestions = """
        query {
          ListContactAccountSuggestions {
            addressBookId
            contact { id name organization emails phones hasPhoto }
            account { userId username name avatarUrl avatarColorIndex }
          }
        }
        """

    /// The calendar's and contacts' documents, for the server's schema check.
    public static let calendarAndContacts: [String: String] = [
        "calendars": calendars, "calendarEvents": calendarEvents, "calendarEvent": calendarEvent,
        "saveCalendarEvent": saveCalendarEvent, "deleteCalendarEvent": deleteCalendarEvent,
        "calendarRequest": calendarRequest, "cancelCalendarRequest": cancelCalendarRequest,
        "calendarShares": calendarShares, "shareCalendar": shareCalendar, "unshareCalendar": unshareCalendar,
        "calendarSharePeople": calendarSharePeople, "calendarShareCircles": calendarShareCircles,
        "createCircleCalendar": createCircleCalendar, "deleteCircleCalendar": deleteCircleCalendar, "saveCalendar": saveCalendar,
        "reminders": reminders, "saveReminder": saveReminder, "setReminderDone": setReminderDone,
        "deleteReminder": deleteReminder, "addressBooks": addressBooks, "contacts": contacts, "contact": contact,
        "saveContact": saveContact, "importContact": importContact, "deleteContact": deleteContact, "removeContactPicture": removeContactPicture, "myContact": myContact, "setMyContact": setMyContact,
        "setContactAccount": setContactAccount, "contactAccountSuggestions": contactAccountSuggestions,
    ]
}

/// CalendarChange is a change to the calendar sent with an id of its own,
/// so a change whose answer was lost is never made twice: before sending
/// again, the server is asked whether it already happened.
public struct CalendarChange: Codable, Sendable, Equatable {
    /// "save" or "delete".
    public let operation: String
    /// What the change is to, for the person: the event's title.
    public let summary: String
    public let requestId: String
    public let variables: [String: JSONValue]

    public init(operation: String, summary: String, variables: [String: JSONValue]) {
        self.operation = operation
        self.summary = summary
        self.requestId = (0..<16).map { _ in String(format: "%02x", UInt8.random(in: 0...255)) }.joined()
        self.variables = variables
    }
}

extension Connection {
    public func calendars() async throws -> [AgentCalendar] {
        struct Answer: Decodable, Sendable { let ListCalendars: [AgentCalendar] }
        return try await execute(Documents.calendars, as: Answer.self).ListCalendars
    }

    /// Who the person's own calendar is shared with, and at what level.
    public func calendarShares(calendarId: String) async throws -> [CalendarShare] {
        struct Answer: Decodable, Sendable { let ListCalendarShares: [CalendarShare] }
        return try await execute(Documents.calendarShares, variables: ["calendarId": .string(calendarId)], as: Answer.self).ListCalendarShares
    }

    /// Shares the person's calendar with a person or a circle (one of the two
    /// identifiers), or changes the level of a share already made.
    public func shareCalendar(calendarId: String, granteeUserId: String?, granteeCircleId: String?, level: CalendarShareLevel) async throws {
        struct Answer: Decodable, Sendable {
            struct Shared: Decodable, Sendable { let id: String }
            let ShareCalendar: Shared
        }
        _ = try await execute(
            Documents.shareCalendar,
            variables: [
                "calendarId": .string(calendarId), "granteeUserId": granteeUserId.map { .string($0) } ?? .null,
                "granteeCircleId": granteeCircleId.map { .string($0) } ?? .null, "shareLevel": .string(level.rawValue),
            ], as: Answer.self)
    }

    /// Takes a share back.
    public func unshareCalendar(shareId: String) async throws {
        struct Answer: Decodable, Sendable { let UnshareCalendar: Bool? }
        _ = try await execute(Documents.unshareCalendar, variables: ["shareId": .string(shareId)], as: Answer.self)
    }

    /// The people the person shares a circle with, who their calendar can be
    /// shared with.
    public func calendarSharePeople() async throws -> [CalendarSharePerson] {
        struct Answer: Decodable, Sendable { let ListCirclePeople: [CalendarSharePerson] }
        return try await execute(Documents.calendarSharePeople, as: Answer.self).ListCirclePeople
    }

    /// The circles the person is in, which their calendar can be shared with.
    public func calendarShareCircles() async throws -> [CalendarShareCircle] {
        struct Answer: Decodable, Sendable { let ListCircles: [CalendarShareCircle] }
        return try await execute(Documents.calendarShareCircles, as: Answer.self).ListCircles
    }

    /// Renames a calendar of the person's own, or a circle's they are an admin
    /// of. The server replaces every field
    /// it is sent, so the ones the app does not change go back as the calendar
    /// holds them: leaving them out would clear its description, its color, the
    /// zone a new event is written in and the day its weeks start on.
    public func saveCalendar(_ calendar: AgentCalendar, name: String) async throws {
        struct Answer: Decodable, Sendable {
            struct Saved: Decodable, Sendable { let id: String }
            let SaveCalendar: Saved
        }
        _ = try await execute(
            Documents.saveCalendar,
            variables: [
                "id": .string(calendar.id), "name": .string(name), "description": .string(calendar.description ?? ""),
                "colour": .string(calendar.colour ?? ""), "timezone": .string(calendar.timezone ?? ""),
                "weekStart": .string(calendar.weekStart ?? ""),
            ], as: Answer.self)
    }

    /// Makes the calendar a circle keeps for itself. Admins only.
    public func createCircleCalendar(circleId: String) async throws {
        struct Answer: Decodable, Sendable {
            struct Made: Decodable, Sendable { let id: String }
            let CreateCircleCalendar: Made
        }
        _ = try await execute(Documents.createCircleCalendar, variables: ["circleId": .string(circleId)], as: Answer.self)
    }

    /// Deletes the calendar a circle keeps for itself, with every event in it,
    /// for everyone in the circle. Admins only.
    public func deleteCircleCalendar(circleId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteCircleCalendar: Bool? }
        _ = try await execute(Documents.deleteCircleCalendar, variables: ["circleId": .string(circleId)], as: Answer.self)
    }

    public func calendarEvents(calendarId: String, from: Date, until: Date) async throws -> [CalendarEvent] {
        struct Answer: Decodable, Sendable { let ListCalendarEvents: [CalendarEvent] }
        return try await execute(Documents.calendarEvents, variables: [
            "calendarId": .string(calendarId), "from": .string(CalendarDates.text(from)), "until": .string(CalendarDates.text(until)),
        ], as: Answer.self).ListCalendarEvents
    }

    public func calendarEvent(calendarId: String, id: String) async throws -> CalendarEventDetail {
        struct Answer: Decodable, Sendable { let GetCalendarEvent: CalendarEventDetail }
        return try await execute(Documents.calendarEvent, variables: ["calendarId": .string(calendarId), "id": .string(id)], as: Answer.self)
            .GetCalendarEvent
    }

    /// Makes a calendar change. A change sent before whose answer was lost
    /// is asked about first, and sent again only if it did not happen.
    public func perform(_ change: CalendarChange, isRetry: Bool) async throws {
        if isRetry {
            struct Asked: Decodable, Sendable {
                struct Request: Decodable, Sendable { let requestId: String }
                let GetCalendarRequest: Request?
            }
            if try await execute(Documents.calendarRequest, variables: ["requestId": .string(change.requestId)], as: Asked.self)
                .GetCalendarRequest != nil
            {
                return
            }
        }
        var variables = change.variables
        variables["requestId"] = .string(change.requestId)
        if change.operation == "delete" {
            struct Answer: Decodable, Sendable { let DeleteCalendarEvent: Bool? }
            _ = try await execute(Documents.deleteCalendarEvent, variables: variables, as: Answer.self)
        } else {
            struct Answer: Decodable, Sendable {
                struct Saved: Decodable, Sendable { let id: String }
                let SaveCalendarEvent: Saved
            }
            _ = try await execute(Documents.saveCalendarEvent, variables: variables, as: Answer.self)
        }
    }

    /// Stops trying a change; true when it had already happened, which
    /// stopping does not undo.
    public func cancel(_ change: CalendarChange) async throws -> Bool {
        struct Answer: Decodable, Sendable {
            struct Request: Decodable, Sendable { let requestId: String }
            let CancelCalendarRequest: Request?
        }
        return try await execute(Documents.cancelCalendarRequest, variables: ["requestId": .string(change.requestId)], as: Answer.self)
            .CancelCalendarRequest != nil
    }

    public func reminders(isDone: Bool) async throws -> [AgentReminder] {
        struct Answer: Decodable, Sendable { let ListReminders: [AgentReminder] }
        return try await execute(Documents.reminders, variables: ["isDone": .bool(isDone)], as: Answer.self).ListReminders
    }

    /// Saves a reminder: only the fields given change.
    public func saveReminder(_ reminderId: String?, fields: [String: JSONValue]) async throws {
        struct Answer: Decodable, Sendable {
            struct Saved: Decodable, Sendable { let id: String }
            let SaveReminder: Saved
        }
        var variables = fields
        if let reminderId { variables["reminderId"] = .string(reminderId) }
        _ = try await execute(Documents.saveReminder, variables: variables, as: Answer.self)
    }

    public func setReminderDone(_ reminderId: String, isDone: Bool) async throws {
        struct Answer: Decodable, Sendable {
            struct Done: Decodable, Sendable { let id: String }
            let SetReminderDone: Done
        }
        _ = try await execute(Documents.setReminderDone, variables: ["reminderId": .string(reminderId), "isDone": .bool(isDone)], as: Answer.self)
    }

    public func deleteReminder(_ reminderId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteReminder: Bool? }
        _ = try await execute(Documents.deleteReminder, variables: ["reminderId": .string(reminderId)], as: Answer.self)
    }

    public func addressBooks() async throws -> [AddressBook] {
        struct Answer: Decodable, Sendable { let ListAddressBooks: [AddressBook] }
        return try await execute(Documents.addressBooks, as: Answer.self).ListAddressBooks
    }

    public func contacts(addressBookId: String, matching words: String? = nil, first: Int? = nil) async throws -> [ContactSummary] {
        struct Answer: Decodable, Sendable { let ListContacts: [ContactSummary] }
        var variables: [String: JSONValue] = ["addressBookId": .string(addressBookId)]
        if let words, !words.isEmpty { variables["query"] = .string(words) }
        if let first { variables["first"] = .number(Double(first)) }
        return try await execute(Documents.contacts, variables: variables, as: Answer.self).ListContacts
    }

    public func contact(_ id: String) async throws -> ContactDetail {
        struct Answer: Decodable, Sendable { let GetContact: ContactDetail }
        return try await execute(Documents.contact, variables: ["id": .string(id)], as: Answer.self).GetContact
    }

    /// Saves a contact, every field sent: an empty one clears what is there.
    public func saveContact(
        addressBookId: String, id: String?, name: String, organization: String, emails: [String], phones: [String], note: String,
        addresses: [ContactAddress]
    ) async throws {
        struct Answer: Decodable, Sendable {
            struct Saved: Decodable, Sendable { let id: String }
            let SaveContact: Saved
        }
        var variables: [String: JSONValue] = [
            "addressBookId": .string(addressBookId), "name": .string(name), "organization": .string(organization),
            "emails": .array(emails.map { .string($0) }), "phones": .array(phones.map { .string($0) }), "note": .string(note),
        ]
        if let id { variables["id"] = .string(id) }
        variables["addresses"] = try JSONDecoder().decode(JSONValue.self, from: JSONEncoder().encode(addresses))
        _ = try await execute(Documents.saveContact, variables: variables, as: Answer.self)
    }

    public func deleteContact(_ id: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteContact: Bool? }
        _ = try await execute(Documents.deleteContact, variables: ["id": .string(id)], as: Answer.self)
    }

    /// Takes the picture off a contact and leaves the rest of the card.
    public func removeContactPicture(_ id: String) async throws {
        struct Answer: Decodable, Sendable {
            struct Contact: Decodable, Sendable { let id: String }
            let RemoveContactPicture: Contact
        }
        _ = try await execute(Documents.removeContactPicture, variables: ["id": .string(id)], as: Answer.self)
    }

    /// The contact that is the person themselves, if one is marked.
    public func myContactId() async throws -> String? {
        struct Answer: Decodable, Sendable {
            struct Page: Decodable, Sendable {
                struct Contact: Decodable, Sendable { let id: String }
                let contact: Contact?
            }
            let AgentGraphPage: Page?
        }
        return try await execute(Documents.myContact, as: Answer.self).AgentGraphPage?.contact?.id
    }

    public func setMyContact(_ contactId: String?) async throws {
        struct Answer: Decodable, Sendable { let SetMyContact: Bool? }
        _ = try await execute(Documents.setMyContact, variables: ["contactId": contactId.map { .string($0) } ?? .null], as: Answer.self)
    }

    /// Says which account on this server a contact is, chosen from the people
    /// the person shares a circle with. An empty or nil `accountUserId` clears
    /// the link. Private to the person: the account is never told.
    public func setContactAccount(addressBookId: String, contactId: String, accountUserId: String?) async throws -> ContactAccountLink {
        struct Answer: Decodable, Sendable { let SetContactAccount: ContactAccountLink }
        let variables: [String: JSONValue] = [
            "addressBookId": .string(addressBookId), "contactId": .string(contactId), "accountUserId": accountUserId.map { .string($0) } ?? .null,
        ]
        return try await execute(Documents.setContactAccount, variables: variables, as: Answer.self).SetContactAccount
    }

    /// The contacts that look like people in the person's circles, each to be
    /// confirmed or left alone.
    public func contactAccountSuggestions() async throws -> [ContactAccountSuggestion] {
        struct Answer: Decodable, Sendable { let ListContactAccountSuggestions: [ContactAccountSuggestion] }
        return try await execute(Documents.contactAccountSuggestions, as: Answer.self).ListContactAccountSuggestions
    }
}
