import SwiftUI
import TeaNodeKit

// A week, a month or a year of the journal: what TeaNode wrote about it, the
// person's looking back, the days in it (a week as a list, a month as a grid
// colored by mood, a year as its months), and its totals.

/// JournalPeriodScreen is a week, a month or a year. The buttons in the bar go
/// to the one before and the one after, and the menu under the title opens
/// the same date as a day or as another kind of period.
struct JournalPeriodScreen: View {
    let connection: Connection
    let periodKind: JournalPeriodKind
    let today: String
    let weekStartsOn: String
    let onZoom: (JournalZoom, String) -> Void
    let onOpenDay: (String) -> Void
    @State private var model: JournalPeriodModel
    @State private var writing: JournalWriteRequest?
    @State private var correcting: JournalCorrectionRequest?
    @State private var isShowingOriginal = false
    @Environment(AppModel.self) private var app

    init(
        connection: Connection, periodKind: JournalPeriodKind, anchorDay: String, today: String, weekStartsOn: String,
        onZoom: @escaping (JournalZoom, String) -> Void, onOpenDay: @escaping (String) -> Void
    ) {
        self.connection = connection
        self.periodKind = periodKind
        self.today = today
        self.weekStartsOn = weekStartsOn
        self.onZoom = onZoom
        self.onOpenDay = onOpenDay
        _model = State(initialValue: JournalPeriodModel(connection: connection, periodKind: periodKind, anchorDay: anchorDay))
    }

    /// The first day of the period shown, once it is read.
    private var shownStart: String {
        model.period?.periodStart
            ?? JournalDays.periodStart(of: model.anchorDay, periodKind: periodKind, weekStartsOn: weekStartsOn) ?? model.anchorDay
    }

    var body: some View {
        content
            .navigationTitle(JournalText.periodTitle(periodKind: periodKind, periodStart: shownStart))
            .navigationBarTitleDisplayMode(.inline)
            .toolbarTitleMenu { zoomMenu }
            .toolbar {
                ToolbarItemGroup(placement: .topBarTrailing) {
                    Button {
                        Task { await move(by: -1) }
                    } label: {
                        Label("Before", systemImage: "chevron.left")
                    }
                    Button {
                        Task { await move(by: 1) }
                    } label: {
                        Label("After", systemImage: "chevron.right")
                    }
                    .disabled(model.period?.isCurrent ?? true)
                }
            }
            .sheet(item: $writing) { request in
                JournalWriteSheet(connection: connection, request: request) {
                    await model.load(isQuiet: true)
                }
            }
            .sheet(item: $correcting) { request in
                JournalCorrectionSheet(connection: connection, request: request) {
                    await model.load(isQuiet: true)
                }
            }
            .toast(done: $model.doneText, problem: $model.problemText)
            .task { await model.load() }
            .task(id: model.isPending) { await model.readWhilePending() }
            .onChange(of: model.anchorDay) { isShowingOriginal = false }
    }

    private func move(by count: Int) async {
        guard let moved = JournalDays.movedPeriodStart(from: shownStart, periodKind: periodKind, by: count) else { return }
        await model.show(anchorDay: moved)
    }

    private var zoomMenu: some View {
        Picker(
            "Show",
            selection: Binding(
                get: { JournalZoom(periodKind: periodKind) },
                set: { zoom in
                    guard zoom != JournalZoom(periodKind: periodKind) else { return }
                    // A day is the period's last day that has been, so a screen
                    // for today is not left for a day in the future.
                    onZoom(zoom, zoomDay)
                })
        ) {
            ForEach(JournalZoom.allCases) { zoom in
                Text(zoom.title).tag(zoom)
            }
        }
    }

    /// The day to carry to another zoom: today when it is in the period, else
    /// the period's first day.
    private var zoomDay: String {
        guard let period = model.period else { return model.anchorDay }
        return period.isCurrent ? today : period.periodStart
    }

