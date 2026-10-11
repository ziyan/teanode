import Foundation

// The month view's arithmetic: which days a month draws in which week, the
// week numbers, which events fall on each day, how many fit as chips, the
// color a calendar is painted, and the Chinese lunar date under a day.

/// CalendarMonth is one month of one year.
public struct CalendarMonth: Hashable, Comparable, Identifiable, Sendable {
    public let year: Int
    /// 1 to 12.
    public let month: Int

    public init(year: Int, month: Int) {
        // Normalized, so month 13 is January of the next year.
        let monthIndex = year * 12 + (month - 1)
        self.year = Int((Double(monthIndex) / 12).rounded(.down))
        self.month = monthIndex - self.year * 12 + 1
    }

    /// The month a moment falls in, in that calendar's time zone.
    public init(containing date: Date, in calendar: Calendar) {
        let parts = calendar.dateComponents([.year, .month], from: date)
        self.init(year: parts.year ?? 1970, month: parts.month ?? 1)
    }

    /// Months counted from year zero, so neighbors differ by one.
    public var id: Int { year * 12 + (month - 1) }

    public func adding(months monthCount: Int) -> CalendarMonth {
        CalendarMonth(year: year, month: month + monthCount)
    }

    /// Local midnight on the first day.
    public func firstMoment(in calendar: Calendar) -> Date? {
        calendar.date(from: DateComponents(year: year, month: month, day: 1))
    }

    /// The window the server is asked for: the first moment of the month to
    /// the first moment of the next.
    public func window(in calendar: Calendar) -> (from: Date, until: Date)? {
        guard let from = firstMoment(in: calendar), let until = adding(months: 1).firstMoment(in: calendar) else { return nil }
        return (from, until)
    }

    public static func < (left: CalendarMonth, right: CalendarMonth) -> Bool { left.id < right.id }
}

/// CalendarDay is one day of one month, without a time or a zone.
public struct CalendarDay: Hashable, Comparable, Sendable {
    public let year: Int
    public let month: Int
    public let day: Int

    public init(year: Int, month: Int, day: Int) {
        self.year = year
        self.month = month
        self.day = day
    }

    /// The day a moment falls on, in that calendar's time zone.
    public init(containing date: Date, in calendar: Calendar) {
        let parts = calendar.dateComponents([.year, .month, .day], from: date)
        self.init(year: parts.year ?? 1970, month: parts.month ?? 1, day: parts.day ?? 1)
    }

    public init?(_ parts: DateComponents?) {
        guard let year = parts?.year, let month = parts?.month, let day = parts?.day else { return nil }
        self.init(year: year, month: month, day: day)
    }

    public var calendarMonth: CalendarMonth { CalendarMonth(year: year, month: month) }

    /// Local midnight at the start of the day.
    public func firstMoment(in calendar: Calendar) -> Date? {
        calendar.date(from: DateComponents(year: year, month: month, day: day))
    }

    public static func < (left: CalendarDay, right: CalendarDay) -> Bool {
        (left.year, left.month, left.day) < (right.year, right.month, right.day)
    }
}

/// MonthLayout is a month as rows of weeks: each row seven places from the
/// calendar's first weekday, the places outside the month left empty, and
/// the week's number.
public struct MonthLayout: Hashable, Sendable {
    public struct Week: Hashable, Sendable {
        /// The week of the year, by the calendar's own rule for the first
        /// week (ISO 8601's in much of Europe, January 1's week in the US).
        public let weekNumber: Int
        /// Seven places; nil where the day belongs to another month.
        public let days: [CalendarDay?]
    }

    public let month: CalendarMonth
    public let weeks: [Week]
    /// The place, 0 to 6, of the month's first day in its first week.
    public let firstDayColumn: Int

