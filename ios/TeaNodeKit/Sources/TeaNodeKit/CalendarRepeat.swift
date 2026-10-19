import Foundation

// How an event repeats, read from and written as an RFC 5545 recurrence rule
// (the RRULE text without its property name, such as
// "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE,FR;UNTIL=20261231T235959Z").
//
// The server takes any rule as text and expands it, so the editor only has
// to build and read the parts a person can choose. The dashboard has the same
// logic in web/src/pages/calendarRepeat.ts, tested against the same table of
// rules; change them together.

/// CalendarRepeat turns a recurrence rule into the parts the event editor
/// shows, and back.
public enum CalendarRepeat {
    /// How often it comes round.
    public enum Frequency: String, CaseIterable, Hashable, Sendable {
        case daily, weekly, monthly, yearly

        fileprivate var ruleName: String { rawValue.uppercased() }
    }

    /// The last day of a repeat that ends on a date, or how many times it
    /// happens, or neither.
    public enum End: Hashable, Sendable {
        case never
        /// The last day it may happen on, in the event's own zone.
        case onDate(CalendarDay)
        case afterCount(occurrenceCount: Int)
    }

    /// What a monthly repeat counts by.
    public enum Monthly: Hashable, Sendable {
        case dayOfMonth(Int)
        /// The second Tuesday: the ordinal is 1 to 5, or -1 for the last;
        /// the weekday is 1 (Monday) to 7 (Sunday).
        case ordinalWeekday(ordinal: Int, weekday: Int)
    }

    /// Parts is what the editor shows of a rule.
    public struct Parts: Hashable, Sendable {
        public var frequency: Frequency
        /// Every this many days, weeks, months or years; 1 or more.
        public var intervalCount: Int
        /// Weekly only: the days it happens on, 1 (Monday) to 7 (Sunday),
        /// in order.
        public var weekdays: [Int]
        /// Monthly only.
        public var monthly: Monthly
        public var end: End
        /// The rule has something the editor cannot show (BYSETPOS, BYMONTH,
        /// several days of a month, a time of day). It is kept as it is
        /// unless the person changes what the editor does show.
        public var hasUnshownParts: Bool

        public init(
            frequency: Frequency, intervalCount: Int = 1, weekdays: [Int], monthly: Monthly, end: End = .never, hasUnshownParts: Bool = false
        ) {
            self.frequency = frequency
            self.intervalCount = intervalCount
            self.weekdays = weekdays
            self.monthly = monthly
            self.end = end
            self.hasUnshownParts = hasUnshownParts
        }
    }

    /// Context is what a rule is read against: the event's own first day,
    /// the zone its days are counted in, and whether it is a whole-day event,
    /// which writes its end as a date and not as a moment.
    public struct Context: Hashable, Sendable {
        public let startDay: CalendarDay
        public let timeZone: TimeZone
        public let isAllDay: Bool

        public init(startDay: CalendarDay, timeZone: TimeZone, isAllDay: Bool) {
            self.startDay = startDay
            self.timeZone = timeZone
            self.isAllDay = isAllDay
        }
    }

    /// Preset is a repeat the Repeat list names; anything else is Custom.
    public enum Preset: CaseIterable, Hashable, Sendable {
        case never, daily, weekdays, weekly, monthly, yearly, custom

        /// The rule a preset writes; nil for Custom, which has no rule of
        /// its own.
        public var rule: String? {
            switch self {
            case .never: ""
            case .daily: "FREQ=DAILY"
            case .weekdays: "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"
            case .weekly: "FREQ=WEEKLY"
            case .monthly: "FREQ=MONTHLY"
            case .yearly: "FREQ=YEARLY"
            case .custom: nil
            }
        }

        /// The preset a rule is, whatever order another program wrote its
        /// parts in.
        public init(rule: String) {
            let normalized = CalendarRepeat.normalized(rule)
            self = Preset.allCases.first { $0.rule.map(CalendarRepeat.normalized) == normalized } ?? .custom
        }
    }

