import SwiftUI
import TeaNodeKit

// What a chart of one kind of Health data draws, worked out from the server's
// periods. Bars for what adds up (steps, energy, time), the night's stages
// stacked for sleep, and a line with a band from the lowest to the highest
// reading for what is read as a level (heart rate, weight). The same plan as
// the dashboard's (web/src/pages/health/healthChart.tsx).

/// HealthRange is how far back a chart reaches.
enum HealthRange: Int, CaseIterable, Identifiable {
    case thirtyDays = 30
    case ninetyDays = 90
    case year = 365
    /// From the first day there is data.
    case all = 0

    var id: Int { rawValue }

    /// The period a range is drawn by until the person chooses one: a bar a
    /// day would be thinner than a pixel over a year.
    var defaultPeriod: HealthPeriod {
        switch self {
        case .all: return .month
        case .year: return .week
        case .thirtyDays, .ninetyDays: return .day
        }
    }
}

/// HealthNumbers are the figures under a chart, worked out from its periods.
struct HealthNumbers {
    /// The average of a kind read as a level, weighted by how many readings
    /// each period holds.
    var averageValue: Double?
    /// The average of a day's amount for a kind that adds up, over the days
    /// that have any.
    var averagePerDayValue: Double?
    var totalValue: Double?
    var lowestValue: Double?
    var highestValue: Double?
}

/// HealthPlot is one period of a chart that has readings.
struct HealthPlot: Identifiable {
    /// The period's first day, "YYYY-MM-DD": its place on the chart.
    let periodKey: String
    /// What a touched mark says it is: the day, the week or the month.
    let titleText: String
    /// The bar's height, or the line's value, in the chart's own unit (hours
    /// for a time).
    let amountValue: Double?
    /// The lowest and the highest reading of a period, for the band behind
    /// a line; nil for the rest.
    var lowestValue: Double?
    var highestValue: Double?
    /// The numbers a touched mark shows.
    let figures: [ChartFigure]
    /// What VoiceOver reads for it.
    let spokenText: String
    var id: String { periodKey }
}

/// HealthFigureRow is one number under a chart.
struct HealthFigureRow: Identifiable {
    let label: String
    let valueText: String
    var id: String { label }
}

/// HealthStageSegment is the hours of one stage of sleep in one period.
struct HealthStageSegment: Identifiable {
    let periodKey: String
    let stage: HealthSleepStage
    let stageTitle: String
    let hours: Double
    var id: String { periodKey + stage.stageName }
}

struct HealthChartPlan {
    enum Shape {
        case bars
        case stages
        case level
    }

    let shape: Shape
    let summaryKind: HealthSummaryKind
    let quantityUnit: String
    let period: HealthPeriod
    /// Every place along the axis, in order: all the days of a day chart (a
    /// day with nothing is a gap), the periods the server sent otherwise.
    let periodKeys: [String]
    let plots: [HealthPlot]
    let stageSegments: [HealthStageSegment]
    let numbers: HealthNumbers

    /// The stages of sleep a chart stacks, from the deepest up. In bed is
    /// left out: it covers the time asleep.
    static let shownStages: [HealthSleepStage] = [.asleepDeep, .asleepCore, .asleepREM, .asleepUnspecified, .awake]

    var hasData: Bool { !plots.isEmpty }

    // MARK: The axes

    /// The places on the axis that carry a label: at most five, evenly
    /// spaced, so they fit at every text size.
    var labelledKeys: [String] {
        guard periodKeys.count > 5 else { return periodKeys }
        let step = Double(periodKeys.count - 1) / 4
        return (0...4).map { periodKeys[Int((Double($0) * step).rounded())] }
    }

    private var includesMultipleYears: Bool {
        guard let first = periodKeys.first, let last = periodKeys.last else { return false }
        return first.prefix(4) != last.prefix(4)
    }

