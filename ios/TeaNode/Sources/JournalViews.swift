import SwiftUI
import TeaNodeKit
import UIKit

// The Journal: the person's days, newest first, in weeks. A week has its
// headline as a row that opens the week; a day opens the day. The menu under
// the title opens the current week, month or year, the calendar button
// chooses a day, the pencil writes about today, and the search field finds the
// days and periods that hold some words.

/// JournalScreen is the Journal's first screen and the stack that its days
/// and periods are pushed on.
struct JournalScreen: View {
    let connection: Connection
    @State private var model: JournalModel
    @State private var search: JournalSearchModel
    @State private var path: [JournalDestination] = []
    @State private var isChoosingDay = false
    @State private var writing: JournalWriteRequest?
    @Environment(AppModel.self) private var app

    init(connection: Connection) {
        self.connection = connection
        _model = State(initialValue: JournalModel(connection: connection))
        _search = State(initialValue: JournalSearchModel(connection: connection))
    }

    var body: some View {
        NavigationStack(path: $path) {
            content
                .navigationTitle(JournalText.title)
                .navigationBarTitleDisplayMode(.large)
                .searchable(text: $search.query, prompt: Text("Search the journal"))
                .task(id: search.searchWords) { await search.search() }
                .toolbarTitleMenu { zoomMenu }
                .toolbar {
                    MoreBackButton()
                    ToolbarItemGroup(placement: .topBarTrailing) {
                        Button {
                            isChoosingDay = true
                        } label: {
                            Label("Choose a day", systemImage: "calendar")
                        }
                        Button {
                            writeAboutToday()
                        } label: {
                            Label("Write today", systemImage: "square.and.pencil")
                        }
                    }
                }
                .navigationDestination(for: JournalDestination.self) { destination in
                    destinationScreen(destination)
                }
        }
        .sheet(isPresented: $isChoosingDay) {
            JournalDayPickerSheet(today: model.shownToday, firstDay: model.firstDay) { chosenDay in
                path.append(.day(chosenDay))
            }
        }
        .sheet(item: $writing) { request in
            JournalWriteSheet(connection: connection, request: request) {
                await model.refreshToday()
            }
        }
        .toast(done: $model.doneText, problem: $model.problemText)
        .toast(done: .constant(nil), problem: $search.problemText)
        .task {
            await model.load()
            #if DEBUG
                openDemoScreen()
            #endif
        }
        .onChange(of: app.openedJournalDay, initial: true) { _, openedDay in
            guard let openedDay else { return }
            app.openedJournalDay = nil
            path = openedDay.isEmpty ? [] : [.day(openedDay)]
        }
        // Coming back from a day, where the words or the mood may have changed.
        .onChange(of: path.isEmpty) { _, isEmpty in
            if isEmpty { Task { await model.refreshToday() } }
        }
    }

    // MARK: Zoom

    /// The menu under the title: this screen is the days, and the others open
    /// the week, month or year that holds today.
    private var zoomMenu: some View {
        Picker(
            "Show",
            selection: Binding(
                get: { JournalZoom.days },
                set: { zoom in
                    guard zoom != .days else { return }
                    path.append(JournalDestination.zoomed(zoom, onDay: model.shownToday))
                })
        ) {
            ForEach(JournalZoom.allCases) { zoom in
                Text(zoom.title).tag(zoom)
            }
        }
    }

    // MARK: Screens

    @ViewBuilder
    private func destinationScreen(_ destination: JournalDestination) -> some View {
        switch destination {
        case .day(let day):
            JournalDayScreen(connection: connection, day: day, today: model.shownToday, weekStartsOn: model.weekStartsOn, onZoom: zoom)
        case .period(let periodKind, let day):
            JournalPeriodScreen(
                connection: connection, periodKind: periodKind, anchorDay: day, today: model.shownToday, weekStartsOn: model.weekStartsOn,
                onZoom: zoom, onOpenDay: { openedDay in path.append(.day(openedDay)) })
        }
    }