    private static let weekdayCodes = ["MO", "TU", "WE", "TH", "FR", "SA", "SU"]

    /// The ordinals a month can name a weekday by: the first to the fifth,
    /// and the last, which is -1 in a rule.
    public static let monthlyOrdinals = [1, 2, 3, 4, 5, -1]

    private static let knownKeys: Set<String> = ["FREQ", "INTERVAL", "BYDAY", "BYMONTHDAY", "COUNT", "UNTIL"]

    // MARK: Days

    /// The day of the week of a day, 1 for Monday to 7 for Sunday.
    public static func weekday(of day: CalendarDay) -> Int {
        guard let date = CalendarDates.utc.date(from: DateComponents(year: day.year, month: day.month, day: day.day)) else { return 1 }
        return ScheduleWeekday.dayNumber(fromCalendarWeekday: CalendarDates.utc.component(.weekday, from: date))
    }

    /// The ordinal of a day's weekday within its month: the second Tuesday
    /// is 2. The fifth, which not every month has, is the last, -1, since
    /// that is what somebody who started on one means.
    public static func ordinal(of day: CalendarDay) -> Int {
        let ordinal = (day.day + 6) / 7
        return ordinal == 5 ? -1 : ordinal
    }

    /// The same date a year on, for a repeat that has just been told to end
    /// on a date. February 29 becomes March 1.
    public static func dayOneYearAfter(_ day: CalendarDay) -> CalendarDay {
        guard let date = CalendarDates.utc.date(from: DateComponents(year: day.year, month: day.month, day: day.day)),
            let later = CalendarDates.utc.date(byAdding: .year, value: 1, to: date),
            let laterDay = CalendarDay(CalendarDates.utc.dateComponents([.year, .month, .day], from: later))
        else { return day }
        return laterDay
    }

    // MARK: Reading

    /// The parts of a repeat that has just been chosen, such as the weekly
    /// one Custom opens on.
    public static func defaultParts(frequency: Frequency, context: Context) -> Parts {
        Parts(
            frequency: frequency, weekdays: [weekday(of: context.startDay)], monthly: .dayOfMonth(context.startDay.day))
    }

