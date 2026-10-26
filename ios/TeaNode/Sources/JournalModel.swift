import Foundation
import Observation
import TeaNodeKit

/// JournalModel is the Journal's timeline: the person's days, newest first,
/// with each week's headline, and today.
@MainActor
@Observable
final class JournalModel {
    /// How many days one read brings.
    static let pageSize = 31

    let connection: Connection

    private(set) var days: [JournalDaySummary] = []
    private(set) var totalCount = 0
    /// The person's today, as the server says it, in their zone.
    private(set) var today = ""
    /// The earliest day with anything in it, empty for none.
    private(set) var firstDay = ""
    private(set) var settings: JournalSettings?
    /// Today whole, for the card at the top: its mood, weather and numbers.
    private(set) var todayDay: JournalDay?
    /// The headline of each week that has one, by the week's first day.
    private(set) var weekHeadlines: [String: String] = [:]
    private(set) var hasLoaded = false
    private(set) var loadFailedText: String?
    private(set) var isLoadingMore = false
    var problemText: String?
    var doneText: String?
    /// The weeks whose headline was asked for already.
    private var requestedWeekStarts: Set<String> = []

    init(connection: Connection) {
        self.connection = connection
    }

    /// The day weeks start on: the server's word for the person's locale,
    /// the phone's own until it has been read.
    var weekStartsOn: String { settings?.weekStartsOn ?? Self.localeWeekStartsOn }

    static var localeWeekStartsOn: String {
        Calendar.current.firstWeekday == 1 ? "sunday" : "monday"
    }

    /// Today, from the phone until the server has said it.
    var shownToday: String { today.isEmpty ? JournalDays.text(Date()) : today }

    var weekGroups: [JournalWeekGroup] {
        JournalDays.weekGroups(of: days, weekStartsOn: weekStartsOn)
    }

    var canLoadMore: Bool { days.count < totalCount }

    // MARK: Reading

    /// Reads the first page of days, today and the settings. Pulling the
    /// screen down asks for the weeks' headlines again too.
    func load(isRefreshing: Bool = false) async {
        defer { hasLoaded = true }
        do {
            let page = try await connection.journalDays(first: Self.pageSize)
            days = page.days
            totalCount = page.totalCount
            today = page.today
            firstDay = page.firstDay
            loadFailedText = nil
        } catch {
            if Task.isCancelled { return }
            if days.isEmpty { loadFailedText = error.loadProblemText } else { problemText = error.loadProblemText }
            return
        }
        if let readSettings = try? await connection.journalSettings() { settings = readSettings }
        await loadToday()
        if isRefreshing { requestedWeekStarts = [] }
        await loadWeekHeadlines()
    }

    private func loadToday() async {
        if let read = try? await connection.journalDay(shownToday) { todayDay = read }
    }

    /// Reads the headline of each week on the screen, one after another: the
    /// list does not wait for them.
    func loadWeekHeadlines() async {
        for group in weekGroups where !requestedWeekStarts.contains(group.weekStart) {
            if Task.isCancelled { return }
            requestedWeekStarts.insert(group.weekStart)
            if let period = try? await connection.journalPeriod(.week, containing: group.weekStart) {
                weekHeadlines[group.weekStart] = period.narrative?.headline ?? ""
            } else {
                requestedWeekStarts.remove(group.weekStart)
            }
        }
    }

    /// Reads the next page when the last day on the screen comes into view.
    func loadMore(after day: JournalDaySummary) async {
        guard day.id == days.last?.id, canLoadMore, !isLoadingMore else { return }
        isLoadingMore = true
        defer { isLoadingMore = false }
        do {
            let page = try await connection.journalDays(first: Self.pageSize, offset: days.count)
            let known = Set(days.map(\.id))
            days += page.days.filter { !known.contains($0.id) }
            totalCount = page.totalCount
        } catch {
            if !Task.isCancelled { problemText = error.loadProblemText }
            return
        }
        await loadWeekHeadlines()
    }

    /// Reads today again, and its row of the list, after the person changed
    /// something about it.
    func refreshToday() async {
        await loadToday()
        guard let page = try? await connection.journalDays(fromDay: shownToday, untilDay: shownToday, first: 1) else { return }
        days.removeAll { $0.day == shownToday }
        if let summary = page.days.first { days.insert(summary, at: 0) }
    }