    /// Replaces the screen on top with the same date at another zoom, so that
    /// going back leaves the screen it came from.
    private func zoom(_ zoom: JournalZoom, onDay day: String) {
        guard !path.isEmpty else { return }
        path[path.count - 1] = JournalDestination.zoomed(zoom, onDay: day)
    }

    private func writeAboutToday() {
        writing = JournalWriteRequest(target: .day(model.shownToday), words: model.todayDay?.words)
    }

    // MARK: Content

    @ViewBuilder
    private var content: some View {
        if let searchWords = search.searchWords {
            JournalSearchResults(model: search, searchWords: searchWords)
        } else if let loadFailedText = model.loadFailedText {
            LoadFailedView(problemText: loadFailedText) { await model.load() }
        } else if !model.hasLoaded {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            List {
                todaySection
                ForEach(model.weekGroups) { group in
                    weekSection(group)
                }
                if model.days.isEmpty {
                    Section {
                        Text("Days appear here as TeaNode sees them. Write about today to start your journal.")
                            .foregroundStyle(.secondary)
                    }
                }
            }
            .refreshable { await model.load(isRefreshing: true) }
            .readableListWidth()
        }
    }

    private var todaySection: some View {
        Section {
            JournalTodayCard(
                today: model.shownToday, day: model.todayDay, setMood: { level in Task { await model.setTodayMood(level) } },
                write: writeAboutToday)
        }
    }

    private func weekSection(_ group: JournalWeekGroup) -> some View {
        Section {
            NavigationLink(value: JournalDestination.period(.week, group.weekStart)) {
                JournalWeekRow(headline: model.weekHeadlines[group.weekStart] ?? "")
            }
            ForEach(group.days) { summary in
                NavigationLink(value: JournalDestination.day(summary.day)) {
                    JournalDayRow(summary: summary, connection: connection)
                }
                .task { await model.loadMore(after: summary) }
            }
        } header: {
            Text(verbatim: JournalText.weekTitle(weekStart: group.weekStart))
        }
    }

    #if DEBUG
        /// The screens a screenshot names: `-TeaNodeScreen journal-day` (the full
        /// day), `journal-day-work` (the same day from its work, with Details and
        /// the work chat folded), `journal-day-words` (a day with words only),
        /// `journal-day-shopping` (the same day from its trips to the shops),
        /// `journal-write` (the write sheet over today), `journal-week`,
        /// `journal-month`, `journal-search` (words typed in the search field).
        private func openDemoScreen() {
            guard DemoServer.isRequested, path.isEmpty else { return }
            switch DemoServer.screen {
            case "journal-day", "journal-day-work", "journal-day-shopping": path = [.day(JournalDemo.fullDay)]
            case "journal-search": search.query = JournalDemo.searchQuery
            case "journal-day-words": path = [.day(JournalDemo.wordsDay)]
            case "journal-week": path = [.period(.week, JournalDemo.fullDay)]
            case "journal-month": path = [.period(.month, JournalDemo.fullDay)]
            case "journal-write": writing = JournalWriteRequest(target: .day(JournalDemo.fullDay), words: JournalDemo.fullDayWords)
            default: break
            }
        }
    #endif
}

/// JournalSearchResults is what the Journal shows while words are typed in
/// its search field: the days and periods that hold them, newest first, a page
/// at a time. A hit opens its day or its period.
struct JournalSearchResults: View {
    let model: JournalSearchModel
    /// The words typed, as they are searched for.
    let searchWords: String

