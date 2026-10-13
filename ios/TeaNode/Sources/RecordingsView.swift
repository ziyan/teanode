import AVFoundation
import AVKit
import SwiftUI
import TeaNodeKit
import UniformTypeIdentifiers

@MainActor
@Observable
final class RecordingsModel {
    let connection: Connection
    var recordings: [Recording] = []
    var localRecordings: [LocalRecording] = []
    var serverProgress: [String: RecordingServerProgress] = [:]
    var problemText: String?
    var doneText: String?
    var loadFailureText: String?
    var hasUnverifiedLocalOwner = false
    var isLoaded = false
    var isImporting = false
    var hasMoreRecordings = false
    var isLoadingPage = false

    init(connection: Connection) { self.connection = connection }

    /// The one list: the phone's recordings and the server's, merged by the identifier they share.
    var rows: [RecordingLibraryRow] {
        RecordingLibrary.rows(recordings: recordings, localRecordings: localRecordings, serverProgress: serverProgress)
    }

    var isStillChanging: Bool { rows.contains { $0.isStillChanging() } }

    /// What the library says while it has no rows.
    var phase: RecordingLibraryPhase {
        guard isLoaded else { return .loading }
        if let loadFailureText { return .failed(loadFailureText) }
        return .empty
    }

    func load() async {
        do {
            if (try? KeychainAccountStore().load(accountKey: connection.accountKey)?.cachedRecordingUserId) == nil {
                _ = try? await connection.recordingOwnerId()
            }
            await loadLocalRecordings()
            recordings = try await connection.recordings()
            hasMoreRecordings = recordings.count == 50
            isLoaded = true
            problemText = nil
            loadFailureText = nil
            RecordingUploads.shared.resume(connection: connection)
            await loadServerProgress()
        } catch {
            problemText = error.localizedDescription
            loadFailureText = error.localizedDescription
            isLoaded = true
        }
    }

    /// Reads the first page again without dropping the older pages already loaded, so a title the server just wrote shows.
    func refresh() async {
        await loadLocalRecordings()
        do {
            let page = try await connection.recordings()
            let freshIds = Set(page.map(\.id))
            recordings = page + recordings.filter { !freshIds.contains($0.id) }
            problemText = nil
        } catch { problemText = error.localizedDescription }
        await loadServerProgress()
    }

    /// Asks the server how the recordings that have no summary yet are doing: uploading, transcribing, failed or done.
    func loadServerProgress() async {
        let waiting = recordings.filter {
            RecordingLibrary.firstLine(of: $0.recordingSummary).isEmpty && serverProgress[$0.id]?.isSettled != true
        }
        for recording in waiting.prefix(20) {
            guard let view = try? await connection.recording(recording.id), view.recording.id == recording.id else { continue }
            serverProgress[recording.id] = RecordingServerProgress(view: view)
        }
    }

    var hasPendingLocalUploads: Bool { !RecordingLibraryProgress.pendingLocalRecordingIds(localRecordings).isEmpty }

    private func isCurrentOwner(_ recordingUserId: String) -> Bool {
        AppModel.shared.connection === connection
            && RecordingLibraryProgress.isCurrentOwner(
                accountKey: connection.accountKey, recordingUserId: recordingUserId,
                currentAccountKey: AppModel.shared.connection?.accountKey,
                currentRecordingUserId: AppModel.shared.account?.cachedRecordingUserId, isCancelled: Task.isCancelled)
            && (try? KeychainAccountStore().load(accountKey: connection.accountKey)?.cachedRecordingUserId) == recordingUserId
    }

    private func loadLocalRecordings() async {
        #if DEBUG
            if DemoServer.isRequested {
                localRecordings = DemoServer.localRecordings
                return
            }
        #endif
        guard let ownerId = try? KeychainAccountStore().load(accountKey: connection.accountKey)?.cachedRecordingUserId,
            isCurrentOwner(ownerId)
        else { return }
        do {
            let journal = try MeetingRecorder.makeJournal()
            let kept = try await journal.recordings(accountKey: connection.accountKey)
            guard isCurrentOwner(ownerId) else { return }
            localRecordings = kept.filter { $0.belongsTo(accountKey: connection.accountKey, recordingUserId: ownerId) }
            hasUnverifiedLocalOwner = localRecordings.count != kept.count
        } catch {
            if isCurrentOwner(ownerId) { localRecordings = [] }
        }
    }