    /// The parts a rule says, to show in the editor. Nil for an empty rule
    /// (it does not repeat) and for text that is not a rule it can read,
    /// which the caller keeps as it is.
    public static func parse(_ rule: String, context: Context) -> Parts? {
        let text = rule.trimmingCharacters(in: .whitespacesAndNewlines).uppercased()
        if text.isEmpty { return nil }
        var values: [String: String] = [:]
        var hasUnshownParts = false
        for entry in text.split(separator: ";", omittingEmptySubsequences: true) {
            guard let separator = entry.firstIndex(of: "="), separator != entry.startIndex else { return nil }
            let key = entry[..<separator].trimmingCharacters(in: .whitespaces)
            if values[key] != nil { return nil }
            values[key] = entry[entry.index(after: separator)...].trimmingCharacters(in: .whitespaces)
            if !knownKeys.contains(key) { hasUnshownParts = true }
        }
        let frequencies: [String: Frequency] = ["DAILY": .daily, "WEEKLY": .weekly, "MONTHLY": .monthly, "YEARLY": .yearly]
        guard let frequency = frequencies[values["FREQ"] ?? ""] else { return nil }

        var parts = defaultParts(frequency: frequency, context: context)
        if let intervalText = values["INTERVAL"] {
            guard let intervalCount = wholeNumber(intervalText), intervalCount >= 1 else { return nil }
            parts.intervalCount = intervalCount
        }

        var days: [(ordinalText: String, weekday: Int)] = []
        for entry in (values["BYDAY"] ?? "").split(separator: ",", omittingEmptySubsequences: true) {
            guard let day = dayEntry(String(entry)) else { return nil }
            days.append(day)
        }
        let monthDayText = values["BYMONTHDAY"]

        switch frequency {
        case .weekly:
            if !days.isEmpty {
                if days.contains(where: { !$0.ordinalText.isEmpty }) {
                    hasUnshownParts = true
                } else {
                    parts.weekdays = Array(Set(days.map(\.weekday))).sorted()
                }
            }
            if monthDayText != nil { hasUnshownParts = true }
        case .monthly:
            if !days.isEmpty && monthDayText != nil {
                hasUnshownParts = true
            } else if days.count == 1 {
                if let ordinal = Int(days[0].ordinalText), monthlyOrdinals.contains(ordinal) {
                    parts.monthly = .ordinalWeekday(ordinal: ordinal, weekday: days[0].weekday)
                } else {
                    hasUnshownParts = true
                }
            } else if days.count > 1 {
                hasUnshownParts = true
            } else if let monthDayText {
                if let dayOfMonth = wholeNumber(monthDayText), (1...31).contains(dayOfMonth), monthDayText.count <= 2 {
                    parts.monthly = .dayOfMonth(dayOfMonth)
                } else {
                    hasUnshownParts = true
                }
            }
        case .daily, .yearly:
            // A daily or yearly rule narrowed to some days is not a repeat
            // this editor can draw.
            if !days.isEmpty || monthDayText != nil { hasUnshownParts = true }
        }

        let countText = values["COUNT"]
        let untilText = values["UNTIL"]
        if let countText {
            guard let occurrenceCount = wholeNumber(countText), occurrenceCount >= 1 else { return nil }
            parts.end = .afterCount(occurrenceCount: occurrenceCount)
            // A rule may have one or the other; with both, the count is
            // shown and the rule is kept as it is.
            if untilText != nil { hasUnshownParts = true }
        } else if let untilText {
            guard let endDay = endDay(ofUntil: untilText, context: context) else { return nil }
            parts.end = .onDate(endDay)
        }

        parts.hasUnshownParts = hasUnshownParts
        return parts
    }

    private static func wholeNumber(_ text: String) -> Int? {
        guard !text.isEmpty, text.allSatisfy({ $0.isASCII && $0.isNumber }) else { return nil }
        return Int(text)
    }

    /// One BYDAY entry: an optional ordinal such as "2" or "-1", then a day.
    private static func dayEntry(_ entry: String) -> (ordinalText: String, weekday: Int)? {
        let code = String(entry.suffix(2))
        guard let codeIndex = weekdayCodes.firstIndex(of: code) else { return nil }
        let ordinalText = String(entry.dropLast(2))
        if !ordinalText.isEmpty {
            let digits = ordinalText.hasPrefix("+") || ordinalText.hasPrefix("-") ? String(ordinalText.dropFirst()) : ordinalText
            guard wholeNumber(digits) != nil, digits.count <= 2 else { return nil }
        }
        return (ordinalText.hasPrefix("+") ? String(ordinalText.dropFirst()) : ordinalText, codeIndex + 1)
    }

    // MARK: Writing

    /// The rule for the parts: the plainest one that means them. A part that
    /// is what the event's own start already says (a weekly repeat on the
    /// weekday it starts on, a monthly one on the day it starts on) is left
    /// out, so that "every week" is written as every week.
    public static func write(_ parts: Parts, context: Context) -> String {
        var pieces = ["FREQ=\(parts.frequency.ruleName)"]
        if parts.intervalCount > 1 { pieces.append("INTERVAL=\(parts.intervalCount)") }
        switch parts.frequency {
        case .weekly:
            let weekdays = Array(Set(parts.weekdays)).sorted()
            let isStartWeekdayOnly = weekdays == [weekday(of: context.startDay)]
            if !weekdays.isEmpty && !isStartWeekdayOnly {
                pieces.append("BYDAY=" + weekdays.map { weekdayCodes[$0 - 1] }.joined(separator: ","))
            }
        case .monthly:
            switch parts.monthly {
            case .ordinalWeekday(let ordinal, let weekday):
                pieces.append("BYDAY=\(ordinal)\(weekdayCodes[weekday - 1])")
            case .dayOfMonth(let dayOfMonth):
                if dayOfMonth != context.startDay.day { pieces.append("BYMONTHDAY=\(dayOfMonth)") }
            }
        case .daily, .yearly:
            break
        }
        switch parts.end {
        case .never:
            break
        case .afterCount(let occurrenceCount):
            pieces.append("COUNT=\(occurrenceCount)")
        case .onDate(let endDay):
            pieces.append("UNTIL=\(untilText(endDay: endDay, context: context))")
        }
        return pieces.joined(separator: ";")
    }

