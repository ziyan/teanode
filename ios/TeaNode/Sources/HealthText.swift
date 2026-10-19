import Foundation
import TeaNodeKit

// How the Health screens write numbers, units, times and days: the table at
// the top of internal/health/words.go, which the dashboard and the command line
// follow too, so that a number reads the same on every surface. The rules that
// need no language (decimals, whole numbers, kilometers, hours and minutes)
// are TeaNodeKit's HealthValueWords, which is tested; what this file adds is
// the words that are a language's: what is counted, and the hours and minutes.

enum HealthText {
    // MARK: Numbers and units

    /// A reading with its unit: "74 bpm", "72.4 kg", "97.5%", "8,412 steps".
    static func quantityText(_ value: Double, healthType: String, quantityUnit: String) -> String {
        text(of: HealthValueWords.parts(healthType: healthType, value: value, quantityUnit: quantityUnit))
    }

    /// A number and the word that follows it, joined the way the table says:
    /// a symbol touches the number, a word follows after a space.
    private static func text(of valueParts: HealthValueParts) -> String {
        switch valueParts.unit {
        case .none: return valueParts.numberText
        case .touching(let symbol): return valueParts.numberText + symbol
        case .spaced(let word): return valueParts.numberText + " " + word
        case .noun(let noun, let isSingular): return countText(valueParts.numberText, noun: noun, isSingular: isSingular)
        }
    }

    /// A count with what it counts: "8,412 steps", "1 flight".
    private static func countText(_ number: String, noun: HealthCountNoun, isSingular: Bool) -> String {
        switch (noun, isSingular) {
        case (.step, true): return String(localized: "health-count-step-single", defaultValue: "\(number) step")
        case (.step, false): return String(localized: "health-count-step-plural", defaultValue: "\(number) steps")
        case (.flight, true): return String(localized: "health-count-flight-single", defaultValue: "\(number) flight")
        case (.flight, false): return String(localized: "health-count-flight-plural", defaultValue: "\(number) flights")
        case (.push, true): return String(localized: "health-count-push-single", defaultValue: "\(number) push")
        case (.push, false): return String(localized: "health-count-push-plural", defaultValue: "\(number) pushes")
        case (.stroke, true): return String(localized: "health-count-stroke-single", defaultValue: "\(number) stroke")
        case (.stroke, false): return String(localized: "health-count-stroke-plural", defaultValue: "\(number) strokes")
        case (.fall, true): return String(localized: "health-count-fall-single", defaultValue: "\(number) fall")
        case (.fall, false): return String(localized: "health-count-fall-plural", defaultValue: "\(number) falls")
        case (.puff, true): return String(localized: "health-count-puff-single", defaultValue: "\(number) puff")
        case (.puff, false): return String(localized: "health-count-puff-plural", defaultValue: "\(number) puffs")
        case (.drink, true): return String(localized: "health-count-drink-single", defaultValue: "\(number) drink")
        case (.drink, false): return String(localized: "health-count-drink-plural", defaultValue: "\(number) drinks")
        }
    }

    /// A plain number with as many decimals as its size is worth.
    static func numberText(_ value: Double) -> String {
        HealthValueWords.numberText(value)
    }

    /// A length of time: "7 h 12 min", "35 min", "45 s".
    static func durationText(_ seconds: Double) -> String {
        switch HealthValueWords.durationParts(seconds: seconds) {
        case .seconds(let secondCount): return String(localized: "\(secondCount.formatted()) s")
        case .minutes(let minuteCount): return String(localized: "\(minuteCount.formatted()) min")
        case .hours(let hourCount): return String(localized: "\(hourCount.formatted()) h")
        case .hoursMinutes(let hourCount, let minuteCount):
            return String(localized: "\(hourCount.formatted()) h \(minuteCount.formatted()) min")
        }
    }

    /// The value a day or period stands for, by how the kind adds up: the
    /// sum of the readings, their average, the time they cover, or how many
    /// there were.
    static func summaryValue(
        sumValue: Double?, averageValue: Double?, durationSeconds: Double?, sampleCount: Int, summaryKind: HealthSummaryKind
    ) -> Double? {
        switch summaryKind {
        case .sum: return sumValue
        case .average: return averageValue
        case .duration: return durationSeconds
        case .count: return Double(sampleCount)
        }
    }

    /// A day's or period's value as it is read: a time as hours and
    /// minutes, a count as a whole number, a quantity with its unit.
    static func amountText(_ value: Double?, summaryKind: HealthSummaryKind, healthType: String, quantityUnit: String) -> String {
        guard let value else { return String(localized: "None") }
        switch summaryKind {
        case .duration: return durationText(value)
        case .count: return numberText(value)
        case .sum, .average: return quantityText(value, healthType: healthType, quantityUnit: quantityUnit)
        }
    }

