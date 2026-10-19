import Foundation

// The day summaries the phone works out itself. A quantity's summary comes
// from HealthKit's statistics, which merge the sources (an iPhone and a Watch
// counting the same walk) the way the Health app does, so only the shape for
// them is here. A category (sleep, mindful minutes) and a workout have no
// such query, so they are worked out here from the samples' times: the union
// of the intervals, so two sources overlapping are counted once. Every
// function takes the Calendar to cut days with (a time zone in it) and never
// reads the current one.

/// HealthDays is the arithmetic on the phone's calendar days, written
/// "YYYY-MM-DD".
public enum HealthDays {
    /// The calendar day a time is in.
    public static func text(of date: Date, calendar: Calendar) -> String {
        let parts = calendar.dateComponents([.year, .month, .day], from: date)
        return String(format: "%04d-%02d-%02d", parts.year ?? 1970, parts.month ?? 1, parts.day ?? 1)
    }

    /// Whether the text is a day written as the server takes it.
    public static func isDayText(_ text: String) -> Bool {
        let characters = Array(text.utf8)
        guard characters.count == 10, characters[4] == UInt8(ascii: "-"), characters[7] == UInt8(ascii: "-") else { return false }
        let digits = characters.enumerated().filter { $0.offset != 4 && $0.offset != 7 }.map(\.element)
        guard digits.allSatisfy({ $0 >= UInt8(ascii: "0") && $0 <= UInt8(ascii: "9") }) else { return false }
        guard let month = Int(text.dropFirst(5).prefix(2)), let day = Int(text.suffix(2)) else { return false }
        return (1...12).contains(month) && (1...31).contains(day)
    }

    /// The moment a day begins in the calendar's time zone.
    public static func startOfDay(_ text: String, calendar: Calendar) -> Date? {
        guard isDayText(text), let year = Int(text.prefix(4)), let month = Int(text.dropFirst(5).prefix(2)), let day = Int(text.suffix(2)) else {
            return nil
        }
        var parts = DateComponents()
        parts.year = year
        parts.month = month
        parts.day = day
        parts.hour = 12
        // Noon is on the day in every zone; the start of that day is not
        // always midnight (some zones skip it when the clocks change).
        guard let noon = calendar.date(from: parts) else { return nil }
        return calendar.startOfDay(for: noon)
    }

    /// From the beginning of a day to the beginning of the next: 23 or 25
    /// hours when the clocks change.
    public static func interval(ofDay text: String, calendar: Calendar) -> DateInterval? {
        guard let start = startOfDay(text, calendar: calendar), let next = calendar.date(byAdding: .day, value: 1, to: start) else { return nil }
        return DateInterval(start: start, end: next)
    }

    /// The days a reading touches, earliest first. The end is not part of the
    /// reading: one that ends at midnight does not touch the day after. A
    /// reading that takes no time is on the day it is at.
    public static func days(from start: Date, to end: Date, calendar: Calendar) -> [String] {
        var result: [String] = []
        var dayStart = calendar.startOfDay(for: start)
        // A reading longer than ten years is a mistake in a date, not a stay.
        while result.count < 3660 {
            result.append(text(of: dayStart, calendar: calendar))
            guard let next = calendar.date(byAdding: .day, value: 1, to: dayStart), next < end else { break }
            dayStart = next
        }
        return result
    }
}

/// HealthIntervals is the union of time ranges.
public enum HealthIntervals {
    /// The ranges merged: sorted, and any that overlap or touch made one.
    public static func union(_ intervals: [DateInterval]) -> [DateInterval] {
        var merged: [DateInterval] = []
        for interval in intervals.sorted(by: { $0.start < $1.start }) where interval.duration > 0 {
            if let last = merged.last, interval.start <= last.end {
                merged[merged.count - 1] = DateInterval(start: last.start, end: max(last.end, interval.end))
            } else {
                merged.append(interval)
            }
        }
        return merged
    }

    /// How many seconds the union of the ranges covers.
    public static func coveredSeconds(_ intervals: [DateInterval]) -> Double { union(intervals).reduce(0) { $0 + $1.duration } }
}

