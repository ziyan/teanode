import Charts
import SwiftUI
import TeaNodeKit

/// StorageWords is the words and figures the storage screen is written in.
enum StorageWords {
    /// A size in 1024-based units, as the system writes a file's size.
    static func bytes(_ sizeBytes: Int64) -> String {
        sizeBytes.formatted(.byteCount(style: .binary))
    }

    /// A day as YYYY-MM-DD, in UTC, written for the person: "Oct 9".
    static func day(_ dayText: String) -> String {
        guard let date = date(dayText) else { return dayText }
        return date.formatted(.dateTime.month(.abbreviated).day())
    }

    static func fullDay(_ dayText: String) -> String {
        guard let date = date(dayText) else { return dayText }
        return date.formatted(date: .abbreviated, time: .omitted)
    }

    private static func date(_ dayText: String) -> Date? {
        let parts = dayText.split(separator: "-").compactMap { Int($0) }
        guard parts.count == 3 else { return nil }
        return Calendar.current.date(from: DateComponents(year: parts[0], month: parts[1], day: parts[2], hour: 12))
    }

    static func breakdown(_ breakdown: StorageBreakdown) -> LocalizedStringKey {
        switch breakdown {
        case .area: "Database by area"
        case .kind: "Files by kind"
        case .person: "Files by person"
        }
    }

    static func disk(_ diskLabel: String) -> String {
        switch diskLabel {
        case "dataDirectory": String(localized: "The data directory disk")
        case "fileDirectory": String(localized: "The file storage disk")
        case "objectStore": String(localized: "The object store")
        default: diskLabel
        }
    }

    static func area(_ areaKey: String) -> String {
        switch areaKey {
        case "mail": String(localized: "Mail")
        case "memory": String(localized: "Memory and knowledge")
        case "conversations": String(localized: "Conversations")
        case "photos": String(localized: "Photos")
        case "recordings": String(localized: "Recordings")
        case "finance": String(localized: "Finance")
        case "location": String(localized: "Location")
        case "health": String(localized: "Health")
        case "journal": String(localized: "Journal")
        case "search": String(localized: "Search indexes and vectors")
        case "other": String(localized: "Everything else")
        default: areaKey
        }
    }

    static func fileKind(_ kindKey: String) -> String {
        switch kindKey {
        case "photoOriginal": String(localized: "Photo originals")
        case "photoPreview": String(localized: "Photo previews")
        case "recordingOriginal": String(localized: "Recording originals")
        case "recordingDerived": String(localized: "Recording audio")
        case "mail": String(localized: "Mail messages")
        case "agentAttachment": String(localized: "Agent attachments")
        default: kindKey
        }
    }

    /// What a chart's series is called: an area, a kind of file, the database
    /// or the files, or the rest.
    static func series(_ seriesKey: String, breakdown: StorageBreakdown) -> String {
        switch seriesKey {
        case StorageUsage.restSeriesKey: String(localized: "The rest")
        case StorageUsage.databaseSeriesKey: String(localized: "Database")
        case StorageUsage.filesSeriesKey: String(localized: "Files")
        default: breakdown == .kind ? fileKind(seriesKey) : area(seriesKey)
        }
    }

    static let fileKinds = ["photoOriginal", "photoPreview", "recordingOriginal", "recordingDerived", "mail", "agentAttachment"]
}

/// StorageUsageView is what the database and the files hold, for the
/// operator: the disks the server measured and the day each fills, the
/// sizes by area, kind of file or person, the days sampled drawn, and what
/// the recent growth comes to. A table stays a table on a phone, scrolling
/// sideways.
struct StorageUsageView: View {
    let connection: Connection
    @State private var breakdown = StorageBreakdown.area
    @State private var range = AgentUsageRange.lastThirtyDays()
    @State private var usage: StorageUsage?
    @State private var isLoading = true
    @State private var problemText: String?

    private var queryId: String {
        "\(breakdown.rawValue) \(range.firstDay.timeIntervalSince1970) \(range.lastDay.timeIntervalSince1970)"
    }

