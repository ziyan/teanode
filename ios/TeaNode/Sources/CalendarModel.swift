import Foundation
import Observation
import TeaNodeKit

/// MonthEvents is one month's events by day. Each month has its own, so a
/// month read from the server redraws that month and no other.
@MainActor
@Observable
final class MonthEvents {
    var eventsByDay: [CalendarDay: [CalendarEvent]] = [:]
}

/// CalendarModel is the calendar as the dashboard's Calendar page shows it:
/// the calendars of events the person reaches (their own, their circles' and
/// the ones shared with them), their events read a month at a time and kept
/// by day, and a change in flight that must be finished or stopped before
/// another is made.
@MainActor
@Observable
final class CalendarModel {
    enum ViewMode: String, CaseIterable, Identifiable {
        case month, week, workweek, day, agenda, reminders
        var id: String { rawValue }
    }

    /// The person's own calendar of events, which a new event goes in unless
    /// they choose another.
    private(set) var calendar: AgentCalendar?
    /// Every calendar of events the person reaches, their own first, then
    /// their circles', then the ones shared with them.
    private(set) var calendars: [AgentCalendar] = []
    /// The calendars hidden on this phone. Remembered here and nowhere else:
    /// whether the family's calendar is in the way on this screen is not
    /// something to carry to the dashboard.
    private(set) var hiddenIds: Set<String> = []
    /// The events of every month read so far. The month view reads each
    /// month as it scrolls into view, and every other view reads the months
    /// its days are in, so moving about asks for nothing twice.
    @ObservationIgnored private var monthEventsByMonth: [CalendarMonth: MonthEvents] = [:]
    private(set) var loadedMonths: Set<CalendarMonth> = []
    private(set) var loadingMonths: Set<CalendarMonth> = []
    var problemText: String?
    var doneText: String?

    var isLoading: Bool { !loadingMonths.isEmpty }

    var viewMode: ViewMode = .month {
        didSet { Task { await load() } }
    }
    /// The day the view is on.
    var focus = Calendar.current.startOfDay(for: Date()) {
        didSet { Task { await load() } }
    }
    /// The months the month view has on screen, read again on a refresh.
    @ObservationIgnored var visibleMonths: Set<CalendarMonth> = []
    @ObservationIgnored private var calendarTask: Task<[AgentCalendar]?, Never>?

    /// A change sent whose answer did not come back, kept on the phone until
    /// it is finished or stopped.
    private(set) var pending: CalendarChange?

    let connection: Connection
    private let pendingKey: String
    private let hiddenKey: String

    init(connection: Connection, username: String) {
        self.connection = connection
        pendingKey = "calendar-pending-change.\(username)"
        hiddenKey = "calendar-hidden.\(username)"
        hiddenIds = Set(UserDefaults.standard.stringArray(forKey: hiddenKey) ?? [])
        if let data = UserDefaults.standard.data(forKey: pendingKey) {
            pending = try? JSONDecoder().decode(CalendarChange.self, from: data)
        }
    }

    /// The person's calendar, in the week they start on.
    var dates: Calendar {
        var dates = Calendar.current
        dates.firstWeekday = calendar?.weekStart == "monday" ? 2 : 1
        return dates
    }

    /// Reads the months the view needs that are not read yet.
    func load() async {
        guard viewMode != .reminders else { return }
        await loadMonths(neededMonths, isForced: false)
    }

    /// Reads the months on screen again, and forgets the others, so they are
    /// read again when they come back into view.
    func reload() async {
        guard viewMode != .reminders else { return }
        let months = neededMonths
        loadedMonths.formIntersection(months)
        await loadMonths(months, isForced: true)
    }

    /// Reads a month the month view is about to show, unless it is read.
    func ensureMonth(_ month: CalendarMonth) {
        guard !loadedMonths.contains(month), !loadingMonths.contains(month) else { return }
        Task { await loadMonths([month], isForced: false) }
    }