/// HealthCategorySample is one category reading, as the day summaries need it.
public struct HealthCategorySample: Hashable, Sendable {
    public let healthSampleId: String
    public let startedAt: Date
    public let endedAt: Date
    /// HealthKit's raw value; for sleep, a HealthSleepStage.
    public let categoryValue: Int

    public init(healthSampleId: String = "", startedAt: Date, endedAt: Date, categoryValue: Int) {
        self.healthSampleId = healthSampleId
        self.startedAt = startedAt
        self.endedAt = endedAt
        self.categoryValue = categoryValue
    }

    /// From a sample about to be sent; nil if it holds no category value.
    public init?(_ sample: HealthSampleInput) {
        guard let categoryValue = sample.categoryValue else { return nil }
        self.init(healthSampleId: sample.healthSampleId, startedAt: sample.startedAt, endedAt: sample.endedAt, categoryValue: categoryValue)
    }
}

/// HealthWorkoutMetadata names where a workout's totals are in the metadata of
/// its sample.
public enum HealthWorkoutMetadata {
    public static let totalEnergyBurnedKilocalories = "totalEnergyBurnedKilocalories"
    public static let totalDistanceMeters = "totalDistanceMeters"
}

/// HealthWorkoutSample is one workout, as the day summaries need it.
public struct HealthWorkoutSample: Hashable, Sendable {
    public let healthSampleId: String
    public let startedAt: Date
    public let endedAt: Date
    public let totalEnergyKilocalories: Double?
    public let totalDistanceMeters: Double?

    public init(
        healthSampleId: String = "", startedAt: Date, endedAt: Date, totalEnergyKilocalories: Double? = nil, totalDistanceMeters: Double? = nil
    ) {
        self.healthSampleId = healthSampleId
        self.startedAt = startedAt
        self.endedAt = endedAt
        self.totalEnergyKilocalories = totalEnergyKilocalories
        self.totalDistanceMeters = totalDistanceMeters
    }

    /// From a "workout" sample about to be sent, reading the totals from its
    /// metadata; nil for any other type.
    public init?(_ sample: HealthSampleInput) {
        guard sample.healthType == "workout" else { return nil }
        self.init(
            healthSampleId: sample.healthSampleId, startedAt: sample.startedAt, endedAt: sample.endedAt,
            totalEnergyKilocalories: sample.sampleMetadata[HealthWorkoutMetadata.totalEnergyBurnedKilocalories]?.healthNumber,
            totalDistanceMeters: sample.sampleMetadata[HealthWorkoutMetadata.totalDistanceMeters]?.healthNumber)
    }
}

/// HealthQuantityStatistics is what HealthKit's statistics answer for one
/// quantity on one day.
public struct HealthQuantityStatistics: Hashable, Sendable {
    public let sumValue: Double?
    public let averageValue: Double?
    public let minimumValue: Double?
    public let maximumValue: Double?
    public let sampleCount: Int

    public init(sumValue: Double? = nil, averageValue: Double? = nil, minimumValue: Double? = nil, maximumValue: Double? = nil, sampleCount: Int = 0) {
        self.sumValue = sumValue
        self.averageValue = averageValue
        self.minimumValue = minimumValue
        self.maximumValue = maximumValue
        self.sampleCount = sampleCount
    }
}

/// HealthDaySummaries works out the summaries of days.
public enum HealthDaySummaries {
    // MARK: Quantities

    /// The summary of a quantity on a day from HealthKit's statistics, with
    /// only the numbers that mean something for the type: the sum for a type
    /// that adds up, the average with the least and the most for one that
    /// averages. The unit is the table's unless one is given.
    public static func quantitySummary(
        healthType: String, summaryDay: String, statistics: HealthQuantityStatistics, quantityUnit: String? = nil
    ) -> HealthDaySummaryInput {
        let info = HealthTypes.info(for: healthType)
        let unit = quantityUnit ?? info?.quantityUnit ?? ""
        switch info?.summaryKind {
        case .sum:
            return HealthDaySummaryInput(
                healthType: healthType, summaryDay: summaryDay, quantityUnit: unit, sumValue: statistics.sumValue, sampleCount: statistics.sampleCount)
        case .average:
            return HealthDaySummaryInput(
                healthType: healthType, summaryDay: summaryDay, quantityUnit: unit, averageValue: statistics.averageValue,
                minimumValue: statistics.minimumValue, maximumValue: statistics.maximumValue, sampleCount: statistics.sampleCount)
        default:
            return HealthDaySummaryInput(
                healthType: healthType, summaryDay: summaryDay, quantityUnit: unit, sumValue: statistics.sumValue,
                averageValue: statistics.averageValue, minimumValue: statistics.minimumValue, maximumValue: statistics.maximumValue,
                sampleCount: statistics.sampleCount)
        }
    }

