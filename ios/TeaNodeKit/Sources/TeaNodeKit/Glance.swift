import Foundation

/// Glance is what a widget shows at a glance: the next events, the unread
/// mail, and the agent's name.
public struct Glance: Codable, Hashable, Sendable {
    /// Event is one coming event as a widget lists it.
    public struct Event: Codable, Hashable, Identifiable, Sendable {
        public let id: String
        public let title: String
        public let location: String
        public let startsAt: Date
        public let endsAt: Date
        public let isAllDay: Bool

        public init(id: String, title: String, location: String, startsAt: Date, endsAt: Date, isAllDay: Bool) {
            self.id = id
            self.title = title
            self.location = location
            self.startsAt = startsAt
            self.endsAt = endsAt
            self.isAllDay = isAllDay
        }
    }

    public let events: [Event]
    public let unreadCount: Int
    public let agentName: String
    /// How many questions and approvals wait for the person, and the
    /// newest of them.
    public let waitingCount: Int
    public let newestWaiting: AgentWaitingInteraction?

    public init(events: [Event], unreadCount: Int, agentName: String, waitingCount: Int = 0, newestWaiting: AgentWaitingInteraction? = nil) {
        self.events = events
        self.unreadCount = unreadCount
        self.agentName = agentName
        self.waitingCount = waitingCount
        self.newestWaiting = newestWaiting
    }

    /// The events not over yet, soonest first, at most `limit`. An all-day
    /// event is on its day wherever the phone is: its stored midnight UTC is
    /// read as the day, and placed at local midnight.
    public static func upcoming(_ events: [CalendarEvent], now: Date, limit: Int, calendar: Calendar = .current) -> [Event] {
        func localDay(_ text: String) -> Date? {
            CalendarDates.day(ofAllDay: text).flatMap { calendar.date(from: $0) }
        }
        let read: [Event] = events.compactMap { event in
            guard !event.isCancelled else { return nil }
            let startsAt = event.allDay ? localDay(event.startsAt) : ServerTime.date(event.startsAt)
            let endsAt = event.allDay ? localDay(event.endsAt) : ServerTime.date(event.endsAt)
            guard let startsAt, let endsAt, endsAt > now else { return nil }
            let title = (event.summary ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            return Event(
                id: event.occurrenceId, title: title, location: event.location ?? "", startsAt: startsAt, endsAt: endsAt,
                isAllDay: event.allDay)
        }
        return Array(read.sorted { ($0.startsAt, $0.title) < ($1.startsAt, $1.title) }.prefix(limit))
    }
}

extension Connection {
    /// Reads what a widget shows: the events of the next `hours` from the
    /// person's own calendar and their circles', and the unread mail in every
    /// mailbox. What other people shared with them stays in the Calendar: a
    /// widget on a lock screen shows what the person is doing, not what
    /// somebody else shared with them. A part the person cannot read is left
    /// empty rather than failing the rest.
    public func glance(now: Date = Date(), hours: Int = 48, limit: Int = 6) async throws -> Glance {
        let services = (try? await serviceCapabilities()) ?? .unavailable
        let agentName = services.isAssistantEnabled ? (try? await agentName()) ?? "" : ""
        var events: [CalendarEvent] = []
        if services.isCalendarEnabled, let calendars = try? await calendars() {
            for calendar in calendars where calendar.calendarKind != "reminders" && (calendar.isOwn || calendar.isCircleCalendar == true) {
                if let read = try? await calendarEvents(calendarId: calendar.id, from: now, until: now.addingTimeInterval(Double(hours) * 3600)) {
                    events += read.filter { !$0.isBusy }
                }
            }
        }
        let unreadCount = services.isMailboxEnabled ? ((try? await mailboxes()) ?? []).reduce(0) { $0 + Int($1.unread) } : 0
        let waiting = services.isAssistantEnabled ? try? await waiting(first: 1) : nil
        return Glance(
            events: Glance.upcoming(events, now: now, limit: limit), unreadCount: unreadCount, agentName: agentName,
            waitingCount: waiting?.waitingCount ?? 0, newestWaiting: waiting?.interactions.first)
    }
}
