import Foundation

// The calendar, its reminders and the address book, as the dashboard's
// Calendar and Contacts pages read and change them (web/src/pages/calendar.tsx,
// web/src/components/reminders.tsx, web/src/pages/addressBook.tsx).

/// AgentCalendar is one of the person's calendars: the events one, or the
/// reminders one.
public struct AgentCalendar: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let name: String
    public let timezone: String?
    /// "sunday" or "monday".
    public let weekStart: String?
    /// "events" or "reminders".
    public let calendarKind: String?
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
    public let occurrence: String?
    public let status: String?

    /// A stable identity for one occurrence of a repeating event.
    public var occurrenceId: String { "\(id)-\(occurrence ?? startsAt)" }
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
    public let name: String?
    public let organization: String?
    public let emails: [String]?
    public let phones: [String]?
    public let note: String?
    public let hasPhoto: Bool?
    public let addresses: [ContactAddress]?
}

extension Documents {
    public static let calendars = """
        query { ListCalendars { id name timezone weekStart calendarKind } }
        """
    public static let calendarEvents = """
        query ($calendarId: String!, $from: String!, $until: String!) {
          ListCalendarEvents(calendarId: $calendarId, from: $from, until: $until) {
            id calendarId summary location startsAt endsAt allDay recurring occurrence status
          }
        }
        """
    public static let calendarEvent = """
        query ($calendarId: String!, $id: String!) {
          GetCalendarEvent(calendarId: $calendarId, id: $id) {
            id summary location description startsAt endsAt allDay
            recurring recurrence status timezone organizer attendees { address name participation role }
          }
        }
        """
    public static let saveCalendarEvent = """
        mutation ($requestId: String!, $calendarId: String!, $id: String, $summary: String, $location: String,
          $description: String, $startsAt: String, $endsAt: String, $allDay: Boolean,
          $timezone: String, $recurrence: String, $status: String, $attendees: [String!]) {
          SaveCalendarEvent(requestId: $requestId, calendarId: $calendarId, id: $id, summary: $summary, location: $location,
            description: $description, startsAt: $startsAt, endsAt: $endsAt, allDay: $allDay,
            timezone: $timezone, recurrence: $recurrence, status: $status, attendees: $attendees) { id }
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
            id name organization emails phones hasPhoto addresses { written }
          }
        }
        """
    public static let contact = """
        query ($id: String!) {
          GetContact(id: $id) {
            id name organization emails phones note hasPhoto
            addresses { street locality region postalCode country }
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
    public static let myContact = """
        query { AgentGraphPage(path: "self") { contact { id } } }
        """
    public static let setMyContact = """
        mutation ($contactId: String) { SetMyContact(contactId: $contactId) }
        """

    /// The calendar's and contacts' documents, for the server's schema check.
    public static let calendarAndContacts: [String: String] = [
        "calendars": calendars, "calendarEvents": calendarEvents, "calendarEvent": calendarEvent,
        "saveCalendarEvent": saveCalendarEvent, "deleteCalendarEvent": deleteCalendarEvent,
        "calendarRequest": calendarRequest, "cancelCalendarRequest": cancelCalendarRequest,
        "reminders": reminders, "saveReminder": saveReminder, "setReminderDone": setReminderDone,
        "deleteReminder": deleteReminder, "addressBooks": addressBooks, "contacts": contacts, "contact": contact,
        "saveContact": saveContact, "deleteContact": deleteContact, "myContact": myContact, "setMyContact": setMyContact,
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
}
