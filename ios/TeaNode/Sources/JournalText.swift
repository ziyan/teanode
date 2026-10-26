import SwiftUI
import TeaNodeKit

// The words, symbols and colors the journal's screens share. The days are the
// person's own calendar days, so a day is put into words with the phone's
// calendar and language, never as the instant it begins.

/// JournalZoom is how much of the journal one screen covers, as the menu
/// under the title offers it.
enum JournalZoom: String, CaseIterable, Identifiable {
    case days, weeks, months, years

    var id: String { rawValue }

    var title: String {
        switch self {
        case .days: String(localized: "Days")
        case .weeks: String(localized: "Weeks")
        case .months: String(localized: "Months")
        case .years: String(localized: "Years")
        }
    }

    /// The kind of period a zoom shows; nil for single days.
    var periodKind: JournalPeriodKind? {
        switch self {
        case .days: nil
        case .weeks: .week
        case .months: .month
        case .years: .year
        }
    }

    init(periodKind: JournalPeriodKind) {
        switch periodKind {
        case .week: self = .weeks
        case .month: self = .months
        case .year: self = .years
        }
    }
}

/// JournalDestination is where the Journal's stack can be.
enum JournalDestination: Hashable {
    case day(String)
    /// A week, month or year, by any day in it.
    case period(JournalPeriodKind, String)

    /// Where a zoom goes from a day: the day itself, or the period of that
    /// kind that holds it.
    static func zoomed(_ zoom: JournalZoom, onDay day: String) -> JournalDestination {
        if let periodKind = zoom.periodKind { return .period(periodKind, day) }
        return .day(day)
    }
}

enum JournalText {
    static var title: String { String(localized: "Journal") }

    // MARK: Mood

    static func moodName(_ level: JournalMoodLevel) -> String {
        switch level {
        case .awful: String(localized: "Awful")
        case .low: String(localized: "Low")
        case .okay: String(localized: "Okay")
        case .good: String(localized: "Good")
        case .great: String(localized: "Great")
        }
    }

    /// The color a mood is drawn in: cool for the low ones, warm green for the
    /// good ones. The system's own colors, so they hold in dark mode.
    static func moodColor(_ level: JournalMoodLevel) -> Color {
        switch level {
        case .awful: .indigo
        case .low: .blue
        case .okay: .gray
        case .good: .mint
        case .great: .green
        }
    }

    /// The color of a mood number the server sent as one of a month's days:
    /// nil for 0, a day with no mood.
    static func dayMoodColor(_ dayMood: Int) -> Color? {
        guard let level = JournalMoodLevel(moodLevel: dayMood) else { return nil }
        return moodColor(level)
    }

    // MARK: Weather

    static func weatherSymbol(_ kind: JournalWeatherKind?) -> String {
        switch kind {
        case .clear: "sun.max"
        case .partlyCloudy: "cloud.sun"
        case .cloudy: "cloud"
        case .fog: "cloud.fog"
        case .drizzle: "cloud.drizzle"
        case .rain: "cloud.rain"
        case .snow: "cloud.snow"
        case .showers: "cloud.heavyrain"
        case .thunderstorm: "cloud.bolt.rain"
        case nil: "cloud.sun"
        }
    }

    static func weatherName(_ kind: JournalWeatherKind?) -> String {
        switch kind {
        case .clear: String(localized: "clear")
        case .partlyCloudy: String(localized: "partly cloudy")
        case .cloudy: String(localized: "cloudy")
        case .fog: String(localized: "fog")
        case .drizzle: String(localized: "drizzle")
        case .rain: String(localized: "rain")
        case .snow: String(localized: "snow")
        case .showers: String(localized: "showers")
        case .thunderstorm: String(localized: "thunderstorms")
        case nil: ""
        }
    }

    /// A temperature in the person's unit, from the server's Celsius.
    static func temperatureText(celsius: Double) -> String {
        Measurement(value: celsius, unit: UnitTemperature.celsius)
            .formatted(.measurement(width: .narrow, usage: .weather, numberFormatStyle: .number.precision(.fractionLength(0))))
    }

    /// "58° / 46°, rain".
    static func weatherLine(_ weather: JournalWeather) -> String {
        let range = temperatureText(celsius: weather.highCelsius) + " / " + temperatureText(celsius: weather.lowCelsius)
        let name = weatherName(weather.kind)
        return name.isEmpty ? range : range + ", " + name
    }

    // MARK: Records

    /// The symbol that stands for a kind of record.
    static func symbol(forRecordKind recordKind: String) -> String {
        switch recordKind {
        case "mail": "envelope"
        case "photo": "photo"
        case "place_visit": "mappin.and.ellipse"
        case "agent_document": "doc.text"
        case "calendar_event", "calendar_occurrence": "calendar"
        case "recording", "recording_passage": "record.circle"
        case "chat_message", "conversation_message": "bubble.left"
        case "finance_transaction", "finance_receipt": "creditcard"
        case "health_day": "heart"
        case "shopping_trip": "cart"
        case "note": "note.text"
        case "reminder": "checklist"
        case "idea": "lightbulb"
        case "goal": "target"
        case "contact": "person"
        default: "circle"
        }
    }

    // MARK: Days

    /// "Thursday, October 22".
    static func longDayText(_ day: String) -> String {
        guard let date = JournalDays.date(day) else { return day }
        return date.formatted(.dateTime.weekday(.wide).month(.wide).day())
    }

    /// "Thu".
    static func weekdayText(_ day: String) -> String {
        guard let date = JournalDays.date(day) else { return "" }
        return date.formatted(.dateTime.weekday(.abbreviated))
    }

