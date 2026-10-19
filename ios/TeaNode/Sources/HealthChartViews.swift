import Charts
import SwiftUI
import TeaNodeKit

// The three charts of a kind of Health data. Each takes the plan worked out
// in HealthChartPlan.swift and the day, week or month a finger is on, as
// Finance's charts do (ChartParts.swift).

/// HealthChartView draws the chart the plan calls for.
struct HealthChartView: View {
    let plan: HealthChartPlan
    @Binding var selectedKey: String?

    var body: some View {
        switch plan.shape {
        case .bars: HealthBarsChart(plan: plan, selectedKey: $selectedKey)
        case .stages: HealthStagesChart(plan: plan, selectedKey: $selectedKey)
        case .level: HealthLevelChart(plan: plan, selectedKey: $selectedKey)
        }
    }
}

extension View {
    /// The axes every Health chart shares: a few dates along the bottom and
    /// the values at the side.
    func healthChartAxes(plan: HealthChartPlan) -> some View {
        chartXAxis {
            AxisMarks(values: plan.labelledKeys) { value in
                AxisGridLine()
                AxisValueLabel {
                    if let periodKey = value.as(String.self) {
                        Text(verbatim: plan.axisLabel(forKey: periodKey)).fixedSize()
                    }
                }
            }
        }
        .chartYAxis {
            AxisMarks { value in
                AxisGridLine()
                AxisValueLabel {
                    if let number = value.as(Double.self) {
                        Text(verbatim: plan.axisText(number))
                    }
                }
            }
        }
    }
}

/// HealthBarsChart is a bar for each period of what adds up.
struct HealthBarsChart: View {
    let plan: HealthChartPlan
    @Binding var selectedKey: String?

    private var selectedPlot: HealthPlot? { plan.plots.first { $0.periodKey == selectedKey } }

    var body: some View {
        Chart {
            ForEach(plan.plots) { plot in
                if let amountValue = plot.amountValue {
                    BarMark(x: .value("Period", plot.periodKey), y: .value("Amount", amountValue))
                        .foregroundStyle(ChartPalette.seriesColor(at: 0))
                        .opacity(ChartPalette.markOpacity(isChosen: plot.periodKey == selectedKey, isAnyChosen: selectedKey != nil))
                        .accessibilityLabel(Text(verbatim: plot.titleText))
                        .accessibilityValue(Text(verbatim: plot.spokenText))
                }
            }
            if let selectedPlot {
                ChartSelectionRule(value: selectedPlot.periodKey) {
                    ChartCallout(title: selectedPlot.titleText, figures: selectedPlot.figures)
                }
            }
        }
        .chartXScale(domain: plan.periodKeys)
        .chartXSelection(value: $selectedKey)
        .keepsSidePanelClosed()
        .healthChartAxes(plan: plan)
        .frame(height: 200)
    }
}

/// HealthStagesChart is a night of sleep for each period, the stages stacked.
struct HealthStagesChart: View {
    let plan: HealthChartPlan
    @Binding var selectedKey: String?

    private var selectedPlot: HealthPlot? { plan.plots.first { $0.periodKey == selectedKey } }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            // The legend's line is tall enough for the callout of a touched
            // night to take its place rather than cover the row above.
            ChartKey(entries: plan.stageKeyEntries)
                .frame(minHeight: 44, alignment: .bottomLeading)
                .opacity(selectedKey == nil ? 1 : 0)
            Chart {
                ForEach(plan.stageSegments) { segment in
                    BarMark(x: .value("Period", segment.periodKey), y: .value("Hours", segment.hours))
                        .foregroundStyle(by: .value("Stage", segment.stageTitle))
                        .opacity(ChartPalette.markOpacity(isChosen: segment.periodKey == selectedKey, isAnyChosen: selectedKey != nil))
                }
                if let selectedPlot {
                    ChartSelectionRule(value: selectedPlot.periodKey) {
                        ChartCallout(title: selectedPlot.titleText, figures: selectedPlot.figures)
                    }
                }
            }
            .chartForegroundStyleScale(domain: plan.stageTitles, range: plan.stageColors)
            .chartLegend(.hidden)
            .chartXScale(domain: plan.periodKeys)
            .chartXSelection(value: $selectedKey)
            .keepsSidePanelClosed()
            .healthChartAxes(plan: plan)
            .frame(height: 200)
        }
    }
}

/// HealthLevelChart is a line of the average of each period, with a band
/// from the lowest reading to the highest.
struct HealthLevelChart: View {
    let plan: HealthChartPlan
    @Binding var selectedKey: String?

    private var selectedPlot: HealthPlot? { plan.plots.first { $0.periodKey == selectedKey } }

    var body: some View {
        Chart {
            ForEach(plan.plots) { plot in
                if let lowestValue = plot.lowestValue, let highestValue = plot.highestValue {
                    AreaMark(
                        x: .value("Period", plot.periodKey), yStart: .value("Lowest", lowestValue), yEnd: .value("Highest", highestValue)
                    )
                    .foregroundStyle(ChartPalette.seriesColor(at: 0).opacity(0.2))
                }
                if let amountValue = plot.amountValue {
                    LineMark(x: .value("Period", plot.periodKey), y: .value("Average", amountValue))
                        .foregroundStyle(ChartPalette.seriesColor(at: 0))
                        .accessibilityLabel(Text(verbatim: plot.titleText))
                        .accessibilityValue(Text(verbatim: plot.spokenText))
                }
            }
            if let selectedPlot, let amountValue = selectedPlot.amountValue {
                PointMark(x: .value("Period", selectedPlot.periodKey), y: .value("Average", amountValue))
                    .foregroundStyle(ChartPalette.seriesColor(at: 0))
                    .symbolSize(60)
                ChartSelectionRule(value: selectedPlot.periodKey) {
                    ChartCallout(title: selectedPlot.titleText, figures: selectedPlot.figures)
                }
            }
        }
        .chartXScale(domain: plan.periodKeys)
        .chartXSelection(value: $selectedKey)
        .keepsSidePanelClosed()
        // Not from zero: a heart rate or a weight moves by a little of
        // itself, and from zero every line would look flat.
        .chartYScale(domain: .automatic(includesZero: false))
        .healthChartAxes(plan: plan)
        .frame(height: 200)
    }
}
