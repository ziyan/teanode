import Charts
import SwiftUI

/// ChartPalette is the one list of colors every chart's series take, in
/// order, and the rows under a chart repeat them, so a row and its slice,
/// bar or line are the same color. They are system colors, so each holds
/// in light mode, dark mode and increased contrast. Green is left out
/// because it marks what can be pressed, and red because it says something
/// failed.
enum ChartPalette {
    static let seriesColors: [Color] = [.blue, .orange, .purple, .cyan, .pink, .yellow, .indigo, .brown]

    /// The color the groups beyond the palette share, as one slice.
    static let restColor = Color(.systemGray)

    static func seriesColor(at index: Int) -> Color {
        seriesColors[index % seriesColors.count]
    }

    /// A mark's opacity while one is chosen: the chosen one stays full and
    /// the rest step back. With none chosen, all are full.
    static func markOpacity(isChosen: Bool, isAnyChosen: Bool) -> Double {
        !isAnyChosen || isChosen ? 1 : 0.3
    }
}

/// ChartSwatch is the dot that ties a row or a legend entry to its mark.
/// It grows with the text beside it.
struct ChartSwatch: View {
    let color: Color
    @ScaledMetric(relativeTo: .body) private var size = 10.0

    var body: some View {
        Circle()
            .fill(color)
            .frame(width: size, height: size)
            .accessibilityHidden(true)
    }
}

/// ChartKeyEntry is one series a chart's legend names.
struct ChartKeyEntry: Identifiable {
    let label: String
    let color: Color
    var id: String { label }
}

/// ChartKey is a chart's legend: each series' color and name, on one line
/// when they fit and one under another when they do not.
struct ChartKey: View {
    let entries: [ChartKeyEntry]

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 16) { entryViews }
            VStack(alignment: .leading, spacing: 4) { entryViews }
        }
        .font(.footnote)
        .foregroundStyle(.secondary)
        .accessibilityElement(children: .combine)
    }

    private var entryViews: some View {
        ForEach(entries) { entry in
            HStack(spacing: 6) {
                ChartSwatch(color: entry.color)
                Text(verbatim: entry.label)
            }
        }
    }
}

/// ChartFigure is one number a callout shows: what it is, when the callout
/// shows more than one, and the color of its series, when it has one.
struct ChartFigure: Identifiable {
    var label: String?
    let valueText: String
    var color: Color?
    var id: String { "\(label ?? "")\(valueText)" }
}

/// ChartCallout is what a touched mark says: when or what it is, and its
/// numbers, as Health and Stocks show above their charts. It sits on the
/// grouped background with a hairline edge, so it stands apart from the
/// list row it covers in both light and dark mode.
struct ChartCallout: View {
    let title: String
    let figures: [ChartFigure]

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: title)
                .font(.caption)
                .foregroundStyle(.secondary)
            HStack(alignment: .top, spacing: 12) {
                ForEach(figures) { figure in
                    VStack(alignment: .leading, spacing: 0) {
                        if let label = figure.label {
                            HStack(spacing: 4) {
                                if let color = figure.color { ChartSwatch(color: color) }
                                Text(verbatim: label)
                            }
                            .font(.caption)
                            .foregroundStyle(.secondary)
                        }
                        Text(verbatim: figure.valueText)
                            .font(.subheadline.weight(.semibold))
                            .monospacedDigit()
                            .foregroundStyle(.primary)
                    }
                }
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
        .background(Color(.systemGroupedBackground), in: .rect(cornerRadius: 10))
        .overlay {
            RoundedRectangle(cornerRadius: 10).strokeBorder(Color(.separator), lineWidth: 0.5)
        }
        .accessibilityElement(children: .combine)
    }
}

/// ChartSelectionRule is the line a touched x value draws through the
/// plot, behind the marks, with its callout above the plot. The callout may
/// rise over what sits above the chart in its row, as a header or a legend,
/// which is where Health puts it; at the sides it stays inside the chart.
struct ChartSelectionRule<Value: Plottable, Callout: View>: ChartContent {
    let value: Value
    @ViewBuilder let callout: () -> Callout

    var body: some ChartContent {
        RuleMark(x: .value("Selected", value))
            .foregroundStyle(Color(.tertiaryLabel))
            .lineStyle(StrokeStyle(lineWidth: 1))
            .zIndex(-1)
            .annotation(position: .top, spacing: 2, overflowResolution: .init(x: .fit(to: .chart), y: .disabled)) {
                callout()
            }
    }
}