    public init(month: CalendarMonth, calendar: Calendar) {
        self.month = month
        guard let first = month.firstMoment(in: calendar),
            let dayCount = calendar.range(of: .day, in: .month, for: first)?.count
        else {
            weeks = []
            firstDayColumn = 0
            return
        }
        let weekday = calendar.component(.weekday, from: first)
        let leading = (weekday - calendar.firstWeekday + 7) % 7
        firstDayColumn = leading
        var places: [CalendarDay?] = Array(repeating: nil, count: leading)
        places += (1...dayCount).map { CalendarDay(year: month.year, month: month.month, day: $0) }
        places += Array(repeating: nil, count: (7 - places.count % 7) % 7)
        weeks = stride(from: 0, to: places.count, by: 7).map { start in
            let days = Array(places[start..<(start + 7)])
            // Every day of a row is in the same week, since rows start on
            // the calendar's first weekday; any day of the month in it will do.
            let inMonth = days.compactMap { $0 }.first
            let moment = inMonth.flatMap { $0.firstMoment(in: calendar) } ?? first
            return Week(weekNumber: calendar.component(.weekOfYear, from: moment), days: days)
        }
    }
}

extension CalendarDates {
    /// The first and last day an event is on in this time zone. An all-day
    /// event covers its own days, read from UTC parts; a timed one runs from
    /// its start's day to its end's, and an end at midnight belongs to the
    /// day before, as nothing of the event happens on the next.
    public static func dayRange(of event: CalendarEvent, in calendar: Calendar) -> ClosedRange<CalendarDay>? {
        if event.allDay {
            guard let first = CalendarDay(day(ofAllDay: event.startsAt)) else { return nil }
            let last = CalendarDay(lastDay(startsAt: event.startsAt, endsAt: event.endsAt)) ?? first
            return first...max(first, last)
        }
        guard let start = ServerTime.date(event.startsAt) else { return nil }
        let first = CalendarDay(containing: start, in: calendar)
        guard let end = ServerTime.date(event.endsAt), end > start else { return first...first }
        let last = CalendarDay(containing: end.addingTimeInterval(-1), in: calendar)
        return first...max(first, last)
    }

    /// The events on each day of a month, every day an event covers: all-day
    /// ones first, then by when they start. Days of other months are left out,
    /// so a month read again replaces only its own days.
    public static func eventsByDay(_ events: [CalendarEvent], in month: CalendarMonth, calendar: Calendar) -> [CalendarDay: [CalendarEvent]] {
        guard let first = month.firstMoment(in: calendar),
            let dayCount = calendar.range(of: .day, in: .month, for: first)?.count
        else { return [:] }
        let monthStart = CalendarDay(year: month.year, month: month.month, day: 1)
        let monthEnd = CalendarDay(year: month.year, month: month.month, day: dayCount)
        var found: [CalendarDay: [CalendarEvent]] = [:]
        for event in events {
            guard let range = dayRange(of: event, in: calendar), range.upperBound >= monthStart, range.lowerBound <= monthEnd else { continue }
            let firstDay = range.lowerBound < monthStart ? 1 : range.lowerBound.day
            let lastDay = range.upperBound > monthEnd ? dayCount : range.upperBound.day
            for dayOfMonth in firstDay...lastDay {
                found[CalendarDay(year: month.year, month: month.month, day: dayOfMonth), default: []].append(event)
            }
        }
        return found.mapValues { $0.sorted(by: isShownBefore) }
    }

    /// The order a day's events are shown in: all-day first, then by start,
    /// then by title.
    public static func isShownBefore(_ left: CalendarEvent, _ right: CalendarEvent) -> Bool {
        if left.allDay != right.allDay { return left.allDay }
        let leftStart = ServerTime.date(left.startsAt) ?? .distantPast
        let rightStart = ServerTime.date(right.startsAt) ?? .distantPast
        if leftStart != rightStart { return leftStart < rightStart }
        return (left.summary ?? "") < (right.summary ?? "")
    }
}

/// EventChipSplit is how a day cell spends its lines: as many chips as fit,
/// and when there are more events than lines, the last line says "+N".
public struct EventChipSplit: Hashable, Sendable {
    public let chipCount: Int
    public let moreCount: Int

