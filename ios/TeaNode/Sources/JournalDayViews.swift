import SwiftUI
import TeaNodeKit

// One day of the journal: how it was, the person's words, the story TeaNode
// wrote from what the day held, and what the day held, in the order it
// happened and by kind.

/// JournalDayScreen is one day. The buttons in the bar go to the day before
/// and the day after, and the menu under the title opens the week, month or
/// year the day is in.
struct JournalDayScreen: View {
    let connection: Connection
    let today: String
    let weekStartsOn: String
    let onZoom: (JournalZoom, String) -> Void
    @State private var model: JournalDayModel
    @State private var writing: JournalWriteRequest?
    @State private var correcting: JournalCorrectionRequest?
    /// The photo of the day that was tapped, shown in the viewer over the day.
    @State private var openedPhoto: JournalOpenedPhoto?
    /// Whether the person's original is shown in place of the touched-up
    /// version.
    @State private var isShowingOriginal = false
    /// The folded parts of the day that were opened ("work-details", "knowledge"),
    /// kept here so that reading the day again does not fold them.
    @State private var expandedParts: Set<String> = []
    /// The long lists that show all their rows and not the first few.
    @State private var fullLists: Set<String> = []
    @Environment(AppModel.self) private var app

    init(connection: Connection, day: String, today: String, weekStartsOn: String, onZoom: @escaping (JournalZoom, String) -> Void) {
        self.connection = connection
        self.today = today
        self.weekStartsOn = weekStartsOn
        self.onZoom = onZoom
        _model = State(initialValue: JournalDayModel(connection: connection, day: day))
    }

    private var isLatestDay: Bool { model.day >= today }