    func refreshUploadProgress() async {
        guard let ownerId = try? KeychainAccountStore().load(accountKey: connection.accountKey)?.cachedRecordingUserId,
            isCurrentOwner(ownerId)
        else { return }
        let pendingIds = RecordingLibraryProgress.pendingLocalRecordingIds(localRecordings)
        await loadLocalRecordings()
        guard isCurrentOwner(ownerId) else { return }
        for local in localRecordings where pendingIds.contains(local.id) {
            guard isCurrentOwner(ownerId) else { return }
            guard let recordingId = local.recordingId, !recordings.contains(where: { $0.id == recordingId }) else { continue }
            let published = try? await connection.recording(recordingId).recording
            guard isCurrentOwner(ownerId) else { return }
            guard let published else { continue }
            recordings = RecordingLibraryProgress.mergingPublishedRecording(
                published, into: recordings, from: local, accountKey: connection.accountKey, recordingUserId: ownerId,
                currentAccountKey: AppModel.shared.connection?.accountKey, currentRecordingUserId: AppModel.shared.account?.cachedRecordingUserId,
                hasMoreRecordings: hasMoreRecordings, isCancelled: Task.isCancelled)
        }
    }

    func loadMore() async {
        guard hasMoreRecordings, !isLoadingPage, let beforeRecordingId = recordings.last?.id else { return }
        isLoadingPage = true
        defer { isLoadingPage = false }
        do {
            let page = try await connection.recordings(beforeRecordingId: beforeRecordingId)
            let knownIds = Set(recordings.map(\.id))
            recordings.append(contentsOf: page.filter { !knownIds.contains($0.id) })
            hasMoreRecordings = page.count == 50 && page.last?.id != beforeRecordingId
            problemText = nil
            await loadServerProgress()
        } catch { problemText = error.localizedDescription }
    }

    func delete(_ recording: Recording) async {
        do {
            try await connection.deleteRecording(recording.id)
            recordings.removeAll { $0.id == recording.id }
            doneText = String(localized: "Recording deleted.")
        } catch { problemText = error.localizedDescription }
    }

    func removeFromPhone(_ localRecording: LocalRecording) async {
        do {
            let journal = try MeetingRecorder.makeJournal()
            try await journal.removeLocalRecording(localRecording.id, accountKey: connection.accountKey)
            await load()
            doneText = String(localized: "Removed from this phone.")
        } catch { problemText = error.localizedDescription }
    }

    func importFile(_ source: URL) async {
        guard !isImporting else { return }
        isImporting = true
        defer { isImporting = false }
        let hasAccess = source.startAccessingSecurityScopedResource()
        defer { if hasAccess { source.stopAccessingSecurityScopedResource() } }
        do {
            guard let ownerId = try KeychainAccountStore().load(accountKey: connection.accountKey)?.cachedRecordingUserId else {
                throw TeaNodeError.refused(String(localized: "Open TeaNode online once to verify this account before recording offline."))
            }
            let contentType = try source.resourceValues(forKeys: [.contentTypeKey]).contentType
            guard let contentType, contentType.conforms(to: .audio) || contentType.conforms(to: .movie), let mimeType = contentType.preferredMIMEType
            else {
                throw TeaNodeError.refused(String(localized: "Choose an audio or video file."))
            }
            let sizeBytes = Int64(try source.resourceValues(forKeys: [.fileSizeKey]).fileSize ?? 0)
            let limits = try? await connection.recordingLimits()
            guard sizeBytes > 0, limits.map { sizeBytes <= $0.maximumMediaBytes } ?? true else {
                throw TeaNodeError.refused(
                    String(
                        localized: "The file exceeds this server's recording limit. Choose a smaller file or ask the operator to adjust the limit."))
            }
            let journal = try MeetingRecorder.makeJournal()
            guard try KeychainAccountStore().load(accountKey: connection.accountKey)?.cachedRecordingUserId == ownerId else {
                throw TeaNodeError.signedOut
            }
            // The file name stays on the original; the server names the recording once it is transcribed.
            var recording = LocalRecording(
                accountKey: connection.accountKey, recordingTitle: "",
                recordingOrigin: contentType.conforms(to: .movie) ? "videoImport" : "audioImport", recordingUserId: ownerId)
            let duration = try? await AVURLAsset(url: source).load(.duration)
            let durationSeconds = duration.map { CMTimeGetSeconds($0) } ?? 0
            var media = LocalRecordingMedia(
                originalFilename: source.lastPathComponent, originalContentType: mimeType,
                timelineStartMS: 0, mediaDurationMS: RecordingClock.durationMS(seconds: durationSeconds))
            media.originalSizeBytes = sizeBytes
            recording.localMedia.append(media)
            guard try KeychainAccountStore().load(accountKey: connection.accountKey)?.cachedRecordingUserId == ownerId else {
                throw TeaNodeError.signedOut
            }
            try await journal.create(recording)
            try await journal.importOriginal(from: source, recordingId: recording.id, mediaId: media.id, accountKey: connection.accountKey)
            let original = try await journal.originalURL(recordingId: recording.id, mediaId: media.id, accountKey: connection.accountKey)
            recording.localMedia[0].originalSha256 = try await Task.detached { try MeetingRecorder.digest(original) }.value
            recording.timelineDurationMS = media.mediaDurationMS
            try await journal.save(recording)
            await load()
        } catch { problemText = error.localizedDescription }
    }
}

