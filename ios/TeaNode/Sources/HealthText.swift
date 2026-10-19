import Foundation
import TeaNodeKit

// How the Health screens write numbers, units, times and days. The same
// rules as the dashboard's (web/src/pages/health/healthApi.ts), with two
// differences: a percentage is shown as the person reads it (HealthKit and
// the server hold 0.97, the screen says 97%), and a long distance is in
// kilometers.

enum HealthText {
    // MARK: Numbers and units

    /// How a unit HealthKit sends is written beside a number. A unit not
    /// listed is written as it came. An empty label is a plain number.
    private static let unitLabels: [String: String] = [
        "count": "", "count/min": "/min", "count/s": "/s", "kcal": "kcal", "m": "m", "km": "km", "mi": "mi", "cm": "cm", "kg": "kg",
        "g": "g", "mg": "mg", "mcg": "mcg", "lb": "lb", "L": "L", "mL": "mL", "L/min": "L/min", "degC": "°C", "degF": "°F",
        "mmHg": "mmHg", "mg/dL": "mg/dL", "s": "s", "min": "min", "hr": "h", "ms": "ms", "m/s": "m/s", "km/h": "km/h", "W": "W",
        "dBASPL": "dB", "ml/(kg*min)": "mL/kg/min", "mL/(kg*min)": "mL/kg/min", "S": "S", "IU": "IU", "appleEffortScore": "",
    ]

    /// The text that follows a number of this unit; empty for none.
    static func unitLabel(_ quantityUnit: String) -> String {
        unitLabels[quantityUnit] ?? quantityUnit
    }

    /// A number with as many decimals as it needs to be read at a glance:
    /// none from a hundred up, one from ten, two below.
    static func numberText(_ value: Double) -> String {
        let size = abs(value)
        let fractionDigitsMost = size >= 100 ? 0 : (size >= 10 ? 1 : 2)
        return value.formatted(.number.precision(.fractionLength(0...fractionDigitsMost)))
    }

    /// A reading with its unit: "72/min", "71.4 kg", "97%", "8,412".
    static func quantityText(_ value: Double, quantityUnit: String) -> String {
        if quantityUnit == "%" { return numberText(value * 100) + "%" }
        if quantityUnit == "m", abs(value) >= 1000 { return numberText(value / 1000) + " km" }
        let label = unitLabel(quantityUnit)
        let number = numberText(value)
        if label.isEmpty { return number }
        if label.hasPrefix("/") || label.hasPrefix("°") { return number + label }
        return number + " " + label
    }

    /// A length of time: "7h 30m", "45m", "30s".
    static func durationText(_ seconds: Double) -> String {
        let formatter = DateComponentsFormatter()
        formatter.unitsStyle = .abbreviated
        formatter.zeroFormattingBehavior = .dropAll
        if seconds < 60 {
            formatter.allowedUnits = [.second]
            return formatter.string(from: seconds.rounded()) ?? ""
        }
        formatter.allowedUnits = [.hour, .minute]
        return formatter.string(from: (seconds / 60).rounded() * 60) ?? ""
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
    static func amountText(_ value: Double?, summaryKind: HealthSummaryKind, quantityUnit: String) -> String {
        guard let value else { return String(localized: "None") }
        switch summaryKind {
        case .duration: return durationText(value)
        case .count: return numberText(value)
        case .sum, .average: return quantityText(value, quantityUnit: quantityUnit)
        }
    }

    /// The latest day of a kind, as the list shows it.
    static func latestAmountText(of type: HealthTypeView) -> String? {
        guard let latest = type.latestDaySummary else { return nil }
        let summaryKind = type.summaryKindValue ?? .sum
        let value = summaryValue(
            sumValue: latest.sumValue, averageValue: latest.averageValue, durationSeconds: latest.durationSeconds,
            sampleCount: latest.sampleCount, summaryKind: summaryKind)
        return amountText(value, summaryKind: summaryKind, quantityUnit: type.quantityUnit)
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
    static func monthText(_ dayText: String, includesYear: Bool) -> String {
        guard let date = date(ofDay: dayText) else { return dayText }
        if includesYear { return date.formatted(.dateTime.month(.abbreviated).year()) }
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

    /// What one reading says: "72/min", "Running, 42m", "Deep, 1h 20m".
    static func readingText(of sample: HealthSampleView, type: HealthTypeView) -> String {
        let durationSeconds = max(0, (sample.endedDate ?? sample.startedDate ?? Date()).timeIntervalSince(sample.startedDate ?? Date()))
        if let quantityValue = sample.quantityValue {
            return quantityText(quantityValue, quantityUnit: sample.quantityUnit.isEmpty ? type.quantityUnit : sample.quantityUnit)
        }
        if type.valueKindValue == .workout {
            return HealthWords.workoutName(sample.workoutActivityType) + ", " + durationText(durationSeconds)
        }
        if type.healthType == "sleepAnalysis", let stage = HealthWords.stage(ofCategoryValue: sample.categoryValue) {
            return HealthWords.stageName(stage) + ", " + durationText(durationSeconds)
        }
        return durationText(durationSeconds)
    }
}