    var body: some View {
        content
            .navigationTitle(JournalText.dayTitle(model.day))
            .navigationSubtitle(JournalText.daySubtitle(model.day, today: today))
            .navigationBarTitleDisplayMode(.inline)
            .toolbarTitleMenu { zoomMenu }
            .toolbar {
                ToolbarItemGroup(placement: .topBarTrailing) {
                    Button {
                        Task { await move(by: -1) }
                    } label: {
                        Label("The day before", systemImage: "chevron.left")
                    }
                    Button {
                        Task { await move(by: 1) }
                    } label: {
                        Label("The day after", systemImage: "chevron.right")
                    }
                    .disabled(isLatestDay)
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
            .fullScreenCover(item: $openedPhoto) { photo in
                JournalPhotoViewer(photoIds: viewerPhotoIds, openedPhotoId: photo.photoId, connection: connection)
            }
            .toast(done: $model.doneText, problem: $model.problemText)
            .task { await model.load() }
            // While the touched-up words or the story are being written, the
            // day is read again every few seconds.
            .task(id: model.isPending) { await model.readWhilePending() }
            .onChange(of: model.day) {
                isShowingOriginal = false
                fullLists = []
            }
    }

    /// The photos the viewer pages through: the ones the day's strip shows.
    private var viewerPhotoIds: [String] {
        (model.journalDay?.photos ?? []).prefix(Self.stripPhotoCount).map(\.photoId)
    }

    private static let stripPhotoCount = 24

    /// Opens a record the day mentions. A photo of the day opens in the viewer
    /// over it, and the person stays on the day; anything else goes to the
    /// part of the app that shows it.
    private func openRecord(_ opening: JournalRecordOpening, in journalDay: JournalDay) {
        if case .photo(let photoId) = opening, journalDay.photos.prefix(Self.stripPhotoCount).contains(where: { $0.photoId == photoId }) {
            openedPhoto = JournalOpenedPhoto(photoId: photoId)
            return
        }
        JournalRecordOpener.open(opening, in: app)
    }

    // MARK: Folded parts and long lists

    /// Whether a folded part of the day is open, and the way to open it.
    private func expansion(_ part: String) -> Binding<Bool> {
        Binding(
            get: { expandedParts.contains(part) },
            set: { isExpanded in
                if isExpanded { expandedParts.insert(part) } else { expandedParts.remove(part) }
            })
    }

    /// The rows of a long list to draw: the first few, and all once asked.
    private func shown<Element>(_ rows: [Element], list: String, limit: Int = JournalRows.limit) -> [Element] {
        JournalRows.shown(rows, limit: limit, isShowingAll: fullLists.contains(list))
    }

    /// The row that shows the rest of a long list, or takes it back.
    @ViewBuilder
    private func showAllRow<Element>(_ rows: [Element], list: String, limit: Int = JournalRows.limit) -> some View {
        if JournalRows.hasMore(rows, limit: limit) {
            Button {
                if fullLists.contains(list) { fullLists.remove(list) } else { fullLists.insert(list) }
            } label: {
                if fullLists.contains(list) {
                    Text("Show fewer")
                } else {
                    Text("Show all \(rows.count)")
                }
            }
        }
    }

    private func move(by dayCount: Int) async {
        guard let moved = JournalDays.adding(dayCount, to: model.day) else { return }
        await model.show(day: moved)
    }

    private var zoomMenu: some View {
        Picker(
            "Show",
            selection: Binding(
                get: { JournalZoom.days },
                set: { zoom in
                    guard zoom != .days else { return }
                    onZoom(zoom, model.day)
                })
        ) {
            ForEach(JournalZoom.allCases) { zoom in
                Text(zoom.title).tag(zoom)
            }
        }
    }

    @ViewBuilder
    private var content: some View {
        if let loadFailedText = model.loadFailedText {
            LoadFailedView(problemText: loadFailedText) { await model.load() }
        } else if let journalDay = model.journalDay {
            ScrollViewReader { proxy in
                List {
                    // In groups: a result builder takes ten views at most.
                    Group {
                        moodSection(journalDay)
                        wordsSection(journalDay)
                        narrativeSection(journalDay)
                        workSection(journalDay)
                        workChatSection(journalDay)
                    }
                    Group {
                        chosenSection(journalDay)
                        mailSection(journalDay)
                        if !journalDay.photos.isEmpty { photosSection(journalDay) }
                        if !journalDay.moments.isEmpty { momentsSection(journalDay) }
                        if !journalDay.visits.isEmpty { placesSection(journalDay) }
                        talkedSection(journalDay)
                    }
                    Group {
                        if !journalDay.recordings.isEmpty { recordingsSection(journalDay) }
                        if let health = journalDay.health, hasHealth(health) { healthSection(health) }
                        if !journalDay.transactions.isEmpty { moneySection(journalDay) }
                        if !journalDay.events.isEmpty || !journalDay.reminders.isEmpty { calendarSection(journalDay) }
                        if !journalDay.shoppingTrips.isEmpty { shoppingSection(journalDay) }
                        knowledgeSection(journalDay)
                    }
                }
                .refreshable { await model.load() }
                // `-TeaNodeScreen journal-day-work` photographs the day from its work.
                .task(id: model.hasLoaded) {
                    #if DEBUG
                        guard model.hasLoaded, DemoServer.isRequested, DemoServer.screen == "journal-day-work" else { return }
                        try? await Task.sleep(for: .milliseconds(400))
                        proxy.scrollTo(Self.workSectionId, anchor: .top)
                    #endif
                }
            }
        } else {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    /// Names the work section, for the screenshot that starts there.
    private static let workSectionId = "journal-work"

    // MARK: How the day was

    private func moodSection(_ journalDay: JournalDay) -> some View {
        Section {
            JournalMoodPicker(chosenLevel: journalDay.mood?.level) { level in
                Task { await model.setMood(level) }
            }
            if let mood = journalDay.mood {
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: moodText(mood))
                    if mood.isFromHealth {
                        Text("From Apple Health")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
            }
            if let weather = journalDay.weather {
                Label {
                    Text(verbatim: weatherText(weather))
                } icon: {
                    Image(systemName: JournalText.weatherSymbol(weather.kind))
                }
                .foregroundStyle(.primary)
            }
        } header: {
            Text("How was the day?")
        }
    }

    /// "Good: tired, but the release is out".
    private func moodText(_ mood: JournalMood) -> String {
        guard let level = mood.level else { return mood.moodWords }
        let name = JournalText.moodName(level)
        return mood.moodWords.isEmpty ? name : name + ": " + mood.moodWords
    }

    /// "58° / 46°, rain, Riverside".
    private func weatherText(_ weather: JournalWeather) -> String {
        let line = JournalText.weatherLine(weather, measurementSystem: app.measurementSystem)
        return weather.placeName.isEmpty ? line : line + ", " + weather.placeName
    }

    // MARK: The person's words

    private func wordsSection(_ journalDay: JournalDay) -> some View {
        let words = journalDay.words
        return Section {
            JournalWordsRows(words: words, isShowingOriginal: $isShowingOriginal)
            Button(
                words?.hasOriginal == true ? String(localized: "Edit your words") : String(localized: "Write about this day"),
                systemImage: "square.and.pencil"
            ) {
                writing = JournalWriteRequest(target: .day(model.day), words: words)
            }
            .actionColor()
        } header: {
            Text("Your words")
        }
    }

    // MARK: The story

    private func narrativeSection(_ journalDay: JournalDay) -> some View {
        let narrative = journalDay.narrative
        let emptyText = journalDay.isToday
            ? String(localized: "TeaNode writes the story of a day after it ends. You can ask for the day so far.")
            : String(localized: "This day has no story yet.")
        // Today's story is the day so far, written when asked.
        let writeNowTitle = journalDay.isToday ? String(localized: "Write today so far") : String(localized: "Write now")
        return Section {
            JournalNarrativeRows(
                narrative: narrative, emptyText: emptyText, canWrite: true, writeNowTitle: writeNowTitle, isBusy: model.isBusy,
                open: { record in openRecord(JournalRecordOpening.of(record: record), in: journalDay) },
                correct: { correcting = JournalCorrectionRequest(target: .day(model.day)) },
                writeAgain: { Task { await model.writeAgain() } },
                removeCorrection: { index in Task { await model.removeCorrection(at: index) } })
        } header: {
            Text("The day, as TeaNode saw it")
        } footer: {
            if narrative?.hasNarrative == true || narrative?.isBeyondDepth == true {
                Text("TeaNode reads your journal, words included, to write this.")
            }
        }
    }

    // MARK: Photos

    private func photosSection(_ journalDay: JournalDay) -> some View {
        Section {
            ScrollView(.horizontal) {
                LazyHStack(spacing: 4) {
                    ForEach(journalDay.photos.prefix(Self.stripPhotoCount)) { photo in
                        Button {
                            openedPhoto = JournalOpenedPhoto(photoId: photo.photoId)
                        } label: {
                            JournalPhotoImage(photoId: photo.photoId, connection: connection)
                                .frame(width: 104, height: 104)
                                .clipShape(RoundedRectangle(cornerRadius: 10))
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel("Photo")
                    }
                }
            }
            .scrollIndicators(.hidden)
            .listRowInsets(EdgeInsets(top: 8, leading: 16, bottom: 8, trailing: 16))
            JournalOpenRow(isOpenable: true, open: { JournalRecordOpener.openPhotos(ofDay: journalDay.day, in: app) }) {
                Text("Open in Photos")
            }
        } header: {
            Text("Photos, \(journalDay.photoTotal)")
        }
    }

    // MARK: Through the day

    private func momentsSection(_ journalDay: JournalDay) -> some View {
        Section {
            ForEach(shown(journalDay.moments, list: "moments", limit: JournalRows.momentLimit)) { moment in
                JournalMomentRow(moment: moment, openRecord: { opening in openRecord(opening, in: journalDay) })
            }
            showAllRow(journalDay.moments, list: "moments", limit: JournalRows.momentLimit)
        } header: {
            Text("Through the day")
        }
    }

    // MARK: Places

    private func placesSection(_ journalDay: JournalDay) -> some View {
        Section {
            if JournalVisitsMap.hasPlaces(journalDay.visits) {
                JournalVisitsMap(visits: journalDay.visits)
                    .listRowInsets(EdgeInsets())
            }
            JournalOpenRow(isOpenable: true, open: { app.show(path: "/location?day=\(journalDay.day)") }) {
                Text(verbatim: placesSummary(journalDay))
            }
            ForEach(shown(journalDay.visits, list: "visits")) { visit in
                LabeledContent {
                    Text(verbatim: JournalText.timeRangeText(from: visit.arrivedAt, until: visit.departedAt))
                        .monospacedDigit()
                } label: {
                    Text(verbatim: visit.placeName)
                }
            }
            showAllRow(journalDay.visits, list: "visits")
        } header: {
            Text("Places")
        }
    }

    /// "5 places, 3.4 mi walked".
    private func placesSummary(_ journalDay: JournalDay) -> String {
        let placeCount = journalDay.facts?.placeCount ?? journalDay.visits.count
        let places = String(localized: "\(placeCount) places")
        guard let walkedMeters = journalDay.facts?.walkedMeters, walkedMeters >= 100 else { return places }
        return places + ", " + String(localized: "\(JournalText.distanceText(meters: walkedMeters, measurementSystem: app.measurementSystem)) walked")
    }

    // MARK: What was chosen

    /// The choices and pins of kinds the sections below do not show already:
    /// a chosen mail is among the mail that mattered, and a chosen document
    /// is among the work.
    private func chosenLinks(_ journalDay: JournalDay) -> [JournalLink] {
        let choices = journalDay.choices.filter { link in
            guard !link.isHidden, let record = link.record, !record.isUnavailable else { return false }
            return record.recordKind != "mail" && record.recordKind != "agent_document"
        }
        let pins = journalDay.pins.filter { link in
            guard let record = link.record else { return false }
            return !record.isUnavailable
        }
        return choices + pins
    }

    @ViewBuilder
    private func chosenSection(_ journalDay: JournalDay) -> some View {
        let links = chosenLinks(journalDay)
        if !links.isEmpty {
            Section {
                ForEach(shown(links, list: "chosen")) { link in
                    if let record = link.record {
                        JournalRecordRow(
                            symbolName: JournalText.symbol(forRecordKind: record.recordKind), title: record.title, detail: link.linkReason,
                            opening: JournalRecordOpening.of(record: record), openRecord: { opening in openRecord(opening, in: journalDay) })
                    }
                }
                showAllRow(links, list: "chosen")
            } header: {
                Text("Chosen for this day")
            }
        }
    }

    // MARK: Work and mail

    /// The documents the agent chose for the day and the person did not hide.
    private func chosenDocuments(_ journalDay: JournalDay) -> [JournalDocument] {
        let hiddenIds = Set(
            journalDay.choices.filter { $0.isHidden && $0.record?.recordKind == "agent_document" }.compactMap { $0.record?.recordId })
        return journalDay.documents.filter { $0.isChosen && !hiddenIds.contains($0.documentId) }
    }

    /// What the day's work was: a row for each project, and under them,
    /// folded, the coding sessions and the documents the agent chose.
    @ViewBuilder
    private func workSection(_ journalDay: JournalDay) -> some View {
        let chosen = chosenDocuments(journalDay)
        if !journalDay.workProjects.isEmpty || !journalDay.codingSessions.isEmpty || !chosen.isEmpty {
            Section {
                ForEach(journalDay.workProjects) { project in
                    JournalWorkProjectRow(project: project)
                }
                if !journalDay.codingSessions.isEmpty || !chosen.isEmpty {
                    DisclosureGroup(isExpanded: expansion("work-details")) {
                        ForEach(shown(journalDay.codingSessions, list: "sessions")) { session in
                            JournalRecordRow(
                                symbolName: "chevron.left.forwardslash.chevron.right", title: session.title, detail: codingDetail(session))
                        }
                        showAllRow(journalDay.codingSessions, list: "sessions")
                        ForEach(shown(chosen, list: "documents")) { document in
                            JournalRecordRow(
                                symbolName: "doc.text", title: document.title,
                                detail: document.linkReason.isEmpty ? document.sourceName : document.linkReason)
                        }
                        showAllRow(chosen, list: "documents")
                    } label: {
                        Text("Details")
                    }
                }
            } header: {
                Text("What you worked on")
            } footer: {
                if let workText = workSummary(journalDay) { Text(verbatim: workText) }
            }
            .id(Self.workSectionId)
        }
    }

    /// "6 h 10 min, 3 projects".
    private func workSummary(_ journalDay: JournalDay) -> String? {
        let text = JournalText.workText(minutes: journalDay.facts?.codingMinutes ?? 0, projectCount: journalDay.workProjects.count)
        return text.isEmpty ? nil : text
    }

    /// The work chat and documents the agent left out as routine, counted,
    /// and folded away until asked for.
    @ViewBuilder
    private func workChatSection(_ journalDay: JournalDay) -> some View {
        let chosenIds = Set(chosenDocuments(journalDay).map(\.documentId))
        let others = journalDay.documents.filter { !chosenIds.contains($0.documentId) }
        let leftOutCount = max(journalDay.documentTotal - chosenIds.count, others.count)
        if leftOutCount > 0 {
            Section {
                if others.isEmpty {
                    Text("\(leftOutCount) more were left out as routine.")
                        .foregroundStyle(.secondary)
                } else {
                    DisclosureGroup(isExpanded: expansion("work-chat")) {
                        ForEach(shown(others, list: "work-chat")) { document in
                            JournalRecordRow(symbolName: "doc.text", title: document.title, detail: document.sourceName)
                        }
                        showAllRow(others, list: "work-chat")
                    } label: {
                        HStack {
                            Text("Work chat and documents")
                            Spacer(minLength: 8)
                            Text(leftOutCount, format: .number)
                                .foregroundStyle(.secondary)
                                .monospacedDigit()
                        }
                    }
                }
            }
        }
    }

    /// "Claude Code, 3 h".
    private func codingDetail(_ session: JournalCodingSession) -> String {
        var parts = [session.assistant]
        if let started = ServerTime.date(session.startedAt), let ended = ServerTime.date(session.endedAt) {
            let minutes = Int(ended.timeIntervalSince(started) / 60)
            if minutes > 0 { parts.append(JournalText.durationText(minutes: minutes)) }
        }
        let stretchCount = session.stretchCount ?? 1
        if stretchCount > 1 { parts.append(String(localized: "\(stretchCount) stretches of work")) }
        return parts.joined(separator: ", ")
    }

    @ViewBuilder
    private func mailSection(_ journalDay: JournalDay) -> some View {
        let mattered = journalDay.mail.filter { $0.isChosen || $0.isImportant }
        if !mattered.isEmpty {
            Section {
                ForEach(shown(mattered, list: "mail")) { message in
                    JournalRecordRow(
                        symbolName: "envelope", title: message.subject, detail: mailDetail(message), opening: .mailItem(message.itemId))
                }
                showAllRow(mattered, list: "mail")
            } header: {
                Text("Mail that mattered")
            }
        }
    }

    private func mailDetail(_ message: JournalMail) -> String {
        var detail = message.linkReason.isEmpty ? message.fromName : message.fromName + ": " + message.linkReason
        let repeatCount = message.repeatCount ?? 1
        if repeatCount > 1 { detail += " " + String(localized: "(\(repeatCount) times)") }
        return detail
    }

    // MARK: Who and what the day talked with

    @ViewBuilder
    private func talkedSection(_ journalDay: JournalDay) -> some View {
        if !journalDay.conversations.isEmpty || !journalDay.chats.isEmpty {
            Section {
                ForEach(shown(journalDay.conversations, list: "conversations")) { conversation in
                    JournalOpenRow(isOpenable: true, open: { app.openedConversationId = conversation.conversationId }) {
                        conversationRow(title: conversationTitle(conversation), messageCount: conversation.messageCount)
                    }
                }
                showAllRow(journalDay.conversations, list: "conversations")
                ForEach(shown(journalDay.chats, list: "chats")) { chat in
                    conversationRow(title: chat.chatName, messageCount: chat.messageCount)
                }
                showAllRow(journalDay.chats, list: "chats")
            } header: {
                Text("Conversations")
            }
        }
    }

    /// The person's main conversation is named in their language, whatever
    /// the server called it.
    private func conversationTitle(_ conversation: JournalConversation) -> String {
        conversation.isMainChat ? String(localized: "Main chat") : conversation.title
    }

    private func conversationRow(title: String, messageCount: Int) -> some View {
        HStack(spacing: 12) {
            Image(systemName: "bubble.left")
                .frame(width: 24)
                .foregroundStyle(.secondary)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: title)
                Text("\(messageCount) messages")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private func recordingsSection(_ journalDay: JournalDay) -> some View {
        Section {
            ForEach(shown(journalDay.recordings, list: "recordings")) { recording in
                JournalRecordRow(
                    symbolName: "record.circle", title: recording.title, detail: recordingDetail(recording),
                    opening: .path("/recordings/\(recording.recordingId)"))
            }
            showAllRow(journalDay.recordings, list: "recordings")
        } header: {
            Text("Recordings")
        }
    }

    private func recordingDetail(_ recording: JournalRecording) -> String {
        let duration = JournalText.durationText(milliseconds: recording.durationMS)
        return recording.summary.isEmpty ? duration : duration + ": " + recording.summary
    }

    // MARK: Health

    private func hasHealth(_ health: JournalHealth) -> Bool {
        health.stepCount > 0 || health.sleepMinutes > 0 || !health.workouts.isEmpty || health.restingHeartRate > 0
    }

    private func healthSection(_ health: JournalHealth) -> some View {
        Section {
            if health.stepCount > 0 {
                LabeledContent("Steps") {
                    Text(health.stepCount, format: .number).monospacedDigit()
                }
            }
            if health.sleepMinutes > 0 {
                VStack(spacing: 8) {
                    LabeledContent("Sleep") {
                        Text(verbatim: JournalText.durationText(minutes: health.sleepMinutes))
                    }
                    if !health.sleepStages.isEmpty {
                        JournalSleepBar(stages: health.sleepStages)
                    }
                }
            }
            if health.restingHeartRate > 0 {
                LabeledContent("Resting heart rate") {
                    Text("\(Int(health.restingHeartRate.rounded())) bpm").monospacedDigit()
                }
            }
            ForEach(health.workouts) { workout in
                LabeledContent {
                    Text(verbatim: workoutDetail(workout)).monospacedDigit()
                } label: {
                    Text(verbatim: HealthWords.workoutName(workout.activityType))
                }
            }
        } header: {
            Text(verbatim: HealthWords.title)
        }
    }

    /// "32 min, 1.6 mi".
    private func workoutDetail(_ workout: JournalWorkout) -> String {
        let minutes = JournalText.durationText(minutes: workout.minutes)
        guard workout.meters > 0 else { return minutes }
        return minutes + ", " + JournalText.distanceText(meters: workout.meters, measurementSystem: app.measurementSystem)
    }

    // MARK: Money

    private func moneySection(_ journalDay: JournalDay) -> some View {
        Section {
            ForEach(shown(journalDay.transactions, list: "money")) { transaction in
                JournalOpenRow(isOpenable: true, open: { app.openedSection = .finance }) {
                    LabeledContent {
                        Text(verbatim: amountText(transaction)).monospacedDigit()
                    } label: {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: transaction.description)
                            if let time = transaction.transactedAt, !JournalText.timeText(time).isEmpty {
                                Text(verbatim: JournalText.timeText(time)).font(.subheadline).foregroundStyle(.secondary)
                            }
                        }
                    }
                }
            }
            showAllRow(journalDay.transactions, list: "money")
        } header: {
            Text("Money")
        }
    }

    /// Money spent is its amount, money received has a plus.
    private func amountText(_ transaction: JournalTransaction) -> String {
        let amount = JournalText.moneyText(minorUnits: abs(transaction.amountMinorUnits), currency: transaction.currency)
        return transaction.amountMinorUnits > 0 ? "+" + amount : amount
    }

    // MARK: Calendar

    private func calendarSection(_ journalDay: JournalDay) -> some View {
        Section {
            ForEach(shown(journalDay.events, list: "events")) { event in
                JournalRecordRow(
                    symbolName: "calendar", title: event.title, detail: eventDetail(event), opening: .part("calendar"))
            }
            showAllRow(journalDay.events, list: "events")
            ForEach(shown(journalDay.reminders, list: "reminders")) { reminder in
                JournalRecordRow(
                    symbolName: reminder.doneAt == nil ? "circle" : "checkmark.circle", title: reminder.title,
                    detail: reminder.doneAt == nil ? "" : String(localized: "Done"), opening: .part("calendar"))
            }
            showAllRow(journalDay.reminders, list: "reminders")
        } header: {
            Text("Calendar")
        }
    }

    /// "9:05 AM to 10:00 AM, Room 2", or "All day".
    private func eventDetail(_ event: JournalEvent) -> String {
        let when = event.isAllDay ? String(localized: "All day") : JournalText.timeRangeText(from: event.startsAt, until: event.endsAt)
        return event.location.isEmpty ? when : when + ", " + event.location
    }

    // MARK: Shopping

    private func shoppingSection(_ journalDay: JournalDay) -> some View {
        Section {
            ForEach(shown(journalDay.shoppingTrips, list: "shopping")) { trip in
                JournalRecordRow(
                    symbolName: "cart", title: trip.listNames.isEmpty ? String(localized: "Shopping trip") : trip.listNames.joined(separator: ", "),
                    detail: String(localized: "\(trip.boughtCount) of \(trip.itemCount) bought"))
            }
            showAllRow(journalDay.shoppingTrips, list: "shopping")
        } header: {
            Text("Shopping")
        }
    }

    // MARK: Knowledge

    /// The pages of the agent's memory with a fact from this day: one line
    /// saying how many, and the pages behind it.
    @ViewBuilder
    private func knowledgeSection(_ journalDay: JournalDay) -> some View {
        if !journalDay.memoryPages.isEmpty {
            Section {
                DisclosureGroup(isExpanded: expansion("knowledge")) {
                    ForEach(shown(journalDay.memoryPages, list: "knowledge")) { page in
                        JournalRecordRow(
                            symbolName: "doc.text", title: page.title, detail: page.path, opening: .path("/knowledge/\(page.path)"))
                    }
                    showAllRow(journalDay.memoryPages, list: "knowledge")
                } label: {
                    Text("TeaNode wrote or changed \(journalDay.memoryPages.count) memory pages from this day")
                }
            } header: {
                Text("Knowledge")
            }
        }
    }
}

/// JournalMomentRow is one thing the day held, with the time it happened.
struct JournalMomentRow: View {
    let moment: JournalMoment
    /// What a tap does, when the screen opens some records itself.
    var openRecord: ((JournalRecordOpening) -> Void)?
    @Environment(AppModel.self) private var app

    var body: some View {
        JournalOpenRow(isOpenable: opening.isOpenable, open: open) {
            HStack(spacing: 12) {
                Text(verbatim: JournalText.timeText(moment.at))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
                    .frame(width: 70, alignment: .leading)
                Image(systemName: JournalText.symbol(forRecordKind: moment.recordKind))
                    .frame(width: 24)
                    .foregroundStyle(.secondary)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: moment.title)
                    if !moment.detail.isEmpty {
                        Text(verbatim: moment.detail)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                }
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var opening: JournalRecordOpening { JournalRecordOpening.of(moment: moment) }

    private func open() {
        if let openRecord {
            openRecord(opening)
        } else {
            JournalRecordOpener.open(opening, in: app)
        }
    }
}