struct RecordingsScreen: View {
    @State private var model: RecordingsModel
    @State private var isImporting = false
    @State private var recorder = MeetingRecorder.shared
    @State private var isShowingCapture = false
    @State private var isStarting = false
    @State private var removedRecording: LocalRecording?
    @State private var isRemoving = false
    @State private var deletedRecording: Recording?
    @State private var isDeleting = false
    @State private var localPlayback: LocalRecording?
    @State private var openedCitation: RecordingLink?

    init(connection: Connection) { _model = State(initialValue: RecordingsModel(connection: connection)) }

    var body: some View {
        NavigationStack {
            List {
                let rows = model.rows
                if rows.isEmpty {
                    RecordingLibraryPlaceholder(phase: model.phase)
                    .frame(maxWidth: .infinity)
                    .listRowBackground(Color.clear)
                }
                ForEach(rows) { row in
                    rowLink(row)
                        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                            if let recording = row.recording {
                                Button(role: .destructive) {
                                    deletedRecording = recording
                                    isDeleting = true
                                } label: {
                                    Label("Delete recording", systemImage: "trash")
                                }
                            }
                            if row.canRemoveFromPhone, let localRecording = row.localRecording {
                                Button {
                                    removedRecording = localRecording
                                    isRemoving = true
                                } label: {
                                    Label("Remove from this phone", systemImage: "iphone.slash")
                                }
                            }
                        }
                        .contextMenu {
                            if row.canRetryUpload {
                                Button {
                                    RecordingUploads.shared.resume(connection: model.connection)
                                    Task { await model.load() }
                                } label: {
                                    Label("Retry upload", systemImage: "arrow.clockwise")
                                }
                            }
                            if row.canPlayLocalOriginal, let localRecording = row.localRecording {
                                Button {
                                    localPlayback = localRecording
                                } label: {
                                    Label("Play local original", systemImage: "play")
                                }
                            }
                            if row.canRemoveFromPhone, let localRecording = row.localRecording {
                                Button(role: .destructive) {
                                    removedRecording = localRecording
                                    isRemoving = true
                                } label: {
                                    Label("Remove from this phone", systemImage: "iphone.slash")
                                }
                            }
                            if let recording = row.recording {
                                Button(role: .destructive) {
                                    deletedRecording = recording
                                    isDeleting = true
                                } label: {
                                    Label("Delete recording", systemImage: "trash")
                                }
                            }
                        }
                }
                if model.hasMoreRecordings {
                    Button {
                        Task { await model.loadMore() }
                    } label: {
                        Label("Load more recordings", systemImage: "chevron.down")
                    }
                    .disabled(model.isLoadingPage)
                }
                if model.hasUnverifiedLocalOwner {
                    Text(
                        "Some local recordings have a different or unverified owner. They are kept on this phone and will not be uploaded automatically."
                    )
                    .font(.footnote).foregroundStyle(.secondary)
                }
            }
            .navigationTitle("Recordings")
            .navigationDestination(item: $openedCitation) { citation in
                RecordingCitationScreen(connection: model.connection, citation: citation)
            }
            .onChange(of: AppModel.shared.openedRecordingLink, initial: true) { _, citation in
                guard let citation, AppModel.shared.openedRecordingAccountKey == model.connection.accountKey else { return }
                AppModel.shared.openedRecordingLink = nil
                openedCitation = citation
            }
            .onChange(of: AppModel.shared.isRecordingRequested) { startRequestedRecording() }
            .onChange(of: recorder.problemText) { _, text in
                guard let text else { return }
                model.problemText = text
                recorder.problemText = nil
            }
            .toolbar {
                MenuButton()
                ToolbarItem(placement: .primaryAction) {
                    Button {
                        isImporting = true
                    } label: {
                        Label("Import audio or video", systemImage: "square.and.arrow.down")
                    }
                }
            }
            .contentMargins(.bottom, 96, for: .scrollContent)
            .refreshable { await model.load() }
            .task {
                await recorder.recover(accountKey: model.connection.accountKey)
                await model.load()
                startRequestedRecording()
                while !Task.isCancelled {
                    do { try await Task.sleep(for: .seconds(5)) } catch { break }
                    if model.hasPendingLocalUploads { await model.refreshUploadProgress() }
                    if model.isStillChanging { await model.refresh() }
                }
            }
            .toast(done: $model.doneText, problem: $model.problemText)
            .safeAreaInset(edge: .bottom) {
                RecordingFloatingButton(
                    isRecording: recorder.hasSession, elapsedMS: recorder.elapsedMS, isStarting: isStarting,
                    onStart: startRecording, onShow: { isShowingCapture = true })
            }
            .sheet(isPresented: $isShowingCapture) {
                RecordingCaptureScreen {
                    await model.refresh()
                    model.doneText = String(localized: "Saved. TeaNode will upload the recording, then name it.")
                }
            }
            .sheet(item: $localPlayback) { recording in
                RecordingLocalPlaybackScreen(recording: recording, accountKey: model.connection.accountKey)
            }
            .fileImporter(isPresented: $isImporting, allowedContentTypes: [.audio, .movie]) { selected in
                switch selected {
                case .success(let file): Task { await model.importFile(file) }
                case .failure(let error): model.problemText = error.localizedDescription
                }
            }
            .confirmationDialog("Remove the local original?", isPresented: $isRemoving, titleVisibility: .visible) {
                Button("Remove from this phone", role: .destructive) {
                    if let removedRecording { Task { await model.removeFromPhone(removedRecording) } }
                }
            } message: {
                Text("Any audio not verified by the server will be lost. This does not delete the server recording.")
            }
            .confirmationDialog("Delete recording and its originals?", isPresented: $isDeleting, titleVisibility: .visible) {
                Button("Delete recording", role: .destructive) {
                    if let deletedRecording { Task { await model.delete(deletedRecording) } }
                }
            } message: {
                Text("Derived audio, transcripts and reference samples will be removed. Learned material will be reconciled.")
            }
            .withoutSwipeBack()
        }
    }

    /// A recording opens whatever it is: a live one on the capture screen, one the server has on its page, and one only the
    /// phone has on its local original.
    @ViewBuilder
    private func rowLink(_ row: RecordingLibraryRow) -> some View {
        if row.isLiveSession {
            Button {
                isShowingCapture = true
            } label: {
                RecordingRowView(row: row)
            }
            .buttonStyle(.plain)
        } else if let recording = row.recording {
            NavigationLink {
                RecordingDetailScreen(connection: model.connection, recording: recording)
            } label: {
                RecordingRowView(row: row)
            }
        } else {
            Button {
                localPlayback = row.localRecording
            } label: {
                RecordingRowView(row: row)
            }
            .buttonStyle(.plain)
            .disabled(!row.canPlayLocalOriginal)
        }
    }

    private func startRecording() {
        guard !isStarting else { return }
        isStarting = true
        Task {
            defer { isStarting = false }
            if !recorder.hasSession {
                await recorder.start(accountKey: model.connection.accountKey)
                await model.refresh()
            }
            if recorder.hasSession { isShowingCapture = true }
        }
    }

    /// Siri, the Action button and Control Center ask for a recording by setting a flag on the app model.
    private func startRequestedRecording() {
        guard AppModel.shared.isRecordingRequested else { return }
        AppModel.shared.isRecordingRequested = false
        startRecording()
    }
}

