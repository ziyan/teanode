import SwiftUI
import TeaNodeKit

// Sharing a recording with a person in a circle, or with a whole circle, and
// reading the ones others shared. The dashboard, the command line and the
// agent's recording tool do the same under the same two levels. What is shared
// is never the audio, a voice sample or the contact a speaker is tied to.

extension RecordingShareLevel {
    /// What a share at this level is called.
    var title: String {
        switch self {
        case .summary: String(localized: "Summary")
        case .transcript: String(localized: "Transcript")
        }
    }

    /// What somebody a recording is shared at this level sees.
    var explanation: String {
        switch self {
        case .summary: String(localized: "They see the title, date, length, the summary and the names of the speakers you named.")
        case .transcript: String(localized: "They see the summary and the words, with your corrections. Never the audio.")
        }
    }
}

/// RecordingShareScreen is who one recording is shared with, at what level, with a way to stop, and a form to share it with
/// someone else: one person in a circle they share, or a whole circle. It says first that the others who speak did not agree.
struct RecordingShareScreen: View {
    let connection: Connection
    let recordingId: String
    @State private var shares: [RecordingShare] = []
    @State private var people: [CalendarSharePerson] = []
    @State private var circles: [CalendarShareCircle] = []
    @State private var isLoaded = false
    @State private var loadProblemText: String?
    @State private var chosen: ShareTarget?
    @State private var level: RecordingShareLevel = .summary
    @State private var stopping: RecordingShare?
    @State private var isWorking = false
    @State private var problemText: String?
    @State private var doneText: String?

    var body: some View {
        List {
            Section {
                Text("The other people who speak in this recording have not agreed to it being shared. Share it only if they would be fine with that.")
                    .font(.callout)
            }
            Section {
                if isLoaded && shares.isEmpty {
                    Text("This recording is not shared with anybody yet.")
                        .foregroundStyle(.secondary)
                }
                ForEach(shares) { share in
                    VStack(alignment: .leading, spacing: 2) {
                        Text(Self.name(of: share))
                        Text(share.level?.title ?? share.recordingShareLevel)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                    .accessibilityElement(children: .combine)
                    .confirmationDialog(
                        "Stop sharing", isPresented: Binding(get: { stopping?.id == share.id }, set: { if !$0 { stopping = nil } }),
                        titleVisibility: .visible, presenting: stopping
                    ) { share in
                        Button("Stop sharing", role: .destructive) { stop(share) }
                    } message: { share in
                        Text("\(Self.name(of: share)) will no longer see this recording.")
                    }
                    .swipeActions {
                        Button("Stop sharing", systemImage: "person.badge.minus", role: .destructive) { stopping = share }
                            .tint(.red)
                    }
                    .contextMenu {
                        Button("Stop sharing", systemImage: "person.badge.minus", role: .destructive) { stopping = share }
                            .tint(.red)
                    }
                }
            } header: {
                Text("Shared with")
            } footer: {
                if !shares.isEmpty {
                    Text("Swipe left, or touch and hold, to stop sharing.")
                }
            }
            if isLoaded && people.isEmpty && circles.isEmpty {
                Section {
                    Text("You do not share a circle with anybody yet. Make or join a circle to share a recording.")
                        .foregroundStyle(.secondary)
                    OpenCirclesLink(connection: connection)
                }
            } else if isLoaded {
                Section {
                    ShareTargetPicker(people: people, circles: circles, chosen: $chosen)
                } footer: {
                    Text("Choose a person, or a whole circle.")
                }
                Section {
                    Picker("How much", selection: $level) {
                        ForEach(RecordingShareLevel.grantable, id: \.self) { each in
                            Text(each.title).tag(each)
                        }
                    }
                    .pickerStyle(.inline)
                    .labelsHidden()
                } header: {
                    Text("How much")
                } footer: {
                    Text(level.explanation)
                }
                Section {
                    Button("Share") { share() }
                        .disabled(isWorking || chosen == nil)
                }
            }
        }
        .navigationTitle("Share this recording")
        .navigationBarTitleDisplayMode(.inline)
        .overlay {
            if let loadProblemText, !isLoaded {
                LoadFailedView(problemText: loadProblemText) { await load() }
            } else if !isLoaded {
                ProgressView()
            }
        }
        .refreshable { await load() }
        .task { await load() }
        .toast(done: $doneText, problem: $problemText)
    }

    /// Who a share is with, as a sentence names them.
    private static func name(of share: RecordingShare) -> String {
        share.isWithCircle ? String(localized: "Everyone in \(share.granteeDisplayName)") : share.granteeDisplayName
    }

    private func load() async {
        do {
            async let readShares = connection.recordingShares(recordingId: recordingId)
            async let readPeople = connection.calendarSharePeople()
            async let readCircles = connection.calendarShareCircles()
            (shares, people, circles) = try await (readShares, readPeople, readCircles)
            isLoaded = true
            loadProblemText = nil
        } catch {
            // With the screen already drawn a failed refresh leaves it be and says so.
            if isLoaded { problemText = error.loadProblemText } else { loadProblemText = error.loadProblemText }
        }
    }

    private func share() {
        guard let target = chosen else { return }
        let name = ShareTargetPicker.name(of: target, people: people, circles: circles)
        isWorking = true
        Task {
            do {
                try await connection.shareRecording(
                    recordingId: recordingId, granteeUserId: target.granteeUserId, granteeCircleId: target.granteeCircleId, level: level)
                doneText = String(localized: "Shared with \(name)")
                chosen = nil
                level = .summary
                shares = try await connection.recordingShares(recordingId: recordingId)
            } catch {
                problemText = error.refusalReason(orSay: String(localized: "That did not work. Check your connection and try again."))
            }
            isWorking = false
        }
    }

    private func stop(_ share: RecordingShare) {
        let name = Self.name(of: share)
        isWorking = true
        Task {
            do {
                try await connection.unshareRecording(shareId: share.id)
                doneText = String(localized: "No longer shared with \(name)")
                shares = try await connection.recordingShares(recordingId: recordingId)
            } catch {
                problemText = error.refusalReason(orSay: String(localized: "That did not work. Check your connection and try again."))
            }
            isWorking = false
        }
    }
}

/// SharedRecordingRow is one recording somebody shared: its title, who shared it, when it was made and how long it is, and
/// how much of it is shared.
struct SharedRecordingRow: View {
    let recording: SharedRecording

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(recording.recordingTitle.isEmpty ? String(localized: "Untitled recording") : recording.recordingTitle)
                .font(.body)
            Text(verbatim: subtitle)
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        .accessibilityElement(children: .combine)
    }