    /// `lineCount` is how many lines the cell has for chips and the "+N".
    public init(eventCount: Int, lineCount: Int) {
        let lineCount = max(1, lineCount)
        if eventCount <= lineCount {
            chipCount = max(0, eventCount)
            moreCount = 0
        } else {
            chipCount = lineCount - 1
            moreCount = eventCount - chipCount
        }
    }
}

/// CalendarColorComponents is a color as red, green and blue from 0 to 1.
public struct CalendarColorComponents: Hashable, Sendable {
    public let red: Double
    public let green: Double
    public let blue: Double

    /// Reads "#rrggbb" or "#rgb", as the server keeps a calendar's colour;
    /// nil for anything else.
    public init?(hexText: String?) {
        guard var text = hexText?.trimmingCharacters(in: .whitespaces), !text.isEmpty else { return nil }
        if text.hasPrefix("#") { text.removeFirst() }
        if text.count == 3 { text = text.map { "\($0)\($0)" }.joined() }
        guard text.count == 6 || text.count == 8, let packed = UInt64(text.prefix(6), radix: 16) else { return nil }
        red = Double((packed >> 16) & 0xFF) / 255
        green = Double((packed >> 8) & 0xFF) / 255
        blue = Double(packed & 0xFF) / 255
    }
}

/// CalendarPalette picks a calendar's color from a small palette when the
/// server keeps none: the same calendar always gets the same one.
public enum CalendarPalette {
    /// A place in a palette of `paletteCount` colors, from the calendar's id.
    /// Swift's own hash changes on every launch, so the id is hashed by FNV-1a
    /// instead.
    public static func index(calendarId: String, paletteCount: Int) -> Int {
        guard paletteCount > 0 else { return 0 }
        var hash: UInt64 = 0xcbf2_9ce4_8422_2325
        for byte in calendarId.utf8 {
            hash ^= UInt64(byte)
            hash = hash &* 0x0000_0100_0000_01b3
        }
        return Int(hash % UInt64(paletteCount))
    }
}

/// LunarDates writes a day's date in the Chinese lunar calendar, as China's
/// calendars print it under the day: 初一 to 三十, and the month's name on
/// its first day.
public enum LunarDates {
    static let dayNames = [
        "初一", "初二", "初三", "初四", "初五", "初六", "初七", "初八", "初九", "初十",
        "十一", "十二", "十三", "十四", "十五", "十六", "十七", "十八", "十九", "二十",
        "廿一", "廿二", "廿三", "廿四", "廿五", "廿六", "廿七", "廿八", "廿九", "三十",
    ]
    static let monthNames = ["正月", "二月", "三月", "四月", "五月", "六月", "七月", "八月", "九月", "十月", "冬月", "腊月"]

    /// The name shown for a lunar day: its month's name on the first day of
    /// a month, with 闰 before a leap month, and the day's name otherwise.
    public static func name(month: Int, day: Int, isLeapMonth: Bool) -> String {
        if day == 1, (1...12).contains(month) {
            return (isLeapMonth ? "闰" : "") + monthNames[month - 1]
        }
        guard (1...30).contains(day) else { return "" }
        return dayNames[day - 1]
    }

    /// The lunar name of the day that starts at local midnight on `day`.
    public static func name(of day: CalendarDay, timeZone: TimeZone) -> String {
        var gregorian = Calendar(identifier: .gregorian)
        gregorian.timeZone = timeZone
        var chinese = Calendar(identifier: .chinese)
        chinese.timeZone = timeZone
        // Noon, so no zone's change of clocks moves it to another day.
        guard let moment = gregorian.date(from: DateComponents(year: day.year, month: day.month, day: day.day, hour: 12)) else { return "" }
        let parts = chinese.dateComponents([.month, .day, .isLeapMonth], from: moment)
        return name(month: parts.month ?? 0, day: parts.day ?? 0, isLeapMonth: parts.isLeapMonth ?? false)
    }
}