    @ViewBuilder
    private var content: some View {
        if let loadFailedText = model.loadFailedText {
            LoadFailedView(problemText: loadFailedText) { await model.load() }
        } else if let period = model.period {
            List {
                Group {
                    narrativeSection(period)
                    lookingBackSection(period)
                    daysSection(period)
                }
                Group {
                    moodSection(period)
                    totalsSection(period)
                    chaptersSection(period)
                    choicesSection(period)
                }
            }
            .refreshable { await model.load() }
        } else {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    // MARK: The story

    private var narrativeTitle: LocalizedStringKey {
        switch periodKind {
        case .week: "The week, as TeaNode saw it"
        case .month: "The month, as TeaNode saw it"
        case .year: "The year, as TeaNode saw it"
        }
    }

    private var emptyNarrativeText: String {
        switch periodKind {
        case .week: String(localized: "This week has no story yet.")
        case .month: String(localized: "This month has no story yet.")
        case .year: String(localized: "This year has no story yet.")
        }
    }

    private func narrativeSection(_ period: JournalPeriod) -> some View {
        Section {
            JournalNarrativeRows(
                narrative: period.narrative, emptyText: emptyNarrativeText, canWrite: true, isBusy: model.isBusy,
                open: { record in JournalRecordOpener.open(JournalRecordOpening.of(record: record), in: app) },
                correct: { correcting = JournalCorrectionRequest(target: model.target) },
                writeAgain: { Task { await model.writeAgain() } },
                removeCorrection: { index in Task { await model.removeCorrection(at: index) } })
        } header: {
            Text(narrativeTitle)
        } footer: {
            if period.narrative?.hasNarrative == true {
                Text("TeaNode reads your journal, words included, to write this.")
            }
        }
    }

    // MARK: Looking back

    private func lookingBackSection(_ period: JournalPeriod) -> some View {
        let words = period.lookingBack
        return Section {
            JournalWordsRows(words: words, isShowingOriginal: $isShowingOriginal)
            Button(
                words?.hasOriginal == true ? String(localized: "Edit looking back") : String(localized: "Write looking back"),
                systemImage: "square.and.pencil"
            ) {
                writing = JournalWriteRequest(target: model.target, words: words)
            }
            .actionColor()
        } header: {
            Text("Looking back")
        } footer: {
            Text("Your own words about this stretch of days, if you want them.")
        }
    }

    // MARK: The days

    @ViewBuilder
    private func daysSection(_ period: JournalPeriod) -> some View {
        switch periodKind {
        case .week:
            weekDaysSection(period)
        case .month:
            monthGridSection(period)
        case .year:
            yearMonthsSection(period)
        }
    }

    private func weekDaysSection(_ period: JournalPeriod) -> some View {
        Section {
            ForEach(period.days) { summary in
                NavigationLink(value: JournalDestination.day(summary.day)) {
                    JournalDayRow(summary: summary, connection: connection)
                }
            }
        } header: {
            Text("The days")
        }
    }

    // MARK: A month

    private func monthGridSection(_ period: JournalPeriod) -> some View {
        let rows = JournalDays.monthRows(monthStart: period.periodStart, weekStartsOn: weekStartsOn)
        let summaries = Dictionary(period.days.map { ($0.day, $0) }, uniquingKeysWith: { first, _ in first })
        return Section {
            Grid(horizontalSpacing: 4, verticalSpacing: 6) {
                GridRow {
                    ForEach(Array(weekdayHeaders(period).enumerated()), id: \.offset) { _, header in
                        Text(verbatim: header)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .frame(maxWidth: .infinity)
                    }
                }
                ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                    GridRow {
                        ForEach(Array(row.enumerated()), id: \.offset) { _, day in
                            dayCell(day, summary: day.flatMap { summaries[$0] })
                        }
                    }
                }
            }
            .padding(.vertical, 4)
        } header: {
            Text("The days")
        } footer: {
            Text("Each day shows its first photo, and is colored by how it was.")
        }
    }

    /// The names of the weekdays in the order the month's columns have.
    private func weekdayHeaders(_ period: JournalPeriod) -> [String] {
        let firstWeekStart = JournalDays.weekStart(of: period.periodStart, weekStartsOn: weekStartsOn) ?? period.periodStart
        return JournalDays.weekDays(startingOn: firstWeekStart).map { JournalText.weekdayInitialText($0) }
    }

    @ViewBuilder
    private func dayCell(_ day: String?, summary: JournalDaySummary?) -> some View {
        if let day {
            Button {
                onOpenDay(day)
            } label: {
                dayCellFace(day, summary: summary)
            }
            .buttonStyle(.plain)
            .disabled(day > today)
            .foregroundStyle(day > today ? Color.secondary : Color.primary)
            .accessibilityLabel(JournalText.longDayText(day))
            .accessibilityValue(moodName(of: summary))
        } else {
            Color.clear.frame(minHeight: 40)
        }
    }

    /// A day of the month: its number on the first photo of the day, or on
    /// the color of its mood when it has no photo. A day with both has its
    /// mood as the edge of the photo.
    private func dayCellFace(_ day: String, summary: JournalDaySummary?) -> some View {
        let photoId = summary?.shownPhotoId
        let moodTint = moodColor(of: summary)
        return ZStack {
            if let photoId {
                JournalPhotoImage(photoId: photoId, connection: connection)
                    .frame(height: 40)
                    .clipShape(RoundedRectangle(cornerRadius: 8))
                    .overlay {
                        if let moodTint {
                            RoundedRectangle(cornerRadius: 8).strokeBorder(moodTint, lineWidth: 3)
                        }
                    }
            } else if let moodTint {
                Circle().fill(moodTint.opacity(0.4))
            }
            Text(verbatim: JournalText.dayNumberText(day))
                .font(.callout.weight(day == today ? .bold : .regular))
                .monospacedDigit()
                .padding(.horizontal, photoId == nil ? 0 : 6)
                .background {
                    if photoId != nil { Capsule().fill(.ultraThinMaterial) }
                }
        }
        .frame(maxWidth: .infinity, minHeight: 40)
        .contentShape(Rectangle())
    }

    private func moodColor(of summary: JournalDaySummary?) -> Color? {
        guard let level = summary?.mood?.level else { return nil }
        return JournalText.moodColor(level)
    }

    private func moodName(of summary: JournalDaySummary?) -> String {
        guard let level = summary?.mood?.level else { return "" }
        return JournalText.moodName(level)
    }

    // MARK: A year

    private func yearMonthsSection(_ period: JournalPeriod) -> some View {
        Section {
            ForEach(period.months) { month in
                NavigationLink(value: JournalDestination.period(.month, month.periodStart)) {
                    VStack(alignment: .leading, spacing: 6) {
                        Text(verbatim: JournalText.monthText(month.periodStart)).font(.headline)
                        if !month.headline.isEmpty {
                            Text(verbatim: month.headline)
                                .fontDesign(.serif)
                                .lineLimit(2)
                        }
                        JournalMoodStrip(dayMoods: month.dayMoods)
                    }
                }
            }
        } header: {
            Text("The months")
        }
    }

    // MARK: Mood and numbers

    @ViewBuilder
    private func moodSection(_ period: JournalPeriod) -> some View {
        if period.moodCounts.contains(where: { $0 > 0 }) {
            Section {
                JournalMoodBar(moodCounts: period.moodCounts)
            } header: {
                Text("How the days were")
            }
        }
    }

    @ViewBuilder
    private func totalsSection(_ period: JournalPeriod) -> some View {
        let rows = totalRows(period.totals)
        if !rows.isEmpty {
            Section {
                ForEach(rows, id: \.title) { row in
                    LabeledContent(row.title) {
                        Text(verbatim: row.value).monospacedDigit()
                    }
                }
            } header: {
                Text("In numbers")
            }
        }
    }

    /// The totals that are not nothing, each with its words.
    private func totalRows(_ totals: JournalFacts?) -> [(title: String, value: String)] {
        guard let totals else { return [] }
        var rows: [(title: String, value: String)] = []
        if totals.photoCount > 0 { rows.append((String(localized: "Photos"), JournalText.countText(totals.photoCount))) }
        if totals.placeCount > 0 { rows.append((String(localized: "Places"), JournalText.countText(totals.placeCount))) }
        if totals.walkedMeters > 0 { rows.append((String(localized: "Walked"), JournalText.distanceText(meters: totals.walkedMeters, measurementSystem: app.measurementSystem))) }
        if totals.stepCount > 0 { rows.append((String(localized: "Steps"), JournalText.countText(totals.stepCount))) }
        if totals.sleepMinutes > 0 { rows.append((String(localized: "Sleep"), JournalText.durationText(minutes: totals.sleepMinutes))) }
        if totals.workoutMinutes > 0 { rows.append((String(localized: "Workouts"), JournalText.durationText(minutes: totals.workoutMinutes))) }
        let workText = JournalText.workText(minutes: totals.codingMinutes, projectCount: totals.projectCount)
        if !workText.isEmpty { rows.append((String(localized: "Work"), workText)) }
        if totals.meetingCount > 0 { rows.append((String(localized: "Meetings"), JournalText.countText(totals.meetingCount))) }
        if totals.mailReceivedCount > 0 { rows.append((String(localized: "Mail received"), JournalText.countText(totals.mailReceivedCount))) }
        if totals.spentMinorUnits > 0 {
            rows.append((String(localized: "Spent"), JournalText.moneyText(minorUnits: totals.spentMinorUnits, currency: totals.spentCurrency)))
        }
        return rows
    }

    // MARK: A year's chapters, and what was chosen

    @ViewBuilder
    private func chaptersSection(_ period: JournalPeriod) -> some View {
        if !period.chapters.isEmpty {
            Section {
                ForEach(Array(period.chapters.enumerated()), id: \.offset) { _, chapter in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: chapter.title).font(.headline)
                        Text(verbatim: JournalText.dayRangeText(from: chapter.fromDay, until: chapter.untilDay))
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                        if !chapter.detail.isEmpty {
                            Text(verbatim: chapter.detail).fontDesign(.serif)
                        }
                    }
                    .accessibilityElement(children: .combine)
                }
            } header: {
                Text("Chapters")
            }
        }
    }

    @ViewBuilder
    private func choicesSection(_ period: JournalPeriod) -> some View {
        let links = period.choices.filter { link in
            guard !link.isHidden, let record = link.record else { return false }
            return !record.isUnavailable
        }
        if !links.isEmpty {
            Section {
                ForEach(links) { link in
                    if let record = link.record {
                        JournalRecordRow(
                            symbolName: JournalText.symbol(forRecordKind: record.recordKind), title: record.title, detail: link.linkReason,
                            opening: JournalRecordOpening.of(record: record))
                    }
                }
            } header: {
                Text("Moments")
            }
        }
    }
}