    private var subtitle: String {
        var parts = [String(localized: "Shared by \(recording.ownerDisplayName)")]
        if let recorded = ServerTime.date(recording.recordedAt) { parts.append(recorded.formatted(date: .abbreviated, time: .omitted)) }
        if recording.timelineDurationMS > 0 { parts.append(RecordingDuration.text(recording.timelineDurationMS)) }
        parts.append(recording.level?.title ?? recording.recordingShareLevel)
        return parts.joined(separator: " · ")
    }
}

/// SharedRecordingsScreen is the recordings other people shared with the person, newest first, a page at a time.
struct SharedRecordingsScreen: View {
    let connection: Connection
    @State private var recordings: [SharedRecording] = []
    @State private var isLoaded = false
    @State private var loadProblemText: String?
    @State private var hasMore = false
    @State private var isLoadingPage = false
    @State private var problemText: String?

    private static let pageSize = 50

    var body: some View {
        List {
            ForEach(recordings) { recording in
                NavigationLink {
                    SharedRecordingScreen(connection: connection, recording: recording)
                } label: {
                    SharedRecordingRow(recording: recording)
                }
                .onAppear {
                    if recording.id == recordings.last?.id, hasMore, !isLoadingPage {
                        Task { await loadMore() }
                    }
                }
            }
        }
        .navigationTitle("Recordings shared with you")
        .navigationBarTitleDisplayMode(.inline)
        .overlay {
            if let loadProblemText, !isLoaded {
                LoadFailedView(problemText: loadProblemText) { await load() }
            } else if !isLoaded {
                ProgressView()
            } else if recordings.isEmpty {
                ContentUnavailableView(
                    "Nothing shared with you", systemImage: "person.2",
                    description: Text("Recordings other people choose to share with you are here."))
            }
        }
        .refreshable { await load() }
        .task { await load() }
        .toast(done: .constant(nil), problem: $problemText)
    }

    private func load() async {
        do {
            let page = try await connection.sharedRecordings(pageSize: Self.pageSize)
            recordings = page
            hasMore = page.count == Self.pageSize
            isLoaded = true
            loadProblemText = nil
        } catch {
            if isLoaded { problemText = error.loadProblemText } else { loadProblemText = error.loadProblemText }
        }
    }

