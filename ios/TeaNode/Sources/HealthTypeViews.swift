import SwiftUI
import TeaNodeKit

// One kind of Health data: its chart over a range, the average and total, and
// under them the person's own recent readings.

/// HealthTypeScreen is the chart of one kind of data, by day, week or month.
struct HealthTypeScreen: View {
    let model: HealthModel
    let type: HealthTypeView
    @State private var rangeChoice = HealthRange.thirtyDays
    @State private var periodChoice = HealthPeriod.day
    /// Whether the person chose the period, which the range then leaves be.
    @State private var isPeriodChosen = false
    @State private var plan: HealthChartPlan?
    @State private var loadFailedText: String?
    /// Said under the chart when the range was shortened to what the server
    /// answers.
    @State private var rangeCutText: String?
    /// Where a finger is on the chart.
    @State private var selectedKey: String?
    @State private var isConfirmingErase = false
    @Environment(\.dismiss) private var dismiss

    /// What the chart was last asked for, so that a change of either choice
    /// reads again.
    private struct ChartRequest: Hashable {
        let rangeChoice: HealthRange
        let periodChoice: HealthPeriod
    }

    private var name: String { model.name(of: type) }

    var body: some View {
        List {
            Section {
                Picker("Range", selection: $rangeChoice) {
                    Text("30 days").tag(HealthRange.thirtyDays)
                    Text("90 days").tag(HealthRange.ninetyDays)
                    Text(HealthWords.yearRangeTitle).tag(HealthRange.year)
                    Text("All").tag(HealthRange.all)
                }
                .pickerStyle(.segmented)
                Picker(HealthWords.periodTitle, selection: periodBinding) {
                    Text("Day").tag(HealthPeriod.day)
                    Text("Week").tag(HealthPeriod.week)
                    Text("Month").tag(HealthPeriod.month)
                }
                .pickerStyle(.segmented)
                chartBlock
            }
            figuresSection
            if model.isViewingOwn {
                HealthReadingsSection(model: model, type: type)
            }
        }
        .navigationTitle(name)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                if model.isViewingOwn {
                    eraseMenu
                }
            }
        }
        .task(id: ChartRequest(rangeChoice: rangeChoice, periodChoice: periodChoice)) { await load() }
        .onChange(of: rangeChoice) {
            selectedKey = nil
            if !isPeriodChosen { periodChoice = rangeChoice.defaultPeriod }
        }
        .onChange(of: periodChoice) { selectedKey = nil }
        .refreshable { await load() }
    }

    // MARK: The chart

    private var periodBinding: Binding<HealthPeriod> {
        Binding(
            get: { periodChoice },
            set: { chosen in
                isPeriodChosen = true
                periodChoice = chosen
            })
    }

    @ViewBuilder
    private var chartBlock: some View {
        if let plan {
            if plan.hasData {
                VStack(alignment: .leading, spacing: 4) {
                    headline(of: plan)
                        .opacity(selectedKey == nil ? 1 : 0)
                    HealthChartView(plan: plan, selectedKey: $selectedKey)
                        .padding(.top, 8)
                    if let rangeCutText {
                        Text(verbatim: rangeCutText)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
            } else {
                Text("Nothing was recorded in this range.").foregroundStyle(.secondary)
            }
        } else if let loadFailedText {
            Text(verbatim: loadFailedText).foregroundStyle(.secondary)
            Button("Try again") { Task { await load() } }
                .actionColor()
        } else {
            ProgressView().frame(maxWidth: .infinity)
        }
    }

    private func headline(of plan: HealthChartPlan) -> some View {
        let headlineRow = plan.figureRows.first
        return VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: headlineRow?.valueText ?? "")
                .font(.title.weight(.semibold))
                .monospacedDigit()
            Text(verbatim: headlineRow?.label ?? "")
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .combine)
    }

    /// The figures after the headline, as rows.
    @ViewBuilder
    private var figuresSection: some View {
        if let plan, plan.hasData, plan.figureRows.count > 1 {
            Section {
                ForEach(plan.figureRows.dropFirst()) { row in
                    LabeledContent(row.label) {
                        Text(verbatim: row.valueText).monospacedDigit()
                    }
                }
            }
        }
    }

    // MARK: Erasing

    private var eraseMenu: some View {
        Menu {
            Button("Erase \(name)", systemImage: "trash", role: .destructive) { isConfirmingErase = true }
                .tint(.red)
        } label: {
            Label("More", systemImage: "ellipsis.circle")
        }
        .confirmationDialog(
            Text(verbatim: String(localized: "health-erase-type-title", defaultValue: "Erase \(name)?")), isPresented: $isConfirmingErase,
            titleVisibility: .visible
        ) {
            Button("Erase \(name)", role: .destructive) {
                Task {
                    if await model.erase(healthType: type.healthType) { dismiss() }
                }
            }
        } message: {
            Text(
                "Erases every reading, day summary and route of \(name) that this server keeps. The iPhone app keeps sending new readings of it while Health is on. Anything your agent already learned stays in its memory until you remove it. This cannot be undone."
            )
        }
    }

    // MARK: Reading

    /// The most days a chart by this period can reach back: the server
    /// answers at most 400 periods, and a week the range starts inside counts
    /// as one of them, so a chart by week reaches back one fewer. A chart by
    /// month is not cut.
    private static func mostDays(of period: HealthPeriod) -> Int? {
        switch period {
        case .day: return 400
        case .week: return 399 * 7
        case .month: return nil
        }
    }

    /// The days the chart reaches over: the range back from today, or from the
    /// first day there is data, and whether that was shortened to what the
    /// server answers.
    private var window: (fromDay: String, toDay: String, mostPeriodCount: Int?) {
        let toDay = HealthDays.text(of: Date(), calendar: Calendar.current)
        var fromDay: String
        if rangeChoice == .all {
            fromDay = String((type.firstDay ?? HealthText.day(toDay, adding: -364)).prefix(10))
        } else {
            fromDay = HealthText.day(toDay, adding: 1 - rangeChoice.rawValue)
        }
        var mostPeriodCount: Int?
        if let mostDays = Self.mostDays(of: periodChoice) {
            let earliestDay = HealthText.day(toDay, adding: 1 - mostDays)
            if fromDay < earliestDay {
                fromDay = earliestDay
                mostPeriodCount = periodChoice == .week ? 399 : 400
            }
        }
        return (fromDay, toDay, mostPeriodCount)
    }

    private func load() async {
        let requestedPeriod = periodChoice
        let requestedWindow = window
        do {
            let summary = try await model.connection.healthSummary(
                healthType: type.healthType, fromDay: requestedWindow.fromDay, toDay: requestedWindow.toDay, period: requestedPeriod,
                ownerUserId: model.selectedUserId)
            if Task.isCancelled { return }
            plan = HealthChartPlan.make(
                summary: summary, type: type, period: requestedPeriod, fromDay: requestedWindow.fromDay, toDay: requestedWindow.toDay)
            rangeCutText = Self.rangeCutText(period: requestedPeriod, mostPeriodCount: requestedWindow.mostPeriodCount)
            loadFailedText = nil
        } catch {
            if Task.isCancelled { return }
            // A chart already drawn stays; only a first read that failed says so.
            if plan == nil { loadFailedText = error.loadProblemText }
        }
    }

    private static func rangeCutText(period: HealthPeriod, mostPeriodCount: Int?) -> String? {
        guard let mostPeriodCount else { return nil }
        if period == .week { return String(localized: "Only the latest \(mostPeriodCount) weeks are drawn.") }
        return String(localized: "Only the latest \(mostPeriodCount) days are drawn.")
    }
}