    /// The rule to keep after the editor was opened on `original`, showing
    /// `initial`, and left showing `edited`. Nothing changed keeps the rule
    /// exactly as it was, so that opening the editor and pressing Done never
    /// loses what another program set (a part the editor cannot show, or a
    /// rule it cannot read at all).
    public static func editedRule(original: String, initial: Parts, edited: Parts, context: Context) -> String {
        if initial == edited { return original.trimmingCharacters(in: .whitespacesAndNewlines) }
        var written = edited
        written.hasUnshownParts = false
        return write(written, context: context)
    }

    // MARK: Normalizing

    /// The rule with its parts in a fixed order and its days sorted, so that
    /// the same repeat written by another program in another order is still
    /// the same repeat.
    fileprivate static func normalized(_ rule: String) -> String {
        rule.trimmingCharacters(in: .whitespacesAndNewlines).uppercased()
            .split(separator: ";", omittingEmptySubsequences: true)
            .map { entry -> String in
                let trimmed = entry.trimmingCharacters(in: .whitespaces)
                guard let separator = trimmed.firstIndex(of: "=") else { return trimmed }
                let key = String(trimmed[..<separator])
                let value = String(trimmed[trimmed.index(after: separator)...])
                guard key == "BYDAY" else { return "\(key)=\(value)" }
                let days = value.split(separator: ",").map(String.init).sorted {
                    (weekdayCodes.firstIndex(of: $0) ?? -1) < (weekdayCodes.firstIndex(of: $1) ?? -1)
                }
                return "\(key)=\(days.joined(separator: ","))"
            }
            .sorted()
            .joined(separator: ";")
    }

    // MARK: The end

    private static func zeroPadded(_ number: Int, width: Int) -> String {
        let text = String(number)
        return String(repeating: "0", count: max(0, width - text.count)) + text
    }

    /// The last day as RFC 5545 asks: a date for a whole-day event, and for
    /// a timed one the end of that day in the event's zone, as a moment in
    /// UTC.
    private static func untilText(endDay: CalendarDay, context: Context) -> String {
        let dateText = zeroPadded(endDay.year, width: 4) + zeroPadded(endDay.month, width: 2) + zeroPadded(endDay.day, width: 2)
        if context.isAllDay { return dateText }
        var zoned = Calendar(identifier: .gregorian)
        zoned.timeZone = context.timeZone
        guard let moment = zoned.date(from: DateComponents(year: endDay.year, month: endDay.month, day: endDay.day, hour: 23, minute: 59, second: 59))
        else { return dateText }
        let parts = CalendarDates.utc.dateComponents([.year, .month, .day, .hour, .minute, .second], from: moment)
        return zeroPadded(parts.year ?? 1970, width: 4) + zeroPadded(parts.month ?? 1, width: 2) + zeroPadded(parts.day ?? 1, width: 2) + "T"
            + zeroPadded(parts.hour ?? 0, width: 2) + zeroPadded(parts.minute ?? 0, width: 2) + zeroPadded(parts.second ?? 0, width: 2) + "Z"
    }

