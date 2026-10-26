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
    /// Whether the person's original is shown in place of the touched-up
    /// version.
    @State private var isShowingOriginal = false
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
            .navigationTitle(JournalText.longDayText(model.day))
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
            .toast(done: $model.doneText, problem: $model.problemText)
            .task { await model.load() }
            // While the touched-up words or the story are being written, the
            // day is read again every few seconds.
            .task(id: model.isPending) { await model.readWhilePending() }
            .onChange(of: model.day) { isShowingOriginal = false }
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
            List {
                // In groups: a result builder takes ten views at most.
                Group {
                    moodSection(journalDay)
                    wordsSection(journalDay)
                    narrativeSection(journalDay)
                    if !journalDay.photos.isEmpty { photosSection(journalDay) }
                    if !journalDay.moments.isEmpty { momentsSection(journalDay) }
                }
                Group {
                    if !journalDay.visits.isEmpty { placesSection(journalDay) }
                    chosenSection(journalDay)
                    workSection(journalDay)
                    mailSection(journalDay)
                    talkedSection(journalDay)
                }
                Group {
                    if !journalDay.recordings.isEmpty { recordingsSection(journalDay) }
                    if let health = journalDay.health, hasHealth(health) { healthSection(health) }
                    if !journalDay.transactions.isEmpty { moneySection(journalDay) }
                    if !journalDay.events.isEmpty || !journalDay.reminders.isEmpty { calendarSection(journalDay) }
                    if !journalDay.shoppingTrips.isEmpty { shoppingSection(journalDay) }
                }
            }
            .refreshable { await model.load() }
        } else {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

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
        let line = JournalText.weatherLine(weather)
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
            ? String(localized: "TeaNode writes the story of a day after it ends.") : String(localized: "This day has no story yet.")
        return Section {
            JournalNarrativeRows(
                narrative: narrative, emptyText: emptyText, canWrite: !journalDay.isToday, isBusy: model.isBusy,
                open: { record in JournalRecordOpener.open(JournalRecordOpening.of(record: record), in: app) },
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
                    ForEach(journalDay.photos.prefix(24)) { photo in
                        Button {
                            JournalRecordOpener.openPhotos(ofDay: journalDay.day, in: app)
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
        } header: {
            Text("Photos, \(journalDay.photoTotal)")
        }
    }

    // MARK: Through the day

    private func momentsSection(_ journalDay: JournalDay) -> some View {
        Section {
            ForEach(journalDay.moments) { moment in
                JournalMomentRow(moment: moment)
            }
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
            ForEach(journalDay.visits) { visit in
                LabeledContent {
                    Text(verbatim: JournalText.timeRangeText(from: visit.arrivedAt, until: visit.departedAt))
                        .monospacedDigit()
                } label: {
                    Text(verbatim: visit.placeName)
                }
            }
        } header: {
            Text("Places")
        }
    }

    /// "5 places, 3.4 mi walked".
    private func placesSummary(_ journalDay: JournalDay) -> String {
        let placeCount = journalDay.facts?.placeCount ?? journalDay.visits.count
        let places = String(localized: "\(placeCount) places")
        guard let walkedMeters = journalDay.facts?.walkedMeters, walkedMeters >= 100 else { return places }
        return places + ", " + String(localized: "\(JournalText.distanceText(meters: walkedMeters)) walked")
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
                ForEach(links) { link in
                    if let record = link.record {
                        JournalRecordRow(
                            symbolName: JournalText.symbol(forRecordKind: record.recordKind), title: record.title, detail: link.linkReason,
                            opening: JournalRecordOpening.of(record: record))
                    }
                }
            } header: {
                Text("Chosen for this day")
            }
        }
    }

    // MARK: Work and mail

    @ViewBuilder
    private func workSection(_ journalDay: JournalDay) -> some View {
        if !journalDay.codingSessions.isEmpty || !journalDay.documents.isEmpty {
            Section {
                ForEach(journalDay.codingSessions) { session in
                    JournalRecordRow(
                        symbolName: "chevron.left.forwardslash.chevron.right", title: session.title, detail: codingDetail(session))
                }
                ForEach(journalDay.documents) { document in
                    JournalRecordRow(
                        symbolName: "doc.text", title: document.title,
                        detail: document.isChosen && !document.linkReason.isEmpty ? document.linkReason : document.sourceName)
                }
            } header: {
                Text("Work")
            } footer: {
                if journalDay.documentTotal > journalDay.documents.count {
                    Text("\(journalDay.documentTotal - journalDay.documents.count) more were left out as routine.")
                }
            }
        }
    }

    /// "Claude Code, 3 h".
    private func codingDetail(_ session: JournalCodingSession) -> String {
        guard let started = ServerTime.date(session.startedAt), let ended = ServerTime.date(session.endedAt) else { return session.assistant }
        let minutes = Int(ended.timeIntervalSince(started) / 60)
        return minutes > 0 ? session.assistant + ", " + JournalText.durationText(minutes: minutes) : session.assistant
    }

    @ViewBuilder
    private func mailSection(_ journalDay: JournalDay) -> some View {
        let mattered = journalDay.mail.filter { $0.isChosen || $0.isImportant }
        if !mattered.isEmpty {
            Section {
                ForEach(mattered) { message in
                    JournalRecordRow(
                        symbolName: "envelope", title: message.subject, detail: mailDetail(message), opening: .mailItem(message.itemId))
                }
            } header: {
                Text("Mail that mattered")
            }
        }
    }

    private func mailDetail(_ message: JournalMail) -> String {
        message.linkReason.isEmpty ? message.fromName : message.fromName + ": " + message.linkReason
    }

    // MARK: Who and what the day talked with

    @ViewBuilder
    private func talkedSection(_ journalDay: JournalDay) -> some View {
        if !journalDay.conversations.isEmpty || !journalDay.chats.isEmpty {
            Section {
                ForEach(journalDay.conversations) { conversation in
                    JournalOpenRow(isOpenable: true, open: { app.openedConversationId = conversation.conversationId }) {
                        conversationRow(title: conversation.title, messageCount: conversation.messageCount)
                    }
                }
                ForEach(journalDay.chats) { chat in
                    conversationRow(title: chat.chatName, messageCount: chat.messageCount)
                }
            } header: {
                Text("Conversations")
            }
        }
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
            ForEach(journalDay.recordings) { recording in
                JournalRecordRow(
                    symbolName: "record.circle", title: recording.title, detail: recordingDetail(recording),
                    opening: .path("/recordings/\(recording.recordingId)"))
            }
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
        return workout.meters > 0 ? minutes + ", " + JournalText.distanceText(meters: workout.meters) : minutes
    }

    // MARK: Money

    private func moneySection(_ journalDay: JournalDay) -> some View {
        Section {
            ForEach(journalDay.transactions) { transaction in
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
            ForEach(journalDay.events) { event in
                JournalRecordRow(
                    symbolName: "calendar", title: event.title, detail: eventDetail(event), opening: .part("calendar"))
            }
            ForEach(journalDay.reminders) { reminder in
                JournalRecordRow(
                    symbolName: reminder.doneAt == nil ? "circle" : "checkmark.circle", title: reminder.title,
                    detail: reminder.doneAt == nil ? "" : String(localized: "Done"), opening: .part("calendar"))
            }
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
            ForEach(journalDay.shoppingTrips) { trip in
                JournalRecordRow(
                    symbolName: "cart", title: trip.listNames.isEmpty ? String(localized: "Shopping trip") : trip.listNames.joined(separator: ", "),
                    detail: String(localized: "\(trip.boughtCount) of \(trip.itemCount) bought"))
            }
        } header: {
            Text("Shopping")
        }
    }
}

/// JournalMomentRow is one thing the day held, with the time it happened.
struct JournalMomentRow: View {
    let moment: JournalMoment
    @Environment(AppModel.self) private var app

    var body: some View {
        JournalOpenRow(isOpenable: opening.isOpenable, open: { JournalRecordOpener.open(opening, in: app) }) {
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
}