/// RecordingFloatingButton is the round record button that floats over the bottom of the library. While a recording is under
/// way it shows the time instead, and opens the recording screen.
struct RecordingFloatingButton: View {
    let isRecording: Bool
    let elapsedMS: Int64
    let isStarting: Bool
    let onStart: @MainActor () -> Void
    let onShow: @MainActor () -> Void

    var body: some View {
        GlassEffectContainer {
            if isRecording {
                Button(action: onShow) {
                    Label {
                        Text(Duration.milliseconds(elapsedMS).formatted(.time(pattern: .hourMinuteSecond)))
                            .monospacedDigit().contentTransition(.numericText())
                    } icon: {
                        Image(systemName: "waveform")
                    }
                    .font(.headline)
                    .padding(.horizontal, 8)
                }
                .buttonStyle(.glassProminent)
                .controlSize(.extraLarge)
                .accessibilityLabel("Show the recording")
            } else {
                Button(action: onStart) {
                    Image(systemName: "mic.fill")
                        .font(.largeTitle)
                        .padding(8)
                }
                .buttonStyle(.glassProminent)
                .buttonBorderShape(.circle)
                .controlSize(.extraLarge)
                .disabled(isStarting)
                .accessibilityLabel("Start recording")
            }
        }
        .padding(.bottom, 8)
        .sensoryFeedback(.impact, trigger: isRecording)
    }
}

/// RecordingRowView is one recording in the library: its name, a line of what was said, when it was made and where it
/// stands.
struct RecordingRowView: View {
    let row: RecordingLibraryRow

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(row.recordingTitle.isEmpty ? String(localized: "Untitled recording") : row.recordingTitle)
                .font(.headline)
                .foregroundStyle(row.recordingTitle.isEmpty ? .secondary : .primary)
                .lineLimit(2)
            if !row.recordingSummaryFirstLine.isEmpty {
                Text(row.recordingSummaryFirstLine).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
            }
            Text(verbatim: "\(row.recordedAt.formatted(date: .abbreviated, time: .shortened)) · \(RecordingDuration.text(row.timelineDurationMS))")
                .font(.caption).foregroundStyle(.secondary)
            RecordingStatusLabel(status: row.status)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}

