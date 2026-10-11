import Foundation
import Observation
import TeaNodeKit

/// CalendarModel is the calendar as the dashboard's Calendar page shows it:
/// the events calendar, its events read a month at a time and kept by day,
/// and a change in flight that must be finished or stopped before another
/// is made.
@MainActor
@Observable
final class CalendarModel {
    enum ViewMode: String, CaseIterable, Identifiable {
        case month, week, workweek, day, agenda, reminders
        var id: String { rawValue }
    }

    private(set) var calendar: AgentCalendar?
    /// The events on each day of every month read so far. The month view
    /// reads each month as it scrolls into view, and every other view reads
    /// the months its days are in, so moving about asks for nothing twice.
    private(set) var eventsByDay: [CalendarDay: [CalendarEvent]] = [:]
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
    @ObservationIgnored private var calendarTask: Task<AgentCalendar?, Never>?

    /// A change sent whose answer did not come back, kept on the phone until
    /// it is finished or stopped.
    private(set) var pending: CalendarChange?

    let connection: Connection
    private let pendingKey: String

    init(connection: Connection, username: String) {
        self.connection = connection
        pendingKey = "calendar-pending-change.\(username)"
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
        guard let calendar = await eventsCalendar() else { return }
        let dates = self.dates
        for month in wanted.sorted() {
            guard let monthWindow = month.window(in: dates) else { continue }
            do {
                let events = try await connection.calendarEvents(calendarId: calendar.id, from: monthWindow.from, until: monthWindow.until)
                var kept = eventsByDay.filter { $0.key.calendarMonth != month }
                kept.merge(CalendarDates.eventsByDay(events, in: month, calendar: dates)) { _, read in read }
                eventsByDay = kept
                loadedMonths.insert(month)
                problemText = nil
            } catch {
                problemText = error.localizedDescription
            }
        }
    }

    /// The events calendar, read once however many months ask for it at once.
    private func eventsCalendar() async -> AgentCalendar? {
        if let calendar { return calendar }
        if calendarTask == nil {
            let connection = self.connection
            calendarTask = Task {
                do {
                    let calendars = try await connection.calendars()
                    return calendars.first { $0.calendarKind == "events" } ?? calendars.first
                } catch {
                    problemText = error.localizedDescription
                    return nil
                }
            }
        }
        let found = await calendarTask?.value
        if found == nil {
            // Asked again next time, rather than remembered as having none.
            calendarTask = nil
        }
        calendar = found
        return found
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
        eventsByDay[CalendarDay(containing: day, in: dates)] ?? []
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
    var invited = ""

    /// The repeat choices the editor offers; any other rule is kept as it is.
    static let repeats = ["", "FREQ=DAILY", "FREQ=WEEKLY", "FREQ=MONTHLY", "FREQ=YEARLY"]

    init(startingAt start: Date) {
        startsAt = start
        endsAt = start.addingTimeInterval(3600)
    }

    init(_ detail: CalendarEventDetail) {
        eventId = detail.id
        summary = detail.summary ?? ""
        location = detail.location ?? ""
        notes = detail.description ?? ""
        isAllDay = detail.allDay
        recurrence = detail.recurrence ?? ""
        status = detail.status ?? ""
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
            "attendees": .array(invited.split(whereSeparator: \.isNewline).map { .string($0.trimmingCharacters(in: .whitespaces)) }.filter { $0 != .string("") }),
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