    func axisLabel(forKey periodKey: String) -> String {
        if period == .month { return HealthText.monthText(periodKey, includesYear: includesMultipleYears) }
        return HealthText.shortDayText(periodKey)
    }

    /// What follows a number on the value axis: hours for a time, nothing
    /// for the rest (the headline has the unit).
    var axisUnitSuffix: String { summaryKind == .duration ? " h" : "" }

    // MARK: The legend of sleep

    var stageKeyEntries: [ChartKeyEntry] {
        Self.shownStages.map { ChartKeyEntry(label: HealthWords.stageName($0), color: Self.color(of: $0)) }
    }

    var stageTitles: [String] { Self.shownStages.map { HealthWords.stageName($0) } }

    var stageColors: [Color] { Self.shownStages.map { Self.color(of: $0) } }

    static func color(of stage: HealthSleepStage) -> Color {
        switch stage {
        case .asleepDeep: return ChartPalette.seriesColor(at: 6)
        case .asleepCore: return ChartPalette.seriesColor(at: 0)
        case .asleepREM: return ChartPalette.seriesColor(at: 3)
        case .asleepUnspecified: return ChartPalette.seriesColor(at: 2)
        case .awake: return ChartPalette.seriesColor(at: 1)
        case .inBed: return ChartPalette.restColor
        }
    }

    // MARK: The figures under the chart

    /// The first figure is the headline; the rest are rows under the chart.
    var figureRows: [HealthFigureRow] {
        func text(_ value: Double?) -> String {
            HealthText.amountText(value, summaryKind: summaryKind, quantityUnit: quantityUnit)
        }
        switch summaryKind {
        case .average:
            return [
                HealthFigureRow(label: String(localized: "Average"), valueText: text(numbers.averageValue)),
                HealthFigureRow(label: String(localized: "Lowest"), valueText: text(numbers.lowestValue)),
                HealthFigureRow(label: String(localized: "Highest"), valueText: text(numbers.highestValue)),
            ]
        case .count:
            return [HealthFigureRow(label: String(localized: "Total"), valueText: text(numbers.totalValue))]
        case .sum, .duration:
            return [
                HealthFigureRow(label: String(localized: "Average per day"), valueText: text(numbers.averagePerDayValue)),
                HealthFigureRow(label: String(localized: "Total"), valueText: text(numbers.totalValue)),
            ]
        }
    }

    // MARK: Making a plan

    /// The plan for the periods the server sent for the days `fromDay` to
    /// `toDay` ("YYYY-MM-DD").
    static func make(summary: HealthSummaryView, type: HealthTypeView, period: HealthPeriod, fromDay: String, toDay: String) -> HealthChartPlan {
        let summaryKind = summary.summaryKindValue ?? type.summaryKindValue ?? .sum
        let quantityUnit = summary.quantityUnit.isEmpty ? type.quantityUnit : summary.quantityUnit
        let filledPeriods = summary.periods.filter { $0.sampleCount > 0 }
        let rowsByKey = Dictionary(filledPeriods.map { (String($0.startDay.prefix(10)), $0) }, uniquingKeysWith: { first, _ in first })
        let periodKeys: [String]
        if period == .day {
            periodKeys = HealthText.days(from: fromDay, to: toDay)
        } else {
            periodKeys = summary.periods.map { String($0.startDay.prefix(10)) }
        }
        let isStaged = summaryKind == .duration && filledPeriods.contains { row in Self.stageSeconds(of: row).values.contains { $0 > 0 } }
        let shape: Shape = isStaged ? .stages : (summaryKind == .average ? .level : .bars)
        let builder = HealthPlotBuilder(
            summaryKind: summaryKind, quantityUnit: quantityUnit, period: period, fromDay: fromDay, toDay: toDay, shape: shape)
        var plots: [HealthPlot] = []
        var stageSegments: [HealthStageSegment] = []
        for periodKey in periodKeys {
            guard let row = rowsByKey[periodKey] else { continue }
            plots.append(builder.plot(for: row, periodKey: periodKey))
            if isStaged { stageSegments.append(contentsOf: builder.segments(for: row, periodKey: periodKey)) }
        }
        return HealthChartPlan(
            shape: shape, summaryKind: summaryKind, quantityUnit: quantityUnit, period: period, periodKeys: periodKeys, plots: plots,
            stageSegments: stageSegments,
            numbers: numbers(of: filledPeriods, summaryKind: summaryKind, fromDay: fromDay, toDay: toDay))
    }