    // MARK: Categories

    /// A reading cut by the days it touches: each day with the part of the
    /// reading on it. A reading that takes no time is on its start's day, as
    /// an interval of no length.
    private static func parts(from start: Date, to end: Date, calendar: Calendar) -> [(day: String, interval: DateInterval)] {
        guard end >= start else { return [] }
        if end == start { return [(HealthDays.text(of: start, calendar: calendar), DateInterval(start: start, end: start))] }
        return HealthDays.days(from: start, to: end, calendar: calendar).compactMap { day in
            guard let window = HealthDays.interval(ofDay: day, calendar: calendar),
                let part = window.intersection(with: DateInterval(start: start, end: end))
            else { return nil }
            return (day, part)
        }
    }

    /// Each distinct sample once: a sample read twice (from two queries) must
    /// not count twice.
    private static func distinct(_ samples: [HealthCategorySample]) -> [HealthCategorySample] {
        var seen = Set<String>()
        return samples.filter { sample in
            guard !sample.healthSampleId.isEmpty else { return true }
            return seen.insert(sample.healthSampleId).inserted
        }
    }

    /// The days' summaries of a category type, by how its days add up in the
    /// table: sleep by stage; mindful minutes and other durations as the time
    /// covered; events and symptoms as a count. `days` limits the answer to
    /// those days ("YYYY-MM-DD"); nil is every day the samples touch.
    public static func categorySummaries(
        healthType: String, samples: [HealthCategorySample], calendar: Calendar, days: Set<String>? = nil
    ) -> [HealthDaySummaryInput] {
        if healthType == "sleepAnalysis" { return sleepSummaries(samples, calendar: calendar, days: days) }
        if HealthTypes.info(for: healthType)?.summaryKind == .duration {
            return durationSummaries(healthType: healthType, samples: samples, calendar: calendar, days: days)
        }
        return countSummaries(healthType: healthType, samples: samples, calendar: calendar, days: days)
    }

    /// Sleep by day. Each stage's seconds are the union of that stage's
    /// intervals, so a phone and a watch both reporting a night count it once;
    /// "asleep" is the union of the asleep stages together, which is the
    /// night's sleep (the stages added would count an overlap of two stages
    /// from two sources twice). durationSeconds is the asleep seconds. A night
    /// that crosses midnight is cut by the calendar's days, each day getting
    /// the part on it. The count is the samples that touch the day.
    public static func sleepSummaries(_ samples: [HealthCategorySample], calendar: Calendar, days: Set<String>? = nil) -> [HealthDaySummaryInput] {
        var stageIntervals: [String: [Int: [DateInterval]]] = [:]
        var sampleCounts: [String: Int] = [:]
        for sample in distinct(samples) {
            for part in parts(from: sample.startedAt, to: sample.endedAt, calendar: calendar) {
                if let days, !days.contains(part.day) { continue }
                sampleCounts[part.day, default: 0] += 1
                stageIntervals[part.day, default: [:]][sample.categoryValue, default: []].append(part.interval)
            }
        }
        return sampleCounts.keys.sorted().map { day in
            let byStage = stageIntervals[day] ?? [:]
            var seconds: [String: Double] = [:]
            for stage in HealthSleepStage.allCases {
                let covered = HealthIntervals.coveredSeconds(byStage[stage.rawValue] ?? [])
                if covered > 0 { seconds[stage.stageName] = covered }
            }
            let asleepSeconds = HealthIntervals.coveredSeconds(
                HealthSleepStage.allCases.filter(\.isAsleep).flatMap { byStage[$0.rawValue] ?? [] })
            seconds[HealthSleepStage.asleepKey] = asleepSeconds
            return HealthDaySummaryInput(
                healthType: "sleepAnalysis", summaryDay: day, sampleCount: sampleCounts[day] ?? 0, durationSeconds: asleepSeconds,
                categorySeconds: seconds)
        }
    }

