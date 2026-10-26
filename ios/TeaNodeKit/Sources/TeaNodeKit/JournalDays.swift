import Foundation

// The arithmetic on the journal's days, weeks, months and years. A journal day
// is a calendar day written YYYY-MM-DD, the person's own, so it is worked on as
// a date and never as an instant: every day here is read at noon in a Gregorian
// calendar, which is on that day in every time zone. The screens put the day
// into words for the person's calendar and language.

/// JournalMoodLevel is how a day was, as the person chose it.
public enum JournalMoodLevel: Int, CaseIterable, Identifiable, Sendable {
    case awful = 1
    case low
    case okay
    case good
    case great

    public var id: Int { rawValue }

    /// The level a server's number stands for; nil for none or one out of range.
    public init?(moodLevel: Int?) {
        guard let moodLevel, let level = JournalMoodLevel(rawValue: moodLevel) else { return nil }
        self = level
    }
}

/// JournalWeatherKind is the one word the server gives a day's weather.
public enum JournalWeatherKind: String, CaseIterable, Sendable {
    case clear
    case partlyCloudy
    case cloudy
    case fog
    case drizzle
    case rain
    case snow
    case showers
    case thunderstorm
}

/// JournalPeriodKind is a stretch of days a period page covers.
public enum JournalPeriodKind: String, CaseIterable, Identifiable, Sendable {
    case week
    case month
    case year

    public var id: String { rawValue }
}

/// JournalDays is the arithmetic on journal days.
public enum JournalDays {
    /// A Gregorian calendar in the zone, so that a phone set to another
    /// calendar still cuts the days the way the server writes them.
    static func gregorianCalendar(in timeZone: TimeZone) -> Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = timeZone
        return calendar
    }

    /// Whether the text is a day written as the server takes it.
    public static func isDayText(_ text: String) -> Bool {
        HealthDays.isDayText(text)
    }

    /// The day at noon, in the zone; nil for text that is not a day.
    public static func date(_ text: String, timeZone: TimeZone = .current) -> Date? {
        guard isDayText(text), let year = Int(text.prefix(4)), let month = Int(text.dropFirst(5).prefix(2)), let day = Int(text.suffix(2)) else {
            return nil
        }
        let calendar = gregorianCalendar(in: timeZone)
        let noon = calendar.date(from: DateComponents(year: year, month: month, day: day, hour: 12))
        // A date that does not exist (the 31st of June) rolls over into the
        // next month in a calendar; it is not the day that was written.
        guard let noon, text == Self.text(noon, timeZone: timeZone) else { return nil }
        return noon
    }

    /// The day a moment is in, written YYYY-MM-DD.
    public static func text(_ date: Date, timeZone: TimeZone = .current) -> String {
        HealthDays.text(of: date, calendar: gregorianCalendar(in: timeZone))
    }

    /// The day a number of days after another (before, when negative).
    public static func adding(_ dayCount: Int, to text: String, timeZone: TimeZone = .current) -> String? {
        guard let start = Self.date(text, timeZone: timeZone), let moved = gregorianCalendar(in: timeZone).date(byAdding: .day, value: dayCount, to: start) else {
            return nil
        }
        return Self.text(moved, timeZone: timeZone)
    }

    /// The first day of the week a day is in. weekStartsOn is "sunday" or
    /// "monday", as the server says; anything else counts as Monday.
    public static func weekStart(of text: String, weekStartsOn: String, timeZone: TimeZone = .current) -> String? {
        guard let start = Self.date(text, timeZone: timeZone) else { return nil }
        // Sunday is 1 and Saturday 7 in a Gregorian calendar.
        let weekday = gregorianCalendar(in: timeZone).component(.weekday, from: start)
        let daysSinceStart = weekStartsOn == "sunday" ? weekday - 1 : (weekday + 5) % 7
        return adding(-daysSinceStart, to: text, timeZone: timeZone)
    }

    /// The first day of the month a day is in.
    public static func monthStart(of text: String) -> String? {
        guard isDayText(text) else { return nil }
        return String(text.prefix(7)) + "-01"
    }

    /// The first day of the year a day is in.
    public static func yearStart(of text: String) -> String? {
        guard isDayText(text) else { return nil }
        return String(text.prefix(4)) + "-01-01"
    }

    /// The first day of the period of a kind that contains a day.
    public static func periodStart(of text: String, periodKind: JournalPeriodKind, weekStartsOn: String, timeZone: TimeZone = .current) -> String? {
        switch periodKind {
        case .week: weekStart(of: text, weekStartsOn: weekStartsOn, timeZone: timeZone)
        case .month: monthStart(of: text)
        case .year: yearStart(of: text)
        }
    }

    /// The first day of the period after (or before, with a negative count)
    /// the one that starts on a day.
    public static func movedPeriodStart(from periodStart: String, periodKind: JournalPeriodKind, by count: Int, timeZone: TimeZone = .current) -> String? {
        guard let start = date(periodStart, timeZone: timeZone) else { return nil }
        let calendar = gregorianCalendar(in: timeZone)
        let moved: Date?
        switch periodKind {
        case .week: moved = calendar.date(byAdding: .day, value: 7 * count, to: start)
        case .month: moved = calendar.date(byAdding: .month, value: count, to: start)
        case .year: moved = calendar.date(byAdding: .year, value: count, to: start)
        }
        guard let moved else { return nil }
        return text(moved, timeZone: timeZone)
    }

    /// The days of a month in rows of seven, starting on the day the week
    /// starts on; a place before the month's first day or after its last is
    /// nil.
    public static func monthRows(monthStart: String, weekStartsOn: String, timeZone: TimeZone = .current) -> [[String?]] {
        guard let first = monthStart.isEmpty ? nil : Self.monthStart(of: monthStart), let firstDate = date(first, timeZone: timeZone),
            let dayCount = gregorianCalendar(in: timeZone).range(of: .day, in: .month, for: firstDate)?.count,
            let gridStart = weekStart(of: first, weekStartsOn: weekStartsOn, timeZone: timeZone), let gridStartDate = date(gridStart, timeZone: timeZone)
        else { return [] }
        let calendar = gregorianCalendar(in: timeZone)
        let leadingCount = calendar.dateComponents([.day], from: gridStartDate, to: firstDate).day ?? 0
        var places: [String?] = Array(repeating: nil, count: leadingCount)
        for offset in 0..<dayCount {
            places.append(adding(offset, to: first, timeZone: timeZone))
        }
        while places.count % 7 != 0 { places.append(nil) }
        return stride(from: 0, to: places.count, by: 7).map { Array(places[$0..<$0 + 7]) }
    }

    /// The weekday names' order for a row of a month's grid: the first
    /// week's days, so the screen can name the columns.
    public static func weekDays(startingOn weekStart: String, timeZone: TimeZone = .current) -> [String] {
        (0..<7).compactMap { adding($0, to: weekStart, timeZone: timeZone) }
    }
}