    /// The seconds in each stage of a period, by the stage's name.
    static func stageSeconds(of row: HealthPeriodView) -> [String: Double] {
        var seconds: [String: Double] = [:]
        for stage in shownStages {
            if let stageValue = row.categorySeconds[stage.stageName] { seconds[stage.stageName] = stageValue }
        }
        return seconds
    }

    private static func numbers(of rows: [HealthPeriodView], summaryKind: HealthSummaryKind, fromDay: String, toDay: String) -> HealthNumbers {
        var numbers = HealthNumbers()
        guard !rows.isEmpty else { return numbers }
        if summaryKind == .average {
            let readingCount = rows.reduce(0) { $0 + $1.sampleCount }
            let weightedSum = rows.reduce(0.0) { $0 + ($1.averageValue ?? 0) * Double($1.sampleCount) }
            numbers.averageValue = readingCount > 0 ? weightedSum / Double(readingCount) : nil
            numbers.lowestValue = rows.compactMap(\.minimumValue).min()
            numbers.highestValue = rows.compactMap(\.maximumValue).max()
            return numbers
        }
        let values = rows.map {
            HealthText.summaryValue(
                sumValue: $0.sumValue, averageValue: $0.averageValue, durationSeconds: $0.durationSeconds, sampleCount: $0.sampleCount,
                summaryKind: summaryKind) ?? 0
        }
        let total = values.reduce(0, +)
        numbers.totalValue = total
        if summaryKind != .count {
            let dayCount = rows.reduce(0) { $0 + HealthPlotBuilder.dayCount(of: $1, fromDay: fromDay, toDay: toDay) }
            numbers.averagePerDayValue = dayCount > 0 ? total / Double(dayCount) : nil
        }
        return numbers
    }
}

/// HealthPlotBuilder makes the plot of one period.
struct HealthPlotBuilder {
    let summaryKind: HealthSummaryKind
    let quantityUnit: String
    let period: HealthPeriod
    let fromDay: String
    let toDay: String
    let shape: HealthChartPlan.Shape

    /// How many days of a period the chart reaches over: the current week or
    /// month has not ended, and the range may start inside one.
    static func dayCount(of row: HealthPeriodView, fromDay: String, toDay: String) -> Int {
        let startDay = String(row.startDay.prefix(10))
        let endDay = String(row.endDay.prefix(10))
        let first = startDay > fromDay ? startDay : fromDay
        let last = endDay < toDay ? endDay : toDay
        return HealthText.dayCount(from: first, to: last)
    }

    /// Whether a period is drawn as the amount of one day of it, as the
    /// Health app does, so a week and a day read on one scale.
    private var isPerDay: Bool {
        period != .day && (summaryKind == .sum || summaryKind == .duration)
    }

    private func perDay(_ row: HealthPeriodView, _ value: Double) -> Double {
        guard isPerDay else { return value }
        return value / Double(Self.dayCount(of: row, fromDay: fromDay, toDay: toDay))
    }

    func plot(for row: HealthPeriodView, periodKey: String) -> HealthPlot {
        let titleText = periodTitle(row, periodKey: periodKey)
        switch shape {
        case .level:
            return levelPlot(for: row, periodKey: periodKey, titleText: titleText)
        case .stages:
            return stagesPlot(for: row, periodKey: periodKey, titleText: titleText)
        case .bars:
            return barsPlot(for: row, periodKey: periodKey, titleText: titleText)
        }
    }