    /// "T", the first letter of a weekday, for the head of a column.
    static func weekdayInitialText(_ day: String) -> String {
        guard let date = JournalDays.date(day) else { return "" }
        return date.formatted(.dateTime.weekday(.narrow))
    }

    /// "March 1 to May 31".
    static func dayRangeText(from firstDay: String, until lastDay: String) -> String {
        guard let first = JournalDays.date(firstDay), let last = JournalDays.date(lastDay), first < last else { return monthDayText(firstDay) }
        return (first..<last).formatted(.interval.month(.wide).day())
    }

    /// "22".
    static func dayNumberText(_ day: String) -> String {
        guard let date = JournalDays.date(day) else { return "" }
        return date.formatted(.dateTime.day())
    }

    /// "October 22".
    static func monthDayText(_ day: String) -> String {
        guard let date = JournalDays.date(day) else { return day }
        return date.formatted(.dateTime.month(.wide).day())
    }

    /// "October 2026".
    static func monthText(_ day: String) -> String {
        guard let date = JournalDays.date(day) else { return day }
        return date.formatted(.dateTime.month(.wide).year())
    }

    /// "Oct".
    static func shortMonthText(_ day: String) -> String {
        guard let date = JournalDays.date(day) else { return day }
        return date.formatted(.dateTime.month(.abbreviated))
    }

    /// "2026".
    static func yearText(_ day: String) -> String {
        guard let date = JournalDays.date(day) else { return day }
        return date.formatted(.dateTime.year())
    }

    /// "Week of October 19".
    static func weekTitle(weekStart: String) -> String {
        String(localized: "Week of \(monthDayText(weekStart))")
    }

    /// The title of a period page.
    static func periodTitle(periodKind: JournalPeriodKind, periodStart: String) -> String {
        switch periodKind {
        case .week: weekTitle(weekStart: periodStart)
        case .month: monthText(periodStart)
        case .year: yearText(periodStart)
        }
    }

    /// "October 19 to 25", the days a week covers.
    static func weekRangeText(periodStart: String, periodEnd: String) -> String {
        guard let start = JournalDays.date(periodStart), let end = JournalDays.date(periodEnd) else { return "" }
        return (start..<end).formatted(.interval.month(.wide).day())
    }

    // MARK: Times and amounts

    /// "9:05 AM", from the server's time.
    static func timeText(_ time: String?) -> String {
        guard let date = ServerTime.date(time) else { return "" }
        return date.formatted(date: .omitted, time: .shortened)
    }

    /// "9:05 AM to 5:20 PM", or just the first when there is no second.
    static func timeRangeText(from start: String?, until end: String?) -> String {
        let startText = timeText(start)
        let endText = timeText(end)
        if startText.isEmpty { return endText }
        if endText.isEmpty { return startText }
        return String(localized: "\(startText) to \(endText)")
    }

    /// "6 h 10 min".
    static func durationText(minutes: Int) -> String {
        Duration.seconds(minutes * 60).formatted(.units(allowed: [.hours, .minutes], width: .abbreviated))
    }

    static func durationText(milliseconds: Int64) -> String {
        durationText(minutes: Int(milliseconds / 60_000))
    }

    /// "$52.10", from minor units.
    static func moneyText(minorUnits: Int64, currency: String) -> String {
        let amount = JournalMoney.amount(minorUnits: minorUnits, currency: currency)
        guard !currency.isEmpty else { return amount.formatted() }
        return amount.formatted(.currency(code: currency.uppercased()))
    }

    /// "3.4 mi", in the person's unit.
    static func distanceText(meters: Double) -> String {
        Measurement(value: meters, unit: UnitLength.meters)
            .formatted(.measurement(width: .abbreviated, usage: .road, numberFormatStyle: .number.precision(.fractionLength(0...1))))
    }

    static func countText(_ count: Int) -> String {
        count.formatted()
    }

    /// The short line a timeline row says under its headline: the high
    /// temperature, then the one number about the day that is most likely to
    /// mean something to the person.
    static func factsLine(of summary: JournalDaySummary) -> String {
        var parts: [String] = []
        if let weather = summary.weather { parts.append(temperatureText(celsius: weather.highCelsius)) }
        if let facts = summary.facts {
            if facts.walkedMeters >= 2000 {
                parts.append(String(localized: "\(distanceText(meters: facts.walkedMeters)) walked"))
            } else if facts.photoCount > 0 {
                parts.append(String(localized: "\(facts.photoCount) photos"))
            } else if facts.spentMinorUnits > 0 {
                parts.append(moneyText(minorUnits: facts.spentMinorUnits, currency: facts.spentCurrency))
            } else if facts.sleepMinutes > 0 {
                parts.append(String(localized: "\(durationText(minutes: facts.sleepMinutes)) sleep"))
            } else if facts.stepCount > 0 {
                parts.append(String(localized: "\(facts.stepCount) steps"))
            }
        }
        return parts.joined(separator: ", ")
    }

    // MARK: Sleep

    static func sleepStageName(_ sleepStage: String) -> String {
        switch sleepStage {
        case "deep": String(localized: "Deep")
        case "core": String(localized: "Core")
        case "rem": String(localized: "REM")
        case "awake": String(localized: "Awake")
        case "inBed": String(localized: "In bed")
        default: sleepStage
        }
    }

    static func sleepStageColor(_ sleepStage: String) -> Color {
        switch sleepStage {
        case "deep": .indigo
        case "core": .blue
        case "rem": .cyan
        case "awake": .orange
        default: .gray
        }
    }
}