    private func loadMore() async {
        guard let beforeRecordingId = recordings.last?.id else { return }
        isLoadingPage = true
        defer { isLoadingPage = false }
        do {
            let page = try await connection.sharedRecordings(beforeRecordingId: beforeRecordingId, pageSize: Self.pageSize)
            let knownIds = Set(recordings.map(\.id))
            recordings.append(contentsOf: page.filter { !knownIds.contains($0.id) })
            hasMore = page.count == Self.pageSize
        } catch { problemText = error.loadProblemText }
    }
}

/// SharedRecordingScreen is one recording somebody shared, as far as the level they gave goes: the title, who shared it, the
/// summary, who spoke and, at the transcript level, what was said, a page at a time. There is no audio, no way to name a
/// speaker and nothing to play: none of that is the reader's.
struct SharedRecordingScreen: View {
    let connection: Connection
    let recording: SharedRecording
    @State private var reading: SharedRecording?
    @State private var passages: [SharedRecordingPassage] = []
    @State private var hasMorePassages = false
    @State private var isLoadingPage = false
    @State private var loadProblemText: String?
    @State private var problemText: String?

    private static let pageSize = 50

    private var shown: SharedRecording { reading ?? recording }

    private var displayedTitle: String {
        shown.recordingTitle.isEmpty ? String(localized: "Untitled recording") : shown.recordingTitle
    }

    private var metaLine: String {
        var parts = [String(localized: "Shared by \(shown.ownerDisplayName)")]
        if let recorded = ServerTime.date(shown.recordedAt) { parts.append(recorded.formatted(date: .abbreviated, time: .omitted)) }
        if shown.timelineDurationMS > 0 { parts.append(RecordingDuration.text(shown.timelineDurationMS)) }
        parts.append(shown.level?.title ?? shown.recordingShareLevel)
        return parts.joined(separator: " · ")
    }

    private var turns: [SharedRecordingTurn] { SharedRecordingTurn.turns(of: passages) }

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 16) {
                Text(displayedTitle).font(.title.bold()).fixedSize(horizontal: false, vertical: true)
                Text(verbatim: metaLine).font(.subheadline).foregroundStyle(.secondary)
                if !shown.recordingSummary.isEmpty { Text(shown.recordingSummary).font(.body) }
                if let speakerNames = reading?.speakerNames, !speakerNames.isEmpty {
                    Text("Speakers: \(ListFormatter.localizedString(byJoining: speakerNames))")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                if shown.level == .summary {
                    Text("The transcript was not shared.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                ForEach(turns) { turn in
                    SharedRecordingTurnView(turn: turn)
                        .onAppear {
                            if turn.id == turns.last?.id, hasMorePassages, !isLoadingPage {
                                Task { await loadMorePassages() }
                            }
                        }
                }
                if isLoadingPage { ProgressView().frame(maxWidth: .infinity) }
            }
            .padding(.horizontal)
            .readableWidth()
        }
        .navigationTitle("Shared recording")
        .navigationBarTitleDisplayMode(.inline)
        .overlay {
            if let loadProblemText, reading == nil {
                LoadFailedView(problemText: loadProblemText) { await load() }
            } else if reading == nil {
                ProgressView()
            }
        }
        .refreshable { await load() }
        .task { await load() }
        .toast(done: .constant(nil), problem: $problemText)
    }

    private func load() async {
        do {
            let read = try await connection.sharedRecording(recordingId: recording.recordingId, pageSize: Self.pageSize)
            reading = read
            passages = read.passages ?? []
            hasMorePassages = read.hasMorePassages ?? false
            loadProblemText = nil
        } catch {
            if reading != nil { problemText = error.loadProblemText } else { loadProblemText = error.loadProblemText }
        }
    }

    private func loadMorePassages() async {
        isLoadingPage = true
        defer { isLoadingPage = false }
        do {
            let read = try await connection.sharedRecording(
                recordingId: recording.recordingId, passageOffset: passages.count, pageSize: Self.pageSize)
            passages.append(contentsOf: read.passages ?? [])
            hasMorePassages = read.hasMorePassages ?? false
        } catch { problemText = error.loadProblemText }
    }
}

/// SharedRecordingTurnView is what one speaker said before someone else spoke: their name and the time, over the words in a
/// grey bubble on the left, as the recording's own page draws other people's.
struct SharedRecordingTurnView: View {
    let turn: SharedRecordingTurn

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Text(verbatim: turn.speakerName)
                    .font(.footnote.weight(.semibold))
                Text(verbatim: RecordingDuration.text(turn.startMS))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            Text(verbatim: turn.text)
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 20, style: .continuous))
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.trailing, 40)
        .accessibilityElement(children: .combine)
    }
}
