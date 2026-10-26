import Foundation

// Pausing where the phone records. A pause is the person's, kept by the
// server so that every device and every person who looks reads the same
// thing; the phone learns it from the server, applies a pause it makes itself
// at once, and tells the server until the server has it. None of it needs
// Core Location, so it is tested here; LocationRecorder.swift in the app
// feeds it.

/// LocationPause is a stretch of time in which the person stopped TeaNode
/// recording where they go. The server keeps one for each time, so that a
/// timeline can show the gap.
public struct LocationPause: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    /// RFC 3339.
    public let startedAt: String
    /// RFC 3339; nil until the person resumes.
    public let endedAt: String?

    public var startedDate: Date? { ServerTime.date(startedAt) }
    public var endedDate: Date? { ServerTime.date(endedAt) }

    /// Whether the pause goes on until the person resumes.
    public var isOpenEnded: Bool { endedAt == nil }

    public init(id: String, startedAt: String, endedAt: String? = nil) {
        self.id = id
        self.startedAt = startedAt
        self.endedAt = endedAt
    }

    public init(id: String, startedDate: Date, endedDate: Date?) {
        self.init(id: id, startedAt: CalendarDates.text(startedDate), endedAt: endedDate.map(CalendarDates.text))
    }

    /// Whether it is in effect at an instant: it has started, and has not
    /// ended. The same rule the server applies to what the phone reports.
    public func isInEffect(at date: Date) -> Bool {
        guard let startedDate, startedDate <= date else { return false }
        if isOpenEnded { return true }
        guard let endedDate else { return false }
        return date < endedDate
    }

    /// The same pause ending at another time, or never.
    func ending(at endedDate: Date?) -> LocationPause {
        LocationPause(id: id, startedAt: startedAt, endedAt: endedDate.map(CalendarDates.text))
    }
}

/// LocationPauseChoice is what the person chooses from when they pause.
public enum LocationPauseChoice: String, CaseIterable, Identifiable, Sendable {
    case anHour
    case untilTomorrowMorning
    case untilResumed

    public var id: String { rawValue }

    /// How long "for an hour" is.
    public static let anHourSeconds = 3600.0

    /// The hour of the morning "until tomorrow morning" is, in the phone's
    /// own zone.
    public static let morningHour = 8

    /// Before this hour, "until tomorrow morning" means this morning: a person
    /// pausing at half past midnight is not asking for a day and a half.
    public static let earliestTomorrowHour = 5

    /// When the pause ends if it begins at `now`; nil for one that goes on
    /// until the person resumes. The server takes only an end time, so the
    /// morning is worked out here, in the zone of the calendar given: 08:00 the
    /// next day, or 08:00 the same day before 05:00.
    public func endDate(from now: Date, calendar: Calendar = .current) -> Date? {
        switch self {
        case .anHour:
            return now.addingTimeInterval(Self.anHourSeconds)
        case .untilTomorrowMorning:
            let fallback = now.addingTimeInterval(24 * 3600)
            let isBeforeEarliestTomorrow = calendar.component(.hour, from: now) < Self.earliestTomorrowHour
            let dayOfMorning = isBeforeEarliestTomorrow ? 0 : 1
            guard let morningDay = calendar.date(byAdding: .day, value: dayOfMorning, to: now) else { return fallback }
            return calendar.date(bySettingHour: Self.morningHour, minute: 0, second: 0, of: morningDay) ?? fallback
        case .untilResumed:
            return nil
        }
    }
}

/// LocationPauseTracker is what the phone believes about the person's pause:
/// the one the server last said was in effect, and a change the person made on
/// the phone that the server has not taken yet. Pausing and resuming apply
/// here at once, so the phone stops recording before the call is answered,
/// and the change is sent until the server has it. It is kept in the phone's
/// defaults, so a closed app that is woken for a visit still knows.
public struct LocationPauseTracker: Codable, Equatable, Sendable {
    /// A change made on the phone, waiting for the server.
    public enum Change: Codable, Equatable, Sendable {
        /// Pause from this instant, or change the end of the pause in effect;
        /// nil ends it when the person resumes.
        case pause(startedAt: Date, endedAt: Date?)
        /// End the pause in effect at this instant.
        case resume(at: Date)
    }