// MARK: Readings

/// HealthReadingsSection is the person's own recent readings of a kind, newest
/// first, a page at a time. Somebody sharing Health shares its charts, never
/// its readings, so this is not shown for them.
struct HealthReadingsSection: View {
    let model: HealthModel
    let type: HealthTypeView
    @State private var samples: [HealthSampleView] = []
    @State private var nextOffset: Int?
    @State private var hasLoaded = false
    @State private var isLoadingMore = false
    @State private var loadFailedText: String?

    nonisolated static let pageSize = 50

    var body: some View {
        Section {
            if hasLoaded, samples.isEmpty, loadFailedText == nil {
                Text("No readings of this kind yet.").foregroundStyle(.secondary)
            }
            ForEach(samples) { sample in
                readingRow(sample)
                    .onAppear {
                        if sample.id == samples.last?.id { Task { await loadMore() } }
                    }
            }
            if let loadFailedText {
                Text(verbatim: loadFailedText).foregroundStyle(.secondary)
                Button("Try again") { Task { await loadFirstPage() } }
                    .actionColor()
            }
        } header: {
            Text("Recent readings")
        }
        .task { await loadFirstPage() }
    }

    @ViewBuilder
    private func readingRow(_ sample: HealthSampleView) -> some View {
        if sample.hasRoute {
            NavigationLink(value: HealthDestination.route(HealthRouteRequest(sample: sample))) {
                HealthReadingRow(sample: sample, type: type)
            }
        } else {
            HealthReadingRow(sample: sample, type: type)
        }
    }

    private func loadFirstPage() async {
        do {
            let page = try await model.connection.healthSamples(healthType: type.healthType, limitCount: Self.pageSize, offset: 0)
            samples = page.samples
            nextOffset = page.nextOffset
            loadFailedText = nil
        } catch {
            if samples.isEmpty { loadFailedText = error.loadProblemText }
        }
        hasLoaded = true
    }

    private func loadMore() async {
        guard let offset = nextOffset, !isLoadingMore else { return }
        isLoadingMore = true
        defer { isLoadingMore = false }
        guard let page = try? await model.connection.healthSamples(healthType: type.healthType, limitCount: Self.pageSize, offset: offset) else { return }
        let knownIds = Set(samples.map(\.id))
        samples.append(contentsOf: page.samples.filter { !knownIds.contains($0.id) })
        nextOffset = page.nextOffset
    }
}

/// HealthReadingRow is one reading: what it says, and when and from where.
struct HealthReadingRow: View {
    let sample: HealthSampleView
    let type: HealthTypeView

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: HealthText.readingText(of: sample, type: type))
                .monospacedDigit()
            Text(verbatim: detailText)
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .combine)
    }

    /// "Oct 18, 2026 at 7:42 AM · Watch".
    private var detailText: String {
        let momentText = sample.startedDate.map { HealthText.momentText($0) } ?? sample.startedAt
        return sample.sourceName.isEmpty ? momentText : momentText + " · " + sample.sourceName
    }
}