    /// The months the current view draws.
    private var neededMonths: Set<CalendarMonth> {
        if viewMode == .month {
            return visibleMonths.isEmpty ? [CalendarMonth(containing: focus, in: dates)] : visibleMonths
        }
        let (from, until) = window
        let last = until.addingTimeInterval(-1)
        let firstMonth = CalendarMonth(containing: from, in: dates)
        let lastMonth = CalendarMonth(containing: last, in: dates)
        return Set((0...max(0, lastMonth.id - firstMonth.id)).map { firstMonth.adding(months: $0) })
    }

    private func loadMonths(_ months: Set<CalendarMonth>, isForced: Bool) async {
        let wanted = months.filter { !loadingMonths.contains($0) && (isForced || !loadedMonths.contains($0)) }
        guard !wanted.isEmpty else { return }
        loadingMonths.formUnion(wanted)
        defer { loadingMonths.subtract(wanted) }
        guard await eventsCalendar() != nil else { return }
        let dates = self.dates
        let read = shownCalendars
        for month in wanted.sorted() {
            guard let monthWindow = month.window(in: dates) else { continue }
            // Every calendar shown, one after another. One that cannot be
            // read (it was shared, and has just been taken back) does not
            // take the rest of the month with it; the person is told.
            var events: [CalendarEvent] = []
            var failure: Error?
            for each in read {
                do {
                    events += try await connection.calendarEvents(calendarId: each.id, from: monthWindow.from, until: monthWindow.until)
                } catch {
                    failure = failure ?? error
                }
            }
            monthEvents(month).eventsByDay = CalendarDates.eventsByDay(events, in: month, calendar: dates)
            if let failure {
                problemText = failure.localizedDescription
            } else {
                loadedMonths.insert(month)
                problemText = nil
            }
        }
    }

    /// The calendars, read once however many months ask for them at once.
    /// Answers the person's own calendar of events.
    private func eventsCalendar() async -> AgentCalendar? {
        if let calendar { return calendar }
        if calendarTask == nil {
            let connection = self.connection
            calendarTask = Task {
                do {
                    return try await connection.calendars()
                } catch {
                    problemText = error.localizedDescription
                    return nil
                }
            }
        }
        let found = await calendarTask?.value
        guard let found else {
            // Asked again next time, rather than remembered as having none.
            calendarTask = nil
            return nil
        }
        let events = found.filter { $0.calendarKind != "reminders" }
        calendars = events
        calendar = events.first(where: \.isOwn) ?? events.first ?? found.first
        return calendar
    }

    /// The calendars whose events are on the screen.
    var shownCalendars: [AgentCalendar] { calendars.filter { !hiddenIds.contains($0.id) } }

    /// The calendars an event can be put in: the person's own, a circle's,
    /// and any shared with them at edit.
    var writableCalendars: [AgentCalendar] { calendars.filter(\.canWrite) }

    /// The calendar with this identifier, if the person reaches it.
    func calendar(withId calendarId: String) -> AgentCalendar? { calendars.first { $0.id == calendarId } ?? (calendar?.id == calendarId ? calendar : nil) }

    /// Shows a calendar, or hides it, and reads the months again without it.
    func setShown(_ calendarId: String, isShown: Bool) async {
        if isShown {
            hiddenIds.remove(calendarId)
        } else {
            hiddenIds.insert(calendarId)
        }
        UserDefaults.standard.set(Array(hiddenIds), forKey: hiddenKey)
        loadedMonths = []
        await reload()
    }

    /// Reads the calendars again: after the person shares theirs or a
    /// circle's is made, or when a refresh is asked for.
    func reloadCalendars() async {
        calendar = nil
        calendarTask = nil
        _ = await eventsCalendar()
        loadedMonths = []
        await reload()
    }

