import Foundation

/// The charts a card can carry and the times it states on a clock of its own
/// (docs/planning/feed-cards-execplan.md): a series over time with a
/// reference line and markers, or bars, each with a line saying where and when
/// the numbers were read.

/// AgentFeedSeriesRole is what a line of a series chart stands for.
public enum AgentFeedSeriesRole: String, Sendable {
    /// What was read.
    case measured
    /// A line to compare with, such as last month or the average.
    case reference
    /// What is expected ahead.
    case projection
}

/// AgentFeedChartKind is how a chart is drawn.
public enum AgentFeedChartKind: String, Sendable {
    case series
    case bars
}

/// AgentFeedChartSeries is one line of a series chart.
public struct AgentFeedChartSeries: Codable, Hashable, Sendable {
    public let seriesName: String
    /// measured, reference or projection; see `seriesRole`.
    public let seriesRoleName: String
    /// One number for each label of the axis, or nil where there is none.
    public let values: [Double?]

    public init(seriesName: String, seriesRoleName: String = "measured", values: [Double?]) {
        self.seriesName = seriesName
        self.seriesRoleName = seriesRoleName
        self.values = values
    }

    /// A role this app does not know is drawn as measured.
    public var seriesRole: AgentFeedSeriesRole { AgentFeedSeriesRole(rawValue: seriesRoleName) ?? .measured }

    private enum CodingKeys: String, CodingKey {
        case seriesName, values
        case seriesRoleName = "seriesRole"
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        seriesName = (try? container.decodeIfPresent(String.self, forKey: .seriesName)) ?? ""
        let roleName = (try? container.decodeIfPresent(String.self, forKey: .seriesRoleName)) ?? ""
        seriesRoleName = roleName.isEmpty ? "measured" : roleName
        values = (try? container.decodeIfPresent([Double?].self, forKey: .values)) ?? []
    }
}

/// AgentFeedChartMarker is a labeled vertical rule on one of the axis labels.
public struct AgentFeedChartMarker: Codable, Hashable, Sendable {
    /// One of the chart's axis labels.
    public let markerAt: String
    public let markerLabel: String

    public init(markerAt: String, markerLabel: String) {
        self.markerAt = markerAt
        self.markerLabel = markerLabel
    }
}

/// AgentFeedChartBar is one bar of a bars chart.
public struct AgentFeedChartBar: Codable, Hashable, Sendable {
    public let barLabel: String
    public let barValue: Double

    public init(barLabel: String, barValue: Double) {
        self.barLabel = barLabel
        self.barValue = barValue
    }
}

/// AgentFeedChart is one chart of a card.
public struct AgentFeedChart: Codable, Hashable, Sendable {
    /// series or bars; see `chartKind`.
    public let chartKindName: String
    public let chartTitle: String
    /// What the numbers count, such as "USD" or "steps".
    public let chartUnit: String
    /// One line saying where and when the numbers were read.
    public let provenance: String
    /// The axis of a series chart, in order.
    public let xLabels: [String]
    public let series: [AgentFeedChartSeries]
    public let markers: [AgentFeedChartMarker]
    /// The bars of a bars chart, largest first.
    public let bars: [AgentFeedChartBar]

    public init(
        chartKindName: String = "series", chartTitle: String = "", chartUnit: String = "", provenance: String = "", xLabels: [String] = [],
        series: [AgentFeedChartSeries] = [], markers: [AgentFeedChartMarker] = [], bars: [AgentFeedChartBar] = []
    ) {
        self.chartKindName = chartKindName
        self.chartTitle = chartTitle
        self.chartUnit = chartUnit
        self.provenance = provenance
        self.xLabels = xLabels
        self.series = series
        self.markers = markers
        self.bars = bars
    }

    private enum CodingKeys: String, CodingKey {
        case chartTitle, provenance, xLabels, series, markers, bars
        case chartKindName = "chartKind"
        case chartUnit = "unit"
    }