    private func barsPlot(for row: HealthPeriodView, periodKey: String, titleText: String) -> HealthPlot {
        let rawValue = HealthText.summaryValue(
            sumValue: row.sumValue, averageValue: row.averageValue, durationSeconds: row.durationSeconds, sampleCount: row.sampleCount,
            summaryKind: summaryKind)
        let value = rawValue.map { perDay(row, $0) }
        let valueText = HealthText.amountText(value, summaryKind: summaryKind, quantityUnit: quantityUnit)
        let figureLabel: String? = isPerDay ? String(localized: "Per day") : nil
        return HealthPlot(
            periodKey: periodKey, titleText: titleText, amountValue: chartAmount(value),
            figures: [ChartFigure(label: figureLabel, valueText: valueText)], spokenText: valueText)
    }

    private func levelPlot(for row: HealthPeriodView, periodKey: String, titleText: String) -> HealthPlot {
        let averageText = HealthText.amountText(row.averageValue, summaryKind: summaryKind, quantityUnit: quantityUnit)
        var figures = [ChartFigure(label: String(localized: "Average"), valueText: averageText)]
        if let lowest = row.minimumValue, let highest = row.maximumValue {
            figures.append(
                ChartFigure(
                    label: String(localized: "Lowest"), valueText: HealthText.quantityText(lowest, quantityUnit: quantityUnit)))
            figures.append(
                ChartFigure(
                    label: String(localized: "Highest"), valueText: HealthText.quantityText(highest, quantityUnit: quantityUnit)))
        }
        return HealthPlot(
            periodKey: periodKey, titleText: titleText, amountValue: row.averageValue, lowestValue: row.minimumValue,
            highestValue: row.maximumValue, figures: figures, spokenText: averageText)
    }

    private func stagesPlot(for row: HealthPeriodView, periodKey: String, titleText: String) -> HealthPlot {
        var figures: [ChartFigure] = []
        for stage in HealthChartPlan.shownStages {
            guard let seconds = row.categorySeconds[stage.stageName], seconds > 0 else { continue }
            figures.append(
                ChartFigure(
                    label: HealthWords.stageName(stage), valueText: HealthText.durationText(perDay(row, seconds)),
                    color: HealthChartPlan.color(of: stage)))
        }
        let asleepSeconds = row.categorySeconds[HealthSleepStage.asleepKey] ?? row.durationSeconds
        let asleepText = HealthText.durationText(perDay(row, asleepSeconds ?? 0))
        return HealthPlot(
            periodKey: periodKey, titleText: titleText, amountValue: asleepSeconds.map { perDay(row, $0) / 3600 }, figures: figures,
            spokenText: asleepText)
    }

    func segments(for row: HealthPeriodView, periodKey: String) -> [HealthStageSegment] {
        HealthChartPlan.shownStages.compactMap { stage in
            guard let seconds = row.categorySeconds[stage.stageName], seconds > 0 else { return nil }
            return HealthStageSegment(
                periodKey: periodKey, stage: stage, stageTitle: HealthWords.stageName(stage), hours: perDay(row, seconds) / 3600)
        }
    }

    /// The height of a bar: hours for a time, else the amount.
    private func chartAmount(_ value: Double?) -> Double? {
        guard let value else { return nil }
        return summaryKind == .duration ? value / 3600 : value
    }

    private func periodTitle(_ row: HealthPeriodView, periodKey: String) -> String {
        switch period {
        case .day:
            return HealthText.mediumDayText(periodKey)
        case .week:
            let fromText = HealthText.mediumDayText(row.startDay)
            let untilText = HealthText.mediumDayText(row.endDay)
            return String(localized: "\(fromText) to \(untilText)")
        case .month:
            guard let date = HealthText.date(ofDay: periodKey) else { return periodKey }
            return date.formatted(.dateTime.month(.wide).year())
        }
    }
}