    /// The day an UNTIL names, in the event's zone.
    private static func endDay(ofUntil until: String, context: Context) -> CalendarDay? {
        let characters = Array(until)
        guard characters.count == 8 || characters.count == 15 || characters.count == 16,
            let year = wholeNumber(String(characters[0..<4])), let month = wholeNumber(String(characters[4..<6])),
            let day = wholeNumber(String(characters[6..<8]))
        else { return nil }
        let dayOfUntil = CalendarDay(year: year, month: month, day: day)
        if characters.count == 8 { return dayOfUntil }
        guard characters[8] == "T", let hour = wholeNumber(String(characters[9..<11])), let minute = wholeNumber(String(characters[11..<13])),
            let second = wholeNumber(String(characters[13..<15]))
        else { return nil }
        let isUniversal = characters.count == 16
        if isUniversal && characters[15] != "Z" { return nil }
        // Without a Z the time is the event's own, and without a zone to move
        // it to a whole-day event keeps the date as written.
        if !isUniversal || context.isAllDay { return dayOfUntil }
        guard let moment = CalendarDates.utc.date(from: DateComponents(year: year, month: month, day: day, hour: hour, minute: minute, second: second))
        else { return nil }
        var zoned = Calendar(identifier: .gregorian)
        zoned.timeZone = context.timeZone
        return CalendarDay(containing: moment, in: zoned)
    }

    // MARK: The sentence

    /// Sentence is what a repeat says in words, in the pieces a screen
    /// puts into its own localized sentences: the day names and the date are
    /// the locale's already, and the screen only places them.
    public struct Sentence: Hashable, Sendable {
        public enum Cadence: Hashable, Sendable {
            case daily(intervalCount: Int)
            case weekly(intervalCount: Int, weekdayNamesText: String)
            case monthlyDay(intervalCount: Int, dayOfMonth: Int)
            case monthlyWeekday(intervalCount: Int, ordinal: Int, weekdayName: String)
            case yearly(intervalCount: Int)
        }

        public enum Ending: Hashable, Sendable {
            case never
            case onDate(dateText: String)
            case afterCount(occurrenceCount: Int)
        }

        public let cadence: Cadence
        public let ending: Ending
    }

    /// The sentence for the parts, with day names, the list of them and the
    /// end date from the locale's own formatters.
    public static func sentence(for parts: Parts, locale: Locale = .current) -> Sentence {
        let cadence: Sentence.Cadence
        switch parts.frequency {
        case .daily:
            cadence = .daily(intervalCount: parts.intervalCount)
        case .yearly:
            cadence = .yearly(intervalCount: parts.intervalCount)
        case .weekly:
            let names = Array(Set(parts.weekdays)).sorted().map { ScheduleWeekday.name($0, locale: locale) }
            cadence = .weekly(intervalCount: parts.intervalCount, weekdayNamesText: names.formatted(.list(type: .and).locale(locale)))
        case .monthly:
            switch parts.monthly {
            case .dayOfMonth(let dayOfMonth):
                cadence = .monthlyDay(intervalCount: parts.intervalCount, dayOfMonth: dayOfMonth)
            case .ordinalWeekday(let ordinal, let weekday):
                cadence = .monthlyWeekday(intervalCount: parts.intervalCount, ordinal: ordinal, weekdayName: ScheduleWeekday.name(weekday, locale: locale))
            }
        }
        let ending: Sentence.Ending
        switch parts.end {
        case .never:
            ending = .never
        case .onDate(let endDay):
            ending = .onDate(dateText: dateText(endDay, locale: locale))
        case .afterCount(let occurrenceCount):
            ending = .afterCount(occurrenceCount: occurrenceCount)
        }
        return Sentence(cadence: cadence, ending: ending)
    }

    /// A day as the locale writes a long date: "December 31, 2026".
    public static func dateText(_ day: CalendarDay, locale: Locale = .current) -> String {
        guard let date = CalendarDates.utc.date(from: DateComponents(year: day.year, month: day.month, day: day.day, hour: 12)) else { return "" }
        return date.formatted(Date.FormatStyle(date: .long, time: .omitted, locale: locale, calendar: CalendarDates.utc, timeZone: CalendarDates.utc.timeZone))
    }
}
