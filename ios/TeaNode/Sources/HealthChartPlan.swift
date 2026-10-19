import SwiftUI
import TeaNodeKit

// What a chart of one kind of Health data draws, worked out from the server's
// periods. Bars for what adds up (steps, energy, time), the night's stages
// stacked for sleep, and a line with a band from the lowest to the highest
// reading for what is read as a level (heart rate, weight). The same plan as
// the dashboard's (web/src/pages/health/healthChart.tsx).

/// HealthRange is how far back a chart reaches. A chart opens on the last 30 days.
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
    let healthType: String
    let quantityUnit: String
    let period: HealthPeriod
    /// Every place along the axis, in order: all the days of a day chart (a
    /// day with nothing is a gap), the periods the server sent otherwise.
    let periodKeys: [String]
    let plots: [HealthPlot]
    let stageSegments: [HealthStageSegment]
    let numbers: HealthNumbers
    /// The stages a chart of sleep has marks for, from the deepest up: those
    /// the periods have time in, and time in bed only when that is all there
    /// is.
    let drawnStages: [HealthSleepStage]
    /// How many seconds one unit of a bar's height is: an hour for sleep, a
    /// minute for workouts.
    let chartUnitSeconds: Double

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

    private var doesSpanYears: Bool {
        guard let first = periodKeys.first, let last = periodKeys.last else { return false }
        return first.prefix(4) != last.prefix(4)
    }

    func axisLabel(forKey periodKey: String) -> String {
        if period == .month { return HealthText.monthText(periodKey, isYearIncluded: doesSpanYears) }
        return HealthText.shortDayText(periodKey)
    }

    /// A number on the value axis: hours or minutes for a time, as the person
    /// reads a percentage for one, and nothing else added (the headline has
    /// the unit).
    func axisText(_ value: Double) -> String {
        if summaryKind == .duration {
            let number = HealthText.axisText(value)
            return chartUnitSeconds == Self.minuteSeconds ? String(localized: "\(number) min") : String(localized: "\(number) h")
        }
        return HealthText.axisText(quantityUnit == "%" ? value * 100 : value)
    }

    static let hourSeconds = 3600.0
    static let minuteSeconds = 60.0

    // MARK: The legend of sleep

    var stageKeyEntries: [ChartKeyEntry] {
        drawnStages.map { ChartKeyEntry(label: HealthWords.stageName($0), color: Self.color(of: $0)) }
    }

    var stageTitles: [String] { drawnStages.map { HealthWords.stageName($0) } }

    var stageColors: [Color] { drawnStages.map { Self.color(of: $0) } }

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
            HealthText.amountText(value, summaryKind: summaryKind, healthType: healthType, quantityUnit: quantityUnit)
        }
        switch summaryKind {
        case .average:
            return [
                HealthFigureRow(label: String(localized: "Average"), valueText: text(numbers.averageValue)),
                HealthFigureRow(label: String(localized: "Lowest"), valueText: text(numbers.lowestValue)),
                HealthFigureRow(label: String(localized: "Highest"), valueText: text(numbers.highestValue)),
            ]
        case .count:
            return [HealthFigureRow(label: HealthWords.totalTitle, valueText: text(numbers.totalValue))]
        case .sum, .duration:
            // The nights of a chart of sleep are not added together: a total
            // of 177 hours says nothing about any night.
            if shape == .stages {
                return [HealthFigureRow(label: String(localized: "Average per day"), valueText: text(numbers.averagePerDayValue))]
            }
            return [
                HealthFigureRow(label: String(localized: "Average per day"), valueText: text(numbers.averagePerDayValue)),
                HealthFigureRow(label: HealthWords.totalTitle, valueText: text(numbers.totalValue)),
            ]
        }
    }

    // MARK: Making a plan

    /// The plan for the periods the server sent for the days `fromDay` to
    /// `toDay` ("YYYY-MM-DD").
    static func make(summary: HealthSummaryView, type: HealthTypeView, period: HealthPeriod, fromDay: String, toDay: String) -> HealthChartPlan {
        let summaryKind = summary.summaryKindValue ?? type.summaryKindValue ?? .sum
        let sentUnit = summary.quantityUnit.isEmpty ? type.quantityUnit : summary.quantityUnit
        let quantityUnit = sentUnit
        let chartUnitSeconds = type.valueKindValue == .workout ? minuteSeconds : hourSeconds
        let filledPeriods = summary.periods.filter { $0.sampleCount > 0 }
        let rowsByKey = Dictionary(filledPeriods.map { (String($0.startDay.prefix(10)), $0) }, uniquingKeysWith: { first, _ in first })
        let periodKeys: [String]
        if period == .day {
            periodKeys = HealthText.days(from: fromDay, to: toDay)
        } else {
            periodKeys = summary.periods.map { String($0.startDay.prefix(10)) }
        }
        let isStaged = summaryKind == .duration && filledPeriods.contains { row in !Self.stageSeconds(of: row).isEmpty }
        let shape: Shape = isStaged ? .stages : (summaryKind == .average ? .level : .bars)
        let builder = HealthPlotBuilder(
            summaryKind: summaryKind, healthType: type.healthType, quantityUnit: quantityUnit, period: period, fromDay: fromDay, toDay: toDay,
            shape: shape, chartUnitSeconds: chartUnitSeconds)
        var plots: [HealthPlot] = []
        var stageSegments: [HealthStageSegment] = []
        for periodKey in periodKeys {
            guard let row = rowsByKey[periodKey] else { continue }
            plots.append(builder.plot(for: row, periodKey: periodKey))
            if isStaged { stageSegments.append(contentsOf: builder.segments(for: row, periodKey: periodKey)) }
        }
        let segmentStages = Set(stageSegments.map(\.stage))
        let drawnStages = (shownStages + [HealthSleepStage.inBed]).filter { segmentStages.contains($0) }
        return HealthChartPlan(
            shape: shape, summaryKind: summaryKind, healthType: type.healthType, quantityUnit: quantityUnit, period: period, periodKeys: periodKeys,
            plots: plots,
            stageSegments: stageSegments,
            numbers: numbers(of: filledPeriods, summaryKind: summaryKind, fromDay: fromDay, toDay: toDay), drawnStages: drawnStages,
            chartUnitSeconds: chartUnitSeconds)
    }

    /// The seconds in each stage of a period, by the stage's name: the stages
    /// asleep and awake that have any time, or, for a night that was only
    /// recorded as in bed, that.
    static func stageSeconds(of row: HealthPeriodView) -> [String: Double] {
        var seconds: [String: Double] = [:]
        for stage in shownStages {
            if let stageValue = row.categorySeconds[stage.stageName], stageValue > 0 { seconds[stage.stageName] = stageValue }
        }
        if seconds.isEmpty, let inBedSeconds = row.categorySeconds[HealthSleepStage.inBed.stageName], inBedSeconds > 0 {
            seconds[HealthSleepStage.inBed.stageName] = inBedSeconds
        }
        return seconds
    }

    /// The stages a period draws, from the deepest up.
    static func stages(of row: HealthPeriodView) -> [HealthSleepStage] {
        let seconds = stageSeconds(of: row)
        return (shownStages + [HealthSleepStage.inBed]).filter { seconds[$0.stageName] != nil }
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
    let healthType: String
    let quantityUnit: String
    let period: HealthPeriod
    let fromDay: String
    let toDay: String
    let shape: HealthChartPlan.Shape
    let chartUnitSeconds: Double

    /// How many days of a period an amount is divided by to be read per day:
    /// the days that have a day summary, as the server counts them, so that a
    /// month with twelve days sent is averaged over twelve and not thirty.
    /// When the server did not say, the days the chart reaches over: the
    /// current week or month has not ended, and the range may start inside
    /// one.
    static func dayCount(of row: HealthPeriodView, fromDay: String, toDay: String) -> Int {
        if row.dayCount > 0 { return row.dayCount }
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
        let valueText = HealthText.amountText(value, summaryKind: summaryKind, healthType: healthType, quantityUnit: quantityUnit)
        let figureLabel: String? = isPerDay ? String(localized: "Per day") : nil
        return HealthPlot(
            periodKey: periodKey, titleText: titleText, amountValue: chartAmount(value),
            figures: [ChartFigure(label: figureLabel, valueText: valueText)], spokenText: valueText)
    }

    private func levelPlot(for row: HealthPeriodView, periodKey: String, titleText: String) -> HealthPlot {
        let averageText = HealthText.amountText(row.averageValue, summaryKind: summaryKind, healthType: healthType, quantityUnit: quantityUnit)
        var figures = [ChartFigure(label: String(localized: "Average"), valueText: averageText)]
        if let lowest = row.minimumValue, let highest = row.maximumValue {
            figures.append(
                ChartFigure(
                    label: String(localized: "Lowest"), valueText: HealthText.quantityText(lowest, healthType: healthType, quantityUnit: quantityUnit)))
            figures.append(
                ChartFigure(
                    label: String(localized: "Highest"), valueText: HealthText.quantityText(highest, healthType: healthType, quantityUnit: quantityUnit)))
        }
        return HealthPlot(
            periodKey: periodKey, titleText: titleText, amountValue: row.averageValue, lowestValue: row.minimumValue,
            highestValue: row.maximumValue, figures: figures, spokenText: averageText)
    }

    private func stagesPlot(for row: HealthPeriodView, periodKey: String, titleText: String) -> HealthPlot {
        let seconds = HealthChartPlan.stageSeconds(of: row)
        var figures: [ChartFigure] = []
        for stage in HealthChartPlan.stages(of: row) {
            guard let stageSeconds = seconds[stage.stageName] else { continue }
            figures.append(
                ChartFigure(
                    label: HealthWords.stageName(stage), valueText: HealthText.durationText(perDay(row, stageSeconds)),
                    color: HealthChartPlan.color(of: stage)))
        }
        // The total is the time asleep, as the headline is: the time awake is
        // a stage of its own and not part of it.
        var asleepSeconds = row.categorySeconds[HealthSleepStage.asleepKey] ?? row.durationSeconds ?? 0
        if asleepSeconds <= 0, let inBedSeconds = seconds[HealthSleepStage.inBed.stageName] { asleepSeconds = inBedSeconds }
        let asleepText = HealthText.durationText(perDay(row, asleepSeconds))
        figures.append(ChartFigure(label: HealthWords.totalTitle, valueText: asleepText))
        return HealthPlot(
            periodKey: periodKey, titleText: titleText, amountValue: perDay(row, asleepSeconds) / chartUnitSeconds, figures: figures,
            spokenText: asleepText)
    }

    func segments(for row: HealthPeriodView, periodKey: String) -> [HealthStageSegment] {
        let seconds = HealthChartPlan.stageSeconds(of: row)
        return HealthChartPlan.stages(of: row).compactMap { stage in
            guard let stageSeconds = seconds[stage.stageName] else { return nil }
            return HealthStageSegment(
                periodKey: periodKey, stage: stage, stageTitle: HealthWords.stageName(stage), hours: perDay(row, stageSeconds) / chartUnitSeconds)
        }
    }

    /// The height of a bar: hours for sleep, minutes for workouts, else the
    /// amount.
    private func chartAmount(_ value: Double?) -> Double? {
        guard let value else { return nil }
        return summaryKind == .duration ? value / chartUnitSeconds : value
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