    /// The identifier of a pause the server does not know yet.
    public static let unconfirmedPauseID = "unconfirmed"

    /// The pause in effect, as the server last said, or nil if it said there
    /// was none.
    public private(set) var confirmedPause: LocationPause?
    public private(set) var pendingChange: Change?

    public init(confirmedPause: LocationPause? = nil, pendingChange: Change? = nil) {
        self.confirmedPause = confirmedPause
        self.pendingChange = pendingChange
    }

    /// The pause as the person sees it: the server's, with the change made on
    /// the phone applied. Not necessarily in effect now: a pause that has
    /// ended is kept so a position measured inside it, noticed late, is still
    /// recognized.
    public var pause: LocationPause? {
        switch pendingChange {
        case nil:
            return confirmedPause
        case .pause(let startedAt, let endedAt)?:
            if let confirmedPause, confirmedPause.isInEffect(at: startedAt) { return confirmedPause.ending(at: endedAt) }
            return LocationPause(id: Self.unconfirmedPauseID, startedDate: startedAt, endedDate: endedAt)
        case .resume(let resumedAt)?:
            guard let confirmedPause, confirmedPause.isInEffect(at: resumedAt) else { return confirmedPause }
            return confirmedPause.ending(at: resumedAt)
        }
    }

    /// Whether a change waits for the server.
    public var hasPendingChange: Bool { pendingChange != nil }

    /// Whether recording is paused at an instant.
    public func isPaused(at date: Date) -> Bool { pause?.isInEffect(at: date) ?? false }

    /// When the pause in effect ends by itself, if it does.
    public func endDate(at now: Date) -> Date? {
        guard let pause, pause.isInEffect(at: now) else { return nil }
        return pause.endedDate
    }

    /// Whether to record a position measured at an instant: not if it falls
    /// in the pause. The server drops it too, so a phone that was offline
    /// cannot fill the gap afterwards; this keeps it out of the outbox.
    public func isRecordable(pointMeasuredAt date: Date) -> Bool { !isPaused(at: date) }

    /// Whether to record a visit iOS noticed: not if it began (or, when the
    /// beginning was not seen, ended) in the pause. A visit that began before
    /// the pause and ended in it is sent whole, and the server ends it where
    /// the pause began.
    public func isRecordable(visitArrivedAt arrivedAt: Date?, departedAt: Date?) -> Bool {
        guard let anchor = arrivedAt ?? departedAt else { return true }
        return !isPaused(at: anchor)
    }

    /// When a departure noticed at an instant is told: where the pause began
    /// if the instant is in it, since the server ended the visit there, else
    /// the instant itself.
    public func departureDate(noticedAt date: Date) -> Date {
        guard let pause, pause.isInEffect(at: date), let startedDate = pause.startedDate else { return date }
        return startedDate
    }

    // MARK: Changing

    /// The person paused on the phone: for the length chosen, or until they
    /// resume (`endedAt` nil).
    public mutating func startPause(until endedAt: Date?, at now: Date) {
        pendingChange = .pause(startedAt: now, endedAt: endedAt)
    }

    /// The person resumed on the phone. A pause the server has not answered
    /// for may already be on its way there, so the resume waits to be sent
    /// after it; nothing is sent when no pause was ever sent.
    public mutating func endPause(at now: Date) {
        guard isPaused(at: now) else { return }
        if let confirmedPause, confirmedPause.isInEffect(at: now) {
            pendingChange = .resume(at: now)
        } else if case .pause? = pendingChange {
            pendingChange = .resume(at: now)
        } else {
            pendingChange = nil
        }
    }

    /// The server said what is in effect, from `GetLocation` or the answer to
    /// a report. A change made on the phone and not yet taken still applies
    /// over it.
    public mutating func learn(_ serverPause: LocationPause?, at now: Date) {
        confirmedPause = serverPause
        // A resume waiting to be sent is kept even when the server says it has
        // no pause: the answer may have left before the pause it ends reached
        // the server. A resume the server does not need changes nothing, unless
        // a newer pause was made on another device and is not yet learned here,
        // which it ends: a short window, and the person resumed anyway.
        settle(at: now)
    }

