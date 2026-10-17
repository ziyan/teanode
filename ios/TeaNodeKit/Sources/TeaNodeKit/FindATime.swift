import Foundation

// Finding a time when several people are all free: the people who shared their
// calendar with the person, a run of days and the hours of the day to offer,
// and the stretches nobody has anything booked in. The server says when each
// is busy (ListCalendarFreeBusy, which the dashboard, the command line's
// `calendar free --with` and the agent's free_together ask too) and FreeTime
// does the sum they all do, so that the time one offers is the time the others
// offer.

extension Documents {
    public static let calendarFreeBusy = """
        query ($userIds: [String!]!, $from: String!, $until: String!) {
          ListCalendarFreeBusy(userIds: $userIds, from: $from, until: $until) {
            userId username name isShared periods { startsAt endsAt }
          }
        }
        """
}

/// CalendarFreeBusy is when one person is busy, for a person they share their
/// calendar with at busy or more. A person who shared nothing has no periods
/// and is not shared: nothing can be said of when they are free.
public struct CalendarFreeBusy: Codable, Hashable, Sendable {
    public struct Period: Codable, Hashable, Sendable {
        public let startsAt: String
        public let endsAt: String

        public init(startsAt: String, endsAt: String) {
            self.startsAt = startsAt
            self.endsAt = endsAt
        }
    }

    public let userId: String
    public let username: String?
    public let name: String?
    public let isShared: Bool
    public let periods: [Period]

    public init(userId: String, username: String? = nil, name: String? = nil, isShared: Bool, periods: [Period] = []) {
        self.userId = userId
        self.username = username
        self.name = name
        self.isShared = isShared
        self.periods = periods
    }
}

/// FreeStretch is a stretch of one day when nobody asked about is busy.
public struct FreeStretch: Hashable, Sendable, Identifiable {
    /// The day, at its start.
    public let day: Date
    public let startsAt: Date
    public let endsAt: Date

    public var id: Date { startsAt }
}

/// FreeTimeQuestion is what the Find a time screen asks, as the person typed
/// it: the days, as the first and the last, and the hours of the day.
public struct FreeTimeQuestion: Hashable, Sendable {
    public var firstDay: Date
    public var lastDay: Date
    /// Minutes into the day, 540 for 09:00.
    public var earliestMinutes = 9 * 60
    public var latestMinutes = 17 * 60

    public init(firstDay: Date, lastDay: Date) {
        self.firstDay = firstDay
        self.lastDay = lastDay
    }

    /// What is wrong with the question, for the screen to say in its own words,
    /// or nil.
    public enum Problem: Sendable {
        case lastDayBeforeFirst
        case dayEndsBeforeItBegins
    }

    public var problem: Problem? {
        if lastDay < firstDay { return .lastDayBeforeFirst }
        if latestMinutes <= earliestMinutes { return .dayEndsBeforeItBegins }
        return nil
    }
}

public enum FreeTime {
    /// The most people one question may hold: the server takes 20 and the
    /// person is one of them.
    public static let mostPeoplePerQuestion = 19

    /// The stretches nobody in `busy` is busy, for each day from `first` up to
    /// but not including `dayAfterLast`, between the earliest and the latest
    /// minute of the day. Overlapping periods are one, so what is left is when
    /// everybody could meet.
    public static func freeByDay(
        busy: [CalendarFreeBusy.Period], from first: Date, dayAfterLast: Date, earliestMinutes: Int, latestMinutes: Int,
        calendar: Calendar = .current
    ) -> [FreeStretch] {
        let periods = busy.compactMap { period -> (Date, Date)? in
            guard let starts = ServerTime.date(period.startsAt), let ends = ServerTime.date(period.endsAt) else { return nil }
            return (starts, ends)
        }
        var free: [FreeStretch] = []
        var day = calendar.startOfDay(for: first)
        while day < dayAfterLast {
            guard let opens = calendar.date(byAdding: .minute, value: earliestMinutes, to: day),
                let closes = calendar.date(byAdding: .minute, value: latestMinutes, to: day)
            else { break }
            var cursor = opens
            let clipped =
                periods
                .map { (max($0.0, opens), min($0.1, closes)) }
                .filter { $0.0 < $0.1 }
                .sorted { $0.0 < $1.0 }
            for (starts, ends) in clipped {
                if starts > cursor { free.append(FreeStretch(day: day, startsAt: cursor, endsAt: starts)) }
                cursor = max(cursor, ends)
            }
            if cursor < closes { free.append(FreeStretch(day: day, startsAt: cursor, endsAt: closes)) }
            guard let next = calendar.date(byAdding: .day, value: 1, to: day) else { break }
            day = next
        }
        return free
    }