/// RecordingStatusLabel says where a recording is in words and a symbol, so no state rests on a color.
struct RecordingStatusLabel: View {
    let status: RecordingRowStatus

    var body: some View {
        Label {
            Text(statusText)
        } icon: {
            Image(systemName: symbolName).symbolEffect(.pulse, isActive: status == .recording)
        }
        .font(.caption)
        .foregroundStyle(needsAttention ? AnyShapeStyle(.red) : AnyShapeStyle(.secondary))
    }

    private var needsAttention: Bool { status == .recording || status == .failed }

    private var statusText: String {
        switch status {
        case .recording: String(localized: "Recording")
        case .paused: String(localized: "Paused")
        case .uploading(let percent): String(localized: "Uploading \(percent)%")
        case .waitingForNetwork: String(localized: "Waiting for network")
        case .transcribing: String(localized: "Transcribing")
        case .ready: String(localized: "Ready")
        case .failed: String(localized: "Failed")
        }
    }

    private var symbolName: String {
        switch status {
        case .recording: "record.circle"
        case .paused: "pause.circle"
        case .uploading: "arrow.up.circle"
        case .waitingForNetwork: "wifi.slash"
        case .transcribing: "waveform"
        case .ready: "checkmark.circle"
        case .failed: "exclamationmark.triangle"
        }
    }
}

enum RecordingLibraryPhase: Equatable {
    case loading
    case empty
    case failed(String)
}

/// RecordingLibraryPlaceholder is what the library says while it has no rows to show.
struct RecordingLibraryPlaceholder: View {
    let phase: RecordingLibraryPhase

    var body: some View {
        switch phase {
        case .loading:
            ProgressView("Loading recordings").padding()
        case .empty:
            ContentUnavailableView(
                "No recordings", systemImage: "waveform", description: Text("Tap the microphone to start a recording, or import audio or video."))
        case .failed(let failureText):
            ContentUnavailableView("Cannot load recordings", systemImage: "exclamationmark.triangle", description: Text(failureText))
        }
    }
}

enum RecordingDuration {
    static func text(_ milliseconds: Int64) -> String {
        Duration.milliseconds(milliseconds).formatted(.time(pattern: milliseconds >= 3_600_000 ? .hourMinuteSecond : .minuteSecond))
    }
}

#if DEBUG
    extension RecordingLibraryRow {
        static func sample(
            _ recordingTitle: String, summary: String = "", status: RecordingRowStatus, durationMS: Int64 = 754_000, hoursAgo: Double = 2
        ) -> RecordingLibraryRow {
            RecordingLibraryRow(
                id: UUID().uuidString, recordingTitle: recordingTitle, recordingSummaryFirstLine: summary,
                recordedAt: Date(timeIntervalSinceNow: -hoursAgo * 3600), timelineDurationMS: durationMS, status: status)
        }

        static var samples: [RecordingLibraryRow] {
            [
                sample("", status: .recording, durationMS: 42_000, hoursAgo: 0),
                sample("", status: .paused, durationMS: 310_000, hoursAgo: 0.5),
                sample("", status: .uploading(percent: 40), hoursAgo: 1),
                sample("", status: .waitingForNetwork, hoursAgo: 1.5),
                sample("", status: .transcribing, hoursAgo: 2),
                sample(
                    "Quarterly planning with the design team", summary: "They chose the second layout and set a review for Friday.", status: .ready,
                    hoursAgo: 26),
                sample("", status: .ready, durationMS: 95_000, hoursAgo: 30),
                sample("Voice memo", status: .failed, durationMS: 20_000, hoursAgo: 50),
            ]
        }
    }

    #Preview("Library rows, light") {
        List(RecordingLibraryRow.samples) { RecordingRowView(row: $0) }.preferredColorScheme(.light)
    }
    #Preview("Library rows, dark") {
        List(RecordingLibraryRow.samples) { RecordingRowView(row: $0) }.preferredColorScheme(.dark)
    }
    #Preview("Library loading, light") { RecordingLibraryPlaceholder(phase: .loading).preferredColorScheme(.light) }
    #Preview("Library loading, dark") { RecordingLibraryPlaceholder(phase: .loading).preferredColorScheme(.dark) }
    #Preview("Library empty, light") { RecordingLibraryPlaceholder(phase: .empty).preferredColorScheme(.light) }
    #Preview("Library empty, dark") { RecordingLibraryPlaceholder(phase: .empty).preferredColorScheme(.dark) }
    #Preview("Library failed, light") {
        RecordingLibraryPlaceholder(phase: .failed("The server did not answer.")).preferredColorScheme(.light)
    }
    #Preview("Library failed, dark") {
        RecordingLibraryPlaceholder(phase: .failed("The server did not answer.")).preferredColorScheme(.dark)
    }
    #Preview("Record button, light") {
        RecordingFloatingButton(isRecording: false, elapsedMS: 0, isStarting: false, onStart: {}, onShow: {}).preferredColorScheme(.light)
    }
    #Preview("Record button, dark") {
        RecordingFloatingButton(isRecording: false, elapsedMS: 0, isStarting: false, onStart: {}, onShow: {}).preferredColorScheme(.dark)
    }
    #Preview("Recording under way, light") {
        RecordingFloatingButton(isRecording: true, elapsedMS: 754_000, isStarting: false, onStart: {}, onShow: {}).preferredColorScheme(.light)
    }
    #Preview("Recording under way, dark") {
        RecordingFloatingButton(isRecording: true, elapsedMS: 754_000, isStarting: false, onStart: {}, onShow: {}).preferredColorScheme(.dark)
    }