    /// The time covered by a category's samples on each day, sources
    /// overlapping counted once (mindful minutes).
    public static func durationSummaries(
        healthType: String, samples: [HealthCategorySample], calendar: Calendar, days: Set<String>? = nil
    ) -> [HealthDaySummaryInput] {
        var intervals: [String: [DateInterval]] = [:]
        var sampleCounts: [String: Int] = [:]
        for sample in distinct(samples) {
            for part in parts(from: sample.startedAt, to: sample.endedAt, calendar: calendar) {
                if let days, !days.contains(part.day) { continue }
                sampleCounts[part.day, default: 0] += 1
                intervals[part.day, default: []].append(part.interval)
            }
        }
        return sampleCounts.keys.sorted().map { day in
            HealthDaySummaryInput(
                healthType: healthType, summaryDay: day, sampleCount: sampleCounts[day] ?? 0,
                durationSeconds: HealthIntervals.coveredSeconds(intervals[day] ?? []))
        }
    }

    /// How many samples there are on each day, by the day each began (events
    /// and symptoms).
    public static func countSummaries(
        healthType: String, samples: [HealthCategorySample], calendar: Calendar, days: Set<String>? = nil
    ) -> [HealthDaySummaryInput] {
        var sampleCounts: [String: Int] = [:]
        for sample in distinct(samples) {
            let day = HealthDays.text(of: sample.startedAt, calendar: calendar)
            if let days, !days.contains(day) { continue }
            sampleCounts[day, default: 0] += 1
        }
        return sampleCounts.keys.sorted().map { HealthDaySummaryInput(healthType: healthType, summaryDay: $0, sampleCount: sampleCounts[$0] ?? 0) }
    }

    // MARK: Workouts

    /// Workouts by the day each began: how many, the time they took
    /// (durationSeconds, added up), and, where the workouts carried them, the
    /// energy burned (categorySeconds["energyKilocalories"]) and the distance
    /// (categorySeconds["distanceMeters"]). A workout that crosses midnight
    /// is all on the day it began.
    public static func workoutSummaries(_ workouts: [HealthWorkoutSample], calendar: Calendar, days: Set<String>? = nil) -> [HealthDaySummaryInput] {
        struct Totals {
            var count = 0
            var seconds = 0.0
            var energy: Double?
            var distance: Double?
        }
        var byDay: [String: Totals] = [:]
        var seen = Set<String>()
        for workout in workouts where workout.endedAt >= workout.startedAt {
            if !workout.healthSampleId.isEmpty, !seen.insert(workout.healthSampleId).inserted { continue }
            let day = HealthDays.text(of: workout.startedAt, calendar: calendar)
            if let days, !days.contains(day) { continue }
            var totals = byDay[day] ?? Totals()
            totals.count += 1
            totals.seconds += workout.endedAt.timeIntervalSince(workout.startedAt)
            if let energy = workout.totalEnergyKilocalories, energy.isFinite { totals.energy = (totals.energy ?? 0) + energy }
            if let distance = workout.totalDistanceMeters, distance.isFinite { totals.distance = (totals.distance ?? 0) + distance }
            byDay[day] = totals
        }
        return byDay.keys.sorted().map { day in
            let totals = byDay[day] ?? Totals()
            var extras: [String: Double] = [:]
            if let energy = totals.energy { extras["energyKilocalories"] = energy }
            if let distance = totals.distance { extras["distanceMeters"] = distance }
            return HealthDaySummaryInput(
                healthType: "workout", summaryDay: day, sampleCount: totals.count, durationSeconds: totals.seconds, categorySeconds: extras)
        }
    }
}

extension JSONValue {
    /// The number this holds, when it is one.
    var healthNumber: Double? {
        if case .number(let number) = self { return number }
        return nil
    }
}
