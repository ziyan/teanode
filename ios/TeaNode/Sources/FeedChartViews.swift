import Charts
import SwiftUI
import TeaNodeKit

// The charts of a feed card (docs/planning/feed-cards-execplan.md, "Charts"):
// a series over time with a reference line and labeled markers, or bars; each
// with the line that says where and when the numbers were read. Drawn with
// Swift Charts.

// MARK: One chart

/// FeedChartView is one chart of a card: its title, the chart, and the
/// provenance as a small muted line under it.
struct FeedChartView: View {
    let chart: AgentFeedChart

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if !chart.chartTitle.isEmpty {
                Text(chart.chartTitle)
                    .font(.subheadline.weight(.semibold))
                    .fixedSize(horizontal: false, vertical: true)
            }
            switch chart.chartKind {
            case .series:
                FeedSeriesChart(chart: chart)
            case .bars:
                FeedBarsChart(chart: chart)
            }
            if !chart.provenance.isEmpty {
                Text(chart.provenance)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

// MARK: Series

/// FeedChartRun is an unbroken stretch of one line: a line stops where its
/// values have no number and starts again after.
private struct FeedChartRun: Identifiable {
    let id: String
    let seriesName: String
    let seriesRole: AgentFeedSeriesRole
    let seriesColor: Color
    let points: [FeedChartPoint]
}

private struct FeedChartPoint: Identifiable {
    let xLabel: String
    let yValue: Double
    var id: String { xLabel }
}

/// FeedSeriesChart draws the lines of a series chart: what was measured in
/// the accent, a reference as a solid line in the primary color, a projection
/// dashed, and markers as labeled vertical rules.
private struct FeedSeriesChart: View {
    let chart: AgentFeedChart
    @ScaledMetric(relativeTo: .body) private var chartHeight: CGFloat = 180

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Chart {
                ForEach(runs) { run in
                    ForEach(run.points) { point in
                        LineMark(
                            x: .value("Time", point.xLabel), y: .value("Value", point.yValue),
                            series: .value("Line", run.id)
                        )
                        .foregroundStyle(run.seriesColor)
                        .lineStyle(lineStyle(for: run.seriesRole))
                        .accessibilityLabel(Text(verbatim: "\(run.seriesName), \(point.xLabel)"))
                        .accessibilityValue(Text(verbatim: chart.valueText(point.yValue)))
                    }
                    // A stretch of one number has no line to show.
                    if run.points.count == 1, let point = run.points.first {
                        PointMark(x: .value("Time", point.xLabel), y: .value("Value", point.yValue))
                            .foregroundStyle(run.seriesColor)
                            .symbolSize(40)
                            .accessibilityHidden(true)
                    }
                }
                ForEach(chart.markers, id: \.markerAt) { marker in
                    RuleMark(x: .value("Time", marker.markerAt))
                        .foregroundStyle(Color.secondary)
                        .lineStyle(StrokeStyle(lineWidth: 1))
                        .annotation(position: .top, alignment: .center, spacing: 2, overflowResolution: .init(x: .fit(to: .chart), y: .disabled)) {
                            Text(verbatim: marker.markerLabel)
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                        }
                        .accessibilityLabel(Text(verbatim: marker.markerAt))
                        .accessibilityValue(Text(verbatim: marker.markerLabel))
                }
            }
            // Not from zero: a level moves by a little of itself, and from
            // zero every line would look flat.
            .chartYScale(domain: .automatic(includesZero: false))
            .chartXAxis {
                AxisMarks(values: shownXLabels) { _ in
                    AxisGridLine()
                    AxisValueLabel()
                }
            }
            .chartYAxis {
                AxisMarks { value in
                    AxisGridLine()
                    AxisValueLabel {
                        if let number = value.as(Double.self) { Text(verbatim: number.formatted(.number.notation(.compactName))) }
                    }
                }
            }
            .chartYAxisLabel(position: .top, alignment: .leading) {
                Text(verbatim: chart.chartUnit)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            // Room over the plot for the markers' words.
            .padding(.top, chart.markers.isEmpty ? 0 : 14)
            .frame(height: chartHeight)
            if chart.series.count > 1 {
                ChartKey(entries: keyEntries)
            }
        }
    }

    private func lineStyle(for seriesRole: AgentFeedSeriesRole) -> StrokeStyle {
        switch seriesRole {
        case .projection: StrokeStyle(lineWidth: 2, lineCap: .round, dash: [5, 4])
        case .measured, .reference: StrokeStyle(lineWidth: 2, lineCap: .round)
        }
    }

    /// The color of a line: the accent for what was measured (a second
    /// measured line takes the next chart color), the primary text color for a
    /// reference and the first chart color, dashed, for a projection.
    private var lineColors: [Color] {
        var measuredCount = 0
        return chart.series.map { line -> Color in
            switch line.seriesRole {
            case .reference:
                return Color.primary
            case .projection:
                return ChartPalette.seriesColor(at: 0)
            case .measured:
                // Charts keep to the chart palette: green is for what can be pressed (DESIGN.md).
                let measuredColor = ChartPalette.seriesColor(at: measuredCount)
                measuredCount += 1
                return measuredColor
            }
        }
    }

    private var runs: [FeedChartRun] {
        let colors = lineColors
        var built: [FeedChartRun] = []
        for (lineIndex, line) in chart.series.enumerated() {
            var points: [FeedChartPoint] = []
            var runIndex = 0
            func finishRun() {
                guard !points.isEmpty else { return }
                built.append(
                    FeedChartRun(
                        id: "\(lineIndex)-\(runIndex)", seriesName: line.seriesName, seriesRole: line.seriesRole,
                        seriesColor: colors[lineIndex], points: points))
                points = []
                runIndex += 1
            }
            for (valueIndex, value) in line.values.enumerated() where valueIndex < chart.xLabels.count {
                if let value {
                    points.append(FeedChartPoint(xLabel: chart.xLabels[valueIndex], yValue: value))
                } else {
                    finishRun()
                }
            }
            finishRun()
        }
        return built
    }

    private var keyEntries: [ChartKeyEntry] {
        let colors = lineColors
        return chart.series.enumerated().map { index, line in ChartKeyEntry(label: line.seriesName, color: colors[index]) }
    }

    /// About four of the axis' labels, so none run into another.
    private var shownXLabels: [String] {
        let step = max(1, Int((Double(chart.xLabels.count) / 4).rounded(.up)))
        return chart.xLabels.enumerated().filter { $0.offset % step == 0 }.map(\.element)
    }
}

// MARK: Bars

/// FeedBarsChart draws horizontal bars, the largest first, each with its
/// value and unit at the end.
/// FeedBarsChart draws bars as rows rather than a Chart: each category's name
/// on a line of its own, then its bar with the value at its end. Swift Charts
/// set the names over the bars of a short horizontal chart.
private struct FeedBarsChart: View {
    let chart: AgentFeedChart

