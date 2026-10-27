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

    /// Where a search hit opens: its day, or its week, month or year.
    static func of(hit: JournalSearchHit) -> JournalDestination {
        guard let periodKind = hit.kind else { return .day(hit.day) }
        return .period(periodKind, hit.day)
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

    /// A temperature in the units the person chose, from the server's Celsius.
    /// Automatic follows the phone's language and region.
    static func temperatureText(celsius: Double, measurementSystem: MeasurementSystem) -> String {
        let temperature = Measurement(value: celsius, unit: UnitTemperature.celsius)
        switch measurementSystem {
        case .automatic:
            return temperature.formatted(
                .measurement(width: .narrow, usage: .weather, numberFormatStyle: .number.precision(.fractionLength(0))))
        case .metric:
            return degreesText(temperature.value)
        case .us:
            return degreesText(temperature.converted(to: .fahrenheit).value)
        }
    }

    /// "58°", the number rounded to a whole degree.
    private static func degreesText(_ degrees: Double) -> String {
        degrees.formatted(.number.precision(.fractionLength(0))) + "°"
    }

    /// "58° / 46°, rain".
    static func weatherLine(_ weather: JournalWeather, measurementSystem: MeasurementSystem) -> String {
        let range =
            temperatureText(celsius: weather.highCelsius, measurementSystem: measurementSystem) + " / "
            + temperatureText(celsius: weather.lowCelsius, measurementSystem: measurementSystem)
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

    /// "Saturday", the title of a day's page, with the date under it
    /// (daySubtitle). The bar keeps a title centered only while it fits
    /// beside the day-before and day-after buttons, and a whole date such as
    /// "Wednesday, September 30" does not, so it would sit centered on some
    /// days and to the left on others.
    static func dayTitle(_ day: String) -> String {
        guard let date = JournalDays.date(day) else { return day }
        return date.formatted(.dateTime.weekday(.wide))
    }

    /// "October 24" under a day's title, with the year when it is not this
    /// year's.
    static func daySubtitle(_ day: String, today: String) -> String {
        guard let date = JournalDays.date(day) else { return "" }
        if day.prefix(4) == today.prefix(4) { return monthDayText(day) }
        return date.formatted(.dateTime.month(.wide).day().year())
    }

    /// The title of a period page: "Week", "October 2026" or "2026". A week's
    /// days go under it (periodSubtitle), for the same reason as a day's date.
    static func periodTitle(periodKind: JournalPeriodKind, periodStart: String) -> String {
        switch periodKind {
        case .week: String(localized: "Week")
        case .month: monthText(periodStart)
        case .year: yearText(periodStart)
        }
    }

    /// "Oct 19 to 25" under a week's title; nothing under a month's or a
    /// year's, whose titles say it all.
    static func periodSubtitle(periodKind: JournalPeriodKind, periodStart: String) -> String {
        guard periodKind == .week, let start = JournalDays.date(periodStart),
            let end = JournalDays.adding(6, to: periodStart).flatMap({ JournalDays.date($0) })
        else { return "" }
        return (start..<end).formatted(.interval.month(.abbreviated).day())
    }

    /// "October 19 to 25", the days a week covers.
    static func weekRangeText(periodStart: String, periodEnd: String) -> String {
        guard let start = JournalDays.date(periodStart), let end = JournalDays.date(periodEnd) else { return "" }
        return (start..<end).formatted(.interval.month(.wide).day())
    }

    // MARK: Shopping and search

    /// "Shopping at Corner Market", or "Shopping" for a trip with no store.
    static func shoppingTitle(storeName: String) -> String {
        let trimmed = storeName.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? String(localized: "Shopping") : String(localized: "Shopping at \(trimmed)")
    }

    /// "7:10 PM to 7:50 PM" and, under it, "for Groceries and Household".
    static func shoppingDetail(_ trip: JournalShoppingTrip) -> String {
        var lines: [String] = []
        let times = timeRangeText(from: trip.startedAt, until: trip.finishedAt)
        if !times.isEmpty { lines.append(times) }
        if !trip.listNames.isEmpty {
            let listText = trip.listNames.formatted(.list(type: .and))
            lines.append(String(localized: "for \(listText)"))
        }
        return lines.joined(separator: "\n")
    }

    /// The title of a search hit: its day, or its week, month or year.
    static func searchHitTitle(_ hit: JournalSearchHit) -> String {
        guard let periodKind = hit.kind else { return longDayText(hit.day) }
        switch periodKind {
        case .week: return weekTitle(weekStart: hit.day)
        case .month: return monthText(hit.day)
        case .year: return yearText(hit.day)
        }
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

    /// "3 projects", the way a day or a period counts its work.
    static func projectCountText(_ projectCount: Int) -> String {
        String(localized: "\(projectCount) projects")
    }

    /// "6 h 10 min, 3 projects": the time spent coding and how many projects
    /// it was spread over. Never the number of sessions, which is how often
    /// an assistant was started and says nothing about the work.
    static func workText(minutes: Int, projectCount: Int) -> String {
        var parts: [String] = []
        if minutes > 0 { parts.append(durationText(minutes: minutes)) }
        if projectCount > 0 { parts.append(projectCountText(projectCount)) }
        return parts.joined(separator: ", ")
    }

    /// "2 h 10 min, 9:20 AM to 5:05 PM": how long a project took and when.
    static func projectTimeText(_ project: JournalWorkProject) -> String {
        var parts: [String] = []
        if project.activeMinutes > 0 { parts.append(durationText(minutes: project.activeMinutes)) }
        let range = timeRangeText(from: project.firstAt, until: project.lastAt == project.firstAt ? nil : project.lastAt)
        if !range.isEmpty { parts.append(range) }
        return parts.joined(separator: ", ")
    }

    /// "$52.10", from minor units.
    static func moneyText(minorUnits: Int64, currency: String) -> String {
        let amount = JournalMoney.amount(minorUnits: minorUnits, currency: currency)
        guard !currency.isEmpty else { return amount.formatted() }
        return amount.formatted(.currency(code: currency.uppercased()))
    }

    /// "3.4 mi" or "5.4 km", in the units the person chose. Automatic follows
    /// the phone's language and region.
    static func distanceText(meters: Double, measurementSystem: MeasurementSystem) -> String {
        let distance = Measurement(value: meters, unit: UnitLength.meters)
        switch measurementSystem {
        case .automatic:
            return distance.formatted(
                .measurement(width: .abbreviated, usage: .road, numberFormatStyle: .number.precision(.fractionLength(0...1))))
        case .metric:
            // Under a kilometer a walk is said in meters, as the phone does.
            let shown = meters < 1000 ? distance : distance.converted(to: .kilometers)
            return shown.formatted(
                .measurement(width: .abbreviated, usage: .asProvided, numberFormatStyle: .number.precision(.fractionLength(0...1))))
        case .us:
            return distance.converted(to: .miles).formatted(
                .measurement(width: .abbreviated, usage: .asProvided, numberFormatStyle: .number.precision(.fractionLength(0...1))))
        }
    }

    static func countText(_ count: Int) -> String {
        count.formatted()
    }

    /// The short line a timeline row says under its headline: the high
    /// temperature, then the one number about the day that is most likely to
    /// mean something to the person.
    static func factsLine(of summary: JournalDaySummary, measurementSystem: MeasurementSystem) -> String {
        var parts: [String] = []
        if let weather = summary.weather {
            parts.append(temperatureText(celsius: weather.highCelsius, measurementSystem: measurementSystem))
        }
        if let facts = summary.facts {
            if facts.walkedMeters >= 2000 {
                parts.append(String(localized: "\(distanceText(meters: facts.walkedMeters, measurementSystem: measurementSystem)) walked"))
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