    // MARK: Today's mood

    /// Sets today's mood from the card.
    func setTodayMood(_ level: JournalMoodLevel) async {
        do {
            try await connection.setJournalMood(day: shownToday, moodLevel: level.rawValue, moodWords: todayDay?.mood?.moodWords)
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "Could not save how today was.")).asReasonSentence
            return
        }
        await refreshToday()
    }
}

/// JournalDayModel is one day of the journal.
@MainActor
@Observable
final class JournalDayModel {
    let connection: Connection

    private(set) var day: String
    private(set) var journalDay: JournalDay?
    private(set) var hasLoaded = false
    private(set) var loadFailedText: String?
    private(set) var isBusy = false
    var problemText: String?
    var doneText: String?

    init(connection: Connection, day: String) {
        self.connection = connection
        self.day = day
    }

    /// Whether the touched-up words or the narrative are being written.
    var isPending: Bool { journalDay?.isPending ?? false }

    /// Moves to another day, which is read at once.
    func show(day newDay: String) async {
        guard newDay != day else { return }
        day = newDay
        journalDay = nil
        hasLoaded = false
        loadFailedText = nil
        await load()
    }

    func load(isQuiet: Bool = false) async {
        let requestedDay = day
        defer { if requestedDay == day { hasLoaded = true } }
        do {
            let read = try await connection.journalDay(requestedDay)
            // The person may have moved to another day while this was read.
            guard requestedDay == day else { return }
            journalDay = read
            loadFailedText = nil
        } catch {
            guard !Task.isCancelled, requestedDay == day, !isQuiet else { return }
            if journalDay == nil { loadFailedText = error.loadProblemText } else { problemText = error.loadProblemText }
        }
    }

    /// Reads the day again every few seconds while something is being written
    /// for it, for as long as the screen would wait.
    func readWhilePending() async {
        var attemptCount = 0
        while JournalPolling.shouldRead(isPending: isPending, attemptCount: attemptCount) {
            do { try await Task.sleep(for: JournalPolling.interval) } catch { return }
            attemptCount += 1
            await load(isQuiet: true)
        }
    }

    // MARK: Changes

    /// Does a change and reads the day again; whether it was done.
    @discardableResult
    private func change(orSay failureText: String, _ operation: () async throws -> Void) async -> Bool {
        isBusy = true
        defer { isBusy = false }
        do {
            try await operation()
        } catch {
            problemText = error.refusalReason(orSay: failureText).asReasonSentence
            return false
        }
        await load(isQuiet: true)
        return true
    }

    func setMood(_ level: JournalMoodLevel) async {
        let words = journalDay?.mood?.moodWords
        await change(orSay: String(localized: "Could not save how the day was.")) {
            try await connection.setJournalMood(day: day, moodLevel: level.rawValue, moodWords: words)
        }
    }

    func clearMood() async {
        await change(orSay: String(localized: "Could not clear how the day was.")) {
            try await connection.setJournalMood(day: day, moodLevel: nil)
        }
    }

    func writeAgain() async {
        await change(orSay: String(localized: "Could not ask for the day to be written again.")) {
            try await connection.writeJournalNarrative(for: .day(day))
        }
    }

    func removeCorrection(at index: Int) async {
        await change(orSay: String(localized: "Could not remove that correction.")) {
            try await connection.removeJournalCorrection(for: .day(day), correctionIndex: index)
        }
    }
}

/// JournalPeriodModel is one week, month or year of the journal.
@MainActor
@Observable
final class JournalPeriodModel {
    let connection: Connection
    let periodKind: JournalPeriodKind

    /// Any day in the period shown: the period that holds it is read.
    private(set) var anchorDay: String
    private(set) var period: JournalPeriod?
    private(set) var hasLoaded = false
    private(set) var loadFailedText: String?
    private(set) var isBusy = false
    var problemText: String?
    var doneText: String?

    init(connection: Connection, periodKind: JournalPeriodKind, anchorDay: String) {
        self.connection = connection
        self.periodKind = periodKind
        self.anchorDay = anchorDay
    }