#endif

struct RecordingPlaybackScreen: View {
    let connection: Connection
    let media: RecordingMedia
    var passageStartMS: Int64? = nil
    var passageEndMS: Int64? = nil
    @State private var player: AVPlayer?
    @State private var localURL: URL?
    @State private var problemText: String?
    @State private var isCompatibleAudioPreview = false
    @State private var hasPlayableOriginal = false
    @State private var recordingUserId: String?
    @State private var isPlaybackVisible = false
    @State private var isChangingPlayback = false
    @State private var playbackTask: Task<Void, Never>?
    @State private var progressText = String(localized: "Downloading original for private playback")
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            Group {
                if let player {
                    VStack {
                        if isCompatibleAudioPreview {
                            Label("Compatible audio preview", systemImage: "waveform")
                            Text("Audio preview prepared locally; original unchanged.")
                                .font(.caption).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                        }
                        VideoPlayer(player: player)
                        if let problemText { Text(problemText).font(.caption).foregroundStyle(.secondary) }
                        if isCompatibleAudioPreview && hasPlayableOriginal {
                            Button {
                                guard !isChangingPlayback else { return }
                                isChangingPlayback = true
                                playbackTask = Task { await useOriginal() }
                            } label: {
                                if isChangingPlayback { ProgressView() } else { Label("Use original", systemImage: "play.rectangle") }
                            }
                            .disabled(isChangingPlayback || (try? requireCurrentConnection()) == nil)
                        } else if !isCompatibleAudioPreview {
                            Button {
                                guard !isChangingPlayback else { return }
                                isChangingPlayback = true
                                playbackTask = Task { await useAudioPreview() }
                            } label: {
                                if isChangingPlayback { ProgressView() } else { Label("Use audio preview", systemImage: "waveform") }
                            }
                            .disabled(isChangingPlayback || (try? requireCurrentConnection()) == nil)
                        }
                        if let localURL {
                            ShareLink(item: localURL) {
                                Label(
                                    isCompatibleAudioPreview
                                        ? String(localized: "Share full audio preview") : String(localized: "Share full original"),
                                    systemImage: "square.and.arrow.up")
                            }
                            .disabled(isChangingPlayback || (try? requireCurrentConnection()) == nil)
                        }
                    }
                } else if let problemText {
                    ContentUnavailableView("Cannot play recording", systemImage: "exclamationmark.triangle", description: Text(problemText))
                } else {
                    ProgressView(progressText)
                }
            }
            .navigationTitle(media.originalFilename)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } } }
            .task {
                guard !Task.isCancelled else { return }
                isPlaybackVisible = true
                defer {
                    if Task.isCancelled || player == nil {
                        if let localURL { try? FileManager.default.removeItem(at: localURL) }
                        localURL = nil
                    }
                }
                do {
                    #if DEBUG
                        if ["recording-compatible-playback", "recording-original-playback", "recording-original-preview"].contains(
                            DemoServer.screen ?? "")
                        {
                            localURL = try DemoServer.recordingPreviewURL()
                            isCompatibleAudioPreview = DemoServer.screen != "recording-original-playback"
                            hasPlayableOriginal = DemoServer.screen != "recording-compatible-playback"
                            player = AVPlayer(url: localURL!)
                            return
                        }
                    #endif
                    recordingUserId = AppModel.shared.account?.cachedRecordingUserId
                    try requireCurrentConnection()
                    let request = try await connection.recordingPlaybackRequest(mediaId: media.id)
                    localURL = try await downloadVerifiedMedia(
                        request, expectedSizeBytes: media.originalSizeBytes, expectedSha256: media.originalSha256,
                        fileExtension: (media.originalFilename as NSString).pathExtension)
                    guard let originalURL = localURL else { throw TeaNodeError.unreadableAnswer }
                    isCompatibleAudioPreview = false
                    var playableAsset = AVURLAsset(url: originalURL)
                    let isOriginalPlayable: Bool
                    do {
                        isOriginalPlayable = try await playableAsset.load(.isPlayable)
                    } catch let assetError as AVError
                        where [.fileFormatNotRecognized, .decoderNotFound, .formatUnsupported].contains(assetError.code)
                    {
                        isOriginalPlayable = false
                    }
                    try requireCurrentConnection()
                    hasPlayableOriginal = isOriginalPlayable
                    if !isOriginalPlayable { progressText = String(localized: "Preparing compatible audio preview") }
                    let compatiblePlayback = try await connection.compatibleRecordingPlayback(for: media, isOriginalPlayable: isOriginalPlayable)
                    try requireCurrentConnection()
                    if let playback = compatiblePlayback {
                        progressText = String(localized: "Downloading compatible audio preview")
                        let previewRequest = try await connection.recordingCompatiblePlaybackRequest(playback, for: media)
                        let previewURL = try await downloadVerifiedMedia(
                            previewRequest, expectedSizeBytes: playback.playbackSizeBytes, expectedSha256: playback.playbackSha256,
                            fileExtension: "wav")
                        try? FileManager.default.removeItem(at: originalURL)
                        localURL = previewURL
                        playableAsset = AVURLAsset(url: previewURL)
                        guard try await playableAsset.load(.isPlayable) else { throw TeaNodeError.unreadableAnswer }
                        isCompatibleAudioPreview = true
                    }
                    try requireCurrentConnection()
                    let preparedPlayer = try await makePlayer(for: playableAsset)
                    try requireCurrentConnection()
                    player = preparedPlayer
                } catch { if !Task.isCancelled { problemText = error.localizedDescription } }
            }
            .onDisappear {
                isPlaybackVisible = false
                playbackTask?.cancel()
                playbackTask = nil
                player?.pause()
                player = nil
                if let localURL { try? FileManager.default.removeItem(at: localURL) }
                localURL = nil
            }
        }
    }
    private func makePlayer(for playableAsset: AVURLAsset) async throws -> AVPlayer {
        if let passageStartMS, let passageEndMS {
            let startOffsetMS = media.playbackOffsetMS(for: passageStartMS)
            let endOffsetMS = media.playbackOffsetMS(for: passageEndMS)
            guard passageStartMS >= media.timelineStartMS, endOffsetMS > startOffsetMS else {
                throw TeaNodeError.unreadableAnswer
            }
            // A trimmed in-memory composition bounds the standard player's entire seekable range.
            let composition = AVMutableComposition()
            let timeRange = CMTimeRange(
                start: CMTime(value: startOffsetMS, timescale: 1000),
                duration: CMTime(value: endOffsetMS - startOffsetMS, timescale: 1000))
            let tracks = try await playableAsset.load(.tracks)
            for originalTrack in tracks {
                guard originalTrack.mediaType == .audio || originalTrack.mediaType == .video,
                    let passageTrack = composition.addMutableTrack(
                        withMediaType: originalTrack.mediaType, preferredTrackID: kCMPersistentTrackID_Invalid)
                else { continue }
                try passageTrack.insertTimeRange(timeRange, of: originalTrack, at: .zero)
                passageTrack.preferredTransform = try await originalTrack.load(.preferredTransform)
            }
            guard !composition.tracks.isEmpty else { throw TeaNodeError.unreadableAnswer }
            return AVPlayer(playerItem: AVPlayerItem(asset: composition))
        } else if passageStartMS != nil || passageEndMS != nil {
            throw TeaNodeError.unreadableAnswer
        } else {
            return AVPlayer(playerItem: AVPlayerItem(asset: playableAsset))
        }
    }

    private func useAudioPreview() async {
        defer { isChangingPlayback = false }
        guard player != nil, !isCompatibleAudioPreview else { return }
        var previewURL: URL?
        do {
            try requireCurrentConnection()
            let playback = try await connection.prepareRecordingPlayback(for: media)
            try requireCurrentConnection()
            let request = try await connection.recordingCompatiblePlaybackRequest(playback, for: media)
            previewURL = try await downloadVerifiedMedia(
                request, expectedSizeBytes: playback.playbackSizeBytes, expectedSha256: playback.playbackSha256, fileExtension: "wav")
            guard let previewURL else { throw TeaNodeError.unreadableAnswer }
            let asset = AVURLAsset(url: previewURL)
            guard try await asset.load(.isPlayable) else { throw TeaNodeError.unreadableAnswer }
            let preparedPlayer = try await makePlayer(for: asset)
            try requireCurrentConnection()
            player?.pause()
            if let localURL { try? FileManager.default.removeItem(at: localURL) }
            localURL = previewURL
            player = preparedPlayer
            isCompatibleAudioPreview = true
            problemText = nil
        } catch {
            if let previewURL { try? FileManager.default.removeItem(at: previewURL) }
            if !Task.isCancelled { problemText = error.localizedDescription }
        }
    }

    private func useOriginal() async {
        defer { isChangingPlayback = false }
        guard player != nil, isCompatibleAudioPreview, hasPlayableOriginal else { return }
        var originalURL: URL?
        do {
            try requireCurrentConnection()
            let request = try await connection.recordingPlaybackRequest(mediaId: media.id)
            originalURL = try await downloadVerifiedMedia(
                request, expectedSizeBytes: media.originalSizeBytes, expectedSha256: media.originalSha256,
                fileExtension: (media.originalFilename as NSString).pathExtension)
            guard let originalURL else { throw TeaNodeError.unreadableAnswer }
            let asset = AVURLAsset(url: originalURL)
            guard try await asset.load(.isPlayable) else { throw TeaNodeError.unreadableAnswer }
            let preparedPlayer = try await makePlayer(for: asset)
            try requireCurrentConnection()
            player?.pause()
            if let localURL { try? FileManager.default.removeItem(at: localURL) }
            localURL = originalURL
            player = preparedPlayer
            isCompatibleAudioPreview = false
            problemText = nil
        } catch {
            if let originalURL { try? FileManager.default.removeItem(at: originalURL) }
            if !Task.isCancelled { problemText = error.localizedDescription }
        }
    }

    private func requireCurrentConnection() throws {
        try Task.checkCancellation()
        guard isPlaybackVisible, AppModel.shared.connection === connection, let recordingUserId,
            RecordingLibraryProgress.isCurrentOwner(
                accountKey: connection.accountKey, recordingUserId: recordingUserId,
                currentAccountKey: AppModel.shared.connection?.accountKey,
                currentRecordingUserId: AppModel.shared.account?.cachedRecordingUserId),
            (try? KeychainAccountStore().load(accountKey: connection.accountKey)?.cachedRecordingUserId) == recordingUserId
        else { throw TeaNodeError.signedOut }
    }

    private func downloadVerifiedMedia(
        _ request: URLRequest, expectedSizeBytes: Int64, expectedSha256: String, fileExtension: String
    ) async throws -> URL {
        try requireCurrentConnection()
        let session = URLSession(configuration: .ephemeral)
        defer { session.invalidateAndCancel() }
        let (temporary, response) = try await session.download(for: request)
        defer { try? FileManager.default.removeItem(at: temporary) }
        let verifiedDigest = try await Task.detached { try MeetingRecorder.digest(temporary) }.value
        try requireCurrentConnection()
        guard (response as? HTTPURLResponse)?.statusCode == 200,
            Int64(try temporary.resourceValues(forKeys: [.fileSizeKey]).fileSize ?? 0) == expectedSizeBytes,
            verifiedDigest == expectedSha256
        else { throw TeaNodeError.unreadableAnswer }
        let destination = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).appendingPathExtension(fileExtension)
        try FileManager.default.moveItem(at: temporary, to: destination)
        return destination
    }

}