    /// The server took the change. `answer` is the pause it now has in effect,
    /// nil after a resume.
    public mutating func changeWasTaken(_ change: Change, answer: LocationPause?, at now: Date) {
        confirmedPause = answer
        if pendingChange == change {
            pendingChange = nil
        } else if case .pause = change, case .resume(let resumedAt)? = pendingChange, let startedDate = answer?.startedDate, resumedAt < startedDate {
            // Resumed while the pause was on its way: the server began it a
            // moment after the tap, and the resume is for that pause.
            pendingChange = .resume(at: startedDate)
        }
        settle(at: now)
    }

    /// The server will not take the change, and asking again cannot help.
    public mutating func changeWasRefused(_ change: Change) {
        if pendingChange == change { pendingChange = nil }
    }

    /// Drops a change that no longer means anything: a pause whose end has
    /// passed, a resume of a pause the server says is over. A resume while the
    /// server has no pause at all is kept: the pause it ends may still be on
    /// its way, and sending it first does no harm.
    public mutating func settle(at now: Date) {
        switch pendingChange {
        case .pause(_, let endedAt)?:
            if let endedAt, endedAt <= now { pendingChange = nil }
        case .resume(let resumedAt)?:
            if let confirmedPause, !confirmedPause.isInEffect(at: resumedAt) { pendingChange = nil }
        case nil:
            break
        }
    }

    // MARK: Keeping

    static func makeEncoder() -> JSONEncoder {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        encoder.outputFormatting = [.sortedKeys]
        return encoder
    }

    static func makeDecoder() -> JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }

    /// What is kept in the defaults.
    public func encoded() throws -> Data { try Self.makeEncoder().encode(self) }

    public static func decoded(from data: Data) throws -> LocationPauseTracker { try makeDecoder().decode(LocationPauseTracker.self, from: data) }

    /// The key an account's tracker is kept under: one for each account, so a
    /// pause made as one person is never read as another's.
    public static func defaultsKey(accountKey: String) -> String { "locationPauseTracker.\(accountKey)" }
}

/// LocationPauseFailure is what the phone does about a pause or a resume the
/// server did not take.
public enum LocationPauseFailure: Equatable, Sendable {
    /// The call did not get through, or the sign-in needs renewing: keep the
    /// change and send it again.
    case retryLater
    /// The server refused it (not allowed to use Location, or an end time it
    /// does not take): asking again cannot change the answer. Carries the
    /// server's reason.
    case refused(String)

    public static func classify(_ error: Error) -> LocationPauseFailure {
        guard let known = error as? TeaNodeError, case .refused(let reason) = known else { return .retryLater }
        if reason.contains("permission denied") || reason.contains("invalid arguments") { return .refused(reason) }
        return .retryLater
    }
}

/// LocationPauseRetry is how long the phone waits before it sends a change of
/// pause again: it doubles after each failure, up to a quarter of an hour.
public enum LocationPauseRetry {
    public static let firstSeconds = 15.0
    public static let mostSeconds = 900.0

    /// The wait after this many failures in a row (the first is 1).
    public static func delaySeconds(afterFailureCount failureCount: Int) -> Double {
        guard failureCount > 1 else { return firstSeconds }
        return min(firstSeconds * pow(2, Double(min(failureCount - 1, 10))), mostSeconds)
    }
}

/// LocationTimelineEntry is one row of a timeline: a visit, a pause, which
/// shows as the gap it is, or a trip between two visits.
public enum LocationTimelineEntry: Identifiable, Hashable, Sendable {
    case visit(PlaceVisit)
    case pause(LocationPause)
    case trip(Trip)

    public var id: String {
        switch self {
        case .visit(let visit): "visit:\(visit.id)"
        case .pause(let pause): "pause:\(pause.id)"
        case .trip(let trip): "trip:\(trip.id)"
        }
    }

    /// Where it is placed on a day: a visit's arrival (else its departure), a
    /// pause's start, a trip's start (the departure from the visit before it,
    /// so it sits between that visit and the next).
    public var anchorDate: Date? {
        switch self {
        case .visit(let visit): visit.anchorDate
        case .pause(let pause): pause.startedDate
        case .trip(let trip): trip.startedDate
        }
    }
}

extension PersonLocation {
    /// Whether the person has paused sharing where they are, at an instant.
    public func isPaused(at date: Date = Date()) -> Bool { pause?.isInEffect(at: date) ?? false }
}