    var isPending: Bool { period?.isPending ?? false }

    /// What the period's words and narrative are changed by: the period that
    /// holds the anchor day.
    var target: JournalTarget { .period(periodKind, containing: anchorDay) }

    func show(anchorDay newAnchorDay: String) async {
        guard newAnchorDay != anchorDay else { return }
        anchorDay = newAnchorDay
        period = nil
        hasLoaded = false
        loadFailedText = nil
        await load()
    }

    func load(isQuiet: Bool = false) async {
        let requestedDay = anchorDay
        defer { if requestedDay == anchorDay { hasLoaded = true } }
        do {
            let read = try await connection.journalPeriod(periodKind, containing: requestedDay)
            guard requestedDay == anchorDay else { return }
            period = read
            loadFailedText = nil
        } catch {
            guard !Task.isCancelled, requestedDay == anchorDay, !isQuiet else { return }
            if period == nil { loadFailedText = error.loadProblemText } else { problemText = error.loadProblemText }
        }
    }

    func readWhilePending() async {
        var attemptCount = 0
        while JournalPolling.shouldRead(isPending: isPending, attemptCount: attemptCount) {
            do { try await Task.sleep(for: JournalPolling.interval) } catch { return }
            attemptCount += 1
            await load(isQuiet: true)
        }
    }

    @discardableResult
    private func change(orSay failureText: String, _ operation: () async throws -> Void) async -> Bool {
        isBusy = true
        defer { isBusy = false }
        do {
            try await operation()
        } catch {
            problemText = error.refusalReason(orSay: failureText).asReasonSentence
            return false
        }
        await load(isQuiet: true)
        return true
    }

    func writeAgain() async {
        await change(orSay: String(localized: "Could not ask for this to be written again.")) {
            try await connection.writeJournalNarrative(for: target)
        }
    }

    func removeCorrection(at index: Int) async {
        await change(orSay: String(localized: "Could not remove that correction.")) {
            try await connection.removeJournalCorrection(for: target, correctionIndex: index)
        }
    }
}

/// JournalSettingsModel is what the person decided about their journal.
@MainActor
@Observable
final class JournalSettingsModel {
    let connection: Connection

    private(set) var settings: JournalSettings?
    private(set) var loadFailedText: String?
    private(set) var isBusy = false
    var problemText: String?
    var doneText: String?

    init(connection: Connection) {
        self.connection = connection
    }

    func load() async {
        do {
            settings = try await connection.journalSettings()
            loadFailedText = nil
        } catch {
            if Task.isCancelled { return }
            if settings == nil { loadFailedText = error.loadProblemText } else { problemText = error.loadProblemText }
        }
    }

    func setWeather(isOn: Bool) async {
        await save(orSay: String(localized: "Could not change the weather setting.")) {
            try await connection.updateJournalSettings(isWeatherOn: isOn)
        }
    }

    func setEveningQuestion(isOn: Bool) async {
        await save(orSay: String(localized: "Could not change the evening question.")) {
            try await connection.updateJournalSettings(isEveningQuestionOn: isOn)
        }
    }

    func setEveningQuestionTime(_ time: String) async {
        await save(orSay: String(localized: "Could not change the time of the evening question.")) {
            try await connection.updateJournalSettings(eveningQuestionTime: time)
        }
    }

    private func save(orSay failureText: String, _ operation: () async throws -> JournalSettings) async {
        isBusy = true
        defer { isBusy = false }
        do {
            settings = try await operation()
        } catch {
            problemText = error.refusalReason(orSay: failureText).asReasonSentence
            // What the server holds is what is shown, so a refused change
            // puts the switch back.
            await load()
        }
    }

    /// Erases the whole journal; whether it did.
    func erase() async -> Bool {
        isBusy = true
        defer { isBusy = false }
        do {
            let erased = try await connection.eraseJournal()
            doneText = Self.erasedText(erased.dayCount)
            await load()
            return true
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "Could not erase the journal.")).asReasonSentence
            return false
        }
    }

    /// "120 days erased."
    static func erasedText(_ dayCount: Int) -> String {
        String(localized: "\(dayCount) days erased.")
    }
}