private struct RecordingLocalPlaybackScreen: View {
    let recording: LocalRecording
    let accountKey: String
    @Environment(\.dismiss) private var dismiss
    @State private var player: AVPlayer?
    @State private var problemText: String?
    @State private var selectedMediaId: UUID?

    var body: some View {
        NavigationStack {
            List {
                if let player { VideoPlayer(player: player).frame(minHeight: 220) }
                Section("Local original media") {
                    ForEach(recording.localMedia.filter { !$0.isDurablyUploaded && $0.originalSha256 != nil }) { media in
                        Button {
                            Task { await play(media) }
                        } label: {
                            Label(
                                Duration.milliseconds(media.timelineStartMS).formatted(.time(pattern: .hourMinuteSecond)),
                                systemImage: selectedMediaId == media.id ? "speaker.wave.2" : "play")
                        }
                    }
                }
                if let problemText { Text(problemText).foregroundStyle(.secondary) }
            }
            .navigationTitle("Local original")
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Close") { dismiss() } } }
            .task {
                if let media = recording.localMedia.first(where: { !$0.isDurablyUploaded && $0.originalSha256 != nil }) { await play(media) }
            }
            .onDisappear {
                player?.pause()
                player = nil
            }
        }
    }

    private func play(_ media: LocalRecordingMedia) async {
        do {
            guard let retained = try KeychainAccountStore().load(accountKey: accountKey),
                recording.belongsTo(accountKey: accountKey, recordingUserId: retained.cachedRecordingUserId)
            else { throw TeaNodeError.signedOut }
            let journal = try MeetingRecorder.makeJournal()
            let source = try await journal.originalURL(recordingId: recording.id, mediaId: media.id, accountKey: accountKey)
            guard FileManager.default.fileExists(atPath: source.path) else { throw TeaNodeError.unreadableAnswer }
            player?.pause()
            player = AVPlayer(url: source)
            selectedMediaId = media.id
            player?.play()
            problemText = nil
        } catch { problemText = error.localizedDescription }
    }
}