    /// Every field may be missing or null.
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        chartKindName = (try? container.decodeIfPresent(String.self, forKey: .chartKindName)) ?? "series"
        chartTitle = (try? container.decodeIfPresent(String.self, forKey: .chartTitle)) ?? ""
        chartUnit = (try? container.decodeIfPresent(String.self, forKey: .chartUnit)) ?? ""
        provenance = (try? container.decodeIfPresent(String.self, forKey: .provenance)) ?? ""
        xLabels = (try? container.decodeIfPresent([String].self, forKey: .xLabels)) ?? []
        series = (try? container.decodeIfPresent([AgentFeedChartSeries?].self, forKey: .series))?.compactMap { $0 } ?? []
        markers = (try? container.decodeIfPresent([AgentFeedChartMarker?].self, forKey: .markers))?.compactMap { $0 } ?? []
        bars = (try? container.decodeIfPresent([AgentFeedChartBar?].self, forKey: .bars))?.compactMap { $0 } ?? []
    }

    /// A kind this app does not know is drawn as a series.
    public var chartKind: AgentFeedChartKind { AgentFeedChartKind(rawValue: chartKindName) ?? .series }

    /// Whether there is anything to draw: a series chart needs a line with a
    /// number in it, a bars chart a bar.
    public var isDrawable: Bool {
        switch chartKind {
        case .series: series.contains { line in line.values.contains { $0 != nil } } && xLabels.count > 1
        case .bars: !bars.isEmpty
        }
    }

    /// The chart's numbers written with its unit, as a person says them.
    public func valueText(_ value: Double) -> String {
        let number = value.formatted(.number.precision(.fractionLength(0...2)))
        return chartUnit.isEmpty ? number : "\(number) \(chartUnit)"
    }
}

/// AgentFeedZone names the zone of a time a card states with its own offset,
/// when that is not the zone the phone is in.
public enum AgentFeedZone {
    /// The seconds east of UTC an RFC 3339 time states ("Z" is 0); nil when it
    /// states none.
    public static func offsetSeconds(in text: String) -> Int? {
        guard let timeStart = text.firstIndex(where: { $0 == "T" || $0 == "t" }) else { return nil }
        let rest = text[text.index(after: timeStart)...]
        if rest.hasSuffix("Z") || rest.hasSuffix("z") { return 0 }
        guard let signIndex = rest.lastIndex(where: { $0 == "+" || $0 == "-" }) else { return nil }
        let parts = rest[rest.index(after: signIndex)...].split(separator: ":")
        guard parts.count == 2, let hours = Int(parts[0]), let minutes = Int(parts[1]) else { return nil }
        let seconds = hours * 3600 + minutes * 60
        return rest[signIndex] == "-" ? -seconds : seconds
    }

    /// "UTC", "UTC+2" or "UTC-3:30".
    public static func name(offsetSeconds: Int) -> String {
        guard offsetSeconds != 0 else { return "UTC" }
        let sign = offsetSeconds < 0 ? "-" : "+"
        let absolute = abs(offsetSeconds)
        let hours = absolute / 3600
        let minutes = absolute % 3600 / 60
        return minutes == 0 ? "UTC\(sign)\(hours)" : "UTC\(sign)\(hours):" + (minutes < 10 ? "0" : "") + "\(minutes)"
    }

    /// The offset of a time that is not the phone's own at that moment; nil
    /// when the time states none, or states the phone's.
    public static func foreignOffsetSeconds(of text: String?, deviceZone: TimeZone = .current) -> Int? {
        guard let text, let offset = offsetSeconds(in: text), let date = ServerTime.date(text) else { return nil }
        return offset == deviceZone.secondsFromGMT(for: date) ? nil : offset
    }

    /// The zone to say beside a time ("UTC+9"); nil when the phone's own.
    public static func foreignName(of text: String?, deviceZone: TimeZone = .current) -> String? {
        foreignOffsetSeconds(of: text, deviceZone: deviceZone).map { name(offsetSeconds: $0) }
    }
}