/// JournalWeekGroup is the days of the timeline that fall in one week.
public struct JournalWeekGroup: Identifiable, Hashable, Sendable {
    /// The first day of the week.
    public let weekStart: String
    /// The week's days that have anything in them, newest first.
    public let days: [JournalDaySummary]

    public var id: String { weekStart }
}

extension JournalDays {
    /// The timeline's days, newest first, grouped by the week each is in.
    /// Days of one week stay together however the pages were cut.
    public static func weekGroups(of days: [JournalDaySummary], weekStartsOn: String, timeZone: TimeZone = .current) -> [JournalWeekGroup] {
        var groups: [JournalWeekGroup] = []
        for day in days {
            let start = weekStart(of: day.day, weekStartsOn: weekStartsOn, timeZone: timeZone) ?? day.day
            if let last = groups.last, last.weekStart == start {
                groups[groups.count - 1] = JournalWeekGroup(weekStart: start, days: last.days + [day])
            } else {
                groups.append(JournalWeekGroup(weekStart: start, days: [day]))
            }
        }
        return groups
    }
}

// MARK: Money

/// JournalMoney turns the server's minor units (cents) into an amount.
public enum JournalMoney {
    /// How many digits a currency has after its point.
    static func fractionDigits(currency: String) -> Int {
        let code = currency.uppercased()
        if ["JPY", "KRW", "VND", "CLP", "ISK", "UGX", "XAF", "XOF", "XPF", "PYG", "RWF"].contains(code) { return 0 }
        if ["BHD", "KWD", "OMR", "JOD", "TND", "IQD", "LYD"].contains(code) { return 3 }
        return 2
    }

    /// The amount a number of minor units stands for: 5210 cents is 52.10.
    public static func amount(minorUnits: Int64, currency: String) -> Decimal {
        var divisor = Decimal(1)
        for _ in 0..<fractionDigits(currency: currency) { divisor *= 10 }
        return Decimal(minorUnits) / divisor
    }
}

// MARK: Polling

/// JournalPolling is how long a day waits for what the server writes in the
/// background: the touched-up words and the narrative.
public enum JournalPolling {
    /// How often the day is read again while something is being written.
    public static let interval: Duration = .seconds(3)
    /// How long the screen keeps asking before it gives up.
    public static let limit: Duration = .seconds(120)
    /// How many reads that is.
    public static var attemptLimit: Int { Int(limit / interval) }

    /// Whether to read again: something is being written, and the screen has
    /// not yet asked as often as it will.
    public static func shouldRead(isPending: Bool, attemptCount: Int) -> Bool {
        isPending && attemptCount < attemptLimit
    }
}