    /// The latest day of a kind, as the list shows it.
    static func latestAmountText(of type: HealthTypeView) -> String? {
        guard let latest = type.latestDaySummary else { return nil }
        let summaryKind = type.summaryKindValue ?? .sum
        let value = summaryValue(
            sumValue: latest.sumValue, averageValue: latest.averageValue, durationSeconds: latest.durationSeconds,
            sampleCount: latest.sampleCount, summaryKind: summaryKind)
        return amountText(value, summaryKind: summaryKind, healthType: type.healthType, quantityUnit: type.quantityUnit)
    }

    /// A number on a chart's axis: short, with no unit (the headline has it).
    static func axisText(_ value: Double) -> String {
        let size = abs(value)
        if size >= 1_000_000 { return (value / 1_000_000).formatted(.number.precision(.fractionLength(0...1))) + "M" }
        if size >= 10_000 { return (value / 1000).formatted(.number.precision(.fractionLength(0...0))) + "k" }
        return numberText(value)
    }

    // MARK: Days and times

    /// The first moment of a day written "YYYY-MM-DD" (or longer, such as
    /// "2026-10-18T00:00:00Z", of which only the day counts).
    static func date(ofDay dayText: String) -> Date? {
        HealthDays.startOfDay(String(dayText.prefix(10)), calendar: Calendar.current)
    }

    /// A day as "Oct 18, 2026".
    static func mediumDayText(_ dayText: String) -> String {
        guard let date = date(ofDay: dayText) else { return dayText }
        return date.formatted(date: .abbreviated, time: .omitted)
    }

    /// A day as "Oct 18", for an axis.
    static func shortDayText(_ dayText: String) -> String {
        guard let date = date(ofDay: dayText) else { return dayText }
        return date.formatted(.dateTime.month(.abbreviated).day())
    }

    /// A month as "Oct", or "Oct 2026" when the chart reaches over years.
    static func monthText(_ dayText: String, isYearIncluded: Bool) -> String {
        guard let date = date(ofDay: dayText) else { return dayText }
        if isYearIncluded { return date.formatted(.dateTime.month(.abbreviated).year()) }
        return date.formatted(.dateTime.month(.abbreviated))
    }

    /// A time of day with its date: "Oct 18, 2026 at 7:42 AM".
    static func momentText(_ date: Date) -> String {
        date.formatted(date: .abbreviated, time: .shortened)
    }

    /// The whole days from one day to another, both counted, so a day to
    /// itself is one; at least one.
    static func dayCount(from firstDayText: String, to lastDayText: String) -> Int {
        guard let first = date(ofDay: firstDayText), let last = date(ofDay: lastDayText) else { return 1 }
        let days = Calendar.current.dateComponents([.day], from: first, to: last).day ?? 0
        return max(1, days + 1)
    }

    /// Every day from the first to the last written "YYYY-MM-DD", at most
    /// `dayCountMost` of them.
    static func days(from firstDayText: String, to lastDayText: String, dayCountMost: Int = 400) -> [String] {
        guard let first = date(ofDay: firstDayText), let last = date(ofDay: lastDayText), first <= last else { return [] }
        let calendar = Calendar.current
        var result: [String] = []
        var day = first
        while day <= last, result.count < dayCountMost {
            result.append(HealthDays.text(of: day, calendar: calendar))
            guard let next = calendar.date(byAdding: .day, value: 1, to: day) else { break }
            day = next
        }
        return result
    }

    /// The day written "YYYY-MM-DD" a number of days from another.
    static func day(_ dayText: String, adding dayCount: Int) -> String {
        guard let date = date(ofDay: dayText), let moved = Calendar.current.date(byAdding: .day, value: dayCount, to: date) else { return dayText }
        return HealthDays.text(of: moved, calendar: Calendar.current)
    }

    // MARK: Readings

    /// A number in a sample's metadata (a workout's distance or energy).
    static func metadataNumber(_ value: JSONValue?) -> Double? {
        guard let value else { return nil }
        if case .number(let number) = value { return number }
        return nil
    }

    /// What one reading says: "72 bpm", "Running, 42 min", "Deep, 1 h 20 min".
    /// A reading that covers no time (a headache, a notification, an entry of
    /// State of Mind) says the kind's name, as the dashboard does.
    static func readingText(of sample: HealthSampleView, type: HealthTypeView) -> String {
        let durationSeconds = max(0, (sample.endedDate ?? sample.startedDate ?? Date()).timeIntervalSince(sample.startedDate ?? Date()))
        if let quantityValue = sample.quantityValue {
            let sentUnit = sample.quantityUnit.isEmpty ? type.quantityUnit : sample.quantityUnit
            return quantityText(quantityValue, healthType: type.healthType, quantityUnit: sentUnit)
        }
        if type.valueKindValue == .workout {
            return HealthWords.workoutName(sample.workoutActivityType) + ", " + durationText(durationSeconds)
        }
        if type.healthType == "sleepAnalysis", let stage = HealthWords.stage(ofCategoryValue: sample.categoryValue) {
            return HealthWords.stageName(stage) + ", " + durationText(durationSeconds)
        }
        if durationSeconds < 1 { return HealthWords.typeName(healthType: type.healthType, serverName: type.typeName) }
        return durationText(durationSeconds)
    }
}