    /// The day after the last the person asked about, which is where the
    /// question ends.
    public static func dayAfter(_ lastDay: Date, calendar: Calendar = .current) -> Date {
        calendar.date(byAdding: .day, value: 1, to: calendar.startOfDay(for: lastDay)) ?? lastDay
    }
}

/// FreeTimeAnswer is what asking for a time came to: the stretches, or the
/// people who stopped sharing since the list was made, since a time that
/// leaves somebody out does not suit them and none is offered.
public enum FreeTimeAnswer: Equatable, Sendable {
    case free([FreeStretch])
    case notShared([String])
}

extension Connection {
    /// When the people are busy between two moments, a question at a time
    /// when there are more of them than one question holds.
    public func calendarFreeBusy(userIds: [String], from: Date, until: Date) async throws -> [CalendarFreeBusy] {
        struct Answer: Decodable, Sendable { let ListCalendarFreeBusy: [CalendarFreeBusy] }
        var answers: [CalendarFreeBusy] = []
        var start = 0
        while start < userIds.count {
            let chunk = Array(userIds[start..<min(start + FreeTime.mostPeoplePerQuestion, userIds.count)])
            answers += try await execute(
                Documents.calendarFreeBusy,
                variables: [
                    "userIds": .array(chunk.map { .string($0) }), "from": .string(CalendarDates.text(from)),
                    "until": .string(CalendarDates.text(until)),
                ], as: Answer.self
            ).ListCalendarFreeBusy
            start += FreeTime.mostPeoplePerQuestion
        }
        return answers
    }

    /// The people who shared their calendar with the person, learned by asking
    /// when everybody is busy tomorrow, which says isShared for each; and
    /// whether somebody they share a circle with was left out.
    public func peopleSharingTheirCalendar(now: Date = Date()) async throws -> (people: [CalendarSharePerson], isAnyoneLeftOut: Bool) {
        let everybody = try await calendarSharePeople()
        if everybody.isEmpty { return ([], false) }
        let tomorrow = FreeTime.dayAfter(now)
        let answers = try await calendarFreeBusy(userIds: everybody.map(\.userId), from: now, until: tomorrow)
        let sharing = Set(answers.filter(\.isShared).map(\.userId))
        let people = everybody.filter { sharing.contains($0.userId) }
        return (people, people.count < everybody.count)
    }

    /// When the person and the people chosen are all free.
    public func freeTime(
        ownUserId: String, with chosen: [CalendarSharePerson], question: FreeTimeQuestion, calendar: Calendar = .current
    ) async throws -> FreeTimeAnswer {
        let first = calendar.startOfDay(for: question.firstDay)
        let dayAfterLast = FreeTime.dayAfter(question.lastDay, calendar: calendar)
        let answers = try await calendarFreeBusy(userIds: [ownUserId] + chosen.map(\.userId), from: first, until: dayAfterLast)
        let missing = chosen.filter { person in !answers.contains { $0.userId == person.userId && $0.isShared } }
        if !missing.isEmpty { return .notShared(missing.map(\.username)) }
        return .free(
            FreeTime.freeByDay(
                busy: answers.flatMap(\.periods), from: first, dayAfterLast: dayAfterLast,
                earliestMinutes: question.earliestMinutes, latestMinutes: question.latestMinutes, calendar: calendar))
    }
}