    var body: some View {
        List {
            if let usage {
                if !usage.fillingDisks.isEmpty {
                    Section {
                        ForEach(usage.fillingDisks) { disk in
                            Label {
                                Text("\(StorageWords.disk(disk.diskLabel)) is projected to fill on \(StorageWords.fullDay(disk.projectedFullOn ?? "")), in about \(Int((disk.daysUntilFull ?? 0).rounded())) days.")
                            } icon: {
                                Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.orange)
                            }
                        }
                    }
                }
                Section("Disks") {
                    ForEach(usage.disks) { disk in
                        diskRow(disk, growth: usage.growth)
                    }
                }
            }
            Section {
                DatePicker("From", selection: $range.firstDay, in: ...range.lastDay, displayedComponents: .date)
                DatePicker("To", selection: $range.lastDay, in: range.firstDay..., displayedComponents: .date)
                Picker("By", selection: $breakdown) {
                    ForEach(StorageBreakdown.allCases, id: \.self) { candidate in
                        Text(StorageWords.breakdown(candidate)).tag(candidate)
                    }
                }
            }
            if let usage {
                Section {
                    LabeledContent("Database", value: StorageWords.bytes(usage.databaseSizeBytes))
                    LabeledContent("Files", value: StorageWords.bytes(usage.fileSizeBytes))
                    LabeledContent("Total", value: StorageWords.bytes(usage.totalSizeBytes))
                }
                Section {
                    chart(usage)
                }
                if usage.growth.isGrowthProjectable {
                    projection(usage.growth)
                }
                if usage.usageRows.isEmpty {
                    Section {
                        Text("Nothing stored.").foregroundStyle(Color(.secondaryLabel))
                    }
                } else {
                    Section {
                        table(usage)
                    }
                }
            }
        }
        .navigationTitle("Disk usage")
        .overlay {
            if usage == nil && isLoading { ProgressView() }
        }
        .task(id: queryId) { await load() }
        .refreshable { await load() }
        .toast(done: .constant(nil), problem: $problemText)
    }

    private func diskRow(_ disk: StorageDisk, growth: StorageGrowth) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                Text(verbatim: StorageWords.disk(disk.diskLabel)).foregroundStyle(Color(.label))
                Spacer()
                if let fullOn = disk.projectedFullOn {
                    Text("Full on \(StorageWords.fullDay(fullOn))")
                        .font(.footnote)
                        .foregroundStyle(disk.isProjectedFull ? Color.orange : Color(.secondaryLabel))
                } else if disk.isDiskMeasured {
                    Text(growth.isGrowthProjectable ? "Not growing" : "Not enough history yet")
                        .font(.footnote)
                        .foregroundStyle(Color(.secondaryLabel))
                }
            }
            if let usedFraction = disk.usedFraction, let freeBytes = disk.diskFreeBytes, let totalBytes = disk.diskTotalBytes {
                ProgressView(value: usedFraction)
                    .tint(disk.isProjectedFull ? Color.orange : Color.accentColor)
                Text("\(StorageWords.bytes(freeBytes)) free of \(StorageWords.bytes(totalBytes))")
                    .font(.footnote)
                    .foregroundStyle(Color(.secondaryLabel))
            } else {
                Text("Not measured")
                    .font(.footnote)
                    .foregroundStyle(Color(.secondaryLabel))
            }
        }
        .accessibilityElement(children: .combine)
    }

    /// The days sampled, a stacked column a day: the largest areas or kinds
    /// apart and the rest together, or the database and the files for
    /// people, who are not sampled.
    @ViewBuilder
    private func chart(_ usage: StorageUsage) -> some View {
        let points = usage.chartPoints
        if points.isEmpty {
            Text("Nothing sampled in this range.").foregroundStyle(Color(.secondaryLabel))
        } else {
            let seriesKeys = seriesOrder(points)
            let names = seriesKeys.map { StorageWords.series($0, breakdown: breakdown) }
            let colors = seriesKeys.enumerated().map { index, key in
                key == StorageUsage.restSeriesKey ? ChartPalette.restColor : ChartPalette.seriesColor(at: index)
            }
            VStack(alignment: .leading, spacing: 8) {
                Text("Storage over time")
                    .font(.subheadline)
                    .foregroundStyle(Color(.secondaryLabel))
                Chart(points) { point in
                    BarMark(
                        x: .value("Day", StorageWords.day(point.sampledOn)),
                        y: .value("Size", point.sizeBytes)
                    )
                    .foregroundStyle(by: .value("Series", StorageWords.series(point.seriesKey, breakdown: breakdown)))
                }
                .chartForegroundStyleScale(domain: names, range: colors)
                .chartLegend(.hidden)
                .chartYAxis {
                    AxisMarks { axisValue in
                        AxisGridLine()
                        AxisValueLabel {
                            if let sizeBytes = axisValue.as(Int64.self) {
                                Text(verbatim: StorageWords.bytes(sizeBytes))
                            }
                        }
                    }
                }
                .frame(height: 200)
                ChartKey(entries: zip(names, colors).map { ChartKeyEntry(label: $0, color: $1) })
            }
        }
    }

    /// The series in the order they are listed in the legend: the largest
    /// of the newest day first, and the rest last.
    private func seriesOrder(_ points: [StorageChartPoint]) -> [String] {
        var order: [String] = []
        for point in points where !order.contains(point.seriesKey) { order.append(point.seriesKey) }
        return order.filter { $0 != StorageUsage.restSeriesKey } + order.filter { $0 == StorageUsage.restSeriesKey }
    }

    private func projection(_ growth: StorageGrowth) -> some View {
        Section("At this rate") {
            LabeledContent("Per day", value: StorageWords.bytes(Int64(growth.growthBytesPerDay)))
            LabeledContent("Now", value: StorageWords.bytes(growth.currentSizeBytes))
            LabeledContent("In 30 days", value: StorageWords.bytes(growth.projectedSize30DaysBytes))
            LabeledContent("In 90 days", value: StorageWords.bytes(growth.projectedSize90DaysBytes))
            LabeledContent("In 365 days", value: StorageWords.bytes(growth.projectedSize365DaysBytes))
            if !growth.areFilesCounted {
                Text("Files in an object store are not counted in the growth.")
                    .font(.footnote)
                    .foregroundStyle(Color(.secondaryLabel))
            }
        }
    }

    private func rowLabel(_ usageRow: StorageUsageRow) -> String {
        switch breakdown {
        case .area: StorageWords.area(usageRow.rowKey)
        case .kind: StorageWords.fileKind(usageRow.rowKey)
        case .person: usageRow.rowKey.isEmpty ? String(localized: "Not tied to a person") : usageRow.rowLabel
        }
    }

    private func table(_ usage: StorageUsage) -> some View {
        ScrollView(.horizontal) {
            Grid(alignment: .trailing, horizontalSpacing: 16, verticalSpacing: 8) {
                GridRow {
                    Text(StorageWords.breakdown(breakdown)).gridColumnAlignment(.leading)
                    Text("Size")
                    if breakdown == .area { Text("Tables") }
                    if breakdown == .person {
                        ForEach(StorageWords.fileKinds, id: \.self) { kindKey in
                            Text(verbatim: StorageWords.fileKind(kindKey))
                        }
                    }
                }
                .font(.caption.weight(.semibold))
                .foregroundStyle(Color(.secondaryLabel))
                Divider()
                ForEach(usage.usageRows) { usageRow in
                    GridRow {
                        Text(verbatim: rowLabel(usageRow)).foregroundStyle(Color(.label))
                        Text(verbatim: StorageWords.bytes(usageRow.rowSizeBytes))
                        if breakdown == .area { Text(verbatim: usageRow.rowTableCount.formatted()) }
                        if breakdown == .person {
                            ForEach(StorageWords.fileKinds, id: \.self) { kindKey in
                                let kindBytes = usageRow.rowFileSizes.first { $0.sizeKey == kindKey }?.sizeBytes ?? 0
                                Text(verbatim: kindBytes > 0 ? StorageWords.bytes(kindBytes) : "")
                            }
                        }
                    }
                    .font(.callout.monospacedDigit())
                    .foregroundStyle(Color(.label))
                }
            }
            .padding(.vertical, 4)
        }
    }

    private func load() async {
        isLoading = true
        defer { isLoading = false }
        do {
            usage = try await connection.storageUsage(breakdown: breakdown, range: range)
        } catch is CancellationError {
            return
        } catch {
            problemText = error.localizedDescription
        }
    }
}