    /// The days a view covers: the month, a week, five days
    /// from Monday, a day, or a month of agenda.
    var window: (Date, Date) {
        let start: Date
        let days: Int
        switch viewMode {
        case .month:
            let monthWindow = CalendarMonth(containing: focus, in: dates).window(in: dates)
            start = monthWindow?.from ?? focus
            days = monthWindow.flatMap { dates.dateComponents([.day], from: $0.from, to: $0.until).day } ?? 30
        case .week:
            start = dates.dateInterval(of: .weekOfYear, for: focus)?.start ?? focus
            days = 7
        case .workweek:
            var monday = dates
            monday.firstWeekday = 2
            start = monday.dateInterval(of: .weekOfYear, for: focus)?.start ?? focus
            days = 5
        case .day:
            start = focus
            days = 1
        case .agenda, .reminders:
            start = focus
            days = 31
        }
        return (start, dates.date(byAdding: .day, value: days, to: start) ?? start)
    }

    /// The days of the window, for the grids.
    var days: [Date] {
        let (from, until) = window
        var result: [Date] = []
        var day = from
        while day < until {
            result.append(day)
            day = dates.date(byAdding: .day, value: 1, to: day) ?? until
        }
        return result
    }

    /// Back or forward a step: a day, a week, a month, or a month of agenda.
    func step(_ direction: Int) {
        switch viewMode {
        case .month:
            focus = dates.date(byAdding: .month, value: direction, to: dates.date(from: dates.dateComponents([.year, .month], from: focus)) ?? focus) ?? focus
        case .week, .workweek:
            focus = dates.date(byAdding: .day, value: 7 * direction, to: focus) ?? focus
        case .day:
            focus = dates.date(byAdding: .day, value: direction, to: focus) ?? focus
        case .agenda, .reminders:
            focus = dates.date(byAdding: .day, value: 31 * direction, to: focus) ?? focus
        }
    }

    /// The events on a day, every one it is part of: all-day ones first,
    /// then by when they start.
    func events(on day: Date) -> [CalendarEvent] {
        let calendarDay = CalendarDay(containing: day, in: dates)
        return monthEvents(calendarDay.calendarMonth).eventsByDay[calendarDay] ?? []
    }

    /// A month's events, empty until the month is read.
    func monthEvents(_ month: CalendarMonth) -> MonthEvents {
        if let found = monthEventsByMonth[month] { return found }
        let made = MonthEvents()
        monthEventsByMonth[month] = made
        return made
    }

    // MARK: Changes

    /// Saves or deletes an event, keeping the change on the phone until the
    /// server answers, so one whose answer is lost is never made twice.
    func make(_ change: CalendarChange) async -> Bool {
        guard pending == nil else {
            problemText = String(localized: "Resolve the pending calendar change first.")
            return false
        }
        keep(change)
        return await send(change, isRetry: false)
    }

    func retryPending() async {
        guard let pending else { return }
        _ = await send(pending, isRetry: true)
    }

    func stopPending() async {
        guard let pending else { return }
        do {
            let hadHappened = try await connection.cancel(pending)
            keep(nil)
            doneText = hadHappened ? String(localized: "Calendar change completed") : String(localized: "The pending change was stopped")
            await reload()
        } catch {
            problemText = error.localizedDescription
        }
    }

    private func send(_ change: CalendarChange, isRetry: Bool) async -> Bool {
        do {
            try await connection.perform(change, isRetry: isRetry)
            keep(nil)
            doneText = change.operation == "delete"
                ? String(localized: "\(change.summary) removed") : String(localized: "\(change.summary) saved")
            await reload()
            return true
        } catch {
            problemText = error.localizedDescription
            return false
        }
    }

    private func keep(_ change: CalendarChange?) {
        pending = change
        if let change, let data = try? JSONEncoder().encode(change) {
            UserDefaults.standard.set(data, forKey: pendingKey)
        } else {
            UserDefaults.standard.removeObject(forKey: pendingKey)
        }
    }
}