    var body: some View {
        if let failedText = model.failedText, model.searchedQuery == searchWords {
            LoadFailedView(problemText: failedText) { await model.search(isDebounced: false) }
        } else if model.hits.isEmpty, model.searchedQuery != searchWords {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if model.hits.isEmpty {
            ContentUnavailableView {
                Label("Nothing matches", systemImage: "magnifyingglass")
            } description: {
                Text("Nothing in your journal has “\(searchWords)”. Try other words, or part of a word.")
            }
        } else {
            List {
                Section {
                    ForEach(model.hits) { hit in
                        NavigationLink(value: JournalDestination.of(hit: hit)) {
                            JournalSearchHitRow(hit: hit)
                        }
                        .task { await model.loadMore(after: hit) }
                    }
                    if model.isLoadingMore {
                        ProgressView().frame(maxWidth: .infinity)
                    }
                } header: {
                    Text("Results for “\(model.searchedQuery)”")
                }
            }
        }
    }
}

/// JournalSearchHitRow is a hit: the day or period it is, its headline in the
/// writing face, and the words around the match.
struct JournalSearchHitRow: View {
    let hit: JournalSearchHit

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: JournalText.searchHitTitle(hit))
                .font(.headline)
            if !hit.headline.isEmpty {
                Text(verbatim: hit.headline)
                    .fontDesign(.serif)
                    .lineLimit(2)
            }
            if !hit.snippet.isEmpty {
                Text(verbatim: hit.snippet)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .lineLimit(3)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// JournalTodayCard is today at the top of the timeline: the weather and what
/// the day has held so far, the five faces to say how it is going, and the one
/// button that writes about it.
struct JournalTodayCard: View {
    let today: String
    let day: JournalDay?
    let setMood: (JournalMoodLevel) -> Void
    let write: () -> Void
    @Environment(AppModel.self) private var app

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            // The date goes under the title when the two do not fit on a line.
            ViewThatFits(in: .horizontal) {
                HStack(alignment: .firstTextBaseline) {
                    Text("Today").font(.headline)
                    Spacer()
                    Text(verbatim: JournalText.longDayText(today))
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                VStack(alignment: .leading, spacing: 2) {
                    Text("Today").font(.headline)
                    Text(verbatim: JournalText.longDayText(today))
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
            if let soFarText {
                Text(verbatim: soFarText)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            JournalMoodPicker(chosenLevel: day?.mood?.level, choose: setMood)
            Button(action: write) {
                Text("Write about today")
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
        }
        .padding(.vertical, 4)
    }

    /// "61° and clear. 2 meetings, 3,880 steps so far."
    private var soFarText: String? {
        var sentences: [String] = []
        if let weather = day?.weather {
            let name = JournalText.weatherName(weather.kind)
            let temperature = JournalText.temperatureText(celsius: weather.highCelsius, measurementSystem: app.measurementSystem)
            sentences.append(name.isEmpty ? temperature : String(localized: "\(temperature) and \(name)."))
        }
        var counted: [String] = []
        if let facts = day?.facts {
            if facts.meetingCount > 0 { counted.append(String(localized: "\(facts.meetingCount) meetings")) }
            if facts.stepCount > 0 { counted.append(String(localized: "\(facts.stepCount) steps so far")) }
        }
        if !counted.isEmpty { sentences.append(counted.joined(separator: ", ") + ".") }
        return sentences.isEmpty ? nil : sentences.joined(separator: " ")
    }
}

/// JournalWeekRow is the row at the top of a week that opens it: the week's
/// headline in the writing face, or the plain words when it has none yet.
struct JournalWeekRow: View {
    let headline: String

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            if headline.isEmpty {
                Text("Open the week")
            } else {
                Text(verbatim: headline)
                    .fontDesign(.serif)
                Text("Read the week")
                    .font(.subheadline)
                    .foregroundStyle(.tint)
            }
        }
    }
}

/// JournalDayRow is a day in the timeline: its date, what the day was in a
/// line or two, its mood and numbers, and the photo that stands for it.
struct JournalDayRow: View {
    let summary: JournalDaySummary
    let connection: Connection
    @Environment(AppModel.self) private var app

    var body: some View {
        HStack(spacing: 12) {
            VStack(spacing: 0) {
                Text(verbatim: JournalText.weekdayText(summary.day))
                    .font(.caption2.weight(.semibold))
                    .textCase(.uppercase)
                    .foregroundStyle(.secondary)
                Text(verbatim: JournalText.dayNumberText(summary.day))
                    .font(.title3.weight(.semibold))
                    .monospacedDigit()
            }
            .frame(minWidth: 40)
            VStack(alignment: .leading, spacing: 3) {
                headlineText
                    .fontDesign(.serif)
                    .lineLimit(2)
                HStack(spacing: 6) {
                    if let level = summary.mood?.level {
                        Circle()
                            .fill(JournalText.moodColor(level))
                            .frame(width: 9, height: 9)
                            .accessibilityHidden(true)
                    }
                    Text(verbatim: detailText)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }
            Spacer(minLength: 0)
            if let photoId = summary.shownPhotoId {
                JournalPhotoImage(photoId: photoId, connection: connection)
                    .frame(width: 52, height: 52)
                    .clipShape(RoundedRectangle(cornerRadius: 10))
                    .accessibilityHidden(true)
            }
        }
        .frame(minHeight: 52)
        .accessibilityElement(children: .combine)
    }

    /// The headline the agent wrote, else the start of the person's words,
    /// else nothing to say yet.
    private var headlineText: Text {
        if !summary.headline.isEmpty { return Text(verbatim: summary.headline) }
        if !summary.wordsExcerpt.isEmpty { return Text(verbatim: summary.wordsExcerpt) }
        return Text("Nothing written yet").foregroundStyle(.secondary)
    }

    /// The mood's word, beside the temperature and the day's most telling
    /// number.
    private var detailText: String {
        var parts: [String] = []
        if let level = summary.mood?.level { parts.append(JournalText.moodName(level)) }
        let facts = JournalText.factsLine(of: summary, measurementSystem: app.measurementSystem)
        if !facts.isEmpty { parts.append(facts) }
        return parts.joined(separator: ", ")
    }
}

/// JournalPhotoImage is the small preview of a photo, filling the space it is
/// given.
struct JournalPhotoImage: View {
    let photoId: String
    let connection: Connection
    @State private var picture: UIImage?

    init(photoId: String, connection: Connection) {
        self.photoId = photoId
        self.connection = connection
        _picture = State(
            initialValue: PhotoImages.cachedPicture(path: JournalPhoto.smallPreviewPath(photoId: photoId), accountKey: connection.accountKey))
    }

    var body: some View {
        Color(.tertiarySystemFill)
            .overlay {
                if let picture {
                    Image(uiImage: picture)
                        .resizable()
                        .scaledToFill()
                }
            }
            .clipped()
            .task(id: photoId) {
                if picture == nil {
                    picture = await PhotoImages.picture(path: JournalPhoto.smallPreviewPath(photoId: photoId), connection: connection)
                }
            }
    }
}

/// JournalDayPickerSheet chooses a day to open, from the first day that has
/// anything to today.
struct JournalDayPickerSheet: View {
    let today: String
    /// The earliest day with anything in it, empty for none.
    let firstDay: String
    let open: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var chosenDate: Date

    init(today: String, firstDay: String, open: @escaping (String) -> Void) {
        self.today = today
        self.firstDay = firstDay
        self.open = open
        _chosenDate = State(initialValue: JournalDays.date(today) ?? Date())
    }

    private var latestDate: Date { JournalDays.date(today) ?? Date() }

    /// The first day with anything in it, or ten years back; never later than
    /// the latest day, which a range could not hold.
    private var earliestDate: Date {
        let tenYearsBack = Calendar.current.date(byAdding: .year, value: -10, to: latestDate) ?? latestDate
        return min(JournalDays.date(firstDay) ?? tenYearsBack, latestDate)
    }

    var body: some View {
        NavigationStack {
            DatePicker("Day", selection: $chosenDate, in: earliestDate...latestDate, displayedComponents: .date)
                .datePickerStyle(.graphical)
                .padding(.horizontal)
                .frame(maxHeight: .infinity, alignment: .top)
                .navigationTitle("Choose a day")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Cancel") { dismiss() }
                    }
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Open") {
                            open(JournalDays.text(chosenDate))
                            dismiss()
                        }
                    }
                }
        }
        .presentationDetents([.medium, .large])
    }
}

/// JournalNotAllowedView is what the Journal says to an account that may not
/// use it: that is a permission, not a service the server has turned off.
struct JournalNotAllowedView: View {
    var body: some View {
        ContentUnavailableView {
            Label("The journal is not available to you", systemImage: AppSection.journal.systemImage)
        } description: {
            Text("Your account is not allowed to keep a journal. Whoever manages your TeaNode server can allow it.")
        }
    }
}