    /// Largest first whatever order the card gave.
    private var sortedBars: [AgentFeedChartBar] {
        chart.bars.sorted { $0.barValue > $1.barValue }
    }

    var body: some View {
        let bars = sortedBars
        let largestValue = max(bars.first?.barValue ?? 0, 0.000_001)
        VStack(alignment: .leading, spacing: 10) {
            ForEach(bars, id: \.barLabel) { bar in
                VStack(alignment: .leading, spacing: 4) {
                    Text(verbatim: bar.barLabel)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    // The bar takes the share of the width its value is of the
                    // largest, leaving room at the end for the value.
                    barRow(bar, share: bar.barValue / largestValue)
                }
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(Text(verbatim: bar.barLabel))
                .accessibilityValue(Text(verbatim: chart.valueText(bar.barValue)))
            }
        }
    }

    private func barRow(_ bar: AgentFeedChartBar, share: Double) -> some View {
        GeometryReader { geometry in
            HStack(spacing: 6) {
                Capsule()
                    .fill(ChartPalette.seriesColor(at: 0))
                    .frame(width: max(geometry.size.width * 0.7 * share, 4), height: 10)
                Text(verbatim: chart.valueText(bar.barValue))
                    .font(.caption)
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .fixedSize()
            }
        }
        .frame(height: 16)
    }
}

// MARK: Previews

private func previewCharts() -> some View {
    VStack(alignment: .leading, spacing: 20) {
        FeedChartView(
            chart: AgentFeedChart(
                chartTitle: "Cellar water level", chartUnit: "cm", provenance: "Read from the sensor at 09:40",
                xLabels: ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat"],
                series: [
                    AgentFeedChartSeries(seriesName: "Level", values: [4, 5, nil, 9, 12, nil]),
                    AgentFeedChartSeries(seriesName: "Average", seriesRoleName: "reference", values: [5, 5, 5, 5, 5, 5]),
                    AgentFeedChartSeries(seriesName: "Expected", seriesRoleName: "projection", values: [nil, nil, nil, nil, 12, 14]),
                ],
                markers: [AgentFeedChartMarker(markerAt: "Wed", markerLabel: "Pump stopped")]))
        FeedChartView(
            chart: AgentFeedChart(
                chartKindName: "bars", chartTitle: "Spending by category", chartUnit: "USD", provenance: "Read from the bank at 09:40",
                bars: [
                    AgentFeedChartBar(barLabel: "Groceries", barValue: 212.5), AgentFeedChartBar(barLabel: "Fuel", barValue: 96),
                    AgentFeedChartBar(barLabel: "Coffee", barValue: 31),
                ]))
    }
    .padding()
}

#Preview("Charts, light") { previewCharts().preferredColorScheme(.light) }
#Preview("Charts, dark") { previewCharts().preferredColorScheme(.dark) }