/// EventDraft is the event editor's fields, read from the event whole or
/// made for a new one, and written back as every field.
struct EventDraft: Identifiable {
    let id = UUID()
    var eventId: String?
    var summary = ""
    var location = ""
    var notes = ""
    var isAllDay = false
    var startsAt = Date()
    var endsAt = Date().addingTimeInterval(3600)
    var recurrence = ""
    var status = ""
    /// Whether the owner marked it private, so that people the calendar is
    /// shared with see only that they are busy.
    var isPrivate = false
    var invited = ""
    /// The calendar it is in or goes in: the person's own unless they chose
    /// another. Nil until the calendars are read.
    var calendarId: String?

    /// The repeat choices the editor offers; any other rule is kept as it is.
    static let repeats = ["", "FREQ=DAILY", "FREQ=WEEKLY", "FREQ=MONTHLY", "FREQ=YEARLY"]

    init(startingAt start: Date) {
        startsAt = start
        endsAt = start.addingTimeInterval(3600)
    }

    init(_ detail: CalendarEventDetail, calendarId: String? = nil) {
        eventId = detail.id
        self.calendarId = calendarId
        summary = detail.summary ?? ""
        location = detail.location ?? ""
        notes = detail.description ?? ""
        isAllDay = detail.allDay
        recurrence = detail.recurrence ?? ""
        status = detail.status ?? ""
        isPrivate = detail.isPrivate ?? false
        invited = (detail.attendees ?? []).map(\.address).joined(separator: "\n")
        if detail.allDay {
            var local = Calendar.current
            local.timeZone = .current
            let start = CalendarDates.day(ofAllDay: detail.startsAt)
            let last = CalendarDates.lastDay(startsAt: detail.startsAt, endsAt: detail.endsAt)
            startsAt = start.flatMap { local.date(from: $0) } ?? Date()
            endsAt = last.flatMap { local.date(from: $0) } ?? startsAt
        } else {
            startsAt = ServerTime.date(detail.startsAt) ?? Date()
            endsAt = ServerTime.date(detail.endsAt) ?? startsAt.addingTimeInterval(3600)
        }
    }

    /// The change that saves it: every field, an empty one clearing it.
    func change(calendar: AgentCalendar) -> CalendarChange {
        let local = Calendar.current
        var variables: [String: JSONValue] = [
            "calendarId": .string(calendar.id),
            "summary": .string(summary.trimmingCharacters(in: .whitespacesAndNewlines)),
            "location": .string(location.trimmingCharacters(in: .whitespacesAndNewlines)),
            "description": .string(notes.trimmingCharacters(in: .whitespacesAndNewlines)),
            "allDay": .bool(isAllDay),
            "recurrence": .string(recurrence.trimmingCharacters(in: .whitespacesAndNewlines)),
            "status": .string(status),
            // Only in the person's own calendar: in a circle's, or one shared
            // with them at edit, every reader is an editor and sees the event
            // whatever it says.
            "isPrivate": calendar.isOwn ? .bool(isPrivate) : .null,
            // Only in the person's own calendar: the invitations go out from,
            // and the answers come back to, the owner, so a circle's calendar
            // or one shared with them has none.
            "attendees": calendar.isOwn
                ? .array(invited.split(whereSeparator: \.isNewline).map { .string($0.trimmingCharacters(in: .whitespaces)) }.filter { $0 != .string("") })
                : .null,
        ]
        variables["id"] = eventId.map { .string($0) } ?? .null
        if isAllDay {
            let first = local.dateComponents([.year, .month, .day], from: startsAt)
            let last = local.dateComponents([.year, .month, .day], from: max(startsAt, endsAt))
            variables["startsAt"] = .string(CalendarDates.allDayText(first))
            variables["endsAt"] = .string(CalendarDates.allDayText(CalendarDates.dayAfter(last)))
            variables["timezone"] = .string("")
        } else {
            variables["startsAt"] = .string(CalendarDates.text(startsAt))
            variables["endsAt"] = .string(CalendarDates.text(max(endsAt, startsAt)))
            variables["timezone"] = .string(calendar.timezone.flatMap { $0.isEmpty ? nil : $0 } ?? TimeZone.current.identifier)
        }
        return CalendarChange(operation: "save", summary: summary, variables: variables)
    }
}
